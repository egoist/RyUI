package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The page finds its files from any route's path: its <base href> is the
// site's base.
func TestWebIndexBase(t *testing.T) {
	for base, want := range map[string]string{"": "/", "/": "/", "app": "/app/", "/app/": "/app/", "/a/b": "/a/b/"} {
		page := webIndex(webPage{Name: "Notes", Lang: "en", Base: base, Host: "asuka/x", Wasm: "app.wasm", Exec: "asuka/x/wasm_exec.js"})
		if !strings.Contains(page, `<base href="`+want+`">`) || strings.Index(page, "<base") > strings.Index(page, "<link") && strings.Contains(page, "<link") {
			t.Errorf("base %q: %s", base, page)
		}
	}
}

// A static site serves the page for its routes' paths (Netlify's
// rewrite), and its headers are under its base.
func TestWebSiteRewritesAndHeaders(t *testing.T) {
	if got := webRedirects("/app/"); got != "/app/*  /app/index.html  200\n" {
		t.Fatalf("_redirects %q", got)
	}
	h := webHeaders("/app/")
	if !strings.HasPrefix(h, "/app/index.html\n  Cache-Control: no-cache\n") || !strings.Contains(h, "/app/*.wasm\n  Cache-Control: public, max-age=31536000, immutable\n  Content-Type: application/wasm\n") {
		t.Fatalf("_headers:\n%s", h)
	}
	if strings.Contains(webHeaders("/"), "//") {
		t.Fatalf("_headers:\n%s", webHeaders("/"))
	}
}

// asuka dev serves the page at a route's path too (a reload, a link
// pasted), and still a 404 for a file that isn't there.
func TestDevServerServesRoutes(t *testing.T) {
	s := &devServer{app: &goApp{cfg: &appConfig{ID: "com.example.notes", Name: "Notes"}}}
	empty := map[string]pluginLib{}
	s.plugins.Store(&empty)
	var none []webScript
	s.scripts.Store(&none)
	for _, c := range []struct {
		path, accept string
		code         int
	}{
		{"/chat/7", "text/html,application/xhtml+xml", 200},
		{"/files/docs/a.md", "text/html", 200},
		{"/nothing.js", "*/*", 404},
	} {
		r := httptest.NewRequest(http.MethodGet, c.path, nil)
		r.Header.Set("Accept", c.accept)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != c.code || (c.code == 200 && !strings.Contains(w.Body.String(), `<base href="/">`)) {
			t.Errorf("%s: %d\n%s", c.path, w.Code, w.Body.String())
		}
	}
}
