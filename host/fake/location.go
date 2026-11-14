package fake

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/egoist/asuka/internal/follow"
	spec "github.com/egoist/asuka/internal/locationspec"
)

// Location levels, as asuka/sdk/location's Level: what the user allowed
// (Location.Level), and what they answer (Location.Answer).
const (
	LocationNotAsked   = spec.NotAsked
	LocationDenied     = spec.Denied
	LocationRestricted = spec.Restricted
	LocationWhenInUse  = spec.WhenInUse
	LocationAlways     = spec.Always
)

// Region transitions, as asuka/sdk/location's Transition (Location.Cross).
const (
	RegionEnter = spec.Enter
	RegionExit  = spec.Exit
	RegionDwell = spec.Dwell
)

// Location is the device's position, the user's permission and what the
// system does in the background, as the hosts' "location",
// "locationAccess" and "locationBackground" modules keep them (package
// location), for tests: Host.Location installs it. Its fields are read
// and set under its lock: set them before the app mounts, or with its
// methods.
//
// It behaves as a phone's system does: questions are asked once (iOS's
// Always once more after WhenInUse), regions need Always and are
// limited, updates and regions' transitions are queued as events until
// the Go code reports each done, so that those that came before the app
// started (Move and Cross before it mounts) reach its handlers once it
// sets them.
type Location struct {
	mu sync.Mutex
	h  *Host
	// Level, Precise and Off are what the user allowed (LocationNotAsked:
	// nothing yet), and whether location services are off.
	Level   byte
	Precise bool
	Off     bool
	// Answer is what the user answers the questions the app asks: the
	// most they allow (LocationDenied: no), and AnswerPrecise whether they
	// leave Precise on.
	Answer        byte
	AnswerPrecise bool
	// Asked are the questions shown, in order: "whenInUse", "always",
	// "precise:<purpose>"; Settings counts OpenSettings.
	Asked    []string
	Settings int
	// Computed leaves the background part to the Go code (as Linux and
	// the web): no "locationBackground" module but its capabilities.
	Computed bool
	// Limit is how many regions the system monitors (default 20).
	Limit int
	// Seconds is how long the system gives an event in the background
	// (0: no deadline).
	Seconds int
	// Here is where the device is (nil: no position yet).
	Here *spec.Position
	// Updates are the background updates running (nil: none), Regions
	// those monitored; Done are the events the Go code reported done, by
	// ID.
	Updates *spec.Updates
	Regions []spec.Region
	Done    []string

	access  *follow.Fake
	current []func([]byte, error)
	next    []func([]byte, error)
	unseen  []byte
	watched bool
	last    *spec.Position // the last update's (Updates.Distance)
	inside  map[string]bool
	events  []spec.Event // not done yet
	handed  map[string]bool
	waiting []func([]byte, error)
	nextID  int
}

// Location gives the host a device's location and the system's
// background location services (the "location", "locationAccess" and
// "locationBackground" modules), the user answering Answer: a host's
// that every phone has.
func (h *Host) Location() *Location {
	l := &Location{h: h, Answer: LocationWhenInUse, AnswerPrecise: true, Limit: 20, inside: map[string]bool{}, handed: map[string]bool{}}
	l.access = follow.NewFake(l.state())
	l.access.Other = l.accessCall
	if h.AsyncModules == nil {
		h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){}
	}
	h.AsyncModules["location"] = l.call
	h.AsyncModules["locationAccess"] = l.access.Answer
	h.AsyncModules["locationBackground"] = l.background
	return l
}

func (l *Location) state() []byte { return spec.Access(l.Level, l.Precise, l.Off) }

func (l *Location) allowed() bool {
	return !l.Off && (l.Level == spec.WhenInUse || l.Level == spec.Always)
}

// Allow changes what the user allows, as they do in Settings; watchers
// of the authorization see it.
func (l *Location) Allow(level byte, precise bool) {
	l.mu.Lock()
	l.Level, l.Precise = level, precise
	s := l.state()
	l.mu.Unlock()
	l.access.Set(s)
}

// ask asks the question for level (spec.WhenInUse or spec.Always), as
// the systems do: nothing once answered. Under l.mu.
func (l *Location) ask(level byte) {
	if l.Level == spec.NotAsked {
		l.Asked = append(l.Asked, "whenInUse")
		switch l.Answer {
		case spec.Denied, spec.Restricted:
			l.Level = l.Answer
		default:
			l.Level = spec.WhenInUse
			l.Precise = l.AnswerPrecise
		}
	}
	if level == spec.Always && l.Level == spec.WhenInUse && !slices.Contains(l.Asked, "always") {
		l.Asked = append(l.Asked, "always")
		if l.Answer == spec.Always {
			l.Level = spec.Always
		}
	}
}

