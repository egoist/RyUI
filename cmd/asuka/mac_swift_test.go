package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// An app's native packages' Swift (testdata/nativeapp's: the kitchen,
// internal/nativegen's testdata) is built with Asuka's Swift into the
// bundle's library, with Swift's concurrency runtime beside it for macOS
// 11, and signed with the app; unchanged, it isn't built again; for an app
// without native packages, the library goes.
func TestMacSwift(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Swift for the Mac builds on a Mac")
	}
	if _, err := exec.LookPath("xcodebuild"); err != nil {
		t.Skip("no Xcode")
	}
	dir, err := filepath.Abs(filepath.Join("testdata", "nativeapp"))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	app := &goApp{dir: dir, name: "nativeapp", cfg: cfg}
	tmp := t.TempDir()
	bundle := filepath.Join(tmp, "App.app")
	os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o755)
	work := filepath.Join(tmp, "work")
	run := macRunner(false)
	if err := macSwift(app, bundle, work, []string{runtime.GOARCH}, false, run); err != nil {
		t.Fatal(err)
	}
	fw := filepath.Join(bundle, "Contents", "Frameworks")
	lib := filepath.Join(fw, macSwiftLib)
	out, err := exec.Command("nm", "-gU", lib).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, sym := range []string{"_asuka_natives_boot", "_asuka_natives_make", "_asuka_native_new", "_asuka_native_call", "_asuka_native_start"} {
		if !containsField(string(out), sym) {
			t.Errorf("the library doesn't export %s", sym)
		}
	}
	if !strings.Contains(string(out), "AsukaModule_github_com_egoist_asuka_internal_nativegen_testdata_kitchen") {
		t.Error("the library hasn't the kitchen's Swift")
	}
	if macImports(lib, "@rpath/libswift_Concurrency.dylib") {
		if _, err := os.Stat(filepath.Join(fw, "libswift_Concurrency.dylib")); err != nil {
			t.Error("the library links Swift concurrency, and its runtime isn't beside it")
		}
	}

	// Unchanged: not built again.
	st, _ := os.Stat(lib)
	time.Sleep(10 * time.Millisecond)
	if err := macSwift(app, bundle, work, []string{runtime.GOARCH}, false, run); err != nil {
		t.Fatal(err)
	}
	if st2, _ := os.Stat(lib); !st2.ModTime().Equal(st.ModTime()) {
		t.Error("built again with nothing changed")
	}

	// Signed with the app, its libraries first.
	os.WriteFile(filepath.Join(bundle, "Contents", "MacOS", "App"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist(map[string]any{
		"CFBundleExecutable": "App", "CFBundleIdentifier": "dev.asuka.test", "CFBundlePackageType": "APPL"})), 0o644)
	if err := signMac(bundle, "-", "", run); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("codesign", "--verify", "--strict", lib).Run(); err != nil {
		t.Errorf("the library isn't signed: %v", err)
	}

	// An app without native packages: no library.
	plain := sqliteTestApp(t)
	if err := macSwift(plain, bundle, work, []string{runtime.GOARCH}, false, run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lib); err == nil {
		t.Error("the library stayed for an app without native packages")
	}
}

func containsField(s, f string) bool {
	for _, line := range strings.Split(s, "\n") {
		if fs := strings.Fields(line); len(fs) > 0 && fs[len(fs)-1] == f {
			return true
		}
	}
	return false
}
