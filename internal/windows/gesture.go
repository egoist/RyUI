//go:build windows && amd64

package windows

import "unsafe"

// Windows Touch gestures (WM_GESTURE): a touch screen's two-finger zoom
// and rotate.

const (
	WM_GESTURE = 0x0119

	GID_BEGIN  = 1
	GID_END    = 2
	GID_ZOOM   = 3
	GID_PAN    = 4
	GID_ROTATE = 5

	GF_BEGIN   = 0x1
	GF_INERTIA = 0x2
	GF_END     = 0x4

	MK_CONTROL = 0x0008
)

// GestureInfo is GESTUREINFO.
type GestureInfo struct {
	Size       uint32
	Flags      uint32
	ID         uint32
	_          uint32
	Target     uintptr
	X, Y       int16 // POINTS, screen pixels
	Instance   uint32
	Sequence   uint32
	_          uint32
	Arguments  uint64
	ExtraBytes uint32
	_          uint32
}

var (
	procGetGestureInfo         = user32.NewProc("GetGestureInfo")
	procCloseGestureInfoHandle = user32.NewProc("CloseGestureInfoHandle")
	procSetGestureConfig       = user32.NewProc("SetGestureConfig")
)

// Gesture reads a WM_GESTURE's lParam (the handle stays open: the
// message goes on to DefWindowProc, which closes it, or CloseGesture).
func Gesture(lp uintptr) (GestureInfo, bool) {
	gi := GestureInfo{Size: uint32(unsafe.Sizeof(GestureInfo{}))}
	r, _, _ := procGetGestureInfo.Call(lp, uintptr(unsafe.Pointer(&gi)))
	return gi, r != 0
}

// CloseGesture closes a WM_GESTURE's handle, for a gesture handled
// without DefWindowProc.
func CloseGesture(lp uintptr) { procCloseGestureInfoHandle.Call(lp) }

// AllowGestures has hwnd get zoom and rotate gestures (rotate is off by
// default) and keeps the other gestures as they are.
func AllowGestures(hwnd uintptr, zoom, rotate bool) {
	type config struct{ ID, Want, Block uint32 }
	c := []config{{GID_ZOOM, 0, 0}, {GID_ROTATE, 0, 0}}
	if zoom {
		c[0].Want = 1 // GC_ZOOM
	} else {
		c[0].Block = 1
	}
	if rotate {
		c[1].Want = 1 // GC_ROTATE
	} else {
		c[1].Block = 1
	}
	procSetGestureConfig.Call(hwnd, 0, uintptr(len(c)), uintptr(unsafe.Pointer(&c[0])), unsafe.Sizeof(c[0]))
}

// RotateAngle is a GID_ROTATE's angle in radians
// (GID_ROTATE_ANGLE_FROM_ARGUMENT).
func RotateAngle(arg uint64) float64 {
	const pi = 3.14159265
	return float64(uint32(arg))/65535*4*pi - 2*pi
}
