package main

import (
	"debug/elf"
	"debug/macho"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/asuka/plugin"
)

// writeTestPluginLib writes the sqlite plugin's library for target to
// path, as testdata/sqliteapp's build would ship it.
func writeTestPluginLib(target, path string) error {
	dir, err := filepath.Abs(filepath.Join("testdata", "sqliteapp"))
	if err != nil {
		return err
	}
	goos, goarch, _ := strings.Cut(target, "-")
	switch target {
	case "darwin":
		goarch = "arm64"
	case "ios", "ios-simulator":
		goos, goarch = "ios", "arm64"
	}
	libs, err := appLibs(dir, goos, goarch, target)
	if err != nil {
		return err
	}
	if len(libs) != 1 || libs[0].lib.Name != "asqlite" {
		return fmt.Errorf("sqliteapp's libraries for %s: %v", target, libs)
	}
	return libs[0].write(path)
}

// An app's plugins are its dependencies with a manifest: sqliteapp has
// the sqlite plugin, with a library for every target; the CLI itself
// (which imports package plugin, not a plugin) has none.
func TestAppPlugins(t *testing.T) {
	app := sqliteTestApp(t)
	ps, err := appPlugins(app.dir, "linux", "amd64")
	if err != nil || len(ps) != 1 || ps[0].m.Name != "sqlite" || filepath.Base(ps[0].dir) != "sqlite" {
		t.Fatalf("%v %v", ps, err)
	}
	none, err := appPlugins(".", "linux", "amd64")
	if err != nil || len(none) != 0 {
		t.Fatalf("the CLI's: %v %v", none, err)
	}
	for _, target := range []string{"darwin", "ios", "ios-simulator", "android-arm64", "android-arm", "android-amd64",
		"linux-amd64", "linux-arm64", "windows-amd64", "windows-arm64", "wasm"} {
		libs, err := appLibs(app.dir, "linux", "amd64", target)
		if err != nil || len(libs) != 1 || libs[0].file.Name == "" {
			t.Errorf("%s: %v %v", target, libs, err)
		}
	}
	if _, err := appLibs(app.dir, "linux", "amd64", "plan9-amd64"); err == nil {
		t.Error("a target the plugin hasn't: no error")
	}
	if pluginBuildFlags(nil) != nil || len(pluginBuildFlags([]pluginLib{{}})) != 1 {
		t.Error("pluginBuildFlags")
	}
}

// A web build puts the plugins' files in lib/ of the host's files and
// names each in the page's libs: sqliteapp's asqlite.wasm, which the host
// starts SQLite's worker with.
func TestWebPluginFiles(t *testing.T) {
	app := sqliteTestApp(t)
	files, err := webPluginFiles(app)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := files["lib/asqlite.wasm"]; !ok || len(files) != 1 {
		t.Fatalf("files: %v", files)
	}
	opts := map[string]any{}
	webPluginOptions(files, "asuka/0123/", opts)
	if want := map[string]string{"asqlite.wasm": "asuka/0123/lib/asqlite.wasm"}; !reflect.DeepEqual(opts["libs"], want) {
		t.Errorf("libs: %v, want %v", opts["libs"], want)
	}
	none := map[string]any{}
	webPluginOptions(nil, "asuka/", none)
	if _, ok := none["libs"]; ok {
		t.Error("an app without plugins: libs")
	}
}

// The libraries go in one place on every platform, so two plugins with a
// file of the same name for a target don't build; for another target
// they may.
func TestPluginLibsSameName(t *testing.T) {
	plug := func(name, lib string, files map[string]string) appPlugin {
		l := plugin.Library{Name: lib, Files: map[string]plugin.File{}}
		for target, f := range files {
			l.Files[target] = plugin.File{Name: f}
		}
		return appPlugin{dir: name, m: &plugin.Manifest{Name: name, Libraries: []plugin.Library{l}}}
	}
	a := plug("a", "liba", map[string]string{"wasm": "x.wasm", "linux-amd64": "liba.so"})
	b := plug("b", "libb", map[string]string{"wasm": "x.wasm", "linux-amd64": "libb.so"})
	if _, err := pluginLibs([]appPlugin{a, b}, "wasm"); err == nil || !strings.Contains(err.Error(), "plugins a and b both have a file x.wasm") {
		t.Errorf("same name: %v", err)
	}
	if libs, err := pluginLibs([]appPlugin{a, b}, "linux-amd64"); err != nil || len(libs) != 2 {
		t.Errorf("different names: %v %v", libs, err)
	}
}

