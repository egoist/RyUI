# Asuka documentation

<!-- repository-only:start -->
[简体中文](zh/README.md)
<!-- repository-only:end -->

Asuka makes apps in Go with each platform's own widgets, on iOS, Android,
macOS, Windows, Linux and the web. An app's interface is Go functions that return views; Asuka
shows them with each platform's own widgets, and updates only the views
that read a signal when it changes.

```go
func Counter() ui.Node {
	count := signal.New(0)
	label := signal.Func(func() string {
		return fmt.Sprintf("Count: %d", count.Get())
	})
	return ui.SafeArea(ui.Style{Align: ui.AlignCenter, Justify: ui.JustifyCenter},
		ui.Text(label),
		ui.Button(ui.ButtonProps{
			Title:   ui.Val("Add"),
			Variant: ui.ButtonFilled,
			OnPress: func() { count.Set(count.Peek() + 1) },
		}),
	)
}

func main() { app.Run(Counter) }
```

## How it works

Your app is Go: its components, its state and the rest of its code. Asuka's
core runs the components, lays the views out (flexbox and grid, in Go),
and sends what changed to the platform's **host**, in a batch for each
frame. The host makes and updates the platform's own views:

| Platform | Host | Its views |
| --- | --- | --- |
| iOS | Swift, with the app's Go built in as a library | UIKit |
| Android | Kotlin, with the app's Go built in as a library | Android Views and Material 3 |
| macOS | Go, in the app's process (AppKit through purego) | AppKit |
| Windows | Go, in the app's process (Win32 through `syscall`) | Win32 and the common controls, drawn as Windows 11's |
| Linux | Go, in the app's process (GTK through purego) | GTK 4 and libadwaita |
| The web | JavaScript, with the app's Go as WebAssembly | The page's DOM and the browser's own controls |

A text field is a `UITextField`, an `EditText`, an `NSTextField`, an
`EDIT` control, a `GtkEntry` or an `<input>`; a stack of screens is a
`UINavigationController`, Material's app bar with its back gesture, the
Mac window's toolbar with its back button, or libadwaita's navigation
view. So an app that sets no style looks as the platform's apps do, and
gets their accessibility, text input and scrolling as they have them.
The desktop hosts call the system's libraries from Go, without cgo, so
a Windows or Linux app builds with Go alone, from any system.

Signals hold the state. A component runs once; when a signal changes,
only the props that read it are sent again, without diffing a tree.

The same app runs everywhere. On a phone it fills the screen; on a
desktop it's a window the user resizes, and it can open more windows
(`ui.Window`, which a phone hasn't) and have a menu bar with
keyboard shortcuts (`ui.MenuBar`, ignored on a phone). A package for
something a platform doesn't have (sensors or home screen widgets on a
Mac) answers there that it isn't available, so the app can check and do
without.

Asuka needs Go 1.26 or later. Apps run on iOS 16, Android 13 (API 33),
macOS 11, Windows 10 and Linux with GTK 4.14 and libadwaita 1.5, and
later; and in current browsers. `asuka dev` runs your app while you work
and reloads it as you save: on the simulator, an emulator or a phone,
with your Go code on your computer; on the computer itself; or in your
browser.

## Guides

- [Getting started](getting-started.md): install the CLI, make an app
  and run it on a phone, the simulator, your computer or a browser.
- [Concepts](concepts.md): components, signals, owners, the main
  thread, and the environment.
- [Layout and style](layout.md): flexbox, sizes, the safe area, colors,
  text, shadows and gradients.
- [Components](components.md): text, buttons, inputs, images, lists,
  video, web views and drawing.
- [Navigation](navigation.md): stacks, tabs, sheets, alerts and menus,
  windows and the menu bar, links into the app, and shares from other
  apps.
- [Motion and gestures](motion.md): animations, transitions, shared
  elements, gestures, keys and haptics.
- [Accessibility](accessibility.md): labels, roles, custom actions,
  announcements, and text that scales.
- [Languages](i18n.md): translated messages with plurals, numbers and
  dates in the user's way, right-to-left layouts, and trying a language.
- [Data](data.md): values kept across launches, SQLite with live
  queries, secure storage, and watching files.
- [Device and services](device.md): the camera, code scanning, photos
  and files, previewing, printing and PDFs, the mail and message
  composers, location, notifications and push, audio, Now Playing and recording,
  network, the media library, work in the background, sign-in and
  biometrics, the screen and the launch screen, maps, the stores'
  services, sensors, image editing, speech, contacts and calendars, home
  screen widgets and shortcuts, and crash reports, on each platform.
- [The desktop](desktop.md): tray icons and the Dock menu, apps in the
  background, global shortcuts, file dialogs and the file manager,
  opening files, one instance, quitting, recent documents, starting at
  login, updates, the displays, power and progress on the app's icon.
- [Plugins](plugins.md): packages that bring native libraries, prebuilt
  for each platform, shipped with the app; SQLite is one.
- [Native code](native.md): native packages, typed modules and views
  declared in Go and implemented in each platform's own project: Swift
  on iOS and macOS, Kotlin on Android, JavaScript on the web, Go on
  Windows and Linux.
- [Development](development.md): `asuka dev` in depth on each platform,
  devices, `asuka vet`, tests and common problems.
- [Testing](testing.md): your app's interface in `go test`, with no
  device or display: tap, type and check what the screen shows,
  snapshots, and waiting for the app's goroutines.
- [Shipping](shipping.md): builds for the App Store and Google Play, a
  signed and notarized Mac app, a Windows folder, a Linux archive and
  Flatpak manifest, desktop apps' signed updates, and what
  `asuka.json` says of the app.
- [The web](web.md): the same app in a browser, as WebAssembly and the
  page's DOM; `asuka dev --platform web`, a static site to deploy,
  embedding, and what it doesn't do yet.

## Reference

- [asuka.json](asuka-json.md): every key of an app's `asuka.json`, its
  type, its default and what it does on each platform.
- [Compared to SwiftUI and Expo](parity.md): what Asuka has, and what it
  doesn't yet.
- [Asuka and MyGo](mygo.md): the platform's widgets or UI drawn by the
  framework, phones or the desktop alone, and when to use each.
- The Go API: `go doc -all github.com/egoist/asuka/ui`, and the other
  packages of the module.
