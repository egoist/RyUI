package watch

import (
	"context"
	"io"

	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Watch is a watch Path keeps while its component lives.
type Watch struct {
	owner    *signal.Owner
	err      *signal.Signal[error]
	watching *signal.Signal[bool]
	root     *signal.Signal[string]
}

// Path watches the file or directory at path while the current owner
// (the component calling it) lives, as New does, and calls f on the main
// thread with each batch of changes. It stops as the owner is disposed.
// Main thread.
//
// The watch follows its accessor: when path changes, the old path's
// watch stops and the new one's starts. An empty path watches nothing.
// The watch starts on a goroutine, since listing a large tree takes a
// while: changes are reported once Watching is true. While f runs, the
// watch waits: what changes meanwhile comes in the next batch, so a busy
// main thread is never flooded.
//
// When the watched directory goes away, f gets a Remove of "." and the
// watch ends (Watching becomes false, Err stays nil). Err says what
// stopped it otherwise, or kept it from starting: the path doesn't
// exist, can't be read, has too many entries (ErrLimit), or the platform
// can't watch (ErrUnsupported).
func Path(path signal.Accessor[string], opts Options, f func(events []Event)) *Watch {
	w := &Watch{err: signal.New[error](nil), watching: signal.New(false), root: signal.New("")}
	w.owner = signal.NewOwner()
	signal.RunWithOwner(w.owner, func() {
		signal.Effect(func() {
			p := ""
			if path != nil {
				p = path.Get()
			}
			signal.OnCleanup(w.start(p, opts, f))
		})
	})
	return w
}

// Err is what stopped the watch, or kept it from starting; nil while it
// runs or starts, and after its directory went away. Reading it tracks
// it.
func (w *Watch) Err() error { return w.err.Get() }

// Watching reports whether the watch runs: true once everything is
// watched, false while it starts and once it stopped. Reading it tracks
// it.
func (w *Watch) Watching() bool { return w.watching.Get() }

// Root is the path watched, absolute and with its symlinks resolved, to
// which the paths of events are relative; empty until the watch runs.
// Reading it tracks it.
func (w *Watch) Root() string { return w.root.Get() }

// Stop stops the watch before its owner is disposed. Main thread.
func (w *Watch) Stop() { w.owner.Dispose() }

func (w *Watch) set(watching bool, root string, err error) {
	signal.Batch(func() {
		w.watching.Set(watching)
		w.root.Set(root)
		w.err.Set(err)
	})
}

// start starts watching p on a goroutine, and returns what stops it.
// live, read and written only on the main thread, says the watch is still
// wanted: what the goroutine posts after it stopped is dropped.
func (w *Watch) start(p string, opts Options, f func([]Event)) (stop func()) {
	w.set(false, "", nil)
	if p == "" {
		return func() {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	live := true
	post := func(do func()) {
		ui.Post(func() {
			if live {
				do()
			}
		})
	}
	go func() {
		watcher, err := New(ctx, p, opts)
		if err != nil {
			if ctx.Err() == nil {
				post(func() { w.set(false, "", err) })
			}
			return
		}
		defer watcher.Close()
		root := watcher.Root()
		post(func() { w.set(true, root, nil) })
		for {
			events, err := watcher.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if err == io.EOF {
					err = nil // its directory went away
				}
				post(func() { w.set(false, root, err) })
				return
			}
			handled := make(chan struct{})
			ui.Post(func() {
				defer close(handled)
				if live && f != nil {
					f(events)
				}
			})
			select {
			case <-handled:
			case <-ctx.Done():
				return
			}
		}
	}()
	return func() {
		live = false
		cancel()
		w.set(false, "", nil)
	}
}
