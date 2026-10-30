package svg

import (
	"math"
	"strconv"
	"strings"

	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/ui"
)

// Path is a layer's outline in viewBox units, transforms applied: per
// segment an ops.PathOp and its arguments, as ui.Path has them.
type Path struct {
	cmds []ops.PathOp
	args []float32
}

func nargs(op ops.PathOp) int {
	switch op {
	case ops.PathOpMove, ops.PathOpLine:
		return 2
	case ops.PathOpQuad, ops.PathOpRect, ops.PathOpEllipse:
		return 4
	case ops.PathOpRoundedRect:
		return 5
	case ops.PathOpCubic:
		return 6
	}
	return 0
}

// Empty reports whether the path has no segments.
func (p *Path) Empty() bool { return len(p.cmds) == 0 }

// Append adds the path to dst, scaled by scale and moved by dx, dy (see
// Icon.Fit), and returns dst.
func (p *Path) Append(dst *ui.Path, scale, dx, dy float32) *ui.Path {
	a := p.args
	x := func(i int) float32 { return a[i]*scale + dx }
	y := func(i int) float32 { return a[i]*scale + dy }
	for _, op := range p.cmds {
		switch op {
		case ops.PathOpMove:
			dst.MoveTo(x(0), y(1))
		case ops.PathOpLine:
			dst.LineTo(x(0), y(1))
		case ops.PathOpQuad:
			dst.QuadTo(x(0), y(1), x(2), y(3))
		case ops.PathOpCubic:
			dst.CubicTo(x(0), y(1), x(2), y(3), x(4), y(5))
		case ops.PathOpClose:
			dst.Close()
		case ops.PathOpRect:
			dst.Rect(x(0), y(1), a[2]*scale, a[3]*scale)
		case ops.PathOpRoundedRect:
			dst.RoundedRect(x(0), y(1), a[2]*scale, a[3]*scale, a[4]*scale)
		case ops.PathOpEllipse:
			dst.Ellipse(x(0), y(1), a[2]*scale, a[3]*scale)
		}
		a = a[nargs(op):]
	}
	return dst
}

// String writes the path as text, for tests and debugging: "M x y",
// "L x y", "Q", "C" and "Z" as in SVG path data (absolute), and "Rect x
// y w h", "RoundedRect x y w h r", "Ellipse x y w h"; numbers rounded to
// three decimals.
func (p *Path) String() string {
	var b strings.Builder
	a := p.args
	for i, op := range p.cmds {
		if i > 0 {
			b.WriteByte(' ')
		}
		switch op {
		case ops.PathOpMove:
			b.WriteString("M")
		case ops.PathOpLine:
			b.WriteString("L")
		case ops.PathOpQuad:
			b.WriteString("Q")
		case ops.PathOpCubic:
			b.WriteString("C")
		case ops.PathOpClose:
			b.WriteString("Z")
		case ops.PathOpRect:
			b.WriteString("Rect")
		case ops.PathOpRoundedRect:
			b.WriteString("RoundedRect")
		case ops.PathOpEllipse:
			b.WriteString("Ellipse")
		}
		n := nargs(op)
		for _, v := range a[:n] {
			b.WriteByte(' ')
			b.WriteString(fmtNum(v))
		}
		a = a[n:]
	}
	return b.String()
}

func fmtNum(v float32) string {
	r := math.Round(float64(v)*1000) / 1000
	if r == 0 {
		r = 0 // no "-0"
	}
	return strconv.FormatFloat(r, 'f', -1, 64)
}

// bounds grows box (minX, minY, maxX, maxY) by the path's points.
func (p *Path) bounds(box *[4]float64, seen *bool) {
	add := func(x, y float32) {
		if !*seen {
			*box = [4]float64{float64(x), float64(y), float64(x), float64(y)}
			*seen = true
			return
		}
		box[0], box[1] = min(box[0], float64(x)), min(box[1], float64(y))
		box[2], box[3] = max(box[2], float64(x)), max(box[3], float64(y))
	}
	a := p.args
	for _, op := range p.cmds {
		n := nargs(op)
		switch op {
		case ops.PathOpRect, ops.PathOpRoundedRect, ops.PathOpEllipse:
			add(a[0], a[1])
			add(a[0]+a[2], a[1]+a[3])
		default:
			for i := 0; i+1 < n; i += 2 {
				add(a[i], a[i+1])
			}
		}
		a = a[n:]
	}
}

// matrix is an affine transform: x' = a x + c y + e, y' = b x + d y + f.
type matrix [6]float64

var identity = matrix{1, 0, 0, 1, 0, 0}

func (m matrix) apply(x, y float64) (float64, float64) {
	return m[0]*x + m[2]*y + m[4], m[1]*x + m[3]*y + m[5]
}

