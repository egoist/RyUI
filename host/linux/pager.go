//go:build linux && !android

package linux

import (
	"math"
	"strconv"
	"time"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/layout"
	"github.com/egoist/ryui/ops"
)

// PageControls and paging Scrolls. GTK has no page control that stands
// alone (AdwCarouselIndicatorDots draws an AdwCarousel's): a PageControl
// is drawn by its view as libadwaita's dots are (the current one larger
// and darker, in the text color), measured, and clicks, the arrow keys
// and a screen reader's slider change the page.
//
// A paging Scroll hides its scroll bars and settles on whole pages: a
// wheel's notch moves a page, as a carousel's does; a touchpad's or a
// touchscreen's swipe scrolls with the fingers, then settles on the page
// it was going to (the next one past a quick flick, else the nearest),
// one page a swipe; the arrow keys, Page Up and Page Down move a page,
// Home and End to the first and last. Each settling sends Paged.

const (
	pageDotSize   = 6.0  // a dot's diameter
	pageDotOn     = 8.0  // the current one's
	pageDotCell   = 13.0 // a dot and the gap to the next
	pageDotsHigh  = 24.0
	pageDotsAlpha = 0.3 // the others' opacity, the current one's 0.9
)

type pageDots struct {
	count, page int
	click, keys Ptr
}

var pagers = map[*node]*pageDots{}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindPageControl {
			return false
		}
		h.makePageDots(n)
		return true
	})
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		if d := pagers[n]; d != nil {
			switch p {
			case ops.PropPageControlCount:
				d.count = max(int(v.Int), 0)
			case ops.PropPageControlPage:
				d.page = int(v.Int)
			default:
				return false
			}
			h.dotsChanged(n, d)
			return true
		}
		if n.scroll != nil && p == ops.PropScrollPaging {
			h.setPaging(n, v.Int != 0)
			return true
		}
		return false
	})
	measureHooks = append(measureHooks, func(h *Host, n *node, c layout.Constraints) (layout.Size, bool) {
		d := pagers[n]
		if d == nil {
			return layout.Size{}, false
		}
		return layout.Size{W: float32(d.count)*pageDotCell + pageDotCell, H: pageDotsHigh}, true
	})
	drawHooks = append(drawHooks, func(h *Host, n *node, snap Ptr, bounds Rect) bool {
		d := pagers[n]
		if d == nil {
			return false
		}
		h.drawDots(n, d, snap, bounds)
		return true
	})
	scrollHooks = append(scrollHooks, func(h *Host, n *node) {
		if p := pagings[n]; p != nil {
			h.pagingMoved(n, p)
		}
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		delete(pagers, n)
		if p := pagings[n]; p != nil {
			p.stop()
			delete(pagings, n)
		}
	})
}

func (h *Host) makePageDots(n *node) {
	d := &pageDots{}
	pagers[n] = d
	w := n.widget
	setRole(w, "slider")
	WidgetSetFocusable(w, true)
	WidgetSetFocusOnClick(w, false)
	WidgetAddCSSClass(w, "ryui-focus-ring")
	d.click = GestureClickNew()
	GestureSingleSetButton(d.click, 1)
	Connect(d.click, "pressed", func(count int, x, y float64) {
		h.chooseDot(n, h.dotAt(n, d, float32(x)))
	})
	WidgetAddController(w, d.click)
	d.keys = EventControllerKeyNew()
	Connect(d.keys, "key-pressed", func(keyval, keycode, state uint32) bool {
		return h.dotsKey(n, d, keyval)
	})
	WidgetAddController(w, d.keys)
}

func (h *Host) dotsChanged(n *node, d *pageDots) {
	if d.count > 0 && n.widget != 0 {
		// A screen reader reads it as a slider: "page 2 of 4".
		setA11yString(n.widget, 18, strconv.Itoa(d.page+1)+" / "+strconv.Itoa(d.count)) // GTK_ACCESSIBLE_PROPERTY_VALUE_TEXT
	}
	queueDraw(n)
}

// dotAt is the dot under x, in the reading direction.
func (h *Host) dotAt(n *node, d *pageDots, x float32) int {
	if d.count == 0 {
		return -1
	}
	w, _ := h.size(n, n.widget)
	i := int(math.Floor(float64((x - (w-float32(d.count)*pageDotCell)/2) / pageDotCell)))
	if n.rtl {
		i = d.count - 1 - i
	}
	return min(max(i, 0), d.count-1)
}

// chooseDot makes page i the current one, as the user chose it.
func (h *Host) chooseDot(n *node, i int) {
	d := pagers[n]
	if d == nil || i < 0 || i >= d.count || i == d.page {
		return
	}
	d.page = i
	h.dotsChanged(n, d)
	h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
}

