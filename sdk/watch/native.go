package watch

import (
	"errors"
	"io/fs"
	"sync"
)

// The system's side of a watch. The scan enrolls targets, the files and
// directories it wants told about, in a native watch, which reports
// notices: where the system says something changed. Notices are only
// hints; the scan looks there and compares.

// target is a file or directory the scan enrolls in a native watch. IDs
// are never reused within a watch.
type target struct {
	id   uint64
	path string
	// dir says the target is a directory. A backend that watches a file
	// only through its directory, which the scan enrolls before it lists
	// it, returns ErrUnsupported for a target that isn't one, and the scan
	// enrolls no more files.
	dir bool
	// info is what the scan saw at path. Backends open the target and
	// check it is still the same file, so that a replacement racing the
	// enrollment is not watched in its place.
	info fs.FileInfo
	// subtree asks to watch everything beneath the directory with this
	// one target, and to name what changes deeper down by its
	// slash-separated path relative to it. A backend that can't returns
	// ErrUnsupported, and the scan enrolls every directory instead.
	subtree bool
}

// kind says what changed, as the system says it: a hint, since the scan
// looks again whatever the kind.
type kind uint8

const (
	// kindDirty means something about the target or its child changed.
	kindDirty kind = iota
	kindWrite
	kindCreate
	kindRemove
	kindMoveFrom
	kindMoveTo
	// kindLost means the system stopped watching the target, for example
	// because it was deleted; the scan enrolls it again if it still
	// exists.
	kindLost
)

// notice reports a change to a target (name empty) or to one of the
// entries of a directory target (name its name; a slash-separated path
// for a subtree target). A notice with err set ends the watch, except
// errOverflow, after which the scan looks at everything again.
type notice struct {
	target uint64
	kind   kind
	name   string
	err    error
}

// errOverflow is the err of a notice when the system dropped changes,
// for example because its queue overflowed.
var errOverflow = errors.New("watch: the system dropped changes")

// native watches the targets the scan adds. Its methods are called from
// the goroutine of the watch. close stops the watch and waits until every
// descriptor and handle is released and notify will not be called again.
type native interface {
	add(t target) error
	remove(id uint64)
	close()
}

// driver is a backend's watcher, run on a goroutine of its own by
// runDriver. Every method but wake runs on that goroutine. wait blocks
// until the system reports changes or wake is called; an error it returns
// ends the watch.
type driver interface {
	add(target) error
	remove(id uint64)
	wait() ([]notice, error)
	wake()
	close()
}

// errClosed is what add returns once the watch has stopped.
var errClosed = errors.New("watch: closed")

type command struct {
	target target
	remove uint64
	reply  chan error
}

// driven is a native running a driver on a goroutine of its own, which
// owns the system's descriptors, adds and removes targets between waits,
// and reports what the system tells it.
type driven struct {
	driver driver
	notify func(notice)
	mu     sync.Mutex
	queue  []command
	closed bool
	done   chan struct{}
}

// runDriver starts the goroutine of d, which calls notify with what d
// reports.
func runDriver(d driver, notify func(notice)) *driven {
	w := &driven{driver: d, notify: notify, done: make(chan struct{})}
	go w.run()
	return w
}

// add enrolls t and returns once the driver has.
func (w *driven) add(t target) error {
	reply := make(chan error, 1)
	if !w.send(command{target: t, reply: reply}) {
		return errClosed
	}
	select {
	case err := <-reply:
		return err
	case <-w.done:
		return errClosed
	}
}

func (w *driven) remove(id uint64) { w.send(command{remove: id}) }

// close stops the goroutine and waits until the driver has closed.
func (w *driven) close() {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	w.driver.wake()
	<-w.done
}

func (w *driven) send(c command) bool {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return false
	}
	w.queue = append(w.queue, c)
	w.mu.Unlock()
	w.driver.wake()
	return true
}

func (w *driven) run() {
	defer close(w.done)
	defer w.driver.close()
	for {
		w.mu.Lock()
		queue, closed := w.queue, w.closed
		w.queue = nil
		w.mu.Unlock()
		if closed {
			return
		}
		for _, c := range queue {
			if c.reply != nil {
				c.reply <- w.driver.add(c.target)
			} else {
				w.driver.remove(c.remove)
			}
		}
		notices, err := w.driver.wait()
		for _, n := range notices {
			w.notify(n)
		}
		if err != nil {
			w.mu.Lock()
			w.closed = true
			w.mu.Unlock()
			w.notify(notice{err: err})
			return
		}
	}
}

// newNative starts the native watch opts asks for: the platform's own, or
// for tests another.
func newNative(notify func(notice), opts Options) (native, error) {
	if opts.backend == "fake" && newFake != nil {
		return newFake(notify, opts), nil
	}
	return newPlatform(notify, opts)
}

// newFake makes the tests' native watch, which they drive by hand.
var newFake func(notify func(notice), opts Options) native
