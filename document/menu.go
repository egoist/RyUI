package document

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/egoist/asuka/recent"
	"github.com/egoist/asuka/ui"
)

// The File menu: its items act on the document in use (its window has
// the focus, or had it last), and are enabled for it. On a Mac,
// DocumentGroup's: New ⌘N, Open… ⌘O, Open Recent (NSDocumentController's,
// the host's), Close ⌘W, Save ⌘S, Duplicate ⇧⌘S, Save As… ⌥⇧⌘S, Rename…,
// Move To…, Revert to Saved. On Windows and Linux, Notepad's and GNOME
// Text Editor's: New Ctrl+N, Open… Ctrl+O, Open Recent, Save Ctrl+S, Save
// As… Ctrl+Shift+S, Revert, Close Ctrl+W, Quit.

// menus are the menu bar's menus: File, with the app's own.
func (ds *Documents[T]) menus() []ui.MenuBarMenu {
	k := ds.k
	file := ui.MenuBarMenu{Title: say("file"), Items: k.fileItems()}
	if k.mac {
		file.Title = "File" // the host's own File menu, in its words
	}
	menus := []ui.MenuBarMenu{file}
	if ds.opts.Menus == nil {
		return menus
	}
	for _, m := range ds.opts.Menus.Get() {
		if strings.EqualFold(m.Title, "File") || m.Title == say("file") {
			menus[0].Items = append(append(menus[0].Items, ui.MenuSeparator), m.Items...)
			continue
		}
		menus = append(menus, m)
	}
	return menus
}

// fileItems are the File menu's items.
func (k *kind) fileItems() []ui.MenuItem {
	b := k.current.Get()
	var s State
	if b != nil {
		s = b.state.Get()
	}
	none := b == nil
	on := func(f func(b *base)) func() {
		return func() {
			if b := k.current.Peek(); b != nil {
				f(b)
			}
		}
	}
	report := func(b *base, action string) func(error) {
		return func(err error) {
			if err != nil && !errors.Is(err, ErrCanceled) {
				k.showError(b, action, b.Name(), err)
			}
		}
	}
	save := ui.MenuItem{Title: say("save"), Shortcut: ui.Cmd("s"), Disabled: none,
		OnPress: on(func(b *base) { b.save(false, report(b, "saved")) })}
	saveAs := ui.MenuItem{Title: say("saveAs"), Shortcut: ui.Cmd("s").With(ui.Shift), Disabled: none,
		OnPress: on(func(b *base) { b.save(true, report(b, "saved")) })}
	items := []ui.MenuItem{
		{Title: say("new"), Shortcut: ui.Cmd("n"), OnPress: func() { k.newUntitled() }},
		{Title: say("open"), Shortcut: ui.Cmd("o"), OnPress: k.openDialog},
		{Title: say("openRecent"), Role: ui.MenuRoleOpenRecent, Submenu: k.recentItems()},
		ui.MenuSeparator,
	}
	close := ui.MenuItem{Title: say("close"), Shortcut: ui.Cmd("w"), Role: ui.MenuRoleClose}
	if k.mac {
		saveAs.Shortcut = ui.Cmd("s").With(ui.Shift | ui.Alt)
		return append(items,
			close,
			save,
			ui.MenuItem{Title: say("duplicate"), Shortcut: ui.Cmd("s").With(ui.Shift), Disabled: none,
				OnPress: on(func(b *base) { b.duplicate() })},
			saveAs,
			ui.MenuItem{Title: say("renameEllipsis"), Disabled: none,
				OnPress: on(func(b *base) { b.Rename("", report(b, "renamed")) })},
			ui.MenuItem{Title: say("moveToEllipsis"), Disabled: none,
				OnPress: on(func(b *base) { b.MoveTo("", report(b, "moved")) })},
			ui.MenuItem{Title: say("revertToSaved"), Disabled: none || !s.Edited || s.Path == "",
				OnPress: on(func(b *base) { b.revert() })},
		)
	}
	return append(items,
		save,
		saveAs,
		ui.MenuItem{Title: say("revert"), Disabled: none || !s.Edited || s.Path == "",
			OnPress: on(func(b *base) { b.revert() })},
		ui.MenuSeparator,
		close,
		ui.MenuQuit,
	)
}

// recentItems are Open Recent's items where the app fills it (Windows,
// Linux; a Mac's host fills its own): the recent documents, newest
// first, then Clear Menu.
func (k *kind) recentItems() []ui.MenuItem {
	var items []ui.MenuItem
	for _, p := range k.recent.Get() {
		items = append(items, ui.MenuItem{Title: filepath.Base(p), OnPress: func() {
			k.open(p, func(_ *base, err error) {
				if err != nil {
					k.showError(nil, "opened", filepath.Base(p), err)
				}
			})
		}})
	}
	if len(items) > 0 {
		items = append(items, ui.MenuSeparator)
	}
	return append(items, ui.MenuItem{Title: say("clearMenu"), Disabled: len(k.recent.Get()) == 0, OnPress: func() {
		recent.Clear(func(error) { k.refreshRecent() })
	}})
}
