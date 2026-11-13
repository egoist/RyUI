package barcode

import (
	"image"
	"image/color"
	"image/draw"
	"math"
	"math/rand"
	"testing"
)

func TestNoFalsePositives(t *testing.T) {
	for seed := int64(0); seed < 6; seed++ {
		rng := rand.New(rand.NewSource(seed))
		// Random noise, coarse and fine.
		for _, cell := range []int{1, 2, 3, 5} {
			img := image.NewGray(image.Rect(0, 0, 640, 480))
			for y := 0; y < 480; y += cell {
				for x := 0; x < 640; x += cell {
					v := uint8(rng.Intn(256))
					if seed%2 == 0 {
						v = uint8(rng.Intn(2) * 255)
					}
					for yy := y; yy < min(y+cell, 480); yy++ {
						for xx := x; xx < min(x+cell, 640); xx++ {
							img.Pix[yy*img.Stride+xx] = v
						}
					}
				}
			}
			if codes := Decode(img); len(codes) != 0 {
				t.Errorf("noise %d/%d: %v", seed, cell, codes)
			}
		}
		// A cluttered scene, sharp and blurred.
		img := canvas(1280, 720, uint8(150+rng.Intn(100)))
		clutter(img, 400, seed)
		if codes := Decode(img); len(codes) != 0 {
			t.Errorf("clutter %d: %v", seed, codes)
		}
		if codes := Decode(noisy(blur(img, 1), 5, seed)); len(codes) != 0 {
			t.Errorf("blurred clutter %d: %v", seed, codes)
		}
		// Stripes: a fence, a shirt, a grille.
		stripes := canvas(800, 600, 255)
		for y := 0; y < 600; y++ {
			for x := 0; x < 800; x++ {
				p := math.Sin(float64(x)*0.3+float64(y)*0.05+float64(seed)) + 0.6*math.Sin(float64(x)*1.1*float64(seed+1)/3)
				if p > 0 {
					stripes.Pix[y*stripes.Stride+x] = 20
				}
			}
		}
		if codes := Decode(stripes); len(codes) != 0 {
			t.Errorf("stripes %d: %v", seed, codes)
		}
	}
	// Flat and empty images.
	for _, img := range []image.Image{canvas(320, 240, 0), canvas(320, 240, 255), canvas(1, 1, 0), image.NewGray(image.Rect(0, 0, 0, 0))} {
		if codes := Decode(img); len(codes) != 0 {
			t.Errorf("flat: %v", codes)
		}
	}
}

