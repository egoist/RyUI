//go:build windows

package windows

import "unsafe"

// PaintPending paints the windows of this thread waiting to be painted,
// through their WM_PAINT.
func PaintPending() {
	var m Msg
	for range 1000 {
		r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, WM_PAINT, WM_PAINT, 1) // PM_REMOVE
		if r == 0 {
			return
		}
		DispatchMessage(&m)
	}
}
