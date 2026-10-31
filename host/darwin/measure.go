//go:build darwin && !ios

package darwin

import (
	"math"
	"strings"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/layout"
	"github.com/egoist/ryui/ops"
)

const big = 1e7

func finite(f float32) bool { return f == f && f < layout.Inf && f > -layout.Inf }

// fit is the width (or height) a view may take: the known size, the
// available space, unbounded for max-content, minContent for
// min-content.
func fit(known, avail float32, minContent float64) float64 {
	if known == known {
		return float64(known)
	}
	if avail >= layout.Inf {
		return big
	}
	if avail <= -layout.Inf {
		return minContent
	}
	return float64(max(avail, 0))
}

// measure asks the real widget for its size within c.
func (h *Host) measure(n *node, c layout.Constraints) layout.Size {
	var s Size
	v := n.view
	switch n.kind {
	case ops.KindText:
		if n.text == "" {
			return layout.Size{}
		}
		widest := 0.0
		if c.AvailW <= -layout.Inf && c.Width != c.Width {
			widest = h.widestWord(n)
		}
		w := fit(c.Width, c.AvailW, widest)
		s = MsgRectToSize(Send(v, "cell"), Sel("cellSizeForBounds:"), Rect{Size: Size{Width: w, Height: big}})
		if w < big {
			s.Width = min(s.Width, w)
		}
	case ops.KindTextField:
		if n.tracking != 0 {
			s = h.measureMultiline(n, c)
			break
		}
		// A field scrolls its text: it can be narrow.
		s = MsgSize(v, Sel("intrinsicContentSize"))
		s.Width = 200
		if c.AvailW <= -layout.Inf {
			s.Width = 80
		} else if finite(c.AvailW) {
			s.Width = min(s.Width, float64(max(c.AvailW, 0)))
		}
	case ops.KindSlider, ops.KindProgress:
		s = MsgSize(v, Sel("intrinsicContentSize"))
		s.Width = 200
		if finite(c.AvailW) {
			s.Width = float64(max(c.AvailW, 0))
		}
		if s.Height <= 0 {
			s.Height = 20
		}
	case ops.KindRichText:
		s = n.rich.measure(h, n, c)
	case ops.KindSymbol:
		if img := Send(v, "image"); img != 0 {
			s = MsgSize(img, Sel("size"))
		}
	case ops.KindImage:
		if img := Send(v, "image"); img != 0 {
			s = MsgSize(img, Sel("size"))
		}
	case ops.KindWebView, ops.KindVideo, ops.KindShape, ops.KindView, ops.KindSelectable:
		s = Size{}
	case ops.KindScroll, ops.KindList:
		s = Size{}
	default:
		s = MsgSize(v, Sel("intrinsicContentSize"))
		if s.Width < 0 { // NSViewNoIntrinsicMetric
			s.Width = 0
			if finite(c.AvailW) {
				s.Width = float64(max(c.AvailW, 0))
			}
		}
		if s.Height < 0 {
			s.Height = 0
		}
	}
	if c.Width == c.Width {
		s.Width = float64(c.Width)
	}
	if c.Height == c.Height {
		s.Height = float64(c.Height)
	}
	k := float64(h.env.Scale)
	if len(h.wins) > 0 {
		// Rounded to its own window's pixels: a Window may be on a display
		// of another scale.
		if w := Send(v, "window"); w != 0 {
			k = MsgFloat(w, Sel("backingScaleFactor"))
		}
	}
	if k <= 0 {
		k = 2
	}
	return layout.Size{W: float32(math.Ceil(s.Width*k) / k), H: float32(math.Ceil(s.Height*k) / k)}
}

// widestWord is a text's min-content width: its widest word.
func (h *Host) widestWord(n *node) float64 {
	attrs := Dict(Const(AppKit(), "NSFontAttributeName"), Send(n.view, "font"))
	widest := 0.0
	for _, w := range strings.Fields(n.text) {
		sz := MsgIDToSize(String(w), Sel("sizeWithAttributes:"), attrs)
		widest = max(widest, sz.Width)
	}
	// The label's own insets, as cellSizeForBounds: adds them.
	return math.Ceil(widest) + 4
}

// measureMultiline sizes a multiline field: as wide as a field would be,
// as tall as its text, up to its line limit.
func (h *Host) measureMultiline(n *node, c layout.Constraints) Size {
	w := 200.0
	if c.Width == c.Width {
		w = float64(c.Width)
	} else if finite(c.AvailW) {
		w = float64(max(c.AvailW, 0))
	}
	tv := n.tracking
	lm := Send(tv, "layoutManager")
	tc := Send(tv, "textContainer")
	Send(lm, "ensureLayoutForTextContainer:", uintptr(tc))
	used := MsgIDToRect(lm, Sel("usedRectForTextContainer:"), tc)
	f := Send(tv, "font")
	line := MsgFloatFromID(lm, Sel("defaultLineHeightForFont:"), f)
	textH := max(used.Size.Height, line)
	if n.lineLimit > 0 {
		textH = min(textH, line*float64(n.lineLimit))
	}
	inset := MsgSize(tv, Sel("textContainerInset"))
	return Size{Width: w, Height: math.Ceil(textH + inset.Height*2 + 4)}
}
