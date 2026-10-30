// Package svg shows SVG icons with the platform's own vector drawing:
// Parse turns the markup into layers of paths (transforms applied, arcs
// made curves), and View draws each with a ui.Shape, scaled to its
// size. Nothing is rasterized.
//
// It reads what icon sets use (Lucide, Material, Heroicons, Feather):
// path, rect, circle, ellipse, line, polyline, polygon, g and use; fill,
// stroke and their widths, caps, joins and opacities, as attributes or
// in a style attribute; and transforms. It skips the rest: text,
// gradients (painted with the icon's color), clipping, masks, filters,
// and fill-rule evenodd (ui.Path fills by the nonzero rule).
//
//	//go:embed download.svg
//	var downloadSVG string
//	var download = svg.MustParse(downloadSVG)
//
//	svg.View(svg.Props{Icon: download, Color: ui.Val(ui.Accent), Style: ui.Style{Width: ui.Pt(24), Height: ui.Pt(24)}})
package svg

import (
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

// Icon is parsed SVG: its viewBox and what it draws.
type Icon struct {
	// ViewBox is the drawing's area: min x, min y, width, height (the
	// svg's viewBox; without one, its width and height).
	ViewBox [4]float32
	// Width and Height are its size in points when its Style gives none:
	// the svg's width and height, else the viewBox's.
	Width, Height float32
	// Layers are drawn in order, the first at the back.
	Layers []Layer
}

// Layer is an outline and how it's painted: filled, stroked or both.
type Layer struct {
	Path         Path
	Fill, Stroke Paint
	// LineWidth is the stroke's, in viewBox units.
	LineWidth float32
	LineCap   ui.LineCap
	LineJoin  ui.LineJoin
}

// Paint is how a fill or stroke is painted: not at all (the zero Paint),
// with a color, or with the icon's color (currentColor, and a fill the
// SVG doesn't give).
type Paint struct {
	// Color is the color, opacity folded into its alpha.
	Color ui.Color
	// Current paints with the icon's color, at Opacity (0 to 1).
	Current bool
	Opacity float32
}

// None reports whether p paints nothing.
func (p Paint) None() bool { return !p.Current && p.Color == 0 }

// opaque reports whether p paints without seeing through.
func (p Paint) opaque() bool {
	if p.Current {
		return p.Opacity >= 1
	}
	_, _, _, a, ok := p.Color.RGBA()
	return !ok || a == 255
}

// Fit is where the viewBox goes in a box of width w and height h: scaled
// by scale (the same both ways) and moved by dx, dy, centered (SVG's
// preserveAspectRatio xMidYMid meet). A point x, y of the viewBox is at
// x*scale+dx, y*scale+dy.
func (ic *Icon) Fit(w, h float32) (scale, dx, dy float32) {
	vx, vy, vw, vh := ic.ViewBox[0], ic.ViewBox[1], ic.ViewBox[2], ic.ViewBox[3]
	if vw <= 0 || vh <= 0 {
		return 1, 0, 0
	}
	scale = min(w/vw, h/vh)
	return scale, (w-vw*scale)/2 - vx*scale, (h-vh*scale)/2 - vy*scale
}

// Props configures a View.
type Props struct {
	Icon *Icon
	// Color paints what the SVG paints with currentColor, and fills it
	// doesn't give (nil: ui.Label, the text color).
	Color signal.Accessor[ui.Color]
	// Style sizes it; without a width or height, it's the Icon's Width
	// and Height, and with one of them, the other keeps its aspect ratio.
	Style ui.Styler
	// Label describes the icon to screen readers; without it the icon is
	// decorative.
	Label string
}

// View draws an icon: a view sized by its Style, holding a Shape per
// layer that fills it.
//
// ui.Shape's colors are set once, so a change of Color makes the shapes
// painted with it again (the others stay); a static Color makes no
// effect.
func View(p Props) ui.Node {
	ic := p.Icon
	if ic == nil {
		ic = &Icon{}
	}
	color, static := ui.Label, true
	if p.Color != nil {
		if c, ok := signal.StaticValue(p.Color); ok {
			color = c
		} else {
			static = false
		}
	}
	vp := ui.ViewProps{Style: p.Style, Label: p.Label}
	if p.Label != "" {
		vp.Role = ui.RoleImage
	} else {
		vp.Decorative = true
	}
	// A stroke's width scales with the icon, and ui.Shape's is fixed:
	// known from the Style when it gives the size in points, else once
	// the view is laid out.
	scale := float32(1)
	var size *signal.Signal[[2]float32]
	known := false
	if st, ok := p.Style.(ui.Style); ok || p.Style == nil {
		var w, h float32
		w, h, known = ic.size(&st)
		vp.Style = st
		if known {
			scale, _, _ = ic.Fit(w, h)
		}
	}
	if !known {
		size = signal.New([2]float32{})
		vp.OnLayout = func(f ui.Frame) { size.Set([2]float32{f.Width, f.Height}) }
	}
	kids := make([]ui.Node, 0, len(ic.Layers))
	for i := range ic.Layers {
		l := &ic.Layers[i]
		byColor := !static && (l.Fill.Current || l.Stroke.Current)
		bySize := size != nil && !l.Stroke.None()
		if !byColor && !bySize {
			kids = append(kids, ic.shape(l, color, scale))
			continue
		}
		kids = append(kids, ui.Dynamic(func() ui.Node {
			c, s := color, scale
			if byColor {
				c = p.Color.Get()
			}
			if bySize {
				sz := size.Get()
				if sz[0] <= 0 || sz[1] <= 0 {
					return ui.Node{}
				}
				s, _, _ = ic.Fit(sz[0], sz[1])
			}
			return ic.shape(l, c, s)
		}))
	}
	return ui.View(vp, kids...)
}

// size completes a Style's size from the icon's, and returns it when
// it's known in points.
func (ic *Icon) size(st *ui.Style) (w, h float32, known bool) {
	ratio := st.AspectRatio
	if ratio <= 0 && ic.ViewBox[3] > 0 {
		ratio = ic.ViewBox[2] / ic.ViewBox[3]
	}
	wa, ha := st.Width.IsAuto(), st.Height.IsAuto()
	switch {
	case wa && ha && st.AspectRatio <= 0:
		st.Width, st.Height = ui.Pt(ic.Width), ui.Pt(ic.Height)
	case wa != ha && st.AspectRatio <= 0 && ratio > 0:
		st.AspectRatio = ratio
	}
	pt := func(d ui.Dim) (float32, bool) { return d.V, d.U == layout.UnitPoint }
	w, okW := pt(st.Width)
	h, okH := pt(st.Height)
	switch {
	case okW && okH:
		return w, h, true
	case okW && ha && ratio > 0:
		return w, w / ratio, true
	case okH && wa && ratio > 0:
		return h * ratio, h, true
	}
	return 0, 0, false
}

var whole = ui.Style{Absolute: true, Left: ui.Pt(0), Top: ui.Pt(0), Width: ui.Pct(100), Height: ui.Pct(100)}

// shape draws a layer, painted with color where it says currentColor,
// its stroke scaled by scale.
func (ic *Icon) shape(l *Layer, color ui.Color, scale float32) ui.Node {
	if color == 0 {
		color = ui.Label
	}
	style := whole
	fc, fo := l.Fill.with(color)
	sc, so := l.Stroke.with(color)
	// What can't take an alpha (a semantic color) takes the view's
	// opacity: the fill's, if both need one.
	if op := min(fo, so); op < 1 {
		if fo < 1 {
			op = fo
		}
		style.Opacity = op
	}
	sp := ui.ShapeProps{
		Path: func(w, h float32) *ui.Path {
			s, dx, dy := ic.Fit(w, h)
			return l.Path.Append(new(ui.Path), s, dx, dy)
		},
		Fill:   fc,
		Stroke: sc,
		Style:  style,
	}
	if sc != 0 {
		sp.LineWidth = l.LineWidth * scale
		sp.LineCap, sp.LineJoin = l.LineCap, l.LineJoin
	}
	return ui.Shape(sp)
}

// with returns the color p paints with, given the icon's, and an
// opacity for the view when the alpha can't be folded into it.
func (p Paint) with(color ui.Color) (ui.Color, float32) {
	if !p.Current {
		return p.Color, 1
	}
	if p.Opacity >= 1 {
		return color, 1
	}
	if r, g, b, a, ok := color.RGBA(); ok {
		return ui.RGBA(r, g, b, uint8(float32(a)*p.Opacity+0.5)), 1
	}
	return color, p.Opacity
}
