// Gallery shows nui's components, one section each.
//
//	go run ./cmd/nui dev -ios examples/gallery
package main

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/nui/app"
	"github.com/egoist/nui/audio"
	"github.com/egoist/nui/camera"
	"github.com/egoist/nui/device"
	"github.com/egoist/nui/location"
	"github.com/egoist/nui/notify"
	"github.com/egoist/nui/scan"
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
				ui.Button(ui.ButtonProps{Title: ui.Val("Open the photos (a grid)"), Icon: ui.IconPhoto, Variant: ui.ButtonBordered,
					OnPress: func() { nav.Push(photos, ui.ScreenOptions{Title: ui.Val("Photos")}) }}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Open the camera"), Icon: ui.IconCamera, Variant: ui.ButtonBordered,
					OnPress: func() { nav.Push(cameraScreen, ui.ScreenOptions{Title: ui.Val("Camera")}) }}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Open a web page"), Icon: ui.IconGlobe, Variant: ui.ButtonBordered,
					OnPress: func() {
						nav.Push(webPage, ui.ScreenOptions{Title: ui.Val("Web"), Actions: []ui.Action{
							{Title: "Run script", Icon: "curlybraces", OnPress: func() {
								ui.EvalJS(webView, `document.getElementById("out").textContent = "Set from Go at " + new Date().toLocaleTimeString()`)
							}},
						}})
					}}),
				motion(toast),
				symbols(),
				fonts(),
				images(),
				shapes(),
				video(),
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

// images: a photo over the network, kept on disk once fetched; until it
// arrives (or if it can't), a placeholder from the app's assets.
func images() ui.Node {
	return section("Images",
		ui.Image(ui.ImageProps{Source: ui.Val("https://picsum.photos/id/1015/600/400"), Placeholder: "placeholder.png",
			Mode: ui.ImageFill, Label: "A river in a valley",
			Style: ui.Style{AspectRatio: 3.0 / 2, CornerRadius: 12}}),
	)
}

// video plays a clip with the platform's player and its controls:
// muted, looping, from the start. It takes the video's size once known.
func video() ui.Node {
	ends := signal.New(0)
	var v ui.Node
	v = ui.Video(ui.VideoProps{Source: ui.Val("https://interactive-examples.mdn.mozilla.net/media/cc0-videos/flower.mp4"),
		AutoPlay: true, Muted: true, Loop: true, OnEnd: func() { ends.Update(func(n int) int { return n + 1 }) },
		Style: ui.Style{CornerRadius: 12}})
	return section("Video", v,
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Button(ui.ButtonProps{Title: ui.Val("Start over"), Variant: ui.ButtonGray, OnPress: func() { ui.Seek(v, 0); ui.Play(v) }}),
			ui.Text(signal.Func(func() string { return fmt.Sprintf("Played through %d times", ends.Get()) }),
				ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel})))
}

// shapes draws outlines (ui.Shape): a ring whose trim animates, and a
// star made of lines.
func shapes() ui.Node {
	done := signal.New(float32(0.3))
	return section("Shapes",
		ui.Row(ui.Style{Gap: 24, Align: ui.AlignCenter},
			ui.View(ui.ViewProps{Label: "Progress", Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}},
				ui.Shape(ui.ShapeProps{Path: ring, Stroke: ui.SecondaryBackground, LineWidth: 8,
					Style: ui.Style{Absolute: true, Left: ui.Pt(0), Top: ui.Pt(0), Width: ui.Pt(72), Height: ui.Pt(72)}}),
				ui.Shape(ui.ShapeProps{Path: ring, Stroke: ui.Accent, LineWidth: 8, LineCap: ui.CapRound, TrimEnd: done,
					Style: ui.Style{Absolute: true, Left: ui.Pt(0), Top: ui.Pt(0), Width: ui.Pt(72), Height: ui.Pt(72)}})),
			ui.Shape(ui.ShapeProps{Path: star, Fill: ui.Orange, Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Fill the ring"), Variant: ui.ButtonGray, OnPress: func() {
				next := float32(0.3)
				if done.Peek() < 1 {
					next = 1
				}
				ui.AnimateWith(ui.Animation{Duration: 0.8, Curve: ui.EaseInOut}, func() { done.Set(next) })
			}}),
		))
}

// ring is a circle inset by half the stroke, starting at the top.
func ring(w, h float32) *ui.Path {
	r := min(w, h)/2 - 4
	return new(ui.Path).Arc(w/2, h/2, r, -math.Pi/2, 3*math.Pi/2, true)
}

