//go:build linux && !android

package linux

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/asuka/camera"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/maps"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/scan"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// The device modules that run headless: every module and method the
// repo's packages call is answered (if only "not available"); the
// background, periodic and screen modules' protocols; the personal data
// Linux has no store of; imageedit's actions; ZBar's codes and
// Tesseract's text; the camera through GStreamer with a test pattern
// for a camera (its view, Capture, recording, codes in view, the take
// dialog), and with none; webauth's dialog with a local server; the
// map; and the sensors without iio-sensor-proxy.

// callAsync calls a method on the main thread and returns its result,
// which fills in when it answers.
func callAsync(h *Host, module, name string, args []byte) *callResult {
	r := &callResult{}
	onMain(func() {
		h.module(module)[name](args, func(b []byte, err error) { *r = callResult{b, err, true} })
	})
	return r
}

// awaitResult waits for an async call's answer.
func awaitResult(t *testing.T, what string, r *callResult) ([]byte, error) {
	t.Helper()
	waitFor(t, what, func() bool { return r.done })
	return r.b, r.err
}

func errIs(err error, s string) bool { return err != nil && err.Error() == s }

func TestModulesLinuxHas(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	want := map[string][]string{
		"ageRange":     {"request"},
		"appleAuth":    {"available", "signIn", "state"},
		"audio":        {"status", "load", "playOnce", "play", "pause", "seek", "volume", "rate", "loop", "release"},
		"audioRecord":  {"start", "stop", "pause", "resume", "level", "status"},
		"background":   {"start", "update", "end", "next"},
		"biometric":    {"check", "authenticate"},
		"browser":      {"open"},
		"calendar":     {"request", "calendars", "events", "add"},
		"camera":       {"take", "capture", "record", "stopRecording"},
		"clipboard":    {"copyImage", "has", "image", "next", "stop"},
		"compose":      {"can", "mail", "message"},
		"contacts":     {"request", "list", "pick", "add"},
		"device":       {"clipboard", "info", "keepAwake"},
		"files":        {"pick"},
		"imageEdit":    {"edit", "size", "videoFrame"},
		"integrity":    {"supported", "generateKey", "attestKey", "assert", "prepare", "token"},
		"js":           {"eval"},
		"library":      {"save"},
		"location":     {"current", "name", "next", "stop"},
		"network":      {"current", "next", "stop"},
		"notify":       {"actionDone", "badge", "cancel", "categories", "channelGroups", "channels", "present", "presented", "remove", "removeAll", "removeThread", "request", "schedule", "showing", "stop", "nextAction", "nextPresent"},
		"periodic":     {"schedule", "cancel", "next", "done"},
		"photoLibrary": {"current", "request", "manage", "list", "thumbnail", "export"},
		"photos":       {"pick"},
		"preview":      {"show"},
		"print":        {"pdf", "file", "html", "webView"},
		"push":         {"register", "nextReceived", "nextOpened", "stop", "nextRewrite", "rewritten"},
		"review":       {"request"},
		"scan":         {"file"},
		"screen":       {"lock", "bars", "preventCapture", "screenshot", "stopScreenshots"},
		"secureStore":  {"get", "set", "delete"},
		"sensors":      {"available", "start", "stop", "stopAll", "next", "steps", "simulate"},
		"speech":       {"speak", "stop", "pause", "resume", "speaking", "voices"},
		"splash":       {"hold", "hide"},
		"tracking":     {"request", "status"},
		"transcribe":   {"check", "file"},
		"vision":       {"analyze"},
		"webAuth":      {"start", "cancel"},
		"widget":       {"set"},
	}
	onMain(func() {
		for mod, ms := range want {
			for _, m := range ms {
				if h.module(mod)[m] == nil {
					t.Errorf("%s.%s isn't on Linux", mod, m)
				}
			}
		}
		for _, kind := range []string{"asuka.camera", "asuka.map", "asuka.appleSignIn"} {
			if builtinViews[kind] == nil {
				t.Errorf("no %s view on Linux", kind)
			}
		}
	})
}

func TestBackgroundTasks(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	var inhibits []bool
	onMain(func() {
		h.module("background") // made: its seam's set
		bgTasks.inhibit = func(on bool) { inhibits = append(inhibits, on) }
	})
	defer onMain(func() { bgTasks.inhibit = nil })
	if _, err := callModule(t, h, "background", "next", nil); !errIs(err, "ended") {
		t.Fatalf("next without tasks: %v", err)
	}
	id := func(n int) []byte { return []byte(fmt.Sprintf(`{"id":%d,"title":"t"}`, n)) }
	callModule(t, h, "background", "start", id(1))
	callModule(t, h, "background", "start", id(2))
	callModule(t, h, "background", "update", id(2))
	next := callAsync(h, "background", "next", nil)
	callModule(t, h, "background", "end", id(1))
	onMain(func() {
		if next.done {
			fatalf(t, "next answered with a task running: %v", next.err)
		}
	})
	callModule(t, h, "background", "end", id(2))
	if _, err := awaitResult(t, "next", next); !errIs(err, "ended") {
		t.Fatalf("next after the last ended: %v", err)
	}
	// Suspend held while any ran, let go once.
	onMain(func() {
		if len(inhibits) != 3 || !inhibits[0] || !inhibits[1] || inhibits[2] {
			fatalf(t, "inhibits %v", inhibits)
		}
	})
}

