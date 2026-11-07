package ui_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

type message struct {
	ID  int
	Day string
}

// A list anchored at its bottom says so; reaching its start loads
// earlier rows, once for each first item.
func TestListAnchorAndStart(t *testing.T) {
	items := signal.NewFunc([]message{{10, "b"}, {11, "b"}, {12, "b"}, {13, "b"}, {14, "b"}}, nil)
	starts := 0
	h, r := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{
			Anchor: ui.AnchorBottom,
			OnStartReached: func() {
				starts++
				if starts > 2 {
					return // the history's start
				}
				first := items.Peek()[0].ID
				var older []message
				for id := first - 3; id < first; id++ {
					older = append(older, message{id, "a"})
				}
				items.Set(append(older, items.Peek()...))
			},
		}, items, func(m message) int { return m.ID }, func(it signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
			return ui.Text(signal.Func(func() string { return fmt.Sprint(it.Get().ID) }))
		})
	})
	lv := h.OfKind(ops.KindList)[0]
	if lv.Int(ops.PropListAnchor) != int64(ui.AnchorBottom) {
		t.Fatal("no anchor")
	}
	cell := h.NewCell()
	// The end, first: nothing to load.
	h.Bind(lv, 4, cell)
	h.RunPosted()
	if starts != 0 {
		t.Fatalf("%d starts at the end", starts)
	}
	h.Bind(lv, 1, cell)
	h.Bind(lv, 0, cell)
	h.RunPosted()
	r.Flush()
	if starts != 1 || len(items.Peek()) != 8 {
		t.Fatalf("starts %d, %d items", starts, len(items.Peek()))
	}
	// The rows that were there keep their IDs; the new ones come first.
	if len(lv.Items) != 8 || lv.Items[3] != 1 {
		t.Fatalf("items %v", lv.Items)
	}
	// The same first item again: nothing; a new first item: more.
	h.Bind(lv, 2, cell)
	h.Bind(lv, 0, cell)
	h.RunPosted()
	if starts != 2 {
		t.Fatalf("starts %d after the new start", starts)
	}
	h.Bind(lv, 1, cell)
	h.RunPosted()
	if starts != 3 {
		t.Fatalf("starts %d after the newer start", starts)
	}
	h.Bind(lv, 0, cell)
	h.Bind(lv, 2, cell)
	h.RunPosted()
	if starts != 3 {
		t.Fatalf("starts %d: the same first item reached twice", starts)
	}
}

// Sections go with the items; headers bind with a negative index into
// cells of their own, and keep their section's ID while its key lasts.
func TestListSections(t *testing.T) {
	items := signal.NewFunc([]message{{1, "Mon"}, {2, "Mon"}, {3, "Tue"}, {4, "Wed"}, {5, "Wed"}}, nil)
	headers := 0
	h, r := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{
			Section: func(i int) string { return items.Peek()[i].Day },
			Header: func(day signal.Accessor[string]) ui.Node {
				headers++
				return ui.Text(signal.Func(func() string { return "— " + day.Get() }), ui.Style{Padding: ui.All(8)})
			},
		}, items, func(m message) int { return m.ID }, func(it signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
			return ui.Text(signal.Func(func() string { return fmt.Sprint(it.Get().ID) }))
		})
	})
	lv := h.OfKind(ops.KindList)[0]
	if got := []int{lv.Sections[0].Start, lv.Sections[1].Start, lv.Sections[2].Start}; len(lv.Sections) != 3 || !slices.Equal(got, []int{0, 2, 3}) {
		t.Fatalf("sections %+v", lv.Sections)
	}
	tue := lv.Sections[1].ID
	hc := h.NewCell()
	h.Bind(lv, -1-1, hc)
	if headers != 1 || hc.Text() != "— Tue" || hc.Frame[2] != 390 {
		t.Fatalf("header %q %v (%d made)", hc.Text(), hc.Frame, headers)
	}
	// Reused for another section: its text changes, nothing is made.
	h.Reset()
	h.Bind(lv, -1-2, hc)
	if headers != 1 || hc.Text() != "— Wed" || h.Count(ops.OpCreate) != 0 {
		t.Fatalf("rebound header %q, %d made\n%s", hc.Text(), headers, h.Log())
	}
	// Earlier messages: Tue keeps its ID, Sun is new.
	items.Set(append([]message{{0, "Sun"}}, items.Peek()...))
	r.Flush()
	if len(lv.Sections) != 4 || lv.Sections[2].ID != tue || lv.Sections[1].Start != 1 {
		t.Fatalf("sections %+v", lv.Sections)
	}
	// A cell made for a row can't become a header without its row going.
	rc := h.NewCell()
	h.Bind(lv, 0, rc)
	h.Bind(lv, -1, rc)
	if rc.Text() != "— Sun" {
		t.Fatalf("cell %q", rc.Text())
	}
	h.Unbind(lv, hc)
	if len(hc.Children) != 0 {
		t.Fatal("header not destroyed")
	}
}

