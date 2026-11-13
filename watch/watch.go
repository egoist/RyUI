// Package watch follows changes to files and directories: a folder the
// app shows, a project open in an editor, a settings file another
// program writes, a document saved by another app.
//
// In a component, Path watches a path while the component lives and
// calls a function on the main thread with each batch of changes:
//
//	func Folder(dir signal.Accessor[string]) ui.Node {
//		names := signal.NewFunc[[]string](nil, slices.Equal)
//		reload := func() {
//			d := signal.Untrack(dir.Get)
//			go func() {
//				list := readNames(d) // os.ReadDir, off the main thread
//				ui.Post(func() { names.Set(list) })
//			}()
//		}
//		reload()
//		w := watch.Path(dir, watch.Options{Ignore: watch.Patterns(".*")}, func([]watch.Event) {
//			reload() // read the folder again
//		})
//		return ui.Column(nil,
//			ui.Show(signal.Func(func() bool { return w.Err() != nil }), func() ui.Node {
//				return ui.Text(signal.Func(func() string { return w.Err().Error() }))
//			}),
//			ui.For(names, func(n string) string { return n }, func(n signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
//				return ui.Text(n)
//			}),
//		)
//	}
//
// Elsewhere, New watches a path from any goroutine and Next returns its
// changes, until Close:
//
//	w, err := watch.New(ctx, dir, watch.Options{Recursive: true})
//	if err != nil {
//		return err
//	}
//	defer w.Close()
//	for {
//		events, err := w.Next(ctx)
//		if err != nil {
//			return err // io.EOF once closed, or once dir went away
//		}
//		for _, e := range events {
//			log.Println(e.Op, e.Path)
//		}
//	}
//
// A watch keeps what it saw of the files (their identity, size,
// modification time and permissions) and, each time the system says
// something changed, looks there again and compares: the events are the
// differences, not every step that made them. A file written many times
// in a row is one Write; one created and removed in between is nothing; a
// file replaced at its path by another, as editors and atomic saves do (a
// temporary file renamed over it), is a Write, and the watch follows the
// new file; a file or directory that moved within the watched directory
// is a Rename with its old and new path, paired by its identity (device
// and inode; volume and file ID on Windows), on every platform. Treat
// events as hints to read the files again.
//
// Each platform's own way, all without cgo:
//
//   - macOS: FSEvents (CoreServices, through purego), with file-level
//     events: one stream covers a whole tree, so a watch costs no file
//     descriptor per file and a large folder is fine.
//   - iOS: kqueue (EVFILT_VNODE), a descriptor per directory and file
//     watched, within the app's sandbox.
//   - Linux and Android: inotify, a watch descriptor per directory
//     (fs.inotify.max_user_watches), through one inotify instance for
//     the app; new directories are watched as they appear.
//   - Windows: ReadDirectoryChangesW with overlapped I/O on a completion
//     port: one handle covers a recursive watch's whole tree, so its
//     directories can still be renamed, and the root's parent is watched
//     too, to know when the root is renamed.
//   - The web: the app's files are the web host's file system in the
//     page's memory (platform/web/src/fs.js), which says what the app's
//     Go code changes in it. Under Node (go test), Node's fs.watch.
//
// When the system drops changes (inotify's queue overflowed, FSEvents
// says to rescan, ReadDirectoryChangesW's buffer was too small), the
// watch looks at everything again and the batch starts with an Overflow
// event: the other events of the batch are what it found.
//
// Symlinks are reported as themselves (created, removed, renamed),
// never followed: a watch doesn't leave its directory. The path given is
// resolved first (macOS's /tmp is /private/tmp), and Root says what it
// resolved to.
package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"
	"time"
)

// Op is the kind of an Event.
type Op uint8

const (
	// Create is a file or directory that appeared.
	Create Op = iota + 1
	// Write is a file whose content may have changed: its size or
	// modification time changed, the system said it was written, or
	// another file replaced it at its path, as atomic saves do.
	// Directories are not written: their entries are what change.
	Write
	// Remove is a file or directory that went away. A Remove of "." is
	// the watched directory or file itself: for a directory, the last
	// event of the watch.
	Remove
	// Rename is a file or directory that moved within the watched
	// directory, from OldPath to Path. What moved in or out of it is a
	// Create or a Remove.
	Rename
	// Attrib is a file or directory whose permissions changed, reported
	// only with Options.Attributes.
	Attrib
	// Overflow says the system dropped changes: the watch looked at
	// everything again, and the other events of the batch are what it
	// found. Path is ".".
	Overflow
)

