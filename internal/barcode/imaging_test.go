package barcode

import (
	"image"
	"math"
	"math/rand"
)

// Image transforms that make drawn codes look photographed.

func canvas(w, h int, level uint8) *image.Gray {
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = level
	}
	return img
}

// bilinear samples src at (x, y), or reports it's outside.
func bilinear(src *image.Gray, x, y float64) (float64, bool) {
	b := src.Bounds()
	if x < -0.5 || y < -0.5 || x > float64(b.Dx())-0.5 || y > float64(b.Dy())-0.5 {
		return 0, false
	}
	x -= 0.5
	y -= 0.5
	x0, y0 := int(math.Floor(x)), int(math.Floor(y))
	fx, fy := x-float64(x0), y-float64(y0)
	at := func(x, y int) float64 {
		x = max(0, min(x, b.Dx()-1))
		y = max(0, min(y, b.Dy()-1))
		return float64(src.Pix[y*src.Stride+x])
	}
	top := at(x0, y0)*(1-fx) + at(x0+1, y0)*fx
	bot := at(x0, y0+1)*(1-fx) + at(x0+1, y0+1)*fx
	return top*(1-fy) + bot*fy, true
}

// warpOnto draws src onto dst with its corners (top left, top right,
// bottom right, bottom left) at the points given.
func warpOnto(dst, src *image.Gray, corners [4]point) {
	sw, sh := float64(src.Bounds().Dx()), float64(src.Bounds().Dy())
	inv := quadToQuad(corners[0].x, corners[0].y, corners[1].x, corners[1].y, corners[2].x, corners[2].y, corners[3].x, corners[3].y,
		0, 0, sw, 0, sw, sh, 0, sh)
	x0, y0, x1, y1 := boundsOf(corners[:]...)
	b := dst.Bounds()
	for y := max(y0, 0); y < min(y1+1, b.Dy()); y++ {
		for x := max(x0, 0); x < min(x1+1, b.Dx()); x++ {
			// Four samples a pixel, for smooth edges.
			var sum float64
			n := 0
			for _, o := range [4][2]float64{{0.25, 0.25}, {0.75, 0.25}, {0.25, 0.75}, {0.75, 0.75}} {
				sx, sy := inv.apply(float64(x)+o[0], float64(y)+o[1])
				if v, ok := bilinear(src, sx, sy); ok {
					sum += v
				} else {
					sum += float64(dst.Pix[y*dst.Stride+x])
				}
				n++
			}
			dst.Pix[y*dst.Stride+x] = uint8(sum/float64(n) + 0.5)
		}
	}
}

// rotated is src turned by deg degrees, scaled, on a canvas of the
// given margin, background white.
func rotated(src *image.Gray, deg, scale float64, margin int) *image.Gray {
	sw, sh := float64(src.Bounds().Dx())*scale, float64(src.Bounds().Dy())*scale
	r := deg * math.Pi / 180
	c, s := math.Cos(r), math.Sin(r)
	w := int(math.Abs(sw*c)+math.Abs(sh*s)) + 2*margin
	h := int(math.Abs(sw*s)+math.Abs(sh*c)) + 2*margin
	dst := canvas(w, h, 255)
	warpOnto(dst, src, rotatedCorners(sw, sh, deg, float64(w)/2, float64(h)/2))
	return dst
}

// rotatedCorners is the corners of a w by h box turned by deg degrees
// about its center, put at (cx, cy).
func rotatedCorners(w, h, deg, cx, cy float64) [4]point {
	r := deg * math.Pi / 180
	c, s := math.Cos(r), math.Sin(r)
	var out [4]point
	for i, p := range [4]point{{-w / 2, -h / 2}, {w / 2, -h / 2}, {w / 2, h / 2}, {-w / 2, h / 2}} {
		out[i] = point{cx + p.x*c - p.y*s, cy + p.x*s + p.y*c}
	}
	return out
}

