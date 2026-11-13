//go:build darwin && !ios

package darwin

import (
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A ProgressRing is AppKit's spinning NSProgressIndicator: determinate,
// following its value, at its regular size (or the style's, square); and
// indeterminate, animating, without a value. VoiceOver reads the ring as
// a progress indicator, and the spinner as a busy indicator.
func TestProgressRing(t *testing.T) {
	done := signal.New(0.25)
	h, _ := mount(t, func() ui.Node {
		return ui.Row(ui.Style{Gap: 12, Padding: ui.All(12), Align: ui.AlignStart},
			ui.ProgressRing(done),
			ui.ProgressRing(nil),
			ui.ProgressRing(done, ui.Style{Width: ui.Pt(16)}),
			ui.ProgressRing(done, ui.Style{Height: ui.Pt(64)}),
		)
	})
	onMain(func() {
		rs := find(h, ops.KindProgressRing)
		if len(rs) != 4 {
			fatalf(t, "%d rings", len(rs))
		}
		byX := func(i int) *node { // in the row's order
			var out *node
			for _, n := range rs {
				x := MsgRect(n.view, Sel("frame")).Origin.X
				rank := 0
				for _, m := range rs {
					if MsgRect(m.view, Sel("frame")).Origin.X < x {
						rank++
					}
				}
				if rank == i {
					out = n
				}
			}
			return out
		}
		ring, spinner, small, large := byX(0), byX(1), byX(2), byX(3)
		for _, n := range []*node{ring, spinner, small, large} {
			if SendInt(n.view, "style") != 1 {
				fatal(t, "not the spinning style")
			}
			role := "AXProgressIndicator"
			if n == spinner {
				role = "AXBusyIndicator" // AppKit's spinner's, as an Activity's
			}
			if r := GoString(Send(n.view, "accessibilityRole")); r != role {
				fatalf(t, "role %q", r)
			}
		}
		if SendInt(ring.view, "isIndeterminate") != 0 || SendInt(spinner.view, "isIndeterminate") == 0 {
			fatal(t, "the ring is indeterminate, or the spinner isn't")
		}
		if v := MsgFloat(ring.view, Sel("doubleValue")); v != 0.25 {
			fatalf(t, "value %v", v)
		}
		if f := MsgRect(ring.view, Sel("frame")).Size; f.Width != 32 || f.Height != 32 || SendInt(ring.view, "controlSize") != 0 {
			fatalf(t, "the regular ring is %+v", f)
		}
		if s := MsgRect(small.view, Sel("frame")).Size; s.Width != 16 || s.Height != 16 || SendInt(small.view, "controlSize") != 1 {
			fatalf(t, "the small ring is %+v, control size %d", s, SendInt(small.view, "controlSize"))
		}
		// AppKit draws its circle at its control sizes only: a larger
		// frame centers the regular one.
		if f := MsgRect(large.view, Sel("frame")); f.Size.Width != 32 || f.Size.Height != 32 || f.Origin.Y != 12+16 {
			fatalf(t, "the large ring is %+v", f)
		}
		h.enter(func() { ui.Post(func() { done.Set(2) }) })
		h.runPosted()
		if v := MsgFloat(ring.view, Sel("doubleValue")); v != 1 {
			fatalf(t, "value %v after setting 2", v)
		}
		shot(h, "progress-ring")
	})
}