func TestImageTypes(t *testing.T) {
	g := qrEncode([]qrSeg{{mode: 4, data: "types"}}, 2, ecM, 2)
	gray := rotated(gridImage(g, 4, 4), 10, 1, 30)
	b := gray.Bounds()
	// Placed off the origin, to check bounds are the image's own.
	off := image.Pt(37, 23)
	r := b.Add(off)
	nrgba := image.NewNRGBA(r)
	rgba := image.NewRGBA(r)
	ycc := image.NewYCbCr(r, image.YCbCrSubsampleRatio420)
	pal := image.NewPaletted(r, color.Palette{color.Black, color.White, color.Gray{128}})
	g16 := image.NewGray16(r)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			v := gray.Pix[y*gray.Stride+x]
			c := color.NRGBA{v, v, v, 255}
			nrgba.Set(x+off.X, y+off.Y, c)
			rgba.Set(x+off.X, y+off.Y, c)
			pal.Set(x+off.X, y+off.Y, c)
			g16.Set(x+off.X, y+off.Y, c)
			ycc.Y[ycc.YOffset(x+off.X, y+off.Y)] = v
			co := ycc.COffset(x+off.X, y+off.Y)
			ycc.Cb[co], ycc.Cr[co] = 128, 128
		}
	}
	// Transparent around it: composed over white.
	clear := image.NewNRGBA(image.Rect(0, 0, b.Dx()+40, b.Dy()+40))
	draw.Draw(clear, b.Add(image.Pt(20, 20)), gray, image.Point{}, draw.Src)
	for i := 3; i < len(clear.Pix); i += 4 {
		if clear.Pix[i-1] > 200 {
			clear.Pix[i] = 0
			clear.Pix[i-1], clear.Pix[i-2], clear.Pix[i-3] = 0, 0, 0
		}
	}
	sub := image.NewGray(image.Rect(0, 0, b.Dx()+100, b.Dy()+100))
	draw.Draw(sub, sub.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(sub, b.Add(image.Pt(50, 50)), gray, image.Point{}, draw.Src)
	cases := map[string]image.Image{
		"nrgba": nrgba, "rgba": rgba, "ycbcr": ycc, "paletted": pal, "gray16": g16,
		"transparent": clear, "subimage": sub.SubImage(image.Rect(30, 30, b.Dx()+80, b.Dy()+80)),
	}
	want := map[string]image.Point{"transparent": {20, 20}, "subimage": {50, 50}}
	for name, img := range cases {
		t.Run(name, func(t *testing.T) {
			codes := expectOne(t, img, QR, "types", QR)
			if len(codes) != 1 {
				return
			}
			o, ok := want[name]
			if !ok {
				o = off
			}
			// The code's own box: 100 pixels turned by 10°, in the middle.
			c := codes[0].Bounds
			cx, cy := (c.Min.X+c.Max.X)/2-o.X, (c.Min.Y+c.Max.Y)/2-o.Y
			side := 100 * (math.Cos(math.Pi/18) + math.Sin(math.Pi/18))
			if abs(cx-b.Dx()/2) > 3 || abs(cy-b.Dy()/2) > 3 || math.Abs(float64(c.Dx())-side) > 4 || math.Abs(float64(c.Dy())-side) > 4 {
				t.Errorf("bounds %v (offset %v), want %.0f square in the middle of %v", c, o, side, b)
			}
		})
	}
}

func TestDedupe(t *testing.T) {
	// One code read by many lines and both finders' scans: once.
	img := barsImage(code128Modules(code128B("once")), 3, 200, 12)
	if codes := Decode(img); len(codes) != 1 {
		t.Errorf("got %v", codes)
	}
	g := qrEncode([]qrSeg{{mode: 4, data: "once"}}, 1, ecL, 0)
	if codes := Decode(gridImage(g, 12, 4)); len(codes) != 1 {
		t.Errorf("got %v", codes)
	}
}

// frame is a 1280 by 720 camera-like frame: a cluttered desk, and a QR
// code on it if text isn't empty.
func frame(text string) *image.YCbCr {
	img := canvas(1280, 720, 200)
	clutter(img, 150, 42)
	if text != "" {
		q := gridImage(qrEncode([]qrSeg{{mode: 4, data: text}}, 4, ecM, 1), 5, 4)
		warpOnto(img, q, rotatedCorners(float64(q.Bounds().Dx()), float64(q.Bounds().Dy()), 12, 640, 360))
	}
	img = noisy(blur(img, 0.8), 4, 1)
	ycc := image.NewYCbCr(img.Bounds(), image.YCbCrSubsampleRatio420)
	copy(ycc.Y, img.Pix)
	return ycc
}

func TestFrame(t *testing.T) {
	expectOne(t, frame("https://example.com/frame"), QR, "https://example.com/frame")
	if codes := Decode(frame("")); len(codes) != 0 {
		t.Errorf("empty frame: %v", codes)
	}
}

func BenchmarkDecode(b *testing.B) {
	for _, c := range []struct{ name, text string }{{"empty-1280x720", ""}, {"qr-1280x720", "https://example.com/frame"}} {
		img := frame(c.text)
		b.Run(c.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				Decode(img)
			}
		})
	}
	small := image.NewYCbCr(image.Rect(0, 0, 640, 480), image.YCbCrSubsampleRatio420)
	f := frame("")
	for y := 0; y < 480; y++ {
		copy(small.Y[y*small.YStride:], f.Y[y*f.YStride:y*f.YStride+640])
	}
	b.Run("empty-640x480", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			Decode(small)
		}
	})
}
