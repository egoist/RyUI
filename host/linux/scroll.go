//go:build linux && !android

package linux

import (
	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// Scroll views: a GtkScrolledWindow (kinetic scrolling, overlay scroll
// bars, overshoot) around a viewport whose child is the content node's
// view, as big as the content's frame.
type scrollView struct {
	sw, viewport Ptr
	horizontal   bool
	content      *node
	hadj, vadj   Ptr
	reported     [2]float64
}

func (h *Host) makeScroll(n *node, horizontal bool) {
	s := n.scroll
	if s == nil {
		s = &scrollView{}
		n.scroll = s
	}
	s.horizontal = horizontal
	if s.sw == 0 {
		s.sw = ScrolledWindowNew()
		s.viewport = ViewportNew(0, 0)
		ViewportSetScrollToFocus(s.viewport, true)
		ScrolledWindowSetChild(s.sw, s.viewport)
		h.setInner(n, s.sw)
		s.hadj = ScrolledWindowGetHAdjustment(s.sw)
		s.vadj = ScrolledWindowGetVAdjustment(s.sw)
		Connect(s.hadj, "value-changed", func() { h.scrolled(n) })
		Connect(s.vadj, "value-changed", func() { h.scrolled(n) })
	}
	if horizontal {
		ScrolledWindowSetPolicy(s.sw, PolicyAutomatic, PolicyNever)
	} else {
		ScrolledWindowSetPolicy(s.sw, PolicyNever, PolicyAutomatic)
	}
}

func (h *Host) scrolled(n *node) {
	s := n.scroll
	x, y := AdjustmentGetValue(s.hadj), AdjustmentGetValue(s.vadj)
	if s.reported == [2]float64{x, y} {
		return
	}
	s.reported = [2]float64{x, y}
	h.emit(n, ops.Event{Kind: ops.EventScrolled, X: x, Y: y})
	h.scrollMoved(n)
}

// setContent makes c's view the scrolled content (nil: none).
func (s *scrollView) setContent(h *Host, c *node) {
	if s.content == c {
		return
	}
	s.content = c
	if c == nil {
		ViewportSetChild(s.viewport, 0)
		return
	}
	h.detach(c.widget)
	ViewportSetChild(s.viewport, c.widget)
}

func (h *Host) setScrollProp(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropScrollHorizontal:
		h.makeScroll(n, v.Int != 0)
	default:
		h.setScrollMore(n, p, v)
	}
}

// scrollTo scrolls to an offset, clamped to the content.
func (h *Host) scrollTo(n *node, x, y float64, animated bool) {
	s := n.scroll
	if s == nil {
		return
	}
	clamp := func(adj Ptr, v float64) float64 {
		return min(max(v, AdjustmentGetLower(adj)), max(AdjustmentGetUpper(adj)-AdjustmentGetPageSize(adj), 0))
	}
	AdjustmentSetValue(s.hadj, clamp(s.hadj, x))
	AdjustmentSetValue(s.vadj, clamp(s.vadj, y))
}
