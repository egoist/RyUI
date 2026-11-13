package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These tests drive the scan with the fake native, which reports only
// what a test tells it to: they say what the watch makes of each notice.
// watch_test.go runs the systems' own watches.

func TestLifecycle(t *testing.T) {
	dir := t.TempDir()
	w := fakeWatch(t, dir, Options{Debounce: -1})
	if want, _ := filepath.EvalSymlinks(dir); w.Root() != want {
		t.Fatalf("root = %q, want %q", w.Root(), want)
	}
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("hello"), 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{})
	// Directories written are not reported: their entries are.
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Create, Path: "a"}}) {
		t.Fatalf("batch = %+v", got)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := w.Next(canceled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w.Close()
	if _, err := w.Next(context.Background()); err != io.EOF || w.Err() != nil {
		t.Fatal(err, w.Err())
	}
	select {
	case <-w.Done():
	case <-waitCtx(t).Done():
		t.Fatal("cleanup timed out")
	}
}

// Canceling the context of a watch stops it as Close does.
func TestContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w, err := New(ctx, t.TempDir(), Options{backend: "fake"})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-w.Done()
	if _, err = w.Next(context.Background()); err != io.EOF || w.Err() != nil {
		t.Fatal(err, w.Err())
	}
}

// A Next waiting as the watch closes returns io.EOF.
func TestCloseWhileWaiting(t *testing.T) {
	w := fakeWatch(t, t.TempDir(), Options{})
	got := make(chan error, 1)
	go func() {
		_, err := w.Next(context.Background())
		got <- err
	}()
	waitUntil(t, "Next to wait", func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()
		return len(w.waiters) == 1
	})
	w.Close()
	if err := <-got; err != io.EOF {
		t.Fatal(err)
	}
}

func TestValidation(t *testing.T) {
	if w, err := New(context.Background(), t.TempDir(), Options{MaxEntries: -1, backend: "fake"}); err == nil {
		w.Close()
		t.Fatal("accepted a negative limit")
	}
	//lint:ignore SA1012 a nil context is what's tested
	if _, err := New(nil, t.TempDir(), Options{}); err == nil { //nolint:staticcheck
		t.Fatal("nil context")
	}
	if _, err := New(context.Background(), "", Options{}); err == nil {
		t.Fatal("empty path")
	}
	if _, err := New(context.Background(), filepath.Join(t.TempDir(), "missing"), Options{backend: "fake"}); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
	// A MaxDelay shorter than Debounce is Debounce.
	w := fakeWatch(t, t.TempDir(), Options{Debounce: time.Second, MaxDelay: time.Millisecond})
	if w.opts.MaxDelay != time.Second {
		t.Fatalf("MaxDelay = %v", w.opts.MaxDelay)
	}
}

func TestRecursiveLimit(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := New(context.Background(), dir, Options{Recursive: true, MaxEntries: 2, backend: "fake"}); !errors.Is(err, ErrLimit) {
		t.Fatalf("limit: %v", err)
	}
}

// The watch doesn't look again while a batch waits for Next, and what
// changed meanwhile comes in the next batch.
func TestBackpressure(t *testing.T) {
	dir := t.TempDir()
	w := fakeWatch(t, dir, Options{Debounce: -1})
	for _, name := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
		notifyAt(t, w, ".", notice{name: name, kind: kindCreate})
		// The first look's batch waits for Next before the second notice.
		waitUntil(t, "a batch of "+name, w.waiting)
	}
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Create, Path: "a"}}) {
		t.Fatalf("first batch = %+v", got)
	}
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Create, Path: "b"}}) {
		t.Fatalf("second batch = %+v", got)
	}
	// Many notices while a batch waits look once.
	for i := range 20 {
		name := fmt.Sprint("c", i)
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
		notifyAt(t, w, ".", notice{name: name, kind: kindCreate})
	}
	seen := 0
	for seen < 20 {
		seen += len(batch(t, w))
	}
	if seen != 20 {
		t.Fatalf("%d events", seen)
	}
}