func TestPeriodic(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	now := time.Unix(1_700_000_000, 0)
	charging, online := false, true
	file := filepath.Join(t.TempDir(), "periodic.json")
	onMain(func() {
		h.module("periodic")
		s := &periodicState
		s.now = func() time.Time { return now }
		s.charging = func() bool { return charging }
		s.online = func() bool { return online }
		s.file = file
		s.loaded, s.due, s.waiting = false, nil, nil
		s.running, s.retry = map[string]bool{}, map[string]time.Time{}
	})
	defer onMain(func() {
		s := &periodicState
		if s.timer != 0 {
			SourceRemove(s.timer)
			s.timer = 0
		}
		s.now, s.file, s.waiting, s.due = nil, "", nil, nil
	})
	schedule := func(name string, secs uint32, flags byte) {
		b := binary.LittleEndian.AppendUint32(nil, uint32(len(name)))
		b = append(b, name...)
		b = binary.LittleEndian.AppendUint32(b, secs)
		if _, err := callModule(t, h, "periodic", "schedule", append(b, flags)); err != nil {
			t.Fatal(err)
		}
	}
	decode := func(b []byte) (string, uint32) {
		n := binary.LittleEndian.Uint32(b)
		return string(b[4 : 4+n]), binary.LittleEndian.Uint32(b[4+n:])
	}
	schedule("sync", 900, 1)
	next := callAsync(h, "periodic", "next", nil)
	tick := func(d time.Duration) {
		onMain(func() {
			now = now.Add(d)
			h.periodicTick()
		})
	}
	tick(899 * time.Second)
	onMain(func() {
		if next.done {
			fatalf(t, "ran before its interval")
		}
	})
	// Offline: deferred a minute.
	online = false
	tick(2 * time.Second)
	onMain(func() {
		if next.done {
			fatalf(t, "ran offline")
		}
	})
	online = true
	tick(30 * time.Second)
	onMain(func() {
		if next.done {
			fatalf(t, "ran before its retry")
		}
	})
	tick(31 * time.Second)
	b, err := awaitResult(t, "the task", next)
	if name, budget := decode(b); err != nil || name != "sync" || budget != 25 {
		t.Fatalf("next: %q %d %v", name, budget, err)
	}
	// Done: the next run an interval later, kept in the file.
	done := binary.LittleEndian.AppendUint32(nil, 4)
	callModule(t, h, "periodic", "done", append(append(done, "sync"...), 1))
	var kept map[string]periodicTask
	data, _ := os.ReadFile(file)
	if json.Unmarshal(data, &kept) != nil || kept["sync"].Last != float64(now.Unix()) || !kept["sync"].Network {
		t.Fatalf("kept: %s", data)
	}
	// A task that wants power waits for it.
	schedule("backup", 900, 2)
	next = callAsync(h, "periodic", "next", nil)
	tick(901 * time.Second) // sync is due too
	b, _ = awaitResult(t, "sync again", next)
	if name, _ := decode(b); name != "sync" {
		t.Fatalf("on battery, ran %q", name)
	}
	next = callAsync(h, "periodic", "next", nil)
	charging = true
	tick(61 * time.Second)
	b, _ = awaitResult(t, "backup", next)
	if name, budget := decode(b); name != "backup" || budget != 240 {
		t.Fatalf("charging: %q %d", name, budget)
	}
	// Cancelled: gone from the file.
	callModule(t, h, "periodic", "cancel", []byte("sync"))
	data, _ = os.ReadFile(file)
	if strings.Contains(string(data), `"sync"`) {
		t.Fatalf("cancelled, still kept: %s", data)
	}
}

func TestScreenModule(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	shots, pics := t.TempDir(), t.TempDir()
	onMain(func() { screenState.dirs = []string{shots, pics} })
	defer onMain(func() { screenState.dirs = nil })
	for _, m := range []string{"lock", "bars", "preventCapture"} {
		if _, err := callModule(t, h, "screen", m, []byte{1, 0}); err != nil {
			t.Fatalf("%s: %v", m, err)
		}
	}
	for _, c := range []struct {
		name          string
		inScreenshots bool
		want          bool
	}{
		{"Screenshot From 2026-11-13 10-00-00.png", true, true}, {"anything.jpg", true, true},
		{"notes.txt", true, false}, {"holiday.png", false, false}, {"Screenshot_20261113.png", false, true},
	} {
		if isScreenshot(c.name, c.inScreenshots) != c.want {
			t.Errorf("isScreenshot(%q, %v): %v", c.name, c.inScreenshots, !c.want)
		}
	}
	shot := callAsync(h, "screen", "screenshot", nil)
	os.WriteFile(filepath.Join(shots, "Screenshot From 2026-11-13 10-00-00.png"), []byte("x"), 0o644)
	if _, err := awaitResult(t, "the screenshot", shot); err != nil {
		t.Fatal(err)
	}
	shot = callAsync(h, "screen", "screenshot", nil)
	os.WriteFile(filepath.Join(pics, "Screenshot_20261113.png"), []byte("x"), 0o644)
	if _, err := awaitResult(t, "a screenshot in Pictures", shot); err != nil {
		t.Fatal(err)
	}
	shot = callAsync(h, "screen", "screenshot", nil)
	callModule(t, h, "screen", "stopScreenshots", nil)
	if _, err := awaitResult(t, "stopped", shot); !errIs(err, "stopped") {
		t.Fatalf("stopped: %v", err)
	}
	onMain(func() {
		if screenState.monitors != nil {
			fatalf(t, "still watching")
		}
	})
}

