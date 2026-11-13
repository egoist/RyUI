package ui

import (
	"encoding/binary"
	"math"

	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

type listImpl interface {
	count() int
	bind(index int, cell ops.NodeID)
	unbind(cell ops.NodeID)
	layoutCells(p *layout.Pass)
	eachCell(f func(*element))
}

// ListProps configures a List.
type ListProps struct {
	// Style is the list's own box. A List fills the space it's given:
	// with no Height and no Grow, it grows.
	Style Styler
	// OnRefresh enables pull to refresh; see ScrollProps.
	OnRefresh  func()
	Refreshing signal.Accessor[bool]
	// OnEndReached is called when the last rows are about to show, to
	// load more: once for each length the list has.
	OnEndReached func()
	// OnStartReached is called when the first rows are about to show, to
	// load earlier ones (a chat's older messages, in a list anchored at
	// its bottom): once for each first item. Rows added before the ones
	// on screen don't move those.
	OnStartReached func()
	// Anchor is where the list rests. AnchorBottom is a chat's: the list
	// opens at its last row, rows that don't fill it sit at its bottom,
	// and while it's scrolled to its end it stays there, as rows are
	// added at the end and as the list gets shorter (the keyboard), as
	// SwiftUI's defaultScrollAnchor(.bottom) does. Whatever the anchor,
	// rows added or removed above the ones on screen keep those where
	// they are (React Native's maintainVisibleContentPosition), unless
	// the list is at the edge it's anchored to.
	Anchor ScrollAnchor
	// SwipeActions are row index's actions, revealed by swiping it: on
	// iOS its trailing actions (a destructive one red, the first a full
	// swipe's); on Android, likewise, Material 3 Expressive's swipe to
	// reveal (the first at the end edge, a full swipe's).
	SwipeActions func(index int) []Action
	// LeadingSwipeActions are revealed by swiping row index the other
	// way, from its leading edge (Mail's Unread and Pin): the first is
	// at the leading edge, and a full swipe's.
	LeadingSwipeActions func(index int) []Action
	// OnScroll is called as it scrolls; see ScrollProps.
	OnScroll func(x, y float32)
	// Columns makes it a grid (a LazyVGrid, a GridLayoutManager): items
	// fill rows of this many, each as wide as the others, Gap apart
	// across and down; a row is as tall as its tallest. A grid has no
	// separators and no swipe actions.
	Columns int
	Gap     float32
	// Appearance is the list's look; a grid's is plain.
	Appearance ListAppearance

	// Section puts the rows in sections: rows next to each other with
	// the same key are one, headed by Header's view (a day in a chat, a
	// letter in contacts). In a plain or bare list, a section's header
	// sticks to the list's top while its rows scroll under it, as
	// UITableView's plain style does; in an inset grouped list, each
	// section is a group of its own, its header above it. Read as the
	// items change, without following signals.
	Section func(index int) string
	// Header is a section's header, given its key. It's a row of its
	// own: made as it comes into view, reused for another section after.
	Header func(section signal.Accessor[string]) Node

	// Selecting puts the list in selection mode (Mail's Edit): each row
	// shows the platform's selection control (UIKit's edit mode circles;
	// a Material checkbox at its end on Android; on desktop, rows are
	// highlighted), a tap selects or deselects it instead of pressing it,
	// and rows don't swipe.
	Selecting signal.Accessor[bool]
	// Selected reports whether row index is selected; it's followed in
	// an effect, so a selection kept in signals shows as it changes.
	Selected func(index int) bool
	// OnSelect is called as the user selects (true) or deselects row
	// index; the app updates what Selected reads. What it doesn't take
	// (Selected still says otherwise) goes back as it was.
	OnSelect func(index int, selected bool)
	// OnSelectBegin is called as the user starts selecting at row index
	// while the list isn't selecting: a two-finger pan over rows on iOS,
	// a long press on Android (Gmail's). The app turns Selecting on and
	// selects the row; on iOS the pan goes on to select the rows it
	// crosses.
	OnSelectBegin func(index int)
}

// ScrollAnchor is where a List rests: its start or its end.
type ScrollAnchor = ops.ScrollAnchor

const (
	// AnchorTop is a list read from its start.
	AnchorTop = ops.ScrollAnchorTop
	// AnchorBottom is a list read from its end: a chat's messages.
	AnchorBottom = ops.ScrollAnchorBottom
)

// ListAppearance is a List's look.
type ListAppearance = ops.ListAppearance

const (
	// ListPlain is rows edge to edge, apart by separators.
	ListPlain = ops.ListAppearancePlain
	// ListInsetGrouped is the rows as one group, inset from the list's
	// sides by the platform's margins, on the grouped background: iOS's
	// inset grouped list (one rounded box, rows apart by separators);
	// Android's Material 3 Expressive segmented list (rows apart by a
	// gap, the group's ends rounded); a grouped form on macOS, as System
	// Settings'; Windows 11's settings cards. Rows are laid out at the
	// list's width less the insets.
	ListInsetGrouped = ops.ListAppearanceInsetGrouped
	// ListBare is rows with nothing around them: no separators, and no
	// background, so what's behind the list shows through. Its rows draw
	// their own cards and leave their own space, and can still be swiped.
	ListBare = ops.ListAppearanceBare
)

// listInset is an inset grouped list's rows' inset from each side: the
// platform's margins (UIKit's layout margins, Material's window margins,
// System Settings' grouped forms, Windows' settings pages).
func listInset(env Environment) float32 {
	wide := env.WidthClass != SizeClassCompact
	switch env.Platform {
	case PlatformIOS:
		if wide {
			return 20
		}
	case PlatformAndroid:
		if wide {
			return 24
		}
	case PlatformMacOS:
		return 20
	}
	return 16
}

type listCell[T any, K comparable] struct {
	el      *element
	item    *signal.Signal[T]
	index   *signal.Signal[int]
	key     K
	dispose func()
	width   float32
	actions []Action // its swipe actions
	sent    []byte   // as last sent
	leading []Action // its leading swipe actions
	sentL   []byte   // as last sent
	sel     int8     // 1: the host shows it selected; -1: unknown
	// A section header's.
	header  bool
	section *signal.Signal[string]
	secID   int64
}

// listSection is a run of rows with the same Section key.
type listSection struct {
	start int
	key   string
	id    int64
}

type listState[T any, K comparable] struct {
	e        *element
	key      func(T) K
	row      func(signal.Accessor[T], signal.Accessor[int]) Node
	eq       func(a, b T) bool
	owner    *signal.Owner
	items    []T
	ids      map[K]int64
	next     int64
	cells    map[ops.NodeID]*listCell[T, K]
	order    []*listCell[T, K]
	swipe    func(int) []Action
	lead     func(int) []Action
	end      func()
	endAt    int // the length the end was last reached at
	start    func()
	startKey K    // the first item the start was last reached at
	started  bool // startKey is set
	// Sections.
	section  func(int) string
	header   func(signal.Accessor[string]) Node
	sections []listSection
	secIDs   map[string][]int64 // a key's sections' IDs, in the order they come
	// Selection.
	selected    func(int) bool
	onSelect    func(int, bool)
	selectBegin func(int)
	// The room the host's selection control takes from the rows.
	rowLead, rowTrail float32
	// A grid's.
	columns int
	gap     float32
	inset   float32 // an inset grouped list's, each side
}

// List is For backed by the platform's recycling list (UICollectionView,
// RecyclerView): rows are made only for the cells on screen, when the
// host binds them, and a recycled cell rebinds its row to another item,
// updating only the props that differ.
func List[T any, K comparable](p ListProps, items signal.Accessor[[]T], key func(T) K, row func(item signal.Accessor[T], index signal.Accessor[int]) Node) Node {
	e := newElement(ops.KindList, true)
	e.setStyle(p.Style, false)
	growByDefault(e)
	refreshable(e, p.OnRefresh, p.Refreshing, ops.PropListRefreshable, ops.PropListRefreshing)
	onScroll(e, p.OnScroll)
	if p.Columns > 1 {
		e.r.batch.SetInt(e.id, ops.PropListColumns, int64(p.Columns))
		if p.Gap > 0 {
			e.r.batch.SetFloat(e.id, ops.PropListGap, float64(p.Gap))
		}
		p.SwipeActions = nil
		p.Appearance = ListPlain
	}
	if p.Columns > 1 {
		// A grid has no swipe actions, and reads from its top (Android's
		// grid can't stack from its end).
		p.LeadingSwipeActions = nil
		p.Anchor = AnchorTop
	}
	st := &listState[T, K]{
		e: e, key: key, row: row, eq: equality[T](),
		owner: signal.CurrentOwner(),
		ids:   map[K]int64{},
		cells: map[ops.NodeID]*listCell[T, K]{},
		swipe: p.SwipeActions, lead: p.LeadingSwipeActions,
		end: p.OnEndReached, endAt: -1, start: p.OnStartReached,
		selected: p.Selected, onSelect: p.OnSelect, selectBegin: p.OnSelectBegin,
		columns: max(p.Columns, 1), gap: p.Gap,
	}
	if p.Section != nil && p.Header != nil {
		st.section, st.header = p.Section, p.Header
		st.secIDs = map[string][]int64{}
	}
	e.list = st
	e.r.lists = append(e.r.lists, e)
	if p.Anchor != AnchorTop {
		e.r.batch.SetEnum(e.id, ops.PropListAnchor, int32(p.Anchor))
	}
	if p.Selecting != nil {
		bind(p.Selecting, func(v bool) { e.r.batch.SetBool(e.id, ops.PropListSelecting, v) })
		e.handlers().rowInset = func(lead, trail float32) {
			// The rows are laid out again at the width they have.
			st.rowLead, st.rowTrail = max(lead, 0), max(trail, 0)
		}
		e.listen(ops.EventRowInset)
	}
	if p.Appearance != ListPlain {
		e.r.batch.SetEnum(e.id, ops.PropListAppearance, int32(p.Appearance))
	}
	if p.Appearance == ListInsetGrouped {
		env := UseEnvironment()
		signal.RenderEffect(func() {
			// The rows are laid out again at the new width.
			if in := listInset(env.Get()); in != st.inset {
				st.inset = in
				e.r.batch.SetFloat(e.id, ops.PropListInset, float64(in))
			}
		})
	}
	signal.RenderEffect(func() {
		list := items.Get()
		signal.Untrack(func() struct{} {
			st.update(list)
			return struct{}{}
		})
	})
	signal.OnCleanup(func() {
		for _, c := range st.order {
			c.dispose()
		}
	})
	return Node{e}
}

// update sends the new item IDs (the host diffs them) and updates the
// rows that are bound.
func (st *listState[T, K]) update(list []T) {
	st.items = list
	ids := make(map[K]int64, len(list))
	index := make(map[K]int, len(list))
	st.e.r.batch.CommandArgs(st.e.id, ops.CmdListUpdate, func(b []byte) []byte {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(list)))
		for i, it := range list {
			k := st.key(it)
			id, ok := st.ids[k]
			if !ok {
				st.next++
				id = st.next
			}
			if _, dup := ids[k]; dup { // duplicate keys get their own IDs
				st.next++
				id = st.next
			} else {
				ids[k] = id
				index[k] = i
			}
			b = binary.LittleEndian.AppendUint64(b, uint64(id))
		}
		if st.section != nil {
			st.makeSections(len(list))
			b = binary.LittleEndian.AppendUint32(b, uint32(len(st.sections)))
			for _, s := range st.sections {
				b = binary.LittleEndian.AppendUint32(b, uint32(s.start))
				b = binary.LittleEndian.AppendUint64(b, uint64(s.id))
			}
		}
		return b
	})
	st.ids = ids
	for _, c := range st.order {
		if c.header {
			continue // a section's ID keeps its key; the host rebinds the rest
		}
		if i, ok := index[c.key]; ok {
			c.item.Set(list[i])
			c.index.Set(i)
			st.swipeActions(c, i)
		}
	}
}