func (l *Location) accessCall(method string, args []byte) ([]byte, error) {
	l.mu.Lock()
	switch method {
	case "request":
		level := byte(spec.WhenInUse)
		if len(args) > 0 {
			level = args[0]
		}
		l.ask(level)
	case "precise":
		if l.allowed() && !l.Precise {
			l.Asked = append(l.Asked, "precise:"+string(args))
			l.Precise = l.AnswerPrecise
		}
	case "settings":
		l.Settings++
		l.mu.Unlock()
		return nil, nil
	default:
		l.mu.Unlock()
		return nil, errors.New("no method " + method)
	}
	s := l.state()
	l.mu.Unlock()
	l.access.Set(s)
	return s, nil
}

// Move puts the device at latitude, longitude (5 meters accurate, now):
// a position for Current and Watch, the background updates', and the
// regions' transitions it makes.
func (l *Location) Move(latitude, longitude float64) {
	l.MoveTo(spec.Position{Lat: latitude, Lon: longitude, Acc: 5, Spd: -1, Crs: -1, T: time.Now().UnixMilli()})
}

// MoveTo puts the device at p.
func (l *Location) MoveTo(p spec.Position) {
	l.mu.Lock()
	l.Here = &p
	var replies []func([]byte, error)
	b, _ := json.Marshal(p)
	if l.allowed() {
		replies = l.current
		l.current = nil
		if l.watched {
			if len(l.next) == 0 {
				l.unseen = b
			} else {
				replies = append(replies, l.next...)
				l.next = nil
			}
		}
		if !l.Computed {
			if u := l.Updates; u != nil {
				min := u.Distance
				if u.Significant {
					min = max(min, 500)
				}
				if l.last == nil || meters(*l.last, p) >= min {
					q := p
					l.last = &q
					l.queue(spec.Event{Positions: []spec.Position{p}})
				}
			}
			if l.Level == spec.Always {
				for _, r := range l.Regions {
					in := meters(spec.Position{Lat: r.Lat, Lon: r.Lon}, p) <= r.Radius
					was, known := l.inside[r.ID]
					l.inside[r.ID] = in
					if known && was == in || !known && !in {
						continue
					}
					kind := spec.Exit
					if in {
						kind = spec.Enter
					}
					if r.Events&kind != 0 {
						at := p
						l.queue(spec.Event{Region: r.ID, Kind: kind, At: &at, T: p.T})
					}
				}
			}
		}
	}
	answer := l.answerEvents()
	l.mu.Unlock()
	for _, r := range replies {
		r(b, nil)
	}
	answer()
}

// Cross reports a region's transition (RegionEnter, RegionExit,
// RegionDwell), as the system does, whether or not it's monitored.
func (l *Location) Cross(region string, kind int) {
	l.mu.Lock()
	ev := spec.Event{Region: region, Kind: kind, T: time.Now().UnixMilli()}
	if l.Here != nil {
		at := *l.Here
		ev.At = &at
	}
	l.queue(ev)
	answer := l.answerEvents()
	l.mu.Unlock()
	answer()
}

// Watched reports whether the Go code waits for the device's next
// position (Watch's): a Move now reaches it, not only the newest of
// several.
func (l *Location) Watched() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.next) > 0
}

// Pending is how many events the Go code hasn't reported done.
func (l *Location) Pending() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.events)
}

// queue keeps an event until it's done. Under l.mu.
func (l *Location) queue(e spec.Event) {
	l.nextID++
	e.ID = "e" + strconv.Itoa(l.nextID)
	e.Seconds = l.Seconds
	l.events = append(l.events, e)
}

// answerEvents hands the events not handed yet to the waiting "next"s;
// the returned func replies, once l.mu is unlocked. Under l.mu.
func (l *Location) answerEvents() func() {
	var calls []func()
	for _, e := range l.events {
		if len(l.waiting) == 0 {
			break
		}
		if l.handed[e.ID] {
			continue
		}
		l.handed[e.ID] = true
		w := l.waiting[0]
		l.waiting = l.waiting[1:]
		b, _ := json.Marshal(e)
		calls = append(calls, func() { w(b, nil) })
	}
	return func() {
		for _, c := range calls {
			c()
		}
	}
}

