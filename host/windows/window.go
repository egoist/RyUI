//go:build windows && amd64

package windows

import (
	"strings"
	"syscall"

	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

const (
	classWindow  = "NuiWindow"  // the app's window, sheets
	classView    = "NuiView"    // the views the host draws
	classMessage = "NuiMessage" // Post's wake-ups

	wmRunPosted = WM_APP + 1
	wmFrame     = WM_APP + 2

	timerLongPress = 1
	timerSpin      = 2
	timerAnimate   = 3
	timerFrame     = 4
)

var wndProcCallback uintptr

// registerClasses registers the window classes, with one window
// procedure for all of them, made once: it routes by window.
func registerClasses() {
	if wndProcCallback != 0 {
		return
	}
	wndProcCallback = syscall.NewCallback(wndProc)
	icon := AppIcon()
	RegisterClass(classWindow, CS_HREDRAW|CS_VREDRAW, wndProcCallback, icon)
	RegisterClass(classView, CS_DBLCLKS, wndProcCallback, 0)
	RegisterClass(classMessage, 0, wndProcCallback, 0)
}

func wndProc(hwnd, m, wp, lp uintptr) uintptr {
	if h := theHost; h != nil {
		if r, ok := h.message(hwnd, uint32(m), wp, lp); ok {
			return r
		}
	}
	return DefWindowProc(hwnd, uint32(m), wp, lp)
}

func (h *Host) message(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	// A menu item chosen, or its accelerator, in one of the app's windows.
	if m == WM_COMMAND && lp == 0 && HiWord(wp) <= 1 && h.keyWindow(hwnd) == hwnd && h.menuCommand(int(LoWord(wp))) {
		return 0, true
	}
	switch hwnd {
	case h.msgWin:
		switch m {
		case wmRunPosted:
			h.runPosted()
			return 0, true
		case WM_TIMER:
			if wp == timerFrame {
				h.tick()
			} else if wp == timerAnimate && h.anim != nil {
				h.anim.step(h)
			}
			return 0, true
		}
		return 0, false
	case h.window:
		return h.windowMessage(m, wp, lp)
	}
	n := h.byHwnd[hwnd]
	if n == nil {
		return 0, false
	}
	if r, ok := h.parentMessage(m, wp, lp); ok {
		return r, true
	}
	switch n.kind {
	case kindSheetWindow:
		return h.sheetMessage(n, m, wp, lp)
	case kindWindowFrame:
		return h.windowFrameMessage(n, m, wp, lp)
	}
	return h.viewMessage(n, hwnd, m, wp, lp)
}

// parentMessage handles what a control tells its parent (every
// control's parent is a window of the host's): commands, notifications,
// colors.
func (h *Host) parentMessage(m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case WM_COMMAND:
		if lp != 0 {
			if c := h.byHwnd[lp]; c != nil {
				h.controlCommand(c, lp, uint32(HiWord(wp)))
				return 0, true
			}
		}
	case WM_NOTIFY:
		hdr := Native[NMHdr](lp)
		if c := h.byHwnd[hdr.HwndFrom]; c != nil {
			return h.notify(c, hdr, lp)
		}
	case WM_HSCROLL, WM_VSCROLL:
		if lp != 0 {
			if c := h.byHwnd[lp]; c != nil && c.kind == ops.KindSlider {
				h.sliderMoved(c)
				return 0, true
			}
		}
	case WM_CTLCOLOREDIT, WM_CTLCOLORSTATIC, WM_CTLCOLORBTN, WM_CTLCOLORLISTBOX:
		if c := h.byHwnd[lp]; c != nil {
			return h.controlColors(c, wp, m), true
		}
	}
	return 0, false
}

