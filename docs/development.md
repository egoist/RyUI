# Development

`ryui dev` runs an app on the iOS simulator or an Android emulator or
phone, and reloads it as you save. This page covers how it does that,
what a reload keeps, devices, assets and logs, what it can't reload, the
problems you may meet, and RyUI's own GUI tests and benchmarks.

## ryui dev

```sh
ryui dev -ios [flags] [dir]
ryui dev -android [flags] [dir]
```

`dir` is the app's directory, the current one by default: a `main`
package that calls `app.Run`. The flags:

- `-ios`, `-android`: the platform. One per run; run two terminals for
  both.
- `-device`: on iOS, a simulator's name or UDID (by default the booted
  iPhone, else a recent iPhone Pro, which it boots); on Android, an adb
  serial or an AVD's name (by default the first device adb lists, else
  the first AVD, which it starts).
- `-embed`: build the app with its Go code in it, and watch.
- `-once`: build the app with its Go code, install it and launch it,
  then exit: no watching, no logs.
- `-v`: print the commands RyUI runs.

## The player and built apps

By default, `ryui dev` runs your Go code on your Mac, and its views in
the **player**: an app named RyUI (`dev.ryui.player`) that is the
platform's host with no app in it. The player is built and installed
once, shared by every app, and installed again only when it changes (a
new version of RyUI). The Go code and the player talk over a socket.

On a save, RyUI rebuilds the Go code for the Mac and restarts it, in
about a second; the player reconnects and shows the new code. Nothing is
installed.

With `-embed` or `-once`, or when the app has native code for the
platform (an `ios/` or `android/` directory, see [Native
code](native.md)), RyUI builds the app itself instead, its Go code inside
as a release build has it, under the app's own name and ID:

- **iOS**: the host, with the app's Swift, is compiled once and cached;
  each save rebuilds the Go code, links the app, signs it for the
  simulator and reinstalls it with `xcrun simctl`.
- **Android**: Gradle builds the app, with its Kotlin, once for each
  version of the host, of the Kotlin and of the manifest; each save
  rebuilds the Go library, pushes it into the app's private files with
  `adb`, where the debug app loads it from, and restarts the app.

The player has every permission any RyUI package needs, and its own
purpose strings. What `ryui.json` gives the app itself (its name, its
ID, a URL `scheme`, `links`, `ios.usage`) only a built app has: try
those with `-embed`. The `accent` color applies in the player too.

## What a reload keeps

In the player, a reload keeps the values of the app's signals, with no
code of yours. The Go process saves them as it stops, to the file the
`RYUI_STATE` environment variable names (`ryui dev` sets it to
`state.json` in the app's cache directory, under
`~/Library/Caches/ryui`), and the next run gives each value back to the
signal made at the same place: the same call path (function names, not
lines), the same type, and the same position among the signals of that
type made there.

What isn't kept:

- Navigation: a reload starts at the root screen.
- Signals RyUI makes itself, and values JSON doesn't carry (funcs,
  unexported fields).
- Signals whose type changed, and those of a type after one of the same
  type you added at the same place.

A built app (`-embed`, `-once`, native code) starts again from nothing
on each reload.

Files your Go code writes in the player (`ryui/store`, `ryui/sqlite`) are
on the Mac: `store.Dir()` is a `data` directory of the app's own, in
`~/Library/Caches/ryui`, one for each platform, as two devices would have
theirs. Delete it to start the app again with no data.

## Reloading

RyUI watches, by polling, the app's directory and its module's:

- Go files and `go.mod` (not `_test.go`);
- Swift and Kotlin files;
- files under `assets/`.

It skips directories starting with `.`, and `testdata`, `build` and
`node_modules`. A build that fails prints its errors and keeps the
running app; the next save tries again.

`ryui.json` isn't watched: `ryui dev` reads it as it starts. Stop it and
run it again after changing `ryui.json`; it rebuilds and reinstalls what
changed.

Native code reloads only in a built app, where RyUI rebuilds and
reinstalls it on each save. Gradle runs again when Kotlin changes, which
takes longer than a Go change.

## Devices

**The iOS simulator** shares the Mac's network, so the player reaches
`ryui dev` directly.

**An Android emulator** reaches the Mac at `10.0.2.2`, and the player
connects there.

**An Android phone over USB**: turn on USB debugging in its developer
options, plug it in and accept the prompt, and check that `adb devices`
lists it. `ryui dev -android` takes the first device adb lists, phone or
emulator; with several, name it with `-device` and its serial. The phone reaches the Mac through
`adb reverse`, which RyUI sets up for ports 7461 (the app) and 7462 (its
assets). In development RyUI builds for 64-bit phones and emulators
(arm64-v8a, x86_64).

