//go:build windows

package windows

import (
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// An alert with fields is the host's own dialog, not a task dialog: its
// fields (the host's text boxes, with their starting text and their
// placeholders), its buttons with the cancel one last, over the main
// window, which is disabled meanwhile. Typing writes the fields' Text and
// the buttons follow Disabled; Enter goes on to the next field, and in the
// last presses the default button once it's enabled; Escape is Cancel.
func TestAlertDialogFields(t *testing.T) {
	name := signal.New("Notes")
	pass := signal.New("")
	var got []string
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("New"), OnPress: func() {
			ui.Alert(ui.AlertProps{Title: "New folder", Message: "Name it.", Fields: []ui.AlertField{
				{Text: name, Placeholder: "Name"},
				{Text: pass, Placeholder: "Password", Secure: true},
			}, Actions: []ui.Action{
				{Title: ui.Val("Cancel"), Role: ui.ActionCancel, OnPress: func() { got = append(got, "cancel") }},
				{Title: ui.Val("Create"), Disabled: signal.Func(func() bool { return pass.Get() == "" }),
					OnPress: func() { got = append(got, name.Peek()+":"+pass.Peek()) }},
			}})
		}})
	})
	saved := taskDialog
	taskDialog = func(uintptr, string, string, string, []TaskButton, int, uint32, func(uintptr)) int {
		t.Error("a task dialog for an alert with fields")
		return IDCANCEL
	}
	defer func() { taskDialog = saved }()
	show := func() (*node, *alertDialog) {
		var b *node
		for _, x := range find(h, ops.KindButton) {
			if x.title == "New" {
				b = x
			}
		}
		h.enter(func() { h.emit(b, ops.Event{Kind: ops.EventPress}) })
		h.runPosted()
		as := find(h, ops.KindAlert)
		if len(as) != 1 || as[0].nav.dlg == nil {
			fatalf("no dialog")
		}
		return as[0], as[0].nav.dlg
	}
	onMain(t, func() {
		n, d := show()
		if len(d.fields) != 2 || len(d.buttons) != 2 {
			fatalf("%d fields, %d buttons", len(d.fields), len(d.buttons))
		}
		if WindowText(d.fields[0].ctl) != "Notes" || d.fields[1].placeholder != "Password" || !d.fields[1].secure {
			fatalf("the fields: %q, %q", WindowText(d.fields[0].ctl), d.fields[1].placeholder)
		}
		if WindowText(d.buttons[0]) != "Create" || WindowText(d.buttons[1]) != "Cancel" || d.def != 1 {
			fatalf("the buttons: %q %q, default %d", WindowText(d.buttons[0]), WindowText(d.buttons[1]), d.def)
		}
		if d.focus != d.fields[0].ctl || IsWindowEnabled(h.window) {
			fatalf("the first field isn't focused, or the window isn't disabled")
		}
		if IsWindowEnabled(d.buttons[0]) {
			fatalf("Create enabled with no password")
		}
		// Enter in the first field: on to the next; in the last, with
		// Create disabled: nothing.
		pressKey(h, d.fields[0].ctl, VK_RETURN, 0, 0)
		pressKey(h, d.fields[1].ctl, VK_RETURN, 0, 0)
		if len(got) != 0 || n.nav.dlg == nil {
			fatalf("Enter with Create disabled: %q", got)
		}
		SetWindowText(d.fields[1].ctl, "secret") // EN_CHANGE, as typing
		if pass.Peek() != "secret" || !IsWindowEnabled(d.buttons[0]) {
			fatalf("the password %q, Create enabled %v", pass.Peek(), IsWindowEnabled(d.buttons[0]))
		}
		pressKey(h, d.fields[1].ctl, VK_RETURN, 0, 0)
		if len(got) != 1 || got[0] != "Notes:secret" || IsWindow(d.win) || !IsWindowEnabled(h.window) {
			fatalf("Enter in the last field: %q", got)
		}
		// Escape: Cancel.
		_, d = show()
		pressKey(h, d.fields[0].ctl, VK_ESCAPE, 0, 0)
		if len(got) != 2 || got[1] != "cancel" || IsWindow(d.win) {
			fatalf("Escape: %q", got)
		}
	})
}
