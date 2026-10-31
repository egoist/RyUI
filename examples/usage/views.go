package main

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/svg"
	"github.com/egoist/ryui/ui"
)

// The icons: Lucide's, and Claude's mark.
var (
	// Claude's mark, from Simple Icons.
	iconClaude    = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor"><path d="m4.7144 15.9555 4.7174-2.6471.079-.2307-.079-.1275h-.2307l-.7893-.0486-2.6956-.0729-2.3375-.0971-2.2646-.1214-.5707-.1215-.5343-.7042.0546-.3522.4797-.3218.686.0608 1.5179.1032 2.2767.1578 1.6514.0972 2.4468.255h.3886l.0546-.1579-.1336-.0971-.1032-.0972L6.973 9.8356l-2.55-1.6879-1.3356-.9714-.7225-.4918-.3643-.4614-.1578-1.0078.6557-.7225.8803.0607.2246.0607.8925.686 1.9064 1.4754 2.4893 1.8336.3643.3035.1457-.1032.0182-.0728-.164-.2733-1.3539-2.4467-1.445-2.4893-.6435-1.032-.17-.6194c-.0607-.255-.1032-.4674-.1032-.7285L6.287.1335 6.6997 0l.9957.1336.419.3642.6192 1.4147 1.0018 2.2282 1.5543 3.0296.4553.8985.2429.8318.091.255h.1579v-.1457l.1275-1.706.2368-2.0947.2307-2.6957.0789-.7589.3764-.9107.7468-.4918.5828.2793.4797.686-.0668.4433-.2853 1.8517-.5586 2.9021-.3643 1.9429h.2125l.2429-.2429.9835-1.3053 1.6514-2.0643.7286-.8196.85-.9046.5464-.4311h1.0321l.759 1.1293-.34 1.1657-1.0625 1.3478-.8804 1.1414-1.2628 1.7-.7893 1.36.0729.1093.1882-.0183 2.8535-.607 1.5421-.2794 1.8396-.3157.8318.3886.091.3946-.3278.8075-1.967.4857-2.3072.4614-3.4364.8136-.0425.0304.0486.0607 1.5482.1457.6618.0364h1.621l3.0175.2247.7892.522.4736.6376-.079.4857-1.2142.6193-1.6393-.3886-3.825-.9107-1.3113-.3279h-.1822v.1093l1.0929 1.0686 2.0035 1.8092 2.5075 2.3314.1275.5768-.3218.4554-.34-.0486-2.2039-1.6575-.85-.7468-1.9246-1.621h-.1275v.17l.4432.6496 2.3436 3.5214.1214 1.0807-.17.3521-.6071.2125-.6679-.1214-1.3721-1.9246L14.38 17.959l-1.1414-1.9428-.1397.079-.674 7.2552-.3156.3703-.7286.2793-.6071-.4614-.3218-.7468.3218-1.4753.3886-1.9246.3157-1.53.2853-1.9004.17-.6314-.0121-.0425-.1397.0182-1.4328 1.9672-2.1796 2.9446-1.7243 1.8456-.4128.164-.7164-.3704.0667-.6618.4008-.5889 2.386-3.0357 1.4389-1.882.929-1.0868-.0062-.1579h-.0546l-6.3385 4.1164-1.1293.1457-.4857-.4554.0608-.7467.2307-.2429 1.9064-1.3114Z"/></svg>`)
	iconTrendUp   = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="22 7 13.5 15.5 8.5 10.5 2 17"/><polyline points="16 7 22 7 22 13"/></svg>`)
	iconCheck     = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M20 6 9 17l-5-5"/></svg>`)
	iconAlert     = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" x2="12" y1="8" y2="12"/><line x1="12" x2="12.01" y1="16" y2="16"/></svg>`)
	iconHourglass = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M5 22h14"/><path d="M5 2h14"/><path d="M17 22v-4.172a2 2 0 0 0-.586-1.414L12 12l-4.414 4.414A2 2 0 0 0 7 17.828V22"/><path d="M7 2v4.172a2 2 0 0 0 .586 1.414L12 12l4.414-4.414A2 2 0 0 0 17 6.172V2"/></svg>`)
	iconExternal  = svg.MustParse(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 3h6v6"/><path d="M10 14 21 3"/><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/></svg>`)
)

