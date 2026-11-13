package conformance

import (
	"math"

	"github.com/egoist/asuka/ui"
)

// Case is one rendering check: a scene in a Cell-point square, on
// white, and what's measured on its pixels.
type Case struct {
	Name  string
	Needs []Feature
	// Scene is the case's views, placed from the cell's top left.
	Scene func() ui.Node
	Check func(p *Probe)
}

var (
	cRed    = ui.RGB(255, 0, 0)
	cBlue   = ui.RGB(0, 0, 255)
	cYellow = ui.RGB(255, 255, 0)
	cBlack  = ui.RGB(0, 0, 0)
	yellow  = RGB{255, 255, 0}
)

// box is a view at (x, y), w by h points, in its parent.
func box(x, y, w, h float32, s ui.Style, kids ...ui.Node) ui.Node {
	s.Absolute = true
	s.Left, s.Top, s.Width, s.Height = ui.Pt(x), ui.Pt(y), ui.Pt(w), ui.Pt(h)
	return ui.View(ui.ViewProps{Style: s}, kids...)
}

// edges checks a red rectangle's four edges on white, through its
// middle.
func edges(p *Probe, what string, x0, y0, x1, y1 float64) {
	mx, my := (x0+x1)/2, (y0+y1)/2
	p.Edge(what+"'s left edge", 0, my, mx, my, white, red, x0)
	p.Edge(what+"'s right edge", Cell-1, my, mx, my, white, red, x1)
	p.Edge(what+"'s top edge", mx, 0, mx, my, white, red, y0)
	p.Edge(what+"'s bottom edge", mx, Cell-1, mx, my, white, red, y1)
}

// shadow checks a black shadow's blur, measured from (x, y) in its solid
// middle out along a line to (x1, y1): that its edge is at want and its σ
// is the Radius's half.
func shadow(p *Probe, what string, x, y, x1, y1, want, radius float64) {
	e, sigma, plateau, ok := p.Blur(x, y, x1, y1)
	if !ok {
		p.Errorf("%s: no shadow from (%g,%g) to (%g,%g): darkness %.2f", what, x, y, x1, y1, plateau)
		return
	}
	p.Logf("%s: edge %.2f, σ %.2f, darkness %.2f", what, e, sigma, plateau)
	if plateau < 0.8 {
		p.Errorf("%s: an opaque black shadow is %.2f dark in its middle, want at least 0.8", what, plateau)
	}
	if math.Abs(e-want) > 2*p.Px()+0.25 {
		p.Errorf("%s: its edge is at %.2f, want %g", what, e, want)
	}
	// A Gaussian's σ, to a pixel and a fifth (shadows are approximated).
	if want := radius / 2; math.Abs(sigma-want) > want/5+p.Px() {
		p.Errorf("%s: σ %.2f, want %.2f (Radius %g)", what, sigma, want, radius)
	}
}

// shadowed is how dark a black shadow is a point outside its view's
// edge, at least: a Gaussian of σ 4 (Radius 8) is 0.4 dark there.
// Android's elevation shadows are lighter by design.
func shadowed(p *Probe) float64 {
	if p.Lacks(ShadowBlur) {
		return 0.05
	}
	return 0.15
}

