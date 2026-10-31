//go:build windows && amd64

package windows

import (
	"encoding/binary"
	"sort"

	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

// A List is core-virtualized, as DESIGN.md says (no Win32 list hosts
// arbitrary rows): a view with a scroll bar whose rows the host places
// itself. As it scrolls, rows coming into view take a row view (a
// recycled one, or a new one) and its cells, host-made views the core
// binds rows into through ListBind; rows going out are kept for reuse. A
// row's height is its cells' (a grid's row holds Columns of them), from
// their frames; rows not yet bound are estimated.
type list struct {
	n       *node
	ids     []int64
	rowOf   map[int64]int
	heights map[int64]float32
	tops    []float32 // each row's top, then the total height
	shown   map[int]*rowView
	free    []*rowView
	columns int
	gap     float32
	width   float32
	est     float32
	offset  float32 // the scroll position, in points

	pending []int64
	updated bool // pending waits for the core to return
	tiling  bool
	queued  bool
	rebind  bool
}

type rowView struct {
	n     *node // a host node of kind kindRow
	cells []*cell
	row   int
	sep   bool
}

// cell is a host-made view a row is bound into.
type cell struct {
	n      *node
	l      *list
	item   int64
	height float32
	swipe  []action
	rv     *rowView
}

func newList(h *Host, n *node) *list {
	l := &list{n: n, columns: 1, est: 44, rowOf: map[int64]int{}, heights: map[int64]float32{}, shown: map[int]*rowView{}}
	n.hwnd = h.makeView(WS_TABSTOP)
	return l
}

func (l *list) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropListColumns:
		l.columns = max(int(v.Int), 1)
	case ops.PropListGap:
		l.gap = float32(v.Float)
	}
	l.rebind = true
	l.queue(h)
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

