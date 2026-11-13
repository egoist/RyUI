//go:build windows

package windows

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	stdimage "image"
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

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ui"
)

// What the modules answer where Windows has nothing to ask: each as the
// package's protocol has it.
func TestModulesAnswer(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		ok := func(module, m string, args []byte) []byte {
			b, err, answered := await(h, module, m, args)
			if !answered || err != nil {
				fatalf("%s.%s: %v (answered %v)", module, m, err, answered)
			}
			return b
		}
		fails := func(module, m string, args []byte, want string) {
			_, err, answered := await(h, module, m, args)
			if !answered || err == nil || !strings.Contains(err.Error(), want) {
				fatalf("%s.%s: %v, want %q", module, m, err, want)
			}
		}
		ok("splash", "hold", nil)
		ok("splash", "hide", nil)
		ok("screen", "lock", []byte{1})
		ok("screen", "bars", []byte{1})
		if b := ok("sensors", "available", []byte{0}); !bytes.Equal(b, []byte{0}) {
			fatalf("sensors.available: %v", b)
		}
		fails("sensors", "start", []byte{0}, "not available")
		fails("sensors", "next", nil, "not available")
		fails("widget", "set", nil, "not available")
		fails("liveActivity", "start", []byte(`{"id":"a","content":{}}`), "not available")
		if b := ok("liveActivity", "enabled", nil); !bytes.Equal(b, []byte{0}) {
			fatalf("liveActivity.enabled: %v", b)
		}
		if b := ok("tracking", "status", nil); len(b) != 1 || (b[0] != 2 && b[0] != 3) {
			fatalf("tracking.status: %v", b)
		}
		if b := ok("integrity", "supported", nil); !bytes.Equal(b, []byte{0}) {
			fatalf("integrity.supported: %v", b)
		}
		fails("integrity", "generateKey", nil, "App Attest")
		fails("integrity", "token", nil, "Play Integrity")
		fails("ageRange", "request", nil, "not available")
		if b := ok("appleAuth", "available", nil); !bytes.Equal(b, []byte{0}) {
			fatalf("appleAuth.available: %v", b)
		}
		fails("appleAuth", "signIn", nil, "not available")
		ok("review", "request", nil)
		var tr struct {
			OnDevice bool
			Language string
		}
		if json.Unmarshal(ok("transcribe", "check", []byte("en-US")), &tr) != nil || tr.OnDevice || tr.Language != "en-US" {
			fatalf("transcribe.check: %+v", tr)
		}
		fails("transcribe", "file", nil, "not available")
		if b := ok("contacts", "request", nil); !bytes.Equal(b, []byte{1}) {
			fatalf("contacts.request: %v", b)
		}
		fails("contacts", "list", nil, "not allowed")
		fails("contacts", "pick", nil, "no contact picker")
		if b := ok("calendar", "request", nil); !bytes.Equal(b, []byte{1}) {
			fatalf("calendar.request: %v", b)
		}
		fails("calendar", "events", nil, "not allowed")

		// Push: registering fails; a wait ends at stop.
		fails("push", "register", nil, "packaged")
		var waited error
		h.asyncModule("push")["nextReceived"](nil, func(_ []byte, e error) { waited = e })
		ok("push", "stop", []byte("nextReceived"))
		if waited == nil || waited.Error() != "stopped" {
			fatalf("push.nextReceived after stop: %v", waited)
		}
	})
}

// Adding a contact or an event opens its file in the user's app: a vCard,
// an iCalendar event.
func TestPersonalFiles(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	var opened []string
	old := openFile
	openFile = func(_ uintptr, path string) error { opened = append(opened, path); return nil }
	defer func() { openFile = old }()
	onMain(t, func() {
		c, _ := json.Marshal(map[string]any{"givenName": "Ada", "familyName": "Lovelace", "phones": []map[string]string{{"label": "mobile", "value": "+44 1"}}})
		if b, err, _ := await(h, "contacts", "add", c); err != nil || !bytes.Equal(b, []byte{0}) {
			fatalf("contacts.add: %v, %v", b, err)
		}
		start := time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC)
		e, _ := json.Marshal(map[string]any{"title": "Tea; then: work", "startMs": start.UnixMilli(), "endMs": start.Add(time.Hour).UnixMilli()})
		if _, err, _ := await(h, "calendar", "add", e); err != nil {
			fatalf("calendar.add: %v", err)
		}
	})
	if len(opened) != 2 {
		t.Fatalf("opened %v", opened)
	}
	vcf, _ := os.ReadFile(opened[0])
	ics, _ := os.ReadFile(opened[1])
	for _, want := range []string{"BEGIN:VCARD", "N:Lovelace;Ada;;;", "FN:Ada Lovelace", "TEL;TYPE=CELL:+44 1"} {
		if !strings.Contains(string(vcf), want) {
			t.Errorf("the vCard has no %q:\n%s", want, vcf)
		}
	}
	for _, want := range []string{"BEGIN:VEVENT", "DTSTART:20260301T093000Z", "DTEND:20260301T103000Z", `SUMMARY:Tea\; then: work`} {
		if !strings.Contains(string(ics), want) {
			t.Errorf("the event has no %q:\n%s", want, ics)
		}
	}
}

