package document

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/asuka/internal/appdir"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Phones and the web: one document at a time, in the app's one root. The
// first screen lists the app's documents (the files of its types in its
// documents folder, newest first), as a document browser does; a
// document's screen is pushed over it, and going back closes it. Every
// document has a file from the start (a new one is "Untitled.txt" in the
// folder at once) and is saved in place as the user edits, when the app
// goes to the background and when it closes, as UIDocument and Android's
// apps save: nothing asks to save. Files from elsewhere (the document
// picker's, another app's) are copied into the folder first.

// fileItem is a document in the app's documents folder.
type fileItem struct {
	Path, Name string
	ModTime    int64
}

// documentsDir is the folder of the app's documents on a phone or the
// web: iOS's container's Documents (which Files shows, with
// UIFileSharingEnabled), elsewhere Documents in the app's data.
func documentsDir() string {
	data := appdir.Data()
	if strings.HasSuffix(data, filepath.Join("Library", "Application Support")) {
		return filepath.Join(filepath.Dir(filepath.Dir(data)), "Documents")
	}
	return filepath.Join(data, "Documents")
}

// documentsFolder is where a save dialog starts for an untitled document:
// the user's Documents.
func documentsFolder() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, "Documents")
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return home
}

// listFiles lists the app's documents again.
func (k *kind) listFiles() {
	dir := k.docsDir
	if dir == "" {
		return
	}
	goIO(func() {
		os.MkdirAll(dir, 0o700)
		entries, _ := os.ReadDir(dir)
		var items []fileItem
		for _, e := range entries {
			if !e.Type().IsRegular() || strings.HasPrefix(e.Name(), ".") || !k.handles(e.Name()) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			items = append(items, fileItem{Path: filepath.Join(dir, e.Name()), Name: e.Name(), ModTime: info.ModTime().UnixNano()})
		}
		slices.SortFunc(items, func(a, b fileItem) int {
			if a.ModTime != b.ModTime {
				if a.ModTime > b.ModTime {
					return -1
				}
				return 1
			}
			return strings.Compare(a.Name, b.Name)
		})
		post(func() { k.files.Set(items) })
	})
}

// freePath is a path in the app's documents for a document named name,
// numbered past the files there ("Untitled 2.txt").
func (k *kind) freePath(name string) string {
	for i := 1; ; i++ {
		n := name
		if i > 1 {
			n += " " + strconv.Itoa(i)
		}
		p := filepath.Join(k.docsDir, k.fileName(n))
		if _, err := os.Lstat(p); err != nil && k.find(p) == nil {
			return p
		}
	}
}

// inDocuments reports whether path is in the app's documents folder.
func (k *kind) inDocuments(path string) bool {
	return k.docsDir != "" && filepath.Dir(path) == k.docsDir
}

// inPlace reports whether a file from elsewhere opens where it is, rather
// than as a copy in the app's documents: never on a phone or the web,
// whose files from elsewhere are copies already, or readable only.
func inPlace(string) bool { return false }

// importFile copies a file from elsewhere into the app's documents, and
// opens the copy.
func (k *kind) importFile(path string) {
	name := filepath.Base(path)
	target := k.freePath(strings.TrimSuffix(name, filepath.Ext(name)))
	goIO(func() {
		err := copyFile(path, target)
		post(func() {
			if err != nil {
				k.showError(nil, "opened", name, err)
				return
			}
			k.listFiles()
			k.open(target, func(_ *base, err error) {
				if err != nil {
					k.showError(nil, "opened", name, err)
				}
			})
		})
	})
}

// deleteFile deletes a document of the app's (its swipe action), closing
// it first if it's open.
func (k *kind) deleteFile(path string) {
	if b := k.find(path); b != nil {
		b.discard()
		b.edited = false
		b.Close()
		b.windowClosed()
	}
	goIO(func() {
		os.Remove(path)
		post(k.listFiles)
	})
}

// mobileRoot is a phone's and the web's root: the documents' list, under
// which a document's screen is pushed.
func (ds *Documents[T]) mobileRoot() ui.Node {
	k := ds.k
	k.autosave = AutosaveInPlace
	return ui.Stack(func() ui.Node {
		k.nav = ui.UseNavigator()
		row := func(item signal.Accessor[fileItem], _ signal.Accessor[int]) ui.Node {
			return ui.View(ui.ViewProps{
				Style:   ui.Style{Padding: ui.Symmetric(12, 16)},
				Role:    ui.RoleButton,
				OnPress: func() { ds.openItem(item.Get().Path) },
			}, ui.Text(signal.Func(func() string { return displayName(item.Get().Name) })))
		}
		list := ui.List(ui.ListProps{
			SwipeActions: func(i int) []ui.Action {
				files := k.files.Peek()
				if i < 0 || i >= len(files) {
					return nil
				}
				p := files[i].Path
				return []ui.Action{
					{Title: signal.Func(func() string { return say("delete") }), Role: ui.ActionDestructive, Icon: ui.IconTrash, OnPress: func() { k.deleteFile(p) }},
					{Title: signal.Func(func() string { return say("duplicate") }), Icon: ui.IconCopy, OnPress: func() {
						k.open(p, func(b *base, err error) {
							if err == nil {
								b.duplicate()
							}
						})
					}},
				}
			},
		}, k.files, func(f fileItem) string { return f.Path }, row)
		return ui.Show(signal.Func(func() bool { return len(k.files.Get()) > 0 }),
			func() ui.Node { return list },
			func() ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1, Justify: ui.JustifyCenter, Align: ui.AlignCenter}},
					ui.Text(signal.Func(func() string { return say("noDocuments") }), ui.Style{Color: ui.SecondaryLabel}))
			})
	}, ui.ScreenOptions{
		Title: signal.Func(func() string { return say("documents") }),
		Actions: []ui.Action{
			{Title: signal.Func(func() string { return say("open") }), Icon: ui.IconFolder, OnPress: k.openDialog},
			{Title: signal.Func(func() string { return say("new") }), Icon: ui.IconCompose, OnPress: func() { k.newUntitled() }},
		},
	})
}

// openItem opens a document of the list.
func (ds *Documents[T]) openItem(path string) {
	ds.k.open(path, func(_ *base, err error) {
		if err != nil {
			ds.k.showError(nil, "opened", filepath.Base(path), err)
		}
	})
}

// displayName is a document's name as a phone shows it: without its
// extension, as Files and Android's apps show documents.
func displayName(name string) string {
	if ext := filepath.Ext(name); ext != "" && ext != name {
		return strings.TrimSuffix(name, ext)
	}
	return name
}

// showScreen shows b's screen, the one on screen going back first: one
// document at a time.
func (k *kind) showScreen(b *base) {
	if k.nav == nil || k.onScreen == b {
		return
	}
	if k.onScreen != nil {
		k.nav.Pop() // which closes it
	}
	k.onScreen = b
	k.nav.Push(func() ui.Node {
		signal.OnCleanup(func() {
			if k.onScreen == b {
				k.onScreen = nil
			}
			b.screenClosed()
		})
		b.view = ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}, k.view(b))
		return b.view
	}, ui.ScreenOptions{Title: signal.Func(func() string { return displayName(b.state.Get().Name) })})
}

// screenClosed closes b as its screen goes: saved in place first.
func (b *base) screenClosed() {
	if b.closed {
		return
	}
	if b.edited && b.autosaves() {
		b.write(b.path, writeSave, nil)
	}
	b.windowClosed()
	b.k.listFiles()
}