func (o Op) String() string {
	switch o {
	case Create:
		return "create"
	case Write:
		return "write"
	case Remove:
		return "remove"
	case Rename:
		return "rename"
	case Attrib:
		return "attrib"
	case Overflow:
		return "overflow"
	}
	return fmt.Sprintf("Op(%d)", uint8(o))
}

// Event is a change to a watched file or directory.
type Event struct {
	Op Op
	// Path is the path relative to the watched directory, separated by
	// slashes; "." is the watched directory or file itself.
	Path string
	// OldPath is where a Rename moved from.
	OldPath string
	// IsDir reports whether it is a directory (as it was, for a Remove).
	IsDir bool
}

func (e Event) String() string {
	if e.Op == Rename {
		return fmt.Sprintf("rename %s -> %s", e.OldPath, e.Path)
	}
	return e.Op.String() + " " + e.Path
}

// Options configures a watch. The zero value watches a file, or a
// directory and what is in it but not in the directories in it.
type Options struct {
	// Recursive watches the directories in the directory too, and those
	// created later.
	Recursive bool
	// Ignore, when set, is asked about every file and directory the watch
	// finds, by its path relative to the watched directory, separated by
	// slashes, and whether it is a directory. What it ignores is not
	// watched, reported or counted in MaxEntries, nor is anything in a
	// directory it ignores, which is never opened. Patterns makes one
	// from glob patterns. It runs on a goroutine of the watch, and must
	// be quick.
	Ignore func(path string, isDir bool) bool
	// Debounce is how long the watch waits for changes to stop before it
	// reports them: 50 ms when zero, right away when negative.
	Debounce time.Duration
	// MaxDelay is the longest changes wait while more keep coming: 500 ms
	// when zero, and never less than Debounce.
	MaxDelay time.Duration
	// MaxEntries is how many files and directories a watch keeps track
	// of before it stops with ErrLimit: when zero, 65536 on macOS, where
	// one stream covers a tree, and 8192 elsewhere, where the system
	// takes a descriptor per directory (inotify) or per file (kqueue).
	MaxEntries int
	// Attributes reports changes of permissions as Attrib events. Without
	// it they are not reported: backup tools, indexers and antivirus
	// touch them often.
	Attributes bool

	// backend picks a system backend other than the platform's default,
	// for tests ("kqueue" on macOS, "fake").
	backend string
}

var (
	// ErrLimit is what a watch stops with when its directory has more
	// than Options.MaxEntries files and directories.
	ErrLimit = errors.New("watch: too many files and directories")
	// ErrUnsupported: the platform can't watch files.
	ErrUnsupported = errors.New("watch: not supported on this platform")
	// errRootChanged: what is watched was replaced since the watch began.
	errRootChanged = errors.New("watched root identity changed")
)

// Watcher is a watch New started. Its methods are safe from any
// goroutine.
type Watcher struct {
	mu sync.Mutex
	// batch is the changes waiting for Next, nil when none do: the watch
	// doesn't look again until Next takes them, which taken tells.
	batch   []Event
	waiters []chan result
	stopped bool
	err     error
	taken   chan struct{}
	done    chan struct{}
	ctx     context.Context
	cancel  context.CancelFunc
	// native is the system's watch, which only the goroutine of the
	// watch uses; what it reports goes through raw. What comes while raw
	// is full, as during a burst while the watch looks, waits in spill
	// (spilled tells), up to MaxEntries notices; past that, overflow says
	// some were dropped and the watch looks at everything again.
	native   native
	raw      chan notice
	spillMu  sync.Mutex
	spill    []notice
	spilled  chan struct{}
	overflow chan struct{}
	root     string
	opts     Options
}

type result struct {
	events []Event
	err    error
}

