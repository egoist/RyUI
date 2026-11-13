//go:build linux && !android

package linux

import (
	"math"
	"testing"

	"github.com/egoist/asuka/colors"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/sdk/shader"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// pixels are a node's view as it draws now (its snapshot), rendered on
// the CPU by a renderer of the test's own: premultiplied BGRA, width by
// height, one a point. (GTK 4.14's GL renderer caches what it draws of a
// path by its render node's address, and render_texture calls outside a
// window's frames never let the entries go: a node made later at a freed
// one's address would show the freed one's drawing.)
type pixels struct {
	b    []byte
	w, h int
}

func render(n *node) pixels {
	w, ht := int(math.Ceil(float64(n.frame[2]))), int(math.Ceil(float64(n.frame[3])))
	s := SnapshotNew()
	theHost.snapshotView(n.widget, s) // what it draws now, not its last frame's
	rn := SnapshotFreeToNode(s)
	if rn == 0 {
		return pixels{w: w, h: ht, b: make([]byte, w*ht*4)}
	}
	defer RenderNodeUnref(rn)
	r := Call(Sym("gsk_cairo_renderer_new"))
	defer ObjectUnref(r)
	var gerr Ptr
	if Call(Sym("gsk_renderer_realize_for_display"), r, DisplayGetDefault(), Addr(&gerr)) == 0 {
		panic("the cairo renderer doesn't realize")
	}
	defer Call(Sym("gsk_renderer_unrealize"), r)
	vp := Rect{W: float32(w), H: float32(ht)}
	tex := RendererRenderTexture(r, rn, &vp)
	defer ObjectUnref(tex)
	tw, th := int(TextureGetWidth(tex)), int(TextureGetHeight(tex))
	p := pixels{b: make([]byte, tw*th*4), w: tw, h: th}
	Draw.TextureDownload(tex, &p.b[0], uintptr(tw*4))
	return p
}

// at is the pixel at x, y: red, green, blue, alpha (premultiplied).
func (p pixels) at(x, y int) [4]byte {
	if x < 0 || y < 0 || x >= p.w || y >= p.h {
		return [4]byte{}
	}
	i := (y*p.w + x) * 4
	return [4]byte{p.b[i+2], p.b[i+1], p.b[i], p.b[i+3]}
}

// reddish, bluish: mostly that color, opaque.
func reddish(c [4]byte) bool  { return c[3] > 200 && c[0] > 180 && c[1] < 90 && c[2] < 90 }
func bluish(c [4]byte) bool   { return c[3] > 200 && c[2] > 180 && c[0] < 90 && c[1] < 90 }
func greenish(c [4]byte) bool { return c[3] > 200 && c[1] > 150 && c[0] < 90 && c[2] < 90 }
func clear4(c [4]byte) bool   { return c[3] < 20 }

// post runs f as the app's code runs (ui.Post: then the core flushes),
// now. Main thread.
func post(h *Host, f func()) {
	ui.Post(f)
	h.runPosted()
}

func onlyNode(t *testing.T, h *Host, k ops.Kind) *node {
	ns := find(h, k)
	if len(ns) != 1 {
		fatalf(t, "%d nodes of kind %v", len(ns), k)
	}
	return ns[0]
}

// A Shape fills its path and strokes the part of its outline its trim
// keeps.
func TestShapeDraws(t *testing.T) {
	trim := signal.New(float32(0.25))
	h, _ := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Padding: ui.All(10), Gap: 10, Align: ui.AlignStart},
			ui.Shape(ui.ShapeProps{Fill: colors.RGB(255, 0, 0), Style: ui.Style{Width: ui.Pt(60), Height: ui.Pt(60)},
				Path: func(w, h float32) *ui.Path { return new(ui.Path).Ellipse(0, 0, w, h) }}),
			// A ring from the top, clockwise: its first quarter is the top
			// right.
			ui.Shape(ui.ShapeProps{Stroke: colors.RGB(0, 0, 255), LineWidth: 8, TrimEnd: trim, Style: ui.Style{Width: ui.Pt(60), Height: ui.Pt(60)},
				Path: func(w, h float32) *ui.Path {
					return new(ui.Path).Arc(w/2, h/2, w/2-4, -math.Pi/2, 3*math.Pi/2, true)
				}}),
		)
	})
	var fill, ring *node
	onMain(func() {
		for _, n := range find(h, ops.KindShape) {
			if n.frame[0] < 50 {
				fill = n
			} else {
				ring = n
			}
		}
	})
	settle(t, h, func() bool { return shapes[fill].path != 0 && shapes[ring].path != 0 && fill.frame[2] == 60 })
	onMain(func() {
		p := render(fill)
		if c := p.at(30, 30); !reddish(c) {
			fatalf(t, "the ellipse's middle is %v", c)
		}
		if c := p.at(2, 2); !clear4(c) {
			fatalf(t, "outside the ellipse is %v", c)
		}
		p = render(ring)
		// 45 degrees round from the top, on the ring: drawn; 135 and 270:
		// not.
		on := func(deg float64) [4]byte {
			a := (deg - 90) * math.Pi / 180
			return p.at(int(30+26*math.Cos(a)), int(30+26*math.Sin(a)))
		}
		if c := on(45); !bluish(c) {
			fatalf(t, "the trimmed ring at 45° is %v", c)
		}
		if c := on(180); !clear4(c) {
			fatalf(t, "the trimmed ring at 180° is %v", c)
		}
		if c := on(270); !clear4(c) {
			fatalf(t, "the trimmed ring at 270° is %v", c)
		}
		// Untrimmed, all of it.
		post(h, func() { trim.Set(1) })
	})
	settle(t, h, func() bool { return shapes[ring].trim[1] == 1 })
	onMain(func() {
		p := render(ring)
		on := func(deg float64) [4]byte {
			a := (deg - 90) * math.Pi / 180
			return p.at(int(30+26*math.Cos(a)), int(30+26*math.Sin(a)))
		}
		for _, d := range []float64{45, 180, 270} {
			if c := on(d); !bluish(c) {
				fatalf(t, "the whole ring at %v° is %v", d, c)
			}
		}
	})
}