// A watched directory removed ends the watch after an event saying so.
func TestRootLoss(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	w := fakeWatch(t, dir, Options{Debounce: -1})
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{kind: kindRemove})
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Remove, Path: ".", IsDir: true}}) {
		t.Fatalf("batch = %+v", got)
	}
	if _, err := w.Next(context.Background()); err != io.EOF || w.Err() != nil {
		t.Fatal(err, w.Err())
	}
}

// A notice for a target removed since doesn't stop the watch.
func TestStaleFailureIgnored(t *testing.T) {
	dir := t.TempDir()
	w := fakeWatch(t, dir, Options{Debounce: -1})
	w.native.(*fakeNative).notify(notice{target: 99999, err: errors.New("retired target")})
	if err := os.WriteFile(filepath.Join(dir, "a"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{name: "a"})
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Create, Path: "a"}}) {
		t.Fatalf("batch = %+v", got)
	}
}

// When the system dropped changes, the watch looks at everything and says
// so first.
func TestOverflow(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	w := fakeWatch(t, dir, Options{Recursive: true, Debounce: -1})
	for _, p := range []string{"a", "sub/b"} {
		if err := os.WriteFile(filepath.Join(dir, p), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	w.native.(*fakeNative).notify(notice{err: errOverflow})
	want := []Event{{Op: Overflow, Path: "."}, {Op: Create, Path: "a"}, {Op: Create, Path: "sub/b"}}
	if got := batch(t, w); !slices.Equal(got, want) {
		t.Fatalf("batch = %+v, want %+v", got, want)
	}
	// Reported once.
	if err := os.WriteFile(filepath.Join(dir, "c"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{name: "c"})
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Create, Path: "c"}}) {
		t.Fatalf("batch = %+v", got)
	}
}

// What comes while raw is full is spilled, and past MaxEntries the watch
// looks at everything instead.
func TestSpill(t *testing.T) {
	w := &Watcher{ctx: context.Background(), opts: Options{MaxEntries: 3}, raw: make(chan notice, 1), spilled: make(chan struct{}, 1), overflow: make(chan struct{}, 1)}
	for i := range 4 {
		w.notice(notice{target: uint64(i)})
	}
	if n := <-w.raw; n.target != 0 {
		t.Fatalf("raw = %+v", n)
	}
	select {
	case <-w.spilled:
	default:
		t.Fatal("spill not signaled")
	}
	select {
	case <-w.overflow:
		t.Fatal("overflow within MaxEntries")
	default:
	}
	if spill := w.unspill(); len(spill) != 3 || spill[0].target != 1 || spill[2].target != 3 {
		t.Fatalf("spill = %+v", spill)
	}
	w.raw <- notice{}
	for i := range 4 {
		w.notice(notice{target: uint64(10 + i)})
	}
	select {
	case <-w.overflow:
	default:
		t.Fatal("no overflow past MaxEntries")
	}
	if spill := w.unspill(); spill != nil {
		t.Fatalf("spill kept past an overflow: %+v", spill)
	}
}

func TestDirectoryRename(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "deep"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "deep", "f"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	w := fakeWatch(t, dir, Options{Recursive: true, Debounce: -1})
	if err := os.Rename(filepath.Join(dir, "a"), filepath.Join(dir, "b")); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{name: "a", kind: kindMoveFrom})
	notifyAt(t, w, ".", notice{name: "b", kind: kindMoveTo})
	// One rename says it all: what is in it moved with it.
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Rename, OldPath: "a", Path: "b", IsDir: true}}) {
		t.Fatalf("batch = %+v", got)
	}
	// The directories moved are enrolled at their new paths.
	if err := os.WriteFile(filepath.Join(dir, "b", "deep", "g"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, "b/deep", notice{name: "g"})
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Create, Path: "b/deep/g"}}) {
		t.Fatalf("batch = %+v", got)
	}
}

