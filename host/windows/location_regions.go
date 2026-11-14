//go:build windows

package windows

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/asuka/internal/appdir"
	spec "github.com/egoist/asuka/internal/locationspec"
	. "github.com/egoist/asuka/internal/windows"
)

// asuka/sdk/location's authorization and regions on Windows (locationspec's
// "locationAccess" and "locationBackground"). The authorization is the
// Geolocator's status (Settings, Privacy & security, Location): a desktop
// app runs out of sight, so allowed is Always; Windows has no approximate
// choice for an app. The regions are Windows.Devices.Geolocation.
// Geofencing's GeofenceMonitor, while the app runs: a background task's
// LocationTrigger needs a packaged app. Where the monitor isn't there,
// the Go package computes them from the positions. The background updates
// are always the Go package's, as the app runs anyway. The regions are
// kept in the app's data, with what each reports; the events in memory
// until Go reports each done.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["locationAccess"] = h.locationAccessModule()
		m["locationBackground"] = h.locationRegionsModule()
	})
}

type locRegions struct {
	loaded   bool
	fences   *Geofences
	opened   bool
	list     []spec.Region
	inside   map[string]bool
	events   []spec.Event
	handed   map[string]bool
	waiting  []func([]byte, error)
	nextID   int
	geo      *Geolocator // for the status
	answered bool        // the user answered this run (RequestAccessAsync)
	allowed  bool
	waits    []accessWait
}

type accessWait struct {
	last  []byte
	reply func([]byte, error)
}

func regionsFile() string { return filepath.Join(appdir.Data(), "asuka-location-regions.json") }

func (h *Host) locRegions() *locRegions {
	r := &h.regionsState
	if r.loaded {
		return r
	}
	r.loaded = true
	r.inside, r.handed = map[string]bool{}, map[string]bool{}
	if b, err := os.ReadFile(regionsFile()); err == nil {
		var saved struct {
			Regions []spec.Region   `json:"regions"`
			Inside  map[string]bool `json:"inside"`
		}
		if json.Unmarshal(b, &saved) == nil {
			r.list = saved.Regions
			if saved.Inside != nil {
				r.inside = saved.Inside
			}
		}
	}
	return r
}

func (h *Host) saveLocRegions() {
	r := h.locRegions()
	b, _ := json.Marshal(map[string]any{"regions": r.list, "inside": r.inside})
	os.MkdirAll(appdir.Data(), 0o755)
	os.WriteFile(regionsFile(), b, 0o644)
}

// geofences is the monitor, opened once and watched (nil where Windows
// has none); the regions kept are added again if it lost them.
func (h *Host) geofences() *Geofences {
	r := h.locRegions()
	if r.opened {
		return r.fences
	}
	r.opened = true
	if !WinRTAvailable() {
		return nil
	}
	g, err := OpenGeofences()
	if err != nil {
		return nil
	}
	r.fences = g
	have := g.IDs()
	for _, rg := range r.list {
		if !slices.Contains(have, rg.ID) {
			g.Add(rg.ID, rg.Lat, rg.Lon, rg.Radius, GeofenceEntered|GeofenceExited, time.Duration(rg.Dwell)*time.Millisecond)
		}
	}
	g.Watch(func(rs []GeofenceReport) {
		h.Post(func() {
			for _, rep := range rs {
				switch rep.State {
				case GeofenceEntered:
					h.regionCrossed(rep.ID, spec.Enter, rep.At)
				case GeofenceExited:
					h.regionCrossed(rep.ID, spec.Exit, rep.At)
				case GeofenceRemoved:
					r := h.locRegions()
					r.list = slices.DeleteFunc(r.list, func(x spec.Region) bool { return x.ID == rep.ID })
					h.saveLocRegions()
				}
			}
		})
	})
	return g
}

// accessState is the authorization on the wire (locationspec.Access).
func (h *Host) accessState() []byte {
	r := h.locRegions()
	if r.geo == nil && WinRTAvailable() {
		if g, err := NewGeolocator(); err == nil {
			r.geo = g
			g.WatchStatus(func(int) { h.Post(h.accessChanged) })
		}
	}
	if r.geo == nil {
		return spec.Access(spec.Denied, false, true)
	}
	level, off := spec.NotAsked, false
	switch r.geo.Status() {
	case PositionDisabled:
		level = spec.Denied
	case PositionNotAvailable:
		level, off = spec.Denied, true
	case PositionNotInitialized:
		if r.answered {
			level = spec.Denied
			if r.allowed {
				level = spec.Always
			}
		}
	default:
		level = spec.Always
	}
	return spec.Access(level, level == spec.Always, off)
}

func (h *Host) accessChanged() {
	r := h.locRegions()
	s := h.accessState()
	var keep []accessWait
	for _, w := range r.waits {
		if string(w.last) != string(s) {
			w.reply(s, nil)
		} else {
			keep = append(keep, w)
		}
	}
	r.waits = keep
}