// PreventCapture keeps the app's windows out of captures, a window that
// shows later too, and lets them be seen again.
func TestPreventCapture(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		await(h, "screen", "preventCapture", []byte{1})
		if a := DisplayAffinity(h.window); a != 0x11 && a != 0x01 {
			fatalf("the window's affinity: %#x", a)
		}
		later := ownWindow(0, "later", WS_OVERLAPPEDWINDOW, 0, 0, 100, 100, 0, nil)
		ShowWindow(later, SW_SHOWNOACTIVATE)
		pumpUntil(func() bool { return DisplayAffinity(later) != 0 })
		if a := DisplayAffinity(later); a != 0x11 && a != 0x01 {
			fatalf("a later window's affinity: %#x", a)
		}
		await(h, "screen", "preventCapture", []byte{0})
		if a := DisplayAffinity(h.window); a != 0 {
			fatalf("the window's affinity after: %#x", a)
		}
		DestroyWindow(later)

		// A screenshot is waited for until stopped.
		var waited error
		answered := false
		h.asyncModule("screen")["screenshot"](nil, func(_ []byte, e error) { waited, answered = e, true })
		await(h, "screen", "stopScreenshots", nil)
		if !answered || waited == nil || waited.Error() != "stopped" {
			fatalf("screen.screenshot after stop: %v (answered %v)", waited, answered)
		}
	})
}

// A background task keeps the system awake; "next" waits until the last
// task ends.
func TestBackgroundTasks(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		if _, err, _ := await(h, "background", "next", nil); err == nil || err.Error() != "ended" {
			fatalf("next with no task: %v", err)
		}
		await(h, "background", "start", []byte(`{"id":1,"title":"Sync"}`))
		await(h, "background", "start", []byte(`{"id":2}`))
		var ended error
		answered := false
		h.asyncModule("background")["next"](nil, func(_ []byte, e error) { ended, answered = e, true })
		await(h, "background", "end", []byte(`{"id":1}`))
		if answered {
			fatalf("next answered with a task left: %v", ended)
		}
		await(h, "background", "end", []byte(`{"id":2}`))
		if !answered || ended == nil || ended.Error() != "ended" {
			fatalf("next after the last: %v (answered %v)", ended, answered)
		}
	})
}

// A periodic task due runs: "next" hands it over, "done" keeps when it
// ran.
func TestPeriodic(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	file := filepath.Join(t.TempDir(), "periodic.json")
	old := periodicFile
	periodicFile = func(*Host) string { return file }
	defer func() { periodicFile = old }()
	// Last ran long ago: due at once.
	os.WriteFile(file, []byte(`{"sync":{"every":900,"last":1}}`), 0o644)
	onMain(t, func() {
		args := binary.LittleEndian.AppendUint32(nil, 4)
		args = append(args, "sync"...)
		args = binary.LittleEndian.AppendUint32(args, 900)
		args = append(args, 0)
		await(h, "periodic", "schedule", args)
		b, err, answered := await(h, "periodic", "next", nil)
		if !answered || err != nil {
			fatalf("periodic.next: %v (answered %v)", err, answered)
		}
		name, rest := readString(b)
		if name != "sync" || len(rest) != 4 || binary.LittleEndian.Uint32(rest) != 25 {
			fatalf("periodic.next: %q %v", name, rest)
		}
		await(h, "periodic", "done", append(binary.LittleEndian.AppendUint32(nil, 4), "sync\x01"...))
		if last := h.periodicTasks()["sync"].Last; time.Since(time.Unix(last, 0)) > time.Minute {
			fatalf("done kept no time: %d", last)
		}
		if periodicState.timers["sync"] == nil {
			fatalf("sync isn't due again")
		}
		await(h, "periodic", "cancel", []byte("sync"))
		if _, kept := h.periodicTasks()["sync"]; kept || periodicState.timers["sync"] != nil {
			fatalf("cancel kept the task")
		}
	})
}

// library.save copies into the collection's folder (numbered when the
// name's taken), or moves.
func TestLibrarySave(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	dir := t.TempDir()
	old := libDir
	libDir = func(c int) (string, error) {
		return filepath.Join(dir, map[int]string{0: "Downloads", 3: "Pictures"}[c]), nil
	}
	defer func() { libDir = old }()
	src := filepath.Join(t.TempDir(), "a.txt")
	os.WriteFile(src, []byte("hi"), 0o644)
	onMain(t, func() {
		save := func(o map[string]any) string {
			args, _ := json.Marshal(o)
			b, err, _ := await(h, "library", "save", args)
			if err != nil {
				fatalf("library.save: %v", err)
			}
			return string(b)
		}
		a := save(map[string]any{"path": src, "collection": 0, "name": "a.txt", "folder": "App"})
		b := save(map[string]any{"path": src, "collection": 0, "name": "a.txt", "folder": "App"})
		if a != filepath.Join(dir, "Downloads", "App", "a.txt") || b != filepath.Join(dir, "Downloads", "App", "a (1).txt") {
			fatalf("saved to %s, %s", a, b)
		}
		c := save(map[string]any{"path": src, "collection": 3, "name": "b.txt", "move": true})
		if got, _ := os.ReadFile(c); string(got) != "hi" || c != filepath.Join(dir, "Pictures", "b.txt") {
			fatalf("moved to %s: %q", c, got)
		}
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			fatalf("the moved file's still there")
		}
		if _, err, _ := await(h, "library", "save", []byte(`{"path":"C:\\nope.txt"}`)); err == nil {
			fatalf("saved a missing file")
		}
	})
}

