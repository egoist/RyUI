package svg_test

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/egoist/nui/host/fake"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/svg"
	"github.com/egoist/nui/ui"
)

// Lucide's "download" and "circle-check", verbatim.
const (
	lucideDownload    = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" x2="12" y1="15" y2="3"/></svg>`
	lucideCircleCheck = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" class="lucide lucide-circle-check"><circle cx="12" cy="12" r="10"/><path d="m9 12 2 2 4-4"/></svg>`
)

// Material Icons' "home" and "favorite" (filled).
const (
	materialHome     = `<svg xmlns="http://www.w3.org/2000/svg" height="24" viewBox="0 0 24 24" width="24"><path d="M0 0h24v24H0z" fill="none"/><path d="M10 20v-6h4v6h5v-8h3L12 3 2 12h3v8z"/></svg>`
	materialFavorite = `<svg xmlns="http://www.w3.org/2000/svg" height="24" viewBox="0 0 24 24" width="24"><path d="M0 0h24v24H0z" fill="none"/><path d="M12 21.35l-1.45-1.32C5.4 15.36 2 12.28 2 8.5 2 5.42 4.42 3 7.5 3c1.74 0 3.41.81 4.5 2.09C13.09 3.81 14.76 3 16.5 3 19.58 3 22 5.42 22 8.5c0 3.78-3.4 6.86-8.55 11.54L12 21.35z"/></svg>`
)

// layers writes an icon's layers as text: a line each, its paint then
// its path.
func layers(ic *svg.Icon) string {
	var b strings.Builder
	for _, l := range ic.Layers {
		fmt.Fprintf(&b, "fill=%s stroke=%s", paint(l.Fill), paint(l.Stroke))
		if !l.Stroke.None() {
			fmt.Fprintf(&b, " width=%g cap=%d join=%d", l.LineWidth, l.LineCap, l.LineJoin)
		}
		fmt.Fprintf(&b, ": %s\n", l.Path.String())
	}
	return b.String()
}

func paint(p svg.Paint) string {
	switch {
	case p.Current && p.Opacity < 1:
		return fmt.Sprintf("current@%g", p.Opacity)
	case p.Current:
		return "current"
	case p.None():
		return "none"
	}
	r, g, b, a, _ := p.Color.RGBA()
	return fmt.Sprintf("#%02x%02x%02x%02x", r, g, b, a)
}

func parse(t *testing.T, markup string) *svg.Icon {
	t.Helper()
	ic, err := svg.Parse([]byte(markup))
	if err != nil {
		t.Fatal(err)
	}
	return ic
}

func want(t *testing.T, ic *svg.Icon, w string) {
	t.Helper()
	if got := layers(ic); got != w {
		t.Fatalf("layers:\n%s\nwant:\n%s", got, w)
	}
}

func TestLucide(t *testing.T) {
	// Its three elements are stroked alike: one layer, one native view.
	ic := parse(t, lucideDownload)
	if ic.ViewBox != [4]float32{0, 0, 24, 24} || ic.Width != 24 || ic.Height != 24 {
		t.Fatalf("viewBox %v, size %gx%g", ic.ViewBox, ic.Width, ic.Height)
	}
	want(t, ic, fmt.Sprintf("fill=none stroke=current width=2 cap=%d join=%d: "+
		"M 21 15 L 21 19 C 21 20.105 20.105 21 19 21 L 5 21 C 3.895 21 3 20.105 3 19 L 3 15 "+
		"M 7 10 L 12 15 L 17 10 M 12 15 L 12 3\n", ui.CapRound, ui.JoinRound))

	ic = parse(t, lucideCircleCheck)
	want(t, ic, fmt.Sprintf("fill=none stroke=current width=2 cap=%d join=%d: "+
		"Ellipse 2 2 20 20 M 9 12 L 11 14 L 15 10\n", ui.CapRound, ui.JoinRound))
}

