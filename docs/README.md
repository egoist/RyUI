# RyUI documentation

RyUI makes native iOS and Android apps in Go. An app's interface is Go
functions that return views; RyUI shows them with each platform's own
widgets (UIKit on iOS, Android Views and Material 3 on Android), and
updates only the views that read a signal when it changes.

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

Your app is Go: its components, its state and the rest of its code, in
one library built into the app. RyUI's core runs the components, lays the
views out (flexbox, in Go), and sends the platform what changed, in a
batch for each frame; the app's host, in Swift on iOS and Kotlin on
Android, makes and updates the platform's own views. A text field is a
`UITextField` or an `EditText`, a list a `UICollectionView` or a
`RecyclerView`, a stack of screens a `UINavigationController` or
Material's app bar with its back gesture. So an app that sets no style
looks as the platform's apps do, and gets their accessibility, text
input and scrolling as they have them.

Signals hold the state. A component runs once; when a signal changes,
only the props that read it are sent again, without diffing a tree.

RyUI needs Go 1.26 or later, and runs on iOS 16 and Android 8 (API 26)
and later. `ryui dev` runs your Go code on your Mac while you work, and
reloads it as you save, on the simulator, an emulator or a phone.

## Guides

- [Getting started](getting-started.md): install the CLI, make an app and
  run it on the simulator or a phone.
- [Concepts](concepts.md): components, signals, owners, and the main
  thread.
- [Layout and style](layout.md): flexbox, sizes, the safe area, colors,
  text, shadows and gradients.
- [Components](components.md): text, buttons, inputs, images, lists,
  video, web views and drawing.
- [Navigation](navigation.md): stacks, tabs, sheets, alerts and menus,
  links into the app, and shares from other apps.
- [Motion and gestures](motion.md): animations, transitions, shared
  elements, gestures and haptics.
- [Accessibility](accessibility.md): labels, roles, custom actions,
  announcements, and text that scales.
- [Data](data.md): values kept across launches, SQLite with live
  queries, and secure storage.
- [Device and services](device.md): the camera, code scanning, photos,
  location, notifications, audio, network, the media library and work
  in the background.
- [Native code](native.md): views and modules of your own, in Swift and
  Kotlin.
- [Development](development.md): `ryui dev` in depth, devices, tests and
  common problems.
- [Shipping](shipping.md): builds for the App Store and Google Play, and
  what `ryui.json` says of the app.

## Reference

- [Compared to SwiftUI and Expo](parity.md): what RyUI has, and what it
  doesn't yet.
- The Go API: `go doc -all github.com/egoist/ryui/ui`, and the other
  packages of the module.
