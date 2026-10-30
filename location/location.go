// Package location is where the device is: now, and as it moves, with
// the user's permission, which it asks for the first time (Core
// Location; Android's location manager, which needs no Play services).
// An app that imports it is given the permissions it needs; nui.json's
// ios.usage.NSLocationWhenInUseUsageDescription says why to the user.
package location

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/egoist/nui/ui"
)

// Position is a place the device was at, at a time.
type Position struct {
	Latitude, Longitude float64
	Accuracy            float64 // meters: the radius the device is within
	Altitude            float64 // meters above sea level; 0: not known
	Time                time.Time
}

// ErrDenied is the error when the user hasn't allowed the app their
// location.
var ErrDenied = errors.New("location: not allowed")

func decode(b []byte, err error) (Position, error) {
	if err != nil {
		if strings.Contains(err.Error(), "denied") {
			return Position{}, ErrDenied
		}
		return Position{}, err
	}
	var w struct {
		Lat, Lon, Acc, Alt float64
		T                  int64
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return Position{}, err
	}
	return Position{Latitude: w.Lat, Longitude: w.Lon, Accuracy: w.Acc, Altitude: w.Alt, Time: time.UnixMilli(w.T)}, nil
}

// Current asks for permission if it hasn't been, then returns where the
// device is. Not on the main thread: it waits for the user, then a fix.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func Current() (Position, error) {
	return decode(ui.CallWait("location", "current", nil))
}

var (
	mu       sync.Mutex
	watchers = map[*func(Position)]bool{}
	running  bool
)

// Watch calls f, on the main thread, with each position as the device
// moves, until stop is called; the first is the position now. It asks
// for permission as Current does; f isn't called if it's denied.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func Watch(f func(Position)) (stop func()) {
	fp := &f
	mu.Lock()
	watchers[fp] = true
	start := !running
	running = true
	mu.Unlock()
	if start {
		go follow()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			delete(watchers, fp)
			last := len(watchers) == 0
			mu.Unlock()
			if last {
				ui.Post(func() { ui.Call("location", "stop", nil, nil) })
			}
		})
	}
}

// follow asks the host for each next position while anyone watches.
func follow() {
	for {
		p, err := decode(ui.CallWait("location", "next", nil))
		mu.Lock()
		fs := make([]func(Position), 0, len(watchers))
		for f := range watchers {
			fs = append(fs, *f)
		}
		if len(fs) == 0 || err != nil {
			running = false
			mu.Unlock()
			return
		}
		mu.Unlock()
		ui.Post(func() {
			for _, f := range fs {
				f(p)
			}
		})
	}
}
