//go:build windows && amd64

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
)

// Save remembers the graphics state (transform, clip), for Restore.
func (g Graphics) Save() uint32 {
	var s uint32
	gdipSaveGraphics.Call(uintptr(g), uintptr(unsafe.Pointer(&s)))
	return s
}

func (g Graphics) Restore(s uint32) { gdipRestoreGraphics.Call(uintptr(g), uintptr(s)) }

func (g Graphics) Scale(x, y float32) { gdipScaleTransform.Call(uintptr(g), f(x), f(y), 0) }

// Rotate turns the world clockwise by degrees (y down).
func (g Graphics) Rotate(degrees float32) { gdipRotateTransform.Call(uintptr(g), f(degrees), 0) }

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
	gdipCreateFont.Call(fam, f(size), style, 2, uintptr(unsafe.Pointer(&font))) // UnitPixel
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
