//go:build !asuka_bundled && !ios && !android && !js

package plugin

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FilePath: the plugin's build if there's one, else the published file,
// downloaded once into the cache and checked against both hashes.
func TestFilePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // the user's cache
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("LocalAppData", t.TempDir())
	raw := []byte("a library's bytes")
	var gz bytes.Buffer
	zw := gzip.NewWriter(&gz)
	zw.Write(raw)
	zw.Close()
	sum := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
	gets := 0
	serve := gz.Bytes()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gets++
		if r.URL.Path != "/rel/l-darwin.gz" {
			http.NotFound(w, r)
			return
		}
		w.Write(serve)
	}))
	defer srv.Close()
	f := File{Name: "libl.dylib", Asset: "l-darwin.gz", Size: int64(gz.Len()), SHA256: sum(gz.Bytes()), RawSize: int64(len(raw)), RawSHA256: sum(raw)}
	l := Library{Name: "l", Release: srv.URL + "/rel", Files: map[string]File{"darwin": f}}

	p, err := FilePath("", l, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); !bytes.Equal(b, raw) || filepath.Base(p) != "libl.dylib" {
		t.Fatalf("%s: %q", p, b)
	}
	if _, err := FilePath("", l, "darwin"); err != nil || gets != 1 || !Cached("", l, "darwin") {
		t.Fatalf("not from the cache: %d downloads, %v", gets, err)
	}
	// A build of the plugin's own comes first.
	dir := t.TempDir()
	StoreBuild(dir, "darwin", "libl.dylib", []byte("built"), "s")
	if p, err := FilePath(dir, l, "darwin"); err != nil || !strings.HasPrefix(p, dir) {
		t.Fatal(p, err)
	}
	// A download that isn't the manifest's file is refused.
	os.Remove(p)
	cp, _ := CachePath(f)
	os.Remove(cp)
	serve = append(gz.Bytes(), 0)
	if _, err := FilePath("", l, "darwin"); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("a changed file: %v", err)
	}
	if _, err := FilePath("", l, "linux-amd64"); err == nil {
		t.Error("a target the library hasn't: no error")
	}
	unpublished := Library{Name: "l", Files: map[string]File{"darwin": {Name: "libl.dylib"}}}
	if _, err := FilePath("", unpublished, "darwin"); err == nil || !strings.Contains(err.Error(), "isn't published") {
		t.Errorf("unpublished: %v", err)
	}
}
