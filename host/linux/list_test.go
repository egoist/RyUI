//go:build linux && !android

package linux

import (
	"fmt"
	"testing"

	"github.com/egoist/ryui/host"
	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// countingCore counts the rows the host binds.
type countingCore struct {
	host.Core
	binds int
}

func (c *countingCore) ListBind(list ops.NodeID, index int, cell ops.NodeID) {
	c.binds++
	c.Core.ListBind(list, index, cell)
}

func countBinds(h *Host) *countingCore {
	c := &countingCore{Core: h.core}
	onMain(func() { h.core = c })
	return c
}

func firstText(n *node) string {
	if n.kind == ops.KindText {
		return n.text
	}
	for _, c := range n.children {
		if s := firstText(c); s != "" {
			return s
		}
	}
	return ""
}

func theList(h *Host) *list { return find(h, ops.KindList)[0].ext.(*list) }

// rowsOf is the list's rows, shown or free: its row views.
func rowsOf(l *list) int { return len(l.shown) + len(l.free) }

func textRow(item signal.Accessor[int], height float32) ui.Node {
	return ui.Text(signal.Func(func() string { return fmt.Sprint("Row ", item.Get()) }), ui.Style{Height: ui.Pt(height)})
}

// A list of 10,000 rows binds only those around what shows, recycles
// their views as it scrolls, and binds again only the rows new to the
// view.
func TestListBindsOnlyWhatShows(t *testing.T) {
	items := make([]int, 10000)
	for i := range items {
		items[i] = i
	}
	h, _ := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{}, signal.Static(items), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(40)}},
					ui.Text(signal.Func(func() string { return fmt.Sprint("Row ", item.Get()) })))
			})
	})
	settle(t, h, func() bool { return len(theList(h).shown) > 0 })
	c := countBinds(h)
	var made int
	onMain(func() {
		l := theList(h)
		if len(l.shown) > 60 {
			fatalf(t, "%d rows shown of 10,000", len(l.shown))
		}
		if total := l.tops[len(l.tops)-1]; total < 10000*40-1 {
			fatalf(t, "the content is %.0f tall", total)
		}
		// Rows from half a screen above what shows to half a screen below.
		most := int(2*l.viewH()/40) + 2
		// Scrolled to the middle, the rows there are bound.
		l.setOffset(200000, false)
		rv := l.shown[5000]
		if rv == nil {
			fatalf(t, "row 5000 isn't shown at 200,000pt")
		}
		if s := firstText(rv.cells[0].n); s != "Row 5000" {
			fatalf(t, "row 5000 shows %q", s)
		}
		if c.binds > most || len(l.shown) > most {
			fatalf(t, "%d binds, %d rows shown, to show a screen of rows", c.binds, len(l.shown))
		}
		// Elsewhere, in the views the rows there went out of.
		made = rowsOf(l)
		l.setOffset(100000, false)
		if rowsOf(l) != made {
			fatalf(t, "%d row views after scrolling, %d before: not recycled", rowsOf(l), made)
		}
		l.setOffset(200000, false)
		rv = l.shown[5000]
		// A row further: one row comes into view, one bind.
		c.binds = 0
		l.setOffset(200040, false)
		if c.binds != 1 {
			fatalf(t, "%d binds scrolling by a row", c.binds)
		}
		// The rows' views are where the rows are, in the rows' view.
		if f := rv.n.frame; f[1] != l.tops[5000] || f[3] != 40 || f[2] != l.width {
			fatalf(t, "row 5000's frame %v, its top %v", f, l.tops[5000])
		}
	})
	// GTK places them so.
	settle(t, h, func() bool {
		rv := theList(h).shown[5000]
		x, y, w, ht := allocation(rv.n.widget)
		return x == 0 && y == rv.n.frame[1] && w == rv.n.frame[2] && ht == 40
	})
	// The user scrolls (the scroll bar's adjustment): the list tiles and
	// reports it.
	var scrolled float32
	onMain(func() {
		l := theList(h)
		AdjustmentSetValue(l.vadj, 4000)
		if l.shown[100] == nil || firstText(l.shown[100].cells[0].n) != "Row 100" {
			fatalf(t, "row 100 isn't shown at 4,000pt")
		}
		scrolled = float32(l.reported)
	})
	if scrolled != 4000 {
		t.Fatalf("reported %v", scrolled)
	}
}

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
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node { return textRow(item, 40) })
	})
	settle(t, h, func() bool { return len(theList(h).shown) > 0 })
	onMain(func() {
		l := theList(h)
		if len(l.sections) != 10 || !l.rows[0].header || !l.rows[21].header {
			fatalf(t, "%d sections, rows %+v", len(l.sections), l.rows[:2])
		}
		// Into section 2: its header (row 42) is pinned at the top.
		y := l.tops[42] + 100
		l.setOffset(y, false)
		rv := l.shown[42]
		if rv == nil {
			fatalf(t, "section 2's header isn't shown")
		}
		if !rv.pinned || rv.n.frame[1] != l.offset() {
			fatalf(t, "the header is at %v (pinned %v), the top at %v", rv.n.frame[1], rv.pinned, l.offset())
		}
		if s := firstText(rv.cells[0].n); s != "Section 2" {
			fatalf(t, "the pinned header says %q", s)
		}
		if WidgetGetLastChild(l.doc.widget) != rv.n.widget {
			fatalf(t, "the pinned header isn't over the rows")
		}
		// Just above section 3: its header pushes section 2's up.
		l.setOffset(l.tops[63]-10, false)
		if want := l.tops[63] - l.rowHeight(42); l.shown[42].n.frame[1] != want {
			fatalf(t, "the header is at %v, want %v", l.shown[42].n.frame[1], want)
		}
	})
}

