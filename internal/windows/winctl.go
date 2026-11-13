//go:build windows

package windows

import "unsafe"

// What controlling top-level windows takes: their placement, their state,
// their system menu, the non-client messages of a window that draws its
// own title bar, and their taskbar button.

const (
	SW_SHOWMINIMIZED   = 2
	SW_MAXIMIZE        = 3
	SW_MINIMIZE        = 6
	SW_SHOWMINNOACTIVE = 7
	SW_RESTORE         = 9

	WM_ACTIVATEAPP       = 0x001C
	WM_NCCALCSIZE        = 0x0083
	WM_NCHITTEST         = 0x0084
	WM_NCMOUSEMOVE       = 0x00A0
	WM_NCLBUTTONDOWN     = 0x00A1
	WM_NCLBUTTONUP       = 0x00A2
	WM_NCLBUTTONDBLCLK   = 0x00A3
	WM_NCRBUTTONDOWN     = 0x00A4
	WM_NCRBUTTONUP       = 0x00A5
	WM_NCRBUTTONDBLCLK   = 0x00A6
	WM_NCMOUSELEAVE      = 0x02A2
	WM_WINDOWPOSCHANGING = 0x0046

	HTNOWHERE   = 0
	HTCLIENT    = 1
	HTCAPTION   = 2
	HTMINBUTTON = 8
	HTMAXBUTTON = 9
	HTTOP       = 12
	HTTOPLEFT   = 13
	HTTOPRIGHT  = 14
	HTCLOSE     = 20

	SC_MINIMIZE = 0xF020
	SC_MAXIMIZE = 0xF030
	SC_CLOSE    = 0xF060
	SC_RESTORE  = 0xF120

	MF_BYCOMMAND = 0x0
	MF_ENABLED   = 0x0

	WA_INACTIVE = 0

	SM_CXSIZEFRAME    = 32
	SM_CYSIZEFRAME    = 33
	SM_CXPADDEDBORDER = 92
	SM_CYCAPTION      = 4

	GWLP_HWNDPARENT = -8
	GW_OWNER        = 4

	TME_NONCLIENT = 0x10

	WS_MINIMIZE = 0x20000000
	WS_MAXIMIZE = 0x01000000

	WS_EX_NOREDIRECTIONBITMAP = 0x00200000
)

// HWND_TOPMOST and HWND_NOTOPMOST are SetWindowPos's places above and
// below the topmost band.
var (
	HWND_TOPMOST   = ^uintptr(0) // -1
	HWND_NOTOPMOST = ^uintptr(1) // -2
)

// WindowPlacement is WINDOWPLACEMENT: a window's normal rectangle (in
// workspace coordinates), and how it shows.
type WindowPlacement struct {
	Length, Flags, ShowCmd uint32
	MinPosition            Point
	MaxPosition            Point
	Normal                 Rect
	Device                 Rect
}

// NCCalcSizeParams is NCCALCSIZE_PARAMS: Rects[0] is the window's
// proposed rectangle, which WM_NCCALCSIZE makes its client area's.
type NCCalcSizeParams struct {
	Rects [3]Rect
	Pos   *WindowPos
}

var (
	procGetWindowPlacement    = user32.NewProc("GetWindowPlacement")
	procSetWindowPlacement    = user32.NewProc("SetWindowPlacement")
	procGetSystemMenu         = user32.NewProc("GetSystemMenu")
	procEnableMenuItem        = user32.NewProc("EnableMenuItem")
	procMonitorFromPoint      = user32.NewProc("MonitorFromPoint")
	procDwmExtendFrame        = dwmapi.NewProc("DwmExtendFrameIntoClientArea")
	procAllowSetForegroundWin = user32.NewProc("AllowSetForegroundWindow")
	procBringWindowToTop      = user32.NewProc("BringWindowToTop")
	procUpdateLayeredWindow   = user32.NewProc("UpdateLayeredWindow")
	procSetLayeredWindowAttrs = user32.NewProc("SetLayeredWindowAttributes")
	procGdiFlush              = gdi32.NewProc("GdiFlush")
)

// SetLayeredOpaque gives a layered window its attributes, fully opaque:
// a layered window shows, and takes the mouse, once it has them (one with
// no pixels of its own, as an invisible strip that answers hit tests).
func SetLayeredOpaque(hwnd uintptr) { procSetLayeredWindowAttrs.Call(hwnd, 0, 255, 2) } // LWA_ALPHA

// UpdateLayered shows a layered window's pixels: the w by h premultiplied
// BGRA bitmap selected into dc, blended over what's behind it by their
// alpha (UpdateLayeredWindow, ULW_ALPHA).
func UpdateLayered(hwnd, dc uintptr, w, h int32) {
	screen := GetDC(0)
	defer ReleaseDC(0, screen)
	size := [2]int32{w, h}
	var origin Point
	blend := uint32(255<<16 | 1<<24) // AC_SRC_OVER, SourceConstantAlpha 255, AC_SRC_ALPHA
	procUpdateLayeredWindow.Call(hwnd, screen, 0, uintptr(unsafe.Pointer(&size)), dc,
		uintptr(unsafe.Pointer(&origin)), 0, uintptr(unsafe.Pointer(&blend)), 2) // ULW_ALPHA
}

