//go:build windows

package windows

import (
	stdimage "image"
	"os"
	"testing"

	"github.com/egoist/asuka/colors"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// paintNow paints what's waiting to be painted in the main window, the
// corners a clipping view's tree painted over included.
func paintNow(h *Host) {
	for range 2 {
		RedrawWindow(h.window, RDW_UPDATENOW|RDW_ALLCHILDREN)
		PaintPending()
	}
	DwmFlush()
}

// redAlong is how much color (red or blue) the pixels along the diagonal from (x, y) in show,
// over white: 0 white, 1 the color.
func redAlong(img *stdimage.NRGBA, x, y, steps int) []float64 {
	var out []float64
	for i := range steps {
		c := img.NRGBAAt(x+i, y+i)
		out = append(out, 1-float64(c.G)/255)
	}
	return out
}

// A view that clips with a corner radius clips its child windows at its
// rounded corners, antialiased; and the corners follow what the child
// draws.
func TestRoundedClip(t *testing.T) {
	color := signal.New(colors.RGB(255, 0, 0))
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(20), Background: colors.RGB(255, 255, 255)}},
			ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(100), Clip: true, CornerRadius: 30}},
				ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style { return ui.Style{Grow: 1, Background: color.Get()} })}),
			),
		)
	})
	var clipper *node
	onMain(t, func() {
		for _, n := range h.nodes {
			if n.clip {
				clipper = n
			}
		}
		if clipper == nil || clipper.clipRects == nil {
			fatalf("the clipping view has no region")
		}
		showHost(h)
		paintNow(h)
	})
	check := func(what string, want func(r, g, b uint8) bool) {
		var img *stdimage.NRGBA
		var x, y int
		onMain(t, func() {
			img = windowImage(h.window)
			if dir := os.Getenv("ASUKA_TEST_PNG"); dir != "" {
				writeWindowPNG(h.window, dir+"/clip-"+what+".png")
			}
			o := MapPoint(clipper.hwnd, 0, Point{})
			v := FrameBounds(h.window)
			x, y = int(o.X-v.Left), int(o.Y-v.Top)
		})
		k := float64(h.scale)
		steps := int(30 * k)
		along := redAlong(img, x, y, steps)
		if along[0] > 0.05 {
			t.Errorf("%s: the corner's pixel shows the child: %.2f", what, along[0])
		}
		partial := 0
		for i, v := range along {
			c := img.NRGBAAt(x+i, y+i)
			if v > 0.08 && v < 0.92 {
				partial++
				if !want(c.R, c.G, c.B) {
					t.Errorf("%s: the edge's pixel %d along the diagonal is %v", what, i, c)
				}
			}
		}
		if partial == 0 {
			t.Errorf("%s: no antialiased pixel along the corner's diagonal: %.2f", what, along)
		}
		if mid := img.NRGBAAt(x+int(50*k), y+int(50*k)); !want(mid.R, mid.G, mid.B) {
			t.Errorf("%s: the middle is %v", what, mid)
		}
	}
	check("red", func(r, g, b uint8) bool { return r > 200 && g == b })
	// The child alone repaints: the parent draws the corners again.
	onMain(t, func() {
		h.enter(func() { ui.Post(func() { color.Set(colors.RGB(0, 0, 255)) }) })
		h.runPosted()
		paintNow(h)
	})
	check("blue", func(r, g, b uint8) bool { return b > 200 && r == g })
	// No longer clipped rounded, no region.
	onMain(t, func() {
		h.enter(func() {})
		clipper.cornerRadius = 0
		h.shapeClip(clipper)
		if clipper.clipRects != nil || h.roundClips != 0 {
			fatalf("still shaped: %d", h.roundClips)
		}
	})
}

// clipRows covers the pixels a rounded rectangle covers whole, and
// clipCoverage agrees with it.
func TestClipRows(t *testing.T) {
	for _, c := range []struct {
		w, h int32
		r    float64
	}{{100, 100, 30}, {40, 20, 10}, {9, 9, 4.5}, {200, 60, 30}, {61, 61, 7.25}} {
		rects := clipRows(c.w, c.h, c.r, nil)
		in := func(x, y int32) bool {
			for _, r := range rects {
				if x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom {
					return true
				}
			}
			return false
		}
		for y := range c.h {
			for x := range c.w {
				cov := clipCoverage(x, y, c.w, c.h, c.r)
				if in(x, y) != (cov >= 1) {
					t.Fatalf("%v: (%d,%d) covered %.3f, in the region: %v", c, x, y, cov, in(x, y))
				}
			}
		}
	}
}
