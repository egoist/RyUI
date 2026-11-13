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

func (h *Host) paintCanvas(n *node, g Graphics) {
	b := n.canvas
	k := h.scaleOf(n)
	i := 0
	alpha := float32(1)
	var saved []uint32
	var alphas []float32
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
		return p
	}
	for i < len(b) {
		op := ops.CanvasOp(b[i])
		i++
		switch op {
		case ops.CanvasOpSave:
			saved = append(saved, g.Save())
			alphas = append(alphas, alpha)
		case ops.CanvasOpRestore:
			if len(saved) > 0 {
				g.Restore(saved[len(saved)-1])
				saved = saved[:len(saved)-1]
				alpha = alphas[len(alphas)-1]
				alphas = alphas[:len(alphas)-1]
			}
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
			w := f()
			if !ok(2) {
				return
			}
			capv, join := int(b[i]), int(b[i+1])
			i += 2
			nd := u32()
			phase := f()
			var dash []float32
			for range nd {
				dash = append(dash, f()/max(w, 0.01)) // GDI+'s dashes are in pen widths
			}
			p := path()
			pen := NewPen(c, w*k)
			caps := [...]int{0, 2, 1}  // butt, round, square: GDI+'s flat, round, square
			joins := [...]int{0, 2, 1} // miter, round, bevel
			pen.SetCaps(caps[min(capv, 2)], joins[min(join, 2)])
			if len(dash) > 0 {
				pen.SetDash(dash, phase/max(w, 0.01))
			}
			g.DrawPath(pen, p)
			pen.Close()
			p.Close()
		case ops.CanvasOpGradient:
			c := u32()
			colors := make([]ARGB, 0, c)
			stops := make([]float32, 0, c)
			for range c {
				colors = append(colors, color())
				stops = append(stops, f())
			}
			x0, y0, x1, y1 := f()*k, f()*k, f()*k, f()*k
			p := path()
			if len(colors) > 0 {
				br := LinearBrush(x0, y0, x1, y1, colors, stops)
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
