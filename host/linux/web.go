//go:build linux && !android

package linux

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A WebView is a WebKitWebView (WebKitGTK 6.0, opened the first time one
// is made). The page posts to Go with window.asuka.postMessage(text), as
// on the other platforms (a script message handler); Preload is a user
// script at document start in every frame; Block is a content filter (the
// rule list format of Safari's content blockers), the page loading once
// it's compiled; Requests follows resource-load-started; a response the
// page can't show is a Download, and a page for a new window NewWindow
// (the "create" signal). The app's assets load as file:// URLs. Its
// prefers-color-scheme follows the app's (libadwaita tells WebKit).
//
// Without WebKitGTK the view shows nothing and reports Failed once,
// saying what to install.
type web struct {
	n        *node
	view     Ptr          // the WebKitWebView (n.inner), the host's reference
	ucc      Ptr          // its user content manager
	handlers []webHandler // its signal handlers
	reqID    uint64       // resource-load-started's handler, with Requests
	block    int          // Block's filters compiled, to keep only the last
	held     func()       // a load waiting for Block's filter to compile
	pending  int          // the filter compiling (its block), 0 for none
	queued   bool         // a WebState report waits for the loop to be idle
	failed   bool         // the load in progress failed: no Navigated
	missing  bool         // no WebKitGTK
	reported bool         // its absence reported
	closed   bool
}

const bridgeJS = `window.asuka = { postMessage: function (m) { window.webkit.messageHandlers.asuka.postMessage(String(m)); } };`

var webKitWarned bool

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindWebView {
			return false
		}
		h.makeWeb(n)
		return true
	})
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		if n.kind != ops.KindWebView {
			return false
		}
		if w, ok := n.ext.(*web); ok {
			w.setProp(h, p, v)
		}
		return true
	})
	commandHooks = append(commandHooks, func(h *Host, n *node, c ops.CommandID, args []byte) bool {
		w, ok := n.ext.(*web)
		if !ok {
			return false
		}
		switch c {
		case ops.CmdEval:
			if w.view != 0 {
				WebKitWebViewEvaluateJavascript(w.view, string(args), -1, 0, 0, 0, 0, 0)
			}
		case ops.CmdWebNavigate:
			if w.view != 0 && len(args) > 0 {
				w.navigate(args[0])
			}
		default:
			return false
		}
		return true
	})
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["web"] = h.webModule()
		m["js"] = h.jsModule()
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		if w, ok := n.ext.(*web); ok {
			w.close()
		}
	})
}

// webKit opens WebKitGTK, saying once on stderr when it can't.
func webKit() error {
	err := LoadWebKit()
	if err != nil && !webKitWarned {
		webKitWarned = true
		fmt.Fprintln(os.Stderr, "asuka:", err)
	}
	return err
}

