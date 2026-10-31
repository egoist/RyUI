//go:build darwin && !ios

package darwin

import (
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
)

// Style.Transition: a view inserted during an animation comes in from its
// transition's state (faded, scaled, offset), and one destroyed during an
// animation goes out to it before it's removed. The view's frame carries
// the scale (around its center) and the offset; a fade is its alpha.

// transitionFrame is the frame f is in at the transition's state.
func transitionFrame(t []byte, f Rect) Rect {
	scale, dx, dy := float64(f32(t, 1)), float64(f32(t, 5)), float64(f32(t, 9))
	if scale > 0 && scale != 1 {
		w, h := f.Size.Width*scale, f.Size.Height*scale
		f.Origin.X += (f.Size.Width - w) / 2
		f.Origin.Y += (f.Size.Height - h) / 2
		f.Size = Size{Width: w, Height: h}
	}
	f.Origin.X += dx
	f.Origin.Y += dy
	return f
}

// enter_ puts n at its transition's state, at once, then animates it to
// f (in the animation the batch is in).
func (h *Host) enter_(n *node, f Rect) {
	alpha := MsgFloat(n.view, Sel("alphaValue"))
	withoutAnimation(func() {
		MsgSetRect(n.view, selSetFrame, transitionFrame(n.transition, f))
		if n.transition[0] != 0 {
			MsgSetFloat(n.view, Sel("setAlphaValue:"), 0)
		}
	})
	a := Send(n.view, "animator")
	MsgSetRect(a, selSetFrame, f)
	if n.transition[0] != 0 {
		MsgSetFloat(a, Sel("setAlphaValue:"), alpha)
	}
}

// exit animates a node going out with its transition and removes its
// view once it has; false: no transition to go out with.
func (h *Host) exit(n *node) bool {
	if !h.animating || n.transition == nil || n.view == 0 || Send(n.view, "window") == 0 {
		return false
	}
	v, t, f := n.view, n.transition, n.frame
	h.exiting[v] = true
	duration := h.anim.duration
	anim := NewBlock(func(_ objc.Block, ctx ID) {
		MsgSetFloat(ctx, Sel("setDuration:"), duration)
		Send(ctx, "setAllowsImplicitAnimation:", 1)
		a := Send(v, "animator")
		MsgSetRect(a, selSetFrame, transitionFrame(t, f))
		if t[0] != 0 {
			MsgSetFloat(a, Sel("setAlphaValue:"), 0)
		}
	})
	done := NewBlock(func(_ objc.Block) {
		delete(h.exiting, v)
		Send(v, "removeFromSuperview")
		Release(v)
	})
	Send(Class("NSAnimationContext"), "runAnimationGroup:completionHandler:", uintptr(anim), uintptr(done))
	return true
}

// inExiting reports whether v is inside a view going out with a
// transition: it leaves with it.
func (h *Host) inExiting(v ID) bool {
	if len(h.exiting) == 0 || v == 0 {
		return false
	}
	for s := Send(v, "superview"); s != 0; s = Send(s, "superview") {
		if h.exiting[s] {
			return true
		}
	}
	return false
}

// forget drops a node whose view stays on screen a while (going out):
// the view is released when it goes.
func (h *Host) forget(n *node) {
	for a, x := range h.a11yActions {
		if x.n == n {
			delete(h.a11yActions, a)
			Release(a)
		}
	}
	if n.view != 0 {
		delete(h.byView, n.view)
		if !h.exiting[n.view] {
			Release(n.view) // its superview keeps it until that goes
		}
		n.view = 0
	}
	if n.menu != 0 {
		delete(h.byMenu, n.menu)
		Release(n.menu)
		n.menu = 0
	}
}
