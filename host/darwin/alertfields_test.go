//go:build darwin && !ios

package darwin

import (
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// An alert's fields are its accessory view: an NSTextField and an
// NSSecureTextField with their starting text, the first being edited as
// it shows. Typing writes their Text, and the default button follows its
// Disabled. Return goes on to the next field, and in the last presses
// the default button once it's enabled, and does nothing before.
func TestAlertFieldsDarwin(t *testing.T) {
	name := signal.New("Notes")
	pass := signal.New("")
	var created []string
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("New"), OnPress: func() {
			ui.Alert(ui.AlertProps{Title: "New folder", Fields: []ui.AlertField{
				{Text: name, Placeholder: "Name"},
				{Text: pass, Placeholder: "Password", Secure: true, ContentType: ui.ContentPassword},
			}, Actions: []ui.Action{
				{Title: ui.Val("Delete"), Role: ui.ActionDestructive},
				{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
				{Title: ui.Val("Create"), Disabled: signal.Func(func() bool { return pass.Get() == "" }),
					OnPress: func() { created = append(created, name.Peek()+":"+pass.Peek()) }},
			}})
		}})
	})
	onMain(func() {
		Send(h.window, "makeKeyAndOrderFront:", 0)
		h.enter(func() { h.emit(find(h, ops.KindButton)[0], ops.Event{Kind: ops.EventPress}) })
		alerts := find(h, ops.KindAlert)
		if len(alerts) != 1 || alerts[0].nav.alert == 0 {
			fatal(t, "no alert")
		}
		n := alerts[0]
		a := n.nav.alert
		w := Send(a, "window")
		in := n.nav.inputs
		if len(in) != 2 || Send(a, "accessoryView") == 0 {
			fatalf(t, "%d fields", len(in))
		}
		if SendBool(in[0], "isKindOfClass:", uintptr(Class("NSSecureTextField"))) || !SendBool(in[1], "isKindOfClass:", uintptr(Class("NSSecureTextField"))) {
			fatal(t, "the password isn't the secure field")
		}
		if got := GoString(Send(in[0], "stringValue")); got != "Notes" {
			fatalf(t, "the name field has %q", got)
		}
		if got := GoString(Send(in[1], "placeholderString")); got != "Password" {
			fatalf(t, "the password's placeholder is %q", got)
		}
		if fr := Send(w, "firstResponder"); fr != in[0] && Send(fr, "delegate") != in[0] {
			fatal(t, "the first field isn't being edited")
		}
		// Return takes the default button, Create, not the first.
		btns := Send(a, "buttons")
		create := Send(btns, "objectAtIndex:", 1)
		if GoString(Send(create, "title")) != "Create" || GoString(Send(create, "keyEquivalent")) != "\r" ||
			GoString(Send(Send(btns, "objectAtIndex:", 0), "keyEquivalent")) == "\r" {
			fatalf(t, "return isn't Create's: %q %q %q", GoString(Send(Send(btns, "objectAtIndex:", 0), "keyEquivalent")), GoString(Send(create, "title")), GoString(Send(create, "keyEquivalent")))
		}
		if SendBool(create, "isEnabled") {
			fatal(t, "Create enabled with no password")
		}
		// Typing in the first field writes Name; return goes on to the
		// next.
		Send(w, "makeKeyAndOrderFront:", 0)
		drain()
		Send(Send(w, "firstResponder"), "selectAll:", 0)
		pressKey(w, kcA, "a", 0, false)
		if name.Peek() != "a" {
			fatalf(t, "the name is %q", name.Peek())
		}
		pressKey(w, kcReturn, "\r", 0, false)
		if fr := Send(w, "firstResponder"); fr != in[1] && Send(fr, "delegate") != in[1] {
			fatal(t, "return didn't go on to the password")
		}
		// Return in the last while Create is disabled: nothing.
		pressKey(w, kcReturn, "\r", 0, false)
		if len(created) != 0 || h.nodes[n.id] == nil || !SendBool(w, "isVisible") {
			fatal(t, "return with Create disabled did something")
		}
		pressKey(w, kcA, "a", 0, false)
		if pass.Peek() != "a" {
			fatalf(t, "the password is %q", pass.Peek())
		}
		if !SendBool(create, "isEnabled") {
			fatal(t, "Create didn't follow the password")
		}
		pressKey(w, kcReturn, "\r", 0, false)
		if len(created) != 1 || created[0] != "a:a" {
			fatalf(t, "created %q", created)
		}
		if len(h.alertFields) != 0 {
			fatalf(t, "%d fields left", len(h.alertFields))
		}
	})
}
