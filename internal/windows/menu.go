//go:build windows && amd64

package windows

import (
	"unicode/utf16"
	"unsafe"
)

// Menu bars, accelerators and the keyboard.

var (
	procCreateMenu              = user32.NewProc("CreateMenu")
	procSetMenu                 = user32.NewProc("SetMenu")
	procGetMenu                 = user32.NewProc("GetMenu")
	procDrawMenuBar             = user32.NewProc("DrawMenuBar")
	procGetSubMenu              = user32.NewProc("GetSubMenu")
	procGetMenuItemCount        = user32.NewProc("GetMenuItemCount")
	procGetMenuStringW          = user32.NewProc("GetMenuStringW")
	procGetMenuState            = user32.NewProc("GetMenuState")
	procGetMenuBarInfo          = user32.NewProc("GetMenuBarInfo")
	procCreateAcceleratorTableW = user32.NewProc("CreateAcceleratorTableW")
	procDestroyAcceleratorTable = user32.NewProc("DestroyAcceleratorTable")
	procTranslateAcceleratorW   = user32.NewProc("TranslateAcceleratorW")
	procVkKeyScanW              = user32.NewProc("VkKeyScanW")
	procGetKeyboardState        = user32.NewProc("GetKeyboardState")
	procSetKeyboardState        = user32.NewProc("SetKeyboardState")
	procGetWindowDC             = user32.NewProc("GetWindowDC")
	procGetForegroundWindow     = user32.NewProc("GetForegroundWindow")
	procGetCurrentObject        = gdi32.NewProc("GetCurrentObject")
)

const (
	MF_POPUP      = 0x0010
	MF_CHECKED    = 0x0008
	MF_DISABLED   = 0x0002
	MF_BYPOSITION = 0x0400

	FVIRTKEY  = 0x01
	FNOINVERT = 0x02
	FSHIFT    = 0x04
	FCONTROL  = 0x08
	FALT      = 0x10

	SM_CYMENU = 15

	VK_DELETE = 0x2E
	VK_F1     = 0x70

	// The messages uxtheme's menu bar ("user API hook") sends its window,
	// to draw it.
	WM_NCPAINT         = 0x0085
	WM_NCACTIVATE      = 0x0086
	WM_UAHDRAWMENU     = 0x0091
	WM_UAHDRAWMENUITEM = 0x0092

	ODS_SELECTED = 0x0001
	ODS_GRAYED   = 0x0002
	ODS_DISABLED = 0x0004
	ODS_HOTLIGHT = 0x0040
	ODS_NOACCEL  = 0x0100
)

// Accel is an accelerator: a key, with FVIRTKEY and modifiers, and the
// command it sends.
type Accel struct {
	Virt uint8
	_    uint8
	Key  uint16
	Cmd  uint16
}

// CreateMenu makes an empty menu bar.
func CreateMenu() uintptr {
	r, _, _ := procCreateMenu.Call()
	return r
}

// SetMenu gives a window its menu bar (0: none).
func SetMenu(hwnd, menu uintptr) { procSetMenu.Call(hwnd, menu) }

// GetMenu is a window's menu bar.
func GetMenu(hwnd uintptr) uintptr {
	r, _, _ := procGetMenu.Call(hwnd)
	return r
}

func DrawMenuBar(hwnd uintptr) { procDrawMenuBar.Call(hwnd) }

func SubMenu(m uintptr, i int) uintptr {
	r, _, _ := procGetSubMenu.Call(m, uintptr(i))
	return r
}

func MenuItemCount(m uintptr) int {
	r, _, _ := procGetMenuItemCount.Call(m)
	return int(int32(r))
}

// MenuString is the text of a menu's item i (by position).
func MenuString(m uintptr, i int) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetMenuStringW.Call(m, uintptr(i), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), MF_BYPOSITION)
	return string(utf16.Decode(buf[:n]))
}

// MenuStringUnits is MenuString as UTF-16, for drawing it.
func MenuStringUnits(m uintptr, i int) []uint16 {
	buf := make([]uint16, 256)
	n, _, _ := procGetMenuStringW.Call(m, uintptr(i), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), MF_BYPOSITION)
	return buf[:n]
}

