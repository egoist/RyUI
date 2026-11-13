//go:build linux && !android

package linux

import (
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// emitSignal emits a widget's signal of no arguments, by name.
func emitSignal(w Ptr, name string) {
	s := CString(name)
	Call(Sym("g_signal_emit_by_name"), w, Addr(s))
}

// An alert's fields are entry rows in a boxed list, under its message:
// their starting text, the first focused. Typing writes their Text, and
// the default response follows its Disabled; activating a row goes on to
// the next, and the last activates the default response once it's
// enabled.
func TestAlertFields(t *testing.T) {
	name := signal.New("Notes")
	pass := signal.New("")
	var created []string
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("New"), OnPress: func() {
			ui.Alert(ui.AlertProps{Title: "New folder", Fields: []ui.AlertField{
				{Text: name, Placeholder: "Name"},
				{Text: pass, Placeholder: "Password", Secure: true},
			}, Actions: []ui.Action{
				{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
				{Title: ui.Val("Create"), Disabled: signal.Func(func() bool { return pass.Get() == "" }),
					OnPress: func() { created = append(created, name.Peek()+":"+pass.Peek()) }},
			}})
		}})
	})
	onMain(func() { h.enter(func() { h.emit(find(h, ops.KindButton)[0], ops.Event{Kind: ops.EventPress}) }) })
	var a *node
	settle(t, h, func() bool {
		as := find(h, ops.KindAlert)
		if len(as) != 1 || len(as[0].nav.rows) != 2 || !WidgetGetMapped(as[0].nav.rows[0]) {
			return false
		}
		a = as[0]
		return WidgetHasFocus(EditableGetDelegate(a.nav.rows[0]))
	})
	enabled := func() bool {
		id := CString("1")
		return Call(Sym("adw_alert_dialog_get_response_enabled"), a.nav.dialog, Addr(id))&0xff != 0
	}
	onMain(func() {
		rows := a.nav.rows
		if got := EditableGetText(rows[0]); got != "Notes" {
			fatalf(t, "the name row has %q", got)
		}
		if enabled() {
			fatalf(t, "Create enabled with no password")
		}
		// Activating the first row: on to the next.
		emitSignal(EditableGetDelegate(rows[0]), "activate")
	})
	settle(t, h, func() bool { return WidgetHasFocus(EditableGetDelegate(a.nav.rows[1])) })
	onMain(func() {
		// The last, with Create disabled: nothing.
		emitSignal(EditableGetDelegate(a.nav.rows[1]), "activate")
		EditableSetText(a.nav.rows[1], "secret")
		if pass.Peek() != "secret" {
			fatalf(t, "the password is %q", pass.Peek())
		}
	})
	if len(created) != 0 {
		t.Fatalf("created %q with Create disabled", created)
	}
	settle(t, h, enabled)
	onMain(func() { emitSignal(EditableGetDelegate(a.nav.rows[1]), "activate") })
	settle(t, h, func() bool { return len(created) == 1 })
	if created[0] != "Notes:secret" {
		t.Fatalf("created %q", created)
	}
}
