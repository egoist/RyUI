//go:build linux && !android

package linux

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A List is core-virtualized, as on macOS and Windows: a
// GtkScrolledWindow (kinetic scrolling, overlay scroll bars, overshoot)
// around a viewport whose child is a view of the host's as tall as all
// the rows, in which the host places the rows itself. As it scrolls, rows
// coming into view take a row view (a recycled one, or a new one) and its
// cells, host-made views the core binds rows into through ListBind; rows
// going out are kept for reuse. A row's height is its cells' (a grid's
// row holds Columns of them), from their frames; rows not yet bound are
// estimated. (GtkListView's rows are widgets of its own factory, sized by
// GTK's measuring, not the core's frames.)
//
// It looks as libadwaita's lists do: a plain list's rows on the view's
// background, a hairline between them; an inset grouped list's in
// .boxed-list cards (a section's each) on the window's background; a bare
// list's on nothing. With sections, each section's header is a row of its
// own (one cell, bound with ListBind's index -1 - the section's); a plain
// or bare list's header stays at the top of the list while its section's
// rows scroll under it.
//
// A row's swipe actions are its context menu (a secondary click, a long
// press on a touchscreen), leading then trailing, as on the other desktop
// hosts. A selecting list's click selects or deselects a row
// (RowSelected), highlighted. F5 refreshes a refreshable list on screen,
// a spinner over its top meanwhile.
type list struct {
	n        *node
	sw, vp   Ptr
	vadj     Ptr
	doc      *node // the view the rows are in, as tall as they are
	ids      []int64
	heights  map[int64]float32 // by item ID, or a header's by its section's ID
	rows     []lrow            // the rows shown: headers and items
	tops     []float32         // each row's top, then the total height
	total    float32           // the doc's height, as last set
	sections []lsection
	shown    map[int]*rowView
	free     []*rowView
	cards    []*node // an inset grouped list's cards, a section's each
	columns  int
	gap      float32
	width    float32
	est      float32
	// An inset grouped list's: its look and its rows' inset from each
	// side (and from its top and bottom).
	appearance ops.ListAppearance
	inset      float32

	// Anchored at the bottom: rows that don't fill it sit at its bottom
	// (lead is the room above them), and while it's at its end it stays
	// there; settling counts scrolls the host makes itself.
	bottom   bool
	stuck    bool
	lead     float32
	settling int
	anim     Ptr // the scroll animating, if any

	selecting bool

	pending  []int64
	pendingS []lsection
	updated  bool // pending waits for the core to return
	tiling   bool
	queued   bool
	rebind   bool
	applied  bool // an update was shown
	dead     bool
	reported float64

	refreshable, refreshing bool
	spinner, shortcut       Ptr
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

// rowView is a row's view: a host node, its cells in it.
type rowView struct {
	n      *node
	l      *list
	cells  []*cell
	row    int
	pinned bool // a header pinned at the top, over the rows
}

// cell is a host-made view a row is bound into.
type cell struct {
	n        *node
	l        *list
	rv       *rowView
	item     int64
	height   float32
	swipe    []action
	leading  []action
	header   bool
	selected bool // the core says so
}

// listCard is an inset grouped list's card behind a section's rows.
type listCard struct{ l *list }

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindList {
			return false
		}
		h.newList(n)
		return true
	})
	propHooks = append(propHooks, listProp)
	commonHooks = append(commonHooks, cellProp)
	commandHooks = append(commandHooks, listCommand)
	frameHooks = append(frameHooks, listFrame)
	releaseHooks = append(releaseHooks, releaseList)
	drawHooks = append(drawHooks, drawList)
	listenHooks = append(listenHooks, listenCell)
	scrollToHooks = append(scrollToHooks, listScrollToOffset)
	dumpHooks = append(dumpHooks, dumpList)
}

// hostID is a new ID for a node the host makes (a list's rows and cells).
func (h *Host) hostID() ops.NodeID {
	h.nextCell++
	return ops.HostIDBit | h.nextCell
}

// hostNode makes a view of the host's own, a child of parent's view.
func (h *Host) hostNode(parent *node) *node {
	n := &node{id: h.hostID(), kind: ops.KindView, opacity: 1, widget: newView(), selected: -1, parent: parent}
	if parent != nil {
		n.dir, n.rtl = parent.dir, parent.rtl
	}
	h.add(n)
	return n
}

func (h *Host) newList(n *node) {
	l := &list{n: n, columns: 1, est: 44, heights: map[int64]float32{}, shown: map[int]*rowView{}, stuck: true, reported: -1}
	n.ext = l
	l.sw = ScrolledWindowNew()
	ScrolledWindowSetPolicy(l.sw, PolicyNever, PolicyAutomatic)
	l.vp = ViewportNew(0, 0)
	ViewportSetScrollToFocus(l.vp, false) // rows come and go as it scrolls
	ScrolledWindowSetChild(l.sw, l.vp)
	h.setInner(n, l.sw)
	l.vadj = ScrolledWindowGetVAdjustment(l.sw)
	Connect(l.vadj, "value-changed", func() { l.scrolled(h) })
	l.doc = h.hostNode(n)
	ViewportSetChild(l.vp, l.doc.widget)
}

