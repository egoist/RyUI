//go:build darwin && !ios

package darwin

import (
	"math"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// Paging Scrolls and PageControls. AppKit has neither: a paging scroll
// view settles on the nearest page when a live scroll (a drag, a swipe
// and its momentum) ends, and a PageControl is drawn, its dots as iOS's,
// the current one in the accent color.

const (
	dotSize  = 7.0  // a dot's diameter
	dotCell  = 14.0 // a dot and its gap
	dotsHigh = 24.0
)

// pageDots is a PageControl's state.
type pageDots struct{ count, page int }

func registerPageDotsClass(flipped objc.MethodDef) {
	DefineClass("AsukaPageDots", "NSView", nil, []objc.MethodDef{
		flipped,
		Method("acceptsFirstMouse:", func(self ID, _ SEL, ev ID) bool { return true }),
		Method("drawRect:", func(self ID, _ SEL, dirty Rect) {
			if n := theHost.byView[self]; n != nil && n.dots != nil {
				theHost.drawDots(n)
			}
		}),
		Method("mouseDown:", func(self ID, _ SEL, ev ID) {
			n := theHost.byView[self]
			if n == nil || n.dots == nil || n.dots.count == 0 {
				return
			}
			p := MsgPointFromID(self, Sel("convertPoint:fromView:"), MsgPoint(ev, Sel("locationInWindow")), 0)
			theHost.chooseDot(n, theHost.dotAt(n, p.X))
		}),
		// VoiceOver adjusts it, as a slider.
		Method("accessibilityPerformIncrement", func(self ID, _ SEL) bool {
			if n := theHost.byView[self]; n != nil && n.dots != nil {
				theHost.chooseDot(n, n.dots.page+1)
			}
			return true
		}),
		Method("accessibilityPerformDecrement", func(self ID, _ SEL) bool {
			if n := theHost.byView[self]; n != nil && n.dots != nil {
				theHost.chooseDot(n, n.dots.page-1)
			}
			return true
		}),
	})
}

func (h *Host) makePageDots(n *node) ID {
	n.dots = &pageDots{}
	v := initView("AsukaPageDots")
	Send(v, "setAccessibilityElement:", 1)
	Send(v, "setAccessibilityRole:", uintptr(String("AXSlider")))
	return v
}

func (h *Host) setPageDots(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropPageControlCount:
		n.dots.count = max(int(v.Int), 0)
	case ops.PropPageControlPage:
		n.dots.page = int(v.Int)
	}
	if d := n.dots; d.count > 0 {
		Send(n.view, "setAccessibilityValue:", uintptr(String(itoa(d.page+1)+"/"+itoa(d.count))))
	}
	Send(n.view, "setNeedsDisplay:", 1)
}

func (h *Host) measureDots(n *node) Size {
	return Size{Width: float64(n.dots.count)*dotCell + dotCell, Height: dotsHigh}
}

// dotAt is the dot under x, in the reading direction.
func (h *Host) dotAt(n *node, x float64) int {
	d := n.dots
	b := MsgRect(n.view, Sel("bounds"))
	i := int(math.Floor((x - (b.Size.Width-float64(d.count)*dotCell)/2) / dotCell))
	if h.isRTL(n) {
		i = d.count - 1 - i
	}
	return min(max(i, 0), d.count-1)
}

func (h *Host) chooseDot(n *node, i int) {
	d := n.dots
	if i < 0 || i >= d.count || i == d.page {
		return
	}
	d.page = i
	Send(n.view, "setNeedsDisplay:", 1)
	h.emit(n, ops.Event{Kind: ops.EventSelected, Int: int64(i)})
}

func (h *Host) drawDots(n *node) {
	d := n.dots
	b := MsgRect(n.view, Sel("bounds"))
	x0 := (b.Size.Width-float64(d.count)*dotCell)/2 + (dotCell-dotSize)/2
	y := (b.Size.Height - dotSize) / 2
	on := Send(Class("NSColor"), "controlAccentColor")
	off := Send(Class("NSColor"), "tertiaryLabelColor")
	rtl := h.isRTL(n)
	for i := range d.count {
		slot := i
		if rtl {
			slot = d.count - 1 - i
		}
		c := off
		if i == d.page {
			c = on
		}
		Send(c, "set")
		r := Rect{Origin: Point{X: x0 + float64(slot)*dotCell, Y: y}, Size: Size{Width: dotSize, Height: dotSize}}
		Send(MsgRectToID(Class("NSBezierPath"), Sel("bezierPathWithOvalInRect:"), r), "fill")
	}
}

// setPaging makes a Scroll settle on whole pages, its scroll bars hidden.
func (h *Host) setPaging(n *node, on bool) {
	if on == n.paging {
		return
	}
	n.paging = on
	Send(n.view, "setHasHorizontalScroller:", Bool(!on && n.horizontal))
	Send(n.view, "setHasVerticalScroller:", Bool(!on && !n.horizontal))
	center := Send(Class("NSNotificationCenter"), "defaultCenter")
	name := String("NSScrollViewDidEndLiveScrollNotification")
	if on {
		Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("liveScrollEnded:")), uintptr(name), uintptr(n.view))
	} else {
		Send(center, "removeObserver:name:object:", uintptr(h.target), uintptr(name), uintptr(n.view))
	}
}

// settlePage scrolls a paging Scroll to the page nearest what it shows,
// animated, and reports it.
func (h *Host) settlePage(view ID) {
	n := h.byView[view]
	if n == nil || !n.paging {
		return
	}
	clip := Send(view, "contentView")
	b := MsgRect(clip, Sel("bounds"))
	size, off := b.Size.Height, b.Origin.Y
	if n.horizontal {
		size, off = b.Size.Width, b.Origin.X
	}
	if size <= 0 {
		return
	}
	page := math.Round(off / size)
	x, y := 0.0, page*size
	if n.horizontal {
		x, y = page*size, 0
	}
	h.scrollToOffset(n, x, y, true)
	h.emit(n, ops.Event{Kind: ops.EventPaged, Int: int64(page)})
}
