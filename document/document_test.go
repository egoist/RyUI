package document

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// The tests run the package on the fake host with its goroutines run at
// once, its clock and timers in the test's hands, and the dialogs and
// alerts answered by the test: what they check is the work done (files
// written, ops sent, questions asked), never how long it took.

type timer struct {
	at      time.Time
	f       func()
	stopped bool
}

type harness struct {
	t      *testing.T
	h      *fake.Host
	r      *ui.Root
	ds     *Documents[string]
	dir    string // the user's files
	clock  time.Time
	timers []*timer
	// saveAs answers the save dialogs, in order ("": cancel).
	saveAs  []string
	folders []string
	opened  []string // the open dialog's answer
	recent  []string
	quits   int // app.Quit's
	watched map[string]func()
	// views counts the documents' views alive.
	views int
}

func setup(t *testing.T, platform ops.Platform, opts Options[string]) *harness {
	t.Helper()
	x := &harness{t: t, dir: t.TempDir(), clock: time.Unix(1_800_000_000, 0), watched: map[string]func(){}}
	t.Setenv("ASUKA_DATA", filepath.Join(x.dir, ".data"))
	saved := []any{now, afterFunc, goIO, watcher, quitAgain, boot.Quit, boot.Reopen, boot.KeepRunning}
	t.Cleanup(func() {
		now, afterFunc, goIO, watcher, quitAgain = saved[0].(func() time.Time), saved[1].(func(time.Duration, func()) func()),
			saved[2].(func(func())), saved[3].(fileWatcher), saved[4].(func())
		boot.Quit, _ = saved[5].(func())
		boot.Reopen, _ = saved[6].(func() bool)
		boot.KeepRunning = saved[7].(bool)
	})
	now = func() time.Time { return x.clock }
	afterFunc = func(d time.Duration, f func()) func() {
		tm := &timer{at: x.clock.Add(d), f: f}
		x.timers = append(x.timers, tm)
		return func() { tm.stopped = true }
	}
	goIO = func(f func()) { f() }
	watcher = fakeWatcher{x}
	boot.Quit = func() { x.quits++ }
	quitAgain = func() {
		x.quits++
		boot.QuitRequested(false)
	}
	x.start(platform, opts)
	return x
}

// start mounts the app: a launch.
func (x *harness) start(platform ops.Platform, opts Options[string]) {
	t := x.t
	if opts.Types == nil {
		opts.Types = []Type{{Name: "Plain Text", Extensions: []string{"txt"}, MIMETypes: []string{"text/plain"}}}
	}
	opts.Read = func(r io.Reader) (string, error) { b, err := io.ReadAll(r); return string(b), err }
	if opts.Write == nil {
		opts.Write = func(w io.Writer, s string) error { _, err := io.WriteString(w, s); return err }
	}
	opts.View = func(d *Document[string]) ui.Node {
		x.views++
		signal.OnCleanup(func() { x.views-- })
		return ui.TextField(ui.TextFieldProps{Value: d.Content(), Multiline: true})
	}
	x.ds = New(opts)
	x.h = fake.New()
	x.h.Env.Platform = platform
	x.h.Modules = map[string]func(string, []byte) ([]byte, error){
		"dialog": func(method string, b []byte) ([]byte, error) {
			switch method {
			case "save":
				if len(x.saveAs) == 0 {
					t.Fatalf("an unexpected save dialog: %s", b)
				}
				p := x.saveAs[0]
				x.saveAs = x.saveAs[1:]
				return []byte(p), nil
			case "folder":
				p := x.folders[0]
				x.folders = x.folders[1:]
				return json.Marshal([]string{p})
			case "open":
				return json.Marshal(x.opened)
			}
			return nil, fmt.Errorf("no method %s", method)
		},
		"recent": func(method string, b []byte) ([]byte, error) {
			switch method {
			case "add":
				x.recent = append([]string{string(b)}, slices.DeleteFunc(x.recent, func(p string) bool { return p == string(b) })...)
			case "clear":
				x.recent = nil
			case "list":
				return json.Marshal(x.recent)
			}
			return nil, nil
		},
	}
	x.r = ui.Mount(x.h, x.ds.Root)
	t.Cleanup(func() {
		if x.r != nil {
			x.r.Dispose()
		}
	})
	x.settle()
}

// quit ends the app as a process ending does, after a quit was let
// through: the next start is the next launch.
func (x *harness) relaunch(platform ops.Platform, opts Options[string]) {
	x.ds.k.recovery.close()
	x.r.Dispose()
	x.r = nil
	x.timers = nil
	x.start(platform, opts)
}

