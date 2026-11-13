//go:build linux && !android

package linux

import (
	"math"
	"strconv"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/ops"
)

// ProgressRings. libadwaita has no determinate ring, so the view draws
// one as AdwSpinner draws its circle: a track of the text color at 15%
// and, over it, an arc in the accent color (Color sets it) from the top,
// clockwise, as far as the value, round capped, its width growing with
// its size as the spinner's does (2.5 pixels at 16 across, 14 at 128).
// The arc moves to a new value, as the core's animation does, or as a
// progress bar's fill eases; at once with reduced motion. Without a
// value it's an AdwSpinner (GtkSpinner before libadwaita 1.6). Orca
// reads it as a progress bar, with its percentage.

const ringSize = 32 // its own size, across

type progressRing struct {
	value, shown float64 // shown: the value drawn, moving to value
	anim         *ringAnim
	paths        ringPaths
}

type ringAnim struct {
	a        animation
	start    time.Time
	from, to float64
}

// ringPaths are the track and the arc, made again only as the size or
// the value drawn changes.
type ringPaths struct {
	w, h, v     float32
	track, fill Ptr
	stroke      Ptr
}

func (p *ringPaths) free() {
	for _, x := range []*Ptr{&p.track, &p.fill} {
		if *x != 0 {
			Call(Draw.FnPathUnref, *x)
			*x = 0
		}
	}
	if p.stroke != 0 {
		Call(Draw.FnStrokeFree, p.stroke)
		p.stroke = 0
	}
}

var rings = map[*node]*progressRing{}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindProgressRing {
			return false
		}
		rings[n] = &progressRing{}
		setRole(n.widget, "progress-bar")
		h.ringA11y(n, rings[n], false)
		return true
	})
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		r := rings[n]
		if r == nil {
			return false
		}
		switch p {
		case ops.PropProgressRingValue:
			h.setRingValue(n, r, min(max(v.Float, 0), 1))
		case ops.PropProgressRingIndeterminate:
			h.setRingIndeterminate(n, r, v.Int != 0)
		default:
			return false
		}
		return true
	})
	measureHooks = append(measureHooks, func(h *Host, n *node, c layout.Constraints) (layout.Size, bool) {
		if rings[n] == nil {
			return layout.Size{}, false
		}
		// Its own size, or square: a style that sizes one side sizes
		// the other.
		s := layout.Size{W: ringSize, H: ringSize}
		if c.Width == c.Width {
			s.H = c.Width
		} else if c.Height == c.Height {
			s.W = c.Height
		}
		return s, true
	})
	drawHooks = append(drawHooks, func(h *Host, n *node, snap Ptr, bounds Rect) bool {
		r := rings[n]
		if r == nil {
			return false
		}
		if n.inner == 0 { // the spinner draws itself
			h.drawRing(n, r, snap, bounds)
		}
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		if r := rings[n]; r != nil {
			r.anim = nil
			r.paths.free()
			delete(rings, n)
		}
	})
}

// setRingIndeterminate shows the spinner, or the ring.
func (h *Host) setRingIndeterminate(n *node, r *progressRing, on bool) {
	if on == (n.inner != 0) {
		return
	}
	if on {
		var s Ptr
		if AdwSpinnerNew != nil {
			s = AdwSpinnerNew()
		} else {
			s = SpinnerNew()
			SpinnerSetSpinning(s, true)
		}
		h.setInner(n, s)
		// The view is the progress bar; the spinner in it isn't read.
		setRole(s, "presentation")
	} else {
		h.setInner(n, 0)
	}
	h.ringA11y(n, r, on)
	queueDraw(n)
}

// setRingValue moves the arc to v.
func (h *Host) setRingValue(n *node, r *progressRing, v float64) {
	r.value = v
	h.ringA11y(n, r, n.inner != 0)
	if h.env.ReduceMotion || !n.placed || n.widget == 0 {
		r.anim = nil
		r.shown = v
		queueDraw(n)
		return
	}
	a := animation{duration: 0.25, curve: ops.AnimationCurveEaseOut}
	if h.animating {
		a = h.anim
	}
	t := &ringAnim{a: a, start: time.Now(), from: r.shown, to: v}
	r.anim = t
	AddTickCallback(n.widget, func(Ptr) bool {
		if rings[n] != r || r.anim != t {
			return false // released, or set again
		}
		f, done := t.a.progress(time.Since(t.start).Seconds())
		r.shown = t.from + (t.to-t.from)*f
		if done {
			r.shown, r.anim = t.to, nil
		}
		queueDraw(n)
		return !done
	})
}

