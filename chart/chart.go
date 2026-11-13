// Package chart draws charts: lines, areas, bars and points over a value
// axis, as Swift Charts' and Material's do. The marks are drawn on a
// ui.Canvas; the axes' labels are the platform's text, measured by it and
// scaled with the user's text size; screen readers get a summary.
//
//	chart.View(chart.Props{
//		Series: ui.Val([]chart.Series{{Name: "Steps", Kind: chart.Bar, Values: steps}}),
//		Labels: ui.Val([]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}),
//		Style:  ui.Style{Height: ui.Pt(200)},
//	})
package chart

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Kind is how a series is drawn.
type Kind uint8

const (
	Line   Kind = iota // a line through its values
	Area               // a line, filled down to the axis's zero (or its bottom)
	Bar                // a bar per value; several bar series stand side by side
	Points             // a dot per value
)

// Series is one set of values: one per label, from the first.
type Series struct {
	Name   string
	Kind   Kind
	Values []float64 // NaN: no value there (a gap in a line)
	// Color is its color; 0: the next of the palette (the accent first).
	Color ui.Color
	// Width is a line's width, in points (0: 2), or a dot's diameter
	// (0: 7).
	Width float32
	// Smooth draws a line or area as a curve through its values
	// (monotone, so it never overshoots them).
	Smooth bool
}

// Props configures a chart.
type Props struct {
	Series signal.Accessor[[]Series]
	// Labels are the categories along the bottom, one per value; with
	// none, the values are numbered from 1.
	Labels signal.Accessor[[]string]
	// Min and Max fix the value axis's range; both zero: from the values,
	// rounded out to whole ticks, and including zero when a series is
	// bars or an area.
	Min, Max float64
	// Format writes a value axis label (and a value, for screen
	// readers); nil: the shortest that tells the ticks apart.
	Format func(float64) string
	// NoGrid leaves out the lines behind the marks at each tick.
	NoGrid bool
	// Label is what screen readers say the chart is ("Steps this week");
	// they read each series' values after it.
	Label signal.Accessor[string]
	// Style is the chart's box: give it a height (or Grow).
	Style ui.Styler
}

// Palette is the colors series take, in turn, when they give none: the
// app's accent, then the system's colors.
var Palette = []ui.Color{colors.Accent, colors.Hex(0xff9500), colors.Hex(0x34c759), colors.Hex(0xaf52de), colors.Hex(0xff2d55), colors.Hex(0x5ac8fa)}

const (
	plotPad   = 4 // the plot's inset, top and bottom: room for a line's width and a dot
	axisGap   = 6 // between the labels and the plot
	tickCount = 5 // about how many value ticks
)

// scale is the value axis: its range and its ticks.
type scale struct {
	lo, hi float64
	ticks  []float64
	step   float64
}

