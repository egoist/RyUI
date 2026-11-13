//go:build windows

package windows

import (
	"errors"

	. "github.com/egoist/asuka/internal/windows"
)

// What the desktop modules (displays.go, power.go, appearance.go) share:
// a window that hears what Windows broadcasts, and a state the Go side
// follows (package internal/follow's protocol).

// The system window is a top-level window of the modules' class, never
// shown: Windows broadcasts WM_DISPLAYCHANGE, WM_SETTINGCHANGE,
// WM_POWERBROADCAST and WM_SYSCOLORCHANGE to top-level windows only (a
// message-only window hears none), and the power and session
// notifications it registers for come to it. Each module adds its
// handler (onSystemMessage).
var systemWin struct {
	hwnd     uintptr
	handlers []func(m uint32, wp, lp uintptr)
}

// systemWindow makes the system window, once.
func systemWindow() uintptr {
	s := &systemWin
	if s.hwnd == 0 {
		s.hwnd = ownWindow(WS_EX_TOOLWINDOW, "", WS_POPUP, 0, 0, 0, 0, 0, func(_ uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
			for _, f := range s.handlers {
				f(m, wp, lp)
			}
			return 0, false
		})
		WatchPower(s.hwnd)
	}
	return s.hwnd
}

// onSystemMessage calls f with each message the system window gets.
func onSystemMessage(f func(m uint32, wp, lp uintptr)) {
	systemWindow()
	systemWin.handlers = append(systemWin.handlers, f)
}

// watched is a state the Go side follows: "current" answers it, "next"
// (args: the state last answered) answers once it's another or events
// came, "stop" answers a waiting "next" with "stopped". Events (power's)
// are queued from the first "next" to "stop", so none is lost between
// two; an answer is the state, then the events.
type watched struct {
	read     func() []byte // the state now
	start    func()        // starts listening, once (nil: nothing to start)
	started  bool
	state    []byte
	events   []byte
	watching bool
	waiting  []watchedWait
}

type watchedWait struct {
	last  []byte
	reply func([]byte, error)
}

func (w *watched) begin() {
	if !w.started {
		w.started = true
		if w.start != nil {
			w.start()
		}
		w.state = w.read()
	}
}

// changed reads the state again and answers who waits for something new.
func (w *watched) changed() {
	if !w.started {
		return
	}
	w.state = w.read()
	w.answer()
}

// emit queues an event while the Go side follows.
func (w *watched) emit(e byte) {
	if !w.watching {
		return
	}
	w.events = append(w.events, e)
	w.answer()
}

func (w *watched) answer() {
	out := append(append([]byte(nil), w.state...), w.events...)
	var keep []watchedWait
	answered := false
	for _, x := range w.waiting {
		if len(w.events) > 0 || string(x.last) != string(w.state) {
			x.reply(out, nil)
			answered = true
		} else {
			keep = append(keep, x)
		}
	}
	w.waiting = keep
	if answered {
		w.events = nil
	}
}

func (w *watched) methods() map[string]asyncMethod {
	return map[string]asyncMethod{
		"current": func(_ []byte, reply func([]byte, error)) {
			w.begin()
			w.state = w.read()
			reply(w.state, nil)
		},
		"next": func(args []byte, reply func([]byte, error)) {
			// Read again: a change no message told of is seen at the
			// next "next".
			w.begin()
			w.state = w.read()
			w.watching = true
			w.waiting = append(w.waiting, watchedWait{last: append([]byte(nil), args...), reply: reply})
			w.answer()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			ws := w.waiting
			w.waiting, w.watching, w.events = nil, false, nil
			for _, x := range ws {
				x.reply(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}