// settle runs what was posted, and flushes.
func (x *harness) settle() {
	for range 100 {
		x.h.RunPosted()
		x.r.Flush()
		x.h.RunPosted()
		if !x.pending() {
			return
		}
	}
	x.t.Fatal("never settled")
}

func (x *harness) pending() bool {
	for _, b := range x.ds.k.docs.Peek() {
		if b.busy {
			return true
		}
	}
	return false
}

// wait moves the clock on by d, firing the timers due.
func (x *harness) wait(d time.Duration) {
	x.clock = x.clock.Add(d)
	for {
		fired := false
		for _, tm := range slices.Clone(x.timers) {
			if !tm.stopped && !tm.at.After(x.clock) {
				tm.stopped = true
				tm.f()
				fired = true
			}
		}
		x.settle()
		if !fired {
			return
		}
	}
}

func (x *harness) docs() []*Document[string] { return signal.Untrack(x.ds.Documents().Get) }

func (x *harness) doc(i int) *Document[string] {
	x.t.Helper()
	ds := x.docs()
	if i >= len(ds) {
		x.t.Fatalf("%d documents, not %d", len(ds), i+1)
	}
	return ds[i]
}

// windows are the document windows open, by title.
func (x *harness) windows() []string {
	var out []string
	for _, v := range x.h.OfKind(ops.KindWindow) {
		out = append(out, v.String(ops.PropWindowTitle))
	}
	slices.Sort(out)
	return out
}

func (x *harness) window(d *Document[string]) *fake.View {
	x.t.Helper()
	for _, v := range x.h.OfKind(ops.KindWindow) {
		if v.String(ops.PropWindowTitle) == x.ds.k.title(d.Name()) {
			return v
		}
	}
	x.t.Fatalf("no window for %s: %v", d.Name(), x.windows())
	return nil
}

// alert is the alert on screen, by its title; "" none.
func (x *harness) alert() (*fake.View, string) {
	for _, v := range x.h.Presented() {
		if v.Kind == ops.KindAlert {
			return v, v.String(ops.PropAlertTitle)
		}
	}
	return nil, ""
}

// answer presses the alert's button titled button.
func (x *harness) answer(title, button string) {
	x.t.Helper()
	v, got := x.alert()
	if v == nil || !strings.Contains(got, title) {
		x.t.Fatalf("alert %q, want one saying %q", got, title)
	}
	i := slices.Index(v.Actions(ops.PropAlertButtons), button)
	if i < 0 {
		x.t.Fatalf("alert %q has no %q: %q", got, button, v.Actions(ops.PropAlertButtons))
	}
	x.h.Choose(v, i)
	x.settle()
}

func (x *harness) noAlert() {
	x.t.Helper()
	if _, got := x.alert(); got != "" {
		x.t.Fatalf("an alert: %q", got)
	}
}

// menu chooses the File menu's item titled title.
func (x *harness) menu(title string) {
	x.t.Helper()
	for i, it := range x.h.MenuBar() {
		if it.Title == title {
			if it.Flags&byte(ops.MenuDisabled) != 0 {
				x.t.Fatalf("%q is disabled", title)
			}
			x.h.ChooseMenuItem(i)
			x.settle()
			return
		}
	}
	x.t.Fatalf("no menu item %q", title)
}

func (x *harness) menuItem(title string) fake.MenuBarItem {
	x.t.Helper()
	for _, it := range x.h.MenuBar() {
		if it.Title == title {
			return it
		}
	}
	x.t.Fatalf("no menu item %q", title)
	return fake.MenuBarItem{}
}

func (x *harness) file(name, content string) string {
	p := filepath.Join(x.dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		x.t.Fatal(err)
	}
	return p
}

func (x *harness) read(path string) string {
	x.t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		x.t.Fatal(err)
	}
	return string(b)
}

func (x *harness) set(d *Document[string], s string) {
	d.Set(s)
	x.settle()
}

// copies are the recovery copies kept, by name or path.
func (x *harness) copies() []string {
	var out []string
	root, _ := x.ds.k.recovery.root()
	dirs, _ := os.ReadDir(root)
	for _, d := range dirs {
		b, err := os.ReadFile(filepath.Join(root, d.Name(), "index.json"))
		if err != nil {
			continue
		}
		var es []recoveryEntry
		json.Unmarshal(b, &es)
		for _, e := range es {
			out = append(out, e.Name+e.Path)
		}
	}
	slices.Sort(out)
	return out
}