func TestMaterial(t *testing.T) {
	// The fill="none" box draws nothing; the icon is filled with the
	// icon's color, as SVG's default black fill.
	want(t, parse(t, materialHome),
		"fill=current stroke=none: M 10 20 L 10 14 L 14 14 L 14 20 L 19 20 L 19 12 L 22 12 L 12 3 L 2 12 L 5 12 L 5 20 Z\n")
	want(t, parse(t, materialFavorite),
		"fill=current stroke=none: M 12 21.35 L 10.55 20.03 C 5.4 15.36 2 12.28 2 8.5 C 2 5.42 4.42 3 7.5 3 "+
			"C 9.24 3 10.91 3.81 12 5.09 C 13.09 3.81 14.76 3 16.5 3 C 19.58 3 22 5.42 22 8.5 "+
			"C 22 12.28 18.6 15.36 13.45 20.04 L 12 21.35 Z\n")
}

// path parses path data in a 24 viewBox and returns its layer's path.
func path(t *testing.T, d string) string {
	t.Helper()
	ic := parse(t, `<svg viewBox="0 0 24 24"><path d="`+d+`"/></svg>`)
	if len(ic.Layers) != 1 {
		t.Fatalf("%q: %d layers", d, len(ic.Layers))
	}
	return ic.Layers[0].Path.String()
}

func TestPathData(t *testing.T) {
	for _, c := range []struct{ d, want string }{
		{"M1.5.5L1e-3-2", "M 1.5 0.5 L 0.001 -2"},
		{"M0 0 1 1 2 2", "M 0 0 L 1 1 L 2 2"},
		{"m1 1 1 1", "M 1 1 L 2 2"},
		{"M1 1h2v2z m1 0 h1", "M 1 1 L 3 1 L 3 3 Z M 2 1 L 3 1"},
		{"M1 1H3V3H1Z", "M 1 1 L 3 1 L 3 3 L 1 3 Z"},
		{"M0 0C0 1 1 1 1 0S2-1 2 0", "M 0 0 C 0 1 1 1 1 0 C 1 -1 2 -1 2 0"},
		{"M0 0c0 1 1 1 1 0s1-1 1 0", "M 0 0 C 0 1 1 1 1 0 C 1 -1 2 -1 2 0"},
		{"M0 0S1 1 2 0", "M 0 0 C 0 0 1 1 2 0"}, // no C before: the first control is the point
		{"M0 0Q1 1 2 0T4 0", "M 0 0 Q 1 1 2 0 Q 3 -1 4 0"},
		{"M0 0q1 1 2 0t2 0 2 0", "M 0 0 Q 1 1 2 0 Q 3 -1 4 0 Q 5 1 6 0"},
		{"M0 0L1 1L2", "M 0 0 L 1 1"}, // drawn up to the error
		{"L1 1", ""},
		{"M0 0A0 1 0 0 1 2 2", "M 0 0 L 2 2"}, // a zero radius: a line
	} {
		ic := parse(t, `<svg viewBox="0 0 24 24"><path d="`+c.d+`"/></svg>`)
		got := ""
		if len(ic.Layers) == 1 {
			got = ic.Layers[0].Path.String()
		}
		if got != c.want {
			t.Errorf("%q: %q, want %q", c.d, got, c.want)
		}
	}
}

func TestArcs(t *testing.T) {
	// Three quarters of the circle of radius 10 at 10, 10, clockwise from
	// its top: a curve per quarter, each ending on the circle.
	if got := path(t, "M10 0A10 10 0 1 1 0 10"); got !=
		"M 10 0 C 15.523 0 20 4.477 20 10 C 20 15.523 15.523 20 10 20 C 4.477 20 0 15.523 0 10" {
		t.Fatal(got)
	}
	// The small arc between the same points, the other way: around 0, 0.
	if got := path(t, "M10 0A10 10 0 0 1 0 10"); got != "M 10 0 C 10 5.523 5.523 10 0 10" {
		t.Fatal(got)
	}
	// Flags without separators; the end relative.
	got := path(t, "M0 0a5 5 0 105 5")
	if !strings.HasSuffix(got, " 5 5") || strings.Count(got, "C") != 3 {
		t.Fatal(got)
	}
	// Radii too small to reach are scaled up: a half circle.
	if got := path(t, "M0 0A1 1 0 0 0 4 0"); got != "M 0 0 C 0 1.105 0.895 2 2 2 C 3.105 2 4 1.105 4 0" {
		t.Fatal(got)
	}
	// A rotated ellipse's arc ends where it's asked to.
	got = path(t, "M2 3A6 3 30 0 1 9 7")
	if !strings.HasSuffix(got, " 9 7") {
		t.Fatal(got)
	}
}

