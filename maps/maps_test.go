package maps

import (
	"testing"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

func TestEncode(t *testing.T) {
	if s := encodeRegion(Region{Center: Coordinate{1.5, -2}, LatDelta: 0.1, LngDelta: 0.2}); s != `{"center":{"lat":1.5,"lng":-2},"latDelta":0.1,"lngDelta":0.2}` {
		t.Fatal(s)
	}
	s := encodeMarkers([]Marker{{ID: "a", At: Coordinate{1, 2}, Title: "A", Color: ui.Hex(0xff0000)}, {ID: "b"}})
	if s != `[{"id":"a","at":{"lat":1,"lng":2},"title":"A","subtitle":"","color":"#ff0000ff"},{"id":"b","at":{"lat":0,"lng":0},"title":"","subtitle":""}]` {
		t.Fatal(s)
	}
}

// The map is a native view; a changed region goes to it, as one prop.
func TestView(t *testing.T) {
	h := fake.New()
	region := signal.New(Region{Center: Coordinate{1, 2}, LatDelta: 0.1, LngDelta: 0.1})
	r := ui.Mount(h, func() ui.Node {
		return View(Props{Region: region, Style: ui.Style{Grow: 1}})
	})
	t.Cleanup(r.Dispose)
	r.Flush()
	var id = -1
	for vid, v := range h.Views {
		if v.Kind == ops.KindNative {
			id = int(vid)
		}
	}
	if id < 0 {
		t.Fatal("no native view")
	}
	h.Reset()
	region.Set(Region{Center: Coordinate{3, 4}, LatDelta: 0.1, LngDelta: 0.1})
	r.Flush()
	if len(h.Ops) != 1 {
		t.Fatalf("a region change: %d ops", len(h.Ops))
	}
}