func TestPersonalDataModules(t *testing.T) {
	// Without Evolution Data Server (modules_eds_test.go has it).
	oldHas := edsHas
	edsHas = func(string) bool { return false }
	defer func() { edsHas = oldHas }()
	h, _ := mount(t, emptyRoot)
	var opened []string
	onMain(func() {
		openDocumentHook = func(h *Host, path string, done func(error)) {
			opened = append(opened, path)
			done(nil)
		}
	})
	defer onMain(func() { openDocumentHook = func(h *Host, path string, done func(error)) { h.launchFile(path, done) } })
	check := func(mod, m string, args []byte, want string, wantErr string) {
		t.Helper()
		b, err := callModule(t, h, mod, m, args)
		if wantErr != "" {
			if err == nil || !strings.HasPrefix(err.Error(), wantErr) {
				t.Errorf("%s.%s: %q %v, want error %q", mod, m, b, err, wantErr)
			}
			return
		}
		if err != nil || string(b) != want {
			t.Errorf("%s.%s: %q %v, want %q", mod, m, b, err, want)
		}
	}
	check("appleAuth", "available", nil, "\x00", "")
	check("appleAuth", "signIn", []byte(`{}`), "", "not available")
	check("photoLibrary", "current", nil, "denied", "")
	check("photoLibrary", "request", nil, "denied", "")
	check("photoLibrary", "list", []byte(`{}`), "", "not available")
	check("contacts", "request", nil, "\x01", "")
	check("contacts", "list", []byte(`{}`), "", "not allowed")
	check("contacts", "pick", nil, "", "not available")
	check("calendar", "request", nil, "\x02", "")
	check("calendar", "events", []byte(`{}`), "", "not allowed")
	check("splash", "hold", nil, "", "")
	check("widget", "set", nil, "", "not available")
	check("transcribe", "check", []byte("en"), "", "not available")
	check("contacts", "add", []byte(`{"givenName":"Ada","familyName":"Lovelace","phones":[{"label":"mobile","value":"+44 1"}],"emails":[{"label":"home","value":"ada@example.com"}]}`), "\x00", "")
	start := time.Date(2026, 12, 24, 0, 0, 0, 0, time.UTC)
	check("calendar", "add", []byte(fmt.Sprintf(`{"title":"Eve; party","allDay":true,"startMs":%d,"endMs":%d}`, start.UnixMilli(), start.AddDate(0, 0, 1).UnixMilli())), "\x00", "")
	if len(opened) != 2 {
		t.Fatalf("opened %v", opened)
	}
	vcf, _ := os.ReadFile(opened[0])
	for _, s := range []string{"BEGIN:VCARD", "N:Lovelace;Ada;;;", "FN:Ada Lovelace", "TEL;TYPE=CELL:+44 1", "EMAIL;TYPE=HOME:ada@example.com"} {
		if !strings.Contains(string(vcf), s) {
			t.Errorf("the vCard has no %q:\n%s", s, vcf)
		}
	}
	ics, _ := os.ReadFile(opened[1])
	for _, s := range []string{"BEGIN:VEVENT", "DTSTART;VALUE=DATE:20261224", "DTEND;VALUE=DATE:20261225", `SUMMARY:Eve\; party`} {
		if !strings.Contains(string(ics), s) {
			t.Errorf("the event has no %q:\n%s", s, ics)
		}
	}
	if !strings.HasSuffix(opened[0], ".vcf") || !strings.HasSuffix(opened[1], ".ics") {
		t.Errorf("files %v", opened)
	}
}

// testImage is w×h: red left, blue right, the bottom half half
// transparent.
func testImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{255, 0, 0, 255}
			if x >= w/2 {
				c = color.NRGBA{0, 0, 255, 255}
			}
			if y >= h/2 {
				c.A = 128
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

// withOrientation is a JPEG with an EXIF orientation.
func withOrientation(jpg []byte, o uint16) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01")
	tiff = binary.BigEndian.AppendUint16(tiff, o)
	tiff = append(tiff, 0, 0, 0, 0, 0, 0)
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	seg := []byte{0xFF, 0xE1}
	seg = binary.BigEndian.AppendUint16(seg, uint16(len(app1)+2))
	return append(append(append([]byte{}, jpg[:2]...), append(seg, app1...)...), jpg[2:]...)
}

