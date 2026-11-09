//go:build darwin && !ios

package darwin

import (
	"math"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

// Swipes (OnSwipe) on a Mac, as UISwipeGestureRecognizer's on iOS: a
// quick flick one way.
//
//   - With the mouse, a pan that ends fast (the view's pan recognizer).
//   - On a trackpad, a two-finger flick (its scroll events, from the
//     fingers touching to their lifting): across always, up and down only
//     where no scroll view around the view scrolls that way. The scroll
//     goes on to the scroll views, which still scroll.
//   - A three-finger swipe (swipeWithEvent:), where the system's settings
//     leave it to apps.

// The swipe directions, as ui.SwipeDirection's bits.
const (
	swipeRight = 1 << 0
	swipeLeft  = 1 << 1
	swipeUp    = 1 << 2
	swipeDown  = 1 << 3
)

// swipeDirection is a gesture's swipe, from how far (points) and how fast
// (points a second) it went when it ended: 0 for none.
func swipeDirection(dx, dy, vx, vy float32) int64 {
	const far, fast = 30, 300
	switch {
	case abs(dx) > abs(dy) && dx > far && vx > fast:
		return swipeRight
	case abs(dx) > abs(dy) && dx < -far && vx < -fast:
		return swipeLeft
	case abs(dy) >= abs(dx) && dy < -far && vy < -fast:
		return swipeUp
	case abs(dy) >= abs(dx) && dy > far && vy > fast:
		return swipeDown
	}
	return 0
}

func abs(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// swipeEvent is a three-finger swipe on a view: false when it takes no
// swipes (its superview's then).
func (h *Host) swipeEvent(view, ev ID) bool {
	n := h.byView[view]
	if n == nil || !n.listening(ops.EventSwipe) {
		return false
	}
	// AppKit's deltas: x 1 a swipe to the left, y 1 a swipe up.
	dx, dy := MsgFloat(ev, Sel("deltaX")), MsgFloat(ev, Sel("deltaY"))
	var dir int64
	switch {
	case dx > 0:
		dir = swipeLeft
	case dx < 0:
		dir = swipeRight
	case dy > 0:
		dir = swipeUp
	case dy < 0:
		dir = swipeDown
	}
	if dir != 0 {
		h.emit(n, ops.Event{Kind: ops.EventSwipe, Int: dir})
	}
	return true
}

// wheelSwipe follows a trackpad's two-finger scroll over a view that
// takes swipes: how far the fingers went, and how fast lately.
type wheelSwipe struct {
	view   ID
	dx, dy float64 // points, the way the fingers moved
	vx, vy float64 // points a second
	last   float64 // the last event's time (seconds)
}

// NSEvent's phases.
const (
	phaseBegan     = 1
	phaseChanged   = 4
	phaseEnded     = 8
	phaseCancelled = 16
)

// wheel looks at a scroll event over view for a swipe. It never takes
// the event: the scroll views around still scroll (false).
func (h *Host) wheel(view, ev ID) bool {
	n := h.byView[view]
	if n == nil || !n.listening(ops.EventSwipe) || !SendBool(ev, "hasPreciseScrollingDeltas") {
		return false
	}
	w := &h.wheelSwipe
	phase := SendInt(ev, "phase")
	t := MsgFloat(ev, Sel("timestamp"))
	// The deltas move the content; the fingers move the other way unless
	// scrolling is natural (the content follows them).
	dx, dy := MsgFloat(ev, Sel("scrollingDeltaX")), MsgFloat(ev, Sel("scrollingDeltaY"))
	if !SendBool(ev, "isDirectionInvertedFromDevice") {
		dx, dy = -dx, -dy
	}
	// AppKit's y grows up the screen in a scroll's deltas: a finger moving
	// down the trackpad scrolls the content down (natural), a positive y.
	switch {
	case phase&phaseBegan != 0:
		*w = wheelSwipe{view: view, last: t}
	case w.view != view:
		return false
	case phase&phaseChanged != 0:
		if dt := t - w.last; dt > 0 {
			// The last ~50 ms's velocity.
			a := math.Min(dt/0.05, 1)
			w.vx += (dx/dt - w.vx) * a
			w.vy += (dy/dt - w.vy) * a
		}
		w.dx += dx
		w.dy += dy
		w.last = t
	case phase&(phaseEnded|phaseCancelled) != 0:
		ended := phase&phaseEnded != 0
		sw := *w
		*w = wheelSwipe{}
		if !ended || t-sw.last > 0.1 {
			return false // the fingers stopped before they lifted
		}
		dir := swipeDirection(float32(sw.dx), float32(sw.dy), float32(sw.vx), float32(sw.vy))
		if dir&(swipeUp|swipeDown) != 0 && h.scrollsVertically(view) {
			return false // the scroll view's
		}
		if dir&(swipeLeft|swipeRight) != 0 && h.scrollsHorizontally(view) {
			return false
		}
		if dir != 0 {
			h.emit(n, ops.Event{Kind: ops.EventSwipe, Int: dir})
		}
	}
	return false
}

// scrollsVertically and scrollsHorizontally are whether a scroll view
// around view scrolls that way.
func (h *Host) scrollsVertically(view ID) bool   { return h.scrollsAround(view, false) }
func (h *Host) scrollsHorizontally(view ID) bool { return h.scrollsAround(view, true) }

func (h *Host) scrollsAround(view ID, across bool) bool {
	for sv := Send(view, "enclosingScrollView"); sv != 0; sv = Send(sv, "enclosingScrollView") {
		doc := Send(sv, "documentView")
		if doc == 0 {
			continue
		}
		d, c := MsgRect(doc, Sel("frame")), MsgRect(Send(sv, "contentView"), Sel("bounds"))
		if across && d.Size.Width > c.Size.Width+0.5 || !across && d.Size.Height > c.Size.Height+0.5 {
			return true
		}
	}
	return false
}
