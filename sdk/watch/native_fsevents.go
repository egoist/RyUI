//go:build darwin && !ios

package watch

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// macOS watches with FSEvents: one stream per directory target, covering
// the whole tree beneath it, so a watch costs no descriptor per file and a
// large folder is fine. With kFSEventStreamCreateFlagFileEvents the stream
// says which file changed rather than only which directory, but its
// notices stay hints: the scan compares, and a notice that changes were
// dropped (MustScanSubDirs, a user or kernel drop) makes it look at
// everything again, as an overflow does elsewhere.
//
// FSEvents is called through purego, without cgo: FSEventStreamCreate,
// a callback made once for the process (purego.NewCallback), and a serial
// dispatch queue (FSEventStreamSetDispatchQueue) every stream delivers on,
// off the main thread, so that stopping a stream can wait for a callback
// already running with dispatch_sync_f.

// FSEventStreamCreateFlags.
const (
	fseNoDefer    = 0x00000002 // kFSEventStreamCreateFlagNoDefer
	fseWatchRoot  = 0x00000004 // kFSEventStreamCreateFlagWatchRoot
	fseFileEvents = 0x00000010 // kFSEventStreamCreateFlagFileEvents
)

// FSEventStreamEventFlags.
const (
	fseMustScanSubDirs = 0x00000001
	fseUserDropped     = 0x00000002
	fseKernelDropped   = 0x00000004
	fseEventIdsWrapped = 0x00000008
	fseHistoryDone     = 0x00000010
	fseRootChanged     = 0x00000020
	fseMount           = 0x00000040
	fseUnmount         = 0x00000080
	fseItemCreated     = 0x00000100
	fseItemRemoved     = 0x00000200
	fseItemInodeMeta   = 0x00000400
	fseItemRenamed     = 0x00000800
	fseItemModified    = 0x00001000
	fseItemFinderInfo  = 0x00002000
	fseItemChangeOwner = 0x00004000
	fseItemXattr       = 0x00008000
	fseItemIsFile      = 0x00010000
	fseItemIsDir       = 0x00020000
	fseItemIsSymlink   = 0x00040000
)

const (
	// fseLatency is how long FSEvents may coalesce events: 10 ms, well
	// under the watch's 50 ms Debounce, so the watch's trailing debounce
	// does the coalescing an app sees and the stream only batches bursts.
	// With fseNoDefer the first event after a quiet period is delivered
	// at once rather than after the latency.
	fseLatency = 0.01
	// fseQueueSize is how many notices a watch holds before it asks the
	// scan to look at everything again.
	fseQueueSize = 4096
	// fseUTF8 is kCFStringEncodingUTF8.
	fseUTF8 = 0x08000100
)

// The FSEvents API, bound once.
var (
	fseStreamCreate     func(alloc, callback uintptr, context *fseContext, paths uintptr, sinceWhen uint64, latency float64, flags uint32) uintptr
	fseStreamStart      func(stream uintptr) uint8
	fseStreamStop       func(stream uintptr)
	fseStreamInvalidate func(stream uintptr)
	fseStreamRelease    func(stream uintptr)
	fseStreamSetQueue   func(stream, queue uintptr)

	cfStringCreateWithCString func(alloc uintptr, s *byte, encoding uint32) uintptr
	cfArrayCreate             func(alloc uintptr, values *uintptr, n int, callbacks uintptr) uintptr
	cfRelease                 func(ref uintptr)
	kCFTypeArrayCallBacks     uintptr

	dispatchQueueCreate func(label *byte, attr uintptr) uintptr
	dispatchSyncF       func(queue, context, work uintptr)
	libcRealpath        func(path, resolved *byte) uintptr

	// fseCallback is the process's one FSEventStreamCallback, and fseFlush
	// what dispatch_sync_f runs to wait for the callbacks already queued:
	// made with the first watch and never freed (purego's callbacks are
	// a fixed number per process).
	fseCallback, fseFlush uintptr

	// fseQueue is the one serial queue every stream delivers on.
	fseQueue uintptr
	fseOnce  sync.Once
	fseErr   error
)

