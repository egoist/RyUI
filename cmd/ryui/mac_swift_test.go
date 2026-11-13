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

// An app's macos/*.swift is compiled with RyUI's Swift into the bundle's
// library, with Swift's concurrency runtime beside it for macOS 11, and
// signed with the app; unchanged, it isn't compiled again; taken away,
// the library goes too.
func TestMacSwift(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Swift for the Mac builds on a Mac")
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		t.Skip("no Xcode command line tools")
	}
	dir := t.TempDir()
	app := &goApp{dir: filepath.Join(dir, "app"), name: "app", cfg: &appConfig{}}
	os.MkdirAll(filepath.Join(app.dir, "macos", "sub"), 0o755)
	src := `import AppKit
@objc(RyUIAppNatives) final class AppNatives: NSObject, RyUINativeModule {
    @MainActor static func register() {
        RyUIRuntime.shared.registerNative("Box") { _ in Box() }
    }
}
@MainActor final class Box: RyUINativeView {
    let view: NSView = NSBox()
    func set(_ name: String, _ value: RyUINativeValue) {}
}
`
	os.WriteFile(filepath.Join(app.dir, "macos", "Natives.swift"), []byte(src), 0o644)
	// Only the files directly in macos/.
	os.WriteFile(filepath.Join(app.dir, "macos", "sub", "Broken.swift"), []byte("this isn't Swift"), 0o644)
	bundle := filepath.Join(dir, "App.app")
	os.MkdirAll(filepath.Join(bundle, "Contents", "MacOS"), 0o755)
	work := filepath.Join(dir, "work")
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
	for _, sym := range []string{"_ryui_natives_boot", "_ryui_natives_make", "_ryui_natives_call"} {
		if !containsField(string(out), sym) {
			t.Errorf("the library doesn't export %s", sym)
		}
	}
	if macImports(lib, "@rpath/libswift_Concurrency.dylib") {
		if _, err := os.Stat(filepath.Join(fw, "libswift_Concurrency.dylib")); err != nil {
			t.Error("the library links Swift concurrency, and its runtime isn't beside it")
		}
	}

	// Unchanged: not compiled again.
	st, _ := os.Stat(lib)
	time.Sleep(10 * time.Millisecond)
	if err := macSwift(app, bundle, work, []string{runtime.GOARCH}, false, run); err != nil {
		t.Fatal(err)
	}
	if st2, _ := os.Stat(lib); !st2.ModTime().Equal(st.ModTime()) {
		t.Error("compiled again with nothing changed")
	}

	// Signed with the app, its libraries first.
	os.WriteFile(filepath.Join(bundle, "Contents", "MacOS", "App"), []byte("#!/bin/sh\n"), 0o755)
	os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), []byte(plist(map[string]any{
		"CFBundleExecutable": "App", "CFBundleIdentifier": "dev.ryui.test", "CFBundlePackageType": "APPL"})), 0o644)
	if err := signMac(bundle, "-", "", run); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("codesign", "--verify", "--strict", lib).Run(); err != nil {
		t.Errorf("the library isn't signed: %v", err)
	}

	// No Swift any more: no library.
	os.Remove(filepath.Join(app.dir, "macos", "Natives.swift"))
	if err := macSwift(app, bundle, work, []string{runtime.GOARCH}, false, run); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(fw); err == nil {
		t.Error("the library stayed with the app's Swift gone")
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
