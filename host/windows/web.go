//go:build windows && amd64

package windows

import (
	"encoding/binary"
	"fmt"
	"html"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

// A WebView is WebView2 (Edge's runtime) in a view of the host's: a
// windowed controller in the view's window. The page posts to Go with
// window.nui.postMessage(text), as on iOS and Android; the app's assets
// are served from https://nui.assets/. The environment is made once, for
// the app, the first time a web view shows; until it and the view's
// controller exist, what the core asks for waits.
//
// A Video is a WebView showing the video in a <video> element, with
// Edge's media controls: Win32 has no video view with controls.
type web struct {
	n          *node
	controller uintptr
	view       uintptr // ICoreWebView2
	settings   uintptr
	pending    []func() // what waits for the controller
	scripts    []string // the preload's script IDs
	url, html  string
	preload    string
	block      []string
	requests   bool
	filtered   bool
	loading    bool
	closed     bool
	video      *video
}

type video struct {
	source         string
	mode           ops.ImageMode
	hideControls   bool
	autoPlay, loop bool
	muted          bool
}

const (
	webAssetsHost = "nui.assets"
	bridgeJS      = `window.nui = { postMessage: function (m) { window.chrome.webview.postMessage(String(m)); } };`
)

// The vtable indices of the WebView2 interfaces used, from WebView2.h.
const (
	envCreateController          = 3
	envCreateWebResourceResponse = 4

	ctlPutIsVisible     = 4
	ctlPutBounds        = 6
	ctlClose            = 24
	ctlGetCoreWebView2  = 25
	ctl2PutDefaultColor = 27

	wvGetSettings                            = 3
	wvGetSource                              = 4
	wvNavigate                               = 5
	wvNavigateToString                       = 6
	wvAddNavigationStarting                  = 7
	wvAddSourceChanged                       = 11
	wvAddHistoryChanged                      = 13
	wvAddNavigationCompleted                 = 15
	wvAddScriptToExecuteOnDocumentCreated    = 27
	wvRemoveScriptToExecuteOnDocumentCreated = 28
	wvExecuteScript                          = 29
	wvReload                                 = 31
	wvAddWebMessageReceived                  = 34
	wvGetCanGoBack                           = 38
	wvGetCanGoForward                        = 39
	wvGoBack                                 = 40
	wvGoForward                              = 41
	wvStop                                   = 43
	wvAddNewWindowRequested                  = 44
	wvAddDocumentTitleChanged                = 46
	wvGetDocumentTitle                       = 48
	wvAddWebResourceRequested                = 55
	wvAddWebResourceRequestedFilter          = 57
	wv3SetVirtualHostMapping                 = 71
	wv4AddDownloadStarting                   = 75

	setPutAreDevToolsEnabled = 12
	set2PutUserAgent         = 22

	navStartingGetURI        = 3
	navCompletedGetIsSuccess = 3
	navCompletedGetStatus    = 4
	msgTryGetString          = 5
	newWinGetURI             = 3
	newWinPutHandled         = 6
	resReqGetRequest         = 3
	resReqPutResponse        = 5
	reqGetURI                = 3
	dlStartGetOperation      = 3
	dlStartPutCancel         = 5
	dlOpGetURI               = 9
	dlOpGetContentDisp       = 10
	dlOpGetMimeType          = 11
	dlOpGetTotalBytes        = 12
)

var (
	iidController2 = NewGUID("c979903e-d4ca-4228-92eb-47ee3fa96eab")
	iidSettings2   = NewGUID("ee9a0f68-f46c-4e32-ac23-ef8cac224d2a")
	iidWebView2_3  = NewGUID("a0d6df20-3b92-416d-aa0c-437a9c727857")
	iidWebView2_4  = NewGUID("20d02d59-6df2-42dc-bd06-f98a694b1302")
)

// The app's WebView2 environment.
var (
	webEnv      uintptr
	webEnvErr   error
	webEnvWait  []func()
	webEnvStart bool
)

func (h *Host) whenWebEnvironment(f func()) {
	if webEnv != 0 || webEnvErr != nil {
		f()
		return
	}
	webEnvWait = append(webEnvWait, f)
	if webEnvStart {
		return
	}
	webEnvStart = true
	// Videos play without a click, as AutoPlay asks.
	os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", "--autoplay-policy=no-user-gesture-required")
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), h.appName, "WebView2")
	os.MkdirAll(dir, 0o755)
	err := CreateWebView2Environment(dir, func(env uintptr, err error) {
		webEnv, webEnvErr = env, err
		if err != nil {
			fmt.Fprintln(os.Stderr, "nui:", err)
		}
		fs := webEnvWait
		webEnvWait = nil
		for _, f := range fs {
			f()
		}
	})
	if err != nil {
		webEnvErr = err
		fmt.Fprintln(os.Stderr, "nui:", err)
		fs := webEnvWait
		webEnvWait = nil
		for _, f := range fs {
			f()
		}
	}
}

