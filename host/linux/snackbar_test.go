//go:build linux && !android

package linux

import (
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
)

// emitToast emits one of an AdwToast's signals, as libadwaita does.
func emitToast(t Ptr, signal string) {
	s := CString(signal)
	Call(Sym("g_signal_emit_by_name"), t, Addr(s))
}

// A snackbar is an AdwToast in the window's toast overlay: its title the
// message, its button the action, its timeout the duration. Its button
// runs the action; its dismissal (its time, its close button) runs
// OnDismiss; a new one replaces it.
func TestSnackbar(t *testing.T) {
	undone, dismissed := 0, 0
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("Delete"), OnPress: func() {
			ui.Snackbar(ui.SnackbarProps{
				Message:   "Note <deleted> & gone",
				Duration:  ui.SnackbarLong,
				Action:    &ui.Action{Title: ui.Val("Undo"), OnPress: func() { undone++ }},
				OnDismiss: func() { dismissed++ },
			})
		}})
	})
	show := func() *node {
		var b *node
		settle(t, h, func() bool {
			bs := find(h, ops.KindButton)
			if len(bs) == 1 && WidgetGetMapped(bs[0].widget) {
				b = bs[0]
			}
			return b != nil
		})
		onMain(func() { h.enter(func() { h.emit(b, ops.Event{Kind: ops.EventPress}) }) })
		var n *node
		settle(t, h, func() bool {
			ns := find(h, ops.KindSnackbar)
			if len(ns) == 1 && ns[0].nav.toast != 0 {
				n = ns[0]
			}
			return n != nil
		})
		return n
	}
	n := show()
	onMain(func() {
		toast := n.nav.toast
		if got := GetStringProperty(toast, "title"); got != "Note <deleted> & gone" {
			fatalf(t, "title %q", got)
		}
		if GetBoolProperty(toast, "use-markup") {
			fatalf(t, "the message is read as markup")
		}
		if got := GetStringProperty(toast, "button-label"); got != "Undo" {
			fatalf(t, "button %q", got)
		}
		if got := GetIntProperty(toast, "timeout"); got != 10 && !h.screenReader {
			fatalf(t, "timeout %d", got)
		}
		h.enter(func() { emitToast(toast, "button-clicked") })
		emitToast(toast, "dismissed") // libadwaita's, after the button: not a dismissal
		if undone != 1 || dismissed != 0 || len(find(h, ops.KindSnackbar)) != 0 {
			fatalf(t, "undone %d dismissed %d", undone, dismissed)
		}
	})
	n = show()
	onMain(func() {
		h.enter(func() { emitToast(n.nav.toast, "dismissed") })
		if undone != 1 || dismissed != 1 || len(find(h, ops.KindSnackbar)) != 0 {
			fatalf(t, "undone %d dismissed %d", undone, dismissed)
		}
	})
	first := show()
	second := show()
	onMain(func() {
		if first == second || dismissed != 2 || len(find(h, ops.KindSnackbar)) != 1 || first.nav.toast != 0 {
			fatalf(t, "dismissed %d, %d snackbars", dismissed, len(find(h, ops.KindSnackbar)))
		}
	})
}