func listProp(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
	l, ok := n.ext.(*list)
	if !ok || n.kind != ops.KindList {
		return false
	}
	switch p {
	case ops.PropListRefreshable:
		l.setRefreshable(h, v.Int != 0)
		return true
	case ops.PropListRefreshing:
		l.setRefreshing(v.Int != 0)
		return true
	case ops.PropListColumns:
		l.columns = max(int(v.Int), 1)
	case ops.PropListGap:
		l.gap = float32(v.Float)
	case ops.PropListAppearance:
		l.appearance = ops.ListAppearance(v.Int)
		queueDraw(n)
	case ops.PropListInset:
		l.inset = float32(v.Float)
	case ops.PropListAnchor:
		l.bottom = ops.ScrollAnchor(v.Int) == ops.ScrollAnchorBottom
		l.stuck = true
	case ops.PropListSelecting:
		l.setSelecting(v.Int != 0)
		return true
	default:
		return false
	}
	l.rebind = true
	l.queue(h)
	return true
}

func listCommand(h *Host, n *node, c ops.CommandID, args []byte) bool {
	l, ok := n.ext.(*list)
	if !ok || n.kind != ops.KindList {
		return false
	}
	switch c {
	case ops.CmdListUpdate:
		l.update(h, args)
	case ops.CmdScrollTo:
		r := reader{b: args}
		index := int(r.u32())
		animated := r.u8() != 0
		// Once the core has returned: binding rows calls into it.
		h.afterCore(func() {
			if !l.dead {
				l.scrollTo(h, index, animated)
			}
		})
	default:
		return false
	}
	return true
}

func listScrollToOffset(h *Host, n *node, x, y float64, animated bool) bool {
	l, ok := n.ext.(*list)
	if !ok || n.kind != ops.KindList {
		return false
	}
	h.afterCore(func() {
		if l.dead {
			return
		}
		l.flush(h)
		end := l.endOffset()
		to := min(max(float32(y), 0), end)
		if l.bottom {
			l.stuck = to >= end-1
		}
		l.setOffset(to, animated)
	})
	return true
}

// listFrame takes a cell's frame as its row's height; a list's new size
// tiles it again.
func listFrame(h *Host, n *node, f [4]float32) bool {
	switch x := n.ext.(type) {
	case *cell:
		x.setHeight(h, f[3])
		return true
	case *list:
		if n.kind == ops.KindList && (f[2] != n.frame[2] || f[3] != n.frame[3] || !n.placed) {
			x.rebind = x.rebind || f[2] != x.width
			x.queue(h)
		}
	}
	return false
}

// update takes the core's new item IDs and sections; rows bind again
// once the core has returned, so binding never re-enters a flush.
func (l *list) update(h *Host, args []byte) {
	if len(args) < 4 {
		return
	}
	le := binary.LittleEndian
	count := int(le.Uint32(args))
	ids := make([]int64, 0, count)
	o := 4
	for range count {
		if o+8 > len(args) {
			break
		}
		ids = append(ids, int64(le.Uint64(args[o:])))
		o += 8
	}
	var secs []lsection
	if o+4 <= len(args) {
		ns := int(le.Uint32(args[o:]))
		o += 4
		for range ns {
			if o+12 > len(args) {
				break
			}
			secs = append(secs, lsection{start: int(le.Uint32(args[o:])), id: int64(le.Uint64(args[o+4:]))})
			o += 12
		}
	}
	l.pending = ids
	l.pendingS = secs
	l.updated = true
	l.rebind = true
	l.queue(h)
}

// queue tiles once the core has returned.
func (l *list) queue(h *Host) {
	if l.queued {
		return
	}
	l.queued = true
	h.afterCore(func() {
		l.queued = false
		if !l.dead {
			l.flush(h)
		}
	})
}

