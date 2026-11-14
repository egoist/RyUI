//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/asuka/colors"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// callNow calls a module's method that answers at once.
func callNow(t *testing.T, h *Host, module, m string, args []byte) ([]byte, error, bool) {
	t.Helper()
	f := h.module(module)[m]
	if f == nil {
		fatalf(t, "%s.%s isn't on macOS", module, m)
	}
	var out []byte
	var err error
	answered := false
	f(args, func(b []byte, e error) { out, err, answered = b, e, true })
	return out, err, answered
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Every module and method the repo's packages call is on macOS (an
// answer, if only "not available"), so none fails "isn't on macOS yet".
func TestSystemModulesThere(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		want := map[string][]string{
			"background":   {"start", "update", "end", "next"},
			"sensors":      {"available", "start", "stop", "stopAll", "next", "steps", "simulate"},
			"widget":       {"set"},
			"liveActivity": {"enabled", "start", "update", "end", "running", "next", "stop"},
			"purchase":     {"available", "products", "buy", "finish", "entitlements", "restore", "status", "manage", "refund", "redeem", "storefront", "next", "stop"},
			"nfc":          {"state", "nextState", "stopState", "settings", "start", "next", "restart", "alert", "stop", "read", "write", "lock", "status", "transceive", "classic", "nextOpened", "stopOpened"},
			"splash":       {"hold", "hide"},
			"screen":       {"lock", "bars", "preventCapture", "screenshot", "stopScreenshots"},
			"tracking":     {"request", "status"},
			"integrity":    {"supported", "generateKey", "attestKey", "assert", "prepare", "token"},
			"ageRange":     {"request"},
			"push":         {"register", "nextReceived", "nextOpened", "stop", "nextRewrite", "rewritten"},
			"periodic":     {"schedule", "cancel", "next", "done"},
			"appleAuth":    {"available", "signIn", "state"},
			"webAuth":      {"start", "cancel"},
			"camera":       {"take", "capture", "record", "stopRecording"},
			"audioRecord":  {"start", "stop", "pause", "resume", "level", "status"},
			"contacts":     {"request", "list", "pick", "add"},
			"calendar":     {"request", "calendars", "events", "add"},
			"photoLibrary": {"current", "request", "manage", "list", "thumbnail", "export"},
			"library":      {"save"},
			"scan":         {"file"},
			"vision":       {"analyze"},
			"transcribe":   {"check", "file"},
			"imageEdit":    {"edit", "size", "videoFrame"},
		}
		for _, kind := range []string{"asuka.camera", "asuka.map", "asuka.appleSignIn"} {
			if builtinViews[kind] == nil {
				t.Errorf("no %s view on macOS", kind)
			}
		}
		for mod, ms := range want {
			for _, m := range ms {
				if h.module(mod)[m] == nil {
					t.Errorf("%s.%s isn't on macOS", mod, m)
				}
			}
		}
	})
}

// A Mac has no motion sensors: none is available, and watching ends.
func TestSensorsNotAvailable(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		for s := byte(0); s <= 6; s++ {
			if b, err, _ := callNow(t, h, "sensors", "available", []byte{s}); err != nil || len(b) != 1 || b[0] != 0 {
				t.Errorf("sensor %d available: %v, %v", s, b, err)
			}
		}
		for _, m := range []string{"start", "next", "steps"} {
			if _, err, ok := callNow(t, h, "sensors", m, []byte{0, 0, 0, 0, 0}); !ok || errText(err) != "not available" {
				t.Errorf("sensors.%s: %v, answered %v; want not available", m, err, ok)
			}
		}
		if _, err, ok := callNow(t, h, "sensors", "stopAll", nil); !ok || err != nil {
			t.Errorf("stopAll: %v", err)
		}
	})
}

// A task holds an activity until the last one ends; "next" waits for it.
func TestBackgroundTasks(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		if _, err, _ := callNow(t, h, "background", "next", nil); errText(err) != "ended" {
			fatalf(t, "next with no task: %v, want ended", err)
		}
		callNow(t, h, "background", "start", []byte(`{"id":1,"title":"Sync"}`))
		callNow(t, h, "background", "start", []byte(`{"id":2}`))
		if bgTasks.activity == 0 {
			fatal(t, "no activity while tasks run")
		}
		var nextErr error
		answered := false
		h.module("background")["next"](nil, func(_ []byte, err error) { nextErr, answered = err, true })
		if answered {
			fatal(t, "next answered while tasks run")
		}
		if _, err, ok := callNow(t, h, "background", "update", []byte(`{"id":1,"progress":0.5}`)); !ok || err != nil {
			fatalf(t, "update: %v", err)
		}
		callNow(t, h, "background", "end", []byte(`{"id":1}`))
		if answered || bgTasks.activity == 0 {
			fatal(t, "one task's end ended them all")
		}
		callNow(t, h, "background", "end", []byte(`{"id":2}`))
		if !answered || errText(nextErr) != "ended" || bgTasks.activity != 0 {
			fatalf(t, "after the last end: next %v (answered %v), activity %v", nextErr, answered, bgTasks.activity)
		}
	})
}