func (h *Host) makeWeb(n *node) {
	n.hwnd = h.makeView(0)
	w := &web{n: n}
	n.web = w
	if n.kind == ops.KindVideo {
		w.video = &video{}
	}
	h.whenWebEnvironment(func() {
		if webEnv == 0 || w.closed || n.hwnd == 0 {
			return
		}
		WithHandler(func(hr, controller uintptr) {
			if Failed(hr) || controller == 0 || w.closed {
				return
			}
			AddRef(controller)
			w.setUp(h, controller)
		}, func(handler uintptr) uintptr {
			return ComCall(webEnv, envCreateController, n.hwnd, handler)
		})
	})
}

// do runs f once the view's controller exists.
func (w *web) do(f func()) {
	if w.view != 0 {
		f()
		return
	}
	w.pending = append(w.pending, f)
}

func (w *web) setUp(h *Host, controller uintptr) {
	w.controller = controller
	ComCall(controller, ctlGetCoreWebView2, uintptr(unsafe.Pointer(&w.view)))
	ComCall(w.view, wvGetSettings, uintptr(unsafe.Pointer(&w.settings)))
	ComCall(w.settings, setPutAreDevToolsEnabled, 1)
	if c2 := QueryInterface(controller, &iidController2); c2 != 0 {
		// Transparent until the page draws: the view's background shows.
		color := uint32(0)
		ComCall(c2, ctl2PutDefaultColor, uintptr(color))
		Release(c2)
	}
	if wv3 := QueryInterface(w.view, &iidWebView2_3); wv3 != 0 {
		ComCall(wv3, wv3SetVirtualHostMapping, Str(webAssetsHost), Str(absAssets(h)), 1) // ALLOW
		Release(wv3)
	}
	var tok int64
	add := func(index int, fn func(a, b uintptr)) {
		WithHandler(fn, func(handler uintptr) uintptr {
			return ComCall(w.view, index, handler, uintptr(unsafe.Pointer(&tok)))
		})
	}
	n := w.n
	add(wvAddNavigationStarting, func(_, _ uintptr) {
		w.loading = true
		h.webState(w)
	})
	add(wvAddNavigationCompleted, func(_, args uintptr) {
		w.loading = false
		var ok int32
		ComCall(args, navCompletedGetIsSuccess, uintptr(unsafe.Pointer(&ok)))
		if ok != 0 {
			h.emit(n, ops.Event{Kind: ops.EventNavigated, Str: w.source()})
		} else if w.video == nil {
			var status int32
			ComCall(args, navCompletedGetStatus, uintptr(unsafe.Pointer(&status)))
			h.emit(n, ops.Event{Kind: ops.EventFailed, Str: "the page couldn't load (WebView2 error " + strconv.Itoa(int(status)) + ")"})
		}
		h.webState(w)
	})
	add(wvAddSourceChanged, func(_, _ uintptr) { h.webState(w) })
	add(wvAddHistoryChanged, func(_, _ uintptr) { h.webState(w) })
	add(wvAddDocumentTitleChanged, func(_, _ uintptr) { h.webState(w) })
	add(wvAddWebMessageReceived, func(_, args uintptr) {
		var p uintptr
		if Failed(ComCall(args, msgTryGetString, uintptr(unsafe.Pointer(&p)))) {
			return
		}
		msg := TakeString(p)
		if w.video != nil {
			h.videoMessage(w, msg)
			return
		}
		h.emit(n, ops.Event{Kind: ops.EventMessage, Str: msg})
	})
	add(wvAddNewWindowRequested, func(_, args uintptr) {
		var p uintptr
		ComCall(args, newWinGetURI, uintptr(unsafe.Pointer(&p)))
		u := TakeString(p)
		ComCall(args, newWinPutHandled, 1)
		if n.listening(ops.EventNewWindow) {
			h.emit(n, ops.Event{Kind: ops.EventNewWindow, Str: u})
			return
		}
		ComCall(w.view, wvNavigate, Str(u))
	})
	add(wvAddWebResourceRequested, func(_, args uintptr) { h.webRequest(w, args) })
	if wv4 := QueryInterface(w.view, &iidWebView2_4); wv4 != 0 {
		WithHandler(func(_, args uintptr) { h.download(w, args) }, func(handler uintptr) uintptr {
			return ComCall(wv4, wv4AddDownloadStarting, handler, uintptr(unsafe.Pointer(&tok)))
		})
		Release(wv4)
	}
	w.addScript(bridgeJS)
	w.colorScheme(h.dark)
	w.resized(h, n)
	ComCall(controller, ctlPutIsVisible, 1)
	fs := w.pending
	w.pending = nil
	for _, f := range fs {
		f()
	}
	if w.video != nil {
		w.loadVideo(h)
	}
}

