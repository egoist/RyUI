//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"sort"

	. "github.com/egoist/nui/internal/darwin"
	"github.com/egoist/nui/ops"
)

// A List is a scroll view whose rows the host places itself: as it
// scrolls, rows coming into view take a row view (a recycled one, or a
// new one) and its cells, host-made views the core binds rows into
// through ListBind; rows going out are kept for reuse. A row's height
// is its cells' (a grid's row holds Columns of them), from their frames;
// rows not yet bound are estimated. (NSTableView would ask for heights
// with a callback returning a float, which purego can't make.)
type list struct {
	n       *node
	scroll  ID
	doc     ID // the flipped document view the rows are in
	ids     []int64
	rowOf   map[int64]int
	heights map[int64]float64
	tops    []float64 // each row's top, then the total height
	shown   map[int]*rowView
	free    []*rowView
	columns int
	gap     float64
	width   float64
	est     float64

	pending []int64
	updated bool // pending waits for the core to return
	tiling  bool
	queued  bool
	rebind  bool
}

type rowView struct {
	view  ID
	cells []*cell
	sep   ID
	row   int
}

// cell is a host-made view a row is bound into.
type cell struct {
	n      *node
	l      *list
	item   int64
	height float64
	swipe  []action
	rv     *rowView
}

func newList(h *Host, n *node) *list {
	l := &list{n: n, columns: 1, est: 44, rowOf: map[int64]int{}, heights: map[int64]float64{}, shown: map[int]*rowView{}}
	l.scroll = initView("NSScrollView")
	Send(l.scroll, "setHasVerticalScroller:", 1)
	Send(l.scroll, "setAutohidesScrollers:", 1)
	Send(l.scroll, "setAutomaticallyAdjustsContentInsets:", 0)
	Send(l.scroll, "setDrawsBackground:", 0)
	l.doc = initView("NuiView")
	Send(l.scroll, "setDocumentView:", uintptr(l.doc))
	Release(l.doc)
	clip := Send(l.scroll, "contentView")
	Send(clip, "setDrawsBackground:", 0)
	Send(clip, "setPostsBoundsChangedNotifications:", 1)
	center := Send(Class("NSNotificationCenter"), "defaultCenter")
	Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("boundsChanged:")),
		uintptr(String("NSViewBoundsDidChangeNotification")), uintptr(clip))
	n.scroll = true // it observes its scroll position
	h.lists[clip] = l
	return l
}

func (l *list) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropListColumns:
		l.columns = max(int(v.Int), 1)
	case ops.PropListGap:
		l.gap = v.Float
	}
	l.rebind = true
	l.queue(h)
}

func (l *list) background(h *Host, n *node) {
	Send(l.scroll, "setDrawsBackground:", 0)
}

// update takes the core's new item IDs; rows bind again once the core
// has returned, so binding never re-enters a flush.
func (l *list) update(h *Host, args []byte) {
	if len(args) < 4 {
		return
	}
	count := int(binary.LittleEndian.Uint32(args))
	ids := make([]int64, 0, count)
	for i := range count {
		o := 4 + i*8
		if o+8 > len(args) {
			break
		}
		ids = append(ids, int64(binary.LittleEndian.Uint64(args[o:])))
	}
	l.pending = ids
	l.updated = true
	l.rebind = true
	l.queue(h)
}

// flush applies an update waiting, if any.
func (l *list) flush(h *Host) {
	if l.updated {
		l.updated = false
		l.ids = l.pending
		l.pending = nil
		clear(l.rowOf)
		for i, id := range l.ids {
			l.rowOf[id] = i
		}
		live := make(map[int64]float64, len(l.ids))
		for _, id := range l.ids {
			if v, ok := l.heights[id]; ok {
				live[id] = v
			}
		}
		l.heights = live
	}
	l.tile(h)
}

// queue tiles once the core has returned.
func (l *list) queue(h *Host) {
	if l.queued {
		return
	}
	l.queued = true
	h.afterCore(func() {
		l.queued = false
		WithPool(func() { l.flush(h) })
	})
}

func (l *list) rowCount() int {
	if l.columns > 1 {
		return (len(l.ids) + l.columns - 1) / l.columns
	}
	return len(l.ids)
}

// rowHeight is a row's height: its cells' tallest, estimated until they
// are bound (a grid's rows add the gap below).
func (l *list) rowHeight(row int) float64 {
	height, known := 0.0, false
	for i := range l.columns {
		j := row*l.columns + i
		if j >= len(l.ids) {
			break
		}
		if v, ok := l.heights[l.ids[j]]; ok {
			height, known = max(height, v), true
		}
	}
	if !known {
		height = l.est
	}
	if l.columns > 1 && row < l.rowCount()-1 {
		height += l.gap
	}
	return height
}

func (l *list) measureTops() {
	rows := l.rowCount()
	l.tops = l.tops[:0]
	y := 0.0
	for r := range rows {
		l.tops = append(l.tops, y)
		y += l.rowHeight(r)
	}
	l.tops = append(l.tops, y)
}