type fakeWatcher struct{ x *harness }

func (w fakeWatcher) watch(path string, changed func()) func() {
	w.x.watched[path] = changed
	return func() { delete(w.x.watched, path) }
}

// focus brings d's window to the front, as the user clicking it.
func (x *harness) focus(d *Document[string]) {
	v := x.window(d)
	x.h.SetKeyWindow(v)
	w := x.h.Window(v)
	for _, o := range x.h.OfKind(ops.KindWindow) {
		if o != v {
			if ow := x.h.Window(o); ow != nil && ow.State.Flags&ops.WindowFocused != 0 {
				s := ow.State
				s.Flags &^= ops.WindowFocused
				x.h.SetWindowState(o, s)
			}
		}
	}
	s := w.State
	s.Flags |= ops.WindowFocused
	if w.State.Flags&ops.WindowFocused != 0 { // blur, then focus again
		s.Flags &^= ops.WindowFocused
		x.h.SetWindowState(v, s)
		s.Flags |= ops.WindowFocused
	}
	x.h.SetWindowState(v, s)
	x.settle()
}

// A launch with nothing to open makes an untitled document; edits mark
// it edited (the window's dot), keep a recovery copy a second later, and
// closing it asks to keep it: Save… saves it where the user says, and
// the window closes, its owner disposed.
func TestUntitled(t *testing.T) {
	x := setup(t, ui.PlatformMacOS, Options[string]{Equal: func(a, b string) bool { return a == b }})
	if got := x.windows(); !slices.Equal(got, []string{"Untitled"}) {
		t.Fatalf("windows %q", got)
	}
	d := x.doc(0)
	w := x.window(d)
	if cfg := ops.DecodeWindowConfig(w.Props[ops.PropWindowConfig].Bytes); cfg.Flags&ops.WindowMenuBar == 0 || cfg.StateKey != "" {
		t.Fatalf("config %+v", cfg)
	}
	if w.Bool(ops.PropWindowEdited) {
		t.Fatal("edited from the start")
	}
	x.set(d, "hello")
	if !w.Bool(ops.PropWindowEdited) || !d.Edited() {
		t.Fatal("not edited after an edit")
	}
	// Back to what it was: unedited (Equal).
	x.set(d, "")
	if w.Bool(ops.PropWindowEdited) {
		t.Fatal("edited after undoing the edit")
	}
	x.set(d, "hello")
	x.wait(time.Second)
	if got := x.copies(); !slices.Equal(got, []string{"Untitled"}) {
		t.Fatalf("recovery copies %q", got)
	}
	// Closing asks; Cancel keeps it open.
	x.h.CloseWindow(w)
	x.settle()
	x.answer("keep this new document “Untitled”", "Cancel")
	if len(x.docs()) != 1 {
		t.Fatal("closed after Cancel")
	}
	target := filepath.Join(x.dir, "hello.txt")
	x.saveAs = []string{filepath.Join(x.dir, "hello")} // the extension is added
	x.h.CloseWindow(w)
	x.settle()
	x.answer("keep this new document", "Save…")
	if got := x.read(target); got != "hello" {
		t.Fatalf("saved %q", got)
	}
	if len(x.docs()) != 0 || x.views != 0 {
		t.Fatalf("%d documents, %d views after closing", len(x.docs()), x.views)
	}
	if got := x.copies(); len(got) != 0 {
		t.Fatalf("recovery copies after saving: %q", got)
	}
	if x.quits != 0 {
		t.Fatal("a Mac's app quit with its last document")
	}
	if !slices.Equal(x.recent, []string{target}) {
		t.Fatalf("recent %q", x.recent)
	}
}

