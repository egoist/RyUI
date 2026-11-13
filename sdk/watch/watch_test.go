package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// These tests run the system's own watch: FSEvents and kqueue on macOS
// (kqueue is iOS's), inotify on Linux, ReadDirectoryChangesW on Windows.
// They wait for what must come, however long it takes (up to guard), and
// check that what mustn't come didn't by a sentinel: a change made after
// it, which must come next.

// backends are the system watches the tests run through here.
func backends() []string {
	if runtime.GOOS == "darwin" {
		return []string{"", "kqueue"}
	}
	return []string{""}
}

func eachBackend(t *testing.T, test func(t *testing.T, backend string)) {
	for _, b := range backends() {
		name := b
		if name == "" {
			name = map[string]string{"darwin": "fsevents", "linux": "inotify", "android": "inotify", "windows": "readdirectorychanges", "js": "fs.watch"}[runtime.GOOS]
		}
		t.Run(name, func(t *testing.T) { test(t, b) })
	}
}

// tracker reads a watch's batches and finds events in them.
type tracker struct {
	t       *testing.T
	w       *Watcher
	ctx     context.Context
	pending []Event
	batches int
}

func start(t *testing.T, backend, path string, opts Options) *tracker {
	t.Helper()
	opts.backend = backend
	if opts.Debounce == 0 {
		opts.Debounce = 10e6 // 10 ms: quick tests
	}
	w, err := New(context.Background(), path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		w.Close()
		<-w.Done()
	})
	return &tracker{t: t, w: w, ctx: waitCtx(t)}
}

// next returns the next event.
func (r *tracker) next() Event {
	r.t.Helper()
	for len(r.pending) == 0 {
		events, err := r.w.Next(r.ctx)
		if err != nil {
			r.t.Fatalf("waiting for an event: %v", err)
		}
		r.batches++
		r.pending = events
	}
	e := r.pending[0]
	r.pending = r.pending[1:]
	return e
}

// until returns the events up to the first that is want, which it
// includes.
func (r *tracker) until(want Event) []Event {
	r.t.Helper()
	var seen []Event
	for {
		e := r.next()
		seen = append(seen, e)
		if e == want {
			return seen
		}
	}
}

// expect waits for want, failing if another event about one of paths
// comes before it.
func (r *tracker) expect(want Event, paths ...string) {
	r.t.Helper()
	for _, e := range r.until(want) {
		if e != want && (slices.Contains(paths, e.Path) || slices.Contains(paths, e.OldPath)) {
			r.t.Fatalf("got %v before %v", e, want)
		}
	}
}

// only makes a sentinel file and waits for its creation, failing if any
// other event comes first: what the test did before must not be
// reported.
func (r *tracker) only(sentinel string) {
	r.t.Helper()
	write(r.t, filepath.Join(r.w.Root(), filepath.FromSlash(sentinel)), "")
	if seen := r.until(Event{Op: Create, Path: sentinel}); len(seen) > 1 {
		r.t.Fatalf("unexpected events before the sentinel: %v", seen[:len(seen)-1])
	}
}

func write(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0755); err != nil {
		t.Fatal(err)
	}
}

func rename(t *testing.T, from, to string) {
	t.Helper()
	if err := os.Rename(from, to); err != nil {
		t.Fatal(err)
	}
}

func remove(t *testing.T, p string) {
	t.Helper()
	if err := os.RemoveAll(p); err != nil {
		t.Fatal(err)
	}
}

// A directory's files created, written, renamed and removed; the
// directories in it, without Recursive, only as entries.
func TestSystemDirectory(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		dir := t.TempDir()
		r := start(t, backend, dir, Options{})
		write(t, filepath.Join(dir, "a"), "hello")
		r.expect(Event{Op: Create, Path: "a"}, "a")
		write(t, filepath.Join(dir, "a"), "hello, world")
		r.expect(Event{Op: Write, Path: "a"}, "a")
		rename(t, filepath.Join(dir, "a"), filepath.Join(dir, "b"))
		r.expect(Event{Op: Rename, OldPath: "a", Path: "b"}, "a", "b")
		remove(t, filepath.Join(dir, "b"))
		r.expect(Event{Op: Remove, Path: "b"}, "b")
		mkdir(t, filepath.Join(dir, "sub"))
		r.expect(Event{Op: Create, Path: "sub", IsDir: true}, "sub")
		// Not recursive: what is in sub is not reported.
		write(t, filepath.Join(dir, "sub", "x"), "x")
		r.only("sentinel")
	})
}

