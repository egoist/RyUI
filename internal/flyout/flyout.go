// Package flyout places a flyout window (ops.WindowFlyout) next to its
// anchor, as the desktop hosts all do: on the side its Placement names,
// flipped to the other side where that has no room and the other has,
// lined up with the anchor along that side (flipped to the other end
// where that has no room), then slid back into the work area of its
// display, then shrunk to fit it, as GTK places menus and Wayland
// compositors place popups. Coordinates are a host's own (points or
// pixels), y down; start is left, as frames are physical.
package flyout

import "github.com/egoist/asuka/ops"

// Rect is a rectangle, y down.
type Rect struct{ X, Y, W, H float64 }

// Right is r's right edge, Bottom its bottom edge.
func (r Rect) Right() float64  { return r.X + r.W }
func (r Rect) Bottom() float64 { return r.Y + r.H }

// Contains reports whether the point x, y is in r.
func (r Rect) Contains(x, y float64) bool {
	return x >= r.X && y >= r.Y && x < r.Right() && y < r.Bottom()
}

// Center returns a w by h rectangle centered in r, its top a third of the
// way down the room left (as Spotlight's panel shows), within r.
func Center(w, h float64, r Rect) Rect {
	w, h = min(w, r.W), min(h, r.H)
	return Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/3, W: w, H: h}
}

type side uint8

const (
	bottom side = iota
	top
	right
	left
)

type align uint8

const (
	center align = iota
	start
	end
)

// split is p's side and alignment.
func split(p ops.Placement) (side, align) {
	if p < 0 || p > ops.PlacementLeftEnd {
		p = ops.PlacementBottom
	}
	return side(p / 3), align(p % 3)
}

// Place returns the frame of a w by h flyout next to anchor, gap away,
// as p says, within work (the work area of the display the anchor is on).
func Place(anchor Rect, w, h float64, p ops.Placement, gap float64, work Rect) Rect {
	s, a := split(p)
	vertical := s == bottom || s == top
	// Along the side: the flyout's extent across it and the room on each
	// side of the anchor.
	var r Rect
	r.W, r.H = w, h
	if vertical {
		after := work.Bottom() - (anchor.Bottom() + gap)
		before := anchor.Y - gap - work.Y
		if s == bottom && h > after && before > after {
			s = top
		} else if s == top && h > before && after > before {
			s = bottom
		}
		if s == bottom {
			r.Y = anchor.Bottom() + gap
		} else {
			r.Y = anchor.Y - gap - h
		}
		r.X = lineUp(anchor.X, anchor.W, w, a, work.X, work.Right())
	} else {
		after := work.Right() - (anchor.Right() + gap)
		before := anchor.X - gap - work.X
		if s == right && w > after && before > after {
			s = left
		} else if s == left && w > before && after > before {
			s = right
		}
		if s == right {
			r.X = anchor.Right() + gap
		} else {
			r.X = anchor.X - gap - w
		}
		r.Y = lineUp(anchor.Y, anchor.H, h, a, work.Y, work.Bottom())
	}
	// Slid back into the work area, then shrunk to it.
	r.W, r.H = min(r.W, work.W), min(r.H, work.H)
	r.X = max(min(r.X, work.Right()-r.W), work.X)
	r.Y = max(min(r.Y, work.Bottom()-r.H), work.Y)
	return r
}

// lineUp is where a flyout size long starts along an anchor at pos, n
// long, lined up as a says; an end with no room flips to the other end
// where that has room.
func lineUp(pos, n, size float64, a align, lo, hi float64) float64 {
	switch a {
	case start:
		if pos+size > hi && pos+n-size >= lo {
			return pos + n - size
		}
		return pos
	case end:
		if pos+n-size < lo && pos+size <= hi {
			return pos
		}
		return pos + n - size
	}
	return pos + (n-size)/2
}
