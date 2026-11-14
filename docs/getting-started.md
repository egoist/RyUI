# Getting started

Asuka needs Go, and for each platform you run on, that platform's tools.
Then one command makes an app, and another runs it, reloading as you
save.

## What you need

**Go 1.26** or later, for every platform. Then, for the ones you want:

- **iOS**: a Mac with Xcode, and its iOS simulator or an iPhone.
- **Android**: a Mac, a Windows 10 or 11 PC (x64 or Arm) or Linux, with
  Android Studio and, from its SDK Manager, an NDK (SDK Tools → NDK),
  and an emulator from its Device Manager, or a phone with USB debugging
  on. Asuka finds the SDK in its usual place, or at `ANDROID_HOME`, and
  Android Studio's Java. On Windows that's all, too: Asuka builds the
  Go code with the NDK's own clang, and Gradle with its `gradlew.bat`.
  On an Arm PC, the NDK and the emulator run emulated (they're x64),
  and the emulator needs an x86_64 system image.
- **macOS**: a Mac, macOS 11 or later. Nothing else to run the app;
  release builds, and an app with Swift of its own (`macos/*.swift`),
  use Xcode's command line tools (`xcode-select --install`), which Xcode
  includes.
- **Windows**: a PC with Windows 10 or 11, x64 or Arm. Nothing else:
  Asuka calls Win32 from Go. `ui.WebView` and `ui.Video` use the
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

The desktop hosts call their platform without cgo, so `asuka build`
makes a Windows or Linux app from any system, and a Mac app from a Mac.

## Install the CLI

```sh
go install github.com/egoist/asuka/cmd/asuka@latest
```

## Make an app

```sh
asuka new hello
cd hello
```

`hello` has:

- `main.go`, the app: a function that returns its interface, given to
  `app.Run`.
- `asuka.json`, its name, its ID (the bundle ID, application ID and
  desktop app ID) and version, and what [the stores and each
  platform](shipping.md) need.
- `go.mod`.

## Run it

```sh
asuka dev                      # this computer: on a Mac, a PC or Linux
asuka dev --platform ios        # the iOS simulator
asuka dev --platform ios --device iphone  # the iPhone plugged in
asuka dev --platform android    # an Android emulator or phone
asuka dev --platform web        # your browser
```

Save a Go file and the app reloads in a moment, the values of its
signals kept. Its output (`fmt.Println`, panics) is in the terminal.

- **On your computer** (`asuka dev`, or `--platform mac`, `windows`,
  `linux`), the app and its host are one Go program: Asuka builds it,
  runs it in a window, and builds and runs it again on each save.
- **On a phone**, the first run builds and installs Asuka's player app,
  which shows your app's views; your Go code runs on your computer and
  talks to it, so a save reloads without reinstalling.
  `--device` picks the simulator or the iPhone (its name or UDID, or
  `iphone` for the one plugged in) or the Android device (an adb serial
  or an AVD's name). `--no-hot` builds your Go code into
  the app instead, as a release build has it; `--once` builds it in and
  launches it once.
- **In a browser**, Asuka serves the app at `http://localhost:8790/` and
  opens it; each save reloads the page. `--lan` serves it to your
  network, to try it in a phone's browser. See [the web](web.md).

To run on an iPhone, plug it in, unlock it, tap Trust and turn on
Developer Mode (Settings › Privacy & Security), and add your Apple ID
to Xcode (Settings › Accounts): `asuka dev --platform ios --device
iphone` signs the player with your team and installs it, the first
time, and reloads it as you save, by USB or Wi-Fi. See
[Devices](development.md#devices).

[Development](development.md) has the rest: what a reload keeps,
devices, logs, and the problems you may meet.

## Change it

`main.go` from `asuka new` is a counter:

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
- Native packages of your own: Go packages that declare a module or a
  view, which each platform implements in its own project (a Swift
  package, a Gradle module, JavaScript, Go on the desktop). `asuka native
  new ./battery` makes one; see [Native code](native.md).

## Next

- [`examples`](../examples): [`gallery`](../examples/gallery) shows every
  component; [`notes`](../examples/notes) keeps notes in SQLite;
  [`navigation`](../examples/navigation) has stacks, tabs and sheets.
- [Shipping](shipping.md): builds for the App Store, Google Play, the
  Mac, Windows, Linux and the web.