// View is the chart: the plot, its value labels along its leading side
// and its category labels along its bottom.
func View(p Props) ui.Node {
	series := p.Series
	if series == nil {
		series = ui.Val[[]Series](nil)
	}
	labels := p.Labels
	sc := signal.NewMemoFunc(func() scale { return valueScale(series.Get(), p.Min, p.Max) }, func(a, b scale) bool {
		return a.lo == b.lo && a.hi == b.hi && a.step == b.step && len(a.ticks) == len(b.ticks)
	})
	count := signal.NewMemo(func() int {
		n := 0
		for _, s := range series.Get() {
			n = max(n, len(s.Values))
		}
		if labels != nil {
			n = max(n, len(labels.Get()))
		}
		return n
	})
	format := p.Format
	if format == nil {
		format = func(v float64) string { return short(v, sc.Get().step) }
	}

	plot := ui.Canvas(func(g *ui.Graphics) { draw(g, series.Get(), sc.Get(), count.Get(), !p.NoGrid) },
		ui.Style{GridColumn: place(2), GridRow: place(1), MinHeight: ui.Pt(40)})

	// The value labels: each centered on its tick, in a column as tall as
	// the plot, as wide as the widest.
	yLabels := ui.Dynamic(func() ui.Node {
		s := sc.Get()
		kids := make([]ui.Node, 0, len(s.ticks))
		for _, t := range s.ticks {
			f := float32((s.hi - t) / (s.hi - s.lo))
			kids = append(kids, ui.Row(ui.Style{Absolute: true, Top: ui.Pct(f * 100), End: ui.Pt(0), Height: ui.Pt(0), Align: ui.AlignCenter},
				ui.StaticText(format(t), labelStyle)))
		}
		// An invisible copy of the widest sizes the column.
		widest := ""
		for _, t := range s.ticks {
			if l := format(t); len(l) > len(widest) {
				widest = l
			}
		}
		// The ticks are inset as the plot insets its drawing.
		ticks := ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Top: ui.Pt(plotPad), Bottom: ui.Pt(plotPad), Start: ui.Pt(0), End: ui.Pt(0)}}, kids...)
		return ui.View(ui.ViewProps{Decorative: true, Style: ui.Style{GridColumn: place(1), GridRow: place(1), Margin: ui.Edges{End: axisGap}}},
			ui.StaticText(widest, ui.Style{Font: labelStyle.Font, Hidden: true, Height: ui.Pt(0)}), ticks)
	})

	// The category labels: each centered under its value (a bar's center),
	// fewer when they'd crowd.
	xLabels := ui.Dynamic(func() ui.Node {
		n := count.Get()
		var ls []string
		if labels != nil {
			ls = labels.Get()
		}
		if n == 0 {
			return ui.Node{}
		}
		every := 1
		if n > 12 {
			every = (n + 5) / 6
		}
		kids := []ui.Node{ui.StaticText("0", ui.Style{Font: labelStyle.Font, Hidden: true})} // its height
		for i := 0; i < n; i += every {
			l := strconv.Itoa(i + 1)
			if i < len(ls) {
				l = ls[i]
			}
			x := (float32(i) + 0.5) / float32(n)
			kids = append(kids, ui.Row(ui.Style{Absolute: true, Left: ui.Pct(x * 100), Top: ui.Pt(0), Width: ui.Pt(0), Justify: ui.JustifyCenter},
				ui.StaticText(l, labelStyle)))
		}
		return ui.View(ui.ViewProps{Decorative: true, Style: ui.Style{GridColumn: place(2), GridRow: place(2), Margin: ui.Edges{Top: axisGap}}}, kids...)
	})

	label := p.Label
	spoken := p.Format // a value, read out whole
	if spoken == nil {
		spoken = func(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
	}
	grid := ui.View(ui.ViewProps{Style: ui.Style{Grow: 1, Grid: &ui.Grid{
		Columns: []layout.GridTrack{layout.T(layout.AutoTrack()), layout.T(layout.Fr(1))},
		Rows:    []layout.GridTrack{layout.T(layout.Fr(1)), layout.T(layout.AutoTrack())},
	}}}, yLabels, plot, xLabels)
	return ui.View(ui.ViewProps{
		Label: signal.Func(func() string { return summary(label, series.Get(), labels, spoken) }),
		Role:  ui.RoleImage,
		Style: p.Style,
	}, grid)
}

var labelStyle = ui.Style{Font: fonts.Caption, Color: colors.SecondaryLabel}

func place(n int) layout.Placement { return layout.Placement{Start: layout.Line(n)} }

