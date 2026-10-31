//go:build windows && amd64

package windows

import (
	"math"
	"syscall"
	"unsafe"
)

// GDI: fonts, text, brushes and device contexts.

const (
	TRANSPARENT = 1
	OPAQUE      = 2

	DT_TOP           = 0x00000000
	DT_LEFT          = 0x00000000
	DT_CENTER        = 0x00000001
	DT_RIGHT         = 0x00000002
	DT_VCENTER       = 0x00000004
	DT_BOTTOM        = 0x00000008
	DT_WORDBREAK     = 0x00000010
	DT_SINGLELINE    = 0x00000020
	DT_EXPANDTABS    = 0x00000040
	DT_NOCLIP        = 0x00000100
	DT_CALCRECT      = 0x00000400
	DT_NOPREFIX      = 0x00000800
	DT_EDITCONTROL   = 0x00002000
	DT_END_ELLIPSIS  = 0x00008000
	DT_WORD_ELLIPSIS = 0x00040000
	DT_HIDEPREFIX    = 0x00100000

	FW_NORMAL   = 400
	FW_SEMIBOLD = 600
	FW_BOLD     = 700

	CLEARTYPE_QUALITY = 5

	NULL_BRUSH = 5

	SRCCOPY = 0x00CC0020
)

// RGB is a COLORREF.
func RGB(r, g, b uint8) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

func CreateFont(lf *LogFont) uintptr {
	r, _, _ := procCreateFontIndirectW.Call(uintptr(unsafe.Pointer(lf)))
	return r
}

func DeleteObject(h uintptr) {
	if h != 0 {
		procDeleteObject.Call(h)
	}
}

func SelectObject(dc, h uintptr) uintptr {
	r, _, _ := procSelectObject.Call(dc, h)
	return r
}

func StockObject(i int) uintptr {
	r, _, _ := procGetStockObject.Call(uintptr(i))
	return r
}

func CreateSolidBrush(c uint32) uintptr {
	r, _, _ := procCreateSolidBrush.Call(uintptr(c))
	return r
}

func FillRect(dc uintptr, r *Rect, brush uintptr) {
	procFillRect.Call(dc, uintptr(unsafe.Pointer(r)), brush)
}

// FillColor fills a rectangle with a COLORREF.
func FillColor(dc uintptr, r Rect, c uint32) {
	b := CreateSolidBrush(c)
	FillRect(dc, &r, b)
	DeleteObject(b)
}

func SetTextColor(dc uintptr, c uint32) { procSetTextColor.Call(dc, uintptr(c)) }
func SetBkMode(dc uintptr, mode int)    { procSetBkMode.Call(dc, uintptr(mode)) }
func SetBkColor(dc uintptr, c uint32)   { procSetBkColor.Call(dc, uintptr(c)) }

// DrawText draws (or with DT_CALCRECT, measures into r) text.
func DrawText(dc uintptr, s []uint16, r *Rect, format uint32) int32 {
	if len(s) == 0 {
		if format&DT_CALCRECT != 0 {
			r.Right, r.Bottom = r.Left, r.Top
		}
		return 0
	}
	h, _, _ := procDrawTextW.Call(dc, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)), uintptr(unsafe.Pointer(r)), uintptr(format))
	return int32(h)
}

// TextExtent is a line of text's size in the DC's font.
func TextExtent(dc uintptr, s []uint16) (w, h int32) {
	if len(s) == 0 {
		return 0, 0
	}
	var sz struct{ Cx, Cy int32 }
	procGetTextExtentPoint32W.Call(dc, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)), uintptr(unsafe.Pointer(&sz)))
	return sz.Cx, sz.Cy
}

func TextMetrics(dc uintptr) TextMetric {
	var tm TextMetric
	procGetTextMetricsW.Call(dc, uintptr(unsafe.Pointer(&tm)))
	return tm
}

// TextOut draws a string at a point.
func TextOut(dc uintptr, x, y int32, s []uint16) {
	if len(s) == 0 {
		return
	}
	procExtTextOutW.Call(dc, uintptr(x), uintptr(y), 0, 0, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)), 0)
}

