package document

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/egoist/asuka/ui"
)

// A document's reads and writes run one at a time, in the order they're
// asked for: a job starts on the main thread (where it takes what it
// writes), its work runs on a goroutine, and what it found comes back to
// the main thread before the next job starts.

// job starts on the main thread and returns the work to do off it (nil:
// nothing to do), which returns what to do on the main thread then.
type job func() (work func() (done func()))

// What the package does that tests replace: the clock, its timers,
// goroutines and the main thread's queue.
var (
	now       = time.Now
	afterFunc = func(d time.Duration, f func()) (stop func()) {
		t := time.AfterFunc(d, func() { ui.Post(f) })
		return func() { t.Stop() }
	}
	goIO = func(f func()) { go f() }
	post = ui.Post
)

// do queues j.
func (b *base) do(j job) {
	b.queue = append(b.queue, j)
	if !b.busy {
		b.next()
	}
}

func (b *base) next() {
	for len(b.queue) > 0 {
		j := b.queue[0]
		b.queue = b.queue[1:]
		work := j()
		if work == nil {
			continue
		}
		b.busy = true
		goIO(func() {
			done := work()
			post(func() {
				b.busy = false
				if done != nil {
					done()
				}
				b.next()
			})
		})
		return
	}
}

// writeOp is why a document is written.
type writeOp int

const (
	writeSave      writeOp = iota // to its file, unless another app changed it
	writeAutosave                 // as writeSave, without asking anything
	writeOverwrite                // to its file, whatever is there
	writeSaveAs                   // to another file, which the document becomes
)

// write writes the document to target, then calls done (if set) with what
// failed: ErrChanged when another app changed its file (the document is
// then in conflict).
func (b *base) write(target string, op writeOp, done func(error)) {
	finish := func(err error) {
		if done != nil {
			done(err)
		}
	}
	b.do(func() func() func() {
		if b.closed && op == writeAutosave && !b.k.quitting {
			finish(nil)
			return nil
		}
		if op == writeAutosave && !b.autosaves() {
			finish(errors.New("document: not saved in place"))
			return nil
		}
		snap := b.ops.capture()
		known, deleted := b.stamp, b.deleted
		return func() func() {
			if op == writeSave || op == writeAutosave {
				cur, exists, err := statStamp(target)
				if err != nil {
					return func() { finish(err) }
				}
				if exists && (!cur.same(known) || deleted) {
					return func() {
						b.conflict, b.deleted = true, false
						b.schedule()
						b.notify()
						finish(ErrChanged)
					}
				}
			}
			stamp, err := writeFileSafely(target, snap.write)
			if err != nil {
				return func() { finish(err) }
			}
			return func() {
				moved := target != b.path
				b.ops.markSaved(snap)
				b.savedAt, b.stamp = snap.at, stamp
				b.conflict, b.deleted = false, false
				if moved {
					b.path, b.readOnly = target, false
				}
				b.dirtySince = time.Time{}
				if b.edited {
					b.dirtySince = now() // edited while it was written
				}
				b.schedule()
				if !b.edited && b.backedUp != 0 {
					b.dropCopy()
				}
				if moved {
					b.k.noteRecent(target)
					b.k.rewatch(b)
				}
				b.notify()
				finish(nil)
			}
		}
	})
}

// reload reads the document's file again and makes it the content, then
// calls done (if set) with what failed.
func (b *base) reload(done func(error)) {
	b.do(func() func() func() {
		path := b.path
		if path == "" {
			if done != nil {
				done(errors.New("document: an untitled document has no file"))
			}
			return nil
		}
		read := b.ops.read
		return func() func() {
			v, stamp, err := readFile(path, read)
			writable := fileWritable(path)
			return func() {
				if err == nil && b.path == path {
					b.ops.replace(v)
					b.stamp = stamp
					b.conflict, b.deleted = false, false
					b.readOnly = !writable
					b.dirtySince = time.Time{}
					b.schedule()
					if b.backedUp != 0 {
						b.dropCopy()
					}
					b.notify()
				}
				if done != nil {
					done(err)
				}
			}
		}
	})
}

