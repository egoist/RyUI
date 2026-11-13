package main

import (
	"debug/macho"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// A Mac app's signature carries the hardened runtime's entitlements for
// what its purpose strings ask for, and none it doesn't need.
func TestMacEntitlements(t *testing.T) {
	cfg := &appConfig{}
	cfg.permissionsFor([]string{"github.com/egoist/asuka/sdk/camera", "github.com/egoist/asuka/sdk/contacts", "github.com/egoist/asuka/sdk/location"})
	app := &goApp{cfg: cfg}
	f, err := writeMacEntitlements(app, t.TempDir())
	if err != nil || f == "" {
		t.Fatalf("entitlements: %q, %v", f, err)
	}
	b, _ := os.ReadFile(f)
	s := string(b)
	for _, want := range []string{"com.apple.security.device.camera", "com.apple.security.personal-information.addressbook",
		"com.apple.security.personal-information.location"} {
		if !strings.Contains(s, want) {
			t.Errorf("no %s in\n%s", want, s)
		}
	}
	for _, not := range []string{"audio-input", "calendars", "aps-environment"} {
		if strings.Contains(s, not) {
			t.Errorf("%s in\n%s", not, s)
		}
	}
	if f, err := writeMacEntitlements(&goApp{cfg: &appConfig{}}, t.TempDir()); f != "" || err != nil {
		t.Errorf("an app that asks for nothing: %q, %v", f, err)
	}
}

// SQLite's library goes in the app's Frameworks, where the package finds
// it from Contents/MacOS (sqlite/locate_darwin.go): for both of the
// app's architectures.
func TestMacSQLiteLib(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "Notes.app")
	lib := filepath.Join(macPluginLibs(bundle), "libasqlite.dylib")
	if rel, _ := filepath.Rel(filepath.Join(bundle, "Contents", "MacOS"), lib); rel != filepath.Join("..", "Frameworks", "libasqlite.dylib") {
		t.Errorf("from the executable: %s", rel)
	}
	if err := writeTestPluginLib("darwin", lib); err != nil {
		t.Fatal(err)
	}
	f, err := macho.OpenFat(lib)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var cpus []macho.Cpu
	for _, a := range f.Arches {
		cpus = append(cpus, a.Cpu)
	}
	if !slices.Contains(cpus, macho.CpuArm64) || !slices.Contains(cpus, macho.CpuAmd64) {
		t.Errorf("architectures: %v", cpus)
	}
}

// The libraries in Frameworks are signed before the app, as it is (the
// hardened runtime loads only its own team's), without its entitlements.
func TestSignMacFrameworks(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "Notes.app")
	lib := filepath.Join(macPluginLibs(bundle), "libasqlite.dylib")
	os.MkdirAll(filepath.Dir(lib), 0o755)
	os.WriteFile(lib, []byte("lib"), 0o755)
	for _, id := range []string{"-", "Developer ID Application: X (T)"} {
		var calls []string
		run := func(dir string, env []string, cmd string, args ...string) error {
			calls = append(calls, cmd+" "+strings.Join(args, " "))
			return nil
		}
		if err := signMac(bundle, id, "ents.plist", run); err != nil {
			t.Fatal(err)
		}
		opts := ""
		if id != "-" {
			opts = " --options runtime --timestamp"
		}
		want := []string{
			"codesign --force --sign " + id + opts + " " + lib,
			"codesign --force --sign " + id + opts + " --entitlements ents.plist " + bundle,
			"codesign --verify --strict " + bundle,
		}
		if !slices.Equal(calls, want) {
			t.Errorf("%s:\n%s\nwant\n%s", id, strings.Join(calls, "\n"), strings.Join(want, "\n"))
		}
	}
}

// A Mac build of an app that uses asuka/sdk/sqlite ships the library in its
// Frameworks, signed with it, and is built without the copy the package
// embeds: it runs from the .app, finding the library there.
func TestBuildMacSQLite(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("a Mac build signs on a Mac")
	}
	app := sqliteTestApp(t)
	out := t.TempDir()
	if err := buildMac(app, &buildOptions{out: out}); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(out, app.displayName()+".app")
	bin := filepath.Join(bundle, "Contents", "MacOS", sanitizeExe(app.displayName()))
	if tags := buildTags(t, bin); tags != pluginTag {
		t.Errorf("tags: %q", tags)
	}
	if b, err := exec.Command("codesign", "--verify", "--deep", "--strict", bundle).CombinedOutput(); err != nil {
		t.Errorf("codesign --verify --deep: %v\n%s", err, b)
	}
	db := filepath.Join(t.TempDir(), "runs.db")
	for _, want := range []string{"runs 1, SQLite 3.", "runs 2, SQLite 3."} {
		c := exec.Command(bin, db)
		c.Env = append(os.Environ(), "ASUKA_SQLITE_LIB=")
		b, err := c.CombinedOutput()
		if err != nil || !strings.HasPrefix(string(b), want) {
			t.Fatalf("%v: %s, want %s", err, b, want)
		}
	}
	// Without it, the app says where it looked.
	os.Remove(filepath.Join(macPluginLibs(bundle), "libasqlite.dylib"))
	if b, err := exec.Command(bin).CombinedOutput(); err == nil || !strings.Contains(string(b), "Frameworks/libasqlite.dylib") {
		t.Errorf("without the library: %v, %s", err, b)
	}
}