// FaceName is the name of the font selected in dc.
func FaceName(dc uintptr) string {
	var buf [64]uint16
	procGetTextFaceW.Call(dc, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	return syscall.UTF16ToString(buf[:])
}

func CreateCompatibleDC(dc uintptr) uintptr {
	r, _, _ := procCreateCompatibleDC.Call(dc)
	return r
}

func CreateCompatibleBitmap(dc uintptr, w, h int32) uintptr {
	r, _, _ := procCreateCompatibleBitmap.Call(dc, uintptr(w), uintptr(h))
	return r
}

func DeleteDC(dc uintptr) { procDeleteDC.Call(dc) }

func BitBlt(dst uintptr, x, y, w, h int32, src uintptr, sx, sy int32) {
	procBitBlt.Call(dst, uintptr(x), uintptr(y), uintptr(w), uintptr(h), src, uintptr(sx), uintptr(sy), SRCCOPY)
}

// DIB is a top-down 32-bit bitmap whose pixels Go can write (BGRA).
func DIB(w, h int32) (bmp uintptr, pixels []byte) {
	type bitmapInfoHeader struct {
		Size                                uint32
		Width, Height                       int32
		Planes, BitCount                    uint16
		Compression, SizeImage              uint32
		XPels, YPels, ClrUsed, ClrImportant int32
	}
	bi := bitmapInfoHeader{Width: w, Height: -h, Planes: 1, BitCount: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))
	var bits uintptr
	r, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if r == 0 || bits == 0 {
		return 0, nil
	}
	return r, unsafe.Slice((*byte)(native(bits)), int(w)*int(h)*4)
}

func SaveDC(dc uintptr) int {
	r, _, _ := procSaveDC.Call(dc)
	return int(r)
}

func RestoreDC(dc uintptr, saved int) { procRestoreDC.Call(dc, uintptr(saved)) }

func IntersectClip(dc uintptr, r Rect) {
	procIntersectClipRect.Call(dc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom))
}

func OffsetViewport(dc uintptr, dx, dy int32) {
	procOffsetViewportOrgEx.Call(dc, uintptr(dx), uintptr(dy), 0)
}

func ViewportOrigin(dc uintptr) Point {
	var p Point
	procGetViewportOrgEx.Call(dc, uintptr(unsafe.Pointer(&p)))
	return p
}

// AddFontFile makes a font file's fonts available to this process.
func AddFontFile(path string) bool {
	r, _, _ := procAddFontResourceExW.Call(Str(path), 0x10, 0) // FR_PRIVATE
	return r != 0
}

// FontInstalled reports whether a font family is installed.
func FontInstalled(dc uintptr, family string) bool {
	lf := LogFont{CharSet: 1} // DEFAULT_CHARSET
	copy(lf.FaceName[:31], UTF16Units(family))
	found := false
	cb := syscall.NewCallback(func(lf, tm, typ, l uintptr) uintptr {
		*(*bool)(native(l)) = true
		return 0
	})
	procEnumFontFamiliesExW.Call(dc, uintptr(unsafe.Pointer(&lf)), cb, uintptr(unsafe.Pointer(&found)), 0)
	return found
}

// Buffered painting (uxtheme): a WM_PAINT draws into a bitmap that's
// copied to the window at the end, so nothing flickers.

// BeginBufferedPaint returns a memory DC to draw r of dc into, and the
// handle EndBufferedPaint takes.
func BeginBufferedPaint(dc uintptr, r *Rect) (paint, mem uintptr) {
	p, _, _ := procBeginBufferedPaint.Call(dc, uintptr(unsafe.Pointer(r)), 1, 0, uintptr(unsafe.Pointer(&mem))) // BPBF_COMPATIBLEBITMAP
	return p, mem
}

func EndBufferedPaint(paint uintptr) {
	if paint != 0 {
		procEndBufferedPaint.Call(paint, 1)
	}
}

