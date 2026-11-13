//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"math"
	"strconv"
	"sync"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/native"
)

// maps.View on a Mac: an MKMapView, as on iOS. Props: region and markers
// (JSON), type (standard, satellite, hybrid, muted), showsUser,
// interactive. Events: region (lat, lng, latDelta, lngDelta, once the
// user moved it: a drag, a scroll, a pinch or a double click), marker
// (id), press (lat, lng: a click off the markers), error (message, when
// the map can't load). It has no size of its own: its style gives it one.

type mapRegion struct{ Lat, Lng, LatDelta, LngDelta float64 }

var (
	msgMapRegion    func(obj ID, sel SEL) mapRegion
	msgSetMapRegion func(obj ID, sel SEL, r mapRegion, animated bool)
	msgSetCoord     func(obj ID, sel SEL, c coordinate)
	msgPointToCoord func(obj ID, sel SEL, p Point, view ID) coordinate
	mapsOnce        sync.Once
	mapsOK          bool
)

func loadMaps() bool {
	mapsOnce.Do(func() {
		if framework("MapKit") == 0 || !HasClass("MKMapView") {
			return
		}
		framework("CoreLocation")
		purego.RegisterFunc(&msgMapRegion, MsgSendStretAddr())
		purego.RegisterFunc(&msgSetMapRegion, MsgSendAddr())
		purego.RegisterFunc(&msgSetCoord, MsgSendAddr())
		purego.RegisterFunc(&msgPointToCoord, MsgSendAddr())
		if msgCoordinate == nil {
			purego.RegisterFunc(&msgCoordinate, MsgSendAddr())
		}
		DefineClass("AsukaMapDelegate", "NSObject", []string{"MKMapViewDelegate", "NSGestureRecognizerDelegate"}, []objc.MethodDef{
			Method("mapView:regionWillChangeAnimated:", func(self ID, _ SEL, m ID, animated bool) {
				if v := mapViews[m]; v != nil {
					v.userMoving = userEventNow()
				}
			}),
			Method("mapView:regionDidChangeAnimated:", func(self ID, _ SEL, m ID, animated bool) {
				if v := mapViews[m]; v != nil {
					v.regionChanged()
				}
			}),
			Method("mapView:viewForAnnotation:", func(self ID, _ SEL, m, a ID) ID {
				if v := mapViews[m]; v != nil {
					return v.viewFor(a)
				}
				return 0
			}),
			Method("mapView:didSelectAnnotationView:", func(self ID, _ SEL, m, av ID) {
				if v := mapViews[m]; v != nil {
					if mk := v.markers[Send(av, "annotation")]; mk != nil {
						v.ctx.Emit("marker", map[string]any{"id": mk.ID})
					}
				}
			}),
			Method("mapViewDidFailLoadingMap:withError:", func(self ID, _ SEL, m, err ID) {
				if v := mapViews[m]; v != nil {
					v.ctx.Emit("error", map[string]any{"message": errorText(err, "the map couldn't load").Error()})
				}
			}),
			Method("clicked:", func(self ID, _ SEL, g ID) {
				if v := mapViews[Send(g, "view")]; v != nil {
					v.clicked(g)
				}
			}),
			Method("gestureRecognizer:shouldRecognizeSimultaneouslyWithGestureRecognizer:", func(self ID, _ SEL, a, b ID) bool { return true }),
		})
		mapDelegate = Send(Class("AsukaMapDelegate"), "new")
		mapsOK = true
	})
	return mapsOK
}

var (
	mapDelegate ID
	mapViews    = map[ID]*mapView{} // by their MKMapView
)

// userEventNow reports whether the event being handled is the user's
// moving the map: the mouse dragged, a scroll, a pinch, a rotation, a
// click (a double click zooms).
func userEventNow() bool {
	ev := Send(Send(Class("NSApplication"), "sharedApplication"), "currentEvent")
	if ev == 0 {
		return false
	}
	switch SendInt(ev, "type") {
	case 1, 2, 6, 22, 29, 30, 18, 31, 32: // mouse down, up, dragged; scroll wheel; gesture, magnify, rotate, swipe, smart magnify
		return true
	}
	return false
}

type mapMarker struct {
	ID       string `json:"id"`
	At       struct{ Lat, Lng float64 }
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Color    string `json:"color"`
}