// New watches the file or directory at path, and with opts.Recursive the
// directories in it, but not those that symlinks or Windows junctions
// point to. It returns once everything is watched; what is already there
// is not reported. Canceling ctx stops the watch, as Close does.
//
// A file is watched through its directory, so that a file replaced, as
// editors save, is reported as written, one removed as removed, and one
// that appears again as created. When the watched directory itself (or
// the directory of the watched file) is removed, renamed or replaced, the
// watch reports a Remove of "." and ends: Next returns io.EOF.
//
// New lists the whole tree before it returns, which takes a while for a
// large one: call it from a goroutine, not the main thread (Path does).
func New(ctx context.Context, path string, opts Options) (*Watcher, error) {
	if ctx == nil {
		return nil, errors.New("watch: nil context")
	}
	if path == "" {
		return nil, errors.New("watch: empty path")
	}
	if opts.MaxEntries < 0 {
		return nil, errors.New("watch: negative MaxEntries")
	}
	if opts.MaxEntries == 0 {
		opts.MaxEntries = defaultMaxEntries(opts.backend)
	}
	if opts.Debounce == 0 {
		opts.Debounce = 50 * time.Millisecond
	}
	opts.Debounce = max(opts.Debounce, 0)
	if opts.MaxDelay == 0 {
		opts.MaxDelay = 500 * time.Millisecond
	}
	opts.MaxDelay = max(opts.MaxDelay, opts.Debounce)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	life, cancel := context.WithCancel(ctx)
	w := &Watcher{
		ctx: life, cancel: cancel, root: root, opts: opts,
		taken: make(chan struct{}, 1), done: make(chan struct{}),
		raw: make(chan notice, 4096), spilled: make(chan struct{}, 1), overflow: make(chan struct{}, 1),
	}
	if w.native, err = newNative(w.notice, opts); err != nil {
		cancel()
		if errors.Is(err, ErrUnsupported) {
			return nil, ErrUnsupported
		}
		return nil, fmt.Errorf("watch: %w", err)
	}
	ready := make(chan error, 1)
	go w.run(ready)
	if err := <-ready; err != nil {
		<-w.done
		return nil, err
	}
	return w, nil
}

// notice is what the system's watch reports to, on its goroutine. When
// raw is full the notice is spilled, and when the spill is full too the
// watch looks at everything again instead, as after any overflow: either
// way no change goes unseen.
func (w *Watcher) notice(n notice) {
	select {
	case <-w.ctx.Done():
		return
	default:
	}
	if n.err != nil && !errors.Is(n.err, errOverflow) {
		// An error stops the watch, so it waits its turn.
		select {
		case w.raw <- n:
		case <-w.ctx.Done():
		}
		return
	}
	select {
	case w.raw <- n:
		return
	default:
	}
	w.spillMu.Lock()
	signal := w.spilled
	if len(w.spill) < w.opts.MaxEntries {
		w.spill = append(w.spill, n)
	} else {
		// More than a look at everything would cost to follow.
		w.spill = nil
		signal = w.overflow
	}
	w.spillMu.Unlock()
	select {
	case signal <- struct{}{}:
	default:
	}
}

// unspill takes the notices spilled.
func (w *Watcher) unspill() []notice {
	w.spillMu.Lock()
	defer w.spillMu.Unlock()
	spill := w.spill
	w.spill = nil
	return spill
}

// Root returns the path the watch watches, absolute and with its
// symlinks resolved (macOS's /tmp is /private/tmp), to which the paths of
// events are relative.
func (w *Watcher) Root() string { return w.root }

