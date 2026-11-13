//go:build darwin && !ios

package darwin

import (
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// A Popover is an NSPopover pointing at its anchor, below it (AppKit moves
// it where there's room), transient: a click outside or Escape closes it,
// reported as Dismissed. While ui.OnBack in it intercepts, it asks the core
// (Back) instead of closing.

type popover struct {
	pop    ID
	anchor ops.NodeID
	size   Size
}

const rectEdgeMaxY = 3 // below, in a flipped view

func (h *Host) setPopoverProp(n *node, p ops.PropID, v *ops.Value) {
	po := n.popover
	switch p {
	case ops.PropPopoverAnchor:
		po.anchor = ops.NodeID(v.Int)
	case ops.PropPopoverWidth:
		po.size.Width = v.Float
	case ops.PropPopoverHeight:
		po.size.Height = v.Float
	default:
		return
	}
	if po.pop != 0 && po.size.Width > 0 && po.size.Height > 0 {
		MsgSetSize(po.pop, Sel("setContentSize:"), po.size)
		MsgSetRect(n.view, selSetFrame, Rect{Size: po.size})
		h.reportScreen(n, po.size)
	}
}

func (h *Host) presentPopover(n *node) {
	// After the batch: its anchor, made with it, isn't in a window yet,
	// nor laid out.
	h.afterCore(func() {
		if h.nodes[n.id] == n && !n.dying {
			h.showPopover(n)
		}
	})
}

func (h *Host) showPopover(n *node) {
	po := n.popover
	a := h.nodes[po.anchor]
	if po.pop != 0 || a == nil || a.view == 0 || Send(a.view, "window") == 0 {
		return
	}
	vc := New("NSViewController")
	MsgSetRect(n.view, selSetFrame, Rect{Size: po.size})
	Send(vc, "setView:", uintptr(n.view))
	p := New("NSPopover")
	Send(p, "setContentViewController:", uintptr(vc))
	Release(vc)
	Send(p, "setBehavior:", 1) // transient
	Send(p, "setAnimates:", 1)
	Send(p, "setDelegate:", uintptr(h.target))
	MsgSetSize(p, Sel("setContentSize:"), po.size)
	po.pop = p
	n.presented = true
	h.reportScreen(n, po.size)
	b := MsgRect(a.view, Sel("bounds"))
	MsgRectIDUint(p, Sel("showRelativeToRect:ofView:preferredEdge:"), b, a.view, rectEdgeMaxY)
}

// popoverOf is the node whose NSPopover p is.
func (h *Host) popoverOf(p ID) *node {
	for _, n := range h.nodes {
		if n.popover != nil && n.popover.pop == p {
			return n
		}
	}
	return nil
}

// popoverShouldClose: a click outside or Escape; ui.OnBack may keep it.
func (h *Host) popoverShouldClose(p ID) bool {
	n := h.popoverOf(p)
	if n == nil || n.dying || !n.interceptBack {
		return true
	}
	h.emit(n, ops.Event{Kind: ops.EventBack})
	return false
}

func (h *Host) popoverClosed(p ID) {
	n := h.popoverOf(p)
	if n == nil {
		return
	}
	n.popover.pop = 0
	Release(p)
	if !n.dying {
		n.dying = true
		h.emit(n, ops.Event{Kind: ops.EventDismissed})
	}
}

func (h *Host) closePopover(n *node) {
	if p := n.popover.pop; p != 0 {
		Send(p, "close") // popoverDidClose: follows, and releases it
	}
}
