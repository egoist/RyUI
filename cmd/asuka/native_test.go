package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testNativeApp is testdata/nativeapp, an app with a native package (the
// kitchen, internal/nativegen's testdata).
func testNativeApp(t *testing.T) *goApp {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "nativeapp"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	return &goApp{dir: dir, name: "nativeapp", cfg: cfg}
}

// An app's native packages are its dependencies with generated native
// code; each platform's are those with its project.
func TestNativePackages(t *testing.T) {
	app := testNativeApp(t)
	all, err := appNatives(app.dir, ".", "android", "arm64")
	if err != nil || len(all) != 1 || all[0].name != "kitchen" || !all[0].main {
		t.Fatalf("%+v %v", all, err)
	}
	for _, pl := range []string{"android", "web", "macos"} {
		if ps, err := nativeFor(app, pl, "arm64"); err != nil || len(ps) != 1 {
			t.Errorf("%s: %+v %v", pl, ps, err)
		}
	}
	if ps, err := nativeFor(sqliteTestApp(t), "android", "arm64"); err != nil || len(ps) != 0 {
		t.Errorf("sqliteapp: %+v %v", ps, err)
	}
	if err := app.cfg.androidNatives(app); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(app.cfg.gradleProps(), " ")
	want := "-PasukaModules= -PasukaNativeModules=native-github_com_egoist_asuka_internal_nativegen_testdata_kitchen=" + filepath.Join(all[0].dir, "android")
	if got != want {
		t.Errorf("gradle:\n  %s\nwant\n  %s", got, want)
	}
	scripts, err := nativeScripts(all)
	if err != nil {
		t.Fatal(err)
	}
	var entries []string
	for _, sc := range scripts {
		if sc.entry {
			entries = append(entries, sc.path)
		}
	}
	if len(scripts) != 2 || !slices.Equal(entries, []string{"packages/github_com_egoist_asuka_internal_nativegen_testdata_kitchen/kitchen.asuka.js"}) {
		t.Errorf("scripts %+v", scripts)
	}
}

// What a native package's Swift needs of the app: its Info.plist's keys
// (a purpose string as a usage the app's asuka.json says better; arrays
// joined; the rest where nothing set it), and its entitlements.
func TestNativeNeeds(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Info.plist"), []byte(plist(map[string]any{
		"NSBluetoothAlwaysUsageDescription": "To find it.", "UIBackgroundModes": []string{"bluetooth-central"}, "Flag": true, "Name": "x",
	})), 0o644)
	os.WriteFile(filepath.Join(dir, "iOS.entitlements"), []byte(plist(map[string]any{"com.example.n": []string{"TAG"}})), 0o644)
	c := &appConfig{}
	c.IOS.Usage = map[string]string{"NSBluetoothAlwaysUsageDescription": "The app's own."}
	c.IOS.BackgroundModes = []string{"audio"}
	if err := c.nativeNeeds([]nativePackage{{path: "example.com/ble", dir: dir}}, "ios"); err != nil {
		t.Fatal(err)
	}
	if c.IOS.Usage["NSBluetoothAlwaysUsageDescription"] != "The app's own." {
		t.Errorf("usage %v", c.IOS.Usage)
	}
	extras := c.plistExtras()
	if bg, _ := extras["UIBackgroundModes"].([]string); !slices.Equal(bg, []string{"audio", "bluetooth-central"}) || extras["Flag"] != true || extras["Name"] != "x" {
		t.Errorf("Info.plist %v", extras)
	}
	ent := filepath.Join(t.TempDir(), "profile.plist")
	os.WriteFile(ent, []byte(plist(map[string]any{})), 0o644)
	if m := c.missingEntitlements(ent); !slices.Equal(m, []string{"com.example.n (example.com/ble)"}) {
		t.Errorf("missing %v", m)
	}
	if err := (&appConfig{}).nativeNeeds([]nativePackage{{dir: t.TempDir()}}, "macos"); err != nil {
		t.Errorf("no Info.plist: %v", err)
	}
}

// The app's Swift package: the host's, and each native package's library
// products; two native packages SwiftPM would take for one say so.
func TestSwiftRootManifest(t *testing.T) {
	app := testNativeApp(t)
	natives, err := nativeFor(app, "ios", "arm64")
	if err != nil || len(natives) != 1 {
		t.Fatal(natives, err)
	}
	b := &swiftBuild{platform: "ios", natives: natives, defines: []string{"ASUKA_MAPS"}, cAsuka: map[string][]byte{}}
	root, err := b.rootManifest()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`.package(path: "../asuka")`, `.package(path: "` + natives[0].dir + `")`, `.product(name: "KitchenNative", package: "kitchen")`, `platforms: [.iOS("` + iosMin + `")]`} {
		if !strings.Contains(root, want) {
			t.Errorf("no %s in\n%s", want, root)
		}
	}
	host := b.hostManifest()
	if !strings.Contains(host, `name: "asuka"`) || !strings.Contains(host, `.define("ASUKA_MAPS")`) || !strings.Contains(host, `.target(name: "CAsuka")`) {
		t.Errorf("host:\n%s", host)
	}
	twin := natives[0]
	twin.path = "example.com/other/kitchen"
	b.natives = append(b.natives, twin)
	if _, err := b.rootManifest(); err == nil || !strings.Contains(err.Error(), "folder's name (kitchen)") {
		t.Errorf("two named kitchen: %v", err)
	}
}

// A web build puts packages' files in lib/ and packages/ of the host's:
// the host's files have neither.
func TestWebHostLeavesPackageDirs(t *testing.T) {
	src, _ := webSource(&goApp{})
	for _, d := range []string{"lib", "packages"} {
		if _, err := fs.Stat(src, d); err == nil {
			t.Errorf("the web host has %s/, where packages' files go", d)
		}
	}
}
