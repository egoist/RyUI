package liveactivity_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/sdk/liveactivity"
	"github.com/egoist/asuka/ui"
)

// Content goes as the hosts and a server's push take it: times in ms, the
// tint as #rrggbbaa, what's empty left out; and comes back the same.
func TestContentJSON(t *testing.T) {
	c := liveactivity.Content{
		Title: "Order #1042", Subtitle: "On its way", Value: "12 min", Symbol: "bicycle",
		Progress: 0.25, ShowProgress: true, Since: time.UnixMilli(1_000), Until: time.UnixMilli(5_000),
		Tint: colors.Hex(0x336699), URL: "shop://orders/1042",
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"title":"Order #1042","subtitle":"On its way","value":"12 min","symbol":"bicycle","progress":0.25,"showProgress":true,"sinceMs":1000,"untilMs":5000,"tint":"#336699ff","url":"shop://orders/1042"}`
	if string(b) != want {
		t.Fatalf("got  %s\nwant %s", b, want)
	}
	var back liveactivity.Content
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.Title != c.Title || back.Value != c.Value || back.Symbol != c.Symbol || back.Tint != c.Tint ||
		!back.Since.Equal(c.Since) || !back.Until.Equal(c.Until) || back.Progress != c.Progress || !back.ShowProgress || back.URL != c.URL {
		t.Fatalf("back: %+v", back)
	}
	if b, _ := json.Marshal(liveactivity.Content{}); string(b) != `{}` {
		t.Errorf("empty: %s", b)
	}
}

// host is a fake host's liveActivity module: it keeps what's started,
// updated and ended, and answers "next" with the events given it.
type host struct {
	h       *fake.Host
	r       *ui.Root
	calls   []string
	args    map[string]map[string]any
	events  [][]byte
	waiting []func([]byte, error)
	off     bool // a host without live activities
}

func newHost(t *testing.T) *host {
	t.Helper()
	m := &host{h: fake.New(), args: map[string]map[string]any{}}
	m.h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){}
	m.h.AsyncModules["liveActivity"] = func(method string, args []byte, reply func([]byte, error)) {
		m.calls = append(m.calls, method)
		var a map[string]any
		json.Unmarshal(args, &a)
		m.args[method] = a
		if m.off && method != "stop" {
			reply(nil, errors.New("not available: live activities are iOS's and Android's"))
			return
		}
		switch method {
		case "enabled":
			reply([]byte{1}, nil)
		case "running":
			reply([]byte(`[{"id":"a1","content":{"title":"Ride","value":"3 min"}}]`), nil)
		case "next":
			if len(m.events) > 0 {
				e := m.events[0]
				m.events = m.events[1:]
				reply(e, nil)
				return
			}
			m.waiting = append(m.waiting, func(b []byte, err error) { reply(b, err) })
		case "stop":
			for _, w := range m.waiting {
				w(nil, errors.New("stopped"))
			}
			m.waiting = nil
			reply(nil, nil)
		default:
			reply(nil, nil)
		}
	}
	m.r = ui.Mount(m.h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(m.r.Dispose)
	return m
}

// send gives the waiting "next" an event (or keeps it). Main thread.
func (m *host) send(e string) {
	if len(m.waiting) > 0 {
		w := m.waiting[0]
		m.waiting = m.waiting[1:]
		w([]byte(e), nil)
		return
	}
	m.events = append(m.events, []byte(e))
}

// wait runs the main thread until cond holds.
func (m *host) wait(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 5000 { // a safety net, not what passes
		m.h.RunPosted()
		m.r.Flush()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("waiting for %s; calls %v", what, m.calls)
}

// Start, Update and End go to the host with the activity's ID; Running
// and Enabled read it.
func TestStartUpdateEnd(t *testing.T) {
	m := newHost(t)
	var a *liveactivity.Activity
	var err error
	done := make(chan bool)
	go func() {
		if !liveactivity.Enabled() {
			err = errors.New("not enabled")
		} else {
			a, err = liveactivity.Start(liveactivity.Content{Title: "Order", Value: "12 min"}, liveactivity.Options{Push: true, Relevance: 50})
		}
		close(done)
	}()
	m.wait(t, "start", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if err != nil || a == nil || len(a.ID) != 16 {
		t.Fatalf("Start: %v, %v", a, err)
	}
	s := m.args["start"]
	if s["id"] != a.ID || s["push"] != true || s["relevance"] != 50.0 || s["content"].(map[string]any)["value"] != "12 min" {
		t.Fatalf("start: %v", s)
	}
	a.Update(liveactivity.Content{Title: "Order", Value: "4 min"}, liveactivity.Alert{Title: "Almost there", Body: "4 minutes away"})
	m.wait(t, "update", func() bool { return m.args["update"] != nil })
	u := m.args["update"]
	if u["id"] != a.ID || u["content"].(map[string]any)["value"] != "4 min" || u["alert"].(map[string]any)["title"] != "Almost there" {
		t.Fatalf("update: %v", u)
	}
	at := time.UnixMilli(9_000_000)
	a.End(liveactivity.Content{Title: "Order", Subtitle: "Delivered"}, liveactivity.Dismiss{At: at})
	m.wait(t, "end", func() bool { return m.args["end"] != nil })
	if e := m.args["end"]; e["id"] != a.ID || e["dismiss"] != "at" || e["atMs"] != 9_000_000.0 || e["content"].(map[string]any)["subtitle"] != "Delivered" {
		t.Fatalf("end: %v", e)
	}
	a.End(liveactivity.Content{}, liveactivity.Dismiss{Now: true})
	m.wait(t, "end now", func() bool { return m.args["end"]["dismiss"] == "now" })

	var as []*liveactivity.Activity
	var cs []liveactivity.Content
	done = make(chan bool)
	go func() { as, cs, err = liveactivity.Running(); close(done) }()
	m.wait(t, "running", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if err != nil || len(as) != 1 || as[0].ID != "a1" || cs[0].Value != "3 min" {
		t.Fatalf("Running: %v %v %v", as, cs, err)
	}
}

// Without live activities (a desktop, the web), Start fails with
// ErrNotAvailable, Enabled is false and Running has none.
func TestNotAvailable(t *testing.T) {
	m := newHost(t)
	m.off = true
	var err error
	var enabled bool
	done := make(chan bool)
	go func() {
		enabled = liveactivity.Enabled()
		_, err = liveactivity.Start(liveactivity.Content{Title: "x"}, liveactivity.Options{})
		close(done)
	}()
	m.wait(t, "start", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if enabled || !errors.Is(err, liveactivity.ErrNotAvailable) {
		t.Fatalf("enabled %v, Start: %v", enabled, err)
	}
}

// Observe gets the host's events, those kept from before it too, on the
// main thread; the poll stops with the last observer, and starts again
// with the next.
func TestObserve(t *testing.T) {
	m := newHost(t)
	m.send(`{"type":"pushToken","id":"a1","token":"abcd"}`) // before anyone observes
	var got []liveactivity.Event
	stop := liveactivity.Observe(func(e liveactivity.Event) { got = append(got, e) })
	m.wait(t, "the kept event", func() bool { return len(got) == 1 })
	if e := got[0]; e.Type != liveactivity.PushToken || e.Activity.ID != "a1" || e.Token != "abcd" {
		t.Fatalf("event %+v", e)
	}
	m.wait(t, "the poll", func() bool { return len(m.waiting) == 1 })
	m.send(`{"type":"ended","id":"a1"}`)
	m.send(`{"type":"pushToStartToken","token":"ffff"}`)
	m.send(`{"type":"started","id":"s1"}`)
	m.send(`{"type":"unknown"}`)
	m.wait(t, "three more", func() bool { return len(got) == 4 && len(m.waiting) == 1 })
	if got[1].Type != liveactivity.Ended || got[1].Activity.ID != "a1" ||
		got[2].Type != liveactivity.PushToStartToken || got[2].Activity != nil || got[2].Token != "ffff" ||
		got[3].Type != liveactivity.Started || got[3].Activity.ID != "s1" {
		t.Fatalf("events %+v", got)
	}
	stop()
	m.wait(t, "the poll to stop", func() bool { return len(m.waiting) == 0 && m.calls[len(m.calls)-1] == "stop" })
	// Observed again: the poll starts again.
	stop = liveactivity.Observe(func(e liveactivity.Event) { got = append(got, e) })
	m.wait(t, "a new poll", func() bool { return len(m.waiting) == 1 })
	m.send(`{"type":"ended","id":"a2"}`)
	m.wait(t, "the new event", func() bool { return len(got) == 5 })
	if got[4].Activity.ID != "a2" {
		t.Fatalf("event %+v", got[4])
	}
	// Stopped, and the poll's last answer given before the root goes.
	stop()
	m.wait(t, "the poll to stop again", func() bool { return len(m.waiting) == 0 && m.calls[len(m.calls)-1] == "stop" })
	m.wait(t, "the answer", func() bool { return true })
}
