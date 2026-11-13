//go:build linux && !android

package linux

import (
	"fmt"
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A button's menu: its items' actions are named by their numbers, a
// submenu's too; choosing one sends its number, and a disabled one is
// disabled.
func TestRichMenu(t *testing.T) {
	var picked []string
	h, _ := mount(t, func() ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val("More"), Menu: []ui.Action{
			ui.MenuSection(ui.Val("Sort by"),
				ui.Action{Title: ui.Val("Name"), Checked: ui.Val(true), OnPress: func() { picked = append(picked, "name") }},
			),
			ui.Submenu(ui.Val("Move to"), "", ui.Action{Title: ui.Val("Archive"), OnPress: func() { picked = append(picked, "archive") }}),
			{Title: ui.Val("Delete"), Disabled: signal.Func(func() bool { return true })},
		}})
	})
	var b *node
	settle(t, h, func() bool {
		bs := find(h, ops.KindButton)
		if len(bs) == 1 && WidgetGetMapped(bs[0].widget) {
			b = bs[0]
		}
		return b != nil
	})
	onMain(func() {
		h.popupMenu(b, -1, -1)
		if !WidgetActivateAction(b.widget, "asuka.item3", 0) { // Move to ▸ Archive
			fatalf(t, "no action for Archive")
		}
		if !WidgetActivateAction(b.widget, "asuka.item1", 0) { // Sort by: Name
			fatalf(t, "no action for Name")
		}
	})
	settle(t, h, func() bool { return fmt.Sprint(picked) == "[archive name]" })
}
