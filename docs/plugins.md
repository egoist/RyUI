# Plugins

A plugin is a Go package that brings native libraries with it: code in
C, Zig or Rust, prebuilt for each platform, that its Go code calls
without cgo. Using one is importing it; the CLI ships its libraries with
the app, on every platform. `asuka/sdk/sqlite` is one
([Data](data.md#records-the-sqlite-plugin)).

## Using a plugin

Import it:

```go
import "github.com/egoist/asuka/sdk/sqlite"

db, err := sqlite.Open("notes.db")
```

Nothing else. `asuka build` (and `asuka dev` on a phone) finds the
plugins among the app's dependencies and ships each one's libraries for
the build's platform. `go run`, `go test` and `asuka dev` on a desktop
download them the first time, into the user's cache, and use them from
there after. Every file is checked against the SHA-256 the plugin's
manifest names before it's used.

### Where the libraries go

| Platform | Where | |
|---|---|---|
| macOS | `Contents/Frameworks` | signed with the app |
| Windows | beside the `.exe` | signed with it, with `ASUKA_WINDOWS_CERT` |
| Linux | `lib/<app>` of the install prefix | in the tarball and the Flatpak |
| iOS | `Frameworks/<library>.framework` | its Info.plist and privacy manifest, signed with the app |
| Android | beside `libasuka.so`, in each ABI | uncompressed, 16 KB pages |
| The web | `lib/` of the host's files | the page's `libs` option names each file's URL |

The libraries of an app's plugins all go in one place, so no two may
have a file of the same name for a platform: the build says which two do.

An app built into another (`asuka build --platform ios --xcframework`)
comes with an XCFramework of each library, for that app to embed too.

## Writing a plugin

A plugin is a package with a manifest beside it, `asuka-plugin.json`:

```json
{
  "name": "sqlite",
  "sources": ["csrc", "internal/build"],
  "libraries": [
    {
      "name": "asqlite",
      "version": "3.54.1",
      "release": "https://github.com/egoist/asuka/releases/download/plugin-sqlite",
      "files": {
        "darwin": {"name": "libasqlite.dylib"},
        "ios": {"name": "asqlite"},
        "ios-simulator": {"name": "asqlite"},
        "android-arm64": {"name": "libasqlite.so"},
        "linux-amd64": {"name": "libasqlite.so"},
        "windows-amd64": {"name": "asqlite.dll"},
        "wasm": {"name": "asqlite.wasm"}
      }
    }
  ]
}
```

- `name` is the plugin's, as the CLI's messages name it.
- `sources` are what its libraries are built from, relative to the
  manifest: a library whose sources changed since it was built isn't
  published. Without it, every file of the package but its tests.
- Each library's `files` name its file for each platform: `darwin` (one
  file for both Macs: arm64 and x86_64), `ios` and `ios-simulator`,
  `android-arm64`, `android-arm`, `android-amd64`, `linux-amd64`,
  `linux-arm64`, `windows-amd64`, `windows-arm64` and `wasm`. A Mac's is
  a `.dylib`; an iOS one is named for the library (its framework's
  binary, with `@rpath/<library>.framework/<library>` as its install
  name). `release` is the GitHub release they're published to.
  `version` is the library's, for its iOS framework.

The plugin's Go code embeds the manifest and loads the library from the
path `plugin.LibraryPath` gives, from a file in the manifest's
directory:

```go
//go:embed asuka-plugin.json
var manifest []byte

func load() (uintptr, error) {
	path, err := plugin.LibraryPath(manifest, "asqlite")
	if err != nil {
		return 0, err
	}
	return purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL) // syscall.LoadLibrary on Windows
}
```

Keep a library's exported symbols few, its own and nothing it links:
another copy of what it's built from may be in the process (a system
SQLite that GTK loads). Call it a whole operation at a time, not a
function per value: each call from Go to C costs more than a small
one's work (the sqlite plugin calls once per statement).

### Building and publishing

A plugin builds its libraries with a tool of its own, as they need:
`sdk/sqlite/internal/build` compiles SQLite with Xcode, the Android
NDK and Zig. It writes each target's file with `plugin.StoreBuild`,
with the plugin's `plugin.SourceHash`, into `build/<target>` beside the
manifest. That directory isn't checked in. In a checkout, the plugin's
own tests and the apps beside it use the build there.

```sh
go generate ./sdk/sqlite                         # build every target
go run ./cmd/asuka plugin publish sdk/sqlite     # publish
```

`asuka plugin publish` gzips each file and names it for its hash
(`asqlite-darwin-1982f596dd7c-libasqlite.dylib.gz`). It uploads the ones
the release doesn't have yet, with `gh`, making the release if it isn't
there. It downloads each one back to check it, and records them in the
manifest, which is what you commit. A file a release has is never
replaced, so every manifest a commit had keeps working. A build older
than the plugin's sources stops it: build again first.

### Native modules

Plugins will bring native modules too, as an app's own (Swift on iOS,
Kotlin on Android, JavaScript on the web, [Native code](native.md)):
the manifest's `modules`, beside its `libraries`.
