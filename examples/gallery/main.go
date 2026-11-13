// Gallery shows Asuka's components, a destination each in a split view's
// sidebar (a drawer on Android, a list that pushes on an iPhone).
//
//	go run ./cmd/asuka dev --platform ios examples/gallery
package main

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/audio"
	"github.com/egoist/asuka/sdk/camera"
	"github.com/egoist/asuka/sdk/camera/record"
	"github.com/egoist/asuka/sdk/chart"
	"github.com/egoist/asuka/sdk/compose"
	"github.com/egoist/asuka/sdk/crash"
	"github.com/egoist/asuka/sdk/device"
	"github.com/egoist/asuka/sdk/location"
	"github.com/egoist/asuka/sdk/notify"
	"github.com/egoist/asuka/sdk/preview"
	"github.com/egoist/asuka/sdk/print"
	"github.com/egoist/asuka/sdk/scan"
	"github.com/egoist/asuka/sdk/secure"
	"github.com/egoist/asuka/sdk/shader"
	"github.com/egoist/asuka/sdk/store"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func App() ui.Node {
	// page is a destination's first screen: its examples in a scroll.
	page := func(kids ...func() ui.Node) func() ui.Node {
		return func() ui.Node {
			nodes := make([]ui.Node, len(kids))
			for i, k := range kids {
				nodes[i] = k()
			}
			return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
				ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 28}, nodes...))
		}
	}
	dest := func(title string, icon ui.Icon, content func() ui.Node, actions ...ui.Action) ui.Destination {
		return ui.Destination{Title: ui.Val(title), Icon: icon, Content: content, Options: ui.ScreenOptions{Actions: actions}}
	}
	badged := func(d ui.Destination, badge signal.Accessor[string]) ui.Destination {
		d.Badge = badge
		return d
	}
	return ui.SplitView(ui.SplitViewProps{Title: ui.Val("Gallery"), Sections: []ui.SidebarSection{
		{Title: ui.Val("Views"), Destinations: []ui.Destination{
			dest("Text and fonts", "textformat", page(fontsPage, textInput)),
			dest("Symbols", ui.IconHome, page(symbols)),
			dest("Images", ui.IconPhoto, page(images)),
			dest("Video", "play.rectangle", page(video)),
			dest("Shapes and canvas", "scribble", page(shapes, canvas)),
			dest("Charts", "chart.bar", page(charts)),
		}},
		{Title: ui.Val("Controls"), Destinations: []ui.Destination{
			dest("Controls", "slider.horizontal.3", page(controls, snackbars)),
			dest("Pages", "rectangle.split.3x1", page(pages)),
			dest("Dates", ui.IconCalendar, page(dates)),
		}},
		{Title: ui.Val("Lists and navigation"), Destinations: []ui.Destination{
			badged(dest("Inbox", ui.IconMail, inbox,
				ui.Action{Title: ui.Val("Top"), Icon: ui.IconUp, OnPress: func() { ui.ScrollToItem(inboxList, 0, true) }}),
				// Its badge counts its messages: swiping one away counts down.
				signal.Func(func() string { return strconv.Itoa(len(inboxItems.Get())) })),
			dest("Settings", ui.IconSettings, settings),
			dest("Photos", ui.IconPhoto, photos),
			dest("Editor", ui.IconCompose, editor),
			dest("Web", ui.IconGlobe, webPage,
				ui.Action{Title: ui.Val("Run script"), Icon: "curlybraces", OnPress: func() {
					ui.EvalJS(webView, `document.getElementById("out").textContent = "Set from Go at " + new Date().toLocaleTimeString()`)
				}},
				ui.Action{Title: ui.Val("Print"), Icon: "printer", OnPress: func() { print.WebView(webView, print.Options{}, nil) }}),
		}},
		{Title: ui.Val("Motion"), Destinations: []ui.Destination{
			badged(dest("Motion", "wand.and.stars", motionPage), ui.Val("New")),
		}},
		{Title: ui.Val("Device"), Destinations: []ui.Destination{
			dest("Camera", ui.IconCamera, cameraScreen),
			dest("Services", "gearshape.2", page(services)),
			dest("Documents", "doc", page(documents)),
			dest("Platform", "iphone", page(platform)),
			dest("Crashes", "exclamationmark.triangle", page(crashes)),
		}},
	}})
}

// motionPage is motion's examples, with the toast they show over the
// screen's bottom and a button back to the top.
func motionPage() ui.Node {
	toast := signal.New(false)
	down := signal.New(false) // scrolled past the first screen
	var scroll ui.Node
	scroll = ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}, OnScroll: func(_, y float32) { down.Set(y > 300) }},
		ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 28}, motion(toast)))
	return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}, scroll, toastOverlay(toast),
		topButton(down, func() { ui.ScrollTo(scroll, 0, 0, true) }))
}

// section is a titled group of examples.
func section(title string, kids ...ui.Node) ui.Node {
	return ui.Column(ui.Style{Gap: 12},
		append([]ui.Node{ui.StaticText(title, ui.Style{Font: fonts.Headline})}, kids...)...)
}

