// Package login starts the app when the user logs in: a setting a
// desktop app offers ("Open at login"), which the system's own settings
// list too, and where the user can turn it off.
//
//	login.Set(true, func(err error) { ... })
//	login.Enabled(func(on bool, err error) { toggle.Set(on) })
//
// macOS 13 and later: the app is a login item of the user's
// (SMAppService's mainAppService; System Settings › General › Login
// Items), which needs the app's bundle: a build (asuka build), or asuka
// dev's. Windows: a value of the user's Run key
// (HKCU\Software\Microsoft\Windows\CurrentVersion\Run), which Task
// Manager's Startup apps lists; it starts the app unelevated. Linux: an
// XDG autostart entry (~/.config/autostart/<id>.desktop); in a Flatpak,
// the Background portal asks the user and writes it. A phone's app can't
// start at login: there Set fails and Enabled is false.
package login

import (
	"errors"

	"github.com/egoist/asuka/ui"
)

// Set makes the app start as the user logs in (on), or no longer. done,
// if not nil, gets what failed, on the main thread: on macOS, an app
// the user must still allow in System Settings fails with
// ErrNeedsApproval (it's registered, and starts once they do). Main
// thread.
func Set(on bool, done func(err error)) {
	b := byte(0)
	if on {
		b = 1
	}
	ui.Call("login", "set", []byte{b}, func(r []byte, err error) {
		if err == nil && len(r) > 0 && r[0] == 2 {
			err = ErrNeedsApproval
		}
		if done != nil {
			done(err)
		}
	})
}

// ErrNeedsApproval is Set's error on macOS when the app is a login item
// the user hasn't allowed yet (System Settings › General › Login Items).
var ErrNeedsApproval = errors.New("login: allow the app to open at login in System Settings › General › Login Items")

// Enabled calls done, on the main thread, with whether the app starts as
// the user logs in: as Set left it, or as the user changed it in the
// system's settings since. Main thread.
func Enabled(done func(on bool, err error)) {
	ui.Call("login", "enabled", nil, func(r []byte, err error) {
		done(err == nil && len(r) > 0 && r[0] == 1, err)
	})
}

// OpenedAtLogin calls done, on the main thread, with whether this run of
// the app is the one the system started as the user logged in: an app
// may then start out of sight (its window minimized, or only its tray
// icon). macOS says so in the launch's Apple Event (answered once the app
// has finished launching); on Windows and Linux the login entry starts
// the app with an argument of Asuka's, which the host takes out of
// os.Args. Main thread.
func OpenedAtLogin(done func(atLogin bool)) {
	ui.Call("login", "openedAtLogin", nil, func(r []byte, err error) {
		done(err == nil && len(r) > 0 && r[0] == 1)
	})
}