func TestScreenAndOthers(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		for _, c := range []struct{ mod, m string }{{"screen", "lock"}, {"screen", "bars"}, {"splash", "hold"}, {"splash", "hide"}, {"push", "rewritten"}} {
			if _, err, ok := callNow(t, h, c.mod, c.m, []byte{1, 0}); !ok || err != nil {
				t.Errorf("%s.%s: %v", c.mod, c.m, err)
			}
		}
		// Capture: the windows' sharing type.
		callNow(t, h, "screen", "preventCapture", []byte{1})
		if s := SendInt(h.window, "sharingType"); s != 0 {
			t.Errorf("preventing capture: the window's sharing type is %d, want none (0)", s)
		}
		callNow(t, h, "screen", "preventCapture", []byte{0})
		if s := SendInt(h.window, "sharingType"); s != 1 {
			t.Errorf("allowing capture: the window's sharing type is %d, want read only (1)", s)
		}
		// Screenshots: waiting until stopped.
		var shotErr error
		answered := false
		h.module("screen")["screenshot"](nil, func(_ []byte, err error) { shotErr, answered = err, true })
		if answered && shotErr == nil {
			fatal(t, "screenshot answered without one")
		}
		if !answered {
			callNow(t, h, "screen", "stopScreenshots", nil)
			if !answered || errText(shotErr) != "stopped" {
				t.Errorf("stopScreenshots: %v (answered %v), want stopped", shotErr, answered)
			}
		}
		if _, err, _ := callNow(t, h, "widget", "set", []byte(`{"kind":"k","entries":[]}`)); err == nil {
			t.Error("widget.set succeeded with no widget extension")
		}
		if b, err, _ := callNow(t, h, "liveActivity", "enabled", nil); err != nil || len(b) != 1 || b[0] != 0 {
			t.Errorf("liveActivity.enabled: %v, %v", b, err)
		}
		if _, err, _ := callNow(t, h, "liveActivity", "start", []byte(`{"id":"a","content":{}}`)); !strings.HasPrefix(errText(err), "not available") {
			t.Errorf("liveActivity.start: %v", err)
		}
		// The test's process has no Swift library: no StoreKit.
		if b, err, _ := callNow(t, h, "purchase", "available", nil); err != nil || len(b) != 1 || b[0] != 0 {
			t.Errorf("purchase.available: %v, %v", b, err)
		}
		if _, err, _ := callNow(t, h, "purchase", "products", []byte(`{"items":[{"id":"pro","kind":"nonConsumable"}]}`)); !strings.HasPrefix(errText(err), "not available") {
			t.Errorf("purchase.products: %v", err)
		}
		if b, err, _ := callNow(t, h, "nfc", "state", nil); err != nil || string(b) != `{"supported":false,"enabled":false}` {
			t.Errorf("nfc.state: %s, %v", b, err)
		}
		if _, err, _ := callNow(t, h, "nfc", "start", []byte(`{"id":1}`)); !strings.HasPrefix(errText(err), "not available: ") {
			t.Errorf("nfc.start: %v", err)
		}
		if _, err, _ := callNow(t, h, "ageRange", "request", nil); errText(err) != "not available" {
			t.Errorf("ageRange.request: %v", err)
		}
		if b, err, _ := callNow(t, h, "tracking", "status", nil); err != nil || len(b) != 1 || b[0] > 3 {
			t.Errorf("tracking.status: %v, %v", b, err)
		}
		if b, err, _ := callNow(t, h, "integrity", "supported", nil); err != nil || len(b) != 1 {
			t.Errorf("integrity.supported: %v, %v", b, err)
		}
		for _, m := range []string{"attestKey", "assert"} {
			if _, err, _ := callNow(t, h, "integrity", m, []byte{1, 2, 3}); errText(err) != "bad args" {
				t.Errorf("integrity.%s with bad args: %v", m, err)
			}
		}
		if _, err, _ := callNow(t, h, "integrity", "token", nil); err == nil {
			t.Error("Play Integrity's token answered on a Mac")
		}
	})
}

