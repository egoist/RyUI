//go:build linux && !android

package linux

import (
	"encoding/json"
	"errors"
	"math"
	"os/exec"
	"strings"
	"time"

	"github.com/egoist/asuka/internal/locationspec"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sdk/location on Linux: the location portal (org.freedesktop.portal.
// Location, which asks the user and works in a sandbox), else GeoClue2
// on the system bus directly; a place's name through geocode-glib
// (GNOME's reverse geocoding), when it's installed. Replies are JSON as
// on the other hosts: lat, lon, acc (meters), alt, t (Unix ms); "denied"
// fails when the user (or GeoClue's agent) doesn't allow it.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["location"] = h.locationModule()
		m["locationAccess"] = h.locationAccessModule()
		m["locationBackground"] = locationBackgroundModule()
	})
}

// locator is the module's state. Main thread.
type locator struct {
	src      locSource // running: started for a current or while watching
	current  []func([]byte, error)
	next     []func([]byte, error)
	unseen   []byte // the newest, not yet given to a next
	updating bool   // watching ("next" until "stop")
	gen      int    // the source's: what an old one reports is dropped

	// The authorization, as this run has seen it: known once a position
	// came (allowed) or the portal or GeoClue refused.
	known, allowed bool
	accessWaits    []accessWait
}

type accessWait struct {
	last  []byte
	reply func([]byte, error)
}

// locSource is where positions come from: the portal's or GeoClue's.
type locSource interface{ stop() }

var loc locator

func (h *Host) locationModule() map[string]method {
	l := &loc
	return map[string]method{
		"current": func(_ []byte, reply func([]byte, error)) {
			l.current = append(l.current, reply)
			h.startLocation()
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			l.updating = true
			if u := l.unseen; u != nil {
				l.unseen = nil
				reply(u, nil)
			} else {
				l.next = append(l.next, reply)
			}
			h.startLocation()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			l.updating = false
			l.unseen = nil
			ws := l.next
			l.next = nil
			for _, w := range ws {
				w(nil, errors.New("stopped"))
			}
			if len(l.current) == 0 {
				h.stopLocation()
			}
			reply(nil, nil)
		},
		"name": func(args []byte, reply func([]byte, error)) {
			var o struct{ Lat, Lon float64 }
			if err := json.Unmarshal(args, &o); err != nil {
				reply(nil, errors.New("location.name: bad arguments"))
				return
			}
			GeocodeReverse(o.Lat, o.Lon, func(p GeocodePlace, err error) {
				if err != nil {
					reply(nil, err)
					return
				}
				reply([]byte(placeName(p)), nil)
			})
		},
	}
}

