package render

import "math"

// Vec is a point or a vector, in an animation's units (its pixels).
type Vec struct{ X, Y float64 }

func (a Vec) Add(b Vec) Vec           { return Vec{a.X + b.X, a.Y + b.Y} }
func (a Vec) Sub(b Vec) Vec           { return Vec{a.X - b.X, a.Y - b.Y} }
func (a Vec) Mul(k float64) Vec       { return Vec{a.X * k, a.Y * k} }
func (a Vec) Len() float64            { return math.Hypot(a.X, a.Y) }
func lerpVec(a, b Vec, t float64) Vec { return Vec{a.X + (b.X-a.X)*t, a.Y + (b.Y-a.Y)*t} }

// Matrix is an affine transform: a point x, y goes to A·x + C·y + E,
// B·x + D·y + F (Core Graphics' and the web's order).
type Matrix struct{ A, B, C, D, E, F float64 }

// Identity is the transform that changes nothing.
var Identity = Matrix{A: 1, D: 1}

// Mul is m after n: (m·n)(p) = m(n(p)).
func (m Matrix) Mul(n Matrix) Matrix {
	return Matrix{
		A: m.A*n.A + m.C*n.B, B: m.B*n.A + m.D*n.B,
		C: m.A*n.C + m.C*n.D, D: m.B*n.C + m.D*n.D,
		E: m.A*n.E + m.C*n.F + m.E, F: m.B*n.E + m.D*n.F + m.F,
	}
}

func (m Matrix) Apply(p Vec) Vec { return Vec{m.A*p.X + m.C*p.Y + m.E, m.B*p.X + m.D*p.Y + m.F} }

// Inverse is m undone (the identity for a degenerate m).
func (m Matrix) Inverse() Matrix {
	det := m.A*m.D - m.B*m.C
	if det == 0 || math.IsNaN(det) {
		return Identity
	}
	return Matrix{
		A: m.D / det, B: -m.B / det, C: -m.C / det, D: m.A / det,
		E: (m.C*m.F - m.D*m.E) / det, F: (m.B*m.E - m.A*m.F) / det,
	}
}

func translate(x, y float64) Matrix { return Matrix{A: 1, D: 1, E: x, F: y} }
func scale(x, y float64) Matrix     { return Matrix{A: x, D: y} }

// rotate turns clockwise on screen (y down) by degrees.
func rotate(deg float64) Matrix {
	s, c := math.Sincos(deg * math.Pi / 180)
	return Matrix{A: c, B: s, C: -s, D: c}
}

// skew is After Effects' skew of sk degrees along the axis at ax degrees.
func skew(sk, ax float64) Matrix {
	if sk == 0 {
		return Identity
	}
	k := -math.Tan(sk * math.Pi / 180)
	return rotate(-ax).Mul(Matrix{A: 1, C: k, D: 1}).Mul(rotate(ax))
}

// Cubic is a cubic Bézier segment.
type Cubic struct{ P0, P1, P2, P3 Vec }

func (c Cubic) at(t float64) Vec {
	u := 1 - t
	return Vec{
		u*u*u*c.P0.X + 3*u*u*t*c.P1.X + 3*u*t*t*c.P2.X + t*t*t*c.P3.X,
		u*u*u*c.P0.Y + 3*u*u*t*c.P1.Y + 3*u*t*t*c.P2.Y + t*t*t*c.P3.Y,
	}
}

// split cuts c at t: the part before and the part after.
func (c Cubic) split(t float64) (Cubic, Cubic) {
	p01, p12, p23 := lerpVec(c.P0, c.P1, t), lerpVec(c.P1, c.P2, t), lerpVec(c.P2, c.P3, t)
	a, b := lerpVec(p01, p12, t), lerpVec(p12, p23, t)
	m := lerpVec(a, b, t)
	return Cubic{c.P0, p01, a, m}, Cubic{m, b, p23, c.P3}
}

// sub is c from t0 to t1.
func (c Cubic) sub(t0, t1 float64) Cubic {
	if t0 <= 0 && t1 >= 1 {
		return c
	}
	if c.straight() {
		// A line's parameter is its length's fraction (tAt's).
		a, b := lerpVec(c.P0, c.P3, t0), lerpVec(c.P0, c.P3, t1)
		return Cubic{a, a, b, b}
	}
	if t1 < 1 {
		c, _ = c.split(t1)
	}
	if t0 > 0 && t1 > 0 {
		_, c = c.split(t0 / t1)
	}
	return c
}

const lengthSteps = 16