// Next waits for the next changes and returns them: a batch of the events
// one look found once changes stopped for Options.Debounce. The watch
// doesn't look again until Next has taken a batch, so nothing is lost
// when Next is called late: what changed meanwhile comes in the next
// batch.
//
// It returns io.EOF once the watch is closed, or ended because its
// directory went away, and the error that stopped it otherwise. Canceling
// ctx cancels the wait, not the watch.
func (w *Watcher) Next(ctx context.Context) ([]Event, error) {
	if ctx == nil {
		return nil, errors.New("watch: nil context")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.mu.Lock()
	if w.batch != nil {
		events := w.batch
		w.batch = nil
		w.mu.Unlock()
		w.take()
		return events, nil
	}
	if w.stopped {
		err := w.err
		if err == nil {
			err = io.EOF
		}
		w.mu.Unlock()
		return nil, err
	}
	waiter := make(chan result, 1)
	w.waiters = append(w.waiters, waiter)
	w.mu.Unlock()
	select {
	case r := <-waiter:
		return r.events, r.err
	case <-ctx.Done():
	}
	w.mu.Lock()
	for i, v := range w.waiters {
		if v == waiter {
			w.waiters = append(w.waiters[:i], w.waiters[i+1:]...)
			w.mu.Unlock()
			return nil, ctx.Err()
		}
	}
	w.mu.Unlock()
	// Answered as the wait was canceled.
	r := <-waiter
	return r.events, r.err
}

// take tells the goroutine of the watch that a batch was taken.
func (w *Watcher) take() {
	select {
	case w.taken <- struct{}{}:
	default:
	}
}

// Close stops the watch and drops the changes Next hasn't returned; Next
// then returns io.EOF. It doesn't wait for the watch to release what it
// holds; Done does. It returns nil.
func (w *Watcher) Close() error {
	w.stop(nil, true)
	return nil
}

// Done is closed once the watch has stopped and released its file
// descriptors, handles and goroutines.
func (w *Watcher) Done() <-chan struct{} { return w.done }

// Err returns the error that stopped the watch: nil while it runs, and
// once it was closed, its context canceled, or its directory went away.
func (w *Watcher) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

func (w *Watcher) stop(err error, discard bool) {
	w.mu.Lock()
	if w.stopped {
		if discard {
			w.batch = nil
		}
		w.mu.Unlock()
		return
	}
	w.stopped = true
	w.err = err
	if discard {
		w.batch = nil
	}
	waiters := w.waiters
	w.waiters = nil
	w.mu.Unlock()
	if err == nil {
		err = io.EOF
	}
	for _, waiter := range waiters {
		waiter <- result{err: err}
	}
	w.cancel()
}

// emit hands events to a waiting Next, or keeps them for the next one.
func (w *Watcher) emit(events []Event) {
	if len(events) == 0 {
		return
	}
	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return
	}
	if len(w.waiters) == 0 {
		w.batch = append(w.batch, events...)
		w.mu.Unlock()
		return
	}
	waiter := w.waiters[0]
	w.waiters = w.waiters[1:]
	w.mu.Unlock()
	waiter <- result{events: events}
}

// waiting reports whether a batch waits for Next.
func (w *Watcher) waiting() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.batch != nil
}

// run is the goroutine of the watch: it enrolls the tree, then waits for
// what the system reports and, once nothing more came for opts.Debounce
// (or opts.MaxDelay passed) and Next took the last batch, looks again.
func (w *Watcher) run(ready chan error) {
	var terminal error
	discard := true
	s := newScan(w)
	defer func() {
		w.stop(terminal, discard)
		s.close()
		w.native.close()
		close(w.done)
	}()
	events, err := s.start()
	if err != nil {
		terminal = err
		ready <- err
		return
	}
	w.emit(events)
	ready <- nil
	var timer *time.Timer
	var timerC <-chan time.Time
	var first time.Time
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	schedule := func() {
		if timerC == nil {
			first = time.Now()
		}
		delay := min(w.opts.Debounce, time.Until(first.Add(w.opts.MaxDelay)))
		if timer == nil {
			timer = time.NewTimer(delay)
		} else {
			timer.Reset(delay)
		}
		timerC = timer.C
	}
	// due says changes settled while a batch waited for Next.
	due := false
	// look looks at what changed and reports it; false ends the watch.
	look := func() bool {
		due = false
		events, err := s.update()
		if err == nil {
			w.emit(events)
			return true
		}
		if w.ctx.Err() != nil {
			return false
		}
		if s.gone() {
			// The directory went away: say so, then end as closed.
			w.emit(s.goneEvents())
			discard = false
			return false
		}
		terminal = err
		return false
	}
	mark := func(n notice) bool {
		if err := s.mark(n); err != nil {
			terminal = err
			return false
		}
		return true
	}
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-w.overflow:
			s.lookAll()
			schedule()
		case <-w.spilled:
			for _, n := range w.unspill() {
				if !mark(n) {
					return
				}
			}
			if s.pending() {
				schedule()
			}
		case n := <-w.raw:
			if !mark(n) {
				return
			}
			if s.pending() {
				schedule()
			}
		case <-timerC:
			timerC = nil
			if w.waiting() {
				due = true
			} else if !look() {
				return
			}
		case <-w.taken:
			if due && timerC == nil && !look() {
				return
			}
		}
	}
}