func (l *Location) call(method string, args []byte, reply func([]byte, error)) {
	l.mu.Lock()
	switch method {
	case "current", "next":
		l.ask(spec.WhenInUse)
		s := l.state()
		if !l.allowed() {
			l.mu.Unlock()
			l.access.Set(s)
			reply(nil, errors.New("denied"))
			return
		}
		defer l.access.Set(s)
		if method == "current" {
			if l.Here == nil {
				l.current = append(l.current, reply)
				l.mu.Unlock()
				return
			}
			b, _ := json.Marshal(*l.Here)
			l.mu.Unlock()
			reply(b, nil)
			return
		}
		if !l.watched && l.Here != nil {
			l.unseen, _ = json.Marshal(*l.Here)
		}
		l.watched = true
		if u := l.unseen; u != nil {
			l.unseen = nil
			l.mu.Unlock()
			reply(u, nil)
			return
		}
		l.next = append(l.next, reply)
		l.mu.Unlock()
	case "stop":
		ws := l.next
		l.next, l.unseen, l.watched = nil, nil, false
		l.mu.Unlock()
		for _, w := range ws {
			w(nil, errors.New("stopped"))
		}
		reply(nil, nil)
	case "name":
		l.mu.Unlock()
		reply(nil, errors.New("no name for this place"))
	default:
		l.mu.Unlock()
		reply(nil, errors.New("no method "+method))
	}
}

func (l *Location) background(method string, args []byte, reply func([]byte, error)) {
	l.mu.Lock()
	if l.Computed && method != "capabilities" {
		l.mu.Unlock()
		reply(nil, errors.New("not available: computed by the Go code"))
		return
	}
	switch method {
	case "capabilities":
		c := spec.Capabilities{Updates: !l.Computed, Regions: !l.Computed, Limit: l.Limit, Dwell: !l.Computed, Relaunch: !l.Computed}
		if l.Computed {
			c.Limit = 0
		}
		l.mu.Unlock()
		b, _ := json.Marshal(c)
		reply(b, nil)
		return
	case "start":
		var u spec.Updates
		if err := json.Unmarshal(args, &u); err != nil {
			l.mu.Unlock()
			reply(nil, err)
			return
		}
		l.ask(spec.WhenInUse)
		s := l.state()
		if !l.allowed() {
			l.mu.Unlock()
			l.access.Set(s)
			reply(nil, errors.New("denied"))
			return
		}
		defer l.access.Set(s)
		l.Updates, l.last = &u, nil
	case "stop":
		l.Updates = nil
	case "updating":
		b, _ := json.Marshal(l.Updates)
		l.mu.Unlock()
		reply(b, nil)
		return
	case "monitor":
		var r spec.Region
		if err := json.Unmarshal(args, &r); err != nil {
			l.mu.Unlock()
			reply(nil, err)
			return
		}
		if !l.allowed() || l.Level != spec.Always {
			l.mu.Unlock()
			reply(nil, errors.New("always"))
			return
		}
		i := slices.IndexFunc(l.Regions, func(x spec.Region) bool { return x.ID == r.ID })
		if i < 0 && len(l.Regions) >= l.Limit {
			l.mu.Unlock()
			reply(nil, fmt.Errorf("too many regions (%d at most)", l.Limit))
			return
		}
		if i >= 0 {
			l.Regions[i] = r
		} else {
			l.Regions = append(l.Regions, r)
		}
		delete(l.inside, r.ID)
		if l.Here != nil && meters(spec.Position{Lat: r.Lat, Lon: r.Lon}, *l.Here) <= r.Radius {
			l.inside[r.ID] = true
			if r.Events&spec.Enter != 0 {
				at := *l.Here
				l.queue(spec.Event{Region: r.ID, Kind: spec.Enter, At: &at, T: time.Now().UnixMilli()})
			}
		}
	case "unmonitor":
		id := string(args)
		l.Regions = slices.DeleteFunc(l.Regions, func(x spec.Region) bool { return id == "" || x.ID == id })
	case "regions":
		b, _ := json.Marshal(append([]spec.Region{}, l.Regions...))
		l.mu.Unlock()
		reply(b, nil)
		return
	case "next":
		l.waiting = append(l.waiting, reply)
		answer := l.answerEvents()
		l.mu.Unlock()
		answer()
		return
	case "done":
		id := string(args)
		l.events = slices.DeleteFunc(l.events, func(e spec.Event) bool { return e.ID == id })
		l.Done = append(l.Done, id)
	default:
		l.mu.Unlock()
		reply(nil, errors.New("no method "+method))
		return
	}
	answer := l.answerEvents()
	l.mu.Unlock()
	answer()
	reply(nil, nil)
}

// meters is the great-circle distance between a and b.
func meters(a, b spec.Position) float64 {
	const r = 6371008.8
	rad := math.Pi / 180
	dLat, dLon := (b.Lat-a.Lat)*rad, (b.Lon-a.Lon)*rad
	s := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(s)))
}