// Cases are the checks, in the sheet's order.
var Cases = []Case{
	{
		Name:  "background",
		Scene: func() ui.Node { return box(20, 30, 60, 40, ui.Style{Background: cRed}) },
		Check: func(p *Probe) { edges(p, "the box", 20, 30, 80, 70) },
	},
	{
		// The core places frames on the device's pixels, so an edge set
		// between them lands on one, crisp: no column half covered.
		Name:  "pixel_aligned",
		Scene: func() ui.Node { return box(20.3, 30.3, 59.4, 39.4, ui.Style{Background: cRed}) },
		Check: func(p *Probe) {
			cov := func(c RGB) float64 { return c.Mix(white, red) }
			for _, line := range [][4]float64{{0, 50, Cell - 1, 50}, {50, 0, 50, Cell - 1}} {
				for _, s := range p.Profile(line[0], line[1], line[2], line[3], cov) {
					if s.v > 0.1 && s.v < 0.9 {
						p.Errorf("the pixel at %.2f from (%g,%g) is %.2f covered: an edge between pixels", s.at, line[0], line[1], s.v)
						break
					}
				}
			}
			edges(p, "the box", 20.3, 30.3, 79.7, 69.7)
		},
	},
	{
		Name:  "corner_radius",
		Scene: func() ui.Node { return box(10, 10, 80, 80, ui.Style{Background: cRed, CornerRadius: 20}) },
		Check: func(p *Probe) {
			edges(p, "the box", 10, 10, 90, 90)
			p.Mix("the corner", 11.5, 11.5, white, red, 0, 0.1)
			p.Mix("the top edge's middle", 50, 11, white, red, 1, 0.1)
			// Along the diagonal, a circular corner's edge is r(1-1/√2) =
			// 5.86 in from the box's corner; iOS's continuous corner a
			// little further in.
			d := diagonal(p, 10, 10)
			p.Logf("the corner's edge %.2f in", d)
			if d < 4.5 || d > 8 {
				p.Errorf("the corner's edge is %.2f in along the diagonal, want 5.86 (circular) to 8", d)
			}
		},
	},
	{
		Name: "border",
		Scene: func() ui.Node {
			return box(10, 10, 80, 80, ui.Style{Background: cYellow, BorderWidth: 6, BorderColor: cBlue})
		},
		Check: func(p *Probe) {
			p.Edge("the border's outside, left", 0, 50, 13, 50, white, blue, 10)
			p.Edge("the border's inside, left", 13, 50, 50, 50, blue, yellow, 16)
			p.Edge("the border's outside, right", Cell-1, 50, 87, 50, white, blue, 90)
			p.Edge("the border's inside, right", 87, 50, 50, 50, blue, yellow, 84)
			p.Edge("the border's inside, top", 50, 13, 50, 50, blue, yellow, 16)
		},
	},
	{
		Name:  "opacity",
		Scene: func() ui.Node { return box(20, 20, 60, 60, ui.Style{Background: cRed, Opacity: 0.5}) },
		Check: func(p *Probe) {
			p.Logf("the middle %.2f red", p.Mix("the middle", 50, 50, white, red, 0.5, 0.06))
		},
	},
	{
		// Two overlapping children of a half-transparent view: drawn as
		// one picture, then faded, the overlap is as light as the rest.
		Name:  "group_opacity",
		Needs: []Feature{GroupOpacity},
		Scene: func() ui.Node {
			return box(10, 10, 80, 80, ui.Style{Opacity: 0.5},
				box(0, 0, 50, 50, ui.Style{Background: cRed}),
				box(30, 30, 50, 50, ui.Style{Background: cRed}),
			)
		},
		Check: func(p *Probe) {
			a := p.Mix("one child", 25, 25, white, red, 0.5, 0.06)
			b := p.Mix("the overlap", 50, 50, white, red, 0.5, 0.06)
			p.Logf("one child %.2f red, the overlap %.2f", a, b)
		},
	},
	{
		// A child larger than its clipping parent shows only inside the
		// parent's rounded bounds.
		Name: "clip",
		Scene: func() ui.Node {
			return box(20, 20, 60, 60, ui.Style{Clip: true, CornerRadius: 16},
				box(-20, -20, 100, 100, ui.Style{Background: cRed}))
		},
		Check: func(p *Probe) {
			edges(p, "the clip", 20, 20, 80, 80)
			p.Mix("outside the clip", 15, 50, white, red, 0, 0.1)
			p.Mix("inside the clip", 50, 50, white, red, 1, 0.1)
			p.Mix("the clip's rounded corner", 21.5, 21.5, white, red, 0, 0.1)
		},
	},
	{
		// The shadow of a box, moved below it: its edges and blur.
		Name:  "shadow_blur",
		Needs: []Feature{ShadowBlur},
		Scene: func() ui.Node {
			return box(20, 8, 60, 30, ui.Style{Background: cRed, Shadow: ui.Shadow{Color: cBlack, Radius: 8, Y: 44}})
		},
		Check: func(p *Probe) {
			// The shadow is the box's shape at (20, 52), 60 by 30.
			shadow(p, "its bottom edge", 50, 67, 50, Cell-1, 82, 8)
			shadow(p, "its left edge", 50, 67, 0, 67, 20, 8)
		},
	},
	{
		// A view without a background casts its shadow from its shape (its
		// bounds, rounded), not from what's drawn in it.
		Name: "shadow_shape",
		Scene: func() ui.Node {
			return box(20, 10, 60, 40, ui.Style{Shadow: ui.Shadow{Color: cBlack, Radius: 8}},
				box(0, 0, 20, 20, ui.Style{Background: cRed}))
		},
		Check: func(p *Probe) {
			c := p.At(65, 51)
			p.Logf("below the view's bottom right, far from its child: %v", c)
			if d := c.Darkness(); d < shadowed(p) || !c.Gray() {
				p.Errorf("no shadow under the view's bottom edge, away from its child: %v (darkness %.2f)", c, d)
			}
		},
	},
	{
		// A view that clips its children still casts its shadow (SwiftUI's
		// .clipShape().shadow(), CSS's overflow: hidden and box-shadow).
		Name: "clip_shadow",
		Scene: func() ui.Node {
			return box(20, 20, 60, 60, ui.Style{Clip: true, CornerRadius: 12, Shadow: ui.Shadow{Color: cBlack, Radius: 8}},
				box(-20, -20, 100, 100, ui.Style{Background: cRed}))
		},
		Check: func(p *Probe) {
			p.Mix("inside the clip", 50, 50, white, red, 1, 0.1)
			sides := [][2]float64{{50, 81.5}, {81.5, 50}, {50, 18.5}}
			if p.Lacks(ShadowBlur) {
				sides = sides[:1] // a light above: the shadow falls below
			}
			for _, at := range sides {
				c := p.At(at[0], at[1])
				switch {
				case !c.Gray():
					p.Errorf("just outside the clip at (%g,%g): %v, the clipped child shows", at[0], at[1], c)
				case c.Darkness() < shadowed(p):
					p.Errorf("just outside the clip at (%g,%g): %v, no shadow", at[0], at[1], c)
				}
			}
			if c := p.At(21.5, 21.5); !c.Gray() {
				p.Errorf("the clip's rounded corner: %v, the clipped child shows", c)
			}
		},
	},
	{
		Name: "gradient",
		Scene: func() ui.Node {
			return box(10, 10, 80, 80, ui.Style{Gradient: ui.LinearGradient(cRed, cBlue)})
		},
		Check: func(p *Probe) { gradient(p, 50, 11, 50, 89) },
	},
	{
		Name: "gradient_across",
		Scene: func() ui.Node {
			return box(10, 10, 80, 80, ui.Style{Gradient: &ui.Gradient{Colors: []ui.Color{cRed, cBlue}, End: ui.Point{X: 1}}})
		},
		Check: func(p *Probe) { gradient(p, 11, 50, 89, 50) },
	},
	{
		Name:  "translate",
		Needs: []Feature{Transform},
		Scene: func() ui.Node {
			return box(10, 30, 40, 40, ui.Style{Background: cRed, Transform: ui.Transform{TranslateX: 30, TranslateY: 10}})
		},
		Check: func(p *Probe) { edges(p, "the moved box", 40, 40, 80, 80) },
	},
	{
		// Scaled about its center.
		Name:  "scale",
		Needs: []Feature{Transform},
		Scene: func() ui.Node {
			return box(20, 20, 60, 60, ui.Style{Background: cRed, Transform: ui.Transform{Scale: 0.5}})
		},
		Check: func(p *Probe) { edges(p, "the scaled box", 35, 35, 65, 65) },
	},
	{
		// Rotated 45° about its center: a diamond, its corners where the
		// box's sides' middles were, 35.4 points from its center.
		Name:  "rotate",
		Needs: []Feature{Transform},
		Scene: func() ui.Node {
			return box(25, 25, 50, 50, ui.Style{Background: cRed, Transform: ui.Transform{Rotate: 45}})
		},
		Check: func(p *Probe) {
			p.Mix("the middle", 50, 50, white, red, 1, 0.1)
			p.Mix("where the box's corner was", 28, 28, white, red, 0, 0.1)
			p.Edge("the diamond's top", 50, 0, 50, 50, white, red, 50-25*math.Sqrt2)
			p.Edge("the diamond's left", 0, 50, 50, 50, white, red, 50-25*math.Sqrt2)
		},
	},
}

