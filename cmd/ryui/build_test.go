package main

import (
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My App")
	os.MkdirAll(dir, 0o755)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "My App" || c.ID != "dev.ryui.my-app" || c.Version != "1.0" || c.Build != 1 || len(c.Orientations) != 2 {
		t.Fatalf("%+v", c)
	}
	os.WriteFile(filepath.Join(dir, "ryui.json"), []byte(`{"id":"com.x.y","icon":"i.png","orientations":["sideways"]}`), 0o644)
	if _, err := loadConfig(dir); err == nil || !strings.Contains(err.Error(), "sideways") {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "ryui.json"), []byte(`{"id":"com.x.y","icon":"i.png","build":3}`), 0o644)
	c, _ = loadConfig(dir)
	if c.ID != "com.x.y" || c.Build != 3 || c.Icon != filepath.Join(dir, "i.png") {
		t.Fatalf("%+v", c)
	}
}

func TestPlist(t *testing.T) {
	got := plist(map[string]any{"B": true, "A": "x & <y>", "C": []string{"p"}, "D": map[string]any{}, "E": []int{1}})
	for _, want := range []string{
		"<key>A</key>\n\t<string>x &amp; &lt;y&gt;</string>", "<key>B</key>\n\t<true/>",
		"<array>\n\t\t<string>p</string>", "<key>D</key>\n\t<dict>\n\t</dict>", "<integer>1</integer>",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in\n%s", want, got)
		}
	}
}

func TestProfileMatches(t *testing.T) {
	exact := &profile{appID: "TEAM.com.example.app"}
	prefix := &profile{appID: "TEAM.com.example.*"}
	any := &profile{appID: "TEAM.*"}
	other := &profile{appID: "OTHER.com.example.app"}
	if !(exact.matches("", "com.example.app") > prefix.matches("", "com.example.app") &&
		prefix.matches("", "com.example.app") > any.matches("", "com.example.app") &&
		any.matches("", "com.example.app") > 0) {
		t.Fatal("ranking")
	}
	if other.matches("TEAM", "com.example.app") != 0 || exact.matches("", "com.example.other") != 0 {
		t.Fatal("mismatch")
	}
}

func TestResize(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			c := color.NRGBA{200, 40, 40, 255}
			if x < 50 {
				c = color.NRGBA{0, 0, 0, 0} // transparent black: mustn't darken the red
			}
			src.SetNRGBA(x, y, c)
		}
	}
	d := resize(src, 10)
	if c := d.NRGBAAt(9, 5); c != (color.NRGBA{200, 40, 40, 255}) {
		t.Fatal(c)
	}
	if c := d.NRGBAAt(0, 5); c.A != 0 {
		t.Fatal(c)
	}
	// A pixel straddling the edge is half covered, and still red.
	e := resize(src, 3)
	if c := e.NRGBAAt(1, 1); c.R != 200 || c.A < 120 || c.A > 135 {
		t.Fatal(c)
	}
}

// The privacy manifest declares the categories of the APIs the binary
// imports or names, each with RyUI's reason, and no others.
func TestPrivacyAccessed(t *testing.T) {
	types := func(imported map[string]bool, data string) []string {
		var out []string
		for _, e := range privacyAccessed(imported, []byte(data)) {
			m := e.(map[string]any)
			out = append(out, m["NSPrivacyAccessedAPIType"].(string)+" "+m["NSPrivacyAccessedAPITypeReasons"].([]string)[0])
		}
		return out
	}
	// Go's runtime: its clock and stat.
	got := types(map[string]bool{"mach_absolute_time": true, "fstat64": true, "write": true}, "")
	want := []string{"NSPrivacyAccessedAPICategoryFileTimestamp C617.1", "NSPrivacyAccessedAPICategorySystemBootTime 35F9.1"}
	if !slices.Equal(got, want) {
		t.Errorf("Go's runtime: %v", got)
	}
	// SQLite's statfs, a class, a method's name (whole, not inside another).
	got = types(map[string]bool{"statfs": true, "OBJC_CLASS_$_NSUserDefaults": true}, "x\x00activeInputModes\x00y\x00notsystemUptime\x00")
	want = []string{"NSPrivacyAccessedAPICategoryDiskSpace E174.1", "NSPrivacyAccessedAPICategoryUserDefaults CA92.1", "NSPrivacyAccessedAPICategoryActiveKeyboards 54BD.1"}
	if !slices.Equal(got, want) {
		t.Errorf("the rest: %v", got)
	}
	if got := types(map[string]bool{"write": true}, ""); len(got) != 0 {
		t.Errorf("none: %v", got)
	}
}

// A profile's App Group is read and written whole: plutil would otherwise
// read the dots in the entitlement's name as steps into dictionaries.
func TestPlistKeyEntitlement(t *testing.T) {
	if _, err := exec.LookPath("plutil"); err != nil {
		t.Skip("no plutil")
	}
	ent := filepath.Join(t.TempDir(), "ent.plist")
	key := plistKey("com.apple.security.application-groups")
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>com.apple.security.application-groups</key><array><string>group.a</string><string>group.b</string></array></dict></plist>`
	if err := os.WriteFile(ent, []byte(plist), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("plutil", "-replace", key, "-json", `["group.b"]`, ent).CombinedOutput(); err != nil {
		t.Fatalf("replace: %v: %s", err, out)
	}
	out, err := exec.Command("plutil", "-extract", key, "json", "-o", "-", ent).Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(out)); got != `["group.b"]` {
		t.Fatalf("got %s", got)
	}
}
