//go:build linux && !android

package linux

import (
	"math"
	"strconv"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A Shape is drawn by its view with GSK: its outline a GskPath, filled
// (nonzero, as Core Graphics fills) and stroked with a GskStroke (caps,
// joins, dashes). The path, the stroke and the trimmed outline are made
// as the props change, so a frame only appends them. Trim takes the part
// of the outline between two of its lengths (a GskPathMeasure's points),
// and moves inside an Animate on the view's frame clock, with the
// animation's curve, as a CAShapeLayer's strokeStart and strokeEnd do.

type shapeView struct {
	path      Ptr // GskPath: the outline (0: none)
	measure   Ptr // GskPathMeasure of path, made as it's first trimmed
	drawn     Ptr // the trimmed outline the stroke draws (path itself, untrimmed)
	stroke    Ptr // GskStroke
	fill      ops.Color
	color     ops.Color // the stroke's
	lineWidth float32
	cap, join int32
	dash      []float32
	dashPhase float32
	trim      [2]float32 // shown: start and end, 0 to 1
	target    [2]float32 // set
	anim      *trimAnim
}

// trimAnim moves a shape's trim on its view's frame clock.
type trimAnim struct {
	a        animation
	start    time.Time
	from, to [2]float32
}

var shapes = map[*node]*shapeView{}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindShape {
			return false
		}
		shapes[n] = &shapeView{lineWidth: 1, trim: [2]float32{0, 1}, target: [2]float32{0, 1}}
		return true
	})
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		s := shapes[n]
		if s == nil {
			return false
		}
		s.setProp(h, n, p, v)
		return true
	})
	drawHooks = append(drawHooks, func(h *Host, n *node, snap Ptr, bounds Rect) bool {
		s := shapes[n]
		if s == nil {
			return false
		}
		s.draw(h, snap)
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		if s := shapes[n]; s != nil {
			s.anim = nil
			s.setPath(0)
			s.freeStroke()
			delete(shapes, n)
		}
	})
}

func (s *shapeView) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropShapePath:
		if s.path == 0 && n.widget != 0 {
			WidgetSetCanTarget(n.widget, false) // clicks go to what's under it, as a shape layer takes none
		}
		s.setPath(buildPath(v.Bytes))
	case ops.PropShapeFill:
		s.fill = ops.Color(v.Int)
	case ops.PropShapeStroke:
		s.color = ops.Color(v.Int)
	case ops.PropShapeLineWidth:
		s.lineWidth = float32(v.Float)
		s.freeStroke()
	case ops.PropShapeLineCap:
		s.cap = int32(min(max(v.Int, 0), 2)) // butt, round, square: GSK's order
		s.freeStroke()
	case ops.PropShapeLineJoin:
		s.join = int32(min(max(v.Int, 0), 2)) // miter, round, bevel
		s.freeStroke()
	case ops.PropShapeDash:
		s.dash, s.dashPhase = s.dash[:0], 0
		if len(v.Bytes) >= 8 {
			r := reader{b: v.Bytes}
			s.dashPhase = r.f32()
			for r.ok(4) {
				s.dash = append(s.dash, r.f32())
			}
		}
		s.freeStroke()
	case ops.PropShapeTrimStart, ops.PropShapeTrimEnd:
		i := 0
		if p == ops.PropShapeTrimEnd {
			i = 1
		}
		s.target[i] = float32(v.Float)
		s.setTrim(h, n)
	}
	queueDraw(n)
}

// setPath takes path as the outline (0: none), letting go of the old.
func (s *shapeView) setPath(path Ptr) {
	if s.drawn != 0 && s.drawn != s.path {
		Call(Draw.FnPathUnref, s.drawn)
	}
	s.drawn = 0
	if s.measure != 0 {
		PathMeasureUnref(s.measure)
		s.measure = 0
	}
	if s.path != 0 {
		Call(Draw.FnPathUnref, s.path)
	}
	s.path = path
	s.trimmed()
}

func (s *shapeView) freeStroke() {
	if s.stroke != 0 {
		Call(Draw.FnStrokeFree, s.stroke)
		s.stroke = 0
	}
}