func (w *web) source() string {
	var p uintptr
	ComCall(w.view, wvGetSource, uintptr(unsafe.Pointer(&p)))
	return TakeString(p)
}

func absAssets(h *Host) string {
	if a, err := filepath.Abs(h.assets); err == nil {
		return a
	}
	return h.assets
}

func (w *web) addScript(js string) {
	WithHandler(func(hr, id uintptr) {
		if !Failed(hr) && id != 0 {
			w.scripts = append(w.scripts, GoStr(id))
		}
	}, func(handler uintptr) uintptr {
		return ComCall(w.view, wvAddScriptToExecuteOnDocumentCreated, Str(js), handler)
	})
}

func (w *web) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	if w.video != nil {
		h.setVideoProp(w, p, v)
		return
	}
	switch p {
	case ops.PropWebViewURL:
		w.url = v.Str
		w.do(func() { w.load(h, w.url) })
	case ops.PropWebViewHTML:
		w.html = v.Str
		w.do(func() { ComCall(w.view, wvNavigateToString, Str(w.html)) })
	case ops.PropWebViewPreload:
		w.preload = v.Str
		w.do(func() {
			for _, id := range w.scripts {
				ComCall(w.view, wvRemoveScriptToExecuteOnDocumentCreated, Str(id))
			}
			w.scripts = nil
			w.addScript(bridgeJS)
			if w.preload != "" {
				w.addScript(w.preload)
			}
		})
	case ops.PropWebViewUserAgent:
		ua := v.Str
		w.do(func() {
			if s2 := QueryInterface(w.settings, &iidSettings2); s2 != 0 {
				ComCall(s2, set2PutUserAgent, Str(ua))
				Release(s2)
			}
		})
	case ops.PropWebViewBlock:
		w.block = decodeStrings(v.Bytes)
		w.do(w.filter)
	case ops.PropWebViewRequests:
		w.requests = v.Int != 0
		w.do(w.filter)
	}
}

// filter has WebView2 tell the host of every request (WebResourceRequested)
// once something wants to see them: Block, Requests.
func (w *web) filter() {
	if w.filtered || len(w.block) == 0 && !w.requests {
		return
	}
	w.filtered = true
	ComCall(w.view, wvAddWebResourceRequestedFilter, Str("*"), 0) // ALL
}

func (h *Host) webRequest(w *web, args uintptr) {
	var req, p uintptr
	ComCall(args, resReqGetRequest, uintptr(unsafe.Pointer(&req)))
	if req == 0 {
		return
	}
	ComCall(req, reqGetURI, uintptr(unsafe.Pointer(&p)))
	Release(req)
	u := TakeString(p)
	if w.requests {
		h.emit(w.n, ops.Event{Kind: ops.EventRequest, Str: u})
	}
	if blocked(u, w.block) {
		var resp uintptr
		ComCall(webEnv, envCreateWebResourceResponse, 0, 403, Str("Blocked"), Str(""), uintptr(unsafe.Pointer(&resp)))
		if resp != 0 {
			ComCall(args, resReqPutResponse, resp)
			Release(resp)
		}
	}
}

// blocked matches a URL against Block's patterns: a host (its
// subdomains too), or with a /, a part of the URL.
func blocked(u string, patterns []string) bool {
	host := u
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	if i := strings.IndexAny(host, "/?#"); i >= 0 {
		host = host[:i]
	}
	if i := strings.LastIndex(host, "@"); i >= 0 {
		host = host[i+1:]
	}
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	for _, p := range patterns {
		if strings.Contains(p, "/") {
			if strings.Contains(u, p) {
				return true
			}
		} else if host == p || strings.HasSuffix(host, "."+p) {
			return true
		}
	}
	return false
}

