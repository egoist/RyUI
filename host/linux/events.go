//go:build linux && !android

package linux

import (
	"math"
	"time"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// A view's input is GTK's event controllers on its widget, added as the
// core listens: a click (Press, Pressed, a secondary click as LongPress),
// a long press, a drag (Pan, and a quick one a Swipe), a swipe, zoom and
// rotate gestures (touch screens, touchpads), motion (Hover), keys
// (Enter and Space press a focused pressable view).

type events struct {
	click, long, drag, swipe, zoom, rotate, motion, key, secondary Ptr

	pressedOn bool
	hovered   bool
	pan       panState
	zoomBegan bool
	rotBegan  bool
	wheel     wheelPinch
}

type panState struct {
	active bool
	x0, y0 float64 // where it began, in the view
	lastDX float64
	lastDY float64
	lastT  time.Time
	vx, vy float64
}

func (n *node) ev() *events {
	if n.events == nil {
		n.events = &events{}
	}
	return n.events
}

func (h *Host) listen(n *node, e ops.EventKind) {
	if e == ops.EventScrolled {
		return // scroll.go reports it as it scrolls
	}
	if n.widget == 0 {
		return
	}
	switch e {
	case ops.EventPress, ops.EventPressed:
		if n.kind == ops.KindButton {
			return // clicked
		}
		h.listenClick(n)
		if e == ops.EventPress {
			h.makePressable(n)
		}
	case ops.EventLongPress:
		h.listenLongPress(n)
	case ops.EventPan, ops.EventSwipe:
		h.listenDrag(n)
		if e == ops.EventSwipe {
			h.listenSwipe(n)
		}
	case ops.EventHover:
		h.listenHover(n)
	case ops.EventPinch:
		h.listenZoom(n)
	case ops.EventRotate:
		h.listenRotate(n)
	default:
		h.listenMore(n, e)
	}
}

func (h *Host) unlisten(n *node, e ops.EventKind) {}

// releaseEvents drops a node's controllers (they go with its widget).
func (h *Host) releaseEvents(n *node) {
	n.events = nil
}

func (h *Host) listenClick(n *node) {
	ev := n.ev()
	if ev.click != 0 {
		return
	}
	g := GestureClickNew()
	ev.click = g
	GestureSingleSetButton(g, 1)
	Connect(g, "pressed", func(count int, x, y float64) {
		ev.pressedOn = true
		h.emit(n, ops.Event{Kind: ops.EventPressed, Bool: true})
	})
	Connect(g, "released", func(count int, x, y float64) {
		was := ev.pressedOn
		h.endPress(n)
		if !was || ev.pan.active {
			return
		}
		w, ht := h.size(n, n.widget)
		if x < 0 || y < 0 || x > float64(w) || y > float64(ht) {
			return // released off the view
		}
		if count == 1 || n.listening(ops.EventPress) {
			h.press(n)
		}
	})
	Connect(g, "stopped", func() { h.endPress(n) })
	Connect(g, "cancel", func(uintptr) { h.endPress(n) })
	WidgetAddController(n.widget, g)
}

func (h *Host) endPress(n *node) {
	ev := n.ev()
	if !ev.pressedOn {
		return
	}
	ev.pressedOn = false
	h.emit(n, ops.Event{Kind: ops.EventPressed, Bool: false})
}

// press is a click or Enter on a pressable view: its tap menu, or Press.
func (h *Host) press(n *node) {
	if n.menu != nil && ops.MenuTrigger(n.menuTrigger) == ops.MenuTriggerTap {
		h.popupMenu(n, -1, -1)
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventPress})
}

// makePressable lets the keyboard reach and press a view that presses:
// Tab focuses it (a focus ring shows), Enter and Space press it, as a
// button.
func (h *Host) makePressable(n *node) {
	ev := n.ev()
	if ev.key != 0 {
		return
	}
	WidgetSetFocusable(n.widget, true)
	WidgetSetFocusOnClick(n.widget, false)
	WidgetAddCSSClass(n.widget, "ryui-focus-ring")
	k := EventControllerKeyNew()
	ev.key = k
	Connect(k, "key-pressed", func(keyval, keycode, state uint32) bool {
		switch keyval {
		case 0xff0d, 0xff8d, 0x020: // Return, KP_Enter, space
			h.press(n)
			return true
		case 0xff67: // Menu
			if n.menu != nil {
				h.popupMenu(n, -1, -1)
				return true
			}
		}
		return false
	})
	WidgetAddController(n.widget, k)
	h.updateA11y(n)
}

