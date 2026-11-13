//go:build windows

package windows

import "unsafe"

// What the host's map view (maps.go) draws with: tiles stretched with
// GDI's halftone, and touch gestures as a map takes them.

var (
	procStretchBlt        = gdi32.NewProc("StretchBlt")
	procSetStretchBltMode = gdi32.NewProc("SetStretchBltMode")
	procSetBrushOrgEx     = gdi32.NewProc("SetBrushOrgEx")
)

// StretchBlt copies src's sw×sh pixels at sx, sy to w×h at x, y of dst,
// averaged (halftone) when smaller.
func StretchBlt(dst uintptr, x, y, w, h int32, src uintptr, sx, sy, sw, sh int32) {
	procSetStretchBltMode.Call(dst, 4) // HALFTONE
	procSetBrushOrgEx.Call(dst, 0, 0, 0)
	procStretchBlt.Call(dst, uintptr(x), uintptr(y), uintptr(w), uintptr(h), src, uintptr(sx), uintptr(sy), uintptr(sw), uintptr(sh), SRCCOPY)
}

// MapGestures has hwnd get a touch screen's zoom, and its pan in any
// direction with one finger (no gutter: a map moves diagonally), with
// inertia.
func MapGestures(hwnd uintptr) {
	type config struct{ ID, Want, Block uint32 }
	c := []config{
		{GID_ZOOM, 1, 0},
		{GID_ROTATE, 0, 1},
		{GID_PAN, 0x1 | 0x2 | 0x4 | 0x10, 0x8}, // GC_PAN, single finger vertically, horizontally, inertia; no gutter
	}
	procSetGestureConfig.Call(hwnd, 0, uintptr(len(c)), uintptr(unsafe.Pointer(&c[0])), unsafe.Sizeof(c[0]))
}
