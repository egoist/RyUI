//go:build windows

package windows

import "unsafe"

// What flyouts, panels and click-through windows take: where a
// notification icon is, the monitor of a rectangle, a window class with
// the drop shadow of menus, and a click that only activates.

const (
	// CS_DROPSHADOW gives a window class's windows the drop shadow of
	// menus and tooltips.
	CS_DROPSHADOW = 0x00020000

	// WM_MOUSEACTIVATE's answers: MA_ACTIVATEANDEAT activates the window
	// and drops the click.
	MA_ACTIVATE       = 1
	MA_ACTIVATEANDEAT = 2

	// CB_GETDROPPEDSTATE reports whether a combo box's list is open.
	CB_GETDROPPEDSTATE = 0x0157

	// DWMWCP_ROUND is DWMWA_WINDOW_CORNER_PREFERENCE's rounded corners
	// (Windows 11's windows').
	DWMWCP_ROUND = 2
)

var (
	procShellNotifyIconGetRect = shell32.NewProc("Shell_NotifyIconGetRect")
	procGetAsyncKeyState       = user32.NewProc("GetAsyncKeyState")
)

// MouseButtonDown reports whether the mouse's left or right button is
// held, whichever app has the mouse (GetAsyncKeyState: the physical
// buttons).
func MouseButtonDown() bool {
	for _, vk := range [...]uintptr{0x01, 0x02} { // VK_LBUTTON, VK_RBUTTON
		if r, _, _ := procGetAsyncKeyState.Call(vk); int16(r) < 0 {
			return true
		}
	}
	return false
}

// NotifyIconRect is the rectangle of the notification icon wnd added
// with id, in screen pixels; ok is false where Windows doesn't say (the
// icon isn't shown: in the overflow while it's closed, or before Windows
// 7).
func NotifyIconRect(wnd uintptr, id uint32) (r Rect, ok bool) {
	if !Has(procShellNotifyIconGetRect) {
		return Rect{}, false
	}
	ident := struct { // NOTIFYICONIDENTIFIER
		Size uint32
		Wnd  uintptr
		ID   uint32
		GUID GUID
	}{Wnd: wnd, ID: id}
	ident.Size = uint32(unsafe.Sizeof(ident))
	hr, _, _ := procShellNotifyIconGetRect.Call(uintptr(unsafe.Pointer(&ident)), uintptr(unsafe.Pointer(&r)))
	if Failed(hr) || r.W() <= 0 || r.H() <= 0 {
		return Rect{}, false
	}
	return r, true
}

// MonitorOfRect is the work area and DPI of the monitor r (screen
// pixels) is most on, or the nearest one; a rectangle of no size is the
// point at its top left.
func MonitorOfRect(r Rect) (work Rect, dpi int) {
	r.Right, r.Bottom = max(r.Right, r.Left+1), max(r.Bottom, r.Top+1)
	m, _, _ := procMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), MONITOR_DEFAULTTONEAREST)
	mi := MonitorInfo{}
	mi.Size = uint32(unsafe.Sizeof(mi))
	procGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi)))
	return mi.Work, MonitorDPI(m)
}

// OwnedBy reports whether the top-level window w is owner or owned by it,
// directly or through windows owned in turn (a dialog over a window over
// it).
func OwnedBy(w, owner uintptr) bool {
	for i := 0; w != 0 && i < 32; i++ {
		if w == owner {
			return true
		}
		w = Owner(w)
	}
	return false
}
