//go:build windows

package windows

import "unsafe"

// GDI+ for a Canvas's display list: saved states, world transforms,
// intersecting clips, and text.

var (
	gdipSaveGraphics       = gdiplus.NewProc("GdipSaveGraphics")
	gdipRestoreGraphics    = gdiplus.NewProc("GdipRestoreGraphics")
	gdipScaleTransform     = gdiplus.NewProc("GdipScaleWorldTransform")
	gdipRotateTransform    = gdiplus.NewProc("GdipRotateWorldTransform")
	gdipCreateFamily       = gdiplus.NewProc("GdipCreateFontFamilyFromName")
	gdipDeleteFamily       = gdiplus.NewProc("GdipDeleteFontFamily")
	gdipCreateFont         = gdiplus.NewProc("GdipCreateFont")
	gdipDeleteFont         = gdiplus.NewProc("GdipDeleteFont")
	gdipCreateStringFormat = gdiplus.NewProc("GdipCreateStringFormat")
	gdipDeleteStringFormat = gdiplus.NewProc("GdipDeleteStringFormat")
	gdipSetFormatAlign     = gdiplus.NewProc("GdipSetStringFormatAlign")
	gdipDrawString         = gdiplus.NewProc("GdipDrawString")
	gdipCellAscent         = gdiplus.NewProc("GdipGetCellAscent")
	gdipEmHeight           = gdiplus.NewProc("GdipGetEmHeight")
	gdipCreateMatrix2      = gdiplus.NewProc("GdipCreateMatrix2")
	gdipDeleteMatrix       = gdiplus.NewProc("GdipDeleteMatrix")
	gdipMultiplyTransform  = gdiplus.NewProc("GdipMultiplyWorldTransform")
	gdipSetPathFillMode    = gdiplus.NewProc("GdipSetPathFillMode")
	gdipCreatePathGradient = gdiplus.NewProc("GdipCreatePathGradientFromPath")
	gdipSetPathGradCenter  = gdiplus.NewProc("GdipSetPathGradientCenterPoint")
	gdipSetPathGradBlend   = gdiplus.NewProc("GdipSetPathGradientPresetBlend")
	gdipCreatePen2         = gdiplus.NewProc("GdipCreatePen2")
)

// Transform multiplies the world transform by the matrix [a c tx; b d ty]
// (applied before what's there, as a nested transform is).
func (g Graphics) Transform(a, b, c, d, tx, ty float32) {
	var m uintptr
	fcall(gdipCreateMatrix2, 0x3f, f(a), f(b), f(c), f(d), f(tx), f(ty), uintptr(unsafe.Pointer(&m)))
	if m == 0 {
		return
	}
	gdipMultiplyTransform.Call(uintptr(g), m, 0) // MatrixOrderPrepend
	gdipDeleteMatrix.Call(m)
}

// ExcludeClip limits drawing to outside p as well.
func (g Graphics) ExcludeClip(p Path) { gdipSetClipPath.Call(uintptr(g), uintptr(p), 4) } // CombineModeExclude

// SetFillMode fills p by the even-odd rule (GDI+'s alternate) or the
// nonzero one (winding).
func (p Path) SetFillMode(evenOdd bool) {
	mode := uintptr(1) // FillModeWinding
	if evenOdd {
		mode = 0
	}
	gdipSetPathFillMode.Call(uintptr(p), mode)
}

// RadialBrush is a gradient out from (fx, fy) to the circle of radius r
// around (cx, cy) through colors at positions (0, the center, to 1, the
// circle): a path gradient over the circle. It paints only the circle:
// RadialFill fills the rest in the last color.
func RadialBrush(cx, cy, r, fx, fy float32, colors []ARGB, positions []float32) Brush {
	if len(colors) < 2 || r <= 0 {
		if len(colors) > 0 {
			return SolidBrush(colors[len(colors)-1])
		}
		return 0
	}
	circle := NewPath()
	defer circle.Close()
	circle.Ellipse(cx-r, cy-r, 2*r, 2*r)
	var b uintptr
	gdipCreatePathGradient.Call(uintptr(circle), uintptr(unsafe.Pointer(&b)))
	if b == 0 {
		return 0
	}
	focus := struct{ X, Y float32 }{fx, fy}
	gdipSetPathGradCenter.Call(b, uintptr(unsafe.Pointer(&focus)))
	// GDI+'s positions run from the boundary (0) in to the center (1).
	n := len(colors)
	cs := make([]ARGB, n)
	ps := make([]float32, n)
	for i := range n {
		cs[i] = colors[n-1-i]
		ps[i] = 1 - positions[n-1-i]
	}
	ps[0], ps[n-1] = 0, 1
	gdipSetPathGradBlend.Call(b, uintptr(unsafe.Pointer(&cs[0])), uintptr(unsafe.Pointer(&ps[0])), uintptr(n))
	return Brush(b)
}

