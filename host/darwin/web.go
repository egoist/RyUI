//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

// A WebView is a WKWebView. The page posts to Go with
// window.ryui.postMessage(text), as on iOS and Android.
type web struct {
	view    ID
	ucc     ID // its user content controller
	preload string
	url     string
	block   int    // Block's rule lists compiled, to keep only the last
	held    func() // a load waiting for Block's rule list to compile
	pending int    // the rule list compiling (its block), 0 for none
	queued  bool   // a WebState report waits for the end of the turn
}

// The page's state the view reports (WebState), observed as it changes.
var webKeys = []string{"URL", "title", "loading", "estimatedProgress", "canGoBack", "canGoForward"}

const bridgeJS = `window.ryui = { postMessage: function (m) { window.webkit.messageHandlers.ryui.postMessage(String(m)); } };`

var webKitLoaded bool

func (h *Host) makeWeb(n *node) ID {
	if !webKitLoaded {
		webKitLoaded = true
		Dlopen("/System/Library/Frameworks/WebKit.framework/WebKit")
	}
	conf := New("WKWebViewConfiguration")
	defer Release(conf)
	ucc := Send(conf, "userContentController")
	Send(ucc, "addScriptMessageHandler:name:", uintptr(h.target), uintptr(String("ryui")))
	w := &web{ucc: Retain(ucc)}
	w.addScript(bridgeJS)
	v := MsgInitRectID(Send(Class("WKWebView"), "alloc"), Sel("initWithFrame:configuration:"), Rect{}, conf)
	Send(v, "setNavigationDelegate:", uintptr(h.target))
	Send(v, "setUIDelegate:", uintptr(h.target))
	if RespondsTo(v, "setInspectable:") {
		Send(v, "setInspectable:", 1)
	}
	w.view = v
	n.web = w
	for _, k := range webKeys {
		Send(v, "addObserver:forKeyPath:options:context:", uintptr(h.target), uintptr(String(k)), 0, 0)
	}
	return v
}

func (w *web) addScript(js string) {
	s := Send(Send(Class("WKUserScript"), "alloc"), "initWithSource:injectionTime:forMainFrameOnly:", uintptr(String(js)), 0, 0)
	Send(w.ucc, "addUserScript:", uintptr(s))
	Release(s)
}

func (w *web) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropWebViewURL:
		u := v.Str // v is the batch's, reused
		w.url = u
		w.hold(func() { w.load(h, u) })
	case ops.PropWebViewHTML:
		html := v.Str
		w.hold(func() { Send(w.view, "loadHTMLString:baseURL:", uintptr(String(html)), 0) })
	case ops.PropWebViewPreload:
		Send(w.ucc, "removeAllUserScripts")
		w.addScript(bridgeJS)
		if v.Str != "" {
			w.addScript(v.Str)
		}
	case ops.PropWebViewUserAgent:
		Send(w.view, "setCustomUserAgent:", uintptr(String(v.Str)))
	case ops.PropWebViewBlock:
		w.setBlock(h, n, v.Bytes)
	}
}

// hold runs a load now, or once Block's rule list has compiled, so the
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
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "file://") || strings.HasPrefix(s, "about:") || strings.HasPrefix(s, "data:") {
		req := Send(Class("NSURLRequest"), "requestWithURL:", uintptr(URL(s)))
		Send(w.view, "loadRequest:", uintptr(req))
		return
	}
	// An asset: the page, with read access to the assets.
	path := s
	if !filepath.IsAbs(path) {
		path = filepath.Join(h.assets, filepath.FromSlash(s))
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	dir := filepath.Dir(path)
	if a, err := filepath.Abs(h.assets); err == nil {
		if _, err := os.Stat(a); err == nil {
			dir = a
		}
	}
	Send(w.view, "loadFileURL:allowingReadAccessToURL:", uintptr(FileURL(path)), uintptr(FileURL(dir)))
}

func (w *web) eval(js string) {
	Send(w.view, "evaluateJavaScript:completionHandler:", uintptr(String(js)), 0)
}

func (w *web) navigate(to byte) {
	switch to {
	case 0:
		Send(w.view, "goBack")
	case 1:
		Send(w.view, "goForward")
	case 2:
		Send(w.view, "reload")
	case 3:
		Send(w.view, "stopLoading")
	}
}