// A subtree target (Windows) names what changes deeper by its path, and
// no other directory is enrolled.
func TestSubtree(t *testing.T) {
	configureFake(t, true, true, nil)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0700); err != nil {
		t.Fatal(err)
	}
	w := fakeWatch(t, dir, Options{Recursive: true, Debounce: -1})
	targets := w.native.(*fakeNative).list()
	if len(targets) != 1 || !targets[0].subtree {
		t.Fatalf("targets %+v", targets)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b", "f"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{name: "a/b/f", kind: kindCreate})
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Create, Path: "a/b/f"}}) {
		t.Fatalf("batch = %+v", got)
	}
	// A notice naming a path that leaves the tree ends the watch rather
	// than being followed.
	notifyAt(t, w, ".", notice{name: "../x"})
	if _, err := w.Next(waitCtx(t)); err == nil || err == io.EOF {
		t.Fatalf("an escaping name: %v", err)
	}
}

// testDiff is diff of an update that found before as after.
func testDiff(before, after map[string]record, writes map[string]bool, attributes bool) []Event {
	changed, entries := testChanged(before, after)
	return diff(changed, entries, writes, attributes)
}

// testChanged is what an update that found before as after looked at:
// every path of either.
func testChanged(before, after map[string]record) (map[string]prior, map[string]record) {
	changed := map[string]prior{}
	for p, e := range before {
		changed[p] = prior{e, true}
	}
	for p := range after {
		if _, ok := changed[p]; !ok {
			changed[p] = prior{}
		}
	}
	return changed, after
}

func TestDiff(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "a"), filepath.Join(dir, "b")
	if err := os.WriteFile(a, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(a)
	if err != nil {
		t.Fatal(err)
	}
	// Stat through a handle captures the identity now. On Windows os.Stat
	// defers reading it until SameFile, when the old name is gone.
	info, err := f.Stat()
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	old := map[string]record{"a": entryOf(info)}
	if err := os.Rename(a, b); err != nil {
		t.Fatal(err)
	}
	if info, err = os.Stat(b); err != nil {
		t.Fatal(err)
	}
	events := testDiff(old, map[string]record{"b": entryOf(info)}, map[string]bool{"a": true}, false)
	if !slices.Equal(events, []Event{{Op: Rename, OldPath: "a", Path: "b"}, {Op: Write, Path: "b"}}) {
		t.Fatalf("%+v", events)
	}
}

// A file replaced at its path by another file, as an atomic save replaces
// it, is written, whatever its size and time: not removed and created.
// What isn't a file replaced by a file stays a removal and a creation.
func TestDiffReplacedFile(t *testing.T) {
	file := func(i uint64) record {
		return record{id: fileID{volume: 1, index: i}, mode: 0600, size: 3}
	}
	dir := record{id: fileID{volume: 1, index: 9, typ: fs.ModeDir}, mode: fs.ModeDir | 0700}
	for _, tt := range []struct {
		name          string
		before, after map[string]record
		want          []Event
	}{
		{"replaced", map[string]record{"a": file(1)}, map[string]record{"a": file(2)},
			[]Event{{Op: Write, Path: "a"}}},
		{"from a temporary file", map[string]record{"a": file(1), "a.tmp": file(2)}, map[string]record{"a": file(2)},
			[]Event{{Op: Write, Path: "a"}, {Op: Remove, Path: "a.tmp"}}},
		{"kept beside", map[string]record{"a": file(1)}, map[string]record{"a": file(2), "a~": file(1)},
			[]Event{{Op: Write, Path: "a"}, {Op: Create, Path: "a~"}}},
		{"by a directory", map[string]record{"a": file(1)}, map[string]record{"a": dir},
			[]Event{{Op: Remove, Path: "a"}, {Op: Create, Path: "a", IsDir: true}}},
	} {
		if events := testDiff(tt.before, tt.after, nil, false); !slices.Equal(events, tt.want) {
			t.Errorf("%s: events = %+v, want %+v", tt.name, events, tt.want)
		}
	}
}

