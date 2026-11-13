//go:build windows

package windows

import (
	"testing"

	"github.com/egoist/asuka/internal/l10n"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A snackbar is an overlay at the bottom of the window, drawn as an
// InfoBar: the message, its action's button and a close button, BUTTON
// controls a screen reader reaches. Its button runs the action; closing
// it, or its time running out, dismisses it; a new one replaces it.
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
		SendMessage(find(h, ops.KindButton)[0].ctl, BM_CLICK, 0, 0)
		h.runPosted()
		ns := find(h, ops.KindSnackbar)
		if len(ns) != 1 || ns[0].snack.owner == 0 {
			fatalf("%d snackbars", len(ns))
		}
		return ns[0]
	}
	onMain(t, func() {
		h.env.ReduceMotion = true // no slides: placed at once, gone at once
		n := show()
		s := n.snack
		if Parent(n.hwnd) != h.window || !IsWindowVisible(n.hwnd) {
			fatalf("not shown on the window")
		}
		if n.a11yLabel != "Note deleted" || WindowText(s.action) != "Undo" || WindowText(s.close) != l10n.Close(h.locale) {
			fatalf("label %q, buttons %q %q", n.a11yLabel, WindowText(s.action), WindowText(s.close))
		}
		// At the bottom, centered, inside the window, above its content.
		cr := ClientRect(h.window)
		r := WindowRectIn(n.hwnd, h.window)
		if r.Bottom > cr.H()-h.px(n, snackMargin)+1 || r.Top < cr.H()/2 || abs32((r.Left+r.Right)/2-cr.W()/2) > 1 {
			fatalf("at %+v in %+v", r, cr)
		}
		if NextWindow(n.hwnd, GW_HWNDPREV) != 0 {
			fatalf("not on top of the window's content")
		}
		// Its action's title follows the app's language while it shows.
		h.enter(func() { ui.Post(func() { title.Set("Rückgängig") }) })
		h.runPosted()
		if WindowText(s.action) != "Rückgängig" {
			fatalf("button %q", WindowText(s.action))
		}
		// Its button: the action runs, not OnDismiss, and it goes.
		SendMessage(s.action, BM_CLICK, 0, 0)
		h.runPosted()
		if undone != 1 || dismissed != 0 || len(find(h, ops.KindSnackbar)) != 0 || len(h.snacks) != 0 {
			fatalf("undone %d dismissed %d snackbars %d", undone, dismissed, len(find(h, ops.KindSnackbar)))
		}

		// The close button dismisses it.
		n = show()
		SendMessage(n.snack.close, BM_CLICK, 0, 0)
		h.runPosted()
		if undone != 1 || dismissed != 1 || len(find(h, ops.KindSnackbar)) != 0 {
			fatalf("undone %d dismissed %d", undone, dismissed)
		}

		// Its time running out dismisses it.
		n = show()
		h.enter(func() { h.snackReport(n, ops.EventDismissed) })
		h.runPosted()
		if dismissed != 2 || len(find(h, ops.KindSnackbar)) != 0 {
			fatalf("dismissed %d", dismissed)
		}

		// A new one replaces the one showing.
		first := show()
		second := show()
		h.runPosted()
		if first == second || dismissed != 3 || len(find(h, ops.KindSnackbar)) != 1 || first.snack.stop != nil || first.hwnd != 0 {
			fatalf("dismissed %d, %d snackbars", dismissed, len(find(h, ops.KindSnackbar)))
		}
	})
}
