// Package localfile finds the file a source names, for the desktop
// hosts' modules that hand files to the system (asuka/sdk/preview,
// asuka/sdk/print, asuka/sdk/compose): a path, a file:// URL, an asset's path, or
// an http(s) URL, downloaded first. The mobile hosts do the same in
// Swift and Kotlin.
package localfile

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Resolve is the file source names: under assets for an asset's path; a
// download into a new folder in cache (its name the server's, or the
// URL's, with the extension of the type it gives if it has none) for an
// http(s) URL. Not on the main thread: a download waits.
func Resolve(source, typ, assets, cache string) (string, error) {
	p := source
	switch {
	case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
		return download(source, typ, cache)
	case strings.HasPrefix(source, "file://"):
		u, err := url.Parse(source)
		if err != nil {
			return "", err
		}
		p = filepath.FromSlash(u.Path)
		if len(p) > 2 && p[0] == '\\' && p[2] == ':' { // file:///C:/... on Windows
			p = p[1:]
		}
	case !filepath.IsAbs(source):
		p = filepath.Join(assets, filepath.FromSlash(source))
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		return "", fmt.Errorf("no file at %s", source)
	}
	return p, nil
}

func download(source, typ, cache string) (string, error) {
	resp, err := http.Get(source)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("couldn't download %s: %s", source, resp.Status)
	}
	name := ""
	if _, params, err := mime.ParseMediaType(resp.Header.Get("Content-Disposition")); err == nil {
		name = params["filename"]
	}
	if name == "" {
		if u, err := url.Parse(source); err == nil {
			name = path.Base(u.Path)
		}
	}
	name = strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(strings.TrimSpace(name))
	if name == "" || name == "." || name == "_" {
		name = "file"
	}
	if filepath.Ext(name) == "" {
		if typ == "" {
			typ, _, _ = strings.Cut(resp.Header.Get("Content-Type"), ";")
		}
		if exts, _ := mime.ExtensionsByType(strings.TrimSpace(typ)); len(exts) > 0 {
			name += exts[0]
		}
	}
	var id [8]byte
	rand.Read(id[:])
	dir := filepath.Join(cache, "asuka-files", hex.EncodeToString(id[:]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		return "", err
	}
	return p, f.Close()
}

// CacheDir is the app's folder in the user's caches, by its ID (a
// bundle identifier) or name.
func CacheDir(app string) string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = os.TempDir()
	}
	return filepath.Join(base, app)
}

// NewPDF is a path for a new PDF in a folder of its own under cache
// (asuka-print/), named after title ("Document" if it has none), as it's
// shared or mailed.
func NewPDF(cache, title string) (string, error) {
	name := strings.TrimSpace(strings.NewReplacer("/", "-", "\\", "-", ":", "-").Replace(title))
	if name == "" || strings.HasPrefix(title, "about:") || strings.HasPrefix(title, "data:") { // a page without a <title>
		name = "Document"
	}
	var id [8]byte
	rand.Read(id[:])
	dir := filepath.Join(cache, "asuka-print", hex.EncodeToString(id[:]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, name+".pdf"), nil
}