// makeSections splits the n items into runs of the same Section key,
// each with an ID it keeps while its key does; a key that comes back
// after another run (a sender's messages, then another's, then the
// first's again) is told apart by which time it comes, and keeps its ID
// too.
func (st *listState[T, K]) makeSections(n int) {
	st.sections = st.sections[:0]
	ids := make(map[string][]int64, len(st.secIDs))
	for i := range n {
		k := st.section(i)
		if len(st.sections) > 0 && st.sections[len(st.sections)-1].key == k {
			continue
		}
		nth := len(ids[k])
		var id int64
		if old := st.secIDs[k]; nth < len(old) {
			id = old[nth]
		} else {
			st.next++
			id = st.next
		}
		ids[k] = append(ids[k], id)
		st.sections = append(st.sections, listSection{start: i, key: k, id: id})
	}
	st.secIDs = ids
}

// swipeActions sends cell c's swipe actions for row i, those that changed.
func (st *listState[T, K]) swipeActions(c *listCell[T, K], i int) {
	// In an effect, the titles and what the actions depend on are followed.
	if st.swipe != nil {
		c.actions = st.swipe(i)
		if b := encodeActions(c.actions); string(b) != string(c.sent) {
			c.sent = b
			st.e.r.batch.SetBytes(c.el.id, ops.PropSwipeActions, b)
		}
	}
	if st.lead != nil {
		c.leading = st.lead(i)
		if b := encodeActions(c.leading); string(b) != string(c.sentL) {
			c.sentL = b
			st.e.r.batch.SetBytes(c.el.id, ops.PropLeadingSwipeActions, b)
		}
	}
}

