//go:build linux && !android

package linux

import (
	"math"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/layout"
	"github.com/egoist/ryui/ops"
)

const big = 1e6

func finite(f float32) bool { return f == f && f < layout.Inf && f > -layout.Inf }

// fit is the width (or height) a view may take: the known size, the
// available space, unbounded for max-content, minContent for
// min-content.
func fit(known, avail float32, minContent float32) float32 {
	if known == known {
		return known
	}
	if avail >= layout.Inf {
		return big
	}
	if avail <= -layout.Inf {
		return minContent
	}
	return max(avail, 0)
}

// widgetSize is a widget's natural size, its height for its width.
func widgetSize(w Ptr, width float32) (float32, float32) {
	var mn, nat int32
	Call(FnWidgetMeasure, w, OrientationHorizontal, ^uintptr(0), Addr(&mn), Addr(&nat), 0, 0)
	ww := float32(nat)
	if width >= 0 && width < big {
		ww = max(min(ww, width), float32(mn))
	}
	var hm, hn int32
	Call(FnWidgetMeasure, w, OrientationVertical, uintptr(int32(math.Ceil(float64(ww)))), Addr(&hm), Addr(&hn), 0, 0)
	return ww, float32(hn)
}

// widgetMin is a widget's minimum width.
func widgetMin(w Ptr) float32 {
	var mn, nat int32
	Call(FnWidgetMeasure, w, OrientationHorizontal, ^uintptr(0), Addr(&mn), Addr(&nat), 0, 0)
	return float32(mn)
}

// measure asks the real widget for its size within c.
func (h *Host) measure(n *node, c layout.Constraints) layout.Size {
	var s layout.Size
	switch n.kind {
	case ops.KindText:
		s = h.measureText(n, c)
	case ops.KindTextField:
		s = h.measureField(n, c)
	case ops.KindSlider, ops.KindProgress:
		_, ht := widgetSize(n.inner, -1)
		s = layout.Size{W: 200, H: max(ht, 20)}
		if finite(c.AvailW) {
			s.W = max(c.AvailW, 0)
		}
	case ops.KindSymbol:
		size, _ := h.font(n)
		s = layout.Size{W: size, H: size}
	case ops.KindImage:
		if n.texture != 0 {
			k := h.env.Scale
			if k <= 0 {
				k = 1
			}
			s = layout.Size{W: float32(TextureGetWidth(n.texture)), H: float32(TextureGetHeight(n.texture))}
			_ = k // one point per pixel, as Loaded reports
		}
	case ops.KindView, ops.KindScroll, ops.KindList, ops.KindWebView, ops.KindVideo,
		ops.KindShape, ops.KindCanvas, ops.KindSelectable, ops.KindShader:
	default:
		if s2, ok := h.measureMore(n, c); ok {
			s = s2
		} else if n.inner != 0 {
			w := fit(c.Width, c.AvailW, widgetMin(n.inner))
			s.W, s.H = widgetSize(n.inner, w)
		}
	}
	if c.Width == c.Width {
		s.W = c.Width
	}
	if c.Height == c.Height {
		s.H = c.Height
	}
	k := h.scaleOf(n)
	return layout.Size{W: float32(math.Ceil(float64(s.W*k))) / k, H: float32(math.Ceil(float64(s.H*k))) / k}
}

// measureText lays the text out as the label will, with Pango: as wide
// as its widest line at the width it may take, as tall as its lines (to
// its line limit); min-content is its widest word.
func (h *Host) measureText(n *node, c layout.Constraints) layout.Size {
	if n.text == "" || n.inner == 0 {
		return layout.Size{}
	}
	l := LabelGetLayout(n.inner)
	if l == 0 {
		return layout.Size{}
	}
	pl := WidgetCreatePangoLayout(n.inner, n.text)
	defer ObjectUnref(pl)
	attrs := h.attrs(n, false)
	LayoutSetAttributes(pl, attrs)
	AttrListUnref(attrs)
	LayoutSetWrap(pl, WrapWordChar)
	if n.lineLimit > 0 {
		LayoutSetHeight(pl, -int32(n.lineLimit))
		LayoutSetEllipsize(pl, EllipsizeEnd)
	}
	var w float32
	if c.Width != c.Width && c.AvailW <= -layout.Inf {
		// Min-content: the widest word.
		LayoutSetWrap(pl, WrapWord)
		LayoutSetWidth(pl, 1)
		var pw, ph int32
		LayoutGetPixelSize(pl, &pw, &ph)
		w = float32(pw)
		LayoutSetWrap(pl, WrapWordChar)
	} else {
		w = fit(c.Width, c.AvailW, 0)
	}
	if w < big {
		LayoutSetWidth(pl, int32(w*1024))
	} else {
		LayoutSetWidth(pl, -1)
	}
	var pw, ph int32
	LayoutGetPixelSize(pl, &pw, &ph)
	return layout.Size{W: min(float32(pw), w), H: float32(ph)}
}

// measureField: a field scrolls its text, so it can be narrow: 200 points,
// or what's available; a multiline one as tall as its text, to its line
// limit.
func (h *Host) measureField(n *node, c layout.Constraints) layout.Size {
	w := float32(200)
	if c.AvailW <= -layout.Inf {
		w = 80
	} else if finite(c.AvailW) {
		w = min(w, max(c.AvailW, 0))
	}
	if c.Width == c.Width {
		w = c.Width
	}
	if n.multiline && n.field != nil && n.field.view != 0 {
		return layout.Size{W: w, H: h.measureMultiline(n, w)} // field.go
	}
	_, ht := widgetSize(n.inner, w)
	return layout.Size{W: w, H: ht}
}

// lineHeight is a line of a node's font.
func (h *Host) lineHeight(n *node) float32 {
	if n.inner == 0 {
		return 20
	}
	pl := WidgetCreatePangoLayout(n.inner, "Xg")
	defer ObjectUnref(pl)
	attrs := h.attrs(n, false)
	LayoutSetAttributes(pl, attrs)
	AttrListUnref(attrs)
	var pw, ph int32
	LayoutGetPixelSize(pl, &pw, &ph)
	return float32(ph)
}

// scaleOf is the scale of the window a node is in: pixels per point.
func (h *Host) scaleOf(n *node) float32 {
	if n != nil && n.widget != 0 {
		if nat := WidgetGetNative(n.widget); nat != 0 {
			if s := NativeGetSurface(nat); s != 0 {
				if SurfaceGetScale != nil {
					return float32(SurfaceGetScale(s))
				}
				return float32(SurfaceGetScaleFactor(s))
			}
		}
	}
	if h.env.Scale > 0 {
		return h.env.Scale
	}
	return 1
}
