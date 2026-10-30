package ui

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"

	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
)

// WebViewProps configures a WebView.
type WebViewProps struct {
	// URL is the page: an http(s) URL, or a file in the app's assets
	// (its relative links are the assets' too).
	URL signal.Accessor[string]
	// HTML is markup to show instead.
	HTML  signal.Accessor[string]
	Style Styler
	// OnLoad is called when a page has loaded (the first, and each one
	// a link leads to), with its URL.
	OnLoad func(url string)
	// OnError is called when a page couldn't load.
	OnError func(message string)
	// OnMessage receives what the page posts: in its script,
	// window.nui.postMessage(text).
	OnMessage func(message string)

	// Preload runs before each page's own scripts, in every frame (an
	// iframe's too): to hook fetch, set globals. Set when the view is
	// made.
	Preload string
	// UserAgent is what pages are fetched with, and navigator.userAgent
	// (empty: the platform's).
	UserAgent string
	// Block keeps requests from loading (an ad blocker's lists): each a
	// host, which matches its subdomains too ("doubleclick.net"), or,
	// with a /, a part of the URL ("/ads/"). Set when the view is made.
	Block []string

	// OnState follows the page: its URL, title, progress, and whether it
	// can go back and forward (an address bar, a progress line).
	OnState func(WebState)
	// OnRequest gets the URL of each request the page makes (scripts,
	// images, media, fetch). Android only: iOS can't see them.
	OnRequest func(url string)
	// OnDownload gets a response the page can't show, a file to save
	// (a link to a .zip, a Content-Disposition: attachment); it doesn't
	// download.
	OnDownload func(WebDownload)
	// OnNewWindow gets a page the page would open in a new window
	// (window.open, a target=_blank link). Without it, the page loads in
	// this view.
	OnNewWindow func(url string)
}

// WebState is a WebView's page, as OnState sees it.
type WebState struct {
	URL, Title              string
	Loading                 bool
	Progress                float32 // 0 to 1
	CanGoBack, CanGoForward bool
}

// WebDownload is a file a WebView's page leads to.
type WebDownload struct {
	URL, MIME, Filename string
	Size                int64 // -1: unknown
}

// WebView shows a web page: the platform's web view (WKWebView, Android's
// WebView), with its scrolling, zooming and text selection. With no Height
// and no Grow, it grows, as a List does. Talk to the page with OnMessage
// and EvalJS.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func WebView(p WebViewProps) Node {
	e := newElement(ops.KindWebView, true)
	e.setStyle(p.Style, false)
	growByDefault(e)
	h := e.handlers()
	if p.OnLoad != nil {
		h.navigated = p.OnLoad
		e.listen(ops.EventNavigated)
	}
	if p.OnError != nil {
		h.failed = p.OnError
		e.listen(ops.EventFailed)
	}
	if p.OnMessage != nil {
		h.message = p.OnMessage
		e.listen(ops.EventMessage)
	}
	b := e.r.batch
	// Before the page loads: they apply to it.
	if p.Preload != "" {
		b.SetString(e.id, ops.PropWebViewPreload, p.Preload)
	}
	if p.UserAgent != "" {
		b.SetString(e.id, ops.PropWebViewUserAgent, p.UserAgent)
	}
	if len(p.Block) > 0 {
		b.SetBytes(e.id, ops.PropWebViewBlock, encodeStrings(p.Block))
	}
	if p.OnRequest != nil {
		b.SetBool(e.id, ops.PropWebViewRequests, true)
	}
	if p.OnState != nil || p.OnRequest != nil || p.OnDownload != nil || p.OnNewWindow != nil {
		h.web = &webHandlers{state: p.OnState, request: p.OnRequest, download: p.OnDownload, newWindow: p.OnNewWindow}
		for _, l := range []struct {
			on   bool
			kind ops.EventKind
		}{{p.OnState != nil, ops.EventWebState}, {p.OnRequest != nil, ops.EventRequest}, {p.OnDownload != nil, ops.EventDownload}, {p.OnNewWindow != nil, ops.EventNewWindow}} {
			if l.on {
				e.listen(l.kind)
			}
		}
	}
	bind(p.HTML, func(v string) { e.r.batch.SetString(e.id, ops.PropWebViewHTML, v) })
	bind(p.URL, func(v string) { e.r.batch.SetString(e.id, ops.PropWebViewURL, assetURL(v)) })
	return Node{e}
}

