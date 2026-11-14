//go:build linux && !android

package linux

import (
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
)

// waitFor lets the main loop run until cond holds (checked on the main
// thread); a web process starts slower than settle waits, so this waits
// longer (a safety net, not what passes).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 6000; i++ {
		var ok bool
		onMain(func() { ok = cond() })
		if ok {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s: never", what)
}

func needWebKit(t *testing.T) {
	t.Helper()
	var err error
	onMain(func() { err = LoadWebKit() })
	if err != nil {
		t.Skip(err)
	}
}

// A WebView loads a page from the network, blocks what Block names from
// its first page on, reports its requests, its state as it changes, a
// file it can't show as a Download, and what the page posts; it runs
// script for its value; its prefers-color-scheme follows the app's.
func TestWebView(t *testing.T) {
	needWebKit(t)
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<!doctype html><title>Home</title><script src="/ads/a.js"></script><script src="/ok.js"></script><a id="f" href="/file.zip">file</a>`))
		case "/file.zip":
			w.Header().Set("Content-Type", "application/zip")
			w.Write([]byte("PK"))
		default:
			w.Header().Set("Content-Type", "text/javascript")
			w.Write([]byte("window.loaded = (window.loaded || 0) + 1"))
		}
	}))
	defer srv.Close()
	var loads, messages, requests []string
	var downloads []ui.WebDownload
	var states []ui.WebState
	var page ui.Node
	h, _ := mount(t, func() ui.Node {
		page = ui.WebView(ui.WebViewProps{
			URL:        ui.Val(srv.URL + "/"),
			Block:      []string{"/ads/"},
			OnLoad:     func(u string) { loads = append(loads, u) },
			OnMessage:  func(m string) { messages = append(messages, m) },
			OnRequest:  func(u string) { requests = append(requests, u) },
			OnDownload: func(d ui.WebDownload) { downloads = append(downloads, d) },
			OnState:    func(s ui.WebState) { states = append(states, s) },
			Style:      ui.Style{Width: ui.Pt(300), Height: ui.Pt(200)},
		})
		return page
	})
	waitFor(t, "the page loads", func() bool { return len(loads) > 0 })
	onMain(func() {
		n := find(h, ops.KindWebView)[0]
		if w, _ := n.extWeb(); w == nil || n.inner != w.view || !IsWebView(n.inner) {
			fatalf(t, "the node's control isn't its web view")
		}
		mu.Lock()
		got := strings.Join(paths, " ")
		mu.Unlock()
		if strings.Contains(got, "/ads/") || !strings.Contains(got, "/ok.js") {
			fatalf(t, "requests %s: /ads/ should be blocked, /ok.js not", got)
		}
		if loads[0] != srv.URL+"/" {
			fatalf(t, "loaded %q", loads[0])
		}
		if r := strings.Join(requests, " "); !strings.Contains(r, "/ok.js") {
			fatalf(t, "Request: %s", r)
		}
	})
	waitFor(t, "the state says Home, loaded", func() bool {
		return len(states) > 0 && states[len(states)-1].Title == "Home" && !states[len(states)-1].Loading
	})

	// Script's value, as JSON: only ok.js ran.
	eval := func(script string) (string, error) {
		var r callResult
		ui.Post(func() {
			ui.EvalJSResult(page, script, func(b json.RawMessage, err error) { r = callResult{b, err, true} })
		})
		waitFor(t, "eval "+script, func() bool { return r.done })
		return string(r.b), r.err
	}
	if v, err := eval("window.loaded"); err != nil || v != "1" {
		t.Fatalf("eval: %q, %v", v, err)
	}
	if v, err := eval("Promise.resolve({a: [1, 'x']})"); err != nil || v != `{"a":[1,"x"]}` {
		t.Fatalf("a promise: %q, %v", v, err)
	}
	if _, err := eval("throw new Error('nope')"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("a throw: %v", err)
	}

	// A message round trip: Go runs script that posts back.
	ui.Post(func() { ui.EvalJS(page, "window.asuka.postMessage('hi ' + document.title)") })
	waitFor(t, "the message", func() bool { return len(messages) > 0 })
	if messages[0] != "hi Home" {
		t.Fatalf("messages %q", messages)
	}

	// Dark mode follows the app's.
	onMain(func() { AdwStyleManagerSetColorScheme(AdwStyleManagerGetDefault(), ColorSchemeForceDark) })
	if v, _ := eval("matchMedia('(prefers-color-scheme: dark)').matches"); v != "true" {
		t.Errorf("dark: prefers-color-scheme dark matches %s", v)
	}
	onMain(func() { AdwStyleManagerSetColorScheme(AdwStyleManagerGetDefault(), ColorSchemeDefault) })

	// A file it can't show: a Download, not a page.
	ui.Post(func() { ui.EvalJS(page, "document.getElementById('f').click()") })
	waitFor(t, "the download", func() bool { return len(downloads) > 0 })
	if d := downloads[0]; !strings.HasSuffix(d.URL, "/file.zip") || d.MIME != "application/zip" || d.Filename != "file.zip" || d.Size != 2 {
		t.Fatalf("download %+v", d)
	}
	onMain(func() {
		if len(loads) != 1 {
			fatalf(t, "loads %q", loads)
		}
	})
}

// HTML shows markup, and URL an asset (a file in the app's assets, its
// links the assets' too).
func TestWebViewHTMLAndAsset(t *testing.T) {
	needWebKit(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "page.html"), []byte(`<!doctype html><title>asset</title><script src="page.js"></script>`), 0o644)
	os.WriteFile(filepath.Join(dir, "page.js"), []byte(`window.asuka.postMessage('asset ' + document.title)`), 0o644)
	t.Setenv("ASUKA_ASSETS", dir)
	var messages []string
	mount(t, func() ui.Node {
		on := func(m string) { messages = append(messages, m) }
		return ui.Column(ui.Style{},
			ui.WebView(ui.WebViewProps{HTML: ui.Val(`<p>hi</p><script>window.asuka.postMessage('html')</script>`), OnMessage: on, Style: ui.Style{Height: ui.Pt(100)}}),
			ui.WebView(ui.WebViewProps{URL: ui.Val("page.html"), OnMessage: on, Style: ui.Style{Height: ui.Pt(100)}}),
		)
	})
	waitFor(t, "both pages", func() bool { return len(messages) == 2 })
	sort.Strings(messages)
	if messages[0] != "asset asset" || messages[1] != "html" {
		t.Fatalf("messages %q", messages)
	}
}

// Preload runs before the page's script; Back after a link goes back;
// the web view goes with its node.
func TestWebViewPreloadBack(t *testing.T) {
	needWebKit(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><title>` + strings.TrimPrefix(r.URL.Path, "/") + `</title><script>window.asuka.postMessage(document.title + ' ' + window.pre)</script>`))
	}))
	defer srv.Close()
	var messages []string
	var states []ui.WebState
	var page ui.Node
	h, _ := mount(t, func() ui.Node {
		page = ui.WebView(ui.WebViewProps{
			URL:       ui.Val(srv.URL + "/one"),
			Preload:   "window.pre = 'early'",
			OnMessage: func(m string) { messages = append(messages, m) },
			OnState:   func(s ui.WebState) { states = append(states, s) },
			Style:     ui.Style{Width: ui.Pt(300), Height: ui.Pt(200)},
		})
		return page
	})
	waitFor(t, "the first page", func() bool { return len(messages) > 0 })
	ui.Post(func() { ui.EvalJS(page, "location.href = '/two'") })
	waitFor(t, "the second page", func() bool { return len(messages) > 1 })
	waitFor(t, "can go back", func() bool { return len(states) > 0 && states[len(states)-1].CanGoBack })
	ui.Post(func() { ui.WebBack(page) })
	waitFor(t, "back", func() bool {
		s := states[len(states)-1]
		return strings.HasSuffix(s.URL, "/one") && s.CanGoForward && !s.Loading
	})
	if messages[0] != "one early" || messages[1] != "two early" {
		t.Fatalf("messages %q", messages)
	}

	var w *web
	onMain(func() {
		n := find(h, ops.KindWebView)[0]
		w, _ = n.extWeb()
		h.enter(func() { h.destroy(n.id) })
		if !w.closed || w.view != 0 || len(w.handlers) != 0 {
			fatalf(t, "the web view wasn't let go: %+v", w)
		}
	})
}