func TestShapes(t *testing.T) {
	ic := parse(t, `<svg viewBox="0 0 24 24">
		<rect x="1" y="2" width="10" height="4"/>
		<rect x="1" y="2" width="10" height="4" rx="1"/>
		<rect x="1" y="2" width="10" height="4" ry="9"/>
		<rect x="1" y="2" width="10" height="4" ry="1.5"/>
		<rect x="0" y="0" width="4" height="2" rx="1" ry="0.5"/>
		<rect width="0" height="4"/>
		<ellipse cx="5" cy="5" rx="3" ry="2"/>
		<circle r="0"/>
		<polygon points="0,0 4,0 2,3"/>
		<polyline points="0 0 4 0 2" fill="none" stroke="red"/>
	</svg>`)
	want(t, ic, "fill=current stroke=none: Rect 1 2 10 4\n"+
		"fill=current stroke=none: RoundedRect 1 2 10 4 1\n"+
		"fill=current stroke=none: M 6 2 C 8.761 2 11 2.895 11 4 C 11 5.105 8.761 6 6 6 C 3.239 6 1 5.105 1 4 C 1 2.895 3.239 2 6 2 Z\n"+ // each radius clamped to half its side
		"fill=current stroke=none: RoundedRect 1 2 10 4 1.5\n"+
		"fill=current stroke=none: M 1 0 L 3 0 C 3.552 0 4 0.224 4 0.5 L 4 1.5 C 4 1.776 3.552 2 3 2 L 1 2 C 0.448 2 0 1.776 0 1.5 L 0 0.5 C 0 0.224 0.448 0 1 0 Z\n"+
		"fill=current stroke=none: Ellipse 2 3 6 4\n"+
		"fill=current stroke=none: M 0 0 L 4 0 L 2 3 Z\n"+
		"fill=none stroke=#ff0000ff width=1 cap=0 join=0: M 0 0 L 4 0\n")
}

func TestTransforms(t *testing.T) {
	ic := parse(t, `<svg viewBox="0 0 10 10">
		<g transform="translate(5 5)"><rect x="-1" y="-1" width="2" height="2" transform="rotate(90)"/></g>
		<circle cx="1" cy="1" r="1" transform="scale(2)"/>
		<path d="M0 0L1 0" transform="matrix(1 0 0 1 3 4)"/>
		<path d="M0 1L0 2" transform="skewX(45)"/>
		<path d="M2 1L3 1" transform="rotate(180 2 2)"/>
		<path d="M0 0L1 0" transform="translate(1,0) scale(2, 3)" fill="none" stroke="#000"/>
		<path d="M0 0L0 1" transform="skewY(45)"/>
	</svg>`)
	want(t, ic, "fill=current stroke=none: M 6 4 L 6 6 L 4 6 L 4 4 Z\n"+
		"fill=current stroke=none: Ellipse 0 0 4 4\n"+
		"fill=current stroke=none: M 3 4 L 4 4\n"+
		"fill=current stroke=none: M 1 1 L 2 2\n"+
		"fill=current stroke=none: M 2 3 L 1 3\n"+
		fmt.Sprintf("fill=none stroke=#000000ff width=%g cap=0 join=0: M 1 0 L 3 0\n", float32(math.Sqrt(6)))+
		"fill=current stroke=none: M 0 0 L 0 1\n")

	// A circle, an arc and a rounded rect under a rotation are curves.
	ic = parse(t, `<svg viewBox="0 0 10 10">
		<circle r="1" transform="rotate(90)"/>
		<path d="M0 0A1 1 0 0 1 2 0" transform="scale(2)"/>
		<rect width="4" height="4" rx="1" transform="rotate(45)"/>
	</svg>`)
	circle := ic.Layers[0].Path.String()
	if !strings.HasPrefix(circle, "M 0 1 C") || strings.Count(circle, "C") != 4 {
		t.Fatal(circle)
	}
	if arc := ic.Layers[1].Path.String(); arc != "M 0 0 C 0 -1.105 0.895 -2 2 -2 C 3.105 -2 4 -1.105 4 0" {
		t.Fatal(arc)
	}
	if rr := ic.Layers[2].Path.String(); strings.Count(rr, "C") != 4 || strings.Contains(rr, "Rect") {
		t.Fatal(rr)
	}
}