// dotsKey: the arrow keys move a page (forward is the reading
// direction's), Home and End to the ends, as a slider's.
func (h *Host) dotsKey(n *node, d *pageDots, keyval uint32) bool {
	next, prev := 1, -1
	if n.rtl {
		next, prev = -1, 1
	}
	switch keyval {
	case keyRight, keyKPRight:
		h.chooseDot(n, d.page+next)
	case keyLeft, keyKPLeft:
		h.chooseDot(n, d.page+prev)
	case keyUp, keyKPUp, keyPageUp:
		h.chooseDot(n, d.page+1)
	case keyDown, keyKPDown, keyPageDown:
		h.chooseDot(n, d.page-1)
	case keyHome:
		h.chooseDot(n, 0)
	case keyEnd:
		h.chooseDot(n, d.count-1)
	default:
		return false
	}
	return true
}

// GDK's keyvals the host takes here.
const (
	keyLeft     = 0xff51
	keyUp       = 0xff52
	keyRight    = 0xff53
	keyDown     = 0xff54
	keyPageUp   = 0xff55
	keyPageDown = 0xff56
	keyHome     = 0xff50
	keyEnd      = 0xff57
	keyKPLeft   = 0xff96
	keyKPUp     = 0xff97
	keyKPRight  = 0xff98
	keyKPDown   = 0xff99
)

func (h *Host) drawDots(n *node, d *pageDots, snap Ptr, bounds Rect) {
	c := h.theme.fg
	if n.color != 0 {
		if t, ok := h.resolve(n.color); ok {
			c = t
		}
	}
	x0 := (bounds.W - float32(d.count)*pageDotCell) / 2
	for i := range d.count {
		slot := i
		if n.rtl {
			slot = d.count - 1 - i
		}
		size, col := float32(pageDotSize), c
		col.A *= pageDotsAlpha
		if i == d.page {
			size, col = pageDotOn, c
			col.A *= 0.9
		}
		cx := x0 + (float32(slot)+0.5)*pageDotCell
		r := Rect{X: cx - size/2, Y: bounds.H/2 - size/2, W: size, H: size}
		rr := Rounded(r, size/2)
		Call(FnSnapshotPushRoundedClip, snap, Addr(&rr))
		Call(FnSnapshotAppendColor, snap, Addr(&col), Addr(&r))
		Call(FnSnapshotPop, snap)
	}
}

// Paging scrolls.

type paging struct {
	n          *node
	wheel      Ptr     // the scroll controller (capture: wheels page)
	keys       Ptr     // the key controller
	sum        float64 // a wheel's notches, as they add up
	touchpad   bool    // a touchpad's scroll is under way
	velocity   float64 // points a second along the axis, as it moved
	lastPos    float64
	lastT      time.Time
	startPage  int // the page a swipe started from
	moving     bool
	settleT    uint32 // the timer that settles a scroll once it rests
	tick       uint32
	animTo     float64 // where the host's scroll goes (animating)
	animating  bool
	selfMoving bool // the host is moving it
	paged      int  // the page last reported
}

var pagings = map[*node]*paging{}

func (h *Host) setPaging(n *node, on bool) {
	s := n.scroll
	p := pagings[n]
	if !on {
		if p != nil {
			p.stop()
			WidgetRemoveController(s.sw, p.wheel)
			WidgetRemoveController(s.sw, p.keys)
			delete(pagings, n)
		}
		ScrolledWindowSetKineticScrolling(s.sw, true)
		if s.horizontal {
			ScrolledWindowSetPolicy(s.sw, PolicyAutomatic, PolicyNever)
		} else {
			ScrolledWindowSetPolicy(s.sw, PolicyNever, PolicyAutomatic)
		}
		return
	}
	if p != nil {
		return
	}
	p = &paging{n: n, paged: -1}
	pagings[n] = p
	// Scrollable, without a scroll bar; no momentum of its own: the
	// host settles each swipe.
	if s.horizontal {
		ScrolledWindowSetPolicy(s.sw, PolicyExternal, PolicyNever)
	} else {
		ScrolledWindowSetPolicy(s.sw, PolicyNever, PolicyExternal)
	}
	ScrolledWindowSetKineticScrolling(s.sw, false)

	c := EventControllerScrollNew(ScrollBothAxes | ScrollKinetic)
	p.wheel = c
	EventControllerSetPropagationPhase(c, PhaseCapture)
	Connect(c, "scroll", func(dx, dy float64) bool { return h.pagingScroll(n, p, dx, dy) })
	Connect(c, "scroll-end", func() {
		if p.touchpad {
			h.settleSoon(n, p, 0)
		}
	})
	Connect(c, "decelerate", func(vx, vy float64) {
		if s.horizontal {
			p.velocity = vx
		} else {
			p.velocity = vy
		}
		h.settleSoon(n, p, 0)
	})
	WidgetAddController(s.sw, c)

	k := EventControllerKeyNew()
	p.keys = k
	EventControllerSetPropagationPhase(k, PhaseCapture)
	Connect(k, "key-pressed", func(keyval, keycode, state uint32) bool {
		return h.pagingKey(n, p, keyval, state)
	})
	WidgetAddController(s.sw, k)
}

