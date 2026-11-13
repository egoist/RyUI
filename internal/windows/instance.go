//go:build windows

package windows

import (
	"syscall"
	"time"
	"unsafe"
)

// One instance of an app: a launch while the app runs hands what it was
// launched with (its URL, or nothing, to bring the app to the front) to
// the running instance's message window with WM_COPYDATA, and quits.

var (
	procCreateMutexW             = kernel32.NewProc("CreateMutexW")
	procFindWindowExW            = user32.NewProc("FindWindowExW")
	procSendMessageTimeoutW      = user32.NewProc("SendMessageTimeoutW")
	procAllowSetForegroundWindow = user32.NewProc("AllowSetForegroundWindow")
)

const (
	WM_COPYDATA        = 0x004A
	errorAlreadyExists = 183
	errorTimeout       = 1460
	hwndMessage        = ^uintptr(2) // HWND_MESSAGE: -3
	smtoAbortIfHung    = 0x0002
)

// copyData is WM_COPYDATA's COPYDATASTRUCT.
type copyData struct {
	tag  uintptr
	size uint32
	ptr  uintptr
}

// CopyDataBytes is what a WM_COPYDATA's lparam carries: its dwData and a
// copy of its bytes.
func CopyDataBytes(lparam uintptr) (tag uintptr, b []byte) {
	if lparam == 0 {
		return 0, nil
	}
	cd := Native[copyData](lparam)
	if cd.size > 0 && cd.ptr != 0 {
		b = append([]byte(nil), unsafe.Slice(Native[byte](cd.ptr), cd.size)...)
	}
	return cd.tag, b
}

// TakeInstance makes this process the app's one instance (the mutex of
// that name says so), or hands payload, tagged, to the running instance's
// message window (of class and title) and reports false: the process
// should quit then. A running instance that doesn't answer in 10 seconds
// (it's quitting, or hung) leaves this one to run: as the instance, if
// the other has gone.
func TakeInstance(mutex, class, title string, tag uintptr, payload []byte) bool {
	for range 200 {
		h, _, err := procCreateMutexW.Call(0, 0, Str(mutex))
		if h == 0 {
			return true // no mutex: run, alone or not
		}
		if err != syscall.Errno(errorAlreadyExists) {
			return true // ours, held until the process ends
		}
		procCloseHandle.Call(h)
		if w, _, _ := procFindWindowExW.Call(hwndMessage, 0, Str(class), Str(title)); w != 0 && sendCopyData(w, tag, payload) {
			return false
		}
		time.Sleep(50 * time.Millisecond) // it's starting, or quitting
	}
	return true
}

// sendCopyData sends payload to w, and lets w's process take the
// foreground (this one has it: the program the user or the shell just
// started). It reports whether w has it: it answered, or it's still busy
// with something else and has it waiting (sending again would hand it
// twice); not if w has gone.
func sendCopyData(w, tag uintptr, payload []byte) bool {
	var pid uint32
	procGetWindowThreadProcessId.Call(w, uintptr(unsafe.Pointer(&pid)))
	if pid != 0 {
		procAllowSetForegroundWindow.Call(uintptr(pid))
	}
	cd := copyData{tag: tag, size: uint32(len(payload))}
	if len(payload) > 0 {
		cd.ptr = uintptr(unsafe.Pointer(&payload[0]))
	}
	var result uintptr
	r, _, err := procSendMessageTimeoutW.Call(w, WM_COPYDATA, 0, uintptr(unsafe.Pointer(&cd)), smtoAbortIfHung, 5000, uintptr(unsafe.Pointer(&result)))
	return r != 0 || err == syscall.Errno(errorTimeout)
}

// RegUserString reads a string value of a key under HKEY_CURRENT_USER,
// and whether it's there.
func RegUserString(path, name string) (string, bool) {
	b, typ, ok := regQuery(path, name)
	if !ok {
		return "", false
	}
	if typ != 1 && typ != 2 { // REG_SZ, REG_EXPAND_SZ
		return "", true
	}
	u := unsafe.Slice((*uint16)(unsafe.Pointer(unsafe.SliceData(b))), len(b)/2)
	return syscall.UTF16ToString(u), true
}

// RegSetUserString sets a string value (REG_SZ) of a key under
// HKEY_CURRENT_USER, making the key if it's not there.
func RegSetUserString(path, name, v string) error { return regSetString(path, name, v) }
