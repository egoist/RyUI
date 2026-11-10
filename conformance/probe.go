package conformance

import (
	"fmt"
	"math"
)

// RGB is a pixel's color, each channel 0 to 255.
type RGB struct{ R, G, B float64 }

var (
	white = RGB{255, 255, 255}
	red   = RGB{255, 0, 0}
	blue  = RGB{0, 0, 255}
)

func (c RGB) String() string { return fmt.Sprintf("(%.0f,%.0f,%.0f)", c.R, c.G, c.B) }

// Mix is how far c is from a toward b, along the line between them: 0 at
// a, 1 at b. An edge's pixel mixes the colors on its sides by how much
// of it each covers.
func (c RGB) Mix(a, b RGB) float64 {
	dr, dg, db := b.R-a.R, b.G-a.G, b.B-a.B
	return ((c.R-a.R)*dr + (c.G-a.G)*dg + (c.B-a.B)*db) / (dr*dr + dg*dg + db*db)
}

// Darkness is how much a shadow over white darkens c: 0 for white, 1
// for black.
func (c RGB) Darkness() float64 { return 1 - (c.R+c.G+c.B)/(3*255) }

// Gray is whether c is a gray (a black shadow over white), not a color.
func (c RGB) Gray() bool {
	return math.Abs(c.R-c.G) < 24 && math.Abs(c.G-c.B) < 24 && math.Abs(c.R-c.B) < 24
}

// Probe measures one case's cell. Points are the cell's, from its top
// left, as the case's scene places its views.
type Probe struct {
	s        *sheet
	x, y     float64 // the cell's origin in the sheet, in points
	lacks    map[Feature]string
	errors   []string
	measured []string
}

// Lacks is whether the host documents that it lacks f: a case that
// needs f only partly checks what holds without it.
func (p *Probe) Lacks(f Feature) bool {
	_, ok := p.lacks[f]
	return ok
}

// Scale is the capture's pixels per point.
func (p *Probe) Scale() float64 { return p.s.sx }

// Px is one device pixel, in points: the tolerance of an edge's place.
func (p *Probe) Px() float64 { return 1 / p.s.sx }

// Errorf reports what's off.
func (p *Probe) Errorf(format string, args ...any) {
	p.errors = append(p.errors, fmt.Sprintf(format, args...))
}

// Logf records what was measured.
func (p *Probe) Logf(format string, args ...any) {
	p.measured = append(p.measured, fmt.Sprintf(format, args...))
}

// pixel is the pixel's color at pixel coordinates (clamped to the
// capture).
func (p *Probe) pixel(px, py int) RGB {
	px = min(max(px, p.s.img.Bounds().Min.X), p.s.maxX)
	py = min(max(py, p.s.img.Bounds().Min.Y), p.s.maxY)
	r, g, b, a := p.s.img.At(px, py).RGBA()
	if a == 0 {
		return RGB{}
	}
	// Straight, not premultiplied: captures are opaque, but be safe.
	k := 255 / float64(a)
	return RGB{float64(r) * k, float64(g) * k, float64(b) * k}
}

// toPx is a point of the cell in the capture's pixels.
func (p *Probe) toPx(x, y float64) (float64, float64) {
	return p.s.ox + (p.x+x)*p.s.sx, p.s.oy + (p.y+y)*p.s.sy
}

// At is the color of the pixel at the point (x, y) of the cell.
func (p *Probe) At(x, y float64) RGB {
	px, py := p.toPx(x, y)
	return p.pixel(int(math.Floor(px)), int(math.Floor(py)))
}

// sample is a pixel's value along a line and where its center is.
type sample struct {
	at float64 // along the line, in the cell's points
	v  float64
}

// Profile samples f of every pixel the line from (x0, y0) to (x1, y1)
// crosses (across or down, one of them fixed), each at its center.
func (p *Probe) Profile(x0, y0, x1, y1 float64, f func(RGB) float64) []sample {
	var out []sample
	if y0 == y1 {
		_, py := p.toPx(0, y0)
		a, _ := p.toPx(x0, 0)
		b, _ := p.toPx(x1, 0)
		step := 1.0
		if b < a {
			step = -1
		}
		for px := math.Floor(a); (b-px)*step >= 0; px += step {
			out = append(out, sample{(px+0.5-p.s.ox)/p.s.sx - p.x, f(p.pixel(int(px), int(math.Floor(py))))})
		}
		return out
	}
	px, _ := p.toPx(x0, 0)
	_, a := p.toPx(0, y0)
	_, b := p.toPx(0, y1)
	step := 1.0
	if b < a {
		step = -1
	}
	for py := math.Floor(a); (b-py)*step >= 0; py += step {
		out = append(out, sample{(py+0.5-p.s.oy)/p.s.sy - p.y, f(p.pixel(int(math.Floor(px)), int(py)))})
	}
	return out
}

// Crossing is where, along a profile, the value first crosses level,
// between the two pixels' centers on either side of it; ok is false if
// it never does.
func Crossing(s []sample, level float64) (at float64, ok bool) {
	for i := 1; i < len(s); i++ {
		a, b := s[i-1], s[i]
		if (a.v-level)*(b.v-level) <= 0 && a.v != b.v {
			t := (level - a.v) / (b.v - a.v)
			return a.at + t*(b.at-a.at), true
		}
	}
	return 0, false
}

// Edge checks that the edge between colors a and b, along the line from
// (x0, y0) to (x1, y1), is at want, to a device pixel.
func (p *Probe) Edge(what string, x0, y0, x1, y1 float64, a, b RGB, want float64) {
	got, ok := Crossing(p.Profile(x0, y0, x1, y1, func(c RGB) float64 { return c.Mix(a, b) }), 0.5)
	if !ok {
		p.Errorf("%s: no edge between (%g,%g) and (%g,%g), want one at %g", what, x0, y0, x1, y1, want)
		return
	}
	if math.Abs(got-want) > p.Px()+0.05 {
		p.Errorf("%s at %.2f, want %g", what, got, want)
	}
}

// Mix checks that the pixel at (x, y) is between a and b by want (0: a,
// 1: b), within tol.
func (p *Probe) Mix(what string, x, y float64, a, b RGB, want, tol float64) float64 {
	c := p.At(x, y)
	got := c.Mix(a, b)
	if math.Abs(got-want) > tol {
		p.Errorf("%s: %v at (%g,%g) is %.2f of the way from %v to %v, want %.2f", what, c, x, y, got, a, b, want)
	}
	return got
}

// Blur measures a shadow's edge along the profile from inside its
// solid middle out, past its edge: where it is (half the plateau's
// darkness) and its Gaussian σ (half the distance between the
// levels a Gaussian's edge passes one σ inside and outside it).
func (p *Probe) Blur(x0, y0, x1, y1 float64) (edge, sigma, plateau float64, ok bool) {
	s := p.Profile(x0, y0, x1, y1, RGB.Darkness)
	if len(s) == 0 {
		return 0, 0, 0, false
	}
	plateau = s[0].v
	if plateau < 0.05 {
		return 0, 0, plateau, false
	}
	// 0.5·erfc(±1/√2): a Gaussian-blurred edge's coverage one σ in and out.
	const in, out = 0.8413447, 0.1586553
	e, ok1 := Crossing(s, plateau*0.5)
	a, ok2 := Crossing(s, plateau*in)
	b, ok3 := Crossing(s, plateau*out)
	if !ok1 || !ok2 || !ok3 {
		return 0, 0, plateau, false
	}
	return e, math.Abs(b-a) / 2, plateau, true
}
