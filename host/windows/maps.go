//go:build windows

package windows

import (
	"bytes"
	"encoding/json"
	"errors"
	stdimage "image"
	"image/draw"
	"image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/native"
)

// maps.View on Windows: a map the host draws, of OpenStreetMap's tiles
// (Windows has no map control for desktop apps: its MapControl is UWP's,
// and needs a Bing Maps key), as GNOME Maps' libshumate does on Linux.
// The tiles come from tile.openstreetmap.org as its usage policy asks:
// a User-Agent naming the app, two downloads at a time, the tiles kept
// on disk (the app's caches, asuka-map-tiles) and used for a week before
// they're asked for again, the attribution "© OpenStreetMap
// contributors" shown on the map, linking to its copyright page. While
// a tile comes, the one above it that's there is drawn bigger.
//
// Props: region and markers (JSON), type (Satellite and Hybrid have no
// free imagery source: they show the streets, as Muted does), showsUser
// (no location layer: nothing shown, as on Linux), interactive. The
// user moves it with a drag (a finger's pan), zooms with the wheel (a
// touchpad's pinch comes as Ctrl and the wheel), a finger's pinch, a
// double click (Shift: out), and + and -; the arrow keys move it.
// Events: region (lat, lng, latDelta, lngDelta, once the user has moved
// it), marker (id: a click on a pin, which shows its title and subtitle
// in a callout above it), press (lat, lng: a click off the markers),
// error (message: tiles that can't be had). It has no size of its own:
// its style gives it one.

func init() {
	builtinViews["asuka.map"] = func(ctx native.Context) native.View { return newMapView(theHost, ctx) }
}

const (
	mapTileSize   = 256
	mapMaxZoom    = 19
	mapTileFresh  = 7 * 24 * time.Hour // the tile policy's least
	mapDownloads  = 2                  // at a time, as the policy allows
	mapMemTiles   = 384                // decoded tiles kept
	mapTimerWheel = 1                  // the wheel's region report, once it stops
	mapTimerZoom  = 2                  // a double click's zoom, animated
)

// mapTileURL is where tiles come from: ASUKA_MAP_TILES ({z}, {x}, {y}),
// else OpenStreetMap's.
func mapTileURL(z, x, y int) string {
	t := os.Getenv("ASUKA_MAP_TILES")
	if t == "" {
		t = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"
	}
	return strings.NewReplacer("{z}", strconv.Itoa(z), "{x}", strconv.Itoa(x), "{y}", strconv.Itoa(y)).Replace(t)
}

// userAgent names Asuka and the app (not the user) to OpenStreetMap's
// servers, as their policies ask.
func (h *Host) userAgent() string {
	app := h.appUserModelID()
	return "Asuka/1 (" + app + "; Windows; +https://github.com/egoist/asuka)"
}

// The tiles, shared by the maps.

type tileKey struct{ z, x, y int32 }

type tileBitmap struct {
	bmp  uintptr
	used uint64
}

type tileJob struct {
	key   tileKey
	frame uint64
}

// tileStore loads tiles: on the main thread, those decoded (bitmaps);
// off it, the downloads.
type tileStore struct {
	bmps   map[tileKey]*tileBitmap
	want   map[tileKey]bool      // asked for, not here yet
	failed map[tileKey]time.Time // couldn't be had: not asked again until then
	use    uint64
	views  map[*mapView]bool
	dc     uintptr // selects a tile to draw it

	mu         sync.Mutex
	cond       *sync.Cond
	jobs       []tileJob // newest last: what's on screen now first
	workers    int
	frame      atomic.Uint64
	dir        string
	agent      string
	client     *http.Client
	requests   atomic.Int64 // downloads made
	inFlight   atomic.Int32
	mostAtOnce atomic.Int32
}

var tiles *tileStore

func (h *Host) tileStore() *tileStore {
	if tiles == nil {
		tiles = newTileStore(filepath.Join(h.cacheDir(), "asuka-map-tiles"), h.userAgent())
	}
	return tiles
}

// newTileStore loads tiles, keeping them in dir.
func newTileStore(dir, agent string) *tileStore {
	t := &tileStore{
		bmps: map[tileKey]*tileBitmap{}, want: map[tileKey]bool{}, failed: map[tileKey]time.Time{}, views: map[*mapView]bool{},
		dir: dir, agent: agent, client: &http.Client{Timeout: 30 * time.Second},
	}
	t.cond = sync.NewCond(&t.mu)
	return t
}

