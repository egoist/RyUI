package ui

import (
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