// makeWindow makes the app's window, 480 by 760 points, centered in the
// work area.
func (h *Host) makeWindow() {
	style := uint32(WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN)
	h.window = CreateWindow(0, classWindow, h.appName, style, 0, 0, 100, 100, 0, 0)
	h.dpi = DPI(h.window)
	h.scale = float32(h.dpi) / 96
	SetDarkTitleBar(h.window, h.dark)
	r := Rect{Right: h.px(480), Bottom: h.px(760)}
	AdjustWindowRect(&r, style, 0, h.dpi)
	work := WorkArea(h.window)
	w, ht := r.W(), min(r.H(), work.H())
	x := work.Left + (work.W()-w)/2
	y := work.Top + (work.H()-ht)/2
	SetWindowPos(h.window, 0, x, y, w, ht, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
}

func (h *Host) windowMessage(m uint32, wp, lp uintptr) (uintptr, bool) {
	if r, ok := h.menuBarMessage(h.window, m, wp, lp); ok {
		return r, true
	}
	switch m {
	case WM_SIZE:
		if h.root != nil && h.root.hwnd != 0 {
			r := ClientRect(h.window)
			SetWindowPos(h.root.hwnd, 0, 0, 0, r.W(), r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		}
		h.environmentChanged()
		return 0, true
	case WM_ERASEBKGND:
		r := ClientRect(h.window)
		FillColor(wp, r, h.theme.bg.ref())
		return 1, true
	case WM_GETMINMAXINFO:
		// WindowOptions' least, or 320 points square.
		least := h.minSize
		if least[0] <= 0 {
			least[0] = 320
		}
		if least[1] <= 0 {
			least[1] = 320
		}
		h.minTrackSize(h.window, least, Native[MinMaxInfo](lp))
		return 0, true
	case WM_DPICHANGED:
		h.dpiChanged(int(LoWord(wp)), *Native[Rect](lp))
		return 0, true
	case WM_SETTINGCHANGE:
		h.settingChanged(GoStr(lp))
		return 0, false
	case WM_THEMECHANGED:
		h.settingChanged("ImmersiveColorSet")
		return 0, false
	case WM_ACTIVATE:
		if LoWord(wp) == 0 {
			h.lifecycle(ops.LifecycleBackground)
		} else {
			h.inUse = h.window
			h.lifecycle(ops.LifecycleForeground)
		}
		return 0, false
	case WM_CLOSE:
		h.quitting = true
		DestroyWindow(h.window)
		return 0, true
	case WM_DESTROY:
		PostQuitMessage(0)
		return 0, true
	}
	if h.root != nil {
		if r, ok := h.parentMessage(m, wp, lp); ok {
			return r, true
		}
	}
	return 0, false
}

// environment is the window's: its client area's size, its DPI as the
// scale, the text size, dark mode, animations (Reduce Motion), the
// locale and time zone. A desktop window has no safe area or keyboard
// over it.
func (h *Host) environment() ops.Environment {
	r := ClientRect(h.window)
	return ops.Environment{
		Width: h.pt(r.W()), Height: h.pt(r.H()),
		Scale:        h.scale,
		FontScale:    TextScale(),
		Dark:         h.dark,
		ReduceMotion: SystemParam(SPI_GETCLIENTAREAANIMATION) == 0,
		Platform:     ops.PlatformWindows,
		Locale:       strings.ReplaceAll(LocaleName(), "-", "_"),
		TimeZone:     timeZone(),
	}
}

var tzName string

func timeZone() string {
	if tzName == "" {
		tzName = TimeZone()
	}
	return tzName
}

// environmentChanged reports a new environment to the root, and to the
// screens and sheets the host sizes.
func (h *Host) environmentChanged() {
	if h.window == 0 {
		return
	}
	env := h.environment()
	if env == h.env {
		h.layoutNav()
		return
	}
	if env.FontScale != h.env.FontScale {
		h.clearFonts()
		h.applyFonts()
	}
	h.env = env
	if h.root != nil {
		h.coreEnv = env
		h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventEnvironment, Env: env})
	}

	h.layoutNav()
}