// loadFSEvents binds FSEvents, CoreFoundation's string and array
// constructors and libdispatch, once, whoever asks first.
func loadFSEvents() error {
	fseOnce.Do(func() {
		const flags = purego.RTLD_GLOBAL | purego.RTLD_NOW
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", flags)
		if err != nil {
			fseErr = err
			return
		}
		lib, err := purego.Dlopen("/System/Library/Frameworks/CoreServices.framework/CoreServices", flags)
		if err != nil {
			fseErr = err
			return
		}
		// CoreServices re-exports FSEvents; the subframework, when it is
		// there, pins the symbols to it.
		if h, err := purego.Dlopen("/System/Library/Frameworks/CoreServices.framework/Frameworks/FSEvents.framework/FSEvents", flags); err == nil {
			lib = h
		}
		if kCFTypeArrayCallBacks, err = purego.Dlsym(cf, "kCFTypeArrayCallBacks"); err != nil {
			fseErr = err
			return
		}
		purego.RegisterLibFunc(&fseStreamCreate, lib, "FSEventStreamCreate")
		purego.RegisterLibFunc(&fseStreamStart, lib, "FSEventStreamStart")
		purego.RegisterLibFunc(&fseStreamStop, lib, "FSEventStreamStop")
		purego.RegisterLibFunc(&fseStreamInvalidate, lib, "FSEventStreamInvalidate")
		purego.RegisterLibFunc(&fseStreamRelease, lib, "FSEventStreamRelease")
		purego.RegisterLibFunc(&fseStreamSetQueue, lib, "FSEventStreamSetDispatchQueue")
		purego.RegisterLibFunc(&cfStringCreateWithCString, cf, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&cfArrayCreate, cf, "CFArrayCreate")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
		purego.RegisterLibFunc(&dispatchQueueCreate, purego.RTLD_DEFAULT, "dispatch_queue_create")
		purego.RegisterLibFunc(&dispatchSyncF, purego.RTLD_DEFAULT, "dispatch_sync_f")
		// The UNIX03 realpath on Intel; Apple silicon has only one.
		realpath := "realpath$DARWIN_EXTSN"
		if _, err := purego.Dlsym(purego.RTLD_DEFAULT, realpath); err != nil {
			realpath = "realpath"
		}
		purego.RegisterLibFunc(&libcRealpath, purego.RTLD_DEFAULT, realpath)
		fseCallback = purego.NewCallback(fseEvents)
		// The callback never blocks, so waiting for it cannot deadlock.
		fseFlush = purego.NewCallback(func() {})
		label := []byte("asuka.watch\x00")
		if fseQueue = dispatchQueueCreate(&label[0], 0); fseQueue == 0 {
			fseErr = errors.New("cannot create the watch's dispatch queue")
		}
	})
	return fseErr
}

// fseContext mirrors FSEventStreamContext. FSEvents copies it, and hands
// info back to the callback, which is how the one callback finds its
// stream.
type fseContext struct {
	version         uintptr
	info            uintptr
	retain          uintptr
	release         uintptr
	copyDescription uintptr
}

// fseStream is one FSEventStream watching one directory target.
type fseStream struct {
	id      uint64
	path    string
	subtree bool
	driver  *fseWatch
	stream  uintptr
	started bool
	context fseContext
	// key is what the stream's context info carries: a number, not a
	// pointer, which FSEvents hands back to the callback.
	key uintptr
}

// fseStreams routes the one FSEventStreamCallback of the process to the
// stream of the event, by the context info FSEvents hands back.
var fseStreams = struct {
	sync.Mutex
	next uintptr
	m    map[uintptr]*fseStream
}{m: map[uintptr]*fseStream{}}

// fseEvents is the FSEventStreamCallback. Without
// kFSEventStreamCreateFlagUseCFTypes the paths are a C array of C
// strings.
func fseEvents(stream, info, numEvents, paths, flags, ids uintptr) {
	fseStreams.Lock()
	s := fseStreams.m[info]
	fseStreams.Unlock()
	if s == nil {
		return
	}
	eventPaths, eventFlags := ptrOf(paths), ptrOf(flags)
	for i := uintptr(0); i < numEvents; i++ {
		p := *(*uintptr)(unsafe.Add(eventPaths, i*unsafe.Sizeof(uintptr(0))))
		if p == 0 {
			continue
		}
		if n, ok := s.event(cString(ptrOf(p)), *(*uint32)(unsafe.Add(eventFlags, i*4))); ok {
			s.deliver(n)
		}
	}
}

