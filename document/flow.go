package document

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/dialog"
	"github.com/egoist/asuka/ui"
)

// What documents ask the user: before a window closes and the app quits,
// as a save meets a file it can't write, and the File menu's commands.
// A close or a quit that must ask is refused, and done again once the
// user has answered and the document is saved or let go, as VS Code does
// it: nothing waits in a loop on the main thread.

// alert shows a question about b, a sheet of its window on a Mac (the
// window comes to the front first).
func (b *base) alert(p ui.AlertProps) {
	if b != nil && b.k.desktop && b.win != nil && !b.closed {
		b.win.Focus()
	}
	ui.Alert(p)
}

func act(title string, role ui.ActionRole, f func()) ui.Action {
	return ui.Action{Title: ui.Val(title), Role: role, OnPress: f}
}

// closeRequested decides whether b's window may close (its
// OnCloseRequest): an unedited document closes, an edited one is saved in
// place or asks first, which keeps the window open until it's answered.
func (b *base) closeRequested() bool {
	if b.flow {
		return false
	}
	if !b.edited || b.k.quitting {
		return true
	}
	b.flow = true
	finish := func(ok bool) {
		b.flow = false
		if ok {
			b.win.Destroy()
		}
	}
	if b.autosaves() {
		b.write(b.path, writeAutosave, func(err error) {
			if err == nil {
				finish(true)
				return
			}
			b.askToSave(finish)
		})
		return false
	}
	b.askToSave(finish)
	return false
}

// askToSave asks whether to save b's changes before it closes, saves or
// discards them, and tells done whether b may close.
func (b *base) askToSave(done func(bool)) {
	s := b.stateNow()
	save := func() {
		b.save(false, func(err error) {
			if err != nil && !errors.Is(err, ErrCanceled) {
				b.k.showError(b, "saved", b.Name(), err)
			}
			done(err == nil)
		})
	}
	discard := func() { b.discard(); done(true) }
	cancel := func() { done(false) }
	if s.Path == "" && b.k.autosave == AutosaveInPlace {
		b.alert(ui.AlertProps{
			Title:   sayWith("keepNewDocument", map[string]any{"name": s.Name}),
			Message: say("keepNewDocumentDetail"),
			Actions: []ui.Action{
				act(say("saveEllipsis"), ui.ActionDefault, save),
				act(say("delete"), ui.ActionDestructive, discard),
				act(say("cancel"), ui.ActionCancel, cancel),
			},
		})
		return
	}
	title := say("save")
	if s.Path == "" {
		title = say("saveEllipsis")
	}
	b.alert(ui.AlertProps{
		Title:   sayWith("saveChanges", map[string]any{"name": s.Name}),
		Message: say("changesLost"),
		Actions: []ui.Action{
			act(title, ui.ActionDefault, save),
			act(say("dontSave"), ui.ActionDestructive, discard),
			act(say("cancel"), ui.ActionCancel, cancel),
		},
	})
}

// discard lets b's unsaved changes go, as it closes.
func (b *base) discard() {
	b.discarded = true
	if b.backedUp != 0 || b.k.recovery.has(b.id) {
		b.dropCopy()
	}
}

// Save writes the document to its file, as File > Save does: an untitled
// one asks where, a read-only one asks for another file, and one whose
// file another app changed asks whether to replace it. done, if set, gets
// what failed (ErrCanceled: the user canceled).
func (b *base) Save(done func(error)) { b.save(false, orNothing(done)) }

// SaveAs asks where to save the document, and writes it there: the
// document is that file from then on. A path saves there without asking.
func (b *base) SaveAs(path string, done func(error)) {
	if path == "" {
		b.save(true, orNothing(done))
		return
	}
	b.write(path, writeSaveAs, orNothing(done))
}

// SaveAnyway writes the document to its file even when another app
// changed the file since.
func (b *base) SaveAnyway(done func(error)) {
	if b.path == "" {
		b.save(true, orNothing(done))
		return
	}
	b.write(b.path, writeOverwrite, orNothing(done))
}

// Revert reads the document's file again, its unsaved changes gone.
func (b *base) Revert(done func(error)) { b.reload(orNothing(done)) }

// Close closes the document's window as the user does: an edited one is
// saved in place or asks first. A phone's goes back to the list.
func (b *base) Close() {
	if b.k.desktop {
		b.win.Close()
		return
	}
	if b.k.nav != nil && b.k.current.Peek() == b {
		b.k.nav.Pop()
	}
}

