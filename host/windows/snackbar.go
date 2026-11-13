//go:build windows

package windows

import (
	"math"
	"time"

	"github.com/egoist/asuka/internal/l10n"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// A Snackbar on Windows: Win32 has none, so it's an overlay at the
// bottom of the window it's on, drawn as Fluent's InfoBar: a card in the
// layer color with its stroke and 8 point corners, the message, a
// standard button for its action and a subtle close button (the host's
// own buttons, drawn as Fluent's in custom draw). It slides up in, and
// down out (it just shows and goes with animations off); its time waits
// while the mouse is over it or the keyboard's focus is in it; screen
// readers hear it as a UI Automation notification, and reach its
// buttons, which are BUTTON controls.
type snack struct {
	owner    uintptr // the top-level window it's on
	action   uintptr // BUTTON: its action (0: none)
	close    uintptr // BUTTON: closes it
	duration ops.SnackbarDuration
	left     time.Duration // of its time
	stop     func()        // its timer's
	slide    func()        // its slide's timer's
	slideAt  time.Time
	out      bool  // sliding out
	offset   int32 // px below its place, while it slides
	text     Rect  // the message's, in its window (px)
	frame    Rect  // its place in its owner's client area (px)
	done     bool  // reported (its action, its time, closed) or destroyed
}

const (
	snackMargin  = 24  // from the window's edges (points)
	snackMaxW    = 560 // at most this wide
	snackPadL    = 16
	snackPadR    = 8
	snackPadY    = 14
	snackGap     = 12
	snackMinH    = 48
	snackButtonH = 32
	snackCloseW  = 32
	snackRadius  = 8
	snackSlide   = 200 * time.Millisecond

	spiGetMessageDuration = 0x2016 // SPI_GETMESSAGEDURATION: seconds
)

func (h *Host) setSnackProp(n *node, p ops.PropID, v *ops.Value) {
	s := n.snack
	switch p {
	case ops.PropSnackbarMessage:
		n.message = v.Str
		n.a11yLabel = v.Str // its group's name
		if s.owner != 0 {
			h.placeSnack(n)
			Invalidate(n.hwnd)
		}
	case ops.PropSnackbarAction:
		n.title = v.Str
		if s.owner != 0 {
			h.snackButton(n)
			h.placeSnack(n)
		}
	case ops.PropSnackbarDuration:
		s.duration = ops.SnackbarDuration(v.Int)
	case ops.PropSnackbarWindow:
		n.nav.on = ops.NodeID(v.Int)
	}
}

// presentSnack shows it at the bottom of the window it's declared in
// (presentOn: the main window, a Window, the sheet over it).
func (h *Host) presentSnack(n *node) {
	s := n.snack
	if s.owner != 0 || s.done || n.dying {
		return
	}
	owner := h.presentOn(n.nav.on)
	if owner == 0 {
		owner = h.window // its Window never opened
	}
	if owner == 0 {
		return
	}
	s.owner = owner
	if owner != h.window {
		n.k = float32(DPI(owner)) / 96
	}
	n.host = snackMessage
	SetParent(n.hwnd, owner)
	SetWindowLong(n.hwnd, GWL_EXSTYLE, WindowLong(n.hwnd, GWL_EXSTYLE)|WS_EX_CONTROLPARENT)
	s.close = CreateWindow(0, "BUTTON", l10n.Close(h.locale), WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, n.hwnd, 2)
	SendMessage(s.close, WM_UPDATEUISTATE, MakeLong(UIS_SET, UISF_HIDEFOCUS), 0)
	if h.tips == 0 {
		h.tips = NewTooltips(h.window)
	}
	SetTooltip(h.tips, s.close, l10n.Close(h.locale))
	h.byHwnd[s.close] = n
	h.snackButton(n)
	h.snacks = append(h.snacks, n)

	animate := !h.env.ReduceMotion
	if animate {
		s.offset = h.snackTravel(n)
	}
	h.placeSnack(n)
	ShowWindow(n.hwnd, SW_SHOWNOACTIVATE)
	if animate {
		h.slideSnack(n, false)
	}
	text := n.message
	if n.title != "" {
		text += ", " + n.title
	}
	Announce(owner, text)
	h.startSnack(n)
}

// snackButton makes, retitles or removes its action's button.
func (h *Host) snackButton(n *node) {
	s := n.snack
	if n.title == "" {
		if s.action != 0 {
			delete(h.byHwnd, s.action)
			DestroyWindow(s.action)
			s.action = 0
		}
		return
	}
	if s.action == 0 {
		s.action = CreateWindow(0, "BUTTON", n.title, WS_CHILD|WS_VISIBLE|WS_TABSTOP|BS_PUSHBUTTON, 0, 0, 0, 0, n.hwnd, 1)
		SendMessage(s.action, WM_SETFONT, h.bodyFont(n), 0)
		SendMessage(s.action, WM_UPDATEUISTATE, MakeLong(UIS_SET, UISF_HIDEFOCUS), 0)
		h.byHwnd[s.action] = n
		return
	}
	SetWindowText(s.action, n.title)
	Invalidate(s.action)
}

// placeSnack lays it out: as wide as its message needs, up to snackMaxW
// and its window's width, centered at the bottom; the message, its
// button and the close button (mirrored right to left).
func (h *Host) placeSnack(n *node) {
	s := n.snack
	if s.owner == 0 || n.hwnd == 0 {
		return
	}
	px := func(v float32) int32 { return h.px(n, v) }
	cr := ClientRect(s.owner)
	maxW := min(px(snackMaxW), cr.W()-2*px(snackMargin))
	dc := h.measureDC
	old := SelectObject(dc, h.bodyFont(n))
	var bw int32
	if s.action != 0 {
		tw, _ := TextExtent(dc, UTF16Units(n.title))
		bw = tw + px(24) // Fluent's button: 12 points each side
	}
	fixed := px(snackPadL) + px(snackGap) + px(snackCloseW) + px(snackPadR)
	if bw > 0 {
		fixed += bw + px(8)
	}
	avail := max(maxW-fixed, px(60))
	tr := Rect{Right: avail}
	DrawText(dc, UTF16Units(n.message), &tr, DT_CALCRECT|DT_WORDBREAK|DT_NOPREFIX)
	SelectObject(dc, old)
	tw, th := min(tr.W(), avail), tr.H()
	height := max(px(snackMinH), th+2*px(snackPadY), px(snackButtonH)+px(16))
	width := tw + fixed
	x := (cr.W() - width) / 2
	y := cr.H() - px(snackMargin) - height
	s.frame = Rect{Left: x, Top: y, Right: x + width, Bottom: y + height}
	SetWindowPos(n.hwnd, HWND_TOP, x, y+s.offset, width, height, SWP_NOACTIVATE)

	rtl := h.isRTL(n)
	at := func(left, w int32) int32 {
		if rtl {
			return width - left - w
		}
		return left
	}
	s.text = Rect{Left: at(px(snackPadL), tw), Top: (height - th) / 2}
	s.text.Right, s.text.Bottom = s.text.Left+tw, s.text.Top+th
	bh := px(snackButtonH)
	next := px(snackPadL) + tw + px(snackGap)
	if s.action != 0 {
		SetWindowPos(s.action, 0, at(next, bw), (height-bh)/2, bw, bh, SWP_NOZORDER|SWP_NOACTIVATE)
	}
	cw := px(snackCloseW)
	SetWindowPos(s.close, 0, at(width-px(snackPadR)-cw, cw), (height-cw)/2, cw, cw, SWP_NOZORDER|SWP_NOACTIVATE)
	Invalidate(n.hwnd)
}

// placeSnacks lays out the snackbars showing: a window resized.
func (h *Host) placeSnacks() {
	for _, n := range h.snacks {
		if !n.snack.done {
			h.placeSnack(n)
		}
	}
}

// paintSnack draws its card: the layer color, its stroke, its corners.
func (h *Host) paintSnack(n *node, g Graphics, w, ht float32) {
	k := h.scaleOf(n)
	p := &h.theme
	r := snackRadius * k
	g.FillRoundRect(p.layer.argb(), 0, 0, w, ht, r)
	g.StrokeRoundRect(p.cardStroke.argb(), max(k, 1), 0.5, 0.5, w-1, ht-1, r)
}

// paintSnackText draws its message.
func (h *Host) paintSnackText(n *node, dc uintptr) {
	tr := n.snack.text
	SetBkMode(dc, TRANSPARENT)
	SetTextColor(dc, h.theme.label.ref())
	old := SelectObject(dc, h.bodyFont(n))
	DrawText(dc, UTF16Units(n.message), &tr, DT_WORDBREAK|DT_NOPREFIX|startSide(h.isRTL(n)))
	SelectObject(dc, old)
}

// snackMessage is its window's own messages: it takes clicks (rather
// than letting them through to what's under it, as a view that doesn't
// press does).
func snackMessage(h *Host, n *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	if m == wmNCHitTest {
		return 1, true // HTCLIENT
	}
	return 0, false
}

// snackClicked is its buttons' BN_CLICKED.
func (h *Host) snackClicked(n *node, ctl uintptr) {
	if ctl == n.snack.close {
		h.snackReport(n, ops.EventDismissed)
		return
	}
	h.snackReport(n, ops.EventAction)
}

// snackReport tells the core it went (its action, its time running out,
// closed), and takes it out; the core destroys it.
func (h *Host) snackReport(n *node, kind ops.EventKind) {
	s := n.snack
	if s.done {
		return
	}
	s.done = true
	h.stopSnack(n)
	h.leaveSnack(n)
	h.emit(n, ops.Event{Kind: kind}) // Action's Int: 0, its one action
}

// startSnack starts its time: four seconds, ten for SnackbarLong.
// Windows' "Dismiss notifications after this amount of time" setting
// (SPI_GETMESSAGEDURATION) lengthens it, as UWP's notifications do;
// with a screen reader on, one with a button stays until it's closed,
// and one without stays long.
func (h *Host) startSnack(n *node) {
	s := n.snack
	if s.duration == ops.SnackbarDurationIndefinite {
		return
	}
	d := 4 * time.Second
	if s.duration == ops.SnackbarDurationLong {
		d = 10 * time.Second
	}
	if secs := SystemParam(spiGetMessageDuration); secs > 5 {
		d = max(d, time.Duration(secs)*time.Second)
	}
	if h.env.ScreenReader {
		if n.title != "" {
			return
		}
		d = max(d, 10*time.Second)
	}
	s.left = d
	h.runSnack(n)
}

// runSnack waits out its time, checking each second whether the mouse
// is over it or the focus in it: then the time waits.
func (h *Host) runSnack(n *node) {
	s := n.snack
	s.stop = ownTimer(1000, func() {
		if s.done {
			return
		}
		if h.snackHeld(n) {
			return
		}
		s.left -= time.Second
		if s.left <= 0 {
			h.snackReport(n, ops.EventDismissed)
		}
	})
}

// snackHeld reports whether the mouse is over it or the keyboard's
// focus in it.
func (h *Host) snackHeld(n *node) bool {
	if f := Focus(); f != 0 && (f == n.hwnd || IsChild(n.hwnd, f)) {
		return true
	}
	p := CursorPos()
	r := WindowRect(n.hwnd)
	return p.X >= r.Left && p.X < r.Right && p.Y >= r.Top && p.Y < r.Bottom
}

func (h *Host) stopSnack(n *node) {
	s := n.snack
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
}

// snackTravel is how far it slides: from below the window's edge.
func (h *Host) snackTravel(n *node) int32 {
	return h.px(n, snackMinH+snackMargin+8)
}

// slideSnack slides it in (up to its place) or out (down past the
// window's edge, then it's gone), easing out, over snackSlide.
func (h *Host) slideSnack(n *node, out bool) {
	s := n.snack
	if s.slide != nil {
		s.slide()
	}
	s.out = out
	from := s.offset
	to := int32(0)
	if out {
		to = max(h.snackTravel(n), s.frame.H()+h.px(n, snackMargin))
	}
	s.slideAt = time.Now()
	s.slide = ownTimer(15, func() {
		t := min(float64(time.Since(s.slideAt))/float64(snackSlide), 1)
		e := 1 - math.Pow(1-t, 3) // ease out
		if out {
			e = t * t * t // ease in, as it leaves
		}
		s.offset = from + int32(math.Round(float64(to-from)*e))
		SetWindowPos(n.hwnd, 0, s.frame.Left, s.frame.Top+s.offset, 0, 0, SWP_NOZORDER|SWP_NOSIZE|SWP_NOACTIVATE)
		if t >= 1 {
			s.slide()
			s.slide = nil
			if out {
				h.dropSnack(n)
			}
		}
	})
}

// leaveSnack takes it out: sliding, or at once with animations off.
func (h *Host) leaveSnack(n *node) {
	s := n.snack
	if s.owner == 0 || s.out {
		return
	}
	if s.action != 0 {
		EnableWindow(s.action, false)
	}
	EnableWindow(s.close, false)
	if h.env.ReduceMotion || !IsWindowVisible(n.hwnd) {
		// Gone at once, after the message in hand (its button's click).
		s.out = true
		ShowWindow(n.hwnd, SW_HIDE)
		h.Post(func() { h.dropSnack(n) })
		return
	}
	h.slideSnack(n, true)
}

// dropSnack lets its window go, once it's out.
func (h *Host) dropSnack(n *node) {
	s := n.snack
	for i, x := range h.snacks {
		if x == n {
			h.snacks = append(h.snacks[:i], h.snacks[i+1:]...)
			break
		}
	}
	if s.slide != nil {
		s.slide()
		s.slide = nil
	}
	for _, b := range []uintptr{s.action, s.close} {
		if b != 0 {
			SetTooltip(h.tips, b, "")
			delete(h.byHwnd, b)
		}
	}
	if n.hwnd != 0 {
		delete(h.byHwnd, n.hwnd)
		DestroyWindow(n.hwnd)
		n.hwnd = 0
	}
}

// destroySnack is the core's Destroy: out, animated.
func (h *Host) destroySnack(n *node) {
	s := n.snack
	s.done = true
	h.stopSnack(n)
	if s.owner == 0 {
		h.dropSnack(n) // never shown
		return
	}
	h.leaveSnack(n)
}