// EvalJS runs script in a WebView's page (its result isn't returned:
// have the script post one, window.nui.postMessage). Main thread.
func EvalJS(web Node, script string) {
	if e := web.e; e != nil && e.kind == ops.KindWebView && !e.dead {
		e.r.batch.Command(e.id, ops.CmdEval, []byte(script))
	}
}

// encodeStrings is u32 count, then each a u32-length string.
func encodeStrings(ss []string) []byte {
	le := binary.LittleEndian
	b := le.AppendUint32(nil, uint32(len(ss)))
	for _, s := range ss {
		b = le.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	return b
}

// webEvent decodes a WebView's own events for its handlers.
func webEvent(h *webHandlers, ev ops.Event) {
	switch ev.Kind {
	case ops.EventRequest:
		if h.request != nil {
			h.request(ev.Str)
		}
	case ops.EventNewWindow:
		if h.newWindow != nil {
			h.newWindow(ev.Str)
		}
	case ops.EventWebState:
		r := wireReader{b: ev.Bytes, ok: true}
		s := WebState{URL: r.str(), Title: r.str()}
		f := r.u8()
		s.Loading, s.CanGoBack, s.CanGoForward = f&1 != 0, f&2 != 0, f&4 != 0
		s.Progress = r.f32()
		if h.state != nil && r.ok {
			h.state(s)
		}
	case ops.EventDownload:
		r := wireReader{b: ev.Bytes, ok: true}
		d := WebDownload{URL: r.str(), MIME: r.str(), Filename: r.str(), Size: int64(r.u64())}
		if h.download != nil && r.ok {
			h.download(d)
		}
	}
}

// wireReader reads an event's little-endian bytes; ok is false once
// they've run out.
type wireReader struct {
	b  []byte
	ok bool
}

func (r *wireReader) take(n int) []byte {
	if len(r.b) < n {
		r.b, r.ok = nil, false
		return make([]byte, n)
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *wireReader) u8() byte    { return r.take(1)[0] }
func (r *wireReader) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }
func (r *wireReader) u64() uint64 { return binary.LittleEndian.Uint64(r.take(8)) }
func (r *wireReader) f32() float32 {
	return math.Float32frombits(r.u32())
}
func (r *wireReader) str() string {
	n := int(r.u32())
	if n > len(r.b) {
		r.b, r.ok = nil, false
		return ""
	}
	return string(r.take(n))
}

// WebBack, WebForward, WebReload and WebStop move a WebView's page through
// its history, load it again, or stop it loading. Main thread.
func WebBack(web Node)    { webNavigate(web, 0) }
func WebForward(web Node) { webNavigate(web, 1) }
func WebReload(web Node)  { webNavigate(web, 2) }
func WebStop(web Node)    { webNavigate(web, 3) }

func webNavigate(web Node, to byte) {
	if e := web.e; e != nil && e.kind == ops.KindWebView && !e.dead {
		e.r.batch.Command(e.id, ops.CmdWebNavigate, []byte{to})
	}
}

// EvalJSResult runs script in a WebView's page, as EvalJS, and calls done
// on the main thread with its value as JSON: the last statement's, or
// what the promise it returns resolves to (null for undefined); or what
// it threw. Main thread.
func EvalJSResult(web Node, script string, done func(result json.RawMessage, err error)) {
	e := web.e
	if e == nil || e.kind != ops.KindWebView || e.dead {
		done(nil, errors.New("ui: EvalJSResult: not a WebView"))
		return
	}
	args := binary.LittleEndian.AppendUint32(nil, uint32(e.id))
	Call("web", "eval", append(args, script...), func(b []byte, err error) {
		if err == nil && !json.Valid(b) {
			err = errors.New("ui: EvalJSResult: the result isn't JSON")
		}
		done(json.RawMessage(b), err)
	})
}

// WebCookies calls done on the main thread with the cookies the
// WebViews' store has for url, as a Cookie header ("a=1; b=2"): what
// the user signed in to in a WebView, for the app's own requests. Main
// thread.
func WebCookies(url string, done func(header string, err error)) {
	Call("web", "cookies", []byte(url), func(b []byte, err error) { done(string(b), err) })
}
