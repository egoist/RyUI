//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// The app's windows (ui.Window): each a top-level window of its own,
// resizable unless its options say otherwise (winctl.go), with its own
// title and taskbar button, its content (the Window node's child) laid
// out at its client size. Closing it asks the core (Dismissed, or
// CloseRequest while the core listens for that), which destroys it, or
// keeps it. Each has its own scale, its DPI:
// one opens at the DPI of the monitor it opens on, and one moved to a
// monitor of another DPI takes the size Windows suggests and draws its
// views at the new scale (WM_DPICHANGED), its Environment reported again.

// openWindow opens a Window's window with its options (winctl.go): its
// content's size (the main window's default if the app gives none), its
// abilities and title bar; where it was left (its StateKey), at its X and
// Y, centered on the window in use's monitor, or placed down and right of
// the window in use, as new windows cascade. A child is owned by its
// parent's window, a modal one disabling it. It shows once the main window
// does (showMain), unless it opens hidden.
func (h *Host) openWindow(n *node) {
	if n.nav.window != 0 {
		return
	}
	cfg := n.nav.cfg
	w, ht := n.nav.size[0], n.nav.size[1]
	if w <= 0 {
		w = defaultWindowWidth
	}
	if ht <= 0 {
		ht = defaultWindowHeight
	}
	c := &wctl{n: n, cfg: cfg, key: cfg.StateKey}
	if n.nav.fitted {
		// Its content was laid out before it opened (flyout.go).
		c.fit, c.fitted = [2]float32{w, ht}, true
	}
	c.waitFit = c.has(ops.WindowFitContent) && !c.fitted
	style := c.style()
	from := h.windowInUse()
	dpi := DPI(from)
	r := frameRect(w, ht, style, dpi)
	x, y := h.cascade(r.W(), r.H())
	if cfg.Flags&ops.WindowChild != 0 {
		c.owner = h.parentWindow(cfg.Parent)
	} else if f, ok := c.anchor(); ok && f.Tray == 0 {
		c.owner = h.parentWindow(cfg.Parent) // a flyout stays above the window it hangs from
	}
	class := classWindow
	if c.frameless() {
		class = classFlyout // a drop shadow (flyout.go)
	}
	win := CreateWindow(h.topLevelStyle()|h.exStyle(c), class, h.windowTitle(n), style, x, y, r.W(), r.H(), c.owner, 0)
	c.hwnd = win
	fw := newNode(0, kindWindowFrame)
	fw.hwnd = win
	fw.nav = &nav{}
	fw.parent = n
	fw.k = dpiScale(DPI(win)) // the DPI of the monitor it opened on
	c.frame = fw
	h.byHwnd[win] = fw
	h.ctls[win] = c
	n.nav.ctl = c
	h.acceptDrops(win)
	SetDarkTitleBar(win, h.dark)
	n.nav.window = win
	n.presented = true
	SetParent(n.hwnd, win)
	h.rescale(n.hwnd, fw.k)
	h.configure(c)
	h.setWindowMenu(c) // before its size: the menu bar takes from its client area
	cr := ClientRect(win)
	SetWindowPos(n.hwnd, 0, 0, 0, cr.W(), cr.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	h.sizeContent(c, w, ht) // its frame as configured (a hidden title bar's), at its DPI
	if !h.restorePlacement(c) {
		switch {
		case cfg.Flags&ops.WindowPositioned != 0:
			h.moveTo(c, cfg.X, cfg.Y)
		case c.owner != 0 && h.isFrameless(c.owner):
			h.centerOver(c, c.owner) // a dialog of a panel or a flyout, over it (flyout.go)
		case cfg.Flags&ops.WindowCentered != 0:
			h.center(c, from)
		}
	}
	h.wins = append(h.wins, n)
	cr = ClientRect(win)
	h.reportScreen(n, h.pt(n, cr.W()), h.pt(n, cr.H()))
	if cfg.Flags&ops.WindowModal != 0 && c.owner != 0 {
		EnableWindow(c.owner, false)
	}
	if h.started && cfg.Flags&ops.WindowHidden == 0 {
		h.showCtl(c, true)
	}
	id := n.id
	h.afterCore(func() {
		if n := h.nodes[id]; n != nil && n.nav.ctl == c {
			h.fitArrived(c) // in case no Fit came: it shows at its size
			h.reportState(c)
		}
	})
}

// dpiScale is the pixels per point at a DPI.
func dpiScale(dpi int) float32 { return float32(dpi) / 96 }

// frameRect is the size of a window of style whose client area is w by
// ht points at dpi.
func frameRect(w, ht float32, style uint32, dpi int) Rect {
	k := float64(dpiScale(dpi))
	r := Rect{Right: int32(math.Round(float64(w) * k)), Bottom: int32(math.Round(float64(ht) * k))}
	AdjustWindowRect(&r, style, 0, dpi)
	return r
}

// windowDPIChanged follows a Window or a sheet (f, its top-level
// window's node) to a monitor of another DPI: its views at the new scale
// (their fonts, their frames in pixels), the size Windows suggests, and
// its Environment reported again, so the core measures and lays it out
// again.
func (h *Host) windowDPIChanged(f *node, dpi int, s Rect) {
	n := f.parent
	f.k = dpiScale(dpi)
	if n != nil && n.hwnd != 0 {
		h.rescale(n.hwnd, f.k)
	}
	SetWindowPos(f.hwnd, 0, s.Left, s.Top, s.W(), s.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	if n != nil && n.hwnd != 0 {
		cr := ClientRect(f.hwnd)
		SetWindowPos(n.hwnd, 0, 0, 0, cr.W(), cr.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		h.reportScreen(n, h.pt(n, cr.W()), h.pt(n, cr.H()))
	}
	RedrawWindow(f.hwnd, RDW_INVALIDATE|RDW_ALLCHILDREN|RDW_ERASE)
}

func (h *Host) windowTitle(n *node) string {
	t := n.title
	if t == "" {
		t = h.appName
	}
	if n.nav != nil && n.nav.edited {
		t = "*" + t
	}
	return t
}

// cascade places a window of w by ht pixels down and right of the window
// in use (down and left, right to left), past the app's windows already
// there, inside the work area.
func (h *Host) cascade(w, ht int32) (x, y int32) {
	from := h.windowInUse()
	wr := WindowRect(from)
	work := WorkArea(from)
	step := int32(32 * DPI(from) / 96)
	x, y = wr.Left+step, wr.Top+step
	across := step
	if h.rtl {
		x, across = wr.Right-step-w, -step
	}
	for range 32 {
		taken := false
		for _, o := range h.topWindows() {
			if r := WindowRect(o); r.Left == x && r.Top == y {
				taken = true
				break
			}
		}
		if !taken {
			break
		}
		x, y = x+across, y+step
	}
	if x+w > work.Right {
		x = max(work.Left, work.Right-w)
	}
	x = max(x, work.Left)
	if y+ht > work.Bottom {
		y = max(work.Top, work.Bottom-ht)
	}
	return x, y
}

// topWindows are the app's own windows: the main one and the Windows'.
func (h *Host) topWindows() []uintptr {
	out := []uintptr{h.window}
	for _, n := range h.wins {
		out = append(out, n.nav.window)
	}
	return out
}

// windowInUse is the app's window the user works in: the one activated
// last, else the main one; if sheets are over it, the top one.
func (h *Host) windowInUse() uintptr {
	w := h.window
	if u := h.inUse; u != 0 && IsWindow(u) && h.keyWindow(u) == u {
		w = u
	}
	for over := true; over; {
		over = false
		for i := len(h.sheets) - 1; i >= 0; i-- {
			if s := h.sheets[i]; s.nav.owner == w && s.nav.window != 0 {
				w, over = s.nav.window, true
				break
			}
		}
	}
	return w
}

// rootOf is the node a top-level window of the app's shows: the root, a
// sheet, a Window.
func (h *Host) rootOf(w uintptr) *node {
	if w == h.window {
		return h.root
	}
	if f := h.byHwnd[w]; f != nil && (f.kind == kindSheetWindow || f.kind == kindWindowFrame) {
		return f.parent
	}
	return nil
}

// closeWindow closes a Window's window, and the sheets over it: its
// placement kept first, and a modal one's owner enabled again before it
// goes, so that the owner is activated.
func (h *Host) closeWindow(n *node) {
	win := n.nav.window
	if win == 0 {
		return
	}
	c := n.nav.ctl
	for i, w := range h.wins {
		if w == n {
			h.wins = append(h.wins[:i], h.wins[i+1:]...)
			break
		}
	}
	for i := len(h.sheets) - 1; i >= 0; i-- {
		if s := h.sheets[i]; s.nav.owner == win {
			h.closeSheet(s)
			h.emit(s, ops.Event{Kind: ops.EventDismissed})
		}
	}
	if c != nil {
		h.savePlacement(c)
		if c.cfg.Flags&ops.WindowModal != 0 && c.owner != 0 && IsWindow(c.owner) {
			EnableWindow(c.owner, true)
		}
		h.forgetChrome(c)
		delete(h.ctls, win)
		n.nav.ctl = nil
	}
	// The window it belongs to (or the main one) is the one in use next,
	// not another app's.
	// A panel or a tray icon's flyout belongs to no window of the app's:
	// the activation goes where Windows takes it, another app's window.
	if ForegroundWindow() == win && !snapshotting() && !(c != nil && c.frameless() && c.owner == 0) {
		next := h.window
		if c != nil && c.owner != 0 && c.owner != h.park && IsWindow(c.owner) {
			next = c.owner
		}
		SetForegroundWindow(next)
	}
	SetParent(n.hwnd, h.park)
	h.forgetTree(win)
	if IsWindow(win) {
		DestroyWindow(win)
	}
	n.nav.window = 0
	if h.inUse == win {
		h.inUse = 0
	}
}

func (h *Host) windowFrameMessage(fw *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	n := fw.parent
	if c := h.ctls[fw.hwnd]; c != nil {
		if r, ok := h.ctlMessage(c, m, wp, lp); ok {
			return r, true
		}
	}
	switch m {
	case WM_SIZE:
		if n != nil && n.hwnd != 0 {
			cr := ClientRect(fw.hwnd)
			SetWindowPos(n.hwnd, 0, 0, 0, cr.W(), cr.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
			h.reportScreen(n, h.pt(n, cr.W()), h.pt(n, cr.H()))
		}
		return 0, true
	case WM_ERASEBKGND:
		FillColor(wp, ClientRect(fw.hwnd), h.bgOf(fw.hwnd).ref())
		return 1, true
	case WM_PRINTCLIENT:
		FillColor(wp, ClientRect(fw.hwnd), h.bgOf(fw.hwnd).ref())
		return 0, true
	case WM_DPICHANGED:
		h.windowDPIChanged(fw, int(LoWord(wp)), *Native[Rect](lp))
		return 0, true
	case WM_ACTIVATE:
		if LoWord(wp) != 0 {
			h.inUse = fw.hwnd
		}
		return 0, false
	case WM_CLOSE:
		// The core closes it.
		if n != nil && !n.dying {
			h.emit(n, ops.Event{Kind: ops.EventDismissed})
		}
		return 0, true
	}
	return 0, false
}

// setWindowTitle is the main window's title; "": the app's name.
func (h *Host) setWindowTitle(title string) {
	if title == "" {
		title = h.appName
	}
	SetWindowText(h.window, title)
}

// windowOptions applies WindowOptions to the main window, before it
// shows: its title, its least and most sizes, its abilities and title bar
// (winctl.go), its client size, centered in the work area or at its X and
// Y; where it was left (its StateKey, "main" by default), restored as it
// shows, wins over them.
func (h *Host) windowOptions(n *node, args []byte) {
	cfg := ops.DecodeWindowConfig(args)
	c := h.mainCtl
	c.n, c.cfg, c.key = n, cfg, mainKey(&cfg)
	if c.frameless() && cfg.StateKey == "" {
		c.key = "" // a panel shows where the cursor is, unless the app keeps its place
	}
	if c.has(ops.WindowFitContent) && !c.fitted {
		c.waitFit = true // shown once its content is laid out (flyout.go)
		h.afterCore(func() { h.fitArrived(c) })
	}
	h.minSize = [2]float32{cfg.MinWidth, cfg.MinHeight}
	if title := cfg.Title; title != "" {
		h.setWindowTitle(title)
	}
	h.configure(c)
	w, ht := cfg.Width, cfg.Height
	cr := ClientRect(h.window)
	if w <= 0 {
		w = h.pt(nil, cr.W())
	}
	if ht <= 0 {
		ht = h.pt(nil, cr.H())
	}
	w, ht = h.clampSize(c, w, ht)
	// The frame as it is (a menu bar too, or no caption), around the new
	// client area.
	wr := WindowRect(h.window)
	fw, fh := wr.W()-cr.W(), wr.H()-cr.H()
	pw, ph := h.px(nil, w)+fw, h.px(nil, ht)+fh
	work := WorkArea(h.window)
	ph = min(ph, work.H())
	pw = min(pw, work.W())
	SetWindowPos(h.window, 0, work.Left+(work.W()-pw)/2, work.Top+(work.H()-ph)/2, pw, ph, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	if cfg.Flags&ops.WindowPositioned != 0 {
		h.moveTo(c, cfg.X, cfg.Y)
	}
	h.environmentChanged() // a hidden title bar's room

}
