package barcode

import "math"

// point is a point in an image's pixels.
type point struct{ x, y float64 }

func dist(a, b point) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }

// perspective maps a plane to another: (x, y) to
// ((a11 x + a21 y + a31) / d, (a12 x + a22 y + a32) / d), with
// d = a13 x + a23 y + a33.
type perspective struct {
	a11, a12, a13, a21, a22, a23, a31, a32, a33 float64
}

func (t *perspective) apply(x, y float64) (float64, float64) {
	d := t.a13*x + t.a23*y + t.a33
	return (t.a11*x + t.a21*y + t.a31) / d, (t.a12*x + t.a22*y + t.a32) / d
}

// quadToQuad maps the quadrilateral of the first four points to that of
// the next four, corner to corner.
func quadToQuad(x0, y0, x1, y1, x2, y2, x3, y3, u0, v0, u1, v1, u2, v2, u3, v3 float64) perspective {
	q := squareToQuad(x0, y0, x1, y1, x2, y2, x3, y3).adjoint()
	s := squareToQuad(u0, v0, u1, v1, u2, v2, u3, v3)
	return s.times(&q)
}

// squareToQuad maps the unit square's (0,0), (1,0), (1,1), (0,1) to the
// corners given.
func squareToQuad(x0, y0, x1, y1, x2, y2, x3, y3 float64) perspective {
	dx3 := x0 - x1 + x2 - x3
	dy3 := y0 - y1 + y2 - y3
	if dx3 == 0 && dy3 == 0 {
		return perspective{x1 - x0, y1 - y0, 0, x2 - x1, y2 - y1, 0, x0, y0, 1}
	}
	dx1, dx2 := x1-x2, x3-x2
	dy1, dy2 := y1-y2, y3-y2
	den := dx1*dy2 - dx2*dy1
	a13 := (dx3*dy2 - dx2*dy3) / den
	a23 := (dx1*dy3 - dx3*dy1) / den
	return perspective{
		x1 - x0 + a13*x1, y1 - y0 + a13*y1, a13,
		x3 - x0 + a23*x3, y3 - y0 + a23*y3, a23,
		x0, y0, 1,
	}
}

func (t perspective) adjoint() perspective {
	return perspective{
		a11: t.a22*t.a33 - t.a23*t.a32,
		a21: t.a23*t.a31 - t.a21*t.a33,
		a31: t.a21*t.a32 - t.a22*t.a31,
		a12: t.a13*t.a32 - t.a12*t.a33,
		a22: t.a11*t.a33 - t.a13*t.a31,
		a32: t.a12*t.a31 - t.a11*t.a32,
		a13: t.a12*t.a23 - t.a13*t.a22,
		a23: t.a13*t.a21 - t.a11*t.a23,
		a33: t.a11*t.a22 - t.a12*t.a21,
	}
}

// times is t after o.
func (t *perspective) times(o *perspective) perspective {
	return perspective{
		a11: t.a11*o.a11 + t.a21*o.a12 + t.a31*o.a13,
		a21: t.a11*o.a21 + t.a21*o.a22 + t.a31*o.a23,
		a31: t.a11*o.a31 + t.a21*o.a32 + t.a31*o.a33,
		a12: t.a12*o.a11 + t.a22*o.a12 + t.a32*o.a13,
		a22: t.a12*o.a21 + t.a22*o.a22 + t.a32*o.a23,
		a32: t.a12*o.a31 + t.a22*o.a32 + t.a32*o.a33,
		a13: t.a13*o.a11 + t.a23*o.a12 + t.a33*o.a13,
		a23: t.a13*o.a21 + t.a23*o.a22 + t.a33*o.a23,
		a33: t.a13*o.a31 + t.a23*o.a32 + t.a33*o.a33,
	}
}

// grid is a code's modules, sampled: dark is 1.
type grid struct {
	w, h int
	bits []uint8
}

func newGrid(w, h int) *grid { return &grid{w, h, make([]uint8, w*h)} }

func (g *grid) at(x, y int) bool { return g.bits[y*g.w+x] != 0 }

func (g *grid) set(x, y int, v bool) {
	if v {
		g.bits[y*g.w+x] = 1
	} else {
		g.bits[y*g.w+x] = 0
	}
}

// sampleGrid reads w by h modules of b, module (x, y)'s center being
// t(x+0.5, y+0.5). It fails if too many fall outside the image.
func sampleGrid(b *bitImage, t *perspective, w, h int) (*grid, bool) {
	g := newGrid(w, h)
	outside := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			px, py := t.apply(float64(x)+0.5, float64(y)+0.5)
			ix, iy := int(math.Floor(px)), int(math.Floor(py))
			if ix < -1 || iy < -1 || ix > b.w || iy > b.h || math.IsNaN(px) || math.IsNaN(py) {
				return nil, false
			}
			if ix < 0 || iy < 0 || ix >= b.w || iy >= b.h {
				// A pixel or so out: nudged in, as ZXing does.
				outside++
				ix = max(0, min(ix, b.w-1))
				iy = max(0, min(iy, b.h-1))
			}
			g.bits[y*w+x] = b.pix[iy*b.w+ix]
		}
	}
	return g, outside <= w+h
}

// boundsOf is the box around the corners given, in whole pixels.
func boundsOf(ps ...point) (x0, y0, x1, y1 int) {
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, p := range ps {
		minX, maxX = math.Min(minX, p.x), math.Max(maxX, p.x)
		minY, maxY = math.Min(minY, p.y), math.Max(maxY, p.y)
	}
	return int(math.Floor(minX)), int(math.Floor(minY)), int(math.Ceil(maxX)), int(math.Ceil(maxY))
}