// reachedStart calls OnStartReached when row i is near the start, once
// per first item.
func (st *listState[T, K]) reachedStart(i int) {
	if st.start == nil || i > 2 || len(st.items) == 0 {
		return
	}
	k := st.key(st.items[0])
	if st.started && k == st.startKey {
		return
	}
	st.started, st.startKey = true, k
	Post(st.start)
}

// showSelected sends whether cell c's row is selected, if the host shows otherwise.
func (st *listState[T, K]) showSelected(c *listCell[T, K], v bool) {
	var s int8
	if v {
		s = 1
	}
	if c.sel == s {
		return
	}
	c.sel = s
	st.e.r.batch.SetBool(c.el.id, ops.PropSelected, v)
}

// reached calls OnEndReached when row i is near the end, once per length.
func (st *listState[T, K]) reached(i int) {
	n := len(st.items)
	if st.end == nil || i < n-3 || n == st.endAt {
		return
	}
	st.endAt = n
	// Rows bind while the host lays out: not then.
	Post(st.end)
}

func (st *listState[T, K]) count() int { return len(st.items) }

func (st *listState[T, K]) bind(i int, cell ops.NodeID) {
	if i < 0 {
		st.bindHeader(-1-i, cell)
		return
	}
	if i >= len(st.items) {
		return
	}
	item := st.items[i]
	st.reached(i)
	st.reachedStart(i)
	c := st.cells[cell]
	wasHeader := c != nil && c.header
	if wasHeader {
		st.unbind(cell)
		c = nil
	}
	if c != nil {
		c.item.Set(item)
		c.index.Set(i)
		c.key = st.key(item)
		st.swipeActions(c, i)
		return
	}
	c = &listCell[T, K]{key: st.key(item)}
	if wasHeader {
		c.sel = -1 // the host's state is a header's: unknown, sent at once
	}
	cellOwner := st.makeCell(c, cell, func() Node {
		c.item = signal.NewFunc(item, st.eq)
		c.index = signal.New(i)
		return st.row(c.item, c.index)
	})
	el := c.el
	if st.swipe != nil || st.lead != nil {
		el.handlers().action = func(k int64) {
			if k < 0 {
				runAction(c.leading, -1-k)
			} else {
				runAction(c.actions, k)
			}
		}
		el.listen(ops.EventAction)
		signal.RunWithOwner(cellOwner, func() {
			signal.RenderEffect(func() { st.swipeActions(c, c.index.Get()) })
		})
	}
	if st.selected != nil {
		signal.RunWithOwner(cellOwner, func() {
			signal.RenderEffect(func() { st.showSelected(c, st.selected(c.index.Get())) })
		})
	}
	if st.onSelect != nil || st.selected != nil {
		el.handlers().rowSelected = func(v bool) {
			// The host shows it as the user left it; what the app doesn't
			// take goes back.
			if v {
				c.sel = 1
			} else {
				c.sel = 0
			}
			i := c.index.Peek()
			if st.onSelect != nil {
				st.onSelect(i, v)
			}
			if st.selected != nil {
				st.showSelected(c, signal.Untrack(func() bool { return st.selected(i) }))
			}
		}
		el.listen(ops.EventRowSelected)
	}
	if st.selectBegin != nil {
		el.handlers().rowSelectBegin = func() { st.selectBegin(c.index.Peek()) }
		el.listen(ops.EventRowSelectBegin)
	}
}

