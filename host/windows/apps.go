//go:build windows

package windows

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	stdimage "image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"github.com/egoist/asuka/internal/appdir"
	. "github.com/egoist/asuka/internal/windows"
)

// asuka/sdk/apps on Windows: the Start menu's All apps, the shell's
// Applications folder (FOLDERID_AppsFolder: desktop apps by their
// shortcuts, packaged apps by their AppUserModelIDs, as PowerToys'
// Command Palette lists them), each item's icon from the shell
// (IShellItemImageFactory), and the item opened with its default verb.
// Each runs on a thread of its own, in a single-threaded apartment.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["apps"] = map[string]asyncMethod{
			"list": func(_ []byte, reply func([]byte, error)) {
				h.offMain(func() (b []byte, err error) {
					withSTA(func() {
						var list []installedApp
						if list, err = listApps(); err == nil {
							b, err = json.Marshal(list)
						}
					})
					return b, err
				}, reply)
			},
			"icon": h.appIcon,
			"open": func(args []byte, reply func([]byte, error)) {
				path := string(args)
				if !isShellItem(path) {
					if _, err := os.Stat(path); err != nil {
						reply(nil, fmt.Errorf("no app at %s", path))
						return
					}
				}
				h.offMain(func() (_ []byte, err error) {
					withSTA(func() { err = ShellExecute(0, "", path) })
					return nil, err
				}, reply)
			},
		}
	})
}

var (
	appsOle32           = syscall.NewLazyDLL("ole32.dll")
	appsShell           = syscall.NewLazyDLL("shell32.dll")
	appsGDI             = syscall.NewLazyDLL("gdi32.dll")
	procAppsCoInit      = appsOle32.NewProc("CoInitializeEx")
	procAppsCoUninit    = appsOle32.NewProc("CoUninitialize")
	procKnownFolderItem = appsShell.NewProc("SHGetKnownFolderItem")
	procAppsGetObject   = appsGDI.NewProc("GetObjectW")

	folderIDAppsFolder       = NewGUID("1e87508d-89c2-42f0-8a7e-645a0f50ca58")
	bhidEnumItems            = NewGUID("94f60519-2850-4924-aa5a-d15e84868039")
	iidAppsShellItem         = NewGUID("43826d1e-e718-42ee-bc55-a1e261c37bfe")
	iidEnumShellItems        = NewGUID("70629033-e363-4a28-a567-0db78006e6d7")
	iidShellItemImageFactory = NewGUID("bcc18b79-ba16-442f-80c4-8a59c30c463b")
)

// IShellItem's and IEnumShellItems' methods, and the names an item has.
const (
	siBindToHandler    = 3
	siGetDisplayName   = 5
	esiNext            = 3
	sigdnNormalDisplay = 0
	sigdnParentParsing = 0x80018001 // SIGDN_PARENTRELATIVEPARSING
	siifGetImage       = 3
	siigbfIconOnly     = 0x4
)

// appsFolder is the shell's path of an item of the Applications folder,
// before its ID.
const appsFolder = `shell:AppsFolder\`

// installedApp is apps.App on the wire.
type installedApp struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Path string `json:"path"`
}

// withSTA runs f with COM in a single-threaded apartment on this thread
// (the shell's objects are apartment-threaded): call it on a goroutine
// of its own.
func withSTA(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr, _, _ := procAppsCoInit.Call(0, 0x2|0x4); !Failed(hr) { // COINIT_APARTMENTTHREADED | COINIT_DISABLE_OLE1DDE
		defer procAppsCoUninit.Call()
	}
	f()
}

// listApps lists the Applications folder's items: each named as the
// Start menu names it, its ID its parsing name in the folder (an
// AppUserModelID, or a desktop app's path under a known folder), its
// path the item's (shell:AppsFolder\<ID>). In an apartment.
func listApps() ([]installedApp, error) {
	var folder uintptr
	if r, _, _ := procKnownFolderItem.Call(uintptr(unsafe.Pointer(&folderIDAppsFolder)), 0, 0,
		uintptr(unsafe.Pointer(&iidAppsShellItem)), uintptr(unsafe.Pointer(&folder))); Failed(r) || folder == 0 {
		return nil, fmt.Errorf("the Applications folder (0x%x)", uint32(r))
	}
	defer Release(folder)
	var items uintptr
	if r := ComCall(folder, siBindToHandler, 0, uintptr(unsafe.Pointer(&bhidEnumItems)),
		uintptr(unsafe.Pointer(&iidEnumShellItems)), uintptr(unsafe.Pointer(&items))); Failed(r) || items == 0 {
		return nil, fmt.Errorf("the Applications folder's items (0x%x)", uint32(r))
	}
	defer Release(items)
	var out []installedApp
	for {
		var item uintptr
		var fetched uint32
		if r := ComCall(items, esiNext, 1, uintptr(unsafe.Pointer(&item)), uintptr(unsafe.Pointer(&fetched))); r != 0 || fetched == 0 || item == 0 {
			break // S_FALSE: the end
		}
		a := installedApp{Name: itemName(item, sigdnNormalDisplay), ID: itemName(item, sigdnParentParsing)}
		Release(item)
		if a.Name == "" || a.ID == "" {
			continue
		}
		a.Path = appsFolder + a.ID
		out = append(out, a)
	}
	return out, nil
}

// itemName is a shell item's name of a kind (SIGDN), or "".
func itemName(item uintptr, sigdn uint32) string {
	var p uintptr
	if Failed(ComCall(item, siGetDisplayName, uintptr(sigdn), uintptr(unsafe.Pointer(&p)))) {
		return ""
	}
	return TakeString(p)
}

// isShellItem reports whether path names an item of the shell's
// namespace rather than a file: shell:AppsFolder\..., ::{CLSID}.
func isShellItem(path string) bool {
	return len(path) > 6 && strings.EqualFold(path[:6], "shell:") || strings.HasPrefix(path, "::{")
}

// appIcon draws the icon the shell shows for an item (the Start menu's
// for an app) as a PNG in the caches, size points square at the
// system's DPI, and answers with its path. A file is used again if it
// was drawn since the app's file last changed, or, for an item that's
// no file (a packaged app), since the app started.
func (h *Host) appIcon(args []byte, reply func([]byte, error)) {
	var a struct {
		Path string  `json:"path"`
		Size float64 `json:"size"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Path == "" || !(a.Size > 0) {
		reply(nil, errors.New("apps.icon: bad arguments"))
		return
	}
	px := int(math.Ceil(a.Size * float64(DPI(0)) / 96))
	h.offMain(func() (b []byte, err error) {
		out := appIconFile(a.Path, px)
		var sources []string
		if !isShellItem(a.Path) {
			if _, err := os.Stat(a.Path); err != nil {
				return nil, fmt.Errorf("no app at %s", a.Path)
			}
			sources = []string{a.Path}
		}
		if appIconFresh(out, sources...) {
			return []byte(out), nil
		}
		withSTA(func() { b, err = shellIconPNG(a.Path, px) })
		if err != nil {
			return nil, err
		}
		if err := writeAppIcon(out, b); err != nil {
			return nil, err
		}
		return []byte(out), nil
	}, reply)
}

