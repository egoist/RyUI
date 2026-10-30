package ui

import (
	"time"

	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
)

type frameFn struct{ f func(time.Duration) }

// OnFrame calls f at each frame the display draws (each vsync: 60 or 120
// a second), with the frame's time, until stop is called or the
// component's owner is disposed: animation driven frame by frame
// (physics, a game, a gauge following a value), as SwiftUI's
// TimelineView(.animation) and Compose's withFrameNanos. What f writes
// shows in that frame. While nothing listens, the host asks for no
// frames. Main thread.
func OnFrame(f func(t time.Duration)) (stop func()) {
	r := activeRoot()
	fn := &frameFn{f}
	r.frames = append(r.frames, fn)
	r.setFrames()
	done := false
	stop = func() {
		if done {
			return
		}
		done = true
		for i, g := range r.frames {
			if g == fn {
				r.frames[i] = nil // the loop in frame compacts
				break
			}
		}
		r.setFrames()
	}
	signal.OnCleanup(stop)
	return stop
}

// setFrames asks the host for frames while any callback is registered.
func (r *Root) setFrames() {
	on := false
	for _, f := range r.frames {
		if f != nil {
			on = true
			break
		}
	}
	if on == r.framesOn || r.el == nil {
		return
	}
	r.framesOn = on
	arg := [1]byte{}
	if on {
		arg[0] = 1
	}
	r.batch.Command(r.el.id, ops.CmdFrames, arg[:])
}

// frame runs the frame callbacks (Dispatch flushes after it): those
// added meanwhile run from the next frame, and the stopped are dropped.
func (r *Root) frame(t time.Duration) {
	n := len(r.frames)
	for i := 0; i < n; i++ {
		if fn := r.frames[i]; fn != nil {
			fn.f(t)
		}
	}
	live := r.frames[:0]
	for _, fn := range r.frames {
		if fn != nil {
			live = append(live, fn)
		}
	}
	clear(r.frames[len(live):])
	r.frames = live
}