// Permissions changed are Attrib events with Options.Attributes, and
// nothing without; a write with them is both.
func TestDiffAttributes(t *testing.T) {
	a := record{id: fileID{volume: 1, index: 1}, mode: 0600, size: 1, mtime: 1}
	b := a
	b.mode = 0400
	if events := testDiff(map[string]record{"a": a}, map[string]record{"a": b}, nil, false); len(events) != 0 {
		t.Fatalf("without Attributes: %+v", events)
	}
	if events := testDiff(map[string]record{"a": a}, map[string]record{"a": b}, nil, true); !slices.Equal(events, []Event{{Op: Attrib, Path: "a"}}) {
		t.Fatalf("with Attributes: %+v", events)
	}
	b.size = 2
	if events := testDiff(map[string]record{"a": a}, map[string]record{"a": b}, nil, true); !slices.Equal(events, []Event{{Op: Write, Path: "a"}, {Op: Attrib, Path: "a"}}) {
		t.Fatalf("written too: %+v", events)
	}
	// Renamed and changed.
	if events := testDiff(map[string]record{"a": a}, map[string]record{"c": b}, nil, true); !slices.Equal(events, []Event{{Op: Rename, OldPath: "a", Path: "c"}, {Op: Write, Path: "c"}, {Op: Attrib, Path: "c"}}) {
		t.Fatalf("renamed: %+v", events)
	}
}

// A file system such as ext4 gives a freed inode to the next file
// created: a directory replaced by a symlink with its inode is a removal
// and a creation, not a write.
func TestDiffReusedInode(t *testing.T) {
	id := fileID{volume: 1, index: 2}
	dir, link := id, id
	dir.typ, link.typ = fs.ModeDir, fs.ModeSymlink
	before := map[string]record{"a": {id: dir, mode: fs.ModeDir | 0700}, "a/f": {id: fileID{volume: 1, index: 3}}}
	after := map[string]record{"a": {id: link, mode: fs.ModeSymlink | 0777}}
	want := []Event{
		{Op: Remove, Path: "a", IsDir: true},
		{Op: Create, Path: "a"},
		{Op: Remove, Path: "a/f"},
	}
	if events := testDiff(before, after, nil, false); !slices.Equal(events, want) {
		t.Fatalf("events = %+v, want %+v", events, want)
	}
}

// testInfos returns the stats of files in dir, as the scan keeps them: a,
// b, c, a directory d, and a hard link of a when the file system has
// them; then a, after a was renamed to a2, by another way.
func testInfos(t testing.TB, dir string) []os.FileInfo {
	t.Helper()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	names := []string{"a", "b", "c", "d"}
	if err := os.Mkdir(filepath.Join(dir, "d"), 0700); err != nil {
		t.Fatal(err)
	}
	if os.Link(filepath.Join(dir, "a"), filepath.Join(dir, "link")) == nil {
		names = append(names, "link")
	}
	var infos []os.FileInfo
	for _, name := range names {
		info, err := os.Lstat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		infos = append(infos, statIdentity(info))
	}
	if err := os.Rename(filepath.Join(dir, "a"), filepath.Join(dir, "a2")); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	info, err := root.Lstat("a2")
	if err != nil {
		t.Fatal(err)
	}
	return append(infos, statIdentity(info))
}

type otherInfo struct{ os.FileInfo }

// The identity renames are paired by is what os.SameFile compares.
func TestIdentity(t *testing.T) {
	infos := testInfos(t, t.TempDir())
	for i, a := range infos {
		ida, ok := identityOf(a)
		if !ok {
			t.Fatalf("no identity for %s (%T)", a.Name(), a)
		}
		for _, b := range infos[i:] {
			idb, _ := identityOf(b)
			if same := os.SameFile(a, b); same != (ida == idb) {
				t.Errorf("%s, %s: SameFile %v, identities %v and %v", a.Name(), b.Name(), same, ida, idb)
			}
		}
	}
	if _, ok := identityOf(otherInfo{infos[0]}); ok {
		t.Error("an identity for a FileInfo of another type")
	}
}

