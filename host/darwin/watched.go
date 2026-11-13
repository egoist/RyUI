//go:build darwin && !ios

package darwin

import (
	"errors"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
)

// watched is a module's state that Go follows (internal/follow):
// "current" answers it, "next" answers once it isn't the state given
// (or with the events queued since), "stop" answers the waiting "next"s
// with "stopped". The state is read again as what it's read from
// changes; events (asuka/power's) are queued from the first "next" until
// "stop", so none is lost between two "next"s.
type watched struct {
	read    func() []byte
	start   func(w *watched) // observes what changes it, once
	started bool
	state   []byte
	events  []byte
	queuing bool
	waiting []watchWait
}

type watchWait struct {
	last  []byte
	reply func([]byte, error)
}

func (w *watched) begin() {
	if !w.started {
		w.started = true
		if w.start != nil {
			w.start(w)
		}
		w.state = w.read()
	}
}

// changed reads the state again and answers who waits for a change.
func (w *watched) changed() {
	if !w.started {
		return
	}
	w.state = w.read()
	w.answer()
}

// event queues an event while Go watches, and answers.
func (w *watched) event(e byte) {
	if !w.queuing {
		return
	}
	w.events = append(w.events, e)
	w.answer()
}

func (w *watched) answer() {
	if len(w.waiting) == 0 {
		return
	}
	out := append(append([]byte(nil), w.state...), w.events...)
	var keep []watchWait
	var ready []func([]byte, error)
	for _, x := range w.waiting {
		if len(w.events) > 0 || string(x.last) != string(w.state) {
			ready = append(ready, x.reply)
		} else {
			keep = append(keep, x)
		}
	}
	w.waiting = keep
	if len(ready) > 0 {
		w.events = nil
	}
	for _, r := range ready {
		r(out, nil)
	}
}

func (w *watched) methods() map[string]method {
	return map[string]method{
		"current": func(_ []byte, reply func([]byte, error)) {
			w.begin()
			w.state = w.read()
			reply(w.state, nil)
		},
		"next": func(args []byte, reply func([]byte, error)) {
			// Read again: a change no notification told of (the Dock resizing
			// the work area) is seen at the next "next".
			w.begin()
			w.state = w.read()
			w.queuing = true
			w.waiting = append(w.waiting, watchWait{last: append([]byte(nil), args...), reply: reply})
			w.answer()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			ws := w.waiting
			w.waiting, w.events, w.queuing = nil, nil, false
			for _, x := range ws {
				x.reply(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// observeNamed calls f, on the main thread, at each notification named
// name from center (0: the default center); it's kept for the process.
func observeNamed(center ID, name string, f func()) {
	if center == 0 {
		center = Send(Class("NSNotificationCenter"), "defaultCenter")
	}
	b := NewBlock(func(_ objc.Block, _ ID) { f() })
	Retain(Send(center, "addObserverForName:object:queue:usingBlock:",
		uintptr(String(name)), 0, uintptr(Send(Class("NSOperationQueue"), "mainQueue")), uintptr(b)))
}

// workspaceCenter is NSWorkspace's notification center (sleep, wake,
// accessibility display options).
func workspaceCenter() ID {
	return Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "notificationCenter")
}