// A list anchored at its bottom: few rows sit at its bottom, it opens at
// its end and stays there as rows come, and rows added above what it
// shows don't move that.
func TestListAnchorBottom(t *testing.T) {
	items := signal.NewFunc([]int{1, 2, 3}, nil)
	h, r := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{Anchor: ui.AnchorBottom}, items, func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node { return textRow(item, 40) })
	})
	settle(t, h, func() bool { return len(theList(h).shown) == 3 })
	set := func(v []int) {
		onMain(func() {
			h.enter(func() {
				items.Set(v)
				r.Flush()
			})
		})
	}
	onMain(func() {
		l := theList(h)
		if vh := l.viewH(); l.lead != vh-120 || l.tops[0] != l.lead {
			fatalf(t, "lead %v, first row at %v, the list %v tall", l.lead, l.tops[0], vh)
		}
	})
	var many []int
	for i := 1; i <= 300; i++ {
		many = append(many, i)
	}
	set(many)
	// It goes to its new end (animated, once it's shown).
	settle(t, h, func() bool {
		l := theList(h)
		return l.anim == 0 && l.offset() == l.endOffset() && l.endOffset() > 0
	})
	onMain(func() {
		l := theList(h)
		if !l.stuck || l.shown[299] == nil {
			fatalf(t, "stuck %v, last row shown %v", l.stuck, l.shown[299] != nil)
		}
		// The user scrolls up: off the end.
		AdjustmentSetValue(l.vadj, 4000)
		if l.stuck {
			fatalf(t, "still stuck to the end")
		}
	})
	var key int64
	var dy float32
	onMain(func() { key, dy, _ = theList(h).anchorRow() })
	// Older rows come first: what shows stays.
	older := make([]int, 0, 350)
	for i := -49; i <= 0; i++ {
		older = append(older, i)
	}
	set(append(older, many...))
	onMain(func() {
		k, d, _ := theList(h).anchorRow()
		if k != key || d != dy {
			fatalf(t, "row %d at %v shows, was %d at %v", k, d, key, dy)
		}
	})
	// Back at the end, it stays there as the list gets shorter.
	onMain(func() {
		l := theList(h)
		AdjustmentSetValue(l.vadj, float64(l.endOffset()))
		if !l.stuck {
			fatalf(t, "at its end, not stuck")
		}
	})
}

