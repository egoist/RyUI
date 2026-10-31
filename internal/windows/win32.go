//go:build windows && amd64

// Package windows is the Win32 layer of nui's Windows host: user32, gdi32,
// GDI+, the common controls (version 6), uxtheme and DWM, called through
// the syscall package, no cgo. Much of it comes from MyGo's Windows
// backend.
//
// It's amd64 only: GDI+ takes float arguments, which Go's Windows calls
// pass in the XMM registers on amd64 (the first four integer registers
// are copied there) but not on arm64.
package windows

import (
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// System DLLs are loaded from the system directory, never from the
// application's directory or the search path.
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll") // a KnownDLL
	sysDir   = systemDirectory()

	user32   = systemDLL("user32.dll")
	gdi32    = systemDLL("gdi32.dll")
	gdiplus  = systemDLL("gdiplus.dll")
	uxtheme  = systemDLL("uxtheme.dll")
	dwmapi   = systemDLL("dwmapi.dll")
	shell32  = systemDLL("shell32.dll")
	comdlg32 = systemDLL("comdlg32.dll")
	ole32    = systemDLL("ole32.dll")
	advapi32 = systemDLL("advapi32.dll")
	ntdll    = systemDLL("ntdll.dll")
	msimg32  = systemDLL("msimg32.dll")
)

func systemDirectory() string {
	buf := make([]uint16, syscall.MAX_PATH)
	n, _, _ := kernel32.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 || int(n) > len(buf) {
		return `C:\Windows\System32`
	}
	return syscall.UTF16ToString(buf[:n])
}

