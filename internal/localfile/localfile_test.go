package localfile

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	assets, cache := t.TempDir(), t.TempDir()
	os.MkdirAll(filepath.Join(assets, "docs"), 0o755)
	os.WriteFile(filepath.Join(assets, "docs", "a.pdf"), []byte("%PDF"), 0o644)
	if p, err := Resolve("docs/a.pdf", "", assets, cache); err != nil || p != filepath.Join(assets, "docs", "a.pdf") {
		t.Fatalf("an asset: %q, %v", p, err)
	}
	abs := filepath.Join(assets, "docs", "a.pdf")
	if p, err := Resolve(abs, "", assets, cache); err != nil || p != abs {
		t.Fatalf("a path: %q, %v", p, err)
	}
	if _, err := Resolve("docs/missing.pdf", "", assets, cache); err == nil || !strings.Contains(err.Error(), "docs/missing.pdf") {
		t.Fatalf("a missing asset: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/report":
			w.Header().Set("Content-Type", "application/pdf")
		case "/named":
			w.Header().Set("Content-Disposition", `attachment; filename="Invoice 7.pdf"`)
		case "/gone":
			http.NotFound(w, r)
			return
		}
		w.Write([]byte("%PDF-1.4"))
	}))
	defer srv.Close()
	for path, want := range map[string]string{"/report": "report.pdf", "/named": "Invoice 7.pdf", "/x/photo.jpg": "photo.jpg"} {
		p, err := Resolve(srv.URL+path, "", assets, cache)
		if err != nil || filepath.Base(p) != want || !strings.HasPrefix(p, filepath.Join(cache, "ryui-files")) {
			t.Fatalf("%s: %q, %v", path, p, err)
		}
		if b, _ := os.ReadFile(p); string(b) != "%PDF-1.4" {
			t.Fatalf("%s: %q", path, b)
		}
	}
	if _, err := Resolve(srv.URL+"/gone", "", assets, cache); err == nil {
		t.Fatal("a 404: no error")
	}
}

func TestNewPDF(t *testing.T) {
	cache := t.TempDir()
	for title, want := range map[string]string{"Q3: Sales/Costs": "Q3- Sales-Costs.pdf", "": "Document.pdf", "about:blank": "Document.pdf"} {
		p, err := NewPDF(cache, title)
		if err != nil || filepath.Base(p) != want {
			t.Fatalf("%q: %q, %v", title, p, err)
		}
		if st, err := os.Stat(filepath.Dir(p)); err != nil || !st.IsDir() {
			t.Fatalf("%q: no folder", title)
		}
	}
	a, _ := NewPDF(cache, "x")
	b, _ := NewPDF(cache, "x")
	if a == b {
		t.Fatal("two PDFs of one title share a path")
	}
}
