//go:build darwin && !ios

package darwin

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ui"
)

// runUntil runs the main run loop until done, or a long while.
func runUntil(done func() bool) bool {
	app := Send(Class("NSApplication"), "sharedApplication")
	mode := Const(Foundation(), "NSDefaultRunLoopMode")
	for i := 0; !done() && i < 2000; i++ { // a safety net, not what passes
		until := MsgFloatID(Class("NSDate"), Sel("dateWithTimeIntervalSinceNow:"), 0.01)
		if ev := Send(app, "nextEventMatchingMask:untilDate:inMode:dequeue:", ^uintptr(0), uintptr(until), uintptr(mode), 1); ev != 0 {
			Send(app, "sendEvent:", uintptr(ev))
		}
	}
	return done()
}

// inCore runs f as an app's code runs, its commands flushed after.
func inCore(h *Host, f func()) {
	h.enter(func() { ui.Post(f) })
	h.runPosted()
}

// A WebView blocks what Block names from its first page on, reports a
// file it can't show as a Download and its state as it changes, and
// runs script for its value.
func TestWebViewBlockDownloadEval(t *testing.T) {
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
	var loads []string
	var downloads []ui.WebDownload
	var states []ui.WebState
	var web ui.Node
	h, _ := mount(t, func() ui.Node {
		web = ui.WebView(ui.WebViewProps{
			URL:        ui.Val(srv.URL + "/"),
			Block:      []string{"/ads/"},
			OnLoad:     func(u string) { loads = append(loads, u) },
			OnDownload: func(d ui.WebDownload) { downloads = append(downloads, d) },
			OnState:    func(s ui.WebState) { states = append(states, s) },
			Style:      ui.Style{Width: ui.Pt(300), Height: ui.Pt(200)},
		})
		return web
	})
	onMain(func() {
		if !runUntil(func() bool { return len(loads) > 0 }) {
			fatalf(t, "the page didn't load: requests %v, states %+v", paths, states)
		}
		mu.Lock()
		got := strings.Join(paths, " ")
		mu.Unlock()
		if strings.Contains(got, "/ads/") || !strings.Contains(got, "/ok.js") {
			fatalf(t, "requests %s: /ads/ should be blocked, /ok.js not", got)
		}
		if len(states) == 0 || states[len(states)-1].Title != "Home" || states[len(states)-1].Loading {
			fatalf(t, "states %+v", states)
		}
		// Script's value, as JSON: only ok.js ran.
		var result string
		var evalErr error
		answered := false
		inCore(h, func() {
			ui.EvalJSResult(web, "window.loaded", func(b json.RawMessage, err error) { result, evalErr, answered = string(b), err, true })
		})
		if !runUntil(func() bool { return answered }) || evalErr != nil || result != "1" {
			fatalf(t, "eval: %q, %v", result, evalErr)
		}
		answered = false
		inCore(h, func() {
			ui.EvalJSResult(web, "throw new Error('nope')", func(b json.RawMessage, err error) { evalErr, answered = err, true })
		})
		if !runUntil(func() bool { return answered }) || evalErr == nil || !strings.Contains(evalErr.Error(), "nope") {
			fatalf(t, "a throw: %v", evalErr)
		}
		inCore(h, func() { ui.EvalJS(web, "document.getElementById('f').click()") })
		if !runUntil(func() bool { return len(downloads) > 0 }) {
			fatal(t, "no download")
		}
		if d := downloads[0]; !strings.HasSuffix(d.URL, "/file.zip") || d.MIME != "application/zip" {
			fatalf(t, "download %+v", d)
		}
	})
}

// ryui/js's module runs a script in a web view of its own: its value,
// a promise's, or what it threw.
func TestJSModule(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		run := func(src string) ([]byte, error) {
			return await(h, "js", "eval", append(binary.LittleEndian.AppendUint32(nil, 5000), src...))
		}
		if b, err := run("1 + 2"); err != nil || string(b) != "3" {
			fatalf(t, "1 + 2: %s, %v", b, err)
		}
		if b, err := run("Promise.resolve({a: [1, 'x']})"); err != nil || string(b) != `{"a":[1,"x"]}` {
			fatalf(t, "a promise: %s, %v", b, err)
		}
		if _, err := run("null.x"); err == nil {
			fatal(t, "no error from a throw")
		}
		if len(h.scripts) != 0 {
			fatalf(t, "%d scripts kept", len(h.scripts))
		}
	})
}

// The clipboard module puts an image file on the clipboard, says it has
// one, reads it back in its own format, and tells a waiting "next" of a
// change; "stop" ends a wait. On a pasteboard of the test's own.
func TestClipboardModule(t *testing.T) {
	testPasteboard = fmt.Sprintf("ryui-test-%d", time.Now().UnixNano())
	defer func() { testPasteboard = "" }()
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		// A 1x1 PNG.
		png, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8DwHwAFBQIAX8jx0gAAAABJRU5ErkJggg==")
		src := filepath.Join(t.TempDir(), "dot.png")
		os.WriteFile(src, png, 0o644)
		var changed []error
		h.module("clipboard")["next"](nil, func(_ []byte, err error) { changed = append(changed, err) })
		if _, err := await(h, "clipboard", "copyImage", []byte(src)); err != nil {
			fatal(t, "copyImage:", err)
		}
		if b, _ := await(h, "clipboard", "has", nil); len(b) != 3 || b[1] != 1 {
			fatalf(t, "has: %v", b)
		}
		b, err := await(h, "clipboard", "image", nil)
		if err != nil || !strings.HasSuffix(string(b), ".png") {
			fatalf(t, "image: %q, %v", b, err)
		}
		defer os.Remove(string(b))
		if got, _ := os.ReadFile(string(b)); !bytes.Equal(got, png) {
			fatal(t, "the image isn't the file's bytes")
		}
		if !runUntil(func() bool { return len(changed) > 0 }) || changed[0] != nil {
			fatalf(t, "next: %v", changed)
		}
		changed = nil
		h.module("clipboard")["next"](nil, func(_ []byte, err error) { changed = append(changed, err) })
		await(h, "clipboard", "stop", nil)
		if len(changed) != 1 || changed[0] == nil || changed[0].Error() != "stopped" {
			fatalf(t, "stop: %v", changed)
		}
		if h.clip.timer != 0 {
			fatal(t, "the timer runs on")
		}
	})
}