// Leading swipe actions are their own prop; their Action events carry
// -1 - the index.
func TestListLeadingSwipe(t *testing.T) {
	items := signal.NewFunc([]int{0, 1, 2}, nil)
	var did []string
	h, _ := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{
			SwipeActions: func(i int) []ui.Action {
				return []ui.Action{{Title: ui.Val("Delete"), OnPress: func() { did = append(did, fmt.Sprint("delete ", i)) }}}
			},
			LeadingSwipeActions: func(i int) []ui.Action {
				return []ui.Action{
					{Title: ui.Val("Unread"), OnPress: func() { did = append(did, fmt.Sprint("unread ", i)) }},
					{Title: ui.Val("Pin"), OnPress: func() { did = append(did, fmt.Sprint("pin ", i)) }},
				}
			},
		}, items, func(i int) int { return i }, func(it signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
			return ui.Text(signal.Func(func() string { return fmt.Sprint(it.Get()) }))
		})
	})
	lv := h.OfKind(ops.KindList)[0]
	c := h.NewCell()
	h.Bind(lv, 1, c)
	if got := c.Actions(ops.PropLeadingSwipeActions); !slices.Equal(got, []string{"Unread", "Pin"}) {
		t.Fatalf("leading %v", got)
	}
	h.Dispatch(ops.Event{ID: c.ID, Kind: ops.EventAction, Int: -2})
	h.Dispatch(ops.Event{ID: c.ID, Kind: ops.EventAction, Int: 0})
	if !slices.Equal(did, []string{"pin 1", "delete 1"}) {
		t.Fatalf("did %v", did)
	}
}

// Selection: the list's mode, each row's state from Selected, the user's
// choices through OnSelect (what the app doesn't take goes back), the
// room the host's control takes, and a selection begun on a row.
func TestListSelection(t *testing.T) {
	items := signal.NewFunc([]int{0, 1, 2, 3}, nil)
	selecting := signal.New(false)
	chosen := signal.NewFunc(map[int]bool{}, nil)
	var begun []int
	h, r := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{
			Selecting: selecting,
			Selected:  func(i int) bool { return chosen.Get()[items.Peek()[i]] },
			OnSelect: func(i int, v bool) {
				if i == 3 {
					return // row 3 can't be selected
				}
				m := map[int]bool{}
				for k, x := range chosen.Peek() {
					m[k] = x
				}
				m[items.Peek()[i]] = v
				chosen.Set(m)
			},
			OnSelectBegin: func(i int) {
				begun = append(begun, i)
				selecting.Set(true)
			},
		}, items, func(i int) int { return i }, func(it signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
			return ui.Text(signal.Func(func() string { return fmt.Sprint(it.Get()) }))
		})
	})
	lv := h.OfKind(ops.KindList)[0]
	cells := make([]*fake.View, 4)
	h.Reset()
	for i := range cells {
		cells[i] = h.NewCell()
		h.Bind(lv, i, cells[i])
	}
	if n := countProp(h, ops.PropSelected); n != 0 {
		t.Fatalf("%d Selected props for rows none of which is selected\n%s", n, h.Log())
	}
	h.Dispatch(ops.Event{ID: cells[2].ID, Kind: ops.EventRowSelectBegin})
	if !lv.Bool(ops.PropListSelecting) || !slices.Equal(begun, []int{2}) {
		t.Fatalf("selecting %v, begun %v", lv.Bool(ops.PropListSelecting), begun)
	}
	// The user selects row 1: the host shows it already, nothing is sent.
	h.Reset()
	h.SelectRow(cells[1], true)
	if !chosen.Peek()[1] || countProp(h, ops.PropSelected) != 0 {
		t.Fatalf("chosen %v\n%s", chosen.Peek(), h.Log())
	}
	// Row 3 isn't taken: it goes back.
	h.SelectRow(cells[3], true)
	if cells[3].Bool(ops.PropSelected) || countProp(h, ops.PropSelected) != 1 {
		t.Fatalf("row 3 selected\n%s", h.Log())
	}
	// The app selects all: the rows follow.
	chosen.Set(map[int]bool{0: true, 1: true, 2: true, 3: true})
	r.Flush()
	for i, c := range cells {
		if !c.Bool(ops.PropSelected) {
			t.Fatalf("row %d not selected", i)
		}
	}
	// A recycled cell shows its new row's state.
	chosen.Set(map[int]bool{})
	r.Flush()
	if cells[0].Bool(ops.PropSelected) {
		t.Fatal("row 0 still selected")
	}
	// The selection control's room: the rows are laid out narrower.
	h.Dispatch(ops.Event{ID: lv.ID, Kind: ops.EventRowInset, X: 40, Y: 0})
	if cells[0].Frame[2] != 390-40 {
		t.Fatalf("row %v, want 350 wide", cells[0].Frame)
	}
	h.Dispatch(ops.Event{ID: lv.ID, Kind: ops.EventRowInset})
	if cells[0].Frame[2] != 390 {
		t.Fatalf("row %v, want 390 wide", cells[0].Frame)
	}
}

