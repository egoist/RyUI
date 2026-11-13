//go:build linux && !android

package linux

import (
	"encoding/json"
	"math"
	"strings"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/native"
)

// maps.View on Linux: libshumate's ShumateSimpleMap (GNOME Maps' map),
// showing OpenStreetMap's tiles; Satellite and Hybrid have no free
// imagery source: they show the streets too, as Muted does. Props:
// region and markers (JSON), type, showsUser (no location layer:
// nothing shown), interactive. Events: region (lat, lng, latDelta,
// lngDelta, once the user moved it), marker (id), press (lat, lng: a
// click off the markers), error (message: no libshumate, which the view
// then shows instead of a map). It has no size of its own: its style
// gives it one.

func init() {
	builtinViews["ryui.map"] = func(ctx native.Context) native.View { return newMapView(theHost, ctx) }
}

type mapRegion struct {
	Center struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"center"`
	LatDelta float64 `json:"latDelta"`
	LngDelta float64 `json:"lngDelta"`
}

type mapMarker struct {
	ID       string `json:"id"`
	At       struct{ Lat, Lng float64 }
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Color    string `json:"color"`
}

type mapView struct {
	h      *Host
	ctx    native.Context
	widget Ptr // the ShumateSimpleMap, or the placeholder
	sm     Ptr
	m      Ptr // its ShumateMap
	vp     Ptr // its viewport
	layer  Ptr // the markers' layer

	pending     *mapRegion // a region waiting for the map's size
	ticking     bool
	setting     bool // the region's being set: not the user's move
	reporting   bool // a region report waits for the loop
	markerHit   bool // a marker took this click
	markers     []mapMarker
	interactive bool
	destroyed   bool
}

func newMapView(h *Host, ctx native.Context) native.View {
	v := &mapView{h: h, ctx: ctx, interactive: true}
	if err := LoadShumate(); err != nil {
		l := LabelNew("Maps need libshumate")
		WidgetAddCSSClass(l, "dim-label")
		LabelSetWrap(l, true)
		v.widget = l
		msg := err.Error()
		h.Post(func() {
			if !v.destroyed {
				ctx.Emit("error", map[string]any{"message": msg})
			}
		})
		return v
	}
	s := &Shumate
	v.sm = s.SimpleMapNew()
	v.widget = v.sm
	reg := s.RegistryNewWithDefaults()
	if src := s.RegistryGetByID(reg, "osm-mapnik"); src != 0 {
		s.SimpleMapSetMapSource(v.sm, src)
	}
	ObjectUnref(reg)
	s.SimpleMapSetShowZoom(v.sm, false)
	v.m = s.SimpleMapGetMap(v.sm)
	v.vp = s.SimpleMapGetViewport(v.sm)
	v.layer = s.MarkerLayerNew(v.vp)
	s.SimpleMapAddOverlay(v.sm, v.layer)
	for _, p := range []string{"notify::latitude", "notify::longitude", "notify::zoom-level"} {
		Connect(v.vp, p, func(uintptr) { v.moved() })
	}
	click := GestureClickNew()
	Connect(click, "released", func(n int, x, y float64) {
		if v.markerHit {
			v.markerHit = false
			return
		}
		if n != 1 || !v.interactive {
			return
		}
		var lat, lng float64
		s.ViewportWidgetToLoc(v.vp, v.m, x, y, &lat, &lng)
		ctx.Emit("press", map[string]any{"lat": lat, "lng": lng})
	})
	WidgetAddController(v.m, click)
	return v
}

func (v *mapView) Handle() uintptr { return v.widget }

// Measure: none of its own; its style gives it a size.
func (v *mapView) Measure(w, h float32) (float32, float32) {
	if w != w {
		w = 0
	}
	if h != h {
		h = 0
	}
	return w, h
}

// Set implements native.View.
func (v *mapView) Set(name string, value any) {
	if v.sm == 0 {
		return
	}
	switch name {
	case "region":
		s, _ := value.(string)
		var r mapRegion
		if json.Unmarshal([]byte(s), &r) != nil {
			return
		}
		v.pending = &r
		v.applyRegion()
	case "markers":
		s, _ := value.(string)
		var ms []mapMarker
		json.Unmarshal([]byte(s), &ms)
		v.setMarkers(ms)
	case "interactive":
		on, _ := value.(bool)
		v.interactive = on
		Dev.WidgetSetCanTarget(v.sm, on)
	}
}

// applyRegion centers the map on the pending region, zoomed to show
// it, once the map has a size.
func (v *mapView) applyRegion() {
	r := v.pending
	if r == nil {
		return
	}
	w, ht := float64(Call(FnWidgetGetWidth, v.m)), float64(Call(FnWidgetGetHeight, v.m))
	if w <= 0 || ht <= 0 {
		if !v.ticking {
			v.ticking = true
			AddTickCallback(v.sm, func(Ptr) bool {
				if v.destroyed || v.pending == nil {
					v.ticking = false
					return false
				}
				if Call(FnWidgetGetWidth, v.m) > 0 {
					v.ticking = false
					v.applyRegion()
					return false
				}
				return true
			})
		}
		return
	}
	v.pending = nil
	zoom := 20.0
	if r.LngDelta > 0 {
		zoom = min(zoom, math.Log2(w*360/(256*r.LngDelta)))
	}
	if r.LatDelta > 0 {
		merc := func(lat float64) float64 {
			lat = max(min(lat, 85), -85) * math.Pi / 180
			return math.Log(math.Tan(math.Pi/4 + lat/2))
		}
		span := math.Abs(merc(r.Center.Lat+r.LatDelta/2) - merc(r.Center.Lat-r.LatDelta/2))
		if span > 0 {
			zoom = min(zoom, math.Log2(ht*2*math.Pi/(256*span)))
		}
	}
	v.setting = true
	Shumate.ViewportSetZoom(v.vp, max(zoom, 0))
	Shumate.MapCenterOn(v.m, r.Center.Lat, r.Center.Lng)
	v.setting = false
}

// moved reports the region, once the loop's idle, when the user moved
// the map.
func (v *mapView) moved() {
	if v.setting || v.reporting || v.destroyed {
		return
	}
	v.reporting = true
	v.h.Post(func() {
		v.reporting = false
		if v.destroyed {
			return
		}
		s := &Shumate
		w, ht := float64(Call(FnWidgetGetWidth, v.m)), float64(Call(FnWidgetGetHeight, v.m))
		var lat0, lng0, lat1, lng1 float64
		s.ViewportWidgetToLoc(v.vp, v.m, 0, 0, &lat0, &lng0)
		s.ViewportWidgetToLoc(v.vp, v.m, w, ht, &lat1, &lng1)
		dl := lng1 - lng0
		if dl < 0 {
			dl += 360
		}
		v.ctx.Emit("region", map[string]any{
			"lat": s.LocationGetLatitude(v.vp), "lng": s.LocationGetLongitude(v.vp),
			"latDelta": math.Abs(lat0 - lat1), "lngDelta": dl,
		})
	})
}

// setMarkers shows the markers: a pin each (its color, if set), its
// title and subtitle its tooltip; a click reports its ID.
func (v *mapView) setMarkers(ms []mapMarker) {
	s := &Shumate
	s.MarkerLayerRemoveAll(v.layer)
	v.markers = ms
	for _, mk := range ms {
		marker := s.MarkerNew()
		pin := ImageNewFromIconName("mark-location-symbolic")
		ImageSetPixelSize(pin, 32)
		if mk.Color != "" {
			v.h.colorPin(pin, mk.Color)
		} else {
			WidgetAddCSSClass(pin, "accent")
		}
		if tip := strings.TrimSpace(mk.Title + "\n" + mk.Subtitle); tip != "" {
			WidgetSetTooltipText(pin, tip)
		}
		id := mk.ID
		g := GestureClickNew()
		Connect(g, "released", func(n int, x, y float64) {
			v.markerHit = true
			v.ctx.Emit("marker", map[string]any{"id": id})
		})
		WidgetAddController(pin, g)
		s.MarkerSetChild(marker, pin)
		s.LocationSetLocation(marker, mk.At.Lat, mk.At.Lng)
		s.MarkerLayerAdd(v.layer, marker)
	}
}

// colorPin colors a pin "#rrggbbaa" (a CSS class of its own).
func (h *Host) colorPin(pin Ptr, color string) {
	c := strings.TrimPrefix(color, "#")
	if len(c) != 8 {
		return
	}
	class := "ryui-pin-" + c
	if !mapPinClasses[class] {
		mapPinClasses[class] = true
		mapPinCSS += "." + class + " { color: #" + c + "; }\n"
		if d := DisplayGetDefault(); d != 0 {
			if mapPinProvider == 0 {
				mapPinProvider = CSSProviderNew()
				StyleContextAddProviderForDisplay(d, mapPinProvider, 700)
			}
			CSSProviderLoadFromString(mapPinProvider, mapPinCSS)
		}
	}
	WidgetAddCSSClass(pin, class)
}

var (
	mapPinClasses  = map[string]bool{}
	mapPinCSS      string
	mapPinProvider Ptr
)

func (v *mapView) Destroy() { v.destroyed = true }