func (h *Host) makeWeb(n *node) {
	w := &web{n: n}
	n.ext = w
	if webKit() != nil {
		w.missing = true
		return
	}
	v := ObjectRefSink(WebKitWebViewNew())
	w.view = v
	w.ucc = WebKitWebViewGetUserContentManager(v)
	settings := WebKitWebViewGetSettings(v)
	WebKitSettingsSetEnableDeveloperExtras(settings, true)
	// Connected before the handler is registered, as WebKit asks.
	w.connect(w.ucc, "script-message-received::asuka", func(val uintptr) {
		h.emit(n, ops.Event{Kind: ops.EventMessage, Str: GoStringFree(JSCValueToString(val))})
	})
	WebKitUserContentManagerRegisterScriptMessageHandler(w.ucc, "asuka", 0)
	w.addScript(bridgeJS)
	on := func(signal string, f any) { w.connect(v, signal, f) }
	on("load-changed", func(ev uintptr) {
		switch ev {
		case WebKitLoadStarted:
			w.failed = false
		case WebKitLoadFinished:
			if !w.failed {
				h.emit(n, ops.Event{Kind: ops.EventNavigated, Str: GoString(WebKitWebViewGetURI(v))})
			}
		}
		h.queueWebState(w)
	})
	on("load-failed", func(ev, uri, gerr uintptr) bool {
		w.failed = true
		dom, code, msg := WebKitError(gerr)
		// Stopped, or a response that's a Download: not a failure.
		if !(dom == WebKitNetworkErrorQuark() && code == WebKitNetworkErrorCancelled) &&
			!(dom == WebKitPolicyErrorQuark() && code == WebKitPolicyErrorFrameLoadInterrupted) {
			h.emit(n, ops.Event{Kind: ops.EventFailed, Str: msg})
		}
		h.queueWebState(w)
		return true // no error page, as WKWebView shows none
	})
	on("web-process-terminated", func(reason uintptr) {
		h.emit(n, ops.Event{Kind: ops.EventFailed, Str: "the page's web process ended"})
		h.queueWebState(w)
	})
	on("decide-policy", func(d, kind uintptr) bool {
		if kind != WebKitPolicyResponse {
			return false
		}
		return h.decideResponse(w, d)
	})
	on("create", func(action uintptr) uintptr {
		// A page for a new window: the app's NewWindow, or this view.
		u := GoString(WebKitURIRequestGetURI(WebKitNavigationActionGetRequest(action)))
		if n.listening(ops.EventNewWindow) {
			h.emit(n, ops.Event{Kind: ops.EventNewWindow, Str: u})
		} else if u != "" {
			WebKitWebViewLoadURI(v, u)
		}
		return 0
	})
	for _, p := range []string{"notify::uri", "notify::title", "notify::estimated-load-progress", "notify::is-loading"} {
		on(p, func(uintptr) { h.queueWebState(w) })
	}
	if bf := WebKitWebViewGetBackForwardList(v); bf != 0 {
		w.connect(bf, "changed", func(a, b uintptr) { h.queueWebState(w) })
	}
	h.setInner(n, v)
}

// webHandler is a signal handler and the object it's connected to.
type webHandler struct {
	obj Ptr
	id  uint64
}

func (w *web) connect(obj Ptr, signal string, f any) {
	w.handlers = append(w.handlers, webHandler{obj, Connect(obj, signal, f)})
}

func (w *web) addScript(js string) {
	s := WebKitUserScriptNew(js, WebKitInjectAllFrames, WebKitInjectAtDocStart, 0, 0)
	WebKitUserContentManagerAddScript(w.ucc, s)
	WebKitUserScriptUnref(s)
}

func (w *web) setProp(h *Host, p ops.PropID, v *ops.Value) {
	if w.missing {
		if (p == ops.PropWebViewURL || p == ops.PropWebViewHTML) && !w.reported {
			w.reported = true
			n := w.n
			h.afterCore(func() {
				h.emit(n, ops.Event{Kind: ops.EventFailed, Str: LoadWebKit().Error()})
			})
		}
		return
	}
	switch p {
	case ops.PropWebViewURL:
		u := v.Str // v is the batch's, reused
		w.hold(func() { w.load(h, u) })
	case ops.PropWebViewHTML:
		html := v.Str
		w.hold(func() { WebKitWebViewLoadHTML(w.view, html, 0) })
	case ops.PropWebViewPreload:
		WebKitUserContentManagerRemoveAllScripts(w.ucc)
		w.addScript(bridgeJS)
		if v.Str != "" {
			w.addScript(v.Str)
		}
	case ops.PropWebViewUserAgent:
		settings := WebKitWebViewGetSettings(w.view)
		if v.Str == "" {
			WebKitSettingsSetUserAgent(settings, 0) // WebKit's own
		} else {
			ua := CString(v.Str)
			WebKitSettingsSetUserAgent(settings, Addr(ua))
		}
	case ops.PropWebViewBlock:
		w.setBlock(h, v.Bytes)
	case ops.PropWebViewRequests:
		on := v.Int != 0
		if on && w.reqID == 0 {
			n := w.n
			w.reqID = Connect(w.view, "resource-load-started", func(res, req uintptr) {
				h.emit(n, ops.Event{Kind: ops.EventRequest, Str: GoString(WebKitURIRequestGetURI(req))})
			})
		} else if !on && w.reqID != 0 {
			Disconnect(w.view, w.reqID)
			w.reqID = 0
		}
	}
}

// hold runs a load now, or once Block's filter has compiled, so the
// page's first requests are blocked too.
func (w *web) hold(load func()) {
	if w.compiling() {
		w.held = load
		return
	}
	load()
}

