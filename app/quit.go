package app

import (
	"slices"
	"sync"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Quit quits the app as the user's Quit does (⌘Q on a Mac, closing the
// main window on Windows and Linux): OnQuit's handlers run first, and
// may cancel it; then the app's windows close, it goes to the
// background (asuka/store writes what's pending) and the process ends.
// Safe from any goroutine.
//
// A phone's app doesn't quit itself (the system ends it), nor does a
// page in a browser: there, Quit does nothing.
func Quit() {
	if boot.Quit != nil {
		boot.Quit()
	}
}

// QuitEvent is a quit about to happen, given to OnQuit's handlers.
type QuitEvent struct {
	// SessionEnding: the user is logging out, or the system restarting or
	// shutting down. The quit can't be canceled then, and the process may
	// end as soon as the handlers return: save what must survive there,
	// at once, without a dialog or a goroutine to wait for.
	SessionEnding bool

	canceled bool
}

// Cancel keeps the app running: the user chose to quit with changes
// unsaved, say, and the app asks first (an alert, then Quit again once
// they're saved or the user lets them go). It does nothing while the
// session ends.
func (e *QuitEvent) Cancel() {
	if !e.SessionEnding {
		e.canceled = true
	}
}

// Canceled reports whether a handler canceled the quit.
func (e *QuitEvent) Canceled() bool { return e.canceled }

var quitHandlers struct {
	sync.Mutex
	fs []*func(*QuitEvent)
}

// OnQuit calls f, on the main thread, as the app is about to quit on
// the desktop, however the quit started: the user's Quit (⌘Q, the app
// menu's, the Dock's), closing the main window (Windows, Linux), Quit,
// a signal (SIGTERM, SIGINT; Ctrl+C in the terminal), the session
// ending. Each handler sees the others' Cancel (e.Canceled). A quit
// that goes on then closes the windows and ends the process; the
// Background lifecycle state still comes after, as it always does.
//
// It stops with the component's owner, or when stop is called. Set it
// in main before Run, or in a component.
//
// macOS asks before it quits (applicationShouldTerminate:); Windows as
// the main window closes (WM_CLOSE) and as the session ends
// (WM_QUERYENDSESSION, which can't be refused); Linux as the main window
// closes (close-request), on a signal and as the session ends
// (query-end). A phone's app is ended by the system without a word: keep
// what matters as it goes to the background (OnLifecycle).
func OnQuit(f func(e *QuitEvent)) (stop func()) {
	p := &f
	quitHandlers.Lock()
	quitHandlers.fs = append(quitHandlers.fs, p)
	quitHandlers.Unlock()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			quitHandlers.Lock()
			defer quitHandlers.Unlock()
			for i, h := range quitHandlers.fs {
				if h == p {
					quitHandlers.fs = append(quitHandlers.fs[:i:i], quitHandlers.fs[i+1:]...)
					return
				}
			}
		})
	}
	signal.OnCleanup(stop)
	return stop
}

// quitRequested runs OnQuit's handlers (boot.QuitRequested).
func quitRequested(ending bool) bool {
	quitHandlers.Lock()
	fs := slices.Clone(quitHandlers.fs)
	quitHandlers.Unlock()
	e := &QuitEvent{SessionEnding: ending}
	signal.Batch(func() {
		for _, f := range fs {
			(*f)(e)
		}
	})
	// What the handlers changed (an alert to show) goes to the host.
	boot.AfterMount(func() { ui.Post(func() {}) })
	relaunchAfterQuit(e.canceled, ending)
	return e.canceled
}

var secondHandlers struct {
	sync.Mutex
	fs []*func([]string, string)
}

// OnSecondInstance calls f, on the main thread, when the app is started
// again while it runs, with that launch's arguments (os.Args[1:]) and
// working directory: on the desktop, an app runs once. The app has come
// to the front by then, and the files and links among the arguments go
// on to ui.OnOpenFiles and ui.OnOpenURL as well: f is for the app's own
// flags (a --new-window of its own, say).
//
// An app with an ID (asuka.json's "id", which asuka build sets) runs
// once on Windows (a mutex of the user's session: the second launch
// hands its arguments to the first through WM_COPYDATA and quits) and
// Linux (a unique GApplication: through D-Bus); on a Mac, Launch
// Services opens the app that runs instead of another, and passes no
// arguments (files and links come as Apple Events), so f never runs
// there. A run under asuka dev, or driven by automation, runs as often
// as it's started.
//
// It stops with the component's owner, or when stop is called.
func OnSecondInstance(f func(args []string, dir string)) (stop func()) {
	p := &f
	secondHandlers.Lock()
	secondHandlers.fs = append(secondHandlers.fs, p)
	secondHandlers.Unlock()
	var once sync.Once
	stop = func() {
		once.Do(func() {
			secondHandlers.Lock()
			defer secondHandlers.Unlock()
			for i, h := range secondHandlers.fs {
				if h == p {
					secondHandlers.fs = append(secondHandlers.fs[:i:i], secondHandlers.fs[i+1:]...)
					return
				}
			}
		})
	}
	signal.OnCleanup(stop)
	return stop
}

// secondInstance runs OnSecondInstance's handlers (boot.SecondInstance).
func secondInstance(args []string, dir string) {
	secondHandlers.Lock()
	fs := slices.Clone(secondHandlers.fs)
	secondHandlers.Unlock()
	if len(fs) == 0 {
		return
	}
	ui.Post(func() {
		for _, f := range fs {
			(*f)(append([]string(nil), args...), dir)
		}
	})
}

func init() {
	boot.QuitRequested = quitRequested
	boot.SecondInstance = secondInstance
}
