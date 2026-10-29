// Gallery shows nui's components, one section each.
//
//	go run ./cmd/nui dev -ios examples/gallery
package main

import (
	"fmt"
	"time"

	"github.com/egoist/nui/app"
	"github.com/egoist/nui/device"
	"github.com/egoist/nui/secure"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/store"
	"github.com/egoist/nui/ui"
)

func App() ui.Node {
	return ui.Stack(func() ui.Node {
		nav := ui.UseNavigator()
		toast := signal.New(false) // shown over the screen, at its bottom
		down := signal.New(false)  // scrolled past the first screen
		var page ui.Node
		page = ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}, OnScroll: func(_, y float32) { down.Set(y > 300) }},
			ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 28},
				ui.Button(ui.ButtonProps{Title: ui.Val("Open the inbox (a List)"), Icon: ui.IconMail, Variant: ui.ButtonBordered,
					OnPress: func() {
						nav.Push(inbox, ui.ScreenOptions{Title: ui.Val("Inbox"), Actions: []ui.Action{
							{Title: "Top", Icon: ui.IconUp, OnPress: func() { ui.ScrollToItem(inboxList, 0, true) }},
						}})
					}}),
				motion(toast),
				symbols(),
				fonts(),
				textInput(),
				controls(),
				dates(),
				services(),
				platform(),
			),
		)
		return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}, page, toastOverlay(toast),
			topButton(down, func() { ui.ScrollTo(page, 0, 0, true) }))
	}, ui.ScreenOptions{Title: ui.Val("Gallery"), Actions: []ui.Action{
		{Title: "Share", Icon: ui.IconShare, OnPress: func() {}},
	}})
}

// section is a titled group of examples.
func section(title string, kids ...ui.Node) ui.Node {
	return ui.Column(ui.Style{Gap: 12},
		append([]ui.Node{ui.StaticText(title, ui.Style{Font: ui.Headline})}, kids...)...)
}

func symbols() ui.Node {
	liked := signal.New(false)
	icons := []ui.Icon{ui.IconHome, ui.IconSearch, ui.IconBell, ui.IconMail, ui.IconCamera,
		ui.IconCalendar, ui.IconLocation, ui.IconCart, ui.IconLock, ui.IconSettings}
	var row []ui.Node
	for _, i := range icons {
		row = append(row, ui.Symbol(i))
	}
	return section("Symbols",
		ui.Row(ui.Style{Gap: 14, Wrap: true}, row...),
		ui.Row(ui.Style{Gap: 14, Align: ui.AlignCenter},
			ui.Symbol(ui.IconStar.Filled(), ui.Style{Color: ui.Yellow, Font: ui.Title}),
			ui.Symbol(ui.IconHeart.Filled(), ui.Style{Color: ui.Red, Font: ui.Title2}),
			ui.Symbol(ui.IconCheck, ui.Style{Color: ui.Green, Font: ui.Caption}),
		),
		ui.Row(ui.Style{Gap: 12, Wrap: true},
			ui.Button(ui.ButtonProps{Title: ui.Val("Compose"), Icon: ui.IconCompose, Variant: ui.ButtonFilled}),
			ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
				if liked.Get() {
					return "Liked"
				}
				return "Like"
			}), Icon: ui.IconHeart, Variant: ui.ButtonTinted, OnPress: func() { liked.Set(!liked.Peek()) }}),
			ui.Button(ui.ButtonProps{Icon: ui.IconMore, Menu: []ui.Action{
				{Title: "Copy", Icon: ui.IconCopy},
				{Title: "Edit", Icon: ui.IconEdit},
				{Title: "Delete", Icon: ui.IconTrash, Role: ui.ActionDestructive},
			}}),
		),
	)
}

// fonts: a font file in the app's assets (assets/fonts/Pacifico-Regular.ttf,
// under the SIL Open Font License), and a system family. Both scale with
// the text size the user picked, as the text styles do.
func fonts() ui.Node {
	return section("Fonts",
		ui.StaticText("Hello from Pacifico", ui.Style{Font: ui.Custom("Pacifico-Regular", ui.Title)}),
		ui.StaticText("The system's serif, in bold", ui.Style{Font: ui.Font{Family: "serif", Weight: ui.Bold}}),
		ui.StaticText("And its monospace", ui.Style{Font: ui.Custom("monospace", ui.Callout)}),
		ui.Button(ui.ButtonProps{Title: ui.Val("A button in Pacifico"), Variant: ui.ButtonTinted,
			Style: ui.Style{Font: ui.Font{Family: "Pacifico-Regular"}}}),
	)
}