// RadialFill fills p with a radial gradient: the last color outside its
// circle, the gradient in it.
func (g Graphics) RadialFill(p Path, cx, cy, r, fx, fy float32, colors []ARGB, positions []float32) {
	if len(colors) == 0 {
		return
	}
	s := g.Save()
	circle := NewPath()
	circle.Ellipse(cx-r, cy-r, 2*r, 2*r)
	g.ExcludeClip(circle)
	last := SolidBrush(colors[len(colors)-1])
	g.FillPath(last, p)
	last.Close()
	circle.Close()
	g.Restore(s)
	br := RadialBrush(cx, cy, r, fx, fy, colors, positions)
	g.FillPath(br, p)
	br.Close()
}

// NewBrushPen is a pen painting with a brush (a gradient).
func NewBrushPen(b Brush, width float32) Pen {
	var p uintptr
	fcall(gdipCreatePen2, 0x2, uintptr(b), f(width), 2, uintptr(unsafe.Pointer(&p))) // UnitPixel
	return Pen(p)
}

// Save remembers the graphics state (transform, clip), for Restore.
func (g Graphics) Save() uint32 {
	var s uint32
	gdipSaveGraphics.Call(uintptr(g), uintptr(unsafe.Pointer(&s)))
	return s
}

func (g Graphics) Restore(s uint32) { gdipRestoreGraphics.Call(uintptr(g), uintptr(s)) }

func (g Graphics) Scale(x, y float32) { fcall(gdipScaleTransform, 0x6, uintptr(g), f(x), f(y), 0) }

// Rotate turns the world clockwise by degrees (y down).
func (g Graphics) Rotate(degrees float32) { fcall(gdipRotateTransform, 0x2, uintptr(g), f(degrees), 0) }

// IntersectClip limits drawing to inside p as well.
func (g Graphics) IntersectClip(p Path) { gdipSetClipPath.Call(uintptr(g), uintptr(p), 1) } // CombineModeIntersect

// DrawText draws s on one line in the family at size pixels, bold or
// not, its baseline at y; x is its left edge, center or right edge
// (align 0, 1, 2).
func (g Graphics) DrawText(s, family string, size float32, bold bool, c ARGB, align int, x, y float32) {
	var fam, font, format uintptr
	gdipCreateFamily.Call(Str(family), 0, uintptr(unsafe.Pointer(&fam)))
	if fam == 0 {
		return
	}
	defer gdipDeleteFamily.Call(fam)
	style := uintptr(0)
	if bold {
		style = 1
	}
	fcall(gdipCreateFont, 0x2, fam, f(size), style, 2, uintptr(unsafe.Pointer(&font))) // UnitPixel
	if font == 0 {
		return
	}
	defer gdipDeleteFont.Call(font)
	var ascent, em uint16
	gdipCellAscent.Call(fam, style, uintptr(unsafe.Pointer(&ascent)))
	gdipEmHeight.Call(fam, style, uintptr(unsafe.Pointer(&em)))
	top := y
	if em > 0 {
		top = y - size*float32(ascent)/float32(em)
	}
	gdipCreateStringFormat.Call(0x00001000, 0, uintptr(unsafe.Pointer(&format))) // StringFormatFlagsNoWrap
	if format == 0 {
		return
	}
	defer gdipDeleteStringFormat.Call(format)
	gdipSetFormatAlign.Call(format, uintptr(align)) // Near, Center, Far
	b := SolidBrush(c)
	defer b.Close()
	rect := [4]float32{x, top, 0, 0} // no box: aligned around x
	units := UTF16Units(s)
	if len(units) == 0 {
		return
	}
	gdipDrawString.Call(uintptr(g), uintptr(unsafe.Pointer(&units[0])), uintptr(len(units)), font,
		uintptr(unsafe.Pointer(&rect)), format, uintptr(b))
}
