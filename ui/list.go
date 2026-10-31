package ui

import (
	"encoding/binary"
	"math"

	"github.com/egoist/ryui/layout"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
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
	// SwipeActions are row index's actions, revealed by swiping it: on
	// iOS its trailing actions (a destructive one red, the first a full
	// swipe's); on Android a full swipe runs the first, over its color
	// and icon, as Material's swipe to dismiss.
	SwipeActions func(index int) []Action
	// OnScroll is called as it scrolls; see ScrollProps.
	OnScroll func(x, y float32)
	// Columns makes it a grid (a LazyVGrid, a GridLayoutManager): items
	// fill rows of this many, each as wide as the others, Gap apart
	// across and down; a row is as tall as its tallest. A grid has no
	// separators and no swipe actions.
	Columns int
	Gap     float32
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
}

type listState[T any, K comparable] struct {
	e     *element
	key   func(T) K
	row   func(signal.Accessor[T], signal.Accessor[int]) Node
	eq    func(a, b T) bool
	owner *signal.Owner
	items []T
	ids   map[K]int64
	next  int64
	cells map[ops.NodeID]*listCell[T, K]
	order []*listCell[T, K]
	swipe func(int) []Action
	end   func()
	endAt int // the length the end was last reached at
	// A grid's.
	columns int
	gap     float32
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
	}
	st := &listState[T, K]{
		e: e, key: key, row: row, eq: equality[T](),
		owner: signal.CurrentOwner(),
		ids:   map[K]int64{},
		cells: map[ops.NodeID]*listCell[T, K]{},
		swipe: p.SwipeActions, end: p.OnEndReached, endAt: -1,
		columns: max(p.Columns, 1), gap: p.Gap,
	}
	e.list = st
	e.r.lists = append(e.r.lists, e)
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
		return b
	})
	st.ids = ids
	for _, c := range st.order {
		if i, ok := index[c.key]; ok {
			c.item.Set(list[i])
			c.index.Set(i)
			st.swipeActions(c, i)
		}
	}
}

// swipeActions sends cell c's swipe actions for row i, if they changed.
func (st *listState[T, K]) swipeActions(c *listCell[T, K], i int) {
	if st.swipe == nil {
		return
	}
	c.actions = st.swipe(i)
	b := encodeActions(c.actions)
	if string(b) == string(c.sent) {
		return
	}
	c.sent = b
	st.e.r.batch.SetBytes(c.el.id, ops.PropSwipeActions, b)
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
	if i < 0 || i >= len(st.items) {
		return
	}
	item := st.items[i]
	st.reached(i)
	c := st.cells[cell]
	if c != nil {
		c.item.Set(item)
		c.index.Set(i)
		c.key = st.key(item)
		st.swipeActions(c, i)
		return
	}
	r := st.e.r
	el := &element{r: r, id: cell, kind: ops.KindView, real: true, cell: true, up: st.e, sent: [4]float32{nan32, nan32, nan32, nan32}}
	el.box = layout.New()
	el.box.Context = el
	ls := layout.DefaultStyle
	ls.FlexDirection = layout.Column
	ls.Width = layout.Pct(100)
	el.box.SetStyle(ls)
	elems[cell] = el
	c = &listCell[T, K]{el: el, key: st.key(item)}
	signal.RunWithOwner(st.owner, func() {
		signal.Root(func(d func()) {
			c.dispose = d
			c.item = signal.NewFunc(item, st.eq)
			c.index = signal.New(i)
			if n := withRoot(r, func() Node { return st.row(c.item, c.index) }); n.e != nil {
				el.appendKid(n.e)
			}
		})
	})
	st.cells[cell] = c
	st.order = append(st.order, c)
	if st.swipe != nil {
		el.handlers().action = func(k int64) { runAction(c.actions, k) }
		el.listen(ops.EventAction)
		st.swipeActions(c, i)
	}
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

// layoutCells lays out the bound rows at the list's width (a grid's
// cells at a column's); each cell's frame tells the host the row's
// height.
func (st *listState[T, K]) layoutCells(p *layout.Pass) {
	w := st.e.box.W
	if st.columns > 1 {
		w = (w - st.gap*float32(st.columns-1)) / float32(st.columns)
	}
	if w <= 0 {
		return
	}
	for _, c := range st.order {
		if c.el.box.Dirty() || c.width != w {
			c.width = w
			c.el.box.Layout(layout.Size{W: w, H: layout.MaxContentSpace}, p)
		}
	}
}