// The clipboard's image: copied from a file, read back as a PNG, and
// watched for changes.
func TestClipboardImage(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 7, 5))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.SetNRGBA(1, 1, color.NRGBA{255, 0, 0, 255})
	path := filepath.Join(t.TempDir(), "i.png")
	var buf bytes.Buffer
	png.Encode(&buf, img)
	os.WriteFile(path, buf.Bytes(), 0o644)
	onMain(t, func() {
		if _, err, _ := await(h, "clipboard", "copyImage", []byte(path)); err != nil {
			fatalf("copyImage: %v", err)
		}
		if b, _, _ := await(h, "clipboard", "has", nil); len(b) != 3 || b[1] != 1 {
			fatalf("has after copying an image: %v", b)
		}
		b, err, _ := await(h, "clipboard", "image", nil)
		if err != nil || len(b) == 0 {
			fatalf("image: %q, %v", b, err)
		}
		f, _ := os.Open(string(b))
		got, err := png.Decode(f)
		f.Close()
		if err != nil || got.Bounds().Dx() != 7 || got.Bounds().Dy() != 5 {
			fatalf("the clipboard's image: %v, %v", got, err)
		}
		if r, g, _, _ := got.At(1, 1).RGBA(); r>>8 != 255 || g != 0 {
			fatalf("the red pixel: %v", got.At(1, 1))
		}

		changed := false
		h.asyncModule("clipboard")["next"](nil, func(_ []byte, e error) { changed = e == nil })
		// Another app may have it open a moment (the clipboard history).
		pumpUntil(func() bool { return SetClipboardText(h.window, "changed") })
		if !pumpUntil(func() bool { return changed }) {
			fatalf("next didn't see the change")
		}
		var stopped error
		h.asyncModule("clipboard")["next"](nil, func(_ []byte, e error) { stopped = e })
		await(h, "clipboard", "stop", nil)
		if stopped == nil || stopped.Error() != "stopped" {
			fatalf("next after stop: %v", stopped)
		}
	})
}

// Windows Hello's answers, as asuka/sdk/biometric's: what the PC has, and the
// prompt's outcomes (Windows asked through seams: a test never meets the
// prompt).
func TestBiometric(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		// The real check: whatever this PC has, three bytes.
		if b, err, _ := await(h, "biometric", "check", nil); err != nil || len(b) != 3 {
			fatalf("biometric.check: %v, %v", b, err)
		}
	})
	oc, ov, os_ := helloCheck, helloVerify, helloSensor
	defer func() { helloCheck, helloVerify, helloSensor = oc, ov, os_ }()
	avail := HelloAvailable
	result := HelloVerified
	helloCheck = func(done func(HelloAvailability, error)) { go done(avail, nil) }
	helloVerify = func(_ uintptr, _ string, done func(HelloResult, error)) { go done(result, nil) }
	helloSensor = func() int { return 1 }
	args := append([]byte{0}, binary.LittleEndian.AppendUint32(nil, 3)...)
	args = append(append(args, "Pay"...), 0, 0, 0, 0)
	onMain(t, func() {
		if b, _, _ := await(h, "biometric", "check", nil); !bytes.Equal(b, []byte{1, 1, 1}) {
			fatalf("check, set up: %v", b)
		}
		for _, c := range []struct {
			r    HelloResult
			want string
		}{{HelloVerified, ""}, {HelloCanceled, "canceled"}, {HelloRetriesExhausted, "not available"}} {
			result = c.r
			_, err, _ := await(h, "biometric", "authenticate", args)
			if (err == nil && c.want != "") || (err != nil && err.Error() != c.want) {
				fatalf("authenticate %d: %v, want %q", c.r, err, c.want)
			}
		}
		avail = HelloNotConfigured
		if _, err, _ := await(h, "biometric", "authenticate", args); err == nil || err.Error() != "not available" {
			fatalf("authenticate, not set up: %v", err)
		}
		if b, _, _ := await(h, "biometric", "check", nil); !bytes.Equal(b, []byte{1, 0, 0}) {
			fatalf("check, not set up: %v", b)
		}
	})
}

