//go:build windows && amd64

package windows

import (
	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// A Popover is a popup window over its anchor, as WinUI's Flyout: below
// the anchor and centered on it (above it where the monitor has no room),
// with Windows 11's rounded corners and border, light dismissed: clicking
// elsewhere (it loses activation) or Escape reports Dismissed, or Back
// while ui.OnBack in it intercepts.

func (h *Host) setPopoverProp(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropPopoverAnchor:
		n.nav.anchor = ops.NodeID(v.Int)
	case ops.PropPopoverWidth:
		n.nav.popW = float32(v.Float)
	case ops.PropPopoverHeight:
		n.nav.popH = float32(v.Float)
	default:
		return
	}
	if n.nav.window != 0 {
		h.placePopover(n)
	}
}

func (h *Host) openPopover(n *node) {
	a := h.nodes[n.nav.anchor]
	if n.nav.window != 0 || a == nil || a.hwnd == 0 {
		return
	}
	owner := RootWindow(a.hwnd)
	win := CreateWindow(WS_EX_TOOLWINDOW|h.topLevelStyle(), classWindow, "", WS_POPUP|WS_CLIPCHILDREN, 0, 0, 1, 1, owner, 0)
	SetCornerPreference(win, 2) // round
	pw := newNode(0, kindPopoverWindow)
	pw.hwnd = win
	pw.nav = &nav{}
	pw.parent = n
	pw.k = dpiScale(DPI(win))
	h.byHwnd[win] = pw
	n.nav.window, n.nav.owner = win, owner
	n.presented = true
	SetParent(n.hwnd, win)
	h.rescale(n.hwnd, pw.k)
	h.placePopover(n)
	if snapshotting() {
		ShowWindow(win, SW_SHOWNOACTIVATE)
	} else {
		ShowWindow(win, SW_SHOW)
	}
}

// placePopover sizes the popover to its content and puts it by its anchor.
func (h *Host) placePopover(n *node) {
	win := n.nav.window
	a := h.nodes[n.nav.anchor]
	if win == 0 || a == nil {
		return
	}
	k := dpiScale(DPI(win))
	w, ht := int32(n.nav.popW*k+0.5), int32(n.nav.popH*k+0.5)
	ar := WindowRect(a.hwnd)
	gap := int32(4 * k)
	x := ar.Left + (ar.W()-w)/2
	y := ar.Bottom + gap
	wa := MonitorWorkArea(ar)
	if y+ht > wa.Bottom && ar.Top-gap-ht >= wa.Top {
		y = ar.Top - gap - ht
	}
	x = min(max(x, wa.Left), max(wa.Right-w, wa.Left))
	SetWindowPos(win, 0, x, y, w, ht, SWP_NOZORDER|SWP_NOACTIVATE)
	SetWindowPos(n.hwnd, 0, 0, 0, w, ht, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	h.reportScreen(n, n.nav.popW, n.nav.popH)
}

func (h *Host) popoverMessage(pw *node, m uint32, wp, lp uintptr) (uintptr, bool) {
	n := pw.parent
	switch m {
	case WM_ERASEBKGND:
		FillColor(wp, ClientRect(pw.hwnd), h.theme.bg.ref())
		return 1, true
	case WM_PRINTCLIENT:
		FillColor(wp, ClientRect(pw.hwnd), h.theme.bg.ref())
		return 0, true
	case WM_ACTIVATE:
		if LoWord(wp) == 0 && n != nil && !n.dying { // WA_INACTIVE: a click elsewhere
			h.cancelPopover(pw)
		}
		return 0, false
	case WM_CLOSE:
		h.cancelPopover(pw)
		return 0, true
	}
	return 0, false
}

// cancelPopover is the user dismissing a popover.
func (h *Host) cancelPopover(pw *node) {
	n := pw.parent
	if n == nil || n.dying {
		return
	}
	if n.interceptBack {
		h.emit(n, ops.Event{Kind: ops.EventBack})
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventDismissed})
}

func (h *Host) closePopover(n *node) {
	win := n.nav.window
	if win == 0 {
		return
	}
	n.nav.window = 0
	SetParent(n.hwnd, h.park)
	h.forgetTree(win)
	DestroyWindow(win)
}