func symbols() ui.Node {
	liked := signal.New(false)
	sortBy := signal.New("Name")
	sorted := func(by string) ui.Action {
		return ui.Action{Title: ui.Val(by), Checked: signal.Func(func() bool { return sortBy.Get() == by }), OnPress: func() { sortBy.Set(by) }}
	}
	icons := []ui.Icon{ui.IconHome, ui.IconSearch, ui.IconBell, ui.IconMail, ui.IconCamera,
		ui.IconCalendar, ui.IconLocation, ui.IconCart, ui.IconLock, ui.IconSettings}
	var row []ui.Node
	for _, i := range icons {
		row = append(row, ui.Symbol(i))
	}
	return section("Symbols",
		ui.Row(ui.Style{Gap: 14, Wrap: true}, row...),
		ui.Row(ui.Style{Gap: 14, Align: ui.AlignCenter},
			ui.Symbol(ui.IconStar.Filled(), ui.Style{Color: colors.Yellow, Font: fonts.Title}),
			ui.Symbol(ui.IconHeart.Filled(), ui.Style{Color: colors.Red, Font: fonts.Title2}),
			ui.Symbol(ui.IconCheck, ui.Style{Color: colors.Green, Font: fonts.Caption}),
		),
		ui.Row(ui.Style{Gap: 12, Wrap: true},
			ui.Button(ui.ButtonProps{Title: ui.Val("Compose"), Icon: ui.IconCompose, Variant: ui.ButtonFilled}),
			ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
				if liked.Get() {
					return "Liked"
				}
				return "Like"
			}), Icon: ui.IconHeart, Variant: ui.ButtonTinted, OnPress: func() { liked.Set(!liked.Peek()) }}),
			// A menu with a section whose choice is checked, a submenu and
			// a disabled item (SwiftUI's Menu with a Section, a nested Menu
			// and .disabled).
			ui.Button(ui.ButtonProps{Icon: ui.IconMore, Label: ui.Val("More"), Menu: []ui.Action{
				{Title: ui.Val("Copy"), Icon: ui.IconCopy},
				{Title: ui.Val("Edit"), Icon: ui.IconEdit},
				ui.MenuSection(ui.Val("Sort by"), sorted("Name"), sorted("Date"), sorted("Size")),
				ui.Submenu(ui.Val("Move to"), ui.IconFolder,
					ui.Action{Title: ui.Val("Inbox")},
					ui.Action{Title: ui.Val("Archive")},
				),
				{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, Disabled: ui.Val(true)},
			}}),
		),
		ui.Text(signal.Func(func() string { return "Sorted by " + sortBy.Get() }),
			ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
	)
}

// fontsPage: a font file in the app's assets (assets/fonts/Pacifico-Regular.ttf,
// under the SIL Open Font License), and a system family. Both scale with
// the text size the user picked, as the text styles do.
func fontsPage() ui.Node {
	return section("Fonts",
		ui.StaticText("Hello from Pacifico", ui.Style{Font: fonts.Custom("Pacifico-Regular", fonts.Title)}),
		ui.StaticText("The system's serif, in bold", ui.Style{Font: fonts.Font{Family: "serif", Weight: fonts.Bold}}),
		ui.StaticText("And its monospace", ui.Style{Font: fonts.Custom("monospace", fonts.Callout)}),
		ui.Button(ui.ButtonProps{Title: ui.Val("A button in Pacifico"), Variant: ui.ButtonTinted,
			Style: ui.Style{Font: fonts.Font{Family: "Pacifico-Regular"}}}),
	)
}

// images: a photo over the network, kept on disk once fetched; until it
// arrives (or if it can't), a placeholder from the app's assets.
func images() ui.Node {
	return section("Images",
		ui.Image(ui.ImageProps{Source: ui.Val("https://picsum.photos/id/1015/600/400"), Placeholder: "placeholder.png",
			Mode: ui.ImageFill, Label: ui.Val("A river in a valley"),
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
				ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel})))
}

// shapes draws outlines (ui.Shape): a ring whose trim animates, and a
// star made of lines.
func shapes() ui.Node {
	done := signal.New(float32(0.3))
	return section("Shapes",
		ui.Row(ui.Style{Gap: 24, Align: ui.AlignCenter, Wrap: true}, // on a narrow phone, the button goes under
			ui.View(ui.ViewProps{Label: ui.Val("Progress"), Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}},
				ui.Shape(ui.ShapeProps{Path: ring, Stroke: colors.SecondaryBackground, LineWidth: 8,
					Style: ui.Style{Absolute: true, Left: ui.Pt(0), Top: ui.Pt(0), Width: ui.Pt(72), Height: ui.Pt(72)}}),
				ui.Shape(ui.ShapeProps{Path: ring, Stroke: colors.Accent, LineWidth: 8, LineCap: ui.CapRound, TrimEnd: done,
					Style: ui.Style{Absolute: true, Left: ui.Pt(0), Top: ui.Pt(0), Width: ui.Pt(72), Height: ui.Pt(72)}})),
			ui.Shape(ui.ShapeProps{Path: star, Fill: colors.Orange, Style: ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}}),
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
			ui.Button(ui.ButtonProps{Icon: ui.IconSend, Label: ui.Val("Send"), Variant: ui.ButtonFilled,
				Disabled: signal.Func(func() bool { return message.Get() == "" }),
				OnPress:  func() { sent.Set(message.Peek()); message.Set("") }}),
		),
		ui.Text(signal.Func(func() string {
			if sent.Get() == "" {
				return "Grows to five lines, then scrolls."
			}
			return "Sent: " + sent.Get()
		}), ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote}),
	)
}