// setTrim shows the trim set: at once, or moving there inside an
// Animate.
func (s *shapeView) setTrim(h *Host, n *node) {
	if s.target == s.trim && s.anim == nil {
		return
	}
	if !h.animating || !n.placed || n.widget == 0 {
		s.anim = nil
		s.trim = s.target
		s.trimmed()
		return
	}
	if s.anim != nil && s.anim.to == s.target {
		return // both ends set in one animation: one tween
	}
	t := &trimAnim{a: h.anim, start: time.Now(), from: s.trim, to: s.target}
	s.anim = t
	AddTickCallback(n.widget, func(Ptr) bool {
		if shapes[n] != s || s.anim != t {
			return false // released, or set again
		}
		f, done := t.a.progress(time.Since(t.start).Seconds())
		s.trim = [2]float32{lerp(t.from[0], t.to[0], f), lerp(t.from[1], t.to[1], f)}
		if done {
			s.trim = t.to
			s.anim = nil
		}
		s.trimmed()
		queueDraw(n)
		return !done
	})
}

// trimmed makes the outline the stroke draws: the path, or its part
// between the trim's ends.
func (s *shapeView) trimmed() {
	if s.drawn != 0 && s.drawn != s.path {
		Call(Draw.FnPathUnref, s.drawn)
	}
	s.drawn = 0
	if s.path == 0 {
		return
	}
	a, b := max(s.trim[0], 0), min(s.trim[1], 1)
	if a <= 0 && b >= 1 {
		s.drawn = s.path
		return
	}
	if b <= a {
		return // nothing of it
	}
	s.drawn = trimPath(s.path, &s.measure, a, b)
}

// trimPath is the part of path from a to b (fractions of its length), its
// measure made in *m if it has none.
func trimPath(path Ptr, m *Ptr, a, b float32) Ptr {
	if *m == 0 {
		*m = PathMeasureNew(path)
	}
	length := Draw.PathMeasureGetLength(*m)
	if !(length > 0) {
		return 0
	}
	var p0, p1 PathPoint
	if !Draw.PathMeasureGetPoint(*m, a*length, p0.Ptr()) || !Draw.PathMeasureGetPoint(*m, b*length, p1.Ptr()) {
		return 0
	}
	builder := PathBuilderNew()
	Draw.PathBuilderAddSegment(builder, path, p0.Ptr(), p1.Ptr())
	return PathBuilderFreeToPath(builder)
}

// gskStroke is the shape's stroke, made as its props change.
func (s *shapeView) gskStroke() Ptr {
	if s.stroke == 0 {
		s.stroke = newStroke(s.lineWidth, s.cap, s.join, s.dash, s.dashPhase)
	}
	return s.stroke
}

// newStroke makes a GskStroke, its miter limit Core Graphics' (10).
func newStroke(width float32, cap, join int32, dash []float32, phase float32) Ptr {
	st := StrokeNew(max(width, 0))
	StrokeSetLineCap(st, cap)
	StrokeSetLineJoin(st, join)
	Draw.StrokeSetMiterLimit(st, 10)
	drawn := float32(0)
	for _, d := range dash {
		drawn += max(d, 0)
	}
	if len(dash) > 0 && drawn > 0 {
		StrokeSetDash(st, &dash[0], uintptr(len(dash)))
		StrokeSetDashOffset(st, phase)
	}
	return st
}

func (s *shapeView) draw(h *Host, snap Ptr) {
	if s.path == 0 {
		return
	}
	if c, ok := h.resolve(s.fill); ok && s.fill != 0 && c.A > 0 {
		Call(FnSnapshotAppendFill, snap, s.path, FillWinding, Addr(&c))
	}
	if s.drawn == 0 || s.lineWidth <= 0 || s.color == 0 {
		return
	}
	if c, ok := h.resolve(s.color); ok && c.A > 0 {
		Call(FnSnapshotAppendStroke, snap, s.drawn, s.gskStroke(), Addr(&c))
	}
}

// buildPath makes a GskPath from Shape's path format (u8 PathOp, then its
// f32 arguments; Canvas's paths too), through its SVG form: 0 if it draws
// nothing.
func buildPath(b []byte) Ptr {
	svg := pathSVG(b, nil)
	if len(svg) == 0 {
		return 0
	}
	p := PathParse(string(svg))
	if p != 0 && Draw.PathIsEmpty(p) {
		Call(Draw.FnPathUnref, p)
		return 0
	}
	return p
}

