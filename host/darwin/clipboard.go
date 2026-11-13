//go:build darwin && !ios

package darwin

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/device's "files" and "clipboard" modules on a Mac.

// filesModule is device.PickFiles: "pick" shows an open panel (a sheet
// on the window in use). args: u8 multiple, then MIME types a line each
// (none: any file); the reply is a JSON array of copies in the caches
// (path, name, type, size), as iOS and Android give; none: cancelled.
func (h *Host) filesModule() map[string]method {
	return map[string]method{
		"pick": func(args []byte, reply func([]byte, error)) {
			multiple := len(args) > 0 && args[0] == 1
			var mimes []string
			if len(args) > 1 {
				mimes = strings.Split(string(args[1:]), "\n")
			}
			p := Send(Class("NSOpenPanel"), "openPanel")
			Send(p, "setCanChooseFiles:", 1)
			Send(p, "setCanChooseDirectories:", 0)
			Send(p, "setAllowsMultipleSelection:", Bool(multiple))
			if types := utTypes(mimes); types != 0 {
				Send(p, "setAllowedContentTypes:", uintptr(types))
			}
			Retain(p)
			var done objc.Block
			done = NewBlock(func(_ objc.Block, resp int) {
				var paths []string
				if resp == 1 { // NSModalResponseOK
					for _, u := range ArrayItems(Send(p, "URLs")) {
						paths = append(paths, GoString(Send(u, "path")))
					}
				}
				Release(p)
				h.Post(func() { done.Release() })
				if len(paths) == 0 {
					reply(nil, nil)
					return
				}
				dir := filepath.Join(cacheDir(), "asuka-files")
				go func() {
					out, err := copyPicked(dir, paths)
					h.Post(func() { reply(out, err) })
				}()
			})
			Send(p, "beginSheetModalForWindow:completionHandler:", uintptr(h.keyWin().window), uintptr(done))
		},
	}
}

// utTypes is an NSArray of the UTTypes of MIME types ("image/*" the
// family's), or 0 for any file.
func utTypes(mimes []string) ID {
	Dlopen("/System/Library/Frameworks/UniformTypeIdentifiers.framework/UniformTypeIdentifiers")
	if !HasClass("UTType") {
		return 0
	}
	var ts []ID
	for _, m := range mimes {
		var t ID
		switch m {
		case "", "*/*":
			return 0
		case "image/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.image")))
		case "video/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.movie")))
		case "audio/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.audio")))
		case "text/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.text")))
		case "application/*":
			t = Send(Class("UTType"), "typeWithIdentifier:", uintptr(String("public.data")))
		default:
			t = Send(Class("UTType"), "typeWithMIMEType:", uintptr(String(m)))
		}
		if t != 0 {
			ts = append(ts, t)
		}
	}
	if len(ts) == 0 {
		return 0
	}
	return Array(ts...)
}

// pickedFile is device.File.
type pickedFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Type string `json:"type"`
	Size int64  `json:"size"`
}

