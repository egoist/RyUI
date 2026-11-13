//go:build linux && !android

package linux

import (
	"math"
	"time"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// Animation: between AnimateBegin and AnimateEnd, frames, opacity and
// transforms the core sets move there over the animation, stepped on the
// frame clock (GTK has no implicit animations of a widget's allocation);
// a view's first frame doesn't animate. Enter and exit transitions start
// from (and go out to) the transition's state.

type animation struct {
	duration, delay   float64 // seconds
	curve             ops.AnimationCurve
	repeat            int32 // times more; -1 forever
	autoreverse       bool
	damping, velocity float64
}

type tween struct {
	a             animation
	start         time.Time
	from, to      [4]float32
	fromOp, toOp  float32
	fromT, toT    transform
	frame, op, tr bool
	exit          bool // a view going out: removed when done
	widget        Ptr
	kept          []*node // destroyed views in it, released with it (exit)
}

func (h *Host) beginAnimation(args []byte) {
	r := reader{b: args}
	a := animation{duration: float64(r.f32()), curve: ops.AnimationCurve(r.u8()), repeat: int32(r.u32())}
	a.autoreverse = r.u8() != 0
	a.delay = float64(r.f32())
	a.damping, a.velocity = float64(r.f32()), float64(r.f32())
	if a.damping <= 0 {
		a.damping = 1
	}
	h.anim = a
	h.animating = a.duration > 0 && !h.env.ReduceMotion
}

func (h *Host) endAnimation() {
	if h.animating {
		h.runMatches() // match.go: once the frames are applied
	}
	h.animating = false
}

func (h *Host) tweenOf(n *node) *tween {
	if h.tweens == nil {
		h.tweens = map[*node]*tween{}
	}
	t := h.tweens[n]
	if t == nil || t.exit {
		t = &tween{}
		h.tweens[n] = t
	}
	t.a = h.anim
	t.start = time.Now()
	h.startTicking()
	return t
}

// animate moves n from where it shows to its new frame.
func (h *Host) animate(n *node, old [4]float32) {
	t := h.tweenOf(n)
	if cur := h.frameNow(n); cur != nil {
		old = *cur
	}
	t.from, t.to, t.frame = old, n.frame, true
	h.relayout(n)
}

func (h *Host) animateOpacity(n *node) {
	t := h.tweenOf(n)
	from := float32(1)
	if n.widget != 0 {
		from = float32(WidgetGetOpacity(n.widget))
	}
	t.fromOp, t.toOp, t.op = from, n.opacity, true
}

func (h *Host) animateTransform(n *node, to transform) {
	t := h.tweenOf(n)
	from := n.transform
	if !from.set {
		from = transform{sx: 1, sy: 1}
	}
	if !to.set {
		to = transform{sx: 1, sy: 1}
	}
	t.fromT, t.toT, t.tr = from, to, true
	n.transform = to
	n.transform.set = true
}

// progress is how far an animation is at a time: its curve's value, and
// whether it's done.
func (a *animation) progress(elapsed float64) (float64, bool) {
	elapsed -= a.delay
	if elapsed <= 0 {
		return 0, false
	}
	d := max(a.duration, 1e-3)
	cycle := elapsed / d
	runs := float64(a.repeat) + 1
	done := a.repeat >= 0 && cycle >= runs
	if done {
		cycle = runs
	}
	i := math.Floor(cycle)
	f := cycle - i
	if done {
		f, i = 1, runs-1
	}
	if a.autoreverse && int(i)%2 == 1 {
		f = 1 - f
	}
	return a.ease(f), done
}

func (a *animation) ease(t float64) float64 {
	switch a.curve {
	case ops.AnimationCurveLinear:
		return t
	case ops.AnimationCurveEaseIn:
		return bezier(t, 0.42, 0, 1, 1)
	case ops.AnimationCurveEaseOut:
		return bezier(t, 0, 0, 0.58, 1)
	case ops.AnimationCurveSpring:
		return spring(t, a.damping, a.velocity, a.duration)
	}
	return bezier(t, 0.42, 0, 0.58, 1)
}

// bezier is a CSS cubic-bezier timing function at t.
func bezier(t, x1, y1, x2, y2 float64) float64 {
	// Solve x(s) = t for s (Newton, then bisection), return y(s).
	cx := func(s float64) float64 { return 3*x1*s*(1-s)*(1-s) + 3*x2*s*s*(1-s) + s*s*s }
	cy := func(s float64) float64 { return 3*y1*s*(1-s)*(1-s) + 3*y2*s*s*(1-s) + s*s*s }
	lo, hi := 0.0, 1.0
	s := t
	for range 20 {
		x := cx(s)
		if math.Abs(x-t) < 1e-5 {
			break
		}
		if x < t {
			lo = s
		} else {
			hi = s
		}
		s = (lo + hi) / 2
	}
	return cy(s)
}

// spring is a damped spring's position from 0 to 1 over the animation's
// duration (the ratio of critical damping, and an initial velocity in
// the change's fractions per second), settling as the duration ends.
func spring(t, damping, velocity, duration float64) float64 {
	if t >= 1 {
		return 1
	}
	// A stiffness that settles within the duration.
	omega := 2 * math.Pi / max(duration, 0.05) * 1.2
	time := t * duration
	if damping < 1 {
		wd := omega * math.Sqrt(1-damping*damping)
		a := 1.0
		b := (damping*omega - velocity) / wd
		return 1 - math.Exp(-damping*omega*time)*(a*math.Cos(wd*time)+b*math.Sin(wd*time))
	}
	return 1 - math.Exp(-omega*time)*(1+(omega-velocity)*time)
}

func lerp(a, b float32, f float64) float32 { return a + (b-a)*float32(f) }

// frameNow is the frame a node shows now, while it's animating.
func (h *Host) frameNow(n *node) *[4]float32 {
	t := h.tweens[n]
	if t == nil || !t.frame {
		return nil
	}
	f, _ := t.a.progress(time.Since(t.start).Seconds())
	var r [4]float32
	for i := range r {
		r[i] = lerp(t.from[i], t.to[i], f)
	}
	return &r
}

func (h *Host) relayout(n *node) {
	if n.widget == 0 {
		return
	}
	if p := WidgetGetParent(n.widget); p != 0 {
		Call(FnQueueAllocate, p)
	}
	Call(FnQueueAllocate, n.widget)
}

func (h *Host) startTicking() {
	if h.ticking || h.main == nil {
		return
	}
	h.ticking = true
	AddTickCallback(h.main.content, func(clock Ptr) bool {
		h.tick(clock)
		return h.ticking
	})
}

// tick steps the animations and sends Frame while the core asks for it.
func (h *Host) tick(clock Ptr) {
	now := time.Now()
	for n, t := range h.tweens {
		f, done := t.a.progress(now.Sub(t.start).Seconds())
		if t.op && n.widget != 0 {
			WidgetSetOpacity(n.widget, float64(lerp(t.fromOp, t.toOp, f)))
		}
		if t.tr {
			n.transform = transform{set: true,
				tx: lerp(t.fromT.tx, t.toT.tx, f), ty: lerp(t.fromT.ty, t.toT.ty, f),
				sx: lerp(t.fromT.sx, t.toT.sx, f), sy: lerp(t.fromT.sy, t.toT.sy, f),
				rotation: lerp(t.fromT.rotation, t.toT.rotation, f)}
		}
		if t.frame || t.tr {
			h.relayout(n)
		}
		if done {
			delete(h.tweens, n)
			if t.tr {
				n.transform = t.toT
				n.transform.set = !(t.toT.tx == 0 && t.toT.ty == 0 && t.toT.sx == 1 && t.toT.sy == 1 && t.toT.rotation == 0)
				h.relayout(n)
			}
			if t.exit {
				h.exited(n, t)
			}
		}
	}
	if h.frames && clock != 0 {
		us := FrameClockGetFrameTime(clock)
		if h.root != nil {
			h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventFrame, Float: float64(us) / 1e6})
		}
	}
	if len(h.tweens) == 0 && !h.frames {
		h.ticking = false
	}
}

