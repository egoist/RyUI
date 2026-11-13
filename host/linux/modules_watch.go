//go:build linux && !android

package linux

import "errors"

// watched is a module's state that Go follows (internal/follow):
// "current" answers it (once it's known), "next" answers it once it isn't
// the state given or events are queued (from the first "next" until
// "stop", so none is lost between two), "stop" answers the waiting ones
// "stopped". The displays, power and appearance modules are its.
type watched struct {
	state    []byte // nil until known
	events   []byte
	watching bool
	current  []func([]byte, error)
	next     []netWait
}

// set makes s the state, answering who waits.
func (w *watched) set(s []byte) {
	w.state = s
	cs := w.current
	w.current = nil
	for _, c := range cs {
		c(s, nil)
	}
	w.answer()
}

// emit queues an event while Go watches.
func (w *watched) emit(e byte) {
	if !w.watching {
		return
	}
	w.events = append(w.events, e)
	w.answer()
}

// answer answers the waiting "next"s that have something new.
func (w *watched) answer() {
	if w.state == nil {
		return
	}
	out := append(append([]byte(nil), w.state...), w.events...)
	var keep []netWait
	answered := false
	for _, n := range w.next {
		if len(w.events) > 0 || string(n.last) != string(w.state) {
			n.reply(out, nil)
			answered = true
		} else {
			keep = append(keep, n)
		}
	}
	w.next = keep
	if answered {
		w.events = nil
	}
}

// methods are "current", "next" and "stop"; start starts following the
// state, once (it's called each time).
func (w *watched) methods(start func() error) map[string]method {
	return map[string]method{
		"current": func(_ []byte, reply func([]byte, error)) {
			if err := start(); err != nil {
				reply(nil, err)
				return
			}
			if w.state != nil {
				reply(w.state, nil)
				return
			}
			w.current = append(w.current, reply)
		},
		"next": func(args []byte, reply func([]byte, error)) {
			if err := start(); err != nil {
				reply(nil, err)
				return
			}
			w.watching = true
			w.next = append(w.next, netWait{last: append([]byte(nil), args...), reply: reply})
			w.answer()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			ws := w.next
			w.next, w.events, w.watching = nil, nil, false
			for _, n := range ws {
				n.reply(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}
