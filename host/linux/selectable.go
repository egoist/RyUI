//go:build linux && !android

package linux

import (
	"math"
	"strings"
	"unicode/utf8"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A Selectable selects across the RichTexts in it as one text, as macOS
// and iOS do. GTK keeps one label's selection at a time (a label that
// selects takes the primary selection, and the one that had it lets its
// selection go), so the selection is the host's: a drag on the
// Selectable's view (claimed in the capture phase, before the labels'
// gestures) maps the pointer to a text and a character in its label's
// Pango layout, each text between the two ends, in reading order (the
// tree's), has its part, and each draws its part's boxes under its label
// (Pango's, GDK's clip region of the range) in libadwaita's selection
// color (the accent's while the Selectable has the focus). A double
// click selects words, a triple click paragraphs (a drag then extends by
// them), Shift+click extends the selection, Ctrl+A in the Selectable
// selects all of it, and Ctrl+C, Ctrl+Insert and the context menu's Copy
// (GTK's words) put its text on the clipboard (each text's part a
// paragraph), as a selection puts it in the primary selection. A press on
// a link, a control or a pressable view in it is theirs; on a
// touchscreen a drag scrolls, a long press selects a word.
//
// The Selectable's view is the tab stop and takes the keys.

type selectArea struct {
	drag   Ptr
	copy   Ptr       // its Copy action, enabled while there's a selection
	anchor [2]selPos // the unit the selection started on (a point, a word, a paragraph)
	has    bool      // anchor is set
	unit   int       // 0 characters, 1 words, 2 paragraphs
	taking bool      // the primary button's drag is selecting
	startX float64
	startY float64

	clicks   int
	lastTime uint32
	lastX    float64
	lastY    float64
}

// selPos is a place in a Selectable's texts: a RichText and a character
// offset in its label's text.
type selPos struct {
	t   *node
	off int
}

var selectAreas = map[*node]*selectArea{}

// textSelections are the texts' selected parts (characters of their
// labels' text), by RichText.
var textSelections = map[*node][2]int{}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindSelectable {
			return false
		}
		h.makeSelectable(n)
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		delete(selectAreas, n)
		delete(textSelections, n)
	})
	drawHooks = append(drawHooks, drawTextSelection)
}

// drawTextSelection draws a text's selected part under its label: the
// boxes of its lines.
func drawTextSelection(h *Host, n *node, snap Ptr, bounds Rect) bool {
	sel, ok := textSelections[n]
	if !ok || sel[0] >= sel[1] || n.kind != ops.KindRichText || n.inner == 0 {
		return false
	}
	text := richOf(n).text
	l := LabelGetLayout(n.inner)
	if l == 0 {
		return false
	}
	var b Rect
	WidgetComputeBounds(n.inner, n.widget, &b)
	var ox, oy int32
	Txt.LabelGetLayoutOffsets(n.inner, &ox, &oy)
	c := withAlpha(h.theme.fg, 0.1) // libadwaita's selection: view_fg_color at 10%
	for p := n.parent; p != nil; p = p.parent {
		if p.kind == ops.KindSelectable {
			if WidgetHasFocus(p.widget) {
				c = withAlpha(h.theme.accentBg, 0.3) // focused: accent_bg_color at 30%
			}
			break
		}
	}
	LayoutRangeBoxes(l, int32(byteAt(text, sel[0])), int32(byteAt(text, sel[1])), func(r IntRect) {
		box := Rect{X: b.X + float32(ox+r.X), Y: b.Y + float32(oy+r.Y), W: float32(r.W), H: float32(r.H)}
		Call(FnSnapshotAppendColor, snap, Addr(&c), Addr(&box))
	})
	return false
}

// byteAt is the byte index of character c of s.
func byteAt(s string, c int) int {
	for i := range s {
		if c == 0 {
			return i
		}
		c--
	}
	return len(s)
}

