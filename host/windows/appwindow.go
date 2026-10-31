//go:build windows && amd64

package windows

import (
	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

// The app's windows (ui.Window): each a top-level window of its own,
// resizable, with its own title and taskbar button, its content (the
// Window node's child) laid out at its client size. Closing it asks the
// core (Dismissed), which destroys it. The app has one scale, the main
// window's DPI: a Window on a monitor of another DPI takes the size
// Windows suggests, its content drawn at the app's scale.

// openWindow opens a Window's window: its content's size (480 by 600
// points if the app gives none), placed down and right of the window in
// use, as new windows cascade. It shows once the main window does.
func (h *Host) openWindow(n *node) {
	if n.nav.window != 0 {
		return
	}
	w, ht := n.nav.size[0], n.nav.size[1]
	if w <= 0 {
		w = 480
	}
	if ht <= 0 {
		ht = 600
	}
	style := uint32(WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN)
	r := Rect{Right: h.px(w), Bottom: h.px(ht)}
	AdjustWindowRect(&r, style, 0, h.dpi)
	x, y := h.cascade(r.W(), r.H())
	win := CreateWindow(0, classWindow, h.windowTitle(n), style, x, y, r.W(), r.H(), 0, 0)
	fw := newNode(0, kindWindowFrame)
	fw.hwnd = win
	fw.nav = &nav{}
	fw.parent = n
	h.byHwnd[win] = fw
	SetDarkTitleBar(win, h.dark)
	n.nav.window = win
	n.presented = true
	SetParent(n.hwnd, win)
	cr := ClientRect(win)
	SetWindowPos(n.hwnd, 0, 0, 0, cr.W(), cr.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	h.wins = append(h.wins, n)
	h.reportScreen(n, h.pt(cr.W()), h.pt(cr.H()))
	if IsWindowVisible(h.window) {
		h.showWindow(win)
	}
}

func (h *Host) showWindow(win uintptr) {
	if snapshotting() {
		ShowWindow(win, SW_SHOWNOACTIVATE)
	} else {
		ShowWindow(win, SW_SHOW)
	}
}

func (h *Host) windowTitle(n *node) string {
	if n.title != "" {
		return n.title
	}
	return h.appName
}

// cascade places a window of w by ht pixels down and right of the window
// in use, past the app's windows already there, inside the work area.
func (h *Host) cascade(w, ht int32) (x, y int32) {
	from := h.windowInUse()
	wr := WindowRect(from)
	work := WorkArea(from)
	step := h.px(32)
	x, y = wr.Left+step, wr.Top+step
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
		x, y = x+step, y+step
	}
	if x+w > work.Right {
		x = max(work.Left, work.Right-w)
	}
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

// closeWindow closes a Window's window, and the sheets over it.
func (h *Host) closeWindow(n *node) {
	win := n.nav.window
	if win == 0 {
		return
	}
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
	// The main window is the one in use next, not another app's.
	if ForegroundWindow() == win && !snapshotting() {
		SetForegroundWindow(h.window)
	}
	SetParent(n.hwnd, h.park)
	h.forgetTree(win)
	DestroyWindow(win)
	n.nav.window = 0
	if h.inUse == win {
		h.inUse = 0
	}
}

func (h *Host) windowFrameMessage(fw *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	n := fw.parent
	switch m {
	case WM_SIZE:
		if n != nil && n.hwnd != 0 {
			cr := ClientRect(fw.hwnd)
			SetWindowPos(n.hwnd, 0, 0, 0, cr.W(), cr.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
			h.reportScreen(n, h.pt(cr.W()), h.pt(cr.H()))
		}
		return 0, true
	case WM_ERASEBKGND:
		FillColor(wp, ClientRect(fw.hwnd), h.theme.bg.ref())
		return 1, true
	case WM_PRINTCLIENT:
		FillColor(wp, ClientRect(fw.hwnd), h.theme.bg.ref())
		return 0, true
	case WM_GETMINMAXINFO:
		if n != nil && (n.nav.minSize[0] > 0 || n.nav.minSize[1] > 0) {
			h.minTrackSize(fw.hwnd, n.nav.minSize, Native[MinMaxInfo](lp))
		}
		return 0, true
	case WM_DPICHANGED:
		s := *Native[Rect](lp)
		SetWindowPos(fw.hwnd, 0, s.Left, s.Top, s.W(), s.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
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

// minTrackSize is the least a window can be sized to: its client area
// least points (a side 0: the system's least), at the app's scale, its
// frame and menu bar at its own DPI.
func (h *Host) minTrackSize(hwnd uintptr, least [2]float32, mi *MinMaxInfo) {
	dpi := DPI(hwnd)
	r := Rect{Right: h.px(least[0]), Bottom: h.px(least[1])}
	AdjustWindowRect(&r, uint32(WindowLong(hwnd, GWL_STYLE)), 0, dpi)
	if GetMenu(hwnd) != 0 {
		r.Bottom += int32(SystemMetric(SM_CYMENU, dpi))
	}
	if least[0] > 0 {
		mi.MinTrackSize.X = max(mi.MinTrackSize.X, r.W())
	}
	if least[1] > 0 {
		mi.MinTrackSize.Y = max(mi.MinTrackSize.Y, r.H())
	}
}

// windowOptions applies WindowOptions to the main window: its title, its
// least size, and its client size, centered in the work area (the host
// doesn't remember the user's).
func (h *Host) windowOptions(args []byte) {
	if len(args) < 16 {
		return
	}
	w, ht := f32(args, 0), f32(args, 4)
	h.minSize = [2]float32{f32(args, 8), f32(args, 12)}
	if title := string(args[16:]); title != "" {
		SetWindowText(h.window, title)
	}
	if w <= 0 && ht <= 0 {
		return
	}
	cr := ClientRect(h.window)
	if w <= 0 {
		w = h.pt(cr.W())
	}
	if ht <= 0 {
		ht = h.pt(cr.H())
	}
	w, ht = max(w, h.minSize[0]), max(ht, h.minSize[1])
	// The frame as it is (a menu bar too), around the new client area.
	wr := WindowRect(h.window)
	fw, fh := wr.W()-cr.W(), wr.H()-cr.H()
	pw, ph := h.px(w)+fw, h.px(ht)+fh
	work := WorkArea(h.window)
	ph = min(ph, work.H())
	pw = min(pw, work.W())
	SetWindowPos(h.window, 0, work.Left+(work.W()-pw)/2, work.Top+(work.H()-ph)/2, pw, ph, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
}
