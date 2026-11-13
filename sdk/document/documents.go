package document

import (
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/sdk/recent"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Documents opens, creates and keeps track of the documents of one kind,
// as AppKit's NSDocumentController and SwiftUI's DocumentGroup do: make
// it with New, and run the app with Run (or mount Root as the app's
// root). Its methods run on the main thread.
type Documents[T any] struct {
	opts Options[T]
	k    *kind
}

// kind is what Documents of any content type share. Main thread only.
type kind struct {
	types         []Type
	autosave      Autosave
	autosaveDelay time.Duration
	askOnQuit     bool
	noUntitled    bool
	recovery      *recoveryStore

	// docs are the open documents, in the order they opened; current is
	// the one in use: its window's focused, or was last.
	docs    *signal.Signal[[]*base]
	current *signal.Signal[*base]
	// recent are the recent documents, for Open Recent where the host
	// has none of its own (Windows, Linux).
	recent *signal.Signal[[]string]

	mounted bool
	desktop bool // windows: macOS, Windows, Linux
	mac     bool
	// nav pushes a document's screen (phones, the web), docsDir is where
	// their documents are, files lists them.
	nav      *ui.Navigator
	onScreen *base
	docsDir  string
	files    *signal.Signal[[]fileItem]
	// view is a document's View.
	view func(b *base) ui.Node

	// starting counts what the launch opens (recovery copies, files):
	// an untitled document is made once none is left and nothing opened.
	starting int
	// quitting: a quit was let through, and the app is ending: windows
	// closing keep their documents' copies.
	quitting bool
	quit     struct{ running, resolved bool }

	create func(content any, name string) *base
	empty  func() any
	read   func(r io.Reader) (any, error)
}

// New makes the documents of one kind. Call it in main, then Run.
func New[T any](opts Options[T]) *Documents[T] {
	switch {
	case len(opts.Types) == 0:
		panic("document: New needs a Type")
	case opts.Read == nil || opts.Write == nil:
		panic("document: New needs Read and Write")
	case opts.View == nil:
		panic("document: New needs View")
	}
	if opts.AutosaveDelay <= 0 {
		opts.AutosaveDelay = 5 * time.Second
	}
	ds := &Documents[T]{opts: opts}
	k := &kind{
		types: opts.Types, autosave: opts.Autosave, autosaveDelay: opts.AutosaveDelay,
		askOnQuit: opts.AskOnQuit, noUntitled: opts.NoUntitled,
		recovery: newRecoveryStore(opts.Types),
		docs:     signal.NewFunc[[]*base](nil, nil),
		current:  signal.New[*base](nil),
		recent:   signal.NewFunc[[]string](nil, slices.Equal),
		files:    signal.NewFunc[[]fileItem](nil, slices.Equal),
	}
	k.create = func(content any, name string) *base {
		v, _ := content.(T)
		return ds.make(v, name).base
	}
	k.empty = func() any {
		if opts.New != nil {
			return opts.New()
		}
		var zero T
		return zero
	}
	k.read = func(r io.Reader) (any, error) { return opts.Read(r) }
	k.view = func(b *base) ui.Node { return opts.View(b.ops.(*Document[T])) }
	ds.k = k
	return ds
}

// Run runs the app with these documents as its windows (app.Run): on the
// desktop the main window stays hidden, and each document has a window
// of its own; on a phone and the web, the app shows its documents' list.
func (ds *Documents[T]) Run() {
	app.RunWindow(ui.WindowOptions{Hidden: true, Title: signal.Func(ds.k.appTitle)}, ds.Root)
}

// Root is the app's root of Run, for an app that runs itself (app.Run
// with a hidden main window on the desktop: ui.WindowOptions.Hidden).
func (ds *Documents[T]) Root() ui.Node {
	k := ds.k
	switch p := ui.Platform(); p {
	case ui.PlatformIOS, ui.PlatformAndroid, ui.PlatformWeb:
	default:
		k.desktop = true
		k.mac = p == ui.PlatformMacOS
	}
	k.mounted = true
	signal.OnCleanup(func() { k.mounted = false })
	if !k.desktop {
		root := ds.mobileRoot()
		k.start()
		return root
	}
	k.start()
	if k.mac {
		// Document apps stay open with no window, and a click on the Dock
		// icon then makes a document (applicationShouldHandleReopen:).
		boot.KeepRunning = true
		boot.Reopen = func() bool {
			post(func() {
				if len(k.docs.Peek()) == 0 && !k.noUntitled {
					k.newUntitled()
				} else if b := k.current.Peek(); b != nil {
					b.win.Focus()
				}
			})
			return true
		}
		signal.OnCleanup(func() { boot.Reopen = nil })
	}
	app.OnQuit(k.onQuit)
	return ui.View(ui.ViewProps{},
		ui.MenuBar(signal.Func(ds.menus)),
		ui.For[*base, *base](k.docs, func(b *base) *base { return b }, func(item signal.Accessor[*base], _ signal.Accessor[int]) ui.Node {
			return ds.window(item.Get().ops.(*Document[T]))
		}),
	)
}

// make makes a document of content v, untitled with name.
func (ds *Documents[T]) make(v T, name string) *Document[T] {
	d := &Document[T]{ds: ds, saved: v, savedKnown: true}
	d.base = &base{k: ds.k, ops: d, name: name, id: randomID()}
	d.content = signal.NewFunc(v, ds.opts.Equal)
	d.state = signal.New(State{})
	d.open = signal.New(true)
	d.win = ui.NewWindowHandle()
	signal.Root(func(dispose func()) {
		d.dispose = dispose
		first := true
		signal.RenderEffect(func() {
			d.content.Get()
			switch {
			case first:
				first = false
			case d.loading:
				d.loading = false
			default:
				d.edit()
			}
		})
	})
	return d
}

// window is a document's window.
func (ds *Documents[T]) window(d *Document[T]) ui.Node {
	b, k := d.base, ds.k
	key := ""
	if b.path != "" {
		key = "document:" + b.path
	}
	return ui.Window(ui.WindowProps{
		Open:      b.open,
		Handle:    b.win,
		MenuBar:   true,
		Title:     signal.Func(func() string { return k.title(b.state.Get().Name) }),
		Edited:    signal.Func(func() bool { return b.state.Get().Edited }),
		File:      signal.Func(func() string { return b.state.Get().Path }),
		Width:     ds.opts.Width,
		Height:    ds.opts.Height,
		MinWidth:  ds.opts.MinWidth,
		MinHeight: ds.opts.MinHeight,
		StateKey:  key,
		Content: func() ui.Node {
			// Its window in use: the menus act on it, and its file is
			// looked at (watches miss changes, VS Code and Notepad++ look
			// then too). Leaving it saves it in place.
			focused := false
			signal.RenderEffect(func() {
				f := b.win.Focused().Get()
				if f == focused {
					return
				}
				focused = f
				if f {
					k.current.Set(b)
					b.checkDisk()
				} else if b.edited && b.autosaves() {
					b.persist(nil)
				}
			})
			b.view = ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}, k.view(b))
			return b.view
		},
		OnCloseRequest: b.closeRequested,
		OnClose:        b.windowClosed,
	})
}

