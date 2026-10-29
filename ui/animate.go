package ui

import (
	"encoding/binary"
	"math"

	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
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
}

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
}

func (r *Root) animBegin() {
	binary.LittleEndian.PutUint32(r.animArgs[:4], math.Float32bits(r.anim.Duration))
	r.animArgs[4] = byte(r.anim.Curve)
	r.batch.Command(r.el.id, ops.CmdAnimateBegin, r.animArgs[:])
}

func (r *Root) animEnd() { r.batch.Command(r.el.id, ops.CmdAnimateEnd, nil) }