// flush applies an update waiting, if any, and tiles.
func (l *list) flush(h *Host) {
	if l.updated {
		l.updated = false
		l.ids = l.pending
		l.pending = nil
		clear(l.rowOf)
		for i, id := range l.ids {
			l.rowOf[id] = i
		}
		live := make(map[int64]float32, len(l.ids))
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
		if l.n.hwnd != 0 {
			l.flush(h)
		}
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
func (l *list) rowHeight(row int) float32 {
	height, known := float32(0), false
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
	y := float32(0)
	for r := range rows {
		l.tops = append(l.tops, y)
		y += l.rowHeight(r)
	}
	l.tops = append(l.tops, y)
}

// viewport is the list's visible height, in points.
func (l *list) viewport(h *Host) float32 {
	return h.pt(l.n, ClientRect(l.n.hwnd).H())
}

// tile shows the rows in view (and half a screen around them), binds the
// ones new to the view, and places them.
func (l *list) tile(h *Host) {
	if l.tiling || h.core == nil || l.n.hwnd == 0 {
		return
	}
	l.tiling = true
	defer func() { l.tiling = false }()
	for pass := 0; pass < 4; pass++ {
		l.measureTops()
		total := l.tops[len(l.tops)-1]
		view := l.viewport(h)
		l.offset = min(max(l.offset, 0), max(total-view, 0))
		top, bottom := l.offset-view/2, l.offset+view*1.5
		rows := l.rowCount()
		first := sort.Search(rows, func(r int) bool { return l.tops[r+1] > top })
		last := sort.Search(rows, func(r int) bool { return l.tops[r] >= bottom })
		// Rows gone from view go back to the pool.
		for r, rv := range l.shown {
			if r < first || r >= last {
				delete(l.shown, r)
				ShowWindow(rv.n.hwnd, SW_HIDE)
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
		l.placeRows(h)
		if !changed {
			break
		}
	}
	l.updateBar(h)
}

// placeRows moves the shown rows to their places below the scroll
// position, at once (one DeferWindowPos).
func (l *list) placeRows(h *Host) {
	d := BeginDeferWindowPos(len(l.shown))
	for r, rv := range l.shown {
		y := l.tops[r] - l.offset
		height := l.rowHeight(r)
		x0, y0 := int32(0), h.px(l.n, y)
		d = d.Defer(rv.n.hwnd, 0, x0, y0, h.px(l.n, l.width), h.px(l.n, y+height)-y0, SWP_NOZORDER|SWP_NOACTIVATE|SWP_SHOWWINDOW|SWP_NOCOPYBITS)
	}
	d.End()
	for r, rv := range l.shown {
		l.placeCells(h, rv, r)
	}
}

func (l *list) take(h *Host) *rowView {
	if k := len(l.free); k > 0 {
		rv := l.free[k-1]
		l.free = l.free[:k-1]
		return rv
	}
	rv := &rowView{row: -1}
	h.nextCell++
	rn := newNode(ops.HostIDBit|h.nextCell, kindRow)
	rn.hwnd = h.makeView(0)
	rn.row = rv
	rn.k = l.n.k
	rv.n = rn
	SetParent(rn.hwnd, l.n.hwnd)
	h.add(rn)
	return rv
}

// bind binds row r's items into the row view's cells.
func (l *list) bind(h *Host, rv *rowView, r int) {
	first := r * l.columns
	k := min(l.columns, len(l.ids)-first)
	for len(rv.cells) < k {
		rv.cells = append(rv.cells, h.newCell(l, rv))
	}
	for i, c := range rv.cells {
		if i >= k {
			ShowWindow(c.n.hwnd, SW_HIDE)
		} else {
			ShowWindow(c.n.hwnd, SW_SHOWNA)
		}
	}
	rv.sep = l.columns <= 1 && r < len(l.ids)-1
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
	n := newNode(ops.HostIDBit|h.nextCell, kindCell)
	n.hwnd = h.makeView(0)
	c := &cell{n: n, l: l, rv: rv}
	n.k = rv.n.k
	n.cell = c
	SetParent(n.hwnd, rv.n.hwnd)
	h.add(n)
	return c
}

// setHeight takes a row's height from its cell's frame.
func (c *cell) setHeight(height float32) {
	l := c.l
	c.height = height
	if old, ok := l.heights[c.item]; ok && old == height {
		return
	}
	if _, ok := l.heights[c.item]; !ok && len(l.heights) < 64 {
		// The estimate follows the first rows'.
		l.est = (l.est*float32(len(l.heights)) + height) / float32(len(l.heights)+1)
	}
	l.heights[c.item] = height
	if !l.tiling {
		l.queue(theHost)
	}
}

// placeCells lays out a row's cells: a grid's side by side, each (width
// - gap × (columns - 1)) / columns wide.
func (l *list) placeCells(h *Host, rv *rowView, r int) {
	if l.columns <= 1 {
		for _, c := range rv.cells {
			SetWindowPos(c.n.hwnd, 0, 0, 0, h.px(l.n, l.width), h.px(l.n, c.height), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		}
		Invalidate(rv.n.hwnd)
		return
	}
	cw := (l.width - l.gap*float32(l.columns-1)) / float32(l.columns)
	for i, c := range rv.cells {
		x := float32(i) * (cw + l.gap)
		SetWindowPos(c.n.hwnd, 0, h.px(l.n, x), 0, h.px(l.n, x+cw)-h.px(l.n, x), h.px(l.n, c.height), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	}
}

// paint draws a list row's separator: a hairline at its bottom, inset as
// the content is, but the last row's.
func (rv *rowView) paint(h *Host, g Graphics, w, ht float32) {
	if !rv.sep {
		return
	}
	k := h.scaleOf(rv.n)
	b := SolidBrush(h.theme.separator.argb())
	g.FillRect(b, 16*k, ht-max(k, 1), w-16*k, max(k, 1))
	b.Close()
}

// resized lays the rows out at the list's new width.
func (l *list) resized(h *Host) {
	w := l.n.frame[2]
	if w != l.width {
		l.width = w
		l.rebind = true // the core lays the rows out at the new width
	}
	l.queue(h)
}

// updateBar places the scroll bar's thumb.
func (l *list) updateBar(h *Host) { h.updateBars(l.n) }

// scrollTo moves the list to an offset (clamped) and tiles.
func (l *list) scrollTo(h *Host, y float32) {
	l.flush(h)
	if len(l.tops) == 0 {
		return
	}
	total := l.tops[len(l.tops)-1]
	y = min(max(y, 0), max(total-l.viewport(h), 0))
	if y == l.offset {
		return
	}
	l.offset = y
	l.tile(h)
	h.showBars(l.n)
	h.emit(l.n, ops.Event{Kind: ops.EventScrolled, X: 0, Y: float64(y)})
}

// scrollToItem scrolls the least that shows an item.
func (l *list) scrollToItem(h *Host, index int) {
	l.flush(h)
	row := index / l.columns
	if row < 0 || row >= l.rowCount() {
		return
	}
	top, bottom := l.tops[row], l.tops[row+1]
	view := l.viewport(h)
	y := l.offset
	if top < y {
		y = top
	} else if bottom > y+view {
		y = bottom - view
	}
	l.scrollTo(h, y)
}

// release forgets the host's rows and cells: the list's window takes
// their windows with it.
func (l *list) release(h *Host) {
	all := append([]*rowView{}, l.free...)
	for _, rv := range l.shown {
		all = append(all, rv)
	}
	for _, rv := range all {
		for _, c := range rv.cells {
			delete(h.nodes, c.n.id)
		}
		delete(h.nodes, rv.n.id)
	}
	l.shown, l.free = map[int]*rowView{}, nil
}

// Scrolling, of a List or a Scroll: the mouse wheel, the scroll bar, the
// keyboard.

// wheel scrolls by a wheel's delta: the system's lines per notch (3), a
// line 16 points. It reports false at the end, so an outer scroll view
// takes it.
func (h *Host) wheel(n *node, horizontal bool, delta int32) bool {
	lines := float32(SystemParam(SPI_GETWHEELSCROLLLINES))
	if lines <= 0 || lines > 100 {
		lines = 3
	}
	d := -float32(delta) / WHEEL_DELTA * lines * 16
	if horizontal {
		d = -d
	}
	// A vertical wheel scrolls a horizontal view only with Shift (else
	// the view it's in), and a horizontal one only a horizontal view.
	if KeyDown(VK_SHIFT) && !horizontal {
		horizontal = true
	}
	if horizontal != n.horizontal {
		return false
	}
	before := h.scrollPos(n)
	if horizontal {
		h.scrollBy(n, d, 0)
	} else {
		h.scrollBy(n, 0, d)
	}
	return h.scrollPos(n) != before
}

func (h *Host) scrollPos(n *node) [2]float32 {
	if n.list != nil {
		return [2]float32{0, n.list.offset}
	}
	return [2]float32{n.scrollX, n.scrollY}
}

func (h *Host) scrollBy(n *node, dx, dy float32) {
	if n.list != nil {
		n.list.scrollTo(h, n.list.offset+dy)
		return
	}
	h.scrollToOffset(n, n.scrollX+dx, n.scrollY+dy)
}

// scrollBar follows the scroll bar's arrows, track and thumb.
func (h *Host) scrollBar(n *node, horizontal bool, code int) {
	bar := SB_VERT
	if horizontal {
		bar = SB_HORZ
	}
	si := Scroll(n.hwnd, bar)
	line := float32(40)
	page := h.pt(n, int32(si.Page))
	pos := h.pt(n, si.Pos)
	switch code {
	case SB_LINEUP:
		pos -= line
	case SB_LINEDOWN:
		pos += line
	case SB_PAGEUP:
		pos -= page
	case SB_PAGEDOWN:
		pos += page
	case SB_THUMBTRACK, SB_THUMBPOSITION:
		pos = h.pt(n, si.TrackPos)
	case SB_TOP:
		pos = 0
	case SB_BOTTOM:
		pos = 1e9
	default:
		return
	}
	if n.list != nil {
		n.list.scrollTo(h, pos)
	} else if horizontal {
		h.scrollToOffset(n, pos, n.scrollY)
	} else {
		h.scrollToOffset(n, n.scrollX, pos)
	}
}

// scrollToOffset scrolls a Scroll to a point of its content (clamped).
func (h *Host) scrollToOffset(n *node, x, y float32) {
	if n.list != nil {
		n.list.scrollTo(h, y)
		return
	}
	if n.kind != ops.KindScroll {
		return
	}
	r := ClientRect(n.hwnd)
	vw, vh := h.pt(n, r.W()), h.pt(n, r.H())
	x = min(max(x, 0), max(n.contentW-vw, 0))
	y = min(max(y, 0), max(n.contentH-vh, 0))
	if !n.horizontal {
		x = 0
	}
	if x == n.scrollX && y == n.scrollY {
		return
	}
	n.scrollX, n.scrollY = x, y
	for _, c := range n.children {
		h.placeFrame(c)
	}
	h.showBars(n)
	h.updateBars(n)
	h.emit(n, ops.Event{Kind: ops.EventScrolled, X: float64(x), Y: float64(y)})
}

// updateScroll takes a Scroll's new size or content size: the position
// clamped, the scroll bar shown when the content overflows.
func (h *Host) updateScroll(n *node) {
	if n.hwnd == 0 {
		return
	}
	if len(n.children) > 0 {
		c := n.children[0]
		n.contentW, n.contentH = c.frame[2], c.frame[3]
	}
	h.updateBars(n)
	r := ClientRect(n.hwnd)
	vw, vh := h.pt(n, r.W()), h.pt(n, r.H())
	x := min(max(n.scrollX, 0), max(n.contentW-vw, 0))
	y := min(max(n.scrollY, 0), max(n.contentH-vh, 0))
	if x != n.scrollX || y != n.scrollY {
		n.scrollX, n.scrollY = x, y
		for _, c := range n.children {
			h.placeFrame(c)
		}
		h.updateBars(n)
	}
}