// An inset grouped list's rows are inset by the core's inset, on a card
// behind them (a section's each).
func TestListInsetGrouped(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{Appearance: ui.ListInsetGrouped}, signal.Static([]int{1, 2, 3}), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
				return ui.Text(signal.Func(func() string { return fmt.Sprint("Row ", item.Get()) }), ui.Style{Padding: ui.All(10)})
			})
	})
	settle(t, h, func() bool { return len(theList(h).shown) == 3 })
	onMain(func() {
		l := theList(h)
		in := l.inset
		if !l.grouped() || in <= 0 {
			fatalf(t, "grouped %v inset %v", l.grouped(), in)
		}
		f := l.shown[0].n.frame
		if f[0] != in || f[1] != in || f[2] != l.width-2*in {
			fatalf(t, "the first row is at %v, the list %v wide", f, l.width)
		}
		if w := find(h, ops.KindText)[0].frame[2]; w != l.width-2*in {
			fatalf(t, "the row's text is %v wide, its cell %v", w, l.width-2*in)
		}
		if len(l.cards) != 1 || !WidgetGetVisible(l.cards[0].widget) {
			fatalf(t, "no card")
		}
		if g := l.cards[0].frame; g[0] != in || g[1] != in || g[3] != l.tops[3]-in-l.tops[0] {
			fatalf(t, "the card is %v", g)
		}
	})
}

// A selecting list's click selects a row (RowSelected), which the app
// keeps and the row shows; its swipe actions are its context menu.
func TestListSelectionAndActions(t *testing.T) {
	selecting := signal.New(false)
	chosen := signal.NewFunc(map[int]bool{}, nil)
	var archived, pinned []int
	h, r := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{
			Selecting: selecting,
			Selected:  func(i int) bool { return chosen.Get()[i] },
			OnSelect: func(i int, on bool) {
				m := map[int]bool{}
				for k, v := range chosen.Peek() {
					m[k] = v
				}
				m[i] = on
				chosen.Set(m)
			},
			SwipeActions: func(i int) []ui.Action {
				return []ui.Action{{Title: ui.Val("Archive"), OnPress: func() { archived = append(archived, i) }}}
			},
			LeadingSwipeActions: func(i int) []ui.Action {
				return []ui.Action{{Title: ui.Val("Pin"), OnPress: func() { pinned = append(pinned, i) }}}
			},
		}, signal.Static([]int{0, 1, 2}), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node { return textRow(item, 40) })
	})
	settle(t, h, func() bool { return len(theList(h).shown) == 3 })
	onMain(func() {
		c := theList(h).shown[1].cells[0]
		if len(c.swipe) != 1 || len(c.leading) != 1 || c.n.events == nil || c.n.events.secondary == 0 {
			fatalf(t, "row 1's actions: %v, %v", c.swipe, c.leading)
		}
		// Its menu: Pin, then Archive.
		if !h.rowMenu(c, 10, 10) {
			fatalf(t, "no menu")
		}
		WidgetActivateAction(c.n.widget, "ryuirow.item0", 0)
		WidgetActivateAction(c.n.widget, "ryuirow.item1", 0)
	})
	settle(t, h, func() bool { return len(pinned) == 1 && len(archived) == 1 })
	if pinned[0] != 1 || archived[0] != 1 {
		t.Fatalf("pinned %v, archived %v", pinned, archived)
	}
	onMain(func() {
		h.enter(func() {
			selecting.Set(true)
			r.Flush()
		})
	})
	onMain(func() {
		l := theList(h)
		if !l.selecting {
			fatalf(t, "not selecting")
		}
		c := l.shown[2].cells[0]
		if h.rowMenu(c, 10, 10) {
			fatalf(t, "a selecting row's menu")
		}
		h.selectRow(c)
	})
	settle(t, h, func() bool { return chosen.Peek()[2] })
	onMain(func() {
		c := theList(h).shown[2].cells[0]
		if !c.selected {
			fatalf(t, "the row doesn't show it's selected")
		}
		// The app selects another: the row shows it.
		h.enter(func() {
			chosen.Set(map[int]bool{0: true})
			r.Flush()
		})
		if c.selected || !theList(h).shown[0].cells[0].selected {
			fatalf(t, "the rows don't show the app's selection")
		}
	})
}