func orNothing(f func(error)) func(error) {
	if f == nil {
		return func(error) {}
	}
	return f
}

// save saves b, asking for a file when it has none or as is set, and
// what to do when its file can't be written or another app changed it.
func (b *base) save(as bool, done func(error)) {
	if as || b.path == "" {
		b.askPath("", "", func(target string, err error) {
			if err != nil {
				done(err)
				return
			}
			b.write(target, writeSaveAs, done)
		})
		return
	}
	if b.readOnly {
		b.askReadOnly(done)
		return
	}
	path := b.path
	b.write(path, writeSave, func(err error) {
		switch {
		case errors.Is(err, ErrChanged):
			b.resolveConflict(done)
		case isPermission(err):
			b.readOnly = true
			b.notify()
			b.askReadOnly(done)
		default:
			done(err)
		}
	})
}

// askReadOnly tells that b's file can't be written, and offers another.
func (b *base) askReadOnly(done func(error)) {
	b.alert(ui.AlertProps{
		Title:   sayWith("readOnly", map[string]any{"name": b.Name()}),
		Message: say("readOnlyDetail"),
		Actions: []ui.Action{
			act(say("saveAs"), ui.ActionDefault, func() { b.save(true, done) }),
			act(say("cancel"), ui.ActionCancel, func() { done(ErrCanceled) }),
		},
	})
}

// resolveConflict asks what to do when another app changed b's file:
// replace it with b, or read it again.
func (b *base) resolveConflict(done func(error)) {
	b.alert(ui.AlertProps{
		Title:   sayWith("changedOnDisk", map[string]any{"name": b.Name()}),
		Message: say("changedOnDiskDetail"),
		Actions: []ui.Action{
			act(say("saveAnyway"), ui.ActionDefault, func() { b.write(b.path, writeOverwrite, done) }),
			act(say("revert"), ui.ActionDestructive, func() { b.reload(done) }),
			act(say("cancel"), ui.ActionCancel, func() { done(ErrCanceled) }),
		},
	})
}

// askPath asks where to save b (or, with title and button, where to
// rename it), with the kind's extension.
func (b *base) askPath(title, button string, done func(string, error)) {
	k := b.k
	start := b.path
	if start == "" {
		start = documentsFolder()
	}
	dialog.Save(dialog.SaveOptions{
		Title: title, Button: button, Start: start, Name: k.fileName(b.Name()),
		Filters: k.filters(), From: b.view,
	}, func(target string, err error) {
		switch {
		case err != nil:
			done("", err)
		case target == "":
			done("", ErrCanceled)
		default:
			if filepath.Ext(target) == "" {
				target = k.fileName(target) // GTK's dialog adds none
			}
			done(target, nil)
		}
	})
}

// filters are the dialogs' filters of the kind's types.
func (k *kind) filters() []dialog.Filter {
	var fs []dialog.Filter
	for _, t := range k.types {
		fs = append(fs, dialog.Filter{Name: t.Name, Types: t.MIMETypes, Extensions: t.Extensions})
	}
	return fs
}

// Duplicate opens an untitled copy of the document, edited, as File >
// Duplicate does: "Notes copy".
func (d *Document[T]) Duplicate() *Document[T] { return d.duplicate().ops.(*Document[T]) }

func (b *base) duplicate() *base {
	k := b.k
	snap := b.ops.capture()
	name := b.Name()
	if b.path != "" {
		name = strings.TrimSuffix(name, filepath.Ext(name))
	}
	name = sayWith("copyName", map[string]any{"name": name})
	d := k.create(snap.value, name)
	d.ops.restore(snap.value)
	d.dirtySince = now()
	if !k.desktop {
		d.path = k.freePath(name)
		k.show(d)
		d.write(d.path, writeSaveAs, func(error) { k.listFiles() })
		return d
	}
	k.show(d)
	return d
}

// Rename renames the document's file in its folder, as File > Rename…
// does ("" asks for the name); an untitled one is saved instead.
func (b *base) Rename(name string, done func(error)) {
	done = orNothing(done)
	if b.path == "" {
		b.save(true, done)
		return
	}
	if name == "" {
		b.askPath(say("rename"), say("rename"), func(target string, err error) {
			if err != nil {
				done(err)
				return
			}
			b.relocate(target, done)
		})
		return
	}
	if strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		done(errors.New("document: invalid file name " + name))
		return
	}
	target := filepath.Join(filepath.Dir(b.path), b.k.fileName(name))
	if _, err := os.Lstat(target); err == nil {
		done(&fs.PathError{Op: "rename", Path: target, Err: fs.ErrExist})
		return
	}
	b.relocate(target, done)
}

