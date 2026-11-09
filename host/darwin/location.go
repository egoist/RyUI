//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
)

// ryui/location on a Mac, with Core Location as on iOS: the position now
// ("current"), each next one while watching ("next", "stop"), a place's
// name ("name"). Replies are JSON: lat, lon, acc (meters), alt, t (Unix
// ms); "denied" fails when the user hasn't allowed it. macOS asks the
// user once, with the app's NSLocationUsageDescription (ryui.json's
// ios.usage), which needs the app's bundle (ryui build -platform mac).

type locator struct {
	manager     ID
	authorizing []func()
	current     []func([]byte, error)
	next        []func([]byte, error)
	unseen      []byte // the newest, not yet given to a next
	updating    bool
}

type coordinate struct{ Lat, Lon float64 }

var msgCoordinate func(obj ID, sel SEL) coordinate

func (h *Host) locationManager() ID {
	l := &h.loc
	if l.manager == 0 {
		Dlopen("/System/Library/Frameworks/CoreLocation.framework/CoreLocation")
		purego.RegisterFunc(&msgCoordinate, MsgSendAddr())
		l.manager = New("CLLocationManager")
		Send(l.manager, "setDelegate:", uintptr(h.target))
		MsgSetFloat(l.manager, Sel("setDesiredAccuracy:"), -1) // kCLLocationAccuracyBest
	}
	return l.manager
}

// Authorization statuses.
const (
	clNotDetermined = 0
	clAuthorized    = 3 // authorizedAlways (macOS's "authorized")
	clWhenInUse     = 4
)

func (h *Host) locationAllowed() bool {
	s := SendInt(h.locationManager(), "authorizationStatus")
	return s == clAuthorized || s == clWhenInUse
}

// authorizeLocation asks the first time, then calls then with whether
// it's allowed.
func (h *Host) authorizeLocation(then func(bool)) {
	m := h.locationManager()
	if SendInt(m, "authorizationStatus") == clNotDetermined {
		h.loc.authorizing = append(h.loc.authorizing, func() { then(h.locationAllowed()) })
		Send(m, "requestWhenInUseAuthorization")
		return
	}
	then(h.locationAllowed())
}

func encodeLocation(l ID) []byte {
	c := msgCoordinate(l, Sel("coordinate"))
	alt := 0.0
	if MsgFloat(l, Sel("verticalAccuracy")) >= 0 {
		alt = MsgFloat(l, Sel("altitude"))
	}
	b, _ := json.Marshal(map[string]any{
		"lat": c.Lat, "lon": c.Lon, "acc": MsgFloat(l, Sel("horizontalAccuracy")), "alt": alt,
		"t": int64(MsgFloat(Send(l, "timestamp"), Sel("timeIntervalSince1970")) * 1000),
	})
	return b
}

func (h *Host) locationModule() map[string]method {
	l := &h.loc
	denied := errors.New("denied")
	return map[string]method{
		"current": func(_ []byte, reply func([]byte, error)) {
			h.authorizeLocation(func(ok bool) {
				if !ok {
					reply(nil, denied)
					return
				}
				l.current = append(l.current, reply)
				Send(l.manager, "requestLocation")
			})
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			h.authorizeLocation(func(ok bool) {
				if !ok {
					reply(nil, denied)
					return
				}
				if u := l.unseen; u != nil {
					l.unseen = nil
					reply(u, nil)
				} else {
					l.next = append(l.next, reply)
				}
				if !l.updating {
					l.updating = true
					Send(l.manager, "startUpdatingLocation")
				}
			})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			l.updating = false
			l.unseen = nil
			if l.manager != 0 {
				Send(l.manager, "stopUpdatingLocation")
			}
			ws := l.next
			l.next = nil
			for _, w := range ws {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
		"name": func(args []byte, reply func([]byte, error)) {
			var o struct{ Lat, Lon float64 }
			json.Unmarshal(args, &o)
			h.locationManager()
			loc := MsgFloat2ID(Send(Class("CLLocation"), "alloc"), Sel("initWithLatitude:longitude:"), o.Lat, o.Lon)
			g := New("CLGeocoder")
			var done objc.Block
			done = NewBlock(func(_ objc.Block, places, err ID) {
				var name, msg string
				if ps := ArrayItems(places); len(ps) > 0 {
					p := ps[0]
					var parts []string
					seen := map[string]bool{}
					for _, k := range [][]string{{"subLocality", "name"}, {"locality", "administrativeArea", "country"}} {
						for _, sel := range k {
							if v := GoString(Send(p, sel)); v != "" {
								if !seen[v] {
									seen[v] = true
									parts = append(parts, v)
								}
								break
							}
						}
					}
					name = strings.Join(parts, ", ")
				} else if err != 0 {
					msg = GoString(Send(err, "localizedDescription"))
				} else {
					msg = "no name for this place"
				}
				h.Post(func() {
					done.Release()
					Release(g)
					Release(loc)
					if msg != "" {
						reply(nil, errors.New(msg))
						return
					}
					reply([]byte(name), nil)
				})
			})
			Send(g, "reverseGeocodeLocation:completionHandler:", uintptr(loc), uintptr(done))
		},
	}
}

// locationAuthorized is the delegate's authorization change.
func (h *Host) locationAuthorized() {
	l := &h.loc
	if l.manager == 0 || SendInt(l.manager, "authorizationStatus") == clNotDetermined {
		return
	}
	ws := l.authorizing
	l.authorizing = nil
	for _, w := range ws {
		w()
	}
}

// locationsUpdated is the delegate's new locations.
func (h *Host) locationsUpdated(locations ID) {
	ls := ArrayItems(locations)
	if len(ls) == 0 {
		return
	}
	l := &h.loc
	data := encodeLocation(ls[len(ls)-1])
	cs := l.current
	l.current = nil
	for _, c := range cs {
		c(data, nil)
	}
	if !l.updating {
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

// locationFailed is the delegate's error: unknown yet (Core Location
// keeps trying), denied, or another.
func (h *Host) locationFailed(err ID) {
	code := SendInt(err, "code")
	if code == 0 { // kCLErrorLocationUnknown
		return
	}
	e := errors.New("denied")
	if code != 1 {
		e = errors.New(GoString(Send(err, "localizedDescription")))
	}
	l := &h.loc
	cs := l.current
	l.current = nil
	for _, c := range cs {
		c(nil, e)
	}
}
