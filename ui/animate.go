package ui

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
)

// Curve is an animation's timing.
type Curve = ops.AnimationCurve

const (
	EaseInOut = ops.AnimationCurveEaseInOut
	Linear    = ops.AnimationCurveLinear
	EaseIn    = ops.AnimationCurveEaseIn
	EaseOut   = ops.AnimationCurveEaseOut
	Spring    = ops.AnimationCurveSpring // critically damped-ish, as UIKit's default spring
)

// Animation is how a change animates.
type Animation struct {
	Duration float32 // seconds; 0: 0.25
	Curve    Curve
	// Repeat runs it again, this many times more; Forever, until its
	// props change again (a pulse, a spinner of your own). With
	// Autoreverses each run goes back before the next.
	Repeat       int
	Autoreverses bool
	// Delay is how long it waits, in seconds, before it starts.
	Delay float32
	// Damping is a Spring's, from 0 to 1: 1 settles without passing its
	// end, less overshoots and bounces back (UIKit's damping ratio); 0:
	// 0.85, a little.
	Damping float32
	// Velocity is a Spring's at the start, as a fraction of the change
	// per second (UIKit's initialSpringVelocity): to carry on a fling, the
	// gesture's velocity divided by the distance left to go. 0: from rest.
	Velocity float32
	// OnDone is called on the main thread once the animation is logically
	// complete (after Delay and Duration, each repeat), whatever its views
	// are still settling: to chain the next step. Never for Forever.
	OnDone func()
}

// Forever repeats an animation until its props change again.
const Forever = -1

// Animate runs f, and the frames and visual props (opacity, background,
// corner radius) that the next flush changes animate natively, with
// UIView animations and Android's animators. Views that appear don't
// animate in. Main thread.
//
//	ui.Animate(func() { expanded.Set(!expanded.Peek()) })
func Animate(f func()) { AnimateWith(Animation{}, f) }

// AnimateWith is Animate with a duration and curve.
func AnimateWith(a Animation, f func()) {
	r := activeRoot()
	if r == nil {
		f()
		return
	}
	if a.Duration <= 0 {
		a.Duration = 0.25
	}
	r.anim = a
	if !r.animating {
		r.animating = true
		r.animBegin()
	}
	signal.Batch(f)
	if a.OnDone != nil && a.Repeat >= 0 {
		runs := float32(a.Repeat + 1)
		if a.Autoreverses {
			runs *= 2
		}
		d := time.Duration(float64(a.Delay+a.Duration*runs) * float64(time.Second))
		time.AfterFunc(d, func() { Post(a.OnDone) })
	}
}

func (r *Root) animBegin() {
	binary.LittleEndian.PutUint32(r.animArgs[:4], math.Float32bits(r.anim.Duration))
	r.animArgs[4] = byte(r.anim.Curve)
	binary.LittleEndian.PutUint32(r.animArgs[5:9], uint32(int32(r.anim.Repeat)))
	r.animArgs[9] = 0
	if r.anim.Autoreverses {
		r.animArgs[9] = 1
	}
	binary.LittleEndian.PutUint32(r.animArgs[10:14], math.Float32bits(max(r.anim.Delay, 0)))
	damping := r.anim.Damping
	if damping <= 0 {
		damping = 0.85
	}
	binary.LittleEndian.PutUint32(r.animArgs[14:18], math.Float32bits(min(damping, 1)))
	binary.LittleEndian.PutUint32(r.animArgs[18:22], math.Float32bits(r.anim.Velocity))
	r.batch.Command(r.el.id, ops.CmdAnimateBegin, r.animArgs[:])
}

func (r *Root) animEnd() { r.batch.Command(r.el.id, ops.CmdAnimateEnd, nil) }
