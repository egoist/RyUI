//go:build darwin && !ios

package darwin

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/asuka/internal/appdir"
	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/apps on a Mac: the .app bundles where Spotlight finds them,
// named by Launch Services (NSFileManager's displayNameAtPath:), their
// icons NSWorkspace's, drawn into a bitmap, and opened by NSWorkspace.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["apps"] = map[string]method{
			"list": func(_ []byte, reply func([]byte, error)) {
				h.offMain(func() ([]byte, error) { return json.Marshal(listApps(appDirs())) }, reply)
			},
			"icon": h.appIcon,
			"open": h.openApp,
		}
	})
}

// installedApp is apps.App on the wire.
type installedApp struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Path string `json:"path"`
}

// appDirs are the folders apps are installed in, as Spotlight and
// Launchpad find them.
func appDirs() []string {
	dirs := []string{"/Applications", "/System/Applications", "/System/Library/CoreServices/Applications"}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	return dirs
}

// finderApp is Finder, which lives with the system's services.
const finderApp = "/System/Library/CoreServices/Finder.app"

// listApps finds the .app bundles in dirs and in their folders one
// level down (Utilities, an app's own folder), never inside a bundle,
// then Finder: each once, where it is, or an app only a symbolic link
// points to (/Applications/Safari.app) at the link. Each is named as
// Finder names it, in the user's language, without .app; its ID is its
// bundle identifier, or its path without one. Off the main thread.
func listApps(dirs []string) []installedApp {
	isApp := func(name string) bool { return strings.HasSuffix(name, ".app") && !strings.HasPrefix(name, ".") }
	var found []string
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if isApp(e.Name()) {
				found = append(found, p)
				continue
			}
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if st, err := os.Stat(p); err != nil || !st.IsDir() {
				continue
			}
			sub, _ := os.ReadDir(p)
			for _, s := range sub {
				if isApp(s.Name()) {
					found = append(found, filepath.Join(p, s.Name()))
				}
			}
		}
	}
	found = append(found, finderApp)
	// Each app once: where it is, else at the first link to it.
	var paths []string
	seen := map[string]bool{}
	for _, links := range []bool{false, true} {
		for _, p := range found {
			st, err := os.Lstat(p)
			if err != nil || (st.Mode()&os.ModeSymlink != 0) != links {
				continue
			}
			real, err := filepath.EvalSymlinks(p)
			if err != nil || seen[real] {
				continue
			}
			if st, err := os.Stat(real); err != nil || !st.IsDir() {
				continue
			}
			seen[real] = true
			paths = append(paths, p)
		}
	}

	fm := Send(Class("NSFileManager"), "defaultManager")
	out := make([]installedApp, 0, len(paths))
	for _, p := range paths {
		WithPool(func() {
			ns := String(p)
			bundle := Send(Class("NSBundle"), "bundleWithPath:", uintptr(ns))
			if bundle == 0 {
				return
			}
			// Finder shows .app where the user has it show every
			// extension; Spotlight and Launchpad never do.
			a := installedApp{
				Name: strings.TrimSuffix(GoString(Send(fm, "displayNameAtPath:", uintptr(ns))), ".app"),
				ID:   GoString(Send(bundle, "bundleIdentifier")),
				Path: p,
			}
			if a.Name == "" {
				a.Name = strings.TrimSuffix(filepath.Base(p), ".app")
			}
			if a.ID == "" {
				a.ID = p // as Steam's shortcuts to its games, which have none
			}
			out = append(out, a)
		})
	}
	return out
}

// appIcon draws the icon Finder shows for an app (NSWorkspace's
// iconForFile:, through a symbolic link: without its arrow) as a PNG in
// the caches, size points square at the main display's scale, and
// answers with its path. A file drawn since the app last changed is
// used again.
func (h *Host) appIcon(args []byte, reply func([]byte, error)) {
	var a struct {
		Path string  `json:"path"`
		Size float64 `json:"size"`
	}
	if err := json.Unmarshal(args, &a); err != nil || a.Path == "" || !(a.Size > 0) {
		reply(nil, errors.New("apps.icon: bad arguments"))
		return
	}
	scale := 1.0
	if s := ArrayItems(Send(Class("NSScreen"), "screens")); len(s) > 0 {
		scale = max(1, MsgFloat(s[0], Sel("backingScaleFactor")))
	}
	px := int(math.Ceil(a.Size * scale))
	h.offMain(func() ([]byte, error) {
		real, err := filepath.EvalSymlinks(a.Path)
		if err != nil {
			return nil, fmt.Errorf("no app at %s", a.Path)
		}
		out := appIconFile(a.Path, px)
		if appIconFresh(out, real, filepath.Join(real, "Contents", "Info.plist")) {
			return []byte(out), nil
		}
		img := Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "iconForFile:", uintptr(String(real)))
		png := appImagePNG(img, a.Size, px)
		if png == nil {
			return nil, fmt.Errorf("couldn't draw the icon of %s", a.Path)
		}
		if err := writeAppIcon(out, png); err != nil {
			return nil, err
		}
		return []byte(out), nil
	}, reply)
}

