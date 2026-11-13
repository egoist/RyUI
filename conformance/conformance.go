// Package conformance checks that every host draws a Style the same
// way: a background's edges, rounded corners, borders, opacity, clips,
// shadows, gradients and transforms. Tests elsewhere check the ops the
// core sends and the views a host makes; these check the pixels.
//
// Sheet lays the cases out between two registration markers; a host's
// runner shows it, captures the screen as an image, and Check finds the
// sheet in the capture, measures each case and reports what's off. The
// cases assert measured quantities with tolerances (where an edge is to
// the device pixel, a shadow's Gaussian σ, a color's mix), not pixels
// against a reference, so one set of cases holds for every host at any
// scale; the platforms draw differently, and that's allowed.
//
// The runners:
//
//	go test ./host/darwin -run Conformance              # macOS, in process
//	go test ./host/windows -run Conformance             # Windows, in process
//	go test -tags e2e ./e2e -run Conformance            # iOS, Android and the web (headless Chrome, at 1×, 1.5× and 2×)
//	RYUI_CONFORMANCE_OUT=/tmp/c go test ...             # also write each capture, as <host>.png
//
// Sheet's app (conformance/app) shows it on any platform:
//
//	go run ./cmd/ryui dev -platform ios conformance/app
package conformance

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/ryui/ui"
)

// Feature is something a case relies on that a host may document it
// lacks; its cases are skipped there, with the reason, rather than fail.
type Feature string

const (
	// ShadowBlur: a shadow's blur is Style.Shadow's Radius (2σ, as CSS
	// and SwiftUI mean it) and its offset is Style.Shadow's X and Y.
	ShadowBlur Feature = "shadow blur and offset"
	// Transform: Style.Transform.
	Transform Feature = "transforms"
	// GroupOpacity: Style.Opacity fades a view and its children as one
	// picture, so overlapping children don't show through each other.
	GroupOpacity Feature = "group opacity"
)

// unsupported is what each host documents it lacks (DESIGN.md, Style's
// comments), and why.
var unsupported = map[string]map[Feature]string{
	"android": {
		ShadowBlur: "Android draws its elevation shadow: the radius is the elevation, and the light sets the offset",
	},
}

// skipped is why host skips c: a feature it lacks. Hosts are "ios",
// "android", "macos", "windows" and "web".
func skipped(host string, c Case) string {
	for _, f := range c.Needs {
		if why, ok := unsupported[host][f]; ok {
			return why
		}
	}
	return ""
}

// The sheet's geometry, in points.
const (
	Columns = 3
	Cell    = 100 // a case's square
	marker  = 8   // the registration squares' side
)

// Magenta is the registration markers' color, which no case uses.
var Magenta = ui.RGB(255, 0, 255)

// SheetSize is the sheet's size in points.
func SheetSize() (w, h float32) {
	rows := (len(Cases) + Columns - 1) / Columns
	return 2*marker + Columns*Cell, 2*marker + float32(rows)*Cell
}

// cellOrigin is case i's top left in the sheet, in points.
func cellOrigin(i int) (x, y float32) {
	return marker + float32(i%Columns)*Cell, marker + float32(i/Columns)*Cell
}

// Sheet is every case in its cell, on white, between a magenta square
// at the top left and one at the bottom right. Show it anywhere on
// screen, at its own size: Check finds it in the capture.
func Sheet() ui.Node {
	w, h := SheetSize()
	at := func(x, y, w, h float32, s ui.Style) ui.Style {
		s.Absolute = true
		s.Left, s.Top, s.Width, s.Height = ui.Pt(x), ui.Pt(y), ui.Pt(w), ui.Pt(h)
		return s
	}
	kids := []ui.Node{
		ui.View(ui.ViewProps{Style: at(0, 0, marker, marker, ui.Style{Background: Magenta})}),
		ui.View(ui.ViewProps{Style: at(w-marker, h-marker, marker, marker, ui.Style{Background: Magenta})}),
	}
	for i, c := range Cases {
		x, y := cellOrigin(i)
		// A View with only layout props makes no native view: the case's
		// views go straight into the sheet, which its white shows under.
		kids = append(kids, ui.View(ui.ViewProps{Style: at(x, y, Cell, Cell, ui.Style{})}, c.Scene()))
	}
	return ui.View(ui.ViewProps{Style: ui.Style{
		Width: ui.Pt(w), Height: ui.Pt(h), Background: ui.RGB(255, 255, 255),
		AlignSelf: ui.AlignCenter,
	}}, kids...)
}

// Result is one case's outcome on a capture.
type Result struct {
	Case     string
	Skipped  string   // why, when the host lacks a feature it needs
	Errors   []string // what's off; none: it passed
	Measured []string // what it measured, for the log
}