func controls() ui.Node {
	period := store.Signal("period", 1) // kept across launches
	volume := signal.New(0.4)
	wifi := signal.New(true)
	agreed := signal.New(false)
	sizes := []string{"Extra small", "Small", "Medium", "Large", "Extra large"}
	size := signal.New(2)
	cups := signal.New(2.0)
	tint := signal.New(colors.Hex(0x039be5))
	milks := []string{"Whole", "Oat", "Almond", "Soy"}
	milk := signal.New(-1) // none yet: the picker shows its label
	return section("Controls",
		ui.Segmented(ui.SegmentedProps{Segments: ui.Val([]string{"Day", "Week", "Month"}), Selected: period}),
		ui.Text(signal.Func(func() string {
			return []string{"Today", "This week", "This month"}[period.Get()]
		}), ui.Style{Color: colors.SecondaryLabel}),
		ui.Row(ui.Style{Align: ui.AlignStart},
			ui.Picker(ui.PickerProps{Items: ui.Val(milks), Selected: milk, Label: ui.Val("Milk")})),
		ui.Text(signal.Func(func() string {
			if i := milk.Get(); i >= 0 {
				return milks[i] + " milk"
			}
			return "No milk chosen"
		}), ui.Style{Color: colors.SecondaryLabel}),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Symbol(ui.IconSpeaker),
			ui.Slider(ui.SliderProps{Value: volume, Style: ui.Style{Grow: 1}}),
		),
		ui.Progress(volume),
		ui.Progress(nil),
		ui.Row(ui.Style{Gap: 16, Align: ui.AlignCenter},
			ui.ProgressRing(volume),
			ui.ProgressRing(volume, ui.Style{Width: ui.Pt(24), Color: colors.Green}),
			ui.ProgressRing(nil),
		),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Text(ui.Val("Wi-Fi"), ui.Style{Grow: 1}),
			ui.Toggle(ui.ToggleProps{Value: wifi}),
		),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Toggle(ui.ToggleProps{Value: agreed, Checkbox: true}),
			ui.Text(ui.Val("I've read the terms"), ui.Style{Grow: 1}),
		),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Activity(),
			ui.StaticText("Loading…", ui.Style{Color: colors.SecondaryLabel}),
		),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Text(signal.Func(func() string { return fmt.Sprintf("%.0f cups", cups.Get()) }), ui.Style{Grow: 1}),
			ui.Stepper(ui.StepperProps{Value: cups, Min: 1, Max: 8}),
		),
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Symbol(ui.Icon("heart.fill"), ui.StyleFunc(func() ui.Style { return ui.Style{Color: tint.Get(), Font: fonts.Title2} })),
			ui.StaticText("Tint", ui.Style{Grow: 1}),
			ui.ColorPicker(ui.ColorPickerProps{Value: tint, Alpha: true, Title: ui.Val("Tint")}),
		),
		popover(),
		ui.Wheel(ui.WheelProps{Items: ui.Val(sizes), Selected: size}),
		ui.Text(signal.Func(func() string { return "Size: " + sizes[size.Get()] }), ui.Style{Color: colors.SecondaryLabel}),
	)
}

// snackbars are brief messages at the bottom of the window: one with an
// Undo, one that stays until it's dismissed, one that just says so.
func snackbars() ui.Node {
	drafts := signal.New(3)
	var hideUpload func()
	return section("Snackbars",
		ui.Text(signal.Func(func() string { return fmt.Sprintf("%d drafts", drafts.Get()) }), ui.Style{Color: colors.SecondaryLabel}),
		ui.Row(ui.Style{Gap: 12, Wrap: true},
			ui.Button(ui.ButtonProps{Title: ui.Val("Delete a draft"), Icon: ui.IconTrash, Variant: ui.ButtonGray, Disabled: signal.Func(func() bool { return drafts.Get() == 0 }), OnPress: func() {
				drafts.Update(func(n int) int { return n - 1 })
				ui.Snackbar(ui.SnackbarProps{
					Message: "Draft deleted",
					Action:  &ui.Action{Title: ui.Val("Undo"), OnPress: func() { drafts.Update(func(n int) int { return n + 1 }) }},
				})
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Copy the link"), Icon: ui.IconCopy, Variant: ui.ButtonGray, OnPress: func() {
				ui.Snackbar(ui.SnackbarProps{Message: "Link copied"})
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Upload"), Icon: "icloud.and.arrow.up", Variant: ui.ButtonGray, OnPress: func() {
				// Until it's done (or the user swipes it away).
				hideUpload = ui.Snackbar(ui.SnackbarProps{Message: "Uploading 3 photos…", Duration: ui.SnackbarIndefinite})
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Finish the upload"), Variant: ui.ButtonGray, OnPress: func() {
				if hideUpload != nil {
					hideUpload()
					hideUpload = nil
				}
				ui.Snackbar(ui.SnackbarProps{Message: "3 photos uploaded", Duration: ui.SnackbarLong})
			}}),
		),
	)
}

// editor asks before going back with unsaved text (ui.OnBack).
func editor() ui.Node {
	text := signal.New("")
	saved := signal.New("")
	nav := ui.UseNavigator()
	ui.OnBack(func() bool {
		if text.Peek() == saved.Peek() {
			return false
		}
		ui.Alert(ui.AlertProps{Title: "Discard your changes?", Actions: []ui.Action{
			{Title: ui.Val("Keep editing"), Role: ui.ActionCancel},
			{Title: ui.Val("Discard"), Role: ui.ActionDestructive, OnPress: func() {
				saved.Set(text.Peek())
				nav.Pop()
			}},
		}})
		return true
	})
	var field ui.Node
	field = ui.TextField(ui.TextFieldProps{Value: text, Placeholder: ui.Val("Type, then go back"), Multiline: true,
		// A bar on the keyboard: a count, and Done.
		Accessory: func() ui.Node {
			return ui.Row(ui.Style{Padding: ui.Symmetric(6, 16), Align: ui.AlignCenter, Material: ui.MaterialBar},
				ui.Text(signal.Func(func() string { return fmt.Sprintf("%d characters", len([]rune(text.Get()))) }),
					ui.Style{Grow: 1, Color: colors.SecondaryLabel, Font: fonts.Footnote}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Done"), OnPress: func() { ui.Blur(field) }}))
		}})
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
		field,
		ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, OnPress: func() { saved.Set(text.Peek()) }}),
	)
}

// popover shows a small panel pointing at its button.
func popover() ui.Node {
	open := signal.New(false)
	return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 12},
		ui.StaticText("Popover", ui.Style{Grow: 1}),
		ui.Popover(ui.PopoverProps{Open: open, MaxWidth: 280, Content: func() ui.Node {
			return ui.Column(ui.Style{Padding: ui.All(16), Gap: 8},
				ui.StaticText("About this gallery", ui.Style{Font: fonts.Headline}),
				ui.StaticText("Every view here is the platform's own, laid out by Asuka's core and driven by signals.",
					ui.Style{Color: colors.SecondaryLabel}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Done"), Variant: ui.ButtonTinted, OnPress: func() { open.Set(false) }}))
		}}, ui.Button(ui.ButtonProps{Icon: ui.Icon("info.circle"), Label: ui.Val("About"), Tooltip: ui.Val("About"),
			OnPress: func() { open.Set(true) }})))
}