func TestImageEdit(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	dir := t.TempDir()
	src := filepath.Join(dir, "in.png")
	var buf bytes.Buffer
	png.Encode(&buf, testImage(200, 100))
	os.WriteFile(src, buf.Bytes(), 0o644)
	buf.Reset()
	jpeg.Encode(&buf, testImage(200, 100), &jpeg.Options{Quality: 95})
	turned := filepath.Join(dir, "turned.jpg")
	os.WriteFile(turned, withOrientation(buf.Bytes(), 6), 0o644)

	type result struct {
		Path          string
		Width, Height int
	}
	edit := func(method, source string, actions any, format string) (result, error) {
		t.Helper()
		args, _ := json.Marshal(map[string]any{"source": source, "actions": actions, "format": format, "quality": 0.9})
		b, err := callModule(t, h, "imageEdit", method, args)
		var r result
		if err == nil {
			json.Unmarshal(b, &r)
		}
		return r, err
	}
	read := func(path string) image.Image {
		t.Helper()
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			t.Fatal(err)
		}
		return img
	}
	rgba := func(img image.Image, x, y int) [4]uint8 {
		c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
		return [4]uint8{c.R, c.G, c.B, c.A}
	}
	near := func(a, b [4]uint8) bool {
		for i := range 4 {
			if math.Abs(float64(a[i])-float64(b[i])) > 40 {
				return false
			}
		}
		return true
	}

	if r, err := edit("size", src, nil, ""); err != nil || r.Width != 200 || r.Height != 100 {
		t.Fatalf("size: %+v %v", r, err)
	}
	// EXIF's orientation 6: a quarter turn, in the size and the pixels.
	if r, err := edit("size", turned, nil, ""); err != nil || r.Width != 100 || r.Height != 200 {
		t.Fatalf("size of a turned JPEG: %+v %v", r, err)
	}
	r, err := edit("edit", turned, []any{}, "png")
	if err != nil || r.Width != 100 || r.Height != 200 {
		t.Fatalf("upright: %+v %v", r, err)
	}
	if img := read(r.Path); !near(rgba(img, 50, 10), [4]uint8{255, 0, 0, 255}) || !near(rgba(img, 50, 190), [4]uint8{0, 0, 255, 255}) {
		t.Errorf("upright pixels: %v %v", rgba(img, 50, 10), rgba(img, 50, 190))
	}
	// Resize with a 0: the aspect ratio kept; transparency kept in PNG.
	r, err = edit("edit", src, []map[string]any{{"op": "resize", "width": 50}}, "png")
	if err != nil || r.Width != 50 || r.Height != 25 {
		t.Fatalf("resize: %+v %v", r, err)
	}
	img := read(r.Path)
	if c := rgba(img, 5, 5); !near(c, [4]uint8{255, 0, 0, 255}) {
		t.Errorf("resized red %v", c)
	}
	if c := rgba(img, 45, 20); !near(c, [4]uint8{0, 0, 255, 128}) {
		t.Errorf("resized half-clear blue %v", c)
	}
	// Crop, turn, flip, fill, pixelate in order; JPEG on white.
	r, err = edit("edit", src, []map[string]any{
		{"op": "crop", "x": 50, "y": 0, "width": 100, "height": 50}, // red | blue, 100×50
		{"op": "rotate", "degrees": 90},                             // red on top, 50×100
		{"op": "flipV"},                                             // blue on top
		{"op": "fill", "x": 0, "y": 0, "width": 10, "height": 10, "rgb": 0x00FF00},
	}, "jpeg")
	if err != nil || r.Width != 50 || r.Height != 100 {
		t.Fatalf("crop, rotate: %+v %v", r, err)
	}
	img = read(r.Path)
	if c := rgba(img, 25, 30); !near(c, [4]uint8{0, 0, 255, 255}) {
		t.Errorf("top after the flip %v", c)
	}
	if c := rgba(img, 25, 80); !near(c, [4]uint8{255, 0, 0, 255}) {
		t.Errorf("bottom after the flip %v", c)
	}
	if c := rgba(img, 4, 4); !near(c, [4]uint8{0, 255, 0, 255}) {
		t.Errorf("filled %v", c)
	}
	// Half clear on white, in JPEG: light.
	r, _ = edit("edit", src, nil, "jpeg")
	if c := rgba(read(r.Path), 20, 80); !near(c, [4]uint8{255, 127, 127, 255}) {
		t.Errorf("half-clear red on white %v", c)
	}
	// An angle: the turned bounds, the corners clear.
	r, err = edit("edit", src, []map[string]any{{"op": "rotate", "degrees": 45}}, "png")
	side := int(math.Ceil(300 / math.Sqrt2))
	if err != nil || r.Width != side || r.Height != side {
		t.Fatalf("rotate 45: %+v %v, want %d", r, err, side)
	}
	if c := rgba(read(r.Path), 1, 1); c[3] != 0 {
		t.Errorf("corner %v", c)
	}
	// Pixelate: squares aligned to the image, the average of each.
	r, _ = edit("edit", src, []map[string]any{{"op": "pixelate", "x": 0, "y": 0, "width": 200, "height": 50, "block": 200}}, "png")
	if c := rgba(read(r.Path), 3, 3); !near(c, [4]uint8{127, 0, 127, 191}) {
		t.Errorf("pixelated %v", c)
	}
	if _, err := edit("edit", filepath.Join(dir, "none.png"), nil, ""); !errIs(err, "no image") {
		t.Errorf("missing: %v", err)
	}
	if _, err := edit("edit", filepath.Join(dir, "../"+filepath.Base(dir)), nil, ""); !errIs(err, "no image") {
		t.Errorf("a folder: %v", err)
	}
	if _, err := edit("edit", src, []map[string]any{{"op": "crop", "x": 500, "y": 0, "width": 10, "height": 10}}, ""); err == nil {
		t.Error("a crop outside: no error")
	}
	if _, err := callModule(t, h, "imageEdit", "size", []byte("{")); !errIs(err, "bad args") {
		t.Errorf("bad args: %v", err)
	}
}