// mul is m then n's parent: the transform that applies n, then m.
func (m matrix) mul(n matrix) matrix {
	return matrix{
		m[0]*n[0] + m[2]*n[1],
		m[1]*n[0] + m[3]*n[1],
		m[0]*n[2] + m[2]*n[3],
		m[1]*n[2] + m[3]*n[3],
		m[0]*n[4] + m[2]*n[5] + m[4],
		m[1]*n[4] + m[3]*n[5] + m[5],
	}
}

// axisAligned reports whether m keeps rectangles rectangles: no
// rotation or skew.
func (m matrix) axisAligned() bool { return m[1] == 0 && m[2] == 0 }

// scale is how much m scales lengths, on average (a stroke's width).
func (m matrix) scale() float64 { return math.Sqrt(math.Abs(m[0]*m[3] - m[1]*m[2])) }

// builder adds segments in user units, through a transform.
type builder struct {
	p *Path
	m matrix
}

func (b *builder) pt(x, y float64) {
	x, y = b.m.apply(x, y)
	b.p.args = append(b.p.args, float32(x), float32(y))
}

func (b *builder) moveTo(x, y float64) { b.p.cmds = append(b.p.cmds, ops.PathOpMove); b.pt(x, y) }
func (b *builder) lineTo(x, y float64) { b.p.cmds = append(b.p.cmds, ops.PathOpLine); b.pt(x, y) }
func (b *builder) close()              { b.p.cmds = append(b.p.cmds, ops.PathOpClose) }

func (b *builder) quadTo(cx, cy, x, y float64) {
	b.p.cmds = append(b.p.cmds, ops.PathOpQuad)
	b.pt(cx, cy)
	b.pt(x, y)
}

func (b *builder) cubicTo(c1x, c1y, c2x, c2y, x, y float64) {
	b.p.cmds = append(b.p.cmds, ops.PathOpCubic)
	b.pt(c1x, c1y)
	b.pt(c2x, c2y)
	b.pt(x, y)
}

// kappa places a cubic's control points to draw a quarter circle.
const kappa = 0.5522847498307936

// rect adds a rectangle with corners of radii rx, ry (clamped): as the
// host's own Rect or RoundedRect when the transform allows, else as
// lines and curves.
func (b *builder) rect(x, y, w, h, rx, ry float64) {
	rx, ry = min(rx, w/2), min(ry, h/2)
	if b.m.axisAligned() {
		sx, sy := math.Abs(b.m[0]), math.Abs(b.m[3])
		x0, y0 := b.m.apply(x, y)
		x1, y1 := b.m.apply(x+w, y+h)
		x0, x1 = min(x0, x1), max(x0, x1)
		y0, y1 = min(y0, y1), max(y0, y1)
		if rx <= 0 || ry <= 0 {
			b.p.cmds = append(b.p.cmds, ops.PathOpRect)
			b.p.args = append(b.p.args, float32(x0), float32(y0), float32(x1-x0), float32(y1-y0))
			return
		}
		if r := rx * sx; math.Abs(r-ry*sy) < 1e-6*max(r, 1) {
			b.p.cmds = append(b.p.cmds, ops.PathOpRoundedRect)
			b.p.args = append(b.p.args, float32(x0), float32(y0), float32(x1-x0), float32(y1-y0), float32(r))
			return
		}
	}
	if rx <= 0 || ry <= 0 {
		b.moveTo(x, y)
		b.lineTo(x+w, y)
		b.lineTo(x+w, y+h)
		b.lineTo(x, y+h)
		b.close()
		return
	}
	// The sides, but not those the corners use up (a pill's).
	kx, ky := rx*kappa, ry*kappa
	side := 2*rx < w
	end := 2*ry < h
	b.moveTo(x+rx, y)
	if side {
		b.lineTo(x+w-rx, y)
	}
	b.cubicTo(x+w-rx+kx, y, x+w, y+ry-ky, x+w, y+ry)
	if end {
		b.lineTo(x+w, y+h-ry)
	}
	b.cubicTo(x+w, y+h-ry+ky, x+w-rx+kx, y+h, x+w-rx, y+h)
	if side {
		b.lineTo(x+rx, y+h)
	}
	b.cubicTo(x+rx-kx, y+h, x, y+h-ry+ky, x, y+h-ry)
	if end {
		b.lineTo(x, y+ry)
	}
	b.cubicTo(x, y+ry-ky, x+rx-kx, y, x+rx, y)
	b.close()
}