func TestPaint(t *testing.T) {
	ic := parse(t, `<svg viewBox="0 0 24 24">
		<rect width="1" height="1" fill="#f00"/>
		<rect width="1" height="1" fill="#00ff0080"/>
		<rect width="1" height="1" fill="#0f08"/>
		<rect width="1" height="1" style="fill: rgb(0, 0, 255); fill-opacity: .5"/>
		<rect width="1" height="1" fill="rgba(100%, 0%, 0%, 0.25)"/>
		<rect width="1" height="1" fill="white" opacity="0.5"/>
		<rect width="1" height="1" fill="currentColor" fill-opacity="50%"/>
		<rect width="1" height="1" fill="url(#gradient)"/>
		<rect width="1" height="1" fill="url(#gradient) navy"/>
		<rect width="1" height="1" fill="bogus"/>
		<rect width="1" height="1" fill="none"/>
		<rect width="1" height="1" fill="none" stroke="#123" stroke-width="0"/>
		<g color="#00f" stroke="currentColor" stroke-width="3" stroke-linejoin="bevel" opacity="0.5">
			<g style="stroke-linecap:square" fill="none"><line x1="0" y1="0" x2="1" y2="1"/></g>
		</g>
		<rect width="1" height="1" display="none"/>
		<rect width="1" height="1" visibility="hidden"/>
	</svg>`)
	want(t, ic, "fill=#ff0000ff stroke=none: Rect 0 0 1 1\n"+
		"fill=#00ff0080 stroke=none: Rect 0 0 1 1\n"+
		"fill=#00ff0088 stroke=none: Rect 0 0 1 1\n"+
		"fill=#0000ff80 stroke=none: Rect 0 0 1 1\n"+
		"fill=#ff000040 stroke=none: Rect 0 0 1 1\n"+
		"fill=#ffffff80 stroke=none: Rect 0 0 1 1\n"+
		"fill=current@0.5 stroke=none: Rect 0 0 1 1\n"+
		"fill=current stroke=none: Rect 0 0 1 1\n"+ // a gradient: the icon's color
		"fill=#000080ff stroke=none: Rect 0 0 1 1\n"+ // its fallback
		"fill=current stroke=none: Rect 0 0 1 1\n"+ // unknown: inherited
		fmt.Sprintf("fill=none stroke=#0000ff80 width=3 cap=%d join=%d: M 0 0 L 1 1\n", ui.CapSquare, ui.JoinBevel))
}

func TestSkipped(t *testing.T) {
	ic := parse(t, `<?xml version="1.0" encoding="UTF-8"?>
	<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">
	<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink"
		xmlns:inkscape="http://www.inkscape.org/namespaces/inkscape" viewBox="0 0 24 24">
		<title>t</title>
		<defs>
			<linearGradient id="g"><stop offset="0" stop-color="#fff"/></linearGradient>
			<path id="p" d="M0 0L1 1"/>
			<filter id="f"><feGaussianBlur stdDeviation="2"/></filter>
		</defs>
		<inkscape:thing><path d="M5 5L6 6"/></inkscape:thing>
		<text x="0" y="10">Hi</text>
		<symbol id="s"><path d="M2 2L3 3"/></symbol>
		<use href="#p" x="2" y="3"/>
		<use xlink:href="#s" transform="translate(1 1)"/>
		<use href="#missing"/>
		<use href="#loop" id="loop"/>
		<path d="M0 0L1 0" fill-rule="evenodd" filter="url(#f)" clip-path="url(#c)"/>
	</svg>`)
	want(t, ic, "fill=current stroke=none: M 2 3 L 3 4\n"+
		"fill=current stroke=none: M 3 3 L 4 4\n"+
		"fill=current stroke=none: M 0 0 L 1 0\n")
}

