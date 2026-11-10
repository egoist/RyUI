package conformance

import (
	"image"
	"image/color"
	"math"
	"testing"
)

// canvas is a synthetic capture: a sheet at (ox, oy) pixels, k pixels
// per point, painted by f from each pixel's center in the sheet's points.
func canvas(w, h int, ox, oy, k float64, f func(x, y float64) color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	sw, sh := SheetSize()
	for py := range h {
		for px := range w {
			x, y := (float64(px)+0.5-ox)/k, (float64(py)+0.5-oy)/k
			c := color.RGBA{40, 40, 40, 255} // the screen around the sheet
			if x >= 0 && y >= 0 && x < float64(sw) && y < float64(sh) {
				c = color.RGBA{255, 255, 255, 255}
				if (x < marker && y < marker) || (x >= float64(sw)-marker && y >= float64(sh)-marker) {
					c = color.RGBA{255, 0, 255, 255}
				} else if f != nil {
					c = f(x, y)
				}
			}
			img.SetRGBA(px, py, c)
		}
	}
	return img
}

func TestLocate(t *testing.T) {
	for _, k := range []float64{1, 1.5, 2, 2.625, 3} {
		ox, oy := 17.0, 61.0
		sw, sh := SheetSize()
		img := canvas(int(ox+float64(sw)*k)+20, int(oy+float64(sh)*k)+30, ox, oy, k, nil)
		s, err := locate(img)
		if err != nil {
			t.Fatalf("at %g×: %v", k, err)
		}
		if s.ox != ox || s.oy != oy || math.Abs(s.sx-k) > 0.01 || math.Abs(s.sy-k) > 0.01 {
			t.Errorf("at %g×: found the sheet at (%g,%g), %g×%g", k, s.ox, s.oy, s.sx, s.sy)
		}
	}
	if _, err := locate(image.NewRGBA(image.Rect(0, 0, 50, 50))); err == nil {
		t.Error("found a sheet in an empty capture")
	}
}

// probeOn is a probe on case 0's cell of a synthetic capture at k×,
// painted by f in the cell's points.
func probeOn(t *testing.T, k float64, f func(x, y float64) color.RGBA) *Probe {
	t.Helper()
	sw, sh := SheetSize()
	img := canvas(int(float64(sw)*k)+10, int(float64(sh)*k)+10, 3, 5, k, func(x, y float64) color.RGBA {
		return f(x-marker, y-marker)
	})
	s, err := locate(img)
	if err != nil {
		t.Fatal(err)
	}
	return &Probe{s: s, x: marker, y: marker}
}

func TestEdge(t *testing.T) {
	for _, k := range []float64{1, 1.5, 2, 3} {
		// A red box from 20.25 to 80, its edge pixels mixed by coverage.
		p := probeOn(t, k, func(x, y float64) color.RGBA {
			px := 1 / k
			cov := math.Max(0, math.Min(1, (x+px/2-20.25)/px)) * math.Max(0, math.Min(1, (80-(x-px/2))/px))
			g := uint8(math.Round(255 * (1 - cov)))
			return color.RGBA{255, g, g, 255}
		})
		p.Edge("left", 0, 50, 50, 50, white, red, 20.25)
		p.Edge("right", 99, 50, 50, 50, white, red, 80)
		if len(p.errors) > 0 {
			t.Errorf("at %g×: %v", k, p.errors)
		}
		p.errors = nil
		p.Edge("left", 0, 50, 50, 50, white, red, 22)
		if len(p.errors) != 1 {
			t.Errorf("at %g×: an edge 1.75 points off passed", k)
		}
	}
}

func TestBlur(t *testing.T) {
	for _, k := range []float64{1, 2, 3} {
		for _, sigma := range []float64{2, 4} {
			// A black shadow's edge at 60, blurred by a Gaussian.
			p := probeOn(t, k, func(x, y float64) color.RGBA {
				d := 0.5 * math.Erfc((x-60)/(sigma*math.Sqrt2))
				g := uint8(math.Round(255 * (1 - d)))
				return color.RGBA{g, g, g, 255}
			})
			e, s, plateau, ok := p.Blur(30, 50, 99, 50)
			if !ok || math.Abs(e-60) > 0.3 || math.Abs(s-sigma) > 0.25 || plateau < 0.99 {
				t.Errorf("at %g×, σ %g: edge %.2f, σ %.2f, plateau %.2f, ok %v", k, sigma, e, s, plateau, ok)
			}
		}
	}
}

func TestCasesFit(t *testing.T) {
	seen := map[string]bool{}
	for _, c := range Cases {
		if seen[c.Name] {
			t.Errorf("two cases named %q", c.Name)
		}
		seen[c.Name] = true
	}
	// The sheet fits a phone's screen in portrait, and the macOS host's
	// default window.
	if w, h := SheetSize(); w > 360 || h > 580 {
		t.Errorf("the sheet is %gx%g points", w, h)
	}
}