// Pairing by identity pairs what comparing every entry with every other
// one would, for updates that rename, replace, hard link, move with a
// directory, create and remove.
func TestDiffPairing(t *testing.T) {
	var infos []record
	for _, info := range testInfos(t, t.TempDir()) {
		infos = append(infos, entryOf(info))
	}
	paths := []string{"a", "b", "c", "d", "d/x", "d/y", "e", "e/x", "e/y", "f"}
	rng := rand.New(rand.NewPCG(1, 2))
	pick := func() map[string]record {
		m := map[string]record{}
		for _, p := range paths {
			if rng.IntN(3) > 0 {
				m[p] = infos[rng.IntN(len(infos))]
			}
		}
		return m
	}
	unique := func(m map[string]record, a record) bool {
		count := 0
		for _, v := range m {
			if v.id == a.id {
				count++
			}
		}
		return count == 1
	}
	paired := 0
	for i := range 5000 {
		before, after := pick(), pick()
		changed, _ := testChanged(before, after)
		ids := newFileIDs(changed, after)
		for old, a := range before {
			want, count := "", 0
			for p, b := range after {
				if b.id == a.id {
					if c, ok := before[p]; !ok || c.id != b.id {
						want = p
						count++
					}
				}
			}
			if count != 1 || !unique(before, a) || !unique(after, a) {
				want = ""
			}
			got := ids.pair(a)
			if got != "" {
				if c, ok := before[got]; ok && c.id == a.id {
					got = "" // not created: the same file at the same path
				}
			}
			if got != want {
				t.Fatalf("case %d, %s: by identity %q, by comparing all %q", i, old, got, want)
			}
			if got != "" {
				paired++
			}
		}
	}
	if paired == 0 {
		t.Fatal("nothing paired")
	}
}

// A directory of n files renamed is paired in time linear in n.
func BenchmarkDiffRename(b *testing.B) {
	for _, n := range []int{2500, 10000, 40000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			dir := b.TempDir()
			before, after := map[string]record{}, map[string]record{}
			for i := range n {
				name := fmt.Sprint("f", i)
				if err := os.WriteFile(filepath.Join(dir, name), nil, 0600); err != nil {
					b.Fatal(err)
				}
				info, err := os.Lstat(filepath.Join(dir, name))
				if err != nil {
					b.Fatal(err)
				}
				e := entryOf(info)
				before["a/"+name], after["b/"+name] = e, e
			}
			info, err := os.Lstat(dir)
			if err != nil {
				b.Fatal(err)
			}
			before["a"], after["b"] = entryOf(info), entryOf(info)
			changed, _ := testChanged(before, after)
			for b.Loop() {
				if events := diff(changed, after, nil, false); len(events) != 1 || events[0].Op != Rename {
					b.Fatal(events)
				}
			}
		})
	}
}

// On Unix the scan takes what reading a directory opened in an os.Root
// says of an entry rather than stat it again: the os package stats every
// entry as it reads it, relative to the directory and without following
// a symlink, not later by a path that could lead out of the root.
func TestEntryInfo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("stat'ed by name in the directory's root")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a", filepath.Join(dir, "l")); err != nil {
		t.Fatal(err)
	}
	want, err := os.Lstat(filepath.Join(dir, "a"))
	if err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	f, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "l"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, entry := range entries {
		info, err := entryInfo(root, entry)
		if err != nil {
			t.Fatalf("%s: %v", entry.Name(), err)
		}
		switch entry.Name() {
		case "a":
			if !os.SameFile(info, want) || info.Size() != 1 {
				t.Fatalf("a: %v", info)
			}
		case "l":
			if info.Mode()&fs.ModeSymlink == 0 {
				t.Fatalf("l: %v, want the symlink", info.Mode())
			}
		}
	}
}