func TestViewBox(t *testing.T) {
	ic := parse(t, `<svg width="48px" height="32"><rect width="48" height="32"/></svg>`)
	if ic.ViewBox != [4]float32{0, 0, 48, 32} || ic.Width != 48 || ic.Height != 32 {
		t.Fatalf("%v %gx%g", ic.ViewBox, ic.Width, ic.Height)
	}
	ic = parse(t, `<svg viewBox="-2 -2 28 28" width="56"><path d="M0 0L1 1"/></svg>`)
	if ic.Width != 56 || ic.Height != 56 {
		t.Fatalf("%gx%g", ic.Width, ic.Height)
	}
	ic = parse(t, `<svg><path d="M2 3L12 8"/></svg>`) // the drawing's bounds
	if ic.ViewBox != [4]float32{2, 3, 10, 5} {
		t.Fatal(ic.ViewBox)
	}
	ic = parse(t, lucideDownload)
	for _, c := range []struct{ w, h, s, dx, dy float32 }{
		{24, 24, 1, 0, 0},
		{48, 48, 2, 0, 0},
		{48, 24, 1, 12, 0},
		{24, 48, 1, 0, 12},
	} {
		if s, dx, dy := ic.Fit(c.w, c.h); s != c.s || dx != c.dx || dy != c.dy {
			t.Errorf("Fit(%g, %g) = %g, %g, %g", c.w, c.h, s, dx, dy)
		}
	}
	ic = parse(t, `<svg viewBox="-2 -2 28 28"/>`)
	if s, dx, dy := ic.Fit(56, 56); s != 2 || dx != 4 || dy != 4 {
		t.Fatal(s, dx, dy)
	}

	for _, bad := range []string{``, `<svg`, `<html/>`, `<svg/>`} {
		if _, err := svg.Parse([]byte(bad)); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("MustParse didn't panic")
		}
	}()
	svg.MustParse(`<html/>`)
}

func mount(t *testing.T, root func() ui.Node) (*fake.Host, *ui.Root) {
	t.Helper()
	h := fake.New()
	r := ui.Mount(h, root)
	t.Cleanup(r.Dispose)
	return h, r
}

// shapePath decodes a Shape's path, as Path.String writes one.
func shapePath(v *fake.View) string {
	b := v.Props[ops.PropShapePath].Bytes
	var out []string
	for len(b) > 0 {
		op := ops.PathOp(b[0])
		b = b[1:]
		name, n := map[ops.PathOp]string{
			ops.PathOpMove: "M", ops.PathOpLine: "L", ops.PathOpQuad: "Q", ops.PathOpCubic: "C",
			ops.PathOpClose: "Z", ops.PathOpRect: "Rect", ops.PathOpRoundedRect: "RoundedRect", ops.PathOpEllipse: "Ellipse",
		}[op], map[ops.PathOp]int{
			ops.PathOpMove: 2, ops.PathOpLine: 2, ops.PathOpQuad: 4, ops.PathOpCubic: 6,
			ops.PathOpRect: 4, ops.PathOpRoundedRect: 5, ops.PathOpEllipse: 4,
		}[op]
		out = append(out, name)
		for range n {
			f := math.Float32frombits(binary.LittleEndian.Uint32(b))
			b = b[4:]
			out = append(out, fmt.Sprint(math.Round(float64(f)*1000)/1000))
		}
	}
	return strings.Join(out, " ")
}