// diagonal is how far in from a box's top left corner (x, y), along its
// diagonal, its rounded edge is: where the pixels turn half red.
func diagonal(p *Probe, x, y float64) float64 {
	k := p.Scale()
	prev, prevD := 0.0, 0.0
	for d := 0.0; d < 20; d += 0.5 / k {
		v := p.At(x+d, y+d).Mix(white, red)
		if v >= 0.5 && d > 0 {
			return prevD + (0.5-prev)/(v-prev)*(d-prevD)
		}
		prev, prevD = v, d
	}
	return math.Inf(1)
}

// gradient checks a red to blue gradient along the line from (x0, y0)
// to (x1, y1): red at its start, blue at its end, half way in the
// middle, and never back.
func gradient(p *Probe, x0, y0, x1, y1 float64) {
	s := p.Profile(x0, y0, x1, y1, func(c RGB) float64 { return c.Mix(red, blue) })
	if len(s) < 3 {
		p.Errorf("no gradient")
		return
	}
	first, last, mid := s[0].v, s[len(s)-1].v, s[len(s)/2].v
	p.Logf("from %.2f through %.2f to %.2f blue", first, mid, last)
	if first > 0.1 {
		p.Errorf("its start is %.2f blue, want red", first)
	}
	if last < 0.9 {
		p.Errorf("its end is %.2f blue, want blue", last)
	}
	if mid < 0.35 || mid > 0.65 {
		p.Errorf("its middle is %.2f blue, want half", mid)
	}
	for i := 1; i < len(s); i++ {
		if s[i].v < s[i-1].v-0.03 {
			p.Errorf("it turns back at %.2f: %.2f after %.2f", s[i].at, s[i].v, s[i-1].v)
			break
		}
	}
}
