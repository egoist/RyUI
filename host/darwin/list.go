//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"sort"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

// A List is a scroll view whose rows the host places itself: as it
// scrolls, rows coming into view take a row view (a recycled one, or a
// new one) and its cells, host-made views the core binds rows into
// through ListBind; rows going out are kept for reuse. A row's height
// is its cells' (a grid's row holds Columns of them), from their frames;
// rows not yet bound are estimated. (NSTableView would ask for heights
// with a callback returning a float, which purego can't make.)
//
// With sections, each section's header is a row of its own (one cell,
// bound with ListBind's index -1 - the section's); a plain or bare
// list's header stays at the top of the list while its section's rows
// scroll under it, as NSTableView's floating group rows do.
type list struct {
	n       *node
	scroll  ID
	doc     ID // the flipped document view the rows are in
	ids     []int64
	rowOf   map[int64]int
	heights map[int64]float64 // by item ID, or a header's by its section's ID
	rows    []lrow            // the rows shown: headers and items
	tops    []float64         // each row's top, then the total height
	shown   map[int]*rowView
	free    []*rowView
	columns int
	gap     float64
	width   float64
	est     float64
	// An inset grouped list's: its look, its rows' inset from each side
	// (and from its top and bottom), and the boxes behind them (a
	// section's each).
	appearance ops.ListAppearance
	inset      float64
	group      ID
	groups     []ID
	sections   []lsection

	// Anchored at the bottom: rows that don't fill it sit at its bottom
	// (lead is the room above them), and while it's at its end it stays
	// there; settling marks a scroll the host makes itself.
	bottom   bool
	stuck    bool
	lead     float64
	settling bool

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
	view  ID
	cells []*cell
	sep   ID
	row   int
}

// cell is a host-made view a row is bound into.
type cell struct {
	n        *node
	l        *list
	item     int64
	height   float64
	swipe    []action
	leading  []action
	header   bool
	selected bool // the core says so
	rv       *rowView
	menu     ID      // its context menu: its swipe actions
	menuTags []int64 // the Action each item of it sends
}

func newList(h *Host, n *node) *list {
	l := &list{n: n, columns: 1, est: 44, rowOf: map[int64]int{}, heights: map[int64]float64{}, shown: map[int]*rowView{}, stuck: true}
	l.scroll = initView("NSScrollView")
	Send(l.scroll, "setHasVerticalScroller:", 1)
	Send(l.scroll, "setAutohidesScrollers:", 1)
	Send(l.scroll, "setAutomaticallyAdjustsContentInsets:", 0)
	Send(l.scroll, "setDrawsBackground:", 0)
	l.doc = initView("RyUIView")
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
	case ops.PropListAppearance:
		l.appearance = ops.ListAppearance(v.Int)
	case ops.PropListInset:
		l.inset = v.Float
	case ops.PropListAnchor:
		l.bottom = ops.ScrollAnchor(v.Int) == ops.ScrollAnchorBottom
		l.stuck = true
	case ops.PropListSelecting:
		l.setSelecting(h, v.Int != 0)
		return
	}
	l.rebind = true
	l.queue(h)
}

// grouped is whether it's an inset grouped list: its rows in one
// rounded box, as System Settings' grouped forms are.
func (l *list) grouped() bool {
	return l.appearance == ops.ListAppearanceInsetGrouped && l.columns <= 1
}

// sticky is whether its headers stay at its top: a plain or bare list's.
func (l *list) sticky() bool {
	return len(l.sections) > 0 && l.appearance != ops.ListAppearanceInsetGrouped
}

// pad is the space above the first row and below the last.
func (l *list) pad() float64 {
	if l.grouped() {
		return l.inset
	}
	return 0
}