// ean13Image draws an EAN-13 barcode of digits (13), scale pixels a
// module, with quiet zones.
func ean13Image(digits string, scale int) *image.RGBA {
	l := []string{"0001101", "0011001", "0010011", "0111101", "0100011", "0110001", "0101111", "0111011", "0110111", "0001011"}
	g := []string{"0100111", "0110011", "0011011", "0100001", "0011101", "0111001", "0000101", "0010001", "0001001", "0010111"}
	r := []string{"1110010", "1100110", "1101100", "1000010", "1011100", "1001110", "1010000", "1000100", "1001000", "1110100"}
	parity := []string{"LLLLLL", "LLGLGG", "LLGGLG", "LLGGGL", "LGLLGG", "LGGLLG", "LGGGLL", "LGLGLG", "LGLGGL", "LGGLGL"}
	bits := "101"
	first := digits[0] - '0'
	for i := 1; i <= 6; i++ {
		d := digits[i] - '0'
		if parity[first][i-1] == 'L' {
			bits += l[d]
		} else {
			bits += g[d]
		}
	}
	bits += "01010"
	for i := 7; i <= 12; i++ {
		bits += r[digits[i]-'0']
	}
	bits += "101"
	quiet := 12
	w, h := (len(bits)+2*quiet)*scale, 60*scale
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for i, b := range bits {
		if b != '1' {
			continue
		}
		for y := 5 * scale; y < h-5*scale; y++ {
			for x := (quiet + i) * scale; x < (quiet+i+1)*scale; x++ {
				o := img.PixOffset(x, y)
				img.Pix[o], img.Pix[o+1], img.Pix[o+2] = 0, 0, 0
			}
		}
	}
	return img
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanAndVision(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	if err := LoadZBar(); err != nil {
		if b, err := callModule(t, h, "scan", "file", []byte("\n/x.png")); err == nil || !strings.HasPrefix(err.Error(), "not available") {
			t.Fatalf("without ZBar: %q %v", b, err)
		}
		t.Skip(err)
	}
	path := filepath.Join(t.TempDir(), "ean.png")
	writePNG(t, path, ean13Image("4006381333931", 3))
	b, err := callModule(t, h, "scan", "file", []byte("\n"+path))
	if err != nil || !bytes.Equal(b, encodeCodes([][2]string{{"ean13", "4006381333931"}})) {
		t.Fatalf("scan: %q %v", b, err)
	}
	// Kinds not asked for: none.
	if b, err := callModule(t, h, "scan", "file", []byte("qr\n"+path)); err != nil || !bytes.Equal(b, encodeCodes(nil)) {
		t.Fatalf("scan for QR codes: %q %v", b, err)
	}
	if _, err := callModule(t, h, "scan", "file", []byte("\n"+path+".none")); !errIs(err, "no image") {
		t.Fatalf("no file: %v", err)
	}
	args, _ := json.Marshal(map[string]any{"path": path, "text": false, "faces": true, "codes": true})
	b, err = callModule(t, h, "vision", "analyze", args)
	var res visResult
	if err != nil || json.Unmarshal(b, &res) != nil || res.Width != ean13Image("4006381333931", 3).Rect.Dx() || len(res.Codes) != 1 || len(res.Faces) != 0 {
		t.Fatalf("analyze: %s %v", b, err)
	}
	c := res.Codes[0]
	if c.Kind != "ean13" || c.Value != "4006381333931" || c.Box.W <= 0.3 || c.Box.X < 0 || c.Box.X+c.Box.W > 1.01 {
		t.Fatalf("code %+v", c)
	}
}

// TestVisionText reads text the host drew with Tesseract.
func TestVisionText(t *testing.T) {
	if err := LoadTesseract(); err != nil {
		t.Skip(err)
	}
	if tessLanguages([]string{"en"}, "") != "eng" {
		t.Fatal("no English data")
	}
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Background: ui.RGB(255, 255, 255), Padding: ui.All(20), Align: ui.AlignStart},
			ui.StaticText("HELLO BROWN WORLD", ui.Style{Color: ui.RGB(0, 0, 0), Font: ui.Font{Size: 48}}))
	})
	var root *node
	settle(t, h, func() bool {
		root = nil
		for _, n := range find(h, ops.KindView) {
			if n.placed && n.frame[2] > 100 {
				root = n
			}
		}
		return root != nil
	})
	var px pixels
	onMain(func() { px = render(root) })
	img := image.NewRGBA(image.Rect(0, 0, px.w, px.h))
	for y := range px.h {
		for x := range px.w {
			c := px.at(x, y)
			o := img.PixOffset(x, y)
			copy(img.Pix[o:o+4], c[:])
		}
	}
	path := filepath.Join(t.TempDir(), "text.png")
	writePNG(t, path, img)
	args, _ := json.Marshal(map[string]any{"path": path, "text": true, "faces": false, "codes": false, "languages": []string{"en-US"}})
	b, err := callModule(t, h, "vision", "analyze", args)
	var res visResult
	if err != nil || json.Unmarshal(b, &res) != nil || len(res.Lines) != 1 {
		t.Fatalf("analyze: %s %v", b, err)
	}
	l := res.Lines[0]
	if l.Text != "HELLO BROWN WORLD" || len(l.Words) != 3 || l.Words[1].Text != "BROWN" || l.Words[1].Start != 6 || l.Words[1].End != 11 {
		t.Fatalf("line %+v", l)
	}
	if l.Box.X <= 0 || l.Box.W <= 0.2 || l.Words[0].Box.X+l.Words[0].Box.W > l.Words[1].Box.X {
		t.Fatalf("boxes %+v", l)
	}
}

// testCamera is a camera for the tests: GStreamer's test pattern.
const testCamera = "videotestsrc is-live=true pattern=smpte ! video/x-raw,width=320,height=240,framerate=15/1"

func useTestCamera(t *testing.T, src string) {
	t.Helper()
	if err := LoadGstApp(); err != nil {
		t.Skip(err)
	}
	onMain(func() { cameraSource = src })
	t.Cleanup(func() { onMain(func() { cameraSource = "" }) })
}

