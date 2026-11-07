//go:build darwin && !ios

package darwin

import (
	"fmt"
	"testing"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// A plain list's section header stays at its top while its section's
// rows scroll under it, and the next header pushes it up.
func TestListStickyHeaders(t *testing.T) {
	items := make([]int, 200)
	for i := range items {
		items[i] = i
	}
	h, _ := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{
			Section: func(i int) string { return fmt.Sprint("Section ", i/20) },
			Header: func(s signal.Accessor[string]) ui.Node {
				return ui.Text(signal.Func(func() string { return s.Get() }), ui.Style{Height: ui.Pt(30)})
			},
		}, signal.Static(items), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.Text(signal.Func(func() string { return fmt.Sprint("Row ", item.Get()) }), ui.Style{Height: ui.Pt(40)})
			})
	})
	onMain(func() {
		l := find(h, ops.KindList)[0].list
		if len(l.sections) != 10 || !l.rows[0].header || l.rows[21].header != true {
			fatalf(t, "%d sections, rows %+v", len(l.sections), l.rows[:2])
		}
		// Into section 2: its header (row 42) is pinned at the top.
		y := l.tops[42] + 100
		h.scrollToOffset(l.n, 0, y, false)
		h.scrolledList(Send(l.scroll, "contentView"))
		rv := l.shown[42]
		if rv == nil {
			fatal(t, "section 2's header isn't shown")
		}
		f := MsgRect(rv.view, Sel("frame"))
		if vb := l.visible(); f.Origin.Y != vb.Origin.Y {
			fatalf(t, "the header is at %v, the top at %v", f.Origin.Y, vb.Origin.Y)
		}
		if s := text(firstText(rv.cells[0].n)); s != "Section 2" {
			fatalf(t, "the pinned header says %q", s)
		}
		// Just above section 3: its header pushes section 2's up.
		y = l.tops[63] - 10
		h.scrollToOffset(l.n, 0, y, false)
		h.scrolledList(Send(l.scroll, "contentView"))
		f = MsgRect(l.shown[42].view, Sel("frame"))
		if want := l.tops[63] - l.rowHeight(42); f.Origin.Y != want {
			fatalf(t, "the header is at %v, want %v", f.Origin.Y, want)
		}
	})
}

// A list anchored at its bottom: few rows sit at its bottom, it opens
// at its end and stays there as rows come, and rows added above what
// it shows don't move that.
func TestListAnchorBottom(t *testing.T) {
	items := signal.NewFunc([]int{1, 2, 3}, nil)
	h, r := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{Anchor: ui.AnchorBottom}, items, func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.Text(signal.Func(func() string { return fmt.Sprint("Row ", item.Get()) }), ui.Style{Height: ui.Pt(40)})
			})
	})
	set := func(v []int) {
		onMain(func() {
			h.enter(func() {
				items.Set(v)
				r.Flush()
			})
		})
	}
	onMain(func() {
		l := find(h, ops.KindList)[0].list
		vb := l.visible()
		if l.lead != vb.Size.Height-120 || l.tops[0] != l.lead {
			fatalf(t, "lead %v, first row at %v, the list %v tall", l.lead, l.tops[0], vb.Size.Height)
		}
	})
	var many []int
	for i := 1; i <= 300; i++ {
		many = append(many, i)
	}
	set(many)
	onMain(func() {
		l := find(h, ops.KindList)[0].list
		vb := l.visible()
		if end := l.tops[len(l.tops)-1] - vb.Size.Height; vb.Origin.Y != end || !l.stuck {
			fatalf(t, "at %v, the end is %v (stuck %v)", vb.Origin.Y, end, l.stuck)
		}
		// The user scrolls up: off the end.
		h.scrollToOffset(l.n, 0, 4000, false)
		h.scrolledList(Send(l.scroll, "contentView"))
		if l.stuck {
			fatal(t, "still stuck to the end")
		}
	})
	var key int64
	var dy float64
	onMain(func() {
		key, dy, _ = find(h, ops.KindList)[0].list.anchorRow()
	})
	// Older rows come first: what shows stays.
	older := make([]int, 0, 350)
	for i := -49; i <= 0; i++ {
		older = append(older, i)
	}
	set(append(older, many...))
	onMain(func() {
		l := find(h, ops.KindList)[0].list
		k, d, _ := l.anchorRow()
		if k != key || d != dy {
			fatalf(t, "row %d at %v shows, was %d at %v", k, d, key, dy)
		}
	})
}