func (w *web) close() {
	for _, k := range webKeys {
		Send(w.view, "removeObserver:forKeyPath:", uintptr(theHost.target), uintptr(String(k)))
	}
	w.block++ // a rule list still compiling is dropped
	Send(w.ucc, "removeScriptMessageHandlerForName:", uintptr(String("ryui")))
	Send(w.view, "setNavigationDelegate:", 0)
	Send(w.view, "setUIDelegate:", 0)
	Release(w.ucc)
}

// queueWebState reports the page's state once for the changes of a turn
// of the main loop.
func (h *Host) queueWebState(v ID) {
	n := h.byView[v]
	if n == nil || n.web == nil || n.web.queued || !n.listening(ops.EventWebState) {
		return
	}
	n.web.queued = true
	h.Post(func() {
		if n.web != nil && h.byView[v] == n {
			n.web.queued = false
			h.webState(v)
		}
	})
}

func (h *Host) webState(v ID) {
	n := h.byView[v]
	if n == nil {
		return
	}
	var b []byte
	str := func(s string) {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	str(GoString(Send(Send(v, "URL"), "absoluteString")))
	str(GoString(Send(v, "title")))
	var flags byte
	if SendBool(v, "isLoading") {
		flags |= 1
	}
	if SendBool(v, "canGoBack") {
		flags |= 2
	}
	if SendBool(v, "canGoForward") {
		flags |= 4
	}
	b = append(b, flags)
	progress := 1.0
	if flags&1 != 0 {
		progress = MsgFloat(v, Sel("estimatedProgress"))
	}
	b = binary.LittleEndian.AppendUint32(b, math.Float32bits(float32(progress)))
	h.emit(n, ops.Event{Kind: ops.EventWebState, Bytes: b})
}

// webMethods are RyUITarget's WebKit delegate methods.
func webMethods() []objc.MethodDef {
	return []objc.MethodDef{
		Method("userContentController:didReceiveScriptMessage:", func(self ID, _ SEL, ucc, msg ID) {
			v := Send(msg, "webView")
			if n := theHost.byView[v]; n != nil {
				theHost.emit(n, ops.Event{Kind: ops.EventMessage, Str: GoString(Send(Send(msg, "body"), "description"))})
			}
		}),
		Method("webView:didFinishNavigation:", func(self ID, _ SEL, v, nav ID) {
			if theHost.scriptLoaded(v) {
				return
			}
			if n := theHost.byView[v]; n != nil {
				theHost.emit(n, ops.Event{Kind: ops.EventNavigated, Str: GoString(Send(Send(v, "URL"), "absoluteString"))})
			}
			theHost.webState(v)
		}),
		Method("webView:didStartProvisionalNavigation:", func(self ID, _ SEL, v, nav ID) { theHost.webState(v) }),
		Method("webView:didFailNavigation:withError:", func(self ID, _ SEL, v, nav, err ID) { theHost.webFailed(v, err) }),
		Method("webView:didFailProvisionalNavigation:withError:", func(self ID, _ SEL, v, nav, err ID) { theHost.webFailed(v, err) }),
		// A response the page can't show, or an attachment: a download.
		Method("webView:decidePolicyForNavigationResponse:decisionHandler:", func(self ID, _ SEL, v, resp, handler ID) {
			theHost.decideResponse(v, resp, handler)
		}),
		Method("webView:createWebViewWithConfiguration:forNavigationAction:windowFeatures:", func(self ID, _ SEL, v, conf, action, features ID) ID {
			// A page for a new window: the app's NewWindow, or this view.
			req := Send(action, "request")
			u := GoString(Send(Send(req, "URL"), "absoluteString"))
			if n := theHost.byView[v]; n != nil && n.listening(ops.EventNewWindow) {
				theHost.emit(n, ops.Event{Kind: ops.EventNewWindow, Str: u})
			} else {
				Send(v, "loadRequest:", uintptr(req))
			}
			return 0
		}),
	}
}

func (h *Host) webFailed(v, err ID) {
	if n := h.byView[v]; n != nil {
		h.emit(n, ops.Event{Kind: ops.EventFailed, Str: GoString(Send(err, "localizedDescription"))})
	}
	h.webState(v)
}