// A recursive watch follows the directories created in it, renamed,
// moved in and out, and what moves between them.
func TestSystemRecursive(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		dir := t.TempDir()
		outside := t.TempDir()
		mkdir(t, filepath.Join(dir, "a"))
		mkdir(t, filepath.Join(dir, "b"))
		r := start(t, backend, dir, Options{Recursive: true})
		mkdir(t, filepath.Join(dir, "child"))
		write(t, filepath.Join(dir, "child", "nested"), "n")
		r.expect(Event{Op: Create, Path: "child/nested"}, "child/nested")
		// Directories made in a burst, each watched in its turn.
		mkdir(t, filepath.Join(dir, "child", "deeper", "deepest"))
		r.expect(Event{Op: Create, Path: "child/deeper/deepest", IsDir: true})
		write(t, filepath.Join(dir, "child", "deeper", "deepest", "f"), "f")
		r.expect(Event{Op: Create, Path: "child/deeper/deepest/f"})
		// A directory renamed is one event, and watched at its new path.
		rename(t, filepath.Join(dir, "child"), filepath.Join(dir, "moved"))
		r.expect(Event{Op: Rename, OldPath: "child", Path: "moved", IsDir: true}, "child/nested", "moved/nested")
		write(t, filepath.Join(dir, "moved", "deeper", "deepest", "g"), "g")
		r.expect(Event{Op: Create, Path: "moved/deeper/deepest/g"})
		// Between directories of the tree.
		write(t, filepath.Join(dir, "a", "x"), "x")
		r.expect(Event{Op: Create, Path: "a/x"})
		rename(t, filepath.Join(dir, "a", "x"), filepath.Join(dir, "b", "x"))
		r.expect(Event{Op: Rename, OldPath: "a/x", Path: "b/x"}, "a/x", "b/x")
		// Out of the tree, and into it.
		rename(t, filepath.Join(dir, "b", "x"), filepath.Join(outside, "x"))
		r.expect(Event{Op: Remove, Path: "b/x"}, "b/x")
		mkdir(t, filepath.Join(outside, "in"))
		write(t, filepath.Join(outside, "in", "y"), "y")
		rename(t, filepath.Join(outside, "in"), filepath.Join(dir, "in"))
		// What moved in with it is created too, and watched.
		r.expect(Event{Op: Create, Path: "in", IsDir: true}, "in")
		r.expect(Event{Op: Create, Path: "in/y"}, "in/y")
		write(t, filepath.Join(dir, "in", "y"), "written")
		r.expect(Event{Op: Write, Path: "in/y"}, "in/y")
		remove(t, filepath.Join(dir, "moved"))
		r.expect(Event{Op: Remove, Path: "moved", IsDir: true}, "moved")
	})
}

// A file saved as editors save, a temporary file renamed over it, is
// written, again and again: never removed and created, which would have
// an app close a document it still has. The file that replaced it is
// watched in its turn.
func TestSystemAtomicSave(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		dir := t.TempDir()
		file := filepath.Join(dir, "note.md")
		write(t, file, "a")
		r := start(t, backend, dir, Options{Recursive: true, Ignore: Patterns(".*")})
		for i := range 5 {
			tmp := filepath.Join(dir, ".note.md.tmp")
			write(t, tmp, strings.Repeat("x", i+2))
			rename(t, tmp, file)
			r.expect(Event{Op: Write, Path: "note.md"}, "note.md")
		}
		write(t, file, "in place")
		r.expect(Event{Op: Write, Path: "note.md"}, "note.md")
		r.only("sentinel")
	})
}

// A file is watched through its directory: written in place, saved as
// editors do, removed and created again; the other files of its
// directory are not reported.
func TestSystemFile(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		dir := t.TempDir()
		file := filepath.Join(dir, "config.json")
		write(t, file, "a")
		r := start(t, backend, file, Options{})
		write(t, file, "bb")
		r.expect(Event{Op: Write, Path: "."})
		write(t, filepath.Join(dir, "sibling"), "x")
		write(t, file+".tmp", "ccc")
		rename(t, file+".tmp", file)
		if e := r.next(); e != (Event{Op: Write, Path: "."}) {
			t.Fatalf("saved: %v", e)
		}
		write(t, file, "dddd")
		if e := r.next(); e != (Event{Op: Write, Path: "."}) {
			t.Fatalf("written in place: %v", e)
		}
		remove(t, file)
		if e := r.next(); e != (Event{Op: Remove, Path: "."}) {
			t.Fatalf("removed: %v", e)
		}
		write(t, file, "e")
		if e := r.next(); e != (Event{Op: Create, Path: "."}) {
			t.Fatalf("created: %v", e)
		}
		write(t, file, "ff")
		if e := r.next(); e != (Event{Op: Write, Path: "."}) {
			t.Fatalf("written: %v", e)
		}
	})
}