// MoveTo moves the document's file into the folder dir, as File > Move
// To… does ("" asks for the folder).
func (b *base) MoveTo(dir string, done func(error)) {
	done = orNothing(done)
	if dir == "" {
		dialog.Folder(dialog.FolderOptions{Title: say("moveToEllipsis"), Button: say("move"), From: b.view}, func(dirs []string, err error) {
			switch {
			case err != nil:
				done(err)
			case len(dirs) == 0:
				done(ErrCanceled)
			default:
				b.MoveTo(dirs[0], done)
			}
		})
		return
	}
	if b.path == "" {
		b.write(filepath.Join(dir, b.k.fileName(b.Name())), writeSaveAs, done)
		return
	}
	target := filepath.Join(dir, filepath.Base(b.path))
	if target == b.path {
		done(nil)
		return
	}
	if _, err := os.Lstat(target); err == nil {
		b.alert(ui.AlertProps{
			Title:   sayWith("alreadyExists", map[string]any{"name": filepath.Base(target)}),
			Message: say("alreadyExistsDetail"),
			Actions: []ui.Action{
				act(say("replace"), ui.ActionDestructive, func() { b.relocate(target, done) }),
				act(say("cancel"), ui.ActionCancel, func() { done(ErrCanceled) }),
			},
		})
		return
	}
	b.relocate(target, done)
}

// revert asks whether to drop b's changes, then reads its file again (File
// > Revert to Saved).
func (b *base) revert() {
	if !b.edited || b.path == "" {
		return
	}
	b.alert(ui.AlertProps{
		Title:   sayWith("revertQuestion", map[string]any{"name": b.Name()}),
		Message: say("revertDetail"),
		Actions: []ui.Action{
			act(say("revert"), ui.ActionDestructive, func() {
				b.reload(func(err error) {
					if err != nil {
						b.k.showError(b, "reverted", b.Name(), err)
					}
				})
			}),
			act(say("cancel"), ui.ActionCancel, nil),
		},
	})
}

// showError tells the user what went wrong with a document.
func (k *kind) showError(b *base, action, name string, err error) {
	detail := err.Error()
	var pe *fs.PathError
	if errors.As(err, &pe) {
		detail = pe.Err.Error()
	}
	if !k.mounted {
		logf("%s: %v", name, err)
		return
	}
	b.alert(ui.AlertProps{Title: sayWith("failed", map[string]any{"action": action, "name": name}), Message: detail})
}

// openDialog asks which documents to open, and opens them.
func (k *kind) openDialog() {
	var from ui.Node
	if b := k.current.Peek(); b != nil {
		from = b.view
	}
	dialog.Open(dialog.OpenOptions{Filters: k.filters(), Multiple: k.desktop, From: from}, func(paths []string, err error) {
		if err != nil || len(paths) == 0 {
			return
		}
		if !k.desktop {
			// A phone's picker gives copies: they go in the app's
			// documents.
			for _, p := range paths {
				k.importFile(p)
			}
			return
		}
		for _, p := range paths {
			k.open(p, func(_ *base, err error) {
				if err != nil {
					k.showError(nil, "opened", filepath.Base(p), err)
				}
			})
		}
	})
}

// The quit: documents are saved in place, kept in their recovery copies
// for the next launch, or asked about (Options.AskOnQuit). The quit is
// canceled while that happens, then asked for again.

// quitAgain quits once the documents are taken care of (tests replace
// it).
var quitAgain = app.Quit

func (k *kind) onQuit(e *app.QuitEvent) {
	if k.quit.resolved {
		k.quit.resolved = false
		k.quitting = true
		return
	}
	if e.Canceled() && !e.SessionEnding {
		return
	}
	if k.quit.running && !e.SessionEnding {
		e.Cancel()
		return
	}
	var edited []*base
	for _, b := range k.docs.Peek() {
		if b.edited {
			edited = append(edited, b)
		}
	}
	if len(edited) == 0 {
		k.quitting = true
		return
	}
	if e.SessionEnding {
		// The session ends: no question, and no time to wait. Each
		// document is saved in place or kept, at once, here.
		k.quitting = true
		for _, b := range edited {
			b.persistNow()
		}
		return
	}
	e.Cancel()
	k.quit.running = true
	k.resolveQuit(edited, func(ok bool) {
		k.quit.running = false
		if ok {
			k.quit.resolved = true
			quitAgain()
		}
	})
}