// aurora is a Kage shader: a band of light that flows across a night sky.
var aurora = shader.MustCompile(`package main

var Time float
var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	uv := pos / rectSize()
	t := Time * 0.4
	wave := sin(uv.x*6+t)*0.5 + sin(uv.x*11-t*1.3)*0.25
	band := smoothstep(0.35, 0, abs(uv.y-0.5-wave*0.18))
	sky := mix(vec3(0.02, 0.03, 0.10), vec3(0.05, 0.08, 0.22), uv.y)
	return vec4(sky+Tint.rgb*band*(0.7+0.3*sin(Time+uv.x*4)), 1)
}
`)

// canvas is a gauge drawn by the app, following a slider.
func canvas() ui.Node {
	level := signal.New(0.62)
	return section("Canvas",
		ui.Canvas(func(g *ui.Graphics) {
			w, h := g.Size()
			cx, cy := w/2, h-12
			r := min(w/2, cy) - 10 // room for half the stroke
			track := g.Path().Arc(cx, cy, r, math.Pi, 2*math.Pi, true)
			g.Stroke(track, colors.SecondaryBackground, ui.StrokeStyle{Width: 14, Cap: ui.CapRound})
			v := float32(level.Get())
			arc := g.Path().Arc(cx, cy, r, math.Pi, math.Pi*float32(1+v), true)
			g.Stroke(arc, colors.Accent, ui.StrokeStyle{Width: 14, Cap: ui.CapRound})
			for i := 0; i <= 10; i++ {
				a := math.Pi * (1 + float64(i)/10)
				x0, y0 := cx+float32(math.Cos(a))*(r-22), cy+float32(math.Sin(a))*(r-22)
				x1, y1 := cx+float32(math.Cos(a))*(r-30), cy+float32(math.Sin(a))*(r-30)
				g.Stroke(g.Path().MoveTo(x0, y0).LineTo(x1, y1), colors.TertiaryLabel, ui.StrokeStyle{Width: 2, Cap: ui.CapRound})
			}
			g.Text(fmt.Sprintf("%.0f%%", v*100), cx, cy-8, ui.TextStyle{Size: 34, Weight: fonts.Semibold, Align: ui.TextCenter})
		}, ui.Style{Height: ui.Pt(170)}),
		ui.Slider(ui.SliderProps{Value: level}),
		// A GPU program: an aurora that flows, in the slider's hue.
		ui.Shader(ui.ShaderProps{
			Program: aurora,
			Uniforms: signal.Func(func() []ui.Uniform {
				return []ui.Uniform{{Name: "Tint", Value: []float32{0.2, float32(0.5 + level.Get()*0.5), 0.6, 1}}}
			}),
			Animated: true,
			Style:    ui.Style{Height: ui.Pt(140), CornerRadius: 16, Clip: true, Background: colors.Hex(0x0b1030)},
		}),
	)
}

// charts are a week's steps as bars, and a smooth line over an area.
func charts() ui.Node {
	days := ui.Val([]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"})
	return section("Charts",
		chart.View(chart.Props{
			Series: ui.Val([]chart.Series{{Name: "Steps", Kind: chart.Bar, Values: []float64{4200, 8100, 6400, 10200, 7300, 12800, 3900}}}),
			Labels: days, Label: ui.Val("Steps this week"), Style: ui.Style{Height: ui.Pt(180)},
		}),
		chart.View(chart.Props{
			Series: ui.Val([]chart.Series{
				{Name: "Heart rate", Kind: chart.Area, Smooth: true, Values: []float64{62, 71, 68, 90, 84, 76, 70}},
				{Name: "Resting", Kind: chart.Line, Color: colors.Hex(0xff9500), Values: []float64{58, 58, 59, 60, 59, 58, 57}},
			}),
			Labels: days, Label: ui.Val("Heart rate this week"), Style: ui.Style{Height: ui.Pt(180)},
		}),
	)
}

// pages is a carousel: a paging Scroll and a PageControl sharing a page.
func pages() ui.Node {
	page := signal.New(0)
	tints := []ui.Color{colors.Hex(0x039be5), colors.Hex(0x43a047), colors.Hex(0xfb8c00), colors.Hex(0x8e24aa)}
	cards := make([]ui.Node, len(tints))
	for i, c := range tints {
		cards[i] = ui.View(ui.ViewProps{Style: ui.Style{Background: c, CornerRadius: 16, Margin: ui.Symmetric(0, 4), Justify: ui.JustifyCenter, Align: ui.AlignCenter}},
			ui.StaticText(fmt.Sprintf("Page %d", i+1), ui.Style{Font: fonts.Title2, Color: colors.RGB(255, 255, 255)}))
	}
	return section("Pages",
		ui.Scroll(ui.ScrollProps{Horizontal: true, Paging: true, Page: page, Style: ui.Style{Height: ui.Pt(160)}}, cards...),
		ui.Row(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 8},
			ui.Button(ui.ButtonProps{Icon: ui.Icon("chevron.backward"), Tooltip: ui.Val("Previous page"), Label: ui.Val("Previous page"),
				OnPress: func() { page.Set(max(page.Peek()-1, 0)) }}),
			ui.PageControl(ui.PageControlProps{Count: ui.Val(len(tints)), Page: page}),
			ui.Button(ui.ButtonProps{Icon: ui.Icon("chevron.forward"), Tooltip: ui.Val("Next page"), Label: ui.Val("Next page"),
				OnPress: func() { page.Set(min(page.Peek()+1, len(tints)-1)) }})),
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
		ui.DatePicker(ui.DatePickerProps{Value: when, Inline: true, Min: time.Now()}),
		ui.Text(signal.Func(func() string { return when.Get().Format("Mon Jan 2, 15:04") }),
			ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote}),
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
			nav := ui.UseNavigator()
			// Each photo has a key: its screen zooms out of it (ZoomFrom).
			return ui.View(ui.ViewProps{
				Label: ui.Val("Photo"),
				OnPress: func() {
					i := signal.Untrack(id.Get)
					nav.Push(func() ui.Node { return photo(i) },
						ui.ScreenOptions{Title: ui.Val("Photo"), ZoomFrom: fmt.Sprintf("photo-%d", i)})
				},
				Style: ui.StyleFunc(func() ui.Style { return ui.Style{AspectRatio: 1, Match: fmt.Sprintf("photo-%d", id.Get())} }),
			}, ui.Image(ui.ImageProps{
				Source:      signal.Func(func() string { return fmt.Sprintf("https://picsum.photos/id/%d/300/300", id.Get()) }),
				Placeholder: "placeholder.png", Mode: ui.ImageFill,
				Style: ui.Style{Grow: 1},
			}))
		})
}

