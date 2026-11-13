// Package dialog shows the system's file dialogs, as a desktop app asks
// for files: Open picks files, Save picks where a file goes, Folder picks
// folders. They give the files' own paths, not copies (device.PickFiles
// gives copies, as a phone's document picker does): the app reads and
// writes the user's files where they are.
//
// macOS shows NSOpenPanel and NSSavePanel as sheets on the window, Windows
// the common item dialogs (IFileOpenDialog, IFileSaveDialog), modal to
// the window, and Linux GTK's file dialog (GtkFileDialog), which in a
// sandbox (Flatpak) is the desktop's file chooser portal, modal to the
// window. Each asks before Save replaces a file that's there.
//
// The web has no paths: a browser gives a page files, never where they
// are. Open and Folder copy what the user picks into the app's files (its
// caches, /tmp), so os.Open reads them; a folder comes with all it holds.
// Where the browser has the File System Access API (Chrome, Edge), Open,
// Save and Folder show the system's own dialogs (showOpenFilePicker,
// showSaveFilePicker, showDirectoryPicker), and the file Save gives is
// the one the user chose: once the app has written it (closed it, or
// renamed a file over it), the browser writes it there. Elsewhere Open
// and Folder are the browser's file input, and Save gives a path in the
// app's caches whose file is downloaded once it's written, by the name
// the app suggested (the browser decides where: its Downloads, or it
// asks).
//
// iOS and Android have no file dialogs of these: Open shows the system's
// document picker and gives copies, as device.PickFiles does, and Save
// and Folder answer ErrUnsupported.
//
// Every function runs on the main thread and answers there, once the
// user has chosen or cancelled; a cancel is no paths and no error.
package dialog

import (
	"encoding/json"
	"errors"
	"mime"
	"strings"

	"github.com/egoist/asuka/device"
	"github.com/egoist/asuka/ui"
)

// ErrUnsupported: the platform has no such dialog (Save and Folder on
// iOS and Android).
var ErrUnsupported = errors.New("dialog: not on this platform")

// Filter is a kind of file a dialog offers: by MIME type, as an HTML
// input's accept takes them ("application/pdf", or a family,
// "image/*"), which each system matches as it knows the types, or by
// extension ("md"), or both. The dialog shows its filters in its type
// menu (Windows, Linux, browsers with the File System Access API); a
// Mac's panel has none, and takes the files of any of them.
type Filter struct {
	// Name is what the type menu shows ("Images"); default: the types
	// and extensions.
	Name string
	// Types are MIME types: "image/png", or a family's, "image/*" (the
	// families image, audio, video and text are each system's own: a Mac's
	// public.image, the extensions Windows knows for each).
	Types []string
	// Extensions are file name extensions, without the dot ("md", "txt").
	Extensions []string
}

// OpenOptions say what Open shows.
type OpenOptions struct {
	// Title is the dialog's title (Windows, Linux); on a Mac the message
	// at the top of the panel, which shows no title as a sheet.
	Title string
	// Button is the label of the button that opens ("Import");
	// default: the system's ("Open").
	Button string
	// Start is the folder the dialog starts in, or a file whose folder it
	// starts in, the file selected (macOS, Windows; GTK's dialog selects
	// nothing). Default: the system's (the folder last used).
	// On the web, a path one of these dialogs gave (the browser starts
	// where that file or folder is), else ignored.
	Start string
	// Filters are the kinds of files offered, the first the one at
	// first; none: any file.
	Filters []Filter
	// Multiple lets the user pick several files.
	Multiple bool
	// ShowHidden shows hidden files (macOS, Windows; GTK's dialog has
	// its own toggle, Ctrl+H).
	ShowHidden bool
	// From is a view in the window the dialog belongs to: the window
	// whose sheet it is on a Mac, its owner elsewhere. Default: the
	// window the user is working in.
	From ui.Node
}

// SaveOptions say what Save shows.
type SaveOptions struct {
	// Title is the dialog's title (Windows, Linux); on a Mac the message
	// at the top of the panel.
	Title string
	// Button is the label of the button that saves ("Export");
	// default: the system's ("Save").
	Button string
	// Start is the folder the dialog starts in, or a file there whose
	// name it suggests. Default: the system's. On the web, as Open's.
	Start string
	// Name is the file name the dialog suggests ("Untitled.md"),
	// before Start's.
	Name string
	// Filters are the kinds of files the user may save as, the first
	// the one at first: a name typed without an extension gets the
	// first extension of the filter chosen (macOS, Windows).
	Filters []Filter
	// ShowHidden shows hidden files (macOS, Windows).
	ShowHidden bool
	// From is a view in the window the dialog belongs to, as Open's.
	From ui.Node
}

// FolderOptions say what Folder shows.
type FolderOptions struct {
	// Title is the dialog's title (Windows, Linux); on a Mac the message
	// at the top of the panel.
	Title string
	// Button is the label of the button that chooses ("Choose");
	// default: the system's.
	Button string
	// Start is the folder the dialog starts in. Default: the system's.
	// On the web, as Open's.
	Start string
	// Multiple lets the user pick several folders (the web: one).
	Multiple bool
	// ShowHidden shows hidden folders (macOS, Windows).
	ShowHidden bool
	// From is a view in the window the dialog belongs to, as Open's.
	From ui.Node
}

