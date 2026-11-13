//go:build linux && !android

package linux

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"
)

// The displays: the monitors GDK has (Xvfb's one), the primary first;
// "next" waits while they're the ones Go has, and "stop" ends it.
func TestDisplaysModule(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	b, err := callModule(t, h, "displays", "current", nil)
	var ds []display
	if err != nil || json.Unmarshal(b, &ds) != nil || len(ds) == 0 {
		t.Fatalf("current: %s, %v", b, err)
	}
	t.Logf("displays %s", b)
	if !ds[0].Primary || ds[0].Scale <= 0 || ds[0].Bounds.Width <= 0 || ds[0].WorkArea.Height <= 0 {
		t.Errorf("primary %+v", ds[0])
	}
	for _, d := range ds[1:] {
		if d.Primary {
			t.Errorf("two primary: %+v", ds)
		}
	}
	if now, err := callModule(t, h, "displays", "next", nil); err != nil || string(now) != string(b) {
		t.Errorf("next without a state: %s, %v", now, err)
	}
	next := callAsync(h, "displays", "next", b)
	onMain(func() { h.readDisplays() }) // nothing changed
	onMain(func() {
		if next.done {
			fatalf(t, "next answered without a change: %s %v", next.b, next.err)
		}
	})
	callModule(t, h, "displays", "stop", nil)
	if _, err := awaitResult(t, "next", next); !errIs(err, "stopped") {
		t.Errorf("stop: %v", err)
	}
}

func TestMutterPrimary(t *testing.T) {
	mon := func(c string) any { return []any{c, "DEL", "U2720Q", "123"} }
	logical := func(x int32, primary bool, conns ...string) any {
		var ms []any
		for _, c := range conns {
			ms = append(ms, mon(c))
		}
		return []any{x, int32(0), 1.0, uint32(0), primary, ms, map[string]any{}}
	}
	state := []any{uint32(1), []any{}, []any{logical(0, false, "eDP-1"), logical(1512, true, "DP-1")}, map[string]any{}}
	if p := mutterPrimary(state); p != "DP-1" {
		t.Errorf("primary %q", p)
	}
	if p := mutterPrimary([]any{uint32(1)}); p != "" {
		t.Errorf("none: %q", p)
	}
}

// The power: its state (no battery under Xvfb, unless the machine has
// one), events queued while Go watches, the idle time or none.
func TestPowerModule(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	s, err := callModule(t, h, "power", "current", nil)
	if err != nil || len(s) != 6 || s[0] == 0 && s[2] != 255 || s[5] != 0 {
		t.Fatalf("current %v, %v", s, err)
	}
	t.Logf("power %v", s)
	next := callAsync(h, "power", "next", s)
	onMain(func() {
		if next.done {
			fatalf(t, "next answered without a change: %v %v", next.b, next.err)
		}
		powerw.w.emit(1) // PrepareForSleep(true)
	})
	b, err := awaitResult(t, "suspend", next)
	if err != nil || string(b) != string(s)+"\x01" {
		t.Errorf("suspend: %v, %v", b, err)
	}
	// Two while Go isn't waiting: both come with the next "next".
	onMain(func() {
		h.setLocked(true)
		h.setLocked(true)
		h.setLocked(false)
	})
	b, err = callModule(t, h, "power", "next", s)
	if err != nil || string(b[6:]) != "\x03\x04" && string(b[6:]) != "\x04" {
		t.Errorf("lock and unlock: %v, %v", b, err)
	}
	callModule(t, h, "power", "stop", nil)
	onMain(func() { powerw.w.emit(2) }) // not watched: dropped
	next = callAsync(h, "power", "next", s)
	onMain(func() {
		if next.done {
			fatalf(t, "an event kept after stop: %v", next.b)
		}
	})
	callModule(t, h, "power", "stop", nil)
	if _, err := awaitResult(t, "next", next); !errIs(err, "stopped") {
		t.Errorf("stop: %v", err)
	}
	if b, err := callModule(t, h, "power", "idle", nil); err != nil || len(b) != 0 && len(b) != 8 {
		t.Errorf("idle %v, %v", b, err)
	}
}

func TestAppearanceModule(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	b, err := callModule(t, h, "appearance", "current", nil)
	if err != nil || len(b) != 6 || b[5] != 0 {
		t.Fatalf("current %v, %v", b, err)
	}
	var a appearanceWatch
	a.setting("accent-color", []any{0.2078, 0.5176, 0.8941})
	if !a.hasAccent || a.accent != [3]byte{53, 132, 228} {
		t.Errorf("accent %v %v", a.hasAccent, a.accent)
	}
	a.setting("accent-color", []any{-1.0, -1.0, -1.0})
	if a.hasAccent {
		t.Error("unset accent kept")
	}
	a.setting("contrast", uint32(1))
	if !a.contrast || !a.contrastKnown {
		t.Error("high contrast")
	}
	a.setting("contrast", nil)
	if a.contrastKnown {
		t.Error("no contrast setting, known")
	}
	// A setting changing answers a waiting next.
	next := callAsync(h, "appearance", "next", b)
	onMain(func() {
		appw.setting("accent-color", []any{0.0, 0.0, 1.0})
		h.appearanceChanged()
	})
	got, err := awaitResult(t, "accent", next)
	if err != nil || len(got) != 6 || got[0] != 1 || got[3] != 255 {
		t.Errorf("changed: %v, %v", got, err)
	}
	onMain(func() {
		appw.setting("accent-color", nil)
		h.appearanceChanged()
	})
	callModule(t, h, "appearance", "stop", nil)
}

func TestDockModule(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	p := append([]byte{1}, binary.LittleEndian.AppendUint64(nil, math.Float64bits(0.4))...)
	if _, err := callModule(t, h, "dock", "progress", p); err != nil {
		t.Fatal("progress:", err)
	}
	onMain(func() {
		if !launch.showing || launch.progress != 0.4 {
			fatalf(t, "launcher %+v", launch)
		}
	})
	b, err := callModule(t, h, "dock", "attention", []byte{1})
	if err != nil || len(b) != 4 {
		t.Fatalf("attention: %v, %v", b, err)
	}
	if id := binary.LittleEndian.Uint32(b); id != 0 {
		onMain(func() {
			if !launch.urgent {
				fatalf(t, "not urgent")
			}
		})
		callModule(t, h, "dock", "cancelAttention", b)
		onMain(func() {
			if launch.urgent || attn.id != 0 {
				fatalf(t, "still urgent")
			}
		})
	}
	callModule(t, h, "dock", "progress", make([]byte, 9))
	onMain(func() {
		if launch.showing {
			fatalf(t, "progress kept")
		}
	})
}