// get is a tile's bitmap, if it's here; else it's asked for (once).
func (t *tileStore) get(h *Host, k tileKey, ask bool) uintptr {
	if b := t.bmps[k]; b != nil {
		t.use++
		b.used = t.use
		return b.bmp
	}
	if !ask || t.want[k] {
		return 0
	}
	if until, ok := t.failed[k]; ok && time.Now().Before(until) {
		return 0
	}
	t.want[k] = true
	t.mu.Lock()
	t.jobs = append(t.jobs, tileJob{k, t.frame.Load()})
	for t.workers < mapDownloads {
		t.workers++
		go t.work(h)
	}
	t.cond.Signal()
	t.mu.Unlock()
	return 0
}

// work loads tiles, the newest asked first; those asked frames ago (off
// screen since) are let go.
func (t *tileStore) work(h *Host) {
	for {
		t.mu.Lock()
		for len(t.jobs) == 0 {
			t.cond.Wait()
		}
		j := t.jobs[len(t.jobs)-1]
		t.jobs = t.jobs[:len(t.jobs)-1]
		t.mu.Unlock()
		if t.frame.Load() > j.frame+2 {
			h.Post(func() { delete(t.want, j.key) })
			continue
		}
		px, err := t.load(j.key)
		h.Post(func() { t.loaded(j.key, px, err) })
	}
}

// load is a tile's pixels (BGRA): the disk's, if fresh; else the
// server's, kept on disk; else the disk's, however old. Off the main
// thread.
func (t *tileStore) load(k tileKey) ([]byte, error) {
	path := filepath.Join(t.dir, strconv.Itoa(int(k.z)), strconv.Itoa(int(k.x)), strconv.Itoa(int(k.y))+".png")
	var stale []byte
	if fi, err := os.Stat(path); err == nil {
		if b, err := os.ReadFile(path); err == nil {
			if time.Since(fi.ModTime()) < mapTileFresh {
				if px, err := decodeTile(b); err == nil {
					return px, nil
				}
			} else {
				stale = b
			}
		}
	}
	b, err := t.download(k)
	if err == nil {
		px, derr := decodeTile(b)
		if derr == nil {
			if os.MkdirAll(filepath.Dir(path), 0o755) == nil {
				tmp := path + ".tmp"
				if os.WriteFile(tmp, b, 0o644) == nil {
					os.Rename(tmp, path)
				}
			}
			return px, nil
		}
		err = derr
	}
	if stale != nil {
		if px, derr := decodeTile(stale); derr == nil {
			return px, nil
		}
	}
	return nil, err
}