// bindHeader binds section s's header into cell.
func (st *listState[T, K]) bindHeader(s int, cell ops.NodeID) {
	if s >= len(st.sections) {
		return
	}
	sec := st.sections[s]
	c := st.cells[cell]
	if c != nil && !c.header {
		// A cell a row was in (a desktop host reuses its cells for both):
		// none of the row's state stays with it.
		r := st.e.r
		if c.sel != 0 {
			r.batch.SetBool(cell, ops.PropSelected, false)
		}
		if len(c.sent) > 0 {
			r.batch.SetBytes(cell, ops.PropSwipeActions, encodeActions(nil))
		}
		if len(c.sentL) > 0 {
			r.batch.SetBytes(cell, ops.PropLeadingSwipeActions, encodeActions(nil))
		}
		st.unbind(cell)
		c = nil
	}
	if c != nil {
		c.section.Set(sec.key)
		c.secID = sec.id
		return
	}
	c = &listCell[T, K]{header: true, secID: sec.id}
	st.makeCell(c, cell, func() Node {
		c.section = signal.New(sec.key)
		return st.header(c.section)
	})
}

// makeCell makes the element of a cell the host made, and its row (or
// header) under an owner of its own; it returns the owner.
func (st *listState[T, K]) makeCell(c *listCell[T, K], cell ops.NodeID, build func() Node) *signal.Owner {
	r := st.e.r
	el := &element{r: r, id: cell, kind: ops.KindView, real: true, cell: true, up: st.e, dir: st.e.dir, sent: [4]float32{nan32, nan32, nan32, nan32}}
	el.box = layout.New()
	el.box.Context = el
	ls := layout.DefaultStyle
	ls.FlexDirection = layout.Column
	ls.Width = layout.Pct(100)
	ls.Direction = el.layoutDirection()
	el.box.SetStyle(ls)
	if el.dir != InheritDirection {
		r.batch.SetEnum(cell, ops.PropLayoutDirection, int32(el.dir))
	}
	elems[cell] = el
	c.el = el
	var cellOwner *signal.Owner
	signal.RunWithOwner(st.owner, func() {
		signal.Root(func(d func()) {
			c.dispose = d
			cellOwner = signal.CurrentOwner()
			if n := withRoot(r, build); n.e != nil {
				el.appendKid(n.e)
			}
		})
	})
	st.cells[cell] = c
	st.order = append(st.order, c)
	return cellOwner
}