// A file the system opens the app with opens in its window, named after
// it; edits are saved in place a moment after they stop, and on closing
// with no question. Opening it again brings its window to the front.
func TestAutosaveInPlace(t *testing.T) {
	x := setup(t, ui.PlatformMacOS, Options[string]{NoUntitled: true})
	if len(x.docs()) != 0 {
		t.Fatal("an untitled document with NoUntitled")
	}
	p := x.file("notes.txt", "one")
	x.h.OpenFiles(p)
	x.settle()
	d := x.doc(0)
	if d.Content().Peek() != "one" || d.Path() != p {
		t.Fatalf("content %q at %q", d.Content().Peek(), d.Path())
	}
	w := x.window(d)
	if w.String(ops.PropWindowFile) != p {
		t.Fatalf("the window's file %q", w.String(ops.PropWindowFile))
	}
	if cfg := ops.DecodeWindowConfig(w.Props[ops.PropWindowConfig].Bytes); cfg.StateKey != "document:"+p {
		t.Fatalf("state key %q", cfg.StateKey)
	}
	x.set(d, "two")
	x.wait(4 * time.Second)
	if x.read(p) != "one" {
		t.Fatal("saved before the delay")
	}
	x.wait(time.Second)
	if x.read(p) != "two" || d.Edited() {
		t.Fatalf("after the delay: %q, edited %v", x.read(p), d.Edited())
	}
	if len(x.copies()) != 0 {
		t.Fatal("a recovery copy of a document saved in place")
	}
	// Again: one window.
	x.h.OpenFiles(p)
	x.settle()
	if len(x.docs()) != 1 {
		t.Fatalf("%d documents after opening it again", len(x.docs()))
	}
	acts := x.h.Window(w).Actions
	if len(acts) == 0 || acts[len(acts)-1] != ops.WindowActionFocus {
		t.Fatalf("its window's actions %v", acts)
	}
	// Closing saves it, without asking.
	x.set(d, "three")
	x.h.CloseWindow(w)
	x.settle()
	x.noAlert()
	if x.read(p) != "three" || len(x.docs()) != 0 {
		t.Fatalf("after closing: %q, %d documents", x.read(p), len(x.docs()))
	}
}

// Without autosave, closing an edited document asks to save it: Don't
// Save closes it, its file as it was and no copy left; Save saves it.
func TestAutosaveOff(t *testing.T) {
	x := setup(t, ui.PlatformWindows, Options[string]{Autosave: AutosaveOff, NoUntitled: true})
	p := x.file("a.txt", "a")
	x.h.OpenFiles(p)
	x.settle()
	d := x.doc(0)
	if got := x.windows(); len(got) != 1 || got[0] != "a.txt - "+appName() {
		t.Fatalf("windows %q", got)
	}
	x.set(d, "b")
	x.wait(10 * time.Second)
	if x.read(p) != "a" {
		t.Fatal("saved without autosave")
	}
	if got := x.copies(); !slices.Equal(got, []string{p}) {
		t.Fatalf("copies %q", got)
	}
	x.h.CloseWindow(x.window(d))
	x.settle()
	x.answer("save the changes made to the document “a.txt”", "Don’t Save")
	if x.read(p) != "a" || len(x.docs()) != 0 || len(x.copies()) != 0 {
		t.Fatalf("after Don't Save: %q, %d documents, copies %q", x.read(p), len(x.docs()), x.copies())
	}
	// Windows apps quit with their last document.
	if x.quits != 1 {
		t.Fatalf("%d quits", x.quits)
	}
	x.h.OpenFiles(p)
	x.settle()
	d = x.doc(0)
	x.set(d, "c")
	x.h.CloseWindow(x.window(d))
	x.settle()
	x.answer("save the changes", "Save")
	if x.read(p) != "c" || len(x.docs()) != 0 {
		t.Fatalf("after Save: %q", x.read(p))
	}
}

