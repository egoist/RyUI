// The segmented control on Windows: a tab control drawn as buttons
// (TCS_BUTTONS), through syscall.

package segmented

import (
	"syscall"
	"unsafe"
)

func init() {
	implementSegmented(newSegmented)
}

var (
	user32         = syscall.NewLazyDLL("user32.dll")
	gdi32          = syscall.NewLazyDLL("gdi32.dll")
	createWindowEx = user32.NewProc("CreateWindowExW")
	sendMessage    = user32.NewProc("SendMessageW")
	getDpiForWin   = user32.NewProc("GetDpiForWindow")
	createFont     = gdi32.NewProc("CreateFontW")
	deleteObject   = gdi32.NewProc("DeleteObject")
)

const (
	wsChild           = 0x40000000
	wsVisible         = 0x10000000
	tcsButtons        = 0x0100 // tabs drawn as buttons
	tcsFixedWidth     = 0x0400
	wmSetFont         = 0x0030
	wmNotify          = 0x004E
	tcmFirst          = 0x1300
	tcmGetItemCount   = tcmFirst + 4
	tcmDeleteAllItems = tcmFirst + 9
	tcmGetItemRect    = tcmFirst + 10
	tcmGetCurSel      = tcmFirst + 11
	tcmSetCurSel      = tcmFirst + 12
	tcmInsertItem     = tcmFirst + 62 // TCM_INSERTITEMW
	tcifText          = 1
	tcnSelChange      = 0xFFFFFDD9 // TCN_SELCHANGE: TCN_FIRST (-550) - 1
)

// tcItem is TCITEMW.
type tcItem struct {
	mask, state, stateMask uint32
	text                   *uint16
	textMax, image         int32
	param                  uintptr
}

// nmhdr is NMHDR, which WM_NOTIFY's lParam points to.
type nmhdr struct {
	from, id uintptr
	code     uint32
}

type rect struct{ left, top, right, bottom int32 }

// segmented is a tab control whose tabs are buttons, the segmented
// control Win32 has: props "items" (comma-separated) and "selected";
// event "changed" with "index".
type segmented struct {
	events   SegmentedEvents
	hwnd     uintptr
	font     uintptr
	selected int
}

func newSegmented(events SegmentedEvents) SegmentedNative {
	class, _ := syscall.UTF16PtrFromString("SysTabControl32")
	// A child window is made in its parent: the context's.
	hwnd, _, _ := createWindowEx.Call(0, uintptr(unsafe.Pointer(class)), 0,
		wsChild|wsVisible|tcsButtons|tcsFixedWidth, 0, 0, 0, 0, events.Context.Parent(), 0, 0, 0)
	// Controls start in the old system font: give it Windows 11's, at the
	// window's DPI.
	face, _ := syscall.UTF16PtrFromString("Segoe UI")
	font, _, _ := createFont.Call(uintptr(-int32(9*dpi(hwnd)/72)), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
	sendMessage.Call(hwnd, wmSetFont, font, 0)
	return &segmented{events: events, hwnd: hwnd, font: font}
}

func dpi(hwnd uintptr) int32 {
	if d, _, _ := getDpiForWin.Call(hwnd); d != 0 {
		return int32(d)
	}
	return 96
}

func (s *segmented) Handle() uintptr { return s.hwnd }

func (s *segmented) SetItems(items []string) {
	sendMessage.Call(s.hwnd, tcmDeleteAllItems, 0, 0)
	for i, t := range items {
		text, _ := syscall.UTF16PtrFromString(t)
		item := tcItem{mask: tcifText, text: text}
		sendMessage.Call(s.hwnd, tcmInsertItem, uintptr(i), uintptr(unsafe.Pointer(&item)))
	}
	sendMessage.Call(s.hwnd, tcmSetCurSel, uintptr(s.selected), 0)
}

func (s *segmented) SetSelected(selected int) {
	s.selected = selected
	sendMessage.Call(s.hwnd, tcmSetCurSel, uintptr(s.selected), 0)
}

func (s *segmented) Measure(w, h float32) (float32, float32) {
	n, _, _ := sendMessage.Call(s.hwnd, tcmGetItemCount, 0, 0)
	if n == 0 {
		return 0, 0
	}
	var r rect
	sendMessage.Call(s.hwnd, tcmGetItemRect, n-1, uintptr(unsafe.Pointer(&r)))
	scale := float32(dpi(s.hwnd)) / 96
	return float32(r.right) / scale, float32(r.bottom) / scale
}

// ParentMessage hears what the control tells its parent: WM_NOTIFY's
// TCN_SELCHANGE as the user picks a button.
func (s *segmented) ParentMessage(msg uint32, wparam, lparam uintptr) (uintptr, bool) {
	if msg != wmNotify {
		return 0, false
	}
	// lParam is the address of an NMHDR, memory Go doesn't manage.
	hdr := *(**nmhdr)(unsafe.Pointer(&lparam))
	if hdr.from != s.hwnd || hdr.code != tcnSelChange {
		return 0, false
	}
	i, _, _ := sendMessage.Call(s.hwnd, tcmGetCurSel, 0, 0)
	s.selected = int(int32(i))
	s.events.Changed(s.selected)
	return 0, true
}

// Destroy frees the font; the host destroys the window.
func (s *segmented) Destroy() { deleteObject.Call(s.font) }