// setFrames turns the Frame events on or off (ui.OnFrame).
func (h *Host) setFrames(on bool) {
	h.frames = on
	if on {
		h.startTicking()
	}
}

// Transitions.

type transition struct {
	fade          bool
	scale, dx, dy float32
}

func (h *Host) setTransition(n *node, b []byte) {
	if len(b) == 0 {
		n.transition = nil
		return
	}
	r := reader{b: b}
	n.transition = &transition{fade: r.u8() != 0, scale: r.f32(), dx: r.f32(), dy: r.f32()}
}

// enter brings a view in from its transition's state, as it's placed for
// the first time during an animation.
func (h *Host) enterFrom(n *node) {
	tr := n.transition
	t := h.tweenOf(n)
	if tr.fade {
		t.fromOp, t.toOp, t.op = 0, n.opacity, true
		WidgetSetOpacity(n.widget, 0)
	}
	from := transform{set: true, sx: 1, sy: 1, tx: tr.dx, ty: tr.dy}
	if tr.scale > 0 {
		from.sx, from.sy = tr.scale, tr.scale
	}
	to := n.transform
	if !to.set {
		to = transform{sx: 1, sy: 1}
	}
	t.fromT, t.toT, t.tr = from, to, true
	n.transform = from
}

// exit keeps a destroyed view on screen, going out to its transition's
// state, until the animation ends. It reports whether it does.
func (h *Host) exit(n *node) bool {
	if h.matchOut(n) || h.keptByExit(n) {
		return true
	}
	if !h.animating || n.transition == nil || n.widget == 0 || WidgetGetParent(n.widget) == 0 {
		return false
	}
	tr := n.transition
	t := h.tweenOf(n)
	t.exit = true
	t.widget = n.widget
	if tr.fade {
		t.fromOp, t.toOp, t.op = float32(WidgetGetOpacity(n.widget)), 0, true
	}
	from := n.transform
	if !from.set {
		from = transform{sx: 1, sy: 1}
	}
	to := transform{set: true, sx: 1, sy: 1, tx: tr.dx, ty: tr.dy}
	if tr.scale > 0 {
		to.sx, to.sy = tr.scale, tr.scale
	}
	t.fromT, t.toT, t.tr = from, to, true
	return true
}

// keptByExit keeps a destroyed view inside one going out: it goes with
// it. It reports whether it does.
func (h *Host) keptByExit(n *node) bool {
	if len(h.tweens) == 0 || n.widget == 0 {
		return false
	}
	for w := WidgetGetParent(n.widget); w != 0; w = WidgetGetParent(w) {
		if x := h.byWidget[w]; x != nil {
			if t := h.tweens[x]; t != nil && t.exit {
				t.kept = append(t.kept, n)
				return true
			}
		}
	}
	return false
}

// exited removes a view that went out, and the views kept in it.
func (h *Host) exited(n *node, t *tween) {
	for _, k := range t.kept {
		h.finishRelease(k)
	}
	if t.widget != 0 && WidgetGetParent(t.widget) != 0 {
		WidgetUnparent(t.widget)
	}
	h.finishRelease(n)
}