// The File menu acts on the document in use: Save, Duplicate, Revert to
// Saved, Rename…, Move To…, and Close closes its window as the user
// does; Open… opens what the dialog gives.
func TestFileMenu(t *testing.T) {
	x := setup(t, ui.PlatformMacOS, Options[string]{Autosave: AutosaveOff, NoUntitled: true})
	var titles []string
	for _, it := range x.h.MenuBar() {
		if it.Depth == 0 && it.Flags&byte(ops.MenuSeparator) == 0 {
			titles = append(titles, it.Title)
		}
	}
	want := []string{"New", "Open…", "Open Recent", "Close", "Save", "Duplicate", "Save As…", "Rename…", "Move To…", "Revert to Saved"}
	if !slices.Equal(titles, want) {
		t.Fatalf("File menu %q", titles)
	}
	if it := x.menuItem("Close"); it.Role != ops.MenuRoleClose || it.Key != "w" {
		t.Fatalf("Close %+v", it)
	}
	if it := x.menuItem("Open Recent"); it.Role != ops.MenuRoleOpenRecent {
		t.Fatalf("Open Recent %+v", it)
	}
	if it := x.menuItem("Save"); it.Flags&byte(ops.MenuDisabled) == 0 {
		t.Fatal("Save enabled with no document")
	}
	p := x.file("a.txt", "a")
	x.opened = []string{p}
	x.menu("Open…")
	d := x.doc(0)
	x.set(d, "edited")
	x.menu("Save")
	if x.read(p) != "edited" || d.Edited() {
		t.Fatal("Save didn't save")
	}
	x.set(d, "again")
	x.menu("Revert to Saved")
	x.answer("revert “a.txt”", "Revert")
	if d.Content().Peek() != "edited" || d.Edited() {
		t.Fatalf("after Revert: %q", d.Content().Peek())
	}
	x.menu("Duplicate")
	if got := x.windows(); !slices.Equal(got, []string{"a copy", "a.txt"}) {
		t.Fatalf("windows %q", got)
	}
	dup := x.doc(1)
	if dup.Content().Peek() != "edited" || !dup.Edited() || dup.Path() != "" {
		t.Fatal("the duplicate isn't an edited untitled copy")
	}
	// Close closes the window in use: the copy's, which asks.
	x.menu("Close")
	x.answer("save the changes made to the document “a copy”", "Don’t Save")
	if len(x.docs()) != 1 {
		t.Fatal("Close closed nothing")
	}
	x.focus(d)
	x.saveAs = []string{filepath.Join(x.dir, "b.txt")}
	x.menu("Rename…")
	if d.Path() != filepath.Join(x.dir, "b.txt") || x.read(d.Path()) != "edited" {
		t.Fatalf("renamed to %q", d.Path())
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("the old file is still there")
	}
	sub := filepath.Join(x.dir, "sub")
	os.Mkdir(sub, 0o755)
	x.folders = []string{sub}
	x.menu("Move To…")
	if d.Path() != filepath.Join(sub, "b.txt") {
		t.Fatalf("moved to %q", d.Path())
	}
	if x.window(d).String(ops.PropWindowFile) != d.Path() {
		t.Fatal("the window's file didn't follow")
	}
	x.saveAs = []string{filepath.Join(x.dir, "c.txt")}
	x.set(d, "as")
	x.menu("Save As…")
	if d.Path() != filepath.Join(x.dir, "c.txt") || x.read(d.Path()) != "as" || x.read(filepath.Join(sub, "b.txt")) != "edited" {
		t.Fatal("Save As didn't make the document its new file")
	}
}

// Files other apps change: an unedited document reads its file again
// (when its window comes to the front, or its watch says); an edited one
// is in conflict, and saving asks: Save Anyway replaces the file. A file
// renamed is followed; one deleted leaves the document Deleted, and
// saving writes it again.
func TestChangedOnDisk(t *testing.T) {
	x := setup(t, ui.PlatformMacOS, Options[string]{Autosave: AutosaveOff, NoUntitled: true})
	p := x.file("a.txt", "a")
	x.h.OpenFiles(p)
	x.settle()
	d := x.doc(0)
	os.WriteFile(p, []byte("from another app"), 0o644)
	os.Chtimes(p, time.Now(), time.Now().Add(time.Hour))
	x.watched[p]()
	x.settle()
	if d.Content().Peek() != "from another app" || d.Edited() {
		t.Fatalf("after another app's change: %q", d.Content().Peek())
	}
	x.set(d, "mine")
	os.WriteFile(p, []byte("theirs"), 0o644)
	os.Chtimes(p, time.Now(), time.Now().Add(2*time.Hour))
	x.focus(d)
	if s := d.State().Get(); !s.Conflict || d.Content().Peek() != "mine" {
		t.Fatalf("state %+v, content %q", s, d.Content().Peek())
	}
	x.menu("Save")
	x.answer("“a.txt” has been changed by another application", "Save Anyway")
	if x.read(p) != "mine" || d.State().Get().Conflict {
		t.Fatalf("after Save Anyway: %q", x.read(p))
	}
	// Renamed in its folder: followed.
	q := filepath.Join(x.dir, "renamed.txt")
	os.Rename(p, q)
	x.watched[p]()
	x.settle()
	if real, _ := filepath.EvalSymlinks(q); real != "" && d.Path() == real {
		q = real // a Mac finds it by its identity, at its real path
	}
	if d.Path() != q || x.window(d).String(ops.PropWindowTitle) != "renamed.txt" {
		t.Fatalf("after a rename: %q", d.Path())
	}
	if x.watched[q] == nil {
		t.Fatal("not watching the renamed file")
	}
	// Deleted: Deleted, and Save writes it again.
	os.Remove(q)
	x.watched[q]()
	x.settle()
	if !d.State().Get().Deleted {
		t.Fatal("not Deleted")
	}
	x.set(d, "back")
	x.menu("Save")
	if x.read(q) != "back" || d.State().Get().Deleted {
		t.Fatal("Save didn't write it again")
	}
}