func onScroll(e *element, f func(x, y float32)) {
	if f != nil {
		e.handlers().scrolled = f
		e.listen(ops.EventScrolled)
	}
}

// ScrollTo scrolls a Scroll or List to the content offset x, y (points
// from its content's top left, as OnScroll reports; past the end is the
// end).
func ScrollTo(scroll Node, x, y float32, animated bool) {
	e := scroll.e
	if e == nil || (e.kind != ops.KindScroll && e.kind != ops.KindList) {
		return
	}
	var args [9]byte
	binary.LittleEndian.PutUint32(args[:], math.Float32bits(x))
	binary.LittleEndian.PutUint32(args[4:], math.Float32bits(y))
	if animated {
		args[8] = 1
	}
	e.r.batch.Command(e.id, ops.CmdScrollToOffset, args[:])
}

// ScrollToEnd scrolls a Scroll or List to its end: its bottom, or a
// horizontal one's trailing edge.
func ScrollToEnd(scroll Node, animated bool) {
	ScrollTo(scroll, math.MaxFloat32, math.MaxFloat32, animated)
}

// ScrollToItem scrolls a List so that item index shows.
func ScrollToItem(list Node, index int, animated bool) {
	e := list.e
	if e == nil || e.kind != ops.KindList {
		return
	}
	var args [5]byte
	binary.LittleEndian.PutUint32(args[:], uint32(index))
	if animated {
		args[4] = 1
	}
	e.r.batch.Command(e.id, ops.CmdScrollTo, args[:])
}

func (st *listState[T, K]) unbind(cell ops.NodeID) {
	c := st.cells[cell]
	if c == nil {
		return
	}
	c.dispose()
	for _, k := range c.el.kids {
		c.el.removeKid(k, true)
	}
	delete(st.cells, cell)
	delete(elems, cell)
	for i, x := range st.order {
		if x == c {
			st.order = append(st.order[:i], st.order[i+1:]...)
			break
		}
	}
}

func (st *listState[T, K]) eachCell(f func(*element)) {
	for _, c := range st.order {
		f(c.el)
	}
}

// layoutCells lays out the bound rows at the list's width (an inset
// grouped list's less its insets, a grid's cells at a column's); each
// cell's frame tells the host the row's height.
func (st *listState[T, K]) layoutCells(p *layout.Pass) {
	hw := st.e.box.W - 2*st.inset // a header's: the whole row
	w := hw - st.rowLead - st.rowTrail
	if st.columns > 1 {
		hw = st.e.box.W
		w = (w - st.gap*float32(st.columns-1)) / float32(st.columns)
	}
	for _, c := range st.order {
		cw := w
		if c.header {
			cw = hw
		}
		if cw <= 0 {
			continue
		}
		if c.el.box.Dirty() || c.width != cw {
			c.width = cw
			c.el.box.Layout(layout.Size{W: cw, H: layout.MaxContentSpace}, p)
		}
	}
}
