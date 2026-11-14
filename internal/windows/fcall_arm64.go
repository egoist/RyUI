//go:build windows

package windows

import (
	"math"
	"syscall"
	"unsafe"
)

// On arm64 a C function takes its integer arguments in X0 to X7 and its
// floating-point ones in V0 to V7, each kind in order, but Go's calls
// load only the integer registers. fcallTramp, called through
// syscall.SyscallN with a frame, loads both from it and jumps to the
// function (fcall_arm64.s).

// fframe is what fcallTramp loads: the function, its integer arguments
// and its floating-point ones (a float32's bits in the low half).
type fframe struct {
	fn uintptr
	x  [8]uintptr
	v  [8]uint64
}

// fcallTrampPC is fcallTramp's address (fcall_arm64.s).
func fcallTrampPC() uintptr

var fcallTrampAddr = fcallTrampPC()

// fcall calls p with args, those whose bit is set in floats float32s
// (their bits, from f).
//
//go:uintptrescapes
func fcall(p *syscall.LazyProc, floats uint32, args ...uintptr) uintptr {
	if p.Find() != nil {
		return 0x80004001 // E_NOTIMPL, as a missing function
	}
	var fr fframe
	fr.fn = p.Addr()
	nx, nv := 0, 0
	for i, a := range args {
		if floats&(1<<i) != 0 {
			fr.v[nv] = uint64(a)
			nv++
		} else {
			fr.x[nx] = a
			nx++
		}
	}
	r, _, _ := syscall.SyscallN(fcallTrampAddr, uintptr(unsafe.Pointer(&fr)))
	return r
}

// ComCallDouble calls a COM method that takes a double: in D0, through
// fcallTramp.
func ComCallDouble(obj uintptr, i int, v float64) uintptr {
	vtbl := *(*uintptr)(native(obj))
	var fr fframe
	fr.fn = *(*uintptr)(unsafe.Add(native(vtbl), i*int(unsafe.Sizeof(uintptr(0)))))
	fr.x[0] = obj
	fr.v[0] = math.Float64bits(v)
	r, _, _ := syscall.SyscallN(fcallTrampAddr, uintptr(unsafe.Pointer(&fr)))
	return r
}

// ComCallRect calls a COM method that takes a RECT by value: in two
// registers on arm64 (a composite of 16 bytes).
func ComCallRect(obj uintptr, i int, r Rect) uintptr {
	return ComCall(obj, i, uintptr(uint32(r.Left))|uintptr(uint32(r.Top))<<32, uintptr(uint32(r.Right))|uintptr(uint32(r.Bottom))<<32)
}

// comCallCircle calls IGeocircleFactory.Create(BasicGeoposition, radius,
// out): the struct, three doubles (a homogeneous aggregate), in D0 to
// D2, the radius in D3, through fcallTramp.
func comCallCircle(obj uintptr, i int, lat, lon, alt, radius float64, out *uintptr) uintptr {
	vtbl := *(*uintptr)(native(obj))
	var fr fframe
	fr.fn = *(*uintptr)(unsafe.Add(native(vtbl), i*int(unsafe.Sizeof(uintptr(0)))))
	fr.x[0] = obj
	fr.x[1] = uintptr(unsafe.Pointer(out))
	fr.v[0], fr.v[1], fr.v[2], fr.v[3] = math.Float64bits(lat), math.Float64bits(lon), math.Float64bits(alt), math.Float64bits(radius)
	r, _, _ := syscall.SyscallN(fcallTrampAddr, uintptr(unsafe.Pointer(&fr)))
	return r
}
