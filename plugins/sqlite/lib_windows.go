//go:build windows

package sqlite

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

// nativeLib is the DLL, loaded with LoadLibrary and called through
// syscall, as host/windows calls Win32.
type nativeLib struct{ fn uintptr }

func (l *nativeLib) call(op, conn, arg uintptr, in, out []byte) int {
	var ip, outp unsafe.Pointer
	if len(in) > 0 {
		ip = unsafe.Pointer(unsafe.SliceData(in))
	}
	if len(out) > 0 {
		outp = unsafe.Pointer(unsafe.SliceData(out))
	}
	r, _, _ := syscall.SyscallN(l.fn, op, conn, arg, uintptr(ip), uintptr(len(in)), uintptr(outp), uintptr(len(out)))
	runtime.KeepAlive(in)
	runtime.KeepAlive(out)
	return int(r)
}

func (l *nativeLib) conns() int  { return 4 }
func (l *nativeLib) wal() bool   { return true }
func (l *nativeLib) waits() bool { return true }

func openLib(path string) (library, error) {
	h, err := syscall.LoadLibrary(path)
	if err != nil {
		return nil, err
	}
	fn, err := syscall.GetProcAddress(h, "asq_call")
	if err != nil {
		return nil, err
	}
	v, err := syscall.GetProcAddress(h, "asq_version")
	if err != nil {
		return nil, err
	}
	n, _, _ := syscall.SyscallN(v)
	if err := versionOK(int(n)); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &nativeLib{fn: fn}, nil
}
