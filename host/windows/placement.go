//go:build windows && amd64

package windows

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	. "github.com/egoist/ryui/internal/windows"
)

// The main window comes back where the user left it, as macOS's frame
// autosave has it: its normal rectangle (in screen pixels, at the DPI it
// was at) and whether it was maximized, kept in a file in the app's data
// directory (RYUI_DATA: Local AppData\<name>), saved when the user is done
// moving or sizing it, maximizes or restores it, or closes it, not on
// every pixel of a drag. It's restored before the window shows, onto a
// monitor that's there (the nearest, if the one it was on is gone), and
// wins over WindowOptions' size; their least size still holds. The host
// keeps the normal rectangle itself, in screen coordinates
// (GetWindowPlacement's are workspace coordinates, off by the taskbar
// when it's on the top or left).

const placementName = "ryui-window.json"

type placement struct {
	X, Y, Width, Height int32
	DPI                 int
	Maximized           bool
}

// placementFile is where the main window's placement is kept, or "" if
// it isn't: not for a test (go test) or an automation run (RYUI_SNAPSHOT,
// RYUI_SCRIPT), which would leave theirs on the machine.
func placementFile() string {
	if testing.Testing() || snapshotting() || os.Getenv("RYUI_SCRIPT") != "" {
		return ""
	}
	d := os.Getenv("RYUI_DATA")
	if d == "" {
		return ""
	}
	return filepath.Join(d, placementName)
}

// savePlacement keeps the main window's placement, once it has shown,
// if it changed. A minimized window keeps what it had.
func (h *Host) savePlacement() {
	if h.placementPath == "" || !h.placed || IsIconic(h.window) {
		return
	}
	zoomed := IsZoomed(h.window)
	if !zoomed {
		h.normal = WindowRect(h.window)
	}
	r := h.normal
	if r.W() <= 0 || r.H() <= 0 {
		return
	}
	b, _ := json.Marshal(placement{X: r.Left, Y: r.Top, Width: r.W(), Height: r.H(), DPI: h.dpi, Maximized: zoomed})
	if bytes.Equal(b, h.savedPlacement) {
		return
	}
	os.MkdirAll(filepath.Dir(h.placementPath), 0o755)
	if os.WriteFile(h.placementPath, b, 0o644) == nil {
		h.savedPlacement = b
	}
}

// restorePlacement places the main window where it was left, before it
// shows; it reports whether it was maximized.
func (h *Host) restorePlacement() (maximized bool) {
	if h.placementPath == "" {
		return false
	}
	b, err := os.ReadFile(h.placementPath)
	var p placement
	if err != nil || json.Unmarshal(b, &p) != nil || p.Width <= 0 || p.Height <= 0 {
		return false
	}
	h.savedPlacement = b
	r := onScreen(Rect{Left: p.X, Top: p.Y, Right: p.X + p.Width, Bottom: p.Y + p.Height})
	// To its monitor first, so the window takes its DPI, then its size
	// there: in pixels at the DPI it was saved at, scaled if the
	// monitor's is another.
	SetWindowPos(h.window, 0, r.Left, r.Top, 0, 0, SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
	if d := DPI(h.window); d != h.dpi {
		h.dpiChanged(d, WindowRect(h.window))
	}
	if p.DPI > 0 && p.DPI != h.dpi {
		r.Right = r.Left + int32(int64(r.W())*int64(h.dpi)/int64(p.DPI))
		r.Bottom = r.Top + int32(int64(r.H())*int64(h.dpi)/int64(p.DPI))
	}
	var mi MinMaxInfo
	h.minTrackSize(h.window, h.leastSize(), &mi)
	r.Right = r.Left + max(r.W(), mi.MinTrackSize.X)
	r.Bottom = r.Top + max(r.H(), mi.MinTrackSize.Y)
	r = onScreen(r)
	SetWindowPos(h.window, 0, r.Left, r.Top, r.W(), r.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	h.normal = r
	return p.Maximized
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
// restoring the main window, to save its placement when it settles.
func (h *Host) placementMessage(m uint32, wp uintptr) {
	switch m {
	case WM_ENTERSIZEMOVE:
		h.sizing = true
	case WM_EXITSIZEMOVE:
		h.sizing = false
		h.savePlacement()
	case WM_SIZE:
		// Maximized, restored, snapped by the keyboard (Win+arrows):
		// not while a drag goes on.
		if !h.sizing && (wp == sizeRestored || wp == sizeMaximized) {
			h.savePlacement()
		}
	case WM_MOVE:
		if !h.sizing && !IsZoomed(h.window) {
			h.savePlacement()
		}
	}
}

const (
	sizeRestored  = 0
	sizeMaximized = 2
)
