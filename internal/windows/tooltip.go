//go:build windows

package windows

import "unsafe"

// Tooltips (the common controls' tooltip control): one per app, with a
// tool per window that has a tooltip. It watches each tool's mouse
// itself (TTF_SUBCLASS), and shows the text where the pointer rests,
// themed as the system's.

const (
	ttsAlwaysTip      = 0x01
	ttsNoPrefix       = 0x02
	ttfIDIsHwnd       = 0x0001
	ttfSubclass       = 0x0010
	ttmAddToolW       = 0x0432
	ttmDelToolW       = 0x0433
	ttmSetMaxTipWidth = 0x0418
	tooltipsClass     = "tooltips_class32"
	tooltipMaxWidthPx = 400
)

type toolInfo struct {
	size     uint32
	flags    uint32
	hwnd     uintptr
	id       uintptr
	rect     Rect
	inst     uintptr
	text     *uint16
	lParam   uintptr
	reserved uintptr
}

// NewTooltips makes the tooltip control, owned by owner (the app's main
// window).
func NewTooltips(owner uintptr) uintptr {
	tt := CreateWindow(WS_EX_TOPMOST, tooltipsClass, "", WS_POPUP|ttsAlwaysTip|ttsNoPrefix, 0, 0, 0, 0, owner, 0)
	if tt != 0 {
		SendMessage(tt, ttmSetMaxTipWidth, 0, tooltipMaxWidthPx)
	}
	return tt
}

// SetTooltip gives the window hwnd the tooltip text, replacing the one
// it had; empty: none.
func SetTooltip(tt, hwnd uintptr, text string) {
	if tt == 0 || hwnd == 0 {
		return
	}
	ti := toolInfo{flags: ttfIDIsHwnd | ttfSubclass, hwnd: hwnd, id: hwnd, inst: Instance()}
	ti.size = uint32(unsafe.Sizeof(ti))
	SendMessage(tt, ttmDelToolW, 0, uintptr(unsafe.Pointer(&ti)))
	if text == "" {
		return
	}
	ti.text = UTF16(text) // the control copies it
	SendMessage(tt, ttmAddToolW, 0, uintptr(unsafe.Pointer(&ti)))
}