// dpiChanged follows the window to a monitor of another DPI: new fonts,
// every window placed again in pixels, the suggested window size.
func (h *Host) dpiChanged(dpi int, suggested Rect) {
	h.dpi = dpi
	h.scale = float32(dpi) / 96
	h.clearFonts()
	h.applyFonts()
	SetWindowPos(h.window, 0, suggested.Left, suggested.Top, suggested.W(), suggested.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	for _, n := range h.nodes {
		if n.hwnd != 0 && n != h.root && n.cell == nil && (n.nav == nil || !n.nav.sizedByHost(n)) {
			h.placeFrame(n)
		}
	}
	for _, n := range h.nodes {
		if n.list != nil {
			n.list.rebind = true
			n.list.queue(h)
		}
	}
	h.environmentChanged()
	RedrawWindow(h.window, RDW_INVALIDATE|RDW_ALLCHILDREN|RDW_ERASE)
}

// settingChanged follows the system's settings: dark mode and the
// accent (ImmersiveColorSet), the locale (intl), the text size.
func (h *Host) settingChanged(what string) {
	switch what {
	case "intl":
		tzName = ""
	case "ImmersiveColorSet", "":
		dark := !AppsUseLightTheme()
		if scheme != ops.ColorSchemeSystem {
			dark = scheme == ops.ColorSchemeDark
		}
		if dark != h.dark || what == "ImmersiveColorSet" {
			h.setDark(dark)
		}
	}
	h.environmentChanged()
}

// setDark redraws the app light or dark.
func (h *Host) setDark(dark bool) {
	h.dark = dark
	SetAppDark(dark)
	h.theme = newPalette(dark, h.accent)
	for k, b := range h.brushes {
		DeleteObject(b)
		delete(h.brushes, k)
	}
	SetDarkTitleBar(h.window, dark)
	for _, s := range h.sheets {
		if s.nav != nil && s.nav.window != 0 {
			SetDarkTitleBar(s.nav.window, dark)
		}
	}
	for _, w := range h.wins {
		SetDarkTitleBar(w.nav.window, dark)
	}
	for _, n := range h.nodes {
		h.themeControl(n)
		if n.web != nil {
			n.web.colorScheme(dark)
		}
	}
	RedrawWindow(h.window, RDW_INVALIDATE|RDW_ALLCHILDREN|RDW_ERASE|RDW_FRAME)
	DrawMenuBar(h.window)
	for _, s := range h.sheets {
		if s.nav != nil && s.nav.window != 0 {
			RedrawWindow(s.nav.window, RDW_INVALIDATE|RDW_ALLCHILDREN|RDW_ERASE|RDW_FRAME)
		}
	}
	for _, w := range h.wins {
		RedrawWindow(w.nav.window, RDW_INVALIDATE|RDW_ALLCHILDREN|RDW_ERASE|RDW_FRAME)
	}
}

// applyFonts gives the controls that draw their own text their fonts
// again.
func (h *Host) applyFonts() {
	for _, n := range h.nodes {
		h.applyFont(n)
	}
}

func (h *Host) lifecycle(s ops.Lifecycle) {
	if h.root == nil {
		return
	}
	h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventLifecycle, Int: int64(s)})
}

// keyMessage handles the app's keys before the dialog navigation does:
// Escape dismisses a sheet, Alt+Left goes back.
func (h *Host) keyMessage(m *Msg) bool {
	if m.Message != WM_KEYDOWN && m.Message != WM_SYSKEYDOWN {
		return false
	}
	switch m.WParam {
	case VK_ESCAPE:
		if w := RootWindow(m.Hwnd); w != h.window {
			if n := h.byHwnd[w]; n != nil && n.kind == kindSheetWindow {
				h.cancelSheet(n)
				return true
			}
		}
	case VK_LEFT:
		if m.Message == WM_SYSKEYDOWN {
			if r := h.rootOf(RootWindow(m.Hwnd)); r != nil {
				h.back(r)
				return true
			}
		}
	}
	return false
}