// A web sign-in ends at the callback scheme's URL, the page's redirect to
// it stopped; or when the app cancels it.
func TestWebAuth(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/login" {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<title>Sign in</title><script>setTimeout(function(){location.href='/done'},50)</script>`))
			return
		}
		http.Redirect(w, r, "asukatest://signed-in?code=42", http.StatusFound)
	}))
	defer srv.Close()
	start := func(id uint32, flags byte, link, scheme string) []byte {
		b := binary.LittleEndian.AppendUint32(nil, id)
		b = append(b, flags)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(link)))
		b = append(b, link...)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(scheme)))
		return append(b, scheme...)
	}
	onMain(t, func() {
		b, err, answered := await(h, "webAuth", "start", start(1, 0, srv.URL+"/login", "asukatest"))
		if !answered || err != nil || string(b) != "asukatest://signed-in?code=42" {
			fatalf("webAuth: %q, %v (answered %v)", b, err, answered)
		}
		b, err, answered = await(h, "webAuth", "start", start(2, 1, srv.URL+"/login", "AsukaTest"))
		if !answered || err != nil || string(b) != "asukatest://signed-in?code=42" {
			fatalf("webAuth, ephemeral: %q, %v (answered %v)", b, err, answered)
		}
		var got error
		done := false
		h.asyncModule("webAuth")["start"](start(3, 0, srv.URL+"/login", ""), func(_ []byte, e error) { got, done = e, true })
		await(h, "webAuth", "cancel", binary.LittleEndian.AppendUint32(nil, 3))
		if !done || got == nil || got.Error() != "canceled" {
			fatalf("webAuth cancelled: %v (answered %v)", got, done)
		}
		if _, err, _ := await(h, "webAuth", "start", start(4, 0, "ftp://x", "")); err == nil || err.Error() != "not a web URL" {
			fatalf("webAuth of ftp: %v", err)
		}
		h.runPosted()
		for hwnd := range ownWindows {
			if TopLevel(hwnd) == hwnd && hwnd != ownMsgWin && IsWindow(hwnd) {
				fatalf("a sign-in window is left")
			}
		}
	})
}

// tone is a microphone of a test: a 440 Hz tone, 50 ms a read.
type tone struct {
	channels, rate int
	on             bool
	t              int
}

func (s *tone) Start() error       { s.on = true; return nil }
func (s *tone) Stop()              { s.on = false }
func (s *tone) Wait(uint32)        { time.Sleep(time.Millisecond) }
func (s *tone) Close(func([]byte)) {}
func (s *tone) Read(f func([]byte)) {
	if !s.on {
		return
	}
	n := s.rate / 20
	b := make([]byte, 0, n*2*s.channels)
	for i := 0; i < n; i++ {
		v := int16(8000 * math.Sin(2*math.Pi*440*float64(s.t)/float64(s.rate)))
		s.t++
		for c := 0; c < s.channels; c++ {
			b = binary.LittleEndian.AppendUint16(b, uint16(v))
		}
	}
	f(b)
}

// A recording: AAC in MPEG-4 (or ADTS, streaming), its status as it
// goes, paused and resumed, stopped, or ended at its maximum.
func TestAudioRecord(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	if !HasMicrophone() {
		onMain(t, func() {
			if _, err, _ := await(h, "audioRecord", "start", []byte{0, 0, 0, 0, 0, 0}); err == nil {
				fatalf("recording without a microphone")
			}
		})
	}
	old := openMicrophone
	openMicrophone = func(channels, rate int) (pcmSource, error) { return &tone{channels: channels, rate: rate}, nil }
	defer func() { openMicrophone = old }()
	dir := t.TempDir()
	start := func(quality byte, maxMs uint32, flags byte, path string) []byte {
		b := append([]byte{quality}, binary.LittleEndian.AppendUint32(nil, maxMs)...)
		b = append(b, flags, 0, 0, 0, 0)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(path)))
		return append(b, path...)
	}
	status := func() (level, recording byte, ms uint32) {
		b, _, _ := await(h, "audioRecord", "status", nil)
		return b[0], b[1], binary.LittleEndian.Uint32(b[2:])
	}
	onMain(t, func() {
		for _, c := range []struct {
			quality, flags byte
			name           string
		}{{0, 0, "voice.m4a"}, {1, 2, "music.aac"}} {
			path := filepath.Join(dir, c.name)
			var got string
			var gerr error
			done := false
			h.asyncModule("audioRecord")["start"](start(c.quality, 0, c.flags, path), func(b []byte, e error) { got, gerr, done = string(b), e, true })
			if _, err, _ := await(h, "audioRecord", "start", start(0, 0, 0, "")); err == nil || err.Error() != "already recording" {
				fatalf("a second recording: %v", err)
			}
			pumpUntil(func() bool { _, _, ms := status(); return ms >= 300 || done })
			level, rec, ms := status()
			if rec != 1 || level == 0 || ms < 300 {
				fatalf("status recording: %d %d %d (%v)", level, rec, ms, gerr)
			}
			await(h, "audioRecord", "pause", nil)
			pumpUntil(func() bool { _, rec, _ := status(); return rec == 0 })
			await(h, "audioRecord", "resume", nil)
			pumpUntil(func() bool { _, rec, _ := status(); return rec == 1 })
			await(h, "audioRecord", "stop", nil)
			if !pumpUntil(func() bool { return done }) || gerr != nil || got != path {
				fatalf("%s: %q, %v", c.name, got, gerr)
			}
			b, _ := os.ReadFile(path)
			switch {
			case c.flags&2 == 0 && (len(b) < 12 || string(b[4:8]) != "ftyp"):
				fatalf("%s isn't MPEG-4: % x", c.name, b[:min(len(b), 16)])
			case c.flags&2 != 0 && (len(b) < 7 || b[0] != 0xff || b[1]&0xf0 != 0xf0):
				fatalf("%s isn't ADTS: % x", c.name, b[:min(len(b), 16)])
			}
			t.Logf("%s: %d bytes", c.name, len(b))
		}
		// At its maximum it ends by itself, in the caches.
		b, err, _ := await(h, "audioRecord", "start", start(0, 500, 0, ""))
		if err != nil || !strings.HasSuffix(string(b), ".m4a") {
			fatalf("a recording to its maximum: %q, %v", b, err)
		}
		os.Remove(string(b))
		if _, rec, _ := status(); rec != 0 {
			fatalf("still recording")
		}
	})
}

