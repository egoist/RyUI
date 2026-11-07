package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// A release's crashes are symbolicated with its dSYMs: the debug
// information of each binary (the app's executable, its framework, its
// extensions'), by its UUID. The host and the extensions are compiled
// with it (-g), the Go code keeps it (no -s -w); dsymutil gathers it
// from the objects the linker linked, and the binaries are stripped of
// it, as Xcode's archive does. They're written beside the build,
// <Name>.dSYMs, for a crash service (Crashlytics' upload-symbols,
// sentry-cli debug-files upload), and in the .ipa's Symbols, for App
// Store Connect's crash reports.

// dsym makes bin's dSYM, work/dSYMs/<name>.dSYM, and strips bin of its
// debug information and local symbols.
func (t *iosToolchain) dsym(work, bin, name string) error {
	if !t.release {
		return nil
	}
	out := filepath.Join(work, "dSYMs", name+".dSYM")
	os.RemoveAll(out)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	if err := t.run(work, nil, "xcrun", "dsymutil", bin, "-o", out); err != nil {
		return err
	}
	return t.run(work, nil, "xcrun", "strip", "-S", "-x", bin)
}

// cacheSymbols makes a release extension's dSYM beside its cached
// executable (exe.dSYM), and strips the executable.
func (t *iosToolchain) cacheSymbols(exe string) error {
	if !t.release {
		return nil
	}
	os.RemoveAll(exe + ".tmp.dSYM") // swiftc's own, for its output's name
	dsym := exe + ".dSYM"
	os.RemoveAll(dsym)
	if err := t.run(filepath.Dir(exe), nil, "xcrun", "dsymutil", exe, "-o", dsym); err != nil {
		return err
	}
	return t.run(filepath.Dir(exe), nil, "xcrun", "strip", "-S", "-x", exe)
}

// cached reports whether an extension's cached executable can be used:
// a release's has its dSYM too.
func (t *iosToolchain) cached(exe string) bool {
	return exists(exe) && (!t.release || exists(exe+".dSYM"))
}

// keepSymbols copies an extension's dSYM (cacheSymbols') into work's.
func (t *iosToolchain) keepSymbols(work, exe, name string) error {
	if !t.release || !exists(exe+".dSYM") {
		return nil // the share sheet's stub, whose code is the framework's
	}
	dst := filepath.Join(work, "dSYMs", name+".dSYM")
	os.RemoveAll(dst)
	os.MkdirAll(filepath.Dir(dst), 0o755)
	return t.run(work, nil, "ditto", exe+".dSYM", dst)
}

// writeSymbols copies work's dSYMs to out/<name>.dSYMs, and returns it.
func (t *iosToolchain) writeSymbols(work, out, name string) (string, error) {
	src := filepath.Join(work, "dSYMs")
	if !exists(src) {
		return "", nil
	}
	dst := filepath.Join(out, name+".dSYMs")
	os.RemoveAll(dst)
	if err := t.run(work, nil, "ditto", src, dst); err != nil {
		return "", err
	}
	return dst, nil
}

// ipaSymbols writes the dSYMs' symbols (xcrun symbols) in the .ipa's
// staging directory, Symbols/, as Xcode's export does when it uploads
// them: App Store Connect symbolicates the crash reports it shows with
// them.
func (t *iosToolchain) ipaSymbols(work, stage string) error {
	ents, _ := os.ReadDir(filepath.Join(work, "dSYMs"))
	if len(ents) == 0 {
		return nil
	}
	dir := filepath.Join(stage, "Symbols")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, e := range ents {
		d := filepath.Join(work, "dSYMs", e.Name())
		if err := t.run(work, nil, "xcrun", "symbols", "-noTextInSOD", "-noDaemon", "-arch", "all", "-symbolsPackageDir", dir, d); err != nil {
			return fmt.Errorf("the symbols of %s: %v", e.Name(), err)
		}
	}
	return nil
}