func textInput() ui.Node {
	name := signal.New("")
	message := signal.New("")
	sent := signal.New("")
	return section("Text input",
		ui.TextField(ui.TextFieldProps{Value: name, Placeholder: ui.Val("Name"), ContentType: "name", Autocapitalize: ui.CapitalizeWords}),
		ui.Row(ui.Style{Gap: 8, Align: ui.AlignEnd},
			ui.TextField(ui.TextFieldProps{Value: message, Placeholder: ui.Val("Message"), Multiline: true,
				Style: ui.Style{Grow: 1, Lines: 5}}),
			ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: "Send", Variant: ui.ButtonFilled,
				Disabled: signal.Func(func() bool { return message.Get() == "" }),
				OnPress:  func() { sent.Set(message.Peek()); message.Set("") }}),
		),
		ui.Text(signal.Func(func() string {
			if sent.Get() == "" {
				return "Grows to five lines, then scrolls."
			}
			return "Sent: " + sent.Get()
		}), ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote}),
	)
}

func controls() ui.Node {
	period := store.Signal("period", 1) // kept across launches
	volume := signal.New(0.4)
	wifi := signal.New(true)
	return section("Controls",
		ui.Segmented(ui.SegmentedProps{Segments: []string{"Day", "Week", "Month"}, Selected: period}),
		ui.Text(signal.Func(func() string {
			return []string{"Today", "This week", "This month"}[period.Get()]
		}), ui.Style{Color: ui.SecondaryLabel}),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Symbol(ui.IconSpeaker),
			ui.Slider(ui.SliderProps{Value: volume, Style: ui.Style{Grow: 1}}),
		),
		ui.Progress(volume),
		ui.Progress(nil),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Text(ui.Val("Wi-Fi"), ui.Style{Grow: 1}),
			ui.Toggle(ui.ToggleProps{Value: wifi}),
		),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Activity(),
			ui.StaticText("Loading…", ui.Style{Color: ui.SecondaryLabel}),
		),
	)
}

func dates() ui.Node {
	when := signal.New(time.Now().Add(24 * time.Hour).Truncate(time.Hour))
	row := func(label string, mode ui.DateMode) ui.Node {
		return ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Text(ui.Val(label), ui.Style{Grow: 1}),
			ui.DatePicker(ui.DatePickerProps{Value: when, Mode: mode, Min: time.Now()}))
	}
	return section("Dates",
		row("Date", ui.DateOnly),
		row("Time", ui.TimeOnly),
		row("Starts", ui.DateAndTime),
		ui.Text(signal.Func(func() string { return when.Get().Format("Mon Jan 2, 15:04") }),
			ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote}),
	)
}

type message struct {
	id      int
	from    string
	subject string
}

func messages(from, n int) []message {
	names := []string{"Ada", "Grace", "Linus", "Ken", "Rob", "Barbara"}
	var out []message
	for i := from; i < from+n; i++ {
		out = append(out, message{i, names[i%len(names)], fmt.Sprintf("Message %d", i+1)})
	}
	return out
}

// inbox is a List: swipe a row to archive or delete it; more load at the
// end; the bar's button scrolls back to the top.
func inbox() ui.Node {
	items := signal.NewFunc(messages(0, 30), nil)
	remove := func(i int) {
		l := append([]message(nil), items.Peek()...)
		items.Set(append(l[:i], l[i+1:]...))
	}
	inboxList = ui.List(ui.ListProps{
		SwipeActions: func(i int) []ui.Action {
			return []ui.Action{
				{Title: "Delete", Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i) }},
				{Title: "Archive", Icon: "archivebox", OnPress: func() { remove(i) }},
			}
		},
		OnEndReached: func() {
			l := items.Peek()
			if len(l) < 120 {
				items.Set(append(append([]message(nil), l...), messages(len(l), 30)...))
			}
		},
	}, items, func(m message) int { return m.id }, func(m signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
		return ui.Row(ui.Style{Padding: ui.Symmetric(12, 20), Gap: 12, Align: ui.AlignCenter},
			ui.Symbol(ui.IconPerson.Filled(), ui.Style{Color: ui.SecondaryLabel, Font: ui.Title2}),
			ui.Column(ui.Style{Grow: 1, Gap: 2},
				ui.Text(signal.Func(func() string { return m.Get().from }), ui.Style{Font: ui.Headline}),
				ui.Text(signal.Func(func() string { return m.Get().subject }), ui.Style{Color: ui.SecondaryLabel}),
			),
		)
	})
	return inboxList
}