// photo is one photo, large: its screen zooms out of the grid's.
func photo(id int) ui.Node {
	return ui.Column(ui.Style{Grow: 1, Background: colors.RGB(0, 0, 0), Justify: ui.JustifyCenter},
		ui.Image(ui.ImageProps{Source: ui.Val(fmt.Sprintf("https://picsum.photos/id/%d/1200/1200", id)),
			Placeholder: "placeholder.png", Mode: ui.ImageFit, Style: ui.Style{AspectRatio: 1}}))
}

// inboxItems are the inbox's messages, kept as other destinations are
// chosen, and counted by its badge.
var inboxItems = signal.NewFunc(messages(0, 30), nil)

func inbox() ui.Node {
	items := inboxItems
	// Removing a message offers to undo it.
	remove := func(i int, said string) {
		l := append([]message(nil), items.Peek()...)
		gone := l[i]
		items.Set(append(l[:i], l[i+1:]...))
		ui.Snackbar(ui.SnackbarProps{Message: said, Action: &ui.Action{Title: ui.Val("Undo"), OnPress: func() {
			l := append([]message(nil), items.Peek()...)
			items.Set(slices.Insert(l, min(i, len(l)), gone))
		}}})
	}
	inboxList = ui.List(ui.ListProps{
		SwipeActions: func(i int) []ui.Action {
			return []ui.Action{
				{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { remove(i, "Message deleted") }},
				{Title: ui.Val("Archive"), Icon: "archivebox", OnPress: func() { remove(i, "Message archived") }},
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
		act := func(said string) func() { return func() { remove(signal.Untrack(i.Get), said) } }
		return ui.View(ui.ViewProps{
			Actions: []ui.A11yAction{{Name: ui.Val("Archive"), Do: act("Message archived")}, {Name: ui.Val("Delete"), Do: act("Message deleted")}},
			Style:   ui.Style{Direction: ui.RowDir, Padding: ui.Symmetric(12, 20), Gap: 12, Align: ui.AlignCenter},
		},
			ui.Symbol(ui.IconPerson.Filled(), ui.Style{Color: colors.SecondaryLabel, Font: fonts.Title2}),
			ui.Column(ui.Style{Grow: 1, Gap: 2},
				ui.Text(signal.Func(func() string { return m.Get().from }), ui.Style{Font: fonts.Headline}),
				ui.Text(signal.Func(func() string { return m.Get().subject }), ui.Style{Color: colors.SecondaryLabel}),
			),
		)
	})
	return inboxList
}

// setting is a row of the settings: an icon, a title, and a switch if
// it has one.
type setting struct {
	icon  ui.Icon
	title string
	on    *signal.Signal[bool]
}

// settings is an inset grouped List: UIKit's inset grouped list on iOS,
// Material 3 Expressive's segmented list on Android. Swipe a row to
// remove it.
func settings() ui.Node {
	items := signal.NewFunc([]setting{
		{ui.IconBell, "Notifications", signal.New(true)},
		{ui.IconLock, "Privacy", nil},
		{ui.IconLocation, "Location", signal.New(false)},
		{ui.IconSpeaker, "Sounds", nil},
		{ui.IconEye, "Display", nil},
		{ui.IconInfo, "About", nil},
	}, nil)
	return ui.List(ui.ListProps{
		Appearance: ui.ListInsetGrouped,
		SwipeActions: func(i int) []ui.Action {
			return []ui.Action{{Title: ui.Val("Remove"), Role: ui.ActionDestructive, OnPress: func() {
				l := append([]setting(nil), items.Peek()...)
				items.Set(append(l[:i], l[i+1:]...))
			}}}
		},
	}, items, func(s setting) string { return s.title }, func(s signal.Accessor[setting], _ signal.Accessor[int]) ui.Node {
		it := signal.Untrack(s.Get)
		kids := []ui.Node{
			ui.Symbol(it.icon, ui.Style{Color: colors.Accent}),
			ui.StaticText(it.title, ui.Style{Grow: 1}),
		}
		if it.on != nil {
			kids = append(kids, ui.Toggle(ui.ToggleProps{Value: it.on}))
		} else {
			kids = append(kids, ui.Symbol(ui.IconForward, ui.Style{Color: colors.TertiaryLabel}))
		}
		return ui.Row(ui.Style{Padding: ui.Symmetric(10, 16), MinHeight: ui.Pt(52), Gap: 16, Align: ui.AlignCenter}, kids...)
	})
}

func services() ui.Node {
	copied := signal.New(false)
	return section("Services",
		ui.StaticText("The segment above is kept: relaunch to see it.", ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote}),
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

// receipt is the markup documents prints and makes a PDF of.
const receipt = `<!doctype html><meta charset="utf-8"><title>Receipt</title>
<style>body { font: 14px system-ui, sans-serif; } h1 { font-size: 22px; } td { padding: 4px 12px 4px 0; }
.total { font-weight: 600; border-top: 1px solid #999; }</style>
<h1>Receipt</h1><p>Thank you for your order.</p>
<table><tr><td>Coffee beans</td><td>2</td><td>$24.00</td></tr><tr><td>Filter papers</td><td>1</td><td>$4.50</td></tr>
<tr class="total"><td>Total</td><td></td><td>$28.50</td></tr></table>`

// documents shows files with the system's viewer, prints, makes a PDF,
// and composes a mail and a text message.
func documents() ui.Node {
	status := signal.New("")
	report := func(what string) func(error) {
		return func(err error) {
			if err != nil {
				status.Set(what + ": " + err.Error())
			}
		}
	}
	var pdfButton, shareButton ui.Node
	pdfButton = ui.Button(ui.ButtonProps{Title: ui.Val("Preview a PDF"), Icon: "doc", Variant: ui.ButtonGray,
		OnPress: func() {
			preview.Show([]string{"docs/Gallery.pdf"}, preview.Options{From: pdfButton}, report("Preview"))
		}})
	shareButton = ui.Button(ui.ButtonProps{Title: ui.Val("Make a PDF and share it"), Icon: ui.IconShare, Variant: ui.ButtonGray, OnPress: func() {
		status.Set("Making the PDF…")
		go func() {
			path, err := print.PDF(receipt, print.PDFOptions{})
			ui.Post(func() {
				if err != nil {
					status.Set("PDF: " + err.Error())
					return
				}
				status.Set("")
				ui.ShareFiles([]string{path}, ui.ShareOptions{Title: "Receipt", From: shareButton}, report("Share"))
			})
		}()
	}})
	return section("Documents",
		ui.Row(ui.Style{Gap: 12, Wrap: true},
			pdfButton,
			ui.Button(ui.ButtonProps{Title: ui.Val("Print a receipt"), Icon: "printer", Variant: ui.ButtonGray, OnPress: func() {
				print.HTML(receipt, print.Options{Job: "Receipt"}, func(r print.Result, err error) {
					if err != nil {
						status.Set("Print: " + err.Error())
						return
					}
					status.Set("Print: " + r.String())
				})
			}}),
			shareButton,
			ui.Button(ui.ButtonProps{Title: ui.Val("Mail the PDF"), Icon: ui.IconMail, Variant: ui.ButtonGray, OnPress: func() {
				compose.Mail(compose.MailMessage{
					To: []string{"friend@example.com"}, Subject: "The gallery's PDF",
					Body: "<p>Here's the <b>gallery's</b> PDF.</p>", HTML: true,
					Attachments: []compose.Attachment{{Source: "docs/Gallery.pdf"}},
				}, func(r compose.Result, err error) {
					if err != nil {
						status.Set("Mail: " + err.Error())
						return
					}
					status.Set("Mail: " + r.String())
				})
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Text a friend"), Icon: "message", Variant: ui.ButtonGray, OnPress: func() {
				compose.Message(compose.TextMessage{To: []string{"+15555550123"}, Body: "Look at Asuka's gallery."}, func(r compose.Result, err error) {
					if err != nil {
						status.Set("Message: " + err.Error())
						return
					}
					status.Set("Message: " + r.String())
				})
			}}),
		),
		ui.Text(status, ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote}),
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
				Gradient:  ui.LinearGradient(colors.RGB(99, 102, 241), colors.RGB(236, 72, 153)),
				Shadow:    ui.Shadow{Color: colors.RGBA(0, 0, 0, 80), Radius: 16, Y: 8},
				Transform: t}
		}),
	}, ui.StaticText("Tap me", ui.Style{Color: colors.RGB(255, 255, 255), Font: fonts.Title3.Weighted(fonts.Bold)}),
		// A material: frosted glass over the gradient.
		ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Top: ui.Pt(16), Right: ui.Pt(16), Material: ui.MaterialThin,
			CornerRadius: 12, Padding: ui.Symmetric(6, 12)}},
			ui.StaticText("Material", ui.Style{Font: fonts.Footnote.Weighted(fonts.Semibold)})))
	puck := ui.View(ui.ViewProps{
		OnPan: func(g ui.Gesture) {
			if g.State == ui.GestureEnded || g.State == ui.GestureCancelled {
				// Back home on a bouncy spring that carries on the fling: its
				// speed toward home, as a fraction of the way left per second.
				d := math.Hypot(float64(g.TranslationX), float64(g.TranslationY))
				v := 0.0
				if d > 1 {
					v = -(float64(g.VelocityX*g.TranslationX) + float64(g.VelocityY*g.TranslationY)) / d / d
				}
				ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.6, Damping: 0.55, Velocity: float32(v)},
					func() { drag.Set(ui.Gesture{}) })
				return
			}
			drag.Set(g)
		},
		Style: ui.StyleFunc(func() ui.Style {
			g := drag.Get()
			return ui.Style{Width: ui.Pt(64), Height: ui.Pt(64), CornerRadius: 32, Background: colors.Accent,
				Shadow:    ui.Shadow{Color: colors.RGBA(0, 0, 0, 60), Radius: 10, Y: 4},
				Transform: ui.Transform{TranslateX: g.TranslationX, TranslateY: g.TranslationY}}
		}),
	})
	return section("Motion",
		card,
		ui.Row(ui.Style{Gap: 16, Align: ui.AlignCenter},
			puck,
			ui.StaticText("Drag the dot; it springs back.", ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote, Grow: 1}),
		),
		matched(),
		pulse(),
		spinner(),
		turner(),
		ui.Button(ui.ButtonProps{Title: ui.Val("Show a toast"), Variant: ui.ButtonGray, OnPress: func() {
			// In, then out a while after it's in: a chain of animations.
			ui.AnimateWith(ui.Animation{OnDone: func() {
				ui.AnimateWith(ui.Animation{Delay: 1.5}, func() { toast.Set(false) })
			}}, func() { toast.Set(true) })
			ui.Announce("Saved") // a screen reader wouldn't notice the toast
		}}),
	)
}

