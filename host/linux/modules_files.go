//go:build linux && !android

package linux

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/localfile"
)

// The modules that take files from the user and hand them to the system,
// on Linux: device's pickers (photos, files: GtkFileDialog, the portal's
// in a sandbox), its clipboard beyond text (GdkClipboard), asuka/sdk/library
// (the user's folders: XDG's Pictures, Videos, Music, Downloads),
// asuka/sdk/preview (the file's app, GtkFileLauncher) and ui.ShareFiles (no
// share sheet on Linux: the files are copied, as text is).

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["photos"] = map[string]method{
			// args: u8 limit; result: the picked images' paths, a line each.
			"pick": func(args []byte, reply func([]byte, error)) {
				limit := 1
				if len(args) > 0 {
					limit = int(args[0])
				}
				h.pickFiles("Choose Images", []string{"image/*"}, limit != 1, limit, func(files []pickedFile, err error) {
					var paths []string
					for _, f := range files {
						paths = append(paths, f.Path)
					}
					reply([]byte(strings.Join(paths, "\n")), err)
				})
			},
		}
		m["files"] = map[string]method{
			// args: u8 multiple, then MIME types a line each; result: JSON
			// of the copies (none: cancelled).
			"pick": func(args []byte, reply func([]byte, error)) {
				multiple := len(args) > 0 && args[0] == 1
				var mimes []string
				if len(args) > 1 {
					mimes = strings.Split(string(args[1:]), "\n")
				}
				h.pickFiles("", mimes, multiple, 0, func(files []pickedFile, err error) {
					if err != nil || len(files) == 0 {
						reply(nil, err)
						return
					}
					b, err := json.Marshal(files)
					reply(b, err)
				})
			},
		}
		m["clipboard"] = h.clipboardModule()
		m["library"] = map[string]method{"save": h.librarySave}
		m["preview"] = map[string]method{"show": h.preview}
		m["share"] = map[string]method{"files": h.shareFiles}
	})
}

// dialogParent is the window dialogs belong to: the one in use.
func (h *Host) dialogParent() Ptr {
	if w := h.activeWin; w != nil {
		return w.window
	}
	if h.main != nil {
		return h.main.window
	}
	return 0
}

// modCacheDir is the app's folder in the user's caches.
func (h *Host) modCacheDir() string { return localfile.CacheDir(h.appID) }