// A split view's sidebar lists the destinations under their sections'
// titles, the chosen one selected; choosing a row shows that
// destination in the detail, under the window's header bar.
func TestSplitView(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.SplitView(ui.SplitViewProps{Title: ui.Val("Gallery"), Sections: []ui.SidebarSection{
			{Title: ui.Val("Views"), Destinations: []ui.Destination{
				{Title: ui.Val("Text"), Icon: "textformat", Content: func() ui.Node { return ui.StaticText("text page") }},
				{Title: ui.Val("Charts"), Icon: "chart.bar", Content: func() ui.Node { return ui.StaticText("charts page") }},
			}},
		}})
	})
	var s *split
	onMain(func() {
		sp := find(h, ops.KindSplit)
		if len(sp) != 1 {
			fatalf(t, "no split")
		}
		s = sp[0].ext.(*split)
		if len(s.widgets) != 3 || !s.rows[0].group {
			fatalf(t, "%d rows", len(s.widgets))
		}
		if ListBoxGetSelectedRow(s.box) != s.widgets[1] {
			fatalf(t, "the first destination isn't selected")
		}
	})
	settle(t, h, func() bool { return headerHome[h.main] == s.content })
	onMain(func() {
		ListBoxSelectRow(s.box, s.widgets[2])
	})
	settle(t, h, func() bool {
		for _, n := range find(h, ops.KindText) {
			if n.text == "charts page" && WidgetGetMapped(n.widget) {
				return true
			}
		}
		return false
	})
	onMain(func() {
		if s.selected != 1 {
			fatalf(t, "selected %d", s.selected)
		}
	})
}

// A popover is a GtkPopover from its anchor, its content's size; the user
// closing it reports Dismissed, and the core closing it closes it.
func TestPopover(t *testing.T) {
	open := signal.New(true)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(20)},
			ui.Popover(ui.PopoverProps{Open: open, Content: func() ui.Node {
				return ui.Column(ui.Style{Padding: ui.All(12)}, ui.StaticText("Hello"))
			}}, ui.Button(ui.ButtonProps{Title: ui.Val("Info")})))
	})
	pop := func() *popover {
		if ps := find(h, ops.KindPopover); len(ps) == 1 {
			return ps[0].ext.(*popover)
		}
		return nil
	}
	settle(t, h, func() bool { p := pop(); return p != nil && p.pop != 0 && WidgetGetMapped(p.pop) })
	settle(t, h, func() bool {
		n := find(h, ops.KindPopover)[0]
		_, _, w, ht := allocation(n.widget)
		return w > 24 && ht > 24 && w == float32(windowSize(n.ext.(*popover).w))
	})
	onMain(func() { PopoverPopdown(pop().pop) })
	settle(t, h, func() bool { return !open.Peek() && pop() == nil })
	onMain(func() { h.enter(func() { open.Set(true); r.Flush() }) })
	settle(t, h, func() bool { p := pop(); return p != nil && p.pop != 0 && WidgetGetMapped(p.pop) })
	var shown Ptr
	onMain(func() {
		shown = pop().pop
		ObjectRef(shown)
		h.enter(func() { open.Set(false); r.Flush() })
	})
	settle(t, h, func() bool { return pop() == nil && !WidgetGetMapped(shown) })
	onMain(func() { ObjectUnref(shown) })
	if open.Peek() {
		t.Fatal("open after the core closed it")
	}
}

