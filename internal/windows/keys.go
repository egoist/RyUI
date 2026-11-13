//go:build windows

package windows

// The keyboard: a key's character on the layout, and whether an input
// method is composing text.

var (
	imm32 = systemDLL("imm32.dll")

	procMapVirtualKeyExW         = user32.NewProc("MapVirtualKeyExW")
	procGetKeyboardLayout        = user32.NewProc("GetKeyboardLayout")
	procImmGetContext            = imm32.NewProc("ImmGetContext")
	procImmReleaseContext        = imm32.NewProc("ImmReleaseContext")
	procImmGetCompositionStringW = imm32.NewProc("ImmGetCompositionStringW")
)

const (
	WM_SYSKEYUP   = 0x0105
	VK_LWIN       = 0x5B
	VK_RWIN       = 0x5C
	VK_PROCESSKEY = 0xE5 // a key an input method took

	mapvkVKToVSC  = 0   // MAPVK_VK_TO_VSC
	mapvkVKToChar = 2   // MAPVK_VK_TO_CHAR
	gcsCompStr    = 0x8 // GCS_COMPSTR
)

// KeyChar is the character virtual key vk types without modifiers on the
// thread's keyboard layout, in its low word (a dead key's with the high
// bit set), 0: none. Unlike ToUnicodeEx, it leaves the keyboard's state
// (a dead key waiting) as it is.
func KeyChar(vk uint32) uint32 {
	hkl, _, _ := procGetKeyboardLayout.Call(0)
	r, _, _ := procMapVirtualKeyExW.Call(uintptr(vk), mapvkVKToChar, hkl)
	return uint32(r)
}

// Composing reports whether an input method is composing text in hwnd
// (marked text: a Japanese or Chinese keyboard's, before it's committed).
func Composing(hwnd uintptr) bool {
	c, _, _ := procImmGetContext.Call(hwnd)
	if c == 0 {
		return false
	}
	n, _, _ := procImmGetCompositionStringW.Call(c, gcsCompStr, 0, 0)
	procImmReleaseContext.Call(hwnd, c)
	return int32(n) > 0
}

// ScanCode is the scan code of virtual key vk on the thread's keyboard
// layout (a key message's lParam has it in bits 16 to 23).
func ScanCode(vk uint32) uint32 {
	hkl, _, _ := procGetKeyboardLayout.Call(0)
	r, _, _ := procMapVirtualKeyExW.Call(uintptr(vk), mapvkVKToVSC, hkl)
	return uint32(r)
}
