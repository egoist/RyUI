// Package plugin is what an Asuka plugin is made of: a Go package, with
// a manifest beside it (asuka-plugin.json) naming the native libraries it
// brings, prebuilt for each platform and published (a GitHub release, each
// file named for its hash), which its Go code loads (through purego or
// syscall: no cgo). The CLI ships the libraries of the plugins an app
// uses with it, for each platform; in go run, go test and asuka dev, a
// plugin's library is its checkout's build or the published one,
// downloaded once into the user's cache, verified.
//
// A plugin's Go code embeds its manifest and asks for its library's path:
//
//	//go:embed asuka-plugin.json
//	var manifest []byte
//
//	path, err := plugin.LibraryPath(manifest, "asqlite")
//
// Its build tool (its own: a plugin's libraries are built as it needs)
// writes each target's library with StoreBuild, into build/<target>
// beside the manifest; asuka plugin publish uploads them and records them
// in the manifest. sdk/sqlite is one.
//
// Native modules (Swift, Kotlin and JavaScript a plugin brings to the
// hosts) will be a manifest's "modules", beside "libraries".
package plugin

import (
	"encoding/json"
	"fmt"
	"runtime"
)

// ManifestFile is a plugin's manifest, in its package's directory.
const ManifestFile = "asuka-plugin.json"

// Manifest is a plugin's manifest.
type Manifest struct {
	// Name is the plugin's, as the CLI names it in its messages.
	Name string `json:"name"`
	// Sources are what its libraries are built from, paths relative to
	// the manifest (directories whole); SourceHash hashes them.
	Sources []string `json:"sources,omitempty"`
	// Libraries are its native libraries.
	Libraries []Library `json:"libraries"`
}

// Library is a native library a plugin brings.
type Library struct {
	// Name is the library's, as its code asks for it, and its framework's
	// on iOS: asqlite.
	Name string `json:"name"`
	// Version is its version, for its framework's Info.plist on iOS.
	Version string `json:"version"`
	// Release is where its files are published:
	// https://github.com/<owner>/<repo>/releases/download/<tag>.
	Release string `json:"release"`
	// Files are its files, by target (Targets).
	Files map[string]File `json:"files"`
}

// File is a library's file for a target.
type File struct {
	// Name is the file's name where it's installed: libasqlite.dylib,
	// asqlite.dll, libasqlite.so; on iOS the library's name.
	Name string `json:"name"`
	// The published file, gzipped: its name in the release, its size and
	// SHA-256. Empty until asuka plugin publish.
	Asset  string `json:"asset,omitempty"`
	Size   int64  `json:"size,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	// The library itself: its size and SHA-256.
	RawSize   int64  `json:"rawSize,omitempty"`
	RawSHA256 string `json:"rawSHA256,omitempty"`
	// Source is the SourceHash it was built from.
	Source string `json:"source,omitempty"`
}

// URL is where f is published.
func (l Library) URL(f File) string { return l.Release + "/" + f.Asset }

// Targets are the platforms a library is built for: darwin (one
// universal file, arm64 and x86_64), ios and ios-simulator (a
// framework's binary), android-<goarch>, linux-<goarch>,
// windows-<goarch>, and wasm (the web's, which a host loads).
var Targets = []string{
	"darwin", "ios", "ios-simulator",
	"android-arm64", "android-arm", "android-amd64",
	"linux-amd64", "linux-arm64",
	"windows-amd64", "windows-arm64",
	"wasm",
}

// Target is goos and goarch's target (ios is the device's: the CLI knows
// which SDK it builds for, and picks ios-simulator).
func Target(goos, goarch string) string {
	switch goos {
	case "darwin":
		return "darwin"
	case "ios":
		return "ios"
	case "js":
		return "wasm"
	}
	return goos + "-" + goarch
}

// Parse reads a manifest.
func Parse(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("plugin: %s: %w", ManifestFile, err)
	}
	if m.Name == "" {
		return nil, fmt.Errorf("plugin: %s: no name", ManifestFile)
	}
	for _, l := range m.Libraries {
		if l.Name == "" {
			return nil, fmt.Errorf("plugin %s: a library with no name", m.Name)
		}
		for t, f := range l.Files {
			if f.Name == "" {
				return nil, fmt.Errorf("plugin %s: library %s: %s's file has no name", m.Name, l.Name, t)
			}
		}
	}
	return &m, nil
}

// Library is the manifest's library named name.
func (m *Manifest) Library(name string) (Library, bool) {
	for _, l := range m.Libraries {
		if l.Name == name {
			return l, true
		}
	}
	return Library{}, false
}

// hostTarget is the target of the platform this program runs on.
func hostTarget() string { return Target(runtime.GOOS, runtime.GOARCH) }
