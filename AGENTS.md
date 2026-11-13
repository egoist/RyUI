# AGENTS.md

Asuka is a UI framework in Go that renders with each platform's own
widgets, driven by fine-grained signals. **Mobile first: iOS and
Android.** Desktop and the web run on the same core: the macOS host
(`host/darwin`, AppKit through purego), the Windows host
(`host/windows`, Win32 through syscall), the Linux host (`host/linux`,
GTK 4 and libadwaita through purego, `internal/gtk`) and the web
(`host/web`, the app as WebAssembly, and `platform/web`, the DOM from
JavaScript). **Read
[DESIGN.md](DESIGN.md) before changing code**; it is the source of truth
for the architecture, the APIs and the milestones. Work milestone by
milestone, in order.

## Hard rules

- **No cgo outside `mobile/`.** Everything else builds with
  `CGO_ENABLED=0` (so desktop hosts can later call native code through
  purego and `syscall`). `mobile/` uses cgo only because Go's
  `c-archive`/`c-shared` build modes need it.
- **The core never imports a host.** `signal`, `ui`, `layout` and `ops`
  talk to hosts only through `host.Host` and `ops`. Adding a component
  means: its kind and props in `ops/schema.go`, `go generate ./ops`, the
  component in `ui`, and its view in every host (`fake` first, then iOS
  and Android, then macOS in `host/darwin`, Windows in `host/windows`,
  Linux in `host/linux` and the web in `platform/web/src`).
- **Nothing phone-only in the core's data structures**: no single global
  root, no assumption that the screen size never changes, so desktop
  windows can be added later without rewriting the core.
- **Main thread.** The core, every host call and every signal write run
  on the platform's main thread. Goroutines reach it with `ui.Post`.
  Never block the main thread, and never make a host wait on a
  goroutine.
- **One flush, two crossings.** Effects emit ops into a batch; a flush
  applies the props batch, lays out, applies the frames batch. Never
  cross into Swift or Kotlin per property.
- **Hot paths** (signal propagation, flush, layout, op encoding and
  decoding): no reflection, no per-op or per-write allocation in the
  steady state.
- **Owners dispose explicitly.** Every effect has an owner; disposing it
  stops the effect and runs its cleanups. Test it whenever you add a
  construct that makes owners (`Show`, `For`, `List` rows, screens).

## Commands