// exifJPEG is a JPEG of img whose EXIF says it's turned (orientation o).
func exifJPEG(img stdimage.Image, o byte) []byte {
	var buf bytes.Buffer
	jpeg.Encode(&buf, img, nil)
	b := buf.Bytes()
	tiff := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 0x01, 3, 0, 1, 0, 0, 0, o, 0, 0, 0, 0, 0, 0, 0}
	app1 := append([]byte("Exif\x00\x00"), tiff...)
	seg := append([]byte{0xff, 0xe1, byte((len(app1) + 2) >> 8), byte(len(app1) + 2)}, app1...)
	return append(append(append([]byte{}, b[:2]...), seg...), b[2:]...)
}

// Images edited: decoded upright, resized, turned, cropped, filled,
// pixelated, written as PNG or JPEG; a video's frame.
func TestImageEdit(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	dir := t.TempDir()
	// 40×20: the left half red, the right blue.
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 40, 20))
	for y := 0; y < 20; y++ {
		for x := 0; x < 40; x++ {
			c := color.NRGBA{255, 0, 0, 255}
			if x >= 20 {
				c = color.NRGBA{0, 0, 255, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	pngPath := filepath.Join(dir, "a.png")
	var buf bytes.Buffer
	png.Encode(&buf, img)
	os.WriteFile(pngPath, buf.Bytes(), 0o644)
	turned := filepath.Join(dir, "turned.jpg")
	os.WriteFile(turned, exifJPEG(img, 6), 0o644)

	type result struct {
		Path          string
		Width, Height int
	}
	call := func(method string, req map[string]any) (result, stdimage.Image) {
		args, _ := json.Marshal(req)
		b, err, _ := await(h, "imageEdit", method, args)
		var r result
		if err != nil || json.Unmarshal(b, &r) != nil {
			fatalf("imageEdit.%s %v: %s, %v", method, req, b, err)
		}
		if r.Path == "" {
			return r, nil
		}
		f, _ := os.Open(r.Path)
		defer f.Close()
		got, _, err := stdimage.Decode(f)
		if err != nil {
			fatalf("the result %s: %v", r.Path, err)
		}
		if got.Bounds().Dx() != r.Width || got.Bounds().Dy() != r.Height {
			fatalf("the result's %v, said %dx%d", got.Bounds(), r.Width, r.Height)
		}
		return r, got
	}
	near := func(c color.Color, r, g, b uint32) bool {
		cr, cg, cb, _ := c.RGBA()
		d := func(a, b uint32) bool { return int(a>>8) >= int(b)-40 && int(a>>8) <= int(b)+40 }
		return d(cr, r) && d(cg, g) && d(cb, b)
	}
	onMain(t, func() {
		if r, _ := call("size", map[string]any{"source": turned}); r.Width != 20 || r.Height != 40 {
			fatalf("size of the turned JPEG: %+v", r)
		}
		if r, _ := call("size", map[string]any{"source": pngPath}); r.Width != 40 || r.Height != 20 {
			fatalf("size of the PNG: %+v", r)
		}
		// Upright: turned clockwise, its red half's on top.
		r, got := call("edit", map[string]any{"source": turned, "format": "png"})
		if r.Width != 20 || r.Height != 40 || !near(got.At(10, 5), 255, 0, 0) || !near(got.At(10, 35), 0, 0, 255) {
			fatalf("the turned JPEG upright: %+v %v %v", r, got.At(10, 5), got.At(10, 35))
		}
		r, got = call("edit", map[string]any{"source": pngPath, "format": "png", "actions": []map[string]any{
			{"op": "resize", "width": 20}, {"op": "rotate", "degrees": 90}, {"op": "flipV"}}})
		// 20×10, turned: 10×20, red on top; flipped: blue on top.
		if r.Width != 10 || r.Height != 20 || !near(got.At(5, 2), 0, 0, 255) || !near(got.At(5, 17), 255, 0, 0) {
			fatalf("resize, rotate, flip: %+v %v %v", r, got.At(5, 2), got.At(5, 17))
		}
		r, got = call("edit", map[string]any{"source": pngPath, "format": "jpeg", "quality": 0.8, "actions": []map[string]any{
			{"op": "crop", "x": 10, "y": 0, "width": 20, "height": 20}, {"op": "fill", "x": 0, "y": 0, "width": 5, "height": 5, "rgb": 0x00ff00},
			{"op": "pixelate", "x": 0, "y": 10, "width": 20, "height": 10, "block": 20}}})
		if r.Width != 20 || r.Height != 20 || !strings.HasSuffix(r.Path, ".jpg") || !near(got.At(2, 2), 0, 255, 0) || !near(got.At(12, 6), 0, 0, 255) {
			fatalf("crop, fill: %+v %v %v", r, got.At(2, 2), got.At(12, 6))
		}
		if !near(got.At(2, 15), 128, 0, 128) || !near(got.At(18, 15), 128, 0, 128) {
			fatalf("pixelated: %v %v", got.At(2, 15), got.At(18, 15))
		}
		r, _ = call("edit", map[string]any{"source": pngPath, "format": "png", "actions": []map[string]any{{"op": "rotate", "degrees": 45}}})
		if r.Width < 42 || r.Width > 44 || r.Height < 42 || r.Height > 44 {
			fatalf("rotated 45°: %+v", r)
		}
		if _, err, _ := await(h, "imageEdit", "edit", []byte(`{"source":"C:\\nope.png"}`)); err == nil || err.Error() != "no image" {
			fatalf("a missing image: %v", err)
		}
	})

	// A video's frame: a second of frames, the first half red, then blue.
	video := filepath.Join(dir, "v.mp4")
	var werr error
	WithCOM(func() {
		v, err := NewVideoWriter(video, 64, 48, 10)
		if err != nil {
			werr = err
			return
		}
		for i := 0; i < 10; i++ {
			frame := make([]byte, 64*48*4)
			for p := 0; p < len(frame); p += 4 {
				if i < 5 {
					frame[p+2] = 255
				} else {
					frame[p] = 255
				}
			}
			if werr = v.Write(frame); werr != nil {
				return
			}
		}
		werr = v.Close()
	})
	if werr != nil {
		t.Fatalf("writing a video: %v", werr)
	}
	onMain(t, func() {
		r, got := call("videoFrame", map[string]any{"source": video, "at": 0, "format": "png"})
		if r.Width != 64 || r.Height != 48 || !near(got.At(30, 20), 255, 0, 0) {
			fatalf("the first frame: %+v %v", r, got.At(30, 20))
		}
		_, got = call("videoFrame", map[string]any{"source": video, "at": 800, "format": "png", "actions": []map[string]any{{"op": "resize", "width": 32}}})
		if got.Bounds().Dx() != 32 || !near(got.At(10, 10), 0, 0, 255) {
			fatalf("the frame at 0.8 s: %v %v", got.Bounds(), got.At(10, 10))
		}
	})
}

// The photo library is the Pictures folder: listed newest first, its
// screenshots apart, thumbnails, copies.
func TestPhotoLibrary(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	root := t.TempDir()
	old := photoRoot
	photoRoot = func() (string, error) { return root, nil }
	defer func() { photoRoot = old }()
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, 600, 400))
	var buf bytes.Buffer
	png.Encode(&buf, img)
	os.MkdirAll(filepath.Join(root, "Screenshots"), 0o755)
	os.WriteFile(filepath.Join(root, "old.png"), buf.Bytes(), 0o644)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(root, "Screenshots", "shot.png"), buf.Bytes(), 0o644)
	type asset struct {
		ID            string
		Width, Height int
		Created       int64
		Screenshot    bool
	}
	onMain(t, func() {
		if b, _, _ := await(h, "photoLibrary", "request", nil); string(b) != "allowed" {
			fatalf("request: %s", b)
		}
		list := func(q string) []asset {
			b, err, _ := await(h, "photoLibrary", "list", []byte(q))
			var as []asset
			if err != nil || json.Unmarshal(b, &as) != nil {
				fatalf("list %s: %s, %v", q, b, err)
			}
			return as
		}
		all := list(`{}`)
		if len(all) != 2 || all[0].ID != "Screenshots/shot.png" || !all[0].Screenshot || all[1].ID != "old.png" || all[1].Width != 600 || all[1].Height != 400 {
			fatalf("list: %+v", all)
		}
		if shots := list(`{"screenshots":true}`); len(shots) != 1 || shots[0].ID != "Screenshots/shot.png" {
			fatalf("screenshots: %+v", shots)
		}
		if page := list(`{"offset":1,"limit":5}`); len(page) != 1 || page[0].ID != "old.png" {
			fatalf("a page: %+v", page)
		}
		b, err, _ := await(h, "photoLibrary", "thumbnail", []byte(`{"id":"old.png","size":100}`))
		if err != nil {
			fatalf("thumbnail: %v", err)
		}
		f, _ := os.Open(string(b))
		c, _, err := stdimage.DecodeConfig(f)
		f.Close()
		if err != nil || c.Height != 100 || c.Width != 150 {
			fatalf("the thumbnail: %+v, %v", c, err)
		}
		if b2, _, _ := await(h, "photoLibrary", "thumbnail", []byte(`{"id":"old.png","size":100}`)); string(b2) != string(b) {
			fatalf("the thumbnail again: %s", b2)
		}
		b, err, _ = await(h, "photoLibrary", "export", []byte(`{"id":"old.png"}`))
		var e struct{ Path, Name string }
		if err != nil || json.Unmarshal(b, &e) != nil || e.Name != "old.png" {
			fatalf("export: %s, %v", b, err)
		}
		if got, _ := os.ReadFile(e.Path); !bytes.Equal(got, buf.Bytes()) {
			fatalf("the export isn't the photo")
		}
		if _, err, _ := await(h, "photoLibrary", "export", []byte(`{"id":"../x.png"}`)); err == nil {
			fatalf("exported outside the library")
		}
	})
}

