//go:build windows

package windows

import (
	"syscall"
	"unsafe"
)

// Global hot keys (RegisterHotKey): a key the system posts to a window
// as WM_HOTKEY whichever app has the keyboard.

var (
	procRegisterHotKey   = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey = user32.NewProc("UnregisterHotKey")
	procSendInput        = user32.NewProc("SendInput")
)

const (
	WM_HOTKEY = 0x0312

	MOD_ALT      = 0x0001
	MOD_CONTROL  = 0x0002
	MOD_SHIFT    = 0x0004
	MOD_WIN      = 0x0008
	MOD_NOREPEAT = 0x4000 // a key held down doesn't post it again

	// ERROR_HOTKEY_ALREADY_REGISTERED: another app (or this one) has
	// the keys.
	ERROR_HOTKEY_ALREADY_REGISTERED syscall.Errno = 1409
)

// RegisterHotKey makes mods+vk post WM_HOTKEY with id (0 to 0xBFFF) to
// hwnd, a window of this thread's.
func RegisterHotKey(hwnd uintptr, id int, mods uint32, vk uint16) error {
	r, _, err := procRegisterHotKey.Call(hwnd, uintptr(id), uintptr(mods), uintptr(vk))
	if r == 0 {
		return err
	}
	return nil
}

// UnregisterHotKey lets a hot key go.
func UnregisterHotKey(hwnd uintptr, id int) {
	procUnregisterHotKey.Call(hwnd, uintptr(id))
}

// keyInput is INPUT for a key (INPUT_KEYBOARD): type, then KEYBDINPUT,
// padded to the union's size (MOUSEINPUT's).
type keyInput struct {
	typ   uint32
	_     uint32 // the union's alignment on 64-bit
	vk    uint16
	scan  uint16
	flags uint32
	time  uint32
	extra uintptr
	_     [8]byte
}

// SendKeys presses the virtual keys in order and lets them go in
// reverse, as a keyboard would (SendInput): for tests and automation.
func SendKeys(vks ...uint16) error {
	const inputKeyboard, keyUp = 1, 0x0002
	in := make([]keyInput, 0, 2*len(vks))
	for _, vk := range vks {
		in = append(in, keyInput{typ: inputKeyboard, vk: vk})
	}
	for i := len(vks) - 1; i >= 0; i-- {
		in = append(in, keyInput{typ: inputKeyboard, vk: vks[i], flags: keyUp})
	}
	r, _, err := procSendInput.Call(uintptr(len(in)), uintptr(unsafe.Pointer(&in[0])), unsafe.Sizeof(in[0]))
	if int(r) != len(in) {
		return err
	}
	return nil
}