// ringA11y tells screen readers its value, as GtkProgressBar does: from
// 0 to 1, read as a percentage; an indeterminate one has none.
func (h *Host) ringA11y(n *node, r *progressRing, indeterminate bool) {
	if n.widget == 0 {
		return
	}
	const valueMax, valueMin, valueNow, valueText = 15, 16, 17, 18 // GTK_ACCESSIBLE_PROPERTY_VALUE_*
	if indeterminate {
		for _, p := range [...]int32{valueMax, valueMin, valueNow, valueText} {
			AccessibleResetProperty(n.widget, p)
		}
		return
	}
	for _, p := range [...]struct {
		id int32
		v  float64
	}{{valueMax, 1}, {valueMin, 0}, {valueNow, r.value}} {
		var val Value
		ValueInit(&val, TypeDouble)
		Sys.ValueSetDouble(&val, p.v)
		id := p.id
		AccessibleUpdateProperty(n.widget, 1, &id, &val)
		ValueUnset(&val)
	}
	setA11yString(n.widget, valueText, strconv.Itoa(int(math.Round(r.value*100)))+"%")
}

// ringWidth is AdwSpinner's stroke at a diameter: 2.5 at 16, 14 at 128,
// in between along a line.
func ringWidth(d float32) float32 {
	return min(max(2.5+(d-16)*(14-2.5)/(128-16), 2.5), 14)
}

func (h *Host) drawRing(n *node, r *progressRing, snap Ptr, b Rect) {
	d := min(b.W, b.H)
	if d <= 0 {
		return
	}
	v := float32(min(max(r.shown, 0), 1))
	p := &r.paths
	if p.w != b.W || p.h != b.H || p.v != v || p.stroke == 0 {
		p.free()
		p.w, p.h, p.v = b.W, b.H, v
		width := min(ringWidth(d), d/2)
		cx, cy, rad := b.W/2, b.H/2, (d-width)/2
		p.track = PathParse(ringSVG(cx, cy, rad, 1))
		if v > 0 {
			p.fill = PathParse(ringSVG(cx, cy, rad, v))
		}
		p.stroke = StrokeNew(width)
		StrokeSetLineCap(p.stroke, 1) // GSK_LINE_CAP_ROUND
	}
	track := h.theme.fg
	track.A *= 0.15
	if p.track != 0 {
		Call(FnSnapshotAppendStroke, snap, p.track, p.stroke, Addr(&track))
	}
	fill := h.theme.accent
	if n.color != 0 {
		if c, ok := h.resolve(n.color); ok {
			fill = c
		}
	}
	if p.fill != 0 {
		Call(FnSnapshotAppendStroke, snap, p.fill, p.stroke, Addr(&fill))
	}
}

// ringSVG is the circle's outline from its top, clockwise on screen, as
// far as v (1: the whole circle), as SVG path data.
func ringSVG(cx, cy, r, v float32) string {
	f := func(x float32) string { return strconv.FormatFloat(float64(x), 'f', 3, 32) }
	top := f(cx) + " " + f(cy-r)
	if v >= 1 {
		return "M " + top + " A " + f(r) + " " + f(r) + " 0 1 1 " + f(cx) + " " + f(cy+r) +
			" A " + f(r) + " " + f(r) + " 0 1 1 " + top + " Z"
	}
	a := float64(v)*2*math.Pi - math.Pi/2
	end := f(cx+r*float32(math.Cos(a))) + " " + f(cy+r*float32(math.Sin(a)))
	large := "0"
	if v > 0.5 {
		large = "1"
	}
	return "M " + top + " A " + f(r) + " " + f(r) + " 0 " + large + " 1 " + end
}
