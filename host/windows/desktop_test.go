//go:build windows

package windows

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ui"
)

// Monitors of different DPIs are placed as asuka/sdk/screen has them: each
// one's origin in pixels over the least scale, its size in its own.
func TestDisplaysInPoints(t *testing.T) {
	r := func(l, tp, w, h int32) Rect { return Rect{Left: l, Top: tp, Right: l + w, Bottom: tp + h} }
	ds := displaysInPoints([]Monitor{
		{Device: `\\.\DISPLAY1`, Bounds: r(0, 0, 1920, 1080), Work: r(0, 0, 1920, 1032), DPI: 96, Primary: true, Hz: 60},
		{Device: `\\.\DISPLAY2`, Bounds: r(1920, -200, 3840, 2160), Work: r(1920, -200, 3840, 2064), DPI: 192, Hz: 59.94},
	})
	want := []display{
		{ID: `\\.\DISPLAY1`, Bounds: pointRect{0, 0, 1920, 1080}, WorkArea: pointRect{0, 0, 1920, 1032}, Scale: 1, RefreshRate: 60, Primary: true},
		{ID: `\\.\DISPLAY2`, Bounds: pointRect{1920, -200, 1920, 1080}, WorkArea: pointRect{1920, -200, 1920, 1032}, Scale: 2, RefreshRate: 59.94},
	}
	if len(ds) != 2 || ds[0] != want[0] || ds[1] != want[1] {
		t.Fatalf("mixed: %+v", ds)
	}
	// The primary at 200%, another at 100% after it: a gap between them.
	ds = displaysInPoints([]Monitor{
		{Bounds: r(0, 0, 3840, 2160), Work: r(0, 0, 3840, 2064), DPI: 192, Primary: true},
		{Bounds: r(3840, 0, 1920, 1080), Work: r(3840, 0, 1920, 1080), DPI: 96},
	})
	if ds[0].Bounds != (pointRect{0, 0, 1920, 1080}) || ds[1].Bounds != (pointRect{3840, 0, 1920, 1080}) {
		t.Fatalf("a gap: %+v", ds)
	}
	// One scale: the pixels over it; the taskbar on the left.
	ds = displaysInPoints([]Monitor{{Bounds: r(0, 0, 2880, 1800), Work: r(72, 0, 2808, 1800), DPI: 144, Primary: true}})
	if ds[0].Bounds != (pointRect{0, 0, 1920, 1200}) || ds[0].WorkArea != (pointRect{48, 0, 1872, 1200}) || ds[0].Scale != 1.5 {
		t.Fatalf("150%%: %+v", ds)
	}
}

func TestPowerState(t *testing.T) {
	for _, c := range []struct {
		s    PowerStatus
		want []byte
	}{
		{PowerStatus{ACLine: 1, BatteryFlag: 128, BatteryPercent: 255}, []byte{0, 0, 255, 0, 0, 0}},
		{PowerStatus{ACLine: 0, BatteryFlag: 2, BatteryPercent: 42, SystemStatus: 1}, []byte{1, 1, 42, 0, 1, 0}},
		{PowerStatus{ACLine: 1, BatteryFlag: 8 | 1, BatteryPercent: 80}, []byte{1, 0, 80, 1, 0, 0}},
		{PowerStatus{ACLine: 255, BatteryFlag: 255, BatteryPercent: 255}, []byte{0, 0, 255, 0, 0, 0}},
	} {
		if got := powerState(c.s); !bytes.Equal(got, c.want) {
			t.Errorf("%+v: %v, want %v", c.s, got, c.want)
		}
	}
}