// Claude's colors: its terracotta mark on its cream, as its app's icon,
// the same in light and dark.
var (
	claude      = ui.Hex(0xD97757)
	claudeCream = ui.Hex(0xF4F3EE)
)

// fill is a view's whole box, for views stacked over each other.
var fill = ui.Style{Absolute: true, Left: ui.Pt(0), Top: ui.Pt(0), Width: ui.Pct(100), Height: ui.Pct(100)}

// badge is a service's mark: on iOS on a rounded square, as an app's
// icon; on Android on Material 3's scalloped "cookie".
func badge(service string, size float32) ui.Node {
	return ui.View(ui.ViewProps{Decorative: true, Style: ui.Style{Width: ui.Pt(size), Height: ui.Pt(size),
		Align: ui.AlignCenter, Justify: ui.JustifyCenter}},
		ui.Shape(ui.ShapeProps{Path: mark(size), Fill: claudeCream, Style: fill}),
		svg.View(svg.Props{Icon: iconClaude, Color: ui.Val(claude),
			Style: ui.Style{Width: ui.Pt(size * 0.56), Height: ui.Pt(size * 0.56)}}),
	)
}

// mark is the badge's shape: on iOS a rounded square, as an app's icon;
// on Android Material 3's "cookie".
func mark(size float32) func(w, h float32) *ui.Path {
	if ios {
		return func(w, h float32) *ui.Path { return new(ui.Path).RoundedRect(0, 0, w, h, size*0.225) }
	}
	return cookie
}

// cookie is a disc with nine soft scallops.
func cookie(w, h float32) *ui.Path {
	p := new(ui.Path)
	r := float64(min(w, h)) / 2
	for i := 0; i <= 180; i++ {
		a := 2 * math.Pi * float64(i) / 180
		d := r * (0.93 + 0.07*math.Cos(9*a))
		x, y := float32(r+d*math.Cos(a)), float32(r+d*math.Sin(a))
		if i == 0 {
			p.MoveTo(x, y)
		} else {
			p.LineTo(x, y)
		}
	}
	return p.Close()
}

// paceColor is a forecast's color: semantic, so it follows dark mode.
func paceColor(p Pace) ui.Color {
	switch p {
	case PaceOK:
		return ui.Green
	case PaceTight:
		return ui.Orange
	case PaceOver, PaceUsedUp:
		return ui.Red
	}
	return ui.SecondaryLabel
}

func paceIcon(p Pace) *svg.Icon {
	switch p {
	case PaceOK:
		return iconCheck
	case PaceTight:
		return iconTrendUp
	case PaceOver, PaceUsedUp:
		return iconAlert
	}
	return iconHourglass
}

// meter is a quota's bar, as Material 3's progress indicator draws one:
// what's left (or, showing use, what's used) filled; the part the
// forecast says will go before the reset in the forecast's color; the
// rest the track; a stop dot at its end. The parts are apart by a gap.
func meter(w signal.Accessor[Window]) ui.Node {
	parts := func() (filled, burn, track float32) {
		win := w.Get()
		used := min(win.Used, 100)
		next := used
		if f := win.Forecast(clock.Get()); f.Pace != PaceUnknown {
			next = min(f.AtReset, 100)
		}
		if showUsed.Get() {
			return float32(used), float32(next - used), float32(100 - next)
		}
		return float32(100 - next), float32(next - used), float32(used)
	}
	pace := signal.Func(func() Pace { return w.Get().Forecast(clock.Get()).Pace })
	part := func(size func() float32, color func() ui.Color) ui.Node {
		return ui.Show(signal.Func(func() bool { return size() >= 0.5 }), func() ui.Node {
			return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
				return ui.Style{Grow: size(), Basis: ui.Pt(0), Height: ui.Pt(4), CornerRadius: 2, Background: color()}
			})})
		})
	}
	if ios {
		// A capsule, as UIProgressView: what's filled over the forecast's
		// part over the track.
		over := func(width func() float32, color func() ui.Color) ui.Node {
			return ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
				return ui.Style{Absolute: true, Left: ui.Pt(0), Top: ui.Pt(0), Height: ui.Pct(100), Width: ui.Pct(width()),
					CornerRadius: 3, Background: color()}
			})})
		}
		return ui.View(ui.ViewProps{Label: "Meter", Style: ui.Style{Height: ui.Pt(6), CornerRadius: 3, Clip: true, Background: ui.Gray.Opacity(0.2)}},
			over(func() float32 { f, b, _ := parts(); return f + b }, func() ui.Color { return paceColor(pace.Get()).Opacity(0.4) }),
			over(func() float32 { f, _, _ := parts(); return f }, func() ui.Color { return ui.Accent }),
		)
	}
	return ui.View(ui.ViewProps{
		Label: "Meter",
		Style: ui.Style{Direction: ui.RowDir, Gap: 4, Height: ui.Pt(4)},
	},
		part(func() float32 { f, _, _ := parts(); return f }, func() ui.Color { return ui.Accent }),
		part(func() float32 { _, b, _ := parts(); return b }, func() ui.Color { return paceColor(pace.Get()).Opacity(0.45) }),
		part(func() float32 { _, _, t := parts(); return t }, func() ui.Color { return ui.Accent.Opacity(0.2) }),
		ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Right: ui.Pt(0), Top: ui.Pt(0),
			Width: ui.Pt(4), Height: ui.Pt(4), CornerRadius: 2, Background: ui.Accent}}),
	)
}

