//go:build windows && amd64

package windows

import (
	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

// The controls are the common controls, drawn as Windows 11 draws its
// own (WinUI's Fluent styles) through custom draw, so they look current
// in light and dark mode while keeping the controls' keyboard, focus and
// accessibility: BUTTON for buttons (Filled is the accent button), a
// BUTTON check box drawn as a toggle switch (Win32 has no switch), a
// trackbar for sliders. Fields are EDITs in a frame the host draws.

// buttonGlyph is the icon a host-made button shows.
func (h *Host) buttonGlyph(n *node, ctl uintptr) (glyph uint16, title string) {
	switch n.kind {
	case ops.KindStepper:
		if ctl == n.ctl {
			return 0xE949, "" // Remove
		}
		return 0xE948, "" // Add
	case kindHeader:
		return n.nav.headerButton(ctl)
	case ops.KindButton:
		return symbolGlyph(n.icon), n.title
	}
	return 0, ""
}

func (h *Host) drawButton(n *node, cd *NMCustomDraw) uintptr {
	if cd.DrawStage != CDDS_PREPAINT {
		return CDRF_DODEFAULT
	}
	dc, r := cd.Hdc, cd.Rc
	ctl := cd.Hdr.HwndFrom
	h.paintBehind(ctl, dc)
	g := NewGraphics(dc)
	k := h.scale
	w, ht := float32(r.W()), float32(r.H())
	hot := cd.ItemState&CDIS_HOT != 0
	pressed := cd.ItemState&CDIS_SELECTED != 0
	disabled := cd.ItemState&CDIS_DISABLED != 0 || n.disabled
	focus := cd.ItemState&CDIS_FOCUS != 0 && cd.ItemState&CDIS_SHOWKEYBOARDCUES != 0
	p := &h.theme
	radius := 4 * k
	if n.cornerRadius > 0 && n.kind == ops.KindButton {
		radius = min(n.cornerRadius*k, w/2, ht/2)
	}

	if n.kind == ops.KindColorPicker {
		c, ok := h.resolve(n.colorValue)
		if !ok {
			c = p.accent
		}
		fill := p.control
		if hot {
			fill = p.controlHot
		}
		g.FillRoundRect(fill.argb(), 0, 0, w, ht, radius)
		g.StrokeRoundRect(p.controlStroke.argb(), max(k, 1), 0, 0, w, ht, radius)
		in := 4 * k
		g.FillRoundRect(c.argb(), in, in, w-2*in, ht-2*in, radius/2)
		g.Close()
		return CDRF_SKIPDEFAULT
	}

	variant := ops.ButtonVariantBordered
	if n.kind == ops.KindButton {
		variant = ops.ButtonVariant(n.variant)
	}
	if n.kind == kindHeader {
		variant = ops.ButtonVariantPlain
	}
	text := p.label
	var fill rgba
	stroke := false
	switch variant {
	case ops.ButtonVariantFilled:
		fill, text = p.accent, p.onAccent
		if hot {
			fill = p.accentHot.over(h.backdrop(n))
		}
		if pressed {
			fill = p.accentPressed.over(h.backdrop(n))
			text = text.withAlpha(0.8).over(fill)
		}
		if disabled {
			fill, text = p.disabled.withAlpha(0.6).over(h.backdrop(n)), p.bg
		}
	case ops.ButtonVariantPlain:
		text = p.accentText
		if n.kind == kindHeader {
			text = p.label
		}
		if hot {
			fill = p.subtleHot
		}
		if pressed {
			fill = p.subtlePressed
			text = text.withAlpha(0.8).over(h.backdrop(n))
		}
	case ops.ButtonVariantTinted:
		fill, text = p.accent.withAlpha(0.15), p.accentText
		if hot {
			fill = p.accent.withAlpha(0.2)
		}
		if pressed {
			fill = p.accent.withAlpha(0.1)
		}
	default: // Gray, Bordered: Fluent's standard button
		fill, stroke = p.control, true
		if hot {
			fill = p.controlHot
		}
		if pressed {
			fill = p.controlPressed
			text = p.secondary
		}
	}
	if n.kind == ops.KindButton {
		if c, ok := h.resolve(n.background); ok {
			fill, stroke = c, false
		}
		if c, ok := h.resolve(n.color); ok {
			text = c
		}
	}
	if disabled && variant != ops.ButtonVariantFilled {
		text = p.disabled
	}
	if fill.a > 0 {
		g.FillRoundRect(fill.argb(), 0, 0, w, ht, radius)
	}
	if stroke {
		g.StrokeRoundRect(p.controlStroke.argb(), max(k, 1), 0, 0, w, ht, radius)
	}
	if focus {
		h.focusRing(g, w, ht, radius)
	}
	g.Close() // before GDI draws the title

	glyph, title := h.buttonGlyph(n, ctl)
	under := fill
	if under.a < 255 {
		under = fill.over(h.backdrop(n))
	}
	color := text.over(under).ref()
	SetBkMode(dc, TRANSPARENT)
	SetTextColor(dc, color)
	font := h.bodyFont()
	if n.kind == ops.KindButton {
		font = h.nodeFont(n)
	}
	ut := UTF16Units(title)
	old := SelectObject(dc, font)
	tw, th := TextExtent(dc, ut)
	gw := int32(0)
	var iconFont uintptr
	if glyph != 0 {
		iconFont = h.iconFont(16 * h.fontScale())
		SelectObject(dc, iconFont)
		gw, _ = TextExtent(dc, []uint16{glyph})
	}
	gap := int32(0)
	if gw > 0 && tw > 0 {
		gap = h.px(8)
	}
	x := r.Left + (r.W()-(gw+gap+tw))/2
	if glyph != 0 {
		gr := Rect{Left: x, Top: r.Top, Right: x + gw, Bottom: r.Bottom}
		DrawText(dc, []uint16{glyph}, &gr, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
		SelectObject(dc, font)
		x += gw + gap
	}
	if tw > 0 {
		y := r.Top + (r.H()-th)/2
		tr := Rect{Left: x, Top: y, Right: min(x+tw, r.Right-h.px(4)), Bottom: y + th}
		if x < r.Left+h.px(4) {
			tr.Left = r.Left + h.px(4)
		}
		DrawText(dc, ut, &tr, DT_SINGLELINE|DT_NOPREFIX|DT_END_ELLIPSIS)
	}
	SelectObject(dc, old)
	return CDRF_SKIPDEFAULT
}

func (h *Host) fontScale() float32 {
	if h.env.FontScale > 0 {
		return h.env.FontScale
	}
	return 1
}

// drawToggle draws a check box as Fluent's toggle switch: a 40 by 20
// pill, outlined and empty off, filled with the accent on, its knob
// sliding to the end.
func (h *Host) drawToggle(n *node, cd *NMCustomDraw) uintptr {
	if cd.DrawStage != CDDS_PREPAINT {
		return CDRF_DODEFAULT
	}
	dc, r := cd.Hdc, cd.Rc
	h.paintBehind(cd.Hdr.HwndFrom, dc)
	g := NewGraphics(dc)
	defer g.Close()
	defer freeTmpBrushes()
	k := h.scale
	p := &h.theme
	hot := cd.ItemState&CDIS_HOT != 0
	pressed := cd.ItemState&CDIS_SELECTED != 0
	disabled := cd.ItemState&CDIS_DISABLED != 0 || n.disabled
	on := SendMessage(cd.Hdr.HwndFrom, BM_GETCHECK, 0, 0) == 1
	tw, th := 40*k, 20*k
	x, y := float32(0), (float32(r.H())-th)/2
	strong := p.secondary
	if disabled {
		strong = p.disabled
	}
	accent := p.accent
	if c, ok := h.resolve(n.color); ok {
		accent = c
	}
	knob := 12 * k
	if hot {
		knob = 14 * k
	}
	if on {
		fill := accent
		if hot {
			fill = p.accentHot.over(h.backdrop(n))
		}
		if pressed {
			fill = p.accentPressed.over(h.backdrop(n))
		}
		if disabled {
			fill = p.disabled
		}
		g.FillRoundRect(fill.argb(), x, y, tw, th, th/2)
		kc := p.onAccent
		g.FillEllipse(SolidBrushTmp(kc.argb()), x+tw-th/2-knob/2, y+th/2-knob/2, knob, knob)
	} else {
		if hot {
			g.FillRoundRect(p.subtleHot.argb(), x, y, tw, th, th/2)
		}
		g.StrokeRoundRect(strong.argb(), max(k, 1), x, y, tw, th, th/2)
		g.FillEllipse(SolidBrushTmp(strong.argb()), x+th/2-knob/2, y+th/2-knob/2, knob, knob)
	}
	if cd.ItemState&CDIS_FOCUS != 0 && cd.ItemState&CDIS_SHOWKEYBOARDCUES != 0 {
		g.StrokeRoundRect(p.label.argb(), 2*k, x-3*k, y-3*k, tw+6*k, th+6*k, th/2+3*k)
	}
	return CDRF_SKIPDEFAULT
}

// drawSlider draws a trackbar as Fluent's slider: a 4-point track filled
// with the accent up to a round thumb, its accent dot growing under the
// mouse.
func (h *Host) drawSlider(n *node, cd *NMCustomDraw) uintptr {
	switch cd.DrawStage {
	case CDDS_PREPAINT:
		return CDRF_NOTIFYITEMDRAW
	case CDDS_ITEMPREPAINT:
	default:
		return CDRF_DODEFAULT
	}
	ctl := cd.Hdr.HwndFrom
	dc := cd.Hdc
	k := h.scale
	p := &h.theme
	var thumb Rect
	SendMessage(ctl, TBM_GETTHUMBRECT, 0, Ptr(&thumb))
	cx := float32(thumb.Left+thumb.Right) / 2
	client := ClientRect(ctl)
	cy := float32(client.H()) / 2
	accent := p.accent
	if c, ok := h.resolve(n.color); ok {
		accent = c
	}
	if n.disabled {
		accent = p.disabled
	}
	switch cd.ItemSpec {
	case TBCD_CHANNEL:
		h.paintBehind(ctl, dc)
		g := NewGraphics(dc)
		th := 4 * k
		r := 10 * k
		x0, x1 := r, float32(client.W())-r
		g.FillRoundRect(p.track.argb(), x0, cy-th/2, x1-x0, th, th/2)
		if cx > x0 {
			g.FillRoundRect(accent.argb(), x0, cy-th/2, cx-x0, th, th/2)
		}
		g.Close()
	case TBCD_THUMB:
		g := NewGraphics(dc)
		d := 20 * k
		g.FillEllipse(SolidBrushTmp(p.knob.argb()), cx-d/2, cy-d/2, d, d)
		pen := NewPen(p.controlStroke.argb(), max(k, 1))
		g.DrawArc(pen, cx-d/2+k/2, cy-d/2+k/2, d-k, d-k, 0, 360)
		pen.Close()
		inner := 10 * k
		hot := cd.ItemState&CDIS_HOT != 0
		if cd.ItemState&CDIS_SELECTED != 0 {
			inner = 8 * k
		} else if hot {
			inner = 12 * k
		}
		g.FillEllipse(SolidBrushTmp(accent.argb()), cx-inner/2, cy-inner/2, inner, inner)
		if cd.ItemState&CDIS_FOCUS != 0 && h.keyboardCues(ctl) {
			fp := NewPen(p.label.argb(), 2*k)
			g.DrawArc(fp, cx-d/2-2*k, cy-d/2-2*k, d+4*k, d+4*k, 0, 360)
			fp.Close()
		}
		g.Close()
		freeTmpBrushes()
	}
	return CDRF_SKIPDEFAULT
}

// A brush made for one fill: GDI+ copies what it needs, so it's freed
// at once.
func SolidBrushTmp(c ARGB) Brush {
	b := SolidBrush(c)
	tmpBrushes = append(tmpBrushes, b)
	return b
}

var tmpBrushes []Brush

func freeTmpBrushes() {
	for _, b := range tmpBrushes {
		b.Close()
	}
	tmpBrushes = tmpBrushes[:0]
}

// Text fields.

// makeField makes a TextField: an EDIT without a border in a view that
// draws Fluent's text box around it.
func (h *Host) makeField(n *node) {
	n.hwnd = h.makeView(0)
	h.makeEdit(n)
}

func (h *Host) makeEdit(n *node) {
	style := uint32(WS_CHILD | WS_VISIBLE | WS_TABSTOP)
	if n.multiline {
		style |= ES_MULTILINE | ES_AUTOVSCROLL | ES_WANTRETURN
	} else {
		style |= ES_AUTOHSCROLL
	}
	if n.keyboard == int32(ops.KeyboardNumber) {
		style |= ES_NUMBER
	}
	old := n.ctl
	n.ctl = CreateWindow(0, "EDIT", "", style, 0, 0, 0, 0, n.hwnd, 1)
	SendMessage(n.ctl, WM_SETFONT, h.nodeFont(n), 0)
	SendMessage(n.ctl, EM_SETMARGINS, EC_LEFTMARGIN|EC_RIGHTMARGIN, 0)
	h.byHwnd[n.ctl] = n
	if old != 0 {
		h.forgetTree(old)
		DestroyWindow(old)
		h.byHwnd[n.ctl] = n
	}
	n.settingText = true
	SetWindowText(n.ctl, editText(n))
	n.settingText = false
	if n.placeholder != "" {
		SendMessage(n.ctl, EM_SETCUEBANNER, 1, Str(n.placeholder))
	}
	if n.secure {
		SendMessage(n.ctl, EM_SETPASSWORDCHAR, 0x25CF, 0)
	}
	ctl := n.ctl
	Subclass(ctl, func(hwnd uintptr, m uint32, w, l uintptr) uintptr {
		return h.editMessage(hwnd, m, w, l)
	})
	h.layoutField(n)
}

// editText is the field's text with Windows's line breaks.
func editText(n *node) string {
	if !n.multiline {
		return n.text
	}
	var b []byte
	for i := 0; i < len(n.text); i++ {
		if n.text[i] == '\n' && (i == 0 || n.text[i-1] != '\r') {
			b = append(b, '\r')
		}
		b = append(b, n.text[i])
	}
	return string(b)
}

// editMessage is an EDIT's subclass: return submits a single-line field
// (and doesn't beep), and Ctrl+A selects all.
func (h *Host) editMessage(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	n := h.byHwnd[hwnd]
	if n == nil {
		return DefSubclass(hwnd, m, w, l)
	}
	switch m {
	case WM_GETDLGCODE:
		r := DefSubclass(hwnd, m, w, l)
		if l != 0 {
			if msg := Native[Msg](l); msg.Message == WM_KEYDOWN && msg.WParam == VK_RETURN {
				r |= DLGC_WANTALLKEYS
			}
		}
		return r
	case WM_KEYDOWN:
		if w == VK_RETURN && !n.multiline {
			h.emit(n, ops.Event{Kind: ops.EventSubmit})
			return 0
		}
		if w == 'A' && KeyDown(VK_CONTROL) {
			SendMessage(hwnd, EM_SETSEL, 0, ^uintptr(0))
			return 0
		}
	case WM_CHAR:
		if (w == '\r' || w == '\n') && !n.multiline || w == 1 { // Ctrl+A's character beeps
			return 0
		}
	case WM_PAINT:
		if n.multiline && n.placeholder != "" && n.text == "" {
			// A multiline EDIT has no cue banner: the placeholder is drawn
			// over it while it's empty.
			r := DefSubclass(hwnd, m, w, l)
			dc := GetDC(hwnd)
			old := SelectObject(dc, h.nodeFont(n))
			SetBkMode(dc, TRANSPARENT)
			SetTextColor(dc, h.theme.secondary.over(h.theme.fieldBg).ref())
			cr := ClientRect(hwnd)
			DrawText(dc, UTF16Units(n.placeholder), &cr, DT_NOPREFIX|DT_WORDBREAK)
			SelectObject(dc, old)
			ReleaseDC(hwnd, dc)
			return r
		}
	}
	return DefSubclass(hwnd, m, w, l)
}

func (h *Host) setField(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropTextFieldValue:
		n.text = v.Str
		n.settingText = true
		SetWindowText(n.ctl, editText(n))
		n.settingText = false

	case ops.PropTextFieldPlaceholder:
		n.placeholder = v.Str
		SendMessage(n.ctl, EM_SETCUEBANNER, 1, Str(v.Str))
	case ops.PropTextFieldSecure:
		n.secure = v.Int != 0
		ch := uintptr(0)
		if n.secure {
			ch = 0x25CF // ●
		}
		SendMessage(n.ctl, EM_SETPASSWORDCHAR, ch, 0)
		Invalidate(n.ctl)
	case ops.PropTextFieldMultiline:
		if on := v.Int != 0; on != n.multiline {
			n.multiline = on
			h.makeEdit(n)
			h.themeControl(n)
		}
	case ops.PropTextFieldKeyboard:
		n.keyboard = int32(v.Int)
		style := WindowLong(n.ctl, GWL_STYLE) &^ ES_NUMBER
		if n.keyboard == int32(ops.KeyboardNumber) {
			style |= ES_NUMBER
		}
		SetWindowLong(n.ctl, GWL_STYLE, style)
	}
}

// layoutField places a view's control: a field's EDIT inside its frame
// (padded as Fluent's text box: 10 points across; one line centered),
// the other controls filling their view.
func (h *Host) layoutField(n *node) {
	if n.ctl == 0 || n.hwnd == 0 {
		return
	}
	r := ClientRect(n.hwnd)
	switch n.kind {
	case ops.KindTextField:
		dc := h.measureDC
		old := SelectObject(dc, h.nodeFont(n))
		tm := TextMetrics(dc)
		SelectObject(dc, old)
		x := h.px(10)
		if n.multiline {
			y := h.px(6)
			SetWindowPos(n.ctl, 0, x, y, max(r.W()-2*x, 1), max(r.H()-y-h.px(6), 1), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		} else {
			lh := tm.Height
			y := (r.H() - lh) / 2
			SetWindowPos(n.ctl, 0, x, y, max(r.W()-2*x, 1), lh, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		}
	case ops.KindWheel:
		// The selection field fills the view; the list drops below it.
		SendMessage(n.ctl, CB_SETITEMHEIGHT, ^uintptr(0), uintptr(max(r.H()-h.px(8), 1)))
		SetWindowPos(n.ctl, 0, 0, 0, r.W(), r.H()+h.px(300), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	case ops.KindSlider:
		SendMessage(n.ctl, TBM_SETTHUMBLENGTH, uintptr(h.px(20)), 0)
		SetWindowPos(n.ctl, 0, 0, 0, r.W(), r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	default:
		SetWindowPos(n.ctl, 0, 0, 0, r.W(), r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	}
}

// paintField draws Fluent's text box: a rounded, filled frame with a
// darker bottom edge, the accent's under the focused field.
func (h *Host) paintField(n *node, g Graphics, w, ht float32) {
	k := h.scale
	p := &h.theme
	bg := p.fieldBg
	if n.focused {
		bg = p.fieldFocusBg
	}
	r := 4 * k
	if n.cornerRadius > 0 {
		r = n.cornerRadius * k
	}
	g.FillRoundRect(bg.argb(), 0, 0, w, ht, r)
	g.StrokeRoundRect(p.controlStroke.argb(), max(k, 1), 0, 0, w, ht, r)
	path := NewPath()
	path.RoundRect(0, 0, w, ht, r)
	g.ClipPath(path)
	edge, line := p.secondary.withAlpha(0.7), max(k, 1)
	if n.focused {
		edge, line = p.accent, 2*k
	}
	b := SolidBrush(edge.argb())
	g.FillRect(b, 0, ht-line, w, line)
	b.Close()
	g.ResetClip()
	path.Close()
}

// Stepper: two standard buttons, minus and plus.

func (h *Host) layoutStepper(n *node) {
	r := ClientRect(n.hwnd)
	gap := h.px(4)
	bw := (r.W() - gap) / 2
	SetWindowPos(n.ctl, 0, 0, 0, bw, r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	SetWindowPos(n.ctl2, 0, bw+gap, 0, r.W()-bw-gap, r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
}

// Segmented: the host's own control, drawn as the Windows Community
// Toolkit's Segmented: a track whose selected segment is raised, with
// an accent pill under its title.

func (h *Host) segmentAt(n *node, x int32) int {
	if len(n.items) == 0 {
		return -1
	}
	w := ClientRect(n.hwnd).W()
	return min(max(int(x*int32(len(n.items))/max(w, 1)), 0), len(n.items)-1)
}

func (h *Host) selectSegment(n *node, i int) {
	if i < 0 || i >= len(n.items) || i == n.selected {
		return
	}
	n.selected = i
	h.redraw(n)
	h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
}

// paintSegmented draws the track and the raised segment (g), or the
// titles (dc).
func (h *Host) paintSegmented(n *node, dc uintptr, g Graphics, r Rect) {
	k := h.scale
	p := &h.theme
	w, ht := float32(r.W()), float32(r.H())
	count := len(n.items)
	sw := w / float32(max(count, 1))
	if g != 0 {
		track := rgba{0, 0, 0, 0x0F}
		if h.dark {
			track = rgba{255, 255, 255, 0x0F}
		}
		g.FillRoundRect(track.argb(), 0, 0, w, ht, 5*k)
		g.StrokeRoundRect(p.stroke.argb(), max(k, 1), 0, 0, w, ht, 5*k)
		if n.selected >= 0 && n.selected < count {
			x := float32(n.selected) * sw
			in := 3 * k
			sel := p.control
			if h.dark {
				sel = hex(0x3A3A3A)
			}
			g.FillRoundRect(sel.argb(), x+in, in, sw-2*in, ht-2*in, 3*k)
			g.StrokeRoundRect(p.stroke.argb(), max(k, 1), x+in, in, sw-2*in, ht-2*in, 3*k)
			accent := p.accent
			if c, ok := h.resolve(n.color); ok {
				accent = c
			}
			pw, ph := 16*k, 3*k
			g.FillRoundRect(accent.argb(), x+sw/2-pw/2, ht-in-ph-1*k, pw, ph, ph/2)
		}
		return
	}
	old := SelectObject(dc, h.nodeFont(n))
	SetBkMode(dc, TRANSPARENT)
	for i, s := range n.items {
		x := float32(i) * sw
		color := p.secondary
		if i == n.selected {
			color = p.label
		}
		if n.disabled {
			color = p.disabled
		}
		SetTextColor(dc, color.over(h.backdrop(n)).ref())
		tr := Rect{Left: int32(x) + h.px(6), Top: r.Top, Right: int32(x+sw) - h.px(6), Bottom: r.Bottom - h.px(2)}
		DrawText(dc, UTF16Units(s), &tr, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX|DT_END_ELLIPSIS)
	}
	SelectObject(dc, old)
}
