//go:build windows

package windows

import "unsafe"

// Regions: a window's shape, a DC's clip.

var (
	procExtCreateRegion = gdi32.NewProc("ExtCreateRegion")
	procGetClipBox      = gdi32.NewProc("GetClipBox")
	procSelectClipRgn   = gdi32.NewProc("SelectClipRgn")
	procExtSelectClip   = gdi32.NewProc("ExtSelectClipRgn")
)

// RegionOf is the region made of rects (in one call: ExtCreateRegion),
// 0 for none. The caller owns it (DeleteObject), unless a window takes
// it (SetWindowRgn).
func RegionOf(rects []Rect) uintptr {
	if len(rects) == 0 {
		return 0
	}
	type rgnDataHeader struct {
		Size, Type, Count, RgnSize uint32
		Bound                      Rect
	}
	hs := int(unsafe.Sizeof(rgnDataHeader{}))
	buf := make([]byte, hs+len(rects)*16)
	hdr := (*rgnDataHeader)(unsafe.Pointer(&buf[0]))
	hdr.Size = uint32(hs)
	hdr.Type = 1 // RDH_RECTANGLES
	hdr.Count = uint32(len(rects))
	hdr.RgnSize = uint32(len(rects) * 16)
	b := rects[0]
	for i, r := range rects {
		*(*Rect)(unsafe.Pointer(&buf[hs+i*16])) = r
		b.Left, b.Top = min(b.Left, r.Left), min(b.Top, r.Top)
		b.Right, b.Bottom = max(b.Right, r.Right), max(b.Bottom, r.Bottom)
	}
	hdr.Bound = b
	r, _, _ := procExtCreateRegion.Call(0, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])))
	return r
}

// ClipBox is the smallest rectangle around what dc draws to, in its
// logical coordinates; ok is false when it draws nowhere.
func ClipBox(dc uintptr) (r Rect, ok bool) {
	c, _, _ := procGetClipBox.Call(dc, uintptr(unsafe.Pointer(&r)))
	return r, c > 1 // SIMPLEREGION or COMPLEXREGION
}

// SelectClip sets dc's clip to rgn (device coordinates); 0 removes it.
func SelectClip(dc, rgn uintptr) { procSelectClipRgn.Call(dc, rgn) }

// IntersectClipRegion narrows dc's clip to rgn (device coordinates).
func IntersectClipRegion(dc, rgn uintptr) { procExtSelectClip.Call(dc, rgn, 1) } // RGN_AND

// RedrawRect invalidates r (client coordinates) of a window, and not its
// children.
func RedrawRect(hwnd uintptr, r Rect) {
	procRedrawWindow.Call(hwnd, uintptr(unsafe.Pointer(&r)), 0, RDW_INVALIDATE)
}

// CreateRectRgn is a rectangle's region.
func CreateRectRgn(x0, y0, x1, y1 int32) uintptr {
	r, _, _ := procCreateRectRgn.Call(uintptr(x0), uintptr(y0), uintptr(x1), uintptr(y1))
	return r
}