// stop ends what the paging has under way.
func (p *paging) stop() {
	SourceRemove(p.settleT)
	p.settleT = 0
	if p.tick != 0 && p.n.scroll != nil {
		Draw.WidgetRemoveTickCallback(p.n.scroll.sw, p.tick)
	}
	p.tick = 0
	p.animating = false
}

// metrics are the scroll's along its axis: where it is, a page's size,
// how many pages.
func (h *Host) pageMetrics(n *node) (pos, size float64, pages int) {
	s := n.scroll
	adj := s.vadj
	if s.horizontal {
		adj = s.hadj
	}
	pos, size = AdjustmentGetValue(adj), AdjustmentGetPageSize(adj)
	if size <= 0 {
		return pos, 0, 0
	}
	pages = max(int(math.Ceil(AdjustmentGetUpper(adj)/size-1e-3)), 1)
	return pos, size, pages
}

// currentPage is the page the scroll shows (or is going to).
func (h *Host) currentPage(n *node, p *paging) int {
	pos, size, pages := h.pageMetrics(n)
	if size <= 0 {
		return 0
	}
	if p.animating {
		pos = p.animTo
	}
	return min(max(int(math.Round(pos/size)), 0), pages-1)
}

// pagingScroll takes a wheel's notches as pages; a touchpad's scroll
// moves it as it goes, and settles once it ends.
func (h *Host) pagingScroll(n *node, p *paging, dx, dy float64) bool {
	s := n.scroll
	if EventControllerScrollGetUnit(p.wheel) == 0 { // GDK_SCROLL_UNIT_WHEEL: a mouse's
		// Along its axis: a horizontal one takes a tilted wheel, or
		// Shift and the wheel; the plain wheel goes on to a scroll
		// around it.
		d := dy
		if s.horizontal {
			d = dx
			if d == 0 && EventControllerGetCurrentEventState(p.wheel)&ModShift != 0 {
				d = dy
			}
		}
		if d == 0 {
			return false
		}
		p.sum += d
		if math.Abs(p.sum) < 1 {
			return true
		}
		step := 1
		if p.sum < 0 {
			step = -1
		}
		p.sum = 0
		h.goToPage(n, p, h.currentPage(n, p)+step)
		return true
	}
	along := dy
	if s.horizontal {
		along = dx
	}
	if along == 0 {
		return false // across it: for a scroll around it
	}
	if !p.touchpad {
		p.touchpad = true
		h.beginSwipe(n, p)
	}
	h.settleSoon(n, p, 150) // X11 sends no end of a touchpad's scroll
	return false            // GTK scrolls it with the fingers
}

// beginSwipe notes where a swipe starts.
func (h *Host) beginSwipe(n *node, p *paging) {
	p.stopAnimation(n)
	p.startPage = h.currentPage(n, p)
	p.moving = true
	p.velocity = 0
	p.lastT = time.Time{}
}

// pagingMoved follows the scroll as it moves (a touchscreen's drag, a
// scroll bar's, a key's): it settles once it rests.
func (h *Host) pagingMoved(n *node, p *paging) {
	if p.selfMoving {
		return
	}
	pos, _, _ := h.pageMetrics(n)
	now := time.Now()
	if !p.moving {
		p.animating = false
		h.beginSwipe(n, p)
	} else if dt := now.Sub(p.lastT).Seconds(); !p.lastT.IsZero() && dt > 0 {
		v := (pos - p.lastPos) / dt
		if dt > 0.1 {
			v = 0
		}
		p.velocity = 0.6*v + 0.4*p.velocity
	}
	p.lastPos, p.lastT = pos, now
	if !p.touchpad {
		h.settleSoon(n, p, 120)
	}
}

// settleSoon settles the scroll after ms milliseconds without moving
// (0: once the signals of this event are through).
func (h *Host) settleSoon(n *node, p *paging, ms uint32) {
	SourceRemove(p.settleT)
	settle := func() bool {
		p.settleT = 0
		if pagings[n] == p {
			h.settle(n, p)
		}
		return false
	}
	if ms == 0 {
		p.settleT = IdleAdd(PriorityDefault, settle)
	} else {
		p.settleT = TimeoutAdd(ms, settle)
	}
}