// A trim set inside an animation moves there on the frame clock.
func TestShapeTrimAnimates(t *testing.T) {
	trim := signal.New(float32(0))
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10)},
			ui.Shape(ui.ShapeProps{Stroke: colors.RGB(0, 0, 255), LineWidth: 4, TrimEnd: trim, Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(10)},
				Path: func(w, h float32) *ui.Path { return new(ui.Path).MoveTo(0, 5).LineTo(w, 5) }}))
	})
	var s *shapeView
	onMain(func() { s = shapes[onlyNode(t, h, ops.KindShape)] })
	settle(t, h, func() bool { return s.path != 0 })
	var between bool
	onMain(func() {
		h.env.ReduceMotion = false
		post(h, func() {
			ui.AnimateWith(ui.Animation{Duration: 0.3, Curve: ui.Linear}, func() { trim.Set(1) })
		})
		if s.anim == nil || s.trim[1] != 0 {
			fatalf(t, "the trim didn't animate: %+v", s.trim)
		}
	})
	settle(t, h, func() bool {
		if s.trim[1] > 0 && s.trim[1] < 1 {
			between = true
		}
		return s.trim[1] == 1 && s.anim == nil
	})
	if !between {
		t.Error("the trim jumped to its end")
	}
}

// A Canvas replays its commands: fills, a clip, a gradient, a stroke, a
// translation and text, its colors in the theme.
func TestCanvasDraws(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10)},
			ui.Canvas(func(g *ui.Graphics) {
				g.Fill(g.Path().Rect(0, 0, 40, 40), colors.RGB(255, 0, 0))
				g.Save()
				g.Clip(g.Path().Rect(50, 0, 20, 40))
				g.Fill(g.Path().Rect(40, 0, 60, 40), colors.RGB(0, 0, 255)) // clipped to 50...70
				g.Restore()
				g.Save()
				g.Translate(0, 50)
				g.Fill(g.Path().Rect(0, 0, 40, 40), colors.RGB(0, 200, 0))
				g.Restore()
				g.FillGradient(g.Path().Rect(100, 0, 100, 40), ui.Gradient{Colors: []ui.Color{colors.RGB(255, 0, 0), colors.RGB(0, 0, 255)},
					Start: ui.Point{X: 100}, End: ui.Point{X: 200}})
				g.Stroke(g.Path().MoveTo(100, 70).LineTo(200, 70), colors.RGB(0, 0, 255), ui.StrokeStyle{Width: 6})
				g.Text("Hi", 220, 30, ui.TextStyle{Size: 30, Color: colors.RGB(0, 0, 0)})
			}, ui.Style{Width: ui.Pt(300), Height: ui.Pt(100)}))
	})
	var n *node
	onMain(func() { n = onlyNode(t, h, ops.KindCanvas) })
	settle(t, h, func() bool { return len(canvases[n].cmds) > 0 && n.frame[2] == 300 })
	onMain(func() {
		p := render(n)
		checks := []struct {
			x, y int
			ok   func([4]byte) bool
			what string
		}{
			{20, 20, reddish, "the red square"},
			{60, 20, bluish, "the clipped blue"},
			{45, 20, clear4, "clipped away"},
			{80, 20, clear4, "clipped away, right"},
			{20, 70, greenish, "the translated square"},
			{102, 20, reddish, "the gradient's start"},
			{198, 20, bluish, "the gradient's end"},
			{150, 70, bluish, "the stroke"},
			{150, 80, clear4, "beside the stroke"},
		}
		for _, c := range checks {
			if px := p.at(c.x, c.y); !c.ok(px) {
				fatalf(t, "%s at %d,%d is %v", c.what, c.x, c.y, px)
			}
		}
		ink := 0
		for y := 0; y < 40; y++ {
			for x := 220; x < 300; x++ {
				if c := p.at(x, y); c[3] > 128 {
					ink++
				}
			}
		}
		if ink < 30 {
			fatalf(t, "the text drew %d pixels", ink)
		}
	})
}

