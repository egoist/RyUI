//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/egoist/asuka/internal/appdir"
	. "github.com/egoist/asuka/internal/darwin"
	spec "github.com/egoist/asuka/internal/locationspec"
)

// asuka/sdk/location's authorization and regions on a Mac (locationspec's
// "locationAccess" and "locationBackground"), with Core Location's
// CLLocationManager as on iOS before 17: region monitoring
// (CLCircularRegion), 20 regions at most, while the app runs (macOS
// doesn't start an app for a region). The background updates are
// computed by the Go package from the positions, as a Mac app runs out of
// sight anyway. The regions are kept in the app's data, with what each
// reports; the events in memory until Go reports each done.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["locationAccess"] = h.locationAccessModule()
		m["locationBackground"] = h.locationRegionsModule()
		// Regions monitored at a launch before: the delegate hears them.
		if len(h.regions().list) > 0 {
			h.locationManager()
		}
	})
}

// Core Location's CLAuthorizationStatus.
const (
	clRestricted = 1
	clDenied     = 2
)

type regionState struct {
	loaded  bool
	list    []spec.Region
	inside  map[string]bool
	initial map[string]bool // just added: is the device inside?
	events  []spec.Event
	handed  map[string]bool
	waiting []func([]byte, error)
	nextID  int

	accessWaits []accessWait
	answering   []func()
}

type accessWait struct {
	last  []byte
	reply func([]byte, error)
}

var msgInitRegion func(obj ID, sel SEL, c coordinate, radius float64, id ID) ID