func TestView(t *testing.T) {
	download := svg.MustParse(lucideDownload)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			svg.View(svg.Props{Icon: download, Style: ui.Style{Width: ui.Pt(24), Height: ui.Pt(24)}}),
			svg.View(svg.Props{Icon: download, Label: "Download", Color: ui.Val(ui.Accent),
				Style: ui.Style{Width: ui.Pt(48), Height: ui.Pt(48)}}),
		)
	})
	shapes := h.OfKind(ops.KindShape)
	if len(shapes) != 2 {
		t.Fatalf("%d shapes:\n%s", len(shapes), h.Root().Tree())
	}
	small, big := shapes[0], shapes[1]
	if small.Parent.Frame != [4]float32{0, 0, 24, 24} || small.Frame != [4]float32{0, 0, 24, 24} ||
		big.Frame != [4]float32{0, 0, 48, 48} {
		t.Fatalf("frames:\n%s", h.Root().Tree())
	}
	if !small.Parent.Bool(ops.PropA11yHidden) || small.Parent.String(ops.PropA11yLabel) != "" {
		t.Fatal("an icon without a label isn't decorative")
	}
	if big.Parent.String(ops.PropA11yLabel) != "Download" || big.Parent.Int(ops.PropA11yRole) != int64(ui.RoleImage) ||
		big.Parent.Bool(ops.PropA11yHidden) {
		t.Fatal("the labeled icon isn't an image to screen readers")
	}
	for _, c := range []struct {
		v     *fake.View
		color ui.Color
		width float64
	}{{small, ui.Label, 2}, {big, ui.Accent, 4}} {
		if _, ok := c.v.Props[ops.PropShapeFill]; ok {
			t.Error("a fill=none layer is filled")
		}
		if ui.Color(c.v.Int(ops.PropShapeStroke)) != c.color || c.v.Float(ops.PropShapeLineWidth) != c.width ||
			ops.LineCap(c.v.Int(ops.PropShapeLineCap)) != ui.CapRound || ops.LineJoin(c.v.Int(ops.PropShapeLineJoin)) != ui.JoinRound {
			t.Errorf("stroke %v, width %g, cap %d, join %d", c.v.Props[ops.PropShapeStroke], c.v.Float(ops.PropShapeLineWidth),
				c.v.Int(ops.PropShapeLineCap), c.v.Int(ops.PropShapeLineJoin))
		}
	}
	if got := shapePath(small); got != download.Layers[0].Path.String() {
		t.Fatalf("24: %s", got)
	}
	// At 48, every coordinate doubles.
	if got := shapePath(big); got != "M 42 30 L 42 38 C 42 40.209 40.209 42 38 42 L 10 42 C 7.791 42 6 40.209 6 38 L 6 30 "+
		"M 14 20 L 24 30 L 34 20 M 24 30 L 24 6" {
		t.Fatalf("48: %s", got)
	}
}

func TestViewSize(t *testing.T) {
	ic := svg.MustParse(`<svg viewBox="0 0 24 12" width="24" height="12"><path d="M0 0L24 12" stroke="#000" stroke-width="2"/></svg>`)
	big := signal.New(false)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Align: ui.AlignStart},
			svg.View(svg.Props{Icon: ic}),                                     // its own size
			svg.View(svg.Props{Icon: ic, Style: ui.Style{Height: ui.Pt(24)}}), // the width from the aspect ratio
			// A size known once it's laid out: the stroke follows it.
			svg.View(svg.Props{Icon: ic, Style: ui.StyleFunc(func() ui.Style {
				if big.Get() {
					return ui.Style{Width: ui.Pt(96), Height: ui.Pt(48)}
				}
				return ui.Style{Width: ui.Pt(72), Height: ui.Pt(72)}
			})}),
		)
	})
	shapes := h.OfKind(ops.KindShape)
	if len(shapes) != 3 {
		t.Fatalf("%d shapes:\n%s", len(shapes), h.Root().Tree())
	}
	for i, c := range []struct {
		frame [4]float32
		width float64
		path  string
	}{
		{[4]float32{0, 0, 24, 12}, 2, "M 0 0 L 24 12"},
		{[4]float32{0, 0, 48, 24}, 4, "M 0 0 L 48 24"},
		{[4]float32{0, 0, 72, 72}, 6, "M 0 18 L 72 54"},
	} {
		s := shapes[i]
		if s.Frame != c.frame || s.Float(ops.PropShapeLineWidth) != c.width || shapePath(s) != c.path {
			t.Errorf("%d: frame %v, width %g, path %s", i, s.Frame, s.Float(ops.PropShapeLineWidth), shapePath(s))
		}
	}
	big.Set(true)
	r.Flush()
	s := h.OfKind(ops.KindShape)[2]
	if s.Float(ops.PropShapeLineWidth) != 8 || shapePath(s) != "M 0 0 L 96 48" {
		t.Fatalf("width %g, path %s", s.Float(ops.PropShapeLineWidth), shapePath(s))
	}
}