// placeGroup puts the groups' boxes behind the rows (a section's each):
// a faint fill over the window's background, rounded, as System
// Settings' groups are.
func (l *list) placeGroup(h *Host) {
	boxes := 0
	if l.grouped() && len(l.ids) > 0 {
		boxes = max(len(l.sections), 1)
	}
	for len(l.groups) < boxes {
		g := initView("NSView")
		Send(g, "setWantsLayer:", 1)
		MsgSetFloat(Send(g, "layer"), Sel("setCornerRadius:"), 10)
		Send(l.doc, "addSubview:positioned:relativeTo:", uintptr(g), uintptr(^uint(0)), 0) // below the rows
		Release(g)
		l.groups = append(l.groups, g)
		l.colors(h)
	}
	if len(l.groups) > 0 {
		l.group = l.groups[0]
	}
	for i, g := range l.groups {
		if i >= boxes {
			Send(g, "setHidden:", 1)
			continue
		}
		Send(g, "setHidden:", 0)
		// From its first item row to its last.
		first, last := -1, -1
		for r, row := range l.rows {
			if !row.header && (len(l.sections) == 0 || row.section == i) {
				if first < 0 {
					first = r
				}
				last = r
			}
		}
		if first < 0 {
			Send(g, "setHidden:", 1)
			continue
		}
		MsgSetRect(g, selSetFrame, Rect{Origin: Point{X: l.inset, Y: l.tops[first]},
			Size: Size{Width: max(l.width-2*l.inset, 0), Height: l.tops[last] + l.rowHeight(last) - l.gapBelow(last) - l.tops[first]}})
	}
}

// colors sets the groups' color again (a layer's CGColor is fixed), and
// the pinned headers' background.
func (l *list) colors(h *Host) {
	fill := "quaternarySystemFillColor" // macOS 14
	if !SendBool(Class("NSColor"), "respondsToSelector:", uintptr(Sel(fill))) {
		fill = "quaternaryLabelColor"
	}
	h.withAppearance(func() {
		for _, g := range l.groups {
			Send(Send(g, "layer"), "setBackgroundColor:", cgColor(Send(Class("NSColor"), fill)))
		}
		for _, rv := range l.shown {
			l.headerBackground(rv)
		}
	})
}

