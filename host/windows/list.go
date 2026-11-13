//go:build windows

package windows

import (
	"encoding/binary"
	"sort"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// A List is core-virtualized, as DESIGN.md says (no Win32 list hosts
// arbitrary rows): a view with a scroll bar whose rows the host places
// itself. As it scrolls, rows coming into view take a row view (a
// recycled one, or a new one) and its cells, host-made views the core
// binds rows into through ListBind; rows going out are kept for reuse. A
// row's height is its cells' (a grid's row holds Columns of them), from
// their frames; rows not yet bound are estimated.
//
// With sections, each section's header is a row of its own (one cell,
// bound with ListBind's index -1 - the section's); a plain or bare
// list's header stays at the top of the list while its section's rows
// scroll under it.
type list struct {
	n        *node
	ids      []int64
	rowOf    map[int64]int
	heights  map[int64]float32 // by item ID, or a header's by its section's ID
	rows     []lrow            // the rows shown: headers and items
	sections []lsection
	tops     []float32 // each row's top, then the total height
	shown    map[int]*rowView
	free     []*rowView
	columns  int
	gap      float32
	width    float32
	est      float32
	offset   float32 // the scroll position, in points
	// An inset grouped list's: its look and its rows' inset from each
	// side (and from its top and bottom).
	appearance ops.ListAppearance
	inset      float32

	// Anchored at the bottom: rows that don't fill it sit at its bottom
	// (lead is the room above them), and while it's at its end it stays
	// there.
	bottom bool
	stuck  bool
	lead   float32

	selecting bool

	pending  []int64
	pendingS []lsection
	updated  bool // pending waits for the core to return
	tiling   bool
	queued   bool
	rebind   bool
	applied  bool // an update was shown
}

// lsection is a section as the core sent it.
type lsection struct {
	start int
	id    int64
}

// lrow is a row the list shows: a section's header, or items (a grid's
// row holds up to Columns of them).
type lrow struct {
	header  bool
	section int
	first   int // the first item's index
	count   int
}

type rowView struct {
	n     *node // a host node of kind kindRow
	cells []*cell
	row   int
	sep   bool
	card  float32 // an inset grouped list's row: its card's height, in points
	// A pinned header's: drawn on the window's background over the rows.
	pinned bool
}

// cell is a host-made view a row is bound into.
type cell struct {
	n        *node
	l        *list
	item     int64
	height   float32
	swipe    []action
	leading  []action
	header   bool
	selected bool // the core says so
	rv       *rowView
}

func newList(h *Host, n *node) *list {
	l := &list{n: n, columns: 1, est: 44, rowOf: map[int64]int{}, heights: map[int64]float32{}, shown: map[int]*rowView{}, stuck: true}
	n.hwnd = h.makeView(WS_TABSTOP)
	return l
}

func (l *list) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropListRefreshable:
		h.setRefreshable(n, v.Int != 0)
		return
	case ops.PropListRefreshing:
		h.setRefreshing(n, v.Int != 0)
		return
	case ops.PropListColumns:
		l.columns = max(int(v.Int), 1)
	case ops.PropListGap:
		l.gap = float32(v.Float)
	case ops.PropListAppearance:
		l.appearance = ops.ListAppearance(v.Int)
	case ops.PropListInset:
		l.inset = float32(v.Float)
	case ops.PropListAnchor:
		l.bottom = ops.ScrollAnchor(v.Int) == ops.ScrollAnchorBottom
		l.stuck = true
	case ops.PropListSelecting:
		l.selecting = v.Int != 0
		for _, rv := range l.shown {
			Invalidate(rv.n.hwnd)
		}
		return
	}
	l.rebind = true
	l.queue(h)
}

