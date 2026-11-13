//go:build darwin && !ios

package darwin

import (
	"slices"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A Picker is a pop-up button: its label while none is chosen (a hidden
// item, not in its menu), then the chosen item; the user's choice writes
// the signal, and code's selects the item.
func TestPicker(t *testing.T) {
	pick := signal.New(-1)
	items := signal.NewFunc([]string{"Name", "Date", "Size"}, slices.Equal[[]string])
	h, r := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Align: ui.AlignStart, Padding: ui.All(16)},
			ui.Picker(ui.PickerProps{Items: items, Selected: pick, Label: ui.Val("Sort by")}))
	})
	onMain(func() {
		p := find(h, ops.KindPicker)[0]
		if !SendBool(p.view, "isKindOfClass:", uintptr(Class("NSPopUpButton"))) {
			fatal(t, "not a pop-up button")
		}
		title := func() string { return GoString(Send(p.view, "titleOfSelectedItem")) }
		if s := title(); s != "Sort by" {
			fatalf(t, "none chosen: %q", s)
		}
		m := Send(p.view, "menu")
		var shown []string
		for i := 0; i < int(SendInt(m, "numberOfItems")); i++ {
			it := Send(m, "itemAtIndex:", uintptr(i))
			if !SendBool(it, "isHidden") {
				shown = append(shown, GoString(Send(it, "title")))
			}
		}
		if !slices.Equal(shown, []string{"Name", "Date", "Size"}) {
			fatalf(t, "menu %q", shown)
		}
		if s := GoString(Send(p.view, "accessibilityLabel")); s != "Sort by" {
			fatalf(t, "accessibility label %q", s)
		}
		f := MsgRect(p.view, Sel("frame"))
		if f.Size.Width < 40 || f.Size.Height < 16 {
			fatalf(t, "frame %+v", f)
		}

		// The user chooses Size, as the menu does: its item, then the action.
		h.enter(func() {
			Send(p.view, "selectItemAtIndex:", 3)
			SendBool(p.view, "sendAction:to:", uintptr(Send(p.view, "action")), uintptr(Send(p.view, "target")))
		})
		if pick.Peek() != 2 || title() != "Size" {
			fatalf(t, "after choosing Size: %d %q", pick.Peek(), title())
		}

		// Code's choice, and new items, keep the chosen one.
		h.enter(func() { pick.Set(0); r.Flush() })
		if title() != "Name" {
			fatalf(t, "after code's choice: %q", title())
		}
		h.enter(func() { items.Set([]string{"Title", "Date"}); r.Flush() })
		if title() != "Title" {
			fatalf(t, "after new items: %q", title())
		}
		h.enter(func() { pick.Set(-1); r.Flush() })
		if title() != "Sort by" {
			fatalf(t, "none chosen again: %q", title())
		}
	})
}