type mapView struct {
	ctx        native.Context
	view       ID
	click      ID
	markers    map[ID]*mapMarker // by their MKPointAnnotation
	userMoving bool
}

func init() {
	builtinViews["asuka.map"] = newMapView
}

func newMapView(ctx native.Context) native.View {
	if !loadMaps() {
		ctx.Emit("error", map[string]any{"message": "MapKit isn't on this Mac"})
		return nil
	}
	v := &mapView{ctx: ctx, markers: map[ID]*mapMarker{}}
	v.view = MsgInitRect(Send(Class("MKMapView"), "alloc"), Sel("initWithFrame:"), Rect{Size: Size{Width: 300, Height: 200}})
	Send(v.view, "setDelegate:", uintptr(mapDelegate))
	v.click = Send(Send(Class("NSClickGestureRecognizer"), "alloc"), "initWithTarget:action:", uintptr(mapDelegate), uintptr(Sel("clicked:")))
	Send(v.click, "setDelegate:", uintptr(mapDelegate))
	Send(v.click, "setDelaysPrimaryMouseButtonEvents:", 0)
	Send(v.view, "addGestureRecognizer:", uintptr(v.click))
	mapViews[v.view] = v
	return v
}

func (v *mapView) Handle() uintptr { return uintptr(v.view) }

// Measure: none of its own.
func (v *mapView) Measure(_, _ float32) (float32, float32) { return 0, 0 }

func (v *mapView) Destroy() {
	delete(mapViews, v.view)
	Send(v.view, "setDelegate:", 0)
	Send(v.view, "removeGestureRecognizer:", uintptr(v.click))
	Release(v.click)
	Release(v.view)
}

func (v *mapView) Set(name string, value any) {
	switch name {
	case "region":
		s, _ := value.(string)
		var o struct {
			Center             struct{ Lat, Lng float64 }
			LatDelta, LngDelta float64
		}
		if json.Unmarshal([]byte(s), &o) != nil {
			return
		}
		r := mapRegion{o.Center.Lat, o.Center.Lng, math.Max(orDefault(o.LatDelta, 0.05), 0.0005), math.Max(orDefault(o.LngDelta, 0.05), 0.0005)}
		// The region the user moved to, given back: already there.
		cur := msgMapRegion(v.view, Sel("region"))
		if math.Abs(cur.Lat-r.Lat) < 1e-6 && math.Abs(cur.Lng-r.Lng) < 1e-6 && math.Abs(cur.LatDelta-r.LatDelta) < 1e-6 {
			return
		}
		msgSetMapRegion(v.view, Sel("setRegion:animated:"), r, Send(v.view, "window") != 0)
	case "markers":
		s, _ := value.(string)
		var list []*mapMarker
		json.Unmarshal([]byte(s), &list)
		v.setMarkers(list)
	case "type":
		n, _ := value.(int64)
		v.setType(int(n))
	case "showsUser":
		on, _ := value.(bool)
		if on {
			askLocationForMap()
		}
		Send(v.view, "setShowsUserLocation:", Bool(on))
	case "interactive":
		on, _ := value.(bool)
		for _, sel := range []string{"setScrollEnabled:", "setZoomEnabled:", "setRotateEnabled:", "setPitchEnabled:"} {
			Send(v.view, sel, Bool(on))
		}
		Send(v.click, "setEnabled:", Bool(on))
	}
}

func orDefault(f, d float64) float64 {
	if f == 0 {
		return d
	}
	return f
}

// setType: standard, satellite, hybrid, muted (macOS 13's
// configurations; before, the map types, muted as standard).
func (v *mapView) setType(t int) {
	if HasClass("MKStandardMapConfiguration") && RespondsTo(v.view, "setPreferredConfiguration:") {
		var c ID
		switch t {
		case 1:
			c = New("MKImageryMapConfiguration")
		case 2:
			c = New("MKHybridMapConfiguration")
		case 3:
			c = Send(Send(Class("MKStandardMapConfiguration"), "alloc"), "initWithEmphasisStyle:", 1) // muted
		default:
			c = New("MKStandardMapConfiguration")
		}
		Send(v.view, "setPreferredConfiguration:", uintptr(c))
		Release(c)
		return
	}
	mt := uintptr(0)
	switch t {
	case 1:
		mt = 1
	case 2:
		mt = 2
	}
	Send(v.view, "setMapType:", mt)
}