// MenuState is a menu item's flags (MF_GRAYED, MF_CHECKED…), by
// position.
func MenuState(m uintptr, i int) uint32 {
	r, _, _ := procGetMenuState.Call(m, uintptr(i), MF_BYPOSITION)
	return uint32(r)
}

// MenuBarRect is a window's menu bar in window coordinates (those of its
// window DC); false: it has none.
func MenuBarRect(hwnd uintptr) (Rect, bool) {
	var mbi struct {
		Size       uint32
		Bar        Rect
		Menu, Hwnd uintptr
		Bits       int32
	}
	mbi.Size = uint32(unsafe.Sizeof(mbi))
	if ok, _, _ := procGetMenuBarInfo.Call(hwnd, 0xFFFFFFFD, 0, uintptr(unsafe.Pointer(&mbi))); ok == 0 { // OBJID_MENU
		return Rect{}, false
	}
	wr := WindowRect(hwnd)
	r := mbi.Bar
	r.Left, r.Right = r.Left-wr.Left, r.Right-wr.Left
	r.Top, r.Bottom = r.Top-wr.Top, r.Bottom-wr.Top
	return r, true
}

// CreateAcceleratorTable makes an accelerator table (0 for none).
func CreateAcceleratorTable(a []Accel) uintptr {
	if len(a) == 0 {
		return 0
	}
	r, _, _ := procCreateAcceleratorTableW.Call(uintptr(unsafe.Pointer(&a[0])), uintptr(len(a)))
	return r
}

func DestroyAcceleratorTable(t uintptr) {
	if t != 0 {
		procDestroyAcceleratorTable.Call(t)
	}
}

// TranslateAccelerator sends hwnd the command of the accelerator a key
// message is, if it's one of the table's.
func TranslateAccelerator(hwnd, table uintptr, m *Msg) bool {
	r, _, _ := procTranslateAcceleratorW.Call(hwnd, table, uintptr(unsafe.Pointer(m)))
	return r != 0
}

// VkKeyScan is the virtual key typing c on the keyboard's layout, and
// the shift state it needs (1 Shift, 2 Ctrl, 4 Alt); false: no key types
// it.
func VkKeyScan(c rune) (vk uint8, shift uint8, ok bool) {
	r, _, _ := procVkKeyScanW.Call(uintptr(uint16(c)))
	if int16(r) == -1 {
		return 0, 0, false
	}
	return uint8(r), uint8(r >> 8), true
}

// KeyboardState is the thread's keys' state.
func KeyboardState() [256]byte {
	var s [256]byte
	procGetKeyboardState.Call(uintptr(unsafe.Pointer(&s[0])))
	return s
}

// SetKeyboardState sets the thread's keys' state (what GetKeyState
// reads), as if they were pressed.
func SetKeyboardState(s [256]byte) {
	procSetKeyboardState.Call(uintptr(unsafe.Pointer(&s[0])))
}

// WindowDC is a DC for a window's whole area, its frame included.
func WindowDC(hwnd uintptr) uintptr {
	r, _, _ := procGetWindowDC.Call(hwnd)
	return r
}

func ForegroundWindow() uintptr {
	r, _, _ := procGetForegroundWindow.Call()
	return r
}

// CurrentFont is the font selected into a DC.
func CurrentFont(dc uintptr) uintptr {
	r, _, _ := procGetCurrentObject.Call(dc, 6) // OBJ_FONT
	return r
}

// UAHMenu is what WM_UAHDRAWMENU carries: the menu bar and the DC to
// draw it in.
type UAHMenu struct {
	Menu, DC uintptr
	Flags    uint32
}

// DrawItem is DRAWITEMSTRUCT.
type DrawItem struct {
	CtlType, CtlID, ItemID, ItemAction, ItemState uint32
	HwndItem, DC                                  uintptr
	Item                                          Rect
	ItemData                                      uintptr
}

// UAHDrawMenuItem is what WM_UAHDRAWMENUITEM carries: a menu bar item to
// draw.
type UAHDrawMenuItem struct {
	Draw     DrawItem
	Menu     UAHMenu
	Position int32
}