// flush applies an update waiting, if any, and tiles. Rows added or
// removed above the ones in view keep those where they are, unless the
// list is at the edge it's anchored to: at its top, new rows there show;
// at its end, anchored at the bottom, it goes to its new end.
func (l *list) flush(h *Host) {
	if w := l.n.frame[2]; w != l.width {
		l.width = w
		l.rebind = true // the core lays the rows out at the new width
	}
	if !l.updated {
		l.tile(h)
		return
	}
	l.updated = false
	key, dy, ok := l.anchorRow()
	if l.bottom && l.stuck || !l.bottom && l.offset() <= 0.5 {
		ok = false
	}
	appended := l.applied && len(l.pending) > len(l.ids) && (len(l.ids) == 0 || l.pending[len(l.pending)-1] != l.ids[len(l.ids)-1])
	l.ids = l.pending
	l.sections = l.pendingS
	l.pending, l.pendingS = nil, nil
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
	// New rows at the end of a chat at its end scroll into view.
	animate := appended && l.bottom && l.stuck && WidgetGetMapped(l.n.widget)
	l.tileKeeping(h, key, dy, ok, animate)
	l.applied = l.applied || len(l.ids) > 0
	if animate {
		l.setOffset(l.endOffset(), true)
	}
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

// grouped is whether it's an inset grouped list: its rows in rounded
// cards, as libadwaita's boxed lists.
func (l *list) grouped() bool {
	return l.appearance == ops.ListAppearanceInsetGrouped && l.columns <= 1
}

// sticky is whether its headers stay at its top: a plain or bare list's.
func (l *list) sticky() bool {
	return len(l.sections) > 0 && l.appearance != ops.ListAppearanceInsetGrouped
}

// pad is the space above the first row and below the last.
func (l *list) pad() float32 {
	if l.grouped() {
		return l.inset
	}
	return 0
}

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
// are bound (a grid's rows add the gap below, but a section's last).
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
	return height + l.gapBelow(r)
}

// gapBelow is the gap below a grid's row, when another row of its
// section follows.
func (l *list) gapBelow(r int) float32 {
	if l.columns > 1 && !l.rows[r].header && r+1 < len(l.rows) && !l.rows[r+1].header {
		return l.gap
	}
	return 0
}

// viewH is how tall what the list shows is: its frame's height.
func (l *list) viewH() float32 {
	if l.n.placed {
		return l.n.frame[3]
	}
	return float32(AdjustmentGetPageSize(l.vadj))
}

func (l *list) offset() float32 { return float32(AdjustmentGetValue(l.vadj)) }

func (l *list) endOffset() float32 { return max(l.total-l.viewH(), 0) }

func (l *list) atEnd() bool {
	return len(l.tops) == 0 || l.offset()+l.viewH() >= l.tops[len(l.tops)-1]-1
}

func (l *list) measureTops() {
	l.tops = l.tops[:0]
	y := l.pad()
	for r := range l.rows {
		l.tops = append(l.tops, y)
		y += l.rowHeight(r)
	}
	l.tops = append(l.tops, y+l.pad())
	// Anchored at the bottom, rows that don't fill it sit at its bottom.
	l.lead = 0
	if l.bottom {
		if vh, total := l.viewH(), l.tops[len(l.tops)-1]; total < vh {
			l.lead = vh - total
			for i := range l.tops {
				l.tops[i] += l.lead
			}
		}
	}
}

// setTotal makes the rows' view as tall as the rows, and the scroll
// range follow at once (the viewport takes it as it's next allocated).
func (l *list) setTotal(total float32) {
	f := [4]float32{0, 0, l.width, total}
	if l.doc.frame == f && l.doc.placed {
		return
	}
	l.doc.frame, l.doc.placed = f, true
	l.total = total
	WidgetSetSizeRequest(l.doc.widget, -1, int32(math.Ceil(float64(total))))
	l.configure(min(l.offset(), max(total-l.viewH(), 0)))
}

// configure sets the scroll position and range.
func (l *list) configure(y float32) {
	vh := float64(l.viewH())
	l.settling++
	AdjustmentConfigure(l.vadj, float64(y), 0, max(float64(l.total), vh), vh*0.1, vh*0.9, vh)
	l.settling--
}

// setOffset scrolls the list itself (not the user): a list anchored at
// its bottom stays stuck to its end, or not, as it was, until it's done.
func (l *list) setOffset(y float32, animated bool) {
	y = min(max(y, 0), l.endOffset())
	if l.anim != 0 {
		AdwAnimationSkip(l.anim) // done: where it was going
	}
	from := l.offset()
	if from == y {
		return
	}
	if !animated || AdwTimedAnimationNew == nil || !WidgetGetMapped(l.n.widget) {
		l.configure(y)
		return
	}
	target := AdwPropertyAnimationTargetNew(l.vadj, "value")
	a := AdwTimedAnimationNew(l.n.widget, float64(from), float64(y), 250, target)
	AdwTimedAnimationSetEasing(a, EaseOutCubic)
	l.anim = a
	l.settling++
	Connect(a, "done", func() {
		if l.anim != a {
			return
		}
		l.anim = 0
		l.settling--
		if l.bottom && !l.dead {
			l.stuck = l.atEnd()
		}
		ObjectUnref(a)
	})
	AdwAnimationPlay(a)
}

