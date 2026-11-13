//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// Paging Scrolls and PageControls, as WinUI's FlipView and PipsPager: a
// paging scroll view moves a whole page per notch of the wheel (a
// precision touchpad's small steps add up to one), with no scroll bar,
// and a PageControl is a row of pips, the current one larger and darker.

const (
	pipCell  = 12.0 // a pip and its gap
	pipOn    = 6.0  // the current pip's diameter
	pipOff   = 4.0
	pipsHigh = 24.0
)

// pageWheel turns a paging Scroll's wheel into pages; true: it took it.
func (h *Host) pageWheel(n *node, horizontal bool, delta int32) bool {
	tilt := horizontal // a tilted wheel, or a touchpad's sideways swipe
	if KeyDown(VK_SHIFT) && !horizontal {
		horizontal = true
	}
	if horizontal != n.horizontal {
		return false
	}
	n.wheelSum += delta
	if n.wheelSum > -WHEEL_DELTA && n.wheelSum < WHEEL_DELTA {
		return true
	}
	// Pages are physical, from the left or top. Down the wheel is the
	// next page along the content (to the left, right to left); a tilt
	// to the right is the page to the right.
	step := 1
	if n.wheelSum > 0 {
		step = -1
	}
	if tilt {
		step = -step
	} else if horizontal && h.isRTL(n) {
		step = -step
	}
	n.wheelSum = 0
	_, view, pos, _ := h.extent(n)
	if view <= 0 {
		return true
	}
	page := int(math.Round(float64(pos/view))) + step
	content, _, _, _ := h.extent(n)
	page = min(max(page, 0), max(int(math.Ceil(float64(content/view)))-1, 0))
	to := float32(page) * view
	if horizontal {
		h.scrollToOffset(n, to, 0)
	} else {
		h.scrollToOffset(n, 0, to)
	}
	h.emit(n, ops.Event{Kind: ops.EventPaged, Int: int64(page)})
	return true
}

func (h *Host) measurePips(n *node) (w, ht float32) {
	return float32(n.pages)*pipCell + pipCell, pipsHigh
}

// pipAt is the pip under x (pixels), in the reading direction.
func (h *Host) pipAt(n *node, x int32) int {
	if n.pages == 0 {
		return -1
	}
	w := h.pt(n, ClientRect(n.hwnd).W())
	i := int(math.Floor(float64((h.pt(n, x) - (w-float32(n.pages)*pipCell)/2) / pipCell)))
	if h.isRTL(n) {
		i = n.pages - 1 - i
	}
	return min(max(i, 0), n.pages-1)
}

func (h *Host) selectPip(n *node, i int) {
	if i < 0 || i >= n.pages || i == n.selected {
		return
	}
	n.selected = i
	h.redraw(n)
	h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
}

func (h *Host) paintPips(n *node, g Graphics, w, ht float32) {
	k := h.scaleOf(n)
	p := &h.theme
	x0 := (w - float32(n.pages)*pipCell*k) / 2
	rtl := h.isRTL(n)
	for i := range n.pages {
		slot := i
		if rtl {
			slot = n.pages - 1 - i
		}
		d, c := pipOff*k, p.tertiary
		if i == n.selected {
			d, c = pipOn*k, p.secondary
		}
		cx := x0 + (float32(slot)+0.5)*pipCell*k
		b := SolidBrush(c.argb())
		g.FillEllipse(b, cx-d/2, ht/2-d/2, d, d)
		b.Close()
	}
}
