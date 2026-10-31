//go:build windows && amd64

package windows

import (
	"math"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// A Shape is a view of the host's that draws its outline with GDI+,
// antialiased: filled, stroked (caps, joins, dashes), trimmed.
type shape struct {
	path         []byte
	fill, stroke ops.Color
	lineWidth    float32
	cap, join    int32
	trimStart    float32
	trimEnd      float32
	dash         []float32
	dashPhase    float32
}

func (s *shape) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropShapePath:
		s.path = append(s.path[:0], v.Bytes...)
	case ops.PropShapeFill:
		s.fill = ops.Color(v.Int)
	case ops.PropShapeStroke:
		s.stroke = ops.Color(v.Int)
	case ops.PropShapeLineWidth:
		s.lineWidth = float32(v.Float)
	case ops.PropShapeLineCap:
		s.cap = int32(v.Int)
	case ops.PropShapeLineJoin:
		s.join = int32(v.Int)
	case ops.PropShapeTrimStart:
		s.trimStart = float32(v.Float)
	case ops.PropShapeTrimEnd:
		s.trimEnd = float32(v.Float)
	case ops.PropShapeDash:
		s.dash = s.dash[:0]
		if len(v.Bytes) >= 8 {
			s.dashPhase = f32(v.Bytes, 0)
			for i := 4; i+4 <= len(v.Bytes); i += 4 {
				s.dash = append(s.dash, f32(v.Bytes, i))
			}
		}
	}
	h.redraw(n)
}

// build makes the GDI+ path from the wire format (u8 PathOp, then its
// f32 arguments), in pixels.
func (s *shape) build(k float32) Path {
	p := NewPath()
	b := s.path
	i := 0
	f := func() float32 {
		if i+4 > len(b) {
			i = len(b) + 1
			return 0
		}
		v := f32(b, i)
		i += 4
		return v
	}
	var cx, cy float32 // the current point
	for i < len(b) {
		op := ops.PathOp(b[i])
		i++
		switch op {
		case ops.PathOpMove:
			p.StartFigure()
			cx, cy = f()*k, f()*k
		case ops.PathOpLine:
			x, y := f()*k, f()*k
			p.Line(cx, cy, x, y)
			cx, cy = x, y
		case ops.PathOpQuad:
			qx, qy, x, y := f()*k, f()*k, f()*k, f()*k
			// A quadratic is the cubic with its control points 2/3 of the
			// way to its own.
			p.Bezier(cx, cy, cx+2*(qx-cx)/3, cy+2*(qy-cy)/3, x+2*(qx-x)/3, y+2*(qy-y)/3, x, y)
			cx, cy = x, y
		case ops.PathOpCubic:
			x1, y1, x2, y2, x, y := f()*k, f()*k, f()*k, f()*k, f()*k, f()*k
			p.Bezier(cx, cy, x1, y1, x2, y2, x, y)
			cx, cy = x, y
		case ops.PathOpClose:
			p.CloseFigure()
		case ops.PathOpRect:
			x, y, w, ht := f()*k, f()*k, f()*k, f()*k
			p.StartFigure()
			p.Rect(x, y, w, ht)
		case ops.PathOpRoundedRect:
			x, y, w, ht := f()*k, f()*k, f()*k, f()*k
			p.RoundRect(x, y, w, ht, f()*k)
		case ops.PathOpEllipse:
			x, y, w, ht := f()*k, f()*k, f()*k, f()*k
			p.StartFigure()
			p.Ellipse(x, y, w, ht)
		case ops.PathOpArc:
			ax, ay, r := f()*k, f()*k, f()*k
			start, end, cw := f(), f(), f()
			sweep := end - start
			if cw != 0 {
				for sweep < 0 {
					sweep += 2 * math.Pi
				}
			} else {
				for sweep > 0 {
					sweep -= 2 * math.Pi
				}
			}
			deg := float32(180 / math.Pi)
			p.Arc(ax-r, ay-r, 2*r, 2*r, start*deg, sweep*deg)
			cx, cy = ax+r*float32(math.Cos(float64(end))), ay+r*float32(math.Sin(float64(end)))
		default:
			return p
		}
	}
	return p
}

func (s *shape) paint(h *Host, n *node, g Graphics) {
	if len(s.path) == 0 {
		return
	}
	k := h.scaleOf(n)
	p := s.build(k)
	defer p.Close()
	if c, ok := h.resolve(s.fill); ok && c.a > 0 {
		b := SolidBrush(c.argb())
		g.FillPath(b, p)
		b.Close()
	}
	c, ok := h.resolve(s.stroke)
	if !ok || c.a == 0 || s.lineWidth <= 0 {
		return
	}
	if s.trimStart > 0 || s.trimEnd < 1 {
		if s.trimEnd <= s.trimStart {
			return
		}
		t := trimmed(p, s.trimStart, s.trimEnd)
		defer t.Close()
		p = t
	}
	pen := NewPen(c.argb(), s.lineWidth*k)
	defer pen.Close()
	caps := [...]int{0, 2, 1}  // butt, round, square: GDI+'s flat, round, square
	joins := [...]int{0, 2, 1} // miter, round, bevel
	pen.SetCaps(caps[min(max(s.cap, 0), 2)], joins[min(max(s.join, 0), 2)])
	if len(s.dash) > 0 {
		// GDI+ dashes are in multiples of the pen's width.
		d := make([]float32, len(s.dash))
		for i, v := range s.dash {
			d[i] = max(v/s.lineWidth, 0.01)
		}
		pen.SetDash(d, s.dashPhase/s.lineWidth)
	}
	g.DrawPath(pen, p)
}

// trimmed is the part of a path from from to to (0 to 1 of its length),
// as a polyline of its flattened curves.
func trimmed(p Path, from, to float32) Path {
	pts, starts := FlattenedPoints(p)
	out := NewPath()
	if len(pts) < 2 {
		return out
	}
	total := float32(0)
	for i := 1; i < len(pts); i++ {
		if !starts[i] {
			total += dist(pts[i-1], pts[i])
		}
	}
	a, b := from*total, to*total
	at := float32(0)
	for i := 1; i < len(pts); i++ {
		if starts[i] {
			out.StartFigure()
			continue
		}
		d := dist(pts[i-1], pts[i])
		s0, s1 := at, at+d
		at = s1
		if s1 <= a || s0 >= b || d == 0 {
			continue
		}
		t0, t1 := max((a-s0)/d, 0), min((b-s0)/d, 1)
		p0, p1 := pts[i-1], pts[i]
		out.Line(p0[0]+(p1[0]-p0[0])*t0, p0[1]+(p1[1]-p0[1])*t0, p0[0]+(p1[0]-p0[0])*t1, p0[1]+(p1[1]-p0[1])*t1)
	}
	return out
}

func dist(a, b [2]float32) float32 {
	return float32(math.Hypot(float64(b[0]-a[0]), float64(b[1]-a[1])))
}