// A read-only file: Save offers Save As….
func TestReadOnly(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root writes any file")
	}
	x := setup(t, ui.PlatformLinux, Options[string]{NoUntitled: true})
	p := x.file("ro.txt", "ro")
	os.Chmod(p, 0o444)
	x.h.OpenFiles(p)
	x.settle()
	d := x.doc(0)
	if !d.State().Get().ReadOnly {
		t.Fatal("not read-only")
	}
	x.set(d, "changed")
	x.wait(10 * time.Second)
	if x.read(p) != "ro" {
		t.Fatal("autosaved a read-only file")
	}
	x.saveAs = []string{filepath.Join(x.dir, "rw.txt")}
	x.menu("Save")
	x.answer("can’t be saved because its file is read-only", "Save As…")
	if d.Path() != filepath.Join(x.dir, "rw.txt") || x.read(d.Path()) != "changed" || d.State().Get().ReadOnly {
		t.Fatalf("after Save As: %+v", d.State().Get())
	}
}

// Quitting keeps the documents: those saved in place are saved, the
// others' changes stay in their recovery copies, and the next launch
// opens them, edited (a file changed meanwhile in conflict).
func TestQuitKeepsDocuments(t *testing.T) {
	x := setup(t, ui.PlatformMacOS, Options[string]{})
	untitled := x.doc(0)
	x.set(untitled, "draft")
	p := x.file("a.txt", "a")
	x.h.OpenFiles(p)
	x.settle()
	saved := x.doc(1)
	x.set(saved, "in place")
	q := x.file("b.txt", "b")
	os.Chmod(q, 0o444)
	x.h.OpenFiles(q)
	x.settle()
	ro := x.doc(2)
	x.set(ro, "kept")
	// The quit waits for the writes, then goes on.
	boot.QuitRequested(false)
	x.settle()
	x.noAlert()
	if x.quits != 1 {
		t.Fatalf("%d quits", x.quits)
	}
	if x.read(p) != "in place" {
		t.Fatal("not saved in place at quit")
	}
	want := []string{"Untitled", q}
	slices.Sort(want)
	if got := x.copies(); !slices.Equal(got, want) {
		t.Fatalf("copies %q", got)
	}
	os.Chmod(q, 0o644)
	x.relaunch(ui.PlatformMacOS, Options[string]{})
	got := map[string]string{}
	for _, d := range x.docs() {
		got[d.Name()] = d.Content().Peek()
		if !d.Edited() {
			t.Errorf("%s isn't edited", d.Name())
		}
	}
	if len(got) != 2 || got["Untitled"] != "draft" || got["b.txt"] != "kept" {
		t.Fatalf("restored %v", got)
	}
	// The copies are this session's now: one each.
	if c := x.copies(); !slices.Equal(c, want) {
		t.Fatalf("copies after the relaunch %q", c)
	}
}

// AskOnQuit: one document is asked about as its window's close; several,
// on a Mac, after asking whether to review them (Review Changes… asks
// each, Cancel keeps the app); on Windows, in one question naming them.
func TestAskOnQuit(t *testing.T) {
	x := setup(t, ui.PlatformMacOS, Options[string]{AskOnQuit: true, Autosave: AutosaveOff, NoUntitled: true})
	a, b := x.file("a.txt", "a"), x.file("b.txt", "b")
	x.h.OpenFiles(a, b)
	x.settle()
	x.set(x.doc(0), "A")
	x.set(x.doc(1), "B")
	if !boot.QuitRequested(false) {
		t.Fatal("the quit wasn't canceled to ask")
	}
	x.settle()
	x.answer("You have 2", "Cancel")
	if x.quits != 0 {
		t.Fatal("quit after Cancel")
	}
	boot.QuitRequested(false)
	x.settle()
	x.answer("You have 2", "Review Changes…")
	x.answer("“a.txt”", "Save")
	x.answer("“b.txt”", "Don’t Save")
	if x.quits != 1 || x.read(a) != "A" || x.read(b) != "b" {
		t.Fatalf("%d quits, %q %q", x.quits, x.read(a), x.read(b))
	}

	x.r.Dispose()
	x.r = nil
	y := setup(t, ui.PlatformWindows, Options[string]{AskOnQuit: true, Autosave: AutosaveOff, NoUntitled: true})
	c, e := y.file("c.txt", "c"), y.file("e.txt", "e")
	y.h.OpenFiles(c, e)
	y.settle()
	y.set(y.doc(0), "C")
	y.set(y.doc(1), "E")
	boot.QuitRequested(false)
	y.settle()
	y.answer("the following 2 documents", "Save All")
	if y.quits != 1 || y.read(c) != "C" || y.read(e) != "E" {
		t.Fatalf("%d quits, %q %q", y.quits, y.read(c), y.read(e))
	}
}

