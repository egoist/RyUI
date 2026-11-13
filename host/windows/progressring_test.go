//go:build windows

package windows

import (
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A ProgressRing is drawn as Fluent's: 32 points by default (or the
// style's size, square), following its value; Narrator reads it as a
// progress bar with its percentage, and an indeterminate one turns.
func TestProgressRing(t *testing.T) {
	done := signal.New(0.25)
	h, _ := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Gap: 12, Padding: ui.All(12), Align: ui.AlignStart},
			ui.ProgressRing(done),
			ui.ProgressRing(nil),
			ui.ProgressRing(done, ui.Style{Width: ui.Pt(64)}),
		)
	})
	onMain(t, func() {
		rs := find(h, ops.KindProgressRing)
		if len(rs) != 3 {
			fatalf("%d rings", len(rs))
		}
		var ring, spinner, large *node
		for _, n := range rs {
			switch f := frame(h, n); {
			case n.indeterminate:
				spinner = n
			case f[2] == 64:
				large = n
			default:
				ring = n
			}
		}
		if ring == nil || spinner == nil || large == nil {
			fatalf("rings %v %v %v", ring, spinner, large)
		}
		if f := frame(h, ring); f[2] != 32 || f[3] != 32 {
			fatalf("the ring's frame is %v", f)
		}
		if f := frame(h, large); f[3] != 64 {
			fatalf("the large ring's frame is %v", f)
		}
		a := accessible{h: h, n: ring}
		if ct := a.Property(UIA_ControlTypePropertyId); ct != int32(UIA_ProgressBarControlTypeId) {
			fatalf("control type %v", ct)
		}
		if v, lo, hi, ok := a.Range(); !ok || v != 25 || lo != 0 || hi != 100 {
			fatalf("range %v %v %v %v", v, lo, hi, ok)
		}
		if _, _, _, ok := (accessible{h: h, n: spinner}).Range(); ok {
			fatalf("an indeterminate ring has a value")
		}
		h.enter(func() { ui.Post(func() { done.Set(0.5) }) })
		h.runPosted()
		if ring.value != 0.5 {
			fatalf("value %v", ring.value)
		}
		if v, _, _, _ := a.Range(); v != 50 {
			fatalf("range value %v", v)
		}
	})
}