// listenLongPress: a held press, or a secondary click (a mouse's right
// button, a touchpad's two-finger click), as macOS and Windows take it.
func (h *Host) listenLongPress(n *node) {
	ev := n.ev()
	if ev.long != 0 {
		return
	}
	g := GestureLongPressNew()
	ev.long = g
	Connect(g, "pressed", func(x, y float64) {
		h.endPress(n)
		h.longPress(n, x, y)
	})
	WidgetAddController(n.widget, g)
	r := GestureClickNew()
	ev.secondary = r
	GestureSingleSetButton(r, 3)
	Connect(r, "pressed", func(count int, x, y float64) {
		h.longPress(n, x, y)
	})
	WidgetAddController(n.widget, r)
}

func (h *Host) longPress(n *node, x, y float64) {
	if n.menu != nil && ops.MenuTrigger(n.menuTrigger) == ops.MenuTriggerLongPress {
		h.popupMenu(n, x, y)
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventLongPress})
}

// listenDrag: a pan with the mouse, a touch or a touchpad's drag. A pan
// that ends fast is a swipe too.
func (h *Host) listenDrag(n *node) {
	ev := n.ev()
	if ev.drag != 0 {
		return
	}
	g := GestureDragNew()
	ev.drag = g
	p := &ev.pan
	Connect(g, "drag-begin", func(x, y float64) {
		*p = panState{x0: x, y0: y, lastT: time.Now()}
	})
	Connect(g, "drag-update", func(dx, dy float64) {
		now := time.Now()
		if !p.active {
			if math.Hypot(dx, dy) < 8 || draggable(n) {
				return // a click, so far; a drag and drop comes before a pan
			}
			p.active = true
			GestureSetState(g, EventSequenceClaimed)
			h.endPress(n)
			h.emitPan(n, ops.GestureStateBegan, dx, dy)
		}
		if dt := now.Sub(p.lastT).Seconds(); dt > 0 {
			// Smoothed, as a platform's velocity tracker.
			p.vx = 0.6*(dx-p.lastDX)/dt + 0.4*p.vx
			p.vy = 0.6*(dy-p.lastDY)/dt + 0.4*p.vy
		}
		p.lastDX, p.lastDY, p.lastT = dx, dy, now
		h.emitPan(n, ops.GestureStateChanged, dx, dy)
	})
	Connect(g, "drag-end", func(dx, dy float64) {
		if !p.active {
			return
		}
		p.active = false
		if time.Since(p.lastT) > 80*time.Millisecond {
			p.vx, p.vy = 0, 0 // it rested before letting go
		}
		h.emitPan(n, ops.GestureStateEnded, dx, dy)
		h.maybeSwipe(n, p.vx, p.vy)
	})
	Connect(g, "cancel", func(uintptr) {
		if p.active {
			p.active = false
			h.emitPan(n, ops.GestureStateCancelled, p.lastDX, p.lastDY)
		}
	})
	WidgetAddController(n.widget, g)
}

func (h *Host) emitPan(n *node, state ops.GestureState, dx, dy float64) {
	p := &n.ev().pan
	h.emit(n, ops.Event{Kind: ops.EventPan, G: ops.Gesture{
		State: state, X: float32(p.x0 + dx), Y: float32(p.y0 + dy),
		TranslationX: float32(dx), TranslationY: float32(dy),
		VelocityX: float32(p.vx), VelocityY: float32(p.vy), Scale: 1,
	}})
}

// maybeSwipe sends Swipe for a pan that ended fast, mostly along one
// axis (500 points a second, as UIKit's swipes take).
func (h *Host) maybeSwipe(n *node, vx, vy float64) {
	if !n.listening(ops.EventSwipe) {
		return
	}
	const fast = 500
	var dir int64
	switch {
	case math.Abs(vx) > math.Abs(vy) && vx > fast:
		dir = 1 // right
	case math.Abs(vx) > math.Abs(vy) && vx < -fast:
		dir = 2 // left
	case math.Abs(vy) > math.Abs(vx) && vy < -fast:
		dir = 4 // up
	case math.Abs(vy) > math.Abs(vx) && vy > fast:
		dir = 8 // down
	}
	if dir != 0 {
		h.emit(n, ops.Event{Kind: ops.EventSwipe, Int: dir})
	}
}

