//go:build linux

package watch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"sync"
	"syscall"
)

// inotifyUser is a target of a watch sharing an inotify watch descriptor.
type inotifyUser struct {
	watch *inotifyWatch
	id    uint64
}

// inotifyGeneration is the targets sharing an inotify watch descriptor:
// inotify returns the same descriptor for the same directory, of any
// watch, and a descriptor removed (retired) can still have events queued
// before its IN_IGNORED, while a new watch of the directory reuses it.
type inotifyGeneration struct {
	users   []inotifyUser
	retired bool
}

// inotifyInstance is the inotify instance of the process, which every
// watch shares, so that watches cost watch descriptors
// (fs.inotify.max_user_watches), not instances (max_user_instances, 128
// on many systems for all the apps of a user), as fsnotify does. A
// goroutine reads it through the runtime's poller and hands each watch
// its notices.
type inotifyInstance struct {
	fd          int
	file        *os.File
	users       int
	generations map[int][]*inotifyGeneration
}

// inotifyShared guards the instance, which lives while watches use it.
var inotifyShared struct {
	sync.Mutex
	instance *inotifyInstance
}

// inotifyWatch is a watch's driver: its targets are directories of the
// shared instance, whose goroutine queues the notices that Wait returns.
// Files are watched through their directory, which names them.
type inotifyWatch struct {
	instance *inotifyInstance
	targets  map[uint64]int // by ID, their descriptors; guarded by inotifyShared
	mu       sync.Mutex
	queue    []notice
	err      error
	signal   chan struct{}
}

func newInotify(notify func(notice)) (native, error) {
	inotifyShared.Lock()
	defer inotifyShared.Unlock()
	in := inotifyShared.instance
	if in == nil {
		fd, err := syscall.InotifyInit1(syscall.IN_NONBLOCK | syscall.IN_CLOEXEC)
		if err != nil {
			return nil, err
		}
		// Non-blocking, so that the runtime's poller waits for it.
		in = &inotifyInstance{fd: fd, file: os.NewFile(uintptr(fd), "inotify"), generations: make(map[int][]*inotifyGeneration)}
		inotifyShared.instance = in
		go in.read(in.file)
	}
	in.users++
	d := &inotifyWatch{instance: in, targets: make(map[uint64]int), signal: make(chan struct{}, 1)}
	return runDriver(d, notify), nil
}

func (d *inotifyWatch) add(t target) error {
	if t.subtree {
		return ErrUnsupported
	}
	if !t.dir {
		// The watch of its directory names what changes in a file.
		return ErrUnsupported
	}
	fd, err := syscall.Open(t.path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), t.path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, t.info) {
		return errRootChanged
	}
	inotifyShared.Lock()
	defer inotifyShared.Unlock()
	in := d.instance
	if in.file == nil {
		return errors.New("inotify instance closed")
	}
	mask := uint32(syscall.IN_CREATE | syscall.IN_MODIFY | syscall.IN_CLOSE_WRITE | syscall.IN_ATTRIB | syscall.IN_DELETE | syscall.IN_MOVED_FROM | syscall.IN_MOVED_TO | syscall.IN_DELETE_SELF | syscall.IN_MOVE_SELF | syscall.IN_UNMOUNT | syscall.IN_ONLYDIR | syscall.IN_DONT_FOLLOW)
	wd, err := syscall.InotifyAddWatch(in.fd, t.path, mask)
	if err != nil {
		return err
	}
	gens := in.generations[wd]
	info, err = os.Lstat(t.path)
	if err != nil || !os.SameFile(info, t.info) {
		// Only a descriptor no target uses is removed.
		if len(gens) == 0 || gens[len(gens)-1].retired {
			syscall.InotifyRmWatch(in.fd, uint32(wd))
		}
		if err != nil {
			return err
		}
		return errRootChanged
	}
	if len(gens) == 0 || gens[len(gens)-1].retired {
		gens = append(gens, &inotifyGeneration{})
	}
	g := gens[len(gens)-1]
	g.users = append(g.users, inotifyUser{d, t.id})
	in.generations[wd] = gens
	d.targets[t.id] = wd
	return nil
}

func (d *inotifyWatch) remove(id uint64) {
	inotifyShared.Lock()
	defer inotifyShared.Unlock()
	d.drop(id)
}

// drop removes target id, under inotifyShared.
func (d *inotifyWatch) drop(id uint64) {
	wd, ok := d.targets[id]
	if !ok {
		return
	}
	delete(d.targets, id)
	in := d.instance
	for _, g := range in.generations[wd] {
		for i, u := range g.users {
			if u == (inotifyUser{d, id}) {
				g.users = append(g.users[:i], g.users[i+1:]...)
				if len(g.users) == 0 && !g.retired {
					g.retired = true
					if in.file != nil {
						syscall.InotifyRmWatch(in.fd, uint32(wd))
					}
				}
				return
			}
		}
	}
}

type inotifyRecord struct {
	wd           int
	mask, cookie uint32
	name         string
}

func parseInotify(buf []byte) ([]inotifyRecord, error) {
	var records []inotifyRecord
	for len(buf) > 0 {
		if len(buf) < 16 {
			return nil, errors.New("truncated inotify record")
		}
		length := uint64(binary.NativeEndian.Uint32(buf[12:16]))
		if length > uint64(len(buf)-16) {
			return nil, errors.New("oversized inotify name")
		}
		name := ""
		if length > 0 {
			data := buf[16 : 16+int(length)]
			end := bytes.IndexByte(data, 0)
			if end < 0 {
				return nil, errors.New("unterminated inotify name")
			}
			name = string(data[:end])
		}
		records = append(records, inotifyRecord{int(int32(binary.NativeEndian.Uint32(buf))), binary.NativeEndian.Uint32(buf[4:8]), binary.NativeEndian.Uint32(buf[8:12]), name})
		buf = buf[16+int(length):]
	}
	return records, nil
}

