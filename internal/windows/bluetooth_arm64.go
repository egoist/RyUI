//go:build windows

package windows

import (
	"math"
	"syscall"
	"unsafe"
)

// comCallGUIDOp calls a COM method that takes a GUID by value, then arg
// (if hasArg), then the out pointer of the operation it answers
// (GattServiceProvider.CreateAsync, GattLocalService.
// CreateCharacteristicAsync, GattLocalCharacteristic.CreateDescriptorAsync):
// arm64 passes a 16-byte composite that isn't all floats in two integer
// registers.
func comCallGUIDOp(obj uintptr, i int, g GUID, arg uintptr, hasArg bool) (hr, op uintptr) {
	lo := uintptr(*(*uint64)(unsafe.Pointer(&g)))
	hi := uintptr(*(*uint64)(unsafe.Add(unsafe.Pointer(&g), 8)))
	if hasArg {
		hr = ComCall(obj, i, lo, hi, arg, uintptr(unsafe.Pointer(&op)))
	} else {
		hr = ComCall(obj, i, lo, hi, uintptr(unsafe.Pointer(&op)))
	}
	return hr, op
}

// rectCall is fcallTramp's frame (fcall_arm64.go) and the out pointer's
// target, together on the heap.
type rectCall struct {
	fr fframe
	op uintptr
}

// comCallRectOp calls a COM method that takes a Windows.Foundation.Rect
// (four float32s) by value and the out pointer of the operation it
// answers (DevicePicker.PickSingleDeviceAsync): on arm64 a homogeneous
// float aggregate, in S0 to S3, through fcallTramp; the out pointer in
// X1.
func comCallRectOp(obj uintptr, i int, r [4]float32) (hr, op uintptr) {
	c := new(rectCall)
	vtbl := *(*uintptr)(native(obj))
	c.fr.fn = *(*uintptr)(unsafe.Add(native(vtbl), i*int(unsafe.Sizeof(uintptr(0)))))
	c.fr.x[0] = obj
	c.fr.x[1] = uintptr(unsafe.Pointer(&c.op))
	for k, f := range r {
		c.fr.v[k] = uint64(math.Float32bits(f))
	}
	hr, _, _ = syscall.SyscallN(fcallTrampAddr, uintptr(unsafe.Pointer(&c.fr)))
	return hr, c.op
}
