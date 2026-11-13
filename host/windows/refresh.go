//go:build windows

package windows

import (
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// Pull to refresh (OnRefresh) on Windows, the Windows way: F5 refreshes
// the refreshable Scroll or List the focus is in, or else the innermost
// one on screen in the window it's pressed in (a menu item with F5 as its
// shortcut wins). While it refreshes a spinner floats at the top of its
// content, on a raised disc, as WinUI's RefreshContainer shows its
// indicator; Refreshing false takes it away.

const vkF5 = 0x74

type refresher struct {
	refreshing bool
	spinner    *node // the disc and its ring
}

func (h *Host) setRefreshable(n *node, on bool) {
	switch {
	case on && n.refresh == nil:
		n.refresh = &refresher{}
	case !on && n.refresh != nil:
		h.setRefreshing(n, false)
		h.dropSpinner(n)
		n.refresh = nil
	}
}

// refresh starts a refresh of n, as the user asked for one.
func (h *Host) refresh(n *node) bool {
	if n.refresh == nil || n.refresh.refreshing || !n.listening(ops.EventRefresh) {
		return false
	}
	h.setRefreshing(n, true) // the core's handler says when it's done
	h.emit(n, ops.Event{Kind: ops.EventRefresh})
	return true
}

// setRefreshing shows the spinner, or takes it away.
func (h *Host) setRefreshing(n *node, on bool) {
	r := n.refresh
	if r == nil || r.refreshing == on || n.hwnd == 0 {
		return
	}
	r.refreshing = on
	if !on {
		if s := r.spinner; s != nil && s.hwnd != 0 {
			s.animating = false
			h.spin(s)
			ShowWindow(s.hwnd, SW_HIDE)
		}
		return
	}
	s := r.spinner
	if s == nil {
		h.nextCell++
		s = newNode(ops.HostIDBit|h.nextCell, ops.KindActivity)
		s.hwnd = h.makeView(0)
		s.k, s.dir = n.k, n.dir
		s.background = ops.SemanticColorValue(ops.SemanticColorSecondaryBackground)
		s.borderWidth = 1
		s.borderColor = ops.SemanticColorValue(ops.SemanticColorSeparator)
		h.byHwnd[s.hwnd] = s
		SetParent(s.hwnd, n.hwnd)
		r.spinner = s
	}
	const size, top = 36, 12 // points
	k := h.scaleOf(n)
	s.k = n.k
	s.cornerRadius = size / 2
	s.inset = [4]float32{8, 8, 8, 8}
	w := ClientRect(n.hwnd).W()
	px := int32(size*k + 0.5)
	SetWindowPos(s.hwnd, HWND_TOP, (w-px)/2, int32(top*k+0.5), px, px, SWP_NOACTIVATE|SWP_SHOWWINDOW)
	s.animating = true
	h.spin(s)
	Invalidate(s.hwnd)
}

func (h *Host) dropSpinner(n *node) {
	if n.refresh == nil || n.refresh.spinner == nil {
		return
	}
	s := n.refresh.spinner
	n.refresh.spinner = nil
	if s.hwnd != 0 {
		hw := s.hwnd
		h.forgetTree(hw)
		DestroyWindow(hw)
	}
}

// refreshKey is F5: the refreshable view the focus is in, or the
// innermost one on screen in the window.
func (h *Host) refreshKey(m *Msg) bool {
	for c := h.byHwnd[Focus()]; c != nil; c = c.parent {
		if c.refresh != nil {
			return h.refresh(c)
		}
	}
	root := h.rootOf(RootWindow(m.Hwnd))
	if root == nil {
		return false
	}
	var found *node
	var walk func(n *node)
	walk = func(n *node) {
		if n.hidden || n.hwnd != 0 && !IsWindowVisible(n.hwnd) {
			return
		}
		if n.refresh != nil {
			found = n
		}
		switch n.kind {
		case ops.KindStack:
			if t := h.topScreen(n); t != nil {
				walk(t)
			}
			return
		case ops.KindTabs:
			if n.selected < len(n.children) {
				walk(n.children[n.selected])
			}
			return
		case ops.KindSheet, ops.KindAlert, ops.KindWindow, ops.KindPopover:
			if n != root {
				return
			}
		}
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(root)
	return found != nil && h.refresh(found)
}
