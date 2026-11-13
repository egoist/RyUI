//go:build linux && !android

package linux

import (
	"slices"
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A Picker is a GtkDropDown: none chosen, it selects nothing (not the
// first, as GTK would) and shows its label; the user's choice writes the
// signal, and code's selects the item.
func TestPicker(t *testing.T) {
	pick := signal.New(-1)
	items := signal.NewFunc([]string{"Name", "Date", "Size"}, slices.Equal[[]string])
	h, r := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Align: ui.AlignStart, Padding: ui.All(16)},
			ui.Picker(ui.PickerProps{Items: items, Selected: pick, Label: ui.Val("Sort by")}))
	})
	var p *node
	settle(t, h, func() bool {
		ps := find(h, ops.KindPicker)
		if len(ps) == 1 && WidgetGetMapped(ps[0].widget) {
			p = ps[0]
		}
		return p != nil
	})
	onMain(func() {
		pk := pickers[p]
		if pk == nil {
			fatalf(t, "no drop-down")
		}
		if i := DropDownGetSelected(pk.drop); i != invalidPosition {
			fatalf(t, "none chosen, yet item %d is", i)
		}
		if !WidgetGetVisible(pk.label) || pk.text != "Sort by" {
			fatalf(t, "the label isn't shown")
		}
		// The user chooses Size.
		DropDownSetSelected(pk.drop, 2)
	})
	settle(t, h, func() bool { return pick.Peek() == 2 })
	onMain(func() {
		pk := pickers[p]
		if WidgetGetVisible(pk.label) {
			fatalf(t, "the label shows over Size")
		}
		pick.Set(0)
		r.Flush()
		if i := DropDownGetSelected(pk.drop); i != 0 {
			fatalf(t, "after code's choice: item %d", i)
		}
		pick.Set(-1)
		r.Flush()
		if i := DropDownGetSelected(pk.drop); i != invalidPosition || !WidgetGetVisible(pk.label) {
			fatalf(t, "none again: item %d", i)
		}
	})
}
