package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestAccent(t *testing.T) {
	dir := t.TempDir()
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, "nui.json"), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"accent": "#7c3aed", "android": {"dynamicColor": true}}`)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := "-ldflags=-s -w -X github.com/egoist/nui/internal/boot.Accent=#7c3aed -X github.com/egoist/nui/internal/boot.DynamicColor=1"
	if got := c.ldflags(true); got != want {
		t.Fatalf("ldflags %q", got)
	}
	if got := (&appConfig{}).ldflags(false); got != "-ldflags=" {
		t.Fatalf("no accent: %q", got)
	}
	write(`{"accent": "purple"}`)
	if _, err := loadConfig(dir); err == nil {
		t.Fatal("a bad accent was taken")
	}
}

func TestLinks(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "nui.json"), []byte(`{"id": "com.example.notes", "scheme": "notes", "links": ["notes.example.com"],
		"android": {"permissions": ["android.permission.CAMERA"]}}`), 0o644)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	m := c.androidManifest(true)
	for _, want := range []string{`android.permission.CAMERA`, `android:scheme="notes"`, `android:autoVerify="true"`,
		`android:host="notes.example.com"`, `usesCleartextTraffic`} {
		if !strings.Contains(m, want) {
			t.Errorf("manifest lacks %s:\n%s", want, m)
		}
	}
	if strings.Contains(c.androidManifest(false), "Cleartext") {
		t.Error("a release manifest allows cleartext")
	}
	if d := c.associatedDomains(true); len(d) != 1 || d[0] != "applinks:notes.example.com?mode=developer" {
		t.Errorf("domains %v", d)
	}
	os.WriteFile(filepath.Join(dir, "nui.json"), []byte(`{"scheme": "my app"}`), 0o644)
	if _, err := loadConfig(dir); err == nil {
		t.Error("a bad scheme was taken")
	}
}

// shareTarget: on iOS, the share extension's scheme beside the app's
// own, and the extension's Info.plist, which takes the app's name,
// version and build keys; on Android, the ACTION_SEND filter.
func TestShareTarget(t *testing.T) {
	c := &appConfig{ID: "com.example.dl", Scheme: "dl", ShareTarget: true}
	got := plist(map[string]any{"CFBundleURLTypes": c.urlTypes(true)})
	if !strings.Contains(got, "<string>dl</string>\n\t\t\t\t<string>nuishare.com.example.dl</string>") {
		t.Errorf("iOS URL types:\n%s", got)
	}
	if t2 := c.urlTypes(false); strings.Contains(plist(map[string]any{"T": t2}), "nuishare") {
		t.Error("macOS has the share scheme")
	}
	if (&appConfig{ID: "x"}).urlTypes(true) != nil {
		t.Error("URL types with no scheme")
	}
	info := c.shareInfo(map[string]any{"CFBundleName": "DL", "CFBundleVersion": "7", "DTSDKName": "iphoneos27.1",
		"CFBundleIdentifier": "com.example.dl", "UILaunchScreen": map[string]any{}})
	if info["CFBundleIdentifier"] != "com.example.dl.share" || info["CFBundleName"] != "DL" || info["CFBundleVersion"] != "7" ||
		info["DTSDKName"] != "iphoneos27.1" || info["NuiShareScheme"] != "nuishare.com.example.dl" || info["UILaunchScreen"] != nil ||
		info["CFBundlePackageType"] != "XPC!" {
		t.Errorf("extension's Info.plist: %v", info)
	}
	if !strings.Contains(c.androidManifest(false), "android.intent.action.SEND") {
		t.Error("no ACTION_SEND filter")
	}
}

func TestFeaturesFromImports(t *testing.T) {
	var c appConfig
	c.permissionsFor([]string{"github.com/egoist/nui/ui", "github.com/egoist/nui/scan", "github.com/egoist/nui/camera"})
	if got := c.gradleModules(); got != "-PnuiModules=scan" {
		t.Errorf("modules: %s", got)
	}
	if !slices.Contains(c.Android.Permissions, "android.permission.CAMERA") || slices.Contains(c.Android.Permissions, "android.permission.POST_NOTIFICATIONS") {
		t.Errorf("permissions: %v", c.Android.Permissions)
	}
	var none appConfig
	none.permissionsFor([]string{"github.com/egoist/nui/ui"})
	if got := none.gradleModules(); got != "-PnuiModules=" {
		t.Errorf("no modules: %s", got)
	}
	if p := playerConfig(); !slices.Contains(p.androidModules, "scan") {
		t.Errorf("the player's modules: %v", p.androidModules)
	}
}

func TestIPad(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "nui.json"), []byte(`{"orientations": ["portrait"]}`), 0o644)
	c, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	// An iPad app, in every orientation whatever the iPhone's: Split
	// View and Stage Manager need them.
	if d := c.iosDevices(); !slices.Equal(d, []int{1, 2}) {
		t.Errorf("devices %v", d)
	}
	if o := c.ipadOrientations(); len(o) != 4 {
		t.Errorf("iPad orientations %v", o)
	}
	os.WriteFile(filepath.Join(dir, "nui.json"), []byte(`{"ios": {"iphoneOnly": true}}`), 0o644)
	if c, err = loadConfig(dir); err != nil {
		t.Fatal(err)
	}
	if d := c.iosDevices(); !slices.Equal(d, []int{1}) || c.ipadOrientations() != nil {
		t.Errorf("iPhone only: devices %v, iPad orientations %v", d, c.ipadOrientations())
	}
}
