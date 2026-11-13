// Package follow follows a host module's state for the packages that
// watch one (asuka/sdk/screen's displays, asuka/sdk/power, asuka/sdk/appearance): a
// goroutine asks the module's "next" with the state it last got, which
// the host answers once its state isn't that one (at once for none), and
// hands each answer to the watchers, on the main thread, until the last
// one stops. "stop" answers a waiting "next" with "stopped".
//
// A module that follows this way has three methods: "current" (the
// state now), "next" (args: the last state; answered with the state once
// it differs, or with events the host queued since) and "stop".
package follow

import (
	"sync"

	"github.com/egoist/asuka/ui"
)

// Module follows one module. Its zero value with Name set is ready.
type Module struct {
	Name string
	// StateLen is how much of an answer is the state passed back to
	// "next" (what follows it is events); 0: all of it.
	StateLen int

	mu       sync.Mutex
	watchers map[*func([]byte)]bool
	running  bool
	// gen is the goroutine that follows: one that wakes to a gen moved
	// on (the last watcher stopped, perhaps another began) ends without
	// touching the module, so a new watch never waits on a goroutine
	// whose "next" the stop answers later, or never (a root gone).
	gen int
}

// Watch calls f, on the main thread, with each answer of "next": the
// state now first, then each change, until stop. Safe from any
// goroutine.
func (m *Module) Watch(f func(reply []byte)) (stop func()) {
	fp := &f
	m.mu.Lock()
	if m.watchers == nil {
		m.watchers = map[*func([]byte)]bool{}
	}
	m.watchers[fp] = true
	start := !m.running
	m.running = true
	if start {
		m.gen++
	}
	gen := m.gen
	m.mu.Unlock()
	if start {
		go m.follow(gen)
	} else {
		// The goroutine waits for a change: this watcher gets the state
		// now of its own.
		go func() {
			b, err := ui.CallWait(m.Name, "current", nil)
			if err != nil {
				return
			}
			ui.Post(func() {
				m.mu.Lock()
				live := m.watchers[fp]
				m.mu.Unlock()
				if live {
					f(b)
				}
			})
		}()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			m.mu.Lock()
			delete(m.watchers, fp)
			last := len(m.watchers) == 0
			if last {
				// The goroutine's "next" is answered "stopped" (or never);
				// a watch from now on starts one of its own.
				m.running = false
				m.gen++
			}
			m.mu.Unlock()
			if last {
				ui.Post(func() { ui.Call(m.Name, "stop", nil, nil) })
			}
		})
	}
}

// follow asks for each change until no one watches: until the module's
// gen isn't gen.
func (m *Module) follow(gen int) {
	var last []byte
	for {
		b, err := ui.CallWait(m.Name, "next", last)
		m.mu.Lock()
		if m.gen != gen {
			m.mu.Unlock()
			return
		}
		fs := make([]func([]byte), 0, len(m.watchers))
		for f := range m.watchers {
			fs = append(fs, *f)
		}
		if len(fs) == 0 || err != nil && err.Error() != "stopped" {
			m.running = false
			m.gen++
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()
		if err != nil {
			continue
		}
		if n := m.StateLen; n > 0 && len(b) > n {
			last = append(last[:0:0], b[:n]...)
		} else {
			last = b
		}
		ui.Post(func() {
			for _, f := range fs {
				f(b)
			}
		})
	}
}