// update takes the core's new item IDs and sections; rows bind again
// once the core has returned, so binding never re-enters a flush.
func (l *list) update(h *Host, args []byte) {
	if len(args) < 4 {
		return
	}
	count := int(binary.LittleEndian.Uint32(args))
	ids := make([]int64, 0, count)
	o := 4
	for range count {
		if o+8 > len(args) {
			break
		}
		ids = append(ids, int64(binary.LittleEndian.Uint64(args[o:])))
		o += 8
	}
	var secs []lsection
	if o+4 <= len(args) {
		ns := int(binary.LittleEndian.Uint32(args[o:]))
		o += 4
		for range ns {
			if o+12 > len(args) {
				break
			}
			secs = append(secs, lsection{start: int(binary.LittleEndian.Uint32(args[o:])), id: int64(binary.LittleEndian.Uint64(args[o+4:]))})
			o += 12
		}
	}
	l.pending = ids
	l.pendingS = secs
	l.updated = true
	l.rebind = true
	l.queue(h)
}

// flush applies an update waiting, if any, and tiles. Rows added or
// removed above the ones in view keep those where they are, unless the
// list is at the edge it's anchored to: at its top, new rows there
// show; at its end, anchored at the bottom, it goes to its new end.
func (l *list) flush(h *Host) {
	if !l.updated {
		l.tile(h)
		return
	}
	l.updated = false
	key, dy, ok := l.anchorRow()
	if l.bottom && l.stuck || !l.bottom && l.offset <= 0 {
		ok = false
	}
	l.ids = l.pending
	l.sections = l.pendingS
	l.pending, l.pendingS = nil, nil
	clear(l.rowOf)
	for i, id := range l.ids {
		l.rowOf[id] = i
	}
	live := make(map[int64]float32, len(l.ids)+len(l.sections))
	for _, id := range l.ids {
		if v, ok := l.heights[id]; ok {
			live[id] = v
		}
	}
	for _, s := range l.sections {
		if v, ok := l.heights[s.id]; ok {
			live[s.id] = v
		}
	}
	l.heights = live
	l.makeRows()
	l.tileKeeping(h, key, dy, ok)
	l.applied = l.applied || len(l.ids) > 0
}

