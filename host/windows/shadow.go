//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// Shadows (Style.Shadow) on Windows: a child window can't draw outside
// itself, so a view's shadow is drawn by the view its window is in, under
// its children, around the view's shape (its corner radius), offset and
// blurred: concentric rounded rectangles from 3σ outside the shape to 3σ
// inside it, whose compounded alpha follows a Gaussian-blurred edge as
// the other platforms' shadows do (shadowSteps). The view's own window
// covers the part under it. Moving or resizing a view with a shadow
// repaints its parent around both its places.

// shadowSpec is a Shadow prop's: color, blur radius and offset (points).
type shadowSpec struct {
	color        ops.Color
	blur, dx, dy float32
}

func readShadow(b []byte) (shadowSpec, bool) {
	if len(b) < 20 {
		return shadowSpec{}, false
	}
	s := shadowSpec{color: ops.Color(le.Uint64(b)), blur: f32(b, 8), dx: f32(b, 12), dy: f32(b, 16)}
	return s, s.color != 0
}

// paintShadows draws the shadows of n's children whose windows are in
// hwnd.
func (h *Host) paintShadows(n *node, hwnd uintptr, g Graphics) {
	for _, c := range n.children {
		if len(c.shadow) == 0 || c.hwnd == 0 || c.hidden || Parent(c.hwnd) != hwnd || !IsWindowVisible(c.hwnd) {
			continue
		}
		s, ok := readShadow(c.shadow)
		if !ok {
			continue
		}
		col, ok := h.resolve(s.color)
		if !ok || col.a == 0 {
			continue
		}
		k := h.scaleOf(n)
		r := WindowRectIn(c.hwnd, hwnd)
		x, y := float32(r.Left)+s.dx*k, float32(r.Top)+s.dy*k
		w, ht := float32(r.W()), float32(r.H())
		radius := min(c.cornerRadius*k, w/2, ht/2)
		// Radius is a blur radius, as CSS and SwiftUI mean it: 2σ.
		a := float64(col.a) / 255 * float64(c.opacity)
		h.shadowBuf = shadowSteps(float64(s.blur*k)/2, a, h.shadowBuf)
		for _, st := range h.shadowBuf {
			spread := max(float32(st.spread), -min(w, ht)/2)
			lc := col
			lc.a = uint8(math.Round(st.alpha * 255))
			if lc.a == 0 {
				continue
			}
			g.FillRoundRect(lc.argb(), x-spread, y-spread, w+2*spread, ht+2*spread, max(radius+spread, 0))
		}
	}
}

// shadowArea is a view's shadow's reach outside its window, in pixels.
func (h *Host) shadowArea(n *node) (Rect, bool) {
	s, ok := readShadow(n.shadow)
	if !ok || n.hwnd == 0 || Parent(n.hwnd) == 0 {
		return Rect{}, false
	}
	k := h.scaleOf(n)
	r := WindowRectIn(n.hwnd, Parent(n.hwnd))
	// 3σ, Radius being 2σ.
	pad := int32(math.Ceil(float64((1.5*s.blur + max(abs(s.dx), abs(s.dy))) * k)))
	return Rect{Left: r.Left - pad, Top: r.Top - pad, Right: r.Right + pad, Bottom: r.Bottom + pad}, true
}

// invalidateShadow repaints the parent where n's shadow is.
func (h *Host) invalidateShadow(n *node) {
	if r, ok := h.shadowArea(n); ok {
		InvalidateRect(Parent(n.hwnd), &r)
	}
}
