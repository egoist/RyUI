package nativegen

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
)

// A package's native code lives beside its Go, a project of the
// platform's own for each:
//
//	Package.swift  a Swift package, for iOS and macOS (the platforms it
//	               names): its Swift in swift/
//	android/       a Gradle module (build.gradle.kts), its Kotlin in
//	               src/main/kotlin, in the package's Kotlin package
//	web/           JavaScript modules: <package>.js
//
// Windows and Linux (and wherever the package says so) implement it in
// Go. Generate writes what connects each to the package's Go. The Swift
// package is at the package's root because SwiftPM knows a package by
// its folder's name: the Go package's, not one every package shares.

// Platforms are the projects a package can have native code in.
var Platforms = []string{"swift", "android", "web"}

// SwiftDir is the folder of a package's Swift, which its Package.swift's
// target is (path: "swift").
const SwiftDir = "swift"

// HasPlatform reports whether the package in dir has native code in
// platform's project (swift: its Package.swift).
func HasPlatform(dir, platform string) bool {
	switch platform {
	case "swift":
		return exists(filepath.Join(dir, "Package.swift"))
	case "android":
		return exists(filepath.Join(dir, platform, "build.gradle.kts"))
	case "web":
		return exists(filepath.Join(dir, platform))
	}
	return false
}

// Files are what the package generates, by their paths relative to its
// directory: its Go, and each platform's it has native code for.
func Files(p *Package) (map[string][]byte, error) {
	goSrc, err := GenerateGo(p)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{GoFile(p.Name): goSrc}
	if HasPlatform(p.Dir, "swift") {
		files[filepath.Join(SwiftDir, SwiftFile(p))] = GenerateSwift(p)
	}
	if HasPlatform(p.Dir, "android") {
		files[filepath.Join("android", KotlinFile(p))] = GenerateKotlin(p)
	}
	if HasPlatform(p.Dir, "web") {
		files[filepath.Join("web", JSFile(p))] = GenerateJS(p)
	}
	return files, nil
}

// Stale are the package's generated files that aren't what the
// declarations say now (missing, or recording another hash), sorted.
func Stale(p *Package) ([]string, error) {
	files, err := Files(p)
	if err != nil {
		return nil, err
	}
	var stale []string
	for rel := range files {
		b, err := os.ReadFile(filepath.Join(p.Dir, rel))
		if err != nil || HashOf(b) != p.Hash {
			stale = append(stale, rel)
		}
	}
	sort.Strings(stale)
	return stale, nil
}

// Write writes the package's generated files where they changed, and
// returns those it wrote. A file that differs only in its line ends (a
// Windows checkout's, CRLF by git's core.autocrlf) hasn't changed.
func Write(p *Package) ([]string, error) {
	files, err := Files(p)
	if err != nil {
		return nil, err
	}
	var wrote []string
	for rel, b := range files {
		path := filepath.Join(p.Dir, rel)
		if old, err := os.ReadFile(path); err == nil && sameText(old, b) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return nil, err
		}
		wrote = append(wrote, rel)
	}
	sort.Strings(wrote)
	return wrote, nil
}

// sameText reports whether a and b are the same text, CRLF and LF line
// ends alike.
func sameText(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	crlf := []byte("\r\n")
	return bytes.Equal(bytes.ReplaceAll(a, crlf, []byte("\n")), bytes.ReplaceAll(b, crlf, []byte("\n")))
}