// F5 refreshes a refreshable list on screen: Refresh, a spinner over
// its top until Refreshing goes false.
func TestListRefresh(t *testing.T) {
	refreshing := signal.New(false)
	refreshed := 0
	h, r := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{OnRefresh: func() { refreshed++; refreshing.Set(true) }, Refreshing: refreshing},
			signal.Static([]int{1, 2, 3}), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node { return textRow(item, 40) })
	})
	settle(t, h, func() bool { return len(theList(h).shown) == 3 })
	onMain(func() {
		l := theList(h)
		if !l.refreshable || l.shortcut == 0 {
			fatalf(t, "refreshable %v, shortcut %v", l.refreshable, l.shortcut)
		}
		if !h.refreshKey(l.n) {
			fatalf(t, "F5 didn't refresh")
		}
	})
	settle(t, h, func() bool {
		l := theList(h)
		return refreshed == 1 && l.refreshing && l.spinner != 0 && WidgetGetMapped(l.spinner)
	})
	onMain(func() {
		// Not again while it refreshes.
		if h.refreshKey(theList(h).n) {
			fatalf(t, "refreshed again while refreshing")
		}
		h.enter(func() {
			refreshing.Set(false)
			r.Flush()
		})
	})
	settle(t, h, func() bool { l := theList(h); return !l.refreshing && !WidgetGetVisible(l.spinner) })
}

// A narrow split view collapses: the sidebar, then the chosen
// destination's content.
func TestSplitViewCollapses(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.SplitView(ui.SplitViewProps{Title: ui.Val("Gallery"), Sections: []ui.SidebarSection{
			{Destinations: []ui.Destination{
				{Title: ui.Val("Text"), Content: func() ui.Node { return ui.StaticText("text page") }},
				{Title: ui.Val("Charts"), Content: func() ui.Node { return ui.StaticText("charts page") }},
			}},
		}})
	})
	var s *split
	onMain(func() {
		s = find(h, ops.KindSplit)[0].ext.(*split)
		if AdwNavigationSplitViewGetCollapsed(s.view) || len(s.widgets) != 2 {
			fatalf(t, "collapsed at %v wide, %d rows", h.main.envW, len(s.widgets))
		}
		WindowSetDefaultSize(h.main.window, 400, 600)
	})
	settle(t, h, func() bool { return AdwNavigationSplitViewGetCollapsed(s.view) })
	onMain(func() {
		if AdwNavigationSplitViewGetShowContent(s.view) {
			fatalf(t, "collapsed, it shows the content first")
		}
		ListBoxSelectRow(s.box, s.widgets[1])
		if !AdwNavigationSplitViewGetShowContent(s.view) {
			fatalf(t, "choosing a destination didn't show it")
		}
	})
	settle(t, h, func() bool {
		for _, n := range find(h, ops.KindText) {
			if n.text == "charts page" && WidgetGetMapped(n.widget) {
				return true
			}
		}
		return false
	})
}

// ScrollToItem scrolls the least that shows the item.
func TestListScrollToItem(t *testing.T) {
	items := make([]int, 1000)
	for i := range items {
		items[i] = i
	}
	var l ui.Node
	h, r := mount(t, func() ui.Node {
		l = ui.List(ui.ListProps{}, signal.Static(items), func(i int) int { return i },
			func(item signal.Accessor[int], _ signal.Accessor[int]) ui.Node { return textRow(item, 40) })
		return l
	})
	settle(t, h, func() bool { return len(theList(h).shown) > 0 })
	onMain(func() { h.enter(func() { ui.ScrollToItem(l, 500, false); r.Flush() }) })
	settle(t, h, func() bool {
		x := theList(h)
		return x.offset() == x.tops[501]-x.viewH() && x.shown[500] != nil
	})
	onMain(func() { h.enter(func() { ui.ScrollToItem(l, 10, true); r.Flush() }) })
	settle(t, h, func() bool { x := theList(h); return x.anim == 0 && x.offset() == x.tops[10] })
}
