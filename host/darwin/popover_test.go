//go:build darwin && !ios

package darwin

import (
	"testing"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
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