// resolveQuit takes care of the edited documents before quitting, and
// tells done whether the app may quit.
func (k *kind) resolveQuit(docs []*base, done func(bool)) {
	var ask []*base
	left := len(docs)
	settled := func() {
		if left--; left > 0 {
			return
		}
		k.askOnQuitting(ask, done)
	}
	for _, b := range docs {
		if k.askOnQuit && !b.autosaves() {
			ask = append(ask, b)
			settled()
			continue
		}
		b.persistQuitting(func(saved bool) {
			if !saved && k.askOnQuit {
				ask = append(ask, b)
			}
			settled()
		})
	}
}

// persistQuitting saves b in place, or (unless the quit asks) writes its
// copy; done tells whether it's in its file.
func (b *base) persistQuitting(done func(saved bool)) {
	backup := func() {
		if b.k.askOnQuit {
			done(false)
			return
		}
		b.backup(func() { done(false) })
	}
	if !b.autosaves() {
		backup()
		return
	}
	b.write(b.path, writeAutosave, func(err error) {
		if err == nil {
			done(true)
			return
		}
		backup()
	})
}

// askOnQuitting asks about the documents the quit can't keep: one is
// asked about as a window's close; several, on a Mac, after a question
// of whether to review them, one by one; on Windows and Linux in one
// dialog naming them.
func (k *kind) askOnQuitting(ask []*base, done func(bool)) {
	switch len(ask) {
	case 0:
		done(true)
		return
	case 1:
		ask[0].askToSave(done)
		return
	}
	discardAll := func() {
		for _, b := range ask {
			b.discard()
		}
		done(true)
	}
	if k.mac {
		var review func(i int)
		review = func(i int) {
			if i == len(ask) {
				done(true)
				return
			}
			ask[i].askToSave(func(ok bool) {
				if !ok {
					done(false)
					return
				}
				review(i + 1)
			})
		}
		ui.Alert(ui.AlertProps{
			Title:   sayWith("reviewBeforeQuitting", map[string]any{"n": len(ask), "app": appName()}),
			Message: say("reviewBeforeQuittingDetail"),
			Actions: []ui.Action{
				act(say("reviewChanges"), ui.ActionDefault, func() { review(0) }),
				act(say("discardChanges"), ui.ActionDestructive, discardAll),
				act(say("cancel"), ui.ActionCancel, func() { done(false) }),
			},
		})
		return
	}
	names := make([]string, len(ask))
	for i, b := range ask {
		names[i] = b.Name()
	}
	var saveAll func(i int)
	saveAll = func(i int) {
		if i == len(ask) {
			done(true)
			return
		}
		b := ask[i]
		b.save(false, func(err error) {
			if err != nil {
				if !errors.Is(err, ErrCanceled) {
					k.showError(b, "saved", b.Name(), err)
				}
				done(false)
				return
			}
			saveAll(i + 1)
		})
	}
	ui.Alert(ui.AlertProps{
		Title:   sayWith("saveManyChanges", map[string]any{"n": len(ask)}),
		Message: strings.Join(names, "\n") + "\n\n" + say("changesLost"),
		Actions: []ui.Action{
			act(say("saveAll"), ui.ActionDefault, func() { saveAll(0) }),
			act(say("dontSave"), ui.ActionDestructive, discardAll),
			act(say("cancel"), ui.ActionCancel, func() { done(false) }),
		},
	})
}

// persistNow saves b in place, or writes its copy, on the main thread and
// at once: the session is ending, and the process may end as the quit's
// handlers return.
func (b *base) persistNow() {
	snap := b.ops.capture()
	if b.autosaves() {
		if cur, exists, err := statStamp(b.path); err == nil && exists && cur.same(b.stamp) {
			if _, err := writeFileSafely(b.path, snap.write); err == nil {
				return
			}
		}
	}
	e := recoveryEntry{ID: b.id, Path: b.path, Stamp: b.stamp}
	if b.path == "" {
		e.Name = b.name
	}
	if err := b.k.recovery.save(e, snap.write); err != nil {
		logf("cannot keep %s: %v", b.Name(), err)
	}
}