// listenSwipe: a touchscreen's or touchpad's flick, which GTK
// recognizes (a mouse's is a fast pan).
func (h *Host) listenSwipe(n *node) {
	ev := n.ev()
	if ev.swipe != 0 {
		return
	}
	g := GestureSwipeNew()
	GestureSingleSetTouchOnly(g, true)
	ev.swipe = g
	Connect(g, "swipe", func(vx, vy float64) {
		if !ev.pan.active {
			h.maybeSwipe(n, vx, vy)
		}
	})
	WidgetAddController(n.widget, g)
}

func (h *Host) listenHover(n *node) {
	ev := n.ev()
	if ev.motion != 0 {
		return
	}
	m := EventControllerMotionNew()
	ev.motion = m
	Connect(m, "enter", func(x, y float64) { h.setHover(n, true) })
	Connect(m, "leave", func() { h.setHover(n, false) })
	WidgetAddController(n.widget, m)
}

func (h *Host) setHover(n *node, on bool) {
	ev := n.ev()
	if ev.hovered == on {
		return
	}
	ev.hovered = on
	h.emit(n, ops.Event{Kind: ops.EventHover, Bool: on})
}

// listenZoom: two fingers pinching (a touchscreen, a touchpad), or Ctrl
// and the wheel.
func (h *Host) listenZoom(n *node) {
	ev := n.ev()
	if ev.zoom != 0 {
		return
	}
	g := GestureZoomNew()
	ev.zoom = g
	Connect(g, "scale-changed", func(scale float64) {
		state := ops.GestureStateChanged
		if !ev.zoomBegan {
			ev.zoomBegan = true
			state = ops.GestureStateBegan
		}
		h.emitTwoFinger(n, ops.EventPinch, state, float32(scale), 0)
	})
	Connect(g, "end", func(uintptr) {
		if ev.zoomBegan {
			ev.zoomBegan = false
			h.emitTwoFinger(n, ops.EventPinch, ops.GestureStateEnded, float32(GestureZoomGetScaleDelta(g)), 0)
		}
	})
	WidgetAddController(n.widget, g)
	h.listenWheelPinch(n)
}

func (h *Host) listenRotate(n *node) {
	ev := n.ev()
	if ev.rotate != 0 {
		return
	}
	g := GestureRotateNew()
	ev.rotate = g
	var start float64
	Connect(g, "angle-changed", func(angle, delta float64) {
		state := ops.GestureStateChanged
		if !ev.rotBegan {
			ev.rotBegan = true
			start = angle - delta
			state = ops.GestureStateBegan
		}
		h.emitTwoFinger(n, ops.EventRotate, state, 1, float32(angle-start))
	})
	Connect(g, "end", func(uintptr) {
		if ev.rotBegan {
			ev.rotBegan = false
			h.emitTwoFinger(n, ops.EventRotate, ops.GestureStateEnded, 1, 0)
		}
	})
	WidgetAddController(n.widget, g)
}

func (h *Host) emitTwoFinger(n *node, kind ops.EventKind, state ops.GestureState, scale, rotation float32) {
	var x, y float64
	ev := n.ev()
	g := ev.zoom
	if kind == ops.EventRotate {
		g = ev.rotate
	}
	if g != 0 {
		GestureGetBoundingBoxCenter(g, &x, &y)
	}
	h.emit(n, ops.Event{Kind: kind, G: ops.Gesture{State: state, X: float32(x), Y: float32(y), Scale: scale, Rotation: rotation}})
}

// wheelPinch is Ctrl and the wheel as a pinch: 10% a notch, begun with
// the first, ended once the wheel rests for 250 ms (as on Windows).
type wheelPinch struct {
	scale  float64
	active bool
	timer  uint32
	ctrl   Ptr
}

func (h *Host) listenWheelPinch(n *node) {
	ev := n.ev()
	if ev.wheel.ctrl != 0 {
		return
	}
	c := EventControllerScrollNew(ScrollVertical | ScrollDiscrete)
	ev.wheel.ctrl = c
	Connect(c, "scroll", func(dx, dy float64) bool {
		if EventControllerGetCurrentEventState(c)&ModControl == 0 {
			return false
		}
		w := &ev.wheel
		state := ops.GestureStateChanged
		if !w.active {
			w.active, w.scale, state = true, 1, ops.GestureStateBegan
		}
		w.scale *= math.Pow(1.1, -dy)
		h.emitTwoFinger(n, ops.EventPinch, state, float32(w.scale), 0)
		SourceRemove(w.timer)
		w.timer = TimeoutAdd(250, func() bool {
			w.timer = 0
			w.active = false
			h.emitTwoFinger(n, ops.EventPinch, ops.GestureStateEnded, float32(w.scale), 0)
			return false
		})
		return true
	})
	WidgetAddController(n.widget, c)
}
