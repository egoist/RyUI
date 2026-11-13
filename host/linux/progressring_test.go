//go:build linux && !android

package linux

import (
	"testing"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A ProgressRing draws its arc from the top, clockwise, as far as its
// value (its Color's), over its track; without a value, it's a spinner.
// It's 32 points across, or its style's size, square, and moves to a new
// value on the frame clock.
func TestProgressRing(t *testing.T) {
	done := signal.New(0.25)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(10), Gap: 10, Align: ui.AlignStart},
			ui.ProgressRing(done, ui.Style{Width: ui.Pt(64), Color: colors.RGB(0, 0, 255)}),
			ui.ProgressRing(nil),
		)
	})
	var ring, spinner *node
	onMain(func() {
		for _, n := range find(h, ops.KindProgressRing) {
			if n.inner != 0 {
				spinner = n
			} else {
				ring = n
			}
		}
		if ring == nil || spinner == nil {
			fatalf(t, "ring %v, spinner %v", ring, spinner)
		}
	})
	settle(t, h, func() bool { return ring.frame[2] > 0 && spinner.frame[2] > 0 })
	onMain(func() {
		if w, ht := ring.frame[2], ring.frame[3]; w != 64 || ht != 64 {
			fatalf(t, "the ring is %vx%v", w, ht)
		}
		if w, ht := spinner.frame[2], spinner.frame[3]; w != ringSize || ht != ringSize {
			fatalf(t, "the spinner is %vx%v", w, ht)
		}
		p := render(ring)
		width := int(ringWidth(64) / 2)
		// A quarter: the arc from the top to the right; not at the bottom
		// or the left, where the track shows.
		if c := p.at(40, width); !bluish(c) {
			fatalf(t, "no arc right of the top: %v", c)
		}
		if c := p.at(63-width, 32); !bluish(c) && !(c[2] > 150) {
			fatalf(t, "no arc at the right: %v", c)
		}
		if c := p.at(32, 63-width); bluish(c) || clear4(c) {
			fatalf(t, "the bottom isn't the track: %v", c)
		}
		if c := p.at(32, 32); !clear4(c) {
			fatalf(t, "the middle is drawn: %v", c)
		}
		h.env.ReduceMotion = false
		post(h, func() { done.Set(1) })
	})
	r := rings[ring]
	var between bool
	settle(t, h, func() bool {
		if r.shown > 0.25 && r.shown < 1 {
			between = true
		}
		return r.shown == 1 && r.anim == nil
	})
	if !between {
		t.Error("the arc jumped to its value")
	}
	onMain(func() {
		if c := render(ring).at(32, 63-int(ringWidth(64)/2)); !bluish(c) {
			fatalf(t, "the full ring's bottom isn't drawn: %v", c)
		}
	})
}