// A file is watched through its directory: written, replaced as editors
// save, removed and created again, then gone with its directory.
func TestFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dir")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config.json")
	if err := os.WriteFile(file, []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sibling"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	w := fakeWatch(t, file, Options{Debounce: -1})
	f := w.native.(*fakeNative)
	enrolled := map[string]bool{}
	for _, target := range f.list() {
		enrolled[target.path] = true
	}
	if !enrolled[w.Root()] || !enrolled[filepath.Dir(w.Root())] || len(enrolled) != 2 {
		t.Fatalf("enrolled %v", enrolled)
	}
	next := func(want Event) {
		t.Helper()
		if got := batch(t, w); !slices.Equal(got, []Event{want}) {
			t.Fatalf("batch = %+v, want %+v", got, want)
		}
	}
	if err := os.WriteFile(file, []byte("bb"), 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{kind: kindWrite})
	next(Event{Op: Write, Path: "."})
	// Other files of the directory are not reported: the next batch is
	// the file's alone.
	if err := os.WriteFile(filepath.Join(dir, "sibling"), []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, "..", notice{name: "sibling", kind: kindWrite})
	// Replaced, as editors save.
	if err := os.WriteFile(filepath.Join(dir, "config.json.tmp"), []byte("ccc"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(dir, "config.json.tmp"), file); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, "..", notice{name: "config.json", kind: kindMoveTo})
	next(Event{Op: Write, Path: "."})
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, "..", notice{name: "config.json", kind: kindRemove})
	next(Event{Op: Remove, Path: "."})
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, "..", notice{name: "config.json", kind: kindCreate})
	next(Event{Op: Create, Path: "."})
	// The new file is watched in its turn.
	found := false
	for _, target := range f.list() {
		found = found || target.path == w.Root()
	}
	if !found {
		t.Fatal("the new file is not enrolled")
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, "..", notice{kind: kindLost})
	next(Event{Op: Remove, Path: "."})
	if _, err := w.Next(context.Background()); err != io.EOF || w.Err() != nil {
		t.Fatal(err, w.Err())
	}
}

// What Ignore ignores is never enrolled, counted or reported.
func TestIgnore(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"node_modules/a/b/c", "node_modules/d", "src/main.go", "debug.log"} {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var asked []string
	ignore := Patterns("node_modules/", "*.log")
	// Only the root, src, src/main.go and src/new.go, created below, count.
	w := fakeWatch(t, dir, Options{Recursive: true, Debounce: -1, MaxEntries: 4, Ignore: func(p string, isDir bool) bool {
		asked = append(asked, p)
		return ignore(p, isDir)
	}})
	for _, p := range asked {
		if strings.HasPrefix(p, "node_modules/") {
			t.Errorf("asked about %s", p)
		}
	}
	for _, target := range w.native.(*fakeNative).list() {
		if strings.Contains(target.path, "node_modules") || strings.HasSuffix(target.path, ".log") {
			t.Errorf("%s enrolled", target.path)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "e"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.log"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{name: "other.log", kind: kindCreate})
	notifyAt(t, w, ".", notice{name: "node_modules", kind: kindWrite})
	if err := os.WriteFile(filepath.Join(dir, "src", "new.go"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, "src", notice{name: "new.go", kind: kindCreate})
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Create, Path: "src/new.go"}}) {
		t.Fatalf("batch = %+v", got)
	}
}

// Notices in an ignored directory, which a subtree target reports, are
// dropped.
func TestIgnoreSubtree(t *testing.T) {
	configureFake(t, true, false, nil)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0700); err != nil {
		t.Fatal(err)
	}
	w := fakeWatch(t, dir, Options{Recursive: true, Debounce: -1, Ignore: Patterns(".git/")})
	if err := os.WriteFile(filepath.Join(dir, ".git", "objects", "x"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{name: ".git/objects/x", kind: kindCreate})
	if err := os.WriteFile(filepath.Join(dir, "y"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{name: "y", kind: kindCreate})
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Create, Path: "y"}}) {
		t.Fatalf("batch = %+v", got)
	}
}

// A backend that watches files through their directories is given only
// directories, and what changes in a file is named by its directory's
// target.
func TestNoFiles(t *testing.T) {
	configureFake(t, false, true, nil)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"f", "a/g", "a/b/h"} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w := fakeWatch(t, dir, Options{Recursive: true, Debounce: -1})
	for _, target := range w.native.(*fakeNative).list() {
		if !target.dir {
			t.Errorf("file %s enrolled", target.path)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b", "h"), []byte("xy"), 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, "a/b", notice{name: "h", kind: kindWrite})
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Write, Path: "a/b/h"}}) {
		t.Fatalf("batch = %+v", got)
	}
}

