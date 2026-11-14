package location

import (
	"math"
	"slices"
	"sync"
	"time"

	"github.com/egoist/asuka/internal/locationspec"
	"github.com/egoist/asuka/sdk/store"
	"github.com/egoist/asuka/ui"
)

// What the system doesn't do itself (Support), done here from the
// positions Watch gets while the app runs: the background updates
// (filtered by distance, batched by Defer) and the regions' transitions.
// What's started is kept in the app's store, and resumes at the next
// launch.

const storeKey = "asuka/sdk/location"

type computedState struct {
	Updates *locationspec.Updates `json:"updates,omitempty"`
	Regions []locationspec.Region `json:"regions,omitempty"`
	// Inside are the regions the device was last known to be in or out
	// of, by ID: since when (Unix ms), and whether it dwelt there.
	Inside map[string]regionState `json:"inside,omitempty"`
}

type regionState struct {
	In    bool  `json:"in"`
	Since int64 `json:"since"`
	Dwelt bool  `json:"dwelt,omitempty"`
}

type engine struct {
	mu      sync.Mutex
	loaded  bool
	st      computedState
	stop    func()    // Watch's
	last    *Position // the last update's
	seen    *Position // the last position's
	pending []locationspec.Position
	flushAt *time.Timer
	dwells  map[string]*time.Timer
}

var computed engine

func (e *engine) load() {
	if e.loaded {
		return
	}
	e.loaded = true
	store.Get(storeKey, &e.st)
	if e.st.Inside == nil {
		e.st.Inside = map[string]regionState{}
	}
}

func (e *engine) save() { store.Set(storeKey, e.st) }

// resume follows again what a launch before this one started, if the
// user still allows it (it doesn't ask at launch). Not on the main
// thread.
func (e *engine) resume() {
	c := capabilities()
	e.mu.Lock()
	e.load()
	want := !c.Updates && e.st.Updates != nil || !c.Regions && len(e.st.Regions) > 0
	e.mu.Unlock()
	if !want {
		return
	}
	if a, err := Authorized(); err != nil || !a.Level.Allowed() {
		return
	}
	ui.Post(e.follow)
}

func (e *engine) startUpdates(o Updates) error {
	if a, err := Request(); err != nil {
		return err
	} else if !a.Level.Allowed() {
		return ErrDenied
	}
	w := o.wire()
	e.mu.Lock()
	e.load()
	e.st.Updates = &w
	e.last = nil
	e.save()
	e.mu.Unlock()
	ui.Post(e.follow)
	return nil
}

func (e *engine) stopUpdates() {
	e.mu.Lock()
	e.load()
	e.st.Updates = nil
	e.pending = nil
	if e.flushAt != nil {
		e.flushAt.Stop()
		e.flushAt = nil
	}
	e.save()
	e.mu.Unlock()
	ui.Post(e.follow)
}

func (e *engine) updating() (locationspec.Updates, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.load()
	if e.st.Updates == nil {
		return locationspec.Updates{}, false
	}
	return *e.st.Updates, true
}

func (e *engine) monitor(r Region) error {
	if a, err := Request(); err != nil {
		return err
	} else if !a.Level.Allowed() {
		return ErrDenied
	}
	w := r.wire()
	e.mu.Lock()
	e.load()
	i := slices.IndexFunc(e.st.Regions, func(x locationspec.Region) bool { return x.ID == w.ID })
	if i >= 0 {
		e.st.Regions[i] = w
	} else {
		e.st.Regions = append(e.st.Regions, w)
	}
	// Unknown until the next position, which says whether it's inside.
	delete(e.st.Inside, w.ID)
	e.stopDwell(w.ID)
	e.save()
	e.mu.Unlock()
	ui.Post(e.follow)
	return nil
}

func (e *engine) unmonitor(id string) {
	e.mu.Lock()
	e.load()
	e.st.Regions = slices.DeleteFunc(e.st.Regions, func(x locationspec.Region) bool { return id == "" || x.ID == id })
	for k := range e.st.Inside {
		if id == "" || k == id {
			delete(e.st.Inside, k)
			e.stopDwell(k)
		}
	}
	e.save()
	e.mu.Unlock()
	ui.Post(e.follow)
}

func (e *engine) regions() []Region {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.load()
	rs := make([]Region, len(e.st.Regions))
	for i, w := range e.st.Regions {
		rs[i] = regionFrom(w)
	}
	return rs
}

// follow watches the positions while anything wants them, and stops
// when nothing does. Main thread.
func (e *engine) follow() {
	c := capsKnown()
	e.mu.Lock()
	want := !c.Updates && e.st.Updates != nil || !c.Regions && len(e.st.Regions) > 0
	stop := e.stop
	if want && stop == nil {
		e.stop = func() {} // starting
	}
	if !want {
		e.stop = nil
	}
	e.mu.Unlock()
	switch {
	case want && stop == nil:
		s := Watch(e.moved)
		e.mu.Lock()
		e.stop = s
		e.mu.Unlock()
		e.resumeDwells()
	case !want && stop != nil:
		stop()
	}
}

// capsKnown is the capabilities asked already (follow runs after a
// call that asked them).
func capsKnown() locationspec.Capabilities {
	capsMu.Lock()
	defer capsMu.Unlock()
	return capsVal
}