// pickedFile is device.File.
type pickedFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// pickFiles shows the open dialog (MIME types, "image/*" a family's;
// none: any file) and copies what's chosen to the caches (as iOS and
// Android give them), off the main thread. Cancelled: no files, no error.
func (h *Host) pickFiles(title string, mimes []string, multiple bool, limit int, done func([]pickedFile, error)) {
	d := Sys.FileDialogNew()
	if title != "" {
		Sys.FileDialogSetTitle(d, title)
	}
	Sys.FileDialogSetModal(d, true)
	if f := fileFilter(mimes); f != 0 {
		store := Sys.ListStoreNew(Sys.FileFilterGetType())
		Sys.ListStoreAppend(store, f)
		Sys.FileDialogSetFilters(d, store)
		Sys.FileDialogSetDefaultFilter(d, f)
		ObjectUnref(store)
		ObjectUnref(f)
	}
	parent := h.dialogParent()
	finish := func(paths []string, gerr Ptr) {
		ObjectUnref(d)
		if gerr != 0 {
			if Cancelled(gerr) {
				ErrorFree(gerr)
				done(nil, nil)
				return
			}
			done(nil, GError(gerr))
			return
		}
		if limit > 0 && len(paths) > limit {
			paths = paths[:limit]
		}
		if len(paths) == 0 {
			done(nil, nil)
			return
		}
		dir := filepath.Join(h.modCacheDir(), "asuka-files")
		go func() {
			files, err := copyPicked(dir, paths)
			h.Post(func() { done(files, err) })
		}()
	}
	if multiple {
		Async(func(a AsyncCall) { Sys.FileDialogOpenMultiple(d, parent, 0, a.Callback, a.Data) }, func(_, res Ptr) {
			var gerr Ptr
			list := Sys.FileDialogOpenMultipleFin(d, res, &gerr)
			var paths []string
			if list != 0 {
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
		})
		return
	}
	Async(func(a AsyncCall) { Sys.FileDialogOpen(d, parent, 0, a.Callback, a.Data) }, func(_, res Ptr) {
		var gerr Ptr
		f := Sys.FileDialogOpenFinish(d, res, &gerr)
		var paths []string
		if f != 0 {
			if p := GoStringFree(Sys.FileGetPath(f)); p != "" {
				paths = append(paths, p)
			}
			ObjectUnref(f)
		}
		finish(paths, gerr)
	})
}

// fileFilter is a filter of the MIME types (0: any file).
func fileFilter(mimes []string) Ptr {
	var ms []string
	for _, m := range mimes {
		m = strings.TrimSpace(m)
		if m == "" || m == "*/*" {
			return 0
		}
		ms = append(ms, m)
	}
	if len(ms) == 0 {
		return 0
	}
	f := Sys.FileFilterNew()
	names := map[string]string{"image/*": "Images", "video/*": "Videos", "audio/*": "Audio", "text/*": "Text"}
	name := strings.Join(ms, ", ")
	if len(ms) == 1 && names[ms[0]] != "" {
		name = names[ms[0]]
	}
	Sys.FileFilterSetName(f, name)
	for _, m := range ms {
		Sys.FileFilterAddMIMEType(f, m)
	}
	return f
}

// copyPicked copies the files into dir, each in a folder of its own (so
// its name stays its own).
func copyPicked(dir string, paths []string) ([]pickedFile, error) {
	out := []pickedFile{}
	for _, src := range paths {
		into := filepath.Join(dir, randomName())
		if err := os.MkdirAll(into, 0o755); err != nil {
			return nil, err
		}
		to := filepath.Join(into, filepath.Base(src))
		size, err := copyFile(src, to)
		if err != nil {
			return nil, err
		}
		out = append(out, pickedFile{Path: to, Name: filepath.Base(src), Type: mime.TypeByExtension(filepath.Ext(src)), Size: size})
	}
	return out, nil
}

func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	n, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return n, err
}

