//go:build linux && !android

package linux

import (
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/ops"
)

// A RichText is a GtkLabel whose text is styled with Pango attributes:
// what draws it (the label's layout) is what measures it (a layout of the
// same text and attributes, at the same width), and it's accessible as
// a label is. Links are the host's own: colored
// and underlined as GTK colors a label's, hit-tested in its layout (a
// pointer over them, a click on one reports Link, else opens it). Inline
// components are shape attributes of their size, on the baseline: their
// views are placed where the layout puts them.
//
// In a Selectable, the Selectable selects across its texts and draws
// each one's part under it (selectable.go).
type richText struct {
	content    []byte
	inlines    []inlineSize
	dirty      bool
	queued     bool
	text       string     // the label's text: U+FFFC for an inline component
	links      []linkSpan // byte ranges of the text
	inlineAt   []int32    // each inline component's byte index
	selectable bool
	pressed    int // the link a click went down on (-1: none)
	overLink   bool
}

type inlineSize struct {
	id   ops.NodeID
	w, h float32
}

type linkSpan struct {
	start, end int32
	url        string
}

func richOf(n *node) *richText {
	r, _ := n.ext.(*richText)
	return r
}

func init() {
	createHooks = append(createHooks, createRich)
	propHooks = append(propHooks, setRichProp)
	textHooks = append(textHooks, func(h *Host, n *node) {
		if r := richOf(n); r != nil && n.kind == ops.KindRichText {
			r.invalidate(h, n)
		}
	})
	measureHooks = append(measureHooks, measureRich)
}

func createRich(h *Host, n *node) bool {
	if n.kind != ops.KindRichText {
		return false
	}
	l := LabelNew("")
	LabelSetWrap(l, true)
	LabelSetWrapMode(l, WrapWordChar)
	LabelSetXAlign(l, 0)
	LabelSetYAlign(l, 0)
	h.setInner(n, l)
	r := &richText{dirty: true, pressed: -1}
	n.ext = r

	// A click on a link is the link's: claimed, so what's under the text
	// doesn't take it too. Elsewhere the click goes on (a Selectable's
	// selection, a pressable view around it).
	g := GestureClickNew()
	EventControllerSetPropagationPhase(g, PhaseCapture)
	Connect(g, "pressed", func(_ int, x, y float64) {
		r.pressed = r.linkAt(n, x, y)
		if r.pressed >= 0 {
			GestureSetState(g, EventSequenceClaimed)
		} else {
			GestureSetState(g, EventSequenceDenied)
		}
	})
	Connect(g, "released", func(_ int, x, y float64) {
		k := r.pressed
		r.pressed = -1
		if k >= 0 && k < len(r.links) && r.linkAt(n, x, y) == k {
			h.openLink(n, r.links[k].url)
		}
	})
	WidgetAddController(l, g)
	m := EventControllerMotionNew()
	Connect(m, "motion", func(x, y float64) { r.hover(n, r.linkAt(n, x, y) >= 0) })
	Connect(m, "leave", func() { r.hover(n, false) })
	WidgetAddController(l, m)
	return true
}

// hover shows the pointer over a link.
func (r *richText) hover(n *node, over bool) {
	if over == r.overLink || n.inner == 0 {
		return
	}
	r.overLink = over
	if over {
		WidgetSetCursorFromName(n.inner, "pointer")
	} else {
		Txt.WidgetSetCursor(n.inner, 0)
	}
}

// openLink reports a link the user clicked, or opens it if the app
// doesn't listen.
func (h *Host) openLink(n *node, url string) {
	if n.listening(ops.EventLink) {
		h.emit(n, ops.Event{Kind: ops.EventLink, Str: url})
		return
	}
	h.openURL(url)
}

func setRichProp(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
	r := richOf(n)
	if r == nil || n.kind != ops.KindRichText {
		return false
	}
	switch p {
	case ops.PropRichTextContent:
		r.content = append(r.content[:0], v.Bytes...)
	case ops.PropRichTextInlines:
		r.inlines = r.inlines[:0]
		rd := reader{b: v.Bytes}
		count := int(rd.u32())
		for i := 0; i < count && rd.ok(12); i++ {
			r.inlines = append(r.inlines, inlineSize{id: ops.NodeID(rd.u32()), w: rd.f32(), h: rd.f32()})
		}
	default:
		return false
	}
	r.invalidate(h, n)
	return true
}

// invalidate builds the label again: when the core measures it, or once
// the core has returned (a change of color alone isn't measured).
func (r *richText) invalidate(h *Host, n *node) {
	r.dirty = true
	if r.queued {
		return
	}
	r.queued = true
	h.afterCore(func() {
		r.queued = false
		if n.widget != 0 && h.nodes[n.id] == n {
			r.build(h, n)
		}
	})
}