func TestPush(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		// What a push says, as push.Message.
		payload := map[string]any{"aps": map[string]any{"alert": map[string]any{"title": "T", "body": "B", "subtitle": "S"},
			"badge": 3.0, "thread-id": "th", "category": "c"}, "url": "https://x", "n": 2.0, "o": map[string]any{"a": 1.0}}
		var m struct {
			ID, Title, Body, Subtitle, Category, Thread string
			Badge                                       int
			Data                                        map[string]string
		}
		json.Unmarshal(pushMessageOf(payload, "id1", "", ""), &m)
		if m.ID != "id1" || m.Title != "T" || m.Body != "B" || m.Subtitle != "S" || m.Badge != 3 || m.Thread != "th" || m.Category != "c" ||
			m.Data["url"] != "https://x" || m.Data["n"] != "2" || m.Data["o"] != `{"a":1}` || len(m.Data) != 3 {
			t.Errorf("a push's message: %+v", m)
		}
		json.Unmarshal(pushMessageOf(map[string]any{"aps": map[string]any{"alert": "just text"}}, "", "", ""), &m)
		if m.Body != "just text" {
			t.Errorf("an alert string: body %q", m.Body)
		}
		// Through an NSDictionary, as the delegate gets it.
		info := Dict(String("aps"), Dict(String("alert"), String("hi")), String("k"), String("v"))
		json.Unmarshal(pushMessage(info, "", "", ""), &m)
		if m.Body != "hi" || m.Data["k"] != "v" {
			t.Errorf("from userInfo: %+v", m)
		}

		// A test binary has no bundle: APNs can't be asked.
		if _, err, ok := callNow(t, h, "push", "register", nil); !ok || err == nil {
			t.Errorf("register without a bundle: %v (answered %v)", err, ok)
		}
		// APNs's answer goes to who registered.
		var reg []byte
		pushState.registering = append(pushState.registering, func(b []byte, err error) { reg = b })
		pushState.allowed = true
		pushRegistered([]byte{0xab, 0x01}, nil)
		if string(reg) != `{"allowed":true,"service":"apns","token":"ab01"}` {
			t.Errorf("registered: %s", reg)
		}
		// Messages wait to be asked for; stop fails the waiting.
		pushDeliver("nextReceived", []byte(`{"id":"a"}`))
		if b, err, ok := callNow(t, h, "push", "nextReceived", nil); !ok || err != nil || string(b) != `{"id":"a"}` {
			t.Errorf("a kept message: %s, %v", b, err)
		}
		var waitErr error
		h.module("push")["nextOpened"](nil, func(_ []byte, err error) { waitErr = err })
		callNow(t, h, "push", "stop", []byte("nextOpened"))
		if errText(waitErr) != "stopped" {
			t.Errorf("stopped: %v", waitErr)
		}
		if _, err, _ := callNow(t, h, "push", "nextRewrite", nil); err == nil {
			t.Error("nextRewrite answered on a Mac")
		}
	})
}

func TestPeriodic(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		if _, err, _ := callNow(t, h, "periodic", "schedule", []byte{1}); errText(err) != "bad args" {
			t.Errorf("schedule with bad args: %v", err)
		}
		name := "asuka-test-task"
		args := binary.LittleEndian.AppendUint32(nil, uint32(len(name)))
		args = append(args, name...)
		args = binary.LittleEndian.AppendUint32(args, 3600)
		args = append(args, 3) // network, charging
		if _, err, _ := callNow(t, h, "periodic", "schedule", args); err != nil {
			fatal(t, err)
		}
		defer callNow(t, h, "periodic", "cancel", []byte(name))
		if tk, kept := periodicTasks()[name]; !kept || tk.Every != 3600 || !tk.Network || !tk.Charging {
			t.Errorf("kept: %+v %v", tk, kept)
		}
		if periodicState.schedulers[name] == 0 {
			fatal(t, "no scheduler")
		}
		// Go waits; the scheduler's turn hands it the task, and done ends
		// the turn.
		var run []byte
		h.module("periodic")["next"](nil, func(b []byte, err error) { run = b })
		result := -1
		completion := NewBlock(func(_ objc.Block, r int) { result = r })
		defer completion.Release()
		done := uintptr(Retain(Send(ID(completion), "copy")))
		// Without what it needs (charging), it's deferred.
		ts := periodicTasks()
		tk := ts[name]
		tk.Charging, tk.Network = false, false
		ts[name] = tk
		setPeriodicTasks(ts)
		periodicFire(name, done)
		want := binary.LittleEndian.AppendUint32(binary.LittleEndian.AppendUint32(nil, uint32(len(name))), 0)
		want = append(want[:4], name...)
		want = binary.LittleEndian.AppendUint32(want, 25)
		if string(run) != string(want) {
			fatalf(t, "next: %v, want %v", run, want)
		}
		if result != -1 {
			fatalf(t, "the turn ended before Go was done: %d", result)
		}
		doneArgs := append(binary.LittleEndian.AppendUint32(nil, uint32(len(name))), name...)
		callNow(t, h, "periodic", "done", append(doneArgs, 1))
		if result != activityFinished {
			t.Errorf("the turn's result: %d, want finished", result)
		}
		if periodicTasks()[name].Last == 0 {
			t.Error("done didn't keep the run's time")
		}
	})
}

