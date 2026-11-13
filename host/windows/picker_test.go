//go:build windows

package windows

import (
	"slices"
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A Picker is a drop-down list combo box: empty, showing its label, while
// none is chosen; the user's choice (CBN_SELCHANGE) writes the signal, and
// code's selects the item.
func TestPicker(t *testing.T) {
	pick := signal.New(-1)
	items := signal.NewFunc([]string{"Name", "Date", "Size"}, slices.Equal[[]string])
	h, r := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Align: ui.AlignStart, Padding: ui.All(16)},
			ui.Picker(ui.PickerProps{Items: items, Selected: pick, Label: ui.Val("Sort by")}))
	})
	onMain(t, func() {
		p := find(h, ops.KindPicker)[0]
		if p.ctl == 0 || ClassName(p.ctl) != "ComboBox" {
			fatalf("not a combo box")
		}
		if n := SendMessage(p.ctl, CB_GETCOUNT, 0, 0); n != 3 {
			fatalf("%d items", n)
		}
		if i := int32(SendMessage(p.ctl, CB_GETCURSEL, 0, 0)); i != -1 {
			fatalf("none chosen, yet item %d is", i)
		}
		if p.pickerLabel != "Sort by" {
			fatalf("label %q", p.pickerLabel)
		}

		// The user chooses Size.
		SendMessage(p.ctl, CB_SETCURSEL, 2, 0)
		h.controlCommand(p, p.ctl, CBN_SELCHANGE)
		if pick.Peek() != 2 {
			fatalf("after choosing Size: %d", pick.Peek())
		}

		// Code's choice, then none again.
		h.enter(func() { pick.Set(0); r.Flush() })
		if i := SendMessage(p.ctl, CB_GETCURSEL, 0, 0); i != 0 {
			fatalf("after code's choice: item %d", i)
		}
		h.enter(func() { pick.Set(5); r.Flush() })
		if i := int32(SendMessage(p.ctl, CB_GETCURSEL, 0, 0)); i != -1 {
			fatalf("out of range: item %d", i)
		}

		// Right to left, it's made again, keeping its items, choice and label.
		h.enter(func() { pick.Set(1); r.Flush() })
		setLocale(h, true, "ar")
		if n, i := SendMessage(p.ctl, CB_GETCOUNT, 0, 0), SendMessage(p.ctl, CB_GETCURSEL, 0, 0); n != 3 || i != 1 {
			fatalf("made again right to left: %d items, item %d chosen", n, i)
		}
	})
}