// shellIconPNG draws the shell's icon of an item, px pixels square
// (IShellItemImageFactory, without thumbnails), as a PNG. In an
// apartment.
func shellIconPNG(path string, px int) ([]byte, error) {
	item := ShellItem(path)
	if item == 0 {
		return nil, fmt.Errorf("no app at %s", path)
	}
	defer Release(item)
	factory := QueryInterface(item, &iidShellItemImageFactory)
	if factory == 0 {
		return nil, fmt.Errorf("no icon for %s", path)
	}
	defer Release(factory)
	var hbm uintptr
	size := uintptr(uint32(px)) | uintptr(uint32(px))<<32 // a SIZE, by value
	if r := ComCall(factory, siifGetImage, size, siigbfIconOnly, uintptr(unsafe.Pointer(&hbm))); Failed(r) || hbm == 0 {
		return nil, fmt.Errorf("no icon for %s (0x%x)", path, uint32(r))
	}
	defer DeleteObject(hbm)
	img, err := dibImage(hbm)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// dibSection is a DIBSECTION, as GetObject fills it.
type dibSection struct {
	bmType, bmWidth, bmHeight, bmWidthBytes int32
	bmPlanes, bmBitsPixel                   uint16
	bmBits                                  uintptr
	biSize                                  uint32
	biWidth, biHeight                       int32
	_                                       [7]uint32 // the rest of the BITMAPINFOHEADER
	_                                       [3]uint32 // dsBitfields
	_                                       uintptr   // dshSection
	_                                       uint32    // dsOffset
}

// dibImage copies a 32-bit DIB section of the shell's, its pixels BGRA
// with straight alpha, as Go's NRGBA images have them.
func dibImage(hbm uintptr) (*stdimage.NRGBA, error) {
	var ds dibSection
	if r, _, _ := procAppsGetObject.Call(hbm, unsafe.Sizeof(ds), uintptr(unsafe.Pointer(&ds))); r == 0 || ds.bmBits == 0 || ds.bmBitsPixel != 32 {
		return nil, fmt.Errorf("the icon isn't a 32-bit bitmap (%d bits)", ds.bmBitsPixel)
	}
	w, h, stride := int(ds.bmWidth), int(ds.bmHeight), int(ds.bmWidthBytes)
	if h < 0 {
		h = -h
	}
	src := unsafe.Slice(Native[byte](ds.bmBits), stride*h)
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, w, h))
	for y := range h {
		row := y
		if ds.biHeight > 0 { // bottom-up
			row = h - 1 - y
		}
		s := src[row*stride:]
		d := img.Pix[y*img.Stride:]
		for x := range w {
			d[4*x], d[4*x+1], d[4*x+2], d[4*x+3] = s[4*x+2], s[4*x+1], s[4*x], s[4*x+3]
		}
	}
	return img, nil
}

// appIconFile is where an app's icon px pixels square is kept: a file
// of the app's caches named for its path.
func appIconFile(path string, px int) string {
	sum := sha256.Sum256([]byte(strings.ToLower(path)))
	return filepath.Join(appdir.Cache(), "asuka-apps", fmt.Sprintf("%x-%d.png", sum[:10], px))
}

// drawnIcons are the icons drawn since the app started.
var drawnIcons struct {
	sync.Mutex
	files map[string]bool
}

// appIconFresh reports whether the icon at png was drawn after each of
// sources last changed; with no sources, whether it was drawn since the
// app started.
func appIconFresh(png string, sources ...string) bool {
	st, err := os.Stat(png)
	if err != nil {
		return false
	}
	if len(sources) == 0 {
		drawnIcons.Lock()
		defer drawnIcons.Unlock()
		return drawnIcons.files[png]
	}
	for _, s := range sources {
		if sst, err := os.Stat(s); err == nil && !sst.ModTime().Before(st.ModTime()) {
			return false
		}
	}
	return true
}

// writeAppIcon writes an icon's PNG to path through a file beside it,
// renamed over it: a reader never sees half a file.
func writeAppIcon(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".icon-*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		os.Remove(f.Name())
		return err
	}
	drawnIcons.Lock()
	if drawnIcons.files == nil {
		drawnIcons.files = map[string]bool{}
	}
	drawnIcons.files[path] = true
	drawnIcons.Unlock()
	return nil
}