// The desktop modules answer as asuka/sdk/screen, asuka/sdk/power,
// asuka/sdk/appearance and asuka/sdk/dock read them, and the system window's
// messages answer a waiting "next".
func TestDesktopModules(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		ok := func(module, m string, args []byte) []byte {
			b, err, answered := await(h, module, m, args)
			if !answered || err != nil {
				fatalf("%s.%s: %v (answered %v)", module, m, err, answered)
			}
			return b
		}
		// next calls "next" with args, and returns its answer, which
		// answered says has come.
		next := func(module string, args []byte) (answer *[]byte, err *error, answered *bool) {
			var b []byte
			var e error
			done := false
			h.asyncModule(module)["next"](args, func(r []byte, er error) { b, e, done = r, er, true })
			return &b, &e, &done
		}
		send := func(m uint32, wp, lp uintptr) { SendMessage(systemWin.hwnd, m, wp, lp) }
		// drain runs the messages waiting.
		drain := func() {
			var msg Msg
			for PeekMessage(&msg) {
				TranslateMessage(&msg)
				DispatchMessage(&msg)
			}
		}

		// Displays: the primary first, in points.
		var ds []display
		cur := ok("displays", "current", nil)
		if err := json.Unmarshal(cur, &ds); err != nil || len(ds) == 0 || !ds[0].Primary || ds[0].Scale <= 0 || ds[0].Bounds.Width <= 0 {
			fatalf("displays.current: %s, %v", cur, err)
		}
		if systemWin.hwnd == 0 || IsWindowVisible(systemWin.hwnd) {
			fatalf("the system window: %#x", systemWin.hwnd)
		}
		// Unchanged, a "next" waits; a display change answers it.
		b, _, answered := next("displays", cur)
		drain()
		if *answered {
			fatalf("displays.next answered unchanged: %s", *b)
		}
		read := displays.read
		changed := []byte(`[{"id":"x"}]`)
		displays.read = func() []byte { return changed }
		send(WM_DISPLAYCHANGE, 32, 0)
		displays.read = read
		if !*answered || !bytes.Equal(*b, changed) {
			fatalf("displays.next after WM_DISPLAYCHANGE: %v %s", *answered, *b)
		}
		_, errp, answered := next("displays", ok("displays", "current", nil))
		ok("displays", "stop", nil)
		if !*answered || *errp == nil || (*errp).Error() != "stopped" {
			fatalf("displays.stop: %v", *errp)
		}

		// Power: its state, the idle time, the session's lock and sleep.
		state := ok("power", "current", nil)
		if len(state) != 6 || state[2] != 255 && state[2] > 100 || state[5] != 0 {
			fatalf("power.current: %v", state)
		}
		if b := ok("power", "idle", nil); len(b) != 8 || binary.LittleEndian.Uint64(b) > 1<<32 {
			fatalf("power.idle: %v", b)
		}
		b, _, answered = next("power", state)
		drain()
		if *answered {
			fatalf("power.next answered unchanged: %v", *b)
		}
		send(WM_WTSSESSION_CHANGE, WTS_SESSION_LOCK, 0)
		if !*answered || !bytes.Equal(*b, append(append([]byte(nil), state...), powerLock)) {
			fatalf("power.next after a lock: %v", *b)
		}
		// Between two "next"s events wait; sleeping is told once.
		send(WM_POWERBROADCAST, PBT_APMSUSPEND, 0)
		send(WM_POWERBROADCAST, PBT_APMSUSPEND, 0)
		b, _, answered = next("power", state)
		if !*answered || !bytes.Equal((*b)[6:], []byte{powerSuspend}) {
			fatalf("power.next after sleeping: %v", *b)
		}
		b, _, answered = next("power", (*b)[:6])
		send(WM_POWERBROADCAST, PBT_APMRESUMEAUTOMATIC, 0)
		send(WM_POWERBROADCAST, PBT_APMRESUMESUSPEND, 0)
		if !*answered || !bytes.Equal((*b)[6:], []byte{powerResume}) {
			fatalf("power.next after waking: %v", *b)
		}
		b, _, answered = next("power", (*b)[:6])
		drain()
		if *answered {
			fatalf("waking told twice: %v", *b)
		}
		ok("power", "stop", nil)
		// Stopped: nothing is queued.
		send(WM_WTSSESSION_CHANGE, WTS_SESSION_UNLOCK, 0)
		if b, _, answered := next("power", state); *answered {
			fatalf("an event kept after stop: %v", *b)
		}
		ok("power", "stop", nil)

		// Appearance: the settings, and a change of them.
		set := ok("appearance", "current", nil)
		if len(set) != 6 || set[0] > 1 || set[4] > 1 || set[5] > 1 {
			fatalf("appearance.current: %v", set)
		}
		b, _, answered = next("appearance", set)
		read = appearance.read
		purple := []byte{1, 0x88, 0x17, 0x98, 1, 0}
		appearance.read = func() []byte { return purple }
		send(WM_SETTINGCHANGE, 0, Str("ImmersiveColorSet"))
		appearance.read = read
		if !*answered || !bytes.Equal(*b, purple) {
			fatalf("appearance.next after a change: %v", *b)
		}
		ok("appearance", "stop", nil)

		// The dock: a progress, taken off; attention asked and cancelled.
		progress := func(state byte, v float64) []byte {
			return binary.LittleEndian.AppendUint64([]byte{state}, math.Float64bits(v))
		}
		ok("dock", "progress", progress(1, 0.5))
		if !dockState.shown || dockState.state != TBPF_NORMAL {
			fatalf("dock.progress: %+v", dockState)
		}
		ok("dock", "progress", progress(4, 0.5))
		if dockState.state != TBPF_ERROR {
			fatalf("dock.progress error: %+v", dockState)
		}
		h.dockMessage(dockState.buttonMsg) // a new button
		ok("dock", "progress", progress(0, 0))
		if dockState.shown {
			fatalf("dock.progress none: %+v", dockState)
		}
		id := ok("dock", "attention", []byte{1})
		if len(id) != 4 {
			fatalf("dock.attention: %v", id)
		}
		ok("dock", "cancelAttention", id)
	})
}