func (l *list) background(h *Host, n *node) {
	Send(l.scroll, "setDrawsBackground:", 0)
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

// flush applies an update waiting, if any. Rows added or removed above
// the ones in view keep those where they are, unless the list is at
// the edge it's anchored to: at its top, new rows there show; at its
// end, anchored at the bottom, it goes to its new end.
func (l *list) flush(h *Host) {
	if !l.updated {
		l.tile(h)
		return
	}
	l.updated = false
	keep, dy, ok := l.anchorRow()
	if l.bottom && l.stuck || !l.bottom && l.atStart() {
		ok = false
	}
	appended := l.applied && len(l.pending) > len(l.ids) && (len(l.ids) == 0 || l.pending[len(l.pending)-1] != l.ids[len(l.ids)-1])
	l.ids = l.pending
	l.sections = l.pendingS
	l.pending, l.pendingS = nil, nil
	clear(l.rowOf)
	for i, id := range l.ids {
		l.rowOf[id] = i
	}
	live := make(map[int64]float64, len(l.ids)+len(l.sections))
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
	animate := appended && l.bottom && l.stuck && SendBool(Send(l.scroll, "window"), "isVisible")
	l.tileKeeping(h, keep, dy, ok, animate)
	l.applied = l.applied || len(l.ids) > 0
	if animate {
		l.toEnd(h, true)
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
// are bound (a grid's rows add the gap below, but a section's last).
func (l *list) rowHeight(r int) float64 {
	row := l.rows[r]
	height, known := 0.0, false
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
func (l *list) gapBelow(r int) float64 {
	if l.columns > 1 && !l.rows[r].header && r+1 < len(l.rows) && !l.rows[r+1].header {
		return l.gap
	}
	return 0
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
	if l.bottom {
		vh := MsgRect(Send(l.scroll, "contentView"), Sel("bounds")).Size.Height
		if total := l.tops[len(l.tops)-1]; total < vh {
			l.lead = vh - total
			for i := range l.tops {
				l.tops[i] += l.lead
			}
		}
	}
}

// visible is the clip view's bounds: what the list shows.
func (l *list) visible() Rect { return MsgRect(Send(l.scroll, "contentView"), Sel("bounds")) }

// tile shows the rows in view (and a screen's worth around them), binds
// the ones new to the view, and places them. Rows above the one at the
// top that change height as they bind (an estimate made real) don't
// move what's in view; a list anchored at its bottom and at its end
// stays there.
func (l *list) tile(h *Host) {
	keep, dy, ok := l.anchorRow()
	l.tileKeeping(h, keep, dy, ok, false)
}

// tileKeeping tiles, keeping the row with key dy below the top (if ok);
// holdEnd leaves a list at its end where it is, for a scroll to its new
// end to animate.
func (l *list) tileKeeping(h *Host, key int64, dy float64, ok, holdEnd bool) {
	if l.tiling || h.core == nil {
		return
	}
	l.tiling = true
	defer func() { l.tiling = false }()
	for pass := 0; pass < 4; pass++ {
		l.measureTops()
		total := l.tops[len(l.tops)-1]
		MsgSetSize(l.doc, Sel("setFrameSize:"), Size{Width: l.width, Height: total})
		switch {
		case l.bottom && l.stuck:
			if !l.settling && !holdEnd {
				l.toEnd(h, false)
			}
		case ok:
			l.restore(h, key, dy)
		}
		vb := l.visible()
		top, bottom := vb.Origin.Y-vb.Size.Height/2, vb.Origin.Y+vb.Size.Height*1.5
		rows := l.rowCount()
		first := sort.Search(rows, func(r int) bool { return l.tops[r+1] > top })
		last := sort.Search(rows, func(r int) bool { return l.tops[r] >= bottom })
		pin := l.pinnedRow(vb.Origin.Y)
		// Rows gone from view go back to the pool.
		for r, rv := range l.shown {
			if (r < first || r >= last) && r != pin {
				delete(l.shown, r)
				Send(rv.view, "setHidden:", 1)
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
	for r, rv := range l.shown {
		l.place(h, rv, r)
	}
	l.placeGroup(h)
}

// anchorRow is the row at the top of what the list shows, by its key,
// and how far its top is below the top of the view.
func (l *list) anchorRow() (key int64, dy float64, ok bool) {
	if len(l.rows) == 0 || len(l.tops) != len(l.rows)+1 {
		return 0, 0, false
	}
	y := l.visible().Origin.Y
	r := sort.Search(len(l.rows), func(r int) bool { return l.tops[r+1] > y })
	if r >= len(l.rows) {
		return 0, 0, false
	}
	return l.rowKey(r), l.tops[r] - y, true
}

// restore scrolls so that the row with key is dy below the top again.
func (l *list) restore(h *Host, key int64, dy float64) {
	for r := range l.rows {
		if l.rowKey(r) != key {
			continue
		}
		vb := l.visible()
		if y := l.tops[r] - dy; y != vb.Origin.Y {
			l.setOffset(h, y, false)
		}
		return
	}
}

// toEnd scrolls to the list's end.
func (l *list) toEnd(h *Host, animated bool) {
	vb := l.visible()
	end := max(l.tops[len(l.tops)-1]-vb.Size.Height, 0)
	if end != vb.Origin.Y {
		l.setOffset(h, end, animated)
	}
}

// setOffset scrolls the list itself (not the user): what it shows
// doesn't move a list anchored at its bottom off its end, or back, until
// it's done.
func (l *list) setOffset(h *Host, y float64, animated bool) {
	x := l.visible().Origin.X
	l.settling = true
	if !animated {
		h.scrollToOffset(l.n, x, y, false)
		l.settling = false
		return
	}
	anim := NewBlock(func(_ objc.Block, ctx ID) {
		MsgSetFloat(ctx, Sel("setDuration:"), 0.25)
		h.scrollToOffset(l.n, x, y, true)
	})
	done := NewBlock(func(_ objc.Block) {
		l.settling = false
		// Not a list gone while it scrolled (its screen closed).
		if l.bottom && h.alive(l.n) {
			l.stuck = l.atEnd()
		}
	})
	Send(Class("NSAnimationContext"), "runAnimationGroup:completionHandler:", uintptr(anim), uintptr(done))
}

func (l *list) atStart() bool { return l.visible().Origin.Y <= 0.5 }

func (l *list) atEnd() bool {
	vb := l.visible()
	return len(l.tops) == 0 || vb.Origin.Y+vb.Size.Height >= l.tops[len(l.tops)-1]-1
}

// pinnedRow is the header row that stays at the top of a plain list:
// its section's, once its own place has scrolled above y (-1: none).
func (l *list) pinnedRow(y float64) int {
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

func (l *list) take(h *Host) *rowView {
	if k := len(l.free); k > 0 {
		rv := l.free[k-1]
		l.free = l.free[:k-1]
		Send(rv.view, "setHidden:", 0)
		return rv
	}
	v := initView("RyUIView")
	Send(v, "setWantsLayer:", 1)
	Send(l.doc, "addSubview:", uintptr(v))
	Release(v)
	return &rowView{view: v, row: -1}
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
		Send(c.n.view, "setHidden:", Bool(i >= k))
	}
	l.headerBackground(rv)
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
		l.highlight(c)
	}
}

// headerBackground gives a header row of a plain list the window's
// background, so the rows it's pinned over don't show through.
func (l *list) headerBackground(rv *rowView) {
	if rv.row < 0 || rv.row >= len(l.rows) {
		return
	}
	layer := Send(rv.view, "layer")
	if l.rows[rv.row].header && l.sticky() {
		theHost.withAppearance(func() {
			Send(layer, "setBackgroundColor:", cgColor(Send(Class("NSColor"), "windowBackgroundColor")))
		})
	} else {
		Send(layer, "setBackgroundColor:", 0)
	}
}

func (h *Host) newCell(l *list, rv *rowView) *cell {
	h.nextCell++
	id := ops.HostIDBit | h.nextCell
	v := initView("RyUIView")
	Send(v, "setWantsLayer:", 1)
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
	if _, ok := l.heights[c.item]; !ok && len(l.heights) < 64 && !c.header {
		// The estimate follows the first rows'.
		l.est = (l.est*float64(len(l.heights)) + height) / float64(len(l.heights)+1)
	}
	l.heights[c.item] = height
	if !l.tiling {
		l.queue(theHost)
	}
}

// place puts a row view at its row's top (a pinned header at the top
// of what shows, pushed up by the next header) and lays out its cells:
// a grid's side by side, each (width - gap × (columns - 1)) / columns
// wide.
func (l *list) place(h *Host, rv *rowView, r int) {
	height := l.rowHeight(r)
	x, w := 0.0, l.width
	if l.grouped() {
		x, w = l.inset, max(l.width-2*l.inset, 0)
	}
	y := l.tops[r]
	row := l.rows[r]
	if row.header && l.sticky() {
		vb := l.visible()
		if y < vb.Origin.Y {
			y = vb.Origin.Y
			for n := r + 1; n < len(l.rows); n++ {
				if l.rows[n].header {
					y = min(y, l.tops[n]-height)
					break
				}
			}
		}
		// Over the rows that scroll under it.
		Send(l.doc, "addSubview:positioned:relativeTo:", uintptr(rv.view), 1, 0)
	}
	MsgSetRect(rv.view, selSetFrame, Rect{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: height}})
	if l.columns <= 1 || row.header {
		for _, c := range rv.cells {
			MsgSetRect(c.n.view, selSetFrame, Rect{Size: Size{Width: w, Height: c.height}})
		}
		l.separator(rv, r, w, height)
		return
	}
	if rv.sep != 0 {
		Send(rv.sep, "setHidden:", 1)
	}
	cw := (l.width - l.gap*float64(l.columns-1)) / float64(l.columns)
	rtl := h.isRTL(l.n)
	for i, c := range rv.cells {
		// A grid's columns run the way the list reads.
		col := i
		if rtl {
			col = l.columns - 1 - i
		}
		MsgSetRect(c.n.view, selSetFrame, Rect{Origin: Point{X: float64(col) * (cw + l.gap)}, Size: Size{Width: cw, Height: c.height}})
	}
}

// separator is a list row's hairline at its bottom, but the last row's
// and a section's last's (nor a header's); in a group, inset from the
// box's sides.
func (l *list) separator(rv *rowView, r int, w, height float64) {
	if l.appearance == ops.ListAppearanceBare {
		return
	}
	if rv.sep == 0 {
		rv.sep = initView("NSBox")
		Send(rv.sep, "setBoxType:", 2) // separator
		Send(rv.view, "addSubview:", uintptr(rv.sep))
		Release(rv.sep)
	}
	x := 0.0
	if l.grouped() {
		x = 10
	}
	MsgSetRect(rv.sep, selSetFrame, Rect{Origin: Point{X: x, Y: height - 1}, Size: Size{Width: max(w-2*x, 0), Height: 1}})
	Send(rv.sep, "setHidden:", Bool(r >= len(l.rows)-1 || l.rows[r].header || l.rows[r+1].header))
}

// resized lays the rows out at the list's new width; one at its end
// stays there.
func (l *list) resized(h *Host) {
	w := l.n.frame.Size.Width
	if w != l.width {
		l.width = w
		l.rebind = true // the core lays the rows out at the new width
	}
	l.queue(h)
}

// scrolledList tiles a list as it scrolls; the user's scrolling moves a
// list anchored at its bottom off its end, or back.
func (h *Host) scrolledList(clip ID) bool {
	l := h.lists[clip]
	if l == nil {
		return false
	}
	if l.bottom && !l.settling && !l.tiling {
		l.stuck = l.atEnd()
	}
	l.tile(h)
	return true
}

func (l *list) scrollTo(h *Host, index int, animated bool) {
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
	b := l.visible()
	top, bottom := l.tops[row], l.tops[row+1]
	if l.sticky() {
		// Below the header pinned over it.
		for r := row; r >= 0; r-- {
			if l.rows[r].header {
				top -= l.rowHeight(r)
				break
			}
		}
	}
	y := b.Origin.Y
	if top < y {
		y = top
	} else if bottom > y+b.Size.Height {
		y = bottom - b.Size.Height
	}
	if l.bottom {
		l.stuck = index == len(l.ids)-1
	}
	l.setOffset(h, y, animated)
}

// setSelecting puts the list in selection mode: a click on a row
// selects or deselects it (RowSelected), and selected rows are
// highlighted, as a table's selected rows are.
func (l *list) setSelecting(h *Host, on bool) {
	if on == l.selecting {
		return
	}
	l.selecting = on
	for _, rv := range l.shown {
		for _, c := range rv.cells {
			l.highlight(c)
		}
	}
}

// highlight shows whether c's row is selected: the selected content
// background (the accent, unemphasized when the window isn't key).
func (l *list) highlight(c *cell) {
	layer := Send(c.n.view, "layer")
	if layer == 0 || c.header {
		return
	}
	if l.selecting && c.selected {
		theHost.withAppearance(func() {
			color := Send(Class("NSColor"), "selectedContentBackgroundColor")
			if w := Send(c.n.view, "window"); w != 0 && !SendBool(w, "isKeyWindow") {
				color = Send(Class("NSColor"), "unemphasizedSelectedContentBackgroundColor")
			}
			Send(layer, "setBackgroundColor:", cgColor(color))
		})
		MsgSetFloat(layer, Sel("setCornerRadius:"), 6)
		return
	}
	Send(layer, "setBackgroundColor:", 0)
}

// setSelected is the core's word on whether c's row is selected.
func (c *cell) setSelected(on bool) {
	c.selected = on
	c.l.highlight(c)
}

// selectClick toggles the selection of the row view is in, if it's in a
// selecting list; false: it isn't.
func (h *Host) selectClick(view ID) bool {
	for v := view; v != 0; v = Send(v, "superview") {
		n := h.byView[v]
		if n == nil || n.cell == nil {
			continue
		}
		c := n.cell
		if !c.l.selecting || c.header {
			return false
		}
		c.selected = !c.selected
		c.l.highlight(c)
		h.emit(n, ops.Event{Kind: ops.EventRowSelected, Bool: c.selected})
		return true
	}
	return false
}

// cellMenu is a row's context menu: its swipe actions, leading then
// trailing (a Mac has no swipes for them but the trackpad's, which a
// list of its own wouldn't see).
func (h *Host) cellMenu(c *cell) ID {
	if c.menu != 0 {
		delete(h.byMenu, c.menu)
		Release(c.menu)
		c.menu = 0
	}
	c.menuTags = c.menuTags[:0]
	var all []action
	for i, a := range c.leading {
		all = append(all, a)
		c.menuTags = append(c.menuTags, int64(-1-i))
	}
	for i, a := range c.swipe {
		all = append(all, a)
		c.menuTags = append(c.menuTags, int64(i))
	}
	if len(all) == 0 || c.l.selecting {
		return 0
	}
	c.menu = h.makeMenuOf(all, "menuItem:")
	h.byMenu[c.menu] = c.n
	return c.menu
}