// length is c's arc length, from lengthSteps chords.
func (c Cubic) length() float64 {
	if c.P0 == c.P1 && c.P2 == c.P3 {
		return c.P3.Sub(c.P0).Len()
	}
	l, prev := 0.0, c.P0
	for i := 1; i <= lengthSteps; i++ {
		p := c.at(float64(i) / lengthSteps)
		l += p.Sub(prev).Len()
		prev = p
	}
	return l
}

// tAt is the parameter at distance d along c (of length total).
func (c Cubic) tAt(d, total float64) float64 {
	if total <= 0 || d <= 0 {
		return 0
	}
	if d >= total {
		return 1
	}
	if c.P0 == c.P1 && c.P2 == c.P3 {
		return d / total
	}
	l, prev := 0.0, c.P0
	for i := 1; i <= lengthSteps; i++ {
		t := float64(i) / lengthSteps
		p := c.at(t)
		s := p.Sub(prev).Len()
		if l+s >= d {
			if s == 0 {
				return t
			}
			return t - (1-(d-l)/s)/lengthSteps
		}
		l += s
		prev = p
	}
	return 1
}

// Contour is a connected run of segments, closed or not.
type Contour struct {
	Segs   []Cubic
	Closed bool
}

// Path is an outline: contours drawn and filled together.
type Path struct{ Contours []Contour }

// shapeData is a path as Lottie stores one: vertices, and each one's in
// and out tangents relative to it.
type shapeData struct {
	V, In, Out []Vec
	Closed     bool
}

// contour is d's segments.
func (d *shapeData) contour() Contour {
	n := len(d.V)
	c := Contour{Closed: d.Closed}
	if n == 0 {
		return c
	}
	seg := func(a, b int) Cubic {
		return Cubic{d.V[a], d.V[a].Add(at(d.Out, a)), d.V[b].Add(at(d.In, b)), d.V[b]}
	}
	for i := 0; i+1 < n; i++ {
		c.Segs = append(c.Segs, seg(i, i+1))
	}
	if d.Closed && n > 1 {
		c.Segs = append(c.Segs, seg(n-1, 0))
	}
	if n == 1 {
		c.Segs = append(c.Segs, Cubic{d.V[0], d.V[0], d.V[0], d.V[0]})
	}
	return c
}

func at(v []Vec, i int) Vec {
	if i < len(v) {
		return v[i]
	}
	return Vec{}
}

// transformed is p with each point through m.
func (p Path) transformed(m Matrix) Path {
	if m == Identity {
		return p
	}
	out := Path{Contours: make([]Contour, len(p.Contours))}
	for i, c := range p.Contours {
		segs := make([]Cubic, len(c.Segs))
		for j, s := range c.Segs {
			segs[j] = Cubic{m.Apply(s.P0), m.Apply(s.P1), m.Apply(s.P2), m.Apply(s.P3)}
		}
		out.Contours[i] = Contour{Segs: segs, Closed: c.Closed}
	}
	return out
}

func (c Contour) length() (total float64, lens []float64) {
	lens = make([]float64, len(c.Segs))
	for i, s := range c.Segs {
		lens[i] = s.length()
		total += lens[i]
	}
	return
}

// trimContour is the part of c from distance a to b along it (b > a;
// both within its length, or wrapping around for a closed contour).
func trimContour(c Contour, a, b float64) []Contour {
	total, lens := c.length()
	if total <= 0 {
		return nil
	}
	if b-a >= total-1e-9 {
		return []Contour{c}
	}
	piece := func(from, to float64) Contour {
		out := Contour{}
		pos := 0.0
		for i, s := range c.Segs {
			l := lens[i]
			s0, s1 := pos, pos+l
			pos = s1
			if s1 <= from || s0 >= to || l == 0 {
				continue
			}
			t0, t1 := 0.0, 1.0
			if from > s0 {
				t0 = s.tAt(from-s0, l)
			}
			if to < s1 {
				t1 = s.tAt(to-s0, l)
			}
			out.Segs = append(out.Segs, s.sub(t0, t1))
		}
		return out
	}
	if a < 0 {
		a += total
		b += total
	}
	if b <= total {
		return []Contour{piece(a, b)}
	}
	// Past the end: a closed contour goes on from its start.
	first, second := piece(a, total), piece(0, b-total)
	if c.Closed && len(first.Segs) > 0 && len(second.Segs) > 0 {
		first.Segs = append(first.Segs, second.Segs...)
		return []Contour{first}
	}
	return []Contour{first, second}
}

