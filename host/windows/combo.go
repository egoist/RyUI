//go:build windows

package windows

import (
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// Wheel and Picker: a drop-down list combo box (CBS_DROPDOWNLIST) drawn as
// WinUI 3's ComboBox, keeping the combo box's keyboard (arrows, F4,
// Alt+Down, typing to search, Escape), focus and UI Automation.
//
// The closed box is the combo box's own WM_PAINT (it's subclassed): the
// control fill and stroke of Fluent's standard button, its bottom edge
// the elevation border's, 4 corners; the chosen item, or a Picker's label
// in the secondary text color while none is chosen; the chevron (E70D in
// Segoe Fluent Icons) at the end; hover and pressed (its list open)
// fills, the disabled colors, and the focus visual from the keyboard.
//
// The list is owner-drawn (CBS_OWNERDRAWFIXED, CBS_HASSTRINGS: the items
// keep their strings): WM_DRAWITEM reaches the box's parent, the host's
// view, which draws each item as WinUI's ComboBoxItem on the flyout
// surface: 5 by 2 in from the list's sides, 11 by 5 and 7 of padding
// around its text, the hovered item's subtle fill, the chosen item's fill
// and its accent pill at the start. The list's window (ComboLBox) is
// subclassed too: 4 more at its top and bottom, its classic frame painted
// over with the surface, and DWM's rounded corners and border on Windows
// 11 (a hairline of its own on Windows 10).
//
// A contrast theme draws both with the system's colors, as WinUI does:
// ButtonFace and ButtonText, Highlight under the pointer; the list in
// Window and WindowText, Highlight for the item under the pointer and the
// chosen item's pill. Right to left, the chevron is on the left, the
// text right-aligned and the pill on the right. Everything is in the
// pixels of the view's window's DPI.

// WinUI's ComboBox metrics, in points.
const (
	comboPadStart   = 12 // the text's padding at the start (ComboBoxPadding: 12,5,0,7)
	comboPadTop     = 5
	comboPadBottom  = 7
	comboGlyphCol   = 38 // the chevron's column at the end
	comboGlyphEnd   = 14 // the chevron's margin at the end
	comboGlyphSize  = 12
	comboMinWidth   = 64 // ComboBoxThemeMinWidth
	comboMinHeight  = 32
	comboLine       = 20 // Body's line height
	comboItemMarX   = 5  // a ComboBoxItem's margin: 5,2,5,2
	comboItemMarY   = 2
	comboItemPadX   = 11 // and its padding: 11,5,11,7
	comboListPadY   = 4  // ComboBoxDropdownContentMargin: 0,4,0,4
	comboPillWidth  = 3
	comboPillHeight = 16
)

// comboStyle is a Wheel's or Picker's combo box.
const comboStyle = CBS_DROPDOWNLIST | CBS_OWNERDRAWFIXED | CBS_HASSTRINGS | WS_VSCROLL | WS_TABSTOP

var chevronDown = []uint16{0xE70D}

// setupCombo follows a combo box just made: its painting, its pointer,
// and its list's window.
func (h *Host) setupCombo(n *node) {
	if n.ctl == 0 {
		return
	}
	n.comboHot = false
	Subclass(n.ctl, func(hwnd uintptr, m uint32, w, l uintptr) uintptr {
		return h.comboMessage(n, hwnd, m, w, l)
	})
	n.comboList = ComboList(n.ctl)
	if list := n.comboList; list != 0 {
		Subclass(list, func(hwnd uintptr, m uint32, w, l uintptr) uintptr {
			return h.comboListMessage(n, hwnd, m, w, l)
		})
		h.styleComboList(n)
		// Its frame again, with the padding (WM_NCCALCSIZE).
		SetWindowPos(list, 0, 0, 0, 0, 0, SWP_FRAMECHANGED|SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
	}
}

// styleComboList gives a combo box's list the theme's scroll bar and, on
// Windows 11, DWM's rounded corners and border, as WinUI's popups have.
func (h *Host) styleComboList(n *node) {
	list := n.comboList
	if list == 0 {
		return
	}
	AllowDark(list, h.dark)
	if h.dark {
		SetWindowTheme(list, "DarkMode_Explorer")
	} else {
		SetWindowTheme(list, "Explorer")
	}
	if Windows11() {
		SetCornerPreference(list, DWMWCP_ROUND)
		SetBorderColor(list, h.comboListBorder().ref())
	}
}

// layoutCombo fills the view with its combo box: the field's height
// (CB_SETITEMHEIGHT -1) is what makes the box as tall as its view, once
// the combo box's own frame is known; the list's items are WinUI's.
func (h *Host) layoutCombo(n *node, r Rect) {
	ctl := n.ctl
	SendMessage(ctl, CB_SETITEMHEIGHT, 0, uintptr(h.comboItemHeight(n)))
	field := max(r.H()-h.px(n, 6), 1)
	SendMessage(ctl, CB_SETITEMHEIGHT, ^uintptr(0), uintptr(field))
	// The height past the box is the list's, where the combo box takes it.
	SetWindowPos(ctl, 0, 0, 0, r.W(), r.H()+h.px(n, 300), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	if got := WindowRect(ctl).H(); got > 0 && got != r.H() {
		SendMessage(ctl, CB_SETITEMHEIGHT, ^uintptr(0), uintptr(max(field+r.H()-got, 1)))
	}
	Invalidate(ctl)
}

// comboLineHeight is a combo box's line of text, in pixels: its font's,
// at least Body's 20 points.
func (h *Host) comboLineHeight(n *node) int32 {
	dc := h.measureDC
	old := SelectObject(dc, h.nodeFont(n))
	tm := TextMetrics(dc)
	SelectObject(dc, old)
	return max(tm.Height, h.px(n, comboLine*h.fontScale()))
}

// comboItemHeight is a list item's height, in pixels: its line, padding
// and margins (36 points in Body).
func (h *Host) comboItemHeight(n *node) int32 {
	return h.comboLineHeight(n) + h.px(n, 2*comboItemMarY+comboPadTop+comboPadBottom)
}

// measureCombo is a Wheel's or Picker's size, in points: its widest item
// (a Picker's label too) with the padding and the chevron's column, at
// least 64 across, and its line with the padding, at least 32 down.
func (h *Host) measureCombo(n *node) (w, ht float32) {
	widest := int32(0)
	dc := h.measureDC
	old := SelectObject(dc, h.nodeFont(n))
	for _, s := range n.items {
		tw, _ := TextExtent(dc, UTF16Units(s))
		widest = max(widest, tw)
	}
	if n.kind == ops.KindPicker && n.pickerLabel != "" {
		tw, _ := TextExtent(dc, UTF16Units(n.pickerLabel))
		widest = max(widest, tw)
	}
	SelectObject(dc, old)
	w = max(comboPadStart+h.pt(n, widest)+comboGlyphCol, comboMinWidth)
	ht = max(comboPadTop+h.pt(n, h.comboLineHeight(n))+comboPadBottom, comboMinHeight)
	return w, ht
}

// comboText is what the closed box shows: the chosen item, or a Picker's
// label (placeholder) while none is.
func (n *node) comboText() (text string, placeholder bool) {
	if n.selected >= 0 && n.selected < len(n.items) {
		return n.items[n.selected], false
	}
	if n.kind == ops.KindPicker {
		return n.pickerLabel, true
	}
	return "", false
}

// comboState is how the closed box is drawn.
type comboState struct {
	hot, pressed, disabled, focus bool
}

func (h *Host) comboState(n *node, ctl uintptr) comboState {
	return comboState{
		hot:      n.comboHot,
		pressed:  SendMessage(ctl, CB_GETDROPPEDSTATE, 0, 0) != 0,
		disabled: n.disabled || !IsWindowEnabled(ctl),
		focus:    Focus() == ctl && h.keyboardCues(ctl),
	}
}

// comboLook is the closed box's colors in a state.
type comboLook struct {
	fill, stroke, bottom rgba
	text, placeholder    rgba
	glyph, focus         rgba
}

func sysRGBA(i int) rgba {
	c := SysColor(i)
	return rgba{uint8(c), uint8(c >> 8), uint8(c >> 16), 255}
}

// comboLook is WinUI's ComboBox's colors: ControlFillColorDefault, its
// Secondary under the pointer and Tertiary pressed; the elevation border
// (ControlStrokeColorDefault, its bottom ControlStrokeColorSecondary);
// TextFillColorPrimary, the label and the chevron TextFillColorSecondary;
// disabled, ControlFillColorDisabled and TextFillColorDisabled.
func (h *Host) comboLook(n *node, s comboState) comboLook {
	if HighContrast() {
		text := sysRGBA(COLOR_BTNTEXT)
		l := comboLook{fill: sysRGBA(COLOR_BTNFACE), stroke: text, bottom: text, text: text, placeholder: text, glyph: text, focus: text}
		switch {
		case s.disabled:
			gray := sysRGBA(COLOR_GRAYTEXT)
			l.stroke, l.bottom, l.text, l.placeholder, l.glyph = gray, gray, gray, gray, gray
		case s.hot || s.pressed:
			hl, on := sysRGBA(COLOR_HIGHLIGHT), sysRGBA(COLOR_HIGHLIGHTTEXT)
			l.fill, l.stroke, l.bottom = hl, hl, hl
			l.text, l.placeholder, l.glyph = on, on, on
		}
		return l
	}
	p := &h.theme
	l := comboLook{fill: p.control, stroke: p.controlStroke, text: p.label, placeholder: p.secondary, glyph: p.secondary, focus: p.label}
	if h.dark {
		l.bottom = rgba{255, 255, 255, 0x08}.over(p.controlStroke)
	} else {
		l.bottom = rgba{0, 0, 0, 0x18}.over(p.controlStroke)
	}
	switch {
	case s.disabled:
		dis := rgba{0xF9, 0xF9, 0xF9, 0x4D}
		if h.dark {
			dis = rgba{255, 255, 255, 0x0B}
		}
		l.fill = dis.over(h.backdrop(n))
		l.bottom = l.stroke
		l.text, l.placeholder, l.glyph = p.disabled, p.disabled, p.disabled
	case s.pressed:
		l.fill, l.bottom = p.controlPressed, l.stroke // no elevation, pressed
	case s.hot:
		l.fill = p.controlHot
	}
	return l
}

// paintCombo draws the closed box into dc, over what's behind it.
func (h *Host) paintCombo(n *node, ctl, dc uintptr) {
	r := ClientRect(ctl)
	if r.W() <= 0 || r.H() <= 0 {
		return
	}
	h.paintBehind(ctl, dc)
	k := h.scaleOf(n)
	w, ht := float32(r.W()), float32(r.H())
	st := h.comboState(n, ctl)
	look := h.comboLook(n, st)
	radius := min(4*k, w/2, ht/2)
	if n.cornerRadius > 0 {
		radius = min(n.cornerRadius*k, w/2, ht/2)
	}
	line := max(k, 1)
	g := NewGraphics(dc)
	g.FillRoundRect(look.fill.argb(), 0, 0, w, ht, radius)
	g.StrokeRoundRect(look.stroke.argb(), line, 0, 0, w, ht, radius)
	if look.bottom != look.stroke {
		path := NewPath()
		path.RoundRect(0, 0, w, ht, radius)
		g.ClipPath(path)
		b := SolidBrush(look.bottom.argb())
		g.FillRect(b, 0, ht-line, w, line)
		b.Close()
		g.ResetClip()
		path.Close()
	}
	if st.focus {
		g.StrokeRoundRect(look.focus.argb(), 2*k, 0, 0, w, ht, max(radius, 4*k))
	}
	g.Close() // before GDI draws the text

	rtl := h.isRTL(n)
	SetBkMode(dc, TRANSPARENT)
	old := SelectObject(dc, h.nodeFont(n))
	start, end := h.px(n, comboPadStart), h.px(n, comboGlyphCol)
	tr := Rect{Left: r.Left + start, Top: r.Top + h.px(n, comboPadTop), Right: r.Right - end, Bottom: r.Bottom - h.px(n, comboPadBottom)}
	if rtl {
		tr.Left, tr.Right = r.Left+end, r.Right-start
	}
	if tr.Bottom <= tr.Top {
		tr.Top, tr.Bottom = r.Top, r.Bottom
	}
	if text, placeholder := n.comboText(); text != "" && tr.Right > tr.Left {
		c := look.text
		if placeholder {
			c = look.placeholder
		}
		SetTextColor(dc, c.over(look.fill).ref())
		DrawText(dc, UTF16Units(text), &tr, DT_SINGLELINE|DT_VCENTER|DT_NOPREFIX|DT_END_ELLIPSIS|startSide(rtl))
	}
	// The chevron, in its column at the end; pressed, a point lower.
	SelectObject(dc, h.iconFont(n, comboGlyphSize))
	gs := h.px(n, comboGlyphSize)
	gx := r.Right - h.px(n, comboGlyphEnd) - gs
	if rtl {
		gx = r.Left + h.px(n, comboGlyphEnd)
	}
	gr := Rect{Left: gx, Top: r.Top, Right: gx + gs, Bottom: r.Bottom}
	if st.pressed {
		gr.Top += h.px(n, 1)
		gr.Bottom += h.px(n, 1)
	}
	SetTextColor(dc, look.glyph.over(look.fill).ref())
	DrawText(dc, chevronDown, &gr, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_NOPREFIX)
	SelectObject(dc, old)
}

// comboMessage is a Wheel's or Picker's combo box's subclass: it paints
// itself, and repaints as its pointer, focus and list change.
func (h *Host) comboMessage(n *node, hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case WM_PAINT:
		var ps PaintStruct
		dc := BeginPaint(hwnd, &ps)
		r := ClientRect(hwnd)
		if r.W() > 0 && r.H() > 0 {
			pb, mem := BeginBufferedPaint(dc, &r)
			if mem != 0 {
				h.paintCombo(n, hwnd, mem)
				EndBufferedPaint(pb)
			} else {
				h.paintCombo(n, hwnd, dc)
			}
		}
		EndPaint(hwnd, &ps)
		return 0
	case WM_PRINTCLIENT:
		h.paintCombo(n, hwnd, w)
		return 0
	case WM_ERASEBKGND:
		return 1 // WM_PAINT covers it all
	case WM_CTLCOLORLISTBOX:
		// Its list asks it (the list's owner), which would ask its parent.
		bg := h.comboListBg()
		SetBkColor(w, bg.ref())
		SetTextColor(w, h.comboListText().ref())
		return h.brush(bg)
	case WM_MOUSEMOVE:
		if !n.comboHot {
			n.comboHot = true
			TrackLeave(hwnd)
			Invalidate(hwnd)
		}
	case WM_MOUSELEAVE:
		if n.comboHot {
			n.comboHot = false
			Invalidate(hwnd)
		}
	case WM_SETFOCUS, WM_KILLFOCUS, WM_ENABLE, WM_UPDATEUISTATE, WM_LBUTTONDOWN, WM_LBUTTONUP, WM_CAPTURECHANGED:
		r := DefSubclass(hwnd, m, w, l)
		Invalidate(hwnd)
		return r
	}
	return DefSubclass(hwnd, m, w, l)
}

// The list: WinUI's flyout surface, light or dark (the host's menus'), or
// Window and WindowText in a contrast theme.

func (h *Host) comboListBg() rgba {
	if HighContrast() {
		return sysRGBA(COLOR_WINDOW)
	}
	return h.menuBackground()
}

func (h *Host) comboListText() rgba {
	if HighContrast() {
		return sysRGBA(COLOR_WINDOWTEXT)
	}
	return h.theme.label
}

func (h *Host) comboListBorder() rgba {
	if HighContrast() {
		return sysRGBA(COLOR_WINDOWTEXT)
	}
	return h.flyoutBorder()
}

// drawComboItem draws a combo box's list item (WM_DRAWITEM): its
// backplate, under the pointer or the keyboard (ODS_SELECTED) and on the
// chosen item, the chosen item's accent pill at the start, and its text.
// The closed box's field draws itself (comboMessage): asked to draw it,
// as a combo box does when its selection changes, the box is repainted.
func (h *Host) drawComboItem(n *node, d *DrawItem) {
	if d.ItemState&ODS_COMBOBOXEDIT != 0 {
		if n.ctl != 0 {
			Invalidate(n.ctl)
		}
		return
	}
	dc, r := d.DC, d.Item
	bg := h.comboListBg()
	FillColor(dc, r, bg.ref())
	i := int(int32(d.ItemID))
	if i < 0 || i >= len(n.items) {
		return
	}
	k := h.scaleOf(n)
	hc := HighContrast()
	highlighted := d.ItemState&ODS_SELECTED != 0
	chosen := i == n.selected
	text := h.comboListText()
	var plate, pill rgba
	if hc {
		if highlighted {
			plate, text = sysRGBA(COLOR_HIGHLIGHT), sysRGBA(COLOR_HIGHLIGHTTEXT)
		}
		pill = sysRGBA(COLOR_HIGHLIGHT)
		if highlighted {
			pill = sysRGBA(COLOR_HIGHLIGHTTEXT)
		}
	} else {
		if highlighted || chosen {
			plate = h.theme.subtleHot.over(bg) // SubtleFillColorSecondary
		}
		pill = h.theme.accent // AccentFillColorDefault
		if c, ok := h.resolve(n.color); ok && c.a > 0 {
			pill = c.over(bg)
		}
	}
	// A mirrored DC's left is its right: GDI follows it, GDI+ doesn't.
	mirrored := Mirrored(dc)
	right := h.isRTL(n) != mirrored // the start on the DC's right
	pr := Rect{Left: r.Left + h.px(n, comboItemMarX), Top: r.Top + h.px(n, comboItemMarY), Right: r.Right - h.px(n, comboItemMarX), Bottom: r.Bottom - h.px(n, comboItemMarY)}
	under := bg
	pw, ph := h.px(n, comboPillWidth), h.px(n, comboPillHeight)
	px := pr.Left
	if right {
		px = pr.Right - pw
	}
	py := pr.Top + (pr.H()-ph)/2
	if mirrored {
		if plate.a > 0 {
			FillRoundRectGDI(dc, pr, h.px(n, 4), plate.ref())
		}
		if chosen {
			FillRoundRectGDI(dc, Rect{Left: px, Top: py, Right: px + pw, Bottom: py + ph}, max(pw/2, 1), pill.ref())
		}
	} else if plate.a > 0 || chosen {
		g := NewGraphics(dc)
		if plate.a > 0 {
			g.FillRoundRect(plate.argb(), float32(pr.Left), float32(pr.Top), float32(pr.W()), float32(pr.H()), 4*k)
		}
		if chosen {
			g.FillRoundRect(pill.argb(), float32(px), float32(py), float32(pw), float32(ph), float32(pw)/2)
		}
		g.Close()
	}
	if plate.a > 0 {
		under = plate
	}
	pad := h.px(n, comboItemPadX)
	tr := Rect{Left: pr.Left + pad, Top: pr.Top + h.px(n, comboPadTop), Right: pr.Right - pad, Bottom: pr.Bottom - h.px(n, comboPadBottom)}
	if tr.Bottom <= tr.Top {
		tr.Top, tr.Bottom = pr.Top, pr.Bottom
	}
	side := uint32(DT_LEFT)
	if right {
		side = DT_RIGHT
	}
	old := SelectObject(dc, h.nodeFont(n))
	SetBkMode(dc, TRANSPARENT)
	SetTextColor(dc, text.over(under).ref())
	DrawText(dc, UTF16Units(n.items[i]), &tr, DT_SINGLELINE|DT_VCENTER|DT_NOPREFIX|DT_END_ELLIPSIS|side|textDirection(h.isRTL(n)))
	SelectObject(dc, old)
}

// comboListMessage is a combo box's list window's subclass: WinUI's 4
// points above and below the items (its client area inset, its window
// made that much taller as the combo box drops it), its frame painted
// over with the surface, and its background.
func (h *Host) comboListMessage(n *node, hwnd uintptr, m uint32, w, l uintptr) uintptr {
	switch m {
	case WM_NCCALCSIZE:
		r := DefSubclass(hwnd, m, w, l)
		rc := Native[Rect](l) // the client area (NCCALCSIZE_PARAMS's first)
		if pad := h.px(n, comboListPadY); rc.Bottom-rc.Top > 2*pad {
			rc.Top += pad
			rc.Bottom -= pad
		}
		return r
	case WM_WINDOWPOSCHANGING:
		// Sized while hidden is the combo box dropping it (or placing it
		// for later): the padding goes on what it asks for, upward when
		// it opens above the box.
		pos := Native[WindowPos](l)
		if pos.Flags&SWP_NOSIZE == 0 && pos.Cy > 0 && !IsWindowVisible(hwnd) {
			extra := 2 * h.px(n, comboListPadY)
			if pos.Flags&SWP_NOMOVE == 0 && n.ctl != 0 && pos.Y+pos.Cy <= WindowRect(n.ctl).Top+1 {
				pos.Y -= extra
			}
			pos.Cy += extra
		}
	case WM_NCPAINT:
		r := DefSubclass(hwnd, m, w, l) // the scroll bar
		dc := WindowDC(hwnd)
		h.paintComboListFrame(hwnd, dc)
		ReleaseDC(hwnd, dc)
		return r
	case WM_PRINT: // its opening animation (AnimateWindow)
		r := DefSubclass(hwnd, m, w, l)
		if l&0x2 != 0 { // PRF_NONCLIENT
			h.paintComboListFrame(hwnd, w)
		}
		return r
	case WM_ERASEBKGND:
		FillColor(w, ClientRect(hwnd), h.comboListBg().ref())
		return 1
	case WM_NCDESTROY:
		Unsubclass(hwnd)
		if n.comboList == hwnd {
			n.comboList = 0
		}
	}
	return DefSubclass(hwnd, m, w, l)
}

// paintComboListFrame paints a list window's frame (its classic border)
// and padding in the surface, the scroll bar left as it is: on Windows 11
// DWM draws the border around it, rounded; before, its hairline is drawn
// here.
func (h *Host) paintComboListFrame(hwnd, dc uintptr) {
	wr, cr := WindowRect(hwnd), ClientRect(hwnd)
	w, ht := wr.W(), wr.H()
	p := ClientToScreen(hwnd, Point{})
	if WindowLong(hwnd, GWL_EXSTYLE)&WS_EX_LAYOUTRTL != 0 {
		p.X -= cr.W() // a mirrored window's client origin is its top right
	}
	c := Rect{Left: p.X - wr.Left, Top: p.Y - wr.Top}
	c.Right, c.Bottom = c.Left+cr.W(), c.Top+cr.H()
	bg := h.comboListBg().ref()
	const b = 1 // WS_BORDER's
	FillColor(dc, Rect{Right: w, Bottom: c.Top}, bg)
	FillColor(dc, Rect{Top: c.Bottom, Right: w, Bottom: ht}, bg)
	FillColor(dc, Rect{Top: c.Top, Right: b, Bottom: c.Bottom}, bg)
	FillColor(dc, Rect{Left: w - b, Top: c.Top, Right: w, Bottom: c.Bottom}, bg)
	if !Windows11() {
		edge := h.comboListBorder().ref()
		FillColor(dc, Rect{Right: w, Bottom: b}, edge)
		FillColor(dc, Rect{Top: ht - b, Right: w, Bottom: ht}, edge)
		FillColor(dc, Rect{Right: b, Bottom: ht}, edge)
		FillColor(dc, Rect{Left: w - b, Right: w, Bottom: ht}, edge)
	}
}

// comboCommand follows a combo box's notifications: the user's choice,
// its list opening and closing.
func (h *Host) comboCommand(n *node, ctl uintptr, code uint32) {
	switch code {
	case CBN_SELCHANGE:
		h.comboChose(n, ctl)
	case CBN_DROPDOWN:
		h.styleComboList(n) // as the theme is now (a contrast theme)
		Invalidate(ctl)
	case CBN_CLOSEUP:
		// The list closed (Escape puts back what was chosen): the box shows
		// the combo box's choice, and the app has it.
		cur := n.selected
		if cur < 0 || cur >= len(n.items) {
			cur = -1
		}
		if int(int32(SendMessage(ctl, CB_GETCURSEL, 0, 0))) != cur {
			h.comboChose(n, ctl)
		}
		Invalidate(ctl)
	}
}

// comboChose takes the combo box's selection as the user's choice, once:
// a click in the list sends CBN_CLOSEUP, then CBN_SELCHANGE.
func (h *Host) comboChose(n *node, ctl uintptr) {
	i := int(int32(SendMessage(ctl, CB_GETCURSEL, 0, 0)))
	Invalidate(ctl)
	if n.comboList != 0 {
		Invalidate(n.comboList) // the pill moves
	}
	if i == n.selected {
		return
	}
	n.selected = i
	h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
}