// Permissions alone are not writes; with Attributes they are reported as
// such.
func TestMetadata(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has only the read-only attribute in a mode")
	}
	for _, attributes := range []bool{false, true} {
		dir := t.TempDir()
		file := filepath.Join(dir, "a")
		if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		w := fakeWatch(t, dir, Options{Debounce: -1, Attributes: attributes})
		if err := os.Chmod(file, 0400); err != nil {
			t.Fatal(err)
		}
		notifyAt(t, w, "a", notice{})
		if attributes {
			if got := batch(t, w); !slices.Equal(got, []Event{{Op: Attrib, Path: "a"}}) {
				t.Fatalf("batch = %+v", got)
			}
			continue
		}
		// The next batch is the write's alone.
		notifyAt(t, w, "a", notice{kind: kindWrite})
		if got := batch(t, w); !slices.Equal(got, []Event{{Op: Write, Path: "a"}}) {
			t.Fatalf("batch = %+v", got)
		}
	}
}

// What the watch can't look at, as a directory without permission or a
// file the system holds (pagefile.sys), is skipped rather than ending the
// watch.
func TestInaccessible(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "busy"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	locked := runtime.GOOS != "windows" && os.Geteuid() != 0
	if locked {
		for _, p := range []string{"locked/a", "unsearchable/b"} {
			if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, p), nil, 0600); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Chmod(filepath.Join(dir, "locked"), 0); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(filepath.Join(dir, "unsearchable"), 0400); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			os.Chmod(filepath.Join(dir, "locked"), 0700)
			os.Chmod(filepath.Join(dir, "unsearchable"), 0700)
		})
	}
	var busy error = fs.ErrPermission
	if runtime.GOOS == "windows" {
		busy = syscall.Errno(32) // ERROR_SHARING_VIOLATION
	}
	configureFake(t, false, false, func(target target) error {
		if filepath.Base(target.path) == "busy" {
			return busy
		}
		return nil
	})
	w := fakeWatch(t, dir, Options{Recursive: true, Debounce: -1})
	for _, target := range w.native.(*fakeNative).list() {
		if filepath.Base(target.path) == "busy" {
			t.Error("busy enrolled")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "x"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	notifyAt(t, w, ".", notice{name: "x", kind: kindCreate})
	notifyAt(t, w, ".", notice{})
	if got := batch(t, w); !slices.Equal(got, []Event{{Op: Create, Path: "x"}}) {
		t.Fatalf("batch = %+v", got)
	}
	if locked {
		notifyAt(t, w, "unsearchable", notice{})
		if err := os.Remove(filepath.Join(dir, "x")); err != nil {
			t.Fatal(err)
		}
		notifyAt(t, w, ".", notice{name: "x"})
		if got := batch(t, w); !slices.Equal(got, []Event{{Op: Remove, Path: "x"}}) {
			t.Fatalf("batch = %+v", got)
		}
	}
}

func TestPatterns(t *testing.T) {
	ignore := Patterns("node_modules", "*.log", ".*", "tmp/", "/dist", "build/cache", "**/testdata", "src/**/gen", "logs/**")
	for _, tt := range []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"node_modules", true, true},
		{"a/b/node_modules", true, true},
		{"x.log", false, true},
		{"deep/x.log", false, true},
		{"x.logs", false, false},
		{".git", true, true},
		{"a/.DS_Store", false, true},
		{"tmp", true, true},
		{"tmp", false, false},
		{"a/tmp", true, true},
		{"dist", true, true},
		{"a/dist", true, false},
		{"build/cache", true, true},
		{"a/build/cache", true, false},
		{"testdata", true, true},
		{"a/b/testdata", true, true},
		{"src/gen", true, true},
		{"src/a/b/gen", true, true},
		{"other/gen", true, false},
		{"logs", true, false},
		{"logs/x", false, true},
		{"logs/a/b", false, true},
		{"main.go", false, false},
	} {
		if got := ignore(tt.path, tt.isDir); got != tt.want {
			t.Errorf("%s (dir %v): %v, want %v", tt.path, tt.isDir, got, tt.want)
		}
	}
	for _, bad := range []string{"", "/", "a//b", "[a"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q: no panic", bad)
				}
			}()
			Patterns(bad)
		}()
	}
}