func TestAuth(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		if b, err, _ := callNow(t, h, "appleAuth", "available", nil); err != nil || len(b) != 1 || b[0] != 1 {
			t.Errorf("appleAuth.available: %v, %v", b, err)
		}
		for s, want := range map[int]byte{0: 0, 1: 1, 2: 2, 3: 3, 7: 2, -1: 2} {
			if got := appleCredentialState(s); got != want {
				t.Errorf("credential state %d: %d, want %d", s, got, want)
			}
		}
		for _, c := range []struct {
			style int
			dark  bool
			want  uintptr
		}{{0, false, 2}, {1, false, 0}, {2, false, 1}, {3, false, 2}, {3, true, 0}} {
			if got := appleButtonStyle(c.style, c.dark); got != c.want {
				t.Errorf("style %d (dark %v): %d, want %d", c.style, c.dark, got, c.want)
			}
		}
		// webAuth: its arguments, and what it turns down.
		args := binary.LittleEndian.AppendUint32(nil, 7)
		args = append(args, 1)
		args = binary.LittleEndian.AppendUint32(args, uint32(len("ftp://x")))
		args = append(args, "ftp://x"...)
		args = binary.LittleEndian.AppendUint32(args, 3)
		args = append(args, "app"...)
		if id, flags, link, scheme, good := decodeWebAuth(args); !good || id != 7 || flags != 1 || link != "ftp://x" || scheme != "app" {
			t.Errorf("decoded: %d %d %q %q %v", id, flags, link, scheme, good)
		}
		if _, err, _ := callNow(t, h, "webAuth", "start", args); errText(err) != "not a web URL" {
			t.Errorf("start with ftp: %v", err)
		}
		if _, err, _ := callNow(t, h, "webAuth", "start", args[:6]); errText(err) != "bad args" {
			t.Errorf("start with bad args: %v", err)
		}
		if _, err, ok := callNow(t, h, "webAuth", "cancel", binary.LittleEndian.AppendUint32(nil, 99)); !ok || err != nil {
			t.Errorf("cancel of no session: %v", err)
		}

		// The button.
		parent := MsgInitRect(Send(Class("NSView"), "alloc"), Sel("initWithFrame:"), Rect{Size: Size{Width: 300, Height: 40}})
		defer Release(parent)
		var pressed int
		v := builtinViews["asuka.appleSignIn"](native.NewContext(uintptr(parent), func(name string, _ map[string]any) {
			if name == "press" {
				pressed++
			}
		}))
		if v == nil {
			fatal(t, "no Sign in with Apple button")
		}
		b := v.(*appleSignInButton)
		v.Set("type", int64(1))
		v.Set("style", int64(3))
		v.Set("cornerRadius", 6.0)
		if !SendBool(b.button, "isKindOfClass:", uintptr(Class("ASAuthorizationAppleIDButton"))) {
			fatal(t, "the button isn't Apple's")
		}
		if r := MsgFloat(b.button, Sel("cornerRadius")); math.Abs(r-6) > 0.01 {
			t.Errorf("corner radius %v", r)
		}
		if w, ht := v.(native.Measurer).Measure(float32(math.Inf(1)), 0); w != 200 || ht != 32 {
			t.Errorf("measured %v×%v", w, ht)
		}
		if w, _ := v.(native.Measurer).Measure(100, 0); w != 140 {
			t.Errorf("narrow: %v, want Apple's least, 140", w)
		}
		SendBool(b.button, "sendAction:to:", uintptr(Send(b.button, "action")), uintptr(Send(b.button, "target")))
		if pressed != 1 {
			t.Errorf("a click pressed it %d times", pressed)
		}
		v.(native.Destroyer).Destroy()
		if authState.buttons[b.box] != nil {
			t.Error("the destroyed button is still known")
		}
	})
}