// read reads the instance's file until it is closed, and queues what it
// says for the watches.
func (in *inotifyInstance) read(file *os.File) {
	var buf [65536]byte
	for {
		size, err := file.Read(buf[:])
		if err == nil && size == 0 {
			err = errors.New("inotify stream ended")
		}
		var records []inotifyRecord
		if err == nil {
			records, err = parseInotify(buf[:size])
		}
		inotifyShared.Lock()
		if err != nil {
			if in.file != nil {
				// Lost: every watch using it ends, and new ones get another.
				for w := range in.watches() {
					w.post(nil, err)
				}
				in.close()
			}
			inotifyShared.Unlock()
			return
		}
		notices := map[*inotifyWatch][]notice{}
		for _, r := range records {
			in.dispatch(r, notices)
		}
		inotifyShared.Unlock()
		for w, n := range notices {
			w.post(n, nil)
		}
	}
}

// watches returns the watches with targets in the instance.
func (in *inotifyInstance) watches() map[*inotifyWatch]bool {
	all := map[*inotifyWatch]bool{}
	for _, gens := range in.generations {
		for _, g := range gens {
			for _, u := range g.users {
				all[u.watch] = true
			}
		}
	}
	return all
}

// dispatch adds what r says to the notices of the watches it concerns,
// under inotifyShared.
func (in *inotifyInstance) dispatch(r inotifyRecord, notices map[*inotifyWatch][]notice) {
	if r.mask&syscall.IN_Q_OVERFLOW != 0 {
		for w := range in.watches() {
			notices[w] = append(notices[w], notice{err: errOverflow})
		}
		return
	}
	gens := in.generations[r.wd]
	if len(gens) == 0 {
		return
	}
	g := gens[0]
	if r.mask&syscall.IN_IGNORED != 0 {
		if len(gens) == 1 {
			delete(in.generations, r.wd)
		} else {
			in.generations[r.wd] = gens[1:]
		}
		if !g.retired {
			// The directory's filesystem was unmounted, or inotify
			// dropped the watch for another reason.
			for _, u := range g.users {
				notices[u.watch] = append(notices[u.watch], notice{target: u.id, kind: kindLost})
				delete(u.watch.targets, u.id)
			}
		}
		return
	}
	if g.retired || len(g.users) == 0 {
		return
	}
	var k kind
	switch {
	case r.mask&syscall.IN_UNMOUNT != 0:
		return // IN_IGNORED follows
	case r.mask&syscall.IN_MOVED_FROM != 0:
		k = kindMoveFrom
	case r.mask&syscall.IN_MOVED_TO != 0:
		k = kindMoveTo
	case r.mask&syscall.IN_CREATE != 0:
		k = kindCreate
	case r.mask&(syscall.IN_DELETE|syscall.IN_DELETE_SELF) != 0:
		k = kindRemove
	case r.mask&(syscall.IN_MODIFY|syscall.IN_CLOSE_WRITE) != 0:
		k = kindWrite
	}
	// IN_ATTRIB is dirty: the core looks, and metadata alone is not a
	// write.
	//
	// A watch that added the directory twice hears of it once, through
	// its latest target.
	seen := map[*inotifyWatch]bool{}
	for i := len(g.users) - 1; i >= 0; i-- {
		u := g.users[i]
		if !seen[u.watch] {
			seen[u.watch] = true
			notices[u.watch] = append(notices[u.watch], notice{target: u.id, kind: k, name: r.name})
		}
	}
	// A deleted directory's automatic IN_IGNORED is expected; retain its
	// removal hint instead of turning it into stream loss.
	if r.mask&syscall.IN_DELETE_SELF != 0 {
		g.retired = true
	}
}

// close closes the instance, under inotifyShared, which ends read.
func (in *inotifyInstance) close() {
	if in.file != nil {
		in.file.Close()
		in.file = nil
	}
	if inotifyShared.instance == in {
		inotifyShared.instance = nil
	}
}

// post queues notices, or the error that ends the watch, for Wait.
func (d *inotifyWatch) post(notices []notice, err error) {
	d.mu.Lock()
	d.queue = append(d.queue, notices...)
	if err != nil && d.err == nil {
		d.err = err
	}
	d.mu.Unlock()
	d.wake()
}

func (d *inotifyWatch) wait() ([]notice, error) {
	d.mu.Lock()
	if len(d.queue) == 0 && d.err == nil {
		d.mu.Unlock()
		<-d.signal
		d.mu.Lock()
	}
	notices, err := d.queue, d.err
	d.queue = nil
	d.mu.Unlock()
	return notices, err
}

func (d *inotifyWatch) wake() {
	select {
	case d.signal <- struct{}{}:
	default:
	}
}

func (d *inotifyWatch) close() {
	inotifyShared.Lock()
	defer inotifyShared.Unlock()
	for id := range d.targets {
		d.drop(id)
	}
	in := d.instance
	if in.users--; in.users == 0 {
		in.close()
	}
}

// newPlatform starts inotify (Linux and Android).
func newPlatform(notify func(notice), opts Options) (native, error) {
	return newInotify(notify)
}
