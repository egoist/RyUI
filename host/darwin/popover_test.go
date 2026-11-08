//go:build darwin && !ios

package darwin

import (
	"testing"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
	kage "github.com/egoist/ryui/shader"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// A popover is an NSPopover as big as its content, shown from its anchor;
// closing it from the core closes the NSPopover, and closing the
// NSPopover reports Dismissed.
func TestPopover(t *testing.T) {
	open := signal.New(true)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20)},
			ui.Popover(ui.PopoverProps{Open: open, Content: func() ui.Node {
				return ui.Column(ui.Style{Padding: ui.All(12)}, ui.StaticText("Hello"))
			}}, ui.Button(ui.ButtonProps{Title: ui.Val("Info")})))
	})
	onMain(func() {
		pops := find(h, ops.KindPopover)
		if len(pops) != 1 || pops[0].popover.pop == 0 {
			fatal(t, "no NSPopover")
		}
		n := pops[0]
		s := MsgSize(n.popover.pop, Sel("contentSize"))
		if s.Width <= 24 || s.Height <= 24 {
			fatalf(t, "content size %+v", s)
		}
		p := n.popover.pop
		Send(p, "close")
		// AppKit tells its delegate once its animation ends, in a run loop
		// a test doesn't run: as it would.
		h.enter(func() { h.popoverClosed(p) })
	})
	onMain(func() { h.enter(r.Flush) })
	if open.Peek() {
		t.Fatal("closing the NSPopover didn't report Dismissed")
	}
	onMain(func() { h.enter(func() { open.Set(true); r.Flush() }) })
	onMain(func() {
		if pops := find(h, ops.KindPopover); len(pops) != 1 || pops[0].popover.pop == 0 {
			fatal(t, "not shown again")
		}
	})
	onMain(func() { h.enter(func() { open.Set(false); r.Flush() }) })
	onMain(func() {
		if len(find(h, ops.KindPopover)) != 0 {
			fatal(t, "the core's close left it")
		}
	})
}

// A PageControl measures its dots, and a click on one selects its page;
// a paging Scroll hides its scroll bars.
func TestPageControlAndPaging(t *testing.T) {
	page := signal.New(0)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20), Gap: 8},
			ui.Scroll(ui.ScrollProps{Horizontal: true, Paging: true, Page: page, Style: ui.Style{Height: ui.Pt(100)}},
				ui.StaticText("one"), ui.StaticText("two"), ui.StaticText("three")),
			ui.PageControl(ui.PageControlProps{Count: ui.Val(3), Page: page}))
	})
	onMain(func() {
		dots := find(h, ops.KindPageControl)[0]
		f := MsgRect(dots.view, Sel("frame"))
		if f.Size.Height != dotsHigh {
			fatalf(t, "dots frame %+v", f)
		}
		h.enter(func() { h.chooseDot(dots, 2) })
		sc := find(h, ops.KindScroll)[0]
		if SendBool(sc.view, "hasHorizontalScroller") {
			fatal(t, "a pager with a scroll bar")
		}
	})
	if page.Peek() != 2 {
		t.Fatalf("page %d", page.Peek())
	}
}

// A field's accessory is made, and never shown: a Mac has no on-screen
// keyboard.
func TestAccessoryOnMac(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.TextField(ui.TextFieldProps{Value: signal.New(""), Accessory: func() ui.Node {
			return ui.Button(ui.ButtonProps{Title: ui.Val("Done")})
		}})
	})
	onMain(func() {
		a := find(h, ops.KindAccessory)
		if len(a) != 1 || Send(a[0].view, "window") != 0 {
			fatal(t, "an accessory in the window")
		}
	})
}

