package watch

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeNative records the targets the scan enrolls; tests report changes
// with notify, as a system's watch would.
type fakeNative struct {
	mu      sync.Mutex
	targets map[uint64]target
	report  func(notice)
	closed  bool
	// subtree makes add accept subtree targets, as Windows does; noFiles
	// makes it refuse targets that aren't directories, as FSEvents,
	// inotify and Windows do.
	subtree, noFiles bool
	// addHook, when set, runs in add before the target is enrolled; an
	// error it returns is add's.
	addHook func(target) error
}

// fakeConfig configures the next fake native made.
var fakeConfig struct {
	sync.Mutex
	subtree, noFiles bool
	addHook          func(target) error
	last             *fakeNative
}

func init() {
	newFake = func(report func(notice), opts Options) native {
		fakeConfig.Lock()
		defer fakeConfig.Unlock()
		f := &fakeNative{targets: map[uint64]target{}, report: report, subtree: fakeConfig.subtree, noFiles: fakeConfig.noFiles, addHook: fakeConfig.addHook}
		fakeConfig.last = f
		return f
	}
}

// configureFake sets how the next fake natives behave, until the test
// ends.
func configureFake(t *testing.T, subtree, noFiles bool, hook func(target) error) {
	fakeConfig.Lock()
	fakeConfig.subtree, fakeConfig.noFiles, fakeConfig.addHook = subtree, noFiles, hook
	fakeConfig.Unlock()
	t.Cleanup(func() {
		fakeConfig.Lock()
		fakeConfig.subtree, fakeConfig.noFiles, fakeConfig.addHook = false, false, nil
		fakeConfig.Unlock()
	})
}

func (f *fakeNative) add(t target) error {
	if t.subtree && !f.subtree || !t.dir && f.noFiles {
		return ErrUnsupported
	}
	if f.addHook != nil {
		if err := f.addHook(t); err != nil {
			return err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.targets[t.id] = t
	return nil
}

func (f *fakeNative) remove(id uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.targets, id)
}

func (f *fakeNative) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	clear(f.targets)
}

func (f *fakeNative) list() []target {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := make([]target, 0, len(f.targets))
	for _, t := range f.targets {
		all = append(all, t)
	}
	return all
}

func (f *fakeNative) notify(n notice) {
	f.mu.Lock()
	closed := f.closed
	f.mu.Unlock()
	if !closed {
		f.report(n)
	}
}

// fakeWatch starts a watch of path with the fake native.
func fakeWatch(t *testing.T, path string, opts Options) *Watcher {
	t.Helper()
	opts.backend = "fake"
	w, err := New(context.Background(), path, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return w
}

// notifyAt reports n for the target at p, relative to the watch's root
// (".." its parent).
func notifyAt(t *testing.T, w *Watcher, p string, n notice) {
	t.Helper()
	f := w.native.(*fakeNative)
	want := filepath.Join(w.root, filepath.FromSlash(p))
	for _, target := range f.list() {
		if target.path == want {
			n.target = target.id
			f.notify(n)
			return
		}
	}
	t.Fatalf("no target for %q", p)
}

// guard is how long a test waits for what must happen: generous, since
// the tests never pass or fail on time, only on what happened.
const guard = 20 * time.Second

// waitCtx is a context the test's waits give up with.
func waitCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), guard)
	t.Cleanup(cancel)
	return ctx
}

// batch returns the next batch of w.
func batch(t *testing.T, w *Watcher) []Event {
	t.Helper()
	events, err := w.Next(waitCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// waitUntil waits for cond, failing after guard.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(guard)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}
