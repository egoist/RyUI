// Web Eval is an e2e's app (e2e/webeval_test.go): a WebView whose page's
// Content Security Policy blocks eval, and what EvalJSResult answers
// there, each on a line the test reads; and one whose page doesn't, for
// a promise's value, awaited.
package main

import (
	"encoding/json"
	"strings"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// page blocks every script of its own, and eval.
const page = `<!doctype html><meta http-equiv="Content-Security-Policy" content="script-src 'none'"><title>Locked</title><p>Locked`

// open is a page with no policy.
const open = `<!doctype html><title>Open</title><p>Open`

// scripts run in order, the line of each saying what it answered.
var scripts = []struct{ name, src string }{
	{"title", "document.title"},
	{"eval", "eval('1')"},
	{"sum", "let a = 2; a * 3"},
	{"object", "({a: [1, 'x', null]})"},
	{"var", "var kept = 5"},
	{"kept", "kept"},
	{"throw", "throw new Error('nope')"},
	{"syntax", "1 +"},
	{"promise", "Promise.resolve(1)"},
}

func App() ui.Node {
	status := signal.New("status: loading")
	lines := make([]*signal.Signal[string], len(scripts))
	for i, s := range scripts {
		lines[i] = signal.New(s.name + ": …")
	}
	var web ui.Node
	var next func(i int)
	next = func(i int) {
		if i == len(scripts) {
			status.Set("status: done")
			return
		}
		ui.EvalJSResult(web, scripts[i].src, func(v json.RawMessage, err error) {
			lines[i].Set(scripts[i].name + ": " + answer(v, err))
			next(i + 1)
		})
	}
	started := false
	web = ui.WebView(ui.WebViewProps{
		HTML: ui.Val(page),
		OnLoad: func(string) {
			if !started {
				started = true
				next(0)
			}
		},
		Style: ui.Style{Height: ui.Pt(80)},
	})
	opened := signal.New("open: …")
	var openWeb ui.Node
	openWeb = ui.WebView(ui.WebViewProps{
		HTML: ui.Val(open),
		OnLoad: func(string) {
			ui.EvalJSResult(openWeb, "Promise.resolve(document.title + 7)", func(v json.RawMessage, err error) {
				opened.Set("open: " + answer(v, err))
			})
		},
		Style: ui.Style{Height: ui.Pt(40)},
	})
	children := []ui.Node{web, openWeb, ui.Text(opened), ui.Text(status)}
	for _, l := range lines {
		children = append(children, ui.Text(l))
	}
	return ui.Column(ui.Style{Padding: ui.All(16), Gap: 4}, children...)
}

// answer is a value's JSON, or "error" and the kind of error: the page
// refusing eval, what the script threw (nope), a syntax error, or a
// value the page's policy keeps EvalJSResult from returning.
func answer(v json.RawMessage, err error) string {
	if err == nil {
		return string(v)
	}
	m := err.Error()
	switch {
	case strings.Contains(m, "unsafe-eval"): // the page's own EvalError
		return "error eval"
	case strings.Contains(m, "nope"):
		return "error nope"
	case strings.Contains(m, "SyntaxError"):
		return "error syntax"
	case strings.Contains(m, "Content Security Policy"):
		return "error policy"
	}
	return "error"
}

func main() { app.Run(App) }
