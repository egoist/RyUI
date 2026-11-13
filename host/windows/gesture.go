//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// Pinches and rotations (OnPinch, OnRotate) on Windows:
//
//   - A touch screen's two fingers: WM_GESTURE's zoom (the distance
//     between them, against where it began) and rotate (the angle since
//     it began), which the view asks for as it listens.
//   - A precision touchpad's pinch, which Windows sends as the wheel with
//     Ctrl held, as it does for apps that zoom with Ctrl and the wheel (a
//     browser): so is a mouse's Ctrl and wheel, 10% a notch. A pinch
//     begins with the first, and ends once none has come for a moment.

const (
	timerPinch = 8
	pinchRest  = 250 // ms without the wheel that end a wheel's pinch
)

// twoFinger is a pinch or rotate under way on a view.
type twoFinger struct {
	wheel     bool    // from the wheel (Ctrl)
	scale     float32 // a wheel pinch's scale so far
	startDist float64 // a zoom's distance between the fingers as it began
	at        Point   // where, in the view (pixels)
}

// wheelPinch takes Ctrl and the wheel over a view that listens for
// pinches: false for one that doesn't (the wheel goes on to its parent).
func (h *Host) wheelPinch(n *node, hwnd uintptr, delta int32, lp uintptr) bool {
	if !n.listening(ops.EventPinch) || n.twoFinger != nil && !n.twoFinger.wheel {
		return false
	}
	at := ScreenToClient(hwnd, Point{X: XParam(lp), Y: YParam(lp)})
	if n.twoFinger == nil {
		n.twoFinger = &twoFinger{wheel: true, scale: 1}
		h.emitPinch(n, ops.GestureStateBegan, at, 1)
	}
	f := n.twoFinger
	f.at = at
	f.scale *= float32(math.Pow(1.1, float64(delta)/120))
	h.emitPinch(n, ops.GestureStateChanged, at, f.scale)
	SetTimer(hwnd, timerPinch, pinchRest)
	return true
}

// endWheelPinch ends a wheel's pinch once the wheel has rested.
func (h *Host) endWheelPinch(n *node, hwnd uintptr) {
	KillTimer(hwnd, timerPinch)
	f := n.twoFinger
	if f == nil || !f.wheel {
		return
	}
	n.twoFinger = nil
	h.emitPinch(n, ops.GestureStateEnded, f.at, f.scale)
}

func (h *Host) emitPinch(n *node, state ops.GestureState, at Point, scale float32) {
	h.emit(n, ops.Event{Kind: ops.EventPinch, G: ops.Gesture{State: state, X: h.pt(n, at.X), Y: h.pt(n, at.Y), Scale: scale}})
}

// touchGesture handles a WM_GESTURE on a view: a zoom it listens for as
// a pinch, a rotate as a rotate. The others (and gestures on views that
// don't listen) go on to DefWindowProc, which hands them to the parent.
func (h *Host) touchGesture(n *node, hwnd, lp uintptr) bool {
	gi, ok := Gesture(lp)
	if !ok {
		return false
	}
	at := ScreenToClient(hwnd, Point{X: int32(gi.X), Y: int32(gi.Y)})
	state := ops.GestureStateChanged
	switch {
	case gi.Flags&GF_BEGIN != 0:
		state = ops.GestureStateBegan
	case gi.Flags&GF_END != 0:
		state = ops.GestureStateEnded
	}
	g := ops.Gesture{State: state, X: h.pt(n, at.X), Y: h.pt(n, at.Y), Scale: 1}
	switch gi.ID {
	case GID_ZOOM:
		if !n.listening(ops.EventPinch) {
			return false
		}
		dist := float64(uint32(gi.Arguments))
		if state == ops.GestureStateBegan || n.twoFinger == nil {
			if f := n.twoFinger; f != nil && f.wheel {
				KillTimer(hwnd, timerPinch)
			}
			n.twoFinger = &twoFinger{startDist: max(dist, 1)}
			g.State = ops.GestureStateBegan
		}
		g.Scale = float32(dist / n.twoFinger.startDist)
		if state == ops.GestureStateEnded {
			n.twoFinger = nil
		}
		h.emit(n, ops.Event{Kind: ops.EventPinch, G: g})
	case GID_ROTATE:
		if !n.listening(ops.EventRotate) {
			return false
		}
		if state != ops.GestureStateBegan {
			// The angle since it began; Windows's is counterclockwise,
			// RyUI's clockwise.
			g.Rotation = -float32(RotateAngle(gi.Arguments))
		}
		h.emit(n, ops.Event{Kind: ops.EventRotate, G: g})
	default:
		return false
	}
	CloseGesture(lp)
	return true
}