// valueScale is the range the values need, rounded out to "nice" ticks
// (1, 2, 2.5 or 5 times a power of ten).
func valueScale(series []Series, fixLo, fixHi float64) scale {
	lo, hi := math.Inf(1), math.Inf(-1)
	zero := false
	for _, s := range series {
		if s.Kind == Bar || s.Kind == Area {
			zero = true
		}
		for _, v := range s.Values {
			if !math.IsNaN(v) && !math.IsInf(v, 0) {
				lo, hi = min(lo, v), max(hi, v)
			}
		}
	}
	if fixLo != 0 || fixHi != 0 {
		lo, hi = fixLo, fixHi
	} else {
		if math.IsInf(lo, 0) {
			lo, hi = 0, 1
		}
		if zero {
			lo, hi = min(lo, 0), max(hi, 0)
		}
	}
	if hi <= lo {
		hi = lo + 1
	}
	step := niceStep((hi - lo) / tickCount)
	if fixLo == 0 && fixHi == 0 {
		lo = math.Floor(lo/step) * step
		hi = math.Ceil(hi/step) * step
	}
	s := scale{lo: lo, hi: hi, step: step}
	for t := math.Ceil(lo/step) * step; t <= hi+step*1e-9; t += step {
		if math.Abs(t) < step*1e-9 {
			t = 0
		}
		s.ticks = append(s.ticks, t)
	}
	return s
}

func niceStep(raw float64) float64 {
	if raw <= 0 || math.IsNaN(raw) {
		return 1
	}
	p := math.Pow(10, math.Floor(math.Log10(raw)))
	for _, m := range []float64{1, 2, 2.5, 5, 10} {
		if raw <= m*p {
			return m * p
		}
	}
	return 10 * p
}

// short writes v with as many decimals as the step needs, thousands as k
// and millions as M.
func short(v, step float64) string {
	a := math.Abs(v)
	switch {
	case a >= 1e6 && step >= 1e5:
		return trim(strconv.FormatFloat(v/1e6, 'f', decimals(step/1e6), 64)) + "M"
	case a >= 1e3 && step >= 100:
		return trim(strconv.FormatFloat(v/1e3, 'f', decimals(step/1e3), 64)) + "k"
	}
	return trim(strconv.FormatFloat(v, 'f', decimals(step), 64))
}

func decimals(step float64) int {
	d := 0
	for step < 1 && d < 6 {
		step *= 10
		d++
	}
	if step != math.Floor(step) && d < 6 {
		d++
	}
	return d
}

