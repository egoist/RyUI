//go:build !asuka_bundled && !ios && !android && !js

package plugin

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// FilePath is a library's file for target on this machine: the plugin's
// own build (BuildPath, in dir, the manifest's directory; "" for none),
// else the published file in the user's cache (CachePath), downloaded
// there first if it isn't, verified against the manifest's hashes. The
// CLI gets what it ships with it; LibraryPath, in go run and go test.
func FilePath(dir string, l Library, target string) (string, error) {
	f, ok := l.Files[target]
	if !ok {
		return "", fmt.Errorf("plugin: library %s has no file for %s", l.Name, target)
	}
	if dir != "" {
		if p := BuildPath(dir, target, f.Name); exists(p) {
			return p, nil
		}
	}
	if f.Asset == "" {
		return "", fmt.Errorf("plugin: library %s for %s isn't published (asuka plugin publish), nor built", l.Name, target)
	}
	p, err := CachePath(f)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(p); err == nil && st.Size() == f.RawSize {
		return p, nil
	}
	raw, err := download(l, f)
	if err != nil {
		return "", fmt.Errorf("plugin: library %s for %s, from %s: %w", l.Name, target, l.URL(f), err)
	}
	if err := writeAtomic(p, raw); err != nil {
		return "", err
	}
	return p, nil
}

// Cached reports whether FilePath has the file without downloading.
func Cached(dir string, l Library, target string) bool {
	f, ok := l.Files[target]
	if !ok {
		return false
	}
	if dir != "" && exists(BuildPath(dir, target, f.Name)) {
		return true
	}
	p, err := CachePath(f)
	if err != nil {
		return false
	}
	st, err := os.Stat(p)
	return err == nil && st.Size() == f.RawSize
}

// CachePath is where FilePath keeps a published file:
// <the user's cache>/asuka/plugins/<its SHA-256>/<name>.
func CachePath(f File) (string, error) {
	if len(f.RawSHA256) < 16 {
		return "", errors.New("plugin: a file with no SHA-256")
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "asuka", "plugins", f.RawSHA256[:16], f.Name), nil
}

func exists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// download fetches f and checks both its hashes.
func download(l Library, f File) ([]byte, error) {
	c := &http.Client{Timeout: 5 * time.Minute}
	resp, err := c.Get(l.URL(f))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(resp.Status)
	}
	gz, err := io.ReadAll(io.LimitReader(resp.Body, f.Size+1))
	if err != nil {
		return nil, err
	}
	if sum := sha256.Sum256(gz); hex.EncodeToString(sum[:]) != f.SHA256 {
		return nil, errors.New("the download isn't the file the manifest names (SHA-256)")
	}
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(zr, f.RawSize+1))
	if err != nil {
		return nil, err
	}
	if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != f.RawSHA256 {
		return nil, errors.New("the library isn't the one the manifest names (SHA-256)")
	}
	return raw, nil
}

// writeAtomic writes b to p whole, then renames it: another process
// getting the same file at once never loads half a library.
func writeAtomic(p string, b []byte) error {
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".lib-*")
	if err != nil {
		return err
	}
	_, werr := f.Write(b)
	cerr := f.Close()
	if err := errors.Join(werr, cerr, os.Chmod(f.Name(), 0o755)); err != nil {
		os.Remove(f.Name())
		return err
	}
	if err := os.Rename(f.Name(), p); err != nil {
		os.Remove(f.Name())
		if st, serr := os.Stat(p); serr == nil && st.Size() == int64(len(b)) {
			return nil // another process's
		}
		return err
	}
	return nil
}