// tile shows the rows in view (and a screen's worth around them), binds
// the ones new to the view, and places them.
func (l *list) tile(h *Host) {
	if l.tiling || h.core == nil {
		return
	}
	l.tiling = true
	defer func() { l.tiling = false }()
	for pass := 0; pass < 4; pass++ {
		l.measureTops()
		total := l.tops[len(l.tops)-1]
		clip := Send(l.scroll, "contentView")
		vb := MsgRect(clip, Sel("bounds"))
		MsgSetSize(l.doc, Sel("setFrameSize:"), Size{Width: l.width, Height: total})
		top, bottom := vb.Origin.Y-vb.Size.Height/2, vb.Origin.Y+vb.Size.Height*1.5
		rows := l.rowCount()
		first := sort.Search(rows, func(r int) bool { return l.tops[r+1] > top })
		last := sort.Search(rows, func(r int) bool { return l.tops[r] >= bottom })
		// Rows gone from view go back to the pool.
		for r, rv := range l.shown {
			if r < first || r >= last {
				delete(l.shown, r)
				Send(rv.view, "setHidden:", 1)
				l.free = append(l.free, rv)
			}
		}
		changed := false
		for r := first; r < last; r++ {
			rv := l.shown[r]
			fresh := rv == nil
			if fresh {
				rv = l.take(h)
				l.shown[r] = rv
			}
			if fresh || l.rebind || rv.row != r {
				rv.row = r
				before := l.rowHeight(r)
				l.bind(h, rv, r)
				if l.rowHeight(r) != before {
					changed = true
				}
			}
		}
		l.rebind = false
		for r, rv := range l.shown {
			l.place(h, rv, r)
		}
		if !changed {
			break
		}
	}
}

func (l *list) take(h *Host) *rowView {
	if k := len(l.free); k > 0 {
		rv := l.free[k-1]
		l.free = l.free[:k-1]
		Send(rv.view, "setHidden:", 0)
		return rv
	}
	v := initView("NuiView")
	Send(l.doc, "addSubview:", uintptr(v))
	Release(v)
	return &rowView{view: v, row: -1}
}

// bind binds row r's items into the row view's cells.
func (l *list) bind(h *Host, rv *rowView, r int) {
	first := r * l.columns
	k := min(l.columns, len(l.ids)-first)
	for len(rv.cells) < k {
		rv.cells = append(rv.cells, h.newCell(l, rv))
	}
	for i, c := range rv.cells {
		Send(c.n.view, "setHidden:", Bool(i >= k))
	}
	for i := range k {
		c := rv.cells[i]
		c.item = l.ids[first+i]
		if v, ok := l.heights[c.item]; ok {
			c.height = v
		}
		index := first + i
		h.enter(func() { h.core.ListBind(l.n.id, index, c.n.id) })
	}
}

func (h *Host) newCell(l *list, rv *rowView) *cell {
	h.nextCell++
	id := ops.HostIDBit | h.nextCell
	v := initView("NuiView")
	n := &node{id: id, kind: ops.KindView, view: v}
	c := &cell{n: n, l: l, rv: rv}
	n.cell = c
	h.add(n)
	Send(rv.view, "addSubview:", uintptr(v))
	return c
}

// setHeight takes a row's height from its cell's frame.
func (c *cell) setHeight(height float64) {
	l := c.l
	c.height = height
	if old, ok := l.heights[c.item]; ok && old == height {
		return
	}
	if _, ok := l.heights[c.item]; !ok && len(l.heights) < 64 {
		// The estimate follows the first rows'.
		l.est = (l.est*float64(len(l.heights)) + height) / float64(len(l.heights)+1)
	}
	l.heights[c.item] = height
	if !l.tiling {
		l.queue(theHost)
	}
}

// place puts a row view at its row's top and lays out its cells: a
// grid's side by side, each (width - gap × (columns - 1)) / columns wide.
func (l *list) place(h *Host, rv *rowView, r int) {
	height := l.rowHeight(r)
	MsgSetRect(rv.view, selSetFrame, Rect{Origin: Point{Y: l.tops[r]}, Size: Size{Width: l.width, Height: height}})
	if l.columns <= 1 {
		for _, c := range rv.cells {
			MsgSetRect(c.n.view, selSetFrame, Rect{Size: Size{Width: l.width, Height: c.height}})
		}
		l.separator(rv, r, height)
		return
	}
	if rv.sep != 0 {
		Send(rv.sep, "setHidden:", 1)
	}
	cw := (l.width - l.gap*float64(l.columns-1)) / float64(l.columns)
	for i, c := range rv.cells {
		MsgSetRect(c.n.view, selSetFrame, Rect{Origin: Point{X: float64(i) * (cw + l.gap)}, Size: Size{Width: cw, Height: c.height}})
	}
}

// separator is a list row's hairline at its bottom, but the last row's.
func (l *list) separator(rv *rowView, r int, height float64) {
	if rv.sep == 0 {
		rv.sep = initView("NSBox")
		Send(rv.sep, "setBoxType:", 2) // separator
		Send(rv.view, "addSubview:", uintptr(rv.sep))
		Release(rv.sep)
	}
	MsgSetRect(rv.sep, selSetFrame, Rect{Origin: Point{Y: height - 1}, Size: Size{Width: l.width, Height: 1}})
	Send(rv.sep, "setHidden:", Bool(r >= len(l.ids)-1))
}

// resized lays the rows out at the list's new width.
func (l *list) resized(h *Host) {
	w := l.n.frame.Size.Width
	if w != l.width {
		l.width = w
		l.rebind = true // the core lays the rows out at the new width
	}
	l.queue(h)
}

// scrolledList tiles a list as it scrolls.
func (h *Host) scrolledList(clip ID) bool {
	l := h.lists[clip]
	if l == nil {
		return false
	}
	l.tile(h)
	return true
}

func (l *list) scrollTo(h *Host, index int, animated bool) {
	l.flush(h)
	row := index / l.columns
	if row < 0 || row >= l.rowCount() {
		return
	}
	clip := Send(l.scroll, "contentView")
	b := MsgRect(clip, Sel("bounds"))
	top, bottom := l.tops[row], l.tops[row+1]
	y := b.Origin.Y
	if top < y {
		y = top
	} else if bottom > y+b.Size.Height {
		y = bottom - b.Size.Height
	}
	h.scrollToOffset(l.n, b.Origin.X, y, animated)
}