// textImage is an image of text, black on white, drawn by GDI.
func textImage(t *testing.T, text string) string {
	const w, ht = 640, 160
	dc := CreateCompatibleDC(0)
	bmp, pixels := DIB(w, ht)
	old := SelectObject(dc, bmp)
	r := Rect{Right: w, Bottom: ht}
	white := CreateSolidBrush(0xffffff)
	FillRect(dc, &r, white)
	DeleteObject(white)
	lf := LogFont{Height: -56, Weight: 400, Quality: 5}
	copy(lf.FaceName[:], UTF16Units("Segoe UI"))
	font := CreateFont(&lf)
	oldFont := SelectObject(dc, font)
	SetBkMode(dc, 1)
	SetTextColor(dc, 0)
	TextOut(dc, 30, 40, UTF16Units(text))
	SelectObject(dc, oldFont)
	DeleteObject(font)
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, w, ht))
	for i := 0; i < w*ht; i++ {
		img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] = pixels[i*4+2], pixels[i*4+1], pixels[i*4], 255
	}
	SelectObject(dc, old)
	DeleteObject(bmp)
	DeleteDC(dc)
	path := filepath.Join(t.TempDir(), "text.png")
	f, _ := os.Create(path)
	png.Encode(f, img)
	f.Close()
	return path
}