// placeName is a place's name as Core Location's is made: its area (or
// name), then its town (or state, or country).
func placeName(p GeocodePlace) string {
	var parts []string
	seen := map[string]bool{}
	for _, k := range [][]string{{p.Area, p.Name}, {p.Town, p.State, p.Country}} {
		for _, v := range k {
			if v != "" {
				if !seen[v] {
					seen[v] = true
					parts = append(parts, v)
				}
				break
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func (h *Host) startLocation() {
	l := &loc
	if l.src != nil {
		return
	}
	l.gen++
	gen := l.gen
	l.src = locStop(func() {}) // starting
	src := h.portalLocation(func(b []byte) {
		if loc.gen == gen {
			h.locationUpdate(b)
		}
	}, func(err error) {
		if loc.gen == gen {
			h.locationFailed(err)
		}
	})
	if loc.gen == gen && loc.src != nil {
		loc.src = src
	}
}

func (h *Host) stopLocation() {
	if s := loc.src; s != nil {
		loc.src = nil
		loc.gen++
		s.stop()
	}
}

// locationUpdate is a new position.
func (h *Host) locationUpdate(data []byte) {
	l := &loc
	h.accessSeen(true)
	cs := l.current
	l.current = nil
	for _, c := range cs {
		c(data, nil)
	}
	if !l.updating {
		h.stopLocation()
		return
	}
	if len(l.next) == 0 {
		l.unseen = data
		return
	}
	ws := l.next
	l.next = nil
	for _, w := range ws {
		w(data, nil)
	}
}

// locationFailed ends what waits: the source couldn't start.
func (h *Host) locationFailed(err error) {
	l := &loc
	if strings.Contains(err.Error(), "denied") {
		h.accessSeen(false)
	}
	if s := l.src; s != nil {
		l.src = nil
		l.gen++
		s.stop()
	}
	ws := append(l.current, l.next...)
	l.current, l.next = nil, nil
	for _, w := range ws {
		w(nil, err)
	}
}

// encodePosition is a position (the portal's or GeoClue's properties) as
// the module's JSON.
func encodePosition(p map[string]any) []byte {
	f := func(k string) float64 {
		v, _ := p[k].(float64)
		return v
	}
	alt := f("Altitude")
	if alt < -1e300 || math.IsNaN(alt) { // -DBL_MAX: not known
		alt = 0
	}
	t := time.Now().UnixMilli()
	if ts, ok := p["Timestamp"].([]any); ok && len(ts) == 2 {
		s, _ := ts[0].(uint64)
		us, _ := ts[1].(uint64)
		if s > 0 {
			t = int64(s)*1000 + int64(us)/1000
		}
	}
	// GeoClue's speed and heading are -1 when not known; the portal's too.
	known := func(k string) float64 {
		if v, ok := p[k].(float64); ok && v >= 0 && !math.IsNaN(v) {
			return v
		}
		return -1
	}
	b, _ := json.Marshal(map[string]any{"lat": f("Latitude"), "lon": f("Longitude"), "acc": f("Accuracy"), "alt": alt,
		"spd": known("Speed"), "crs": known("Heading"), "t": t})
	return b
}

// The portal's session.

type portalLoc struct {
	h       *Host
	conn    Ptr
	session string
	sub     uint32
	stopped bool
}

func (h *Host) portalLocation(update func([]byte), fail func(error)) locSource {
	s := &portalLoc{h: h}
	conn, err := Bus(BusSession)
	if err != nil {
		return h.geoclueLocation(update, fail)
	}
	s.conn = conn
	var fallback locSource
	// The session: positions come as LocationUpdated signals for it.
	opts := VDict(map[string]Ptr{
		"session_handle_token": VStr(portalToken()),
		"distance-threshold":   VU32(0),
		"time-threshold":       VU32(0),
		"accuracy":             VU32(5), // exact
	})
	DBusCall(conn, portalDest, portalPath, "org.freedesktop.portal.Location", "CreateSession", VTuple(opts), -1, func(r Ptr, err error) {
		if s.stopped {
			if err == nil {
				s.closeSession(r)
			}
			return
		}
		if err != nil {
			if errNoPortal(err) {
				// No portal: GeoClue itself.
				fallback = h.geoclueLocation(update, fail)
				return
			}
			if strings.Contains(err.Error(), "NotAllowed") { // location services off, or the app not allowed
				err = errors.New("denied")
			}
			fail(err)
			return
		}
		args, _ := VariantGo(r).([]any)
		if len(args) == 0 {
			fail(errors.New("location: a bad reply from the portal"))
			return
		}
		s.session, _ = args[0].(string)
		s.sub = DBusSubscribe(conn, portalDest, "org.freedesktop.portal.Location", "LocationUpdated", portalPath, func(sig DBusSignal) {
			a, _ := VariantGo(sig.Params).([]any)
			if len(a) < 2 || s.stopped {
				return
			}
			if p, _ := a[0].(string); p != s.session {
				return
			}
			pos, _ := a[1].(map[string]any)
			update(encodePosition(pos))
		})
		h.portalRequest("org.freedesktop.portal.Location", "Start", func(token string) Ptr {
			return VTuple(VPath(s.session), VStr(h.parentWindow()), VDict(map[string]Ptr{"handle_token": VStr(token)}))
		}, nil, func(code uint32, _ map[string]any, err error) {
			if s.stopped {
				return
			}
			switch {
			case err != nil:
				s.stop()
				fail(err)
			case code == 1:
				s.stop()
				fail(errors.New("denied"))
			case code != 0:
				s.stop()
				fail(errors.New("denied")) // the portal's other end: not allowed, or no location service
			}
		})
	})
	return locStop(func() {
		if fallback != nil {
			fallback.stop()
			return
		}
		s.stop()
	})
}

type locStop func()

func (f locStop) stop() { f() }

func (s *portalLoc) stop() {
	if s.stopped {
		return
	}
	s.stopped = true
	DBusUnsubscribe(s.conn, s.sub)
	if s.session != "" {
		DBusCall(s.conn, portalDest, s.session, "org.freedesktop.portal.Session", "Close", 0, -1, func(Ptr, error) {})
	}
}

func (s *portalLoc) closeSession(r Ptr) {
	if args, _ := VariantGo(r).([]any); len(args) > 0 {
		if p, _ := args[0].(string); p != "" {
			DBusCall(s.conn, portalDest, p, "org.freedesktop.portal.Session", "Close", 0, -1, func(Ptr, error) {})
		}
	}
}

// GeoClue2, without the portal.

const geoclue = "org.freedesktop.GeoClue2"

type geoclueLoc struct {
	conn    Ptr
	client  string
	sub     uint32
	stopped bool
}

func (h *Host) geoclueLocation(update func([]byte), fail func(error)) locSource {
	g := &geoclueLoc{}
	conn, err := Bus(BusSystem)
	if err != nil {
		fail(errors.New("location: no location service (" + err.Error() + ")"))
		return locStop(func() {})
	}
	g.conn = conn
	failed := func(err error) {
		if g.stopped {
			return
		}
		g.stop()
		if strings.Contains(err.Error(), "AccessDenied") {
			fail(errors.New("denied"))
			return
		}
		if errNoPortal(err) {
			fail(errors.New("location: no location service (GeoClue isn't installed)"))
			return
		}
		fail(err)
	}
	DBusCall(conn, geoclue, "/org/freedesktop/GeoClue2/Manager", geoclue+".Manager", "GetClient", 0, -1, func(r Ptr, err error) {
		if err != nil {
			failed(err)
			return
		}
		args, _ := VariantGo(r).([]any)
		if len(args) == 0 {
			failed(errors.New("location: a bad reply from GeoClue"))
			return
		}
		g.client, _ = args[0].(string)
		if g.stopped {
			g.stopClient()
			return
		}
		set := func(prop string, v Ptr, then func()) {
			DBusCall(conn, geoclue, g.client, "org.freedesktop.DBus.Properties", "Set",
				VTuple(VStr(geoclue+".Client"), VStr(prop), VVariant(v)), -1, func(_ Ptr, err error) {
					if err != nil {
						failed(err)
						return
					}
					then()
				})
		}
		g.sub = DBusSubscribe(conn, geoclue, geoclue+".Client", "LocationUpdated", g.client, func(sig DBusSignal) {
			a, _ := VariantGo(sig.Params).([]any)
			if len(a) < 2 || g.stopped {
				return
			}
			at, _ := a[1].(string)
			DBusCall(conn, geoclue, at, "org.freedesktop.DBus.Properties", "GetAll", VTuple(VStr(geoclue+".Location")), -1, func(r Ptr, err error) {
				if err != nil || g.stopped {
					return
				}
				if a, _ := VariantGo(r).([]any); len(a) > 0 {
					if pos, ok := a[0].(map[string]any); ok {
						update(encodePosition(pos))
					}
				}
			})
		})
		set("DesktopId", VStr(h.appID), func() {
			set("RequestedAccuracyLevel", VU32(8), func() { // exact
				DBusCall(conn, geoclue, g.client, geoclue+".Client", "Start", 0, -1, func(_ Ptr, err error) {
					if err != nil {
						failed(err)
					}
				})
			})
		})
	})
	return g
}

func (g *geoclueLoc) stop() {
	if g.stopped {
		return
	}
	g.stopped = true
	DBusUnsubscribe(g.conn, g.sub)
	g.stopClient()
}

func (g *geoclueLoc) stopClient() {
	if g.client != "" {
		DBusCall(g.conn, geoclue, g.client, geoclue+".Client", "Stop", 0, -1, func(Ptr, error) {})
	}
}

// asuka/sdk/location's authorization (locationspec's "locationAccess"):
// neither the portal nor GeoClue says it before an app asks for a
// position, so it's what this run has seen: NotAsked until a position
// comes (Always: a desktop app runs out of sight) or a refusal (Denied).
// "request" asks for a position to find out. Precise: the portal's
// accuracy is the app's to choose, not the user's.

func accessState() []byte {
	switch {
	case !loc.known:
		return locationspec.Access(locationspec.NotAsked, false, false)
	case loc.allowed:
		return locationspec.Access(locationspec.Always, true, false)
	}
	return locationspec.Access(locationspec.Denied, false, false)
}

// accessSeen notes what a position (or a refusal) says, and answers the
// followers.
func (h *Host) accessSeen(allowed bool) {
	l := &loc
	if l.known && l.allowed == allowed {
		return
	}
	l.known, l.allowed = true, allowed
	s := accessState()
	ws := l.accessWaits
	l.accessWaits = nil
	for _, w := range ws {
		if string(w.last) != string(s) {
			w.reply(s, nil)
		} else {
			l.accessWaits = append(l.accessWaits, w)
		}
	}
}

func (h *Host) locationAccessModule() map[string]method {
	l := &loc
	return map[string]method{
		"current": func(_ []byte, reply func([]byte, error)) { reply(accessState(), nil) },
		"next": func(last []byte, reply func([]byte, error)) {
			if s := accessState(); len(last) == 0 || string(last) != string(s) {
				reply(s, nil)
				return
			}
			l.accessWaits = append(l.accessWaits, accessWait{append([]byte(nil), last...), reply})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			ws := l.accessWaits
			l.accessWaits = nil
			for _, w := range ws {
				w.reply(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
		"request": func(_ []byte, reply func([]byte, error)) {
			if l.known {
				reply(accessState(), nil)
				return
			}
			l.current = append(l.current, func([]byte, error) { reply(accessState(), nil) })
			h.startLocation()
		},
		"precise": func(_ []byte, reply func([]byte, error)) { reply(accessState(), nil) },
		"settings": func(_ []byte, reply func([]byte, error)) {
			if err := exec.Command("gnome-control-center", "location").Start(); err != nil {
				reply(nil, errors.New("not available: no GNOME Settings"))
				return
			}
			reply(nil, nil)
		},
	}
}

// locationBackgroundModule says GeoClue has no geofences and no
// background updates: the Go package computes both from the positions
// while the app runs.
func locationBackgroundModule() map[string]method {
	computed := func(_ []byte, reply func([]byte, error)) {
		reply(nil, errors.New("not available: computed by the Go code on Linux"))
	}
	return map[string]method{
		"capabilities": func(_ []byte, reply func([]byte, error)) {
			b, _ := json.Marshal(locationspec.Capabilities{})
			reply(b, nil)
		},
		"start": computed, "stop": computed, "updating": computed, "monitor": computed,
		"unmonitor": computed, "regions": computed, "next": computed, "done": computed,
	}
}