// scrolled: the user scrolled (or the host did). The rows in view tile;
// the user's scrolling moves a list anchored at its bottom off its end,
// or back.
func (l *list) scrolled(h *Host) {
	if l.dead {
		return
	}
	y := AdjustmentGetValue(l.vadj)
	if y != l.reported {
		l.reported = y
		h.emit(l.n, ops.Event{Kind: ops.EventScrolled, Y: y})
	}
	if l.tiling {
		return
	}
	if l.bottom && l.settling == 0 {
		l.stuck = l.atEnd()
	}
	if h.depth > 0 {
		l.queue(h)
		return
	}
	l.tile(h)
}

// anchorRow is the row at the top of what the list shows, by its key,
// and how far its top is below the top of the view.
func (l *list) anchorRow() (key int64, dy float32, ok bool) {
	if len(l.rows) == 0 || len(l.tops) != len(l.rows)+1 {
		return 0, 0, false
	}
	y := l.offset()
	r := sort.Search(len(l.rows), func(r int) bool { return l.tops[r+1] > y })
	if r >= len(l.rows) {
		return 0, 0, false
	}
	return l.rowKey(r), l.tops[r] - y, true
}

// restore scrolls so that the row with key is dy below the top again.
func (l *list) restore(key int64, dy float32) {
	for r := range l.rows {
		if l.rowKey(r) == key {
			if y := l.tops[r] - dy; y != l.offset() {
				l.configure(min(max(y, 0), l.endOffset()))
			}
			return
		}
	}
}

// pinnedRow is the header row that stays at the top of a plain list:
// its section's, once its own place has scrolled above y (-1: none).
func (l *list) pinnedRow(y float32) int {
	if !l.sticky() || len(l.rows) == 0 {
		return -1
	}
	r := sort.Search(len(l.rows), func(r int) bool { return l.tops[r+1] > y })
	if r >= len(l.rows) {
		return -1
	}
	s := l.rows[r].section
	for h := r; h >= 0; h-- {
		if l.rows[h].header && l.rows[h].section == s {
			if l.tops[h] >= y {
				return -1
			}
			return h
		}
	}
	return -1
}

// tile shows the rows in view (and half a screen around them), binds the
// ones new to the view, and places them. Rows above the one at the top
// that change height as they bind (an estimate made real) don't move
// what's in view; a list anchored at its bottom and at its end stays
// there.
func (l *list) tile(h *Host) {
	key, dy, ok := l.anchorRow()
	l.tileKeeping(h, key, dy, ok, false)
}

// tileKeeping tiles, keeping the row with key dy below the top (if ok);
// holdEnd leaves a list at its end where it is, for a scroll to its new
// end to animate.
func (l *list) tileKeeping(h *Host, key int64, dy float32, ok, holdEnd bool) {
	if l.tiling || h.core == nil || l.dead || !l.n.placed || l.width <= 0 {
		return
	}
	l.tiling = true
	defer func() { l.tiling = false }()
	for pass := 0; pass < 4; pass++ {
		l.measureTops()
		l.setTotal(l.tops[len(l.tops)-1])
		switch {
		case l.bottom && l.stuck:
			if l.settling == 0 && !holdEnd {
				l.configure(l.endOffset())
			}
		case ok:
			l.restore(key, dy)
		}
		y, vh := l.offset(), l.viewH()
		top, bottom := y-vh/2, y+vh*1.5
		rows := len(l.rows)
		first := sort.Search(rows, func(r int) bool { return l.tops[r+1] > top })
		last := sort.Search(rows, func(r int) bool { return l.tops[r] >= bottom })
		pin := l.pinnedRow(y)
		// Rows gone from view go back to the pool.
		for r, rv := range l.shown {
			if (r < first || r >= last) && r != pin {
				delete(l.shown, r)
				WidgetSetVisible(rv.n.widget, false)
				rv.row = -1
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
				before := l.rowHeight(r)
				l.bind(h, rv, r)
				if l.rowHeight(r) != before {
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
		if !changed {
			break
		}
	}
	moved := false
	for r, rv := range l.shown {
		if l.place(h, rv, r) {
			moved = true
		}
	}
	if l.placeCards() {
		moved = true
	}
	if moved {
		Call(FnQueueAllocate, l.doc.widget)
	}
}

func (l *list) take(h *Host) *rowView {
	if k := len(l.free); k > 0 {
		rv := l.free[k-1]
		l.free = l.free[:k-1]
		WidgetSetVisible(rv.n.widget, true)
		return rv
	}
	rv := &rowView{l: l, row: -1}
	rv.n = h.hostNode(l.doc)
	rv.n.ext = rv
	WidgetInsertBefore(rv.n.widget, l.doc.widget, 0) // last: over the cards
	return rv
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
		WidgetSetVisible(c.n.widget, i < k)
	}
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
		list, id := l.n.id, c.n.id
		h.enter(func() { h.core.ListBind(list, index, id) })
		queueDraw(c.n)
	}
	queueDraw(rv.n)
}

func (h *Host) newCell(l *list, rv *rowView) *cell {
	n := h.hostNode(rv.n)
	c := &cell{n: n, l: l, rv: rv}
	n.ext = c
	WidgetSetParent(n.widget, rv.n.widget)
	// A selecting list's click selects the row: taken before the row's
	// own views see it.
	g := GestureClickNew()
	GestureSingleSetButton(g, 1)
	EventControllerSetPropagationPhase(g, PhaseCapture)
	Connect(g, "pressed", func(count int, x, y float64) {
		if l.selecting && !c.header {
			GestureSetState(g, EventSequenceClaimed)
		}
	})
	Connect(g, "released", func(count int, x, y float64) {
		if l.selecting && !c.header {
			h.selectRow(c)
		}
	})
	WidgetAddController(n.widget, g)
	return c
}

// setHeight takes a row's height from its cell's frame.
func (c *cell) setHeight(h *Host, height float32) {
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
		l.queue(h)
	}
}