// blur is a Gaussian blur of sigma pixels.
func blur(img *image.Gray, sigma float64) *image.Gray {
	r := int(math.Ceil(3 * sigma))
	k := make([]float64, 2*r+1)
	var sum float64
	for i := range k {
		d := float64(i - r)
		k[i] = math.Exp(-d * d / (2 * sigma * sigma))
		sum += k[i]
	}
	for i := range k {
		k[i] /= sum
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	tmp := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var v float64
			for i, kv := range k {
				xx := max(0, min(w-1, x+i-r))
				v += kv * float64(img.Pix[y*img.Stride+xx])
			}
			tmp[y*w+x] = v
		}
	}
	out := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var v float64
			for i, kv := range k {
				yy := max(0, min(h-1, y+i-r))
				v += kv * tmp[yy*w+x]
			}
			out.Pix[y*out.Stride+x] = uint8(v + 0.5)
		}
	}
	return out
}

// noisy adds Gaussian noise of sigma levels.
func noisy(img *image.Gray, sigma float64, seed int64) *image.Gray {
	rng := rand.New(rand.NewSource(seed))
	out := image.NewGray(img.Bounds())
	copy(out.Pix, img.Pix)
	for i, p := range out.Pix {
		v := float64(p) + rng.NormFloat64()*sigma
		out.Pix[i] = uint8(max(0, min(255, v)))
	}
	return out
}

// lit is img under light that falls from full at one corner to share at
// the other, plus a dull contrast: levels squeezed into [lo, hi].
func lit(img *image.Gray, share float64, lo, hi float64) *image.Gray {
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	out := image.NewGray(b)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			f := 1 - (1-share)*(float64(x)/w+float64(y)/h)/2
			v := lo + (hi-lo)*float64(img.Pix[y*img.Stride+x])/255
			out.Pix[y*out.Stride+x] = uint8(v*f + 0.5)
		}
	}
	return out
}

// clutter draws random shapes and text-like strokes on img.
func clutter(img *image.Gray, n int, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	fill := func(x0, y0, x1, y1 int, v uint8, in func(x, y int) bool) {
		for y := max(0, y0); y < min(h, y1); y++ {
			for x := max(0, x0); x < min(w, x1); x++ {
				if in(x, y) {
					img.Pix[y*img.Stride+x] = v
				}
			}
		}
	}
	for i := 0; i < n; i++ {
		v := uint8(rng.Intn(256))
		x, y := rng.Intn(w), rng.Intn(h)
		switch rng.Intn(4) {
		case 0: // a rectangle
			rw, rh := 5+rng.Intn(80), 5+rng.Intn(80)
			fill(x, y, x+rw, y+rh, v, func(int, int) bool { return true })
		case 1: // a disc
			r := 3 + rng.Intn(40)
			fill(x-r, y-r, x+r, y+r, v, func(px, py int) bool { return (px-x)*(px-x)+(py-y)*(py-y) <= r*r })
		case 2: // a line
			a := rng.Float64() * math.Pi
			l := 20 + rng.Float64()*150
			t := 1 + rng.Intn(4)
			for s := 0.0; s < l; s += 0.5 {
				px, py := x+int(s*math.Cos(a)), y+int(s*math.Sin(a))
				fill(px, py, px+t, py+t, v, func(int, int) bool { return true })
			}
		case 3: // a line of text-like glyphs
			gh := 6 + rng.Intn(14)
			for c := 0; c < 3+rng.Intn(20); c++ {
				gx := x + c*gh*2/3
				for s := 0; s < 3; s++ {
					sx, sy := gx+rng.Intn(gh/2+1), y+rng.Intn(gh)
					if rng.Intn(2) == 0 {
						fill(sx, sy, sx+2, sy+gh/2, 0, func(int, int) bool { return true })
					} else {
						fill(sx, sy, sx+gh/2, sy+2, 0, func(int, int) bool { return true })
					}
				}
			}
		}
	}
}

// placed is a cluttered w by h canvas with src drawn in the quad given.
func placed(w, h int, src *image.Gray, corners [4]point, seed int64) *image.Gray {
	dst := canvas(w, h, 230)
	clutter(dst, 60, seed)
	// The code's own paper, a little larger than it.
	warpOnto(dst, src, corners)
	return dst
}