func (h *Host) makeSelectable(n *node) {
	s := &selectArea{}
	selectAreas[n] = s
	w := n.widget
	WidgetAddCSSClass(w, "asuka-selectable")
	WidgetSetFocusable(w, true)
	WidgetSetFocusOnClick(w, false) // the drag grabs it when it selects

	s.drag = GestureDragNew()
	GestureSingleSetButton(s.drag, 0) // the primary (selecting) and secondary (the menu)
	EventControllerSetPropagationPhase(s.drag, PhaseCapture)
	Connect(s.drag, "drag-begin", func(x, y float64) {
		button := GestureSingleGetCurrentButton(s.drag)
		state := EventControllerGetCurrentEventState(s.drag)
		if eventFromTouch(s.drag) || !h.selectTakes(n, x, y, button) {
			GestureSetState(s.drag, EventSequenceDenied)
			return
		}
		GestureSetState(s.drag, EventSequenceClaimed)
		WidgetGrabFocus(w)
		if button == 3 {
			h.selectMenu(n, x, y)
			return
		}
		h.selectPress(n, x, y, h.countClick(s, x, y, Sel.EventControllerGetEventTime(s.drag)), state&ModShift != 0)
	})
	Connect(s.drag, "drag-update", func(dx, dy float64) {
		if s.taking {
			h.selectTo(n, h.selectHit(n, s.startX+dx, s.startY+dy))
		}
	})
	Connect(s.drag, "drag-end", func(dx, dy float64) {
		if s.taking {
			s.taking = false
			h.selectPrimary(n)
		}
	})
	WidgetAddController(w, s.drag)

	// A touchscreen: a long press selects the word.
	long := GestureLongPressNew()
	GestureSingleSetTouchOnly(long, true)
	Connect(long, "pressed", func(x, y float64) {
		if !h.selectTakes(n, x, y, 1) {
			return
		}
		WidgetGrabFocus(w)
		h.selectPress(n, x, y, 2, false)
		s.taking = false
		h.selectPrimary(n)
	})
	WidgetAddController(w, long)

	// The selection's color follows the focus.
	focus := EventControllerFocusNew()
	redraw := func() {
		for _, t := range h.selectTexts(n) {
			if _, ok := textSelections[t]; ok {
				Call(FnWidgetQueueDraw, t.widget)
			}
		}
	}
	Connect(focus, "enter", redraw)
	Connect(focus, "leave", redraw)
	WidgetAddController(w, focus)

	// Its actions: the menu's, and a screen reader's (AT-SPI lists a
	// widget's action groups: "selection.copy", "selection.select-all").
	group := SimpleActionGroupNew()
	s.copy = SimpleActionNew("copy", 0)
	Connect(s.copy, "activate", func(uintptr) { h.selectCopy(n) })
	SimpleActionSetEnabled(s.copy, false)
	ActionMapAddAction(group, s.copy)
	ObjectUnref(s.copy)
	all := SimpleActionNew("select-all", 0)
	Connect(all, "activate", func(uintptr) { h.selectAll(n) })
	ActionMapAddAction(group, all)
	ObjectUnref(all)
	WidgetInsertActionGroup(w, "selection", group) // the view holds it, and them
	ObjectUnref(group)

	keys := EventControllerKeyNew()
	Connect(keys, "key-pressed", func(keyval, keycode, state uint32) bool {
		return h.selectKey(n, keyval, state)
	})
	WidgetAddController(w, keys)
}

// eventFromTouch reports whether a gesture's event comes from a
// touchscreen.
func eventFromTouch(g Ptr) bool {
	dev := Call(Sym("gtk_gesture_get_device"), g)
	return dev != 0 && Call(Sym("gdk_device_get_source"), dev) == 3 // GDK_SOURCE_TOUCHSCREEN
}

// countClick counts a press as GTK does: within the double-click time
// and distance of the last, the next click of a run of three.
func (h *Host) countClick(s *selectArea, x, y float64, t uint32) int {
	st := SettingsGetDefault()
	wait, dist := 400, 5
	if st != 0 {
		wait = GetIntProperty(st, "gtk-double-click-time")
		dist = GetIntProperty(st, "gtk-double-click-distance")
	}
	if s.clicks > 0 && t-s.lastTime <= uint32(wait) && math.Abs(x-s.lastX) <= float64(dist) && math.Abs(y-s.lastY) <= float64(dist) {
		s.clicks = s.clicks%3 + 1
	} else {
		s.clicks = 1
	}
	s.lastTime, s.lastX, s.lastY = t, x, y
	return s.clicks
}