// chart draws a window's use from its start to now, a line, and the
// forecast's from now to its reset (or to when it runs out), dashed.
func chart(w signal.Accessor[Window]) ui.Node {
	size := signal.New([2]float32{})
	const pad = 8
	// at is where a time and a percent used are drawn.
	at := func(win Window, t time.Time, used float64, width, height float32) (float32, float32) {
		start := win.Resets.Add(-win.Period())
		x := float32(t.Sub(start).Seconds() / win.Period().Seconds())
		return pad + min(max(x, 0), 1)*(width-2*pad), height - pad - float32(min(used, 100)/100)*(height-2*pad)
	}
	// points are the use so far: the window's start, its samples, now.
	points := func(width, height float32) [][2]float32 {
		win, now := w.Get(), clock.Get()
		x, y := at(win, win.Resets.Add(-win.Period()), 0, width, height)
		pts := [][2]float32{{x, y}}
		for _, s := range win.History {
			x, y := at(win, s.At, s.Used, width, height)
			pts = append(pts, [2]float32{x, y})
		}
		x, y = at(win, now, win.Used, width, height)
		return append(pts, [2]float32{x, y})
	}
	// end is where the forecast ends.
	end := func(width, height float32) (float32, float32) {
		win := w.Get()
		f := win.Forecast(clock.Get())
		switch f.Pace {
		case PaceOver:
			return at(win, f.Out, 100, width, height)
		case PaceUsedUp:
			return at(win, clock.Get(), 100, width, height) // nothing more to forecast
		}
		return at(win, win.Resets, f.AtReset, width, height)
	}
	line := func(width, height float32) *ui.Path {
		p := new(ui.Path)
		for i, pt := range points(width, height) {
			if i == 0 {
				p.MoveTo(pt[0], pt[1])
			} else {
				p.LineTo(pt[0], pt[1])
			}
		}
		return p
	}
	area := func(width, height float32) *ui.Path {
		pts := points(width, height)
		p := line(width, height)
		return p.LineTo(pts[len(pts)-1][0], height-pad).LineTo(pts[0][0], height-pad).Close()
	}
	dot := func(r float32, where func(width, height float32) (float32, float32)) func(width, height float32) *ui.Path {
		return func(width, height float32) *ui.Path {
			x, y := where(width, height)
			return new(ui.Path).Ellipse(x-r, y-r, 2*r, 2*r)
		}
	}
	now := func(width, height float32) (float32, float32) {
		pts := points(width, height)
		return pts[len(pts)-1][0], pts[len(pts)-1][1]
	}
	pace := signal.Func(func() Pace { return w.Get().Forecast(clock.Get()).Pace })
	label := func(t time.Time) string {
		if w.Get().Period() < 24*time.Hour {
			return t.Local().Format("15:04")
		}
		return t.Local().Format("Jan 2")
	}
	return ui.Column(ui.Style{Gap: 8},
		ui.View(ui.ViewProps{
			Label:    "Use so far, and the forecast to the reset",
			OnLayout: func(f ui.Frame) { size.Set([2]float32{f.Width, f.Height}) },
			Style:    ui.Style{Height: ui.Pt(150)},
		},
			// The limit, and nothing used.
			ui.Shape(ui.ShapeProps{Style: fill, Stroke: ui.SecondaryLabel.Opacity(0.5), Dash: []float32{2, 4},
				Path: func(width, height float32) *ui.Path { return new(ui.Path).MoveTo(pad, pad).LineTo(width-pad, pad) }}),
			ui.Shape(ui.ShapeProps{Style: fill, Stroke: ui.Separator,
				Path: func(width, height float32) *ui.Path {
					return new(ui.Path).MoveTo(pad, height-pad).LineTo(width-pad, height-pad)
				}}),
			ui.Shape(ui.ShapeProps{Style: fill, Path: area, Fill: ui.Accent.Opacity(0.15)}),
			ui.Shape(ui.ShapeProps{Style: fill, Path: line, Stroke: ui.Accent, LineWidth: 3, LineCap: ui.CapRound, LineJoin: ui.JoinRound}),
			// The forecast, in its pace's color: made again when that changes.
			ui.Dynamic(func() ui.Node {
				p := pace.Get()
				if p == PaceUnknown {
					return ui.Node{}
				}
				c := paceColor(p)
				return ui.View(ui.ViewProps{Style: fill},
					ui.Shape(ui.ShapeProps{Style: fill, Stroke: c, LineWidth: 3, LineCap: ui.CapRound, Dash: []float32{7, 7},
						Path: func(width, height float32) *ui.Path {
							x0, y0 := now(width, height)
							x1, y1 := end(width, height)
							return new(ui.Path).MoveTo(x0, y0).LineTo(x1, y1)
						}}),
					ui.Shape(ui.ShapeProps{Style: fill, Fill: c, Path: dot(5, end)}),
				)
			}),
			ui.Shape(ui.ShapeProps{Style: fill, Fill: ui.Accent, Stroke: ui.SecondaryGroupedBackground, LineWidth: 3, Path: dot(8, now)}),
			ui.Text(ui.Val("Now"), ui.StyleFunc(func() ui.Style {
				s := size.Get()
				x, y := now(max(s[0], 1), max(s[1], 1))
				// Centered over the dot, inside the chart.
				return ui.Style{Absolute: true, Left: ui.Pt(min(max(x-30, 0), s[0]-60)), Top: ui.Pt(max(y-36, 0)),
					Width: ui.Pt(60), TextAlign: ui.TextCenter, Font: ui.Footnote, Color: ui.SecondaryLabel}
			})),
		),
		ui.Row(ui.Style{Justify: ui.JustifySpaceBetween},
			ui.Text(signal.Func(func() string { return label(w.Get().Resets.Add(-w.Get().Period())) }), ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),
			ui.Text(signal.Func(func() string { return label(w.Get().Resets) }), ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),
		),
	)
}