// backup writes the recovery copy of the document's unsaved changes, if
// they changed since the last, then calls done (if set).
func (b *base) backup(done func()) {
	b.do(func() func() func() {
		if !b.edited || b.changes == b.backedUp {
			if done != nil {
				done()
			}
			return nil
		}
		snap := b.ops.capture()
		e := recoveryEntry{ID: b.id, Path: b.path, Stamp: b.stamp}
		if b.path == "" {
			e.Name = b.name
		}
		store := b.k.recovery
		return func() func() {
			err := store.save(e, snap.write)
			return func() {
				if err == nil {
					b.backedUp = snap.at
					b.dirtySince = time.Time{}
					if b.changes != snap.at {
						b.dirtySince = now() // edited while it was written
						b.schedule()
					}
				} else {
					logf("cannot write the recovery copy of %s: %v", e.Name+e.Path, err)
				}
				if done != nil {
					done()
				}
			}
		}
	})
}

// dropCopy removes the document's recovery copy.
func (b *base) dropCopy() {
	b.backedUp = 0
	id, store := b.id, b.k.recovery
	b.do(func() func() func() {
		return func() func() {
			store.remove(id)
			return nil
		}
	})
}

// checkDisk compares the document's file with what it last read or
// wrote: an unedited document reads its file again, an edited one is in
// conflict, a file another app moved is followed, and a deleted one
// leaves the document Deleted (saving writes it again).
func (b *base) checkDisk() {
	b.do(func() func() func() {
		path, known := b.path, b.stamp
		if path == "" || b.closed {
			return nil
		}
		wasDeleted, conflict := b.deleted, b.conflict
		return func() func() {
			cur, exists, err := statStamp(path)
			if err == nil && !exists && !wasDeleted {
				// Network shares report files gone that are not (VS Code),
				// and a save that replaces a file has it gone a moment.
				time.Sleep(100 * time.Millisecond)
				cur, exists, err = statStamp(path)
			}
			if err != nil {
				return nil
			}
			moved := ""
			if !exists && !wasDeleted && !conflict && known.ID != "" {
				moved = movedTo(filepath.Dir(path), known.ID)
			}
			writable := exists && fileWritable(path)
			return func() {
				if b.path != path {
					return // moved meanwhile
				}
				switch {
				case !exists && moved != "":
					b.path = moved
					b.k.noteRecent(moved)
					b.k.rewatch(b)
					b.notify()
				case !exists:
					if !b.deleted {
						b.deleted = true
						b.schedule()
						b.notify()
					}
				case cur.same(known) && !b.deleted:
					if b.readOnly != !writable {
						b.readOnly = !writable
						b.schedule()
						b.notify()
					}
				case !b.edited:
					b.reload(nil)
				default:
					if !b.conflict || b.deleted {
						b.conflict, b.deleted = true, false
						b.schedule()
						b.notify()
					}
				}
			}
		}
	})
}

// relocate moves the document's file to target, which the document then
// is (Rename, Move To), then calls done with what failed.
func (b *base) relocate(target string, done func(error)) {
	b.do(func() func() func() {
		path := b.path
		return func() func() {
			err := os.Rename(path, target)
			if errors.Is(err, syscall.EXDEV) {
				if err = copyFile(path, target); err == nil {
					err = os.Remove(path)
				}
			}
			stamp, _, _ := statStamp(target)
			writable := fileWritable(target)
			return func() {
				if err == nil {
					b.path, b.stamp, b.readOnly = target, stamp, !writable
					b.k.noteRecent(target)
					b.k.rewatch(b)
					b.notify()
				}
				done(err)
			}
		}
	})
}

// isPermission reports whether err is the system refusing a write.
func isPermission(err error) bool { return errors.Is(err, fs.ErrPermission) }
