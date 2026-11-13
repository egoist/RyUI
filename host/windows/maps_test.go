//go:build windows

package windows

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/asuka/colors"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/sdk/maps"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// tileColor is a test tile's color: its zoom level's.
func tileColor(z int) color.NRGBA {
	return color.NRGBA{uint8(40 + 10*z), 120, uint8(200 - 5*z), 255}
}

// tileServer serves tiles of one color a level, counting the requests,
// their User-Agents and how many come at once.
type tileServer struct {
	*httptest.Server
	mu       sync.Mutex
	paths    []string
	agents   []string
	inFlight int
	most     int
	fail     bool
}

func newTileServer(t *testing.T) *tileServer {
	s := &tileServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.paths = append(s.paths, r.URL.Path)
		s.agents = append(s.agents, r.UserAgent())
		s.inFlight++
		s.most = max(s.most, s.inFlight)
		fail := s.fail
		s.mu.Unlock()
		defer func() { s.mu.Lock(); s.inFlight--; s.mu.Unlock() }()
		time.Sleep(5 * time.Millisecond) // overlapping downloads would be seen
		if fail {
			http.Error(w, "no", 500)
			return
		}
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		z, _ := strconv.Atoi(parts[0])
		img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 256, 256))
		c := tileColor(z)
		for i := 0; i < len(img.Pix); i += 4 {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
		}
		var b bytes.Buffer
		png.Encode(&b, img)
		w.Write(b.Bytes())
	}))
	t.Cleanup(s.Close)
	t.Setenv("ASUKA_MAP_TILES", s.URL+"/{z}/{x}/{y}.png")
	return s
}

func (s *tileServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.paths)
}

// mapPixels draws the view into a bitmap and gives its BGRA pixels.
func mapPixels(v *mapView) ([]byte, int32) {
	r := ClientRect(v.hwnd)
	dc := CreateCompatibleDC(0)
	defer DeleteDC(dc)
	bmp, bits := DIB(r.W(), r.H())
	defer DeleteObject(bmp)
	old := SelectObject(dc, bmp)
	v.paint(dc)
	SelectObject(dc, old)
	return append([]byte(nil), bits...), r.W()
}

