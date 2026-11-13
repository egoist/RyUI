package watch

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync/atomic"
)

// scan keeps what a watch saw of its tree, and which entries the
// backend watches. The system only says where something changed, so the
// watch lists or stats those places again and compares what it finds with
// what it saw: the events are the differences. Only the goroutine of the
// watch uses it.
type scan struct {
	w *Watcher
	// root is the watched directory, or the directory of the watched file
	// (file), at dir; identity is what it is, and name the file's name.
	root     *os.Root
	identity os.FileInfo
	dir      string
	file     bool
	name     string
	// entries are what the scan saw, by slash-separated path relative to
	// the root ("." for the root), and children the names in each listed
	// directory; targets what it enrolled.
	entries  map[string]record
	children map[string]map[string]bool
	targets  map[string]targetRef
	ids      map[uint64]string
	next     uint64
	// subtree says the root's target watches the whole tree (Windows), so
	// no other directory is enrolled; noSubtree that the backend can't.
	// noFiles says the backend watches files only through their
	// directories, so no file is enrolled.
	subtree, noSubtree, noFiles bool
	// What to look at again in the next update: directories to list,
	// entries to stat, entries the system said were written, and whether
	// to list every directory, after the system lost changes.
	lists, stats, writes map[string]bool
	all                  bool
	// races are entries that changed while the scan looked at them, which
	// are reported as written.
	races map[string]bool
	// ignored holds the directories Ignore ignored, by name in each
	// listed directory that has some, so that notices about what is in
	// them are dropped.
	ignored map[string]map[string]bool
	// before holds what an update changed, as it was before.
	before map[string]prior
	// overflowed says the system dropped changes since the last update,
	// which looks at everything and says so.
	overflowed bool
}

// record is what the scan keeps of a file or directory: its
// identity, and what says its content changed. It holds no pointer, so a
// snapshot of many costs little memory and nothing for the collector to
// scan.
type record struct {
	id    fileID
	size  int64
	mtime int64
	mode  fs.FileMode
}

func (e record) isDir() bool { return e.mode.IsDir() }

// unknownIDs numbers the entries whose identity can't be read, none
// on the systems that watch, so that each is a file of its own.
var unknownIDs atomic.Uint64

// entryOf returns what the scan keeps of info, a stat taken now:
// its identity is read now (StatIdentity), not when it is compared.
func entryOf(info os.FileInfo) record {
	id, ok := identityOf(statIdentity(info))
	if !ok {
		id = fileID{volume: ^uint64(0), index: unknownIDs.Add(1)}
	}
	id.typ = info.Mode().Type()
	return record{id: id, size: info.Size(), mtime: info.ModTime().UnixNano(), mode: info.Mode()}
}

// prior is an entry as it was before an update; ok is false for
// one that didn't exist.
type prior struct {
	entry record
	ok    bool
}

// targetRef is what the scan keeps of a target it enrolled.
type targetRef struct {
	id                 uint64
	identity           fileID
	directory, subtree bool
}

func newScan(w *Watcher) *scan {
	return &scan{
		w:        w,
		entries:  map[string]record{},
		children: map[string]map[string]bool{},
		targets:  map[string]targetRef{},
		ids:      map[uint64]string{},
		lists:    map[string]bool{},
		stats:    map[string]bool{},
		writes:   map[string]bool{},
		races:    map[string]bool{},
		ignored:  map[string]map[string]bool{},
	}
}

func (s *scan) close() {
	if s.root != nil {
		s.root.Close()
	}
}