func (h *Host) regions() *regionState {
	r := &h.locRegions
	if r.loaded {
		return r
	}
	r.loaded = true
	r.inside, r.initial, r.handed = map[string]bool{}, map[string]bool{}, map[string]bool{}
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

func regionsFile() string { return filepath.Join(appdir.Data(), "asuka-location-regions.json") }

func (h *Host) saveRegions() {
	r := h.regions()
	b, _ := json.Marshal(map[string]any{"regions": r.list, "inside": r.inside})
	os.MkdirAll(appdir.Data(), 0o755)
	os.WriteFile(regionsFile(), b, 0o644)
}

// accessState is the authorization on the wire (locationspec.Access).
func (h *Host) accessState() []byte {
	m := h.locationManager()
	level := spec.NotAsked
	switch SendInt(m, "authorizationStatus") {
	case clRestricted:
		level = spec.Restricted
	case clDenied:
		level = spec.Denied
	case clAuthorized:
		level = spec.Always // a Mac app runs out of sight: "authorized" is Always
	case clWhenInUse:
		level = spec.WhenInUse
	}
	precise := SendInt(m, "accuracyAuthorization") == 0 // CLAccuracyAuthorizationFullAccuracy
	on := SendBool(Class("CLLocationManager"), "locationServicesEnabled")
	return spec.Access(level, precise, !on)
}

// accessChanged answers the followers whose last state isn't the state
// now, and the requests waiting for an answer.
func (h *Host) accessChanged() {
	r := h.regions()
	s := h.accessState()
	var keep []accessWait
	for _, w := range r.accessWaits {
		if string(w.last) != string(s) {
			w.reply(s, nil)
		} else {
			keep = append(keep, w)
		}
	}
	r.accessWaits = keep
	if SendInt(h.locationManager(), "authorizationStatus") != clNotDetermined {
		as := r.answering
		r.answering = nil
		for _, a := range as {
			a()
		}
	}
}

// requestAccess asks for level (spec.WhenInUse, spec.Always), then calls
// done with the state: when the user answers, or within a second if no
// question shows (asked before).
func (h *Host) requestAccess(level byte, done func([]byte)) {
	m := h.locationManager()
	s := SendInt(m, "authorizationStatus")
	ask := ""
	switch {
	case s == clNotDetermined && level == spec.Always:
		ask = "requestAlwaysAuthorization"
	case s == clNotDetermined:
		ask = "requestWhenInUseAuthorization"
	case s == clWhenInUse && level == spec.Always:
		ask = "requestAlwaysAuthorization"
	}
	if ask == "" {
		done(h.accessState())
		return
	}
	r := h.regions()
	answered := false
	answer := func() {
		if !answered {
			answered = true
			done(h.accessState())
		}
	}
	r.answering = append(r.answering, answer)
	Send(m, ask)
	if s != clNotDetermined {
		time.AfterFunc(time.Second, func() { h.Post(answer) })
	}
}

func (h *Host) locationAccessModule() map[string]method {
	return map[string]method{
		"current": func(_ []byte, reply func([]byte, error)) { reply(h.accessState(), nil) },
		"next": func(last []byte, reply func([]byte, error)) {
			if s := h.accessState(); len(last) == 0 || string(last) != string(s) {
				reply(s, nil)
				return
			}
			r := h.regions()
			r.accessWaits = append(r.accessWaits, accessWait{append([]byte(nil), last...), reply})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			r := h.regions()
			ws := r.accessWaits
			r.accessWaits = nil
			for _, w := range ws {
				w.reply(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
		"request": func(args []byte, reply func([]byte, error)) {
			level := byte(spec.WhenInUse)
			if len(args) > 0 {
				level = args[0]
			}
			h.requestAccess(level, func(s []byte) { reply(s, nil) })
		},
		"precise": func(args []byte, reply func([]byte, error)) {
			m := h.locationManager()
			if s := SendInt(m, "authorizationStatus"); s != clAuthorized && s != clWhenInUse || SendInt(m, "accuracyAuthorization") == 0 {
				reply(h.accessState(), nil)
				return
			}
			key := string(args)
			if key == "" {
				key = "default"
			}
			var done objc.Block
			done = NewBlock(func(_ objc.Block, err ID) {
				h.Post(func() {
					done.Release()
					reply(h.accessState(), nil)
				})
			})
			Send(m, "requestTemporaryFullAccuracyAuthorizationWithPurposeKey:completion:", uintptr(String(key)), uintptr(done))
		},
		"settings": func(_ []byte, reply func([]byte, error)) {
			ws := Send(Class("NSWorkspace"), "sharedWorkspace")
			Send(ws, "openURL:", uintptr(URL("x-apple.systempreferences:com.apple.preference.security?Privacy_LocationServices")))
			reply(nil, nil)
		},
	}
}

const macRegionLimit = 20

func (h *Host) locationRegionsModule() map[string]method {
	notAvailable := func(_ []byte, reply func([]byte, error)) {
		reply(nil, errors.New("not available: computed by the Go code on a Mac"))
	}
	return map[string]method{
		"capabilities": func(_ []byte, reply func([]byte, error)) {
			b, _ := json.Marshal(map[string]any{"updates": false, "regions": true, "limit": macRegionLimit, "dwell": false, "relaunch": false,
				"maxRadius": MsgFloat(h.locationManager(), Sel("maximumRegionMonitoringDistance"))})
			reply(b, nil)
		},
		"start":    notAvailable,
		"stop":     notAvailable,
		"updating": func(_ []byte, reply func([]byte, error)) { reply([]byte("null"), nil) },
		"monitor": func(args []byte, reply func([]byte, error)) {
			var rg spec.Region
			if err := json.Unmarshal(args, &rg); err != nil {
				reply(nil, err)
				return
			}
			m := h.locationManager()
			if s := SendInt(m, "authorizationStatus"); s != clAuthorized && s != clWhenInUse {
				reply(nil, errors.New("always"))
				return
			}
			r := h.regions()
			rest := slices.DeleteFunc(slices.Clone(r.list), func(x spec.Region) bool { return x.ID == rg.ID })
			if len(rest) >= macRegionLimit {
				reply(nil, fmt.Errorf("too many regions (%d at most on macOS)", macRegionLimit))
				return
			}
			if max := MsgFloat(m, Sel("maximumRegionMonitoringDistance")); max > 0 && rg.Radius > max {
				reply(nil, fmt.Errorf("region %s: a radius of %.0f m is more than macOS's %.0f m", rg.ID, rg.Radius, max))
				return
			}
			if msgInitRegion == nil {
				purego.RegisterFunc(&msgInitRegion, MsgSendAddr())
			}
			c := msgInitRegion(Send(Class("CLCircularRegion"), "alloc"), Sel("initWithCenter:radius:identifier:"),
				coordinate{rg.Lat, rg.Lon}, rg.Radius, String(rg.ID))
			Send(c, "setNotifyOnEntry:", 1)
			Send(c, "setNotifyOnExit:", 1)
			r.list = append(rest, rg)
			delete(r.inside, rg.ID)
			r.initial[rg.ID] = true
			h.saveRegions()
			Send(m, "startMonitoringForRegion:", uintptr(c))
			Release(c)
			reply(nil, nil)
		},
		"unmonitor": func(args []byte, reply func([]byte, error)) {
			id := string(args)
			r := h.regions()
			m := h.locationManager()
			for _, c := range ArrayItems(Send(Send(m, "monitoredRegions"), "allObjects")) {
				if id == "" || GoString(Send(c, "identifier")) == id {
					Send(m, "stopMonitoringForRegion:", uintptr(c))
				}
			}
			r.list = slices.DeleteFunc(r.list, func(x spec.Region) bool { return id == "" || x.ID == id })
			for k := range r.inside {
				if id == "" || k == id {
					delete(r.inside, k)
				}
			}
			h.saveRegions()
			reply(nil, nil)
		},
		"regions": func(_ []byte, reply func([]byte, error)) {
			b, _ := json.Marshal(append([]spec.Region{}, h.regions().list...))
			reply(b, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			r := h.regions()
			r.waiting = append(r.waiting, reply)
			h.handRegionEvents()
		},
		"done": func(args []byte, reply func([]byte, error)) {
			r := h.regions()
			id := string(args)
			r.events = slices.DeleteFunc(r.events, func(e spec.Event) bool { return e.ID == id })
			delete(r.handed, id)
			reply(nil, nil)
		},
	}
}

func (h *Host) handRegionEvents() {
	r := h.regions()
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

// regionCrossed is a region's transition (spec.Enter, spec.Exit): an
// event, if it's one the region reports. From unknown to outside is none:
// the region was just added with the device out of it.
func (h *Host) regionCrossed(id string, kind int) {
	r := h.regions()
	i := slices.IndexFunc(r.list, func(x spec.Region) bool { return x.ID == id })
	if i < 0 {
		return
	}
	was, known := r.inside[id]
	in := kind == spec.Enter
	r.inside[id] = in
	h.saveRegions()
	if !known && !in || known && was == in || r.list[i].Events&kind == 0 {
		return
	}
	r.nextID++
	e := spec.Event{ID: "r" + strconv.Itoa(r.nextID), Region: id, Kind: kind, T: time.Now().UnixMilli()}
	if l := Send(h.locationManager(), "location"); l != 0 {
		var p spec.Position
		if json.Unmarshal(encodeLocation(l), &p) == nil {
			e.At = &p
		}
	}
	r.events = append(r.events, e)
	h.handRegionEvents()
}

// The delegate's region callbacks (classes.go).

func (h *Host) regionStarted(region ID) {
	if id := GoString(Send(region, "identifier")); h.regions().initial[id] {
		Send(h.locationManager(), "requestStateForRegion:", uintptr(region))
	}
}

func (h *Host) regionState(state int, region ID) {
	id := GoString(Send(region, "identifier"))
	r := h.regions()
	if !r.initial[id] {
		return
	}
	delete(r.initial, id)
	switch state {
	case 1: // CLRegionStateInside
		h.regionCrossed(id, spec.Enter)
	case 2: // CLRegionStateOutside
		h.regionCrossed(id, spec.Exit)
	}
}

func (h *Host) regionFailed(region, err ID) {
	if region == 0 {
		return
	}
	id := GoString(Send(region, "identifier"))
	r := h.regions()
	r.list = slices.DeleteFunc(r.list, func(x spec.Region) bool { return x.ID == id })
	delete(r.inside, id)
	delete(r.initial, id)
	h.saveRegions()
	fmt.Fprintf(os.Stderr, "asuka: location: region %s isn't monitored: %s\n", id, GoString(Send(err, "localizedDescription")))
}
