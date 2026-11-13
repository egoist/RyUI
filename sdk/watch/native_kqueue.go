//go:build darwin

package watch

import (
	"os"
	"sync"
	"syscall"
)

// iOS watches with kqueue: EVFILT_VNODE on a descriptor of each directory
// and file watched, opened for events only (O_EVTONLY, which doesn't keep
// a volume from unmounting). A directory says when its entries change
// (NOTE_WRITE), a file when it is written (NOTE_WRITE, NOTE_EXTEND), and
// either when it is deleted, renamed or revoked; the scan looks and
// compares. FSEvents is not on iOS. macOS uses it too when asked (the
// tests run every case through both).
//
// A wake is an EVFILT_USER event, triggered from any goroutine.

// kqueueFlags are the vnode events a target asks for.
const kqueueFlags = syscall.NOTE_DELETE | syscall.NOTE_WRITE | syscall.NOTE_EXTEND | syscall.NOTE_ATTRIB | syscall.NOTE_LINK | syscall.NOTE_RENAME | syscall.NOTE_REVOKE

type kqueueTarget struct {
	id  uint64
	fd  int
	dir bool
}

type kqueueWatch struct {
	kq int
	// mu guards closed, for wake, which may run as close closes kq.
	mu     sync.Mutex
	closed bool
	// targets by ID and by descriptor; only the driver's goroutine uses
	// them.
	targets map[uint64]*kqueueTarget
	fds     map[int]*kqueueTarget
	events  [64]syscall.Kevent_t
}

func newKqueue(notify func(notice)) (native, error) {
	kq, err := syscall.Kqueue()
	if err != nil {
		return nil, os.NewSyscallError("kqueue", err)
	}
	syscall.CloseOnExec(kq)
	wake := []syscall.Kevent_t{{Ident: 0, Filter: syscall.EVFILT_USER, Flags: syscall.EV_ADD | syscall.EV_CLEAR}}
	if _, err := syscall.Kevent(kq, wake, nil, nil); err != nil {
		syscall.Close(kq)
		return nil, os.NewSyscallError("kevent", err)
	}
	d := &kqueueWatch{kq: kq, targets: map[uint64]*kqueueTarget{}, fds: map[int]*kqueueTarget{}}
	return runDriver(d, notify), nil
}

func (d *kqueueWatch) add(t target) error {
	if t.subtree {
		return ErrUnsupported
	}
	flags := syscall.O_EVTONLY | syscall.O_CLOEXEC | syscall.O_NOFOLLOW
	if t.dir {
		flags |= syscall.O_DIRECTORY
	}
	fd, err := syscall.Open(t.path, flags, 0)
	for err == syscall.EINTR {
		fd, err = syscall.Open(t.path, flags, 0)
	}
	if err != nil {
		return &os.PathError{Op: "open", Path: t.path, Err: err}
	}
	// The descriptor must be what the scan saw, not a file that replaced
	// it since.
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		syscall.Close(fd)
		return &os.PathError{Op: "fstat", Path: t.path, Err: err}
	}
	want, ok := identityOf(t.info)
	if !ok || want.volume != uint64(st.Dev) || want.index != uint64(st.Ino) {
		syscall.Close(fd)
		return errRootChanged
	}
	change := []syscall.Kevent_t{{Ident: uint64(fd), Filter: syscall.EVFILT_VNODE, Flags: syscall.EV_ADD | syscall.EV_CLEAR | syscall.EV_ENABLE, Fflags: kqueueFlags}}
	if _, err := syscall.Kevent(d.kq, change, nil, nil); err != nil {
		syscall.Close(fd)
		return os.NewSyscallError("kevent", err)
	}
	if old := d.targets[t.id]; old != nil {
		d.drop(old)
	}
	k := &kqueueTarget{id: t.id, fd: fd, dir: t.dir}
	d.targets[t.id] = k
	d.fds[fd] = k
	return nil
}

// drop closes k's descriptor, which removes its event from the queue.
func (d *kqueueWatch) drop(k *kqueueTarget) {
	delete(d.targets, k.id)
	delete(d.fds, k.fd)
	syscall.Close(k.fd)
}

func (d *kqueueWatch) remove(id uint64) {
	if k := d.targets[id]; k != nil {
		d.drop(k)
	}
}

func (d *kqueueWatch) wait() ([]notice, error) {
	n, err := syscall.Kevent(d.kq, nil, d.events[:], nil)
	if err == syscall.EINTR {
		return nil, nil
	}
	if err != nil {
		return nil, os.NewSyscallError("kevent", err)
	}
	var notices []notice
	for _, ev := range d.events[:n] {
		if ev.Filter != syscall.EVFILT_VNODE {
			continue // a wake
		}
		k := d.fds[int(ev.Ident)]
		if k == nil {
			continue // removed since
		}
		if ev.Flags&syscall.EV_ERROR != 0 {
			notices = append(notices, notice{target: k.id, kind: kindLost})
			continue
		}
		notices = append(notices, kqueueNotice(k, ev.Fflags))
	}
	return notices, nil
}

// kqueueNotice translates what kqueue says of a target into a notice.
func kqueueNotice(k *kqueueTarget, flags uint32) notice {
	n := notice{target: k.id}
	switch {
	case flags&(syscall.NOTE_DELETE|syscall.NOTE_RENAME|syscall.NOTE_REVOKE) != 0:
		// Gone from its path, or moved: the scan looks whether something
		// is there now, and enrolls it again.
		n.kind = kindLost
	case !k.dir && flags&(syscall.NOTE_WRITE|syscall.NOTE_EXTEND) != 0:
		n.kind = kindWrite
	}
	// A directory written, linked or with new attributes is dirty: the
	// scan lists it.
	return n
}

func (d *kqueueWatch) wake() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.closed {
		trigger := []syscall.Kevent_t{{Ident: 0, Filter: syscall.EVFILT_USER, Fflags: syscall.NOTE_TRIGGER}}
		syscall.Kevent(d.kq, trigger, nil, nil)
	}
}

func (d *kqueueWatch) close() {
	for _, k := range d.targets {
		d.drop(k)
	}
	d.mu.Lock()
	d.closed = true
	syscall.Close(d.kq)
	d.mu.Unlock()
}
