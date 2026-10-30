# nui

Native iOS and Android apps in Go. nui renders with each platform's own
widgets (UIKit on iOS, Android Views and Material 3 on Android) and
updates them with fine-grained signals: a change touches only the views
that read it.

```go
func Counter() ui.Node {
	count := signal.New(0)
	return ui.SafeArea(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 16},
		ui.Text(signal.Func(func() string { return fmt.Sprintf("Count: %d", count.Get()) })),
		ui.Button(ui.ButtonProps{Title: ui.Val("Add"), Variant: ui.ButtonFilled,
			OnPress: func() { count.Set(count.Peek() + 1) }}),
	)
}

func main() { app.Run(Counter) }
```

- **Native controls**: stacks and tabs with the platform's navigation,
  lists, text fields, pickers, sheets, alerts, menus, web views, video,
  and your own native views.
- **Motion**: animations, transforms, enter and exit transitions, shared
  elements, gestures.
- **Batteries**: SQLite with live queries, secure storage, the camera and
  code scanning, photos, location, notifications, audio.
- **`nui dev`**: your Go code runs on your Mac and reloads on save, the
  app's state kept, on the simulator, an emulator or a phone.
- **`nui build`**: a signed `.ipa` for the App Store, and an `.aab` for
  Google Play.

## Quick start

You need Go 1.26 or later, and Xcode (iOS) or Android Studio with an NDK
(Android).

```sh
go install github.com/egoist/nui/cmd/nui@latest
nui new hello
cd hello
nui dev -ios        # or: nui dev -android
```

See [the documentation](docs/README.md) for the guides, and
[`examples`](examples) for apps: [`gallery`](examples/gallery) shows
every component.

nui is young: the API may change before 1.0.

## License

MIT