// GdiFlush finishes GDI's drawing, so the pixels of a DIB section are
// there to read.
func GdiFlush() { procGdiFlush.Call() }

// Placement is a window's placement.
func Placement(hwnd uintptr) WindowPlacement {
	p := WindowPlacement{}
	p.Length = uint32(unsafe.Sizeof(p))
	procGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return p
}

// SetPlacement sets a window's placement.
func SetPlacement(hwnd uintptr, p WindowPlacement) {
	p.Length = uint32(unsafe.Sizeof(p))
	procSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&p)))
}

// EnableSystemCommand enables or grays a command of a window's system
// menu (SC_CLOSE grays its close button too).
func EnableSystemCommand(hwnd uintptr, cmd uint32, on bool) {
	m, _, _ := procGetSystemMenu.Call(hwnd, 0)
	if m == 0 {
		return
	}
	flags := uintptr(MF_BYCOMMAND | MF_ENABLED)
	if !on {
		flags |= MF_GRAYED
	}
	procEnableMenuItem.Call(m, uintptr(cmd), flags)
}

// Owner is a top-level window's owner, or 0.
func Owner(hwnd uintptr) uintptr {
	r, _, _ := procGetWindow.Call(hwnd, GW_OWNER)
	return r
}

// TrackNonClientLeave asks for WM_NCMOUSELEAVE (nonClient) or
// WM_MOUSELEAVE when the pointer leaves hwnd.
func TrackNonClientLeave(hwnd uintptr, nonClient bool) {
	tme := struct {
		Size, Flags uint32
		Track       uintptr
		HoverTime   uint32
	}{Flags: TME_LEAVE, Track: hwnd}
	if nonClient {
		tme.Flags |= TME_NONCLIENT
	}
	tme.Size = uint32(unsafe.Sizeof(tme))
	procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
}

// MonitorArea is the whole rectangle of the monitor a window is most on
// (what full screen covers).
func MonitorArea(hwnd uintptr) Rect {
	m, _, _ := procMonitorFromWindow.Call(hwnd, MONITOR_DEFAULTTONEAREST)
	mi := MonitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi)))
	return mi.Monitor
}

// MonitorAt is the monitor at p (screen pixels), or the nearest, and its
// rectangle, work area and DPI.
func MonitorAt(p Point) (area, work Rect, dpi int) {
	pt := uintptr(uint32(p.X)) | uintptr(uint32(p.Y))<<32
	m, _, _ := procMonitorFromPoint.Call(pt, MONITOR_DEFAULTTONEAREST)
	mi := MonitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi)))
	return mi.Monitor, mi.Work, MonitorDPI(m)
}

var procGetDpiForMonitor = systemDLL("shcore.dll").NewProc("GetDpiForMonitor")

// MonitorDPI is a monitor's DPI (96 if Windows can't say).
func MonitorDPI(m uintptr) int {
	if m != 0 && Has(procGetDpiForMonitor) {
		var dx, dy uint32
		if r, _, _ := procGetDpiForMonitor.Call(m, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx != 0 {
			return int(dx)
		}
	}
	return DPI(0)
}

// SetCaptionColor sets the color DWM paints a window's caption in
// (Windows 11): DWMWA_COLOR_NONE (0xFFFFFFFE) none, so the accent never
// shows on what's left of a hidden title bar.
func SetCaptionColor(hwnd uintptr, c uint32) {
	procDwmSetWindowAttribute.Call(hwnd, 35, uintptr(unsafe.Pointer(&c)), 4) // DWMWA_CAPTION_COLOR
}

// ExtendFrame extends DWM's frame into the client area by top pixels (the
// shadow of a window without a caption).
func ExtendFrame(hwnd uintptr, top int32) {
	m := struct{ Left, Right, Top, Bottom int32 }{Top: top}
	procDwmExtendFrame.Call(hwnd, uintptr(unsafe.Pointer(&m)))
}

// BringWindowToTop puts a window at the top of the Z order.
func BringWindowToTop(hwnd uintptr) { procBringWindowToTop.Call(hwnd) }

// TaskbarDeleteTab takes a window's button off the taskbar
// (ITaskbarList::DeleteTab), or puts it back (AddTab). Explorer makes the
// button again when the window shows or Explorer restarts
// (TaskbarButtonCreated): call it then too.
func TaskbarDeleteTab(hwnd uintptr, remove bool) bool {
	if taskbarList == 0 {
		taskbarList = CoCreate(&clsidTaskbarList, &iidTaskbarList3)
		if taskbarList == 0 {
			return false
		}
		ComCall(taskbarList, 3) // HrInit
	}
	i := 4 // AddTab
	if remove {
		i = 5 // DeleteTab
	}
	return !Failed(ComCall(taskbarList, i, hwnd))
}