// The session ending can't wait: documents are saved or kept at once.
func TestSessionEnding(t *testing.T) {
	x := setup(t, ui.PlatformWindows, Options[string]{})
	x.set(x.doc(0), "draft")
	if boot.QuitRequested(true) {
		t.Fatal("canceled the session's end")
	}
	if got := x.copies(); !slices.Equal(got, []string{"Untitled"}) {
		t.Fatalf("copies %q", got)
	}
}

// A Mac's Dock icon clicked with no window makes a document.
func TestReopen(t *testing.T) {
	x := setup(t, ui.PlatformMacOS, Options[string]{})
	if !boot.KeepRunning {
		t.Fatal("a Mac's document app doesn't keep running with no window")
	}
	x.h.CloseWindow(x.window(x.doc(0)))
	x.settle()
	if len(x.docs()) != 0 {
		t.Fatal("not closed")
	}
	if !boot.Reopen() {
		t.Fatal("Reopen not handled")
	}
	x.settle()
	if got := x.windows(); !slices.Equal(got, []string{"Untitled"}) {
		t.Fatalf("windows %q", got)
	}
}

// Windows and Linux: Open Recent is the app's, from the recent documents.
func TestOpenRecentItems(t *testing.T) {
	x := setup(t, ui.PlatformLinux, Options[string]{NoUntitled: true})
	p := x.file("a.txt", "a")
	x.h.OpenFiles(p)
	x.settle()
	var sub []string
	for _, it := range x.h.MenuBar() {
		if it.Depth == 1 {
			sub = append(sub, it.Title)
		}
	}
	if !slices.Equal(sub, []string{"a.txt", "", "Clear Menu"}) {
		t.Fatalf("Open Recent %q", sub)
	}
	if items := x.h.MenuBar(); items[len(items)-1].Role != ops.MenuRoleQuit {
		t.Fatalf("the File menu ends with %+v, not Quit", items[len(items)-1])
	}
}

// Phones and the web: a list of the app's documents; New makes a file in
// it at once and shows its screen; edits are saved in place; going back
// closes it. Opening another document goes back first.
func TestMobile(t *testing.T) {
	x := setup(t, ui.PlatformIOS, Options[string]{})
	if len(x.docs()) != 0 || len(x.h.OfKind(ops.KindWindow)) != 0 {
		t.Fatal("a document, or a window, at launch on a phone")
	}
	stack := x.h.OfKind(ops.KindStack)[0]
	screen := x.h.OfKind(ops.KindScreen)[0]
	if got := screen.Actions(ops.PropScreenActions); !slices.Equal(got, []string{"Open…", "New"}) {
		t.Fatalf("actions %q", got)
	}
	x.h.Choose(screen, 1)
	x.settle()
	d := x.doc(0)
	dir := x.ds.k.docsDir
	if d.Path() != filepath.Join(dir, "Untitled.txt") || x.read(d.Path()) != "" {
		t.Fatalf("new document at %q", d.Path())
	}
	if n := len(x.h.OfKind(ops.KindScreen)); n != 2 {
		t.Fatalf("%d screens", n)
	}
	x.set(d, "on the go")
	x.wait(5 * time.Second)
	if x.read(d.Path()) != "on the go" {
		t.Fatal("not saved in place")
	}
	x.set(d, "and back")
	x.h.Back(stack)
	x.settle()
	if x.read(filepath.Join(dir, "Untitled.txt")) != "and back" || len(x.docs()) != 0 || x.views != 0 {
		t.Fatalf("after going back: %q, %d documents", x.read(filepath.Join(dir, "Untitled.txt")), len(x.docs()))
	}
	if got := x.ds.k.files.Peek(); len(got) != 1 || got[0].Name != "Untitled.txt" {
		t.Fatalf("the list %+v", got)
	}
	// A second new one is numbered.
	x.h.Choose(screen, 1)
	x.settle()
	if got := x.doc(0).Path(); got != filepath.Join(dir, "Untitled 2.txt") {
		t.Fatalf("second document %q", got)
	}
	// A file from elsewhere is copied in.
	p := x.file("shared.txt", "shared")
	x.h.OpenFiles(p)
	x.settle()
	if got := x.docs(); len(got) != 1 || got[0].Path() != filepath.Join(dir, "shared.txt") || got[0].Content().Peek() != "shared" {
		t.Fatalf("documents %v", got)
	}
	if n := len(x.h.OfKind(ops.KindScreen)); n != 2 {
		t.Fatalf("%d screens: not one document at a time", n)
	}
}

