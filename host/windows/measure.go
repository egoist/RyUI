//go:build windows && amd64

package windows

import (
	"math"

	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
)

const big = 1e6

func finite(f float32) bool { return f == f && f < layout.Inf && f > -layout.Inf }

// avail is the width a view may take, in points: the known size, the
// available space, unbounded for max-content, minContent for
// min-content.
func avail(known, space float32, minContent float32) float32 {
	if known == known {
		return known
	}
	if space >= layout.Inf {
		return big
	}
	if space <= -layout.Inf {
		return minContent
	}
	return max(space, 0)
}

// measure sizes a view as the control draws itself, in points, rounded
// up to pixels.
func (h *Host) measure(n *node, c layout.Constraints) layout.Size {
	var w, ht float32
	switch n.kind {
	case ops.KindText:
		w, ht = h.measureText(n, c)
	case ops.KindButton:
		w, ht = h.measureButton(n)
	case ops.KindTextField:
		w, ht = 200, 32
		if c.AvailW <= -layout.Inf {
			w = 80
		} else if finite(c.AvailW) {
			w = min(w, max(c.AvailW, 0))
		}
		if n.multiline {
			ht = h.measureMultiline(n, c)
		}
	case ops.KindToggle:
		w, ht = 40, 20
	case ops.KindSlider:
		w, ht = 200, 32
		if finite(c.AvailW) {
			w = max(c.AvailW, 0)
		}
	case ops.KindProgress:
		w, ht = 200, 4
		if finite(c.AvailW) {
			w = max(c.AvailW, 0)
		}
	case ops.KindActivity:
		w, ht = 32, 32
	case ops.KindStepper:
		w, ht = 92, 32
	case ops.KindSegmented:
		widest := float32(0)
		dc := h.measureDC
		old := SelectObject(dc, h.nodeFont(n))
		for _, s := range n.items {
			tw, _ := TextExtent(dc, UTF16Units(s))
			widest = max(widest, h.pt(tw))
		}
		SelectObject(dc, old)
		w, ht = (widest+28)*float32(len(n.items)), 32
	case ops.KindWheel:
		widest := float32(0)
		dc := h.measureDC
		old := SelectObject(dc, h.nodeFont(n))
		for _, s := range n.items {
			tw, _ := TextExtent(dc, UTF16Units(s))
			widest = max(widest, h.pt(tw))
		}
		SelectObject(dc, old)
		w, ht = max(widest+48, 120), 32
	case ops.KindDatePicker:
		var sz struct{ Cx, Cy int32 }
		SendMessage(n.ctl, DTM_GETIDEALSIZE, 0, Ptr(&sz))
		w, ht = max(h.pt(sz.Cx)+8, 100), max(h.pt(sz.Cy), 32)
	case ops.KindColorPicker:
		w, ht = 44, 32
	case ops.KindSymbol:
		s := h.nodeSpec(n)
		w, ht = s.size*1.2, s.size*1.2
	case ops.KindImage:
		if b := n.image.bmp; b != nil {
			w, ht = float32(b.W), float32(b.H)
		}
	case ops.KindRichText:
		w, ht = n.rich.measure(h, n, c)
	}
	if c.Width == c.Width {
		w = c.Width
	}
	if c.Height == c.Height {
		ht = c.Height
	}
	k := h.scale
	return layout.Size{W: float32(math.Ceil(float64(w*k-0.01))) / k, H: float32(math.Ceil(float64(ht*k-0.01))) / k}
}

// measureText measures a text with DrawText in the font it's drawn in;
// its min-content width is its widest word.
func (h *Host) measureText(n *node, c layout.Constraints) (w, ht float32) {
	if len(n.utext) == 0 {
		return 0, 0
	}
	dc := h.measureDC
	old := SelectObject(dc, h.nodeFont(n))
	defer SelectObject(dc, old)
	widest := float32(0)
	if c.AvailW <= -layout.Inf && c.Width != c.Width {
		widest = h.widestWord(dc, n.utext)
	}
	limit := avail(c.Width, c.AvailW, widest)
	r := Rect{Right: int32(math.Floor(float64(limit*h.scale) + 0.01))}
	if limit >= big {
		r.Right = 1 << 24
	}
	f := textFormat(n) | DT_CALCRECT
	DrawText(dc, n.utext, &r, f)
	if n.lineLimit > 1 {
		tm := TextMetrics(dc)
		r.Bottom = min(r.Bottom, tm.Height*int32(n.lineLimit))
	}
	return h.pt(r.Right), h.pt(r.Bottom)
}

// widestWord is a text's widest word, in points.
func (h *Host) widestWord(dc uintptr, s []uint16) float32 {
	widest := int32(0)
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ' ' || s[i] == '\n' || s[i] == '\t' {
			if i > start {
				w, _ := TextExtent(dc, s[start:i])
				widest = max(widest, w)
			}
			start = i + 1
		}
	}
	return h.pt(widest)
}

// measureButton is Fluent's button: its title, an icon before it,
// padded 11 points across, at least 32 tall (and as wide).
func (h *Host) measureButton(n *node) (w, ht float32) {
	dc := h.measureDC
	old := SelectObject(dc, h.nodeFont(n))
	tw, th := TextExtent(dc, UTF16Units(n.title))
	SelectObject(dc, old)
	w = h.pt(tw)
	if symbolGlyph(n.icon) != 0 {
		w += 16 * h.fontScale()
		if tw > 0 {
			w += 8
		}
	}
	return max(w+24, 32), max(h.pt(th)+12, 32)
}

// measureMultiline is a multiline field's height: its text's, up to its
// line limit, padded.
func (h *Host) measureMultiline(n *node, c layout.Constraints) float32 {
	w := float32(200)
	if c.Width == c.Width {
		w = c.Width
	} else if finite(c.AvailW) {
		w = max(c.AvailW, 0)
	}
	dc := h.measureDC
	old := SelectObject(dc, h.nodeFont(n))
	defer SelectObject(dc, old)
	tm := TextMetrics(dc)
	s := UTF16Units(n.text)
	if len(s) == 0 || s[len(s)-1] == '\n' {
		s = append(s, ' ')
	}
	r := Rect{Right: max(h.px(w-20), 1)}
	DrawText(dc, s, &r, DT_CALCRECT|DT_WORDBREAK|DT_EDITCONTROL|DT_NOPREFIX)
	lines := max(r.Bottom, tm.Height)
	if n.lineLimit > 0 {
		lines = min(lines, tm.Height*int32(n.lineLimit))
	}
	return h.pt(lines) + 12
}
