package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// asuka dev -platform web reloads the page when JavaScript it loads as it
// is changes: the app's web/*.js, the host's files and those under them
// (a checkout's src/modules/).
func TestWebWatcher(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "modules"), 0o755)
	write := func(name, s string, at time.Time) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, at, at)
	}
	t0 := time.Now().Add(-time.Hour)
	write("natives.js", "a", t0)
	write("modules/maps.js", "a", t0)
	w := newWebWatcher(dir)
	if w.changed() {
		t.Fatal("changed with nothing changed")
	}
	write("natives.js", "b", t0.Add(time.Second))
	if !w.changed() || w.changed() {
		t.Fatal("a script's change: noticed once")
	}
	write("modules/maps.js", "b", t0.Add(time.Second))
	if !w.changed() {
		t.Fatal("a change in a directory under it wasn't noticed")
	}
	write("more.js", "a", t0)
	if !w.changed() {
		t.Fatal("a new script wasn't noticed")
	}
	if newWebWatcher("").changed() {
		t.Fatal("no directory: never changed")
	}
}