// A canvas draws its display list: a red fill on its left half, green on
// its right, read back from the view rendered into a bitmap.
func TestCanvas(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Canvas(func(g *ui.Graphics) {
			w, ht := g.Size()
			g.Fill(g.Path().Rect(0, 0, w/2, ht), ui.RGB(255, 0, 0))
			g.Save()
			g.Translate(w/2, 0)
			g.Fill(g.Path().Rect(0, 0, w/2, ht), ui.RGB(0, 255, 0))
			g.Restore()
			g.Text("Hi", w/2, ht/2, ui.TextStyle{Align: ui.TextCenter})
		}, ui.Style{Width: ui.Pt(100), Height: ui.Pt(40)})
	})
	onMain(func() {
		c := find(h, ops.KindCanvas)[0]
		if len(c.canvas) == 0 {
			fatal(t, "no display list")
		}
		b := MsgRect(c.view, Sel("bounds"))
		rep := MsgRectToID(c.view, Sel("bitmapImageRepForCachingDisplayInRect:"), b)
		MsgRectIDVoid(c.view, Sel("cacheDisplayInRect:toBitmapImageRep:"), b, rep)
		px := func(x, y int) ops.Color {
			return h.goColor(Send(rep, "colorAtX:y:", uintptr(x), uintptr(y)))
		}
		k := int(SendInt(rep, "pixelsWide")) / 100
		if r, g, _, _, _ := px(10*k, 35*k).RGBA(); r < 200 || int(r)-int(g) < 100 { // a color space away from pure red
			fatalf(t, "left %v", px(10*k, 35*k))
		}
		if r, g, _, _, _ := px(90*k, 35*k).RGBA(); g < 200 || int(g)-int(r) < 100 {
			fatalf(t, "right %v", px(90*k, 35*k))
		}
	})
}

// A split view is a split view controller: its sidebar lists the
// destinations under their sections, the detail Stack fills its content,
// and choosing a row shows that destination.
func TestSplitView(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.SplitView(ui.SplitViewProps{Title: ui.Val("Gallery"), Sections: []ui.SidebarSection{
			{Title: ui.Val("Views"), Destinations: []ui.Destination{
				{Title: ui.Val("Text"), Icon: "textformat", Content: func() ui.Node { return ui.StaticText("text page") }},
				{Title: ui.Val("Charts"), Icon: "chart.bar", Content: func() ui.Node { return ui.StaticText("charts page") }},
			}},
		}})
	})
	onMain(func() {
		sp := find(h, ops.KindSplit)
		if len(sp) != 1 {
			fatal(t, "no split")
		}
		s := sp[0].split
		if SendInt(s.table, "numberOfRows") != 3 || SendInt(s.table, "selectedRow") != 1 {
			fatalf(t, "rows %d, selected %d", SendInt(s.table, "numberOfRows"), SendInt(s.table, "selectedRow"))
		}
		set := Send(Class("NSIndexSet"), "indexSetWithIndex:", 2)
		h.enter(func() { Send(s.table, "selectRowIndexes:byExtendingSelection:", uintptr(set), 0) })
		found := false
		for _, n := range find(h, ops.KindText) {
			if text(n) == "charts page" {
				found = true
			}
		}
		if !found {
			fatal(t, "choosing Charts didn't show it")
		}
	})
}

// A checkbox toggle is a check box, and a click toggles its value.
func TestCheckbox(t *testing.T) {
	on := signal.New(false)
	h, _ := mount(t, func() ui.Node { return ui.Toggle(ui.ToggleProps{Value: on, Checkbox: true}) })
	onMain(func() {
		n := find(h, ops.KindToggle)[0]
		if GoString(Send(Send(n.view, "class"), "description")) != "NSButton" {
			fatalf(t, "a %s", GoString(Send(Send(n.view, "class"), "description")))
		}
		h.enter(func() { Send(n.view, "performClick:", 0) })
	})
	if !on.Peek() {
		t.Fatal("a click didn't check it")
	}
}

// A shader's Metal library compiles and draws: its pipeline is made.
func TestShader(t *testing.T) {
	prog := kage.MustCompile(`package main

var Time float
var Tint vec4

func Fragment(dst vec4, pos vec2) vec4 {
	return vec4(Tint.rgb*pos.x/rectSize().x, 1) + vec4(sin(Time)*0)
}
`)
	h, _ := mount(t, func() ui.Node {
		return ui.Shader(ui.ShaderProps{Program: prog,
			Uniforms: ui.Val([]ui.Uniform{{Name: "Tint", Value: []float32{1, 0, 0, 1}}}),
			Style:    ui.Style{Width: ui.Pt(80), Height: ui.Pt(40)}})
	})
	onMain(func() {
		if mtlDevice == 0 {
			return // no Metal device
		}
		s := find(h, ops.KindShader)[0].shader
		if s.layer == 0 || s.pipeline == 0 {
			fatalf(t, "layer %v, pipeline %v (failed %v)", s.layer, s.pipeline, s.failed)
		}
	})
}