func (v *mapView) setMarkers(list []*mapMarker) {
	old := make([]ID, 0, len(v.markers))
	for a := range v.markers {
		old = append(old, a)
	}
	if len(old) > 0 {
		Send(v.view, "removeAnnotations:", uintptr(Array(old...)))
		for _, a := range old {
			Release(a)
		}
	}
	clear(v.markers)
	adds := make([]ID, 0, len(list))
	for _, m := range list {
		a := New("MKPointAnnotation")
		msgSetCoord(a, Sel("setCoordinate:"), coordinate{m.At.Lat, m.At.Lng})
		if m.Title != "" {
			Send(a, "setTitle:", uintptr(String(m.Title)))
		}
		if m.Subtitle != "" {
			Send(a, "setSubtitle:", uintptr(String(m.Subtitle)))
		}
		v.markers[a] = m
		adds = append(adds, a)
	}
	if len(adds) > 0 {
		Send(v.view, "addAnnotations:", uintptr(Array(adds...)))
	}
}

// viewFor is a marker's view: a balloon in its color, its title under
// it (a Mac's marker views show no callout).
func (v *mapView) viewFor(a ID) ID {
	m := v.markers[a]
	if m == nil || !HasClass("MKMarkerAnnotationView") {
		return 0
	}
	av := Send(v.view, "dequeueReusableAnnotationViewWithIdentifier:", uintptr(String("asuka")))
	if av == 0 {
		av = Autorelease(Send(Send(Class("MKMarkerAnnotationView"), "alloc"), "initWithAnnotation:reuseIdentifier:", uintptr(a), uintptr(String("asuka"))))
	}
	Send(av, "setAnnotation:", uintptr(a))
	Send(av, "setMarkerTintColor:", uintptr(markerColor(m.Color)))
	Send(av, "setCanShowCallout:", Bool(m.Title != ""))
	return av
}

// markerColor is "#rrggbbaa" as an NSColor (0: the system's red).
func markerColor(s string) ID {
	if len(s) != 9 || s[0] != '#' {
		return 0
	}
	n, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return 0
	}
	c := func(shift uint) float64 { return float64(n>>shift&0xff) / 255 }
	return MsgColor(Class("NSColor"), Sel("colorWithSRGBRed:green:blue:alpha:"), c(24), c(16), c(8), c(0))
}

func (v *mapView) regionChanged() {
	moved := v.userMoving
	v.userMoving = false
	if !moved {
		return
	}
	r := msgMapRegion(v.view, Sel("region"))
	v.ctx.Emit("region", map[string]any{"lat": r.Lat, "lng": r.Lng, "latDelta": r.LatDelta, "lngDelta": r.LngDelta})
}

// clicked is a click on the map: a press where it isn't on a marker.
func (v *mapView) clicked(g ID) {
	p := MsgIDToPoint(g, Sel("locationInView:"), v.view)
	for a := range v.markers {
		if av := Send(v.view, "viewForAnnotation:", uintptr(a)); av != 0 {
			f := MsgRect(av, Sel("frame"))
			if p.X >= f.Origin.X && p.X <= f.Origin.X+f.Size.Width && p.Y >= f.Origin.Y && p.Y <= f.Origin.Y+f.Size.Height {
				return
			}
		}
	}
	c := msgPointToCoord(v.view, Sel("convertPoint:toCoordinateFromView:"), p, v.view)
	v.ctx.Emit("press", map[string]any{"lat": c.Lat, "lng": c.Lon})
}

// askLocationForMap asks for the location the first time the map shows
// the user, if the app says why (asuka/location's purpose string).
func askLocationForMap() {
	info := Send(Send(Class("NSBundle"), "mainBundle"), "infoDictionary")
	if info == 0 {
		return
	}
	has := func(k string) bool { return Send(info, "objectForKey:", uintptr(String(k))) != 0 }
	if !has("NSLocationUsageDescription") && !has("NSLocationWhenInUseUsageDescription") {
		return
	}
	if h := theHost; h != nil {
		m := h.locationManager()
		if SendInt(m, "authorizationStatus") == clNotDetermined {
			Send(m, "requestWhenInUseAuthorization")
		}
	}
}
