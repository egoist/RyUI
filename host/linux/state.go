//go:build linux && !android

package linux

import (
	"os"
	"path/filepath"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/winstate"
)

// Windows come back as the user left them: their size and whether they
// were maximized, in asuka-window.json in the app's data directory, as
// GNOME's apps keep theirs, by the key the app gives each (the main
// window's "main" unless it names another; a Window's only with a
// StateKey). Not their place: the compositor places windows (Wayland),
// and GTK 4 has no way to on X11 either. A state is saved as its window
// closes (the user closing it, or the core closing a Window) and as the
// app quits, and restored before the window shows, over the size the app
// asks for. Tests and automation runs keep none.

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

// stateStore is the windows' states, read once.
func (h *Host) stateStore() *winstate.Store {
	if h.states == nil {
		h.states = winstate.Open(windowStatePath())
	}
	return h.states
}

// restoreWindow sizes w as the user left it under its key, and maximizes
// it if it was; it reports whether there was a state.
func (h *Host) restoreWindow(w *win) bool {
	if w.stateKey == "" {
		return false
	}
	var s windowState
	if !h.stateStore().Load(w.stateKey, &s) || s.Width < 100 || s.Height < 100 {
		return false
	}
	WindowSetDefaultSize(w.window, s.Width, s.Height)
	if s.Maximized {
		WindowMaximize(w.window)
	}
	w.restored = true
	return true
}

// saveWindow keeps w's size (its unmaximized one: GTK's default size,
// which follows the user's resizing) and whether it's maximized, under
// its key.
func (h *Host) saveWindow(w *win) {
	if w.stateKey == "" || w.window == 0 {
		return
	}
	var s windowState
	WindowGetDefaultSize(w.window, &s.Width, &s.Height)
	if s.Width <= 0 || s.Height <= 0 {
		return
	}
	s.Maximized = WindowIsMaximized(w.window)
	h.stateStore().Save(w.stateKey, s)
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