// makeRows lays the items and the sections' headers out in rows.
func (l *list) makeRows() {
	l.rows = l.rows[:0]
	add := func(sec, from, to int) {
		for i := from; i < to; i += l.columns {
			l.rows = append(l.rows, lrow{section: sec, first: i, count: min(l.columns, to-i)})
		}
	}
	if len(l.sections) == 0 {
		add(0, 0, len(l.ids))
		return
	}
	for s, sec := range l.sections {
		end := len(l.ids)
		if s+1 < len(l.sections) {
			end = l.sections[s+1].start
		}
		l.rows = append(l.rows, lrow{header: true, section: s})
		add(s, min(sec.start, len(l.ids)), min(max(end, sec.start), len(l.ids)))
	}
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

// cardGap is the space between an inset grouped list's cards, as
// between Windows 11's settings cards.
const cardGap = 4

// grouped is whether it's an inset grouped list: each row a card, as
// Windows 11's settings are.
func (l *list) grouped() bool {
	return l.appearance == ops.ListAppearanceInsetGrouped && l.columns <= 1
}

// pad is the space above the first row and below the last.
func (l *list) pad() float32 {
	if l.grouped() {
		return l.inset
	}
	return 0
}

// sticky is whether its headers stay at its top: a plain or bare list's.
func (l *list) sticky() bool {
	return len(l.sections) > 0 && l.appearance != ops.ListAppearanceInsetGrouped
}

func (l *list) rowCount() int { return len(l.rows) }

// rowKey is the ID a row's height is kept under: its first item's, or
// its section's.
func (l *list) rowKey(r int) int64 {
	row := l.rows[r]
	if row.header {
		return l.sections[row.section].id
	}
	return l.ids[row.first]
}

// rowHeight is a row's height: its cells' tallest, estimated until they
// are bound (a grid's rows add the gap below, an inset grouped list's
// the space between cards, but not before a header or at the end).
func (l *list) rowHeight(r int) float32 {
	row := l.rows[r]
	height, known := float32(0), false
	if row.header {
		height, known = l.heights[l.sections[row.section].id]
	}
	for i := range row.count {
		if v, ok := l.heights[l.ids[row.first+i]]; ok {
			height, known = max(height, v), true
		}
	}
	if !known {
		height = l.est
	}
	if !row.header && r+1 < len(l.rows) && !l.rows[r+1].header {
		if l.columns > 1 {
			height += l.gap
		}
		if l.grouped() {
			height += cardGap
		}
	}
	return height
}

func (l *list) measureTops() {
	rows := l.rowCount()
	l.tops = l.tops[:0]
	y := l.pad()
	for r := range rows {
		l.tops = append(l.tops, y)
		y += l.rowHeight(r)
	}
	l.tops = append(l.tops, y+l.pad())
	// Anchored at the bottom, rows that don't fill it sit at its bottom.
	l.lead = 0
	if l.bottom && l.n.hwnd != 0 {
		if view, total := l.viewport(theHost), l.tops[len(l.tops)-1]; total < view {
			l.lead = view - total
			for i := range l.tops {
				l.tops[i] += l.lead
			}
		}
	}
}

// anchorRow is the row at the top of what the list shows, by its key,
// and how far its top is below the top of the view.
func (l *list) anchorRow() (key int64, dy float32, ok bool) {
	if len(l.rows) == 0 || len(l.tops) != len(l.rows)+1 {
		return 0, 0, false
	}
	r := sort.Search(len(l.rows), func(r int) bool { return l.tops[r+1] > l.offset })
	if r >= len(l.rows) {
		return 0, 0, false
	}
	return l.rowKey(r), l.tops[r] - l.offset, true
}

// atEnd reports whether it shows its last row's bottom.
func (l *list) atEnd(h *Host) bool {
	return len(l.tops) == 0 || l.offset+l.viewport(h) >= l.tops[len(l.tops)-1]-1
}

// pinnedRow is the header row that stays at the top of a plain list:
// its section's, once its own place has scrolled above the top (-1:
// none).
func (l *list) pinnedRow() int {
	if !l.sticky() || len(l.rows) == 0 {
		return -1
	}
	r := sort.Search(len(l.rows), func(r int) bool { return l.tops[r+1] > l.offset })
	if r >= len(l.rows) {
		return -1
	}
	s := l.rows[r].section
	for h := r; h >= 0; h-- {
		if l.rows[h].header && l.rows[h].section == s {
			if l.tops[h] >= l.offset {
				return -1
			}
			return h
		}
	}
	return -1
}

// viewport is the list's visible height, in points.
func (l *list) viewport(h *Host) float32 {
	return h.pt(l.n, ClientRect(l.n.hwnd).H())
}

// tile shows the rows in view (and half a screen around them), binds the
// ones new to the view, and places them. Rows above the one at the top
// that change height as they bind (an estimate made real) don't move
// what's in view; a list anchored at its bottom and at its end stays
// there.
func (l *list) tile(h *Host) {
	key, dy, ok := l.anchorRow()
	l.tileKeeping(h, key, dy, ok)
}

// tileKeeping tiles, keeping the row with key dy below the top (if ok).
func (l *list) tileKeeping(h *Host, key int64, dy float32, ok bool) {
	if l.tiling || h.core == nil || l.n.hwnd == 0 {
		return
	}
	l.tiling = true
	defer func() { l.tiling = false }()
	before := l.offset
	for pass := 0; pass < 4; pass++ {
		l.measureTops()
		total := l.tops[len(l.tops)-1]
		view := l.viewport(h)
		switch {
		case l.bottom && l.stuck:
			l.offset = total - view
		case ok:
			for r := range l.rows {
				if l.rowKey(r) == key {
					l.offset = l.tops[r] - dy
					break
				}
			}
		}
		l.offset = min(max(l.offset, 0), max(total-view, 0))
		top, bottom := l.offset-view/2, l.offset+view*1.5
		rows := l.rowCount()
		first := sort.Search(rows, func(r int) bool { return l.tops[r+1] > top })
		last := sort.Search(rows, func(r int) bool { return l.tops[r] >= bottom })
		pin := l.pinnedRow()
		// Rows gone from view go back to the pool.
		for r, rv := range l.shown {
			if (r < first || r >= last) && r != pin {
				delete(l.shown, r)
				ShowWindow(rv.n.hwnd, SW_HIDE)
				l.free = append(l.free, rv)
			}
		}
		changed := false
		show := func(r int) {
			rv := l.shown[r]
			fresh := rv == nil
			if fresh {
				rv = l.take(h)
				l.shown[r] = rv
			}
			if fresh || l.rebind || rv.row != r {
				rv.row = r
				was := l.rowHeight(r)
				l.bind(h, rv, r)
				if l.rowHeight(r) != was {
					changed = true
				}
			}
		}
		for r := first; r < last; r++ {
			show(r)
		}
		if pin >= 0 && (pin < first || pin >= last) {
			show(pin)
		}
		l.rebind = false
		l.placeRows(h)
		if !changed {
			break
		}
	}
	l.updateBar(h)
	if l.offset != before {
		h.emit(l.n, ops.Event{Kind: ops.EventScrolled, X: 0, Y: float64(l.offset)})
	}
}

// placeRows moves the shown rows to their places below the scroll
// position, at once (one DeferWindowPos); a pinned header at the top,
// pushed up by the next header, over the rows.
func (l *list) placeRows(h *Host) {
	d := BeginDeferWindowPos(len(l.shown))
	pinned := uintptr(0)
	for r, rv := range l.shown {
		y := l.tops[r] - l.offset
		height := l.rowHeight(r)
		x, w := l.rowX()
		rv.pinned = false
		if l.rows[r].header && l.sticky() && y < 0 {
			y = 0
			for n := r + 1; n < len(l.rows); n++ {
				if l.rows[n].header {
					y = min(y, l.tops[n]-l.offset-height)
					break
				}
			}
			rv.pinned = true
			pinned = rv.n.hwnd
		}
		x0, y0 := h.px(l.n, x), h.px(l.n, y)
		d = d.Defer(rv.n.hwnd, 0, x0, y0, h.px(l.n, x+w)-x0, h.px(l.n, y+height)-y0, SWP_NOZORDER|SWP_NOACTIVATE|SWP_SHOWWINDOW|SWP_NOCOPYBITS)
	}
	d.End()
	if pinned != 0 {
		// Over the rows that scroll under it.
		SetWindowPos(pinned, HWND_TOP, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	}
	for r, rv := range l.shown {
		l.placeCells(h, rv, r)
	}
}

// rowX is where a row starts across the list and how wide it is.
func (l *list) rowX() (x, w float32) {
	if l.grouped() {
		return l.inset, max(l.width-2*l.inset, 0)
	}
	return 0, l.width
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
	rn.dir = l.n.dir
	rv.n = rn
	SetParent(rn.hwnd, l.n.hwnd)
	h.add(rn)
	return rv
}

// setDirection gives the rows the list's direction.
func (l *list) setDirection(d ops.LayoutDirection) {
	for _, rv := range l.shown {
		rv.n.dir = d
	}
	for _, rv := range l.free {
		rv.n.dir = d
	}
}

// bind binds row r's items (or its section's header) into the row
// view's cells.
func (l *list) bind(h *Host, rv *rowView, r int) {
	row := l.rows[r]
	k := row.count
	if row.header {
		k = 1
	}
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
	rv.sep = l.columns <= 1 && !l.grouped() && l.appearance != ops.ListAppearanceBare && !row.header &&
		r < len(l.rows)-1 && !l.rows[r+1].header
	for i := range k {
		c := rv.cells[i]
		c.header = row.header
		index := row.first + i
		if row.header {
			c.item = l.sections[row.section].id
			index = -1 - row.section
		} else {
			c.item = l.ids[index]
		}
		if v, ok := l.heights[c.item]; ok {
			c.height = v
		}
		h.enter(func() { h.core.ListBind(l.n.id, index, c.n.id) })
	}
	Invalidate(rv.n.hwnd)
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
	if _, ok := l.heights[c.item]; !ok && len(l.heights) < 64 && !c.header {
		// The estimate follows the first rows'.
		l.est = (l.est*float32(len(l.heights)) + height) / float32(len(l.heights)+1)
	}
	l.heights[c.item] = height
	if !l.tiling {
		l.queue(theHost)
	}
}

// placeCells lays out a row's cells: a grid's side by side, each (width
// - gap × (columns - 1)) / columns wide, the first on the right, right to
// left.
func (l *list) placeCells(h *Host, rv *rowView, r int) {
	if l.columns <= 1 || l.rows[r].header {
		_, w := l.rowX()
		rv.card = 0
		if l.grouped() && !l.rows[r].header {
			rv.card = l.rowHeight(r)
			if r+1 < l.rowCount() && !l.rows[r+1].header {
				rv.card -= cardGap
			}
		}
		for _, c := range rv.cells {
			SetWindowPos(c.n.hwnd, 0, 0, 0, h.px(l.n, w), h.px(l.n, c.height), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		}
		Invalidate(rv.n.hwnd)
		return
	}
	cw := (l.width - l.gap*float32(l.columns-1)) / float32(l.columns)
	rtl := h.isRTL(l.n)
	for i, c := range rv.cells {
		x := float32(i) * (cw + l.gap)
		if rtl {
			x = l.width - x - cw
		}
		SetWindowPos(c.n.hwnd, 0, h.px(l.n, x), 0, h.px(l.n, x+cw)-h.px(l.n, x), h.px(l.n, c.height), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	}
}

// paint draws a list row's separator: a hairline at its bottom, inset as
// the content is, but the last row's; or an inset grouped list's row's
// card, as Windows 11's settings cards (CardBackgroundFillColorDefault,
// CardStrokeColorDefault, ControlCornerRadius).
func (rv *rowView) paint(h *Host, g Graphics, w, ht float32) {
	k := h.scaleOf(rv.n)
	if rv.pinned {
		b := SolidBrush(h.theme.bg.argb())
		g.FillRect(b, 0, 0, w, ht)
		b.Close()
	}
	if rv.selected() {
		// A selected row: the subtle fill and the accent's pill at its
		// start, as a WinUI list's selected item.
		c := h.theme.subtleHot
		radius := 4 * k
		g.FillRoundRect(c.argb(), 0, 0, w, ht, radius)
		pill := 3 * k
		x := 0 * k
		if h.isRTL(rv.n) {
			x = w - pill
		}
		ph := min(16*k, ht/2)
		g.FillRoundRect(h.theme.accent.argb(), x, (ht-ph)/2, pill, ph, pill/2)
	}
	if rv.card > 0 {
		ch := min(rv.card*k, ht)
		g.FillRoundRect(h.theme.card.argb(), 0, 0, w, ch, 4*k)
		g.StrokeRoundRect(h.theme.cardStroke.argb(), max(k, 1), 0, 0, w, ch, 4*k)
		return
	}
	if !rv.sep {
		return
	}
	x := 16 * k // inset at the start
	if h.isRTL(rv.n) {
		x = 0
	}
	b := SolidBrush(h.theme.separator.argb())
	g.FillRect(b, x, ht-max(k, 1), w-16*k, max(k, 1))
	b.Close()
}

// selected reports whether the row shows as selected: one of its cells
// is, in a selecting list.
func (rv *rowView) selected() bool {
	for _, c := range rv.cells {
		if c.selected && !c.header && c.l.selecting {
			return true
		}
	}
	return false
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

// scrollTo moves the list to an offset (clamped) and tiles; the user's
// scrolling (and the app's) moves a list anchored at its bottom off its
// end, or back.
func (l *list) scrollTo(h *Host, y float32) {
	l.flush(h)
	if len(l.tops) == 0 {
		return
	}
	total := l.tops[len(l.tops)-1]
	y = min(max(y, 0), max(total-l.viewport(h), 0))
	if l.bottom {
		l.stuck = y >= max(total-l.viewport(h), 0)-1
	}
	if y == l.offset {
		return
	}
	l.offset = y
	l.tile(h)
	h.showBars(l.n)
	h.emit(l.n, ops.Event{Kind: ops.EventScrolled, X: 0, Y: float64(l.offset)})
}

// scrollToItem scrolls the least that shows an item (below a header
// pinned over it).
func (l *list) scrollToItem(h *Host, index int) {
	l.flush(h)
	if index < 0 || index >= len(l.ids) {
		return
	}
	// The item's row (headers in between have no items: not a search).
	row := -1
	for r, x := range l.rows {
		if !x.header && index >= x.first && index < x.first+x.count {
			row = r
			break
		}
	}
	if row < 0 {
		return
	}
	top, bottom := l.tops[row], l.tops[row+1]
	if l.sticky() {
		for r := row; r >= 0; r-- {
			if l.rows[r].header {
				top -= l.rowHeight(r)
				break
			}
		}
	}
	view := l.viewport(h)
	y := l.offset
	if top < y {
		y = top
	} else if bottom > y+view {
		y = bottom - view
	}
	l.scrollTo(h, y)
}

// selectClick toggles the selection of the row n is in, if it's in a
// selecting list; false: it isn't.
func (h *Host) selectClick(n *node) bool {
	for p := n; p != nil; p = p.parent {
		c := p.cell
		if c == nil {
			continue
		}
		if !c.l.selecting || c.header {
			return false
		}
		c.selected = !c.selected
		Invalidate(c.rv.n.hwnd)
		h.emit(c.n, ops.Event{Kind: ops.EventRowSelected, Bool: c.selected})
		return true
	}
	return false
}

// cellOf is the list cell n is in, if any.
func cellOf(n *node) *cell {
	for p := n; p != nil; p = p.parent {
		if p.cell != nil {
			return p.cell
		}
	}
	return nil
}

// rowMenu shows a row's swipe actions, leading then trailing, as its
// context menu (a mouse has no swipes); a leading one's Action is -1 -
// its index. False: it has none.
func (h *Host) rowMenu(c *cell) bool {
	if c == nil || c.l.selecting || len(c.leading)+len(c.swipe) == 0 {
		return false
	}
	all := append(append([]action{}, c.leading...), c.swipe...)
	id := c.n.id
	nl := len(c.leading)
	h.afterCore(func() {
		n := h.nodes[id]
		if n == nil || n.hwnd == 0 {
			return
		}
		rtl := h.isRTL(n)
		m, om := h.actionMenu(all, rtl)
		p := CursorPos()
		flags := uint32(TPM_LEFTALIGN | TPM_TOPALIGN | TPM_RIGHTBUTTON)
		if rtl {
			flags |= TPM_LAYOUTRTL
		}
		h.modal++
		if om != nil {
			h.menuLoop(true)
		}
		got := TrackPopupMenu(m, flags, p.X, p.Y, RootWindow(n.hwnd))
		h.menuLoop(false)
		h.modal--
		DestroyMenu(m)
		h.forget(om)
		if got <= 0 {
			return
		}
		i := int64(got - 1)
		if int(i) < nl {
			i = -1 - i
		} else {
			i -= int64(nl)
		}
		if n := h.nodes[id]; n != nil {
			h.emit(n, ops.Event{Kind: ops.EventAction, Int: i})
		}
	})
	return true
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
		if h.isRTL(n) {
			d = -d // down the wheel is on along the content, to the left
		}
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
	n.scrollEnd = max(n.contentW-vw, 0) - x
	for _, c := range n.children {
		h.placeFrame(c)
	}
	h.showBars(n)
	h.updateBars(n)
	h.emit(n, ops.Event{Kind: ops.EventScrolled, X: float64(x), Y: float64(y)})
}

// updateScroll takes a Scroll's new size or content size: the position
// clamped, the scroll bar shown when the content overflows. A horizontal
// one going right to left starts at its trailing end, and keeps its distance
// from that end as they change: the content stays at its origin, so what
// shows doesn't move, and its position, which is physical, does.
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
	followsEnd := n.horizontal && h.isRTL(n)
	if followsEnd {
		x = max(n.contentW-vw-n.scrollEnd, 0)
	}
	if x != n.scrollX || y != n.scrollY {
		n.scrollX, n.scrollY = x, y
		for _, c := range n.children {
			h.placeFrame(c)
		}
		h.updateBars(n)
		if followsEnd {
			h.emit(n, ops.Event{Kind: ops.EventScrolled, X: float64(x), Y: float64(y)})
		}
	}
}

// restartScroll puts a horizontal Scroll at its start in a new direction: its
// right end, right to left; its left, left to right.
func (h *Host) restartScroll(n *node) {
	if !n.horizontal || n.hwnd == 0 {
		return
	}
	n.scrollEnd = 0
	if h.isRTL(n) {
		h.updateScroll(n)
		return
	}
	h.scrollToOffset(n, 0, n.scrollY)
}