// star is a five-pointed star in the box.
func star(w, h float32) *ui.Path {
	p := new(ui.Path)
	for i := range 10 {
		r := min(w, h) / 2
		if i%2 == 1 {
			r *= 0.42
		}
		a := float64(i)*math.Pi/5 - math.Pi/2
		x, y := w/2+r*float32(math.Cos(a)), h/2+r*float32(math.Sin(a))
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	return p.Close()
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
	sizes := []string{"Extra small", "Small", "Medium", "Large", "Extra large"}
	size := signal.New(2)
	cups := signal.New(2.0)
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
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Text(signal.Func(func() string { return fmt.Sprintf("%.0f cups", cups.Get()) }), ui.Style{Grow: 1}),
			ui.Stepper(ui.StepperProps{Value: cups, Min: 1, Max: 8}),
		),
		ui.Wheel(ui.WheelProps{Items: sizes, Selected: size}),
		ui.Text(signal.Func(func() string { return "Size: " + sizes[size.Get()] }), ui.Style{Color: ui.SecondaryLabel}),
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
// photos is a grid of photos over the network: a List with Columns,
// each cell square, the images kept on disk once fetched.
func photos() ui.Node {
	var ids []int
	for i := range 90 {
		ids = append(ids, 10+i)
	}
	return ui.List(ui.ListProps{Columns: 3, Gap: 2}, ui.Val(ids), func(id int) int { return id },
		func(id signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
			return ui.Image(ui.ImageProps{
				Source:      signal.Func(func() string { return fmt.Sprintf("https://picsum.photos/id/%d/300/300", id.Get()) }),
				Placeholder: "placeholder.png", Mode: ui.ImageFill,
				Style: ui.Style{AspectRatio: 1},
			})
		})
}

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
	}, items, func(m message) int { return m.id }, func(m signal.Accessor[message], i signal.Accessor[int]) ui.Node {
		// What swiping does, for a screen reader's user.
		act := func() { remove(signal.Untrack(i.Get)) }
		return ui.View(ui.ViewProps{
			Actions: []ui.A11yAction{{Name: "Archive", Do: act}, {Name: "Delete", Do: act}},
			Style:   ui.Style{Direction: ui.RowDir, Padding: ui.Symmetric(12, 20), Gap: 12, Align: ui.AlignCenter},
		},
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
		spinner(),
		turner(),
		ui.Button(ui.ButtonProps{Title: ui.Val("Show a toast"), Variant: ui.ButtonGray, OnPress: func() {
			ui.Animate(func() { toast.Set(true) })
			ui.Announce("Saved") // a screen reader wouldn't notice the toast
			go func() {
				time.Sleep(1500 * time.Millisecond)
				ui.Post(func() { ui.Animate(func() { toast.Set(false) }) })
			}()
		}}),
	)
}

// pulse is a repeating animation: a dot that breathes until it's
// stopped (set without an animation).
// spinner turns a gear frame by frame (ui.OnFrame), at a speed the
// slider sets: animation the app computes, rather than the platform.
func spinner() ui.Node {
	speed := signal.New(0.5) // turns a second
	angle := signal.New(float32(0))
	var last time.Duration
	ui.OnFrame(func(t time.Duration) {
		if last != 0 {
			a := angle.Peek() + float32((t-last).Seconds()*speed.Peek()*360)
			angle.Set(float32(math.Mod(float64(a), 360)))
		}
		last = t
	})
	return ui.Row(ui.Style{Gap: 16, Align: ui.AlignCenter},
		ui.View(ui.ViewProps{Decorative: true, Style: ui.StyleFunc(func() ui.Style {
			return ui.Style{Width: ui.Pt(64), Justify: ui.JustifyCenter, Align: ui.AlignCenter, Transform: ui.Transform{Rotate: angle.Get()}}
		})}, ui.Symbol(ui.IconSettings, ui.Style{Font: ui.LargeTitle, Color: ui.Accent})),
		ui.Slider(ui.SliderProps{Value: speed, Min: 0, Max: 2, Style: ui.Style{Grow: 1}}))
}