**An iPhone**: `ryui dev` runs on the simulator only. To run on an
iPhone, make a build signed for it, `ryui build -ios -distribution
development` (see [Shipping](shipping.md#ios)), and install the `.ipa`
with Xcode's Devices window.

## Assets

In the player, the app's `assets/` directory stays on the Mac: the Go
process serves it over HTTP, and the player loads `ui.Image`'s asset
paths, and the fonts in `assets/fonts`, from there. A change to an asset
reloads the app.

A built app has its assets inside it: on iOS, copied into the app on
each build; on Android, pushed into the app's private files when they
changed.

## Logs

- **In the player**, the terminal shows the Go process's output:
  `fmt.Println`, `log`, panics.
- **A built app on iOS**: the app's output, Go's and Swift's `print`,
  through the simulator's console.
- **A built app on Android**: the app's logcat. Android drops a
  process's standard output, so the Go code's goes to logcat, tagged
  `ryui`.
- With `-once`, nothing: read them with Console.app or `adb logcat`.

## Backgrounding the app

Leaving the app in the player (to take a photo with the camera app, to
sign in in the browser) doesn't end the session. While the player is in
the background, the Go code waits up to ten minutes for an answer from
it, rather than the usual ten seconds, since the system may freeze it.
When Android cuts a backgrounded app's network, the session is kept for
three minutes; a player that comes back in time takes it up again, with
its screens as they were. A player that comes back later starts the
app again, at its root screen.

Stopping `ryui dev` (Ctrl-C) stops the player too, so it doesn't join
the next session, which may be another app's.

## Common problems

- **`no iOS simulator SDK (install Xcode)`**: install Xcode, open it
  once, and add an iOS simulator runtime in its Components settings.
  `no iPhone simulator; add one in Xcode` and `no simulator "..."`
  mean there's no simulator of that name: `xcrun simctl list devices`
  lists them.
- **`no Android SDK: install Android Studio or set ANDROID_HOME`**: RyUI
  looks in `ANDROID_HOME`, `ANDROID_SDK_ROOT`, then `~/Library/Android/sdk`.
- **`no Android NDK`**: install one from Android Studio's SDK Manager
  (SDK Tools, NDK).
- **`no Android device or emulator`**: make an AVD in Android Studio's
  Device Manager, or plug in a phone. RyUI waits up to four minutes for
  an emulator it starts to boot (`the emulator didn't boot`).
- **Java**: Gradle needs a JDK: `JAVA_HOME`, else Android Studio's own.
- **The first Android build is slow**: Gradle downloads itself and
  Android's libraries, once.
- **A release build on the phone**: an app installed by `ryui build`
  (another signing key) is uninstalled, with its data, to install the
  debug one.
- **The CLI and the app's RyUI**: the CLI carries the hosts' Swift and
  Kotlin, and the app's `go.mod` picks RyUI's Go code. Install the CLI at
  the version the app requires.
- **Starting over**: RyUI's builds and caches are in
  `~/Library/Caches/ryui`; delete it to build everything again, and the
  kept state with it.

## GUI tests

RyUI's own repository tests the examples on a simulator and an emulator,
through the platform's views, in `e2e/`:

```sh
go test -tags e2e ./e2e -run IOS       # the booted simulator, else a recent iPhone
go test -tags e2e ./e2e -run Android   # the device ryui dev picks
```

Each test builds an example with `ryui dev -once` (a debug build, with
its Go code), then taps, types and reads the screen through the debug
hosts' automation: a notification on iOS, a broadcast on Android. The
counter, notes and navigation examples have tests, the same on both
platforms. They wait for the screen to show what they expect, up to a
minute, never for a set time.

The iOS tests need Xcode (`xcrun`); the Android ones need `adb` on your
`PATH`, and the SDK and NDK that `ryui dev -android` needs. Without
them, the tests are skipped.

## Benchmarks

`bench/` compares RyUI with SwiftUI and Jetpack Compose: the same app
written in each, timed frame by frame in release builds.

```sh
go run ./bench/run -ios        # ryui and SwiftUI on the simulator
go run ./bench/run -android    # ryui and Compose on the running emulator or device
```

`-runs` sets how many runs of each (5 by default); the medians go to
`bench/results`. [`bench/README.md`](../bench/README.md) has the
scenarios, how they're measured, and the latest results.

## ryui.json

`ryui dev` and `ryui build` read the same `ryui.json`; see
[Shipping](shipping.md#ryuijson) for its fields.
