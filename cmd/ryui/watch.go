package main

import (
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// watcher notices changes to Go sources by polling their modification
// times (no dependencies, and cheap for app-sized trees).
type watcher struct {
	roots []string
	last  string
}

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
			if !code && !asset || strings.HasSuffix(name, "_test.go") {
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
	w.last = s
	return true
}
