//go:build windows

package windows

import (
	. "github.com/egoist/asuka/internal/windows"
)

// Style.Transition on Windows, as on macOS: a view inserted during an
// animation comes in from its transition's state, and one destroyed
// during one goes out to it before its window goes. The frame carries the
// scale (around the view's center) and the offset, stepped with the
// animation's other frames; a fade fades the view's own drawing (a child
// window can't be translucent: what's in it doesn't fade). A view going
// out keeps its window, and the windows in it, until it's gone.

// transitionFrame is the frame f is in at the transition's state.
func transitionFrame(t []byte, f [4]float32) [4]float32 {
	scale, dx, dy := f32(t, 1), f32(t, 5), f32(t, 9)
	if scale > 0 && scale != 1 {
		w, h := f[2]*scale, f[3]*scale
		f[0] += (f[2] - w) / 2
		f[1] += (f[3] - h) / 2
		f[2], f[3] = w, h
	}
	f[0] += dx
	f[1] += dy
	return f
}

// enterTransition starts a view placed for the first time during an
// animation at its transition's state, animating to f. False: it has no
// transition, or it isn't animating.
func (h *Host) enterTransition(n *node, f [4]float32) bool {
	a := h.anim
	if a == nil || !a.active || len(n.transition) < 13 || n.placed || n.hwnd == 0 || n.parent == nil {
		return false
	}
	from := transitionFrame(n.transition, f)
	n.frame = f
	n.placed = true
	h.placeAt(n, from)
	tw := &tween{from: from, to: f}
	if n.transition[0] != 0 {
		tw.fade, tw.enter, tw.opacity = true, true, n.opacity
		n.opacity = 0.02 // drawn faint, not hidden
		h.applyOpacity(n)
		h.redraw(n)
	}
	a.tweens[n] = tw
	return true
}

// exitTransition keeps a view destroyed during an animation on screen
// while it goes out to its transition's state. False: it has none, or it
// isn't animating or shown.
func (h *Host) exitTransition(n *node) bool {
	a := h.anim
	if a == nil || !a.active || len(n.transition) < 13 || n.hwnd == 0 || !IsWindowVisible(n.hwnd) || n.parent == nil {
		return false
	}
	p := n.parent
	for i, x := range p.children {
		if x == n {
			p.children = append(p.children[:i], p.children[i+1:]...)
			break
		}
	}
	// n.parent stays: its scroll offset places it as it goes.
	tw := &tween{from: n.frame, to: transitionFrame(n.transition, n.frame), exit: true}
	if n.transition[0] != 0 {
		tw.fade, tw.opacity = true, n.opacity
	}
	a.tweens[n] = tw
	if h.exiting == nil {
		h.exiting = map[uintptr]*node{}
	}
	h.exiting[n.hwnd] = n
	return true
}

// inExiting reports whether hwnd is inside a view going out: it goes
// with it.
func (h *Host) inExiting(hwnd uintptr) bool {
	if len(h.exiting) == 0 || hwnd == 0 {
		return false
	}
	for w := Parent(hwnd); w != 0; w = Parent(w) {
		if h.exiting[w] != nil {
			return true
		}
	}
	return false
}

// stepFade fades a view's own drawing as its tween goes (e: 0 to 1).
func (h *Host) stepFade(n *node, tw *tween, e float32) {
	if !tw.fade {
		return
	}
	o := tw.opacity * (1 - e)
	if tw.enter {
		o = tw.opacity * e
	}
	n.opacity = max(o, 0.02)
	if tw.enter && e >= 1 {
		n.opacity = tw.opacity
	}
	h.applyOpacity(n)
	h.redraw(n)
}

// exited lets a view that went out go, its windows with it.
func (h *Host) exited(n *node) {
	// By the node: its window may have gone already (with the one it was
	// in), and its handle with it.
	for w, x := range h.exiting {
		if x == n {
			delete(h.exiting, w)
		}
	}
	n.parent = nil
	h.release(n)
}
