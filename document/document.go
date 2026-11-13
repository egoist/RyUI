// Package document makes document-based apps: apps whose windows each
// show a file, as a text editor, a drawing app or a notes app do. It
// gives them what AppKit's NSDocument and NSDocumentController give a
// Mac app, on every platform Asuka runs on:
//
//   - a window per document on the desktop, named after its file, with a
//     dot in a Mac's close button (a * before the title on Windows and
//     Linux) while it has unsaved changes, and the file's icon in a Mac's
//     title bar;
//   - the File menu's New, Open…, Open Recent, Close, Save, Save As…,
//     Duplicate, Rename…, Move To… and Revert, with their shortcuts;
//   - autosave, and asking to save before a window closes or the app
//     quits only where saving can't happen by itself;
//   - a recovery copy of every unsaved change, so that a crash or a quit
//     loses nothing;
//   - the files the system opens the app with (ui.OnOpenFiles), routed to
//     the window that has them if they're open;
//   - the files that other apps change, move or delete while they're open.
//
// On a phone and in a browser there's one document at a time: a list of
// the app's documents, and a document's screen pushed over it, saved in
// place as the user edits, as UIDocument and Android's apps do.
//
//	func main() {
//		document.New(document.Options[string]{
//			Types: []document.Type{{Name: "Plain Text", Extensions: []string{"txt"}}},
//			Read:  func(r io.Reader) (string, error) { b, err := io.ReadAll(r); return string(b), err },
//			Write: func(w io.Writer, s string) error { _, err := io.WriteString(w, s); return err },
//			Equal: func(a, b string) bool { return a == b },
//			View: func(doc *document.Document[string]) ui.Node {
//				return ui.TextField(ui.TextFieldProps{Value: doc.Content(), Multiline: true, Plain: true})
//			},
//		}).Run()
//	}
//
// Everything here runs on the main thread, as the rest of Asuka does;
// files are read and written on goroutines, and their results come back
// to the main thread.
package document

