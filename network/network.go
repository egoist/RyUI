// Package network is the device's network: whether it's online, over
// Wi-Fi or cellular, and whether the connection costs (expo-network's
// getNetworkStateAsync): to download large files on Wi-Fi only, to say
// the app is offline. Android's ConnectivityManager; iOS's NWPathMonitor.
package network

import (
	"errors"
	"sync"

	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

// Type is the kind of connection.
type Type uint8

const (
	None     Type = iota // offline
	WiFi                 // Wi-Fi
	Cellular             // mobile data
	Ethernet             // a cable
	Other                // a VPN alone, Bluetooth...
)

func (t Type) String() string {
	return [...]string{"none", "wifi", "cellular", "ethernet", "other"}[min(int(t), 4)]
}

// State is the network the device uses now.
type State struct {
	Connected bool
	Type      Type
	// Metered: the connection costs, or is limited: mobile data, a phone's
	// hotspot (Android's not NOT_METERED; iOS's isExpensive).
	Metered bool
	// Constrained: the user asked apps to use less data (Android's Data
	// Saver; iOS's Low Data Mode).
	Constrained bool
}

var errBadReply = errors.New("network: a bad reply")

// decode reads the host's reply: u8 connected, u8 type, u8 metered, u8
// constrained.
func decode(b []byte, err error) (State, error) {
	if err != nil {
		return State{}, err
	}
	if len(b) < 4 {
		return State{}, errBadReply
	}
	return State{Connected: b[0] == 1, Type: Type(b[1]), Metered: b[2] == 1, Constrained: b[3] == 1}, nil
}

func encode(s State) []byte {
	b2 := func(v bool) byte {
		if v {
			return 1
		}
		return 0
	}
	return []byte{b2(s.Connected), byte(s.Type), b2(s.Metered), b2(s.Constrained)}
}

// Current is the network now. Not on the main thread.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func Current() (State, error) {
	return decode(ui.CallWait("network", "current", nil))
}

var (
	mu       sync.Mutex
	watchers = map[*func(State)]bool{}
	running  bool
)

// Watch calls f, on the main thread, with the network now and each time
// it changes, until stop.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func Watch(f func(State)) (stop func()) {
	fp := &f
	mu.Lock()
	watchers[fp] = true
	start := !running
	running = true
	mu.Unlock()
	if start {
		go follow()
	} else {
		go func() {
			if s, err := Current(); err == nil {
				ui.Post(func() {
					mu.Lock()
					live := watchers[fp]
					mu.Unlock()
					if live {
						f(s)
					}
				})
			}
		}()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			delete(watchers, fp)
			last := len(watchers) == 0
			mu.Unlock()
			if last {
				ui.Post(func() { ui.Call("network", "stop", nil, nil) })
			}
		})
	}
}

// follow asks the host for each change ("next" answers once the state
// isn't the one given), until no one watches.
func follow() {
	var last []byte
	for {
		b, err := ui.CallWait("network", "next", last)
		s, err := decode(b, err)
		mu.Lock()
		fs := make([]func(State), 0, len(watchers))
		for f := range watchers {
			fs = append(fs, *f)
		}
		if len(fs) == 0 || err != nil {
			running = false
			mu.Unlock()
			return
		}
		mu.Unlock()
		last = encode(s)
		ui.Post(func() {
			for _, f := range fs {
				f(s)
			}
		})
	}
}

// Use is the network as a signal, for a component: it follows changes
// while the component lives. Main thread.
func Use() signal.Accessor[State] {
	s := signal.New(State{Connected: true, Type: Other})
	stop := Watch(func(n State) { s.Set(n) })
	signal.OnCleanup(stop)
	return s
}
