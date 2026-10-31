// Package maps shows a map (expo-maps): Apple Maps (MapKit) on iOS,
// Google Maps on Android, with markers, the user's location and the
// region the app moves it to.
//
// Android: Google's Maps SDK, which an app that imports this package is
// built with, and a Google Maps Platform API key (Maps SDK for Android)
// in ryui.json's android.mapsKey. ShowsUser needs the location
// permission: import ryui/location and ask for it (location.Current)
// first.
package maps

import (
	"encoding/json"
	"fmt"

	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// Coordinate is a place on Earth, in degrees.
type Coordinate struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// Region is what the map shows: its center, and how many degrees of
// latitude and longitude it spans (about 0.01 is a neighborhood, 0.1 a
// city, 10 a country).
type Region struct {
	Center   Coordinate `json:"center"`
	LatDelta float64    `json:"latDelta"`
	LngDelta float64    `json:"lngDelta"`
}

// Marker is a pin on the map.
type Marker struct {
	// ID names it, for OnMarkerPress.
	ID       string     `json:"id"`
	At       Coordinate `json:"at"`
	Title    string     `json:"title"`    // in its callout (iOS) or info window (Android)
	Subtitle string     `json:"subtitle"` // under the title
	// Color tints it (ui.RGB, ui.Hex; default: the platform's red).
	Color ui.Color `json:"-"`
}

// Type is the map's look.
type Type uint8

const (
	Standard  Type = iota // streets
	Satellite             // imagery
	Hybrid                // imagery with streets
	Muted                 // streets in quieter colors (iOS); Standard elsewhere
)

// Props are the map's.
type Props struct {
	// Region is where the map shows; changing it moves the map there
	// (animated). nil: the platform's (the whole country, or the world).
	Region signal.Accessor[Region]
	// Markers are its pins.
	Markers signal.Accessor[[]Marker]
	Type    Type
	// ShowsUser shows the user's location, with the permission.
	ShowsUser bool
	// NoInteraction: the user can't move or zoom it (a preview).
	NoInteraction bool
	// OnRegionChange gets the region once the user has moved the map
	// (not as it moves).
	OnRegionChange func(Region)
	// OnMarkerPress gets the ID of a marker the user taps.
	OnMarkerPress func(id string)
	// OnPress gets where the user taps the map (not a marker).
	OnPress func(Coordinate)
	// OnError gets why the map can't show (Android: no API key).
	OnError func(message string)
	// Style is the map's box: give it a size, or Grow.
	Style ui.Styler
}

// marker is a Marker on the wire, its color as #rrggbbaa.
type marker struct {
	Marker
	Color string `json:"color,omitempty"`
}

// View is a map.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func View(p Props) ui.Node {
	np := ui.NativeProps{Style: p.Style, Props: map[string]any{
		"type": int(p.Type), "showsUser": p.ShowsUser, "interactive": !p.NoInteraction,
	}, On: map[string]func(ui.NativeEvent){}}
	if p.Region != nil {
		np.Props["region"] = signal.Func(func() string { return encodeRegion(p.Region.Get()) })
	}
	if p.Markers != nil {
		np.Props["markers"] = signal.Func(func() string { return encodeMarkers(p.Markers.Get()) })
	}
	if p.OnRegionChange != nil {
		np.On["region"] = func(e ui.NativeEvent) {
			p.OnRegionChange(Region{Center: Coordinate{e.Float("lat"), e.Float("lng")}, LatDelta: e.Float("latDelta"), LngDelta: e.Float("lngDelta")})
		}
	}
	if p.OnMarkerPress != nil {
		np.On["marker"] = func(e ui.NativeEvent) { p.OnMarkerPress(e.String("id")) }
	}
	if p.OnPress != nil {
		np.On["press"] = func(e ui.NativeEvent) { p.OnPress(Coordinate{e.Float("lat"), e.Float("lng")}) }
	}
	if p.OnError != nil {
		np.On["error"] = func(e ui.NativeEvent) { p.OnError(e.String("message")) }
	}
	return ui.Native("ryui.map", np)
}

func encodeRegion(r Region) string {
	b, _ := json.Marshal(r)
	return string(b)
}

func encodeMarkers(ms []Marker) string {
	out := make([]marker, len(ms))
	for i, m := range ms {
		out[i] = marker{Marker: m}
		if r, g, b, a, ok := m.Color.RGBA(); ok {
			out[i].Color = fmt.Sprintf("#%02x%02x%02x%02x", r, g, b, a)
		}
	}
	b, _ := json.Marshal(out)
	return string(b)
}
