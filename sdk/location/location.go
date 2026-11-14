// Package location is where the device is: now, and as it moves, with
// the user's permission, which it asks for the first time (Core
// Location; Android's location manager, which needs no Play services).
// An app that imports it is given the permissions it needs; asuka.json's
// ios.usage.NSLocationWhenInUseUsageDescription says why to the user.
//
// With the app out of sight too (expo-location's background updates and
// geofencing): the permission's levels (Request, RequestAlways,
// RequestPrecise, UseAuthorization), updates that go on in the
// background (StartUpdates, OnUpdates) and regions the system watches
// for the app (Monitor, OnCrossing). Their events reach the app's Go code
// even when it isn't running: the system starts the app in the
// background, without a screen (main runs, and the root component), and
// hands them over; set the handlers as the app starts.
package location

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/egoist/asuka/internal/locationspec"
	"github.com/egoist/asuka/ui"
)

// Position is a place the device was at, at a time.
type Position struct {
	Latitude, Longitude float64
	Accuracy            float64 // meters: the radius the device is within
	Altitude            float64 // meters above sea level; 0: not known
	// Speed (meters per second) and Course (degrees from true north,
	// clockwise): negative when not known.
	Speed, Course float64
	Time          time.Time
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
	w := locationspec.Position{Spd: -1, Crs: -1}
	if err := json.Unmarshal(b, &w); err != nil {
		return Position{}, err
	}
	return position(w), nil
}

// position is a host's position; one that leaves out speed and course
// doesn't know them (its Spd and Crs were -1 before it was read).
func position(w locationspec.Position) Position {
	return Position{Latitude: w.Lat, Longitude: w.Lon, Accuracy: w.Acc, Altitude: w.Alt, Speed: w.Spd, Course: w.Crs, Time: time.UnixMilli(w.T)}
}

func (p Position) wire() locationspec.Position {
	return locationspec.Position{Lat: p.Latitude, Lon: p.Longitude, Acc: p.Accuracy, Alt: p.Altitude, Spd: p.Speed, Crs: p.Course, T: p.Time.UnixMilli()}
}

// Current asks for permission if it hasn't been, then returns where the
// device is. Not on the main thread: it waits for the user, then a fix.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Current() (Position, error) {
	return decode(ui.CallWait("location", "current", nil))
}

var (
	mu       sync.Mutex
	watchers = map[*func(Position)]bool{}
	running  bool
	// gen is the goroutine that follows: one that wakes to a gen moved
	// on (the last watcher stopped, perhaps another began) ends without
	// touching the watchers, so a new watch never waits on a goroutine
	// whose "next" the stop answers later, or never (a root gone).
	gen int
)

// Watch calls f, on the main thread, with each position as the device
// moves, until stop is called; the first is the position now. It asks
// for permission as Current does; f isn't called if it's denied.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Watch(f func(Position)) (stop func()) {
	fp := &f
	mu.Lock()
	watchers[fp] = true
	start := !running
	running = true
	if start {
		gen++
	}
	g := gen
	mu.Unlock()
	if start {
		go follow(g)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			delete(watchers, fp)
			last := len(watchers) == 0
			if last {
				running = false
				gen++
			}
			mu.Unlock()
			if last {
				ui.Post(func() { ui.Call("location", "stop", nil, nil) })
			}
		})
	}
}

// follow asks the host for each next position while anyone watches:
// until gen isn't g.
func follow(g int) {
	for {
		p, err := decode(ui.CallWait("location", "next", nil))
		mu.Lock()
		if gen != g {
			mu.Unlock()
			return
		}
		fs := make([]func(Position), 0, len(watchers))
		for f := range watchers {
			fs = append(fs, *f)
		}
		if len(fs) == 0 || err != nil && err.Error() != "stopped" {
			running = false
			gen++
			mu.Unlock()
			return
		}
		mu.Unlock()
		if err != nil {
			continue
		}
		ui.Post(func() {
			for _, f := range fs {
				f(p)
			}
		})
	}
}

// PlaceName is what a place is called, as a person would say it ("Mission
// District, San Francisco"), in the device's language. It asks the platform's
// geocoder, which asks its servers: ask only when the user wants the name.
// Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func PlaceName(latitude, longitude float64) (string, error) {
	args, err := json.Marshal(map[string]float64{"lat": latitude, "lon": longitude})
	if err != nil {
		return "", err
	}
	b, err := ui.CallWait("location", "name", args)
	return string(b), err
}