func services() ui.Node {
	copied := signal.New(false)
	return section("Services",
		ui.StaticText("The segment above is kept: relaunch to see it.", ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote}),
		ui.Row(ui.Style{Gap: 12},
			ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
				if copied.Get() {
					return "Copied"
				}
				return "Copy link"
			}), Icon: ui.IconCopy, Variant: ui.ButtonGray, OnPress: func() { ui.Copy("https://go.dev"); copied.Set(true) }}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Share"), Icon: ui.IconShare, Variant: ui.ButtonGray,
				OnPress: func() { ui.Share("https://go.dev") }}),
		),
	)
}

// motion animates transforms (a tap flips and grows the card, with a
// spring), follows a drag, and brings a toast in and out with a
// transition.
func motion(toast *signal.Signal[bool]) ui.Node {
	flipped := signal.New(false)
	drag := signal.New(ui.Gesture{})
	card := ui.View(ui.ViewProps{
		OnPress: func() {
			ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.5}, func() { flipped.Set(!flipped.Peek()) })
		},
		Style: ui.StyleFunc(func() ui.Style {
			t := ui.Transform{}
			if flipped.Get() {
				t = ui.Transform{Rotate: 6, Scale: 1.04}
			}
			return ui.Style{Height: ui.Pt(120), CornerRadius: 20, Padding: ui.All(20), Justify: ui.JustifyEnd,
				Gradient:  ui.LinearGradient(ui.RGB(99, 102, 241), ui.RGB(236, 72, 153)),
				Shadow:    ui.Shadow{Color: ui.RGBA(0, 0, 0, 80), Radius: 16, Y: 8},
				Transform: t}
		}),
	}, ui.StaticText("Tap me", ui.Style{Color: ui.RGB(255, 255, 255), Font: ui.Title3.Weighted(ui.Bold)}),
		// A material: frosted glass over the gradient.
		ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Top: ui.Pt(16), Right: ui.Pt(16), Material: ui.MaterialThin,
			CornerRadius: 12, Padding: ui.Symmetric(6, 12)}},
			ui.StaticText("Material", ui.Style{Font: ui.Footnote.Weighted(ui.Semibold)})))
	puck := ui.View(ui.ViewProps{
		OnPan: func(g ui.Gesture) {
			if g.State == ui.GestureEnded || g.State == ui.GestureCancelled {
				ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.45}, func() { drag.Set(ui.Gesture{}) })
				return
			}
			drag.Set(g)
		},
		Style: ui.StyleFunc(func() ui.Style {
			g := drag.Get()
			return ui.Style{Width: ui.Pt(64), Height: ui.Pt(64), CornerRadius: 32, Background: ui.Accent,
				Shadow:    ui.Shadow{Color: ui.RGBA(0, 0, 0, 60), Radius: 10, Y: 4},
				Transform: ui.Transform{TranslateX: g.TranslationX, TranslateY: g.TranslationY}}
		}),
	})
	return section("Motion",
		card,
		ui.Row(ui.Style{Gap: 16, Align: ui.AlignCenter},
			puck,
			ui.StaticText("Drag the dot; it springs back.", ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote, Grow: 1}),
		),
		pulse(),
		ui.Button(ui.ButtonProps{Title: ui.Val("Show a toast"), Variant: ui.ButtonGray, OnPress: func() {
			ui.Animate(func() { toast.Set(true) })
			go func() {
				time.Sleep(1500 * time.Millisecond)
				ui.Post(func() { ui.Animate(func() { toast.Set(false) }) })
			}()
		}}),
	)
}

