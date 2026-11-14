//go:build windows

package windows

import "unsafe"

// comCallGUIDOp calls a COM method that takes a GUID by value, then arg
// (if hasArg), then the out pointer of the operation it answers
// (GattServiceProvider.CreateAsync, GattLocalService.
// CreateCharacteristicAsync, GattLocalCharacteristic.CreateDescriptorAsync):
// Windows x64 passes a 16-byte struct by reference, to a copy the caller
// makes.
func comCallGUIDOp(obj uintptr, i int, g GUID, arg uintptr, hasArg bool) (hr, op uintptr) {
	p := &g
	if hasArg {
		hr = ComCall(obj, i, uintptr(unsafe.Pointer(p)), arg, uintptr(unsafe.Pointer(&op)))
	} else {
		hr = ComCall(obj, i, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&op)))
	}
	return hr, op
}

// comCallRectOp calls a COM method that takes a Windows.Foundation.Rect
// (four float32s) by value and the out pointer of the operation it
// answers (DevicePicker.PickSingleDeviceAsync): by reference on x64.
func comCallRectOp(obj uintptr, i int, r [4]float32) (hr, op uintptr) {
	p := &r
	hr = ComCall(obj, i, uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&op)))
	return hr, op
}
