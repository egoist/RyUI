package document

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// The system's watch of a document's file: another app writing it,
// replacing it as editors save, and deleting it each say so.
func TestSystemWatcher(t *testing.T) {
	saved := post
	t.Cleanup(func() { post = saved })
	changes := make(chan struct{}, 64)
	post = func(f func()) { f() }

	dir := t.TempDir()
	path := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(path, []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A long poll, so that what's seen is the system's watch's.
	w := systemWatcher{poll: pollWatcher{every: time.Hour}}
	stop := w.watch(path, func() { changes <- struct{}{} })
	defer stop()

	// The watch is set up on a goroutine: write until it says so.
	await := func(what string, do func()) {
		t.Helper()
		deadline := time.After(10 * time.Second)
		for {
			do()
			select {
			case <-changes:
				// Drain what the same change said more than once.
				for {
					select {
					case <-changes:
						continue
					case <-time.After(200 * time.Millisecond):
					}
					break
				}
				return
			case <-time.After(300 * time.Millisecond):
			case <-deadline:
				t.Fatalf("%s: no change came", what)
			}
		}
	}
	n := 0
	await("written", func() {
		n++
		os.WriteFile(path, []byte("two"+string(rune('a'+n%26))), 0o644)
	})
	await("replaced", func() {
		tmp := filepath.Join(dir, ".notes.txt.tmp")
		os.WriteFile(tmp, []byte("three"), 0o644)
		os.Rename(tmp, path)
	})
	await("deleted", func() {
		os.Remove(path)
		os.WriteFile(filepath.Join(dir, "other"), nil, 0o644) // nothing to remove again
	})
}
