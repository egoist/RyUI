package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// watcher notices changes to Go sources and catalogs by polling their
// modification times (no dependencies, and cheap for app-sized trees).
type watcher struct {
	roots []string
	last  string
}

// beforeRebuild runs when something changed, before the rebuild: it writes
// what's generated from what changed (the catalogs' functions), which the
// same change shouldn't then notice.
var beforeRebuild func()

func newWatcher(roots ...string) *watcher {
	w := &watcher{roots: roots}
	w.last = w.scan()
	return w
}

func (w *watcher) scan() string {
	var b strings.Builder
	for _, root := range w.roots {
		filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "build" || name == "node_modules") {
					return filepath.SkipDir
				}
				return nil
			}
			// Go, the app's native code (ios/, android/) and its assets.
			code := strings.HasSuffix(name, ".go") || name == "go.mod" || strings.HasSuffix(name, ".swift") || strings.HasSuffix(name, ".kt")
			asset := strings.Contains(filepath.ToSlash(path), "/assets/")
			catalog := strings.HasSuffix(name, ".json") && (name == "asuka.json" || isCatalogDir(filepath.Dir(path)))
			if !code && !asset && !catalog || strings.HasSuffix(name, "_test.go") {
				return nil
			}
			if info, err := d.Info(); err == nil {
				b.WriteString(path)
				b.WriteString(info.ModTime().Format(time.RFC3339Nano))
			}
			return nil
		})
	}
	return b.String()
}

func (w *watcher) changed() bool {
	s := w.scan()
	if s == w.last {
		return false
	}
	if beforeRebuild != nil {
		beforeRebuild()
		s = w.scan()
	}
	w.last = s
	vetAgain()
	return true
}

// isCatalogDir reports whether dir holds catalogs: it has a catalog_gen.go
// beside them.
func isCatalogDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "catalog_gen.go"))
	return err == nil
}