// settle moves a scroll that came to rest to the page it was going to:
// past a quick flick the next one, else the nearest; one page a swipe.
func (h *Host) settle(n *node, p *paging) {
	if !p.moving {
		return
	}
	p.moving, p.touchpad = false, false
	pos, size, pages := h.pageMetrics(n)
	if size <= 0 {
		return
	}
	at := pos / size
	page := int(math.Round(at))
	const flick = 300 // points a second
	switch {
	case p.velocity > flick:
		page = int(math.Ceil(at - 1e-3))
	case p.velocity < -flick:
		page = int(math.Floor(at + 1e-3))
	}
	page = min(max(page, p.startPage-1, 0), p.startPage+1, pages-1)
	h.goToPage(n, p, page)
}

// goToPage scrolls to a page, animated, and reports it.
func (h *Host) goToPage(n *node, p *paging, page int) {
	_, size, pages := h.pageMetrics(n)
	if size <= 0 {
		return
	}
	page = min(max(page, 0), pages-1)
	p.moving = false
	h.animateScroll(n, p, float64(page)*size)
	if page != p.paged {
		p.paged = page
		h.emit(n, ops.Event{Kind: ops.EventPaged, Int: int64(page)})
	}
}

// animateScroll moves the scroll to an offset along its axis, easing
// out over a quarter second on its frame clock (at once with Reduce
// Motion).
func (h *Host) animateScroll(n *node, p *paging, to float64) {
	s := n.scroll
	adj := s.vadj
	if s.horizontal {
		adj = s.hadj
	}
	from := AdjustmentGetValue(adj)
	p.stopAnimation(n)
	if from == to || h.env.ReduceMotion || !WidgetGetMapped(s.sw) {
		p.selfMoving = true
		AdjustmentSetValue(adj, to)
		p.selfMoving = false
		return
	}
	p.animating, p.animTo = true, to
	start := time.Now()
	const duration = 0.25
	p.tick = AddTickCallback(s.sw, func(Ptr) bool {
		if pagings[n] != p || !p.animating {
			return false
		}
		t := min(time.Since(start).Seconds()/duration, 1)
		e := 1 - math.Pow(1-t, 3)
		p.selfMoving = true
		AdjustmentSetValue(adj, from+(to-from)*e)
		p.selfMoving = false
		if t >= 1 {
			p.animating, p.tick = false, 0
			return false
		}
		return true
	})
}

func (p *paging) stopAnimation(n *node) {
	if p.tick != 0 && n.scroll != nil {
		Draw.WidgetRemoveTickCallback(n.scroll.sw, p.tick)
	}
	p.tick = 0
	p.animating = false
}

// pagingKey: a page with the arrow keys along the axis, Page Up and Page
// Down; Home and End go to the ends. A text field in a page keeps its
// keys.
func (h *Host) pagingKey(n *node, p *paging, keyval, state uint32) bool {
	if state&(ModControl|ModAlt|ModSuper) != 0 || focusEdits(n) {
		return false
	}
	s := n.scroll
	step := 0
	switch keyval {
	case keyRight, keyKPRight:
		if s.horizontal {
			step = 1
		}
	case keyLeft, keyKPLeft:
		if s.horizontal {
			step = -1
		}
	case keyDown, keyKPDown:
		if !s.horizontal {
			step = 1
		}
	case keyUp, keyKPUp:
		if !s.horizontal {
			step = -1
		}
	case keyPageDown:
		step = 1
	case keyPageUp:
		step = -1
	case keyHome:
		h.goToPage(n, p, 0)
		return true
	case keyEnd:
		_, _, pages := h.pageMetrics(n)
		h.goToPage(n, p, pages-1)
		return true
	}
	if step == 0 {
		return false
	}
	h.goToPage(n, p, h.currentPage(n, p)+step)
	return true
}

// focusEdits reports whether the focus in n's window is in a text it
// edits (a field's), which keeps the keys.
func focusEdits(n *node) bool {
	root := WidgetGetRoot(n.widget)
	if root == 0 {
		return false
	}
	f := WindowGetFocus(root)
	if f == 0 {
		return false
	}
	if editableType == 0 {
		editableType = Call(Sym("gtk_editable_get_type"))
		textViewType = Call(Sym("gtk_text_view_get_type"))
	}
	return TypeCheckInstanceIsA(f, editableType) || TypeCheckInstanceIsA(f, textViewType)
}

var editableType, textViewType GType
