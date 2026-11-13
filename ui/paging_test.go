package ui_test

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func scrolledTo(t *testing.T, c ops.Op) (x, y float32, animated bool) {
	t.Helper()
	b := c.Value.Bytes
	return math.Float32frombits(binary.LittleEndian.Uint32(b)), math.Float32frombits(binary.LittleEndian.Uint32(b[4:])), b[8] != 0
}

// A paging Scroll and a PageControl share a page: the user's swipe
// writes it, code's write scrolls there (animated), and right to left the
// first page is the rightmost.
func TestPaging(t *testing.T) {
	page := signal.New(0)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Grow: 1},
			ui.Scroll(ui.ScrollProps{Horizontal: true, Paging: true, Page: page, Style: ui.Style{Height: ui.Pt(200)}},
				ui.StaticText("one"),
				ui.StaticText("two"),
				ui.StaticText("three")),
			ui.PageControl(ui.PageControlProps{Count: ui.Val(3), Page: page}))
	})
	sc := h.OfKind(ops.KindScroll)[0]
	pc := h.OfKind(ops.KindPageControl)[0]
	if f := h.One(ops.KindText, "two").Frame; f[0] != 390 || f[2] != 390 || f[3] != 200 {
		t.Fatalf("the second page's frame %v", f)
	}
	if !sc.Bool(ops.PropScrollPaging) || pc.Int(ops.PropPageControlCount) != 3 {
		t.Fatal("no paging or count")
	}
	h.Reset()
	h.Dispatch(ops.Event{ID: sc.ID, Kind: ops.EventPaged, Int: 2})
	r.Flush()
	if page.Peek() != 2 || pc.Int(ops.PropPageControlPage) != 2 {
		t.Fatalf("page %d, control %d", page.Peek(), pc.Int(ops.PropPageControlPage))
	}
	if c := h.Commands(ops.CmdScrollToOffset); len(c) != 0 {
		t.Fatalf("the user's page scrolled again: %v", c)
	}
	h.Select(pc, 1)
	r.Flush()
	c := h.Commands(ops.CmdScrollToOffset)
	if len(c) != 1 {
		t.Fatalf("scrolls %v", c)
	}
	if x, y, a := scrolledTo(t, c[0]); x != 390 || y != 0 || !a {
		t.Fatalf("scrolled to %v, %v, animated %v", x, y, a)
	}
}

func TestPagingRightToLeft(t *testing.T) {
	page := signal.New(0)
	h, r := mount(t, func() ui.Node {
		return ui.WithDirection(ui.RightToLeft, func() ui.Node {
			return ui.Scroll(ui.ScrollProps{Horizontal: true, Paging: true, Page: page, Style: ui.Style{Height: ui.Pt(200)}},
				ui.StaticText("one"),
				ui.StaticText("two"),
				ui.StaticText("three"))
		})
	})
	sc := h.OfKind(ops.KindScroll)[0]
	c := h.Commands(ops.CmdScrollToOffset)
	if len(c) != 1 {
		t.Fatalf("first scroll %v", c)
	}
	if x, _, a := scrolledTo(t, c[0]); x != 780 || a {
		t.Fatalf("the first page right to left is at %v (animated %v)", x, a)
	}
	h.Dispatch(ops.Event{ID: sc.ID, Kind: ops.EventPaged, Int: 0}) // the leftmost
	r.Flush()
	if page.Peek() != 2 {
		t.Fatalf("the leftmost page is %d", page.Peek())
	}
}