func (w *web) load(h *Host, s string) {
	if s == "" {
		return
	}
	if i := strings.Index(s, ":"); i > 0 && !filepath.IsAbs(s) {
		switch strings.ToLower(s[:i]) {
		case "http", "https", "file", "about", "data", "blob":
			WebKitWebViewLoadURI(w.view, s)
			return
		}
	}
	// An asset (or a file): its relative links are its directory's.
	p := s
	if !filepath.IsAbs(p) {
		p = filepath.Join(h.assets, filepath.FromSlash(s))
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	WebKitWebViewLoadURI(w.view, (&url.URL{Scheme: "file", Path: p}).String())
}

func (w *web) navigate(to byte) {
	switch to {
	case 0:
		WebKitWebViewGoBack(w.view)
	case 1:
		WebKitWebViewGoForward(w.view)
	case 2:
		WebKitWebViewReload(w.view)
	case 3:
		WebKitWebViewStopLoading(w.view)
	}
}

func (w *web) close() {
	if w.closed {
		return
	}
	w.closed = true
	w.block++ // a filter still compiling is dropped
	w.held = nil
	if w.view == 0 {
		return
	}
	for _, hd := range w.handlers {
		Disconnect(hd.obj, hd.id)
	}
	w.handlers = nil
	if w.reqID != 0 {
		Disconnect(w.view, w.reqID)
		w.reqID = 0
	}
	WebKitWebViewStopLoading(w.view)
	ObjectUnref(w.view) // its parent's reference goes with the node's view
	w.view, w.ucc = 0, 0
}

// queueWebState reports the page's state once for the changes of a turn
// of the main loop.
func (h *Host) queueWebState(w *web) {
	if w.queued || w.closed || !w.n.listening(ops.EventWebState) {
		return
	}
	w.queued = true
	IdleAdd(PriorityDefault, func() bool {
		w.queued = false
		if !w.closed {
			h.webState(w)
		}
		return false
	})
}

func (h *Host) webState(w *web) {
	v := w.view
	var b []byte
	str := func(s string) {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	str(GoString(WebKitWebViewGetURI(v)))
	str(GoString(WebKitWebViewGetTitle(v)))
	var flags byte
	if WebKitWebViewIsLoading(v) {
		flags |= 1
	}
	if WebKitWebViewCanGoBack(v) {
		flags |= 2
	}
	if WebKitWebViewCanGoForward(v) {
		flags |= 4
	}
	b = append(b, flags)
	progress := 1.0
	if flags&1 != 0 {
		progress = WebKitWebViewGetEstimatedLoadProgress(v)
	}
	b = binary.LittleEndian.AppendUint32(b, math.Float32bits(float32(progress)))
	h.emit(w.n, ops.Event{Kind: ops.EventWebState, Bytes: b})
}

// decideResponse answers a response's policy decision: the main frame's
// that the view can't show, or an attachment, is ignored and reported as
// a Download; the rest load (WebKit's own decision).
func (h *Host) decideResponse(w *web, d Ptr) bool {
	if !WebKitResponsePolicyDecisionIsMainFrameMain(d) {
		return false
	}
	r := WebKitResponsePolicyDecisionGetResponse(d)
	var disposition, length string
	if hdrs := WebKitURIResponseGetHTTPHeaders(r); hdrs != 0 {
		disposition = GoString(SoupMessageHeadersGetOne(hdrs, "Content-Disposition"))
		length = GoString(SoupMessageHeadersGetOne(hdrs, "Content-Length"))
	}
	if WebKitResponsePolicyDecisionIsMimeTypeSupported(d) && !strings.HasPrefix(strings.ToLower(strings.TrimSpace(disposition)), "attachment") {
		return false
	}
	WebKitPolicyDecisionIgnore(d)
	u := GoString(WebKitURIResponseGetURI(r))
	name := GoString(WebKitURIResponseGetSuggestedFilename(r))
	if name == "" {
		if pu, err := url.Parse(u); err == nil {
			name = path.Base(pu.Path)
		}
		if name == "" || name == "/" || name == "." {
			name = "download"
		}
	}
	size := int64(-1)
	if length != "" {
		size = int64(WebKitURIResponseGetContentLength(r))
	}
	var b []byte
	str := func(s string) {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	str(u)
	str(GoString(WebKitURIResponseGetMimeType(r)))
	str(name)
	b = binary.LittleEndian.AppendUint64(b, uint64(size))
	h.emit(w.n, ops.Event{Kind: ops.EventDownload, Bytes: b})
	return true
}

// Block: a content filter.

var filterStore Ptr

// setBlock compiles Block's patterns (u32 count, then u32-length
// strings) into a content filter: a host blocks it and its subdomains, a
// part of a URL any URL with it.
func (w *web) setBlock(h *Host, b []byte) {
	WebKitUserContentManagerRemoveAllFilters(w.ucc)
	w.block++
	gen := w.block
	var rules []map[string]any
	if len(b) >= 4 {
		count := int(binary.LittleEndian.Uint32(b))
		b = b[4:]
		for range count {
			if len(b) < 4 {
				break
			}
			l := int(binary.LittleEndian.Uint32(b))
			if l > len(b)-4 {
				break
			}
			p := string(b[4 : 4+l])
			b = b[4+l:]
			esc := regexp.QuoteMeta(p)
			filter := esc
			if !strings.Contains(p, "/") {
				filter = `^[a-z]+://([^/:]+\.)?` + esc + `[:/]`
			}
			rules = append(rules, map[string]any{"trigger": map[string]any{"url-filter": filter}, "action": map[string]any{"type": "block"}})
		}
	}
	js, err := json.Marshal(rules)
	if len(rules) == 0 || err != nil {
		w.released()
		return
	}
	if filterStore == 0 {
		dir, err := os.UserCacheDir()
		if err != nil {
			dir = os.TempDir()
		}
		filterStore = WebKitUserContentFilterStoreNew(filepath.Join(dir, h.appName, "content-filters"))
	}
	w.pending = gen
	bytes := BytesNew(&js[0], uintptr(len(js)))
	id := fmt.Sprintf("asuka-block-%d", w.n.id)
	Async(func(a AsyncCall) {
		WebKitUserContentFilterStoreSave(filterStore, id, bytes, 0, a.Callback, a.Data)
	}, func(src, res Ptr) {
		var gerr Ptr
		f := WebKitUserContentFilterStoreSaveFinish(src, res, &gerr)
		if gerr != 0 {
			fmt.Fprintln(os.Stderr, "asuka: the WebView's Block didn't compile:", webKitErrorMessage(gerr))
			ErrorFree(gerr)
		}
		if f != 0 {
			defer WebKitUserContentFilterUnref(f)
		}
		if w.block != gen {
			return // another filter, or the view went
		}
		if f != 0 {
			WebKitUserContentManagerAddFilter(w.ucc, f)
		}
		w.released()
	})
	BytesUnref(bytes)
}

// compiling reports whether Block's filter is being compiled.
func (w *web) compiling() bool { return w.pending != 0 && w.pending == w.block }

// released runs the load held while the filter compiled.
func (w *web) released() {
	w.pending = 0
	if f := w.held; f != nil {
		w.held = nil
		f()
	}
}

// The web module and asuka/js.

// evalBody runs s in the page's world and answers with its value as
// JSON: the last statement's, or what its promise resolves to.
const evalBody = `const v = await (0, eval)(s); return JSON.stringify(v === undefined ? null : v)`

// evalIn runs script in a web view and answers with its value as JSON,
// or what it threw.
func evalIn(view Ptr, script string, reply func([]byte, error)) {
	d := WebGLib.VariantDictNew(0)
	WebGLib.VariantDictInsert(d, "s", VariantNewString(script))
	args := WebGLib.VariantRefSink(WebGLib.VariantDictEnd(d))
	WebGLib.VariantDictUnref(d)
	Async(func(a AsyncCall) {
		WebKitWebViewCallAsyncJavascriptFunction(view, evalBody, -1, args, 0, 0, 0, a.Callback, a.Data)
	}, func(src, res Ptr) {
		var gerr Ptr
		val := WebKitWebViewCallAsyncJavascriptFinish(src, res, &gerr)
		if gerr != 0 {
			msg := webKitErrorMessage(gerr)
			ErrorFree(gerr)
			reply(nil, errors.New(msg))
			return
		}
		out := []byte("null")
		if val != 0 {
			if s := GoStringFree(JSCValueToString(val)); s != "" {
				out = []byte(s)
			}
			ObjectUnref(val)
		}
		reply(out, nil)
	})
	WebGLib.VariantUnref(args)
}

// webModule is the "web" module: "eval" (args: u32 a WebView's node id,
// then the script) answers with the script's value as JSON; "cookies"
// (args: a URL) with the cookies the WebViews have for it, as a Cookie
// header.
func (h *Host) webModule() map[string]method {
	return map[string]method{
		"eval": func(args []byte, reply func([]byte, error)) {
			if len(args) < 4 {
				reply(nil, errors.New("bad arguments"))
				return
			}
			n := h.nodes[ops.NodeID(binary.LittleEndian.Uint32(args))]
			w, ok := n.extWeb()
			if !ok {
				reply(nil, errors.New("not a WebView"))
				return
			}
			if w.view == 0 {
				reply(nil, LoadWebKit())
				return
			}
			evalIn(w.view, string(args[4:]), reply)
		},
		"cookies": func(args []byte, reply func([]byte, error)) {
			u, err := url.Parse(string(args))
			if err != nil || u.Hostname() == "" {
				reply(nil, nil)
				return
			}
			if err := webKit(); err != nil {
				reply(nil, err)
				return
			}
			mgr := WebKitNetworkSessionGetCookieManager(WebKitNetworkSessionGetDefault())
			Async(func(a AsyncCall) {
				WebKitCookieManagerGetCookies(mgr, u.String(), 0, a.Callback, a.Data)
			}, func(src, res Ptr) {
				var gerr Ptr
				list := WebKitCookieManagerGetCookiesFinish(src, res, &gerr)
				if gerr != 0 {
					msg := webKitErrorMessage(gerr)
					ErrorFree(gerr)
					reply(nil, errors.New(msg))
					return
				}
				var parts []string
				for _, c := range WebKitListItems(list) {
					parts = append(parts, GoString(SoupCookieGetName(c))+"="+GoString(SoupCookieGetValue(c)))
					SoupCookieFree(c)
				}
				WebGLib.ListFree(list)
				reply([]byte(strings.Join(parts, "; ")), nil)
			})
		},
	}
}

// extWeb is a WebView node's web view.
func (n *node) extWeb() (*web, bool) {
	if n == nil {
		return nil, false
	}
	w, ok := n.ext.(*web)
	return w, ok
}

// script is a asuka/js run: a web view that's never shown, and who waits
// for it.
type script struct {
	view    Ptr
	handler uint64
	timer   uint32
	reply   func([]byte, error)
}

// scriptsRunning counts the asuka/js runs under way.
var scriptsRunning int

// jsModule is the "js" module: "eval" (args: u32 timeout ms, then the
// script) runs the script in a web view made for it and let go after,
// and answers with its value as JSON, or what it threw (or "timeout").
func (h *Host) jsModule() map[string]method {
	return map[string]method{
		"eval": func(args []byte, reply func([]byte, error)) {
			if len(args) < 4 {
				reply(nil, errors.New("bad arguments"))
				return
			}
			if err := webKit(); err != nil {
				reply(nil, err)
				return
			}
			ms := binary.LittleEndian.Uint32(args)
			src := string(args[4:])
			v := ObjectRefSink(WebKitWebViewNew())
			s := &script{view: v, reply: reply}
			scriptsRunning++
			s.handler = Connect(v, "load-changed", func(ev uintptr) {
				if ev != WebKitLoadFinished || s.reply == nil {
					return
				}
				evalIn(v, src, func(b []byte, err error) { s.end(b, err) })
			})
			s.timer = TimeoutAdd(ms, func() bool {
				s.timer = 0
				s.end(nil, errors.New("timeout"))
				return false
			})
			WebKitWebViewLoadHTML(v, "<!doctype html><html></html>", 0)
		},
	}
}

// end answers a script once, and lets its web view go.
func (s *script) end(b []byte, err error) {
	if s.reply == nil {
		return
	}
	reply := s.reply
	s.reply = nil
	scriptsRunning--
	if s.timer != 0 {
		SourceRemove(s.timer)
		s.timer = 0
	}
	Disconnect(s.view, s.handler)
	WebKitWebViewStopLoading(s.view)
	ObjectUnref(s.view)
	reply(b, err)
}

func webKitErrorMessage(e Ptr) string {
	_, _, msg := WebKitError(e)
	return msg
}