func randomName() string {
	var b [12]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// The clipboard beyond text.

// clipWatch is the clipboard module's watching: "next" waits for
// GdkClipboard's changed signal.
type clipWatch struct {
	waiting []func([]byte, error)
	handler uint64
	cb      Ptr
}

var clip clipWatch

func modClipboard() Ptr { return DisplayGetClipboard(DisplayGetDefault()) }

// clipboardModule is device's clipboard: "has" (u8 text, image, URL),
// "image" (the clipboard's image as a PNG in the caches; empty: none),
// "copyImage" (a file's path), and, while watching, "next" (answered at
// the next change) and "stop".
func (h *Host) clipboardModule() map[string]method {
	return map[string]method{
		"has": func(_ []byte, reply func([]byte, error)) {
			f := Sys.ClipboardGetFormats(modClipboard())
			text := Sys.ContentFormatsContainMIME(f, "text/plain;charset=utf-8") || Sys.ContentFormatsContainMIME(f, "text/plain") ||
				Sys.ContentFormatsContainGType(f, TypeString)
			img := Sys.ContentFormatsContainGType(f, Sys.TextureGetType()) || Sys.ContentFormatsContainMIME(f, "image/png")
			url := Sys.ContentFormatsContainMIME(f, "text/uri-list") || Sys.ContentFormatsContainGType(f, Sys.FileListGetType()) ||
				Sys.ContentFormatsContainGType(f, Sys.FileGetTypeOfObject())
			reply([]byte{b2u(text), b2u(img), b2u(url)}, nil)
		},
		"image": func(_ []byte, reply func([]byte, error)) {
			cb := modClipboard()
			f := Sys.ClipboardGetFormats(cb)
			if !Sys.ContentFormatsContainGType(f, Sys.TextureGetType()) && !Sys.ContentFormatsContainMIME(f, "image/png") {
				reply(nil, nil)
				return
			}
			Async(func(a AsyncCall) { Sys.ClipboardReadTextureAsync(cb, 0, a.Callback, a.Data) }, func(_, res Ptr) {
				var gerr Ptr
				tex := Sys.ClipboardReadTextureFinish(cb, res, &gerr)
				if tex == 0 {
					if gerr != 0 {
						ErrorFree(gerr)
					}
					reply(nil, nil)
					return
				}
				png := Sys.TextureSaveToPNGBytes(tex)
				ObjectUnref(tex)
				var n uintptr
				b := GoBytes(BytesGetData(png, &n), int(n))
				BytesUnref(png)
				dir := filepath.Join(h.modCacheDir(), "asuka-clipboard")
				if err := os.MkdirAll(dir, 0o755); err != nil {
					reply(nil, err)
					return
				}
				path := filepath.Join(dir, randomName()+".png")
				if err := os.WriteFile(path, b, 0o644); err != nil {
					reply(nil, err)
					return
				}
				reply([]byte(path), nil)
			})
		},
		"copyImage": func(args []byte, reply func([]byte, error)) {
			path := string(args)
			if _, err := os.Stat(path); err != nil {
				reply(nil, errors.New("no file at "+path))
				return
			}
			var gerr Ptr
			tex := TextureNewFromFilename(path, &gerr)
			if tex == 0 {
				if gerr != 0 {
					ErrorFree(gerr)
				}
				reply(nil, errors.New("not an image"))
				return
			}
			Sys.ClipboardSetTexture(modClipboard(), tex)
			ObjectUnref(tex)
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			c := &clip
			c.waiting = append(c.waiting, reply)
			if c.handler == 0 {
				c.cb = modClipboard()
				c.handler = Connect(c.cb, "changed", func() {
					ws := c.waiting
					c.waiting = nil
					c.stopWatching()
					for _, w := range ws {
						w(nil, nil)
					}
				})
			}
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			c := &clip
			ws := c.waiting
			c.waiting = nil
			c.stopWatching()
			for _, w := range ws {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

func (c *clipWatch) stopWatching() {
	if c.handler != 0 {
		Disconnect(c.cb, c.handler)
		c.handler = 0
	}
}

// asuka/sdk/library: the file copied into the user's folder for its
// collection (XDG's), in a folder of the app's if asked; a name taken
// gets a number. The answer is where it went.

// G_USER_DIRECTORY_*.
const (
	userDirDownload = 2
	userDirMusic    = 3
	userDirPictures = 4
	userDirVideos   = 7
)

// userSpecialDir is one of the user's folders (XDG's user-dirs), ""
// when it isn't set (tests replace it).
var userSpecialDir = func(id int32) string { return GoString(Sys.GetUserSpecialDir(id)) }

func (h *Host) librarySave(args []byte, reply func([]byte, error)) {
	var a struct {
		Path       string `json:"path"`
		Collection int    `json:"collection"`
		Name       string `json:"name"`
		Folder     string `json:"folder"`
		Move       bool   `json:"move"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Path == "" {
		reply(nil, errors.New("library.save: bad arguments"))
		return
	}
	dirs := [...]struct {
		id   int32
		name string
	}{{userDirDownload, "Downloads"}, {userDirVideos, "Videos"}, {userDirMusic, "Music"}, {userDirPictures, "Pictures"}}
	c := dirs[0]
	if a.Collection > 0 && a.Collection < len(dirs) {
		c = dirs[a.Collection]
	}
	dir := userSpecialDir(c.id)
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, c.name)
	}
	if f := strings.TrimSpace(a.Folder); f != "" {
		dir = filepath.Join(dir, filepath.Base(f))
	}
	name := filepath.Base(a.Name)
	if name == "" || name == "." || name == "/" {
		name = filepath.Base(a.Path)
	}
	go func() {
		to, err := saveInto(dir, name, a.Path, a.Move)
		h.Post(func() { reply([]byte(to), err) })
	}()
}

// saveInto copies (or moves) src into dir as name, numbered if it's
// taken ("Photo (2).jpg").
func saveInto(dir, name, src string, move bool) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 1; ; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		to := filepath.Join(dir, n)
		f, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		f.Close()
		if move && os.Rename(src, to) == nil {
			return to, nil
		}
		if _, err := copyFile(src, to); err != nil {
			os.Remove(to)
			return "", err
		}
		if move {
			os.Remove(src)
		}
		return to, nil
	}
}

// asuka/sdk/preview: the start file in its app (Linux has no Quick Look of
// its own), answered once it's handed over.

type modSource struct {
	Source string `json:"source"`
	Type   string `json:"type"`
	Name   string `json:"name"`
}

// localFiles finds the files sources name (downloading URLs) off the
// main thread, and calls done with them on it.
func (h *Host) localFiles(sources []modSource, done func([]string, error)) {
	assets, cache := h.assets, h.modCacheDir()
	if abs, err := filepath.Abs(assets); err == nil {
		assets = abs
	}
	go func() {
		paths := make([]string, len(sources))
		var err error
		for i, s := range sources {
			if paths[i], err = localfile.Resolve(s.Source, s.Type, assets, cache); err != nil {
				break
			}
		}
		h.Post(func() { done(paths, err) })
	}()
}

func (h *Host) preview(args []byte, reply func([]byte, error)) {
	var a struct {
		Files []modSource `json:"files"`
		Start int         `json:"start"`
	}
	if err := json.Unmarshal(args, &a); err != nil || len(a.Files) == 0 {
		reply(nil, errors.New("preview.show: bad arguments"))
		return
	}
	f := a.Files[min(max(a.Start, 0), len(a.Files)-1)]
	h.localFiles([]modSource{f}, func(paths []string, err error) {
		if err != nil {
			reply(nil, err)
			return
		}
		h.launchFile(paths[0], func(err error) {
			if err != nil {
				err = errors.New("no viewer")
			}
			reply(nil, err)
		})
	})
}

// launchFile opens a file in the user's app for its type (through the
// OpenURI portal in a sandbox).
func (h *Host) launchFile(path string, done func(error)) {
	file := Sys.FileNewForPath(path)
	l := Sys.FileLauncherNew(file)
	ObjectUnref(file)
	Async(func(a AsyncCall) { Sys.FileLauncherLaunch(l, h.dialogParent(), 0, a.Callback, a.Data) }, func(_, res Ptr) {
		var gerr Ptr
		ok := Sys.FileLauncherLaunchFinish(l, res, &gerr)
		ObjectUnref(l)
		if !ok {
			done(GError(gerr))
			return
		}
		done(nil)
	})
}

// shareFiles (ui.ShareFiles): Linux has no share sheet, so the files go
// on the clipboard (as a file list, which file managers paste), with a
// toast saying so, as shared text does. args: u32 view, string MIME,
// string UTI, string title, u32 count, the paths.
func (h *Host) shareFiles(args []byte, reply func([]byte, error)) {
	le := binary.LittleEndian
	if len(args) < 4 {
		reply(nil, errors.New("share.files: bad arguments"))
		return
	}
	rest := args[4:]
	for i := 0; i < 3; i++ { // MIME, UTI, title
		_, rest = readString(rest)
	}
	if len(rest) < 4 {
		reply(nil, errors.New("share.files: bad arguments"))
		return
	}
	n := int(le.Uint32(rest))
	rest = rest[4:]
	var files []Ptr
	for i := 0; i < n; i++ {
		var p string
		p, rest = readString(rest)
		if p != "" {
			files = append(files, Sys.FileNewForPath(p))
		}
	}
	if len(files) == 0 {
		reply(nil, errors.New("share.files: no files"))
		return
	}
	list := Sys.FileListNewFromArray(&files[0], uintptr(len(files)))
	var v Value
	ValueInit(&v, Sys.FileListGetType())
	Sys.ValueSetBoxed(&v, list)
	Sys.ClipboardSetValue(modClipboard(), &v)
	ValueUnset(&v)
	Call(Sym("g_boxed_free"), uintptr(Sys.FileListGetType()), list)
	for _, f := range files {
		ObjectUnref(f)
	}
	if len(files) == 1 {
		h.toast("File copied to clipboard")
	} else {
		h.toast("Files copied to clipboard")
	}
	reply(nil, nil)
}
