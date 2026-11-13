// Web Crash is a web e2e's app (e2e/webcrash_test.go): crash reports
// (a Go panic, read on the next launch; an uncaught JavaScript error,
// at once), a map of OpenStreetMap's tiles, and a native view no script
// registers, each on a line the test reads.
package main

import (
	"fmt"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/sdk/crash"
	"github.com/egoist/asuka/sdk/maps"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func App() ui.Node {
	goReport := signal.New("go: none")
	jsReport := signal.New("exception: none")
	mapLine := signal.New("map: none")
	region := signal.New("region: none")
	crash.OnReport(func(r crash.Report) {
		switch r.Kind {
		case crash.Go:
			fn := ""
			if len(r.Threads) > 0 && len(r.Threads[0].Frames) > 0 {
				fn = r.Threads[0].Frames[0].Function
			}
			goReport.Set(fmt.Sprintf("go: %v %s | %s | %s %s", r.Fatal, r.Message, fn, r.Context.System, r.Context.SystemVersion))
		case crash.Exception:
			jsReport.Set(fmt.Sprintf("exception: %v %s | crumbs %d", r.Fatal, r.Message, len(r.Context.Breadcrumbs)))
		}
	})
	crash.Breadcrumb("started")
	paris := maps.Coordinate{Lat: 48.8584, Lng: 2.2945}
	return ui.SafeArea(ui.Style{Padding: ui.All(12), Gap: 6},
		ui.StaticText("status: ready"),
		ui.Text(goReport),
		ui.Text(jsReport),
		ui.Text(mapLine),
		ui.Text(region),
		ui.Native("Missing", ui.NativeProps{Props: map[string]any{"value": 1}}),
		ui.StaticText("after: native"),
		ui.Button(ui.ButtonProps{Title: ui.Val("Panic"), OnPress: func() {
			var m map[string]int
			m["boom"]++ // a nil map: panics
		}}),
		maps.View(maps.Props{
			Region: signal.New(maps.Region{Center: paris, LatDelta: 0.02, LngDelta: 0.02}),
			Markers: signal.Func(func() []maps.Marker {
				return []maps.Marker{{ID: "eiffel", At: paris, Title: "Eiffel Tower", Subtitle: "Paris"}}
			}),
			OnMarkerPress: func(id string) { mapLine.Set("map: marker " + id) },
			OnPress: func(c maps.Coordinate) {
				mapLine.Set(fmt.Sprintf("map: press %v", c.Lat > 48 && c.Lat < 50 && c.Lng > 2 && c.Lng < 3))
			},
			OnRegionChange: func(r maps.Region) {
				region.Set(fmt.Sprintf("region: moved %v", r.Center != paris && r.LatDelta > 0 && r.LngDelta > 0))
			},
			OnError: func(m string) { mapLine.Set("map: error " + m) },
			Style:   ui.Style{Height: ui.Pt(320)},
		}),
	)
}

func main() { app.Run(App) }