// copyPicked copies the files into dir, each in a directory of its own
// (so its name stays its own), and describes the copies as JSON.
func copyPicked(dir string, paths []string) ([]byte, error) {
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
	return json.Marshal(out)
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

// testPasteboard names a pasteboard the tests use instead of the user's.
var testPasteboard string

func clipPasteboard() ID {
	if testPasteboard != "" {
		return Send(Class("NSPasteboard"), "pasteboardWithName:", uintptr(String(testPasteboard)))
	}
	return Send(Class("NSPasteboard"), "generalPasteboard")
}

// clipboard is the clipboard module's watching: "next" waits for the
// general pasteboard's change count to move, which a timer checks (a Mac
// has no notification for it) while one waits.
type clipboard struct {
	waiting []func([]byte, error)
	seen    int
	timer   ID
}

// clipboardModule is asuka/device's clipboard: "has" (u8 text, image,
// URL), "image" (the clipboard's image as a file in the caches, in its
// own format; empty: none), "copyImage" (a file's path), and, while
// watching, "next" (answered at the next change) and "stop".
func (h *Host) clipboardModule() map[string]method {
	pb := clipPasteboard
	return map[string]method{
		"has": func(_ []byte, reply func([]byte, error)) {
			p := pb()
			text := Send(p, "availableTypeFromArray:", uintptr(Array(String("public.utf8-plain-text")))) != 0
			img := SendBool(p, "canReadObjectForClasses:options:", uintptr(Array(Class("NSImage"))), 0)
			url := Send(p, "availableTypeFromArray:", uintptr(Array(String("public.url"), String("public.file-url")))) != 0
			reply([]byte{b2u(text), b2u(img), b2u(url)}, nil)
		},
		"image": func(_ []byte, reply func([]byte, error)) {
			data, ext := clipboardImage(pb())
			if data == nil {
				reply(nil, nil)
				return
			}
			dir := filepath.Join(cacheDir(), "asuka-clipboard")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				reply(nil, err)
				return
			}
			f := filepath.Join(dir, randomName()+"."+ext)
			if err := os.WriteFile(f, data, 0o644); err != nil {
				reply(nil, err)
				return
			}
			reply([]byte(f), nil)
		},
		"copyImage": func(args []byte, reply func([]byte, error)) {
			path := string(args)
			img := Send(Send(Class("NSImage"), "alloc"), "initWithContentsOfFile:", uintptr(String(path)))
			if img == 0 {
				if _, err := os.Stat(path); err != nil {
					reply(nil, errors.New("no file at "+path))
				} else {
					reply(nil, errors.New("not an image"))
				}
				return
			}
			defer Release(img)
			p := pb()
			Send(p, "clearContents")
			// The file's own bytes, for apps that take its type, and the
			// image, for those that take AppKit's.
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
			if t := map[string]string{"png": "public.png", "jpg": "public.jpeg", "jpeg": "public.jpeg", "heic": "public.heic", "gif": "com.compuserve.gif"}[ext]; t != "" {
				if b, err := os.ReadFile(path); err == nil {
					Send(p, "setData:forType:", uintptr(Data(b)), uintptr(String(t)))
				}
			}
			Send(p, "writeObjects:", uintptr(Array(img)))
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			c := &h.clip
			if len(c.waiting) == 0 {
				c.seen = SendInt(pb(), "changeCount")
			}
			c.waiting = append(c.waiting, reply)
			if c.timer == 0 {
				t := Retain(MsgTargetTimer(Class("NSTimer"), Sel("timerWithTimeInterval:target:selector:userInfo:repeats:"), 0.5, h.target, Sel("clipboardTick:"), 0, true))
				Send(Send(Class("NSRunLoop"), "mainRunLoop"), "addTimer:forMode:", uintptr(t), uintptr(String("kCFRunLoopCommonModes")))
				c.timer = t
			}
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			ws := h.clip.waiting
			h.clip.waiting = nil
			h.stopClipboardTimer()
			for _, w := range ws {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// clipboardTick checks for a change while "next" waits.
func (h *Host) clipboardTick() {
	c := &h.clip
	n := SendInt(clipPasteboard(), "changeCount")
	if n == c.seen {
		return
	}
	c.seen = n
	ws := c.waiting
	c.waiting = nil
	h.stopClipboardTimer()
	for _, w := range ws {
		w(nil, nil)
	}
}

func (h *Host) stopClipboardTimer() {
	if t := h.clip.timer; t != 0 {
		Send(t, "invalidate")
		Release(t)
		h.clip.timer = 0
	}
}

// clipboardImage is the clipboard's image as it was copied (its bytes, in
// the first image type it has), or as PNG from what AppKit reads.
func clipboardImage(p ID) ([]byte, string) {
	for _, t := range []struct{ uti, ext string }{{"public.png", "png"}, {"public.jpeg", "jpg"}, {"public.heic", "heic"}, {"com.compuserve.gif", "gif"}} {
		if d := Send(p, "dataForType:", uintptr(String(t.uti))); d != 0 {
			return Bytes(d), t.ext
		}
	}
	imgs := Send(p, "readObjectsForClasses:options:", uintptr(Array(Class("NSImage"))), 0)
	items := ArrayItems(imgs)
	if len(items) == 0 {
		return nil, ""
	}
	tiff := Send(items[0], "TIFFRepresentation")
	rep := Send(Class("NSBitmapImageRep"), "imageRepWithData:", uintptr(tiff))
	if rep == 0 {
		return nil, ""
	}
	png := Send(rep, "representationUsingType:properties:", 4, uintptr(Dict())) // NSBitmapImageFileTypePNG
	if png == 0 {
		return nil, ""
	}
	return Bytes(png), "png"
}

func b2u(b bool) byte {
	if b {
		return 1
	}
	return 0
}
