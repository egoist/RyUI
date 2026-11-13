//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/asuka/internal/windows"
)

// Style.Transform. A child window can't be transformed, so the frame
// carries what it can: a translation moves the view's window, and a scale
// (about its center) sizes it and scales what's in it, the view's scale
// (k, pixels per point) multiplied by it for its tree: fonts, frames,
// controls and drawing at the scaled size, still windows that take
// clicks and the keyboard. A scale that differs between the axes sizes
// the window by each, and what's in it by their geometric mean. A
// rotation draws the view, and what's in it, turned, as a picture
// (picture.go).

// xform is a view's transform: a translation (points), a scale on each
// axis, and a rotation (degrees, clockwise).
type xform struct {
	tx, ty, sx, sy, rot float32
}

var identity = xform{sx: 1, sy: 1}

func readXform(b []byte) xform {
	if len(b) < 20 {
		return identity
	}
	t := xform{tx: f32(b, 0), ty: f32(b, 4), sx: f32(b, 8), sy: f32(b, 12), rot: f32(b, 16)}
	if t.sx == 0 && t.sy == 0 {
		t.sx, t.sy = 1, 1
	}
	return t
}

func lerpXform(a, b xform, e float32) xform {
	l := func(x, y float32) float32 { return x + (y-x)*e }
	return xform{l(a.tx, b.tx), l(a.ty, b.ty), l(a.sx, b.sx), l(a.sy, b.sy), l(a.rot, b.rot)}
}

// zoomOf is the scale of what's in a view of transform t: its scale's,
// the geometric mean of the axes' (a flip is drawn unflipped).
func zoomOf(t xform) float32 {
	sx, sy := float32(math.Abs(float64(t.sx))), float32(math.Abs(float64(t.sy)))
	z := sx
	if sx != sy {
		z = float32(math.Sqrt(float64(sx * sy)))
	}
	return max(z, 0.01)
}

// own is the scale n's transform gives what's in it (1: none).
func (n *node) own() float32 {
	if n.zoom == 0 {
		return 1
	}
	return n.zoom
}

// kFor is the k of a view whose own scale is z in a view of k base (0:
// the main window's).
func (h *Host) kFor(base, z float32) float32 {
	if z == 1 {
		return base
	}
	if base == 0 {
		base = h.scale
	}
	return base * z
}

// setTransform follows Style.Transform: during an animation, it steps
// there with the frames.
func (h *Host) setTransform(n *node, b []byte) {
	to := readXform(b)
	if a := h.anim; a != nil && a.active && n.hwnd != 0 && n.placed && IsWindowVisible(n.hwnd) && to != n.xf {
		tw := a.tweens[n]
		if tw == nil {
			tw = &tween{from: n.frame, to: n.frame}
			a.tweens[n] = tw
		}
		tw.xf, tw.xfrom, tw.xto = true, n.xf, to
		return
	}
	h.applyXform(n, to)
}

// applyXform gives n transform t: its tree's scale, its window's place.
func (h *Host) applyXform(n *node, t xform) {
	if n.xf == (xform{}) {
		n.xf = identity
	}
	if t == n.xf {
		return
	}
	n.xf = t
	z := zoomOf(t)
	if z == 1 {
		z = 0
	}
	if (z == 0) != (n.zoom == 0) {
		if z == 0 {
			h.zoomed--
		} else {
			h.zoomed++
		}
	}
	if z != n.zoom && n.hwnd != 0 {
		base := h.outerK(n)
		n.zoom = z
		h.rescale(n.hwnd, base)
	} else {
		n.zoom = z
	}
	h.updatePicture(n) // a rotation's (picture.go)
	if n.placed {
		h.placeFrame(n)
	}
}

// outerK is the k of the view n is in (0: the main window's): n's
// without its own scale.
func (h *Host) outerK(n *node) float32 {
	if n.k == 0 {
		return 0
	}
	k := n.k / n.own()
	if k == h.scale {
		return 0
	}
	return k
}

// transformed is the rectangle (pixels, in its parent) n's window takes
// for frame f (points, in its parent, less a scroll view's offset) under
// its transform: scaled about its center, then moved.
func (h *Host) transformed(n *node, x, y, w, ht float32) (x0, y0, x1, y1 int32) {
	k := h.scaleOf(n) / n.own() // the parent's
	t := n.xf
	if t == (xform{}) {
		t = identity
	}
	sx, sy := float32(math.Abs(float64(t.sx))), float32(math.Abs(float64(t.sy)))
	cx, cy := x+w/2+t.tx, y+ht/2+t.ty
	w, ht = w*sx, ht*sy
	r := func(v float32) int32 { return int32(math.Round(float64(v * k))) }
	return r(cx - w/2), r(cy - ht/2), r(cx + w/2), r(cy + ht/2)
}

// zoomUpTo is the product of the scales the transforms of w's view and
// those it's in, up to top's, give what's in them.
func (h *Host) zoomUpTo(w, top uintptr) float32 {
	if h.zoomed == 0 {
		return 1
	}
	z := float32(1)
	for x := w; x != 0; x = Parent(x) {
		if m := h.byHwnd[x]; m != nil && m.hwnd == x {
			z *= m.own()
		}
		if x == top {
			break
		}
	}
	return z
}
