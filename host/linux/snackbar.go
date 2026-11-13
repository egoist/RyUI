//go:build linux && !android

package linux

import (
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A Snackbar is an AdwToast in the AdwToastOverlay around the content of
// the window it's on (newWin wraps every window's): its title the
// message, its button the action (button-label, button-clicked), its
// timeout the duration. libadwaita shows one toast at a time, slides it
// in and out, gives it a close button and has screen readers announce
// it (from 1.6; before, the host announces it).

func (h *Host) setSnackProp(n *node, p ops.PropID, v *ops.Value) {
	t := n.nav.toast
	switch p {
	case ops.PropSnackbarMessage:
		n.message = v.Str
		if t != 0 {
			AdwToastSetTitle(t, v.Str)
		}
	case ops.PropSnackbarAction:
		n.title = v.Str
		if t != 0 {
			setToastButton(t, v.Str)
		}
	case ops.PropSnackbarDuration:
		n.nav.duration = ops.SnackbarDuration(v.Int)
	case ops.PropSnackbarWindow:
		n.nav.window = ops.NodeID(v.Int)
	}
}

// setToastButton sets the toast's button's label; empty, it has none.
func setToastButton(t Ptr, label string) {
	if label == "" {
		Call(Sym("adw_toast_set_button_label"), t, 0) // NULL: no button
		return
	}
	AdwToastSetButtonLabel(t, label)
}

// snackSeconds is the toast's timeout: four seconds, ten for
// SnackbarLong, none (until it's closed) for SnackbarIndefinite. GNOME
// has no recommended timeout: with a screen reader on, one with a button
// stays until it's closed, and one without stays long, as Material does
// without one.
func (h *Host) snackSeconds(n *node) uint32 {
	var s uint32 = 4
	switch n.nav.duration {
	case ops.SnackbarDurationIndefinite:
		return 0
	case ops.SnackbarDurationLong:
		s = 10
	}
	if h.screenReader {
		if n.title != "" {
			return 0
		}
		s = max(s, 10)
	}
	return s
}

func (h *Host) presentSnack(n *node) {
	w := h.declaredWin(n)
	if n.nav.toast != 0 || n.nav.gone || w == nil || w.toasts == 0 {
		return
	}
	t := AdwToastNew(n.message)
	ObjectRef(t) // the overlay takes one; this one is the node's
	n.nav.toast = t
	if AdwToastSetUseMarkup != nil {
		AdwToastSetUseMarkup(t, false) // the message is text
	}
	if n.title != "" {
		setToastButton(t, n.title)
	}
	AdwToastSetTimeout(t, h.snackSeconds(n))
	Connect(t, "button-clicked", func() {
		if n.nav.gone || n.nav.popping {
			return
		}
		n.nav.gone = true
		h.emit(n, ops.Event{Kind: ops.EventAction}) // its one action: 0
	})
	// Its time ran out, or the user closed it (after its button too:
	// gone already).
	Connect(t, "dismissed", func() {
		if n.nav.gone || n.nav.popping {
			return
		}
		n.nav.gone = true
		h.emit(n, ops.Event{Kind: ops.EventDismissed})
	})
	AdwToastOverlayAddToast(w.toasts, t)
	if AdwGetMinorVersion == nil || AdwGetMinorVersion() < 6 {
		text := n.message
		if n.title != "" {
			text += ", " + n.title
		}
		h.announce(text)
	}
}

// destroySnack is the core's Destroy: the toast goes, animated.
func (h *Host) destroySnack(n *node) {
	t := n.nav.toast
	if t == 0 {
		return
	}
	n.nav.popping = true
	if !n.nav.gone {
		AdwToastDismiss(t)
	}
	ObjectUnref(t)
	n.nav.toast = 0
}
