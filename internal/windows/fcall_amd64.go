//go:build windows

package windows

import (
	"math"
	"syscall"
	"unsafe"
)

// fcall calls p with args, those whose bit is set in floats float32s
// (their bits, from f). Windows x64 passes a float in the XMM register of
// its position, and Go's calls copy the first four integer registers
// there: positions are all it takes.
//
//go:uintptrescapes
func fcall(p *syscall.LazyProc, floats uint32, args ...uintptr) uintptr {
	r, _, _ := p.Call(args...)
	return r
}

// ComCallRect calls a COM method that takes a RECT by value: by
// reference on x64.
func ComCallRect(obj uintptr, i int, r Rect) uintptr {
	return ComCall(obj, i, uintptr(unsafe.Pointer(&r)))
}

// ComCallDouble calls a COM method that takes a double: in the XMM
// register of its position, which Go's calls fill from the integer one.
func ComCallDouble(obj uintptr, i int, v float64) uintptr {
	return ComCall(obj, i, uintptr(math.Float64bits(v)))
}

// comCallCircle calls IGeocircleFactory.Create(BasicGeoposition, radius,
// out): the 24-byte struct by reference on x64, the radius in the XMM
// register of its position.
func comCallCircle(obj uintptr, i int, lat, lon, alt, radius float64, out *uintptr) uintptr {
	pos := [3]float64{lat, lon, alt}
	return ComCall(obj, i, uintptr(unsafe.Pointer(&pos)), uintptr(math.Float64bits(radius)), uintptr(unsafe.Pointer(out)))
}