// Each ABI's Go library gets the plugins' libraries for that ABI beside
// it: the sqlite plugin's an ELF shared library for the architecture,
// with 16 KB pages.
func TestAndroidPluginLibraries(t *testing.T) {
	machines := map[string]elf.Machine{"arm64": elf.EM_AARCH64, "arm": elf.EM_ARM, "amd64": elf.EM_X86_64}
	app := sqliteTestApp(t)
	for _, a := range androidABIs {
		t.Run(a.abi, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "jniLibs", a.abi)
			libs, err := appLibs(app.dir, "android", a.goarch, "android-"+a.goarch)
			if err != nil {
				t.Fatal(err)
			}
			if err := writeLibs(libs, dir); err != nil {
				t.Fatal(err)
			}
			f, err := elf.Open(filepath.Join(dir, "libasqlite.so"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if f.Type != elf.ET_DYN || f.Machine != machines[a.goarch] {
				t.Errorf("%v %v, want a shared library for %v", f.Type, f.Machine, machines[a.goarch])
			}
			for _, p := range f.Progs {
				if p.Type == elf.PT_LOAD && p.Align < 16384 {
					t.Errorf("a segment aligned to %d: Android 15's 16 KB pages need 16384", p.Align)
				}
			}
			syms, _ := f.DynamicSymbols()
			if !slices.ContainsFunc(syms, func(s elf.Symbol) bool { return s.Name == "asq_call" }) {
				t.Error("no asq_call")
			}
			if soname, _ := f.DynString(elf.DT_SONAME); len(soname) > 0 && soname[0] != "libasqlite.so" {
				t.Errorf("soname %q", soname)
			}
		})
	}
}

// asuka dev puts the plugins' libraries beside the Go code's in
// files/asuka, read-only, and removes an earlier build's.
func TestDevLibsScript(t *testing.T) {
	with := devLibsScript("/data/local/tmp/go.so", []devLib{{tmp: "/data/local/tmp/sq.so", name: "libasqlite.so"}})
	for _, want := range []string{
		"rm -f files/asuka/*.so",
		"cp /data/local/tmp/go.so files/asuka/libasuka.so && chmod 444 files/asuka/libasuka.so",
		"cp /data/local/tmp/sq.so files/asuka/libasqlite.so && chmod 444 files/asuka/libasqlite.so",
	} {
		if !strings.Contains(with, want) {
			t.Errorf("%q lacks %q", with, want)
		}
	}
	if without := devLibsScript("/data/local/tmp/go.so", nil); strings.Contains(without, "sq.so") {
		t.Errorf("without plugins: %q", without)
	}
}

// A plugin's library's framework is the library, an Info.plist that
// names it and a privacy manifest; the library is a dylib for the
// framework's platform and the app's minimum iOS, loaded by its path in
// the app's Frameworks.
func TestPluginFramework(t *testing.T) {
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no Xcode")
	}
	app := sqliteTestApp(t)
	for _, c := range []struct {
		sdk, platform string
		machoPlatform uint32 // LC_BUILD_VERSION's: 2 iOS, 7 the simulator
		arches        []macho.Cpu
	}{
		{"iphoneos", "iPhoneOS", 2, []macho.Cpu{macho.CpuArm64}},
		{"iphonesimulator", "iPhoneSimulator", 7, []macho.Cpu{macho.CpuAmd64, macho.CpuArm64}},
	} {
		t.Run(c.sdk, func(t *testing.T) {
			dir := t.TempDir()
			// One already there (an earlier build's) is replaced whole.
			os.MkdirAll(filepath.Join(dir, "asqlite.framework", "stale"), 0o755)
			tc := &iosToolchain{sdkName: c.sdk}
			libs, err := tc.pluginLibs(app)
			if err != nil || len(libs) != 1 {
				t.Fatal(libs, err)
			}
			fw, err := tc.writePluginFramework(dir, libs[0])
			if err != nil {
				t.Fatal(err)
			}
			if fw != filepath.Join(dir, "asqlite.framework") {
				t.Fatal(fw)
			}
			ents, _ := os.ReadDir(fw)
			var names []string
			for _, e := range ents {
				names = append(names, e.Name())
			}
			if want := []string{"Info.plist", "PrivacyInfo.xcprivacy", "asqlite"}; !slices.Equal(names, want) {
				t.Fatalf("framework holds %v, want %v", names, want)
			}
			info := plistJSON(t, filepath.Join(fw, "Info.plist"))
			for k, want := range map[string]any{
				"CFBundleExecutable": "asqlite", "CFBundlePackageType": "FMWK", "CFBundleIdentifier": "dev.asuka.plugin.asqlite",
				"CFBundleShortVersionString": "3.54.1", "MinimumOSVersion": iosMin, "CFBundleSupportedPlatforms": []any{c.platform},
			} {
				if !reflect.DeepEqual(info[k], want) {
					t.Errorf("Info.plist's %s is %v, want %v", k, info[k], want)
				}
			}
			privacy := plistJSON(t, filepath.Join(fw, "PrivacyInfo.xcprivacy"))
			var reasons []string
			for _, e := range privacy["NSPrivacyAccessedAPITypes"].([]any) {
				m := e.(map[string]any)
				reasons = append(reasons, fmt.Sprint(m["NSPrivacyAccessedAPIType"], " ", m["NSPrivacyAccessedAPITypeReasons"]))
			}
			want := []string{"NSPrivacyAccessedAPICategoryFileTimestamp [C617.1]", "NSPrivacyAccessedAPICategoryDiskSpace [E174.1]"}
			if !slices.Equal(reasons, want) || privacy["NSPrivacyTracking"] != false {
				t.Errorf("privacy manifest: %v", privacy)
			}
			arches := checkFrameworkDylib(t, filepath.Join(fw, "asqlite"), c.machoPlatform)
			if !slices.Equal(arches, c.arches) {
				t.Errorf("architectures %v, want %v", arches, c.arches)
			}
		})
	}
}