// title is a document window's title: its name, and the app's after it
// on Windows and Linux, as Notepad and gedit show them.
func (k *kind) title(name string) string {
	if k.mac || !k.desktop {
		return name
	}
	return name + " - " + appName()
}

// appTitle is the main window's title: on the web, the tab's, the
// document on screen's name.
func (k *kind) appTitle() string {
	if b := k.current.Get(); b != nil && !k.desktop {
		return b.state.Get().Name
	}
	return ""
}

// show opens b's window (a phone's screen) and starts following it.
func (k *kind) show(b *base) {
	b.notify()
	k.docs.Set(append(slices.Clone(k.docs.Peek()), b))
	k.current.Set(b)
	b.schedule()
	if b.path != "" {
		k.noteRecent(b.path)
		k.watch(b)
	}
	if !k.desktop {
		k.showScreen(b)
	}
}

// windowClosed closes b with its window.
func (b *base) windowClosed() {
	k := b.k
	if b.closed {
		return
	}
	b.closed = true
	if b.stopTimer != nil {
		b.stopTimer()
		b.stopTimer = nil
	}
	docs := slices.DeleteFunc(slices.Clone(k.docs.Peek()), func(x *base) bool { return x == b })
	k.docs.Set(docs)
	if k.current.Peek() == b {
		var next *base
		if n := len(docs); n > 0 {
			next = docs[n-1]
		}
		k.current.Set(next)
	}
	k.unwatch(b)
	if b.edited && !b.discarded {
		// Closed without asking (Destroy, the app ending): the changes
		// stay in the recovery copy, for the next launch.
		b.backup(nil)
	} else if b.backedUp != 0 || k.recovery.has(b.id) {
		b.dropCopy()
	}
	b.do(func() func() func() {
		if b.dispose != nil {
			b.dispose()
		}
		return nil
	})
	// Windows and Linux apps quit with their last document's window; a
	// Mac's stays open, its menu bar making the next.
	if k.desktop && !k.mac && len(docs) == 0 && !k.quitting && k.starting == 0 {
		app.Quit()
	}
}

// NewDocument makes an untitled document with Options.New's content and
// opens its window, as File > New does.
func (ds *Documents[T]) NewDocument() *Document[T] {
	return ds.k.newUntitled().ops.(*Document[T])
}

// newUntitled makes an untitled document. On a phone and the web it's a
// file in the app's documents at once, as their apps make one.
func (k *kind) newUntitled() *base {
	used := map[int]bool{}
	for _, b := range k.docs.Peek() {
		if b.path == "" {
			used[b.untitled] = true
		}
	}
	n := 1
	for used[n] {
		n++
	}
	b := k.create(k.empty(), untitledName(n))
	b.untitled = n
	if !k.desktop {
		b.path = k.freePath(untitledName(n))
		b.edited, b.dirtySince = true, now()
		k.show(b)
		b.write(b.path, writeSaveAs, func(err error) { k.listFiles() })
		return b
	}
	k.show(b)
	return b
}

