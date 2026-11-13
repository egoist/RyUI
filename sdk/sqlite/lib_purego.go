//go:build (darwin || linux) && !js

package sqlite

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
)

// nativeLib is the library loaded with dlopen, called through purego:
// macOS, Linux, iOS and Android (where the build mode's cgo is purego's
// way to C, not Asuka's).
type nativeLib struct{ fn uintptr }

func (l *nativeLib) call(op, conn, arg uintptr, in, out []byte) int {
	var ip, outp unsafe.Pointer
	if len(in) > 0 {
		ip = unsafe.Pointer(unsafe.SliceData(in))
	}
	if len(out) > 0 {
		outp = unsafe.Pointer(unsafe.SliceData(out))
	}
	// The buffers are Go's heap's, which doesn't move; the shim keeps
	// no pointer to them past the call.
	r, _, _ := purego.SyscallN(l.fn, op, conn, arg, uintptr(ip), uintptr(len(in)), uintptr(outp), uintptr(len(out)))
	runtime.KeepAlive(in)
	runtime.KeepAlive(out)
	return int(r)
}

func (l *nativeLib) conns() int  { return 4 }
func (l *nativeLib) wal() bool   { return true }
func (l *nativeLib) waits() bool { return true }

// openLib loads the library at path (or, a bare name, the platform's
// search finds it).
func openLib(path string) (library, error) {
	h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	fn, err := purego.Dlsym(h, "asq_call")
	if err != nil {
		return nil, err
	}
	if err := checkVersion(h); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &nativeLib{fn: fn}, nil
}

func checkVersion(h uintptr) error {
	v, err := purego.Dlsym(h, "asq_version")
	if err != nil {
		return err
	}
	n, _, _ := purego.SyscallN(v)
	return versionOK(int(n))
}
