//go:build windows

package windows

import (
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// An alert with text fields (AlertProps.Fields) can't be a task dialog,
// which holds no edit controls: it's a dialog of the host's own, laid
// out as WinUI's ContentDialog. A window over the one it's declared in
// (which is disabled meanwhile, as under a sheet), with the app's name
// in its title bar as a task dialog has; in it the title (Subtitle, 20
// semibold) and the message (Body), the fields (the host's own text
// boxes, Fluent's), then a band of buttons on the window's background
// below a stroke, as wide as each other, the cancel one last and the
// default one in the accent color.
//
// The first field has the focus. Tab goes through the fields and the
// buttons; Enter in a field goes on to the next, and in the last presses
// the default button (the first whose role is Default) if it's enabled;
// Enter or Space on a button presses it; Escape and the close button are
// the cancel button, when there's one (a task dialog without one can't
// be closed either). Screen readers read the title and the message (its
// statics), each field by its placeholder, and the buttons.

// Its measures, in points (ContentDialog's).
const (
	alertDlgWidth   = 400 // its content's width
	alertDlgPad     = 24  // around the content and the buttons
	alertDlgGap     = 12  // under the title
	alertDlgField   = 32  // a field's height
	alertDlgSpace   = 8   // between fields, between buttons
	alertDlgButton  = 32  // a button's height
	alertDlgAbove   = 16  // over the first field
	alertDlgStrokeW = 1
)

// STATIC's styles.
const (
	ssLeft        = 0x0000
	ssRight       = 0x0002
	ssNoPrefix    = 0x0080
	ssEditControl = 0x2000
)

// alertDialog is an alert's dialog, while it shows.
type alertDialog struct {
	win            uintptr
	wn             *node // the window's (kindAlertWindow), whose parent is the alert
	owner          uintptr
	title, message uintptr // STATICs (0: none)
	fields         []*node
	buttons        []uintptr
	actions        []int // each button's action
	def            int   // the default action (-1: none)
	cancel         int   // the cancel action (-1: none)
	focus          uintptr
	footer         int32 // the buttons' band's top, in pixels
	done           bool
}

// alertDialogDefault is the default action of an alert: the first whose
// role is Default; -1 if none.
func alertDialogDefault(as []action) int {
	for i, a := range as {
		if a.role == ops.AlertButtonRoleDefault {
			return i
		}
	}
	return -1
}

// openAlertDialog shows an alert with fields as the host's own dialog.
func (h *Host) openAlertDialog(n *node) {
	owner := h.presentOn(n.nav.on)
	if owner == 0 {
		owner = h.window
	}
	d := &alertDialog{owner: owner, def: alertDialogDefault(n.actions), cancel: -1}
	for i, a := range n.actions {
		if a.role == ops.AlertButtonRoleCancel {
			d.cancel = i
		}
	}
	// The buttons: in order, the cancel one last (ContentDialog's close
	// button).
	for i, a := range n.actions {
		if a.role != ops.AlertButtonRoleCancel {
			d.actions = append(d.actions, i)
		}
	}
	if d.cancel >= 0 {
		d.actions = append(d.actions, d.cancel)
	}
	style := uint32(WS_POPUP | WS_CAPTION | WS_CLIPCHILDREN)
	if d.cancel >= 0 {
		style |= WS_SYSMENU // the close button: Cancel
	}
	dpi := DPI(owner)
	k := dpiScale(dpi)
	win := CreateWindow(h.topLevelStyle()|WS_EX_DLGMODALFRAME|WS_EX_CONTROLPARENT, classWindow, h.appName, style, 0, 0, 100, 100, owner, 0)
	if dd := DPI(win); dd != dpi {
		dpi, k = dd, dpiScale(dd)
	}
	wn := newNode(0, kindAlertWindow)
	wn.hwnd = win
	wn.parent = n
	wn.k = k
	d.win, d.wn = win, wn
	h.byHwnd[win] = wn
	n.nav.dlg = d
	SetDarkTitleBar(win, h.dark)

	rtl := h.isRTL(n)
	side, ex := uint32(ssLeft), uint32(0)
	if rtl {
		side, ex = ssRight, WS_EX_RTLREADING
	}
	if n.title != "" {
		d.title = CreateWindow(ex, "STATIC", n.title, WS_CHILD|WS_VISIBLE|ssNoPrefix|ssEditControl|side, 0, 0, 0, 0, win, 0)
		h.byHwnd[d.title] = wn
	}
	if n.message != "" {
		d.message = CreateWindow(ex, "STATIC", n.message, WS_CHILD|WS_VISIBLE|ssNoPrefix|ssEditControl|side, 0, 0, 0, 0, win, 0)
		h.byHwnd[d.message] = wn
	}
	for i, f := range n.nav.fields {
		h.nextCell++
		fn := newNode(ops.HostIDBit|h.nextCell, ops.KindTextField)
		fn.k = k
		fn.alertOf = n
		fn.placeholder = f.Placeholder
		fn.text = f.Text
		fn.secure = f.Secure()
		fn.keyboard = int32(f.Keyboard)
		h.makeField(fn)
		h.byHwnd[fn.hwnd] = fn
		SetParent(fn.hwnd, win)
		SetAccessibleName(fn.ctl, f.Placeholder) // no label beside it: its placeholder is its name
		if i == 0 {
			d.focus = fn.ctl
			SendMessage(fn.ctl, EM_SETSEL, 0, ^uintptr(0))
		}
		d.fields = append(d.fields, fn)
	}
	for _, i := range d.actions {
		a := n.actions[i]
		bs := uint32(WS_CHILD | WS_VISIBLE | WS_TABSTOP | BS_PUSHBUTTON)
		if i == d.def {
			bs = WS_CHILD | WS_VISIBLE | WS_TABSTOP | BS_DEFPUSHBUTTON
		}
		b := CreateWindow(0, "BUTTON", a.title, bs, 0, 0, 0, 0, win, uintptr(100+i))
		SendMessage(b, WM_UPDATEUISTATE, MakeLong(UIS_SET, UISF_HIDEFOCUS), 0)
		if a.is(ops.MenuDisabled) {
			EnableWindow(b, false)
		}
		h.byHwnd[b] = wn
		d.buttons = append(d.buttons, b)
	}
	w, ht := h.layoutAlertDialog(n)
	r := Rect{Right: w, Bottom: ht}
	AdjustWindowRect(&r, style, WS_EX_DLGMODALFRAME, dpi)
	wr := WindowRect(owner)
	x := wr.Left + (wr.W()-r.W())/2
	y := wr.Top + (wr.H()-r.H())/2
	SetWindowPos(win, 0, x, y, r.W(), r.H(), SWP_NOZORDER|SWP_NOACTIVATE)
	EnableWindow(owner, false)
	n.presented = true
	if snapshotting() {
		ShowWindow(win, SW_SHOWNOACTIVATE)
	} else {
		ShowWindow(win, SW_SHOW)
	}
	if d.focus != 0 {
		SetFocus(d.focus)
	}
}

// layoutAlertDialog places the dialog's controls in its window at its
// scale, mirrored right to left, and returns its client size (pixels).
func (h *Host) layoutAlertDialog(n *node) (w, ht int32) {
	d := n.nav.dlg
	wn := d.wn
	k := wn.k
	px := func(v float32) int32 { return h.px(wn, v) }
	pad, cw := px(alertDlgPad), px(alertDlgWidth)
	w = cw + 2*pad
	rtl := h.isRTL(n)
	across := func(x, width int32) int32 {
		if rtl {
			return w - x - width
		}
		return x
	}
	y := pad
	text := func(ctl uintptr, s string, spec fontSpec) {
		spec.k = k
		font := h.font(spec)
		SendMessage(ctl, WM_SETFONT, font, 0)
		dc := h.measureDC
		old := SelectObject(dc, font)
		tr := Rect{Right: cw}
		DrawText(dc, UTF16Units(s), &tr, DT_CALCRECT|DT_WORDBREAK|DT_NOPREFIX|DT_EDITCONTROL|textDirection(rtl))
		SelectObject(dc, old)
		SetWindowPos(ctl, 0, pad, y, cw, tr.H(), SWP_NOZORDER|SWP_NOACTIVATE)
		y += tr.H()
	}
	if d.title != 0 {
		text(d.title, n.title, h.spec(int32(ops.TextStyleTitle2), 0, 0, ""))
		if d.message != 0 {
			y += px(alertDlgGap)
		}
	}
	if d.message != 0 {
		text(d.message, n.message, h.spec(int32(ops.TextStyleBody), 0, 0, ""))
	}
	for i, f := range d.fields {
		if i == 0 {
			if d.title != 0 || d.message != 0 {
				y += px(alertDlgAbove)
			}
		} else {
			y += px(alertDlgSpace)
		}
		f.k = k
		SendMessage(f.ctl, WM_SETFONT, h.nodeFont(f), 0)
		fh := px(alertDlgField * h.fontScale())
		SetWindowPos(f.hwnd, 0, pad, y, cw, fh, SWP_NOZORDER|SWP_NOACTIVATE)
		h.layoutField(f)
		y += fh
	}
	y += pad
	d.footer = y
	// The buttons, as wide as each other across the band.
	if c := int32(len(d.buttons)); c > 0 {
		gap := px(alertDlgSpace)
		bw := (cw - gap*(c-1)) / c
		bh := px(alertDlgButton * h.fontScale())
		for i, b := range d.buttons {
			x := pad + int32(i)*(bw+gap)
			SetWindowPos(b, 0, across(x, bw), y+pad, bw, bh, SWP_NOZORDER|SWP_NOACTIVATE)
		}
		y += pad + bh + pad
	}
	return w, y
}

// alertDialogMessage handles the dialog's window's messages.
func (h *Host) alertDialogMessage(wn *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	n := wn.parent
	if n == nil || n.nav == nil || n.nav.dlg == nil {
		return 0, false
	}
	d := n.nav.dlg
	switch m {
	case WM_PAINT:
		h.paintWindow(wn, wn.hwnd)
		return 0, true
	case WM_ERASEBKGND:
		return 1, true
	case WM_PRINTCLIENT:
		h.paint(wn, wn.hwnd, wp, ClientRect(wn.hwnd))
		return 0, true
	case WM_CLOSE:
		h.cancelAlertDialog(wn)
		return 0, true
	case WM_ACTIVATE:
		if LoWord(wp) != WA_INACTIVE {
			h.inUse = wn.hwnd
			if d.focus != 0 && IsWindow(d.focus) {
				SetFocus(d.focus)
			}
		} else if f := Focus(); f != 0 && IsChild(wn.hwnd, f) {
			d.focus = f // where the focus goes back to
		}
		return 0, true
	case WM_DPICHANGED:
		wn.k = dpiScale(int(LoWord(wp)))
		s := *Native[Rect](lp)
		w, ht := h.layoutAlertDialog(n)
		r := Rect{Right: w, Bottom: ht}
		AdjustWindowRect(&r, uint32(WindowLong(wn.hwnd, GWL_STYLE)), WS_EX_DLGMODALFRAME, int(LoWord(wp)))
		SetWindowPos(wn.hwnd, 0, s.Left, s.Top, r.W(), r.H(), SWP_NOZORDER|SWP_NOACTIVATE)
		RedrawWindow(wn.hwnd, RDW_INVALIDATE|RDW_ALLCHILDREN|RDW_ERASE)
		return 0, true
	}
	return 0, false
}

// paintAlertDialog draws the dialog's background: its content on the
// layer's color, the buttons' band on the window's, a stroke between.
func (h *Host) paintAlertDialog(wn *node, g Graphics, r Rect) {
	n := wn.parent
	if n == nil || n.nav == nil || n.nav.dlg == nil {
		return
	}
	p := &h.theme
	w, ht := float32(r.W()), float32(r.H())
	top := float32(n.nav.dlg.footer)
	b := SolidBrush(p.layer.argb())
	g.FillRect(b, 0, 0, w, top)
	b.Close()
	b = SolidBrush(p.bg.argb())
	g.FillRect(b, 0, top, w, ht-top)
	b.Close()
	b = SolidBrush(p.cardStroke.argb())
	g.FillRect(b, 0, top, w, max(float32(h.px(wn, alertDlgStrokeW)), 1))
	b.Close()
}

// alertDialogColors answers a static's WM_CTLCOLORSTATIC: the label's
// color on the content's.
func (h *Host) alertDialogColors(dc uintptr) uintptr {
	bg := h.theme.layer
	SetTextColor(dc, h.theme.label.ref())
	SetBkColor(dc, bg.ref())
	return h.brush(bg)
}

// alertDialogCommand is a button of the dialog pressed: its action.
func (h *Host) alertDialogCommand(wn *node, ctl uintptr, code uint32) {
	n := wn.parent
	if code != BN_CLICKED || n == nil || n.nav.dlg == nil {
		return
	}
	d := n.nav.dlg
	for k, b := range d.buttons {
		if b == ctl {
			h.pressAlertDialog(n, d.actions[k])
			return
		}
	}
}

// pressAlertDialog presses action i, if it's enabled: Action, and the
// core destroys the alert, which closes the dialog.
func (h *Host) pressAlertDialog(n *node, i int) {
	d := n.nav.dlg
	if d == nil || d.done || i < 0 || i >= len(n.actions) || n.actions[i].is(ops.MenuDisabled) || n.dying {
		return
	}
	d.done = true
	h.emit(n, ops.Event{Kind: ops.EventAction, Int: int64(i)})
}

// cancelAlertDialog is Escape or the close button: the cancel button,
// when there's one.
func (h *Host) cancelAlertDialog(wn *node) {
	if n := wn.parent; n != nil && n.nav.dlg != nil && n.nav.dlg.cancel >= 0 {
		h.pressAlertDialog(n, n.nav.dlg.cancel)
	}
}

// alertFieldEvent is an event of a dialog's field: typing is the alert's
// FieldChanged; Enter goes on to the next field, and in the last presses
// the default button if it's enabled.
func (h *Host) alertFieldEvent(f *node, ev ops.Event) {
	n := f.alertOf
	d := n.nav.dlg
	if d == nil || n.dying {
		return
	}
	i := -1
	for k, x := range d.fields {
		if x == f {
			i = k
		}
	}
	if i < 0 {
		return
	}
	switch ev.Kind {
	case ops.EventTextChanged:
		h.emit(n, ops.Event{Kind: ops.EventFieldChanged, Bytes: ops.AppendFieldChanged(nil, i, ev.Str)})
	case ops.EventSubmit:
		if i+1 < len(d.fields) {
			next := d.fields[i+1].ctl
			SetFocus(next)
			SendMessage(next, EM_SETSEL, 0, ^uintptr(0))
			return
		}
		h.pressAlertDialog(n, d.def)
	}
}

// updateAlertDialog shows the buttons' titles and Disabled again.
func (h *Host) updateAlertDialog(n *node) {
	d := n.nav.dlg
	if d == nil {
		return
	}
	for k, i := range d.actions {
		if i >= len(n.actions) {
			continue
		}
		a := n.actions[i]
		b := d.buttons[k]
		if WindowText(b) != a.title {
			SetWindowText(b, a.title)
		}
		EnableWindow(b, !a.is(ops.MenuDisabled))
		Invalidate(b)
	}
}

// closeAlertDialog closes the dialog: the window it's over comes back
// first, so it's activated.
func (h *Host) closeAlertDialog(n *node) {
	d := n.nav.dlg
	if d == nil {
		return
	}
	n.nav.dlg = nil
	EnableWindow(d.owner, true)
	if h.inUse == d.win {
		h.inUse = d.owner
	}
	for _, fn := range d.fields {
		ClearAccessibleName(fn.ctl) // as Dynamic Annotation asks, before the window goes
	}
	h.forgetTree(d.win)
	DestroyWindow(d.win)
}

// alertDialogDefaultButton reports whether ctl is the dialog's default
// button.
func (h *Host) alertDialogDefaultButton(wn *node, ctl uintptr) bool {
	n := wn.parent
	if n == nil || n.nav == nil || n.nav.dlg == nil {
		return false
	}
	d := n.nav.dlg
	for k, b := range d.buttons {
		if b == ctl {
			return d.actions[k] == d.def
		}
	}
	return false
}