// ptrOf returns the pointer at the native address p, memory FSEvents
// owns, not Go.
func ptrOf(p uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&p)) }

// cString copies the NUL-terminated string at p.
func cString(p unsafe.Pointer) string {
	n := 0
	for *(*byte)(unsafe.Add(p, n)) != 0 {
		n++
	}
	return string(unsafe.Slice((*byte)(p), n))
}

// event translates one FSEvents event of the stream into a notice: pure,
// so the mapping can be tested without the framework.
func (s *fseStream) event(path string, flags uint32) (notice, bool) {
	prefix := s.path
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	rel, ok := strings.CutPrefix(path, prefix)
	if !ok {
		if path != s.path {
			return notice{}, false
		}
		rel = ""
	}
	if flags&(fseMustScanSubDirs|fseUserDropped|fseKernelDropped|fseEventIdsWrapped) != 0 {
		// Changes were dropped: the scan lists everything again.
		return notice{target: s.id, err: errOverflow}, true
	}
	if !s.subtree && strings.Contains(rel, "/") {
		// FSEvents is recursive, but a target without subtree is about its
		// directory and the entries in it, which a change deeper down
		// doesn't change.
		return notice{}, false
	}
	n := notice{target: s.id, name: rel}
	switch {
	case flags&fseHistoryDone != 0:
		return notice{}, false
	case flags&fseRootChanged != 0:
		// The watched directory was moved, renamed or replaced: the scan
		// stats it and ends the watch when it is gone.
		return n, true
	case flags&fseUnmount != 0:
		n.kind = kindLost
	case flags&fseMount != 0:
		// A volume appeared in the tree.
	case flags&(fseItemRemoved|fseItemRenamed|fseItemCreated) != 0:
		// Dirty: the scan looks. A write on APFS sets ItemCreated too (an
		// O_TRUNC write reports the file as created), and a metadata
		// change can carry ItemModified, so the structural flags must win:
		// only a plain content write is a write hint, and the scan's
		// comparison reports the writes that come dirty.
	case flags&fseItemModified != 0 && flags&fseItemIsFile != 0:
		n.kind = kindWrite
	}
	return n, true
}

// deliver hands a notice to wait, or, with the queue full, asks the scan
// to look at everything again. It never blocks: it runs on the dispatch
// queue, which a stream's teardown waits on.
func (s *fseStream) deliver(n notice) {
	d := s.driver
	d.mu.Lock()
	if len(d.notices) < fseQueueSize {
		d.notices = append(d.notices, n)
	} else {
		d.overflow = true
	}
	d.mu.Unlock()
	d.wake()
}

// stop stops and releases the stream. Only the driver's goroutine calls
// it, but a callback may be running: dispatch_sync_f waits for the queue,
// after which nothing touches s.
func (s *fseStream) stop() {
	if s.stream == 0 {
		return
	}
	if s.started {
		fseStreamStop(s.stream)
		s.started = false
	}
	fseStreamInvalidate(s.stream)
	dispatchSyncF(fseQueue, 0, fseFlush)
	fseStreams.Lock()
	delete(fseStreams.m, s.key)
	fseStreams.Unlock()
	fseStreamRelease(s.stream)
	s.stream = 0
}

// fseWatch is the FSEvents driver: one stream per directory target, all
// delivering on fseQueue.
type fseWatch struct {
	// notices are filled by the callback, on the dispatch queue, and taken
	// by wait on the driver's goroutine; overflow says there were too
	// many, so the scan must look at everything.
	mu       sync.Mutex
	notices  []notice
	overflow bool
	// signal interrupts wait, buffered so wake never waits.
	signal chan struct{}
	// streams are the running streams by target ID; only the driver's
	// goroutine (add, remove, close) uses it.
	streams map[uint64]*fseStream
}

