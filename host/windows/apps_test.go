//go:build windows

package windows

import (
	"encoding/json"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ui"
)

// asuka/sdk/apps: the Start menu's apps, named, by ID, at their items; an
// icon of the right pixel size for one, and for a program's file, kept
// and used again; a missing app fails.
func TestApps(t *testing.T) {
	cache := t.TempDir()
	t.Setenv("ASUKA_CACHE", cache)
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	var b []byte
	var err error
	onMain(t, func() { b, err, _ = await(h, "apps", "list", nil) })
	if err != nil {
		t.Fatal(err)
	}
	var list []installedApp
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("no apps")
	}
	var explorer *installedApp
	for i, a := range list {
		if a.Name == "" || a.ID == "" || a.Path != appsFolder+a.ID {
			t.Errorf("an app %+v", a)
		}
		if a.ID == "Microsoft.Windows.Explorer" {
			explorer = &list[i]
		}
	}
	t.Logf("%d apps; File Explorer: %+v", len(list), explorer)

	icon := func(path string, size float64) (string, error) {
		args, _ := json.Marshal(map[string]any{"path": path, "size": size})
		var b []byte
		var err error
		onMain(t, func() { b, err, _ = await(h, "apps", "icon", args) })
		return string(b), err
	}
	px := int(math.Ceil(32 * float64(DPI(0)) / 96))
	item := list[0].Path
	if explorer != nil {
		item = explorer.Path
	}
	exe := filepath.Join(os.Getenv("SystemRoot"), "explorer.exe")
	for _, path := range []string{item, exe} {
		p, err := icon(path, 32)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
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
		if b := img.Bounds(); b.Dx() != px || b.Dy() != px {
			t.Fatalf("%s: a 32 point icon is %v pixels, want %d", path, b, px)
		}
		st, _ := os.Stat(p)
		p2, err := icon(path, 32)
		st2, _ := os.Stat(p2)
		if err != nil || p2 != p || !st2.ModTime().Equal(st.ModTime()) {
			t.Fatalf("%s again: %s %v", path, p2, err)
		}
	}
	if _, err := icon(filepath.Join(t.TempDir(), "missing.exe"), 16); err == nil || !strings.Contains(err.Error(), "missing.exe") {
		t.Fatalf("a missing app's icon: %v", err)
	}
	onMain(t, func() { _, err, _ = await(h, "apps", "open", []byte(filepath.Join(t.TempDir(), "missing.exe"))) })
	if err == nil || !strings.Contains(err.Error(), "missing.exe") {
		t.Fatalf("open of a missing app: %v", err)
	}
}