var flat = shader.MustCompile(`package main

var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	return Tint
}
`)

// A Shader renders on the CPU into a texture the view draws.
func TestShaderRenders(t *testing.T) {
	tint := signal.New([4]float32{0, 1, 0, 1})
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10)},
			ui.Shader(ui.ShaderProps{Program: flat,
				Uniforms: signal.Func(func() []ui.Uniform { v := tint.Get(); return []ui.Uniform{{Name: "Tint", Value: v[:]}} }),
				Style:    ui.Style{Width: ui.Pt(80), Height: ui.Pt(40)}}))
	})
	var n *node
	onMain(func() { n = onlyNode(t, h, ops.KindShader) })
	settle(t, h, func() bool { return kages[n].texture != 0 && !kages[n].busy })
	onMain(func() {
		if c := render(n).at(40, 20); !greenish(c) {
			fatalf(t, "the shader drew %v", c)
		}
		post(h, func() { tint.Set([4]float32{1, 0, 0, 1}) })
	})
	settle(t, h, func() bool { return !kages[n].busy && reddish(render(n).at(40, 20)) })
}

// A PageControl is measured, draws its dots, and a click on a dot or an
// arrow key changes the page.
func TestPageControl(t *testing.T) {
	page := signal.New(0)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Align: ui.AlignStart},
			ui.PageControl(ui.PageControlProps{Count: ui.Val(4), Page: page}))
	})
	var n *node
	onMain(func() { n = onlyNode(t, h, ops.KindPageControl) })
	settle(t, h, func() bool { return pagers[n].count == 4 && n.frame[2] > 0 })
	onMain(func() {
		if w, ht := n.frame[2], n.frame[3]; w != 5*pageDotCell || ht != pageDotsHigh {
			fatalf(t, "measured %vx%v", w, ht)
		}
		p := render(n)
		inked := func(i int) bool {
			x := int((n.frame[2]-4*pageDotCell)/2 + (float32(i)+0.5)*pageDotCell)
			return p.at(x, int(pageDotsHigh/2))[3] > 40
		}
		for i := range 4 {
			if !inked(i) {
				fatalf(t, "dot %d isn't drawn", i)
			}
		}
		x := (n.frame[2]-4*pageDotCell)/2 + 2.5*pageDotCell
		h.chooseDot(n, h.dotAt(n, pagers[n], x)) // as a click there
		if page.Peek() != 2 {
			fatalf(t, "a click on the third dot: page %d", page.Peek())
		}
		h.dotsKey(n, pagers[n], keyRight)
		h.dotsKey(n, pagers[n], keyRight)
		h.dotsKey(n, pagers[n], keyRight) // past the last: stays
		if page.Peek() != 3 {
			fatalf(t, "after the arrow keys: page %d", page.Peek())
		}
		h.dotsKey(n, pagers[n], keyHome)
		if page.Peek() != 0 || pagers[n].page != 0 {
			fatalf(t, "Home: page %d", page.Peek())
		}
	})
}