// build sets the label's text and attributes from the content, styled by
// the view's text props (each run's defaults).
func (r *richText) build(h *Host, n *node) {
	if !r.dirty || n.inner == 0 {
		return
	}
	r.dirty = false
	runs := ops.DecodeRich(r.content)
	var b strings.Builder
	list := h.attrs(n, true)
	r.links, r.inlineAt = r.links[:0], r.inlineAt[:0]
	add := func(a Ptr, start, end int) {
		setAttrRange(a, uint32(start), uint32(end))
		AttrListInsert(list, a)
	}
	inline := 0
	for _, run := range runs {
		s := run.Style
		start := b.Len()
		if s.Flags&16 != 0 {
			// An inline component: a shape of its size on the baseline.
			b.WriteString("￼")
			r.inlineAt = append(r.inlineAt, int32(start))
			var in inlineSize
			if inline < len(r.inlines) {
				in = r.inlines[inline]
			}
			inline++
			box := PangoRectangle{Y: -int32(in.h*1024 + 0.5), W: int32(in.w*1024 + 0.5), H: int32(in.h*1024 + 0.5)}
			add(AttrShapeNew(&box, &box), start, b.Len())
			continue
		}
		b.WriteString(run.Text)
		end := b.Len()
		if run.Text == "\n" || end == start {
			continue
		}
		h.runAttrs(n, s, func(a Ptr) { add(a, start, end) })
		if s.Link != "" {
			r.links = append(r.links, linkSpan{int32(start), int32(end), s.Link})
		}
	}
	r.text = b.String()
	delete(textSelections, n) // as a label's goes with its text
	n.text = strings.ReplaceAll(r.text, "￼", "")
	l := n.inner
	LabelSetText(l, r.text)
	LabelSetAttributes(l, list)
	AttrListUnref(list)
	LabelSetXAlign(l, h.xalign(n))
	j := int32(JustifyLeft)
	switch h.physicalAlign(n) {
	case 1:
		j = JustifyCenter
	case 2:
		j = JustifyRight
	case 3:
		j = JustifyFill
	}
	LabelSetJustify(l, j)
	if n.lineLimit > 0 {
		LabelSetLines(l, int32(n.lineLimit))
		LabelSetEllipsize(l, EllipsizeEnd)
	} else {
		LabelSetLines(l, -1)
		LabelSetEllipsize(l, EllipsizeNone)
	}
	r.updateSelectable(n)
	if n.widget != 0 {
		Call(FnQueueAllocate, n.widget) // the inline components move with the text
	}
}

// runAttrs adds a run's style as Pango attributes: its font where it sets
// one (the view's otherwise), italic, monospaced, underline (a link's
// too), strikethrough, its color (a link's the accent) and background.
func (h *Host) runAttrs(n *node, s ops.RichStyle, add func(Ptr)) {
	if s.TextStyle != 0 || s.Size != 0 || s.Weight != 0 {
		x := node{textStyle: n.textStyle, fontSize: n.fontSize, fontWeight: n.fontWeight}
		if s.TextStyle != 0 {
			x.textStyle = int32(s.TextStyle)
		}
		if s.Size != 0 {
			x.fontSize = s.Size
		}
		if s.Weight != 0 {
			x.fontWeight = int32(s.Weight)
		}
		size, weight := h.font(&x)
		add(AttrSizeNewAbsolute(int32(size*1024 + 0.5)))
		add(AttrWeightNew(weight))
	}
	if s.Flags&1 != 0 {
		add(AttrStyleNew(2)) // PANGO_STYLE_ITALIC
	}
	if s.Flags&2 != 0 {
		add(AttrFamilyNew("monospace"))
	}
	if s.Flags&4 != 0 || s.Link != "" {
		add(AttrUnderlineNew(1)) // single
	}
	if s.Flags&8 != 0 {
		add(AttrStrikethroughNew(true))
	}
	c, ok := h.resolve(s.Color)
	if !ok && s.Link != "" {
		c, ok = h.theme.accent, true
	}
	if ok {
		add(AttrForegroundNew(u16(c.R), u16(c.G), u16(c.B)))
		if c.A < 1 {
			add(AttrForegroundAlphaNew(u16(c.A)))
		}
	}
	if c, ok := h.resolve(s.Background); ok && c.A > 0 {
		add(AttrBackgroundNew(u16(c.R), u16(c.G), u16(c.B)))
		if c.A < 1 {
			add(AttrBackgroundAlphaNew(u16(c.A)))
		}
	}
}