// DrawParentBackground draws what's behind hwnd (its parent, through
// WM_ERASEBKGND and WM_PRINTCLIENT) into dc, for the parts hwnd doesn't
// cover itself.
func DrawParentBackground(hwnd, dc uintptr, r *Rect) {
	procDrawThemeParentBackground.Call(hwnd, dc, uintptr(unsafe.Pointer(r)))
}

// AlphaBlend draws a premultiplied 32-bit bitmap selected in src.
func AlphaBlend(dst uintptr, x, y, w, h int32, src uintptr, sx, sy, sw, sh int32, alpha uint8) {
	bf := uintptr(0) | uintptr(alpha)<<16 | 1<<24 // AC_SRC_OVER, SourceConstantAlpha, AC_SRC_ALPHA
	procAlphaBlend.Call(dst, uintptr(x), uintptr(y), uintptr(w), uintptr(h), src, uintptr(sx), uintptr(sy), uintptr(sw), uintptr(sh), bf)
}

// GDI+: antialiased shapes and images.

var (
	gdipStartup           = gdiplus.NewProc("GdiplusStartup")
	gdipCreateFromHDC     = gdiplus.NewProc("GdipCreateFromHDC")
	gdipDeleteGraphics    = gdiplus.NewProc("GdipDeleteGraphics")
	gdipSetSmoothing      = gdiplus.NewProc("GdipSetSmoothingMode")
	gdipSetPixelOffset    = gdiplus.NewProc("GdipSetPixelOffsetMode")
	gdipSetInterpolation  = gdiplus.NewProc("GdipSetInterpolationMode")
	gdipSetCompQuality    = gdiplus.NewProc("GdipSetCompositingQuality")
	gdipCreateSolidFill   = gdiplus.NewProc("GdipCreateSolidFill")
	gdipCreateLineBrush   = gdiplus.NewProc("GdipCreateLineBrush")
	gdipSetLineBlend      = gdiplus.NewProc("GdipSetLinePresetBlend")
	gdipDeleteBrush       = gdiplus.NewProc("GdipDeleteBrush")
	gdipCreatePen1        = gdiplus.NewProc("GdipCreatePen1")
	gdipDeletePen         = gdiplus.NewProc("GdipDeletePen")
	gdipSetPenStartCap    = gdiplus.NewProc("GdipSetPenStartCap")
	gdipSetPenEndCap      = gdiplus.NewProc("GdipSetPenEndCap")
	gdipSetPenLineJoin    = gdiplus.NewProc("GdipSetPenLineJoin")
	gdipSetPenDashArray   = gdiplus.NewProc("GdipSetPenDashArray")
	gdipSetPenDashOffset  = gdiplus.NewProc("GdipSetPenDashOffset")
	gdipCreatePath        = gdiplus.NewProc("GdipCreatePath")
	gdipDeletePath        = gdiplus.NewProc("GdipDeletePath")
	gdipAddPathArc        = gdiplus.NewProc("GdipAddPathArc")
	gdipAddPathLine       = gdiplus.NewProc("GdipAddPathLine")
	gdipAddPathBezier     = gdiplus.NewProc("GdipAddPathBezier")
	gdipAddPathRect       = gdiplus.NewProc("GdipAddPathRectangle")
	gdipAddPathEllipse    = gdiplus.NewProc("GdipAddPathEllipse")
	gdipStartFigure       = gdiplus.NewProc("GdipStartPathFigure")
	gdipCloseFigure       = gdiplus.NewProc("GdipClosePathFigure")
	gdipFillPath          = gdiplus.NewProc("GdipFillPath")
	gdipDrawPath          = gdiplus.NewProc("GdipDrawPath")
	gdipFillRect          = gdiplus.NewProc("GdipFillRectangle")
	gdipFillEllipse       = gdiplus.NewProc("GdipFillEllipse")
	gdipDrawArc           = gdiplus.NewProc("GdipDrawArc")
	gdipDrawLine          = gdiplus.NewProc("GdipDrawLine")
	gdipCreateBitmap      = gdiplus.NewProc("GdipCreateBitmapFromScan0")
	gdipDisposeImage      = gdiplus.NewProc("GdipDisposeImage")
	gdipDrawImageRectRect = gdiplus.NewProc("GdipDrawImageRectRect")
	gdipSetClipPath       = gdiplus.NewProc("GdipSetClipPath")
	gdipResetClip         = gdiplus.NewProc("GdipResetClip")
	gdipTranslate         = gdiplus.NewProc("GdipTranslateWorldTransform")
	gdipFlatten           = gdiplus.NewProc("GdipFlattenPath")
	gdipPointCount        = gdiplus.NewProc("GdipGetPointCount")
	gdipPathPoints        = gdiplus.NewProc("GdipGetPathPoints")
	gdipPathTypes         = gdiplus.NewProc("GdipGetPathTypes")
	gdipAddPathLines      = gdiplus.NewProc("GdipAddPathLine2")
	gdipCreatePath2       = gdiplus.NewProc("GdipCreatePath2")
)

