//go:build linux && !android

package linux

import (
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ui"
)

// What tests in package linux_test use: those that import packages
// importing package app, which imports this one (asuka/sdk/document).

// OnMain runs f on the main thread; Fatalf fails the test from there.
var (
	OnMain = onMain
	Fatalf = fatalf
)

// Mount mounts root on a new host (mount).
func Mount(t *testing.T, root func() ui.Node) *Host { h, _ := mount(t, root); return h }

// Settle runs the main loop until cond holds (settle).
func Settle(t *testing.T, h *Host, cond func() bool) { settle(t, h, cond) }

// WindowInfo is what a Window's GtkWindow shows: its title, its title
// bar's subtitle, and whether its header bar has the app's main menu.
type WindowInfo struct {
	Title, Subtitle string
	Menu            bool
}

// Windows are the Windows' (not the main window's).
func (h *Host) Windows() []WindowInfo {
	var out []WindowInfo
	for _, w := range h.wins {
		if w == h.main {
			continue
		}
		wi := WindowInfo{
			Title:    GoString(Ptr(Call(Sym("gtk_window_get_title"), w.window))),
			Subtitle: GoString(Ptr(Call(Sym("adw_window_title_get_subtitle"), w.title))),
		}
		wi.Menu = w.chrome.menus != 0 && h.appMenu != 0
		out = append(out, wi)
	}
	return out
}

// MainVisible reports whether the main window shows.
func (h *Host) MainVisible() bool { return WidgetGetVisible(h.main.window) }

// OpenFiles opens files as the desktop does (GApplication's open).
func (h *Host) OpenFiles(lines string) { h.enter(func() { h.incomingURL(lines) }) }

// ChooseMenuItem chooses the app menu's item titled title.
func (h *Host) ChooseMenuItem(title string) error { return h.activateAppItem(title) }

// CloseWindows closes the Windows as their close buttons do.
func (h *Host) CloseWindows() {
	for _, w := range h.wins {
		if w != h.main {
			WindowClose(w.window)
		}
	}
}