// updateSelectable notes whether the text is in a Selectable, which
// selects in it (selectable.go): its label isn't selectable itself (GTK
// keeps one label's selection at a time).
func (r *richText) updateSelectable(n *node) {
	sel := false
	for p := n.parent; p != nil; p = p.parent {
		if p.kind == ops.KindSelectable {
			sel = true
			break
		}
	}
	r.selectable = sel
}

// layoutAt is a layout of the label's text and attributes at a width
// (big: unbounded), to its line limit: what the label draws at that
// width. The caller unrefs it.
func (r *richText) layoutAt(n *node, w float32, wrap int32) Ptr {
	pl := WidgetCreatePangoLayout(n.inner, r.text)
	if a := Txt.LabelGetAttributes(n.inner); a != 0 {
		LayoutSetAttributes(pl, a)
	}
	LayoutSetWrap(pl, wrap)
	if n.lineLimit > 0 {
		LayoutSetHeight(pl, -int32(n.lineLimit))
		LayoutSetEllipsize(pl, EllipsizeEnd)
	}
	if w < big {
		LayoutSetWidth(pl, int32(w*1024))
	} else {
		LayoutSetWidth(pl, -1)
	}
	return pl
}

// measureRich: as wide as its widest line at the width it may take, as
// tall as its lines (to its line limit); min-content is its widest word.
func measureRich(h *Host, n *node, c layout.Constraints) (layout.Size, bool) {
	r := richOf(n)
	if r == nil || n.kind != ops.KindRichText {
		return layout.Size{}, false
	}
	r.build(h, n)
	r.updateSelectable(n)
	if r.text == "" || n.inner == 0 {
		return layout.Size{}, true
	}
	var w float32
	if c.Width != c.Width && c.AvailW <= -layout.Inf {
		pl := r.layoutAt(n, 0, WrapWord) // a word a line: the widest word
		LayoutSetWidth(pl, 1)
		var pw, ph int32
		LayoutGetPixelSize(pl, &pw, &ph)
		ObjectUnref(pl)
		w = float32(pw)
	} else {
		w = fit(c.Width, c.AvailW, 0)
	}
	inner := w
	if inner < big {
		inner = max(inner-n.inset[1]-n.inset[3], 0)
	}
	pl := r.layoutAt(n, inner, WrapWordChar)
	var pw, ph int32
	LayoutGetPixelSize(pl, &pw, &ph)
	ObjectUnref(pl)
	return layout.Size{W: min(float32(pw)+n.inset[1]+n.inset[3], w), H: float32(ph) + n.inset[0] + n.inset[2]}, true
}

// linkAt is the link under a point of the label, or -1.
func (r *richText) linkAt(n *node, x, y float64) int {
	if len(r.links) == 0 || n.inner == 0 {
		return -1
	}
	l := LabelGetLayout(n.inner)
	if l == 0 {
		return -1
	}
	var ox, oy int32
	Txt.LabelGetLayoutOffsets(n.inner, &ox, &oy)
	var index, trailing int32
	if !Txt.LayoutXYToIndex(l, int32((x-float64(ox))*1024), int32((y-float64(oy))*1024), &index, &trailing) {
		return -1
	}
	for k, s := range r.links {
		if index >= s.start && index < s.end {
			return k
		}
	}
	return -1
}

// placeInline puts an inline component (a child of a RichText, its frame
// at 0, 0) where the label's layout puts its shape: its bottom on the
// line's baseline. The label is allocated first (it's the view's first
// child).
func (h *Host) placeInline(p, c *node, f *[4]float32) {
	r := richOf(p)
	if r == nil || p.inner == 0 {
		return
	}
	k := -1
	for i, in := range r.inlines {
		if in.id == c.id {
			k = i
			break
		}
	}
	if k < 0 || k >= len(r.inlineAt) {
		return
	}
	l := LabelGetLayout(p.inner)
	if l == 0 {
		return
	}
	var ox, oy int32
	Txt.LabelGetLayoutOffsets(p.inner, &ox, &oy)
	it := Txt.LayoutGetIter(l)
	for Txt.LayoutIterGetIndex(it) < r.inlineAt[k] && Txt.LayoutIterNextChar(it) {
	}
	var ext PangoRectangle
	Txt.LayoutIterGetCharExtents(it, &ext)
	base := Txt.LayoutIterGetBaseline(it)
	Txt.LayoutIterFree(it)
	x := min(ext.X, ext.X+ext.W)
	f[0] = p.inset[3] + float32(ox) + float32(x)/1024
	f[1] = p.inset[0] + float32(oy) + float32(base)/1024 - r.inlines[k].h
}

// setAttrRange sets a Pango attribute's byte range (PangoAttribute's
// start_index and end_index, after its class pointer).
func setAttrRange(attr Ptr, start, end uint32) {
	*At[uint32](attr + 8) = start
	*At[uint32](attr + 12) = end
}
