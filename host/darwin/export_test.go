//go:build darwin && !ios

package darwin

import (
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// What tests in package darwin_test use: those that import packages
// importing package app, which imports this one (asuka/sdk/document).

// OnMain runs f on the main thread (onMain); Fatal and Fatalf fail the
// test from there.
var (
	OnMain = onMain
	Fatal  = fatal
	Fatalf = fatalf
)

// Mount mounts root on a new host (mount).
func Mount(t *testing.T, root func() ui.Node) *Host { h, _ := mount(t, root); return h }

// RunPosted runs what was posted to the main thread.
func (h *Host) RunPosted() { h.runPosted() }

// Do runs f in the core, as an event's handler would, and flushes.
func (h *Host) Do(f func()) { post(h, f) }

// MainWindow is the main window's NSWindow.
func (h *Host) MainWindow() ID { return h.window }

// Windows are the Windows' NSWindows.
func (h *Host) Windows() []ID {
	var out []ID
	for w := range h.wins {
		out = append(out, w)
	}
	return out
}

// OpenFile opens a file as Finder does (application:openURLs:).
func (h *Host) OpenFile(path string) { h.enter(func() { h.openURLs(Array(FileURL(path))) }) }

// Reopen is the Dock icon clicked with no window showing.
func (h *Host) Reopen() {
	SendBool(h.target, "applicationShouldHandleReopen:hasVisibleWindows:", uintptr(Send(Class("NSApplication"), "sharedApplication")), 0)
}

// RecentMenu is the host's Open Recent submenu.
func RecentMenu() ID { return recentState.menu }

// Snapshot writes what the window w shows, its title bar too, as a PNG.
func Snapshot(w ID, path string) { writeViewPNG(Send(Send(w, "contentView"), "superview"), path) }