// Open shows the system's open dialog and calls done with the paths of
// the files the user picked, none if they cancelled. Main thread.
//
// On iOS and Android it shows the document picker, and the paths are
// copies in the app's caches (device.PickFiles' files), the app's to
// keep or move: Title, Button, Start and ShowHidden don't apply there.
func Open(o OpenOptions, done func(paths []string, err error)) {
	a := args{Title: o.Title, Button: o.Button, Start: o.Start, Filters: filters(o.Filters),
		Multiple: o.Multiple, Hidden: o.ShowHidden, View: ui.ViewID(o.From)}
	ui.Call("dialog", "open", a.encode(), func(b []byte, err error) {
		if noModule(err) {
			pickCopies(o, done)
			return
		}
		paths, err := decodePaths(b, err)
		done(paths, err)
	})
}

// Save shows the system's save dialog and calls done with the path the
// user chose, "" if they cancelled. The dialog has asked already whether
// to replace a file that's there: the app writes the file. Main thread.
func Save(o SaveOptions, done func(path string, err error)) {
	a := args{Title: o.Title, Button: o.Button, Start: o.Start, Name: o.Name, Filters: filters(o.Filters),
		Hidden: o.ShowHidden, View: ui.ViewID(o.From)}
	ui.Call("dialog", "save", a.encode(), func(b []byte, err error) {
		if err != nil {
			done("", hostError(err))
			return
		}
		done(string(b), nil)
	})
}

// Folder shows the system's dialog for choosing folders and calls done
// with the paths of those the user chose, none if they cancelled. Main
// thread.
func Folder(o FolderOptions, done func(paths []string, err error)) {
	a := args{Title: o.Title, Button: o.Button, Start: o.Start, Multiple: o.Multiple,
		Hidden: o.ShowHidden, View: ui.ViewID(o.From)}
	ui.Call("dialog", "folder", a.encode(), func(b []byte, err error) {
		paths, err := decodePaths(b, err)
		done(paths, err)
	})
}

// args are a dialog's options as the hosts take them (JSON).
type args struct {
	Title    string   `json:"title,omitempty"`
	Button   string   `json:"button,omitempty"`
	Start    string   `json:"start,omitempty"`
	Name     string   `json:"name,omitempty"`
	Filters  []filter `json:"filters,omitempty"`
	Multiple bool     `json:"multiple,omitempty"`
	Hidden   bool     `json:"hidden,omitempty"`
	View     uint32   `json:"view,omitempty"` // a view in the dialog's window
}

// filter is a Filter as the hosts take it: a name always, the
// extensions without dots.
type filter struct {
	Name       string   `json:"name"`
	Types      []string `json:"types,omitempty"`
	Extensions []string `json:"extensions,omitempty"`
}

func (a args) encode() []byte {
	b, _ := json.Marshal(a)
	return b
}

func filters(fs []Filter) []filter {
	var out []filter
	for _, f := range fs {
		var types, exts []string
		for _, t := range f.Types {
			if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
				types = append(types, t)
			}
		}
		for _, e := range f.Extensions {
			if e = strings.TrimLeft(strings.TrimSpace(e), "*."); e != "" {
				exts = append(exts, e)
			}
		}
		if len(types) == 0 && len(exts) == 0 {
			continue
		}
		name := f.Name
		if name == "" {
			var parts []string
			parts = append(parts, types...)
			for _, e := range exts {
				parts = append(parts, "*."+e)
			}
			name = strings.Join(parts, ", ")
		}
		out = append(out, filter{Name: name, Types: types, Extensions: exts})
	}
	return out
}

// decodePaths reads open's and folder's answer: a JSON array of paths,
// nothing if the user cancelled.
func decodePaths(b []byte, err error) ([]string, error) {
	if err != nil {
		return nil, hostError(err)
	}
	if len(b) == 0 {
		return nil, nil
	}
	var paths []string
	if err := json.Unmarshal(b, &paths); err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, nil
	}
	return paths, nil
}

// hostError is ErrUnsupported for a host that has no such dialog.
func hostError(err error) error {
	if err != nil && (err.Error() == "unsupported" || noModule(err)) {
		return ErrUnsupported
	}
	return err
}

// noModule reports whether the host has no dialog module: the phones'
// hosts answer so for a module they haven't ("no module method
// dialog.open").
func noModule(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "no module method dialog.")
}

// pickCopies is Open on a phone: the document picker, through
// device.PickFiles, its copies' paths.
func pickCopies(o OpenOptions, done func([]string, error)) {
	device.PickFiles(device.FileOptions{Types: mimeTypes(o.Filters), Multiple: o.Multiple}, func(files []device.File, err error) {
		var paths []string
		for _, f := range files {
			paths = append(paths, f.Path)
		}
		done(paths, err)
	})
}

// mimeTypes are the filters' MIME types, their extensions' too; none (any
// file) if an extension has no type the phone could match: a filter it
// can't name mustn't hide the user's files.
func mimeTypes(fs []Filter) []string {
	var types []string
	for _, f := range filters(fs) {
		types = append(types, f.Types...)
		for _, e := range f.Extensions {
			t, _, _ := strings.Cut(mime.TypeByExtension("."+e), ";")
			if t == "" {
				return nil
			}
			types = append(types, t)
		}
	}
	return types
}