// start enrolls the root and, below it, what the watch covers. The events
// it returns are for entries that changed while it looked at them.
func (s *scan) start() ([]Event, error) {
	info, err := os.Lstat(s.w.root)
	if err != nil {
		return nil, err
	}
	info = statIdentity(info)
	s.dir = s.w.root
	if !info.IsDir() {
		// A file is watched through its directory, which names it.
		s.file = true
		s.dir, s.name = filepath.Dir(s.w.root), filepath.Base(s.w.root)
	}
	if s.root, err = openRoot(s.dir); err != nil {
		return nil, err
	}
	// The identity through the handle, as Windows loads that of a path
	// only when it is first compared.
	if s.identity, err = s.root.Stat("."); err != nil {
		return nil, err
	}
	s.identity = statIdentity(s.identity)
	if s.file {
		if info, err = os.Lstat(s.dir); err != nil {
			return nil, err
		}
		info = statIdentity(info)
	}
	if !os.SameFile(info, s.identity) {
		return nil, &os.PathError{Op: "watch", Path: s.dir, Err: errRootChanged}
	}
	if err = s.watchParent(); err != nil {
		return nil, err
	}
	info = s.identity
	if s.file {
		if info, err = s.root.Lstat(s.name); err != nil {
			return nil, err
		}
	}
	// The initial contents are not events, so what the scan sees is not
	// kept as it was before (touch).
	s.before = nil
	if err = s.visit(s.root, ".", info); err != nil {
		return nil, err
	}
	s.before = map[string]prior{}
	return s.events(), nil
}

// watchParent enrolls the directory of a watched file, and the parent of
// the watched directory on Windows, where the directory's own handle
// reports nothing when it is renamed or replaced; "..", whose notices
// about other entries are ignored. A drive's root, which can't be renamed,
// has no parent.
func (s *scan) watchParent() error {
	if !s.file && runtime.GOOS != "windows" {
		return nil
	}
	parent := filepath.Dir(s.w.root)
	if parent == s.w.root {
		return nil
	}
	info, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	return s.add("..", statIdentity(info))
}

// gone reports whether the watched directory, or the directory of the
// watched file, went away or was replaced.
func (s *scan) gone() bool {
	info, err := os.Lstat(s.dir)
	return errors.Is(err, fs.ErrNotExist) || err == nil && (!info.IsDir() || !os.SameFile(statIdentity(info), s.identity))
}

// goneEvents are the last events of a watch whose directory went away.
func (s *scan) goneEvents() []Event {
	e, ok := s.entries["."]
	if !ok {
		return nil // a watched file that was already gone
	}
	return []Event{{Op: Remove, Path: ".", IsDir: e.isDir()}}
}

// osPath is the path of p for root.
func (s *scan) osPath(p string) string {
	if s.file {
		return s.name
	}
	return filepath.FromSlash(p)
}

// base is the name of p in its directory: "." for the watched directory,
// and the watched file's name.
func (s *scan) base(p string) string {
	if s.file {
		return s.name
	}
	return path.Base(p)
}

// lstat stats p, without following a symlink, in dir, the directory that
// has it: one call by its name, where the root would open every directory
// above it in turn.
func (s *scan) lstat(dir *os.Root, p string) (os.FileInfo, error) {
	info, err := dir.Lstat(s.base(p))
	return info, s.pathError(err, p)
}

// entryInfo returns what entry, child read from d, is, as lstat would.
func (s *scan) entryInfo(d *dirHandle, entry fs.DirEntry, child string) (os.FileInfo, error) {
	info, err := entryInfo(d.root, entry)
	return info, s.pathError(err, child)
}

// pathError names p, from the root, in err, which names it relative to the
// directory that has it.
func (s *scan) pathError(err error, p string) error {
	if e, ok := err.(*os.PathError); ok {
		e.Path = s.osPath(p)
	}
	return err
}

// ignores reports whether p is in a directory Ignore ignored.
func (s *scan) ignores(p string) bool {
	for p != "." {
		if s.ignored[path.Dir(p)][path.Base(p)] {
			return true
		}
		p = path.Dir(p)
	}
	return false
}

// skip reports whether Ignore ignores child, and remembers in ignored the
// directories it does.
func (s *scan) skip(child string, isDir bool) bool {
	if s.w.opts.Ignore == nil || !s.w.opts.Ignore(child, isDir) {
		return false
	}
	if isDir {
		s.ignoredIn(path.Dir(child))[path.Base(child)] = true
	}
	return true
}