// Measure finds the sheet in img, a capture of a screen that shows it,
// and runs every case on it.
func Measure(img image.Image, host string) ([]Result, error) {
	s, err := locate(img)
	if err != nil {
		return nil, err
	}
	var out []Result
	for i, c := range Cases {
		r := Result{Case: c.Name}
		if why := skipped(host, c); why != "" {
			r.Skipped = why
			out = append(out, r)
			continue
		}
		x, y := cellOrigin(i)
		p := &Probe{s: s, x: float64(x), y: float64(y), lacks: unsupported[host]}
		c.Check(p)
		r.Errors, r.Measured = p.errors, p.measured
		out = append(out, r)
	}
	return out, nil
}

// Check runs Measure on img and reports each case as a subtest of t. It
// writes img to $RYUI_CONFORMANCE_OUT/<name>.png when that's set, so
// the captures can be looked at side by side.
func Check(t *testing.T, name string, img image.Image, host string) {
	t.Helper()
	if dir := os.Getenv("RYUI_CONFORMANCE_OUT"); dir != "" {
		if err := writePNG(filepath.Join(dir, name+".png"), img); err != nil {
			t.Log(err)
		}
	}
	results, err := Measure(img, host)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	for _, r := range results {
		t.Run(r.Case, func(t *testing.T) {
			if len(r.Measured) > 0 {
				t.Log(strings.Join(r.Measured, ", "))
			}
			if r.Skipped != "" {
				t.Skip(r.Skipped)
			}
			for _, e := range r.Errors {
				t.Error(e)
			}
		})
	}
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// sheet is where the sheet is in a capture: its top left, in pixels,
// and the pixels per point.
type sheet struct {
	img        image.Image
	ox, oy     float64
	sx, sy     float64
	maxX, maxY int
}

// locate finds the sheet by its markers: the magenta square nearest the
// capture's top left and the one nearest its bottom right.
func locate(img image.Image) (*sheet, error) {
	b := img.Bounds()
	magenta := func(x, y int) bool {
		r, g, bl, _ := img.At(x, y).RGBA()
		return r>>8 > 200 && g>>8 < 70 && bl>>8 > 200
	}
	// Each marker's pixels: a square's bounding box. The top left one is
	// the magenta pixels nearest the top left, then its box grows.
	box := func(x, y int) image.Rectangle {
		x0, y0, x1, y1 := x, y, x, y
		for x0 > b.Min.X && magenta(x0-1, y) {
			x0--
		}
		for x1+1 < b.Max.X && magenta(x1+1, y) {
			x1++
		}
		for y0 > b.Min.Y && magenta(x, y0-1) {
			y0--
		}
		for y1+1 < b.Max.Y && magenta(x, y1+1) {
			y1++
		}
		return image.Rect(x0, y0, x1+1, y1+1)
	}
	var first, last image.Rectangle
	found := false
	for y := b.Min.Y; y < b.Max.Y && !found; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if magenta(x, y) {
				// Its row's middle: the square's middle column, so the box
				// isn't cut by an antialiased corner.
				r := box(x, y)
				first, found = box((r.Min.X+r.Max.X)/2, y+(r.Dx())/2), true
				break
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("no registration marker in the %dx%d capture: is the sheet on screen?", b.Dx(), b.Dy())
	}
	found = false
	for y := b.Max.Y - 1; y >= b.Min.Y && !found; y-- {
		for x := b.Max.X - 1; x >= b.Min.X; x-- {
			if magenta(x, y) {
				r := box(x, y)
				last, found = box((r.Min.X+r.Max.X)/2, y-r.Dx()/2), true
				break
			}
		}
	}
	if !found || last == first {
		return nil, fmt.Errorf("one registration marker in the capture: is all of the sheet on screen?")
	}
	w, h := SheetSize()
	s := &sheet{
		img: img, maxX: b.Max.X - 1, maxY: b.Max.Y - 1,
		ox: float64(first.Min.X), oy: float64(first.Min.Y),
		sx: float64(last.Max.X-first.Min.X) / float64(w),
		sy: float64(last.Max.Y-first.Min.Y) / float64(h),
	}
	// Each marker is marker points square: the scales agree, or what was
	// found isn't the sheet.
	for _, k := range []float64{float64(first.Dx()) / marker, float64(first.Dy()) / marker, float64(last.Dx()) / marker, float64(last.Dy()) / marker} {
		if k < s.sx-1.5/marker*2 || k > s.sx+1.5/marker*2 {
			return nil, fmt.Errorf("the markers (%v, %v) don't match the sheet's scale %.3f", first, last, s.sx)
		}
	}
	if d := s.sx - s.sy; d > 0.02 || d < -0.02 {
		return nil, fmt.Errorf("the sheet's scale differs across (%.3f) and down (%.3f)", s.sx, s.sy)
	}
	return s, nil
}
