# nui documentation

nui makes native iOS and Android apps in Go. An app's interface is Go
functions that return views; nui shows them with each platform's own
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

## Guides

- [Getting started](getting-started.md): install the CLI, make an app and
  run it on the simulator or a phone.
- [Shipping](shipping.md): builds for the App Store and Google Play, and
  what `nui.json` says of the app.

## Reference

- [Compared to SwiftUI and Expo](parity.md): what nui has, and what it
  doesn't yet.