// The map shows the region the app sets, of the tiles the server has
// (two downloads at a time, the request naming Asuka), its markers and
// the attribution; a drag moves it and reports the region, the wheel
// zooms it, a click on a pin reports the marker, one off them the place;
// tiles are kept on disk and not downloaded again.
func TestMapView(t *testing.T) {
	srv := newTileServer(t)
	saved := tiles
	tiles = newTileStore(t.TempDir(), "Asuka/1 (test)")
	defer func() { tiles = saved }()
	type ev struct{ name string }
	var events []ev
	region := signal.New(maps.Region{Center: maps.Coordinate{Lat: 37.7749, Lng: -122.4194}, LatDelta: 0.08, LngDelta: 0.08})
	h, _ := mount(t, func() ui.Node {
		return maps.View(maps.Props{
			Region: region,
			Markers: signal.Static([]maps.Marker{
				{ID: "ferry", At: maps.Coordinate{Lat: 37.7955, Lng: -122.3937}, Title: "Ferry Building", Subtitle: "Ferries"},
				{ID: "park", At: maps.Coordinate{Lat: 37.7694, Lng: -122.4862}, Title: "Golden Gate Park", Color: colors.Hex(0x2E7D32)},
			}),
			Style: ui.Style{Width: ui.Pt(400), Height: ui.Pt(300)},
		})
	})
	var v *mapView
	onMain(t, func() {
		for view := range tiles.views {
			v = view
		}
		if v == nil {
			fatalf("no map view")
		}
		// The events, straight from the view.
		v.ctx = native.NewContext(v.ctx.Parent(), func(name string, values map[string]any) {
			events = append(events, ev{name})
			lastEvent = values
		})
		mapPixels(v) // a hidden window isn't painted: the test paints it
		if !pumpUntil(func() bool { return srv.count() > 0 && len(tiles.want) == 0 && len(tiles.bmps) > 0 }) {
			fatalf("no tiles: %d asked, %d here", srv.count(), len(tiles.bmps))
		}
		mapPixels(v) // asks for what shows at the zoom now
		pumpUntil(func() bool { return len(tiles.want) == 0 })
		rg := v.region()
		lat, lng, dlat, dlng := rg["lat"].(float64), rg["lng"].(float64), rg["latDelta"].(float64), rg["lngDelta"].(float64)
		if math.Abs(lat-37.7749) > 1e-6 || math.Abs(lng+122.4194) > 1e-6 || dlat < 0.0799 || dlng < 0.0799 || (math.Abs(dlat-0.08) > 1e-3 && math.Abs(dlng-0.08) > 1e-3) {
			fatalf("the region: %v", rg)
		}
		px, w := mapPixels(v)
		at := func(x, y int32) color.NRGBA {
			i := (y*w + x) * 4
			return color.NRGBA{px[i+2], px[i+1], px[i], 255}
		}
		z := int(math.Round(v.zoom + math.Log2(v.scale())))
		r := ClientRect(v.hwnd)
		if c := at(r.W()/4, r.H()/4); c != tileColor(z) {
			fatalf("the tiles: %v at zoom %d (%v), want %v", c, z, v.zoom, tileColor(z))
		}
		// The ferry's pin, red, just above its tip.
		fx, fy := v.point(37.7955, -122.3937)
		if c := at(int32(fx), int32(fy-10*v.scale())); c.R < 200 || c.G > 80 {
			fatalf("the ferry's pin: %v", c)
		}
		gx, gy := v.point(37.7694, -122.4862)
		if c := at(int32(gx), int32(gy-10*v.scale())); c.G < 100 || c.R > 80 {
			fatalf("the park's green pin: %v", c)
		}
		// The attribution: dark text on light, bottom right.
		a := v.attribution()
		dark, light := 0, 0
		for y := a.Top; y < a.Bottom; y++ {
			for x := a.Left; x < a.Right; x++ {
				c := at(x, y)
				switch {
				case c.R < 120 && c.G < 120 && c.B < 120:
					dark++
				case c.R > 180 && c.G > 180:
					light++
				}
			}
		}
		if dark < 30 || light < 300 {
			fatalf("the attribution: %d dark pixels, %d light", dark, light)
		}

		// A drag east: the map shows what's west.
		events = nil
		SendMessage(v.hwnd, WM_LBUTTONDOWN, 1, lparam(100, 100))
		SendMessage(v.hwnd, WM_MOUSEMOVE, 1, lparam(140, 110))
		SendMessage(v.hwnd, WM_MOUSEMOVE, 1, lparam(200, 120))
		SendMessage(v.hwnd, WM_LBUTTONUP, 0, lparam(200, 120))
		if len(events) != 1 || events[0].name != "region" || lastEvent["lng"].(float64) >= lng || lastEvent["lat"].(float64) <= lat {
			fatalf("a drag: %v %v", events, lastEvent)
		}
		lng2 := lastEvent["lng"].(float64)

		// A click on the ferry's pin.
		fx, fy = v.point(37.7955, -122.3937)
		events = nil
		click := func(x, y int32) {
			SendMessage(v.hwnd, WM_LBUTTONDOWN, 1, lparam(x, y))
			SendMessage(v.hwnd, WM_LBUTTONUP, 0, lparam(x, y))
			v.lastUp = time.Time{} // not a double click
		}
		click(int32(fx), int32(fy-15*v.scale()))
		if len(events) != 1 || events[0].name != "marker" || lastEvent["id"] != "ferry" || v.selected != "ferry" {
			fatalf("a click on a pin: %v %v", events, lastEvent)
		}
		// Its callout shows above it, white.
		px, w = mapPixels(v)
		if c := at(int32(fx), int32(fy-50*v.scale())); c.R < 240 || c.G < 240 || c.B < 240 {
			fatalf("the callout: %v", c)
		}
		// A click on the map: the place.
		events = nil
		click(50, 250)
		wantLat, wantLng := v.at(Point{X: 50, Y: 250})
		if len(events) != 1 || events[0].name != "press" || lastEvent["lat"] != wantLat || lastEvent["lng"] != wantLng || v.selected != "" {
			fatalf("a click on the map: %v %v", events, lastEvent)
		}
		if math.Abs(wantLng-lng2) > 0.1 || math.Abs(wantLat-lat) > 0.1 {
			fatalf("the place clicked: %v, %v", wantLat, wantLng)
		}

		// The wheel zooms in around the pointer, and reports once it stops.
		events = nil
		zoom := v.zoom
		p := ClientToScreen(v.hwnd, Point{X: 200, Y: 150})
		SendMessage(v.hwnd, WM_MOUSEWHEEL, uintptr(uint16(240))<<16, lparam(p.X, p.Y))
		if math.Abs(v.zoom-zoom-1) > 1e-9 {
			fatalf("the wheel: zoom %v, was %v", v.zoom, zoom)
		}
		if !pumpUntil(func() bool { return len(events) == 1 }) || events[0].name != "region" || lastEvent["lngDelta"].(float64) >= dlng*0.6 {
			fatalf("the wheel's report: %v %v", events, lastEvent)
		}

		// The app's region moves it, without a report.
		events = nil
		h.enter(func() {
			ui.Post(func() {
				region.Set(maps.Region{Center: maps.Coordinate{Lat: 48.8584, Lng: 2.2945}, LatDelta: 0.02, LngDelta: 0.02})
			})
		})
		pumpUntil(func() bool { return math.Abs(v.region()["lat"].(float64)-48.8584) < 1e-6 })
		rg = v.region()
		if math.Abs(rg["lat"].(float64)-48.8584) > 1e-6 || math.Abs(rg["lng"].(float64)-2.2945) > 1e-6 || len(events) != 0 {
			fatalf("the app's region: %v, %v", rg, events)
		}
	})
	srv.mu.Lock()
	most, agent := srv.most, srv.agents[0]
	srv.mu.Unlock()
	if most > mapDownloads || !strings.HasPrefix(agent, "Asuka/") {
		t.Fatalf("%d downloads at once, User-Agent %q", most, agent)
	}

	// Another map of the same tiles: from the disk, not downloaded again.
	onMain(t, func() {
		pumpUntil(func() bool { return len(tiles.want) == 0 })
		dir := tiles.dir
		views := tiles.views
		tiles = newTileStore(dir, "Asuka/1 (test)")
		tiles.views = views
		v.t = tiles
		before := srv.count()
		h.enter(func() {
			ui.Post(func() {
				region.Set(maps.Region{Center: maps.Coordinate{Lat: 37.7749, Lng: -122.4194}, LatDelta: 0.08, LngDelta: 0.08})
			})
		})
		pumpUntil(func() bool { return math.Abs(v.region()["lat"].(float64)-37.7749) < 1e-6 })
		mapPixels(v)
		if !pumpUntil(func() bool { return len(tiles.want) == 0 && len(tiles.bmps) > 0 }) || srv.count() != before {
			fatalf("from the disk: %d downloads (were %d), %d tiles", srv.count(), before, len(tiles.bmps))
		}
	})
}

// Tiles that can't be had: the map says so, once.
func TestMapViewOffline(t *testing.T) {
	srv := newTileServer(t)
	srv.fail = true
	saved := tiles
	tiles = newTileStore(t.TempDir(), "Asuka/1 (test)")
	defer func() { tiles = saved }()
	var errs []string
	mount(t, func() ui.Node {
		return maps.View(maps.Props{
			OnError: func(m string) { errs = append(errs, m) },
			Style:   ui.Style{Width: ui.Pt(300), Height: ui.Pt(200)},
		})
	})
	onMain(t, func() {
		for v := range tiles.views {
			mapPixels(v)
		}
		if !pumpUntil(func() bool { return len(errs) > 0 && len(tiles.want) == 0 }) {
			fatalf("no error")
		}
		if len(errs) != 1 || !strings.Contains(errs[0], "can't be loaded") {
			fatalf("errors: %q", errs)
		}
	})
}

func lparam(x, y int32) uintptr { return uintptr(uint16(int16(x))) | uintptr(uint16(int16(y)))<<16 }

// lastEvent is the values of the last event the recorder saw.
var lastEvent map[string]any