// A paging Scroll hides its scroll bar, moves a page with the keys,
// settles a scroll that rests between pages on the one it was going to,
// and reports each page with Paged.
func TestPaging(t *testing.T) {
	page := signal.New(0)
	h, _ := mount(t, func() ui.Node {
		cards := make([]ui.Node, 4)
		for i := range cards {
			cards[i] = ui.View(ui.ViewProps{Style: ui.Style{Background: colors.Blue}})
		}
		return ui.Column(ui.Style{Padding: ui.All(10), Align: ui.AlignStart},
			ui.Scroll(ui.ScrollProps{Horizontal: true, Paging: true, Page: page, Style: ui.Style{Width: ui.Pt(200), Height: ui.Pt(100)}}, cards...))
	})
	var n *node
	var p *paging
	onMain(func() {
		n = onlyNode(t, h, ops.KindScroll)
		p = pagings[n]
		if p == nil {
			fatalf(t, "the scroll doesn't page")
		}
	})
	settle(t, h, func() bool {
		_, size, pages := h.pageMetrics(n)
		return size == 200 && pages == 4
	})
	pos := func() float64 { x, _, _ := h.pageMetrics(n); return x }
	onMain(func() {
		var hp, vp int32
		Call(Sym("gtk_scrolled_window_get_policy"), n.scroll.sw, Addr(&hp), Addr(&vp))
		if hp != PolicyExternal {
			fatalf(t, "the horizontal policy is %d", hp)
		}
		h.pagingKey(n, p, keyRight, 0)
	})
	settle(t, h, func() bool { return pos() == 200 && page.Peek() == 1 })
	// A swipe that rests a third of the way to the next page goes back;
	// one that flicks on goes to the next.
	onMain(func() {
		AdjustmentSetValue(n.scroll.hadj, 270)
		p.velocity = 0
		h.settle(n, p)
	})
	settle(t, h, func() bool { return pos() == 200 && !p.animating })
	onMain(func() {
		AdjustmentSetValue(n.scroll.hadj, 250)
		p.velocity = 900
		h.settle(n, p)
	})
	settle(t, h, func() bool { return pos() == 400 && page.Peek() == 2 })
	// One page a swipe, however far it went.
	onMain(func() {
		AdjustmentSetValue(n.scroll.hadj, 590)
		p.startPage = 0
		p.moving = true
		p.velocity = 0
		h.settle(n, p)
	})
	settle(t, h, func() bool { return pos() == 200 && page.Peek() == 1 })
	// The app's page moves it.
	onMain(func() { post(h, func() { page.Set(3) }) })
	settle(t, h, func() bool { return pos() == 600 })
}

// F5 refreshes the refreshable scroll: Refresh, a spinner over it until
// Refreshing goes false.
func TestRefresh(t *testing.T) {
	refreshing := signal.New(false)
	refreshes := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Scroll(ui.ScrollProps{Refreshing: refreshing, OnRefresh: func() { refreshes++; refreshing.Set(true) },
			Style: ui.Style{Grow: 1}}, ui.StaticText("content"))
	})
	var n *node
	onMain(func() { n = onlyNode(t, h, ops.KindScroll) })
	settle(t, h, func() bool { return WidgetGetMapped(n.widget) })
	onMain(func() {
		if !h.refreshKey(n) || refreshes != 1 {
			fatalf(t, "F5: %d refreshes", refreshes)
		}
		r := refreshers[n]
		if r.spinner == 0 || !WidgetGetVisible(r.spinner) || WidgetGetParent(r.spinner) != n.widget {
			fatalf(t, "no spinner while it refreshes")
		}
		if h.refreshKey(n) || refreshes != 1 {
			fatalf(t, "a second F5 while it refreshes refreshed")
		}
		post(h, func() { refreshing.Set(false) })
		if WidgetGetVisible(r.spinner) {
			fatalf(t, "the spinner stayed")
		}
	})
}

