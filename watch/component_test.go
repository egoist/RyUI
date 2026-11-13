package watch_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/egoist/asuka/watch"
)

// mount mounts root on a fake host, and returns what runs the main
// thread's posted work until cond holds.
func mount(t *testing.T, root func() ui.Node) (*ui.Root, func(what string, cond func() bool)) {
	t.Helper()
	h := fake.New()
	r := ui.Mount(h, root)
	t.Cleanup(r.Dispose)
	until := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			h.RunPosted()
			r.Flush()
			if cond() {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(2 * time.Millisecond)
		}
	}
	return r, until
}

func has(events []watch.Event, e watch.Event) bool { return slices.Contains(events, e) }

// Path watches while its component lives: each batch reaches f on the
// main thread, and disposing the component stops the watch and its
// goroutines.
func TestPathLifetime(t *testing.T) {
	dir := t.TempDir()
	goroutines := runtime.NumGoroutine()
	shown := signal.New(true)
	var events []watch.Event
	var w *watch.Watch
	r, until := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			w = watch.Path(signal.Static(dir), watch.Options{Debounce: 5e6}, func(batch []watch.Event) {
				events = append(events, batch...)
			})
			return ui.StaticText("folder")
		})
	})
	until("the watch to start", w.Watching)
	if want, _ := filepath.EvalSymlinks(dir); w.Root() != want || w.Err() != nil {
		t.Fatalf("root %q, err %v", w.Root(), w.Err())
	}
	if err := os.WriteFile(filepath.Join(dir, "a"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	until("a's creation", func() bool { return has(events, watch.Event{Op: watch.Create, Path: "a"}) })
	shown.Set(false)
	r.Flush()
	if w.Watching() {
		t.Fatal("still watching once disposed")
	}
	until("the goroutines to end", func() bool { return runtime.NumGoroutine() <= goroutines })
	n := len(events)
	if err := os.WriteFile(filepath.Join(dir, "b"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	// Watched again, once shown: b was made before, so the next event is
	// c's.
	shown.Set(true)
	r.Flush()
	until("the watch to start again", w.Watching)
	if err := os.WriteFile(filepath.Join(dir, "c"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	until("c's creation", func() bool { return has(events, watch.Event{Op: watch.Create, Path: "c"}) })
	if has(events[n:], watch.Event{Op: watch.Create, Path: "b"}) {
		t.Fatalf("b reported while not watched: %v", events[n:])
	}
}

// The watch follows its path: the old one's stops as the new one's
// starts; an empty path watches nothing.
func TestPathFollows(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	path := signal.New(a)
	var events []watch.Event
	var w *watch.Watch
	r, until := mount(t, func() ui.Node {
		w = watch.Path(path, watch.Options{Debounce: 5e6}, func(batch []watch.Event) {
			events = append(events, batch...)
		})
		return ui.StaticText("folder")
	})
	until("a's watch", w.Watching)
	path.Set(b)
	r.Flush()
	until("b's watch", func() bool { return w.Watching() && filepath.Base(w.Root()) == filepath.Base(b) })
	if err := os.WriteFile(filepath.Join(a, "in-a"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(b, "in-b"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	until("in-b", func() bool { return has(events, watch.Event{Op: watch.Create, Path: "in-b"}) })
	if has(events, watch.Event{Op: watch.Create, Path: "in-a"}) {
		t.Fatalf("a's watch outlived its path: %v", events)
	}
	path.Set("")
	r.Flush()
	if w.Watching() || w.Root() != "" || w.Err() != nil {
		t.Fatalf("empty path: watching %v, root %q, err %v", w.Watching(), w.Root(), w.Err())
	}
}

// What keeps a watch from starting is Err; a watched directory that goes
// away ends the watch after f hears of it, without an error.
func TestPathErrors(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "dir")
	path := signal.New(dir)
	var events []watch.Event
	var w *watch.Watch
	r, until := mount(t, func() ui.Node {
		w = watch.Path(path, watch.Options{Debounce: 5e6}, func(batch []watch.Event) {
			events = append(events, batch...)
		})
		return ui.StaticText("folder")
	})
	until("the error", func() bool { return w.Err() != nil })
	if !errors.Is(w.Err(), fs.ErrNotExist) || w.Watching() {
		t.Fatalf("err %v, watching %v", w.Err(), w.Watching())
	}
	if err := os.Mkdir(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Set again to try again: a new value, then the path.
	path.Set("")
	r.Flush()
	path.Set(dir)
	r.Flush()
	until("the watch", w.Watching)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	until("the end", func() bool { return !w.Watching() })
	if w.Err() != nil || !has(events, watch.Event{Op: watch.Remove, Path: ".", IsDir: true}) {
		t.Fatalf("err %v, events %v", w.Err(), events)
	}
}

// Stop stops a watch before its owner is disposed.
func TestPathStop(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	var w *watch.Watch
	_, until := mount(t, func() ui.Node {
		w = watch.Path(signal.Static(dir), watch.Options{Debounce: 5e6}, func([]watch.Event) { calls++ })
		return ui.StaticText("folder")
	})
	until("the watch", w.Watching)
	w.Stop()
	if w.Watching() {
		t.Fatal("watching after Stop")
	}
	if err := os.WriteFile(filepath.Join(dir, "a"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	// Another watch of the same directory hears of it; the stopped one
	// doesn't.
	other, err := watch.New(t.Context(), dir, watch.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := os.WriteFile(filepath.Join(dir, "b"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Next(t.Context()); err != nil {
		t.Fatal(err)
	}
	until("posted work", func() bool { return true })
	if calls != 0 {
		t.Fatalf("%d calls after Stop", calls)
	}
}