// trim cuts the contours to start..end (fractions of their length, at
// an offset in turns): each contour on its own (simultaneously), or all
// of them as one run (individually, AE's name for it).
func trim(paths []Path, start, end, offset float64, individually bool) []Path {
	if start > end {
		start, end = end, start
	}
	start, end = start+offset, end+offset
	if end-start >= 1-1e-9 {
		return paths
	}
	if end-start <= 0 {
		return make([]Path, len(paths))
	}
	shift := math.Floor(start)
	start, end = start-shift, end-shift
	if !individually {
		out := make([]Path, len(paths))
		for i, p := range paths {
			for _, c := range p.Contours {
				total, _ := c.length()
				out[i].Contours = append(out[i].Contours, trimContour(c, start*total, end*total)...)
			}
		}
		return out
	}
	// All the contours as one run, in order.
	var total float64
	for _, p := range paths {
		for _, c := range p.Contours {
			l, _ := c.length()
			total += l
		}
	}
	a, b := start*total, end*total
	var ranges [][2]float64
	ranges = append(ranges, [2]float64{a, min(b, total)})
	if b > total {
		ranges = append(ranges, [2]float64{0, b - total})
	}
	out := make([]Path, len(paths))
	pos := 0.0
	for i, p := range paths {
		for _, c := range p.Contours {
			l, _ := c.length()
			for _, r := range ranges {
				from, to := max(r[0], pos), min(r[1], pos+l)
				if to > from {
					out[i].Contours = append(out[i].Contours, trimContour(Contour{Segs: c.Segs}, from-pos, to-pos)...)
				}
			}
			pos += l
		}
	}
	return out
}

func (c Cubic) straight() bool { return c.P0 == c.P1 && c.P2 == c.P3 }

// roundCorners rounds the corners between straight segments of c by
// radius r (no more than half of either segment), as After Effects'
// Round Corners does: a quarter circle's curve where the lines meet.
func roundCorners(c Contour, r float64) Contour {
	n := len(c.Segs)
	if r <= 0 || n < 2 {
		return c
	}
	const k = 1 - 0.5519 // a quarter circle's control points, from its ends toward the corner
	segs := append([]Cubic(nil), c.Segs...)
	type corner struct {
		at   int // the joint after segs[at]
		a, b Vec // where the rounding starts and ends
		v    Vec
	}
	var corners []corner
	joints := n - 1
	if c.Closed {
		joints = n
	}
	for i := 0; i < joints; i++ {
		s, t := c.Segs[i], c.Segs[(i+1)%n]
		if !s.straight() || !t.straight() {
			continue
		}
		v := s.P3
		in, out := s.P0.Sub(v), t.P3.Sub(v)
		li, lo := in.Len(), out.Len()
		if li == 0 || lo == 0 {
			continue
		}
		ri, ro := min(r, li/2), min(r, lo/2)
		corners = append(corners, corner{at: i, a: v.Add(in.Mul(ri / li)), b: v.Add(out.Mul(ro / lo)), v: v})
	}
	if len(corners) == 0 {
		return c
	}
	// Each straight segment shortened at its rounded ends.
	for _, cn := range corners {
		i, j := cn.at, (cn.at+1)%n
		segs[i].P3, segs[i].P2 = cn.a, cn.a
		segs[j].P0, segs[j].P1 = cn.b, cn.b
	}
	out := Contour{Closed: c.Closed}
	ci := 0
	for i := range segs {
		out.Segs = append(out.Segs, segs[i])
		if ci < len(corners) && corners[ci].at == i {
			cn := corners[ci]
			out.Segs = append(out.Segs, Cubic{cn.a, lerpVec(cn.a, cn.v, 1-k), lerpVec(cn.b, cn.v, 1-k), cn.b})
			ci++
		}
	}
	return out
}

// signedArea is a contour's area, positive when it runs clockwise on
// screen (y down), from its flattened outline.
func signedArea(c Contour) float64 {
	area := 0.0
	var prev Vec
	first := true
	var start Vec
	for _, s := range c.Segs {
		for i := 0; i <= 8; i++ {
			p := s.at(float64(i) / 8)
			if first {
				start, prev, first = p, p, false
				continue
			}
			area += prev.X*p.Y - p.X*prev.Y
			prev = p
		}
	}
	area += prev.X*start.Y - start.X*prev.Y
	return area / 2
}

// reversed is c run the other way.
func reversed(c Contour) Contour {
	out := Contour{Closed: c.Closed, Segs: make([]Cubic, len(c.Segs))}
	for i, s := range c.Segs {
		out.Segs[len(c.Segs)-1-i] = Cubic{s.P3, s.P2, s.P1, s.P0}
	}
	return out
}

// clockwise is p with every closed contour running clockwise: filled by
// the nonzero rule, overlapping contours then make their union.
func clockwise(p Path) Path {
	out := Path{Contours: make([]Contour, len(p.Contours))}
	for i, c := range p.Contours {
		if signedArea(c) < 0 {
			c = reversed(c)
		}
		out.Contours[i] = c
	}
	return out
}