func countProp(h *fake.Host, p ops.PropID) int {
	n := 0
	for _, op := range h.Ops {
		if op.Code == ops.OpSetProp && op.Prop == p {
			n++
		}
	}
	return n
}

// A key that comes back after another section keeps its section's ID
// (by which time it comes), so hosts don't remake its header.
func TestListSectionIDsOfRepeatedKeys(t *testing.T) {
	items := signal.NewFunc([]message{{1, "Ada"}, {2, "Bob"}, {3, "Ada"}}, nil)
	h, r := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{
			Section: func(i int) string { return items.Peek()[i].Day },
			Header:  func(k signal.Accessor[string]) ui.Node { return ui.Text(k) },
		}, items, func(m message) int { return m.ID }, func(it signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
			return ui.Text(signal.Func(func() string { return fmt.Sprint(it.Get().ID) }))
		})
	})
	lv := h.OfKind(ops.KindList)[0]
	before := slices.Clone(lv.Sections)
	items.Set(append(slices.Clone(items.Peek()), message{4, "Ada"}))
	r.Flush()
	if len(lv.Sections) != 3 || lv.Sections[0].ID != before[0].ID || lv.Sections[2].ID != before[2].ID || lv.Sections[0].ID == lv.Sections[2].ID {
		t.Fatalf("before %+v, after %+v", before, lv.Sections)
	}
}

// A cell a selected, swipeable row was in that becomes a header (desktop
// hosts reuse cells for both) loses the row's state, and one that
// becomes a row again shows its own.
func TestListCellBetweenHeaderAndRow(t *testing.T) {
	items := signal.NewFunc([]message{{1, "Mon"}, {2, "Tue"}}, nil)
	h, r := mount(t, func() ui.Node {
		return ui.List(ui.ListProps{
			Section:      func(i int) string { return items.Peek()[i].Day },
			Header:       func(k signal.Accessor[string]) ui.Node { return ui.Text(k) },
			Selecting:    ui.Val(true),
			Selected:     func(i int) bool { return i == 0 },
			SwipeActions: func(i int) []ui.Action { return []ui.Action{{Title: ui.Val("Delete")}} },
		}, items, func(m message) int { return m.ID }, func(it signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
			return ui.Text(signal.Func(func() string { return fmt.Sprint(it.Get().ID) }))
		})
	})
	_ = r
	lv := h.OfKind(ops.KindList)[0]
	c := h.NewCell()
	h.Bind(lv, 0, c)
	if !c.Bool(ops.PropSelected) || len(c.Actions(ops.PropSwipeActions)) != 1 {
		t.Fatal("the row isn't selected and swipeable")
	}
	h.Bind(lv, -1, c)
	if c.Bool(ops.PropSelected) || len(c.Actions(ops.PropSwipeActions)) != 0 || c.Text() != "Mon" {
		t.Fatalf("the header keeps the row's state: %v %v %q", c.Bool(ops.PropSelected), c.Actions(ops.PropSwipeActions), c.Text())
	}
	h.Bind(lv, 1, c) // row 1: not selected, sent as it is
	if _, ok := c.Props[ops.PropSelected]; !ok || c.Bool(ops.PropSelected) {
		t.Fatal("the row's state isn't sent")
	}
}
