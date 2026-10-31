//go:build windows && amd64

package windows

import (
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// Visual styles (uxtheme): what draws the system's own controls and
// menus, for parts the host draws itself that should look as theirs do.

var (
	procOpenThemeData          = uxtheme.NewProc("OpenThemeData")
	procOpenThemeDataForDpi    = uxtheme.NewProc("OpenThemeDataForDpi")
	procCloseThemeData         = uxtheme.NewProc("CloseThemeData")
	procDrawThemeBackground    = uxtheme.NewProc("DrawThemeBackground")
	procGetThemePartSize       = uxtheme.NewProc("GetThemePartSize")
	procGetThemeMargins        = uxtheme.NewProc("GetThemeMargins")
	procGetThemeColor          = uxtheme.NewProc("GetThemeColor")
	procIsThemePartDefined     = uxtheme.NewProc("IsThemePartDefined")
	procSystemParametersInfoDp = user32.NewProc("SystemParametersInfoForDpi")
	procSetMenuItemInfoW       = user32.NewProc("SetMenuItemInfoW")
	procEndMenu                = user32.NewProc("EndMenu")
)

// The Menu class's parts and states (vsstyle.h).
const (
	MENU_POPUPBACKGROUND      = 9
	MENU_POPUPBORDERS         = 10
	MENU_POPUPCHECK           = 11
	MENU_POPUPCHECKBACKGROUND = 12
	MENU_POPUPGUTTER          = 13
	MENU_POPUPITEM            = 14
	MENU_POPUPSEPARATOR       = 15

	MPI_NORMAL      = 1
	MPI_HOT         = 2
	MPI_DISABLED    = 3
	MPI_DISABLEDHOT = 4

	MC_CHECKMARKNORMAL   = 1
	MC_CHECKMARKDISABLED = 2

	MCB_DISABLED = 1
	MCB_NORMAL   = 2

	TMT_SIZINGMARGINS  = 3601
	TMT_CONTENTMARGINS = 3602
	TMT_FILLCOLOR      = 3802
	TMT_TEXTCOLOR      = 3803

	TS_TRUE = 1
)

// Margins is MARGINS.
type Margins struct{ Left, Right, Top, Bottom int32 }

// OpenTheme opens a window's visual style class (such as "Menu") at a
// DPI; 0 if there's none (classic theme).
func OpenTheme(hwnd uintptr, class string, dpi int) uintptr {
	if Has(procOpenThemeDataForDpi) {
		r, _, _ := procOpenThemeDataForDpi.Call(hwnd, Str(class), uintptr(dpi))
		return r
	}
	r, _, _ := procOpenThemeData.Call(hwnd, Str(class))
	return r
}

func CloseTheme(t uintptr) {
	if t != 0 {
		procCloseThemeData.Call(t)
	}
}

// DrawThemeBackground draws a part in a state, in r.
func DrawThemeBackground(t, dc uintptr, part, state int, r Rect) {
	procDrawThemeBackground.Call(t, dc, uintptr(part), uintptr(state), uintptr(unsafe.Pointer(&r)), 0)
}

// ThemePartSize is a part's true size.
func ThemePartSize(t, dc uintptr, part, state int) (w, h int32) {
	var s struct{ Cx, Cy int32 }
	procGetThemePartSize.Call(t, dc, uintptr(part), uintptr(state), 0, TS_TRUE, uintptr(unsafe.Pointer(&s)))
	return s.Cx, s.Cy
}

// ThemeMargins is a part's margins property (TMT_CONTENTMARGINS…).
func ThemeMargins(t, dc uintptr, part, state, prop int) Margins {
	var m Margins
	procGetThemeMargins.Call(t, dc, uintptr(part), uintptr(state), uintptr(prop), 0, uintptr(unsafe.Pointer(&m)))
	return m
}

// ThemeColor is a part's color property (a COLORREF); false if it has
// none.
func ThemeColor(t uintptr, part, state, prop int) (uint32, bool) {
	var c uint32
	r, _, _ := procGetThemeColor.Call(t, uintptr(part), uintptr(state), uintptr(prop), uintptr(unsafe.Pointer(&c)))
	return c, r == 0
}

// MenuFont is the system's menu font (NONCLIENTMETRICS' lfMenuFont) at
// a DPI.
func MenuFont(dpi int) LogFont {
	var ncm struct {
		Size                                   uint32
		BorderWidth, ScrollWidth, ScrollHeight int32
		CaptionWidth, CaptionHeight            int32
		CaptionFont                            LogFont
		SmCaptionWidth, SmCaptionHeight        int32
		SmCaptionFont                          LogFont
		MenuWidth, MenuHeight                  int32
		MenuFont, StatusFont, MessageFont      LogFont
		PaddedBorderWidth                      int32
	}
	ncm.Size = uint32(unsafe.Sizeof(ncm))
	const spiGetNonClientMetrics = 0x0029
	if Has(procSystemParametersInfoDp) {
		procSystemParametersInfoDp.Call(spiGetNonClientMetrics, uintptr(ncm.Size), uintptr(unsafe.Pointer(&ncm)), 0, uintptr(dpi))
	} else {
		procSystemParametersInfoW.Call(spiGetNonClientMetrics, uintptr(ncm.Size), uintptr(unsafe.Pointer(&ncm)), 0)
	}
	return ncm.MenuFont
}

// Owner-drawn menu items.

const (
	MFT_OWNERDRAW = 0x0100
	MIIM_FTYPE    = 0x0100
	MIIM_DATA     = 0x0020

	WM_MEASUREITEM   = 0x002C
	WM_DRAWITEM      = 0x002B
	WM_MENUCHAR      = 0x0120
	WM_INITMENUPOPUP = 0x0117

	ODT_MENU = 1

	MNC_EXECUTE = 2
)

// MeasureItem is MEASUREITEMSTRUCT.
type MeasureItem struct {
	CtlType, CtlID, ItemID, ItemWidth, ItemHeight uint32
	ItemData                                      uintptr
}

// SetMenuItemOwnerDraw makes a menu's item i (by position) drawn by its
// owner window (WM_MEASUREITEM, WM_DRAWITEM), data its item data.
func SetMenuItemOwnerDraw(m uintptr, i int, data uintptr, flags uint32) {
	mii := struct {
		Size, Mask, Type, State, ID uint32
		SubMenu, Checked, Unchecked uintptr
		Data                        uintptr
		TypeData                    uintptr
		Cch                         uint32
		Item                        uintptr
	}{Mask: MIIM_FTYPE | MIIM_DATA, Type: MFT_OWNERDRAW | flags, Data: data}
	mii.Size = uint32(unsafe.Sizeof(mii))
	procSetMenuItemInfoW.Call(m, uintptr(i), 1, uintptr(unsafe.Pointer(&mii)))
}

// EndMenu closes the menu showing.
func EndMenu() { procEndMenu.Call() }

var (
	procGetMenuItemInfoW = user32.NewProc("GetMenuItemInfoW")
)

const (
	MENU_POPUPSUBMENU = 16

	ODS_NOFOCUSRECT = 0x0200

	SM_CXMENUCHECK = 71

	COLOR_MENUTEXT = 7
	COLOR_GRAYTEXT = 17
)

// MenuItemData is a menu's item i's (by position) item data.
func MenuItemData(m uintptr, i int) uintptr {
	mii := struct {
		Size, Mask, Type, State, ID uint32
		SubMenu, Checked, Unchecked uintptr
		Data                        uintptr
		TypeData                    uintptr
		Cch                         uint32
		Item                        uintptr
	}{Mask: MIIM_DATA}
	mii.Size = uint32(unsafe.Sizeof(mii))
	procGetMenuItemInfoW.Call(m, uintptr(i), 1, uintptr(unsafe.Pointer(&mii)))
	return mii.Data
}

// SysColor is a system color, a COLORREF.
func SysColor(i int) uint32 {
	r, _, _ := procGetSysColor.Call(uintptr(i))
	return uint32(r)
}

var procSetMenuInfo = user32.NewProc("SetMenuInfo")

const ODS_CHECKED = 0x0008

// SetMenuBackground gives a menu (not its submenus) a background brush.
func SetMenuBackground(m, brush uintptr) {
	mi := struct {
		Size, Mask, Style, Max uint32
		Back                   uintptr
		ContextHelpID          uint32
		MenuData               uintptr
	}{Mask: 0x2, Back: brush} // MIM_BACKGROUND
	mi.Size = uint32(unsafe.Sizeof(mi))
	procSetMenuInfo.Call(m, uintptr(unsafe.Pointer(&mi)))
}

// Menu windows: the windows (class #32768) popup menus show in.

var (
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procGetClassNameW       = user32.NewProc("GetClassNameW")

	menuHook     uintptr
	menuHookProc uintptr
	menuCreated  func(hwnd uintptr)
)

const (
	WM_INITMENU      = 0x0116
	WM_ENTERMENULOOP = 0x0211
	WM_EXITMENULOOP  = 0x0212
	MN_GETHMENU      = 0x01E1
)

// WatchMenuWindows calls f with each menu window the thread makes until
// StopWatchingMenuWindows (a CBT hook on the thread: HCBT_CREATEWND).
func WatchMenuWindows(f func(hwnd uintptr)) {
	menuCreated = f
	if menuHook != 0 {
		return
	}
	if menuHookProc == 0 {
		menuHookProc = syscall.NewCallback(func(code, w, l uintptr) uintptr {
			if int32(code) == 3 && menuCreated != nil && ClassName(w) == "#32768" { // HCBT_CREATEWND
				menuCreated(w)
			}
			r, _, _ := procCallNextHookEx.Call(0, code, w, l)
			return r
		})
	}
	tid, _, _ := procGetCurrentThreadId.Call()
	menuHook, _, _ = procSetWindowsHookExW.Call(5, menuHookProc, 0, tid) // WH_CBT
}

func StopWatchingMenuWindows() {
	if menuHook != 0 {
		procUnhookWindowsHookEx.Call(menuHook)
		menuHook = 0
	}
	menuCreated = nil
}

// ClassName is a window's class name.
func ClassName(hwnd uintptr) string {
	var buf [64]uint16
	n, _, _ := procGetClassNameW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return string(utf16.Decode(buf[:n]))
}

// SetCornerPreference asks DWM to round a window's corners (Windows 11):
// 2 round, 3 round small, 1 not.
func SetCornerPreference(hwnd uintptr, pref uint32) {
	procDwmSetWindowAttribute.Call(hwnd, 33, uintptr(unsafe.Pointer(&pref)), 4) // DWMWA_WINDOW_CORNER_PREFERENCE
}

// SetBorderColor sets the border DWM draws around a window (Windows 11),
// a COLORREF.
func SetBorderColor(hwnd uintptr, c uint32) {
	procDwmSetWindowAttribute.Call(hwnd, 34, uintptr(unsafe.Pointer(&c)), 4) // DWMWA_BORDER_COLOR
}

var procEnumThreadWindows = user32.NewProc("EnumThreadWindows")

var enumMenus = syscall.NewCallback(func(hwnd, l uintptr) uintptr {
	if ClassName(hwnd) == "#32768" && IsWindowVisible(hwnd) {
		*Native[uintptr](l) = hwnd
		return 0
	}
	return 1
})

// OpenMenuWindow is the thread's menu window showing (the last one
// opened, a submenu's over its menu's), or 0.
func OpenMenuWindow() uintptr {
	var w uintptr
	tid, _, _ := procGetCurrentThreadId.Call()
	procEnumThreadWindows.Call(tid, enumMenus, uintptr(unsafe.Pointer(&w)))
	return w
}

var procGetMenuItemRect = user32.NewProc("GetMenuItemRect")

// MenuItemRect is a menu's item i's rectangle on the screen (a menu
// bar's item, or a menu's showing).
func MenuItemRect(hwnd, m uintptr, i int) Rect {
	var r Rect
	procGetMenuItemRect.Call(hwnd, m, uintptr(i), uintptr(unsafe.Pointer(&r)))
	return r
}

// MenuItemType is a menu's item i's (by position) type (MFT_OWNERDRAW,
// MFT_SEPARATOR…).
func MenuItemType(m uintptr, i int) uint32 {
	mii := struct {
		Size, Mask, Type, State, ID uint32
		SubMenu, Checked, Unchecked uintptr
		Data                        uintptr
		TypeData                    uintptr
		Cch                         uint32
		Item                        uintptr
	}{Mask: MIIM_FTYPE}
	mii.Size = uint32(unsafe.Sizeof(mii))
	procGetMenuItemInfoW.Call(m, uintptr(i), 1, uintptr(unsafe.Pointer(&mii)))
	return mii.Type
}