// appImagePNG draws an NSImage into an sRGB bitmap px pixels square that's
// pt points square (so the image picks its representation for that
// scale), and encodes it as PNG; nil if it can't. In a pool.
func appImagePNG(img ID, pt float64, px int) []byte {
	if img == 0 {
		return nil
	}
	rep := Send(Send(Class("NSBitmapImageRep"), "alloc"),
		"initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bytesPerRow:bitsPerPixel:",
		0, uintptr(px), uintptr(px), 8, 4, 1, 0, uintptr(String("NSCalibratedRGBColorSpace")), 0, 0)
	if rep == 0 {
		return nil
	}
	Autorelease(rep)
	if srgb := Send(rep, "bitmapImageRepByRetaggingWithColorSpace:", uintptr(Send(Class("NSColorSpace"), "sRGBColorSpace"))); srgb != 0 {
		rep = srgb
	}
	MsgSetSize(rep, Sel("setSize:"), Size{Width: pt, Height: pt})
	ctx := Send(Class("NSGraphicsContext"), "graphicsContextWithBitmapImageRep:", uintptr(rep))
	if ctx == 0 {
		return nil
	}
	gc := Class("NSGraphicsContext")
	Send(gc, "saveGraphicsState")
	Send(gc, "setCurrentContext:", uintptr(ctx))
	Send(ctx, "setImageInterpolation:", 3) // NSImageInterpolationHigh
	MsgSetRect(img, Sel("drawInRect:"), Rect{Size: Size{Width: pt, Height: pt}})
	Send(ctx, "flushGraphics")
	Send(gc, "restoreGraphicsState")
	data := Send(rep, "representationUsingType:properties:", 4, uintptr(Dict())) // NSBitmapImageFileTypePNG
	if data == 0 {
		return nil
	}
	return Bytes(data)
}

// openApp starts the app at its path, or brings it to the front
// (NSWorkspace's openApplicationAtURL:configuration:completionHandler:,
// which answers off the main thread).
func (h *Host) openApp(args []byte, reply func([]byte, error)) {
	path := string(args)
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		reply(nil, fmt.Errorf("no app at %s", path))
		return
	}
	ws := Send(Class("NSWorkspace"), "sharedWorkspace")
	url := FileURL(path)
	if !HasClass("NSWorkspaceOpenConfiguration") { // before macOS 10.15
		if !SendBool(ws, "openURL:", uintptr(url)) {
			reply(nil, fmt.Errorf("couldn't open %s", path))
			return
		}
		reply(nil, nil)
		return
	}
	conf := Send(Class("NSWorkspaceOpenConfiguration"), "configuration")
	var done objc.Block
	done = NewBlock(func(_ objc.Block, _ ID, nserr ID) {
		var err error
		if nserr != 0 {
			err = errorText(nserr, "couldn't open "+path)
		}
		h.Post(func() {
			done.Release()
			reply(nil, err)
		})
	})
	Send(ws, "openApplicationAtURL:configuration:completionHandler:", uintptr(url), uintptr(conf), uintptr(done))
}

// appIconFile is where an app's icon px pixels square is kept: a file
// of the app's caches named for its path.
func appIconFile(path string, px int) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(appdir.Cache(), "asuka-apps", fmt.Sprintf("%x-%d.png", sum[:10], px))
}

// appIconFresh reports whether the icon at png was drawn after each of
// sources (the files it's drawn from) last changed.
func appIconFresh(png string, sources ...string) bool {
	st, err := os.Stat(png)
	if err != nil {
		return false
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
	}
	return err
}