// mark records what n says changed, for the next update. It returns an
// error when n ends the watch.
func (s *scan) mark(n notice) error {
	if n.err != nil {
		if errors.Is(n.err, errOverflow) {
			s.lookAll()
			return nil
		}
		if _, ok := s.ids[n.target]; !ok && n.target != 0 {
			return nil // a target removed since
		}
		return n.err
	}
	p, ok := s.ids[n.target]
	if !ok {
		return nil
	}
	if p == ".." {
		name := filepath.Base(s.w.root)
		if n.kind == kindLost {
			// The next update enrolls it again, or ends the watch.
			s.unenroll("..")
		} else if n.name != name && (n.name != "" || !s.file) {
			return nil
		}
		if s.file {
			s.stats["."] = true
			if n.kind == kindWrite {
				s.writes["."] = true
			}
		} else {
			s.lists["."] = true
		}
		return nil
	}
	if s.file {
		// The file itself (macOS).
		if n.kind == kindLost {
			s.unenroll(".")
		}
		s.stats["."] = true
		if n.kind == kindWrite {
			s.writes["."] = true
		}
		return nil
	}
	t := s.targets[p]
	listable := t.directory && (p == "." || s.w.opts.Recursive)
	if n.kind == kindLost {
		// The next update enrolls it again if it still exists, and looks
		// at everything if it watched the whole tree.
		if p == "." && s.subtree {
			s.all = true
		}
		s.unenroll(p)
		n = notice{kind: kindDirty}
	}
	if n.name == "" || !listable {
		if listable {
			s.lists[p] = true
		} else {
			s.stats[p] = true
		}
		if n.kind == kindWrite {
			s.writes[p] = true
		}
		return nil
	}
	if !validWatchName(n.name, t.subtree) {
		return errors.New("mygo: invalid name in a file watch notice")
	}
	// Statting a child lists its directory if it appeared, went away or
	// was replaced.
	child := path.Join(p, n.name)
	if s.ignores(child) {
		return nil
	}
	s.stats[child] = true
	if n.kind == kindWrite {
		s.writes[child] = true
	}
	if len(s.lists)+len(s.stats)+len(s.writes) > s.w.opts.MaxEntries {
		s.all = true
		clear(s.lists)
		clear(s.stats)
		clear(s.writes)
	}
	return nil
}