// embedPlugins puts a framework in the app's Frameworks for each of its
// plugins' libraries, and none for an app without plugins.
func TestEmbedPlugins(t *testing.T) {
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no Xcode")
	}
	tc := &iosToolchain{sdkName: "iphonesimulator"}
	bundle := filepath.Join(t.TempDir(), "A.app")
	if err := tc.embedPlugins(bundle, &goApp{dir: "."}); err != nil || exists(filepath.Join(bundle, "Frameworks")) {
		t.Fatalf("without plugins: %v", err)
	}
	if err := tc.embedPlugins(bundle, sqliteTestApp(t)); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(bundle, "Frameworks", "asqlite.framework", "asqlite")) {
		t.Fatal("no asqlite.framework in the app's Frameworks")
	}
}

// checkFrameworkDylib checks the Mach-O at path is a dylib named
// @rpath/asqlite.framework/asqlite, built for platform and iosMin, that
// exports asq_call, and returns its architectures.
func checkFrameworkDylib(t *testing.T, path string, platform uint32) []macho.Cpu {
	t.Helper()
	var files []*macho.File
	if fat, err := macho.OpenFat(path); err == nil {
		defer fat.Close()
		for _, a := range fat.Arches {
			files = append(files, a.File)
		}
	} else {
		f, err := macho.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		files = append(files, f)
	}
	var arches []macho.Cpu
	for _, f := range files {
		arches = append(arches, f.Cpu)
		if f.Type != macho.TypeDylib {
			t.Errorf("%v: type %v, not a dylib", f.Cpu, f.Type)
		}
		var id string
		var plat, minos uint32
		for _, l := range f.Loads {
			raw := l.Raw()
			switch binary.LittleEndian.Uint32(raw) {
			case 0xd: // LC_ID_DYLIB
				off := binary.LittleEndian.Uint32(raw[8:])
				id = strings.TrimRight(string(raw[off:]), "\x00")
			case 0x32: // LC_BUILD_VERSION
				plat, minos = binary.LittleEndian.Uint32(raw[8:]), binary.LittleEndian.Uint32(raw[12:])
			}
		}
		if id != "@rpath/asqlite.framework/asqlite" {
			t.Errorf("%v: install name %q", f.Cpu, id)
		}
		if got := fmt.Sprintf("%d.%d", minos>>16, minos>>8&0xff); plat != platform || got != iosMin {
			t.Errorf("%v: built for platform %d, iOS %s; want %d, %s", f.Cpu, plat, got, platform, iosMin)
		}
		if f.Symtab == nil || !slices.ContainsFunc(f.Symtab.Syms, func(s macho.Symbol) bool { return s.Name == "_asq_call" }) {
			t.Errorf("%v: no asq_call", f.Cpu)
		}
	}
	return arches
}

// plistJSON reads the property list at path, through plutil.
func plistJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	out, err := exec.Command("plutil", "-convert", "json", "-o", "-", path).Output()
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// The release URL's repository and tag, for asuka plugin publish.
func TestGitHubRelease(t *testing.T) {
	repo, tag, err := githubRelease("https://github.com/egoist/asuka/releases/download/plugin-sqlite")
	if err != nil || repo != "egoist/asuka" || tag != "plugin-sqlite" {
		t.Fatal(repo, tag, err)
	}
	for _, bad := range []string{"https://example.com/a/b/releases/download/t", "https://github.com/a/b", "::"} {
		if _, _, err := githubRelease(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}