var gdipToken uintptr

// StartGDIPlus starts GDI+ once.
func StartGDIPlus() {
	if gdipToken != 0 {
		return
	}
	input := struct {
		Version                  uint32
		DebugEventCallback       uintptr
		SuppressBackgroundThread int32
		SuppressExternalCodecs   int32
	}{Version: 1}
	gdipStartup.Call(uintptr(unsafe.Pointer(&gdipToken)), uintptr(unsafe.Pointer(&input)), 0)
}

func f(v float32) uintptr { return uintptr(math.Float32bits(v)) }

// ARGB is a GDI+ color.
type ARGB uint32

func MakeARGB(a, r, g, b uint8) ARGB {
	return ARGB(uint32(a)<<24 | uint32(r)<<16 | uint32(g)<<8 | uint32(b))
}

// Graphics draws antialiased into a DC.
type Graphics uintptr

func NewGraphics(dc uintptr) Graphics {
	var g uintptr
	gdipCreateFromHDC.Call(dc, uintptr(unsafe.Pointer(&g)))
	if g != 0 {
		gdipSetSmoothing.Call(g, 4)     // SmoothingModeAntiAlias
		gdipSetPixelOffset.Call(g, 4)   // PixelOffsetModeHalf: pixel centers at .5
		gdipSetInterpolation.Call(g, 7) // HighQualityBicubic
		gdipSetCompQuality.Call(g, 4)   // AssumeLinear: blends in sRGB, as WinUI does (HighQuality is gamma-corrected)
	}
	return Graphics(g)
}

func (g Graphics) Close() {
	if g != 0 {
		gdipDeleteGraphics.Call(uintptr(g))
	}
}

func (g Graphics) Translate(dx, dy float32) {
	gdipTranslate.Call(uintptr(g), f(dx), f(dy), 0)
}

// Brush is a GDI+ brush.
type Brush uintptr

func SolidBrush(c ARGB) Brush {
	var b uintptr
	gdipCreateSolidFill.Call(uintptr(c), uintptr(unsafe.Pointer(&b)))
	return Brush(b)
}

// LinearBrush is a gradient from (x0, y0) to (x1, y1) through colors at
// positions (0 to 1).
func LinearBrush(x0, y0, x1, y1 float32, colors []ARGB, positions []float32) Brush {
	if len(colors) < 2 {
		if len(colors) == 1 {
			return SolidBrush(colors[0])
		}
		return 0
	}
	p0 := struct{ X, Y float32 }{x0, y0}
	p1 := struct{ X, Y float32 }{x1, y1}
	if p0 == p1 {
		p1.X += 0.01
	}
	var b uintptr
	gdipCreateLineBrush.Call(uintptr(unsafe.Pointer(&p0)), uintptr(unsafe.Pointer(&p1)), uintptr(colors[0]), uintptr(colors[len(colors)-1]), 0, uintptr(unsafe.Pointer(&b)))
	if b != 0 {
		gdipSetLineBlend.Call(b, uintptr(unsafe.Pointer(&colors[0])), uintptr(unsafe.Pointer(&positions[0])), uintptr(len(colors)))
	}
	return Brush(b)
}