// ellipse adds the ellipse at cx, cy of radii rx, ry: as the host's own
// Ellipse when the transform allows, else as four curves.
func (b *builder) ellipse(cx, cy, rx, ry float64) {
	if b.m.axisAligned() {
		x0, y0 := b.m.apply(cx-rx, cy-ry)
		x1, y1 := b.m.apply(cx+rx, cy+ry)
		x0, x1 = min(x0, x1), max(x0, x1)
		y0, y1 = min(y0, y1), max(y0, y1)
		b.p.cmds = append(b.p.cmds, ops.PathOpEllipse)
		b.p.args = append(b.p.args, float32(x0), float32(y0), float32(x1-x0), float32(y1-y0))
		return
	}
	kx, ky := rx*kappa, ry*kappa
	b.moveTo(cx+rx, cy)
	b.cubicTo(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry)
	b.cubicTo(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy)
	b.cubicTo(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry)
	b.cubicTo(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy)
	b.close()
}

// arc adds the SVG elliptical arc from x1, y1 to x2, y2 as cubics: the
// endpoint-to-center conversion of SVG 1.1's implementation notes
// (F.6.5, F.6.6), then a curve per quarter turn or less.
func (b *builder) arc(x1, y1, rx, ry, phi float64, large, sweep bool, x2, y2 float64) {
	if x1 == x2 && y1 == y2 {
		return
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		b.lineTo(x2, y2)
		return
	}
	sin, cos := math.Sincos(phi * math.Pi / 180)
	dx, dy := (x1-x2)/2, (y1-y2)/2
	x1p := cos*dx + sin*dy
	y1p := -sin*dx + cos*dy
	if l := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry); l > 1 {
		s := math.Sqrt(l)
		rx, ry = rx*s, ry*s
	}
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	co := math.Sqrt(max(0, num/den))
	if large == sweep {
		co = -co
	}
	cxp, cyp := co*rx*y1p/ry, -co*ry*x1p/rx
	cx := cos*cxp - sin*cyp + (x1+x2)/2
	cy := sin*cxp + cos*cyp + (y1+y2)/2
	ux, uy := (x1p-cxp)/rx, (y1p-cyp)/ry
	vx, vy := (-x1p-cxp)/rx, (-y1p-cyp)/ry
	t1 := math.Atan2(uy, ux)
	dt := math.Atan2(ux*vy-uy*vx, ux*vx+uy*vy)
	if !sweep && dt > 0 {
		dt -= 2 * math.Pi
	} else if sweep && dt < 0 {
		dt += 2 * math.Pi
	}
	n := int(math.Ceil(math.Abs(dt)/(math.Pi/2) - 1e-9))
	if n < 1 {
		n = 1
	}
	d := dt / float64(n)
	k := 4.0 / 3 * math.Tan(d/4)
	// on maps a point of the unit circle onto the ellipse.
	on := func(u, v float64) (float64, float64) {
		return cx + rx*cos*u - ry*sin*v, cy + rx*sin*u + ry*cos*v
	}
	for i := range n {
		a, z := t1+float64(i)*d, t1+float64(i+1)*d
		sa, ca := math.Sincos(a)
		sz, cz := math.Sincos(z)
		c1x, c1y := on(ca-k*sa, sa+k*ca)
		c2x, c2y := on(cz+k*sz, sz-k*cz)
		ex, ey := on(cz, sz)
		if i == n-1 {
			ex, ey = x2, y2 // exactly where it was asked to end
		}
		b.cubicTo(c1x, c1y, c2x, c2y, ex, ey)
	}
}

// scanner reads path data and point lists.
type scanner struct {
	s string
	i int
}

func (s *scanner) skip() {
	for s.i < len(s.s) {
		switch s.s[s.i] {
		case ' ', '\t', '\n', '\r', '\f', ',':
			s.i++
		default:
			return
		}
	}
}

// num reads a number: "1.5.5" is 1.5 then .5, "1e-3" one number, "2-1"
// two.
func (s *scanner) num() (float64, bool) {
	s.skip()
	start, i := s.i, s.i
	if i < len(s.s) && (s.s[i] == '+' || s.s[i] == '-') {
		i++
	}
	digits := false
	for i < len(s.s) && s.s[i] >= '0' && s.s[i] <= '9' {
		i++
		digits = true
	}
	if i < len(s.s) && s.s[i] == '.' {
		i++
		for i < len(s.s) && s.s[i] >= '0' && s.s[i] <= '9' {
			i++
			digits = true
		}
	}
	if !digits {
		return 0, false
	}
	if i < len(s.s) && (s.s[i] == 'e' || s.s[i] == 'E') {
		j := i + 1
		if j < len(s.s) && (s.s[j] == '+' || s.s[j] == '-') {
			j++
		}
		if j < len(s.s) && s.s[j] >= '0' && s.s[j] <= '9' {
			for j < len(s.s) && s.s[j] >= '0' && s.s[j] <= '9' {
				j++
			}
			i = j
		}
	}
	v, err := strconv.ParseFloat(s.s[start:i], 64)
	if err != nil {
		return 0, false
	}
	s.i = i
	return v, true
}