// turner is a square two fingers turn (OnRotate) and pinch (OnPinch);
// letting go keeps where it is.
func turner() ui.Node {
	angle, base := signal.New(float32(0)), float32(0)
	scale, baseScale := signal.New(float32(1)), float32(1)
	return ui.Column(ui.Style{Gap: 12, Align: ui.AlignCenter, Padding: ui.Symmetric(24, 0)},
		ui.View(ui.ViewProps{
			Label: "A square to turn",
			OnRotate: func(g ui.Gesture) {
				angle.Set(base + g.Rotation*180/math.Pi)
				if g.State == ui.GestureEnded || g.State == ui.GestureCancelled {
					base = angle.Peek()
				}
			},
			OnPinch: func(g ui.Gesture) {
				scale.Set(min(max(baseScale*g.Scale, 0.5), 2))
				if g.State == ui.GestureEnded || g.State == ui.GestureCancelled {
					baseScale = scale.Peek()
				}
			},
			Style: ui.StyleFunc(func() ui.Style {
				return ui.Style{Width: ui.Pt(160), Height: ui.Pt(160), CornerRadius: 24, Background: ui.Purple,
					Transform: ui.Transform{Rotate: angle.Get(), Scale: scale.Get()}}
			}),
		}),
		ui.Text(signal.Func(func() string { return fmt.Sprintf("Turned %.0f°, at %.1f×", angle.Get(), scale.Get()) }),
			ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}))
}

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
	where := signal.New("") // location's, notify's
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
			ui.Button(ui.ButtonProps{Title: ui.Val("Read a code in a photo"), Icon: ui.IconQRCode, Variant: ui.ButtonGray, OnPress: func() {
				device.PickPhotos(1, func(paths []string, err error) {
					if len(paths) == 0 {
						return
					}
					scan.File(paths[0], func(codes []scan.Code, err error) {
						switch {
						case err != nil:
							where.Set("Couldn't: " + err.Error())
						case len(codes) == 0:
							where.Set("No code in it")
						default:
							where.Set(fmt.Sprintf("%s: %s", codes[0].Kind, codes[0].Value))
						}
					})
				})
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Where am I?"), Icon: ui.IconLocation, Variant: ui.ButtonGray, OnPress: func() {
				where.Set("Finding you…")
				go func() {
					p, err := location.Current()
					ui.Post(func() {
						if err != nil {
							where.Set("Couldn't: " + err.Error())
							return
						}
						where.Set(fmt.Sprintf("%.4f, %.4f (within %.0f m)", p.Latitude, p.Longitude, p.Accuracy))
					})
				}()
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Take a photo"), Icon: ui.IconCamera, Variant: ui.ButtonGray, OnPress: func() {
				camera.Take(func(path string, err error) {
					switch {
					case err != nil:
						where.Set(err.Error())
					case path != "":
						photos.Set([]string{path})
					}
				})
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Play a chime"), Icon: "speaker.wave.2", Variant: ui.ButtonGray,
				OnPress: func() { audio.Play("sounds/chime.wav") }}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Remind me in 5 s"), Icon: ui.IconBell, Variant: ui.ButtonGray, OnPress: func() {
				go func() {
					ok, err := notify.Request()
					msg := "Allow notifications in Settings"
					if err == nil && ok {
						err = notify.Schedule(notify.Notification{ID: "remind", Title: "Gallery", Body: "Five seconds have passed.", After: 5 * time.Second})
						msg = "Set: in 5 s, even if you leave the app"
					}
					if err != nil {
						msg = err.Error()
					}
					ui.Post(func() { where.Set(msg) })
				}()
			}}),
		),
		ui.Text(secret, ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote}),
		ui.Text(where, ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote}),
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
				ui.View(ui.ViewProps{Decorative: true}, ui.Symbol(ui.IconCheck, ui.Style{Color: ui.Green})),
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

// cameraScreen shows the camera live (camera.View) with a shutter
// (camera.Capture), and the last photo taken; it reads the codes it sees
// (nui/scan).
func cameraScreen() ui.Node {
	last := signal.New("")
	status := signal.New("")
	return ui.Column(ui.Style{Grow: 1},
		camera.View(camera.ViewProps{Style: ui.Style{Grow: 1}, OnError: func(m string) { status.Set(m) },
			Scan: scan.Codes(func(c scan.Code) {
				status.Set(fmt.Sprintf("%s: %s", c.Kind, c.Value))
				ui.Haptic(ui.HapticSuccess)
			})}),
		ui.SafeArea(nil, ui.Row(ui.Style{Padding: ui.All(16), Gap: 16, Align: ui.AlignCenter},
			ui.Show(signal.Func(func() bool { return last.Get() != "" }), func() ui.Node {
				return ui.Image(ui.ImageProps{Source: last, Mode: ui.ImageFill, Label: "The last photo",
					Style: ui.Style{Width: ui.Pt(56), Height: ui.Pt(56), CornerRadius: 8, Clip: true}})
			}),
			ui.Text(status, ui.Style{Grow: 1, Color: ui.SecondaryLabel, Font: ui.Footnote}),
			ui.Button(ui.ButtonProps{Icon: ui.IconCamera, Label: "Take a photo", Variant: ui.ButtonFilled, OnPress: func() {
				camera.Capture(func(path string, err error) {
					if err != nil {
						status.Set(err.Error())
						return
					}
					last.Set(path)
					status.Set("Saved")
				})
			}}))))
}

// webView is the web page's, which the bar's button runs script in.
var webView ui.Node

// webPage is a web page from markup: its button posts a message to Go
// (window.nui.postMessage); the bar's button runs script in it.
func webPage() ui.Node {
	webView = ui.WebView(ui.WebViewProps{
		HTML: ui.Val(`<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1">
<style>:root { color-scheme: light dark; font: -apple-system-body; font-family: system-ui; } body { padding: 20px; }
button { font: inherit; padding: 10px 16px; border-radius: 10px; border: 0; background: #0a84ff; color: white; }</style>
<h2>A web page</h2>
<p>Its button talks to Go; the bar's button runs script here.</p>
<button onclick="nui.postMessage('Hello from the page')">Post a message</button>
<p id="out"></p>`),
		OnMessage: func(m string) { ui.Alert(ui.AlertProps{Title: "The page says", Message: m}) },
	})
	return ui.SafeArea(ui.Style{Grow: 1}, webView)
}

// inboxList is the inbox's List, which the bar's button scrolls.
var inboxList ui.Node

func main() { app.Run(App) }