// pulse is a repeating animation: a dot that breathes until it's
// stopped (set without an animation).
func pulse() ui.Node {
	on := signal.New(false)
	return ui.Row(ui.Style{Gap: 16, Align: ui.AlignCenter},
		ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(64), Height: ui.Pt(64), Justify: ui.JustifyCenter, Align: ui.AlignCenter}},
			ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
				s := ui.Style{Width: ui.Pt(24), Height: ui.Pt(24), CornerRadius: 12, Background: ui.Red}
				if on.Get() {
					s.Transform = ui.Transform{Scale: 1.6}
					s.Opacity = 0.35
				}
				return s
			})})),
		ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
			if on.Get() {
				return "Stop"
			}
			return "Pulse"
		}), Variant: ui.ButtonGray, OnPress: func() {
			if on.Peek() {
				on.Set(false)
				return
			}
			ui.AnimateWith(ui.Animation{Duration: 0.8, Curve: ui.EaseInOut, Repeat: ui.Forever, Autoreverses: true}, func() { on.Set(true) })
		}}),
	)
}

// platform uses the native modules: the secure store, device info, the
// in-app browser and the photo picker.
func platform() ui.Node {
	secret := signal.New("")
	info := signal.New("")
	photos := signal.NewFunc([]string(nil), nil)
	go func() {
		if i, err := device.GetInfo(); err == nil {
			ui.Post(func() { info.Set(i.Model + ", " + i.System + " " + i.SystemVersion) })
		}
	}()
	return section("Platform",
		ui.Text(info, ui.Style{Color: ui.SecondaryLabel}),
		ui.Row(ui.Style{Gap: 12, Wrap: true},
			ui.Button(ui.ButtonProps{Title: ui.Val("Keep a secret"), Icon: ui.IconLock, Variant: ui.ButtonGray, OnPress: func() {
				secure.Save("token", "s3cr3t-"+time.Now().Format("150405"), func(err error) {
					if err != nil {
						secret.Set("Couldn't keep it: " + err.Error())
						return
					}
					secure.Load("token", func(v string, ok bool, err error) { secret.Set("Kept: " + v) })
				})
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Browse"), Icon: ui.IconGlobe, Variant: ui.ButtonGray,
				OnPress: func() { device.Browse("https://go.dev") }}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Pick photos"), Icon: ui.IconPhoto, Variant: ui.ButtonGray, OnPress: func() {
				device.PickPhotos(3, func(paths []string, err error) { photos.Set(paths) })
			}}),
		),
		ui.Text(secret, ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote}),
		ui.Row(ui.Style{Gap: 8}, ui.For(photos, func(p string) string { return p }, func(p signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
			return ui.Image(ui.ImageProps{Source: p, Mode: ui.ImageFill, Style: ui.Style{Width: ui.Pt(96), Height: ui.Pt(96), CornerRadius: 12, Clip: true}})
		})),
	)
}

// toastOverlay floats the toast over the screen's bottom, above the home
// indicator, sliding up in and down out.
func toastOverlay(shown signal.Accessor[bool]) ui.Node {
	env := ui.UseEnvironment()
	return ui.Show(shown, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			return ui.Style{Absolute: true, Left: ui.Pt(20), Right: ui.Pt(20), Bottom: ui.Pt(env.Get().SafeBottom + 12),
				Background: ui.SecondaryBackground, CornerRadius: 16, Padding: ui.Symmetric(14, 18),
				Shadow:     ui.Shadow{Color: ui.RGBA(0, 0, 0, 50), Radius: 14, Y: 4},
				Transition: ui.SlideUp}
		})},
			ui.Row(ui.Style{Gap: 10, Align: ui.AlignCenter},
				ui.Symbol(ui.IconCheck, ui.Style{Color: ui.Green}),
				ui.StaticText("Saved")))
	})
}

// topButton scrolls the page back to its top: shown once it's scrolled
// down (ScrollProps.OnScroll, ui.ScrollTo).
func topButton(shown signal.Accessor[bool], top func()) ui.Node {
	env := ui.UseEnvironment()
	return ui.Show(shown, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			return ui.Style{Absolute: true, Right: ui.Pt(20), Bottom: ui.Pt(env.Get().SafeBottom + 12), Transition: ui.Pop}
		})},
			ui.Button(ui.ButtonProps{Icon: ui.IconUp, Label: "Back to top", Variant: ui.ButtonFilled, OnPress: top}))
	})
}

// inboxList is the inbox's List, which the bar's button scrolls.
var inboxList ui.Node

func main() { app.Run(App) }
