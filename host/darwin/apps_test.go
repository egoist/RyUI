//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// asuka/sdk/apps: the list has the system's apps, named and with their
// bundle IDs, and Finder; each once.
func TestAppsList(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	var b []byte
	var err error
	onMain(func() { b, err = await(h, "apps", "list", nil) })
	if err != nil {
		t.Fatal(err)
	}
	var list []installedApp
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	byID := map[string]installedApp{}
	paths := map[string]bool{}
	for _, a := range list {
		if a.Name == "" || a.ID == "" || a.Path == "" || strings.HasSuffix(a.Name, ".app") {
			t.Errorf("an app %+v", a)
		}
		if paths[a.Path] {
			t.Errorf("%s twice", a.Path)
		}
		paths[a.Path] = true
		byID[a.ID] = a
	}
	for id, path := range map[string]string{
		"com.apple.calculator": "/System/Applications/Calculator.app",
		"com.apple.TextEdit":   "/System/Applications/TextEdit.app",
		"com.apple.finder":     "/System/Library/CoreServices/Finder.app",
		"com.apple.Terminal":   "/System/Applications/Utilities/Terminal.app", // a folder down
	} {
		a, ok := byID[id]
		if !ok {
			t.Errorf("no %s in %d apps", id, len(list))
			continue
		}
		if a.Path != path {
			t.Errorf("%s at %s, want %s", id, a.Path, path)
		}
	}
	// Safari is a link in /Applications to the system's cryptex: listed
	// once, at the link.
	if a, ok := byID["com.apple.Safari"]; ok && a.Path != "/Applications/Safari.app" {
		t.Errorf("Safari at %s", a.Path)
	}

	// A folder of the test's: apps one level down, not inside a bundle,
	// nor in a hidden folder; one without a bundle ID is its path.
	dir := t.TempDir()
	mkApp := func(p, id string) {
		os.MkdirAll(filepath.Join(p, "Contents", "MacOS"), 0o755)
		plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleName</key><string>` +
			strings.TrimSuffix(filepath.Base(p), ".app") + `</string>`
		if id != "" {
			plist += `<key>CFBundleIdentifier</key><string>` + id + `</string>`
		}
		os.WriteFile(filepath.Join(p, "Contents", "Info.plist"), []byte(plist+`</dict></plist>`), 0o644)
	}
	mkApp(filepath.Join(dir, "One.app"), "test.asuka.one")
	mkApp(filepath.Join(dir, "Folder", "Two.app"), "test.asuka.two")
	mkApp(filepath.Join(dir, "One.app", "Contents", "Helper.app"), "test.asuka.helper")
	mkApp(filepath.Join(dir, "Folder", "Deeper", "Three.app"), "test.asuka.three")
	mkApp(filepath.Join(dir, ".Hidden", "Four.app"), "test.asuka.four")
	mkApp(filepath.Join(dir, "NoID.app"), "")
	os.Symlink(filepath.Join(dir, "One.app"), filepath.Join(dir, "Folder", "Link.app"))
	var got []installedApp
	onMain(func() { got = listApps([]string{dir}) })
	var names []string
	for _, a := range got {
		if a.Path == finderApp {
			continue
		}
		names = append(names, fmt.Sprintf("%s %s %s", a.Name, a.ID, strings.TrimPrefix(a.Path, dir)))
	}
	slices.Sort(names)
	want := []string{
		"NoID " + filepath.Join(dir, "NoID.app") + " /NoID.app",
		"One test.asuka.one /One.app",
		"Two test.asuka.two /Folder/Two.app",
	}
	if strings.Join(names, "\n") != strings.Join(want, "\n") {
		t.Fatalf("listApps:\n%s\nwant\n%s", strings.Join(names, "\n"), strings.Join(want, "\n"))
	}
}

// asuka/sdk/apps: Icon writes a PNG of the size at the main display's
// scale, kept and used again; a missing app fails.
func TestAppsIcon(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("ASUKA_CACHE", cache)
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	var scale float64
	onMain(func() { scale = MsgFloat(ArrayItems(Send(Class("NSScreen"), "screens"))[0], Sel("backingScaleFactor")) })
	icon := func(path string, size float64) (string, error) {
		args, _ := json.Marshal(map[string]any{"path": path, "size": size})
		var b []byte
		var err error
		onMain(func() { b, err = await(h, "apps", "icon", args) })
		return string(b), err
	}
	for _, size := range []float64{32, 17.5} {
		p, err := icon("/System/Applications/Calculator.app", size)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(p, cache) {
			t.Fatalf("the icon at %s, not in the caches", p)
		}
		f, err := os.Open(p)
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		px := int(math.Ceil(size * scale))
		if b := img.Bounds(); b.Dx() != px || b.Dy() != px {
			t.Fatalf("a %v point icon is %v pixels, want %d", size, b, px)
		}
		// Not all transparent: the icon's drawn.
		_, _, _, a := img.At(px/2, px/2).RGBA()
		if a == 0 {
			t.Fatalf("the icon's middle is transparent")
		}
		// Asked again, the same file, not drawn again.
		st, _ := os.Stat(p)
		p2, err := icon("/System/Applications/Calculator.app", size)
		st2, _ := os.Stat(p2)
		if err != nil || p2 != p || !st2.ModTime().Equal(st.ModTime()) {
			t.Fatalf("again: %s %v (%v, was %v)", p2, err, st2.ModTime(), st.ModTime())
		}
	}
	// Through a link: the app's icon (Safari's link in /Applications).
	if _, err := os.Lstat("/Applications/Safari.app"); err == nil {
		if _, err := icon("/Applications/Safari.app", 16); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := icon(filepath.Join(t.TempDir(), "Missing.app"), 16); err == nil || !strings.Contains(err.Error(), "Missing.app") {
		t.Fatalf("a missing app: %v", err)
	}
	var err error
	onMain(func() { _, err = await(h, "apps", "icon", []byte(`{"path":"/x.app"}`)) })
	if err == nil {
		t.Fatal("no size: no error")
	}
}

// asuka/sdk/apps: Open of a missing app fails (no app is launched here).
func TestAppsOpenMissing(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	var err error
	onMain(func() { _, err = await(h, "apps", "open", []byte(filepath.Join(t.TempDir(), "Missing.app"))) })
	if err == nil || !strings.Contains(err.Error(), "Missing.app") {
		t.Fatalf("open of a missing app: %v", err)
	}
}
