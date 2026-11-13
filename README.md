# RyUI

Native apps in Go for iOS, Android, macOS, Windows, Linux and the web.
RyUI renders with each platform's own widgets (UIKit on iOS, Android
Views and Material 3 on Android, AppKit on the Mac, Win32 drawn as
Windows 11's, GTK 4 and libadwaita on Linux, the DOM in a browser) and
updates them with fine-grained signals: a change touches only the views
that read it.

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

- **Native controls**: stacks and tabs with the platform's navigation,
  lists, text fields, pickers, sheets, alerts, menus, web views, video,
  and your own native views.
- **Motion**: animations, transforms, enter and exit transitions, shared
  elements, gestures.
- **Languages**: typed messages from ICU catalogs that follow the language as
  it changes, CLDR's plurals, numbers and dates, and right-to-left layouts that
  mirror with no code.
- **Batteries**: SQLite with live queries, secure storage, the camera and
  code scanning, photos, location, notifications, audio.
- **`ryui dev`**: your Go code runs on your Mac and reloads on save, the
  app's state kept, on the simulator, an emulator or a phone.
- **`ryui build`**: a signed `.ipa` for the App Store, and an `.aab` for
  Google Play; **`ryui publish`** uploads them, numbered after the store's
  last build, to TestFlight (its groups, App Review) or a Play track.
- **macOS**: the same app on AppKit, in one Go process
  (`ryui dev -platform mac`, `ryui build -platform mac`).
- **Windows**: the same app on Win32 and the common controls,
  drawn as Windows 11's, in one Go process (`ryui dev -platform windows`,
  `ryui build -platform windows`).
- **Linux**: the same app on GTK 4 and libadwaita, called
  without cgo, in one Go process (`ryui dev -platform linux`, `ryui build
  -platform linux`: a folder to install, a `.tar.gz` and a Flatpak
  manifest, from any system).
- **The web**: the same app in a browser, its Go code compiled to
  WebAssembly and its views the page's DOM, placed where RyUI's layout
  puts them (`ryui dev -platform web`, `ryui build -platform web`: a static site). See
  [the web](docs/web.md).

## Quick start

You need Go 1.26 or later, and Xcode (iOS) or Android Studio with an NDK
(Android).

```sh
go install github.com/egoist/ryui/cmd/ryui@latest
ryui new hello
cd hello
ryui dev -platform ios     # or android, web; on a Mac or PC, ryui dev alone runs it there
```

See [the documentation](docs/README.md) for the guides, and
[`examples`](examples) for apps: [`gallery`](examples/gallery) shows
every component.

RyUI is young: the API may change before 1.0.

## License

MIT
