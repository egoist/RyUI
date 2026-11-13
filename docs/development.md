# Development

`asuka dev` runs an app and reloads it as you save: on the iOS simulator,
an Android emulator or phone, your Mac, PC or Linux machine, or your
browser. This page covers how it does that on each, what a reload keeps,
devices, assets and logs, what it can't reload, `asuka vet`, the problems
you may meet, and Asuka's own tests and benchmarks.

## asuka dev

```sh
asuka dev [flags] [dir]                     # this computer: macOS, Windows or Linux
asuka dev --platform ios [flags] [dir]       # the iOS simulator
asuka dev --platform android [flags] [dir]   # an Android emulator or phone
asuka dev --platform web [flags] [dir]       # your browser
```

`--platform` is one of `ios`, `android`, `mac`, `windows`, `linux` and
`web`; `asuka build` takes it likewise. Without it, the app runs (or
builds) for the computer you're on. One platform per run: run two
terminals for two.

`dir` is the app's directory, the current one by default: a `main`
package that calls `app.Run`. The flags:

- `--platform`: as above.
- `--lang`: show a language for this run (`de`, `zh-Hans`), or a pseudo
  locale: `en-XA` (accented and longer) or `ar-XB` (right to left).
  `--dir rtl` lays any app out right to left. See
  [Languages](i18n.md).
- `--once`: build the app and run it once, without watching. On the
  desktop, `asuka dev` exits when the app does; on a phone, it builds
  the app with its Go code, installs it and launches it, then exits,
  without logs; in a browser, it serves the app.
- `-v`: print the commands Asuka runs.
- `--no-vet`: don't run [`asuka vet`](#asuka-vet) beside the builds.

On a phone:

- `--device`: on iOS, a simulator's name or UDID (by default the booted
  iPhone, else a recent iPhone Pro, which it boots); on Android, an adb
  serial or an AVD's name (by default the first device adb lists, else
  the first AVD, which it starts).
- `--no-hot`: no hot reload: build the app with its Go code in it, and
  rebuild and reinstall it on each save, starting it again from nothing.

