//go:build linux && !android

package linux

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// asuka/sdk/dialog (GtkFileDialog: GTK's own dialog, or the desktop's file
// chooser portal in a sandbox, modal to the window) and asuka/sdk/shell (the
// file manager's FileManager1 interface, GtkFileLauncher, GIO's trash)
// on Linux.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["dialog"] = map[string]method{
			"open":   func(args []byte, reply func([]byte, error)) { h.fileDialog("open", args, reply) },
			"save":   func(args []byte, reply func([]byte, error)) { h.fileDialog("save", args, reply) },
			"folder": func(args []byte, reply func([]byte, error)) { h.fileDialog("folder", args, reply) },
		}
		m["shell"] = map[string]method{
			"reveal": h.shellReveal,
			"open":   h.shellOpen,
			"trash":  shellTrash,
			"beep": func(_ []byte, reply func([]byte, error)) {
				Dialogs.DisplayBeep(DisplayGetDefault())
				reply(nil, nil)
			},
		}
	})
}

// dialogArgs are asuka/sdk/dialog's options.
type dialogArgs struct {
	Title   string `json:"title"`
	Button  string `json:"button"`
	Start   string `json:"start"`
	Name    string `json:"name"`
	Filters []struct {
		Name       string   `json:"name"`
		Types      []string `json:"types"`
		Extensions []string `json:"extensions"`
	} `json:"filters"`
	Multiple bool   `json:"multiple"`
	Hidden   bool   `json:"hidden"` // GTK's dialog has its own toggle (Ctrl+H)
	View     uint32 `json:"view"`
}

// fileDialog shows GTK's file dialog to open files (kind "open"), save
// one ("save") or choose folders ("folder"), and answers with the paths
// (open, folder: JSON) or the path (save) chosen; nothing: cancelled.
func (h *Host) fileDialog(kind string, args []byte, reply func([]byte, error)) {
	var a dialogArgs
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("dialog."+kind+": bad arguments"))
		return
	}
	d := Sys.FileDialogNew()
	Sys.FileDialogSetModal(d, true)
	if a.Title != "" {
		Sys.FileDialogSetTitle(d, a.Title)
	}
	if a.Button != "" {
		Dialogs.FileDialogSetAcceptLabel(d, a.Button)
	}
	if a.Start != "" {
		if st, err := os.Stat(a.Start); err == nil && st.IsDir() {
			setInitialFolder(d, a.Start)
		} else {
			// A file: its folder. GTK's open dialog (and the portal's)
			// selects nothing in it; Save suggests its name.
			setInitialFolder(d, filepath.Dir(a.Start))
			if kind == "save" && a.Name == "" {
				Dialogs.FileDialogSetInitialName(d, filepath.Base(a.Start))
			}
		}
	}
	if kind == "save" && a.Name != "" {
		Dialogs.FileDialogSetInitialName(d, a.Name)
	}
	if kind != "folder" && len(a.Filters) > 0 {
		store := Sys.ListStoreNew(Sys.FileFilterGetType())
		for i, f := range a.Filters {
			ff := Sys.FileFilterNew()
			Sys.FileFilterSetName(ff, f.Name)
			for _, t := range f.Types {
				if t == "*/*" || t == "*" {
					Dialogs.FileFilterAddPattern(ff, "*")
				} else {
					Sys.FileFilterAddMIMEType(ff, t) // "image/*" too
				}
			}
			for _, e := range f.Extensions {
				if e == "*" {
					Dialogs.FileFilterAddPattern(ff, "*")
				} else {
					Sys.FileFilterAddSuffix(ff, e)
				}
			}
			Sys.ListStoreAppend(store, ff)
			if i == 0 {
				Sys.FileDialogSetDefaultFilter(d, ff)
			}
			ObjectUnref(ff)
		}
		Sys.FileDialogSetFilters(d, store)
		ObjectUnref(store)
	}

	parent := h.dialogWindow(ops.NodeID(a.View))
	finish := func(paths []string, gerr Ptr) {
		ObjectUnref(d)
		switch {
		case gerr != 0 && Cancelled(gerr):
			ErrorFree(gerr)
			reply(nil, nil)
		case gerr != 0:
			reply(nil, GError(gerr))
		case len(paths) == 0:
			reply(nil, nil)
		case kind == "save":
			reply([]byte(paths[0]), nil)
		default:
			b, err := json.Marshal(paths)
			reply(b, err)
		}
	}
	// one finishes a method that gives a GFile; many one that gives a
	// GListModel of them.
	one := func(fin func(d, res Ptr, err *Ptr) Ptr) func(_, res Ptr) {
		return func(_, res Ptr) {
			var gerr Ptr
			var paths []string
			if f := fin(d, res, &gerr); f != 0 {
				if p := GoStringFree(Sys.FileGetPath(f)); p != "" {
					paths = append(paths, p)
				}
				ObjectUnref(f)
			}
			finish(paths, gerr)
		}
	}
	many := func(fin func(d, res Ptr, err *Ptr) Ptr) func(_, res Ptr) {
		return func(_, res Ptr) {
			var gerr Ptr
			var paths []string
			if list := fin(d, res, &gerr); list != 0 {
				n := Sys.ListModelGetNItems(list)
				for i := uint32(0); i < n; i++ {
					f := Sys.ListModelGetItem(list, i)
					if p := GoStringFree(Sys.FileGetPath(f)); p != "" {
						paths = append(paths, p)
					}
					ObjectUnref(f)
				}
				ObjectUnref(list)
			}
			finish(paths, gerr)
		}
	}
	switch {
	case kind == "save":
		Async(func(c AsyncCall) { Dialogs.FileDialogSave(d, parent, 0, c.Callback, c.Data) }, one(Dialogs.FileDialogSaveFinish))
	case kind == "folder" && a.Multiple:
		Async(func(c AsyncCall) { Dialogs.FileDialogSelectFolders(d, parent, 0, c.Callback, c.Data) }, many(Dialogs.FileDialogSelectFoldersFin))
	case kind == "folder":
		Async(func(c AsyncCall) { Dialogs.FileDialogSelectFolder(d, parent, 0, c.Callback, c.Data) }, one(Dialogs.FileDialogSelectFolderFin))
	case a.Multiple:
		Async(func(c AsyncCall) { Sys.FileDialogOpenMultiple(d, parent, 0, c.Callback, c.Data) }, many(Sys.FileDialogOpenMultipleFin))
	default:
		Async(func(c AsyncCall) { Sys.FileDialogOpen(d, parent, 0, c.Callback, c.Data) }, one(Sys.FileDialogOpenFinish))
	}
}

