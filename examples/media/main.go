// Media: images, gestures, animation and a native view, one screen each.
package main

import (
	"fmt"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

func App() ui.Node {
	return ui.Stack(func() ui.Node {
		nav := ui.UseNavigator()
		item := func(title string, screen func() ui.Node) ui.Node {
			return ui.Button(ui.ButtonProps{Title: ui.Val(title), Variant: ui.ButtonTinted, OnPress: func() {
				nav.Push(screen, ui.ScreenOptions{Title: ui.Val(title)})
			}})
		}
		return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
			item("Images", images),
			item("Gestures", gestures),
			item("Animation", animation),
			item("Native view", native),
		)
	}, ui.ScreenOptions{Title: ui.Val("Media")})
}

func images() ui.Node {
	status := signal.New("Loading…")
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
			ui.StaticText("From the network, filling a 16:9 box", ui.Style{Font: ui.Headline}),
			ui.Image(ui.ImageProps{
				Source:  ui.Val("https://picsum.photos/id/1015/1200/800"),
				Mode:    ui.ImageFill,
				Label:   "A river between mountains",
				Style:   ui.Style{AspectRatio: 16.0 / 9, CornerRadius: 12, Background: ui.SecondaryBackground},
				OnLoad:  func(w, h float32) { status.Set(fmt.Sprintf("Loaded, %.0f×%.0f", w, h)) },
				OnError: func(m string) { status.Set("Failed: " + m) },
			}),
			ui.Text(status, ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),
			ui.StaticText("From the app's assets, at its natural size", ui.Style{Font: ui.Headline}),
			ui.Image(ui.ImageProps{Source: ui.Val("sunset.png"), Style: ui.Style{AlignSelf: ui.AlignStart}}),
		),
	)
}

func gestures() ui.Node {
	// A card you drag around and pinch to resize; it springs back.
	x, y := signal.New(float32(0)), signal.New(float32(0))
	size := signal.New(float32(120))
	var startX, startY, startSize float32
	swipe := signal.New("Swipe the bar below")
	last := signal.New("")
	names := map[ui.SwipeDirection]string{ui.SwipeLeft: "left", ui.SwipeRight: "right", ui.SwipeUp: "up", ui.SwipeDown: "down"}
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16},
		ui.Text(signal.Func(func() string {
			if l := last.Get(); l != "" {
				return l
			}
			return "Drag the card, pinch it, let go"
		}), ui.Style{Color: ui.SecondaryLabel}),
		ui.View(ui.ViewProps{Style: ui.Style{Grow: 1, Background: ui.SecondaryBackground, CornerRadius: 16, Clip: true}},
			ui.View(ui.ViewProps{
				OnPan: func(g ui.Gesture) {
					switch g.State {
					case ui.GestureBegan:
						startX, startY = x.Peek(), y.Peek()
					case ui.GestureChanged:
						signal.Batch(func() { x.Set(startX + g.TranslationX); y.Set(startY + g.TranslationY) })
					default:
						last.Set(fmt.Sprintf("Dragged %.0f, %.0f at %.0f pt/s", g.TranslationX, g.TranslationY, max(abs(g.VelocityX), abs(g.VelocityY))))
						ui.AnimateWith(ui.Animation{Duration: 0.5, Curve: ui.Spring}, func() { x.Set(0); y.Set(0) })
					}
				},
				OnPinch: func(g ui.Gesture) {
					switch g.State {
					case ui.GestureBegan:
						startSize = size.Peek()
					case ui.GestureChanged:
						size.Set(min(max(startSize*g.Scale, 60), 280))
					default:
						last.Set(fmt.Sprintf("Pinched ×%.2f", g.Scale))
						ui.AnimateWith(ui.Animation{Duration: 0.5, Curve: ui.Spring}, func() { size.Set(120) })
					}
				},
				Style: ui.StyleFunc(func() ui.Style {
					s := size.Get()
					return ui.Style{Absolute: true, Left: ui.Pt(40 + x.Get()), Top: ui.Pt(40 + y.Get()),
						Width: ui.Pt(s), Height: ui.Pt(s), Background: ui.Accent, CornerRadius: 24,
						Justify: ui.JustifyCenter, Align: ui.AlignCenter}
				}),
			}, ui.StaticText("Drag me", ui.Style{Color: ui.RGB(255, 255, 255), Font: ui.Headline})),
		),
		ui.View(ui.ViewProps{
			OnSwipe: func(d ui.SwipeDirection) { swipe.Set("Swiped " + names[d]) },
			Style:   ui.Style{Height: ui.Pt(72), Background: ui.SecondaryBackground, CornerRadius: 16, Justify: ui.JustifyCenter, Align: ui.AlignCenter},
		}, ui.Text(swipe)),
	)
}

func animation() ui.Node {
	open := signal.New(false)
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16},
		ui.Button(ui.ButtonProps{Title: ui.Val("Toggle"), Variant: ui.ButtonFilled, OnPress: func() {
			ui.Animate(func() { open.Set(!open.Peek()) })
		}}),
		ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			s := ui.Style{Height: ui.Pt(80), Background: ui.Orange, CornerRadius: 12, Opacity: 0.6, Padding: ui.All(16)}
			if open.Get() {
				s.Height, s.Background, s.CornerRadius, s.Opacity = ui.Pt(240), ui.Purple, 32, 1
			}
			return s
		})}, ui.StaticText("A card that grows, rounds and darkens", ui.Style{Color: ui.RGB(255, 255, 255)})),
		ui.Row(ui.Style{Gap: 12},
			ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
				w := float32(60)
				if open.Get() {
					w = 200
				}
				return ui.Style{Width: ui.Pt(w), Height: ui.Pt(24), Background: ui.Green, CornerRadius: 12}
			})}),
			ui.View(ui.ViewProps{Style: ui.Style{Grow: 1, Height: ui.Pt(24), Background: ui.Blue, CornerRadius: 12}}),
		),
	)
}

func native() ui.Node {
	selected := signal.New(1)
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16},
		ui.StaticText("A segmented control the app registers in Swift and Kotlin (ios/, android/):", ui.Style{Color: ui.SecondaryLabel}),
		ui.Native("Segmented", ui.NativeProps{
			Props: map[string]any{"items": "Day,Week,Month", "selected": selected},
			On: map[string]func(ui.NativeEvent){"changed": func(e ui.NativeEvent) {
				selected.Set(int(e.Int("index")))
			}},
		}),
		ui.Text(signal.Func(func() string { return fmt.Sprint("Selected: ", []string{"Day", "Week", "Month"}[selected.Get()]) })),
		ui.Button(ui.ButtonProps{Title: ui.Val("Select Month from Go"), OnPress: func() { selected.Set(2) }}),
	)
}

func abs(v float32) float32 { return max(v, -v) }

func main() { app.Run(App) }
