# Getting started

## What you need

- **Go 1.26** or later.
- **For iOS**: a Mac with Xcode, and its iOS simulator.
- **For Android**: Android Studio, with, from its SDK Manager, an NDK
  (SDK Tools → NDK), and an emulator from its Device Manager, or a phone
  with USB debugging on. nui finds the SDK in its usual place, or at
  `ANDROID_HOME`, and Android Studio's Java.

## Install the CLI

```sh
go install github.com/egoist/nui/cmd/nui@latest
```

## Make an app

```sh
nui new hello
cd hello
```

`hello` has:

- `main.go`, the app: a function that returns its interface, given to
  `app.Run`.
- `nui.json`, its name, its ID (the bundle ID and application ID) and
  version, and what [the stores](shipping.md) need.
- `go.mod`.

## Run it

```sh
nui dev -ios        # the iOS simulator
nui dev -android    # an Android emulator or phone
```

The first run builds and installs nui's player app, which shows your
app's views; your Go code runs on your Mac, and talks to the player. Save
a Go file and the app reloads in a moment, without reinstalling, the
values of its signals kept. Its output (`fmt.Println`, panics) is in the
terminal.

- `-device` picks the simulator (its name or UDID) or the Android device
  (an adb serial or an AVD's name).
- `-embed` builds your Go code into the app instead, as a release build
  has it; `-once` builds it in and launches it once.

To run on an iPhone, make a build signed for it: `nui build -ios
-distribution development`, then install the `.ipa` with Xcode's Devices
window.

## Change it

`main.go` from `nui new` is a counter:

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

## An app's files

- `assets/`: images and other files, `ui.Image(ui.ImageProps{Source:
  ui.Val("photo.jpg")})`, and fonts in `assets/fonts`.
- `ios/*.swift` and `android/*.kt`: native views of your own, shown with
  `ui.Native`, and modules your Go code calls with `ui.Call`.

## Next

- [`examples`](../examples): [`gallery`](../examples/gallery) shows every
  component; [`notes`](../examples/notes) keeps notes in SQLite;
  [`navigation`](../examples/navigation) has stacks, tabs and sheets.
- [Shipping](shipping.md): builds for the App Store and Google Play.