// A view's screen reader actions are its context menu; an adjustable
// view takes the arrow keys.
func TestA11yActionsAdjust(t *testing.T) {
	did, adjusted := "", 0
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Gap: 8},
			ui.View(ui.ViewProps{Label: ui.Val("Mail"), Style: ui.Style{Height: ui.Pt(40)}, Actions: []ui.A11yAction{
				{Name: ui.Val("Reply"), Do: func() { did = "reply" }},
				{Name: ui.Val("Delete"), Do: func() { did = "delete" }},
			}}),
			ui.View(ui.ViewProps{Label: ui.Val("Volume"), OnAdjust: func(by int) { adjusted += by }, Style: ui.Style{Height: ui.Pt(20)}}),
		)
	})
	var mail, vol *node
	settle(t, h, func() bool {
		for n, a := range a11yViews {
			if len(a.actions) == 2 {
				mail = n
			}
			if a.adjustable {
				vol = n
			}
		}
		return mail != nil && vol != nil && WidgetGetMapped(mail.widget)
	})
	onMain(func() {
		a := a11yViews[mail]
		if !h.a11yKey(mail, a, 0xff67, 0) { // Menu
			fatalf(t, "the Menu key showed no actions")
		}
		if !WidgetActivateAction(mail.widget, "action.Delete", 0) {
			fatalf(t, "no action to take")
		}
	})
	settle(t, h, func() bool { return did == "delete" })
	onMain(func() {
		a := a11yViews[vol]
		h.a11yKey(vol, a, keyUp, 0)
		h.a11yKey(vol, a, keyUp, 0)
		h.a11yKey(vol, a, keyLeft, 0)
		if adjusted != 1 {
			fatalf(t, "adjusted by %d, want 1", adjusted)
		}
	})
}

// A view with a key destroyed during an animation waits in its top,
// where it was, for the one inserted with its key, which starts at its
// place and size; the old one goes as the animation ends.
func TestMatch(t *testing.T) {
	big := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Align: ui.AlignStart},
			ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(10), Background: colors.Gray}}, ui.Show(big, func() ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(200), Height: ui.Pt(150), Background: colors.Blue, Match: "card"}})
			}, func() ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(40), Height: ui.Pt(30), Background: colors.Blue, Match: "card"}},
					ui.StaticText("in it"))
			})))
	})
	keyed := func() *node {
		for n, k := range matchKeys {
			if k == "card" {
				return n
			}
		}
		return nil
	}
	settle(t, h, func() bool { n := keyed(); return n != nil && n.placed && WidgetGetMapped(n.widget) })
	onMain(func() {
		h.env.ReduceMotion = false
		small := keyed()
		text := onlyNode(t, h, ops.KindText)
		post(h, func() { ui.AnimateWith(ui.Animation{Duration: 0.4}, func() { big.Set(true) }) })
		if WidgetGetParent(small.widget) != h.root.widget {
			fatalf(t, "the old view didn't wait in its top")
		}
		ot := h.tweens[small]
		if ot == nil || !ot.exit || !ot.op || ot.toOp != 0 {
			fatalf(t, "the old view doesn't fade out: %+v", ot)
		}
		if WidgetGetParent(text.widget) != small.widget {
			fatalf(t, "the old view's text didn't stay in it")
		}
		large := keyed()
		tw := h.tweens[large]
		near := func(a, b float32) bool { return a-b < 0.5 && b-a < 0.5 }
		if tw == nil || !near(tw.from[2], 40) || !near(tw.from[3], 30) || !near(tw.to[2], 200) {
			fatalf(t, "the new view's tween %+v", tw)
		}
		if !near(ot.to[2], 200) || !near(ot.to[3], 150) {
			fatalf(t, "the old view goes to %v", ot.to)
		}
	})
	settle(t, h, func() bool {
		large := keyed()
		return len(h.tweens) == 0 && large != nil && large.frame[2] == 200
	})
	onMain(func() {
		for _, n := range h.nodes {
			if n.kind == ops.KindText {
				fatalf(t, "the old view's text stayed")
			}
		}
		if c := WidgetGetFirstChild(h.root.widget); c == 0 || WidgetGetNextSibling(c) != 0 {
			fatalf(t, "the root has more than its view")
		}
	})
}