func TestMapView(t *testing.T) {
	_, _ = mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		var events []string
		v := builtinViews["asuka.map"](native.NewContext(0, func(name string, _ map[string]any) { events = append(events, name) }))
		if v == nil {
			fatal(t, "no map view")
		}
		m := v.(*mapView)
		v.Set("region", `{"center":{"lat":48.8566,"lng":2.3522},"latDelta":0.1,"lngDelta":0.1}`)
		r := msgMapRegion(m.view, Sel("region"))
		if math.Abs(r.Lat-48.8566) > 0.01 || math.Abs(r.Lng-2.3522) > 0.01 {
			t.Errorf("region: %+v", r)
		}
		v.Set("markers", `[{"id":"a","at":{"lat":48.85,"lng":2.35},"title":"A","color":"#ff0000ff"},{"id":"b","at":{"lat":48.86,"lng":2.34}}]`)
		if n := SendInt(Send(m.view, "annotations"), "count"); n != 2 || len(m.markers) != 2 {
			t.Errorf("%d annotations, %d markers; want 2", n, len(m.markers))
		}
		for a, mk := range m.markers {
			c := msgCoordinate(a, Sel("coordinate"))
			if math.Abs(c.Lat-mk.At.Lat) > 1e-9 || math.Abs(c.Lon-mk.At.Lng) > 1e-9 {
				t.Errorf("marker %s at %+v", mk.ID, c)
			}
			av := m.viewFor(a)
			if av == 0 || !SendBool(av, "isKindOfClass:", uintptr(Class("MKMarkerAnnotationView"))) {
				t.Errorf("marker %s's view: %v", mk.ID, av)
			}
		}
		v.Set("markers", `[]`)
		if n := SendInt(Send(m.view, "annotations"), "count"); n != 0 {
			t.Errorf("%d annotations after clearing", n)
		}
		for ty := int64(0); ty <= 3; ty++ {
			v.Set("type", ty)
		}
		v.Set("interactive", false)
		if SendBool(m.view, "isScrollEnabled") || SendBool(m.view, "isZoomEnabled") {
			t.Error("a map that isn't interactive scrolls or zooms")
		}
		// Our own move isn't the user's.
		m.regionChanged()
		if len(events) != 0 {
			t.Errorf("events without the user: %v", events)
		}
		if c := markerColor("#00ff0080"); c == 0 {
			t.Error("no color for #00ff0080")
		}
		if markerColor("red") != 0 {
			t.Error("a color for a name")
		}
		v.(native.Destroyer).Destroy()
		if mapViews[m.view] != nil {
			t.Error("the destroyed map is still known")
		}
	})
}

// A ColorPicker's title and opacity go to the shared color panel as its
// well opens it.
func TestColorWellTitle(t *testing.T) {
	title := ui.Val("Tint")
	h, _ := mount(t, func() ui.Node {
		return ui.Row(ui.Style{},
			ui.ColorPicker(ui.ColorPickerProps{Value: signal.New(colors.Hex(0x3366cc)), Alpha: true, Title: title}),
			ui.ColorPicker(ui.ColorPickerProps{Value: signal.New(colors.Hex(0x000000))}))
	})
	onMain(func() {
		var wells []ID
		for v := range colorWells {
			if h.byView[v] != nil {
				wells = append(wells, v)
			}
		}
		if len(wells) != 2 {
			fatalf(t, "%d wells, want 2", len(wells))
		}
		panel := Send(Class("NSColorPanel"), "sharedColorPanel")
		def := colorPanelTitle()
		for _, w := range wells {
			p := colorWells[w]
			Send(w, "activate:", 1)
			if got := GoString(Send(panel, "title")); p.title == "Tint" && got != "Tint" || p.title == "" && got != def {
				t.Errorf("the panel's title: %q (the well's %q, the panel's own %q)", got, p.title, def)
			}
			if got := SendBool(panel, "showsAlpha"); got != p.alpha {
				t.Errorf("the panel shows alpha %v, the well asks %v", got, p.alpha)
			}
			Send(w, "deactivate")
		}
		Send(panel, "orderOut:", 0)
	})
}