```sh
go build ./... && go vet ./...      # CGO_ENABLED=0: everything but mobile/
go test ./...                       # signal, layout, ui with host/fake, ops, CLI
go test ./layout -run CSS -update-known  # after fixing layout: refresh known_failures.txt
go test ./ops -update               # after changing the wire format: refresh golden files
go test ./host/darwin -run Conformance   # what the macOS host draws for a Style, measured (package conformance)
go test -tags e2e ./e2e -run Conformance # likewise on the simulator, the emulator and headless Chrome (ASUKA_CONFORMANCE_OUT=dir: the captures)
swift test --package-path platform/ios # the Swift decoder against the golden files (macOS)
node --test platform/web                # the JavaScript decoder, likewise
(cd platform/android && ./gradlew :asuka:testDebugUnitTest)  # the Kotlin decoder, likewise (ANDROID_HOME set)
go generate ./ops                   # Go constants, Swift, Kotlin and JavaScript decoders
go run platform/web/gen_symbols.go  # the web host's symbols, from the Android host's
go generate ./i18n                  # CLDR's tables, from the pinned release
go run ./cmd/asuka generate examples/i18n  # an app's catalog functions (dev and build do it too)
go run ./cmd/asuka vet ./...                # the analyzers (package vet): a signal read once, written off the main thread
go run ./cmd/asuka dev -platform ios -lang ar examples/i18n  # a language for one run; -lang ar-XB, en-XA: pseudo locales; -dir rtl: any app right to left
go run ./cmd/asuka dev -platform ios examples/counter       # iOS simulator (Xcode): the Go code runs here, in the player
go run ./cmd/asuka dev -platform android examples/counter   # Android emulator (SDK + NDK), likewise
go run ./cmd/asuka dev -platform ios -once examples/counter # build the app with its Go code, launch once (-embed: and watch)
go test ./internal/remote                        # the dev transport, against the fake host
go test -tags e2e ./e2e -run IOS                 # GUI tests: the examples on the simulator (Android: -run Android, an emulator running)
go run ./cmd/asuka build -platform ios -simulator examples/counter    # a release build (-platform ios alone: a signed .ipa)
go run ./cmd/asuka build -platform android examples/counter           # an .aab (ASUKA_KEYSTORE, ASUKA_KEY_ALIAS, ASUKA_KEYSTORE_PASSWORD)
go run ./cmd/asuka publish -platform ios -groups Friends examples/counter  # build and upload to TestFlight (ASUKA_ASC_KEY_ID, ASUKA_ASC_ISSUER_ID, ASUKA_ASC_KEY)
go run ./cmd/asuka publish -platform android -track internal examples/counter  # build and upload to Google Play (ASUKA_PLAY_SERVICE_ACCOUNT)
go run ./cmd/asuka dev examples/counter            # macOS (on a Mac, no -platform: mac): the app and its AppKit host in one process, rerun on save
go run ./cmd/asuka build examples/counter          # likewise: a universal .app (-sign "Developer ID Application: ..."; default ad hoc)
go test ./host/darwin                            # the macOS host, driving real AppKit views (macOS)
go test ./host/darwin -run Swift               # an app's macos/*.swift, compiled and loaded by the host (Xcode's command line tools)
(cd examples/counter && ASUKA_SNAPSHOT=/tmp/c.png ASUKA_DUMP=1 go run .)  # macOS: the window as a PNG and its view tree, then quit
go run ./cmd/asuka dev examples/counter            # Windows (on a PC, no -platform: windows): the app and its Win32 host in one process, rerun on save
go run ./cmd/asuka build -platform windows examples/counter  # a folder: the .exe (icon, manifest, version) and its assets; from any system
go test ./host/windows                           # the Windows host, driving real Win32 windows (Windows)
xvfb-run -a dbus-run-session go test ./host/linux  # the Linux host, driving real GTK windows (GTK 4.14, libadwaita 1.5)
go run ./cmd/asuka dev examples/counter            # Linux (on Linux, no -platform: linux): the app and its GTK host in one process, rerun on save
go run ./cmd/asuka build -platform linux examples/counter    # an install prefix, a .tar.gz and a Flatpak manifest; from any system
go run host/linux/gen_symbols.go                 # the Linux host's fallback symbols, from the web host's
go test -tags e2e ./e2e -run Linux               # GUI tests: the examples under Xvfb (Linux)
(cd examples/counter && ASUKA_SNAPSHOT=c.png ASUKA_DUMP=1 ASUKA_SCRIPT="tap:Add" go run .)  # Windows: steps, then the window as a PNG and its view tree
go run ./cmd/asuka dev -platform web examples/counter       # the web: served to the browser, reloaded on save (-lan: to a phone)
go run ./cmd/asuka build -platform web examples/counter     # a static site, in examples/counter/dist/web
GOOS=js GOARCH=wasm go vet ./host/web ./app       # the web host's Go side
go test -tags e2e ./e2e -run Web                  # GUI tests: the examples in headless Chrome
```

## Conventions

- Tests assert op streams from `host/fake` and count work (ops,
  measurements, allocations). They never pass or fail on wall-clock
  time: wait for the condition, count the work.
- Prefer the platform's behavior and defaults; an app that sets no style
  must look native (UIKit on iOS, Material 3 on Android). When unsure
  how a platform behaves, read its docs or an established framework's
  implementation (React Native, react-navigation, Solid, Taffy, the CSS specs) before
  inventing one.
- Swift host: UIKit only, no SwiftUI. Kotlin host: Android Views and
  Material Components, no Compose. macOS host: AppKit only, from Go
  through `internal/darwin` (purego), no cgo, no SwiftUI; an app's own
  Swift (`macos/*.swift`) is compiled with `platform/macos` into a
  library the host loads through purego (`host/darwin/swift.go`), never
  linked. Windows host:
  Win32 and the common controls, from Go through `internal/windows`
  (syscall), no cgo, no WinUI; the controls drawn as Fluent's in custom
  draw, so an app that sets no style looks like Windows 11. Linux host:
  GTK 4 and libadwaita widgets from Go through `internal/gtk` (purego),
  no cgo, no GtkBuilder or Blueprint; an app that sets no style looks
  like a GNOME app. Web host:
  the DOM from plain JavaScript modules in `platform/web/src` (no build
  step, no framework), the browser's own elements for controls and
  dialogs; its Go side (`host/web`) only carries batches, measures and
  events.
- Every prop that shows text is an accessor, so it can follow the app's
  language (`locales.Live.Title()`); only what's shown once (an alert) takes a
  string. Frames are always physical: a host never mirrors what the core places.
- Keep generated files (`ops` decoders, `locales/catalog_gen.go`) committed and up to date.