In a browser: `--port`, `--lan` (serve to your network, for a phone's
browser) and `--no-open`; see [the
web](web.md#asuka-dev--platform-web).

## On the desktop

On macOS, Windows and Linux, the app's Go code and the platform's host
are one Go program, built for this computer with `CGO_ENABLED=0`. `asuka
dev` builds it and runs it; on each save it builds it again, stops the
running app, which saves its signals' values as it quits, and runs the
new one, which takes them back (see [What a reload
keeps](#what-a-reload-keeps)). A reload takes about as long as `go
build`. A build that fails prints its errors and keeps the running
app.

The app reads its `assets/` where they are, so changing one needs no
copy. Its data (`store.Dir()`, SQLite databases there) is in a `data`
directory of its own in Asuka's cache (see [Starting
over](#common-problems)), not the installed app's, so `asuka dev` and a
release don't share it. Its output (`fmt.Println`, `log`, panics) prints
in the terminal.

**macOS.** `asuka dev` on a Mac runs the app from an `.app` like a
release's, in Asuka's cache: its `Info.plist` (its name, bundle ID, URL
scheme, purpose strings and languages from `asuka.json`) and icon, signed
ad hoc. So the app is itself to the system, as when it's opened from the
Finder: the Dock shows its name and icon, notifications are its own, and
its privacy prompts (the camera, location, contacts) ask for it, with
its purpose strings, not for the terminal. Packages that need a
provisioning profile's entitlements (push, Sign in with Apple, App
Attest) work only in a release build signed with one: see
[Shipping](shipping.md#macos).

**Windows.** `asuka dev` runs only on Windows itself; `asuka build
--platform windows` builds from any system. The dev build is a console
program, so its output prints in the terminal; a release has no
console. To stop the app for a reload, Asuka closes its standard input
(Windows can't send a process Ctrl-C), and it saves its state as it
quits.

**Linux.** `asuka dev` on Linux runs the app on GTK 4 and libadwaita,
which it needs installed (see [Getting
started](getting-started.md#what-you-need)), under your session's
display, Wayland or X11. It gets the name, ID, version and accent of
`asuka.json`, as a release does; Asuka stops it with SIGINT, and it saves
its state as it quits. On a Mac or a PC, `asuka dev --platform linux`
builds the app for Linux but can't run it there, unless
`ASUKA_LINUX_RUN` names a command that runs a program on a Linux machine
with a display, which sees this computer's files at the same paths: a
VM, as `ASUKA_LINUX_RUN="orb -m ubuntu"` (OrbStack) or
`ASUKA_LINUX_RUN="limactl shell default"` (Lima). Asuka runs the app with
it, for that machine's architecture.

## On a phone: the player and built apps

iOS needs a Mac; Android, a Mac or Linux. By default, `asuka dev` runs
your Go code on your computer, and its views in the **player**: an app
named Asuka (`dev.asuka.player`) that is the platform's host with no app
in it. The player is built and installed once, shared by every app, and
installed again only when it changes (a new version of Asuka). The Go
code and the player talk over a socket.

On a save, Asuka rebuilds the Go code for your computer and restarts it,
in about a second; the player reconnects and shows the new code.
Nothing is installed.

With `--no-hot` or `--once`, or when the app uses native packages with code
for the platform (a `Package.swift` or `android/`, see [Native
code](native.md)), Asuka builds the app itself instead, its Go code inside
as a release build has it, under the app's own name and ID:

- **iOS**: the host, with the native packages' Swift, is built once
  and again only as it changes;
  each save rebuilds the Go code, links the app, signs it for the
  simulator and reinstalls it with `xcrun simctl`.
- **Android**: Gradle builds the app, with its Kotlin, once for each
  version of the host, of the Kotlin and of the manifest; each save
  rebuilds the Go library, pushes it into the app's private files with
  `adb`, where the debug app loads it from, and restarts the app.

The player has every permission any Asuka package needs, and its own
purpose strings. What `asuka.json` gives the app itself (its name, its
ID, a URL `scheme`, `links`, `ios.usage`) only a built app has: try
those with `--no-hot`. The `accent` color applies in the player too.

## What a reload keeps

A reload keeps the values of the app's signals, with no code of yours:
in the player, on the desktop and in the browser. The app saves them as
it stops, to the file the `ASUKA_STATE` environment variable names
(`asuka dev` sets it to `state.json` in the app's directory in Asuka's
cache), and the next run gives each value back to the signal made at
the same place: the same call path (function names, not lines), the
same type, and the same position among the signals of that type made
there.

What isn't kept:

- Navigation: a reload starts at the root screen.
- Signals Asuka makes itself, and values JSON doesn't carry (funcs,
  unexported fields).
- Signals whose type changed, and those of a type after one of the same
  type you added at the same place.

A built phone app (`--no-hot`, `--once`, native code) starts again from
nothing on each reload.

Files your Go code writes (`asuka/sdk/store`, `asuka/sdk/sqlite`) are on your
computer, in the player as on the desktop: `store.Dir()` is a `data`
directory of the app's own in Asuka's cache, one for each platform, as
two devices would have theirs. Delete it to start the app again with no
data. In the browser they're the page's (IndexedDB): see [the
web](web.md#the-apps-files).

## Reloading

Asuka watches, by polling, the app's directory and its module's:

- Go files and `go.mod` (not `_test.go`);
- Swift and Kotlin files;
- the language catalogs;
- files under `assets/`;
- with `--platform web`, the app's scripts (`web/*.js`), and in a
  checkout of Asuka the web host's (`platform/web/src`): the page
  reloads with them, without a build.

It skips directories starting with `.`, and `testdata`, `build` and
`node_modules`. A build that fails prints its errors and keeps the
running app; the next save tries again.

`asuka.json`'s fields are read as `asuka dev` starts. Stop it and run it
again after changing them; it rebuilds and reinstalls what changed.

Native code reloads only in a built phone app, where Asuka rebuilds and
reinstalls it on each save. Gradle runs again when Kotlin changes, which
takes longer than a Go change. A desktop app's native code in Go
reloads as the rest of it does; a Mac app's native packages' Swift is
built again as it changes (a second or two), and the app runs again with its
signals' values kept.

## Devices

**Your computer**: `asuka dev` alone, on a Mac, a PC or Linux.

**The iOS simulator** shares the Mac's network, so the player reaches
`asuka dev` directly.

**An Android emulator** reaches your computer at `10.0.2.2`, and the
player connects there.

**An Android phone over USB**: turn on USB debugging in its developer
options, plug it in and accept the prompt, and check that `adb devices`
lists it. `asuka dev --platform android` takes the first device adb lists,
phone or emulator; with several, name it with `--device` and its serial.
The phone reaches your computer through `adb reverse`, which Asuka sets
up for ports 7461 (the app) and 7462 (its assets). In development Asuka
builds for 64-bit phones and emulators (arm64-v8a, x86_64).

**An iPhone**: `asuka dev` runs on the simulator only. To run on an
iPhone, make a build signed for it, `asuka build --platform ios
--distribution development` (see [Shipping](shipping.md#ios)), and
install the `.ipa` with Xcode's Devices window.

**A phone's browser**: `asuka dev --platform web --lan` serves the app to
your network, and prints the addresses to open.

## Assets

On the desktop, the app reads `assets/` in place. In the player, the
app's `assets/` directory stays on your computer: the Go process serves
it over HTTP, and the player loads `ui.Image`'s asset paths, and the
fonts in `assets/fonts`, from there. In the browser, the dev server
serves them. A change to an asset reloads the app.

A built phone app has its assets inside it: on iOS, copied into the app
on each build; on Android, pushed into the app's private files when they
changed.

## Logs

- **On the desktop and in the player**, the terminal shows the Go
  process's output: `fmt.Println`, `log`, panics.
- **In the browser**, the terminal shows the app's output and the
  page's errors too.
- **A built app on iOS**: the app's output, Go's and Swift's `print`,
  through the simulator's console.
- **A built app on Android**: the app's logcat. Android drops a
  process's standard output, so the Go code's goes to logcat, tagged
  `asuka`.
- With `--once` on a phone, nothing: read them with Console.app or `adb
  logcat`.

## Backgrounding the app

Leaving the app in the player (to take a photo with the camera app, to
sign in in the browser) doesn't end the session. While the player is in
the background, the Go code waits up to ten minutes for an answer from
it, rather than the usual ten seconds, since the system may freeze it.
When Android cuts a backgrounded app's network, the session is kept for
three minutes; a player that comes back in time takes it up again, with
its screens as they were. A player that comes back later starts the
app again, at its root screen.

Stopping `asuka dev` (Ctrl-C) stops the player too, so it doesn't join
the next session, which may be another app's. On the desktop it stops
the app.

## asuka vet

```sh
asuka vet [flags] [packages]
```

`asuka vet` finds the mistakes that build and run but do the wrong thing.
It reports:

- **reads**: a signal read while a component builds its views, so what
  it shows never changes, as `ui.StaticText(fmt.Sprint(count.Get()))`
  or `if open.Get() { ... }` in a component's body. See [What
  updates](concepts.md#what-updates).
- **mainthread**: a signal written on a goroutine instead of in
  `ui.Post`. See [The main thread](concepts.md#the-main-thread).
- **memowrites**: a signal written inside a memo, which panics.

```
$ asuka vet
main.go:14:23: count.Get() is read once, while App builds its views, so the constant made from it never changes: give the prop signal.Func(func() T { ... }) instead, or use count.Peek() to read it once on purpose
```

It reports only what it's sure of, so a report is worth fixing. Each
message names the fix, and how to say you meant it: a read once on
purpose is `Peek`, or `signal.Untrack` for an accessor, which it never
reports.

The packages are `./...` by default, as `go vet` takes them. So are the
flags (`-tags=...`, `-json`), and one per check: `-reads=false` turns
reads off. `asuka vet` exits with 1 when it reports anything, for CI.
It runs the same checks as `go vet -vettool=$(which asuka)`, where `go
vet`'s own checks don't run.

`asuka dev` runs it in the background when it starts and after each
save, and prints what's new as warnings; `asuka build` runs it before it
builds. Neither stops for what it finds. It takes a second or two, beside
a build; `--no-vet` turns it off for either.

## Snapshots and scripts

A desktop app takes a few environment variables that make it show a
state and capture it, for checking a change by eye or in CI. Run the app
with `go run .` in its directory:

```sh
ASUKA_SNAPSHOT=shot.png go run .                  # the window as a PNG, once it has shown, then quit
ASUKA_SNAPSHOT=shot.png ASUKA_DUMP=1 go run .      # and its view tree, in shot.png.txt
ASUKA_SCRIPT="tap:Add;tap:Add" ASUKA_SNAPSHOT=shot.png go run .   # Windows, Linux: steps first
```

- `ASUKA_SNAPSHOT` writes the window as a PNG once it has shown (and
  the script has run), and quits; `ASUKA_SNAPSHOT_STAY=1` keeps the app
  running.
- `ASUKA_DUMP` writes the view tree beside it: each view's kind, frame
  and what it shows.
- `ASUKA_SCRIPT` (Windows and Linux) runs steps, separated by `;`,
  before the snapshot: `tap:<text>` (the view showing it: a button's
  title, a text, a tab), `type:<text>` (into the focused field), `back`,
  `key:<shortcut>` (`ctrl+shift+k`), `menu:<item>` (the menu bar's),
  `escape`, `close` (the window in use), `wait:<duration>` (`500ms`)
  and `snap:<file>`. Each host has a few more: see its `automation.go`.

## Common problems

- **`no iOS simulator SDK (install Xcode)`**: install Xcode, open it
  once, and add an iOS simulator runtime in its Components settings.
  `no iPhone simulator; add one in Xcode` and `no simulator "..."`
  mean there's no simulator of that name: `xcrun simctl list devices`
  lists them.
- **`no Android SDK: install Android Studio or set ANDROID_HOME`**: Asuka
  looks in `ANDROID_HOME`, `ANDROID_SDK_ROOT`, then
  `~/Library/Android/sdk` on a Mac and `~/Android/Sdk` on Linux.
- **`no Android NDK`**: install one from Android Studio's SDK Manager
  (SDK Tools, NDK).
- **`no Android device or emulator`**: make an AVD in Android Studio's
  Device Manager, or plug in a phone. Asuka waits up to four minutes for
  an emulator it starts to boot (`the emulator didn't boot`).
- **Java**: Gradle needs a JDK: `JAVA_HOME`, else Android Studio's own.
- **The first Android build is slow**: Gradle downloads itself and
  Android's libraries, once.
- **A release build on the phone**: an app installed by `asuka build`
  (another signing key) is uninstalled, with its data, to install the
  debug one.
- **`asuka dev --platform windows runs the app on this PC`**: `asuka dev`
  runs a Windows app on Windows only. Build it elsewhere with `asuka
  build --platform windows`, and copy the folder to a PC.
- **`... isn't installed` on Linux**: the host needs GTK 4.14 and
  libadwaita 1.5 or later (`the Linux host needs GTK 4.14 and libadwaita
  1.5 or later`), and some packages a library of their own: WebKitGTK
  6.0 for `ui.WebView`, GStreamer for video and audio, libsecret for
  `asuka/sdk/secure`. The error names it; install it with your distribution's
  package manager.
- **A Linux app that doesn't open**: it needs a display. Over SSH or in
  CI, run it under Xvfb (`xvfb-run -a`), with a session bus
  (`dbus-run-session`) for what goes through D-Bus.
- **The CLI and the app's Asuka**: the CLI carries the hosts' Swift and
  Kotlin, and the app's `go.mod` picks Asuka's Go code. Install the CLI at
  the version the app requires.
- **Starting over**: Asuka's builds, caches, kept state and the apps'
  development data are in Asuka's cache directory: `~/Library/Caches/asuka`
  on a Mac, `~/.cache/asuka` on Linux, `%LocalAppData%\asuka` on Windows.
  Delete it to build everything again, with no state or data.

## Tests

Asuka's own repository tests each host through the platform's real
views.

```sh
go test ./...                                      # the core, against host/fake
go test ./host/darwin                              # macOS: real AppKit views
go test ./host/windows                             # Windows: real windows
xvfb-run -a dbus-run-session go test ./host/linux  # Linux: real GTK widgets
swift test --package-path platform/ios             # the Swift host's decoder
node --test platform/web                           # the web host's decoder
```

`go test ./host/darwin -run Conformance` and `go test ./host/windows
-run Conformance` check what the host draws for a style (edges, corners,
borders, shadows, gradients), measured in a capture of the window; the
e2e tests below do the same on the simulator, the emulator and headless
Chrome.

### GUI tests

The examples run on each platform in `e2e/`, driven through the debug
hosts' automation:

```sh
go test -tags e2e ./e2e -run IOS       # the booted simulator, else a recent iPhone
go test -tags e2e ./e2e -run Android   # the device asuka dev picks
go test -tags e2e ./e2e -run Web       # headless Chrome (ASUKA_CHROME: its path)
xvfb-run -a dbus-run-session go test -tags e2e ./e2e -run Linux  # GTK 4, on Linux
```

Each test builds an example with `asuka dev --once` (a debug build, with
its Go code), then taps, types and reads the screen: through a
notification on iOS, a broadcast on Android, the dev server on the web
(`platform/web/src/automation.js`), and a file in the directory
`ASUKA_COMMANDS` names on Linux (`host/linux/automation.go`). The
counter, notes, chat, navigation and languages examples have tests, the
same on each platform. They wait for the screen to show what they
expect, up to a minute, never for a set time.

The iOS tests need Xcode (`xcrun`); the Android ones need `adb` on your
`PATH`, and the SDK and NDK that `asuka dev --platform android` needs; the
Linux ones a display (Xvfb's, in CI) and a session bus. Without them,
the tests are skipped. macOS and Windows have no e2e tests yet: their
host tests drive real windows instead.

## Benchmarks

`bench/` compares Asuka with SwiftUI and Jetpack Compose: the same app
written in each, timed frame by frame in release builds.

```sh
go run ./bench/run -ios        # asuka and SwiftUI on the simulator
go run ./bench/run -android    # asuka and Compose on the running emulator or device
```

`-runs` sets how many runs of each (5 by default); the medians go to
`bench/results`. [`bench/README.md`](../bench/README.md) has the
scenarios, how they're measured, and the latest results.

## asuka.json

`asuka dev` and `asuka build` read the same `asuka.json`; see
[Shipping](shipping.md#asukajson) for its fields.
