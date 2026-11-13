//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// asuka/dialog (NSOpenPanel, NSSavePanel: sheets on the window) and
// asuka/shell (NSWorkspace, NSFileManager) on a Mac.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["dialog"] = map[string]method{
			"open":   func(args []byte, reply func([]byte, error)) { h.fileDialog("open", args, reply) },
			"save":   func(args []byte, reply func([]byte, error)) { h.fileDialog("save", args, reply) },
			"folder": func(args []byte, reply func([]byte, error)) { h.fileDialog("folder", args, reply) },
		}
		m["shell"] = map[string]method{
			"reveal": shellReveal,
			"open":   shellOpen,
			"trash":  func(args []byte, reply func([]byte, error)) { h.shellTrash(args, reply) },
			"beep": func(_ []byte, reply func([]byte, error)) {
				beep()
				reply(nil, nil)
			},
		}
	})
}

// dialogArgs are asuka/dialog's options.
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
	Hidden   bool   `json:"hidden"`
	View     uint32 `json:"view"`
}

// panelsOpen are the panels on screen (tests answer them).
var panelsOpen = map[ID]bool{}

// fileDialog shows an open panel (kind "open", "folder") or a save panel
// ("save") as a sheet on the dialog's window, and answers with the paths
// (open, folder: JSON) or the path (save) chosen; nothing: cancelled.
func (h *Host) fileDialog(kind string, args []byte, reply func([]byte, error)) {
	var a dialogArgs
	if err := json.Unmarshal(args, &a); err != nil {
		reply(nil, errors.New("dialog."+kind+": bad arguments"))
		return
	}
	var p ID
	if kind == "save" {
		p = Send(Class("NSSavePanel"), "savePanel")
	} else {
		p = Send(Class("NSOpenPanel"), "openPanel")
		Send(p, "setCanChooseFiles:", Bool(kind == "open"))
		Send(p, "setCanChooseDirectories:", Bool(kind == "folder"))
		Send(p, "setAllowsMultipleSelection:", Bool(a.Multiple))
		Send(p, "setResolvesAliases:", 1)
	}
	// A sheet has no title bar: the title is the panel's message too.
	if a.Title != "" {
		Send(p, "setTitle:", uintptr(String(a.Title)))
		Send(p, "setMessage:", uintptr(String(a.Title)))
	}
	if a.Button != "" {
		Send(p, "setPrompt:", uintptr(String(a.Button)))
	}
	Send(p, "setShowsHiddenFiles:", Bool(a.Hidden))
	Send(p, "setCanCreateDirectories:", Bool(kind != "open"))
	dir, name := startIn(a.Start, kind == "folder")
	if dir != "" {
		Send(p, "setDirectoryURL:", uintptr(FileURL(dir)))
	}
	if a.Name != "" {
		name = a.Name
	}
	if kind == "save" && name != "" {
		Send(p, "setNameFieldStringValue:", uintptr(String(name)))
	}
	if kind != "folder" {
		var types []ID
		for _, f := range a.Filters {
			ts := contentTypes(f.Types, f.Extensions)
			if ts == nil { // a filter of any file
				types = nil
				break
			}
			types = append(types, ts...)
		}
		if len(types) > 0 {
			Send(p, "setAllowedContentTypes:", uintptr(Array(types...)))
		}
	}

	Retain(p)
	panelsOpen[p] = true
	var done objc.Block
	done = NewBlock(func(_ objc.Block, resp int) {
		delete(panelsOpen, p)
		var out []byte
		if resp == 1 { // NSModalResponseOK
			if kind == "save" {
				out = []byte(GoString(Send(Send(p, "URL"), "path")))
			} else {
				var paths []string
				for _, u := range ArrayItems(Send(p, "URLs")) {
					paths = append(paths, GoString(Send(u, "path")))
				}
				if len(paths) > 0 {
					out, _ = json.Marshal(paths)
				}
			}
		}
		Release(p)
		h.Post(func() { done.Release() })
		reply(out, nil)
	})
	if w := h.dialogWindow(ops.NodeID(a.View)); w != 0 {
		Send(p, "beginSheetModalForWindow:completionHandler:", uintptr(w), uintptr(done))
		return
	}
	// No window on screen (all closed, the app in the background): a
	// panel of its own, in front.
	Send(Send(Class("NSApplication"), "sharedApplication"), "activateIgnoringOtherApps:", 1)
	Send(p, "beginWithCompletionHandler:", uintptr(done))
}

