# AGENTS.md

nui is a UI framework in Go that renders with each platform's own
widgets, driven by fine-grained signals. **Mobile first: iOS and
Android.** Desktop (macOS, Windows, Linux) comes later on the same core;
don't build desktop hosts until the milestones reach them. **Read
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
  and Android).
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
swift test --package-path hosts/ios # the Swift decoder against the golden files (macOS)
(cd hosts/android && ./gradlew :nui:testDebugUnitTest)  # the Kotlin decoder, likewise (ANDROID_HOME set)
go generate ./ops                   # Go constants, Swift and Kotlin decoders
go run ./cmd/nui dev -ios examples/counter       # iOS simulator (Xcode): the Go code runs here, in the player
go run ./cmd/nui dev -android examples/counter   # Android emulator (SDK + NDK), likewise
go run ./cmd/nui dev -ios -once examples/counter # build the app with its Go code, launch once (-embed: and watch)
go test ./internal/remote                        # the dev transport, against the fake host
go run ./cmd/nui build -ios -simulator examples/counter    # a release build (-ios alone: a signed .ipa)
go run ./cmd/nui build -android examples/counter           # .aab and .apk (NUI_KEYSTORE, NUI_KEY_ALIAS, NUI_KEYSTORE_PASSWORD)
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
  Material Components, no Compose.
- Keep generated files (`ops` decoders) committed and up to date.
