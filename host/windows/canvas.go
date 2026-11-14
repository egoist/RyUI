//go:build windows

package windows

import (
	"encoding/binary"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// A Canvas's display list, replayed with GDI+ as its view paints, in
// pixels at its window's scale, semantic colors in the theme's colors.
// GDI+ has no layer opacity: Alpha multiplies the colors drawn after it.
// A radial gradient is a path gradient over its circle (its focus the
// path's center point), the last color past it.

func (h *Host) paintCanvas(n *node, g Graphics) {
	b := n.canvas
	k := h.scaleOf(n)
	i := 0
	alpha := float32(1)
	evenOdd := false
	var saved []uint32
	var alphas []float32
	var rules []bool
	ok := func(c int) bool { return i+c <= len(b) }
	f := func() float32 {
		if !ok(4) {
			i = len(b)
			return 0
		}
		v := f32(b, i)
		i += 4
		return v
	}
	u32 := func() int {
		if !ok(4) {
			i = len(b)
			return 0
		}
		v := int(binary.LittleEndian.Uint32(b[i:]))
		i += 4
		return v
	}
	color := func() ARGB {
		if !ok(8) {
			i = len(b)
			return 0
		}
		c, _ := h.resolve(ops.Color(binary.LittleEndian.Uint64(b[i:])))
		i += 8
		return c.withAlpha(alpha).argb()
	}
	path := func() Path {
		c := min(u32(), len(b)-i)
		p := buildPath(b[i:i+c], k)
		i += c
		p.SetFillMode(evenOdd)
		return p
	}
	stops := func() ([]ARGB, []float32) {
		c := u32()
		colors := make([]ARGB, 0, c)
		positions := make([]float32, 0, c)
		for range c {
			colors = append(colors, color())
			positions = append(positions, f())
		}
		return colors, positions
	}
	pen := func(c ARGB, br Brush) Pen {
		w := f()
		if !ok(2) {
			i = len(b)
			return 0
		}
		capv, join := int(b[i]), int(b[i+1])
		i += 2
		nd := u32()
		phase := f()
		var dash []float32
		for range nd {
			dash = append(dash, f()/max(w, 0.01)) // GDI+'s dashes are in pen widths
		}
		var p Pen
		if br != 0 {
			p = NewBrushPen(br, w*k)
		} else {
			p = NewPen(c, w*k)
		}
		caps := [...]int{0, 2, 1}  // butt, round, square: GDI+'s flat, round, square
		joins := [...]int{0, 2, 1} // miter, round, bevel
		p.SetCaps(caps[min(capv, 2)], joins[min(join, 2)])
		if len(dash) > 0 {
			p.SetDash(dash, phase/max(w, 0.01))
		}
		return p
	}
	for i < len(b) {
		op := ops.CanvasOp(b[i])
		i++
		switch op {
		case ops.CanvasOpSave:
			saved = append(saved, g.Save())
			alphas = append(alphas, alpha)
			rules = append(rules, evenOdd)
		case ops.CanvasOpRestore:
			if len(saved) > 0 {
				g.Restore(saved[len(saved)-1])
				saved = saved[:len(saved)-1]
				alpha = alphas[len(alphas)-1]
				alphas = alphas[:len(alphas)-1]
				evenOdd = rules[len(rules)-1]
				rules = rules[:len(rules)-1]
			}
		case ops.CanvasOpTransform:
			// In points; the world is in pixels: the translation scales.
			a, bb, c, d, tx, ty := f(), f(), f(), f(), f(), f()
			g.Transform(a, bb, c, d, tx*k, ty*k)
		case ops.CanvasOpFillRule:
			if ok(1) {
				evenOdd = b[i] == 1
				i++
			}
		case ops.CanvasOpRadialGradient:
			colors, positions := stops()
			cx, cy, r, fx, fy := f()*k, f()*k, f()*k, f()*k, f()*k
			p := path()
			g.RadialFill(p, cx, cy, r, fx, fy, colors, positions)
			p.Close()
		case ops.CanvasOpStrokeGradient:
			start := i
			// The pen's brush comes after its style: read past it first.
			f()
			i += 2
			nd := u32()
			i += 4 + 4*nd
			if !ok(1) {
				return
			}
			radial := b[i] == 1
			i++
			colors, positions := stops()
			var br Brush
			if radial {
				cx, cy, r, fx, fy := f()*k, f()*k, f()*k, f()*k, f()*k
				br = RadialBrush(cx, cy, r, fx, fy, colors, positions)
			} else {
				x0, y0, x1, y1 := f()*k, f()*k, f()*k, f()*k
				br = LinearBrush(x0, y0, x1, y1, colors, positions)
			}
			end := i
			i = start
			pn := pen(0, br)
			i = end
			p := path()
			g.DrawPath(pn, p)
			pn.Close()
			br.Close()
			p.Close()
		case ops.CanvasOpTranslate:
			x, y := f(), f()
			g.Translate(x*k, y*k)
		case ops.CanvasOpScale:
			x, y := f(), f()
			g.Scale(x, y)
		case ops.CanvasOpRotate:
			g.Rotate(f())
		case ops.CanvasOpAlpha:
			alpha *= f()
		case ops.CanvasOpClip:
			p := path()
			g.IntersectClip(p)
			p.Close()
		case ops.CanvasOpFill:
			c := color()
			p := path()
			br := SolidBrush(c)
			g.FillPath(br, p)
			br.Close()
			p.Close()
		case ops.CanvasOpStroke:
			c := color()
			pn := pen(c, 0)
			p := path()
			g.DrawPath(pn, p)
			pn.Close()
			p.Close()
		case ops.CanvasOpGradient:
			colors, positions := stops()
			x0, y0, x1, y1 := f()*k, f()*k, f()*k, f()*k
			p := path()
			if len(colors) > 0 {
				br := LinearBrush(x0, y0, x1, y1, colors, positions)
				g.FillPath(br, p)
				br.Close()
			}
			p.Close()
		case ops.CanvasOpText:
			c := color()
			size := f()
			if !ok(2) {
				return
			}
			weight, align := ops.FontWeight(b[i]), int(b[i+1])
			i += 2
			x, y := f(), f()
			l := min(u32(), len(b)-i)
			s := string(b[i : i+l])
			i += l
			if align > 2 {
				align = 0
			}
			g.DrawText(s, h.face(fontSpec{size: size}), size*k, weight >= ops.FontWeightSemibold, c, align, x*k, y*k)
		default:
			return
		}
	}
	for j := len(saved) - 1; j >= 0; j-- {
		g.Restore(saved[j])
	}
}