func systemDLL(name string) *syscall.LazyDLL { return syscall.NewLazyDLL(sysDir + `\` + name) }

// SystemDir is the system directory (System32).
func SystemDir() string { return sysDir }

// Has reports whether an optional API exists on this version of Windows.
func Has(p *syscall.LazyProc) bool { return p.Find() == nil }

// Build is the build of Windows 10 or 11 (22000 and later: Windows 11).
// RtlGetVersion tells the version whatever the executable's manifest
// declares.
var Build = sync.OnceValue(func() uint32 {
	var v struct {
		Size, Major, Minor, Build, Platform uint32
		CSD                                 [128]uint16
	}
	v.Size = uint32(unsafe.Sizeof(v))
	ntdll.NewProc("RtlGetVersion").Call(uintptr(unsafe.Pointer(&v)))
	switch {
	case v.Major > 10:
		return ^uint32(0)
	case v.Major < 10:
		return 0
	}
	return v.Build
})

// Windows11 reports Windows 11 or later.
func Windows11() bool { return Build() >= 22000 }

// UTF16 is s as a NUL-terminated UTF-16 string (NULs in s dropped).
func UTF16(s string) *uint16 { return &UTF16z(s)[0] }

// UTF16z is UTF16 as a slice, its NUL included.
func UTF16z(s string) []uint16 {
	b := make([]uint16, 0, len(s)+1)
	for _, r := range s {
		if r == 0 {
			continue
		}
		if r >= 0x10000 {
			r1, r2 := utf16.EncodeRune(r)
			b = append(b, uint16(r1), uint16(r2))
		} else {
			b = append(b, uint16(r))
		}
	}
	return append(b, 0)
}

// UTF16Units is s in UTF-16, without a NUL.
func UTF16Units(s string) []uint16 {
	b := UTF16z(s)
	return b[:len(b)-1]
}

// Str is a pointer argument for s.
func Str(s string) uintptr { return uintptr(unsafe.Pointer(UTF16(s))) }

// StrOpt is Str, or 0 for an empty string.
func StrOpt(s string) uintptr {
	if s == "" {
		return 0
	}
	return Str(s)
}

// native turns an address of memory Go doesn't manage into a pointer.
func native(addr uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&addr)) }

// GoStr reads a NUL-terminated UTF-16 string at a native address.
func GoStr(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*uint16)(unsafe.Add(native(p), 2*n)) != 0 {
		n++
	}
	return string(utf16.Decode(unsafe.Slice((*uint16)(native(p)), n)))
}

// Native is a pointer to memory Go doesn't manage (a notification's
// struct, given as an lParam).
func Native[T any](p uintptr) *T { return (*T)(native(p)) }

func LoWord(v uintptr) uint16 { return uint16(v) }
func HiWord(v uintptr) uint16 { return uint16(v >> 16) }

// XParam and YParam decode the signed coordinates of an lParam.
func XParam(v uintptr) int32 { return int32(int16(LoWord(v))) }
func YParam(v uintptr) int32 { return int32(int16(HiWord(v))) }

func MakeLong(lo, hi uint16) uintptr { return uintptr(uint32(lo) | uint32(hi)<<16) }

// Ptr is a pointer argument.
func Ptr[T any](p *T) uintptr { return uintptr(unsafe.Pointer(p)) }

type Rect struct{ Left, Top, Right, Bottom int32 }

func (r Rect) W() int32 { return r.Right - r.Left }
func (r Rect) H() int32 { return r.Bottom - r.Top }

type Point struct{ X, Y int32 }

type Msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      Point
	Private uint32
}

type WndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type PaintStruct struct {
	Hdc        uintptr
	Erase      int32
	Paint      Rect
	Restore    int32
	IncUpdate  int32
	RGBReserve [32]byte
}

type LogFont struct {
	Height, Width, Escapement, Orientation, Weight int32
	Italic, Underline, StrikeOut, CharSet          byte
	OutPrecision, ClipPrecision, Quality, Pitch    byte
	FaceName                                       [32]uint16
}

type TextMetric struct {
	Height, Ascent, Descent, InternalLeading, ExternalLeading int32
	AveCharWidth, MaxCharWidth, Weight, Overhang              int32
	DigitizedAspectX, DigitizedAspectY                        int32
	FirstChar, LastChar, DefaultChar, BreakChar               uint16
	Italic, Underlined, StruckOut, PitchAndFamily, CharSet    byte
}

type ScrollInfo struct {
	Size, Mask    uint32
	Min, Max      int32
	Page          uint32
	Pos, TrackPos int32
}

type NMHdr struct {
	HwndFrom uintptr
	IDFrom   uintptr
	Code     int32
}

type NMCustomDraw struct {
	Hdr        NMHdr
	DrawStage  uint32
	Hdc        uintptr
	Rc         Rect
	ItemSpec   uintptr
	ItemState  uint32
	ItemLParam uintptr
}

type TrackMouseEventS struct {
	Size, Flags uint32
	Hwnd        uintptr
	HoverTime   uint32
}

type SystemTime struct {
	Year, Month, DayOfWeek, Day, Hour, Minute, Second, Milliseconds uint16
}

type NMDateTimeChange struct {
	Hdr   NMHdr
	Flags uint32
	St    SystemTime
}

type MinMaxInfo struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize Point
}

type WindowPos struct {
	Hwnd, After  uintptr
	X, Y, Cx, Cy int32
	Flags        uint32
}

type MonitorInfo struct {
	Size          uint32
	Monitor, Work Rect
	Flags         uint32
}

var (
	// kernel32
	procGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadId   = kernel32.NewProc("GetCurrentThreadId")
	procCreateActCtxW        = kernel32.NewProc("CreateActCtxW")
	procActivateActCtx       = kernel32.NewProc("ActivateActCtx")
	procLoadLibraryExW       = kernel32.NewProc("LoadLibraryExW")
	procGetProcAddress       = kernel32.NewProc("GetProcAddress")
	procGlobalAlloc          = kernel32.NewProc("GlobalAlloc")
	procGlobalLock           = kernel32.NewProc("GlobalLock")
	procGlobalUnlock         = kernel32.NewProc("GlobalUnlock")
	procGlobalFree           = kernel32.NewProc("GlobalFree")
	procGetUserDefaultLocale = kernel32.NewProc("GetUserDefaultLocaleName")
	procGetLocaleInfoEx      = kernel32.NewProc("GetLocaleInfoEx")
	procGetDynamicTimeZone   = kernel32.NewProc("GetDynamicTimeZoneInformation")
	procCreateEventW         = kernel32.NewProc("CreateEventW")

	// user32
	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procGetDpiForSystem               = user32.NewProc("GetDpiForSystem")
	procAdjustWindowRectExForDpi      = user32.NewProc("AdjustWindowRectExForDpi")
	procSystemParametersInfoW         = user32.NewProc("SystemParametersInfoW")
	procRegisterClassExW              = user32.NewProc("RegisterClassExW")
	procCreateWindowExW               = user32.NewProc("CreateWindowExW")
	procDestroyWindow                 = user32.NewProc("DestroyWindow")
	procDefWindowProcW                = user32.NewProc("DefWindowProcW")
	procGetMessageW                   = user32.NewProc("GetMessageW")
	procPeekMessageW                  = user32.NewProc("PeekMessageW")
	procTranslateMessage              = user32.NewProc("TranslateMessage")
	procDispatchMessageW              = user32.NewProc("DispatchMessageW")
	procIsDialogMessageW              = user32.NewProc("IsDialogMessageW")
	procPostMessageW                  = user32.NewProc("PostMessageW")
	procSendMessageW                  = user32.NewProc("SendMessageW")
	procPostQuitMessage               = user32.NewProc("PostQuitMessage")
	procShowWindow                    = user32.NewProc("ShowWindow")
	procUpdateWindow                  = user32.NewProc("UpdateWindow")
	procSetWindowPos                  = user32.NewProc("SetWindowPos")
	procBeginDeferWindowPos           = user32.NewProc("BeginDeferWindowPos")
	procDeferWindowPos                = user32.NewProc("DeferWindowPos")
	procEndDeferWindowPos             = user32.NewProc("EndDeferWindowPos")
	procGetClientRect                 = user32.NewProc("GetClientRect")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procSetWindowTextW                = user32.NewProc("SetWindowTextW")
	procGetWindowTextW                = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW          = user32.NewProc("GetWindowTextLengthW")
	procGetWindowLongPtrW             = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW             = user32.NewProc("SetWindowLongPtrW")
	procSetParent                     = user32.NewProc("SetParent")
	procGetParent                     = user32.NewProc("GetParent")
	procInvalidateRect                = user32.NewProc("InvalidateRect")
	procRedrawWindow                  = user32.NewProc("RedrawWindow")
	procBeginPaint                    = user32.NewProc("BeginPaint")
	procEndPaint                      = user32.NewProc("EndPaint")
	procGetDC                         = user32.NewProc("GetDC")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procSetFocus                      = user32.NewProc("SetFocus")
	procGetFocus                      = user32.NewProc("GetFocus")
	procEnableWindow                  = user32.NewProc("EnableWindow")
	procIsWindowEnabled               = user32.NewProc("IsWindowEnabled")
	procIsWindowVisible               = user32.NewProc("IsWindowVisible")
	procIsWindow                      = user32.NewProc("IsWindow")
	procLoadCursorW                   = user32.NewProc("LoadCursorW")
	procSetCursor                     = user32.NewProc("SetCursor")
	procLoadImageW                    = user32.NewProc("LoadImageW")
	procSetCapture                    = user32.NewProc("SetCapture")
	procReleaseCapture                = user32.NewProc("ReleaseCapture")
	procGetCapture                    = user32.NewProc("GetCapture")
	procTrackMouseEvent               = user32.NewProc("TrackMouseEvent")
	procCreatePopupMenu               = user32.NewProc("CreatePopupMenu")
	procAppendMenuW                   = user32.NewProc("AppendMenuW")
	procTrackPopupMenuEx              = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu                   = user32.NewProc("DestroyMenu")
	procClientToScreen                = user32.NewProc("ClientToScreen")
	procScreenToClient                = user32.NewProc("ScreenToClient")
	procMapWindowPoints               = user32.NewProc("MapWindowPoints")
	procSetTimer                      = user32.NewProc("SetTimer")
	procKillTimer                     = user32.NewProc("KillTimer")
	procMessageBeep                   = user32.NewProc("MessageBeep")
	procOpenClipboard                 = user32.NewProc("OpenClipboard")
	procEmptyClipboard                = user32.NewProc("EmptyClipboard")
	procSetClipboardData              = user32.NewProc("SetClipboardData")
	procCloseClipboard                = user32.NewProc("CloseClipboard")
	procFillRect                      = user32.NewProc("FillRect")
	procDrawTextW                     = user32.NewProc("DrawTextW")
	procMonitorFromWindow             = user32.NewProc("MonitorFromWindow")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procGetKeyState                   = user32.NewProc("GetKeyState")
	procPrintWindow                   = user32.NewProc("PrintWindow")
	procGetAncestor                   = user32.NewProc("GetAncestor")
	procSetForegroundWindow           = user32.NewProc("SetForegroundWindow")
	procSetScrollInfo                 = user32.NewProc("SetScrollInfo")
	procGetScrollInfo                 = user32.NewProc("GetScrollInfo")
	procShowScrollBar                 = user32.NewProc("ShowScrollBar")
	procGetWindow                     = user32.NewProc("GetWindow")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procWindowFromPoint               = user32.NewProc("WindowFromPoint")
	procGetSystemMetricsForDpi        = user32.NewProc("GetSystemMetricsForDpi")
	procGetDoubleClickTime            = user32.NewProc("GetDoubleClickTime")
	procSetWindowRgn                  = user32.NewProc("SetWindowRgn")
	procMsgWaitForMultipleObjectsEx   = user32.NewProc("MsgWaitForMultipleObjectsEx")
	procNotifyWinEvent                = user32.NewProc("NotifyWinEvent")
	procGetSysColor                   = user32.NewProc("GetSysColor")
	procIsChild                       = user32.NewProc("IsChild")
	procFlashWindow                   = user32.NewProc("FlashWindow")

	// gdi32
	procCreateFontIndirectW    = gdi32.NewProc("CreateFontIndirectW")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procGetStockObject         = gdi32.NewProc("GetStockObject")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procSetTextColor           = gdi32.NewProc("SetTextColor")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procSetBkColor             = gdi32.NewProc("SetBkColor")
	procGetTextExtentPoint32W  = gdi32.NewProc("GetTextExtentPoint32W")
	procGetTextMetricsW        = gdi32.NewProc("GetTextMetricsW")
	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	procGetTextFaceW           = gdi32.NewProc("GetTextFaceW")
	procEnumFontFamiliesExW    = gdi32.NewProc("EnumFontFamiliesExW")
	procSetViewportOrgEx       = gdi32.NewProc("SetViewportOrgEx")
	procGetViewportOrgEx       = gdi32.NewProc("GetViewportOrgEx")
	procOffsetViewportOrgEx    = gdi32.NewProc("OffsetViewportOrgEx")
	procIntersectClipRect      = gdi32.NewProc("IntersectClipRect")
	procSaveDC                 = gdi32.NewProc("SaveDC")
	procRestoreDC              = gdi32.NewProc("RestoreDC")
	procCreateRectRgn          = gdi32.NewProc("CreateRectRgn")
	procAddFontResourceExW     = gdi32.NewProc("AddFontResourceExW")
	procExtTextOutW            = gdi32.NewProc("ExtTextOutW")

	// uxtheme, dwmapi, shell32, comdlg32
	procBufferedPaintInit         = uxtheme.NewProc("BufferedPaintInit")
	procBeginBufferedPaint        = uxtheme.NewProc("BeginBufferedPaint")
	procEndBufferedPaint          = uxtheme.NewProc("EndBufferedPaint")
	procDrawThemeParentBackground = uxtheme.NewProc("DrawThemeParentBackground")
	procSetWindowTheme            = uxtheme.NewProc("SetWindowTheme")
	procDwmSetWindowAttribute     = dwmapi.NewProc("DwmSetWindowAttribute")
	procShellExecuteW             = shell32.NewProc("ShellExecuteW")
	procChooseColorW              = comdlg32.NewProc("ChooseColorW")
	procCoInitializeEx            = ole32.NewProc("CoInitializeEx")
	procRegOpenKeyExW             = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW          = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey               = advapi32.NewProc("RegCloseKey")
	procAlphaBlend                = msimg32.NewProc("AlphaBlend")
)

// Instance is the executable's module handle.
func Instance() uintptr {
	h, _, _ := procGetModuleHandleW.Call(0)
	return h
}

func CurrentThreadID() uint32 {
	id, _, _ := procGetCurrentThreadId.Call()
	return uint32(id)
}

// Window messages and styles.
const (
	WM_CREATE           = 0x0001
	WM_DESTROY          = 0x0002
	WM_MOVE             = 0x0003
	WM_SIZE             = 0x0005
	WM_ACTIVATE         = 0x0006
	WM_SETFOCUS         = 0x0007
	WM_KILLFOCUS        = 0x0008
	WM_ENABLE           = 0x000A
	WM_SETREDRAW        = 0x000B
	WM_SETTEXT          = 0x000C
	WM_PAINT            = 0x000F
	WM_CLOSE            = 0x0010
	WM_QUIT             = 0x0012
	WM_ERASEBKGND       = 0x0014
	WM_SHOWWINDOW       = 0x0018
	WM_SETTINGCHANGE    = 0x001A
	WM_SETCURSOR        = 0x0020
	WM_MOUSEACTIVATE    = 0x0021
	WM_GETMINMAXINFO    = 0x0024
	WM_SETFONT          = 0x0030
	WM_WINDOWPOSCHANGED = 0x0047
	WM_NOTIFY           = 0x004E
	WM_CONTEXTMENU      = 0x007B
	WM_GETDLGCODE       = 0x0087
	WM_NCDESTROY        = 0x0082
	WM_KEYDOWN          = 0x0100
	WM_KEYUP            = 0x0101
	WM_CHAR             = 0x0102
	WM_SYSKEYDOWN       = 0x0104
	WM_COMMAND          = 0x0111
	WM_SYSCOMMAND       = 0x0112
	WM_TIMER            = 0x0113
	WM_HSCROLL          = 0x0114
	WM_VSCROLL          = 0x0115
	WM_CTLCOLOREDIT     = 0x0133
	WM_CTLCOLORLISTBOX  = 0x0134
	WM_CTLCOLORBTN      = 0x0135
	WM_CTLCOLORSTATIC   = 0x0138
	WM_MOUSEMOVE        = 0x0200
	WM_LBUTTONDOWN      = 0x0201
	WM_LBUTTONUP        = 0x0202
	WM_LBUTTONDBLCLK    = 0x0203
	WM_RBUTTONDOWN      = 0x0204
	WM_RBUTTONUP        = 0x0205
	WM_MOUSEWHEEL       = 0x020A
	WM_MOUSEHWHEEL      = 0x020E
	WM_CAPTURECHANGED   = 0x0215
	WM_DPICHANGED       = 0x02E0
	WM_MOUSELEAVE       = 0x02A3
	WM_PRINTCLIENT      = 0x0318
	WM_THEMECHANGED     = 0x031A
	WM_USER             = 0x0400
	WM_APP              = 0x8000

	WS_OVERLAPPED       = 0x00000000
	WS_POPUP            = 0x80000000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_DISABLED         = 0x08000000
	WS_CLIPSIBLINGS     = 0x04000000
	WS_CLIPCHILDREN     = 0x02000000
	WS_CAPTION          = 0x00C00000
	WS_BORDER           = 0x00800000
	WS_VSCROLL          = 0x00200000
	WS_HSCROLL          = 0x00100000
	WS_SYSMENU          = 0x00080000
	WS_THICKFRAME       = 0x00040000
	WS_GROUP            = 0x00020000
	WS_TABSTOP          = 0x00010000
	WS_MINIMIZEBOX      = 0x00020000
	WS_MAXIMIZEBOX      = 0x00010000
	WS_OVERLAPPEDWINDOW = WS_OVERLAPPED | WS_CAPTION | WS_SYSMENU | WS_THICKFRAME | WS_MINIMIZEBOX | WS_MAXIMIZEBOX

	WS_EX_DLGMODALFRAME  = 0x00000001
	WS_EX_TOPMOST        = 0x00000008
	WS_EX_TRANSPARENT    = 0x00000020
	WS_EX_TOOLWINDOW     = 0x00000080
	WS_EX_CONTROLPARENT  = 0x00010000
	WS_EX_COMPOSITED     = 0x02000000
	WS_EX_NOACTIVATE     = 0x08000000
	WS_EX_LAYERED        = 0x00080000
	WS_EX_NOPARENTNOTIFY = 0x00000004

	CS_VREDRAW    = 0x0001
	CS_HREDRAW    = 0x0002
	CS_DBLCLKS    = 0x0008
	CW_USEDEFAULT = ^uintptr(0x7fffffff) // 0x80000000 as an int

	SW_HIDE           = 0
	SW_SHOWNORMAL     = 1
	SW_SHOWNOACTIVATE = 4
	SW_SHOW           = 5
	SW_SHOWNA         = 8

	SWP_NOSIZE        = 0x0001
	SWP_NOMOVE        = 0x0002
	SWP_NOZORDER      = 0x0004
	SWP_NOREDRAW      = 0x0008
	SWP_NOACTIVATE    = 0x0010
	SWP_FRAMECHANGED  = 0x0020
	SWP_SHOWWINDOW    = 0x0040
	SWP_HIDEWINDOW    = 0x0080
	SWP_NOCOPYBITS    = 0x0100
	SWP_NOOWNERZORDER = 0x0200

	HWND_TOP    = 0
	HWND_BOTTOM = 1

	GWL_STYLE     = -16
	GWL_EXSTYLE   = -20
	GWLP_USERDATA = -21

	GA_ROOT     = 2
	GW_HWNDPREV = 3
	GW_HWNDNEXT = 2

	RDW_INVALIDATE  = 0x0001
	RDW_ERASE       = 0x0004
	RDW_ALLCHILDREN = 0x0080
	RDW_UPDATENOW   = 0x0100
	RDW_FRAME       = 0x0400

	MK_LBUTTON = 0x0001

	VK_BACK    = 0x08
	VK_TAB     = 0x09
	VK_RETURN  = 0x0D
	VK_SHIFT   = 0x10
	VK_CONTROL = 0x11
	VK_MENU    = 0x12
	VK_ESCAPE  = 0x1B
	VK_SPACE   = 0x20
	VK_PRIOR   = 0x21
	VK_NEXT    = 0x22
	VK_END     = 0x23
	VK_HOME    = 0x24
	VK_LEFT    = 0x25
	VK_UP      = 0x26
	VK_RIGHT   = 0x27
	VK_DOWN    = 0x28
	VK_F5      = 0x74

	DLGC_WANTARROWS  = 0x0001
	DLGC_WANTTAB     = 0x0002
	DLGC_WANTALLKEYS = 0x0004
	DLGC_WANTCHARS   = 0x0080
	DLGC_BUTTON      = 0x2000

	IDC_ARROW = 32512
	IDC_HAND  = 32649
	IDC_IBEAM = 32513

	TME_LEAVE = 0x00000002

	SB_HORZ             = 0
	SB_VERT             = 1
	SB_LINEUP           = 0
	SB_LINEDOWN         = 1
	SB_PAGEUP           = 2
	SB_PAGEDOWN         = 3
	SB_THUMBPOSITION    = 4
	SB_THUMBTRACK       = 5
	SB_TOP              = 6
	SB_BOTTOM           = 7
	SB_ENDSCROLL        = 8
	SIF_RANGE           = 0x1
	SIF_PAGE            = 0x2
	SIF_POS             = 0x4
	SIF_TRACKPOS        = 0x10
	SIF_DISABLENOSCROLL = 0x8
	SIF_ALL             = SIF_RANGE | SIF_PAGE | SIF_POS | SIF_TRACKPOS

	MF_STRING       = 0x0000
	MF_GRAYED       = 0x0001
	MF_SEPARATOR    = 0x0800
	TPM_RETURNCMD   = 0x0100
	TPM_RIGHTBUTTON = 0x0002
	TPM_LEFTALIGN   = 0x0000
	TPM_TOPALIGN    = 0x0000

	SPI_GETWHEELSCROLLLINES    = 0x0068
	SPI_GETCLIENTAREAANIMATION = 0x1042

	MONITOR_DEFAULTTONEAREST = 2

	PW_RENDERFULLCONTENT = 2

	WHEEL_DELTA = 120
)

// Common control classes, styles and messages.
const (
	BS_PUSHBUTTON    = 0x0000
	BS_DEFPUSHBUTTON = 0x0001
	BS_CHECKBOX      = 0x0002
	BS_AUTOCHECKBOX  = 0x0003
	BS_OWNERDRAW     = 0x000B
	BM_GETCHECK      = 0x00F0
	BM_SETCHECK      = 0x00F1
	BM_SETSTYLE      = 0x00F4
	BM_CLICK         = 0x00F5
	BN_CLICKED       = 0

	ES_LEFT            = 0x0000
	ES_CENTER          = 0x0001
	ES_RIGHT           = 0x0002
	ES_MULTILINE       = 0x0004
	ES_PASSWORD        = 0x0020
	ES_AUTOVSCROLL     = 0x0040
	ES_AUTOHSCROLL     = 0x0080
	ES_NOHIDESEL       = 0x0100
	ES_READONLY        = 0x0800
	ES_WANTRETURN      = 0x1000
	ES_NUMBER          = 0x2000
	EM_GETSEL          = 0x00B0
	EM_SETSEL          = 0x00B1
	EM_GETRECT         = 0x00B2
	EM_SETRECT         = 0x00B3
	EM_SETRECTNP       = 0x00B4
	EM_GETLINECOUNT    = 0x00BA
	EM_SETMARGINS      = 0x00D3
	EM_SETPASSWORDCHAR = 0x00CC
	EM_SETCUEBANNER    = 0x1501
	EN_SETFOCUS        = 0x0100
	EN_KILLFOCUS       = 0x0200
	EN_CHANGE          = 0x0300
	EC_LEFTMARGIN      = 1
	EC_RIGHTMARGIN     = 2

	CBS_DROPDOWNLIST   = 0x0003
	CB_ADDSTRING       = 0x0143
	CB_GETCURSEL       = 0x0147
	CB_RESETCONTENT    = 0x014B
	CB_SETCURSEL       = 0x014E
	CB_SETITEMHEIGHT   = 0x0153
	CB_GETCOMBOBOXINFO = 0x0164
	CBN_SELCHANGE      = 1

	TBS_HORZ           = 0x0000
	TBS_NOTICKS        = 0x0010
	TBS_BOTH           = 0x0008
	TBS_FOCUSDISP      = 0x0400 // not a real style: unused
	TBM_GETPOS         = WM_USER
	TBM_SETPOS         = WM_USER + 5
	TBM_SETRANGEMIN    = WM_USER + 7
	TBM_SETRANGEMAX    = WM_USER + 8
	TBM_SETPAGESIZE    = WM_USER + 21
	TBM_SETLINESIZE    = WM_USER + 23
	TBM_GETTHUMBRECT   = WM_USER + 25
	TBM_GETCHANNELRECT = WM_USER + 26
	TBM_SETTHUMBLENGTH = WM_USER + 27

	DTS_SHORTDATEFORMAT = 0x0000
	DTS_TIMEFORMAT      = 0x0009
	DTM_GETSYSTEMTIME   = 0x1001
	DTM_SETSYSTEMTIME   = 0x1002
	DTM_SETRANGE        = 0x1004
	DTM_SETFORMATW      = 0x1032
	DTM_GETIDEALSIZE    = 0x100F
	DTM_SETMCFONT       = 0x1009
	GDT_VALID           = 0
	GDT_NONE            = 1
	GDTR_MIN            = 1
	GDTR_MAX            = 2
	DTN_DATETIMECHANGE  = -759

	NM_CUSTOMDRAW = -12
	NM_CLICK      = -2

	CDDS_PREPAINT         = 0x00000001
	CDDS_POSTPAINT        = 0x00000002
	CDDS_ITEM             = 0x00010000
	CDDS_ITEMPREPAINT     = CDDS_ITEM | CDDS_PREPAINT
	CDRF_DODEFAULT        = 0x0
	CDRF_SKIPDEFAULT      = 0x4
	CDRF_NOTIFYITEMDRAW   = 0x20
	CDRF_NOTIFYPOSTPAINT  = 0x10
	CDIS_SELECTED         = 0x0001
	CDIS_GRAYED           = 0x0002
	CDIS_DISABLED         = 0x0004
	CDIS_CHECKED          = 0x0008
	CDIS_FOCUS            = 0x0010
	CDIS_HOT              = 0x0040
	CDIS_SHOWKEYBOARDCUES = 0x0200
	TBCD_TICS             = 1
	TBCD_THUMB            = 2
	TBCD_CHANNEL          = 3

	WM_UPDATEUISTATE = 0x0128
	UIS_SET          = 1
	UISF_HIDEFOCUS   = 0x1
)

// GetMessage waits for the next message; false at WM_QUIT.
func GetMessage(m *Msg) bool {
	r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(m)), 0, 0, 0)
	return int32(r) > 0
}

// PeekMessage takes the next message, if there's one.
func PeekMessage(m *Msg) bool {
	r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(m)), 0, 0, 0, 1) // PM_REMOVE
	return r != 0
}

func TranslateMessage(m *Msg) { procTranslateMessage.Call(uintptr(unsafe.Pointer(m))) }
func DispatchMessage(m *Msg)  { procDispatchMessageW.Call(uintptr(unsafe.Pointer(m))) }

// IsDialogMessage handles keyboard navigation (Tab, arrows) in a window
// of controls.
func IsDialogMessage(hwnd uintptr, m *Msg) bool {
	r, _, _ := procIsDialogMessageW.Call(hwnd, uintptr(unsafe.Pointer(m)))
	return r != 0
}

func PostQuitMessage(code int) { procPostQuitMessage.Call(uintptr(code)) }

// WaitMessage waits up to ms for input or a posted message.
func WaitMessage(ms uint32) {
	procMsgWaitForMultipleObjectsEx.Call(0, 0, uintptr(ms), 0x04FF, 0x0004) // QS_ALLINPUT, MWMO_INPUTAVAILABLE
}

func PostMessage(hwnd uintptr, m uint32, w, l uintptr) {
	procPostMessageW.Call(hwnd, uintptr(m), w, l)
}

func SendMessage(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(hwnd, uintptr(m), w, l)
	return r
}

func DefWindowProc(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(m), w, l)
	return r
}

// RegisterClass registers a window class whose window procedure is
// proc; it returns false if it fails.
func RegisterClass(name string, style uint32, proc uintptr, icon uintptr) bool {
	cursor, _, _ := procLoadCursorW.Call(0, IDC_ARROW)
	wc := WndClassEx{
		Style:     style,
		WndProc:   proc,
		Instance:  Instance(),
		Cursor:    cursor,
		Icon:      icon,
		IconSm:    icon,
		ClassName: UTF16(name),
	}
	wc.Size = uint32(unsafe.Sizeof(wc))
	r, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	return r != 0
}

// AppIcon is the executable's icon (resource 1, as nui build embeds it),
// or 0.
func AppIcon() uintptr {
	h, _, _ := user32.NewProc("LoadIconW").Call(Instance(), 1)
	return h
}

func CreateWindow(exStyle uint32, class, title string, style uint32, x, y, w, h int32, parent uintptr, id uintptr) uintptr {
	hwnd, _, _ := procCreateWindowExW.Call(uintptr(exStyle), Str(class), Str(title), uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h), parent, id, Instance(), 0)
	return hwnd
}

func DestroyWindow(hwnd uintptr) { procDestroyWindow.Call(hwnd) }

func IsWindow(hwnd uintptr) bool {
	r, _, _ := procIsWindow.Call(hwnd)
	return r != 0
}

func ShowWindow(hwnd uintptr, cmd int) { procShowWindow.Call(hwnd, uintptr(cmd)) }
func UpdateWindow(hwnd uintptr)        { procUpdateWindow.Call(hwnd) }

func IsWindowVisible(hwnd uintptr) bool {
	r, _, _ := procIsWindowVisible.Call(hwnd)
	return r != 0
}

func SetWindowPos(hwnd, after uintptr, x, y, w, h int32, flags uint32) {
	procSetWindowPos.Call(hwnd, after, uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(flags))
}

// DeferPos moves several windows of one parent at once (BeginDeferWindowPos).
type DeferPos uintptr

func BeginDeferWindowPos(n int) DeferPos {
	r, _, _ := procBeginDeferWindowPos.Call(uintptr(n))
	return DeferPos(r)
}

func (d DeferPos) Defer(hwnd, after uintptr, x, y, w, h int32, flags uint32) DeferPos {
	if d == 0 {
		SetWindowPos(hwnd, after, x, y, w, h, flags)
		return 0
	}
	r, _, _ := procDeferWindowPos.Call(uintptr(d), hwnd, after, uintptr(x), uintptr(y), uintptr(w), uintptr(h), uintptr(flags))
	return DeferPos(r)
}

func (d DeferPos) End() {
	if d != 0 {
		procEndDeferWindowPos.Call(uintptr(d))
	}
}

func ClientRect(hwnd uintptr) Rect {
	var r Rect
	procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

func WindowRect(hwnd uintptr) Rect {
	var r Rect
	procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

// WindowRectIn is a window's rectangle in its parent's client
// coordinates.
func WindowRectIn(hwnd, parent uintptr) Rect {
	r := WindowRect(hwnd)
	procMapWindowPoints.Call(0, parent, uintptr(unsafe.Pointer(&r)), 2)
	return r
}

func MapPoint(from, to uintptr, p Point) Point {
	procMapWindowPoints.Call(from, to, uintptr(unsafe.Pointer(&p)), 1)
	return p
}

func ClientToScreen(hwnd uintptr, p Point) Point {
	procClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return p
}

func ScreenToClient(hwnd uintptr, p Point) Point {
	procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return p
}

func CursorPos() Point {
	var p Point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

func SetWindowText(hwnd uintptr, s string) { procSetWindowTextW.Call(hwnd, Str(s)) }

func WindowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}

func WindowLong(hwnd uintptr, index int32) uintptr {
	r, _, _ := procGetWindowLongPtrW.Call(hwnd, uintptr(index))
	return r
}

func SetWindowLong(hwnd uintptr, index int32, v uintptr) {
	procSetWindowLongPtrW.Call(hwnd, uintptr(index), v)
}

func SetParent(hwnd, parent uintptr) { procSetParent.Call(hwnd, parent) }

func Parent(hwnd uintptr) uintptr {
	r, _, _ := procGetParent.Call(hwnd)
	return r
}

func RootWindow(hwnd uintptr) uintptr {
	r, _, _ := procGetAncestor.Call(hwnd, GA_ROOT)
	return r
}

func NextWindow(hwnd uintptr, cmd uint32) uintptr {
	r, _, _ := procGetWindow.Call(hwnd, uintptr(cmd))
	return r
}

// EnumChildWindows calls cb (made once with syscall.NewCallback, taking
// a window and the list) for each descendant of hwnd.
func EnumChildWindows(hwnd, cb uintptr, list *[]uintptr) {
	user32.NewProc("EnumChildWindows").Call(hwnd, cb, uintptr(unsafe.Pointer(list)))
}

func IsChild(parent, hwnd uintptr) bool {
	r, _, _ := procIsChild.Call(parent, hwnd)
	return r != 0
}

func Invalidate(hwnd uintptr) { procInvalidateRect.Call(hwnd, 0, 0) }

func InvalidateRect(hwnd uintptr, r *Rect) {
	procInvalidateRect.Call(hwnd, uintptr(unsafe.Pointer(r)), 0)
}

func RedrawWindow(hwnd uintptr, flags uint32) { procRedrawWindow.Call(hwnd, 0, 0, uintptr(flags)) }

func BeginPaint(hwnd uintptr, ps *PaintStruct) uintptr {
	r, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(ps)))
	return r
}

func EndPaint(hwnd uintptr, ps *PaintStruct) { procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(ps))) }

func GetDC(hwnd uintptr) uintptr {
	r, _, _ := procGetDC.Call(hwnd)
	return r
}

func ReleaseDC(hwnd, dc uintptr) { procReleaseDC.Call(hwnd, dc) }

func SetFocus(hwnd uintptr) { procSetFocus.Call(hwnd) }

func Focus() uintptr {
	r, _, _ := procGetFocus.Call()
	return r
}

func EnableWindow(hwnd uintptr, on bool) { procEnableWindow.Call(hwnd, b2u(on)) }

func IsWindowEnabled(hwnd uintptr) bool {
	r, _, _ := procIsWindowEnabled.Call(hwnd)
	return r != 0
}

func Cursor(id uintptr) uintptr {
	r, _, _ := procLoadCursorW.Call(0, id)
	return r
}

func SetCursor(c uintptr) { procSetCursor.Call(c) }

func SetCapture(hwnd uintptr) { procSetCapture.Call(hwnd) }
func ReleaseCapture()         { procReleaseCapture.Call() }

func Capture() uintptr {
	r, _, _ := procGetCapture.Call()
	return r
}

// TrackLeave asks for WM_MOUSELEAVE when the mouse leaves hwnd.
func TrackLeave(hwnd uintptr) {
	t := TrackMouseEventS{Flags: TME_LEAVE, Hwnd: hwnd}
	t.Size = uint32(unsafe.Sizeof(t))
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&t)))
}

func SetTimer(hwnd, id uintptr, ms uint32) { procSetTimer.Call(hwnd, id, uintptr(ms), 0) }
func KillTimer(hwnd, id uintptr)           { procKillTimer.Call(hwnd, id) }

func KeyDown(vk int) bool {
	r, _, _ := procGetKeyState.Call(uintptr(vk))
	return int16(r) < 0
}

func MessageBeep() { procMessageBeep.Call(0xFFFFFFFF) }

func SetForegroundWindow(hwnd uintptr) { procSetForegroundWindow.Call(hwnd) }

func DoubleClickTime() uint32 {
	r, _, _ := procGetDoubleClickTime.Call()
	return uint32(r)
}

// NotifyWinEvent tells accessibility clients about an event on hwnd.
func NotifyWinEvent(event uint32, hwnd uintptr, object, child int32) {
	procNotifyWinEvent.Call(uintptr(event), hwnd, uintptr(object), uintptr(child))
}

// PrintWindow draws a window, its children and DirectComposition content
// too, into dc.
func PrintWindow(hwnd, dc uintptr) bool {
	r, _, _ := procPrintWindow.Call(hwnd, dc, PW_RENDERFULLCONTENT)
	return r != 0
}

// SetScroll sets a scroll bar's range, page and position.
func SetScroll(hwnd uintptr, bar int, max, page, pos int32) {
	si := ScrollInfo{Mask: SIF_RANGE | SIF_PAGE | SIF_POS, Min: 0, Max: max, Page: uint32(max0(page)), Pos: pos}
	si.Size = uint32(unsafe.Sizeof(si))
	procSetScrollInfo.Call(hwnd, uintptr(bar), uintptr(unsafe.Pointer(&si)), 1)
}

// Scroll returns a scroll bar's info.
func Scroll(hwnd uintptr, bar int) ScrollInfo {
	si := ScrollInfo{Mask: SIF_ALL}
	si.Size = uint32(unsafe.Sizeof(si))
	procGetScrollInfo.Call(hwnd, uintptr(bar), uintptr(unsafe.Pointer(&si)))
	return si
}

func ShowScrollBar(hwnd uintptr, bar int, show bool) {
	procShowScrollBar.Call(hwnd, uintptr(bar), b2u(show))
}

func max0(v int32) int32 {
	if v < 0 {
		return 0
	}
	return v
}

// SystemParam reads an int or bool system parameter.
func SystemParam(action uint32) uint32 {
	var v uint32
	procSystemParametersInfoW.Call(uintptr(action), 0, uintptr(unsafe.Pointer(&v)), 0)
	return v
}

// Menus.

func CreatePopupMenu() uintptr {
	r, _, _ := procCreatePopupMenu.Call()
	return r
}

func AppendMenu(m uintptr, flags uint32, id uintptr, title string) {
	procAppendMenuW.Call(m, uintptr(flags), id, StrOpt(title))
}

// TrackPopupMenu shows a menu at a screen point and returns the chosen
// item's ID (0: none).
func TrackPopupMenu(m uintptr, flags uint32, x, y int32, owner uintptr) int {
	r, _, _ := procTrackPopupMenuEx.Call(m, uintptr(flags|TPM_RETURNCMD), uintptr(x), uintptr(y), owner, 0)
	return int(r)
}

func DestroyMenu(m uintptr) { procDestroyMenu.Call(m) }

// DPI.

// DPIAware makes the process per-monitor DPI aware (v2): windows get
// WM_DPICHANGED and draw at each monitor's DPI, not scaled by the system.
func DPIAware() {
	if Has(procSetProcessDpiAwarenessContext) {
		procSetProcessDpiAwarenessContext.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2: -4
	}
}

// DPI is a window's dots per inch (96: 100%).
func DPI(hwnd uintptr) int {
	if hwnd != 0 && Has(procGetDpiForWindow) {
		if d, _, _ := procGetDpiForWindow.Call(hwnd); d != 0 {
			return int(d)
		}
	}
	if Has(procGetDpiForSystem) {
		if d, _, _ := procGetDpiForSystem.Call(); d != 0 {
			return int(d)
		}
	}
	return 96
}

// AdjustWindowRect grows a client rectangle to the window's, at dpi.
func AdjustWindowRect(r *Rect, style, exStyle uint32, dpi int) {
	if Has(procAdjustWindowRectExForDpi) {
		procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(r)), uintptr(style), 0, uintptr(exStyle), uintptr(dpi))
		return
	}
	user32.NewProc("AdjustWindowRectEx").Call(uintptr(unsafe.Pointer(r)), uintptr(style), 0, uintptr(exStyle))
}

func SystemMetric(index, dpi int) int {
	if Has(procGetSystemMetricsForDpi) {
		r, _, _ := procGetSystemMetricsForDpi.Call(uintptr(index), uintptr(dpi))
		return int(r)
	}
	r, _, _ := user32.NewProc("GetSystemMetrics").Call(uintptr(index))
	return int(r)
}

// WorkArea is the work area of the monitor a window is on.
func WorkArea(hwnd uintptr) Rect {
	m, _, _ := procMonitorFromWindow.Call(hwnd, MONITOR_DEFAULTTONEAREST)
	mi := MonitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi)))
	return mi.Work
}

// ActivateCommonControls activates version 6 of the common controls
// (themed controls, task dialogs) for executables without a manifest
// asking for it, like go run builds: shell32.dll carries such a manifest
// as resource 124.
func ActivateCommonControls() {
	if !Has(procCreateActCtxW) {
		return
	}
	type actCtx struct {
		Size                  uint32
		Flags                 uint32
		Source                *uint16
		ProcessorArchitecture uint16
		LangID                uint16
		AssemblyDirectory     *uint16
		ResourceName          uintptr
		ApplicationName       *uint16
		Module                uintptr
	}
	const (
		actCtxFlagResourceNameValid      = 0x008
		actCtxFlagAssemblyDirectoryValid = 0x004
	)
	ctx := actCtx{
		Flags:             actCtxFlagResourceNameValid | actCtxFlagAssemblyDirectoryValid,
		Source:            UTF16(sysDir + `\shell32.dll`),
		AssemblyDirectory: UTF16(sysDir),
		ResourceName:      124,
	}
	ctx.Size = uint32(unsafe.Sizeof(ctx))
	h, _, _ := procCreateActCtxW.Call(uintptr(unsafe.Pointer(&ctx)))
	if h == ^uintptr(0) { // INVALID_HANDLE_VALUE
		return
	}
	var cookie uintptr
	procActivateActCtx.Call(h, uintptr(unsafe.Pointer(&cookie)))
	type initCC struct{ Size, ICC uint32 }
	icc := initCC{Size: 8, ICC: 0x0000FFFF} // every class
	if p := CommonControl("InitCommonControlsEx"); p != 0 {
		syscall.SyscallN(p, uintptr(unsafe.Pointer(&icc)))
	}
	procCoInitializeEx.Call(0, 0x2) // COINIT_APARTMENTTHREADED: rich edit, dialogs
	procBufferedPaintInit.Call()
}

// CommonControl resolves a function of the common controls library,
// loaded by name so the activation context picks version 6, and only
// from the system directory.
func CommonControl(name string) uintptr {
	h, _, _ := procLoadLibraryExW.Call(Str("comctl32.dll"), 0, 0x800) // LOAD_LIBRARY_SEARCH_SYSTEM32
	if h == 0 {
		return 0
	}
	cname := append([]byte(name), 0)
	p, _, _ := procGetProcAddress.Call(h, uintptr(unsafe.Pointer(&cname[0])))
	return p
}

// LoadSystemLibrary loads a DLL of the system directory (msftedit.dll).
func LoadSystemLibrary(name string) uintptr {
	h, _, _ := procLoadLibraryExW.Call(Str(name), 0, 0x800)
	return h
}

var (
	subclassProc uintptr
	setSubclass  uintptr
	defSubclass  uintptr
	subclassOnce sync.Once
)

// Subclass routes hwnd's messages through f (SetWindowSubclass); f
// calls DefSubclass for what it doesn't handle. One callback serves
// every subclassed window: f is found by window.
func Subclass(hwnd uintptr, f func(hwnd uintptr, m uint32, w, l uintptr) uintptr) {
	subclassOnce.Do(func() {
		setSubclass = CommonControl("SetWindowSubclass")
		defSubclass = CommonControl("DefSubclassProc")
		subclassProc = syscall.NewCallback(func(hwnd, m, w, l, id, ref uintptr) uintptr {
			if f := subclassed[hwnd]; f != nil {
				return f(hwnd, uint32(m), w, l)
			}
			return DefSubclass(hwnd, uint32(m), w, l)
		})
	})
	subclassed[hwnd] = f
	syscall.SyscallN(setSubclass, hwnd, subclassProc, 1, 0)
}

var subclassed = map[uintptr]func(hwnd uintptr, m uint32, w, l uintptr) uintptr{}

// Unsubclass forgets hwnd's subclass (it's being destroyed).
func Unsubclass(hwnd uintptr) {
	if _, ok := subclassed[hwnd]; ok {
		delete(subclassed, hwnd)
		syscall.SyscallN(CommonControl("RemoveWindowSubclass"), hwnd, subclassProc, 1)
	}
}

func DefSubclass(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	r, _, _ := syscall.SyscallN(defSubclass, hwnd, uintptr(m), w, l)
	return r
}

// Theme.

// SetWindowTheme sets a control's visual style ("DarkMode_Explorer",
// "Explorer", "DarkMode_CFD"; "" for the default).
func SetWindowTheme(hwnd uintptr, name string) {
	procSetWindowTheme.Call(hwnd, StrOpt(name), 0)
}

var (
	allowDarkWindow uintptr
	preferredMode   uintptr
	flushMenus      uintptr
	darkOnce        sync.Once
)

func darkProcs() {
	darkOnce.Do(func() {
		if Build() < 18362 { // the ordinals are 1903's
			return
		}
		h, _, _ := procLoadLibraryExW.Call(Str("uxtheme.dll"), 0, 0x800)
		if h == 0 {
			return
		}
		ord := func(n uintptr) uintptr {
			p, _, _ := procGetProcAddress.Call(h, n)
			return p
		}
		allowDarkWindow, preferredMode, flushMenus = ord(133), ord(135), ord(136)
	})
}

// SetAppDark has the system draw the app's menus and scroll bars dark
// (uxtheme's SetPreferredAppMode and FlushMenuThemes, undocumented but
// what Explorer and Notepad use).
func SetAppDark(dark bool) {
	darkProcs()
	if preferredMode == 0 {
		return
	}
	mode := uintptr(3) // ForceLight
	if dark {
		mode = 2 // ForceDark
	}
	syscall.SyscallN(preferredMode, mode)
	if flushMenus != 0 {
		syscall.SyscallN(flushMenus)
	}
}

// AllowDark lets a control take dark visual styles.
func AllowDark(hwnd uintptr, dark bool) {
	darkProcs()
	if allowDarkWindow != 0 {
		syscall.SyscallN(allowDarkWindow, hwnd, b2u(dark))
	}
}

// SetDarkTitleBar draws a window's title bar dark or light, and gives it
// Windows 11's rounded corners and Mica backdrop when backdrop is set.
func SetDarkTitleBar(hwnd uintptr, dark bool) {
	v := uint32(0)
	if dark {
		v = 1
	}
	procDwmSetWindowAttribute.Call(hwnd, 20, uintptr(unsafe.Pointer(&v)), 4) // DWMWA_USE_IMMERSIVE_DARK_MODE
}

// AppsUseLightTheme reads the user's app mode (Settings, Personalization,
// Colors): false is dark.
func AppsUseLightTheme() bool {
	v, ok := RegDWORD(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, "AppsUseLightTheme")
	return !ok || v != 0
}

// AccentPalette is the system's accent colors, lightest to darkest
// (Light3, Light2, Light1, the accent, Dark1, Dark2, Dark3), as
// 0x00BBGGRR; ok is false if the system has none.
func AccentPalette() (p [7]uint32, ok bool) {
	b, ok := RegBinary(`Software\Microsoft\Windows\CurrentVersion\Explorer\Accent`, "AccentPalette")
	if !ok || len(b) < 28 {
		return p, false
	}
	for i := range p {
		p[i] = uint32(b[i*4]) | uint32(b[i*4+1])<<8 | uint32(b[i*4+2])<<16
	}
	return p, true
}

// TextScale is the user's text size (Settings, Accessibility, Text
// size), 1 to 2.25.
func TextScale() float32 {
	v, ok := RegDWORD(`Software\Microsoft\Accessibility`, "TextScaleFactor")
	if !ok || v < 100 {
		return 1
	}
	return float32(v) / 100
}

// Registry.

const hkeyCurrentUser = 0x80000001

func regQuery(path, name string) ([]byte, uint32, bool) {
	var key uintptr
	if r, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, Str(path), 0, 0x20019, uintptr(unsafe.Pointer(&key))); r != 0 { // KEY_READ
		return nil, 0, false
	}
	defer procRegCloseKey.Call(key)
	var typ, size uint32
	if r, _, _ := procRegQueryValueExW.Call(key, Str(name), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size))); r != 0 || size == 0 {
		return nil, 0, false
	}
	buf := make([]byte, size)
	if r, _, _ := procRegQueryValueExW.Call(key, Str(name), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size))); r != 0 {
		return nil, 0, false
	}
	return buf[:size], typ, true
}

// RegDWORD reads a DWORD value under HKEY_CURRENT_USER.
func RegDWORD(path, name string) (uint32, bool) {
	b, typ, ok := regQuery(path, name)
	if !ok || typ != 4 || len(b) < 4 {
		return 0, false
	}
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24, true
}

// RegBinary reads a binary value under HKEY_CURRENT_USER.
func RegBinary(path, name string) ([]byte, bool) {
	b, typ, ok := regQuery(path, name)
	return b, ok && typ == 3
}

// Locale and time zone.

// LocaleName is the user's locale, as en-US.
func LocaleName() string {
	buf := make([]uint16, 85)
	n, _, _ := procGetUserDefaultLocale.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// LocaleInfo reads a locale's information (LOCALE_SSHORTDATE...); ""
// is the user's locale.
func LocaleInfo(locale string, typ uint32) string {
	buf := make([]uint16, 128)
	n, _, _ := procGetLocaleInfoEx.Call(StrOpt(locale), uintptr(typ), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if n == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// TimeZone is the system's time zone as an IANA name (Asia/Shanghai),
// through the ICU Windows ships (1903 and later); "" if unknown.
func TimeZone() string {
	var tz struct {
		Bias         int32
		StandardName [32]uint16
		StandardDate SystemTime
		StandardBias int32
		DaylightName [32]uint16
		DaylightDate SystemTime
		DaylightBias int32
		KeyName      [128]uint16
		Dynamic      byte
	}
	if r, _, _ := procGetDynamicTimeZone.Call(uintptr(unsafe.Pointer(&tz))); r == 0xFFFFFFFF {
		return ""
	}
	icu := systemDLL("icu.dll")
	p := icu.NewProc("ucal_getTimeZoneIDForWindowsID")
	if p.Find() != nil {
		return ""
	}
	key := syscall.UTF16ToString(tz.KeyName[:])
	k := UTF16z(key)
	var out [64]uint16
	var status int32
	n, _, _ := p.Call(uintptr(unsafe.Pointer(&k[0])), uintptr(len(k)-1), 0, uintptr(unsafe.Pointer(&out[0])), uintptr(len(out)), uintptr(unsafe.Pointer(&status)))
	if status > 0 || int(n) <= 0 || int(n) > len(out) {
		return ""
	}
	return string(utf16.Decode(out[:n]))
}

// Shell, clipboard, color dialog.

// OpenURL opens a URL (or a file) with the app the system has for it.
func OpenURL(u string) {
	procShellExecuteW.Call(0, Str("open"), Str(u), 0, 0, SW_SHOWNORMAL)
}

// SetClipboardText puts text on the clipboard.
func SetClipboardText(owner uintptr, s string) bool {
	if r, _, _ := procOpenClipboard.Call(owner); r == 0 {
		return false
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()
	u := UTF16z(s)
	h, _, _ := procGlobalAlloc.Call(0x0002, uintptr(len(u)*2)) // GMEM_MOVEABLE
	if h == 0 {
		return false
	}
	p, _, _ := procGlobalLock.Call(h)
	copy(unsafe.Slice((*uint16)(native(p)), len(u)), u)
	procGlobalUnlock.Call(h)
	if r, _, _ := procSetClipboardData.Call(13, h); r == 0 { // CF_UNICODETEXT
		procGlobalFree.Call(h)
		return false
	}
	return true
}

// ChooseColor shows the color dialog; ok is false if the user cancels.
func ChooseColor(owner uintptr, initial uint32) (c uint32, ok bool) {
	var custom [16]uint32
	type chooseColor struct {
		Size         uint32
		Owner        uintptr
		Instance     uintptr
		Result       uint32
		CustColors   *uint32
		Flags        uint32
		CustData     uintptr
		Hook         uintptr
		TemplateName *uint16
	}
	cc := chooseColor{Owner: owner, Result: initial, CustColors: &custom[0], Flags: 0x1 | 0x2} // CC_RGBINIT | CC_FULLOPEN
	cc.Size = uint32(unsafe.Sizeof(cc))
	r, _, _ := procChooseColorW.Call(uintptr(unsafe.Pointer(&cc)))
	return cc.Result, r != 0
}

func b2u(b bool) uintptr {
	if b {
		return 1
	}
	return 0
}

// FrameBounds is a window's visible bounds on screen: its rectangle less
// the invisible resize borders (DWMWA_EXTENDED_FRAME_BOUNDS).
func FrameBounds(hwnd uintptr) Rect {
	var r Rect
	if hr, _, _ := dwmapi.NewProc("DwmGetWindowAttribute").Call(hwnd, 9, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r)); hr != 0 {
		return WindowRect(hwnd)
	}
	return r
}