// Open opens the document at path in its window, or brings its window to
// the front if it's open already; done, if set, gets the document or
// what failed.
func (ds *Documents[T]) Open(path string, done func(*Document[T], error)) {
	ds.k.open(path, func(b *base, err error) {
		if done == nil {
			return
		}
		if err != nil {
			done(nil, err)
			return
		}
		done(b.ops.(*Document[T]), nil)
	})
}

// open opens the document at path, or focuses it if it's open.
func (k *kind) open(path string, done func(*base, error)) {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if b := k.find(path); b != nil {
		k.focus(b)
		done(b, nil)
		return
	}
	k.starting++
	read := k.read
	goIO(func() {
		v, stamp, err := readFile(path, read)
		writable := err == nil && fileWritable(path)
		var info os.FileInfo
		if err == nil {
			info, _ = os.Stat(path)
		}
		post(func() {
			k.starting--
			if err != nil {
				done(nil, err)
				k.startedUp()
				return
			}
			if b := k.findInfo(path, info); b != nil { // opened meanwhile
				k.focus(b)
				done(b, nil)
				return
			}
			b := k.create(v, "")
			b.path, b.stamp, b.readOnly = path, stamp, !writable
			k.show(b)
			done(b, nil)
		})
	})
}

// find returns the open document of the file at path.
func (k *kind) find(path string) *base {
	info, _ := os.Stat(path)
	return k.findInfo(path, info)
}

func (k *kind) findInfo(path string, info os.FileInfo) *base {
	for _, b := range k.docs.Peek() {
		if b.path == "" {
			continue
		}
		if b.path == path {
			return b
		}
		if info != nil {
			if other, err := os.Stat(b.path); err == nil && os.SameFile(info, other) {
				return b
			}
		}
	}
	return nil
}

// focus brings b's window to the front (a phone: its screen).
func (k *kind) focus(b *base) {
	k.current.Set(b)
	if k.desktop {
		b.win.Focus()
		return
	}
	k.showScreen(b)
}

// Documents is the open documents, in the order they opened.
func (ds *Documents[T]) Documents() signal.Accessor[[]*Document[T]] {
	return signal.Func(func() []*Document[T] {
		var out []*Document[T]
		for _, b := range ds.k.docs.Get() {
			out = append(out, b.ops.(*Document[T]))
		}
		return out
	})
}

// Current is the document in use: its window has the focus, or had it
// last (a phone: the one on screen); nil when none is open.
func (ds *Documents[T]) Current() signal.Accessor[*Document[T]] {
	return signal.Func(func() *Document[T] {
		if b := ds.k.current.Get(); b != nil {
			return b.ops.(*Document[T])
		}
		return nil
	})
}

// start opens what the launch has: the documents a crash or a quit left
// unsaved, the files the app was opened with, or else (desktop) an
// untitled document.
func (k *kind) start() {
	k.starting++ // the restore
	ui.OnOpenFiles(func(paths []string) { k.openFiles(paths) })
	app.OnLifecycle(func(s app.State) {
		if s == app.Background {
			k.persistAll()
		}
	})
	if !k.desktop {
		k.docsDir = documentsDir()
		k.listFiles()
	} else if !k.mac {
		k.refreshRecent()
	}
	k.restore(func() {
		k.starting--
		k.startedUp()
	})
}

// startedUp makes an untitled document once the launch opened nothing.
func (k *kind) startedUp() {
	if k.starting == 0 && k.desktop && !k.noUntitled && len(k.docs.Peek()) == 0 && !k.quitting {
		k.starting = -1 // once
		k.newUntitled()
		k.starting = 0
	}
}

// openFiles opens the files the system gave the app (Finder, Explorer,
// the Dock, the command line, Open Recent): those of the kind's types,
// and on a phone or the web, where they're copies, a copy in the app's
// documents.
func (k *kind) openFiles(paths []string) {
	for _, p := range paths {
		if !k.handles(p) {
			continue
		}
		if !k.desktop && !k.inDocuments(p) && !inPlace(p) {
			k.importFile(p)
			continue
		}
		k.open(p, func(_ *base, err error) {
			if err != nil {
				k.showError(nil, "opened", filepath.Base(p), err)
			}
		})
	}
}

// persistAll saves every edited document in place, or writes its copy
// (the app going to the background).
func (k *kind) persistAll() {
	for _, b := range k.docs.Peek() {
		if b.edited {
			b.persist(nil)
		}
	}
}

// noteRecent records path as a recent document.
func (k *kind) noteRecent(path string) {
	if !k.mounted || !k.desktop {
		return
	}
	recent.Add(path, func(error) {
		if !k.mac {
			k.refreshRecent()
		}
	})
}

// refreshRecent reads the recent documents again (Windows, Linux).
func (k *kind) refreshRecent() {
	recent.List(func(paths []string, err error) {
		if err == nil {
			k.recent.Set(paths)
		}
	})
}