// moved is a position Watch got. Main thread.
func (e *engine) moved(p Position) {
	c := capsKnown()
	e.mu.Lock()
	seen := p
	e.seen = &seen
	if e.st.Inside == nil {
		e.st.Inside = map[string]regionState{}
	}
	var crossings []locationspec.Event
	if !c.Regions {
		crossings = e.cross(p)
	}
	var batch []locationspec.Position
	if u := e.st.Updates; u != nil && !c.Updates {
		batch = e.update(*u, p)
	}
	e.mu.Unlock()
	for _, ev := range crossings {
		events.deliver(ev)
	}
	if batch != nil {
		events.deliver(locationspec.Event{Positions: batch})
	}
}

// update keeps p if it's far enough from the last one, and returns the
// batch to deliver now, if one is due.
func (e *engine) update(u locationspec.Updates, p Position) []locationspec.Position {
	min := u.Distance
	if u.Significant {
		min = max(min, 500)
	}
	if e.last != nil && distance(e.last.Latitude, e.last.Longitude, p.Latitude, p.Longitude) < min {
		return nil
	}
	last := p
	e.last = &last
	e.pending = append(e.pending, p.wire())
	if u.Defer <= 0 {
		b := e.pending
		e.pending = nil
		return b
	}
	if e.flushAt == nil {
		e.flushAt = time.AfterFunc(time.Duration(u.Defer)*time.Millisecond, func() { ui.Post(e.flush) })
	}
	return nil
}

// flush delivers the batch Defer held. Main thread.
func (e *engine) flush() {
	e.mu.Lock()
	b := e.pending
	e.pending = nil
	e.flushAt = nil
	e.mu.Unlock()
	if len(b) > 0 {
		events.deliver(locationspec.Event{Positions: b})
	}
}

// cross updates each region's state with p, and returns its transitions.
// A position tells only when its accuracy decides it: inside, if it's
// within the radius and no less exact than it; outside, if even its
// nearest point is out.
func (e *engine) cross(p Position) []locationspec.Event {
	var out []locationspec.Event
	changed := false
	now := p.Time.UnixMilli()
	at := p.wire()
	for _, r := range e.st.Regions {
		d := distance(r.Lat, r.Lon, p.Latitude, p.Longitude)
		var in bool
		switch {
		case d <= r.Radius && p.Accuracy <= r.Radius:
			in = true
		case d-p.Accuracy > r.Radius:
			in = false
		default:
			continue
		}
		was, known := e.st.Inside[r.ID]
		if known && was.In == in {
			continue
		}
		e.st.Inside[r.ID] = regionState{In: in, Since: now}
		changed = true
		kind := locationspec.Exit
		if in {
			kind = locationspec.Enter
			e.startDwell(r, r.Dwell)
		} else {
			e.stopDwell(r.ID)
		}
		// From unknown to outside is no transition: the region was just
		// added (or the app just started) with the device out of it.
		if !known && !in || r.Events&kind == 0 {
			continue
		}
		pos := at
		out = append(out, locationspec.Event{Region: r.ID, Kind: kind, At: &pos, T: now})
	}
	if changed {
		e.save()
	}
	return out
}

// startDwell reports Dwell for r after wait, if the device stays.
func (e *engine) startDwell(r locationspec.Region, wait int64) {
	if r.Events&locationspec.Dwell == 0 {
		return
	}
	e.stopDwell(r.ID)
	if e.dwells == nil {
		e.dwells = map[string]*time.Timer{}
	}
	id := r.ID
	e.dwells[id] = time.AfterFunc(time.Duration(max(wait, 0))*time.Millisecond, func() { ui.Post(func() { e.dwelt(id) }) })
}

func (e *engine) stopDwell(id string) {
	if t := e.dwells[id]; t != nil {
		t.Stop()
		delete(e.dwells, id)
	}
}

// resumeDwells restarts the dwell timers of the regions the device was
// inside as the last launch ended. Main thread.
func (e *engine) resumeDwells() {
	e.mu.Lock()
	defer e.mu.Unlock()
	now := time.Now().UnixMilli()
	for _, r := range e.st.Regions {
		if s, ok := e.st.Inside[r.ID]; ok && s.In && !s.Dwelt && e.dwells[r.ID] == nil {
			e.startDwell(r, s.Since+r.Dwell-now)
		}
	}
}

// dwelt is a dwell timer's end. Main thread.
func (e *engine) dwelt(id string) {
	e.mu.Lock()
	delete(e.dwells, id)
	s, ok := e.st.Inside[id]
	if !ok || !s.In || s.Dwelt || !slices.ContainsFunc(e.st.Regions, func(r locationspec.Region) bool { return r.ID == id }) {
		e.mu.Unlock()
		return
	}
	s.Dwelt = true
	e.st.Inside[id] = s
	e.save()
	var at *locationspec.Position
	if e.seen != nil {
		w := e.seen.wire()
		at = &w
	}
	e.mu.Unlock()
	events.deliver(locationspec.Event{Region: id, Kind: locationspec.Dwell, At: at, T: time.Now().UnixMilli()})
}

// distance is the great-circle distance between two points, in meters.
func distance(lat1, lon1, lat2, lon2 float64) float64 {
	const r = 6371008.8 // the Earth's mean radius
	rad := math.Pi / 180
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) + math.Cos(lat1*rad)*math.Cos(lat2*rad)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * r * math.Asin(math.Min(1, math.Sqrt(a)))
}