// rowX is where a row starts across the list and how wide it is.
func (l *list) rowX() (x, w float32) {
	if l.grouped() {
		return l.inset, max(l.width-2*l.inset, 0)
	}
	return 0, l.width
}

// place puts a row view at its row's top (a pinned header at the top of
// what shows, pushed up by the next header) and lays out its cells: a
// grid's side by side, each (width - gap × (columns - 1)) / columns wide,
// the first at the right when it reads right to left. It reports whether
// the row moved.
func (l *list) place(h *Host, rv *rowView, r int) bool {
	height := l.rowHeight(r)
	x, w := l.rowX()
	y := l.tops[r]
	row := l.rows[r]
	pinned := false
	if row.header && l.sticky() {
		if off := l.offset(); y < off {
			y = off
			for n := r + 1; n < len(l.rows); n++ {
				if l.rows[n].header {
					y = min(y, l.tops[n]-height)
					break
				}
			}
			pinned = true
		}
	}
	if pinned && WidgetGetLastChild(l.doc.widget) != rv.n.widget {
		// Over the rows that scroll under it.
		WidgetInsertBefore(rv.n.widget, l.doc.widget, 0)
	}
	if pinned != rv.pinned {
		rv.pinned = pinned
		queueDraw(rv.n)
	}
	moved := setHostFrame(rv.n, [4]float32{x, y, w, height})
	cells := false
	if l.columns <= 1 || row.header {
		for _, c := range rv.cells {
			cells = setHostFrame(c.n, [4]float32{0, 0, w, c.height}) || cells
		}
	} else {
		cw := (l.width - l.gap*float32(l.columns-1)) / float32(l.columns)
		for i, c := range rv.cells {
			col := i
			if l.n.rtl {
				col = l.columns - 1 - i
			}
			cells = setHostFrame(c.n, [4]float32{float32(col) * (cw + l.gap), 0, cw, c.height}) || cells
		}
	}
	if cells {
		Call(FnQueueAllocate, rv.n.widget)
	}
	return moved
}

// setHostFrame sets a host node's frame, which its parent's view places
// it at; it reports whether it changed.
func setHostFrame(n *node, f [4]float32) bool {
	if n.frame == f && n.placed {
		return false
	}
	resized := n.frame[2] != f[2] || n.frame[3] != f[3]
	n.frame, n.placed = f, true
	if resized {
		queueDraw(n)
	}
	return true
}

// placeCards puts an inset grouped list's cards behind its rows, a
// section's each, from its first item row to its last.
func (l *list) placeCards() bool {
	boxes := 0
	if l.grouped() && len(l.ids) > 0 {
		boxes = max(len(l.sections), 1)
	}
	moved := false
	for len(l.cards) < boxes {
		c := theHost.hostNode(l.doc)
		c.ext = &listCard{l: l}
		WidgetInsertAfter(c.widget, l.doc.widget, 0) // first: under the rows
		l.cards = append(l.cards, c)
	}
	for i, c := range l.cards {
		if i >= boxes {
			WidgetSetVisible(c.widget, false)
			continue
		}
		first, last := -1, -1
		if len(l.sections) == 0 {
			first, last = 0, len(l.rows)-1
		} else {
			// The section's rows: its header, then its items.
			for r := range l.rows {
				if l.rows[r].section == i && !l.rows[r].header {
					if first < 0 {
						first = r
					}
					last = r
				} else if first >= 0 {
					break
				}
			}
		}
		if first < 0 || last < first {
			WidgetSetVisible(c.widget, false)
			continue
		}
		WidgetSetVisible(c.widget, true)
		x, w := l.rowX()
		f := [4]float32{x, l.tops[first], w, l.tops[last] + l.rowHeight(last) - l.gapBelow(last) - l.tops[first]}
		if setHostFrame(c, f) {
			moved = true
		}
		queueDraw(c) // what of it shows follows the scroll
	}
	return moved
}