// selectTakes reports whether a press at x, y in a Selectable selects
// (button 1) or shows the selection's menu (3): not on a link, a control
// or a view that takes the press itself (a pressable view, one with a
// context menu or actions of its own).
func (h *Host) selectTakes(s *node, x, y float64, button uint32) bool {
	if button != 1 && button != 3 {
		return false
	}
	picked := WidgetPick(s.widget, x, y, 0)
	for w := picked; w != 0 && w != s.widget; w = WidgetGetParent(w) {
		n := h.byWidget[w]
		if n == nil {
			if t := h.labelNode(w); t != nil && button == 1 {
				var p Point
				WidgetComputePoint(s.widget, w, &Point{X: float32(x), Y: float32(y)}, &p)
				if richOf(t).linkAt(t, float64(p.X), float64(p.Y)) >= 0 {
					return false
				}
			}
			continue
		}
		if n.inner != 0 && n.kind != ops.KindRichText && n.kind != ops.KindText {
			return false // a control
		}
		if n.listening(ops.EventPress) || n.listening(ops.EventLongPress) || n.listening(ops.EventPan) {
			return false
		}
		if button == 3 && (len(n.menu) > 0 || a11yViews[n] != nil && len(a11yViews[n].actions) > 0) {
			return false
		}
	}
	return picked != 0
}

// labelNode is the RichText whose label w is, or nil.
func (h *Host) labelNode(w Ptr) *node {
	p := WidgetGetParent(w)
	if n := h.byWidget[p]; n != nil && n.inner == w && n.kind == ops.KindRichText {
		return n
	}
	return nil
}

// selectTexts are the RichTexts in a Selectable that show, in reading
// order.
func (h *Host) selectTexts(s *node) []*node {
	var out []*node
	var walk func(n *node)
	walk = func(n *node) {
		for _, c := range n.children {
			if c.kind == ops.KindRichText && c.inner != 0 && richOf(c) != nil && WidgetGetMapped(c.inner) {
				out = append(out, c)
			}
			walk(c)
		}
	}
	walk(s)
	return out
}