func (b Brush) Close() {
	if b != 0 {
		gdipDeleteBrush.Call(uintptr(b))
	}
}

// Pen is a GDI+ pen.
type Pen uintptr

func NewPen(c ARGB, width float32) Pen {
	var p uintptr
	gdipCreatePen1.Call(uintptr(c), f(width), 2, uintptr(unsafe.Pointer(&p))) // UnitPixel
	return Pen(p)
}

// SetCaps sets the line cap (0 flat, 2 round, 1 square) and join (0
// miter, 1 bevel, 2 round).
func (p Pen) SetCaps(cap, join int) {
	gdipSetPenStartCap.Call(uintptr(p), uintptr(cap))
	gdipSetPenEndCap.Call(uintptr(p), uintptr(cap))
	gdipSetPenLineJoin.Call(uintptr(p), uintptr(join))
}

// SetDash dashes the pen: lengths in multiples of its width, as GDI+
// takes them.
func (p Pen) SetDash(lengths []float32, offset float32) {
	if len(lengths) == 0 {
		return
	}
	gdipSetPenDashArray.Call(uintptr(p), uintptr(unsafe.Pointer(&lengths[0])), uintptr(len(lengths)))
	gdipSetPenDashOffset.Call(uintptr(p), f(offset))
}

func (p Pen) Close() {
	if p != 0 {
		gdipDeletePen.Call(uintptr(p))
	}
}

// Path is a GDI+ path.
type Path uintptr

func NewPath() Path {
	var p uintptr
	gdipCreatePath.Call(0, uintptr(unsafe.Pointer(&p))) // FillModeAlternate
	return Path(p)
}

func (p Path) Close() {
	if p != 0 {
		gdipDeletePath.Call(uintptr(p))
	}
}

func (p Path) StartFigure() { gdipStartFigure.Call(uintptr(p)) }
func (p Path) CloseFigure() { gdipCloseFigure.Call(uintptr(p)) }

func (p Path) Line(x0, y0, x1, y1 float32) {
	gdipAddPathLine.Call(uintptr(p), f(x0), f(y0), f(x1), f(y1))
}

func (p Path) Bezier(x0, y0, x1, y1, x2, y2, x3, y3 float32) {
	gdipAddPathBezier.Call(uintptr(p), f(x0), f(y0), f(x1), f(y1), f(x2), f(y2), f(x3), f(y3))
}

func (p Path) Arc(x, y, w, h, start, sweep float32) {
	gdipAddPathArc.Call(uintptr(p), f(x), f(y), f(w), f(h), f(start), f(sweep))
}

func (p Path) Rect(x, y, w, h float32) { gdipAddPathRect.Call(uintptr(p), f(x), f(y), f(w), f(h)) }

func (p Path) Ellipse(x, y, w, h float32) {
	gdipAddPathEllipse.Call(uintptr(p), f(x), f(y), f(w), f(h))
}

// RoundRect adds a rounded rectangle (r: the corners' radius).
func (p Path) RoundRect(x, y, w, h, r float32) {
	r = min(r, w/2, h/2)
	if r <= 0 {
		p.Rect(x, y, w, h)
		return
	}
	d := 2 * r
	p.StartFigure()
	p.Arc(x, y, d, d, 180, 90)
	p.Arc(x+w-d, y, d, d, 270, 90)
	p.Arc(x+w-d, y+h-d, d, d, 0, 90)
	p.Arc(x, y+h-d, d, d, 90, 90)
	p.CloseFigure()
}

func (g Graphics) FillPath(b Brush, p Path) { gdipFillPath.Call(uintptr(g), uintptr(b), uintptr(p)) }
func (g Graphics) DrawPath(pen Pen, p Path) { gdipDrawPath.Call(uintptr(g), uintptr(pen), uintptr(p)) }

func (g Graphics) FillRect(b Brush, x, y, w, h float32) {
	gdipFillRect.Call(uintptr(g), uintptr(b), f(x), f(y), f(w), f(h))
}

func (g Graphics) FillEllipse(b Brush, x, y, w, h float32) {
	gdipFillEllipse.Call(uintptr(g), uintptr(b), f(x), f(y), f(w), f(h))
}

