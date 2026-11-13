//go:build windows

package windows

import (
	"errors"
	"syscall"
	"unsafe"
)

// Audio: MCI's mpegvideo device (DirectShow's playback: MP3, AAC, WAV,
// WMA...), driven by command strings; a play's end is MM_MCINOTIFY to a
// window.

var (
	winmm              = systemDLL("winmm.dll")
	procMciSendString  = winmm.NewProc("mciSendStringW")
	procMciGetErrorStr = winmm.NewProc("mciGetErrorStringW")
)

// MM_MCINOTIFY: wParam MCI_NOTIFY_SUCCESSFUL (1) once a play ends, lParam
// the device's ID.
const (
	MM_MCINOTIFY          = 0x3B9
	MCI_NOTIFY_SUCCESSFUL = 1
)

// MCI sends cmd, its notifications (if it asks for them) to hwnd, and
// returns the device's answer.
func MCI(cmd string, hwnd uintptr) (string, error) {
	var buf [256]uint16
	r, _, _ := procMciSendString.Call(Str(cmd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), hwnd)
	if r != 0 {
		var msg [256]uint16
		procMciGetErrorStr.Call(r, uintptr(unsafe.Pointer(&msg[0])), uintptr(len(msg)))
		return "", errors.New(syscall.UTF16ToString(msg[:]))
	}
	return syscall.UTF16ToString(buf[:]), nil
}