// scrollTo scrolls the least that shows an item (below a header pinned
// over it).
func (l *list) scrollTo(h *Host, index int, animated bool) {
	l.flush(h)
	if index < 0 || index >= len(l.ids) || len(l.tops) != len(l.rows)+1 {
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
	y, vh := l.offset(), l.viewH()
	if top < y {
		y = top
	} else if bottom > y+vh {
		y = bottom - vh
	}
	if l.bottom {
		l.stuck = index == len(l.ids)-1
	}
	l.setOffset(y, animated)
}

// Selection.

func (l *list) setSelecting(on bool) {
	if on == l.selecting {
		return
	}
	l.selecting = on
	for _, rv := range l.shown {
		for _, c := range rv.cells {
			queueDraw(c.n)
		}
	}
}

// selectRow toggles a selecting list's row, as the user clicked it.
func (h *Host) selectRow(c *cell) {
	c.selected = !c.selected
	queueDraw(c.n)
	h.emit(c.n, ops.Event{Kind: ops.EventRowSelected, Bool: c.selected})
}

// cellProp takes a cell's props: its swipe actions and whether it's
// selected.
func cellProp(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
	c, ok := n.ext.(*cell)
	if !ok {
		return false
	}
	switch p {
	case ops.PropSwipeActions:
		c.swipe = decodeMenu(v.Bytes)
	case ops.PropLeadingSwipeActions:
		c.leading = decodeMenu(v.Bytes)
	case ops.PropSelected:
		c.selected = v.Int != 0
		queueDraw(n)
	default:
		return false
	}
	if len(c.swipe)+len(c.leading) > 0 {
		h.listenRowMenu(c)
	}
	return true
}

func listenCell(h *Host, n *node, e ops.EventKind) bool {
	c, ok := n.ext.(*cell)
	if !ok {
		return false
	}
	switch e {
	case ops.EventAction:
		h.listenRowMenu(c)
		return true
	case ops.EventRowSelected:
		return true // the cell's selection click
	}
	return false
}

// listenRowMenu gives a cell its context menu: a secondary click, or a
// long press on a touchscreen.
func (h *Host) listenRowMenu(c *cell) {
	ev := c.n.ev()
	if ev.secondary != 0 {
		return
	}
	r := GestureClickNew()
	ev.secondary = r
	GestureSingleSetButton(r, 3)
	Connect(r, "pressed", func(count int, x, y float64) {
		if h.rowMenu(c, x, y) {
			GestureSetState(r, EventSequenceClaimed)
		}
	})
	WidgetAddController(c.n.widget, r)
	g := GestureLongPressNew()
	ev.long = g
	GestureSingleSetTouchOnly(g, true)
	Connect(g, "pressed", func(x, y float64) {
		if h.rowMenu(c, x, y) {
			GestureSetState(g, EventSequenceClaimed)
		}
	})
	WidgetAddController(c.n.widget, g)
}

// rowMenu shows a row's swipe actions, leading then trailing, as its
// context menu at (x, y) in its cell; a leading one's Action is -1 - its
// index. False: it has none.
func (h *Host) rowMenu(c *cell, x, y float64) bool {
	if c.header || c.l.selecting || len(c.leading)+len(c.swipe) == 0 || c.n.widget == 0 {
		return false
	}
	all := append(append([]action{}, c.leading...), c.swipe...)
	nl := len(c.leading)
	group := SimpleActionGroupNew()
	for i := range all {
		a := SimpleActionNew("item"+strconv.Itoa(i), 0)
		code := int64(i - nl)
		if i < nl {
			code = int64(-1 - i)
		}
		Connect(a, "activate", func(param uintptr) {
			if !c.l.dead && c.n.widget != 0 {
				h.emit(c.n, ops.Event{Kind: ops.EventAction, Int: code})
			}
		})
		ActionMapAddAction(group, a)
		ObjectUnref(a)
	}
	WidgetInsertActionGroup(c.n.widget, "asukarow", group)
	ObjectUnref(group)
	model := h.menuModel(all, "asukarow.item")
	pop := PopoverMenuNewFromModel(model)
	ObjectUnref(model)
	WidgetSetParent(pop, c.n.widget)
	r := IntRect{X: int32(x), Y: int32(y), W: 1, H: 1}
	PopoverSetPointingTo(pop, &r)
	PopoverSetHasArrow(pop, false)
	if c.n.rtl {
		WidgetSetHAlign(pop, AlignEnd)
	} else {
		WidgetSetHAlign(pop, AlignStart)
	}
	Connect(pop, "closed", func() {
		// Unparented once its action (which runs after it closes) has.
		IdleAdd(PriorityDefaultIdle, func() bool {
			if WidgetGetParent(pop) != 0 {
				WidgetUnparent(pop)
			}
			return false
		})
	})
	PopoverPopup(pop)
	return true
}

// Refreshing: F5 or Ctrl+R refreshes the refreshable list the focus is
// in, or else the innermost one on screen (refresh.go's shortcut of the
// window's, as Windows has), a spinner floating over its top while it
// does.

func (l *list) setRefreshable(h *Host, on bool) {
	l.refreshable = on
	if !on {
		l.setRefreshing(false)
		return
	}
	if l.shortcut != 0 {
		return
	}
	sc := ShortcutControllerNew()
	ShortcutControllerSetScope(sc, ShortcutScopeGlobal)
	for _, key := range []string{"F5", "<Control>r"} {
		act := ShortcutAction(func() bool { return !l.dead && h.refreshKey(l.n) })
		ShortcutControllerAddShortcut(sc, ShortcutNew(ShortcutTriggerParseString(key), act))
	}
	WidgetAddController(l.n.widget, sc)
	l.shortcut = sc
}

// refresh starts a refresh, as the user asked for one.
func (l *list) refresh(h *Host) bool {
	if l.dead || !l.refreshable || l.refreshing || !l.n.listening(ops.EventRefresh) || !WidgetGetMapped(l.n.widget) {
		return false
	}
	l.setRefreshing(true) // the core's handler says when it's done
	h.emit(l.n, ops.Event{Kind: ops.EventRefresh})
	return true
}

func (l *list) setRefreshing(on bool) {
	if l.refreshing == on {
		return
	}
	l.refreshing = on
	if on && l.spinner == 0 {
		box := BoxNew(OrientationHorizontal, 0)
		WidgetAddCSSClass(box, "card")
		var sp Ptr
		if AdwSpinnerNew != nil {
			sp = AdwSpinnerNew()
		} else {
			sp = SpinnerNew()
			SpinnerSetSpinning(sp, true)
		}
		WidgetSetSizeRequest(sp, 24, 24)
		for _, f := range []func(Ptr, int32){WidgetSetMarginTop, WidgetSetMarginBottom, WidgetSetMarginStart, WidgetSetMarginEnd} {
			f(sp, 8)
		}
		BoxAppend(box, sp)
		WidgetSetHAlign(box, AlignCenter)
		WidgetSetVAlign(box, AlignStart)
		WidgetSetMarginTop(box, 12)
		WidgetSetCanTarget(box, false)
		WidgetSetParent(box, l.n.widget) // after the scrolled window: over it
		l.spinner = box
	}
	if l.spinner != 0 {
		WidgetSetVisible(l.spinner, on)
		if AdwSpinnerNew == nil {
			SpinnerSetSpinning(WidgetGetFirstChild(l.spinner), on)
		}
	}
}

// Drawing: a plain list's background, its rows' separators, a pinned
// header's background, a selected row's highlight, the inset grouped
// list's cards.

// Shades of libadwaita's (light, dark): $border_color is the text color
// at 15%, $card_shade_color a black of 7% (36% dark).
func (h *Host) cardShade() RGBA {
	if h.dark {
		return RGBA{B: 6.0 / 255, A: 0.36}
	}
	return RGBA{B: 6.0 / 255, A: 0.07}
}

func drawList(h *Host, n *node, snap Ptr, bounds Rect) bool {
	switch x := n.ext.(type) {
	case *list:
		if n.kind != ops.KindList {
			return false
		}
		if x.appearance == ops.ListAppearancePlain && n.background == 0 && len(n.gradient) == 0 && n.material == 0 {
			c := h.theme.view
			Call(FnSnapshotAppendColor, snap, Addr(&c), Addr(&bounds))
		}
		return true
	case *rowView:
		x.draw(h, snap, bounds)
		return true
	case *cell:
		x.draw(h, snap, bounds)
		return true
	case *listCard:
		x.draw(h, n, snap, bounds)
		return true
	}
	return false
}

func (rv *rowView) draw(h *Host, snap Ptr, b Rect) {
	l := rv.l
	r := rv.row
	if r < 0 || r >= len(l.rows) {
		return
	}
	if rv.pinned {
		c := h.theme.view
		if l.appearance == ops.ListAppearanceBare {
			c = h.theme.bg
		}
		Call(FnSnapshotAppendColor, snap, Addr(&c), Addr(&b))
	}
	if l.appearance == ops.ListAppearanceBare || l.columns > 1 || l.rows[r].header ||
		r+1 >= len(l.rows) || l.rows[r+1].header {
		return
	}
	c := withAlpha(h.theme.fg, 0.15)
	if l.grouped() {
		c = h.cardShade()
	}
	line := Rect{X: 0, Y: b.H - 1, W: b.W, H: 1}
	Call(FnSnapshotAppendColor, snap, Addr(&c), Addr(&line))
}

// draw highlights a selected row of a selecting list: the accent at a
// quarter, as libadwaita's selected rows; in a card, its first and last
// rows rounded as the card is.
func (c *cell) draw(h *Host, snap Ptr, b Rect) {
	l := c.l
	if !l.selecting || !c.selected || c.header {
		return
	}
	col := withAlpha(h.theme.accentBg, 0.25)
	rr := Rounded(b, 0)
	if l.grouped() {
		r := c.rv.row
		if r >= 0 && r < len(l.rows) {
			radius := Size{W: 12, H: 12}
			if r == 0 || l.rows[r-1].header {
				rr.Corners[0], rr.Corners[1] = radius, radius
			}
			if r+1 >= len(l.rows) || l.rows[r+1].header {
				rr.Corners[2], rr.Corners[3] = radius, radius
			}
		}
	} else if l.columns > 1 || l.appearance == ops.ListAppearanceBare {
		rr = Rounded(b, 6)
	}
	Call(FnSnapshotPushRoundedClip, snap, Addr(&rr))
	Call(FnSnapshotAppendColor, snap, Addr(&col), Addr(&b))
	Call(FnSnapshotPop, snap)
}

// draw draws an inset grouped list's card: libadwaita's .card (12 points
// round, its hairline and soft shadows), the part of it around what
// shows (a section of thousands of rows is as tall).
func (lc *listCard) draw(h *Host, n *node, snap Ptr, b Rect) {
	l := lc.l
	const margin = 64
	top := l.offset() - n.frame[1] - margin
	bottom := l.offset() + l.viewH() - n.frame[1] + margin
	r := b
	if top > 0 {
		r.Y = top
	}
	if bottom < b.H {
		r.H = bottom - r.Y
	} else {
		r.H = b.H - r.Y
	}
	if r.H <= 0 {
		return
	}
	rr := Rounded(r, 12)
	shadows := [3][4]float32{{0, 0, 1, 0}, {0, 1, 1, 3}, {0, 2, 2, 6}} // dx, dy, spread, blur
	alphas := [3]float32{0.03, 0.07, 0.03}
	if h.dark {
		alphas = [3]float32{0.09, 0.09, 0.06}
	}
	for i, s := range shadows {
		c := RGBA{B: 6.0 / 255, A: alphas[i]}
		SnapshotAppendOutsetShadow(snap, &rr, &c, s[0], s[1], s[2], s[3])
	}
	c := h.theme.card
	Call(FnSnapshotPushRoundedClip, snap, Addr(&rr))
	Call(FnSnapshotAppendColor, snap, Addr(&c), Addr(&r))
	Call(FnSnapshotPop, snap)
}

// releaseList lets go of a list's rows, cells and cards as it goes; the
// rows' content, the core's, goes with the core's own Destroy.
func releaseList(h *Host, n *node) {
	l, ok := n.ext.(*list)
	if !ok || n.kind != ops.KindList || l.dead {
		return
	}
	l.dead = true
	if l.anim != 0 {
		AdwAnimationSkip(l.anim)
	}
	forget := func(x *node) {
		for _, c := range x.children {
			c.parent = nil // the core's: its Destroy has nothing to take it out of
		}
		x.children = nil
		delete(h.nodes, x.id)
		if x.widget != 0 {
			delete(h.byWidget, x.widget)
			if WidgetGetParent(x.widget) != 0 {
				WidgetUnparent(x.widget)
			}
			ObjectUnref(x.widget)
			x.widget = 0
		}
	}
	rows := l.free
	for _, rv := range l.shown {
		rows = append(rows, rv)
	}
	for _, rv := range rows {
		for _, c := range rv.cells {
			forget(c.n)
		}
		forget(rv.n)
	}
	for _, c := range l.cards {
		forget(c)
	}
	ViewportSetChild(l.vp, 0)
	forget(l.doc)
	l.shown, l.free, l.cards = map[int]*rowView{}, nil, nil
}

// dumpList adds the rows a list shows to the view tree's dump.
func dumpList(h *Host, n *node, depth int, b *strings.Builder, walk func(*node, int)) {
	l, ok := n.ext.(*list)
	if !ok || n.kind != ops.KindList {
		return
	}
	rows := make([]int, 0, len(l.shown))
	for r := range l.shown {
		rows = append(rows, r)
	}
	sort.Ints(rows)
	for _, r := range rows {
		rv := l.shown[r]
		f := rv.n.frame
		fmt.Fprintf(b, "%srow %d [%.0f,%.0f %.0fx%.0f]", strings.Repeat("  ", depth+1), r, f[0], f[1], f[2], f[3])
		if rv.pinned {
			b.WriteString(" pinned")
		}
		b.WriteByte('\n')
		for _, c := range rv.cells {
			if WidgetGetVisible(c.n.widget) {
				walk(c.n, depth+2)
			}
		}
	}
}
