package main

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/egoist/ryui/platform/macos"
)

// An app's native code on the Mac in Swift: its macos/*.swift, compiled
// with RyUI's Swift for it (platform/macos) into one library in the
// bundle, Contents/Frameworks/RyUIAppNatives.dylib, which the host
// (host/darwin) loads as it starts. The app's Go still builds with no
// cgo: the host calls the library through purego.

// macSwiftLib is the library's name, as the host looks for it.
const macSwiftLib = "RyUIAppNatives.dylib"

// macSwiftArch is swiftc's name for a Go architecture.
func macSwiftArch(goarch string) string {
	if goarch == "amd64" {
		return "x86_64"
	}
	return goarch
}

// macSwiftSources are the app's macos/*.swift (only the files directly
// in it, as with ios/ and android/).
func macSwiftSources(appDir string) []string {
	srcs, _ := filepath.Glob(filepath.Join(appDir, "macos", "*.swift"))
	slices.Sort(srcs)
	return srcs
}

// macSwiftArgs is swiftc's command line for one architecture.
func macSwiftArgs(arch string, release bool, srcs []string, out string) []string {
	opt := "-Onone"
	if release {
		opt = "-O"
	}
	args := []string{"swiftc", "-swift-version", "5", "-module-name", "RyUIApp",
		"-target", arch + "-apple-macos" + macMin, "-emit-library", opt,
		// The Swift runtime: the system's, else (macOS 11, for Swift
		// concurrency) the copy beside the library.
		"-Xlinker", "-rpath", "-Xlinker", "/usr/lib/swift",
		"-Xlinker", "-rpath", "-Xlinker", "@loader_path",
		"-Xlinker", "-install_name", "-Xlinker", "@rpath/" + macSwiftLib,
	}
	if !release {
		args = append(args, "-g")
	}
	return append(append(args, srcs...), "-o", out)
}

// macSwift compiles the app's macos/*.swift into the bundle's
// Frameworks, for archs (Go's names), or takes away a library left from
// before when it has none. In ryui dev, a build whose sources haven't
// changed keeps the library it has.
func macSwift(app *goApp, bundle, work string, archs []string, release bool, run func(dir string, env []string, cmd string, args ...string) error) error {
	fw := filepath.Join(bundle, "Contents", "Frameworks")
	lib := filepath.Join(fw, macSwiftLib)
	srcs := macSwiftSources(app.dir)
	if len(srcs) == 0 {
		os.RemoveAll(fw)
		return nil
	}
	if _, err := exec.LookPath("xcrun"); err != nil {
		return fmt.Errorf("ryui: %s has Swift (macos/*.swift): install Xcode's command line tools (xcode-select --install)", app.name)
	}
	dir := filepath.Join(work, "swift")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	rt, err := fs.ReadFile(macos.Files, "Sources/RyUIMac/RyUIMacRuntime.swift")
	if err != nil {
		return err
	}
	runtimeSrc := filepath.Join(dir, "RyUIMacRuntime.swift")
	if err := os.WriteFile(runtimeSrc, rt, 0o644); err != nil {
		return err
	}
	all := append([]string{runtimeSrc}, srcs...)
	stamp := filepath.Join(dir, "stamp")
	key := fileHash(all...) + " " + strings.Join(archs, ",") + " " + strconv.FormatBool(release) + " " + bundle
	if old, err := os.ReadFile(stamp); err == nil && string(old) == key {
		if _, err := os.Stat(lib); err == nil {
			return nil
		}
	}
	if err := os.MkdirAll(fw, 0o755); err != nil {
		return err
	}
	var parts []string
	for _, a := range archs {
		out := filepath.Join(dir, "RyUIAppNatives-"+a+".dylib")
		if err := run(app.dir, nil, "xcrun", macSwiftArgs(macSwiftArch(a), release, all, out)...); err != nil {
			return err
		}
		parts = append(parts, out)
	}
	if len(parts) == 1 {
		if err := copyFile(parts[0], lib); err != nil {
			return err
		}
	} else if err := run(dir, nil, "lipo", append([]string{"-create", "-output", lib}, parts...)...); err != nil {
		return err
	}
	if err := macSwiftBackDeploy(lib, fw); err != nil {
		return err
	}
	return os.WriteFile(stamp, []byte(key), 0o644)
}

// macSwiftBackDeploy copies Swift's concurrency runtime beside the
// library when it links it: macOS 12 and later have it, macOS 11 (the
// apps' minimum) doesn't, so the library finds this copy there (its
// rpath), as Xcode's builds do.
func macSwiftBackDeploy(lib, fw string) error {
	const name = "libswift_Concurrency.dylib"
	dst := filepath.Join(fw, name)
	if !macImports(lib, "@rpath/"+name) {
		os.Remove(dst)
		return nil
	}
	out, err := exec.Command("xcrun", "--find", "swiftc").Output()
	if err != nil {
		return fmt.Errorf("ryui: finding swiftc: %v", err)
	}
	src := filepath.Join(filepath.Dir(strings.TrimSpace(string(out))), "..", "lib", "swift-5.5", "macosx", name)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("ryui: the app's Swift uses Swift concurrency, which macOS 11 doesn't have, and this toolchain has no copy to ship (%s)", src)
	}
	return copyFile(src, dst)
}

// macImports reports whether a Mach-O file (thin or universal) links a
// library.
func macImports(path, lib string) bool {
	var files []*macho.File
	if fat, err := macho.OpenFat(path); err == nil {
		defer fat.Close()
		for _, a := range fat.Arches {
			files = append(files, a.File)
		}
	} else if f, err := macho.Open(path); err == nil {
		defer f.Close()
		files = append(files, f)
	}
	for _, f := range files {
		for _, l := range f.Loads {
			if machoDylib(l.Raw(), f.ByteOrder) == lib {
				return true
			}
		}
	}
	return false
}

// machoDylib is the library a load command links (LC_LOAD_DYLIB,
// LC_LOAD_WEAK_DYLIB: debug/macho's ImportedLibraries leaves the weak
// ones out, and Swift links its concurrency runtime weakly), or "".
func machoDylib(raw []byte, bo binary.ByteOrder) string {
	const loadDylib, loadWeakDylib = 0xc, 0x80000018
	if len(raw) < 12 {
		return ""
	}
	if cmd := bo.Uint32(raw); cmd != loadDylib && cmd != loadWeakDylib {
		return ""
	}
	off := int(bo.Uint32(raw[8:]))
	if off >= len(raw) {
		return ""
	}
	name := raw[off:]
	if i := bytes.IndexByte(name, 0); i >= 0 {
		name = name[:i]
	}
	return string(name)
}

// copyFile copies a library (executable, as the linker makes them).
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	os.Remove(dst) // a signed file is replaced, not written over
	return os.WriteFile(dst, data, 0o755)
}