// quota is a percent used, as the setting shows it: left or used.
func quota(used float64) string {
	if showUsed.Get() {
		return fmt.Sprintf("%.0f%% used", used)
	}
	return fmt.Sprintf("%.0f%% left", max(100-used, 0))
}

// outlook is what a forecast says, for its pace.
func outlook(f Forecast, now time.Time) string {
	switch {
	case f.Pace == PaceUsedUp:
		return "Used up"
	case f.Pace == PaceOver:
		return "Runs out " + when(f.Out, now)
	case showUsed.Get():
		return fmt.Sprintf("~%.0f%% used at reset", min(f.AtReset, 100))
	default:
		return fmt.Sprintf("~%.0f%% left at reset", max(100-f.AtReset, 0))
	}
}

// when is a time to come: its hour today, else its day too.
func when(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return t.Format("15:04")
	}
	if now.Add(24*time.Hour).Format("20060102") == t.Format("20060102") {
		return "tomorrow " + t.Format("15:04")
	}
	return t.Format("Jan 2, 15:04")
}

// ago is how long ago t was.
func ago(t, now time.Time) string {
	d := now.Sub(t)
	plural := func(n int, unit string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s ago", unit)
		}
		return fmt.Sprintf("%d %ss ago", n, unit)
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return plural(int(d.Hours()), "hour")
	}
	return plural(int(d.Hours()/24), "day")
}