// pathSVG appends the path as SVG path data, which gsk_path_parse reads.
// Arcs go as SVG arcs of a quarter turn at most; a rectangle, rounded
// or not, and an ellipse are closed figures of their own, clockwise on
// screen from their top left (an ellipse from its right), as Core
// Graphics adds them.
func pathSVG(b []byte, out []byte) []byte {
	r := reader{b: b}
	f := func() float32 {
		v := r.f32()
		if v != v || v > 1e9 || v < -1e9 {
			return 0
		}
		return v
	}
	num := func(v float32) {
		out = append(out, ' ')
		out = strconv.AppendFloat(out, float64(v), 'g', -1, 32)
	}
	cmd := func(c byte, vs ...float32) {
		if len(out) > 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
		for _, v := range vs {
			num(v)
		}
	}
	arc := func(rx, ry float32, sweep bool, x, y float32) {
		s := float32(0)
		if sweep {
			s = 1
		}
		cmd('A', rx, ry, 0, 0, s, x, y)
	}
	current := false
	for r.off < len(b) {
		op := ops.PathOp(r.u8())
		switch op {
		case ops.PathOpMove:
			cmd('M', f(), f())
			current = true
		case ops.PathOpLine:
			x, y := f(), f()
			if !current {
				cmd('M', x, y)
			} else {
				cmd('L', x, y)
			}
			current = true
		case ops.PathOpQuad:
			cx, cy, x, y := f(), f(), f(), f()
			if !current {
				cmd('M', cx, cy)
			}
			cmd('Q', cx, cy, x, y)
			current = true
		case ops.PathOpCubic:
			x1, y1, x2, y2, x, y := f(), f(), f(), f(), f(), f()
			if !current {
				cmd('M', x1, y1)
			}
			cmd('C', x1, y1, x2, y2, x, y)
			current = true
		case ops.PathOpClose:
			if current {
				cmd('Z')
			}
		case ops.PathOpRect:
			x, y, w, ht := f(), f(), f(), f()
			cmd('M', x, y)
			cmd('L', x+w, y)
			cmd('L', x+w, y+ht)
			cmd('L', x, y+ht)
			cmd('Z')
			current = true
		case ops.PathOpRoundedRect:
			x, y, w, ht := f(), f(), f(), f()
			rad := min(f(), abs32(w)/2, abs32(ht)/2)
			if rad <= 0 {
				cmd('M', x, y)
				cmd('L', x+w, y)
				cmd('L', x+w, y+ht)
				cmd('L', x, y+ht)
				cmd('Z')
			} else {
				cmd('M', x+rad, y)
				cmd('L', x+w-rad, y)
				arc(rad, rad, true, x+w, y+rad)
				cmd('L', x+w, y+ht-rad)
				arc(rad, rad, true, x+w-rad, y+ht)
				cmd('L', x+rad, y+ht)
				arc(rad, rad, true, x, y+ht-rad)
				cmd('L', x, y+rad)
				arc(rad, rad, true, x+rad, y)
				cmd('Z')
			}
			current = true
		case ops.PathOpEllipse:
			x, y, w, ht := f(), f(), f(), f()
			rx, ry := w/2, ht/2
			cx, cy := x+rx, y+ry
			if rx == 0 || ry == 0 {
				continue
			}
			cmd('M', cx+rx, cy)
			arc(abs32(rx), abs32(ry), true, cx, cy+ry)
			arc(abs32(rx), abs32(ry), true, cx-rx, cy)
			arc(abs32(rx), abs32(ry), true, cx, cy-ry)
			arc(abs32(rx), abs32(ry), true, cx+rx, cy)
			cmd('Z')
			current = true
		case ops.PathOpArc:
			cx, cy, rad := f(), f(), f()
			start, end, cw := float64(f()), float64(f()), f()
			// The screen's clockwise (y down) is the angle growing.
			sweep := end - start
			if cw != 0 {
				for sweep < 0 {
					sweep += 2 * math.Pi
				}
				sweep = min(sweep, 2*math.Pi)
			} else {
				for sweep > 0 {
					sweep -= 2 * math.Pi
				}
				sweep = max(sweep, -2*math.Pi)
			}
			at := func(a float64) (float32, float32) {
				return cx + rad*float32(math.Cos(a)), cy + rad*float32(math.Sin(a))
			}
			x0, y0 := at(start)
			if current {
				cmd('L', x0, y0) // a line joins it, from the figure's last point
			} else {
				cmd('M', x0, y0)
			}
			current = true
			if rad <= 0 || sweep == 0 {
				continue
			}
			k := int(math.Ceil(math.Abs(sweep)/(math.Pi/2) - 1e-9))
			for i := 1; i <= k; i++ {
				x, y := at(start + sweep*float64(i)/float64(k))
				arc(abs32(rad), abs32(rad), sweep > 0, x, y)
			}
		default:
			return out
		}
	}
	return out
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
