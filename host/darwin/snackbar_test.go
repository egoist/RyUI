//go:build darwin && !ios

package darwin

import (
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A snackbar is an overlay at the bottom of the window: its message, its
// action's button and a close button, in the system's material. Its
// button runs the action; closing it, or its time running out, dismisses
// it; a new one replaces it.
func TestSnackbar(t *testing.T) {
	undone, dismissed := 0, 0
	title := signal.New("Undo")
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Delete"), OnPress: func() {
			ui.Snackbar(ui.SnackbarProps{
				Message:   "Note deleted",
				Action:    &ui.Action{Title: title, OnPress: func() { undone++ }},
				OnDismiss: func() { dismissed++ },
			})
		}})
	})
	show := func() *node {
		h.enter(func() { h.act(find(h, ops.KindButton)[0].view) })
		ns := find(h, ops.KindSnackbar)
		if len(ns) != 1 || ns[0].snack.box == 0 {
			fatalf(t, "%d snackbars", len(ns))
		}
		return ns[0]
	}
	onMain(func() {
		n := show()
		s := n.snack
		if Send(s.box, "superview") != h.content {
			fatal(t, "not on the window's content")
		}
		if got := GoString(Send(s.label, "stringValue")); got != "Note deleted" {
			fatalf(t, "message %q", got)
		}
		if got := GoString(Send(s.button, "title")); got != "Undo" {
			fatalf(t, "button %q", got)
		}
		// Its message is measured in the system's font, on one line.
		if lf := MsgRect(s.label, Sel("frame")); lf.Size.Width < 60 || lf.Size.Height < 12 || lf.Size.Height > 24 {
			fatalf(t, "the message's frame %+v", lf)
		}
		if GoString(Send(s.effect, "className")) != "NSVisualEffectView" {
			fatal(t, "no material")
		}
		// At the bottom, centered, inside the window.
		cb := MsgRect(h.content, Sel("bounds"))
		f := MsgRect(s.box, Sel("frame"))
		if f.Origin.Y+f.Size.Height > cb.Size.Height-snackMargin+0.5 || f.Origin.Y < cb.Size.Height/2 {
			fatalf(t, "frame %+v in %+v", f, cb)
		}
		if mid := f.Origin.X + f.Size.Width/2; mid < cb.Size.Width/2-1 || mid > cb.Size.Width/2+1 {
			fatalf(t, "not centered: %+v in %+v", f, cb)
		}
		// Its action's title follows the app's language while it shows.
		h.enter(func() { ui.Post(func() { title.Set("Rückgängig") }) })
		h.runPosted()
		if got := GoString(Send(s.button, "title")); got != "Rückgängig" {
			fatalf(t, "button %q", got)
		}
		// Its time stops under the mouse, and goes on after.
		if s.timer == 0 {
			fatal(t, "no timer")
		}
		h.snackHover(s.box, true)
		if s.timer != 0 {
			fatal(t, "the timer runs under the mouse")
		}
		h.snackHover(s.box, false)
		if s.timer == 0 {
			fatal(t, "the timer didn't resume")
		}

		// Its button: the action runs, not OnDismiss, and it goes.
		h.enter(func() { h.act(s.button) })
		if undone != 1 || dismissed != 0 || len(find(h, ops.KindSnackbar)) != 0 {
			fatalf(t, "undone %d dismissed %d snackbars %d", undone, dismissed, len(find(h, ops.KindSnackbar)))
		}

		// The close button dismisses it.
		n = show()
		h.enter(func() { h.act(n.snack.close) })
		if undone != 1 || dismissed != 1 || len(find(h, ops.KindSnackbar)) != 0 {
			fatalf(t, "undone %d dismissed %d", undone, dismissed)
		}

		// Its time running out dismisses it.
		n = show()
		h.enter(func() { h.snackReport(n, ops.EventDismissed) })
		if dismissed != 2 || len(find(h, ops.KindSnackbar)) != 0 {
			fatalf(t, "dismissed %d", dismissed)
		}

		// A new one replaces the one showing.
		first := show()
		second := show()
		if first == second || dismissed != 3 || len(find(h, ops.KindSnackbar)) != 1 || !first.snack.done || first.snack.timer != 0 {
			fatalf(t, "dismissed %d, %d snackbars", dismissed, len(find(h, ops.KindSnackbar)))
		}
		if h.byView[first.snack.close] != nil {
			fatal(t, "the replaced one's buttons still route")
		}
	})
}
