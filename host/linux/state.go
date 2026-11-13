//go:build linux && !android

package linux

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/gtk"
)

// The main window comes back as the user left it: its size and whether
// it was maximized (a Wayland app can't place its window; the compositor
// does), in asuka-window.json in the app's data directory, as GNOME's apps
// keep theirs. Tests and automation runs keep none.

type windowState struct {
	Width     int32 `json:"width"`
	Height    int32 `json:"height"`
	Maximized bool  `json:"maximized"`
}

func windowStatePath() string {
	if os.Getenv("ASUKA_SNAPSHOT") != "" || os.Getenv("ASUKA_SCRIPT") != "" || os.Getenv("ASUKA_TEST") != "" {
		return ""
	}
	return filepath.Join(dataDir(), "asuka-window.json")
}

func (h *Host) restoreWindow(w *win) {
	p := windowStatePath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var s windowState
	if json.Unmarshal(b, &s) != nil || s.Width < 100 || s.Height < 100 {
		return
	}
	WindowSetDefaultSize(w.window, s.Width, s.Height)
	if s.Maximized {
		WindowMaximize(w.window)
	}
	w.restored = true
}

func (h *Host) saveWindow(w *win) {
	p := windowStatePath()
	if p == "" {
		return
	}
	var s windowState
	WindowGetDefaultSize(w.window, &s.Width, &s.Height)
	s.Maximized = WindowIsMaximized(w.window)
	b, _ := json.Marshal(s)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, b, 0o644)
}

// init points the app's data at the XDG data directory (Flatpak's own),
// in a folder named for the app's ID: before the app's packages read it
// (store.Signal in a package variable).
func init() {
	if os.Getenv("ASUKA_DATA") != "" {
		return
	}
	name := boot.ID
	if name == "" {
		name = filepath.Base(os.Args[0])
	}
	os.Setenv("ASUKA_DATA", filepath.Join(xdgDataHome(), name))
}