func TestCameraView(t *testing.T) {
	useTestCamera(t, testCamera)
	rec := recordSource
	recordSource = "audiotestsrc is-live=true wave=sine"
	defer func() { recordSource = rec }()
	var errs []string
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			camera.View(camera.ViewProps{Mode: camera.Video, Style: ui.Style{Width: ui.Pt(320), Height: ui.Pt(240)},
				OnError: func(m string) { errs = append(errs, m) }}))
	})
	var v *cameraView
	waitFor(t, "the camera's frames", func() bool {
		v = cameraCurrent
		return v != nil && v.pipe != 0 && v.frames > 0
	})
	onMain(func() {
		if !v.video || len(errs) > 0 {
			fatalf(t, "video %v, errors %v", v.video, errs)
		}
	})
	// Capture: the frame, not mirrored as the preview is (SMPTE's bars:
	// gray at the left, blue at the right).
	b, err := callModule(t, h, "camera", "capture", nil)
	if err != nil {
		t.Fatal("capture:", err)
	}
	f, _ := os.Open(string(b))
	img, err := jpeg.Decode(f)
	f.Close()
	if err != nil || img.Bounds().Dx() != 320 || img.Bounds().Dy() != 240 {
		t.Fatalf("photo %v %v", img, err)
	}
	r0, g0, b0, _ := img.At(10, 10).RGBA()
	r1, g1, b1, _ := img.At(310, 10).RGBA()
	if r0>>8 < 150 || g0>>8 < 150 || b0>>8 < 150 || r1>>8 > 80 || g1>>8 > 80 || b1>>8 < 150 {
		t.Fatalf("photo's left %d,%d,%d, right %d,%d,%d", r0>>8, g0>>8, b0>>8, r1>>8, g1>>8, b1>>8)
	}
	// Record, muted, until stopped.
	recArgs := func(mute bool, maxMS uint32) []byte {
		a := []byte{0}
		if mute {
			a[0] = 1
		}
		return binary.LittleEndian.AppendUint32(a, maxMS)
	}
	if _, _, _, _, err := func() (a, b, c, d string, e error) {
		onMain(func() { a, b, c, d, e = cameraEncoders(true) })
		return
	}(); err != nil {
		t.Skip(err)
	}
	recording := callAsync(h, "camera", "record", recArgs(true, 0))
	waitFor(t, "recording", func() bool { return v.recording != nil && v.pipe != 0 || recording.done })
	if _, err := callModule(t, h, "camera", "record", recArgs(true, 0)); !errIs(err, "already recording") {
		t.Fatalf("twice: %v", err)
	}
	var at int
	onMain(func() { at = v.frames })
	waitFor(t, "a second recorded", func() bool { return v.frames >= at+15 })
	callModule(t, h, "camera", "stopRecording", nil)
	b, err = awaitResult(t, "the recording", recording)
	checkVideo := func(path string) {
		t.Helper()
		data, rerr := os.ReadFile(path)
		if rerr != nil || len(data) < 1000 {
			t.Fatalf("the video: %d bytes, %v", len(data), rerr)
		}
		if strings.HasSuffix(path, ".mp4") && string(data[4:8]) != "ftyp" {
			t.Fatalf("not MPEG-4: % x", data[:8])
		}
	}
	if err != nil {
		t.Fatal("record:", err)
	}
	checkVideo(string(b))
	// The preview goes on.
	waitFor(t, "the preview after", func() bool { return v.pipe != 0 && v.recording == nil })
	// With sound, stopped by its maximum.
	b, err = awaitResult(t, "a recording with sound", callAsync(h, "camera", "record", recArgs(false, 1500)))
	if err != nil {
		t.Fatal("record with sound:", err)
	}
	checkVideo(string(b))
	video := string(b)
	// imageedit's frame of it.
	args, _ := json.Marshal(map[string]any{"source": video, "at": 500, "format": "png", "actions": []any{}})
	fb, err := callModule(t, h, "imageEdit", "videoFrame", args)
	var fr struct{ Width, Height int }
	if err != nil || json.Unmarshal(fb, &fr) != nil || fr.Width != 320 || fr.Height != 240 {
		t.Fatalf("videoFrame: %s %v", fb, err)
	}
	args, _ = json.Marshal(map[string]any{"source": video + ".none", "at": 0})
	if _, err := callModule(t, h, "imageEdit", "videoFrame", args); !errIs(err, "no image") {
		t.Fatalf("videoFrame of nothing: %v", err)
	}
}

// The camera reads codes in view (a still of a barcode for a camera).
func TestCameraScan(t *testing.T) {
	if err := LoadGstApp(); err != nil || !GstHas("zbar") || !GstHas("imagefreeze") {
		t.Skip("no zbar or imagefreeze element")
	}
	path := filepath.Join(t.TempDir(), "ean.png")
	writePNG(t, path, ean13Image("4006381333931", 3))
	useTestCamera(t, fmt.Sprintf("filesrc location=%q ! pngdec ! imagefreeze is-live=true ! videoconvert ! video/x-raw,framerate=10/1", path))
	var codes []scan.Code
	var scanner = scan.Codes(func(c scan.Code) { codes = append(codes, c) })
	mount(t, func() ui.Node {
		return camera.View(camera.ViewProps{Scan: scanner, Style: ui.Style{Width: ui.Pt(320), Height: ui.Pt(240)}})
	})
	waitFor(t, "the code", func() bool { return len(codes) > 0 })
	// Still in view for ten more frames: not reported again.
	var v *cameraView
	onMain(func() { v = cameraCurrent })
	at := v.frames
	waitFor(t, "more frames", func() bool { return v.frames >= at+10 })
	onMain(func() {
		// Once while in view.
		if len(codes) != 1 || codes[0].Kind != scan.EAN13 || codes[0].Value != "4006381333931" {
			fatalf(t, "codes %+v", codes)
		}
	})
}