// matched is a row that opens into a card: its artwork and title are
// the card's (Style.Match), growing from the row's places into the
// card's, and back.
func matched() ui.Node {
	open := signal.New(false)
	toggle := func() {
		ui.AnimateWith(ui.Animation{Curve: ui.Spring, Duration: 0.5}, func() { open.Set(!open.Peek()) })
	}
	art := func(size ui.Dim, radius float32) ui.Style {
		return ui.Style{Width: size, Height: size, AspectRatio: 1, CornerRadius: radius, Match: "album",
			Gradient: ui.LinearGradient(colors.RGB(251, 146, 60), colors.RGB(219, 39, 119))}
	}
	return ui.Column(ui.Style{},
		ui.Show(open, func() ui.Node {
			return ui.View(ui.ViewProps{OnPress: toggle, Label: ui.Val("Night Drive, close"),
				Style: ui.Style{Gap: 12, Align: ui.AlignCenter}},
				ui.View(ui.ViewProps{Style: art(ui.Pt(240), 24)}),
				ui.StaticText("Night Drive", ui.Style{Font: fonts.Title2.Weighted(fonts.Bold), Match: "album-title"}))
		}, func() ui.Node {
			return ui.View(ui.ViewProps{OnPress: toggle, Label: ui.Val("Night Drive, open"),
				Style: ui.Style{Direction: ui.RowDir, Gap: 12, Align: ui.AlignCenter}},
				ui.View(ui.ViewProps{Style: art(ui.Pt(56), 10)}),
				ui.StaticText("Night Drive", ui.Style{Font: fonts.Headline, Match: "album-title"}))
		}))
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
		})}, ui.Symbol(ui.IconSettings, ui.Style{Font: fonts.LargeTitle, Color: colors.Accent})),
		ui.Slider(ui.SliderProps{Value: speed, Min: 0, Max: 2, Style: ui.Style{Grow: 1}}))
}