func (h *Host) locationAccessModule() map[string]asyncMethod {
	return map[string]asyncMethod{
		"current": func(_ []byte, reply func([]byte, error)) { reply(h.accessState(), nil) },
		"next": func(last []byte, reply func([]byte, error)) {
			if s := h.accessState(); len(last) == 0 || string(last) != string(s) {
				reply(s, nil)
				return
			}
			r := h.locRegions()
			r.waits = append(r.waits, accessWait{append([]byte(nil), last...), reply})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			r := h.locRegions()
			ws := r.waits
			r.waits = nil
			for _, w := range ws {
				w.reply(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
		"request": func(_ []byte, reply func([]byte, error)) {
			RequestLocation(func(allowed bool, err error) {
				h.Post(func() {
					r := h.locRegions()
					if err == nil {
						r.answered, r.allowed = true, allowed
					}
					h.accessChanged()
					reply(h.accessState(), nil)
				})
			})
		},
		// Windows has no approximate choice for an app.
		"precise": func(_ []byte, reply func([]byte, error)) { reply(h.accessState(), nil) },
		"settings": func(_ []byte, reply func([]byte, error)) {
			withSTA(func() { ShellExecute(0, "", "ms-settings:privacy-location") })
			reply(nil, nil)
		},
	}
}

func (h *Host) locationRegionsModule() map[string]asyncMethod {
	computed := failing("not available: computed by the Go code on Windows")
	return map[string]asyncMethod{
		"capabilities": func(_ []byte, reply func([]byte, error)) {
			b, _ := json.Marshal(spec.Capabilities{Regions: h.geofences() != nil})
			reply(b, nil)
		},
		"start":    computed,
		"stop":     computed,
		"updating": func(_ []byte, reply func([]byte, error)) { reply([]byte("null"), nil) },
		"monitor": func(args []byte, reply func([]byte, error)) {
			var rg spec.Region
			if err := json.Unmarshal(args, &rg); err != nil {
				reply(nil, err)
				return
			}
			g := h.geofences()
			if g == nil {
				reply(nil, errors.New("not available: no geofence monitor"))
				return
			}
			if s := h.accessState(); s[0] != spec.Always {
				reply(nil, errors.New("denied"))
				return
			}
			// Both states, to know where the device is for the next Enter.
			if err := g.Add(rg.ID, rg.Lat, rg.Lon, rg.Radius, GeofenceEntered|GeofenceExited, time.Duration(rg.Dwell)*time.Millisecond); err != nil {
				reply(nil, err)
				return
			}
			r := h.locRegions()
			r.list = append(slices.DeleteFunc(r.list, func(x spec.Region) bool { return x.ID == rg.ID }), rg)
			delete(r.inside, rg.ID)
			h.saveLocRegions()
			reply(nil, nil)
		},
		"unmonitor": func(args []byte, reply func([]byte, error)) {
			id := string(args)
			if g := h.geofences(); g != nil {
				g.Remove(id)
			}
			r := h.locRegions()
			r.list = slices.DeleteFunc(r.list, func(x spec.Region) bool { return id == "" || x.ID == id })
			for k := range r.inside {
				if id == "" || k == id {
					delete(r.inside, k)
				}
			}
			h.saveLocRegions()
			reply(nil, nil)
		},
		"regions": func(_ []byte, reply func([]byte, error)) {
			b, _ := json.Marshal(append([]spec.Region{}, h.locRegions().list...))
			reply(b, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			h.geofences() // its reports start
			r := h.locRegions()
			r.waiting = append(r.waiting, reply)
			h.handRegionEvents()
		},
		"done": func(args []byte, reply func([]byte, error)) {
			r := h.locRegions()
			id := string(args)
			r.events = slices.DeleteFunc(r.events, func(e spec.Event) bool { return e.ID == id })
			delete(r.handed, id)
			reply(nil, nil)
		},
	}
}

func (h *Host) handRegionEvents() {
	r := h.locRegions()
	for _, e := range r.events {
		if len(r.waiting) == 0 {
			return
		}
		if r.handed[e.ID] {
			continue
		}
		r.handed[e.ID] = true
		w := r.waiting[0]
		r.waiting = r.waiting[1:]
		b, _ := json.Marshal(e)
		w(b, nil)
	}
}

// regionCrossed is a geofence's new state: an event, if it's one the
// region reports. From unknown to outside is none: the region was just
// added with the device out of it.
func (h *Host) regionCrossed(id string, kind int, at *Fix) {
	r := h.locRegions()
	i := slices.IndexFunc(r.list, func(x spec.Region) bool { return x.ID == id })
	if i < 0 {
		return
	}
	was, known := r.inside[id]
	in := kind == spec.Enter
	r.inside[id] = in
	h.saveLocRegions()
	if !known && !in || known && was == in || r.list[i].Events&kind == 0 {
		return
	}
	r.nextID++
	e := spec.Event{ID: "r" + strconv.Itoa(r.nextID), Region: id, Kind: kind, T: time.Now().UnixMilli()}
	if at != nil {
		e.At = &spec.Position{Lat: at.Lat, Lon: at.Lon, Acc: at.Acc, Alt: at.Alt, Spd: at.Spd, Crs: at.Crs, T: at.T}
	}
	r.events = append(r.events, e)
	h.handRegionEvents()
}