// validWatchName reports whether name names a child, or for a subtree a
// path beneath it.
func validWatchName(name string, subtree bool) bool {
	if strings.ContainsRune(name, 0) || strings.ContainsRune(name, filepath.Separator) && filepath.Separator != '/' {
		return false
	}
	if !subtree && strings.Contains(name, "/") {
		return false
	}
	for part := range strings.SplitSeq(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// lookAll makes the next update look at everything, after the system
// dropped changes.
func (s *scan) lookAll() {
	s.all = true
	s.overflowed = true
}

// pending reports whether an update has anything to do.
func (s *scan) pending() bool {
	return s.all || len(s.lists) > 0 || len(s.stats) > 0 || len(s.writes) > 0
}

// update looks again at what changed and returns the events.
func (s *scan) update() ([]Event, error) {
	if err := s.w.ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Lstat(s.dir)
	if err != nil {
		return nil, err
	}
	if info = statIdentity(info); !info.IsDir() || !os.SameFile(info, s.identity) {
		return nil, &os.PathError{Op: "watch", Path: s.dir, Err: errRootChanged}
	}
	if err = s.watchParent(); err != nil {
		return nil, err
	}
	s.before = map[string]prior{}
	if s.file {
		all := s.all
		s.all = false
		if all || len(s.stats) > 0 {
			if err := s.statFile(); err != nil {
				return nil, err
			}
		}
		clear(s.stats)
		return s.events(), nil
	}
	if s.all {
		s.all = false
		for p, e := range s.entries {
			if e.isDir() && (p == "." || s.w.opts.Recursive) {
				s.lists[p] = true
			}
		}
	}
	for p := range s.stats {
		if err := s.stat(p); err != nil {
			return nil, err
		}
	}
	// Parents first: listing a directory again can replace those below it.
	// Listing a directory lists those to list in it, from it.
	dirs := make([]string, 0, len(s.lists))
	for p := range s.lists {
		dirs = append(dirs, p)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if a, b := strings.Count(dirs[i], "/"), strings.Count(dirs[j], "/"); a != b {
			return a < b
		}
		return dirs[i] < dirs[j]
	})
	for _, p := range dirs {
		if !s.lists[p] || !s.listable(p) {
			continue // listed, removed or replaced with its parent
		}
		delete(s.lists, p)
		if err := s.list(nil, p); err != nil {
			return nil, err
		}
	}
	clear(s.lists)
	clear(s.stats)
	return s.events(), nil
}

// events compares what the last update changed with what it was, then
// forgets both.
func (s *scan) events() []Event {
	writes := s.writes
	for p := range s.races {
		writes[p] = true
	}
	for p := range writes {
		if _, ok := s.entries[p]; ok {
			s.touch(p)
		}
	}
	events := diff(s.before, s.entries, writes, s.w.opts.Attributes)
	a := s.before["."]
	b, ok := s.entries["."]
	if s.file && a.ok && ok && !a.entry.isDir() && !b.isDir() && a.entry.id != b.id {
		// A file replaced, as editors save, is written.
		events = []Event{{Op: Write, Path: "."}}
		if s.w.opts.Attributes && a.entry.mode != b.mode {
			events = append(events, Event{Op: Attrib, Path: "."})
		}
	}
	if s.overflowed && s.before != nil {
		s.overflowed = false
		events = append([]Event{{Op: Overflow, Path: "."}}, events...)
	}
	clear(s.before)
	clear(s.writes)
	clear(s.races)
	return events
}

// statFile looks at the watched file again.
func (s *scan) statFile() error {
	old, known := s.entries["."]
	info, err := s.root.Lstat(s.name)
	if errors.Is(err, fs.ErrNotExist) {
		if known {
			s.removeTree(".")
		}
		return nil
	}
	if s.inaccessible(".", err) {
		return nil
	}
	if err != nil {
		return err
	}
	if known && old.id == entryOf(info).id {
		s.set(".", info)
		return s.enroll(".", info)
	}
	if known {
		s.removeTree(".")
	}
	return s.visit(s.root, ".", info)
}

// stat looks at p again. For an entry it didn't know, or one that is gone
// or was replaced, it lists the directory that has it instead.
func (s *scan) stat(p string) error {
	known := p
	for known != "." && !s.has(known) {
		known = path.Dir(known)
	}
	if known != p && path.Dir(p) == known && s.w.opts.Ignore != nil && s.listable(known) {
		// Something new that Ignore may ignore, which needs no listing.
		info, err := s.root.Lstat(filepath.FromSlash(p))
		if err == nil && s.skip(p, info.IsDir()) {
			return nil
		}
	}
	if known == "." || known != p && s.listable(known) {
		s.lists[known] = true
		return nil
	}
	p = known
	old := s.entries[p]
	info, err := s.root.Lstat(filepath.FromSlash(p))
	if err == nil && old.id == entryOf(info).id {
		s.set(p, info)
		return s.enroll(p, info)
	}
	if s.inaccessible(p, err) {
		return nil
	}
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	s.lists[path.Dir(p)] = true
	return nil
}

// inaccessible reports whether err says p can't be looked at, as a
// directory without permission or, on Windows, a file the system holds
// (pagefile.sys): it is left as it was seen, if at all, rather than
// ending the watch. The watched directory itself can't be.
func (s *scan) inaccessible(p string, err error) bool {
	return err != nil && (p != "." || s.file) && inaccessible(err)
}

// ignoredIn returns the directories ignored in directory p.
func (s *scan) ignoredIn(p string) map[string]bool {
	if s.ignored[p] == nil {
		s.ignored[p] = map[string]bool{}
	}
	return s.ignored[p]
}

// has reports whether the scan knows p.
func (s *scan) has(p string) bool {
	_, ok := s.entries[p]
	return ok
}

// listable reports whether p is a directory the watch lists.
func (s *scan) listable(p string) bool {
	e, ok := s.entries[p]
	return ok && s.descends(p, e.isDir())
}

// list lists directory p again, in dir, the directory that has it, or
// from the root when dir is nil: what appeared or was replaced in it is
// visited, what is gone removed, and the rest stat'ed again. The
// directories in it still to list are listed from it.
func (s *scan) list(dir *os.Root, p string) error {
	name := s.base(p)
	if dir == nil {
		dir, name = s.root, filepath.FromSlash(p)
	}
	d, err := s.openDir(dir, name, p, s.entries[p])
	if err != nil {
		if p == "." {
			return err
		}
		if errors.Is(err, fs.ErrNotExist) || errors.Is(err, errRootChanged) {
			// Gone or replaced: what has it knows.
			return s.list(nil, path.Dir(p))
		}
		if s.inaccessible(p, err) {
			return nil
		}
		return err
	}
	defer d.close()
	if err := s.enroll(p, d.info); err != nil {
		return err
	}
	s.set(p, d.info)
	seen := map[string]bool{}
	delete(s.ignored, p)
	for {
		entries, err := d.file.ReadDir(128)
		for _, entry := range entries {
			name := entry.Name()
			child := path.Join(p, name)
			if s.skip(child, entry.IsDir()) {
				continue
			}
			seen[name] = true
			info, err := s.entryInfo(d, entry, child)
			if errors.Is(err, fs.ErrNotExist) {
				s.races[p] = true
				delete(seen, name)
				continue
			}
			if s.inaccessible(child, err) {
				continue // as it was, if it was known
			}
			if err != nil {
				return err
			}
			if old, ok := s.entries[child]; ok {
				if old.id == entryOf(info).id {
					s.set(child, info)
					if err := s.enroll(child, info); err != nil {
						return err
					}
					continue
				}
				s.removeTree(child)
			}
			if err := s.visit(d.root, child, info); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if s.inaccessible(p, err) {
			return nil // what is gone is not known
		}
		if err != nil {
			return err
		}
	}
	for name := range s.children[p] {
		if !seen[name] {
			s.removeTree(path.Join(p, name))
		}
	}
	for name := range s.children[p] {
		if child := path.Join(p, name); s.lists[child] {
			delete(s.lists, child)
			if s.listable(child) {
				if err := s.list(d.root, child); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// dirHandle is a directory of the tree, open to be read: root holds
// what is in it, which is looked at by name, relative to it, and file
// reads it.
type dirHandle struct {
	root *os.Root
	file *os.File
	info os.FileInfo
	// owned says root is the directory's own, which close closes, rather
	// than the scan's.
	owned bool
}

func (d *dirHandle) close() {
	d.file.Close()
	if d.owned {
		d.root.Close()
	}
}

// openDir opens directory p, at name in dir, which must still be the
// directory want.
func (s *scan) openDir(dir *os.Root, name, p string, want record) (*dirHandle, error) {
	d := &dirHandle{root: s.root}
	if p != "." {
		root, err := dir.OpenRoot(name)
		if err != nil {
			return nil, s.openError(err, dir, name, p, want)
		}
		d.root, d.owned = root, true
	}
	var err error
	if d.file, err = d.root.Open("."); err != nil {
		if d.owned {
			d.root.Close()
		}
		return nil, s.pathError(err, p)
	}
	if d.info, err = d.file.Stat(); err != nil {
		d.close()
		return nil, err
	}
	if !d.info.IsDir() || want.id != entryOf(d.info).id {
		d.close()
		return nil, &os.PathError{Op: "watch", Path: s.osPath(p), Err: errRootChanged}
	}
	return d, nil
}

// openError is what opening directory p, at name in dir, failing with err
// says: errRootChanged when it is no longer the directory want, as
// when a symlink replaced it, which the root follows only within dir.
func (s *scan) openError(err error, dir *os.Root, name, p string, want record) error {
	if errors.Is(err, fs.ErrNotExist) || s.inaccessible(p, err) {
		return s.pathError(err, p)
	}
	checked, statErr := dir.Lstat(name)
	if statErr != nil {
		return s.pathError(statErr, p)
	}
	if !checked.IsDir() || want.id != entryOf(checked).id {
		return &os.PathError{Op: "watch", Path: s.osPath(p), Err: errRootChanged}
	}
	return s.pathError(err, p)
}

// visit records p, which wasn't known, in dir, the directory that has it,
// and when the watch covers it what is beneath it. An entry removed or
// replaced while it looks is skipped, and its directory reported as
// written.
func (s *scan) visit(dir *os.Root, p string, info os.FileInfo) error {
	if err := s.w.ctx.Err(); err != nil {
		return err
	}
	if len(s.entries) >= s.w.opts.MaxEntries || len(s.races) > s.w.opts.MaxEntries {
		return ErrLimit
	}
	gone := func() error {
		s.unenroll(p)
		s.races[path.Dir(p)] = true
		return nil
	}
	// Enroll first, then check the entry is still what was enrolled, so
	// that nothing changes unseen in between. Replacements, as editors'
	// atomic saves do, are retried a few times. What isn't enrolled is
	// watched through its directory, enrolled before it was listed, or
	// not at all, and needs no check.
	var d *dirHandle
	for attempt := 0; ; attempt++ {
		if attempt == 3 {
			return gone()
		}
		if !s.enrolls(p, info) {
			s.unenroll(p)
		} else {
			err := s.enroll(p, info)
			if errors.Is(err, fs.ErrPermission) || errors.Is(err, ErrUnsupported) || errors.Is(err, context.Canceled) {
				return err
			}
			checked, statErr := s.lstat(dir, p)
			if (p != "." || s.file) && (errors.Is(statErr, fs.ErrNotExist) || errors.Is(err, fs.ErrNotExist)) {
				return gone()
			}
			if statErr != nil {
				return statErr
			}
			if !os.SameFile(info, checked) {
				if p == "." && !s.file {
					return errRootChanged
				}
				s.races[p] = true
				info = checked
				continue
			}
			if err != nil {
				return err
			}
			info = checked
		}
		if !s.descends(p, info.IsDir()) {
			break
		}
		opened, err := s.openDir(dir, s.base(p), p, entryOf(info))
		if err == nil {
			d, info = opened, opened.info
			break
		}
		if s.inaccessible(p, err) {
			break // known, but not what is in it
		}
		if p == "." {
			return err
		}
		if errors.Is(err, fs.ErrNotExist) {
			return gone()
		}
		if !errors.Is(err, errRootChanged) {
			return err
		}
		s.races[p] = true
		if info, err = s.lstat(dir, p); errors.Is(err, fs.ErrNotExist) {
			return gone()
		} else if err != nil {
			return err
		}
	}
	s.set(p, info)
	if d == nil {
		return nil
	}
	defer d.close()
	for {
		entries, err := d.file.ReadDir(128)
		for _, entry := range entries {
			child := path.Join(p, entry.Name())
			if s.skip(child, entry.IsDir()) {
				continue
			}
			info, err := s.entryInfo(d, entry, child)
			if errors.Is(err, fs.ErrNotExist) {
				s.races[p] = true
				continue
			}
			if s.inaccessible(child, err) {
				continue
			}
			if err != nil {
				return err
			}
			if err := s.visit(d.root, child, info); err != nil {
				return err
			}
		}
		if err == io.EOF || s.inaccessible(p, err) {
			return nil // what it could list
		}
		if err != nil {
			return err
		}
	}
}

// descends reports whether the watch covers what is in p, a directory
// if isDir.
func (s *scan) descends(p string, isDir bool) bool {
	// Symlinks and Windows junctions are not directories to Lstat.
	return isDir && !s.file && (p == "." || s.w.opts.Recursive)
}

// touch remembers p as it is before an update changes it; nothing during
// the first scan.
func (s *scan) touch(p string) {
	if s.before == nil {
		return
	}
	if _, ok := s.before[p]; !ok {
		e, ok := s.entries[p]
		s.before[p] = prior{e, ok}
	}
}

func (s *scan) set(p string, info os.FileInfo) {
	s.touch(p)
	// What an update compares with what came before it, so the identity
	// doesn't follow a later replacement of the entry.
	s.entries[p] = entryOf(info)
	if p != "." {
		dir := path.Dir(p)
		if s.children[dir] == nil {
			s.children[dir] = map[string]bool{}
		}
		s.children[dir][path.Base(p)] = true
	}
}

// removeTree forgets p and what is beneath it.
func (s *scan) removeTree(p string) {
	for name := range s.children[p] {
		s.removeTree(path.Join(p, name))
	}
	delete(s.children, p)
	delete(s.ignored, p)
	s.touch(p)
	delete(s.entries, p)
	if p != "." {
		delete(s.children[path.Dir(p)], path.Base(p))
	}
	s.unenroll(p)
}

// enrolls reports whether enroll has the backend watch p, which the scan
// saw as info: not when the root's target covers it, it is a file the
// backend watches through its directory, or a symlink or a special file.
// A watched file is enrolled only while it is a file.
func (s *scan) enrolls(p string, info os.FileInfo) bool {
	return (info.Mode().IsRegular() && !s.noFiles || info.IsDir() && !s.file) && (!s.subtree || p == ".")
}

// enroll has the backend watch p, which the scan saw as info, if it
// enrolls it.
func (s *scan) enroll(p string, info os.FileInfo) error {
	if !s.enrolls(p, info) {
		s.unenroll(p)
		return nil
	}
	if err := s.add(p, info); !s.inaccessible(p, err) {
		return err
	}
	return nil // seen, but not watched
}

func (s *scan) add(p string, info os.FileInfo) error {
	if old, ok := s.targets[p]; ok {
		if old.identity == entryOf(info).id {
			return nil
		}
		s.unenroll(p)
	}
	s.next++
	t := target{id: s.next, path: filepath.Join(s.w.root, filepath.FromSlash(p)), dir: info.IsDir(), info: info}
	if p == "." && s.w.opts.Recursive && !s.noSubtree {
		t.subtree = true
		err := s.w.native.add(t)
		if errors.Is(err, ErrUnsupported) {
			s.noSubtree = true
			t.subtree = false
		} else if err != nil {
			return &os.PathError{Op: "watch", Path: t.path, Err: err}
		} else {
			// Directories below enrolled while the root had no target are
			// covered by it now.
			for q := range s.targets {
				if q != "." && q != ".." {
					s.unenroll(q)
				}
			}
			s.subtree = true
			s.record(p, t)
			return nil
		}
	}
	if err := s.w.native.add(t); err != nil {
		if !t.dir && errors.Is(err, ErrUnsupported) {
			// Its directory, enrolled before it was listed, watches it.
			s.noFiles = true
			return nil
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		return &os.PathError{Op: "watch", Path: t.path, Err: err}
	}
	s.record(p, t)
	return nil
}

func (s *scan) record(p string, t target) {
	s.targets[p] = targetRef{id: t.id, identity: entryOf(t.info).id, directory: t.dir, subtree: t.subtree}
	s.ids[t.id] = p
}

func (s *scan) unenroll(p string) {
	if t, ok := s.targets[p]; ok {
		delete(s.targets, p)
		delete(s.ids, t.id)
		s.w.native.remove(t.id)
		if p == "." {
			s.subtree = false
		}
	}
}

// diff turns what an update changed into events: changed holds
// what it looked at as it was before, entries what is now. An entry whose
// identity moved to another path is a rename, when nothing else has that
// identity; a file replaced at its path by another file is a write.
func diff(changed map[string]prior, entries map[string]record, writes map[string]bool, attributes bool) []Event {
	removed := make(map[string]record)
	created := make(map[string]record)
	// A file replaced at its path by another file, as editors and atomic
	// saves replace it (a temporary file renamed over it), is written, as a
	// watched file replaced is: not removed, then created, which would
	// have an app close a document it still has.
	replaced := make(map[string]bool)
	for p, was := range changed {
		now, ok := entries[p]
		if was.ok && ok && was.entry.id != now.id && was.entry.mode.IsRegular() && now.mode.IsRegular() {
			replaced[p] = true
			continue
		}
		if was.ok && (!ok || was.entry.id != now.id) {
			removed[p] = was.entry
		}
		if ok && (!was.ok || was.entry.id != now.id) {
			created[p] = now
		}
	}
	type group struct {
		path   string
		events []Event
	}
	var groups []group
	paired := make(map[string]string)
	keys := make([]string, 0, len(removed))
	for p := range removed {
		keys = append(keys, p)
	}
	sort.Strings(keys)
	// Pairing looks each removed entry up by identity: comparing it with
	// every entry instead would make a directory of n files renamed n²
	// comparisons.
	var ids *fileIDs
	if len(removed) > 0 && len(created) > 0 {
		ids = newFileIDs(changed, entries)
	}
	for _, old := range keys {
		a := removed[old]
		// A hard link makes pairing by identity ambiguous: the identity
		// must be the only one of its kind before, after and among what
		// was created.
		match := ""
		if ids != nil {
			if match = ids.pair(a); match != "" {
				if _, ok := created[match]; !ok {
					match = ""
				}
			}
		}
		if match == "" {
			continue
		}
		// A replacement is a removal and a creation even if the old name
		// survives elsewhere.
		if _, exists := entries[old]; exists {
			continue
		}
		if changed[match].ok {
			continue
		}
		// Moved with a directory renamed above it, which says it all.
		descendant := false
		for parent := path.Dir(old); parent != "."; parent = path.Dir(parent) {
			if dest, ok := paired[parent]; ok && match == dest+strings.TrimPrefix(old, parent) {
				descendant = true
				break
			}
		}
		paired[old] = match
		delete(removed, old)
		b := created[match]
		delete(created, match)
		var events []Event
		if !descendant {
			events = append(events, Event{Op: Rename, OldPath: old, Path: match, IsDir: a.isDir()})
		}
		if !b.isDir() && (writes[old] || writes[match] || contentChanged(a, b)) {
			events = append(events, Event{Op: Write, Path: match})
		}
		if attributes && a.mode != b.mode {
			events = append(events, Event{Op: Attrib, Path: match, IsDir: b.isDir()})
		}
		groups = append(groups, group{match, events})
	}
	for p, was := range changed {
		var events []Event
		if a, ok := removed[p]; ok {
			events = append(events, Event{Op: Remove, Path: p, IsDir: a.isDir()})
		}
		if b, ok := created[p]; ok {
			events = append(events, Event{Op: Create, Path: p, IsDir: b.isDir()})
		}
		if now, ok := entries[p]; replaced[p] || was.ok && ok && !now.isDir() && was.entry.id == now.id && (writes[p] || contentChanged(was.entry, now)) {
			events = append(events, Event{Op: Write, Path: p})
		}
		if now, ok := entries[p]; attributes && was.ok && ok && (replaced[p] || was.entry.id == now.id) && was.entry.mode != now.mode {
			events = append(events, Event{Op: Attrib, Path: p, IsDir: now.isDir()})
		}
		if len(events) > 0 {
			groups = append(groups, group{p, events})
		}
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].path < groups[j].path })
	var result []Event
	for _, g := range groups {
		result = append(result, g.events...)
	}
	return result
}

// fileID is a file's identity as os.SameFile compares it (device and
// inode; volume and file index on Windows), usable as a map key, with its
// type: a file never changes type, but a file system such as ext4 gives
// a freed inode to the next file created, so that a directory removed and
// a symlink created in its place can have the same inode.
type fileID struct {
	volume, index uint64
	typ           fs.FileMode
}

// fileIDs counts the entries of an update's before and after by
// identity, keeping the path of one, for pairing renames.
type fileIDs struct {
	before, after map[fileID]idSeen
}

type idSeen struct {
	path  string
	count int
}

// newFileIDs counts what an update looked at, as it was (changed)
// and is (entries).
func newFileIDs(changed map[string]prior, entries map[string]record) *fileIDs {
	ids := &fileIDs{before: make(map[fileID]idSeen, len(changed)), after: make(map[fileID]idSeen, len(changed))}
	count := func(m map[fileID]idSeen, p string, id fileID) {
		seen := m[id]
		seen.path = p
		seen.count++
		m[id] = seen
	}
	for p, was := range changed {
		if was.ok {
			count(ids.before, p, was.entry.id)
		}
		if now, ok := entries[p]; ok {
			count(ids.after, p, now.id)
		}
	}
	return ids
}

// pair returns the path after has for a, an entry before, when a's
// identity is the only one of its kind before and after; "" otherwise.
func (ids *fileIDs) pair(a record) string {
	if ids.before[a.id].count != 1 {
		return ""
	}
	if seen := ids.after[a.id]; seen.count == 1 {
		return seen.path
	}
	return ""
}

// contentChanged reports whether a file's content may have changed, as
// its size or modification time says; its permissions and other metadata,
// which backup tools, indexers and antivirus touch, don't count.
func contentChanged(a, b record) bool {
	return a.size != b.size || a.mtime != b.mtime
}