// turner is a square two fingers turn (OnRotate) and pinch (OnPinch);
// letting go keeps where it is.
func turner() ui.Node {
	angle, base := signal.New(float32(0)), float32(0)
	scale, baseScale := signal.New(float32(1)), float32(1)
	return ui.Column(ui.Style{Gap: 12, Align: ui.AlignCenter, Padding: ui.Symmetric(24, 0)},
		ui.View(ui.ViewProps{
			Label: ui.Val("A square to turn"),
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
				return ui.Style{Width: ui.Pt(160), Height: ui.Pt(160), CornerRadius: 24, Background: colors.Purple,
					Transform: ui.Transform{Rotate: angle.Get(), Scale: scale.Get()}}
			}),
		}),
		ui.Text(signal.Func(func() string { return fmt.Sprintf("Turned %.0f°, at %.1f×", angle.Get(), scale.Get()) }),
			ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}))
}

func pulse() ui.Node {
	on := signal.New(false)
	return ui.Row(ui.Style{Gap: 16, Align: ui.AlignCenter},
		ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(64), Height: ui.Pt(64), Justify: ui.JustifyCenter, Align: ui.AlignCenter}},
			ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
				s := ui.Style{Width: ui.Pt(24), Height: ui.Pt(24), CornerRadius: 12, Background: colors.Red}
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
		ui.Text(info, ui.Style{Color: colors.SecondaryLabel}),
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
		ui.Text(secret, ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote}),
		ui.Text(where, ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote}),
		ui.Row(ui.Style{Gap: 8}, ui.For(photos, func(p string) string { return p }, func(p signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
			return ui.Image(ui.ImageProps{Source: p, Mode: ui.ImageFill, Style: ui.Style{Width: ui.Pt(96), Height: ui.Pt(96), CornerRadius: 12, Clip: true}})
		})),
	)
}

// crashes shows the crash reports kept (asuka/sdk/crash): the app's own
// crashes, from earlier runs, and the errors it recorded. A real app
// sends each to its server and deletes it; here, Delete only forgets it.
func crashes() ui.Node {
	reports := signal.NewFunc([]crash.Report(nil), nil)
	crash.OnReport(func(r crash.Report) { reports.Set(append(reports.Peek(), r)) })
	crash.Breadcrumb("gallery: opened")
	return section("Crash reports",
		ui.StaticText("Crash the app, open it again: the crash is here, with what the app was doing.", ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote}),
		ui.Row(ui.Style{Gap: 12, Wrap: true},
			ui.Button(ui.ButtonProps{Title: ui.Val("Crash the app"), Icon: ui.IconWarning, Variant: ui.ButtonGray, OnPress: func() {
				crash.Breadcrumb("gallery: crashing on purpose")
				crash.Set("screen", "gallery")
				var cart map[string]int
				cart["items"]++ // a nil map: Go panics
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Crash on a goroutine"), Variant: ui.ButtonGray, OnPress: func() {
				crash.Breadcrumb("gallery: crashing off the main thread")
				go func() { panic(fmt.Sprintf("sync failed at %s", time.Now().Format("15:04:05"))) }()
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Record an error"), Variant: ui.ButtonGray, OnPress: func() {
				crash.Record(fmt.Errorf("checkout: %w", errNoNetwork))
			}}),
		),
		ui.Show(signal.Func(func() bool { return len(reports.Get()) == 0 }), func() ui.Node {
			return ui.StaticText("No reports.", ui.Style{Color: colors.SecondaryLabel})
		}),
		ui.Column(ui.Style{Gap: 10}, ui.For(reports, func(r crash.Report) string { return r.ID }, func(r signal.Accessor[crash.Report], _ signal.Accessor[int]) ui.Node {
			return ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
				ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 2},
					ui.Text(signal.Func(func() string { return r.Get().Message }), ui.Style{Font: fonts.Subheadline, Lines: 2}),
					ui.Text(signal.Func(func() string {
						c := r.Get().Context
						s := fmt.Sprintf("%s · %s · %s %s", r.Get().Kind, r.Get().Time.Local().Format("Jan 2 15:04"), c.AppVersion, c.Build)
						if n := len(c.Breadcrumbs); n > 0 {
							s += " · last: " + c.Breadcrumbs[n-1].Message
						}
						return s
					}), ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote, Lines: 2})),
				ui.Button(ui.ButtonProps{Icon: ui.IconTrash, Label: ui.Val("Delete"), Variant: ui.ButtonPlain, OnPress: func() {
					id := r.Get().ID
					crash.Delete(id)
					var keep []crash.Report
					for _, x := range reports.Peek() {
						if x.ID != id {
							keep = append(keep, x)
						}
					}
					reports.Set(keep)
				}}))
		})),
	)
}