// selectHit is the place in a Selectable's texts at x, y in its view: in
// the text under it, or on the line nearest it across; above or between
// texts, the start of the next; below them all, the end of the last.
func (h *Host) selectHit(s *node, x, y float64) selPos {
	texts := h.selectTexts(s)
	if len(texts) == 0 {
		return selPos{}
	}
	best, bestDist := -1, math.Inf(1)
	for i, t := range texts {
		var b Rect
		if !WidgetComputeBounds(t.inner, s.widget, &b) {
			continue
		}
		top, bottom := float64(b.Y), float64(b.Y+b.H)
		if y < top || y >= bottom {
			continue
		}
		d := 0.0
		if x < float64(b.X) {
			d = float64(b.X) - x
		} else if x > float64(b.X+b.W) {
			d = x - float64(b.X+b.W)
		}
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	if best >= 0 {
		return h.textHit(s, texts[best], x, y)
	}
	for _, t := range texts {
		var b Rect
		if WidgetComputeBounds(t.inner, s.widget, &b) && float64(b.Y) > y {
			return selPos{t, 0}
		}
	}
	last := texts[len(texts)-1]
	return selPos{last, utf8.RuneCountInString(richOf(last).text)}
}

// textHit is the character offset in t's label nearest x, y (in the
// Selectable's view): Pango's, at the grapheme's leading or trailing
// edge.
func (h *Host) textHit(s, t *node, x, y float64) selPos {
	text := richOf(t).text
	l := LabelGetLayout(t.inner)
	if l == 0 {
		return selPos{t, 0}
	}
	var p Point
	WidgetComputePoint(s.widget, t.inner, &Point{X: float32(x), Y: float32(y)}, &p)
	var ox, oy int32
	Txt.LabelGetLayoutOffsets(t.inner, &ox, &oy)
	var index, trailing int32
	// Outside the layout, Pango clamps to its nearest place.
	Txt.LayoutXYToIndex(l, int32((float64(p.X)-float64(ox))*1024), int32((float64(p.Y)-float64(oy))*1024), &index, &trailing)
	i := min(max(int(index), 0), len(text))
	return selPos{t, utf8.RuneCountInString(text[:i]) + int(trailing)}
}

// order is a place's index in reading order (texts' index, offset).
func selectOrder(texts []*node, p selPos) (int, int) {
	for i, t := range texts {
		if t == p.t {
			return i, p.off
		}
	}
	return -1, 0
}

func selectLess(texts []*node, a, b selPos) bool {
	ai, ao := selectOrder(texts, a)
	bi, bo := selectOrder(texts, b)
	return ai < bi || ai == bi && ao < bo
}

// selectUnit is the unit (a character's place, a word, a paragraph) at
// p.
func (h *Host) selectUnit(p selPos, unit int) [2]selPos {
	if p.t == nil || unit == 0 {
		return [2]selPos{p, p}
	}
	runes := []rune(richOf(p.t).text)
	n := len(runes)
	if n == 0 {
		return [2]selPos{p, p}
	}
	c := min(p.off, n-1)
	if unit == 2 {
		s, e := c, c
		for s > 0 && runes[s-1] != '\n' {
			s--
		}
		for e < n && runes[e] != '\n' {
			e++
		}
		return [2]selPos{{p.t, s}, {p.t, e}}
	}
	l := LabelGetLayout(p.t.inner)
	if l == 0 {
		return [2]selPos{p, p}
	}
	s, e := wordAt(LogAttrs(l), n, c)
	return [2]selPos{{p.t, s}, {p.t, e}}
}

// wordAt is the word character c is in (Pango's word boundaries), else
// the run of white space, else the character itself.
func wordAt(attrs []uint32, n, c int) (int, int) {
	if len(attrs) < n+1 || c < 0 || c >= n {
		return c, c
	}
	s := c
	for {
		if attrs[s]&LogAttrWordStart != 0 {
			break
		}
		if attrs[s]&LogAttrWordEnd != 0 || s == 0 {
			s = -1
			break
		}
		s--
	}
	if s >= 0 {
		e := c + 1
		for e < n && attrs[e]&LogAttrWordEnd == 0 {
			e++
		}
		return s, e
	}
	if attrs[c]&LogAttrWhite != 0 {
		s, e := c, c+1
		for s > 0 && attrs[s-1]&LogAttrWhite != 0 {
			s--
		}
		for e < n && attrs[e]&LogAttrWhite != 0 {
			e++
		}
		return s, e
	}
	return c, c + 1
}

// selectPress starts a selection at x, y: clicks 1 at the character, 2
// the word, 3 the paragraph; extend (Shift) extends the one there is.
func (h *Host) selectPress(s *node, x, y float64, clicks int, extend bool) {
	a := selectAreas[s]
	if a == nil {
		return
	}
	p := h.selectHit(s, x, y)
	if p.t == nil {
		return
	}
	a.taking, a.startX, a.startY = true, x, y
	if extend && a.has && h.inTexts(s, a.anchor[0].t) && h.inTexts(s, a.anchor[1].t) {
		h.selectTo(s, p)
		return
	}
	a.unit = min(max(clicks, 1), 3) - 1
	a.anchor = h.selectUnit(p, a.unit)
	a.has = true
	h.selectRange(s, a.anchor[0], a.anchor[1])
}

func (h *Host) inTexts(s, t *node) bool {
	for p := t; p != nil; p = p.parent {
		if p == s {
			return t.inner != 0 && h.nodes[t.id] == t
		}
	}
	return false
}

// selectTo extends the selection from its anchor to p, by the unit it
// started with.
func (h *Host) selectTo(s *node, p selPos) {
	a := selectAreas[s]
	if a == nil || !a.has || p.t == nil {
		return
	}
	texts := h.selectTexts(s)
	u := h.selectUnit(p, a.unit)
	if selectLess(texts, u[0], a.anchor[0]) {
		h.selectRange(s, u[0], a.anchor[1])
	} else {
		h.selectRange(s, a.anchor[0], u[1])
	}
}

// selectRange selects from a to b: in each text between, its part.
func (h *Host) selectRange(s *node, a, b selPos) {
	texts := h.selectTexts(s)
	if area := selectAreas[s]; area != nil && area.copy != 0 {
		defer func() { SimpleActionSetEnabled(area.copy, h.selectedText(s) != "") }()
	}
	if selectLess(texts, b, a) {
		a, b = b, a
	}
	ai, ao := selectOrder(texts, a)
	bi, bo := selectOrder(texts, b)
	for i, t := range texts {
		n := utf8.RuneCountInString(richOf(t).text)
		st, en := 0, 0
		switch {
		case ai < 0 || bi < 0 || i < ai || i > bi:
		case i == ai && i == bi:
			st, en = ao, bo
		case i == ai:
			st, en = ao, n
		case i == bi:
			st, en = 0, bo
		default:
			st, en = 0, n
		}
		st, en = min(st, n), min(en, n)
		old, had := textSelections[t]
		if st == en {
			if had {
				delete(textSelections, t)
				Call(FnWidgetQueueDraw, t.widget)
			}
			continue
		}
		if !had || old != [2]int{st, en} {
			textSelections[t] = [2]int{st, en}
			Call(FnWidgetQueueDraw, t.widget)
		}
	}
}

// selectAll selects every text in the Selectable.
func (h *Host) selectAll(s *node) {
	a := selectAreas[s]
	texts := h.selectTexts(s)
	if a == nil || len(texts) == 0 {
		return
	}
	last := texts[len(texts)-1]
	a.anchor = [2]selPos{{texts[0], 0}, {texts[0], 0}}
	a.unit, a.has = 0, true
	h.selectRange(s, selPos{texts[0], 0}, selPos{last, utf8.RuneCountInString(richOf(last).text)})
	h.selectPrimary(s)
}

// selectedText is the selection's text: each text's part, a paragraph
// each, without its inline components.
func (h *Host) selectedText(s *node) string {
	var parts []string
	for _, t := range h.selectTexts(s) {
		sel, ok := textSelections[t]
		if !ok {
			continue
		}
		runes := []rune(richOf(t).text)
		st, en := min(sel[0], len(runes)), min(sel[1], len(runes))
		parts = append(parts, strings.ReplaceAll(string(runes[st:en]), "￼", ""))
	}
	return strings.Join(parts, "\n")
}

// selectCopy puts the selection's text on the clipboard.
func (h *Host) selectCopy(s *node) bool {
	text := h.selectedText(s)
	if text == "" {
		return false
	}
	ClipboardSetText(DisplayGetClipboard(WidgetGetDisplay(s.widget)), text)
	return true
}

// selectPrimary makes the selection X11's and Wayland's primary
// selection, as a label's is.
func (h *Host) selectPrimary(s *node) {
	if Sel.DisplayGetPrimaryClipboard == nil {
		return
	}
	if text := h.selectedText(s); text != "" {
		ClipboardSetText(Sel.DisplayGetPrimaryClipboard(WidgetGetDisplay(s.widget)), text)
	}
}

// selectKey: Ctrl+A selects all, Ctrl+C and Ctrl+Insert copy, the Menu
// key and Shift+F10 show the menu.
func (h *Host) selectKey(s *node, keyval, state uint32) bool {
	const keyA, keyC, keyInsert, keyMenu, keyF10 = 0x61, 0x63, 0xff63, 0xff67, 0xffc7
	ctrl := state&ModControl != 0 && state&(ModAlt|ModSuper) == 0
	switch {
	case ctrl && (keyval == keyA || keyval == keyA-0x20):
		h.selectAll(s)
		return true
	case ctrl && (keyval == keyC || keyval == keyC-0x20 || keyval == keyInsert):
		return h.selectCopy(s)
	case keyval == keyMenu || keyval == keyF10 && state&ModShift != 0:
		h.selectMenu(s, -1, -1)
		return true
	}
	return false
}

// selectMenu shows the selection's context menu (GTK's words for a
// label's): Copy, Select All; at x, y, or under the view (x < 0).
func (h *Host) selectMenu(s *node, x, y float64) {
	m := MenuNew()
	for _, it := range [][2]string{{Translated("_Copy"), "selection.copy"}, {Translated("Select _All"), "selection.select-all"}} {
		item := MenuItemNew(it[0], it[1])
		MenuAppendItem(m, item)
		ObjectUnref(item)
	}
	pop := PopoverMenuNewFromModel(m)
	ObjectUnref(m)
	WidgetSetParent(pop, s.widget)
	if x >= 0 {
		r := IntRect{X: int32(x), Y: int32(y), W: 1, H: 1}
		PopoverSetPointingTo(pop, &r)
		PopoverSetHasArrow(pop, false)
		if s.rtl {
			WidgetSetHAlign(pop, AlignEnd)
		} else {
			WidgetSetHAlign(pop, AlignStart)
		}
	} else {
		PopoverSetPosition(pop, 3) // GTK_POS_BOTTOM
	}
	Connect(pop, "closed", func() {
		IdleAdd(PriorityDefaultIdle, func() bool {
			WidgetUnparent(pop)
			return false
		})
	})
	h.popups = append(h.popups, pop)
	PopoverPopup(pop)
}
