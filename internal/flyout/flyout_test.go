package flyout

import (
	"testing"

	"github.com/egoist/asuka/ops"
)

func TestPlace(t *testing.T) {
	work := Rect{X: 0, Y: 25, W: 1440, H: 875} // under a 25-point menu bar
	icon := Rect{X: 1200, Y: 0, W: 24, H: 25}
	for _, c := range []struct {
		name   string
		anchor Rect
		w, h   float64
		p      ops.Placement
		gap    float64
		want   Rect
	}{
		{"under a menu bar icon, centered", icon, 280, 300, ops.PlacementBottom, 0, Rect{1072, 25, 280, 300}},
		{"near the right edge, slid in", Rect{X: 1420, Y: 0, W: 20, H: 25}, 280, 300, ops.PlacementBottom, 0, Rect{1160, 25, 280, 300}},
		{"start, flipped to the end", Rect{X: 1300, Y: 100, W: 100, H: 20}, 200, 100, ops.PlacementBottomStart, 4, Rect{1200, 124, 200, 100}},
		{"end", Rect{X: 100, Y: 100, W: 100, H: 20}, 60, 50, ops.PlacementBottomEnd, 0, Rect{140, 120, 60, 50}},
		// A taskbar icon at the bottom: no room below, so above.
		{"bottom flips to top", Rect{X: 1300, Y: 870, W: 24, H: 30}, 280, 300, ops.PlacementBottom, 8, Rect{1160, 562, 280, 300}},
		{"top", Rect{X: 600, Y: 500, W: 40, H: 20}, 100, 100, ops.PlacementTop, 4, Rect{570, 396, 100, 100}},
		{"right", Rect{X: 100, Y: 300, W: 50, H: 20}, 100, 60, ops.PlacementRightStart, 0, Rect{150, 300, 100, 60}},
		{"right flips to left", Rect{X: 1400, Y: 300, W: 20, H: 20}, 100, 60, ops.PlacementRight, 0, Rect{1300, 280, 100, 60}},
		{"taller than the work area: shrunk", icon, 200, 2000, ops.PlacementBottom, 0, Rect{1112, 25, 200, 875}},
	} {
		if got := Place(c.anchor, c.w, c.h, c.p, c.gap, work); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestCenter(t *testing.T) {
	if got := Center(600, 60, Rect{X: 0, Y: 25, W: 1440, H: 875}); got != (Rect{420, 25 + 815.0/3, 600, 60}) {
		t.Fatalf("got %+v", got)
	}
}
