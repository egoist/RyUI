package main

import (
	"bytes"
	"crypto/sha256"
	"debug/macho"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/egoist/asuka/platform/macos"
)

// The Mac's native code in Swift: the app's native packages' (their
// Package.swift), built with Asuka's Swift for it (platform/macos, the
// module Asuka they import) as Swift packages (native_swift.go), linked
// into one library in the bundle, Contents/Frameworks/AsukaNatives.dylib,
// which the host (host/darwin) loads as it starts. The app's Go still
// builds with no cgo: the host calls the library through purego.

// macSwiftLib is the library's name, as the host looks for it.
const macSwiftLib = "AsukaNatives.dylib"

// macSwiftArch is swiftc's name for a Go architecture.
func macSwiftArch(goarch string) string {
	if goarch == "amd64" {
		return "x86_64"
	}
	return goarch
}

// macSwiftLinkArgs is swiftc's command line linking the library for one
// architecture from a build of the app's Swift (its packages' objects).
func macSwiftLinkArgs(arch string, o *swiftPackageOutput, out string) []string {
	args := []string{"swiftc", "-target", arch + "-apple-macos" + macMin, "-emit-library",
		// The Swift runtime: the system's, else (macOS 11, for Swift
		// concurrency) the copy beside the library.
		"-Xlinker", "-rpath", "-Xlinker", "/usr/lib/swift",
		"-Xlinker", "-rpath", "-Xlinker", "@loader_path",
		"-Xlinker", "-install_name", "-Xlinker", "@rpath/" + macSwiftLib,
	}
	args = append(append(args, o.objects...), o.linkFlags()...)
	return append(args, "-o", out)
}

// macEmbedPackages puts the build's frameworks beside the library (fw)
// and its resource bundles in res, taking away those an earlier build put
// there (listed in dir) that this one hasn't.
func macEmbedPackages(o *swiftPackageOutput, dir, fw, res string) error {
	list := filepath.Join(dir, "embedded")
	if b, err := os.ReadFile(list); err == nil {
		for _, p := range strings.Split(strings.TrimSpace(string(b)), "\n") {
			if p != "" {
				os.RemoveAll(p)
			}
		}
	}
	if o == nil {
		os.Remove(list)
		return nil
	}
	if err := os.MkdirAll(res, 0o755); err != nil {
		return err
	}
	if err := o.embed(fw, res); err != nil {
		return err
	}
	var put []string
	for _, f := range o.frameworks {
		put = append(put, filepath.Join(fw, filepath.Base(f)))
	}
	for _, r := range o.bundles {
		put = append(put, filepath.Join(res, filepath.Base(r)))
	}
	return os.WriteFile(list, []byte(strings.Join(put, "\n")), 0o644)
}

// macSwift builds the app's native packages' Swift into the bundle's
// Frameworks, for archs (Go's names), or takes away a library left from
// before when it has none. In asuka dev, a build whose packages haven't
// changed keeps the library it has.
func macSwift(app *goApp, bundle, work string, archs []string, release bool, run func(dir string, env []string, cmd string, args ...string) error) error {
	fw := filepath.Join(bundle, "Contents", "Frameworks")
	lib := filepath.Join(fw, macSwiftLib)
	dir := filepath.Join(work, "swift")
	natives, err := nativeFor(app, "macos", "arm64")
	if err != nil {
		return err
	}
	// What the app's packages build in (featurePermissions' macSwift:
	// asuka/sdk/purchase's StoreKit), with or without natives.
	var defines []string
	if app.cfg != nil {
		defines = app.cfg.macSwift
	}
	if len(natives) == 0 && len(defines) == 0 {
		os.Remove(lib)
		os.Remove(filepath.Join(fw, "libswift_Concurrency.dylib"))
		return macEmbedPackages(nil, dir, fw, "")
	}
	if _, err := exec.LookPath("xcodebuild"); err != nil {
		if len(natives) == 0 {
			return fmt.Errorf("asuka: %s's packages need Swift on the Mac (%s): install Xcode", app.name, strings.Join(defines, ", "))
		}
		return fmt.Errorf("asuka: %s's native packages have Swift (%s): install Xcode", app.name, natives[0].path)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	host, err := swiftSources(macos.Files, "Sources/AsukaMac")
	if err != nil {
		return err
	}
	stamp := filepath.Join(dir, "stamp")
	h := sha256.New()
	fmt.Fprintln(h, archs, release, bundle, defines)
	for name, b := range host {
		fmt.Fprintln(h, name, len(b))
		h.Write(b)
	}
	for _, p := range natives {
		fmt.Fprintln(h, p.dir, dirDigest(p.dir)) // a local package changes as it's worked on
	}
	key := hex.EncodeToString(h.Sum(nil))
	if old, err := os.ReadFile(stamp); err == nil && string(old) == key {
		if _, err := os.Stat(lib); err == nil {
			return nil
		}
	}
	if err := os.MkdirAll(fw, 0o755); err != nil {
		return err
	}
	var parts []string
	var built *swiftPackageOutput
	for _, a := range archs {
		out := filepath.Join(dir, "AsukaNatives-"+a+".dylib")
		b := &swiftBuild{dir: filepath.Join(dir, "packages"), platform: "macos", sdkName: "macosx",
			arch: macSwiftArch(a), release: release, defines: defines, host: host, natives: natives, run: run}
		o, err := b.build()
		if err != nil {
			return err
		}
		if built == nil {
			built = o // its frameworks and bundles: the first architecture's (a Mac framework is universal)
		}
		if err := run(app.dir, nil, "xcrun", macSwiftLinkArgs(macSwiftArch(a), o, out)...); err != nil {
			return err
		}
		parts = append(parts, out)
	}
	if err := macEmbedPackages(built, dir, fw, filepath.Join(bundle, "Contents", "Resources")); err != nil {
		return err
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
		return fmt.Errorf("asuka: finding swiftc: %v", err)
	}
	src := filepath.Join(filepath.Dir(strings.TrimSpace(string(out))), "..", "lib", "swift-5.5", "macosx", name)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("asuka: the app's Swift uses Swift concurrency, which macOS 11 doesn't have, and this toolchain has no copy to ship (%s)", src)
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
