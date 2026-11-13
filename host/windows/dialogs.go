//go:build windows

package windows

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"slices"
	"strings"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// asuka/sdk/dialog (the common item dialogs, modal to the window) and
// asuka/sdk/shell (Explorer, the Recycle Bin) on Windows.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["dialog"] = map[string]asyncMethod{
			"open":   func(args []byte, reply func([]byte, error)) { h.fileDialog("open", args, reply) },
			"save":   func(args []byte, reply func([]byte, error)) { h.fileDialog("save", args, reply) },
			"folder": func(args []byte, reply func([]byte, error)) { h.fileDialog("folder", args, reply) },
		}
		m["shell"] = map[string]asyncMethod{
			"reveal": func(args []byte, reply func([]byte, error)) {
				path := string(args)
				h.offMain(func() ([]byte, error) {
					if _, err := os.Lstat(path); err != nil {
						return nil, fmt.Errorf("no file at %s", path)
					}
					return nil, RevealInFolder(path)
				}, reply)
			},
			"open": func(args []byte, reply func([]byte, error)) {
				path := string(args)
				if _, err := os.Stat(path); err != nil {
					reply(nil, fmt.Errorf("no file at %s", path))
					return
				}
				// ShellExecute may ask which app opens a type that has none:
				// once the core has returned.
				h.afterCore(func() {
					err := ShellExecute(h.windowInUse(), "", path)
					if errors.Is(err, ErrNoAssociation) {
						err = errors.New("no app")
					}
					reply(nil, err)
				})
			},
			"trash": func(args []byte, reply func([]byte, error)) {
				path := string(args)
				h.offMain(func() ([]byte, error) { return nil, Trash(path) }, reply)
			},
			"beep": func(_ []byte, reply func([]byte, error)) {
				AlertBeep()
				reply(nil, nil)
			},
		}
	})
}

// dialogArgs are asuka/sdk/dialog's options.
type dialogArgs struct {
	Title    string         `json:"title"`
	Button   string         `json:"button"`
	Start    string         `json:"start"`
	Name     string         `json:"name"`
	Filters  []dialogFilter `json:"filters"`
	Multiple bool           `json:"multiple"`
	Hidden   bool           `json:"hidden"`
	View     uint32         `json:"view"`
}

type dialogFilter struct {
	Name       string   `json:"name"`
	Types      []string `json:"types"`
	Extensions []string `json:"extensions"`
}

// dialogCreated (tests) gets each file dialog before it shows.
var dialogCreated func(dlg uintptr)

// fileDialog shows the open dialog (kind "open", "folder") or the save
// dialog ("save") once the core has returned (it runs a modal loop),
// modal to the dialog's window, and answers with the paths (open, folder:
// JSON) or the path (save) chosen; nothing: cancelled.
func (h *Host) fileDialog(kind string, args []byte, reply func([]byte, error)) {
	var a dialogArgs
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("dialog."+kind+": bad arguments"))
		return
	}
	o := FileDialogOptions{Save: kind == "save", Folders: kind == "folder", Multiple: a.Multiple, Hidden: a.Hidden,
		Title: a.Title, Button: a.Button, Created: dialogCreated}
	o.Folder, o.Name = startIn(a.Start, kind == "folder")
	if a.Name != "" {
		o.Name = a.Name
	}
	switch {
	case kind == "folder":
		o.Name = ""
	case kind == "open":
		// A file that's there is selected (its name in the name field);
		// Name is Save's.
		if st, err := os.Stat(a.Start); err != nil || st.IsDir() {
			o.Name = ""
		} else {
			o.Name = filepath.Base(a.Start)
		}
	}
	if kind != "folder" {
		o.Filters, o.DefaultExt = dialogFilters(a.Filters)
	}
	h.afterCore(func() {
		h.modal++
		paths, err := FileDialog(h.dialogOwner(ops.NodeID(a.View)), o)
		h.modal--
		switch {
		case err != nil || len(paths) == 0:
			reply(nil, err)
		case kind == "save":
			reply([]byte(paths[0]), nil)
		default:
			b, err := json.Marshal(paths)
			reply(b, err)
		}
	})
}

// dialogOwner is the window a dialog is modal to: the view's (0: the one
// the user works in, or the sheet on it).
func (h *Host) dialogOwner(view ops.NodeID) uintptr {
	if n := h.nodes[view]; view != 0 && n != nil && n.hwnd != 0 {
		if w := RootWindow(n.hwnd); w != 0 && IsWindowVisible(w) {
			return w
		}
	}
	return h.windowInUse()
}

// startIn is the folder a dialog starts in, and the name it suggests,
// from a path: a folder, or a file (in a folder that's there).
func startIn(start string, folder bool) (dir, name string) {
	if start == "" {
		return "", ""
	}
	if st, err := os.Stat(start); err == nil && st.IsDir() {
		return start, ""
	}
	if folder {
		return filepath.Dir(start), ""
	}
	return filepath.Dir(start), filepath.Base(start)
}

// typeFamilies are the extensions of a family of MIME types, as
// Windows's own apps know them.
var typeFamilies = map[string][]string{
	"image/*": {"png", "jpg", "jpeg", "gif", "bmp", "webp", "heic", "heif", "tif", "tiff", "ico", "svg", "avif"},
	"video/*": {"mp4", "m4v", "mov", "webm", "avi", "wmv", "mkv"},
	"audio/*": {"mp3", "m4a", "aac", "wav", "flac", "ogg", "opus", "wma"},
	"text/*":  {"txt", "csv", "md", "html", "htm", "xml", "json", "log"},
	"font/*":  {"ttf", "otf", "woff", "woff2"},
}

// dialogFilters are the dialog's file types (each filter's patterns: its
// extensions, its MIME types' as the registry gives them) and the
// extension a name typed without one gets (the first filter's first).
func dialogFilters(fs []dialogFilter) ([]FileFilter, string) {
	var out []FileFilter
	def := ""
	for _, f := range fs {
		var pats []string
		add := func(ext string) {
			p := "*." + strings.TrimPrefix(ext, ".")
			if ext == "*" {
				p = "*.*"
			}
			if !slices.Contains(pats, p) {
				pats = append(pats, p)
			}
		}
		for _, e := range f.Extensions {
			add(e)
		}
		for _, t := range f.Types {
			switch {
			case t == "*/*" || t == "*" || t == "application/*":
				add("*")
			case typeFamilies[t] != nil:
				for _, e := range typeFamilies[t] {
					add(e)
				}
			default:
				exts, _ := mime.ExtensionsByType(t)
				for _, e := range exts {
					add(e)
				}
			}
		}
		if len(pats) == 0 {
			continue
		}
		if def == "" && len(out) == 0 && pats[0] != "*.*" {
			def = strings.TrimPrefix(pats[0], "*.")
		}
		out = append(out, FileFilter{Name: f.Name, Patterns: strings.Join(pats, ";")})
	}
	return out, def
}
