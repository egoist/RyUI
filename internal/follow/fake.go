package follow

import (
	"errors"
	"sync"
)

// Fake is a host's side of a followed module, for tests on host/fake
// (its AsyncModules): "current" answers the state, "next" answers once
// the state isn't the one given or events are queued, "stop" answers a
// waiting "next" with "stopped". Other methods go to Other.
type Fake struct {
	Other func(method string, args []byte) ([]byte, error)

	mu       sync.Mutex
	state    []byte
	events   []byte
	watching bool
	waiting  []fakeWait
	calls    []string
}

type fakeWait struct {
	last  []byte
	reply func([]byte, error)
}

// NewFake is a Fake whose state is state.
func NewFake(state []byte) *Fake { return &Fake{state: state} }

// Answer is the module's methods: AsyncModules' function.
func (f *Fake) Answer(method string, args []byte, reply func([]byte, error)) {
	f.mu.Lock()
	f.calls = append(f.calls, method)
	switch method {
	case "current":
		s := f.state
		f.mu.Unlock()
		reply(s, nil)
	case "next":
		f.watching = true
		f.waiting = append(f.waiting, fakeWait{last: args, reply: reply})
		f.answer()
	case "stop":
		ws := f.waiting
		f.waiting, f.watching, f.events = nil, false, nil
		f.mu.Unlock()
		for _, w := range ws {
			w.reply(nil, errors.New("stopped"))
		}
		reply(nil, nil)
	default:
		f.mu.Unlock()
		if f.Other == nil {
			reply(nil, errors.New("no method "+method))
			return
		}
		reply(f.Other(method, args))
	}
}

// answer answers the waiting "next"s that have something new, and
// unlocks f.
func (f *Fake) answer() {
	var answered []func([]byte, error)
	var keep []fakeWait
	out := append(append([]byte(nil), f.state...), f.events...)
	for _, w := range f.waiting {
		if len(f.events) > 0 || string(w.last) != string(f.state) {
			answered = append(answered, w.reply)
		} else {
			keep = append(keep, w)
		}
	}
	f.waiting = keep
	if len(answered) > 0 {
		f.events = nil
	}
	f.mu.Unlock()
	for _, r := range answered {
		r(out, nil)
	}
}

// Set changes the state, answering a waiting "next".
func (f *Fake) Set(state []byte) {
	f.mu.Lock()
	f.state = state
	f.answer()
}

// Emit queues events while the module is watched, answering a waiting
// "next".
func (f *Fake) Emit(events ...byte) {
	f.mu.Lock()
	if f.watching {
		f.events = append(f.events, events...)
	}
	f.answer()
}

// Calls are the methods called so far.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// Waiting is how many "next"s wait.
func (f *Fake) Waiting() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.waiting)
}
