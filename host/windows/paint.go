//go:build windows && amd64

package windows

import (
	"math"

	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

// Painting: every view of the host's own class draws itself into a
// buffered DC (no flicker), first what's behind it where it's not opaque
// (its parent, asked with WM_PRINTCLIENT through
// DrawThemeParentBackground: rounded corners, text over an image), then
// its background, border and content.

func (h *Host) paintWindow(n *node, hwnd uintptr) {
	var ps PaintStruct
	dc := BeginPaint(hwnd, &ps)
	r := ClientRect(hwnd)
	if r.W() > 0 && r.H() > 0 {
		pb, mem := BeginBufferedPaint(dc, &r)
		if mem != 0 {
			h.paint(n, hwnd, mem, r)
			EndBufferedPaint(pb)
		} else {
			h.paint(n, hwnd, dc, r)
		}
	}
	EndPaint(hwnd, &ps)
}

// opaque reports whether n's own drawing covers all of it.
func (h *Host) opaque(n *node) bool {
	switch n.kind {
	case ops.KindScreen, ops.KindSheet, kindHeader, kindTabBar:
		return n.cornerRadius == 0
	}
	c, ok := h.resolve(n.background)
	return ok && c.a == 255 && n.cornerRadius == 0 && n.opacity >= 1
}

// paint draws n into dc, whose client rectangle is r (pixels).
func (h *Host) paint(n *node, hwnd, dc uintptr, r Rect) {
	if !h.opaque(n) {
		h.paintBehind(hwnd, dc)
	}
	w, ht := float32(r.W()), float32(r.H())
	g := NewGraphics(dc)
	defer g.Close()
	k := h.scale
	radius := min(n.cornerRadius*k, w/2, ht/2)
	bg, hasBg := h.resolve(n.background)
	switch {
	case hasBg:
	case n.kind == ops.KindScreen || n.kind == ops.KindSheet:
		bg, hasBg = h.theme.bg, true
	case n.kind == kindHeader || n.kind == kindTabBar:
		bg, hasBg = h.theme.bg, true
	}
	if n.opacity < 1 {
		bg = bg.withAlpha(n.opacity)
	}
	if n.gradient != nil {
		h.paintGradient(g, n, w, ht, radius)
	} else if hasBg && bg.a > 0 {
		if radius > 0 {
			g.FillRoundRect(bg.argb(), 0, 0, w, ht, radius)
		} else {
			// GDI+, not GDI: GDI leaves the bitmap's alpha 0, and GDI+
			// blends what it draws next against that alpha.
			b := SolidBrush(bg.argb())
			g.FillRect(b, 0, 0, w, ht)
			b.Close()
		}
	}
	if n.pressable() && !n.disabled && (n.hot || n.pressed) {
		c := h.theme.subtleHot
		if n.pressed {
			c = h.theme.subtlePressed
			if !h.dark {
				c = rgba{0, 0, 0, 0x0C}
			}
		}
		if radius > 0 {
			g.FillRoundRect(c.argb(), 0, 0, w, ht, radius)
		} else {
			b := SolidBrush(c.argb())
			g.FillRect(b, 0, 0, w, ht)
			b.Close()
		}
	}
	if n.borderWidth > 0 {
		bc, ok := h.resolve(n.borderColor)
		if !ok {
			bc = h.theme.label
		}
		g.StrokeRoundRect(bc.argb(), n.borderWidth*k, 0, 0, w, ht, radius)
	}
	switch n.kind {
	case ops.KindText:
		h.paintText(n, dc, r)
	case ops.KindImage:
		h.paintImage(n, g, w, ht, radius)
	case ops.KindSymbol:
		h.paintSymbol(n, dc, r)
	case ops.KindShape:
		n.shape.paint(h, n, g)
	case ops.KindSegmented:
		h.paintSegmented(n, dc, g, r)
	case ops.KindProgress:
		h.paintProgress(n, g, w, ht)
	case ops.KindActivity:
		h.paintActivity(n, g, w, ht)
	case ops.KindTextField:
		h.paintField(n, g, w, ht)
	case kindHeader:
		h.paintHeader(n, dc, g, r)
	case kindTabBar:
		h.paintTabBar(n, dc, g, r)
	case kindRow:
		if n.row != nil {
			n.row.paint(h, g, w, ht)
		}
	}
	if n.focused && n.pressable() && h.keyboardCues(hwnd) {
		h.focusRing(g, w, ht, radius)
	}
}

// pressable views highlight under the mouse and take clicks.
func (n *node) pressable() bool {
	return n.listening(ops.EventPress) || n.listening(ops.EventLongPress) || len(n.menu) > 0 && n.kind == ops.KindView
}

// interactive views take the mouse; the others let it through to what's
// under them (a text in a pressable row).
func (n *node) interactive() bool {
	if n.pressable() || n.listening(ops.EventPan) || n.listening(ops.EventPinch) || n.listening(ops.EventSwipe) {
		return true
	}
	switch n.kind {
	case ops.KindScroll, ops.KindList, ops.KindSegmented, ops.KindTextField, kindHeader, kindTabBar,
		ops.KindButton, ops.KindToggle, ops.KindSlider, ops.KindStepper, ops.KindWheel, ops.KindDatePicker,
		ops.KindColorPicker, ops.KindRichText, ops.KindWebView, ops.KindSelectable:
		return true
	}
	return false
}

func (h *Host) keyboardCues(hwnd uintptr) bool {
	const wmQueryUIState = 0x0129
	return SendMessage(hwnd, wmQueryUIState, 0, 0)&UISF_HIDEFOCUS == 0
}

// focusRing is Fluent's focus visual: a 2-pixel outer stroke in the text
// color.
func (h *Host) focusRing(g Graphics, w, ht, radius float32) {
	k := h.scale
	g.StrokeRoundRect(h.theme.label.argb(), 2*k, 0, 0, w, ht, max(radius, 4*k))
}

// redraw repaints n and its children (they draw n behind them).
func (h *Host) redraw(n *node) {
	if n.hwnd != 0 {
		RedrawWindow(n.hwnd, RDW_INVALIDATE|RDW_ALLCHILDREN)
	}
}

// paintGradient fills the view with its linear gradient.
func (h *Host) paintGradient(g Graphics, n *node, w, ht, radius float32) {
	b := n.gradient
	if len(b) < 4 {
		return
	}
	count := int(le.Uint32(b))
	if count == 0 || len(b) < 4+count*12+16 {
		return
	}
	colors := make([]ARGB, count)
	pos := make([]float32, count)
	for i := range count {
		o := 4 + i*12
		c, _ := h.resolve(ops.Color(le.Uint64(b[o:])))
		colors[i] = c.argb()
		pos[i] = min(max(f32(b, o+8), 0), 1)
	}
	// GDI+ wants the stops to start at 0 and end at 1.
	if pos[0] > 0 {
		colors = append([]ARGB{colors[0]}, colors...)
		pos = append([]float32{0}, pos...)
	}
	if pos[len(pos)-1] < 1 {
		colors = append(colors, colors[len(colors)-1])
		pos = append(pos, 1)
	}
	o := 4 + count*12
	x0, y0, x1, y1 := f32(b, o)*w, f32(b, o+4)*ht, f32(b, o+8)*w, f32(b, o+12)*ht
	br := LinearBrush(x0, y0, x1, y1, colors, pos)
	defer br.Close()
	p := NewPath()
	defer p.Close()
	p.RoundRect(0, 0, w, ht, radius)
	g.FillPath(br, p)
}

// textColor is n's text color over what's behind it (GDI text is
// opaque).
func (h *Host) textColor(n *node, def rgba) rgba {
	c, ok := h.resolve(n.color)
	if !ok {
		c = def
	}
	if n.disabled {
		c = h.theme.disabled
	}
	if n.opacity < 1 {
		c = c.withAlpha(n.opacity)
	}
	return c.over(h.backdrop(n))
}

// textFormat is DrawText's flags for n's alignment and line limit.
func textFormat(n *node) uint32 {
	f := uint32(DT_NOPREFIX | DT_EDITCONTROL)
	switch ops.TextAlign(n.textAlign) {
	case ops.TextAlignCenter:
		f |= DT_CENTER
	case ops.TextAlignEnd:
		f |= DT_RIGHT
	}
	if n.lineLimit == 1 {
		return f | DT_SINGLELINE | DT_END_ELLIPSIS
	}
	f |= DT_WORDBREAK
	if n.lineLimit > 1 {
		f |= DT_END_ELLIPSIS
	}
	return f
}

func (h *Host) paintText(n *node, dc uintptr, r Rect) {
	if len(n.utext) == 0 {
		return
	}
	old := SelectObject(dc, h.nodeFont(n))
	SetBkMode(dc, TRANSPARENT)
	SetTextColor(dc, h.textColor(n, h.theme.label).ref())
	if n.lineLimit > 1 {
		tm := TextMetrics(dc)
		r.Bottom = min(r.Bottom, r.Top+tm.Height*int32(n.lineLimit))
	}
	DrawText(dc, n.utext, &r, textFormat(n))
	SelectObject(dc, old)
}

// paintSymbol draws a symbol's glyph of Segoe Fluent Icons, centered.
func (h *Host) paintSymbol(n *node, dc uintptr, r Rect) {
	glyph := symbolGlyph(n.symbol)
	if glyph == 0 {
		return
	}
	s := h.nodeSpec(n)
	old := SelectObject(dc, h.iconFont(s.size))
	SetBkMode(dc, TRANSPARENT)
	SetTextColor(dc, h.textColor(n, h.theme.label).ref())
	u := []uint16{glyph}
	DrawText(dc, u, &r, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	SelectObject(dc, old)
}

// paintProgress is Fluent's progress bar: a hairline track and a 3-point
// fill in the accent color (Color sets it); indeterminate, a segment
// that runs along it.
func (h *Host) paintProgress(n *node, g Graphics, w, ht float32) {
	k := h.scale
	fill, ok := h.resolve(n.color)
	if !ok {
		fill = h.theme.accent
	}
	th := 3 * k
	y := (ht - th) / 2
	track := NewPen(h.theme.track.argb(), max(1*k, 1))
	g.DrawLine(track, 0, ht/2, w, ht/2)
	track.Close()
	if n.indeterminate {
		// A segment 40% of the track, moving across and back in.
		t := float32(math.Mod(n.phase, 1))
		seg := w * 0.4
		x := -seg + t*(w+seg)
		x0, x1 := max(x, 0), min(x+seg, w)
		if x1 > x0 {
			g.FillRoundRect(fill.argb(), x0, y, x1-x0, th, th/2)
		}
		return
	}
	v := float32(min(max(n.value, 0), 1))
	if v > 0 {
		g.FillRoundRect(fill.argb(), 0, y, max(w*v, th), th, th/2)
	}
}

// paintActivity is Fluent's progress ring: an arc that turns and grows.
func (h *Host) paintActivity(n *node, g Graphics, w, ht float32) {
	if !n.animating {
		return
	}
	k := h.scale
	c, ok := h.resolve(n.color)
	if !ok {
		c = h.theme.accent
	}
	d := min(w, ht)
	stroke := max(d/8, 2*k)
	pen := NewPen(c.argb(), stroke)
	pen.SetCaps(2, 2)
	t := n.phase
	start := float32(math.Mod(t*360*1.5, 360))
	sweep := float32(60 + 200*(0.5-0.5*math.Cos(t*math.Pi)))
	x, y := (w-d)/2+stroke/2, (ht-d)/2+stroke/2
	g.DrawArc(pen, x, y, d-stroke, d-stroke, start, sweep)
	pen.Close()
}

func f32(b []byte, i int) float32 { return math.Float32frombits(le.Uint32(b[i:])) }

// paintBehind draws what's behind a window into its DC: its parent,
// drawn with the DC's origin moved to the parent's (which draws what's
// behind itself in turn, where it isn't opaque). uxtheme's
// DrawThemeParentBackground does this through WM_PRINTCLIENT, but
// ignores calls nested in another, which a tree of transparent views
// makes.
func (h *Host) paintBehind(hwnd, dc uintptr) {
	parent := Parent(hwnd)
	if parent == 0 {
		return
	}
	at := WindowRectIn(hwnd, parent)
	saved := SaveDC(dc)
	OffsetViewport(dc, -at.Left, -at.Top)
	pr := ClientRect(parent)
	if pn := h.byHwnd[parent]; pn != nil && pn.hwnd == parent && pn.kind != kindSheetWindow {
		h.paint(pn, parent, dc, pr)
	} else if pn != nil && pn.ctl == parent {
		DrawParentBackground(hwnd, dc, nil)
	} else {
		g := NewGraphics(dc)
		b := SolidBrush(h.theme.bg.argb())
		g.FillRect(b, 0, 0, float32(pr.W()), float32(pr.H()))
		b.Close()
		g.Close()
	}
	RestoreDC(dc, saved)
}