func TestCameraTake(t *testing.T) {
	useTestCamera(t, testCamera)
	h, _ := mount(t, emptyRoot)
	taken := callAsync(h, "camera", "take", nil)
	waitFor(t, "the dialog's camera", func() bool {
		return cameraTaking != nil && cameraTaking.view.pipe != 0 && cameraTaking.view.frames > 0
	})
	onMain(func() { click(cameraTaking.button) })
	b, err := awaitResult(t, "the photo", taken)
	if err != nil || !strings.HasSuffix(string(b), ".jpg") {
		t.Fatalf("take: %q %v", b, err)
	}
	if _, err := os.Stat(string(b)); err != nil {
		t.Fatal(err)
	}
	onMain(func() {
		if cameraTaking != nil {
			fatalf(t, "the dialog's still there")
		}
	})
	// Cancelled: "", no error.
	taken = callAsync(h, "camera", "take", nil)
	waitFor(t, "the dialog", func() bool { return cameraTaking != nil })
	onMain(func() { click(cameraTaking.cancel) })
	if b, err := awaitResult(t, "cancelled", taken); err != nil || len(b) != 0 {
		t.Fatalf("cancelled: %q %v", b, err)
	}
	// Closed (Escape): cancelled too.
	taken = callAsync(h, "camera", "take", nil)
	waitFor(t, "the dialog", func() bool { return cameraTaking != nil && cameraTaking.view.frames > 0 })
	onMain(func() { AdwDialogClose(cameraTaking.dialog) })
	if b, err := awaitResult(t, "closed", taken); err != nil || len(b) != 0 {
		t.Fatalf("closed: %q %v", b, err)
	}
}

// Without a camera: the portal has none (or isn't there) and there's no
// Video4Linux device.
func TestNoCamera(t *testing.T) {
	if err := LoadGstApp(); err != nil {
		t.Skip(err)
	}
	devs := cameraDevices
	cameraDevices = func() []string { return nil }
	defer func() { cameraDevices = devs }()
	var errs []string
	h, _ := mount(t, func() ui.Node {
		return camera.View(camera.ViewProps{Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(100)}, OnError: func(m string) { errs = append(errs, m) }})
	})
	if _, err := awaitResult(t, "take", callAsync(h, "camera", "take", nil)); !errIs(err, "no camera") {
		t.Fatalf("take: %v", err)
	}
	waitFor(t, "the view's error", func() bool { return len(errs) > 0 })
	onMain(func() {
		if errs[0] != "no camera" {
			fatalf(t, "error %q", errs[0])
		}
	})
	if _, err := callModule(t, h, "camera", "capture", nil); !errIs(err, "the camera isn't showing") {
		t.Fatalf("capture: %v", err)
	}
}

func TestWebAuth(t *testing.T) {
	needWebKit(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Path {
		case "/login":
			fmt.Fprint(w, `<p>Signing in</p><script>setTimeout(function(){ location.href = "MyApp://done?code=42" }, 50)</script>`)
		case "/redirect":
			http.Redirect(w, r, "myapp://done?code=7", http.StatusFound)
		default:
			fmt.Fprint(w, `<p>Waiting</p>`)
		}
	}))
	defer srv.Close()
	h, _ := mount(t, emptyRoot)
	args := func(id uint32, flags byte, link, scheme string) []byte {
		b := binary.LittleEndian.AppendUint32(nil, id)
		b = append(b, flags)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(link)))
		b = append(b, link...)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(scheme)))
		return append(b, scheme...)
	}
	b, err := awaitResult(t, "the callback", callAsync(h, "webAuth", "start", args(1, 0, srv.URL+"/login", "myapp")))
	if err != nil || string(b) != "MyApp://done?code=42" && string(b) != "myapp://done?code=42" {
		t.Fatalf("start: %q %v", b, err)
	}
	b, err = awaitResult(t, "a redirect", callAsync(h, "webAuth", "start", args(2, 1, srv.URL+"/redirect", "myapp")))
	if err != nil || string(b) != "myapp://done?code=7" {
		t.Fatalf("ephemeral, redirected: %q %v", b, err)
	}
	onMain(func() {
		if len(webAuthSessions) != 0 {
			fatalf(t, "sessions left: %d", len(webAuthSessions))
		}
	})
	// Canceled by the app, and by closing the dialog.
	r := callAsync(h, "webAuth", "start", args(3, 0, srv.URL+"/wait", "myapp"))
	waitFor(t, "the dialog", func() bool { return webAuthSessions[3] != nil })
	callModule(t, h, "webAuth", "cancel", binary.LittleEndian.AppendUint32(nil, 3))
	if _, err := awaitResult(t, "canceled", r); !errIs(err, "canceled") {
		t.Fatalf("cancel: %v", err)
	}
	r = callAsync(h, "webAuth", "start", args(4, 0, srv.URL+"/wait", "myapp"))
	waitFor(t, "the dialog", func() bool {
		s := webAuthSessions[4]
		return s != nil && !WebKitWebViewIsLoading(s.view) && WidgetGetMapped(s.view)
	})
	onMain(func() { AdwDialogClose(webAuthSessions[4].dialog) })
	if _, err := awaitResult(t, "closed", r); !errIs(err, "canceled") {
		t.Fatalf("closed: %v", err)
	}
	if _, err := callModule(t, h, "webAuth", "start", args(5, 0, "myapp://x", "myapp")); !errIs(err, "not a web URL") {
		t.Fatalf("not web: %v", err)
	}
}

