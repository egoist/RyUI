package document

import (
	"context"
	"io"
	"os"
	"sync"
	"time"

	"github.com/egoist/asuka/watch"
)

// Documents notice what other apps do to their files: a watch of each
// open document's file, and a look whenever its window comes to the front
// (watches miss changes: VS Code and Notepad++ look then too). An
// unedited document reads its file again, an edited one is in conflict
// and asks at its next save, a file another app moved is followed (on a
// Mac anywhere on its volume, elsewhere in its folder), and a deleted one
// leaves the document open, written again by its next save.
//
// The watch is behind fileWatcher: asuka/watch's watch of the file (the
// system's notifications, through its folder, so a file an editor
// replaces stays watched), and where it can't watch (the platform hasn't
// one, the folder went away) a look at the file's stamp, a cheap stat
// every two seconds.

// fileWatcher tells when a file may have changed.
type fileWatcher interface {
	// watch calls changed, on the main thread, when the file at path may
	// have changed (written, replaced, moved, deleted), until stop.
	watch(path string, changed func()) (stop func())
}

// watcher is the package's (tests replace it).
var watcher fileWatcher = systemWatcher{poll: pollWatcher{every: 2 * time.Second}}

// systemWatcher watches a file with asuka/watch, and polls it where that
// can't, or once its watch ended (its folder removed or moved: the file
// may come back, or the document follows it elsewhere).
type systemWatcher struct{ poll pollWatcher }

func (s systemWatcher) watch(path string, changed func()) (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var stopPoll func()
	stopped := false
	go func() {
		if w, err := watch.New(ctx, path, watch.Options{}); err == nil {
			defer w.Close()
			for {
				if _, err := w.Next(ctx); err != nil {
					break
				}
				post(changed)
			}
			if ctx.Err() != nil {
				return
			}
			post(changed) // the watch ended: its folder went
		}
		mu.Lock()
		defer mu.Unlock()
		if !stopped {
			stopPoll = s.poll.watch(path, changed)
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			mu.Lock()
			stopped = true
			if stopPoll != nil {
				stopPoll()
			}
			mu.Unlock()
		})
	}
}

// pollWatcher looks at a file's stamp every so often.
type pollWatcher struct{ every time.Duration }

func (p pollWatcher) watch(path string, changed func()) (stop func()) {
	quit := make(chan struct{})
	var once sync.Once
	go func() {
		last, ok, _ := statStamp(path)
		t := time.NewTicker(p.every)
		defer t.Stop()
		for {
			select {
			case <-quit:
				return
			case <-t.C:
			}
			cur, exists, err := statStamp(path)
			if err != nil || (exists == ok && cur == last) {
				continue
			}
			last, ok = cur, exists
			post(changed)
		}
	}()
	return func() { once.Do(func() { close(quit) }) }
}

// watch starts watching b's file, again after it moved.
func (k *kind) watch(b *base) {
	k.unwatch(b)
	if b.path == "" || b.closed {
		return
	}
	path := b.path
	b.unwatchFile = watcher.watch(path, func() {
		if !b.closed && b.path == path {
			b.checkDisk()
		}
	})
	b.watched = path
}

// unwatch stops watching b's file.
func (k *kind) unwatch(b *base) {
	if b.unwatchFile != nil {
		b.unwatchFile()
		b.unwatchFile = nil
	}
	b.watched = ""
}

// rewatch follows b's file to where it is now, after a save as, a rename
// or another app's move.
func (k *kind) rewatch(b *base) {
	if b.watched != b.path {
		k.watch(b)
	}
}

// restore opens the documents of the recovery sessions no instance
// holds, edited, keeping their copies in this session, then calls done.
func (k *kind) restore(done func()) {
	store, read := k.recovery, k.read
	type found struct {
		o                         orphan
		v                         any
		cur                       fileStamp
		exists, writable, haveCur bool
	}
	goIO(func() {
		orphans, release := store.claim()
		var list []found
		for _, o := range orphans {
			f, err := os.Open(o.file)
			if err != nil {
				continue
			}
			v, err := read(io.Reader(f))
			f.Close()
			if err != nil {
				logf("cannot read the recovery copy of %s: %v", o.entry.Name+o.entry.Path, err)
				continue
			}
			g := found{o: o, v: v}
			if p := o.entry.Path; p != "" {
				g.cur, g.exists, _ = statStamp(p)
				g.writable = g.exists && fileWritable(p)
				g.haveCur = true
			}
			list = append(list, g)
		}
		post(func() {
			restored := map[string]bool{}
			left := 1
			settled := func() {
				if left--; left == 0 {
					goIO(func() { release(restored) })
				}
			}
			for _, g := range list {
				e := g.o.entry
				if e.Path != "" && k.find(e.Path) != nil {
					continue // a file the app was opened with, say
				}
				b := k.create(g.v, e.Name)
				b.id = e.ID
				b.ops.restore(g.v)
				if e.Path != "" {
					b.path, b.stamp = e.Path, e.Stamp
					switch {
					case !g.exists:
						b.deleted = true
					case !g.cur.same(e.Stamp):
						b.conflict = true
					}
					b.readOnly = g.exists && !g.writable
				} else if n := untitledNumber(e.Name); n > 0 {
					b.untitled = n
				}
				b.dirtySince = now()
				restored[g.o.file] = true
				left++
				b.backup(settled) // this session's copy, before the old one goes
				if k.desktop {
					k.show(b)
				} else {
					k.recoverQuietly(b)
				}
			}
			settled()
			done()
		})
	})
}

// recoverQuietly writes a document a phone's crash left unsaved to its
// file, or beside it as "Notes (Recovered)" if another app changed or
// deleted the file meanwhile: a phone shows no document it didn't ask
// for.
func (k *kind) recoverQuietly(b *base) {
	if b.conflict || b.deleted || b.readOnly || b.path == "" {
		name := b.Name()
		b.path = k.freePath(sayWith("recoveredName", map[string]any{"name": displayName(name)}))
		b.conflict, b.deleted, b.readOnly = false, false, false
	}
	b.write(b.path, writeSaveAs, func(err error) {
		if err != nil {
			logf("cannot recover %s: %v", b.Name(), err)
			return // the copy stays for the next launch
		}
		b.closed = true
		if b.dispose != nil {
			b.dispose()
		}
		k.listFiles()
	})
}