import (
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Type is a kind of file the documents are. List the same in asuka.json's
// fileTypes, which makes the system offer the app for them.
type Type struct {
	// Name says what the files are ("Plain Text"); dialogs show it.
	Name string
	// Extensions are the files' name extensions, without the dot
	// ("txt", "text"). New documents are saved with the first type's
	// first.
	Extensions []string
	// MIMETypes are the files' MIME types ("text/plain"), for the
	// dialogs of the platforms that pick files by type (a browser's,
	// Android's).
	MIMETypes []string
}

// Autosave is how documents save their changes.
type Autosave int

const (
	// AutosaveInPlace writes a document's changes to its file a moment
	// after the user stops editing, when its window loses the focus and
	// when it closes, as Mac apps do: a saved document never asks to be
	// saved. An untitled one asks when it closes (Save…, Delete, Cancel),
	// and so does one that can't be written (read-only, changed by
	// another app, deleted).
	AutosaveInPlace Autosave = iota
	// AutosaveOff writes documents when the user saves them, and asks to
	// save their changes when they close, as Windows and Linux apps
	// traditionally do. Phones and the web always save in place.
	AutosaveOff
)

// ErrCanceled is what a document's command gives when the user cancels
// it, in a dialog it showed.
var ErrCanceled = errors.New("document: canceled")

// ErrChanged is what Save gives when another app changed the document's
// file since it was read or saved: SaveAnyway writes it all the same.
var ErrChanged = errors.New("document: the file was changed by another application")

// Options describe one kind of document, whose content is a T. Types,
// Read, Write and View are required.
type Options[T any] struct {
	// Types are the kinds of files the documents are: open dialogs offer
	// them, and new documents are saved as the first.
	Types []Type
	// New is the content of a new, untitled document (nil: T's zero
	// value).
	New func() T
	// Read reads a content from a file, on a goroutine.
	Read func(r io.Reader) (T, error)
	// Write writes a content to a file, on a goroutine, with the content
	// as it was when saving started: edits made meanwhile stay unsaved. A
	// content that's a value the app replaces (a string, a struct of
	// values, a persistent snapshot) is written as it is; a mutable one
	// needs Snapshot.
	Write func(w io.Writer, content T) error
	// Snapshot, for a mutable content (a pointer to a model the app
	// changes in place, telling with Document.Changed), copies it on the
	// main thread before it's written: Write gets the copy.
	Snapshot func(content T) T
	// Equal compares contents: a document whose content is Equal to its
	// file's isn't edited, so that undoing back to it, or typing a
	// character and deleting it, leaves it unedited. Without it, every
	// change edits the document until it's saved. It also skips a Set of
	// the content it has.
	Equal func(a, b T) bool
	// View is what a document's window (a phone's screen) shows. It's
	// built when the document opens and disposed when it closes.
	View func(doc *Document[T]) ui.Node

	// Autosave is how documents save: AutosaveInPlace (the default) or
	// AutosaveOff (desktop only).
	Autosave Autosave
	// AutosaveDelay is how long after the last edit a document is saved
	// in place: 5 seconds by default. One edited without a pause is
	// saved at least every 30 seconds.
	AutosaveDelay time.Duration
	// AskOnQuit asks to save the documents with unsaved changes as the
	// app quits, rather than keeping them for the next launch (where they
	// open again, still edited, as VS Code's and Zed's do).
	AskOnQuit bool
	// NoUntitled opens no untitled document when the app starts with
	// nothing to open, nor (macOS) when its Dock icon is clicked with no
	// window open.
	NoUntitled bool

	// Width and Height are a document window's content size as it first
	// opens, in points (0: the host's); MinWidth and MinHeight the least.
	// Where the user leaves a document's window is remembered by its file.
	Width, Height, MinWidth, MinHeight float32
	// Menus are the app's own menus, beside the File menu: one titled
	// File adds its items to it, after the documents' own.
	Menus signal.Accessor[[]ui.MenuBarMenu]
}

// State describes a document.
type State struct {
	// Name is the document's file name, or the name of an untitled one
	// ("Untitled 2").
	Name string
	// Path is its file; "" while it's untitled.
	Path string
	// Edited: it has unsaved changes.
	Edited bool
	// ReadOnly: its file can't be written; saving asks for another.
	ReadOnly bool
	// Conflict: another app changed its file while it had unsaved
	// changes; saving asks which to keep.
	Conflict bool
	// Deleted: its file was deleted; saving writes it again.
	Deleted bool
}

// Document is an open document: its content, the file it was read from
// and is saved to, and its window. Its methods run on the main thread.
type Document[T any] struct {
	*base
	ds      *Documents[T]
	content *signal.Signal[T]
	// saved is the content the file has, when savedKnown (a document
	// restored from its recovery copy doesn't know it).
	saved      T
	savedKnown bool
	// loading: the content was set from the file, not edited (the
	// effect that follows it skips one run).
	loading bool
}

// Content is the document's content, a signal: read it in the view, and
// set it to edit the document (a text field's Value can be it). Reading
// the file again (Revert, another app's change) sets it too.
func (d *Document[T]) Content() *signal.Signal[T] { return d.content }

// Set replaces the content, which edits the document unless Equal finds
// it equal to the file's.
func (d *Document[T]) Set(v T) { d.content.Set(v) }

// Changed tells that a mutable content changed in place (see
// Options.Snapshot): it edits the document.
func (d *Document[T]) Changed() {
	d.changes++
	d.setEdited(true)
}

// SetEdited sets whether the document has unsaved changes, for an app
// that knows better (one keeping its own undo history): false makes the
// content the file's, without writing it.
func (d *Document[T]) SetEdited(edited bool) {
	d.changes++
	if !edited {
		d.savedAt = d.changes
		d.saved, d.savedKnown = d.content.Peek(), true
	}
	d.setEdited(edited)
}

// edit follows a change of the content signal.
func (d *Document[T]) edit() {
	d.changes++
	edited := true
	if eq := d.ds.opts.Equal; eq != nil && d.savedKnown {
		edited = !eq(d.content.Peek(), d.saved)
	}
	d.setEdited(edited)
}

// setContent sets the content from the file, or a recovery copy.
func (d *Document[T]) setContent(v T) {
	cur := d.content.Peek()
	if eq := d.ds.opts.Equal; eq == nil || !eq(cur, v) {
		d.loading = true // the signal's effect runs, at once or at the batch's end
	}
	d.content.Set(v)
}

func (d *Document[T]) capture() snapshot {
	v := d.content.Peek()
	if s := d.ds.opts.Snapshot; s != nil {
		v = s(v)
	}
	write := d.ds.opts.Write
	return snapshot{at: d.changes, value: v, write: func(w io.Writer) error { return write(w, v) }}
}

func (d *Document[T]) markSaved(s snapshot) {
	d.saved, d.savedKnown = s.value.(T), true
	d.edited = d.changes != s.at
	if d.edited {
		if eq := d.ds.opts.Equal; eq != nil && eq(d.content.Peek(), d.saved) {
			d.edited = false
		}
	}
}

func (d *Document[T]) replace(v any) {
	d.saved, d.savedKnown = v.(T), true
	d.setContent(v.(T))
	d.changes++
	d.savedAt = d.changes
	d.edited = false
}

func (d *Document[T]) restore(v any) {
	d.setContent(v.(T))
	d.savedKnown = false
	d.changes++
	d.edited = true
}

func (d *Document[T]) read(r io.Reader) (any, error) { return d.ds.opts.Read(r) }

// snapshot is what a write of a document writes: the content at change
// at.
type snapshot struct {
	at    uint64
	value any
	write func(io.Writer) error
}

// ops is what the generic Document gives the rest of the machinery.
type ops interface {
	capture() snapshot
	markSaved(s snapshot)
	replace(v any)
	restore(v any)
	read(r io.Reader) (any, error)
}

// base is the state of a document that doesn't depend on its content's
// type. Main thread only.
type base struct {
	k   *kind
	ops ops
	// id names its recovery copy.
	id string

	path string
	// name is an untitled document's; untitled its number, which other
	// untitled documents don't take.
	name     string
	untitled int
	// changes counts the content's changes; savedAt is the change the
	// file has, backedUp the one the recovery copy has (0: none).
	changes, savedAt, backedUp uint64
	edited                     bool
	// stamp is the file as the document last read or wrote it.
	stamp                       fileStamp
	readOnly, conflict, deleted bool
	// closed: its window closed; discarded: the user chose not to keep
	// its changes.
	closed, discarded bool
	// dirtySince is when the oldest change not saved or copied was made.
	dirtySince time.Time
	stopTimer  func()

	// IO: the reads and writes of its file and recovery copy, one at a
	// time, in order (io.go).
	queue []job
	busy  bool
	// flow: a question about it is up (closing, saving), which another
	// waits for.
	flow bool

	// unwatchFile stops the watch of its file, watched (watch.go).
	unwatchFile func()
	watched     string

	state   *signal.Signal[State]
	open    *signal.Signal[bool] // its window's
	win     *ui.WindowHandle     // its window's (desktop)
	view    ui.Node              // a view in its window, which its dialogs belong to
	dispose func()               // its owner's
}

// State is the document's state, a signal: its name, file, and whether
// it has unsaved changes, can't be written, or another app changed or
// deleted its file.
func (b *base) State() signal.Accessor[State] { return b.state }

// Path is the document's file, "" while it's untitled.
func (b *base) Path() string { return b.path }

// Name is the document's file name, or an untitled one's ("Untitled 2").
func (b *base) Name() string {
	if b.path != "" {
		return filepath.Base(b.path)
	}
	return b.name
}

// Edited reports whether it has unsaved changes.
func (b *base) Edited() bool { return b.edited }

// Window is the handle of its window (on a phone, the app's).
func (b *base) Window() *ui.WindowHandle { return b.win }

func (b *base) stateNow() State {
	return State{Name: b.Name(), Path: b.path, Edited: b.edited, ReadOnly: b.readOnly, Conflict: b.conflict, Deleted: b.deleted}
}

// notify tells the views and the menus that the state changed.
func (b *base) notify() { b.state.Set(b.stateNow()) }

// setEdited takes the edited state after a change of the content.
func (b *base) setEdited(edited bool) {
	b.edited = edited
	switch {
	case !edited:
		b.dirtySince = time.Time{}
	case b.dirtySince.IsZero():
		b.dirtySince = now()
	}
	b.schedule()
	if !edited && b.backedUp != 0 {
		b.dropCopy() // back to what the file has: the copy is of no use
	}
	b.notify()
}

// autosaves reports whether the document is saved in place.
func (b *base) autosaves() bool {
	return b.k.autosave == AutosaveInPlace && b.path != "" && !b.readOnly && !b.conflict && !b.deleted
}

// schedule arms the timer that autosaves the document, or writes its
// recovery copy, once the edits stop.
func (b *base) schedule() {
	if b.stopTimer != nil {
		b.stopTimer()
		b.stopTimer = nil
	}
	if !b.edited || b.closed {
		return
	}
	delay, most := time.Second, 5*time.Second
	if b.autosaves() {
		delay, most = b.k.autosaveDelay, 30*time.Second
	}
	if left := most - now().Sub(b.dirtySince); left < delay {
		delay = max(left, 0)
	}
	b.stopTimer = afterFunc(delay, func() { b.stopTimer = nil; b.persist(nil) })
}

// persist saves the document in place, or else writes its recovery copy;
// done, if set, then runs.
func (b *base) persist(done func()) {
	if b.closed && !b.k.quitting {
		if done != nil {
			done()
		}
		return
	}
	if b.autosaves() {
		b.write(b.path, writeAutosave, func(err error) {
			if err != nil {
				b.backup(done)
			} else if done != nil {
				done()
			}
		})
		return
	}
	b.backup(done)
}

// fileName is a file name of name with the kind's first extension.
func (k *kind) fileName(name string) string {
	ext := k.ext()
	if ext == "" || strings.HasSuffix(strings.ToLower(name), "."+strings.ToLower(ext)) {
		return name
	}
	return name + "." + ext
}

// ext is the extension new documents are saved with.
func (k *kind) ext() string {
	for _, t := range k.types {
		if len(t.Extensions) > 0 {
			return t.Extensions[0]
		}
	}
	return ""
}

// handles reports whether the kind opens path, by its extension.
func (k *kind) handles(path string) bool {
	ext := strings.TrimPrefix(filepath.Ext(path), ".")
	for _, t := range k.types {
		for _, e := range t.Extensions {
			if strings.EqualFold(e, ext) {
				return true
			}
		}
	}
	return false
}