// A burst of changes comes as one batch: a file written many times is
// one write, and a file created and removed between two looks is
// nothing.
func TestSystemBurst(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		dir := t.TempDir()
		write(t, filepath.Join(dir, "f"), "")
		// A long quiet, so that the burst ends before the watch looks.
		r := start(t, backend, dir, Options{Debounce: 1e9, MaxDelay: 30e9})
		for i := range 200 {
			write(t, filepath.Join(dir, "f"), fmt.Sprint(i))
		}
		for i := range 20 {
			p := filepath.Join(dir, fmt.Sprint("tmp", i))
			write(t, p, "")
			remove(t, p)
		}
		write(t, filepath.Join(dir, "sentinel"), "")
		seen := r.until(Event{Op: Create, Path: "sentinel"})
		writes := 0
		for _, e := range seen {
			switch {
			case e == Event{Op: Write, Path: "f"}:
				writes++
			case e != Event{Op: Create, Path: "sentinel"}:
				t.Errorf("unexpected %v", e)
			}
		}
		if writes != 1 || r.batches != 1 {
			t.Fatalf("%d writes in %d batches: %v", writes, r.batches, seen)
		}
	})
}

// A watched directory removed ends the watch: the last batch has a Remove
// of ".", then Next says io.EOF, and Err nil.
func TestSystemDeletedDirectory(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		for i, recursive := range []bool{false, true, false} {
			dir := filepath.Join(t.TempDir(), "root")
			mkdir(t, dir)
			if i < 2 { // the last empty
				mkdir(t, filepath.Join(dir, "sub", "deeper"))
				write(t, filepath.Join(dir, "sub", "f"), "f")
				write(t, filepath.Join(dir, "g"), "g")
			}
			r := start(t, backend, dir, Options{Recursive: recursive})
			remove(t, dir)
			r.until(Event{Op: Remove, Path: ".", IsDir: true})
			if len(r.pending) > 0 {
				t.Fatalf("after the end: %v", r.pending)
			}
			if _, err := r.w.Next(r.ctx); err != io.EOF || r.w.Err() != nil {
				t.Fatalf("after the end: %v, %v", err, r.w.Err())
			}
			<-r.w.Done()
		}
	})
}

// The same for a watched directory renamed.
func TestSystemRenamedRoot(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		parent := t.TempDir()
		dir := filepath.Join(parent, "root")
		mkdir(t, dir)
		r := start(t, backend, dir, Options{Recursive: true})
		rename(t, dir, filepath.Join(parent, "renamed"))
		r.until(Event{Op: Remove, Path: ".", IsDir: true})
		if _, err := r.w.Next(r.ctx); err != io.EOF {
			t.Fatalf("after the end: %v", err)
		}
	})
}

// Symlinks are entries of their own, never followed.
func TestSystemSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need a privilege on Windows")
	}
	eachBackend(t, func(t *testing.T, backend string) {
		dir := t.TempDir()
		outside := t.TempDir()
		write(t, filepath.Join(outside, "target"), "t")
		r := start(t, backend, dir, Options{Recursive: true})
		if err := os.Symlink(outside, filepath.Join(dir, "l")); errors.Is(err, errors.ErrUnsupported) {
			t.Skip("no symlinks here")
		} else if err != nil {
			t.Fatal(err)
		}
		r.expect(Event{Op: Create, Path: "l"}, "l")
		write(t, filepath.Join(outside, "target"), "changed")
		write(t, filepath.Join(outside, "new"), "new")
		r.only("sentinel")
		remove(t, filepath.Join(dir, "l"))
		r.expect(Event{Op: Remove, Path: "l"}, "l")
		if _, err := os.Stat(filepath.Join(outside, "target")); err != nil {
			t.Fatal(err)
		}
	})
}

// Permissions changed are reported with Attributes, and not without.
func TestSystemAttributes(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		for _, attributes := range []bool{false, true} {
			dir := t.TempDir()
			file := filepath.Join(dir, "f")
			write(t, file, "f")
			r := start(t, backend, dir, Options{Attributes: attributes})
			if err := os.Chmod(file, 0444); err != nil {
				t.Fatal(err)
			}
			if attributes {
				r.expect(Event{Op: Attrib, Path: "f"}, "f")
			}
			r.only("sentinel")
			os.Chmod(file, 0644)
		}
	})
}