// On a page whose Content Security Policy blocks eval, EvalJSResult
// still answers with the last statement's value, as JSON; what a script
// threw, or a value WebKit can't return (a promise), is an error.
func TestWebViewEvalContentSecurityPolicy(t *testing.T) {
	needWebKit(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "script-src 'self'")
		if r.URL.Path == "/ok.js" {
			w.Header().Set("Content-Type", "text/javascript")
			w.Write([]byte("window.loaded = 1"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><title>Locked</title><script src="/ok.js"></script>`))
	}))
	defer srv.Close()
	var loads []string
	var page ui.Node
	mount(t, func() ui.Node {
		page = ui.WebView(ui.WebViewProps{
			URL:    ui.Val(srv.URL + "/"),
			OnLoad: func(u string) { loads = append(loads, u) },
			Style:  ui.Style{Width: ui.Pt(300), Height: ui.Pt(200)},
		})
		return page
	})
	waitFor(t, "the page loads", func() bool { return len(loads) > 0 })
	eval := func(script string) (string, error) {
		var r callResult
		ui.Post(func() {
			ui.EvalJSResult(page, script, func(b json.RawMessage, err error) { r = callResult{b, err, true} })
		})
		waitFor(t, "eval "+script, func() bool { return r.done })
		return string(r.b), r.err
	}
	for _, c := range []struct{ script, want string }{
		{"document.title", `"Locked"`},
		{"window.loaded", `1`},
		{"let a = 2; a * 3", `6`},
		{"({a: [1, 'x', null]})", `{"a":[1,"x",null]}`},
		{"new Date(0)", `"1970-01-01T00:00:00.000Z"`},
		{"undefined", `null`},
		{"var kept = 5", `null`},
		{"kept", `5`},
	} {
		if v, err := eval(c.script); err != nil || v != c.want {
			t.Fatalf("%s: %q, %v; want %s", c.script, v, err, c.want)
		}
	}
	if _, err := eval("eval('1')"); err == nil {
		t.Fatal("the page doesn't block eval")
	}
	if _, err := eval("throw new Error('nope')"); err == nil || !strings.Contains(err.Error(), "nope") {
		t.Fatalf("a throw: %v", err)
	}
	if _, err := eval("1 +"); err == nil || !strings.Contains(err.Error(), "SyntaxError") {
		t.Fatalf("a syntax error: %v", err)
	}
	if _, err := eval("Promise.resolve(1)"); err == nil || !strings.Contains(err.Error(), "Content Security Policy") {
		t.Fatalf("a promise: %v", err)
	}
}

// asuka/sdk/js's module runs a script in a web view of its own: its value, a
// promise's, or what it threw; none is kept after.
func TestJSModule(t *testing.T) {
	needWebKit(t)
	h, _ := mount(t, emptyRoot)
	run := func(src string) ([]byte, error) {
		return callModule(t, h, "js", "eval", append(binary.LittleEndian.AppendUint32(nil, 20000), src...))
	}
	if b, err := run("1 + 2"); err != nil || string(b) != "3" {
		t.Fatalf("1 + 2: %s, %v", b, err)
	}
	if b, err := run("Promise.resolve({a: [1, 'x']})"); err != nil || string(b) != `{"a":[1,"x"]}` {
		t.Fatalf("a promise: %s, %v", b, err)
	}
	if _, err := run("null.x"); err == nil {
		t.Fatal("no error from a throw")
	}
	if _, err := callModule(t, h, "js", "eval", append(binary.LittleEndian.AppendUint32(nil, 50), "new Promise(() => {})"...)); err == nil || err.Error() != "timeout" {
		t.Fatalf("a script that never ends: %v", err)
	}
	onMain(func() {
		if scriptsRunning != 0 {
			fatalf(t, "%d scripts kept", scriptsRunning)
		}
	})
}

// The web module's cookies are the WebViews' store's, for a URL.
func TestWebCookies(t *testing.T) {
	needWebKit(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "session", Value: "abc", Path: "/"})
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<!doctype html><title>c</title>`))
	}))
	defer srv.Close()
	loaded := false
	h, _ := mount(t, func() ui.Node {
		return ui.WebView(ui.WebViewProps{URL: ui.Val(srv.URL + "/"), OnLoad: func(string) { loaded = true },
			Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(100)}})
	})
	waitFor(t, "the page", func() bool { return loaded })
	b, err := callModule(t, h, "web", "cookies", []byte(srv.URL+"/"))
	if err != nil || !strings.Contains(string(b), "session=abc") {
		t.Fatalf("cookies: %q, %v", b, err)
	}
}