func TestViewColor(t *testing.T) {
	ic := svg.MustParse(`<svg viewBox="0 0 24 24">
		<circle cx="12" cy="12" r="10" fill="#ff0000"/>
		<path d="M6 12h12" fill="none" stroke="currentColor" stroke-width="2"/>
		<rect x="8" y="8" width="8" height="8" fill="currentColor" fill-opacity="0.5"/>
	</svg>`)
	color := signal.New(ui.RGB(0, 0, 255))
	h, r := mount(t, func() ui.Node {
		return svg.View(svg.Props{Icon: ic, Color: color, Style: ui.Style{Width: ui.Pt(24), Height: ui.Pt(24)}})
	})
	shapes := h.OfKind(ops.KindShape)
	if len(shapes) != 3 {
		t.Fatalf("%d shapes", len(shapes))
	}
	red := shapes[0]
	if ui.Color(red.Int(ops.PropShapeFill)) != ui.RGB(255, 0, 0) || ui.Color(shapes[1].Int(ops.PropShapeStroke)) != ui.RGB(0, 0, 255) ||
		ui.Color(shapes[2].Int(ops.PropShapeFill)) != ui.RGBA(0, 0, 255, 128) {
		t.Fatalf("colors: %v %v %v", red.Props[ops.PropShapeFill], shapes[1].Props[ops.PropShapeStroke], shapes[2].Props[ops.PropShapeFill])
	}

	// The two layers painted with the icon's color are made again with
	// the new one; the red circle isn't touched.
	h.Reset()
	color.Set(ui.RGB(0, 128, 0))
	r.Flush()
	if h.Count(ops.OpCreate) != 2 || h.Count(ops.OpDestroy) != 2 {
		t.Fatalf("%d creates, %d destroys:\n%s", h.Count(ops.OpCreate), h.Count(ops.OpDestroy), h.Log())
	}
	for _, op := range h.Ops {
		if op.ID == red.ID {
			t.Fatalf("the red circle changed:\n%s", h.Log())
		}
	}
	shapes = h.OfKind(ops.KindShape)
	if len(shapes) != 3 || shapes[0] != red || ui.Color(shapes[1].Int(ops.PropShapeStroke)) != ui.RGB(0, 128, 0) ||
		ui.Color(shapes[2].Int(ops.PropShapeFill)) != ui.RGBA(0, 128, 0, 128) || shapes[1].Float(ops.PropShapeLineWidth) != 2 ||
		shapePath(shapes[1]) != "M 6 12 L 18 12" {
		t.Fatalf("after:\n%s", h.Log())
	}

	// A semantic color can't take the layer's opacity: the view does.
	color.Set(ui.Accent)
	r.Flush()
	shapes = h.OfKind(ops.KindShape)
	if ui.Color(shapes[2].Int(ops.PropShapeFill)) != ui.Accent || shapes[2].Float(ops.PropOpacity) != 0.5 {
		t.Fatalf("semantic: %v %v", shapes[2].Props[ops.PropShapeFill], shapes[2].Props[ops.PropOpacity])
	}
}

func TestViewStatic(t *testing.T) {
	// A static color and size make no effects of the icon's own: after
	// mounting, nothing changes.
	ic := svg.MustParse(materialHome)
	h, r := mount(t, func() ui.Node {
		return svg.View(svg.Props{Icon: ic, Color: ui.Val(ui.Red), Style: ui.Style{Width: ui.Pt(24), Height: ui.Pt(24)}})
	})
	s := h.One(ops.KindShape, "")
	if ui.Color(s.Int(ops.PropShapeFill)) != ui.Red {
		t.Fatal(s.Props)
	}
	if _, ok := s.Props[ops.PropShapeStroke]; ok {
		t.Fatal("stroked")
	}
	h.Reset()
	r.Flush()
	if len(h.Ops) != 0 {
		t.Fatalf("a flush with nothing changed:\n%s", h.Log())
	}
}