// download reports a file the page can't show, with Download, instead of
// saving it, when the core listens.
func (h *Host) download(w *web, args uintptr) {
	n := w.n
	if !n.listening(ops.EventDownload) {
		return
	}
	var op, p uintptr
	ComCall(args, dlStartGetOperation, uintptr(unsafe.Pointer(&op)))
	ComCall(args, dlStartPutCancel, 1)
	if op == 0 {
		return
	}
	defer Release(op)
	ComCall(op, dlOpGetURI, uintptr(unsafe.Pointer(&p)))
	u := TakeString(p)
	ComCall(op, dlOpGetMimeType, uintptr(unsafe.Pointer(&p)))
	mime := TakeString(p)
	ComCall(op, dlOpGetContentDisp, uintptr(unsafe.Pointer(&p)))
	name := fileName(TakeString(p), u)
	var size int64 = -1
	ComCall(op, dlOpGetTotalBytes, uintptr(unsafe.Pointer(&size)))
	var b []byte
	str := func(s string) {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	str(u)
	str(mime)
	str(name)
	b = binary.LittleEndian.AppendUint64(b, uint64(size))
	h.emit(n, ops.Event{Kind: ops.EventDownload, Bytes: b})
}

// fileName is a download's name: its Content-Disposition's, or the URL's
// last part.
func fileName(disposition, u string) string {
	if i := strings.Index(disposition, "filename="); i >= 0 {
		s := strings.Trim(disposition[i+len("filename="):], `"; `)
		if j := strings.IndexByte(s, ';'); j >= 0 {
			s = strings.Trim(s[:j], `" `)
		}
		if s != "" {
			return s
		}
	}
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	return u[strings.LastIndexByte(u, '/')+1:]
}

func (w *web) load(h *Host, s string) {
	if s == "" {
		return
	}
	if !strings.Contains(s, ":") {
		// An asset: from the assets' virtual host, so it reaches the others.
		s = "https://" + webAssetsHost + "/" + strings.TrimPrefix(filepath.ToSlash(s), "/")
	}
	ComCall(w.view, wvNavigate, Str(s))
}

func (w *web) eval(js string) {
	w.do(func() { ComCall(w.view, wvExecuteScript, Str(js), 0) })
}

func (w *web) navigate(to byte) {
	w.do(func() {
		switch to {
		case 0:
			ComCall(w.view, wvGoBack)
		case 1:
			ComCall(w.view, wvGoForward)
		case 2:
			ComCall(w.view, wvReload)
		case 3:
			ComCall(w.view, wvStop)
		}
	})
}

// resized gives the controller the view's size.
func (w *web) resized(h *Host, n *node) {
	if w.controller == 0 || n.hwnd == 0 {
		return
	}
	r := ClientRect(n.hwnd)
	ComCall(w.controller, ctlPutBounds, uintptr(unsafe.Pointer(&r))) // a RECT goes by reference on x64
}

func (w *web) close() {
	w.closed = true
	if w.controller != 0 {
		ComCall(w.controller, ctlClose)
		Release(w.settings)
		Release(w.view)
		Release(w.controller)
		w.controller, w.view, w.settings = 0, 0, 0
	}
}

func (h *Host) webState(w *web) {
	n := w.n
	if w.video != nil || !n.listening(ops.EventWebState) {
		return
	}
	var b []byte
	str := func(s string) {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	str(w.source())
	var p uintptr
	ComCall(w.view, wvGetDocumentTitle, uintptr(unsafe.Pointer(&p)))
	str(TakeString(p))
	var back, fwd int32
	ComCall(w.view, wvGetCanGoBack, uintptr(unsafe.Pointer(&back)))
	ComCall(w.view, wvGetCanGoForward, uintptr(unsafe.Pointer(&fwd)))
	var flags byte
	progress := float32(1)
	if w.loading {
		flags |= 1
		progress = 0.1 // WebView2 doesn't tell how far a load is
	}
	if back != 0 {
		flags |= 2
	}
	if fwd != 0 {
		flags |= 4
	}
	b = append(b, flags)
	b = binary.LittleEndian.AppendUint32(b, math.Float32bits(progress))
	h.emit(n, ops.Event{Kind: ops.EventWebState, Bytes: b})
}

// Video.

func (h *Host) setVideoProp(w *web, p ops.PropID, v *ops.Value) {
	vd := w.video
	switch p {
	case ops.PropVideoSource:
		vd.source = v.Str
		w.do(func() { w.loadVideo(h) })
		return
	case ops.PropVideoMode:
		vd.mode = ops.ImageMode(v.Int)
	case ops.PropVideoHideControls:
		vd.hideControls = v.Int != 0
	case ops.PropVideoAutoPlay:
		vd.autoPlay = v.Int != 0
	case ops.PropVideoLoop:
		vd.loop = v.Int != 0
	case ops.PropVideoMuted:
		vd.muted = v.Int != 0
	}
	w.eval(w.videoAttrs())
}

// videoAttrs is the script that sets the <video>'s attributes from the
// props.
func (w *web) videoAttrs() string {
	vd := w.video
	fit := "contain"
	switch vd.mode {
	case ops.ImageModeFill:
		fit = "cover"
	case ops.ImageModeStretch:
		fit = "fill"
	case ops.ImageModeCenter:
		fit = "none"
	}
	return fmt.Sprintf(`(function(){var v=document.querySelector('video');if(!v)return;v.style.objectFit=%q;v.controls=%v;v.loop=%v;v.muted=%v;v.autoplay=%v;})()`,
		fit, !vd.hideControls, vd.loop, vd.muted, vd.autoPlay)
}

// loadVideo shows the page of the video: a <video> filling it, which
// posts its natural size once known, its end and its errors.
func (w *web) loadVideo(h *Host) {
	vd := w.video
	if vd == nil || vd.source == "" || w.view == 0 {
		return
	}
	src := vd.source
	if !strings.Contains(src, ":") {
		src = "https://" + webAssetsHost + "/" + strings.TrimPrefix(filepath.ToSlash(src), "/")
	}
	var attrs strings.Builder
	if !vd.hideControls {
		attrs.WriteString(" controls")
	}
	if vd.loop {
		attrs.WriteString(" loop")
	}
	if vd.muted {
		attrs.WriteString(" muted")
	}
	if vd.autoPlay {
		attrs.WriteString(" autoplay")
	}
	page := `<!doctype html><html><head><meta charset="utf-8"><style>html,body{margin:0;height:100%;background:#000;overflow:hidden}video{width:100%;height:100%;display:block}</style></head><body>` +
		`<video playsinline` + attrs.String() + ` src="` + html.EscapeString(src) + `"></video><script>var v=document.querySelector('video');` +
		`v.addEventListener('loadedmetadata',function(){chrome.webview.postMessage('loaded:'+v.videoWidth+','+v.videoHeight)});` +
		`v.addEventListener('ended',function(){chrome.webview.postMessage('ended')});` +
		`v.addEventListener('error',function(){chrome.webview.postMessage('failed:'+(v.error&&v.error.message||'the video could not play'))});` +
		`</script></body></html>`
	ComCall(w.view, wvNavigateToString, Str(page))
	w.eval(w.videoAttrs())
}

func (h *Host) videoMessage(w *web, msg string) {
	n := w.n
	switch {
	case strings.HasPrefix(msg, "loaded:"):
		ws, hs, _ := strings.Cut(msg[len("loaded:"):], ",")
		vw, _ := strconv.ParseFloat(ws, 64)
		vh, _ := strconv.ParseFloat(hs, 64)
		h.emit(n, ops.Event{Kind: ops.EventLoaded, X: vw, Y: vh})
	case msg == "ended":
		h.emit(n, ops.Event{Kind: ops.EventEnded})
	case strings.HasPrefix(msg, "failed:"):
		h.emit(n, ops.Event{Kind: ops.EventFailed, Str: msg[len("failed:"):]})
	}
}

// play, pause and seek drive a Video's <video>.
func (w *web) play()  { w.eval(`document.querySelector('video').play()`) }
func (w *web) pause() { w.eval(`document.querySelector('video').pause()`) }
func (w *web) seek(t float64) {
	w.eval(fmt.Sprintf(`document.querySelector('video').currentTime=%g`, t))
}

var iidWebView2_13 = NewGUID("f75f09a8-667e-4983-88d6-c8773f315e84")

// colorScheme has the page's prefers-color-scheme follow the app's dark
// mode (the system's, unless the app sets its own).
func (w *web) colorScheme(dark bool) {
	if w.view == 0 {
		return
	}
	wv13 := QueryInterface(w.view, &iidWebView2_13)
	if wv13 == 0 {
		return
	}
	defer Release(wv13)
	var profile uintptr
	ComCall(wv13, 105, uintptr(unsafe.Pointer(&profile))) // get_Profile
	if profile == 0 {
		return
	}
	scheme := uintptr(1) // LIGHT
	if dark {
		scheme = 2 // DARK
	}
	ComCall(profile, 9, scheme) // put_PreferredColorScheme
	Release(profile)
}