func trim(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// summary is what screen readers say: the label, then each series'
// values.
func summary(label signal.Accessor[string], series []Series, labels signal.Accessor[[]string], format func(float64) string) string {
	var b strings.Builder
	if label != nil {
		b.WriteString(label.Get())
	}
	var ls []string
	if labels != nil {
		ls = labels.Get()
	}
	for _, s := range series {
		if b.Len() > 0 {
			b.WriteString(". ")
		}
		if s.Name != "" {
			b.WriteString(s.Name)
			b.WriteString(": ")
		}
		for i, v := range s.Values {
			if i > 0 {
				b.WriteString(", ")
			}
			if i < len(ls) {
				b.WriteString(ls[i])
				b.WriteString(" ")
			}
			if math.IsNaN(v) {
				b.WriteString("–")
			} else {
				b.WriteString(format(v))
			}
		}
	}
	return b.String()
}

// draw draws the grid and the marks: bars first, then areas, lines and
// points over them.
func draw(g *ui.Graphics, series []Series, s scale, n int, grid bool) {
	w, h := g.Size()
	if n == 0 || w <= 0 || h <= 0 {
		return
	}
	pad := float32(plotPad)
	y := func(v float64) float32 { return pad + (h-2*pad)*float32((s.hi-v)/(s.hi-s.lo)) }
	slot := w / float32(n)
	x := func(i int) float32 { return slot * (float32(i) + 0.5) }
	if grid {
		for _, t := range s.ticks {
			ty := y(t)
			g.Stroke(g.Path().MoveTo(0, ty).LineTo(w, ty), colors.Separator, ui.StrokeStyle{Width: 1})
		}
	}
	colorOf := func(i int, s Series) ui.Color {
		if s.Color != 0 {
			return s.Color
		}
		return Palette[i%len(Palette)]
	}
	base := min(max(0, s.lo), s.hi)
	var bars []int
	for i, sr := range series {
		if sr.Kind == Bar {
			bars = append(bars, i)
		}
	}
	for bi, i := range bars {
		sr := series[i]
		group := slot * 0.7
		bw := group / float32(len(bars))
		radius := min(bw/4, 4)
		for j, v := range sr.Values {
			if math.IsNaN(v) {
				continue
			}
			x0 := x(j) - group/2 + float32(bi)*bw + bw*0.08
			y0, y1 := y(v), y(base)
			top, ht := min(y0, y1), float32(math.Abs(float64(y1-y0)))
			g.Fill(g.Path().RoundedRect(x0, top, bw*0.84, ht, min(radius, ht/2)), colorOf(i, sr))
		}
	}
	for _, kind := range []Kind{Area, Line, Points} {
		for i, sr := range series {
			if sr.Kind != kind {
				continue
			}
			c := colorOf(i, sr)
			switch kind {
			case Area, Line:
				line := trace(g.Path(), sr, x, y)
				if kind == Area {
					fill := trace(g.Path(), sr, x, y)
					first, last := -1, -1
					for j, v := range sr.Values {
						if !math.IsNaN(v) {
							if first < 0 {
								first = j
							}
							last = j
						}
					}
					if first >= 0 {
						fill.LineTo(x(last), y(base)).LineTo(x(first), y(base)).Close()
						g.FillGradient(fill, ui.Gradient{Colors: []ui.Color{c.Opacity(0.35), c.Opacity(0.04)},
							Start: ui.Point{Y: 0}, End: ui.Point{Y: h}})
					}
				}
				lw := sr.Width
				if lw <= 0 {
					lw = 2
				}
				g.Stroke(line, c, ui.StrokeStyle{Width: lw, Cap: ui.CapRound, Join: ui.JoinRound})
			case Points:
				d := sr.Width
				if d <= 0 {
					d = 7
				}
				for j, v := range sr.Values {
					if !math.IsNaN(v) {
						g.Fill(g.Path().Ellipse(x(j)-d/2, y(v)-d/2, d, d), c)
					}
				}
			}
		}
	}
}

// trace adds a series' line to p: straight, or a monotone cubic
// (Fritsch–Carlson) when it's smooth; NaNs break it.
func trace(p *ui.Path, s Series, x func(int) float32, y func(float64) float32) *ui.Path {
	vals := s.Values
	open := false
	for j := 0; j < len(vals); j++ {
		if math.IsNaN(vals[j]) {
			open = false
			continue
		}
		px, py := x(j), y(vals[j])
		if !open {
			p.MoveTo(px, py)
			open = true
			continue
		}
		if !s.Smooth {
			p.LineTo(px, py)
			continue
		}
		x0, y0 := x(j-1), y(vals[j-1])
		m0, m1 := slope(vals, j-1, x, y), slope(vals, j, x, y)
		dx := (px - x0) / 3
		p.CubicTo(x0+dx, y0+m0*dx, px-dx, py-m1*dx, px, py)
	}
	return p
}

// slope is a monotone curve's tangent at value i (in points per point).
func slope(vals []float64, i int, x func(int) float32, y func(float64) float32) float32 {
	sec := func(a, b int) (float32, bool) {
		if a < 0 || b >= len(vals) || math.IsNaN(vals[a]) || math.IsNaN(vals[b]) {
			return 0, false
		}
		return (y(vals[b]) - y(vals[a])) / (x(b) - x(a)), true
	}
	l, okL := sec(i-1, i)
	r, okR := sec(i, i+1)
	switch {
	case okL && okR:
		if l*r <= 0 {
			return 0 // a peak or a valley: flat, so it doesn't overshoot
		}
		return 2 / (1/l + 1/r)
	case okL:
		return l
	case okR:
		return r
	}
	return 0
}

// String names a kind.
func (k Kind) String() string {
	switch k {
	case Line:
		return "line"
	case Area:
		return "area"
	case Bar:
		return "bar"
	case Points:
		return "points"
	}
	return fmt.Sprintf("Kind(%d)", uint8(k))
}