// Vision reads an image's text, line and words with their boxes; finds
// no faces in it; scan.file says Windows reads no codes.
func TestVision(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	var path string
	onMain(t, func() { path = textImage(t, "Hello World 42") })
	onMain(t, func() {
		args, _ := json.Marshal(map[string]any{"path": path, "codes": true})
		b, err, _ := await(h, "vision", "analyze", args)
		if err != nil && strings.Contains(err.Error(), "no OCR language") {
			t.Logf("no OCR here: %v", err)
			args, _ = json.Marshal(map[string]any{"path": path, "text": false})
			b, err, _ = await(h, "vision", "analyze", args)
		}
		var r struct {
			Width, Height int
			Lines         []struct {
				Text  string
				Box   struct{ X, Y, W, H float64 }
				Words []struct {
					Text       string
					Start, End int
					Box        struct{ X, Y, W, H float64 }
				}
			}
			Faces []any
			Codes []any
		}
		if err != nil || json.Unmarshal(b, &r) != nil {
			fatalf("vision.analyze: %s, %v", b, err)
		}
		t.Logf("%s", b)
		if r.Width != 640 || r.Height != 160 || len(r.Faces) != 0 || r.Codes == nil || len(r.Codes) != 0 {
			fatalf("vision.analyze: %+v", r)
		}
		if len(r.Lines) > 0 {
			l := r.Lines[0]
			if l.Text != "Hello World 42" || len(l.Words) != 3 || l.Words[1].Text != "World" || l.Words[1].Start != 6 || l.Words[1].End != 11 {
				fatalf("the line: %+v", l)
			}
			if l.Box.X < 0.02 || l.Box.X > 0.1 || l.Box.Y < 0.15 || l.Box.Y > 0.45 || l.Words[1].Box.X <= l.Words[0].Box.X {
				fatalf("the boxes: %+v", l)
			}
		}
		if _, err, _ := await(h, "vision", "analyze", []byte(`{"path":"C:\\nope.png"}`)); err == nil || err.Error() != "no image" {
			fatalf("a missing image: %v", err)
		}
		// Text, no codes: none.
		if b, err, _ := await(h, "scan", "file", []byte("qr\n"+path)); err != nil || string(b) != "\x00\x00\x00\x00" {
			fatalf("scan.file of text: %q, %v", b, err)
		}
	})
}

