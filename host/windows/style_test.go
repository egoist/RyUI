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

// capture paints what waits and returns the window as it shows, and
// where (pixels) a view's client area is in it.
func capture(t *testing.T, h *Host, n *node) (img *stdimage.NRGBA, x, y int) {
	onMain(t, func() {
		paintNow(h)
		img = windowImage(h.window)
		if dir := os.Getenv("ASUKA_TEST_PNG"); dir != "" {
			writeWindowPNG(h.window, dir+"/"+t.Name()+".png")
		}
		o := MapPoint(n.hwnd, 0, Point{})
		v := FrameBounds(h.window)
		x, y = int(o.X-v.Left), int(o.Y-v.Top)
	})
	return
}

func nodeWith(h *Host, f func(n *node) bool) *node {
	for _, n := range h.nodes {
		if f(n) {
			return n
		}
	}
	return nil
}

// A view with child windows at an opacity between 0 and 1 is drawn
// faded, what's in it with it, by a picture over it, and still takes
// clicks; at 1 the picture goes.
func TestOpacityPicture(t *testing.T) {
	op := signal.New(float32(0.5))
	presses := 0
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(20), Background: colors.RGB(255, 255, 255)}},
			ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
				return ui.Style{Width: ui.Pt(80), Height: ui.Pt(80), Opacity: op.Get()}
			})},
				ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(40), Height: ui.Pt(40), Background: colors.RGB(255, 0, 0)}, OnPress: func() { presses++ }}),
			),
		)
	})
	var faded *node
	onMain(t, func() {
		showHost(h)
		faded = nodeWith(h, func(n *node) bool { return n.opacity == 0.5 })
		if faded == nil || faded.pict == nil || !IsWindowVisible(faded.pict.pic.hwnd) {
			fatalf("a view with child windows at opacity 0.5 has no picture")
		}
	})
	img, x, y := capture(t, h, faded)
	k := float64(h.scale)
	if c := img.NRGBAAt(x+int(20*k), y+int(20*k)); c.R < 250 || c.G < 110 || c.G > 145 || c.G != c.B {
		t.Errorf("the faded child is %v, want half red over white", c)
	}
	if c := img.NRGBAAt(x+int(60*k), y+int(60*k)); c.R < 250 || c.G < 250 {
		t.Errorf("the faded view's empty part is %v, want white", c)
	}
	onMain(t, func() {
		// A click where the child is goes through the picture to it (the
		// picture is transparent to the mouse; the child still there).
		p := MapPoint(faded.hwnd, 0, Point{X: int32(20 * k), Y: int32(20 * k)})
		if r := SendMessage(faded.pict.pic.hwnd, wmNCHitTest, 0, MakeLong(uint16(int16(p.X)), uint16(int16(p.Y)))); r != htTransparent {
			fatalf("the picture takes the mouse: %d", int32(r))
		}
		child := faded.children[0]
		if r := WindowRect(child.hwnd); !IsWindowVisible(child.hwnd) || p.X < r.Left || p.X >= r.Right || p.Y < r.Top || p.Y >= r.Bottom {
			fatalf("the faded child isn't under the point: %v, %v", r, p)
		}
		h.enter(func() { ui.Post(func() { op.Set(1) }) })
		h.runPosted()
		if faded.pict != nil {
			fatalf("a picture stays at opacity 1")
		}
	})
}

