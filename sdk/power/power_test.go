package power

import (
	"encoding/binary"
	"slices"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/internal/follow"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func TestWire(t *testing.T) {
	for _, s := range []State{{Level: -1}, {Battery: true, OnBattery: true, Level: 0.42, LowPower: true, Thermal: Serious}, {Battery: true, Level: 1, Charging: true, Thermal: Critical}} {
		got, err := decodeState(encodeState(s))
		if err != nil || got != s {
			t.Errorf("%+v: %+v %v", s, got, err)
		}
	}
	if _, err := decodeState([]byte{1, 2}); err == nil {
		t.Error("short: no error")
	}
	if Serious.String() != "serious" || Lock.String() != "lock" || Event(0).String() != "unknown" {
		t.Error(Serious, Lock, Event(0))
	}
}

func mount(t *testing.T, m *follow.Fake) func(what string, cond func() bool) {
	h := fake.New()
	h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){"power": m.Answer}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	return func(what string, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); { // a safety net, not what passes
			h.RunPosted()
			r.Flush()
			if cond() {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("waiting for %s; calls %v", what, m.Calls())
	}
}

func goWait(wait func(string, func() bool), what string, f func()) {
	done := make(chan struct{})
	go func() { f(); close(done) }()
	wait(what, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
}

// Watch sees the state and its changes, not the events; OnSuspend and the
// others see their event only; the host stops watching with the last.
func TestWatchAndEvents(t *testing.T) {
	plugged := State{Battery: true, Level: 0.8, Charging: true}
	m := follow.NewFake(encodeState(plugged))
	wait := mount(t, m)

	var cur State
	var err error
	goWait(wait, "Current", func() { cur, err = Current() })
	if err != nil || cur != plugged {
		t.Fatalf("Current: %+v, %v", cur, err)
	}

	var states []State
	var events []Event
	suspends := 0
	var stops []func()
	ui.Post(func() {
		stops = append(stops,
			Watch(func(s State) { states = append(states, s) }),
			OnEvent(func(e Event) { events = append(events, e) }),
			OnSuspend(func() { suspends++ }))
	})
	wait("the state now", func() bool { return len(states) == 1 && m.Waiting() > 0 })
	m.Emit(byte(Lock))
	wait("lock", func() bool { return len(events) == 1 })
	m.Emit(byte(Suspend), byte(Resume))
	wait("suspend and resume", func() bool { return len(events) == 3 })
	unplugged := State{Battery: true, OnBattery: true, Level: 0.79}
	m.Set(encodeState(unplugged))
	wait("unplugged", func() bool { return len(states) == 2 })
	if states[0] != plugged || states[1] != unplugged {
		t.Errorf("states %+v", states)
	}
	if !slices.Equal(events, []Event{Lock, Suspend, Resume}) || suspends != 1 {
		t.Errorf("events %v, %d suspends", events, suspends)
	}
	ui.Post(func() {
		for _, s := range stops {
			s()
		}
	})
	wait("stop", func() bool { return slices.Contains(m.Calls(), "stop") && m.Waiting() == 0 })
}

// Use follows the power while its component lives.
func TestUse(t *testing.T) {
	m := follow.NewFake(encodeState(State{Level: -1}))
	h := fake.New()
	h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){"power": m.Answer}
	show := signal.New(true)
	var p signal.Accessor[State]
	r := ui.Mount(h, func() ui.Node {
		return ui.Show(show, func() ui.Node {
			p = Use()
			return ui.StaticText("x")
		})
	})
	t.Cleanup(r.Dispose)
	pump := func(cond func() bool) bool {
		for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); { // a safety net, not what passes
			h.RunPosted()
			r.Flush()
			if cond() {
				return true
			}
			time.Sleep(time.Millisecond)
		}
		return false
	}
	m.Set(encodeState(State{Level: -1, LowPower: true}))
	if !pump(func() bool { return p.Get().LowPower }) {
		t.Fatal("Low Power Mode didn't come")
	}
	show.Set(false)
	r.Flush()
	if !pump(func() bool { return slices.Contains(m.Calls(), "stop") }) {
		t.Fatalf("disposed, still watching: %v", m.Calls())
	}
}

func TestIdleTime(t *testing.T) {
	idle := binary.LittleEndian.AppendUint64(nil, 90_500)
	m := follow.NewFake(nil)
	m.Other = func(method string, _ []byte) ([]byte, error) { return idle, nil }
	wait := mount(t, m)
	var d time.Duration
	var err error
	goWait(wait, "IdleTime", func() { d, err = IdleTime() })
	if err != nil || d != 90500*time.Millisecond {
		t.Fatalf("idle %v, %v", d, err)
	}
	idle = nil
	goWait(wait, "IdleTime", func() { d, err = IdleTime() })
	if err != ErrIdleUnknown {
		t.Fatalf("unknown: %v, %v", d, err)
	}
}