func TestMapView(t *testing.T) {
	var loadErr error
	onMain(func() { loadErr = LoadShumate() })
	var errs []string
	var regions []maps.Region
	var pressed []string
	region := signal.New(maps.Region{Center: maps.Coordinate{Lat: 48.8566, Lng: 2.3522}, LatDelta: 0.05, LngDelta: 0.05})
	h, _ := mount(t, func() ui.Node {
		return maps.View(maps.Props{
			Region:         region,
			Markers:        signal.Static([]maps.Marker{{ID: "louvre", At: maps.Coordinate{Lat: 48.8606, Lng: 2.3376}, Title: "Louvre", Color: ui.RGB(255, 0, 0)}}),
			OnRegionChange: func(r maps.Region) { regions = append(regions, r) },
			OnMarkerPress:  func(id string) { pressed = append(pressed, id) },
			OnError:        func(m string) { errs = append(errs, m) },
			Style:          ui.Style{Width: ui.Pt(400), Height: ui.Pt(300)},
		})
	})
	if loadErr != nil {
		waitFor(t, "the error", func() bool { return len(errs) > 0 })
		t.Skip(loadErr)
	}
	var v *mapView
	waitFor(t, "the map", func() bool {
		for _, n := range find(h, ops.KindNative) {
			if nv, ok := n.ext.(*nativeView); ok && nv.view != nil {
				v, _ = nv.view.(*mapView)
			}
		}
		return v != nil && v.pending == nil && Call(FnWidgetGetWidth, v.m) > 0
	})
	onMain(func() {
		lat, lng := Shumate.LocationGetLatitude(v.vp), Shumate.LocationGetLongitude(v.vp)
		if math.Abs(lat-48.8566) > 1e-6 || math.Abs(lng-2.3522) > 1e-6 {
			fatalf(t, "centered on %v, %v", lat, lng)
		}
		// 0.05° across 400 points: zoom about 13.
		if z := Shumate.ViewportGetZoom(v.vp); z < 12 || z > 14 {
			fatalf(t, "zoom %v", z)
		}
		if len(v.markers) != 1 || len(regions) != 0 || len(errs) != 0 {
			fatalf(t, "markers %d, regions %v, errors %v", len(v.markers), regions, errs)
		}
	})
	// The user's move is reported; the app's isn't. (The region asked
	// for fits: 0.05° of latitude fills the height, which shows twice
	// the longitude across.)
	onMain(func() { Shumate.LocationSetLocation(v.vp, 48.86, 2.35) })
	waitFor(t, "the region", func() bool { return len(regions) == 1 })
	onMain(func() {
		r := regions[0]
		if math.Abs(r.Center.Lat-48.86) > 1e-6 || math.Abs(r.LatDelta-0.05) > 0.005 || r.LngDelta < 0.09 || r.LngDelta > 0.11 {
			fatalf(t, "region %+v", r)
		}
	})
	ui.Post(func() {
		region.Set(maps.Region{Center: maps.Coordinate{Lat: 51.5, Lng: -0.12}, LatDelta: 1, LngDelta: 1})
	})
	waitFor(t, "moved", func() bool { return math.Abs(Shumate.LocationGetLatitude(v.vp)-51.5) < 1e-6 })
	onMain(func() {
		if len(regions) != 1 {
			fatalf(t, "the app's move reported: %v", regions)
		}
	})
	_ = pressed
}

func TestSensorsWithoutProxy(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	var props map[string]any
	done := false
	onMain(func() { sensorProps(func(p map[string]any) { props, done = p, true }) })
	waitFor(t, "the proxy", func() bool { return done })
	if props != nil {
		t.Skip("iio-sensor-proxy runs here")
	}
	for _, k := range []byte{0, 1, 2, 3, 4, 5, 6} {
		if b, err := callModule(t, h, "sensors", "available", []byte{k}); err != nil || string(b) != "\x00" {
			t.Fatalf("available %d: %q %v", k, b, err)
		}
	}
	if _, err := callModule(t, h, "sensors", "start", binary.LittleEndian.AppendUint32([]byte{0}, 50000)); !errIs(err, "not available") {
		t.Fatalf("start: %v", err)
	}
	if _, err := callModule(t, h, "sensors", "steps", make([]byte, 16)); !errIs(err, "not available") {
		t.Fatalf("steps: %v", err)
	}
	// The wire: samples queued until "next", which "stopAll" ends.
	onMain(func() {
		sensorAdd(sensorAccelerometer, orientationGravity("left-up"))
		sensorAdd(sensorLight, []float64{320})
	})
	b, err := callModule(t, h, "sensors", "next", nil)
	if err != nil || len(b) != 2*10+8*4 || b[0] != 0 || b[9] != 3 || b[34] != 5 {
		t.Fatalf("next: %d bytes % x %v", len(b), b, err)
	}
	if x := math.Float64frombits(binary.LittleEndian.Uint64(b[10:])); x != -1 {
		t.Fatalf("left side up: x %v", x)
	}
	if at := math.Float64frombits(binary.LittleEndian.Uint64(b[1:])); at <= 0 {
		t.Fatalf("time %v", at)
	}
	next := callAsync(h, "sensors", "next", nil)
	callModule(t, h, "sensors", "stopAll", nil)
	if _, err := awaitResult(t, "stopped", next); !errIs(err, "stopped") {
		t.Fatalf("stopAll: %v", err)
	}
}