// dialogWindow is the window a dialog is a sheet on: the window of the
// view (0: the one the user works in), or the sheet on it (a Sheet's, an
// alert's), as a sheet on a window with one waits for it to close; 0 if
// it isn't on screen.
func (h *Host) dialogWindow(view ops.NodeID) ID {
	var w ID
	if n := h.nodes[view]; view != 0 && n != nil && n.view != 0 {
		w = Send(n.view, "window")
	}
	if w == 0 {
		if k := h.keyWin(); k != nil {
			w = k.window
		}
	}
	for w != 0 {
		s := Send(w, "attachedSheet")
		if s == 0 {
			break
		}
		w = s
	}
	if w == 0 || !SendBool(w, "isVisible") {
		return 0
	}
	return w
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

// contentTypes are the UTTypes of MIME types ("image/*" the family's)
// and extensions; nil for any file.
func contentTypes(mimes, exts []string) []ID {
	Dlopen("/System/Library/Frameworks/UniformTypeIdentifiers.framework/UniformTypeIdentifiers")
	if !HasClass("UTType") {
		return nil
	}
	var ts []ID
	for _, m := range mimes {
		var t ID
		switch m {
		case "*/*", "*":
			return nil
		case "image/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.image")))
		case "video/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.movie")))
		case "audio/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.audio")))
		case "text/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.text")))
		case "font/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.font")))
		case "application/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.data")))
		default:
			t = Send(Class("UTType"), "typeWithMIMEType:", uintptr(String(m)))
		}
		if t != 0 {
			ts = append(ts, t)
		}
	}
	for _, e := range exts {
		if e == "*" {
			return nil
		}
		// An extension no app declares is a dynamic type of its own.
		if t := Send(Class("UTType"), "typeWithFilenameExtension:", uintptr(String(e))); t != 0 {
			ts = append(ts, t)
		}
	}
	return ts
}

// asuka/shell.

// shellReveal shows the item selected in a Finder window of its folder.
func shellReveal(args []byte, reply func([]byte, error)) {
	path := string(args)
	if _, err := os.Lstat(path); err != nil {
		reply(nil, fmt.Errorf("no file at %s", path))
		return
	}
	Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "activateFileViewerSelectingURLs:", uintptr(Array(FileURL(path))))
	reply(nil, nil)
}

// shellOpen opens the item in its app (a folder in Finder), as a double
// click in Finder does.
func shellOpen(args []byte, reply func([]byte, error)) {
	path := string(args)
	if _, err := os.Stat(path); err != nil {
		reply(nil, fmt.Errorf("no file at %s", path))
		return
	}
	ws := Send(Class("NSWorkspace"), "sharedWorkspace")
	u := FileURL(path)
	if Send(ws, "URLForApplicationToOpenURL:", uintptr(u)) == 0 {
		reply(nil, errors.New("no app"))
		return
	}
	if !SendBool(ws, "openURL:", uintptr(u)) {
		reply(nil, fmt.Errorf("couldn't open %s", path))
		return
	}
	reply(nil, nil)
}

// shellTrash moves the item to the trash of its volume (NSFileManager,
// off the main thread), which fails where the volume has none: it never
// deletes.
func (h *Host) shellTrash(args []byte, reply func([]byte, error)) {
	path := string(args)
	if _, err := os.Lstat(path); err != nil {
		reply(nil, fmt.Errorf("no file at %s", path))
		return
	}
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		var err error
		WithPool(func() {
			var nserr ID
			fm := Send(Class("NSFileManager"), "defaultManager")
			if !SendBool(fm, "trashItemAtURL:resultingItemURL:error:", uintptr(FileURL(path)), 0, uintptr(unsafe.Pointer(&nserr))) {
				err = errorText(nserr, "couldn't move "+path+" to the Trash")
			}
		})
		h.Post(func() { reply(nil, err) })
	}()
}

var nsBeep func()

// beep is NSBeep: the user's alert sound.
func beep() {
	if nsBeep == nil {
		purego.RegisterLibFunc(&nsBeep, AppKit(), "NSBeep")
	}
	nsBeep()
}