// The words follow the app's language, and untitled names read back.
func TestWords(t *testing.T) {
	if untitledName(1) != "Untitled" || untitledName(3) != "Untitled 3" {
		t.Fatalf("%q %q", untitledName(1), untitledName(3))
	}
	if untitledNumber("Untitled") != 1 || untitledNumber("Untitled 12") != 12 || untitledNumber("Notes") != 0 {
		t.Fatal("untitledNumber")
	}
}

// Every translation has every word, and says them in its language.
func TestTranslations(t *testing.T) {
	var en map[string]string
	b, _ := files.ReadFile("messages/en.json")
	json.Unmarshal(b, &en)
	names, _ := files.ReadDir("messages")
	for _, n := range names {
		var m map[string]string
		b, _ := files.ReadFile("messages/" + n.Name())
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", n.Name(), err)
		}
		for k := range en {
			if m[k] == "" {
				t.Errorf("%s has no %q", n.Name(), k)
			}
		}
	}
	if got := catalog.Format("de-AT", "untitledNumbered", map[string]any{"n": 2}); got != "Ohne Titel 2" {
		t.Errorf("German: %q", got)
	}
	if got := catalog.Format("ja", "saveChanges", map[string]any{"name": "a.txt"}); !strings.Contains(got, "a.txt") || !strings.Contains(got, "保存") {
		t.Errorf("Japanese: %q", got)
	}
	if got := catalog.Format("sw", "untitled", nil); got != "Untitled" {
		t.Errorf("a language without them: %q", got)
	}
}

// A mutable content: Changed edits the document, and Snapshot copies it
// on the main thread before it's written, so an edit made while it's
// written isn't in the file and leaves the document edited.
func TestMutableContent(t *testing.T) {
	type model struct{ lines []string }
	x := setup(t, ui.PlatformMacOS, Options[string]{NoUntitled: true})
	p := x.file("m.txt", "one")
	snapshots := 0
	var during func()
	ds := New(Options[*model]{
		Types: []Type{{Name: "Lines", Extensions: []string{"txt"}}},
		Read: func(r io.Reader) (*model, error) {
			b, err := io.ReadAll(r)
			return &model{lines: strings.Split(string(b), "\n")}, err
		},
		Write: func(w io.Writer, m *model) error {
			if during != nil {
				during()
			}
			_, err := io.WriteString(w, strings.Join(m.lines, "\n"))
			return err
		},
		Snapshot:   func(m *model) *model { snapshots++; return &model{lines: slices.Clone(m.lines)} },
		View:       func(d *Document[*model]) ui.Node { return ui.StaticText("lines") },
		NoUntitled: true,
	})
	x.r.Dispose()
	x.h = fake.New()
	x.h.Env.Platform = ui.PlatformMacOS
	x.h.Modules = map[string]func(string, []byte) ([]byte, error){"recent": func(string, []byte) ([]byte, error) { return []byte("[]"), nil }}
	x.r = ui.Mount(x.h, ds.Root)
	x.r.Flush()
	var d *Document[*model]
	ds.Open(p, func(doc *Document[*model], err error) { d = doc })
	for range 10 {
		x.h.RunPosted()
		x.r.Flush()
	}
	if d == nil {
		t.Fatal("not opened")
	}
	m := d.Content().Peek()
	m.lines = append(m.lines, "two")
	d.Changed()
	if !d.Edited() {
		t.Fatal("Changed didn't edit it")
	}
	// An edit while the file is written: on the goroutine, the snapshot
	// stays as it was taken.
	during = func() { m.lines = append(m.lines, "three") }
	d.Save(nil)
	for range 10 {
		x.h.RunPosted()
		x.r.Flush()
	}
	if got := x.read(p); got != "one\ntwo" || snapshots == 0 {
		t.Fatalf("wrote %q (%d snapshots)", got, snapshots)
	}
	d.Changed() // the edit made meanwhile, told
	if !d.Edited() {
		t.Fatal("the edit made while saving isn't unsaved")
	}
	// SetEdited(false): what's there is the file's now, without writing.
	d.SetEdited(false)
	if d.Edited() || x.read(p) != "one\ntwo" {
		t.Fatal("SetEdited(false)")
	}
}
