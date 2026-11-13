# Getting started

RyUI needs Go, and for each platform you run on, that platform's tools.
Then one command makes an app, and another runs it, reloading as you
save.

## What you need

**Go 1.26** or later, for every platform. Then, for the ones you want:

- **iOS**: a Mac with Xcode, and its iOS simulator.
- **Android**: a Mac or Linux, with Android Studio and, from its SDK
  Manager, an NDK (SDK Tools → NDK), and an emulator from its Device
  Manager, or a phone with USB debugging on. RyUI finds the SDK in its
  usual place, or at `ANDROID_HOME`, and Android Studio's Java.
- **macOS**: a Mac, macOS 11 or later. Nothing else to run the app;
  release builds, and an app with Swift of its own (`macos/*.swift`),
  use Xcode's command line tools (`xcode-select --install`), which Xcode
  includes.
- **Windows**: a PC with Windows 10 or 11, x64 or Arm. Nothing else:
  RyUI calls Win32 from Go. `ui.WebView` and `ui.Video` use the
  Microsoft Edge WebView2 Runtime, which Windows 11 has, as most
  Windows 10 PCs do.
- **Linux**: GTK 4.14 and libadwaita 1.5 or later (Ubuntu 24.04,
  Fedora 40, Debian 13 and later have them), on Wayland or X11:

  ```sh
  sudo apt install libgtk-4-1 libadwaita-1-0   # Debian, Ubuntu
  sudo dnf install gtk4 libadwaita             # Fedora
  sudo pacman -S gtk4 libadwaita               # Arch
  ```

  `ui.WebView` needs WebKitGTK 6.0 (`libwebkitgtk-6.0-4`), and
  `ui.Video` and audio GStreamer; a package that needs a library that
  isn't there says which, as an error.
- **The web**: a browser. The app is built as WebAssembly by Go alone.

The desktop hosts call their platform without cgo, so `ryui build`
makes a Windows or Linux app from any system, and a Mac app from a Mac.

## Install the CLI

```sh
go install github.com/egoist/ryui/cmd/ryui@latest
```

## Make an app

```sh
ryui new hello
cd hello
```

`hello` has:

- `main.go`, the app: a function that returns its interface, given to
  `app.Run`.
- `ryui.json`, its name, its ID (the bundle ID, application ID and
  desktop app ID) and version, and what [the stores and each
  platform](shipping.md) need.
- `go.mod`.

## Run it

```sh
ryui dev                      # this computer: on a Mac, a PC or Linux
ryui dev -platform ios        # the iOS simulator
ryui dev -platform android    # an Android emulator or phone
ryui dev -platform web        # your browser
```

Save a Go file and the app reloads in a moment, the values of its
signals kept. Its output (`fmt.Println`, panics) is in the terminal.

- **On your computer** (`ryui dev`, or `-platform mac`, `windows`,
  `linux`), the app and its host are one Go program: RyUI builds it,
  runs it in a window, and builds and runs it again on each save.
- **On a phone**, the first run builds and installs RyUI's player app,
  which shows your app's views; your Go code runs on your computer and
  talks to it, so a save reloads without reinstalling.
  `-device` picks the simulator (its name or UDID) or the Android device
  (an adb serial or an AVD's name). `-embed` builds your Go code into
  the app instead, as a release build has it; `-once` builds it in and
  launches it once.
- **In a browser**, RyUI serves the app at `http://localhost:8790/` and
  opens it; each save reloads the page. `-lan` serves it to your
  network, to try it in a phone's browser. See [the web](web.md).

To run on an iPhone, make a build signed for it: `ryui build -platform ios
-distribution development`, then install the `.ipa` with Xcode's Devices
window.

[Development](development.md) has the rest: what a reload keeps,
devices, logs, and the problems you may meet.

## Change it

`main.go` from `ryui new` is a counter:

```go
func App() ui.Node {
	count := signal.New(0)
	return ui.SafeArea(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 16},
		ui.Text(signal.Func(func() string { return fmt.Sprintf("Count: %d", count.Get()) })),
		ui.Button(ui.ButtonProps{Title: ui.Val("Add"), OnPress: func() { count.Set(count.Peek() + 1) }}),
	)
}
```

- **Signals** hold the app's state: `signal.New`, `Get` to read (and be
  updated when it changes), `Set` to write. A `signal.Func` is computed
  from others.
- **Components** are Go functions that return a `ui.Node`. They run once:
  what changes afterwards is what read a signal, and only that.
- **Style** is flexbox, in points: `Grow`, `Gap`, `Padding`, `Align`,
  `Justify`, and the look (`Background`, `CornerRadius`, `Shadow`...). An
  app that sets none looks as the platform's apps do.

The same code is the app on every platform. On a desktop, `app.RunWindow`
gives the main window its title and size, and `ui.Window` and
`ui.MenuBar` add windows and a menu bar (see
[Navigation](navigation.md)).

## An app's files

- `assets/`: images and other files, `ui.Image(ui.ImageProps{Source:
  ui.Val("photo.jpg")})`, and fonts in `assets/fonts`.
- `locales/`: the app's messages in each language (see
  [Languages](i18n.md)).
- Native code of your own, for views shown with `ui.Native` and modules
  your Go code calls with `ui.Call` (see [Native code](native.md)):
  `ios/*.swift`, `android/*.kt`, `macos/*.swift`, `web/*.js`, and, for
  macOS, Windows and Linux, Go files built for that platform.

## Next

- [`examples`](../examples): [`gallery`](../examples/gallery) shows every
  component; [`notes`](../examples/notes) keeps notes in SQLite;
  [`navigation`](../examples/navigation) has stacks, tabs and sheets.
- [Shipping](shipping.md): builds for the App Store, Google Play, the
  Mac, Windows, Linux and the web.