func (t *tileStore) download(k tileKey) ([]byte, error) {
	n := t.inFlight.Add(1)
	defer t.inFlight.Add(-1)
	for {
		m := t.mostAtOnce.Load()
		if n <= m || t.mostAtOnce.CompareAndSwap(m, n) {
			break
		}
	}
	t.requests.Add(1)
	req, err := http.NewRequest("GET", mapTileURL(int(k.z), int(k.x), int(k.y)), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", t.agent)
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New(resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// decodeTile is a PNG's pixels as a 256×256 bitmap's BGRA.
func decodeTile(b []byte) ([]byte, error) {
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	rgba := stdimage.NewRGBA(stdimage.Rect(0, 0, mapTileSize, mapTileSize))
	if img.Bounds().Dx() == mapTileSize && img.Bounds().Dy() == mapTileSize {
		draw.Draw(rgba, rgba.Rect, img, img.Bounds().Min, draw.Src)
	} else { // another size: nearest pixels
		bd := img.Bounds()
		for y := range mapTileSize {
			for x := range mapTileSize {
				rgba.Set(x, y, img.At(bd.Min.X+x*bd.Dx()/mapTileSize, bd.Min.Y+y*bd.Dy()/mapTileSize))
			}
		}
	}
	px := rgba.Pix
	for i := 0; i < len(px); i += 4 {
		px[i], px[i+2] = px[i+2], px[i]
	}
	return px, nil
}

// loaded keeps a tile come, and redraws the maps; one that couldn't be
// had is asked again in half a minute, and the maps say so once.
func (t *tileStore) loaded(k tileKey, px []byte, err error) {
	delete(t.want, k)
	if err != nil {
		t.failed[k] = time.Now().Add(30 * time.Second)
		for v := range t.views {
			v.tileFailed(err)
		}
		return
	}
	delete(t.failed, k)
	bmp, bits := DIB(mapTileSize, mapTileSize)
	if bmp == 0 {
		return
	}
	copy(bits, px)
	t.use++
	t.bmps[k] = &tileBitmap{bmp: bmp, used: t.use}
	if len(t.bmps) > mapMemTiles {
		t.trim()
	}
	for v := range t.views {
		v.tileCame(k)
	}
}

// trim lets the least recently drawn tiles go, down to three quarters.
func (t *tileStore) trim() {
	type kb struct {
		k    tileKey
		used uint64
	}
	all := make([]kb, 0, len(t.bmps))
	for k, b := range t.bmps {
		all = append(all, kb{k, b.used})
	}
	// The oldest: a partial selection by repeated passes is enough here.
	for len(t.bmps) > mapMemTiles*3/4 {
		oi := 0
		for i := range all {
			if all[i].used < all[oi].used {
				oi = i
			}
		}
		if b := t.bmps[all[oi].k]; b != nil {
			DeleteObject(b.bmp)
			delete(t.bmps, all[oi].k)
		}
		all[oi] = all[len(all)-1]
		all = all[:len(all)-1]
	}
}

// The view.

type mapMarker struct {
	ID string `json:"id"`
	At struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"at"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
	Color    string `json:"color"`
}

type mapRegion struct {
	Center struct {
		Lat float64 `json:"lat"`
		Lng float64 `json:"lng"`
	} `json:"center"`
	LatDelta float64 `json:"latDelta"`
	LngDelta float64 `json:"lngDelta"`
}

type mapView struct {
	h    *Host
	ctx  native.Context
	hwnd uintptr
	t    *tileStore

	// Where it shows: the center, as fractions of the world (Web
	// Mercator, x east from 180°W, y south from 85°N), and the zoom
	// (the world is 256·2^zoom points wide).
	cx, cy, zoom float64
	pending      *mapRegion // a region waiting for the view's size

	markers     []mapMarker
	selected    string // the marker whose callout shows
	interactive bool

	// The mouse.
	down       bool
	dragging   bool
	downAt     Point
	downCenter [2]float64
	lastUp     time.Time
	lastUpAt   Point
	doubled    bool // the press was a double click's second
	gesture    Point
	gestureArg uint64
	moved      bool // the user moved it since the last report

	// A double click's zoom, animated: from, to, around a point, started.
	zoomFrom, zoomTo float64
	zoomAt           Point
	zoomStart        time.Time

	back      uintptr // the back buffer
	backBits  []byte
	backSize  Point
	attrK     float64 // the attribution's width, measured at this scale
	attrW     int32
	failed    bool // tiles couldn't be had: said once
	destroyed bool
}

func newMapView(h *Host, ctx native.Context) *mapView {
	v := &mapView{h: h, ctx: ctx, interactive: true, cx: 0.5, cy: 0.5, zoom: 1}
	v.t = h.tileStore()
	v.t.views[v] = true
	v.hwnd = ownWindow(0, "", WS_CHILD|WS_VISIBLE|WS_TABSTOP, 0, 0, 0, 0, ctx.Parent(), func(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
		return v.message(m, wp, lp)
	})
	MapGestures(v.hwnd)
	return v
}

func (v *mapView) Handle() uintptr { return v.hwnd }

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

func (v *mapView) Destroy() {
	v.destroyed = true
	delete(v.t.views, v)
	if v.back != 0 {
		DeleteObject(v.back)
		v.back = 0
	}
}

// Set implements native.View.
func (v *mapView) Set(name string, value any) {
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
		v.markers = ms
		found := false
		for _, m := range ms {
			found = found || m.ID == v.selected
		}
		if !found {
			v.selected = ""
		}
		Invalidate(v.hwnd)
	case "interactive":
		on, _ := value.(bool)
		v.interactive = on
	}
}

// scale is the view's pixels per point.
func (v *mapView) scale() float64 { return float64(DPI(v.hwnd)) / 96 }

// size is the view's size in points.
func (v *mapView) size() (float64, float64) {
	r := ClientRect(v.hwnd)
	k := v.scale()
	return float64(r.W()) / k, float64(r.H()) / k
}

// world is the world's width in points at the zoom.
func (v *mapView) world() float64 { return mapTileSize * math.Exp2(v.zoom) }

func mercY(lat float64) float64 {
	lat = max(min(lat, 85.05112878), -85.05112878) * math.Pi / 180
	return (1 - math.Log(math.Tan(lat)+1/math.Cos(lat))/math.Pi) / 2
}

func mercLat(y float64) float64 {
	return math.Atan(math.Sinh(math.Pi*(1-2*y))) * 180 / math.Pi
}

// applyRegion shows the pending region, once the view has a size: its
// center, zoomed to show its span.
func (v *mapView) applyRegion() {
	r := v.pending
	w, ht := v.size()
	if r == nil || w <= 0 || ht <= 0 {
		return
	}
	v.pending = nil
	zoom := float64(mapMaxZoom)
	if r.LngDelta > 0 {
		zoom = min(zoom, math.Log2(w*360/(mapTileSize*r.LngDelta)))
	}
	if r.LatDelta > 0 {
		span := math.Abs(mercY(r.Center.Lat+r.LatDelta/2) - mercY(r.Center.Lat-r.LatDelta/2))
		if span > 0 {
			zoom = min(zoom, math.Log2(ht/(mapTileSize*span)))
		}
	}
	v.zoom = max(zoom, 0)
	v.cx = (r.Center.Lng + 180) / 360
	v.cy = mercY(r.Center.Lat)
	v.clamp()
	Invalidate(v.hwnd)
}

// clamp keeps the zoom in range and the map's poles in view; the world
// wraps east and west.
func (v *mapView) clamp() {
	w, ht := v.size()
	least := 0.0
	if ht > 0 {
		least = math.Log2(max(ht, 1) / mapTileSize)
	}
	v.zoom = max(min(v.zoom, mapMaxZoom), max(least, 0))
	v.cx -= math.Floor(v.cx)
	half := ht / 2 / v.world()
	v.cy = max(min(v.cy, 1-half), half)
	_ = w
}

// region is what the view shows.
func (v *mapView) region() map[string]any {
	w, ht := v.size()
	W := v.world()
	top, bottom := mercLat(v.cy-ht/2/W), mercLat(v.cy+ht/2/W)
	return map[string]any{
		"lat": mercLat(v.cy), "lng": v.cx*360 - 180,
		"latDelta": math.Abs(top - bottom), "lngDelta": min(w/W*360, 360),
	}
}

// report tells the app the region the user moved it to.
func (v *mapView) report() {
	if !v.moved || v.destroyed {
		return
	}
	v.moved = false
	v.ctx.Emit("region", v.region())
}

// at is the place at a point of the view (pixels).
func (v *mapView) at(p Point) (lat, lng float64) {
	r := ClientRect(v.hwnd)
	k := v.scale()
	W := v.world()
	x := v.cx + (float64(p.X)/k-float64(r.W())/k/2)/W
	y := v.cy + (float64(p.Y)/k-float64(r.H())/k/2)/W
	x -= math.Floor(x)
	return mercLat(y), x*360 - 180
}

// point is where a place is in the view (pixels): the copy of the world
// nearest the center.
func (v *mapView) point(lat, lng float64) (float64, float64) {
	r := ClientRect(v.hwnd)
	k := v.scale()
	W := v.world()
	dx := (lng+180)/360 - v.cx
	dx -= math.Round(dx)
	dy := mercY(lat) - v.cy
	return float64(r.W())/2 + dx*W*k, float64(r.H())/2 + dy*W*k
}

// zoomBy zooms by dz around a point (pixels), which stays where it is.
func (v *mapView) zoomBy(dz float64, p Point) {
	r := ClientRect(v.hwnd)
	k := v.scale()
	ox, oy := (float64(p.X)-float64(r.W())/2)/k, (float64(p.Y)-float64(r.H())/2)/k
	W0 := v.world()
	px, py := v.cx+ox/W0, v.cy+oy/W0
	v.zoom += dz
	v.clamp()
	W := v.world()
	v.cx, v.cy = px-ox/W, py-oy/W
	v.clamp()
	v.moved = true
	Invalidate(v.hwnd)
}

// panBy moves the map by dx, dy pixels.
func (v *mapView) panBy(dx, dy float64) {
	k := v.scale()
	W := v.world()
	v.cx -= dx / k / W
	v.cy -= dy / k / W
	v.clamp()
	v.moved = true
	Invalidate(v.hwnd)
}

func (v *mapView) tileCame(k tileKey) { Invalidate(v.hwnd) }

func (v *mapView) tileFailed(err error) {
	if v.failed || v.destroyed {
		return
	}
	v.failed = true
	v.ctx.Emit("error", map[string]any{"message": "maps: the map's tiles can't be loaded: " + err.Error()})
}

func pointOf(lp uintptr) Point {
	return Point{X: int32(int16(LoWord(lp))), Y: int32(int16(HiWord(lp)))}
}

func (v *mapView) message(m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case WM_ERASEBKGND:
		return 1, true
	case WM_PAINT:
		var ps PaintStruct
		dc := BeginPaint(v.hwnd, &ps)
		v.paint(dc)
		EndPaint(v.hwnd, &ps)
		return 0, true
	case WM_SIZE:
		if v.pending != nil {
			v.applyRegion()
		} else {
			v.clamp()
		}
		return 0, false
	case WM_GETDLGCODE:
		return DLGC_WANTARROWS | DLGC_WANTCHARS, true
	case WM_SETCURSOR:
		if v.interactive && LoWord(lp) == 1 { // HTCLIENT
			p := ScreenToClient(v.hwnd, CursorPos())
			if v.attributionHit(p) || v.markerAt(p) != "" {
				SetCursor(Cursor(IDC_HAND))
			} else {
				SetCursor(Cursor(IDC_ARROW))
			}
			return 1, true
		}
	case WM_LBUTTONDOWN:
		p := pointOf(lp)
		SetFocus(v.hwnd)
		if !v.interactive {
			return 0, true
		}
		SetCapture(v.hwnd)
		v.down, v.dragging, v.downAt, v.downCenter = true, false, p, [2]float64{v.cx, v.cy}
		v.doubled = time.Since(v.lastUp) < time.Duration(DoubleClickTime())*time.Millisecond &&
			abs32(p.X-v.lastUpAt.X) <= 4 && abs32(p.Y-v.lastUpAt.Y) <= 4
		if v.doubled && v.markerAt(p) == "" && !v.attributionHit(p) {
			dz := 1.0
			if wp&0x4 != 0 { // MK_SHIFT: out
				dz = -1
			}
			v.animateZoom(dz, p)
		}
		return 0, true
	case WM_MOUSEMOVE:
		if !v.down {
			return 0, false
		}
		p := pointOf(lp)
		if !v.dragging && (abs32(p.X-v.downAt.X) > 3 || abs32(p.Y-v.downAt.Y) > 3) {
			v.dragging = true
		}
		if v.dragging {
			v.cx, v.cy = v.downCenter[0], v.downCenter[1]
			v.panBy(float64(p.X-v.downAt.X), float64(p.Y-v.downAt.Y))
		}
		return 0, true
	case WM_LBUTTONUP:
		if !v.down {
			return 0, false
		}
		p := pointOf(lp)
		v.down = false
		ReleaseCapture()
		if v.dragging {
			v.dragging = false
			v.report()
			return 0, true
		}
		if v.doubled {
			v.doubled, v.lastUp = false, time.Time{}
			return 0, true
		}
		v.lastUp, v.lastUpAt = time.Now(), p
		v.click(p)
		return 0, true
	case WM_CAPTURECHANGED:
		if v.down && lp != v.hwnd {
			v.down, v.dragging = false, false
			v.report()
		}
		return 0, false
	case WM_MOUSEWHEEL:
		if !v.interactive {
			return 0, false
		}
		delta := float64(int16(HiWord(wp))) / 120
		p := ScreenToClient(v.hwnd, pointOf(lp))
		// A notch is half a level; a touchpad's pinch (Ctrl) comes in
		// small steps, as fine as it is.
		v.zoomBy(delta/2, p)
		SetTimer(v.hwnd, mapTimerWheel, 300)
		return 0, true
	case WM_TIMER:
		switch wp {
		case mapTimerWheel:
			KillTimer(v.hwnd, mapTimerWheel)
			v.report()
		case mapTimerZoom:
			v.stepZoom()
		}
		return 0, true
	case WM_KEYDOWN:
		if !v.interactive {
			return 0, false
		}
		r := ClientRect(v.hwnd)
		mid := Point{X: r.W() / 2, Y: r.H() / 2}
		step := float64(min(r.W(), r.H())) / 8
		switch wp {
		case 0xBB, 0x6B: // + (=), the keypad's +
			v.animateZoom(1, mid)
		case 0xBD, 0x6D: // -, the keypad's -
			v.animateZoom(-1, mid)
		case 0x25: // left
			v.panBy(step, 0)
		case 0x27:
			v.panBy(-step, 0)
		case 0x26: // up
			v.panBy(0, step)
		case 0x28:
			v.panBy(0, -step)
		default:
			return 0, false
		}
		v.report()
		return 0, true
	case WM_GESTURE:
		gi, ok := Gesture(lp)
		if !ok || !v.interactive {
			return 0, false
		}
		p := ScreenToClient(v.hwnd, Point{X: int32(gi.X), Y: int32(gi.Y)})
		switch gi.ID {
		case GID_ZOOM:
			if gi.Flags&GF_BEGIN == 0 && v.gestureArg > 0 && gi.Arguments > 0 {
				v.zoomBy(math.Log2(float64(gi.Arguments)/float64(v.gestureArg)), p)
			}
			v.gestureArg = gi.Arguments
		case GID_PAN:
			if gi.Flags&GF_BEGIN == 0 {
				v.panBy(float64(p.X-v.gesture.X), float64(p.Y-v.gesture.Y))
			}
			v.gesture = p
		case GID_END:
			v.gestureArg = 0
			v.report()
		default:
			return 0, false
		}
		CloseGesture(lp)
		return 0, true
	}
	return 0, false
}

// animateZoom zooms by dz around p over a fifth of a second.
func (v *mapView) animateZoom(dz float64, p Point) {
	v.zoomFrom, v.zoomTo, v.zoomAt, v.zoomStart = v.zoom, max(min(v.zoom+dz, mapMaxZoom), 0), p, time.Now()
	SetTimer(v.hwnd, mapTimerZoom, 15)
}

func (v *mapView) stepZoom() {
	f := min(float64(time.Since(v.zoomStart))/float64(200*time.Millisecond), 1)
	f = 1 - (1-f)*(1-f) // easing out
	v.zoomBy(v.zoomFrom+(v.zoomTo-v.zoomFrom)*f-v.zoom, v.zoomAt)
	if f >= 1 {
		KillTimer(v.hwnd, mapTimerZoom)
		v.report()
	}
}

// click is a click that didn't move the map: on the attribution, a
// marker, or the map.
func (v *mapView) click(p Point) {
	if v.attributionHit(p) {
		OpenURL("https://www.openstreetmap.org/copyright")
		return
	}
	if id := v.markerAt(p); id != "" {
		v.selected = id
		Invalidate(v.hwnd)
		v.ctx.Emit("marker", map[string]any{"id": id})
		return
	}
	if v.selected != "" {
		v.selected = ""
		Invalidate(v.hwnd)
	}
	lat, lng := v.at(p)
	v.ctx.Emit("press", map[string]any{"lat": lat, "lng": lng})
}

// pin is a marker's pin's box (pixels): its tip at the place.
func (v *mapView) pin(m mapMarker) (x0, y0, x1, y1 float64) {
	k := v.scale()
	x, y := v.point(m.At.Lat, m.At.Lng)
	w, ht := 26*k, 36*k
	return x - w/2, y - ht, x + w/2, y
}

// markerAt is the marker whose pin is at p, the one drawn on top.
func (v *mapView) markerAt(p Point) string {
	px, py := float64(p.X), float64(p.Y)
	for i := len(v.markers) - 1; i >= 0; i-- {
		x0, y0, x1, y1 := v.pin(v.markers[i])
		if px >= x0 && px <= x1 && py >= y0 && py <= y1 {
			return v.markers[i].ID
		}
	}
	return ""
}

// attribution is the attribution's box (pixels), at the bottom right.
func (v *mapView) attribution() Rect {
	r := ClientRect(v.hwnd)
	k := v.scale()
	if v.attrK != k {
		f := attributionFont(k)
		dc := CreateCompatibleDC(0)
		old := SelectObject(dc, f)
		m := Rect{Right: 4000, Bottom: 4000}
		DrawText(dc, UTF16Units(mapAttribution), &m, DT_CALCRECT|DT_SINGLELINE|DT_NOPREFIX)
		SelectObject(dc, old)
		DeleteDC(dc)
		DeleteObject(f)
		v.attrK, v.attrW = k, m.W()
	}
	w, ht := v.attrW+int32(math.Round(10*k)), int32(math.Round(16*k))
	return Rect{Left: r.Right - w, Top: r.Bottom - ht, Right: r.Right, Bottom: r.Bottom}
}

// attributionFont is the attribution's: Segoe UI, 11 points.
func attributionFont(k float64) uintptr {
	lf := LogFont{Height: -int32(math.Round(11 * k)), Weight: 400, Quality: 5}
	copy(lf.FaceName[:], UTF16Units("Segoe UI"))
	return CreateFont(&lf)
}

func (v *mapView) attributionHit(p Point) bool {
	a := v.attribution()
	return p.X >= a.Left && p.X < a.Right && p.Y >= a.Top && p.Y < a.Bottom
}

// paint draws the map into dc: the tiles, the pins, a callout, the
// attribution; through a back buffer.
func (v *mapView) paint(dc uintptr) {
	r := ClientRect(v.hwnd)
	if r.W() <= 0 || r.H() <= 0 {
		return
	}
	if v.back == 0 || v.backSize != (Point{X: r.W(), Y: r.H()}) {
		if v.back != 0 {
			DeleteObject(v.back)
		}
		v.back, v.backBits = DIB(r.W(), r.H())
		v.backSize = Point{X: r.W(), Y: r.H()}
		if v.back == 0 {
			return
		}
	}
	mem := CreateCompatibleDC(dc)
	defer DeleteDC(mem)
	old := SelectObject(mem, v.back)
	defer SelectObject(mem, old)
	FillColor(mem, r, RGB(0xE8, 0xE4, 0xDE)) // land, before its tiles come
	v.paintTiles(mem, r)
	v.paintMarkers(mem)
	v.paintAttribution(mem)
	BitBlt(dc, 0, 0, r.W(), r.H(), mem, 0, 0)
}

// paintTiles draws the tiles of the zoom level nearest the view's at
// its pixels (a 2× display: the next level's), each scaled to fit; one
// that isn't here yet is drawn from one above it that is.
func (v *mapView) paintTiles(dc uintptr, r Rect) {
	t := v.t
	t.frame.Add(1)
	if t.dc == 0 {
		t.dc = CreateCompatibleDC(0)
	}
	k := v.scale()
	zf := v.zoom + math.Log2(k)
	z := int(math.Max(0, math.Min(math.Round(zf), mapMaxZoom)))
	n := 1 << z
	worldPx := mapTileSize * math.Exp2(zf)
	tilePx := worldPx / float64(n)
	ox := v.cx*worldPx - float64(r.W())/2
	oy := v.cy*worldPx - float64(r.H())/2
	tx0, tx1 := int(math.Floor(ox/tilePx)), int(math.Floor((ox+float64(r.W())-1)/tilePx))
	ty0, ty1 := max(int(math.Floor(oy/tilePx)), 0), min(int(math.Floor((oy+float64(r.H())-1)/tilePx)), n-1)
	for ty := ty0; ty <= ty1; ty++ {
		for tx := tx0; tx <= tx1; tx++ {
			x0 := int32(math.Floor(float64(tx)*tilePx - ox))
			x1 := int32(math.Floor(float64(tx+1)*tilePx - ox))
			y0 := int32(math.Floor(float64(ty)*tilePx - oy))
			y1 := int32(math.Floor(float64(ty+1)*tilePx - oy))
			wx := ((tx % n) + n) % n
			if bmp := t.get(v.h, tileKey{int32(z), int32(wx), int32(ty)}, true); bmp != 0 {
				v.blit(dc, bmp, x0, y0, x1-x0, y1-y0, 0, 0, mapTileSize)
				continue
			}
			for dz := 1; dz <= min(z, 6); dz++ {
				pk := tileKey{int32(z - dz), int32(wx >> dz), int32(ty >> dz)}
				if bmp := t.get(v.h, pk, false); bmp != 0 {
					part := mapTileSize >> dz
					v.blit(dc, bmp, x0, y0, x1-x0, y1-y0, int32((wx&(1<<dz-1))*part), int32((ty&(1<<dz-1))*part), int32(part))
					break
				}
			}
		}
	}
}

func (v *mapView) blit(dc, bmp uintptr, x, y, w, h, sx, sy, ss int32) {
	t := v.t
	old := SelectObject(t.dc, bmp)
	if w == ss && h == ss {
		BitBlt(dc, x, y, w, h, t.dc, sx, sy)
	} else {
		StretchBlt(dc, x, y, w, h, t.dc, sx, sy, ss, ss)
	}
	SelectObject(t.dc, old)
}

// markerColor is a marker's "#rrggbbaa", or Windows's red.
func markerColor(s string) ARGB {
	c := strings.TrimPrefix(s, "#")
	if len(c) == 8 {
		if u, err := strconv.ParseUint(c, 16, 32); err == nil {
			return MakeARGB(uint8(u), uint8(u>>24), uint8(u>>16), uint8(u>>8))
		}
	}
	return MakeARGB(255, 0xE8, 0x11, 0x23)
}

// paintMarkers draws each marker's pin, and the selected one's callout.
func (v *mapView) paintMarkers(dc uintptr) {
	if len(v.markers) == 0 {
		return
	}
	g := NewGraphics(dc)
	if g == 0 {
		return
	}
	defer g.Close()
	k := float32(v.scale())
	var sel *mapMarker
	for i := range v.markers {
		m := v.markers[i]
		x0, y0, x1, y1 := v.pin(m)
		cx, w := float32(x0+x1)/2, float32(x1-x0)
		tip, top := float32(y1), float32(y0)
		rad := w / 2
		// A teardrop: a circle, its sides running down to the tip.
		p := NewPath()
		p.Arc(cx-rad, top, w, w, 120, 300)
		p.Line(cx+rad*0.5, top+rad+rad*float32(math.Sqrt(3)/2), cx, tip)
		p.CloseFigure()
		shadow := SolidBrush(MakeARGB(60, 0, 0, 0))
		g.Translate(0, k)
		g.FillPath(shadow, p)
		g.Translate(0, -k)
		shadow.Close()
		b := SolidBrush(markerColor(m.Color))
		g.FillPath(b, p)
		b.Close()
		pen := NewPen(MakeARGB(255, 255, 255, 255), 1.5*k)
		g.DrawPath(pen, p)
		pen.Close()
		p.Close()
		dot := SolidBrush(MakeARGB(255, 255, 255, 255))
		g.FillEllipse(dot, cx-rad*0.38, top+rad-rad*0.38, rad*0.76, rad*0.76)
		dot.Close()
		if m.ID == v.selected && (m.Title != "" || m.Subtitle != "") {
			sel = &v.markers[i]
		}
	}
	if sel != nil {
		v.paintCallout(dc, g, *sel)
	}
}

// paintCallout draws a marker's title and subtitle in a bubble above
// its pin.
func (v *mapView) paintCallout(dc uintptr, g Graphics, m mapMarker) {
	k := v.scale()
	x0, y0, x1, _ := v.pin(m)
	font := func(bold bool, pt float64) uintptr {
		lf := LogFont{Height: -int32(math.Round(pt * k)), Weight: 400, Quality: 5}
		if bold {
			lf.Weight = 600
		}
		copy(lf.FaceName[:], UTF16Units("Segoe UI"))
		return CreateFont(&lf)
	}
	tf, sf := font(true, 14), font(false, 12)
	defer DeleteObject(tf)
	defer DeleteObject(sf)
	measure := func(f uintptr, s string) Rect {
		r := Rect{Right: 4000, Bottom: 4000}
		if s == "" {
			return Rect{}
		}
		old := SelectObject(dc, f)
		DrawText(dc, UTF16Units(s), &r, DT_CALCRECT|DT_SINGLELINE|DT_NOPREFIX)
		SelectObject(dc, old)
		return r
	}
	tr, sr := measure(tf, m.Title), measure(sf, m.Subtitle)
	pad := int32(math.Round(10 * k))
	w := max(tr.W(), sr.W()) + 2*pad
	ht := tr.H() + sr.H() + 2*int32(math.Round(7*k))
	cx := int32((x0 + x1) / 2)
	bottom := int32(y0) - int32(math.Round(6*k))
	box := Rect{Left: cx - w/2, Top: bottom - ht, Right: cx + w/2, Bottom: bottom}
	g.FillRoundRect(MakeARGB(40, 0, 0, 0), float32(box.Left), float32(box.Top)+float32(k), float32(box.W()), float32(box.H()), float32(8*k))
	g.FillRoundRect(MakeARGB(255, 255, 255, 255), float32(box.Left), float32(box.Top), float32(box.W()), float32(box.H()), float32(8*k))
	SetBkMode(dc, 1)
	y := box.Top + int32(math.Round(7*k))
	for _, line := range []struct {
		f     uintptr
		s     string
		r     Rect
		color uint32
	}{{tf, m.Title, tr, RGB(0x1A, 0x1A, 0x1A)}, {sf, m.Subtitle, sr, RGB(0x5F, 0x5F, 0x5F)}} {
		if line.s == "" {
			continue
		}
		old := SelectObject(dc, line.f)
		SetTextColor(dc, line.color)
		rr := Rect{Left: box.Left + pad, Top: y, Right: box.Right - pad, Bottom: y + line.r.H()}
		DrawText(dc, UTF16Units(line.s), &rr, DT_SINGLELINE|DT_NOPREFIX)
		SelectObject(dc, old)
		y += line.r.H()
	}
}

// paintAttribution draws "© OpenStreetMap contributors" at the bottom
// right, as OpenStreetMap asks of maps of its data.
func (v *mapView) paintAttribution(dc uintptr) {
	a := v.attribution()
	k := v.scale()
	g := NewGraphics(dc)
	if g != 0 {
		b := SolidBrush(MakeARGB(190, 255, 255, 255))
		g.FillRect(b, float32(a.Left), float32(a.Top), float32(a.W()), float32(a.H()))
		b.Close()
		g.Close()
	}
	f := attributionFont(k)
	defer DeleteObject(f)
	old := SelectObject(dc, f)
	defer SelectObject(dc, old)
	SetBkMode(dc, 1)
	SetTextColor(dc, RGB(0x33, 0x33, 0x33))
	r := a
	r.Right -= int32(math.Round(5 * k))
	DrawText(dc, UTF16Units(mapAttribution), &r, DT_SINGLELINE|DT_NOPREFIX|0x2|0x4) // DT_RIGHT | DT_VCENTER
}

const mapAttribution = "© OpenStreetMap contributors"
