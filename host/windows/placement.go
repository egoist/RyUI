//go:build windows

package windows

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/internal/winstate"
)

// The app's windows come back where the user left them, as macOS's frame
// autosave has it: the main window always (under the key "main", or the
// app's StateKey), a Window with a StateKey. Each one's normal rectangle
// (in screen pixels, at the DPI it was at) and whether it was maximized
// are kept by key (internal/winstate) in a file in the app's data
// directory (ASUKA_DATA: Local AppData\<name>), saved when the user is
// done moving or sizing it, maximizes or restores it, or closes it, not on
// every pixel of a drag. It's restored before the window shows, onto a
// monitor that's there (the nearest, if the one it was on is gone), and
// wins over the options' size and place; their least and most sizes still
// hold. The host keeps the normal rectangle itself, in screen coordinates
// (GetWindowPlacement's are workspace coordinates, off by the taskbar
// when it's on the top or left). A file an older host wrote, the main
// window's alone, is read as the main window's.

const placementName = "asuka-window.json"

type placement struct {
	X, Y, Width, Height int32
	DPI                 int
	Maximized           bool
}

// placementFile is where the windows' placements are kept, or "" if
// they aren't: not for a test (go test) or an automation run
// (ASUKA_SNAPSHOT, ASUKA_SCRIPT), which would leave theirs on the machine.
func placementFile() string {
	if testing.Testing() || snapshotting() || os.Getenv("ASUKA_SCRIPT") != "" {
		return ""
	}
	d := os.Getenv("ASUKA_DATA")
	if d == "" {
		return ""
	}
	return filepath.Join(d, placementName)
}

// store is the placements' file, read once (again if its path changed).
func (h *Host) store() *winstate.Store {
	if h.states == nil || h.statesPath != h.placementPath {
		h.states, h.statesPath = winstate.Open(h.placementPath), h.placementPath
	}
	return h.states
}

// savePlacement keeps a window's placement, once it has shown. A
// minimized window, or one in full screen, keeps what it had.
func (h *Host) savePlacement(c *wctl) {
	if c == nil || c.key == "" || h.placementPath == "" || !c.placed || c.full != nil || !IsWindow(c.hwnd) || IsIconic(c.hwnd) {
		return
	}
	zoomed := IsZoomed(c.hwnd)
	if !zoomed {
		c.normal = WindowRect(c.hwnd)
	}
	r := c.normal
	if r.W() <= 0 || r.H() <= 0 {
		return
	}
	h.store().Save(c.key, placement{X: r.Left, Y: r.Top, Width: r.W(), Height: r.H(), DPI: DPI(c.hwnd), Maximized: zoomed})
}

// restorePlacement places a window where it was left, before it shows;
// it reports whether it was found, and remembers whether it was
// maximized (showCtl maximizes it).
func (h *Host) restorePlacement(c *wctl) bool {
	if c == nil || c.key == "" || h.placementPath == "" {
		return false
	}
	var p placement
	if !h.store().Load(c.key, &p) || p.Width <= 0 || p.Height <= 0 {
		return false
	}
	r := onScreen(Rect{Left: p.X, Top: p.Y, Right: p.X + p.Width, Bottom: p.Y + p.Height})
	// To its monitor first, so the window takes its DPI, then its size
	// there: in pixels at the DPI it was saved at, scaled if the
	// monitor's is another.
	SetWindowPos(c.hwnd, 0, r.Left, r.Top, 0, 0, SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
	dpi := DPI(c.hwnd)
	switch {
	case c.main && dpi != h.dpi:
		h.dpiChanged(dpi, WindowRect(c.hwnd))
	case c.frame != nil && dpiScale(dpi) != c.frame.k:
		h.windowDPIChanged(c.frame, dpi, WindowRect(c.hwnd))
	}
	if p.DPI > 0 && p.DPI != dpi {
		r.Right = r.Left + int32(int64(r.W())*int64(dpi)/int64(p.DPI))
		r.Bottom = r.Top + int32(int64(r.H())*int64(dpi)/int64(p.DPI))
	}
	var mi MinMaxInfo
	h.minMax(c, &mi)
	r.Right = r.Left + max(r.W(), mi.MinTrackSize.X)
	r.Bottom = r.Top + max(r.H(), mi.MinTrackSize.Y)
	if mi.MaxTrackSize.X > 0 {
		r.Right = r.Left + min(r.W(), mi.MaxTrackSize.X)
	}
	if mi.MaxTrackSize.Y > 0 {
		r.Bottom = r.Top + min(r.H(), mi.MaxTrackSize.Y)
	}
	r = onScreen(r)
	SetWindowPos(c.hwnd, 0, r.Left, r.Top, r.W(), r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	c.normal = r
	c.restoreMax = p.Maximized
	return true
}

// onScreen moves a window's rectangle onto the work area of the monitor
// it's most on (or the nearest), unless it's on it already: its top in
// it, and at least half of it.
func onScreen(r Rect) Rect { return clampTo(r, MonitorWorkArea(r)) }

func clampTo(r, work Rect) Rect {
	in := Rect{Left: max(r.Left, work.Left), Top: max(r.Top, work.Top), Right: min(r.Right, work.Right), Bottom: min(r.Bottom, work.Bottom)}
	area := func(r Rect) int64 { return int64(max(r.W(), 0)) * int64(max(r.H(), 0)) }
	if r.Top >= work.Top && r.Top < work.Bottom && 2*area(in) >= area(r) {
		return r
	}
	w, ht := min(r.W(), work.W()), min(r.H(), work.H())
	x := min(max(r.Left, work.Left), work.Right-w)
	y := min(max(r.Top, work.Top), work.Bottom-ht)
	return Rect{Left: x, Top: y, Right: x + w, Bottom: y + ht}
}

// placementMessage follows the user moving, sizing, maximizing and
// restoring a window, to save its placement when it settles.
func (h *Host) placementMessage(c *wctl, m uint32, wp uintptr) {
	switch m {
	case WM_ENTERSIZEMOVE:
		c.sizing = true
	case WM_EXITSIZEMOVE:
		c.sizing = false
		h.savePlacement(c)
	case WM_SIZE:
		// Maximized, restored, snapped by the keyboard (Win+arrows):
		// not while a drag goes on.
		if !c.sizing && (wp == sizeRestored || wp == sizeMaximized) {
			h.savePlacement(c)
		}
	case WM_MOVE:
		if !c.sizing && !IsZoomed(c.hwnd) {
			h.savePlacement(c)
		}
	}
}

const (
	sizeRestored  = 0
	sizeMaximized = 2
)
