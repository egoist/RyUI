//go:build linux && !android

package linux

import (
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A Popover is a GtkPopover pointing at its anchor's view, below it (GTK
// moves it where there's room), its content the popover node's view at
// the size the core gives it (Width, Height), a layout root of its own.
// It closes on a click outside or Escape, reported as Dismissed; while
// ui.OnBack in it intercepts, Escape asks the core (Back) instead.
//
// An Accessory (a field's bar over an on-screen keyboard) is made and
// never shown: a desktop has no on-screen keyboard.

type popover struct {
	pop     Ptr
	anchor  ops.NodeID
	w, h    float32
	closing bool // the core closes it: no Dismissed
}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		switch n.kind {
		case ops.KindPopover:
			n.nav = &nav{}
			n.ext = &popover{}
			return true
		case ops.KindAccessory:
			return true
		}
		return false
	})
	propHooks = append(propHooks, popoverProp)
	releaseHooks = append(releaseHooks, releasePopover)
	presentPopoverHook = func(h *Host, n *node) {
		// After the batch: its anchor, made with it, isn't laid out yet.
		h.afterCore(func() {
			if h.nodes[n.id] == n {
				h.showPopover(n)
			}
		})
	}
}

func popoverProp(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
	po, ok := n.ext.(*popover)
	if !ok || n.kind != ops.KindPopover {
		return false
	}
	switch p {
	case ops.PropPopoverAnchor:
		po.anchor = ops.NodeID(v.Int)
	case ops.PropPopoverWidth:
		po.w = float32(v.Float)
	case ops.PropPopoverHeight:
		po.h = float32(v.Float)
	default:
		return false
	}
	if po.w > 0 && po.h > 0 && n.widget != 0 {
		WidgetSetSizeRequest(n.widget, windowSize(po.w), windowSize(po.h))
	}
	return true
}

func (h *Host) showPopover(n *node) {
	po := n.ext.(*popover)
	a := h.nodes[po.anchor]
	if po.pop != 0 || a == nil || a.widget == 0 || n.widget == 0 {
		return
	}
	if !WidgetGetMapped(a.widget) {
		// Not on screen yet (the window shows after the first batch): once
		// it is.
		var id uint64
		w := a.widget
		id = Connect(w, "map", func() {
			Disconnect(w, id)
			IdleAdd(PriorityDefault, func() bool {
				if h.nodes[n.id] == n && h.nodes[a.id] == a {
					h.showPopover(n)
				}
				return false
			})
		})
		return
	}
	pop := PopoverNew()
	ObjectRefSink(pop) // the host's, until it's unparented
	po.pop = pop
	if po.w > 0 && po.h > 0 {
		WidgetSetSizeRequest(n.widget, windowSize(po.w), windowSize(po.h))
	}
	h.detach(n.widget)
	PopoverSetChild(pop, n.widget)
	PopoverSetPosition(pop, 3) // GTK_POS_BOTTOM
	WidgetSetParent(pop, a.widget)
	k := EventControllerKeyNew()
	EventControllerSetPropagationPhase(k, PhaseCapture)
	Connect(k, "key-pressed", func(keyval, keycode, state uint32) bool {
		if keyval == 0xff1b && n.interceptBack && !po.closing { // Escape
			h.emit(n, ops.Event{Kind: ops.EventBack})
			return true
		}
		return false
	})
	WidgetAddController(pop, k)
	Connect(pop, "closed", func() {
		if po.closing {
			return
		}
		po.closing = true
		h.emit(n, ops.Event{Kind: ops.EventDismissed})
	})
	PopoverPopup(pop)
}

// releasePopover closes a popover the core destroyed (closing it) or the
// user dismissed; it's unparented once GTK is done with it.
func releasePopover(h *Host, n *node) {
	po, ok := n.ext.(*popover)
	if !ok || n.kind != ops.KindPopover || po.pop == 0 {
		return
	}
	pop := po.pop
	po.pop = 0
	if !po.closing {
		po.closing = true
		PopoverPopdown(pop)
	}
	PopoverSetChild(pop, 0)
	IdleAdd(PriorityDefaultIdle, func() bool {
		if WidgetGetParent(pop) != 0 {
			WidgetUnparent(pop)
		}
		ObjectUnref(pop)
		return false
	})
}