func (g Graphics) DrawArc(pen Pen, x, y, w, h, start, sweep float32) {
	gdipDrawArc.Call(uintptr(g), uintptr(pen), f(x), f(y), f(w), f(h), f(start), f(sweep))
}

func (g Graphics) DrawLine(pen Pen, x0, y0, x1, y1 float32) {
	gdipDrawLine.Call(uintptr(g), uintptr(pen), f(x0), f(y0), f(x1), f(y1))
}

// FillRoundRect fills a rounded rectangle with a color.
func (g Graphics) FillRoundRect(c ARGB, x, y, w, h, r float32) {
	p := NewPath()
	p.RoundRect(x, y, w, h, r)
	b := SolidBrush(c)
	g.FillPath(b, p)
	b.Close()
	p.Close()
}

// StrokeRoundRect strokes a rounded rectangle inside its bounds.
func (g Graphics) StrokeRoundRect(c ARGB, width, x, y, w, h, r float32) {
	p := NewPath()
	p.RoundRect(x+width/2, y+width/2, w-width, h-width, max(r-width/2, 0))
	pen := NewPen(c, width)
	g.DrawPath(pen, p)
	pen.Close()
	p.Close()
}

// ClipPath clips what's drawn next to a path (ResetClip ends it).
func (g Graphics) ClipPath(p Path) { gdipSetClipPath.Call(uintptr(g), uintptr(p), 0) } // CombineModeReplace
func (g Graphics) ResetClip()      { gdipResetClip.Call(uintptr(g)) }

// Bitmap is a GDI+ bitmap over premultiplied BGRA pixels.
type Bitmap struct {
	h      uintptr
	W, H   int32
	pixels []byte // kept alive: GDI+ reads them
}

// NewBitmap makes a bitmap of premultiplied BGRA pixels (a copy is not
// made: pixels must not change).
func NewBitmap(w, h int32, pixels []byte) *Bitmap {
	var b uintptr
	gdipCreateBitmap.Call(uintptr(w), uintptr(h), uintptr(w*4), 0xE200B, uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&b))) // PixelFormat32bppPARGB
	if b == 0 {
		return nil
	}
	return &Bitmap{h: b, W: w, H: h, pixels: pixels}
}

func (b *Bitmap) Close() {
	if b != nil && b.h != 0 {
		gdipDisposeImage.Call(b.h)
		b.h = 0
	}
}

// DrawImage draws the part (sx, sy, sw, sh) of b into (x, y, w, h).
func (g Graphics) DrawImage(b *Bitmap, x, y, w, h, sx, sy, sw, sh float32) {
	if b == nil || b.h == 0 {
		return
	}
	gdipDrawImageRectRect.Call(uintptr(g), b.h, f(x), f(y), f(w), f(h), f(sx), f(sy), f(sw), f(sh), 2, 0, 0, 0) // UnitPixel
}

// FlattenedPoints flattens a path's curves into lines (in place) and
// returns its points, each marked where a figure starts; a closed
// figure ends with its first point again.
func FlattenedPoints(p Path) (pts [][2]float32, starts []bool) {
	gdipFlatten.Call(uintptr(p), 0, f(0.25))
	var n int32
	gdipPointCount.Call(uintptr(p), uintptr(unsafe.Pointer(&n)))
	if n <= 0 {
		return nil, nil
	}
	raw := make([][2]float32, n)
	types := make([]byte, n)
	gdipPathPoints.Call(uintptr(p), uintptr(unsafe.Pointer(&raw[0])), uintptr(n))
	gdipPathTypes.Call(uintptr(p), uintptr(unsafe.Pointer(&types[0])), uintptr(n))
	first := 0
	for i := range raw {
		start := types[i]&7 == 0
		if start {
			first = len(pts)
		}
		pts = append(pts, raw[i])
		starts = append(starts, start)
		if types[i]&0x80 != 0 && first < len(pts) {
			pts = append(pts, pts[first])
			starts = append(starts, false)
		}
	}
	return pts, starts
}