var errNoNetwork = errors.New("no network")

// toastOverlay floats the toast over the screen's bottom, above the home
// indicator, sliding up in and down out.
func toastOverlay(shown signal.Accessor[bool]) ui.Node {
	env := ui.UseEnvironment()
	return ui.Show(shown, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			return ui.Style{Absolute: true, Left: ui.Pt(20), Right: ui.Pt(20), Bottom: ui.Pt(env.Get().SafeBottom + 12),
				Background: colors.SecondaryBackground, CornerRadius: 16, Padding: ui.Symmetric(14, 18),
				Shadow:     ui.Shadow{Color: colors.RGBA(0, 0, 0, 50), Radius: 14, Y: 4},
				Transition: ui.TransitionSlideUp}
		})},
			ui.Row(ui.Style{Gap: 10, Align: ui.AlignCenter},
				ui.View(ui.ViewProps{Decorative: true}, ui.Symbol(ui.IconCheck, ui.Style{Color: colors.Green})),
				ui.StaticText("Saved")))
	})
}

// topButton scrolls the page back to its top: shown once it's scrolled
// down (ScrollProps.OnScroll, ui.ScrollTo).
func topButton(shown signal.Accessor[bool], top func()) ui.Node {
	env := ui.UseEnvironment()
	return ui.Show(shown, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
			return ui.Style{Absolute: true, Right: ui.Pt(20), Bottom: ui.Pt(env.Get().SafeBottom + 12), Transition: ui.TransitionPop}
		})},
			ui.Button(ui.ButtonProps{Icon: ui.IconUp, Label: ui.Val("Back to top"), Variant: ui.ButtonFilled, OnPress: top}))
	})
}

// cameraScreen shows the camera live (camera.View) with a shutter
// (camera.Capture), and the last photo taken; it records videos
// (asuka/sdk/camera/record), played on a screen of their own, and reads the
// codes it sees (asuka/sdk/scan).
func cameraScreen() ui.Node {
	nav := ui.UseNavigator()
	last := signal.New("")
	status := signal.New("")
	recording := signal.New(false)
	return ui.Column(ui.Style{Grow: 1},
		camera.View(camera.ViewProps{Mode: camera.Video, Style: ui.Style{Grow: 1}, OnError: func(m string) { status.Set(m) },
			Scan: scan.Codes(func(c scan.Code) {
				status.Set(fmt.Sprintf("%s: %s", c.Kind, c.Value))
				ui.Haptic(ui.HapticSuccess)
			})}),
		ui.SafeArea(nil, ui.Row(ui.Style{Padding: ui.All(16), Gap: 16, Align: ui.AlignCenter},
			ui.Show(signal.Func(func() bool { return last.Get() != "" }), func() ui.Node {
				return ui.Image(ui.ImageProps{Source: last, Mode: ui.ImageFill, Label: ui.Val("The last photo"),
					Style: ui.Style{Width: ui.Pt(56), Height: ui.Pt(56), CornerRadius: 8, Clip: true}})
			}),
			ui.Text(status, ui.Style{Grow: 1, Color: colors.SecondaryLabel, Font: fonts.Footnote}),
			ui.Button(ui.ButtonProps{Icon: ui.IconVideo, Variant: ui.ButtonTinted,
				Title: signal.Func(func() string {
					if recording.Get() {
						return "Stop"
					}
					return "Record"
				}),
				OnPress: func() {
					if recording.Get() {
						record.Stop()
						return
					}
					recording.Set(true)
					status.Set("Recording…")
					record.Start(record.Options{MaxDuration: time.Minute}, func(path string, err error) {
						recording.Set(false)
						if err != nil {
							status.Set(err.Error())
							return
						}
						status.Set("Recorded")
						nav.Push(func() ui.Node {
							return ui.Video(ui.VideoProps{Source: ui.Val(path), AutoPlay: true, Loop: true, Style: ui.Style{Grow: 1}})
						}, ui.ScreenOptions{Title: ui.Val("The video")})
					})
				}}),
			ui.Button(ui.ButtonProps{Icon: ui.IconCamera, Label: ui.Val("Take a photo"), Variant: ui.ButtonFilled, OnPress: func() {
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
// (window.asuka.postMessage); the bar's button runs script in it.
func webPage() ui.Node {
	webView = ui.WebView(ui.WebViewProps{
		HTML: ui.Val(`<!doctype html><meta name="viewport" content="width=device-width, initial-scale=1">
<style>:root { color-scheme: light dark; font: -apple-system-body; font-family: system-ui; } body { padding: 20px; }
button { font: inherit; padding: 10px 16px; border-radius: 10px; border: 0; background: #0a84ff; color: white; }</style>
<h2>A web page</h2>
<p>Its button talks to Go; the bar's button runs script here.</p>
<button onclick="asuka.postMessage('Hello from the page')">Post a message</button>
<p id="out"></p>`),
		OnMessage: func(m string) { ui.Alert(ui.AlertProps{Title: "The page says", Message: m}) },
	})
	return ui.SafeArea(ui.Style{Grow: 1}, webView)
}

// inboxList is the inbox's List, which the bar's button scrolls.
var inboxList ui.Node

func main() { app.Run(App) }