// A directory the watch can't read is not watched, and the rest is; one
// it can't open can't be watched at all.
func TestSystemPermissions(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() <= 0 {
		t.Skip("needs Unix permissions that apply")
	}
	eachBackend(t, func(t *testing.T, backend string) {
		dir := t.TempDir()
		locked := filepath.Join(dir, "locked")
		mkdir(t, locked)
		write(t, filepath.Join(locked, "secret"), "s")
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(locked, 0755) })
		if _, err := New(context.Background(), locked, Options{backend: backend}); !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("watching an unreadable directory: %v", err)
		}
		r := start(t, backend, dir, Options{Recursive: true})
		write(t, filepath.Join(dir, "visible"), "v")
		r.expect(Event{Op: Create, Path: "visible"})
		// Once readable, what is in it is found as it changes.
		if err := os.Chmod(locked, 0755); err != nil {
			t.Fatal(err)
		}
		write(t, filepath.Join(locked, "new"), "n")
		write(t, filepath.Join(dir, "touch"), "")
		r.until(Event{Op: Create, Path: "touch"})
		// A directory whose parent can't be listed (nor watched) is
		// watched still.
		hidden := filepath.Join(t.TempDir(), "hidden")
		mkdir(t, filepath.Join(hidden, "dir"))
		if err := os.Chmod(hidden, 0o300); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(hidden, 0o755) })
		r = start(t, backend, filepath.Join(hidden, "dir"), Options{})
		write(t, filepath.Join(hidden, "dir", "f"), "")
		r.expect(Event{Op: Create, Path: "f"})
	})
}

// Many watches at once, several of the same directory, each hear of what
// changes in theirs.
func TestSystemMany(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		shared := t.TempDir()
		var trackers []*tracker
		var dirs []string
		for i := range 48 {
			dir := shared
			if i%6 != 0 {
				dir = t.TempDir()
			}
			dirs = append(dirs, dir)
			trackers = append(trackers, start(t, backend, dir, Options{Recursive: i%2 == 0}))
		}
		write(t, filepath.Join(shared, "shared"), "")
		for i, dir := range dirs {
			if dir != shared {
				write(t, filepath.Join(dir, fmt.Sprint("own", i)), "")
			}
		}
		for i, r := range trackers {
			if dirs[i] == shared {
				r.expect(Event{Op: Create, Path: "shared"})
			} else {
				r.expect(Event{Op: Create, Path: fmt.Sprint("own", i)})
			}
		}
	})
}

// openFiles counts the process's open file descriptors, -1 where it
// can't.
func openFiles() int {
	dir := map[string]string{"darwin": "/dev/fd", "linux": "/proc/self/fd"}[runtime.GOOS]
	if dir == "" {
		return -1
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return -1
	}
	return len(entries)
}

// Closing a watch stops its events and releases every descriptor and
// goroutine it had, however it was busy.
func TestSystemCloseLeaksNothing(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		// A first watch loads what stays for the process (FSEvents' queue).
		warm := start(t, backend, t.TempDir(), Options{})
		warm.w.Close()
		<-warm.w.Done()
		files, goroutines := openFiles(), runtime.NumGoroutine()
		var watchers []*Watcher
		var dirs []string
		for i := range 16 {
			dir := t.TempDir()
			mkdir(t, filepath.Join(dir, "a", "b"))
			write(t, filepath.Join(dir, "a", "b", "f"), "f")
			w, err := New(context.Background(), dir, Options{backend: backend, Recursive: i%2 == 0, Debounce: -1})
			if err != nil {
				t.Fatal(err)
			}
			watchers = append(watchers, w)
			dirs = append(dirs, dir)
		}
		if n := openFiles(); files >= 0 && backend == "kqueue" && n < files+16*4 {
			t.Fatalf("kqueue: %d descriptors open, from %d", n, files)
		}
		// Busy as they close.
		for i, dir := range dirs {
			write(t, filepath.Join(dir, "a", "x"), "x")
			if i%2 == 0 {
				// A batch waiting for Next.
				ctx := waitCtx(t)
				if _, err := watchers[i].Next(ctx); err != nil {
					t.Fatal(err)
				}
			}
		}
		for _, w := range watchers {
			w.Close()
		}
		for i, w := range watchers {
			<-w.Done()
			write(t, filepath.Join(dirs[i], "after"), "")
			if events, err := w.Next(context.Background()); err != io.EOF {
				t.Fatalf("after Close: %v, %v", events, err)
			}
		}
		waitUntil(t, "the descriptors to be closed", func() bool { return openFiles() <= files })
		waitUntil(t, "the goroutines to end", func() bool { return runtime.NumGoroutine() <= goroutines })
	})
}

// A watch canceled while it lists a large tree stops.
func TestSystemCancelStart(t *testing.T) {
	eachBackend(t, func(t *testing.T, backend string) {
		dir := t.TempDir()
		for i := range 20 {
			sub := filepath.Join(dir, fmt.Sprint("d", i))
			mkdir(t, sub)
			for j := range 50 {
				write(t, filepath.Join(sub, fmt.Sprint("f", j)), "")
			}
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := New(ctx, dir, Options{backend: backend, Recursive: true}); !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled: %v", err)
		}
		// And with a limit, too many to watch.
		if _, err := New(context.Background(), dir, Options{backend: backend, Recursive: true, MaxEntries: 100}); !errors.Is(err, ErrLimit) {
			t.Fatalf("limit: %v", err)
		}
	})
}
