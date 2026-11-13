//go:build linux && !android

package linux

import "github.com/egoist/asuka/internal/boot"

// Quitting, as app.OnQuit sees it: app.Quit (boot.Quit), closing the
// app's last window, a signal (SIGTERM, SIGINT, SIGHUP) and the session
// ending (query-end) each ask the core's OnQuit handlers first, once: a
// quit they allowed only finishes. They may cancel it, except as the
// session ends.

// askQuit asks the core's OnQuit handlers whether the app may quit
// (ending: the session is, and they can't refuse), and reports whether
// it goes on. Once allowed, it's allowed. Main thread.
func (h *Host) askQuit(ending bool) bool {
	if h.quitAllowed {
		return true
	}
	cancel := false
	if boot.QuitRequested != nil && h.root != nil {
		h.enter(func() { cancel = boot.QuitRequested(ending) })
	}
	if cancel && !ending {
		return false
	}
	h.quitAllowed = true
	return true
}

// requestQuit quits the app as the user's quit does (app.Quit), unless
// an OnQuit handler cancels it. Main thread.
func (h *Host) requestQuit() {
	if h.askQuit(false) {
		h.quit()
	}
}

// closingLast reports whether closing w would close the app's last
// window: the app quits then, so the user's close asks OnQuit first.
// (An app that keeps running without windows doesn't quit then.)
func (h *Host) closingLast(w *win) bool {
	for _, o := range h.wins {
		if o != w && !o.closing {
			return false
		}
	}
	return true
}
