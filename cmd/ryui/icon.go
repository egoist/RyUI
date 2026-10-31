package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// loadIcon reads a square PNG icon.
func loadIcon(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("icon %s: %v", path, err)
	}
	b := img.Bounds()
	if b.Dx() != b.Dy() {
		return nil, fmt.Errorf("icon %s is %d×%d: it should be square (1024×1024)", path, b.Dx(), b.Dy())
	}
	if b.Dx() < 512 {
		fmt.Printf("ryui: icon %s is %d×%d; 1024×1024 looks sharp everywhere\n", path, b.Dx(), b.Dy())
	}
	n := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(n, n.Bounds(), img, b.Min, draw.Src)
	return n, nil
}

// resize scales a square image to size×size, averaging the source area
// each pixel covers (in premultiplied alpha, so edges don't darken).
func resize(src *image.NRGBA, size int) *image.NRGBA {
	sw := src.Bounds().Dx()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	scale := float64(sw) / float64(size)
	for y := 0; y < size; y++ {
		y0, y1 := float64(y)*scale, float64(y+1)*scale
		for x := 0; x < size; x++ {
			x0, x1 := float64(x)*scale, float64(x+1)*scale
			var r, g, b, a, wsum float64
			for sy := int(y0); sy < int(math.Ceil(y1)) && sy < sw; sy++ {
				wy := math.Min(y1, float64(sy+1)) - math.Max(y0, float64(sy))
				for sx := int(x0); sx < int(math.Ceil(x1)) && sx < sw; sx++ {
					wx := math.Min(x1, float64(sx+1)) - math.Max(x0, float64(sx))
					w := wx * wy
					c := src.NRGBAAt(sx, sy)
					al := float64(c.A) / 255
					r += float64(c.R) * al * w
					g += float64(c.G) * al * w
					b += float64(c.B) * al * w
					a += al * w
					wsum += w
				}
			}
			if wsum == 0 || a == 0 {
				continue
			}
			dst.SetNRGBA(x, y, color.NRGBA{
				R: uint8(math.Round(r / a)), G: uint8(math.Round(g / a)), B: uint8(math.Round(b / a)),
				A: uint8(math.Round(a / wsum * 255)),
			})
		}
	}
	return dst
}

// opaque flattens an icon on white: the App Store rejects icons with
// transparency.
func opaque(src *image.NRGBA) *image.NRGBA {
	dst := image.NewNRGBA(src.Bounds())
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, image.Point{}, draw.Over)
	return dst
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func newNRGBA(size int) *image.NRGBA { return image.NewNRGBA(image.Rect(0, 0, size, size)) }