// flag reads an arc flag: a single 0 or 1, separators optional
// ("a1 1 0 00.5.5").
func (s *scanner) flag() (bool, bool) {
	s.skip()
	if s.i < len(s.s) && (s.s[s.i] == '0' || s.s[s.i] == '1') {
		s.i++
		return s.s[s.i-1] == '1', true
	}
	return false, false
}

func (s *scanner) nums(dst []float64) bool {
	for i := range dst {
		v, ok := s.num()
		if !ok {
			return false
		}
		dst[i] = v
	}
	return true
}

// pathData adds SVG path data. As SVG says, what comes before an error
// is drawn and the rest isn't.
func (b *builder) pathData(d string) {
	s := scanner{s: d}
	var cx, cy, sx, sy float64 // the current point, the subpath's start
	var lcx, lcy float64       // the last control point, for S and T
	var cmd, prev byte
	var a [7]float64
	started := false
	for {
		s.skip()
		if s.i >= len(s.s) {
			return
		}
		if c := s.s[s.i]; c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			cmd = c
			s.i++
		} else if cmd == 0 {
			return
		}
		rel := cmd >= 'a'
		up := cmd &^ 0x20
		if up == 'Z' {
			b.close()
			cx, cy = sx, sy
			prev, cmd = 'Z', 0 // a number after Z is an error
			continue
		}
		if !started && up != 'M' {
			return
		}
		var ox, oy float64
		if rel {
			ox, oy = cx, cy
		}
		switch up {
		case 'M':
			if !s.nums(a[:2]) {
				return
			}
			cx, cy = ox+a[0], oy+a[1]
			sx, sy = cx, cy
			b.moveTo(cx, cy)
			started = true
			// Coordinates after the first are lines.
			if rel {
				cmd = 'l'
			} else {
				cmd = 'L'
			}
		case 'L':
			if !s.nums(a[:2]) {
				return
			}
			cx, cy = ox+a[0], oy+a[1]
			b.lineTo(cx, cy)
		case 'H':
			if !s.nums(a[:1]) {
				return
			}
			cx = ox + a[0]
			b.lineTo(cx, cy)
		case 'V':
			if !s.nums(a[:1]) {
				return
			}
			cy = oy + a[0]
			b.lineTo(cx, cy)
		case 'C':
			if !s.nums(a[:6]) {
				return
			}
			b.cubicTo(ox+a[0], oy+a[1], ox+a[2], oy+a[3], ox+a[4], oy+a[5])
			lcx, lcy = ox+a[2], oy+a[3]
			cx, cy = ox+a[4], oy+a[5]
		case 'S':
			if !s.nums(a[:4]) {
				return
			}
			c1x, c1y := cx, cy
			if prev == 'C' || prev == 'S' {
				c1x, c1y = 2*cx-lcx, 2*cy-lcy
			}
			b.cubicTo(c1x, c1y, ox+a[0], oy+a[1], ox+a[2], oy+a[3])
			lcx, lcy = ox+a[0], oy+a[1]
			cx, cy = ox+a[2], oy+a[3]
		case 'Q':
			if !s.nums(a[:4]) {
				return
			}
			b.quadTo(ox+a[0], oy+a[1], ox+a[2], oy+a[3])
			lcx, lcy = ox+a[0], oy+a[1]
			cx, cy = ox+a[2], oy+a[3]
		case 'T':
			if !s.nums(a[:2]) {
				return
			}
			qx, qy := cx, cy
			if prev == 'Q' || prev == 'T' {
				qx, qy = 2*cx-lcx, 2*cy-lcy
			}
			b.quadTo(qx, qy, ox+a[0], oy+a[1])
			lcx, lcy = qx, qy
			cx, cy = ox+a[0], oy+a[1]
		case 'A':
			if !s.nums(a[:3]) {
				return
			}
			large, ok1 := s.flag()
			sweep, ok2 := s.flag()
			if !ok1 || !ok2 || !s.nums(a[3:5]) {
				return
			}
			x2, y2 := ox+a[3], oy+a[4]
			b.arc(cx, cy, a[0], a[1], a[2], large, sweep, x2, y2)
			cx, cy = x2, y2
		default:
			return
		}
		prev = up
	}
}

// points adds a polyline or polygon's points.
func (b *builder) points(list string, closed bool) {
	s := scanner{s: list}
	var a [2]float64
	for n := 0; s.nums(a[:]); n++ {
		if n == 0 {
			b.moveTo(a[0], a[1])
		} else {
			b.lineTo(a[0], a[1])
		}
	}
	if closed && len(b.p.cmds) > 0 {
		b.close()
	}
}
