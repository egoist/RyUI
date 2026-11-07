//go:build windows

package crashrun

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procSetStdHandle = kernel32.NewProc("SetStdHandle")
	procLockFileEx   = kernel32.NewProc("LockFileEx")
	procUnlockFileEx = kernel32.NewProc("UnlockFileEx")
)

// lock: the open file is the run's mark that it hasn't ended (Alive).
func lock(f *os.File) {}

// Alive reports whether the run whose crash output is path still runs:
// its process has the file open, so it can't be opened unshared.
func Alive(path string) bool {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ, 0, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return err == errSharingViolation
	}
	syscall.CloseHandle(h)
	return false
}

// redirectStderr points the process's standard error at path when it has
// none (an app built for the GUI, without a console). The runtime prints
// a Windows exception it can't handle (an access violation in a DLL, say)
// to standard error, and that, unlike a panic, isn't in its crash output
// (SetCrashOutput): it's kept here instead. Only the runtime's own
// writes follow the handle; os.Stderr, opened at start, doesn't.
func redirectStderr(path string) {
	h, err := syscall.GetStdHandle(syscall.STD_ERROR_HANDLE)
	if err == nil && h != 0 && h != syscall.InvalidHandle {
		if t, err := syscall.GetFileType(h); err == nil && t != syscall.FILE_TYPE_UNKNOWN {
			return // a console, a file or a pipe: what's printed is seen there
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	stderr := int32(syscall.STD_ERROR_HANDLE)
	if r, _, _ := procSetStdHandle.Call(uintptr(uint32(stderr)), f.Fd()); r == 0 {
		f.Close()
		return
	}
	stderrFile = f // kept open for the process's life
}

var stderrFile *os.File

const errSharingViolation = syscall.Errno(32) // ERROR_SHARING_VIOLATION

// Lock holds an exclusive lock on path (created if need be) until unlock:
// one process at a time reads the directory into reports.
func Lock(path string) (unlock func()) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return func() {}
	}
	const exclusive = 2 // LOCKFILE_EXCLUSIVE_LOCK
	var ol syscall.Overlapped
	procLockFileEx.Call(f.Fd(), exclusive, 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
	return func() {
		procUnlockFileEx.Call(f.Fd(), 0, 1, 0, uintptr(unsafe.Pointer(&ol)))
		f.Close()
	}
}