func newFSEvents(notify func(notice)) (native, error) {
	if err := loadFSEvents(); err != nil {
		return nil, fmt.Errorf("FSEvents: %w", err)
	}
	d := &fseWatch{signal: make(chan struct{}, 1), streams: map[uint64]*fseStream{}}
	return runDriver(d, notify), nil
}

// canonicalPath resolves path with realpath, which gives the names as
// they are on disk (FSEvents reports them so, which a path given on a
// case-insensitive volume may not be), or returns it as it is when it
// can't.
func canonicalPath(path string) string {
	in := append([]byte(path), 0)
	var out [1024]byte // PATH_MAX
	if libcRealpath(&in[0], &out[0]) == 0 {
		return path
	}
	for i, c := range out {
		if c == 0 {
			return string(out[:i])
		}
	}
	return path
}

// covered reports whether a whole tree above path is already watched: a
// target the scan enrolls below it needs no stream of its own.
func (d *fseWatch) covered(path string) bool {
	for _, s := range d.streams {
		prefix := s.path
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		if path == s.path || strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func (d *fseWatch) add(t target) error {
	if !t.dir {
		// A file is covered by the stream of the directory that holds it,
		// which the scan enrolls for a file watch, and FSEvents watches
		// trees, not single files.
		return ErrUnsupported
	}
	streamPath := canonicalPath(t.path)
	if d.covered(streamPath) {
		// A stream above t already watches its whole tree: FSEvents is
		// recursive, so a target beneath it needs no stream of its own.
		return nil
	}
	info, err := os.Lstat(streamPath)
	if err != nil {
		return err
	}
	if !os.SameFile(info, t.info) {
		return errRootChanged
	}
	path := append([]byte(streamPath), 0)
	cfPath := cfStringCreateWithCString(0, &path[0], fseUTF8)
	runtime.KeepAlive(path)
	if cfPath == 0 {
		return errors.New("cannot make the watch's path a CFString")
	}
	values := []uintptr{cfPath}
	cfPaths := cfArrayCreate(0, &values[0], 1, kCFTypeArrayCallBacks)
	runtime.KeepAlive(values)
	if cfPaths == 0 {
		cfRelease(cfPath)
		return errors.New("cannot make the watch's paths a CFArray")
	}
	s := &fseStream{id: t.id, path: streamPath, subtree: t.subtree, driver: d}
	fseStreams.Lock()
	fseStreams.next++
	s.key = fseStreams.next
	fseStreams.m[s.key] = s
	fseStreams.Unlock()
	s.context.info = s.key
	// kFSEventStreamEventIdSinceNow: the scan has looked already.
	stream := fseStreamCreate(0, fseCallback, &s.context, cfPaths, ^uint64(0), fseLatency, fseNoDefer|fseFileEvents|fseWatchRoot)
	cfRelease(cfPaths)
	cfRelease(cfPath)
	if stream == 0 {
		fseStreams.Lock()
		delete(fseStreams.m, s.key)
		fseStreams.Unlock()
		return errors.New("cannot create an FSEventStream")
	}
	s.stream = stream
	fseStreamSetQueue(stream, fseQueue)
	if fseStreamStart(stream) == 0 {
		s.stop()
		return errors.New("cannot start an FSEventStream")
	}
	s.started = true
	d.streams[t.id] = s
	return nil
}

func (d *fseWatch) remove(id uint64) {
	if s := d.streams[id]; s != nil {
		s.stop()
		delete(d.streams, id)
	}
}

// wait returns the notices that came in, or, after an overflow, one
// notice that makes the scan look at everything again. wake makes it
// return, so that the command wake announced is taken.
func (d *fseWatch) wait() ([]notice, error) {
	<-d.signal
	d.mu.Lock()
	defer d.mu.Unlock()
	notices := d.notices
	d.notices = nil
	if d.overflow {
		// The notices held are covered by the scan's look at everything.
		d.overflow = false
		return []notice{{err: errOverflow}}, nil
	}
	return notices, nil
}

func (d *fseWatch) wake() {
	select {
	case d.signal <- struct{}{}:
	default:
	}
}

func (d *fseWatch) close() {
	for id, s := range d.streams {
		s.stop()
		delete(d.streams, id)
	}
}