// A scale transform sizes the view's window about its center and scales
// what's in it; a translation moves it; a rotation draws it as a turned
// picture, its own window shaped to nothing.
func TestTransform(t *testing.T) {
	tf := signal.New(ui.Transform{Scale: 0.5})
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(20), Background: colors.RGB(255, 255, 255)}},
			ui.View(ui.ViewProps{Style: ui.StyleFunc(func() ui.Style {
				return ui.Style{Width: ui.Pt(100), Height: ui.Pt(100), Background: colors.RGB(0, 0, 255), Transform: tf.Get()}
			})},
				ui.Text(ui.Val("Hi"), ui.Style{Absolute: true, Left: ui.Pt(10), Top: ui.Pt(10)}),
			),
		)
	})
	onMain(t, func() {
		showHost(h)
		v := nodeWith(h, func(n *node) bool { return n.zoom != 0 })
		text := nodeWith(h, func(n *node) bool { return n.text == "Hi" })
		if v == nil || text == nil {
			fatalf("no scaled view (%v) or text (%v)", v, text)
		}
		k := h.scale
		r := WindowRectIn(v.hwnd, Parent(v.hwnd))
		want := func(pt float32) int32 { return int32(pt*k + 0.5) }
		if d := r.W() - want(50); d < -1 || d > 1 {
			fatalf("the scaled view is %d pixels wide, want %d", r.W(), want(50))
		}
		if d := r.Left - want(45); d < -1 || d > 1 {
			fatalf("the scaled view's left is %d, want %d (its center kept)", r.Left, want(45))
		}
		if h.scaleOf(text) != k*0.5 {
			fatalf("the text in it draws at %v pixels a point, want %v", h.scaleOf(text), k*0.5)
		}
		tr := WindowRectIn(text.hwnd, v.hwnd)
		if d := tr.Left - want(5); d < -1 || d > 1 {
			fatalf("the text is at %d in it, want %d", tr.Left, want(5))
		}
		h.enter(func() { ui.Post(func() { tf.Set(ui.Transform{TranslateX: 30}) }) })
		h.runPosted()
		r = WindowRectIn(v.hwnd, Parent(v.hwnd))
		if v.zoom != 0 || h.scaleOf(text) != k || r.W() != want(100) {
			fatalf("unscaled: zoom %v, text at %v, width %d", v.zoom, h.scaleOf(text), r.W())
		}
		if d := r.Left - want(50); d < -1 || d > 1 {
			fatalf("moved 30 points: left %d, want %d", r.Left, want(50))
		}
		h.enter(func() { ui.Post(func() { tf.Set(ui.Transform{Rotate: 30}) }) })
		h.runPosted()
		if v.pict == nil || !v.pict.shaped || !IsWindowVisible(v.pict.pic.hwnd) {
			fatalf("no picture for the rotated view")
		}
		pic := v.pict.pic
		h.enter(func() { ui.Post(func() { tf.Set(ui.Transform{}) }) })
		h.runPosted()
		if v.pict != nil || pic.hwnd != 0 || h.nodes[pic.id] != nil {
			fatalf("the picture stays after the rotation goes")
		}
	})
}

// A material blurs what's behind it and tints it.
func TestMaterial(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(200), Height: ui.Pt(100), Background: colors.RGB(255, 255, 255)}},
			ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Left: ui.Pt(100), Top: ui.Pt(0), Width: ui.Pt(100), Height: ui.Pt(100), Background: colors.RGB(0, 0, 0)}}),
			ui.View(ui.ViewProps{Style: ui.Style{Absolute: true, Left: ui.Pt(50), Top: ui.Pt(20), Width: ui.Pt(100), Height: ui.Pt(60), Material: ui.MaterialRegular}}),
		)
	})
	var m *node
	onMain(t, func() {
		showHost(h)
		m = nodeWith(h, func(n *node) bool { return n.material != 0 })
	})
	img, x, y := capture(t, h, m)
	k := float64(h.scale)
	// At the edge between white and black, behind its middle: a gray.
	edge := img.NRGBAAt(x+int(50*k), y+int(30*k))
	left := img.NRGBAAt(x+int(5*k), y+int(30*k))
	right := img.NRGBAAt(x+int(95*k), y+int(30*k))
	t.Logf("left %v, edge %v, right %v", left, edge, right)
	if edge.R < 40 || edge.R > 215 || edge.R != edge.G {
		t.Errorf("behind the edge's middle: %v, want a blurred gray", edge)
	}
	if left.R < right.R+30 {
		t.Errorf("the material doesn't follow what's behind it: %v on white, %v on black", left, right)
	}
	if right.R < 20 {
		t.Errorf("on black: %v, want tinted", right)
	}
}
