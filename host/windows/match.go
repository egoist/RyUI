//go:build windows

package windows

import (
	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// Style.Match on Windows, as on macOS: when a view with a key is destroyed
// during an animation and another with the key is inserted, the new one
// grows from the old one's place and size, and the old one moves along to
// the new one's, fading its own drawing. The views with keys are noted
// where they are (in their top-level window's client area) as the
// animated batch starts destroying; the old view's window moves into its
// top-level window, where it was, on top, until the batch's frames are
// applied (when the frames batch ends its animation); then they're
// matched, the frames stepped with the animation's other tweens. A view
// going with no match fades and goes.

type matchOut struct {
	n   *node
	top uintptr // its top-level window
	r   Rect    // where it was, in top's client area (pixels)
}

type matching struct {
	count  int // nodes with a key
	frames map[string]matchOut
	outs   map[string]matchOut
	ins    []*node
}

func (h *Host) setMatch(n *node, key string) {
	if n.match != "" {
		h.match.count--
	}
	n.match = key
	if key != "" {
		h.match.count++
	}
}

// matchOut keeps a view with a key destroyed during an animation in its
// top-level window, where it was, until its match comes. True if it did.
func (h *Host) matchOut(n *node) bool {
	m := &h.match
	a := h.anim
	if a == nil || !a.active || m.count == 0 && n.match == "" {
		if n.match != "" {
			m.count--
			n.match = ""
		}
		return false
	}
	if m.frames == nil {
		m.frames = map[string]matchOut{}
		note := func(x *node) {
			if x.match == "" || !x.placed || x.hwnd == 0 || !IsWindowVisible(x.hwnd) {
				return
			}
			top := RootWindow(x.hwnd)
			m.frames[x.match] = matchOut{top: top, r: WindowRectIn(x.hwnd, top)}
		}
		note(n) // already gone from the nodes
		for _, x := range h.nodes {
			note(x)
		}
	}
	key := n.match
	if key == "" {
		return false
	}
	m.count--
	n.match = ""
	o, ok := m.frames[key]
	if !ok || o.r.W() <= 0 || o.r.H() <= 0 {
		return false
	}
	if old, ok := m.outs[key]; ok {
		h.exited(old.n) // two going with one key: the last waits
	}
	if p := n.parent; p != nil {
		for i, x := range p.children {
			if x == n {
				p.children = append(p.children[:i], p.children[i+1:]...)
				break
			}
		}
	}
	n.parent = nil
	SetParent(n.hwnd, o.top)
	SetWindowPos(n.hwnd, HWND_TOP, o.r.Left, o.r.Top, o.r.W(), o.r.H(), SWP_NOACTIVATE|SWP_NOCOPYBITS)
	k := h.scaleOf(n)
	n.frame = [4]float32{float32(o.r.Left) / k, float32(o.r.Top) / k, float32(o.r.W()) / k, float32(o.r.H()) / k}
	if h.exiting == nil {
		h.exiting = map[uintptr]*node{}
	}
	h.exiting[n.hwnd] = n // what's in it goes with it
	o.n = n
	if m.outs == nil {
		m.outs = map[string]matchOut{}
	}
	if len(m.outs) == 0 {
		// If no frames come to match it (nothing was laid out), it goes
		// once the core has returned.
		h.afterCore(func() {
			for k, o := range m.outs {
				delete(m.outs, k)
				h.fadeOut(o.n)
			}
		})
	}
	m.outs[key] = o
	return true
}

// matchIn notes a view inserted during an animation: it, or a view in it,
// may be one an old one's going to.
func (h *Host) matchIn(n *node) {
	if a := h.anim; a != nil && a.active && (h.match.count > 0 || len(h.match.outs) > 0) {
		h.match.ins = append(h.match.ins, n)
	}
}

// runMatches matches the views going and coming, as the frames batch
// ends its animation: their tweens step with the animation's.
func (h *Host) runMatches() {
	m := &h.match
	ins := m.ins
	m.ins = nil
	a := h.anim
	if len(m.outs) == 0 || a == nil {
		return
	}
	var coming []*node
	var collect func(n *node)
	collect = func(n *node) {
		if _, ok := m.outs[n.match]; ok && n.match != "" {
			coming = append(coming, n)
		}
		for _, c := range n.children {
			collect(c)
		}
	}
	for _, n := range ins {
		collect(n)
	}
	for _, n := range coming {
		o, ok := m.outs[n.match]
		if !ok || n.hwnd == 0 || RootWindow(n.hwnd) != o.top || o.n.hwnd == 0 {
			continue
		}
		delete(m.outs, n.match)
		parent := Parent(n.hwnd)
		to := WindowRectIn(n.hwnd, o.top)
		if to.W() <= 0 || to.H() <= 0 || parent == 0 {
			h.fadeOut(o.n)
			continue
		}
		k := h.scaleOf(n)
		// The old place, in the new view's parent, in points (placeAt
		// takes away a scroll's offset: it's put back).
		at := MapPoint(o.top, parent, Point{X: o.r.Left, Y: o.r.Top})
		from := [4]float32{float32(at.X) / k, float32(at.Y) / k, float32(o.r.W()) / k, float32(o.r.H()) / k}
		if p := n.parent; p != nil && p.kind == ops.KindScroll {
			from[0] += p.scrollX
			from[1] += p.scrollY
		}
		h.placeAt(n, from)
		a.tweens[n] = &tween{from: from, to: n.frame}
		kt := h.scaleOf(o.n)
		a.tweens[o.n] = &tween{from: o.n.frame, exit: true, fade: true, opacity: o.n.opacity,
			to: [4]float32{float32(to.Left) / kt, float32(to.Top) / kt, float32(to.W()) / kt, float32(to.H()) / kt}}
	}
	for k, o := range m.outs {
		delete(m.outs, k)
		h.fadeOut(o.n)
	}
}

// fadeOut fades an old view's own drawing where it is, and lets it go.
func (h *Host) fadeOut(n *node) {
	if a := h.anim; a != nil && n.hwnd != 0 {
		a.tweens[n] = &tween{from: n.frame, to: n.frame, exit: true, fade: true, opacity: n.opacity}
		return
	}
	h.exited(n)
}
