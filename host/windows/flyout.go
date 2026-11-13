//go:build windows

package windows

import (
	"math"

	"github.com/egoist/asuka/internal/flyout"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// Panels, flyouts, windows sized to their content and click-through
// windows (ops.WindowConfig's Panel, Flyout, FitContent, ClickThrough).
//
//   - A panel (a launcher's window) and a flyout are frameless popups
//     (WS_POPUP) and tool windows (WS_EX_TOOLWINDOW), of a class with the
//     drop shadow of menus, with Windows 11's rounded corners. Neither has
//     a taskbar button: a panel and a tray icon's flyout are owned by the
//     host's hidden window, as a window kept off the taskbar is
//     (winctl.go), and a flyout hanging from a rectangle of a window by
//     that window, which keeps it above. A panel is topmost (its options
//     say AlwaysOnTop), as is a tray icon's flyout, over other apps'
//     windows, and a flyout of a topmost window. Behind their content is
//     their background or the window color: DWM's system backdrops show
//     only where a window draws nothing, and every view of the host is a
//     child window that draws all of itself (Style.Material draws Fluent's
//     acrylic in a view).
//   - Either takes the keyboard as it shows (the key press of a global
//     shortcut lets the app take the foreground) and closes as the user
//     closes it (WM_CLOSE: CloseRequest while the core listens, else
//     Dismissed for a Window, else the main window's closing, which hides
//     it in an app that keeps running) on an Escape its content leaves (an
//     open combo box's list takes it) and as it loses the keyboard: to
//     another app, or to a window of the app's that isn't its own. Its own
//     are the windows it owns, through others in turn: a modal Window
//     declared in it, an alert or a file dialog over it (the host's
//     dialogs are owned by the window in use, the flyout then), a sheet, a
//     popover. While one of them is modal it's disabled, and stays open;
//     the dialog gives it the keyboard back as it closes. It closes once
//     the activation has settled (a posted message), and only if what's
//     active then isn't its own.
//   - A press on the tray icon a flyout hangs from takes the activation
//     to the taskbar: the flyout stays, and the click (NIN_SELECT) is the
//     app's OnClick, which toggles it; if it stays open, it gets the
//     keyboard back. A click on the window a flyout hangs from only closes
//     the flyout (MA_ACTIVATEANDEAT), as menus and WinUI's flyouts close,
//     so the button that opened it doesn't open it again.
//   - A panel shows centered across the work area of the monitor with the
//     cursor, a third of the way down (flyout.Center), each time it
//     shows, unless the app placed it (its X and Y, SetPosition, Center,
//     or where the user left it under a StateKey).
//   - A flyout is placed against its anchor as it shows, as its Flyout
//     prop changes and as its size does (flyout.Place), in the work area
//     of the anchor's monitor, at that monitor's DPI: a tray icon's
//     rectangle (Shell_NotifyIconGetRect; where Windows doesn't say, the
//     cursor), or a rectangle of its parent's content.
//   - A window sized to its content takes the size WindowAction Fit
//     gives, keeping its top left (a flyout is placed again). One that
//     opens shown waits for its first Fit, which comes in the frames'
//     batch of the flush that opens it, so it never shows at its Config
//     size first.
//   - A click-through window is layered and transparent to the mouse
//     (WS_EX_LAYERED with WS_EX_TRANSPARENT, opaque): Windows hands its
//     clicks to what's under it, another app's window too.

const (
	classFlyout = "AsukaFlyout" // panels' and flyouts' windows: classWindow's, with a drop shadow

	wmLightDismiss  = WM_APP + 0x60 // a panel or flyout lost the keyboard: it closes if it's still away
	wmFlyoutRefocus = WM_APP + 0x61 // its tray icon was clicked: it gets the keyboard back if it stays
)

// frameless reports whether the window is a panel or a flyout.
func (c *wctl) frameless() bool { return c.has(ops.WindowPanel | ops.WindowFlyout) }

// anchor is a flyout's Flyout prop; ok is false for a window that's no
// flyout, or has no anchor yet.
func (c *wctl) anchor() (ops.Flyout, bool) {
	if !c.has(ops.WindowFlyout) || c.n == nil || c.n.nav == nil || !c.n.nav.hasFlyout {
		return ops.Flyout{}, false
	}
	return c.n.nav.flyout, true
}

// exStyle is the extended style a Window's window is made with for its
// options: a tool window, topmost for a tray icon's flyout and a flyout
// of a topmost window.
func (h *Host) exStyle(c *wctl) uint32 {
	var ex uint32
	if c.frameless() {
		ex |= WS_EX_TOOLWINDOW
	}
	if f, ok := c.anchor(); ok && f.Tray != 0 {
		ex |= WS_EX_TOPMOST
	}
	if c.owner != 0 && WindowLong(c.owner, GWL_EXSTYLE)&WS_EX_TOPMOST != 0 {
		ex |= WS_EX_TOPMOST // above a topmost owner: a tray flyout's dialog
	}
	return ex
}

// configureFrameless gives a panel or a flyout its look, the main
// window's too (made before its options came): a tool window, rounded
// corners, no menu bar; and a click-through window its styles.
func (h *Host) configureFrameless(c *wctl) {
	if c.frameless() {
		SetWindowLong(c.hwnd, GWL_EXSTYLE, WindowLong(c.hwnd, GWL_EXSTYLE)|WS_EX_TOOLWINDOW)
		SetCornerPreference(c.hwnd, DWMWCP_ROUND)
		if c.main && GetMenu(c.hwnd) != 0 {
			SetMenu(c.hwnd, 0)
		}
		if f, ok := c.anchor(); ok && f.Tray != 0 {
			SetWindowPos(c.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
		}
	}
	if c.has(ops.WindowClickThrough) {
		h.setClickThrough(c, true)
	}
}

// setClickThrough lets the mouse through the window, or gives it the
// mouse again: layered (opaque) and transparent to it, or neither.
func (h *Host) setClickThrough(c *wctl, on bool) {
	ex := WindowLong(c.hwnd, GWL_EXSTYLE)
	const through = WS_EX_LAYERED | WS_EX_TRANSPARENT
	if on == (ex&through == through) {
		return
	}
	if on {
		SetWindowLong(c.hwnd, GWL_EXSTYLE, ex|through)
		SetLayeredOpaque(c.hwnd)
		return
	}
	SetWindowLong(c.hwnd, GWL_EXSTYLE, ex&^through)
	RedrawWindow(c.hwnd, RDW_INVALIDATE|RDW_ERASE|RDW_FRAME|RDW_ALLCHILDREN)
}

// clickThrough reports whether the mouse goes through the window.
func clickThrough(hwnd uintptr) bool {
	const through = WS_EX_LAYERED | WS_EX_TRANSPARENT
	return WindowLong(hwnd, GWL_EXSTYLE)&through == through
}

// beforeShow places a panel or a flyout as it's about to show.
func (h *Host) beforeShow(c *wctl) {
	switch {
	case c.has(ops.WindowFlyout):
		h.placeFlyout(c)
	case c.has(ops.WindowPanel) && !c.appPlaced && !c.has(ops.WindowPositioned):
		h.centerPanel(c)
	}
}

// contentSize is the size the window's content is to have, in points:
// its last Fit's, else its client area's.
func (h *Host) contentSize(c *wctl) (float32, float32) {
	if c.fitted {
		return c.fit[0], c.fit[1]
	}
	k := c.k(h)
	cr := ClientRect(c.hwnd)
	return float32(cr.W()) / k, float32(cr.H()) / k
}

// takeDPI moves the window onto the monitor whose work area is work,
// at dpi, before it's placed there: its views at that scale, so that it's
// sized in that monitor's pixels.
func (h *Host) takeDPI(c *wctl, work Rect, dpi int) {
	if dpiScale(dpi) == c.k(h) {
		return
	}
	SetWindowPos(c.hwnd, 0, work.Left, work.Top, 0, 0, SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
	// WM_DPICHANGED took it there, unless Windows sent none (a hidden
	// window can be left at the DPI it had).
	now := DPI(c.hwnd)
	if dpiScale(now) == c.k(h) {
		return
	}
	switch {
	case c.main:
		h.dpiChanged(now, WindowRect(c.hwnd))
	case c.frame != nil:
		h.windowDPIChanged(c.frame, now, WindowRect(c.hwnd))
	}
}

// centerPanel puts a panel on the monitor with the cursor, centered
// across its work area, a third of the way down.
func (h *Host) centerPanel(c *wctl) {
	if c.full != nil || IsZoomed(c.hwnd) || IsIconic(c.hwnd) {
		return
	}
	_, work, dpi := MonitorAt(CursorPos())
	h.takeDPI(c, work, dpi)
	k := c.k(h)
	w, ht := h.contentSize(c)
	wr, cr := WindowRect(c.hwnd), ClientRect(c.hwnd)
	fw, fh := wr.W()-cr.W(), wr.H()-cr.H() // none, for a frameless panel
	r := flyout.Center(float64(round(w*k)+fw), float64(round(ht*k)+fh), rectF(work))
	setRect(c.hwnd, r)
}

// placeFlyout puts a flyout against its anchor, at its content's size, in
// the work area of the anchor's monitor.
func (h *Host) placeFlyout(c *wctl) {
	f, ok := c.anchor()
	if !ok || c.hwnd == 0 || !IsWindow(c.hwnd) {
		return
	}
	a := h.anchorRect(c, f)
	work, dpi := MonitorOfRect(a)
	h.takeDPI(c, work, dpi)
	k := c.k(h)
	w, ht := h.contentSize(c)
	r := flyout.Place(rectF(a), float64(round(w*k)), float64(round(ht*k)), f.Placement, float64(f.Gap*k), rectF(work))
	setRect(c.hwnd, r)
}

// anchorRect is a flyout's anchor on the screen, in pixels: its tray
// icon's rectangle (the cursor where Windows doesn't say where it is), or
// its rectangle of its parent's content.
func (h *Host) anchorRect(c *wctl, f ops.Flyout) Rect {
	if f.Tray != 0 {
		if r, ok := h.trayIconRect(f.Tray); ok {
			return r
		}
		p := CursorPos()
		return Rect{Left: p.X, Top: p.Y, Right: p.X, Bottom: p.Y}
	}
	content, k := h.parentContent(c.cfg.Parent)
	if content == 0 {
		p := CursorPos()
		return Rect{Left: p.X, Top: p.Y, Right: p.X, Bottom: p.Y}
	}
	o := WindowRect(content) // screen pixels, also in a mirrored window
	x, y := o.Left+round(f.X*k), o.Top+round(f.Y*k)
	return Rect{Left: x, Top: y, Right: x + round(f.Width*k), Bottom: y + round(f.Height*k)}
}

// parentContent is the content window of the node a flyout belongs to (a
// Window's, or the root's) and its pixels per point.
func (h *Host) parentContent(id ops.NodeID) (uintptr, float32) {
	if p := h.nodes[id]; p != nil && p.kind == ops.KindWindow && p.nav != nil {
		if pc := p.nav.ctl; pc != nil && p.hwnd != 0 {
			return p.hwnd, pc.k(h)
		}
	}
	if h.root != nil && h.root.hwnd != 0 {
		return h.root.hwnd, h.scale
	}
	return 0, h.scale
}

// trayIconRect is the rectangle of the tray icon id (asuka/sdk/tray's), as
// the notification area shows it.
func (h *Host) trayIconRect(id uint32) (Rect, bool) {
	s := h.tray
	if s == nil {
		return Rect{}, false
	}
	t := s.icons[id]
	if t == nil || !t.added {
		return Rect{}, false
	}
	return NotifyIconRect(s.hwnd, id)
}

func rectF(r Rect) flyout.Rect {
	return flyout.Rect{X: float64(r.Left), Y: float64(r.Top), W: float64(r.W()), H: float64(r.H())}
}

// setRect gives a window a rectangle flyout computed, in pixels.
func setRect(hwnd uintptr, r flyout.Rect) {
	x, y := int32(math.Round(r.X)), int32(math.Round(r.Y))
	w, ht := int32(math.Round(r.W)), int32(math.Round(r.H))
	SetWindowPos(hwnd, 0, x, y, w, ht, SWP_NOZORDER|SWP_NOACTIVATE)
}

// fitWindow sizes the window to its content as WindowAction Fit says: a
// flyout placed again against its anchor, others keeping their top left.
// One waiting for its first Fit shows now.
func (h *Host) fitWindow(c *wctl, w, ht float32) {
	c.fit, c.fitted = [2]float32{w, ht}, true
	if c.full == nil && !IsZoomed(c.hwnd) && !IsIconic(c.hwnd) {
		if c.has(ops.WindowFlyout) {
			h.placeFlyout(c)
		} else {
			h.sizeContent(c, w, ht)
		}
	}
	h.fitArrived(c)
}

// fitArrived shows a window that waited for its size, if it was to show.
func (h *Host) fitArrived(c *wctl) {
	if !c.waitFit {
		return
	}
	c.waitFit = false
	if c.showPending {
		c.showPending = false
		h.showCtl(c, c.showActivate)
	}
}

// lightDismisses reports whether the window closes as it loses the
// keyboard: a panel's or a flyout's, open.
func (c *wctl) lightDismisses() bool {
	return c.frameless() && c.hwnd != 0 && IsWindowVisible(c.hwnd)
}

// ownsActive reports whether the window, or one of its own (a dialog, a
// modal Window, a sheet or a popover over it), has the keyboard.
func (c *wctl) ownsActive() bool {
	fg := ForegroundWindow()
	return fg != 0 && OwnedBy(fg, c.hwnd)
}

// lightDismissMessage handles what closes a panel or a flyout as it
// loses the keyboard, and what keeps it; and the click on a window a
// flyout hangs from, which only closes the flyout. ok false lets the
// rest of the window's handling run.
func (h *Host) lightDismissMessage(c *wctl, m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case WM_ACTIVATE:
		if LoWord(wp) == WA_INACTIVE {
			h.mayDismiss(c, lp)
		}
	case WM_ACTIVATEAPP:
		if wp == 0 { // another app's window is activated
			h.mayDismiss(c, 0)
		}
	case WM_MOUSEACTIVATE:
		if LoWord(lp) == HTCLIENT && h.hasActiveFlyout(c.hwnd) {
			return MA_ACTIVATEANDEAT, true
		}
	case wmLightDismiss:
		c.dismissPending = false
		if c.lightDismisses() && IsWindowEnabled(c.hwnd) && !c.ownsActive() {
			h.closeAsUser(c)
		}
		return 0, true
	case wmFlyoutRefocus:
		if c.lightDismisses() && IsWindowEnabled(c.hwnd) && !c.ownsActive() && !snapshotting() {
			SetForegroundWindow(c.hwnd)
		}
		return 0, true
	}
	return 0, false
}

// mayDismiss closes a panel or a flyout that lost the keyboard to next (0:
// another app's window, or none), once the activation has settled: unless
// next is its own, a window modal to it keeps it, or the press was on the
// tray icon it hangs from (whose click toggles it).
func (h *Host) mayDismiss(c *wctl, next uintptr) {
	if !c.lightDismisses() || c.dismissPending {
		return
	}
	if next != 0 && OwnedBy(next, c.hwnd) || !IsWindowEnabled(c.hwnd) || h.pressedOnAnchor(c) {
		return
	}
	c.dismissPending = true
	PostMessage(c.hwnd, wmLightDismiss, 0, 0)
}

// pressedOnAnchor reports whether a mouse button is down on the tray icon
// a flyout hangs from.
func (h *Host) pressedOnAnchor(c *wctl) bool {
	f, ok := c.anchor()
	if !ok || f.Tray == 0 || !MouseButtonDown() {
		return false
	}
	r, ok := h.trayIconRect(f.Tray)
	p := CursorPos()
	return ok && p.X >= r.Left && p.X < r.Right && p.Y >= r.Top && p.Y < r.Bottom
}

// windowActivated follows one of the app's top-level windows being
// activated: the panels and flyouts it isn't one of the own windows of
// have lost the keyboard (one of their own had it, as a popover over a
// flyout, so they had none to lose).
func (h *Host) windowActivated(hwnd uintptr) {
	for _, c := range h.ctls {
		if c.hwnd != hwnd && c.lightDismisses() && !OwnedBy(hwnd, c.hwnd) {
			h.mayDismiss(c, hwnd)
		}
	}
}

// hasActiveFlyout reports whether a flyout hanging from a rectangle of
// the window hwnd is open with the keyboard, which a click on hwnd closes.
func (h *Host) hasActiveFlyout(hwnd uintptr) bool {
	for _, c := range h.ctls {
		if c.has(ops.WindowFlyout) && c.owner == hwnd && c.lightDismisses() && IsWindowEnabled(c.hwnd) && c.ownsActive() {
			return true
		}
	}
	return false
}

// closeAsUser closes a window as the user's closing it does: its close
// button's path (CloseRequest, Dismissed, the main window's closing).
func (h *Host) closeAsUser(c *wctl) {
	if c.hwnd != 0 && IsWindow(c.hwnd) {
		SendMessage(c.hwnd, WM_CLOSE, 0, 0)
	}
}

// escapeDismisses closes the panel or the flyout an Escape is for, unless
// its content takes it (an open combo box's list closes).
func (h *Host) escapeDismisses(m *Msg) bool {
	c := h.ctls[RootWindow(m.Hwnd)]
	if c == nil || !c.lightDismisses() || m.Message != WM_KEYDOWN {
		return false
	}
	if SendMessage(m.Hwnd, CB_GETDROPPEDSTATE, 0, 0) != 0 {
		return false
	}
	h.closeAsUser(c)
	return true
}

// trayClicked follows a click on tray icon id: a flyout hanging from it
// that the click leaves open gets the keyboard back, once the app's
// OnClick has run.
func (h *Host) trayClicked(id uint32) {
	for _, c := range h.ctls {
		if f, ok := c.anchor(); ok && f.Tray == id && c.lightDismisses() {
			PostMessage(c.hwnd, wmFlyoutRefocus, 0, 0)
		}
	}
}

// centerOver centers a window over another (a dialog over a panel or a
// flyout), within that one's monitor's work area.
func (h *Host) centerOver(c *wctl, over uintptr) {
	o, wr := WindowRect(over), WindowRect(c.hwnd)
	work := MonitorWorkArea(o)
	x := o.Left + (o.W()-wr.W())/2
	y := o.Top + (o.H()-wr.H())/2
	x = min(max(x, work.Left), max(work.Right-wr.W(), work.Left))
	y = min(max(y, work.Top), max(work.Bottom-wr.H(), work.Top))
	SetWindowPos(c.hwnd, 0, x, y, 0, 0, SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
}

// isFrameless reports whether hwnd is a panel's or a flyout's window.
func (h *Host) isFrameless(hwnd uintptr) bool {
	c := h.ctls[hwnd]
	return c != nil && c.frameless()
}