func setInitialFolder(d Ptr, dir string) {
	f := Sys.FileNewForPath(dir)
	Dialogs.FileDialogSetInitialFolder(d, f)
	ObjectUnref(f)
}

// dialogWindow is the window a dialog belongs to: the view's (0: the one
// in use).
func (h *Host) dialogWindow(view ops.NodeID) Ptr {
	if n := h.nodes[view]; view != 0 && n != nil && n.widget != 0 {
		if root := WidgetGetRoot(n.widget); root != 0 && WidgetGetMapped(root) {
			return root
		}
	}
	return h.dialogParent()
}

// asuka/sdk/shell.

// shellReveal shows the item selected in a window of its folder: the
// file manager's org.freedesktop.FileManager1 (Files, Dolphin, Nemo,
// Thunar...); without one, the folder in its app.
func (h *Host) shellReveal(args []byte, reply func([]byte, error)) {
	path := string(args)
	if _, err := os.Lstat(path); err != nil {
		reply(nil, fmt.Errorf("no file at %s", path))
		return
	}
	abs, _ := filepath.Abs(path)
	f := Sys.FileNewForPath(abs)
	uri := GoStringFree(Sys.FileGetURI(f))
	ObjectUnref(f)
	inFolder := func() {
		h.launch(filepath.Dir(abs), func(err error) { reply(nil, err) })
	}
	conn, err := Bus(BusSession)
	if err != nil {
		inFolder()
		return
	}
	DBusCall(conn, "org.freedesktop.FileManager1", "/org/freedesktop/FileManager1", "org.freedesktop.FileManager1", "ShowItems",
		VTuple(VStrings([]string{uri}), VStr("")), 5000, func(_ Ptr, err error) {
			if err != nil {
				inFolder()
				return
			}
			reply(nil, nil)
		})
}

// shellOpen opens the item in the user's app for its type (a folder in
// the file manager), through the OpenURI portal in a sandbox.
func (h *Host) shellOpen(args []byte, reply func([]byte, error)) {
	path := string(args)
	if _, err := os.Stat(path); err != nil {
		reply(nil, fmt.Errorf("no file at %s", path))
		return
	}
	h.launch(path, func(err error) { reply(nil, err) })
}

// launch is launchFile, its "no application" error "no app".
func (h *Host) launch(path string, done func(error)) {
	file := Sys.FileNewForPath(path)
	l := Sys.FileLauncherNew(file)
	ObjectUnref(file)
	Async(func(a AsyncCall) { Sys.FileLauncherLaunch(l, h.dialogParent(), 0, a.Callback, a.Data) }, func(_, res Ptr) {
		var gerr Ptr
		ok := Sys.FileLauncherLaunchFinish(l, res, &gerr)
		ObjectUnref(l)
		switch {
		case ok:
			done(nil)
		case Cancelled(gerr): // the portal's app chooser closed
			ErrorFree(gerr)
			done(nil)
		case notSupported(gerr):
			ErrorFree(gerr)
			done(errors.New("no app"))
		default:
			done(GError(gerr))
		}
	})
}

// notSupported reports whether a GError is G_IO_ERROR_NOT_SUPPORTED:
// GIO's "no application is registered" for a type, and its trash's "no
// trash" on a volume without one.
func notSupported(gerr Ptr) bool {
	d, c := GErrorCode(gerr)
	return gerr != 0 && d == uint32(Call(Sym("g_io_error_quark"))) && c == 15
}

// shellTrash moves the item to the trash of its volume (GIO's,
// asynchronously), which fails where the volume has none: it never
// deletes.
func shellTrash(args []byte, reply func([]byte, error)) {
	path := string(args)
	if _, err := os.Lstat(path); err != nil {
		reply(nil, fmt.Errorf("no file at %s", path))
		return
	}
	f := Sys.FileNewForPath(path)
	Async(func(a AsyncCall) { Dialogs.FileTrashAsync(f, 0, 0, a.Callback, a.Data) }, func(_, res Ptr) {
		var gerr Ptr
		ok := Dialogs.FileTrashFinish(f, res, &gerr)
		ObjectUnref(f)
		if ok {
			reply(nil, nil)
			return
		}
		reply(nil, GError(gerr))
	})
}
