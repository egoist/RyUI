//go:build e2e

package e2e

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestWebCrashAndMap runs e2e/testdata/webcrash in headless Chrome:
//
//   - a native view no script registers shows nothing and the page goes
//     on (the views after it are there);
//   - maps.View draws OpenStreetMap's tiles (answered here, through
//     DevTools' Fetch, so the test asks nothing of openstreetmap.org),
//     with its attribution and its pin; a click on the pin, on the map,
//     and a drag each reach Go;
//   - an uncaught JavaScript error is a report at once, not fatal;
//   - a Go panic ends the program, and the next launch reports it, fatal,
//     with Go's crash output and the browser in its context.
func TestWebCrashAndMap(t *testing.T) {
	browser := chrome()
	if browser == "" {
		t.Skip("no Chrome (ASUKA_CHROME)")
	}
	pageURL := webServer(t, "e2e/testdata/webcrash")
	c := launchCDP(t, browser)
	var created struct{ TargetID string }
	if err := c.call("", "Target.createTarget", map[string]any{"url": "about:blank"}, &created, ""); err != nil {
		t.Fatal(err)
	}
	page := c.attach(t, created.TargetID)

	// The tiles: a PNG each, from here.
	var tile bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 256, 256))
	for y := range 256 {
		for x := range 256 {
			img.Set(x, y, color.RGBA{0xaa, 0xd3, 0xdf, 0xff})
		}
	}
	png.Encode(&tile, img)
	body := base64.StdEncoding.EncodeToString(tile.Bytes())
	var tiles atomic.Int64
	var badTile atomic.Value
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	go func() {
		seen := 0
		for {
			select {
			case <-stop:
				return
			default:
			}
			c.mu.Lock()
			evs := append([]cdpMessage(nil), c.events[seen:]...)
			seen = len(c.events)
			c.mu.Unlock()
			for _, e := range evs {
				if e.Method != "Fetch.requestPaused" {
					continue
				}
				var p struct {
					RequestID string
					Request   struct{ URL string }
				}
				json.Unmarshal(e.Params, &p)
				parts := strings.Split(strings.TrimPrefix(p.Request.URL, "https://tile.openstreetmap.org/"), "/")
				if len(parts) != 3 || !strings.HasSuffix(parts[2], ".png") {
					badTile.Store(p.Request.URL)
				}
				tiles.Add(1)
				var res json.RawMessage
				c.call(page, "Fetch.fulfillRequest", map[string]any{
					"requestId": p.RequestID, "responseCode": 200, "body": body,
					"responseHeaders": []map[string]string{{"name": "Content-Type", "value": "image/png"}, {"name": "Access-Control-Allow-Origin", "value": "*"}},
				}, &res, "")
			}
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
		}
	}()
	for _, m := range []struct {
		method string
		params any
	}{
		{"Page.enable", map[string]any{}},
		{"Fetch.enable", map[string]any{"patterns": []map[string]string{{"urlPattern": "https://tile.openstreetmap.org/*"}}}},
		{"Page.navigate", map[string]any{"url": pageURL}},
	} {
		var res json.RawMessage
		if err := c.call(page, m.method, m.params, &res, ""); err != nil {
			t.Fatal(err)
		}
	}
	s := &webScreen{t: t, url: pageURL}
	showsLine(t, s, "status: ready")

	// The native view no script registers: nothing in its place, the rest there.
	showsLine(t, s, "after: native")
	// The map's is a native view too, holding its element; the other holds none.
	if got := c.eval(t, page, `[...document.querySelectorAll(".asuka-native")].map((e) => e.childElementCount).sort().join(",")`); got != "0,1" {
		t.Fatalf("native views' elements: %q", got)
	}

	// The map: tiles from OpenStreetMap's server, drawn; the attribution; the pin.
	loaded := func() string {
		return c.eval(t, page, `document.querySelectorAll(".asuka-map-img.asuka-map-loaded").length + " " + (document.querySelector(".asuka-map-attribution")?.textContent || "") + " " + document.querySelectorAll(".asuka-map-pin").length`)
	}
	until(t, func() bool {
		f := strings.Fields(loaded())
		n, _ := strconv.Atoi(f[0])
		return n >= 4 && strings.Contains(loaded(), "© OpenStreetMap contributors") && strings.HasSuffix(loaded(), " 1")
	}, loaded)
	if u := badTile.Load(); u != nil {
		t.Fatalf("a tile from %v", u)
	}
	// A zoom that shows 0.02° across a phone's width: level 15 or so (×2 on a 2× screen: one more).
	if got := c.eval(t, page, `(() => { const u = [...document.querySelectorAll(".asuka-map-img")].map((i) => i.src).find((s) => s.includes("openstreetmap")); return u.split("/")[3]; })()`); got < "14" || got > "17" {
		t.Fatalf("tiles of level %s", got)
	}

	// The pin: Go hears of it, and its callout shows.
	c.eval(t, page, `document.querySelector(".asuka-map-pin").click(), "ok"`)
	showsLine(t, s, "map: marker eiffel")
	if got := c.eval(t, page, `document.querySelector(".asuka-map-callout")?.textContent || ""`); got != "Eiffel TowerParis" {
		t.Fatalf("the callout: %q", got)
	}

	// A click on the map, and a drag of it, with the mouse.
	rect := c.eval(t, page, `(() => { const r = document.querySelector(".asuka-map").getBoundingClientRect(); return [r.left, r.top, r.width, r.height].map(Math.round).join(" "); })()`)
	var x, y, w, h float64
	if _, err := fmt.Sscan(rect, &x, &y, &w, &h); err != nil {
		t.Fatalf("the map's box %q: %v", rect, err)
	}
	mouse := func(typ string, px, py float64, buttons int) {
		t.Helper()
		var res json.RawMessage
		p := map[string]any{"type": typ, "x": px, "y": py, "button": "left", "buttons": buttons, "clickCount": 1}
		if typ == "mouseMoved" {
			p["button"] = "none"
		}
		if err := c.call(page, "Input.dispatchMouseEvent", p, &res, ""); err != nil {
			t.Fatal(err)
		}
	}
	cx, cy := x+w/4, y+h/4
	mouse("mousePressed", cx, cy, 1)
	mouse("mouseReleased", cx, cy, 0)
	showsLine(t, s, "map: press true")
	mouse("mousePressed", cx, cy, 1)
	for i := 1; i <= 10; i++ {
		mouse("mouseMoved", cx+float64(i)*8, cy+float64(i)*5, 1)
	}
	mouse("mouseReleased", cx+80, cy+50, 0)
	showsLine(t, s, "region: moved true")
	if n := tiles.Load(); n < 4 {
		t.Fatalf("%d tiles asked for", n)
	}

	// An uncaught JavaScript error: a report now, the page going on.
	c.eval(t, page, `setTimeout(() => { throw new TypeError("boom from js"); }, 0), "ok"`)
	showsLine(t, s, "exception: false TypeError: boom from js | crumbs 1")

	// A Go panic: the program ends; the next launch reports it.
	s.tap("Panic")
	until(t, func() bool { return c.eval(t, page, `document.querySelector(".asuka-error")?.textContent || ""`) != "" }, func() string {
		return "no error shown; " + c.eval(t, page, `(() => { const k = asukaHost.crash; return [k.go.exited, k.go.exitCode, k.path, k.ended, k.printed.length].join(" "); })()`)
	})
	// Its crash kept (the app's files written), the run's mark goes.
	marked := func() string {
		return c.eval(t, page, `Object.keys(localStorage).filter((k) => k.startsWith("asuka-run:")).join(",")`)
	}
	until(t, func() bool { return marked() == "" }, marked)
	var res json.RawMessage
	if err := c.call(page, "Page.navigate", map[string]any{"url": pageURL}, &res, ""); err != nil {
		t.Fatal(err)
	}
	showsLine(t, s, "status: ready")
	report := func() string { return lineOf(s, "go: ") }
	until(t, func() bool { return strings.HasPrefix(report(), "go: true panic: assignment to entry in nil map") }, report)
	if r := report(); !strings.Contains(r, "| main.App.func") || !strings.Contains(r, "| Web ") {
		t.Fatalf("the report: %q", r)
	}
}