// The camera, on a PC without one: Take and the view say so; with a
// camera the engine can't open, the take dialog shows over the window
// (which waits), cancels with Escape, and fails as the view does.
func TestCamera(t *testing.T) {
	var errs []string
	h, _ := mount(t, func() ui.Node {
		return ui.Native("asuka.camera", ui.NativeProps{
			Style: ui.Style{Width: ui.Pt(200), Height: ui.Pt(150)},
			Props: map[string]any{"facing": "back", "video": true},
			On:    map[string]func(ui.NativeEvent){"error": func(e ui.NativeEvent) { errs = append(errs, e.String("message")) }},
		})
	})
	onMain(t, func() {
		pumpUntil(func() bool { return len(errs) > 0 })
		if HasCamera() {
			t.Logf("a camera: %v", errs)
			return
		}
		if len(errs) != 1 || errs[0] != "no camera" {
			fatalf("the view's errors: %v", errs)
		}
		if _, err, _ := await(h, "camera", "take", nil); err == nil || err.Error() != "no camera" {
			fatalf("take: %v", err)
		}
		if _, err, _ := await(h, "camera", "capture", nil); err == nil || err.Error() != "the camera isn't showing" {
			fatalf("capture: %v", err)
		}
		if _, err, _ := await(h, "camera", "record", []byte{0, 0, 0, 0, 0}); err == nil || err.Error() != "the camera isn't showing" {
			fatalf("record: %v", err)
		}
	})
	old := cameraDevices
	cameraDevices = func() ([]VideoDevice, error) { return []VideoDevice{{Name: "Test Camera"}}, nil }
	defer func() { cameraDevices = old }()
	onMain(t, func() {
		var got []byte
		var gerr error
		done := false
		h.asyncModule("camera")["take"](nil, func(b []byte, e error) { got, gerr, done = b, e, true })
		s := cameraTaking
		if s == nil || !IsWindow(s.d.hwnd) || IsWindowEnabled(s.d.owner) {
			fatalf("the take dialog: %+v", s)
		}
		SendMessage(s.d.hwnd, WM_KEYDOWN, 0x1B, 0)
		if !done || gerr != nil || got == nil || len(got) != 0 || IsWindow(s.d.hwnd) || !IsWindowEnabled(s.d.owner) {
			fatalf("cancelled: %q, %v, %v", got, gerr, done)
		}
		done = false
		h.asyncModule("camera")["take"](nil, func(b []byte, e error) { got, gerr, done = b, e, true })
		if !pumpUntil(func() bool { return done }) || gerr == nil || gerr.Error() != "no camera" {
			fatalf("a camera that won't open: %q, %v", got, gerr)
		}
	})
}

// Every module and method Asuka's packages call has an answer on
// Windows (the audit: grep ui.Call, ui.CallWait in the packages).
func TestEveryModuleAnswers(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	async := map[string]string{
		"ageRange":     "request",
		"appleAuth":    "available signIn state",
		"audio":        "load play playOnce pause seek volume loop rate release status",
		"audioRecord":  "level pause resume start status stop",
		"background":   "end next start update",
		"biometric":    "authenticate check",
		"calendar":     "add calendars events request",
		"camera":       "capture record stopRecording take",
		"clipboard":    "copyImage has image next stop",
		"compose":      "can mail message",
		"contacts":     "add list pick request",
		"dialog":       "folder open save",
		"files":        "pick",
		"imageEdit":    "edit size videoFrame",
		"integrity":    "assert attestKey generateKey prepare supported token",
		"js":           "eval",
		"library":      "save",
		"location":     "current name next stop",
		"network":      "current next stop",
		"notify":       "actionDone badge cancel categories channelGroups channels present presented remove removeAll removeThread request schedule showing stop nextAction nextPresent",
		"periodic":     "cancel done next schedule",
		"photoLibrary": "current export list manage request thumbnail",
		"photos":       "pick",
		"preview":      "show",
		"print":        "file html webView pdf",
		"push":         "nextRewrite register rewritten stop nextReceived nextOpened",
		"review":       "request",
		"scan":         "file",
		"screen":       "bars lock preventCapture screenshot stopScreenshots",
		"sensors":      "available next start steps stop stopAll",
		"share":        "files",
		"shell":        "beep open reveal trash",
		"speech":       "pause resume speak speaking stop voices",
		"splash":       "hide hold",
		"tracking":     "request status",
		"transcribe":   "check file",
		"vision":       "analyze",
		"web":          "eval cookies",
		"webAuth":      "cancel start",
		"widget":       "set",
	}
	onMain(t, func() {
		for module, methods := range async {
			m := h.asyncModule(module)
			for _, name := range strings.Fields(methods) {
				if m[name] == nil {
					t.Errorf("%s.%s has no answer on Windows", module, name)
				}
			}
		}
		for _, module := range []string{"secureStore", "device", "browser"} {
			if h.module(module) == nil {
				t.Errorf("%s has no answer on Windows", module)
			}
		}
		for _, kind := range []string{"asuka.camera", "asuka.map"} {
			if builtinViews[kind] == nil {
				t.Errorf("no %s view on Windows", kind)
			}
		}
	})
}
