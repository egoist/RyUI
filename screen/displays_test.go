package screen_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/internal/follow"
	"github.com/egoist/asuka/screen"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

var (
	builtIn = screen.Display{ID: "1", Name: "Built-in Retina Display", Bounds: screen.Rect{Width: 1512, Height: 982},
		WorkArea: screen.Rect{Y: 33, Width: 1512, Height: 949}, Scale: 2, RefreshRate: 120, Primary: true}
	external = screen.Display{ID: "2", Name: "DELL U2720Q", Bounds: screen.Rect{X: 1512, Y: -200, Width: 2560, Height: 1440},
		WorkArea: screen.Rect{X: 1512, Y: -200, Width: 2560, Height: 1440}, Scale: 1, RefreshRate: 60}
)

func encode(ds ...screen.Display) []byte {
	b, _ := json.Marshal(ds)
	return b
}

// mount is a fake host whose "displays" module is m, and a pump that runs
// the main thread until cond holds.
func mount(t *testing.T, m *follow.Fake) (*fake.Host, func(what string, cond func() bool)) {
	h := fake.New()
	h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){"displays": m.Answer}
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	return h, func(what string, cond func() bool) {
		t.Helper()
		for range 5000 { // a safety net, not what passes
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

// Displays reads the host's list; WatchDisplays gets it now, then each
// change, until stop, which stops the host's watching.
func TestDisplays(t *testing.T) {
	m := follow.NewFake(encode(builtIn))
	_, wait := mount(t, m)

	var got []screen.Display
	var err error
	done := make(chan struct{})
	go func() { got, err = screen.Displays(); close(done) }()
	wait("Displays", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if err != nil || !slices.Equal(got, []screen.Display{builtIn}) {
		t.Fatalf("Displays: %+v, %v", got, err)
	}

	var seen [][]screen.Display
	var stop func()
	ui.Post(func() { stop = screen.WatchDisplays(func(ds []screen.Display) { seen = append(seen, ds) }) })
	wait("the displays now", func() bool { return len(seen) == 1 && m.Waiting() == 1 })
	if !slices.Equal(seen[0], []screen.Display{builtIn}) {
		t.Fatalf("first: %+v", seen[0])
	}
	m.Set(encode(builtIn, external))
	wait("a display plugged in", func() bool { return len(seen) == 2 })
	if !slices.Equal(seen[1], []screen.Display{builtIn, external}) {
		t.Fatalf("plugged in: %+v", seen[1])
	}
	ui.Post(stop)
	wait("stop", func() bool { return slices.Contains(m.Calls(), "stop") && m.Waiting() == 0 })
	m.Set(encode(external))
	for range 20 {
		wait("nothing", func() bool { return true })
	}
	if len(seen) != 2 {
		t.Fatalf("watched after stop: %+v", seen)
	}
}

// UseDisplays follows the displays while its component lives.
func TestUseDisplays(t *testing.T) {
	m := follow.NewFake(encode(builtIn))
	h := fake.New()
	h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){"displays": m.Answer}
	show := signal.New(true)
	var ds signal.Accessor[[]screen.Display]
	r := ui.Mount(h, func() ui.Node {
		return ui.Show(show, func() ui.Node {
			ds = screen.UseDisplays()
			return ui.Text(signal.Func(func() string { return string(rune('0' + len(ds.Get()))) }))
		})
	})
	t.Cleanup(r.Dispose)
	pump := func(cond func() bool) bool {
		for range 5000 {
			h.RunPosted()
			r.Flush()
			if cond() {
				return true
			}
			time.Sleep(time.Millisecond)
		}
		return false
	}
	if !pump(func() bool { return len(ds.Get()) == 1 }) {
		t.Fatal("no displays")
	}
	m.Set(encode(builtIn, external))
	if !pump(func() bool { return len(ds.Get()) == 2 }) {
		t.Fatal("the change didn't come")
	}
	show.Set(false)
	r.Flush()
	if !pump(func() bool { return slices.Contains(m.Calls(), "stop") }) {
		t.Fatalf("disposed, still watching: %v", m.Calls())
	}
}

func TestPrimaryNearestMatching(t *testing.T) {
	ds := []screen.Display{external, builtIn}
	if p, ok := screen.Primary(ds); !ok || p.ID != "1" {
		t.Errorf("primary %v", p.ID)
	}
	if _, ok := screen.Primary(nil); ok {
		t.Error("primary of none")
	}
	for _, c := range []struct {
		x, y float32
		id   string
	}{{10, 10, "1"}, {1600, 0, "2"}, {-50, 500, "1"}, {5000, -300, "2"}, {1000, 1100, "1"}, {1500, 1100, "2"}} {
		if d, _ := screen.Nearest(ds, c.x, c.y); d.ID != c.id {
			t.Errorf("nearest %v,%v: %v, want %v", c.x, c.y, d.ID, c.id)
		}
	}
	// A window mostly on the external display.
	if d, _ := screen.Matching(ds, screen.Rect{X: 1400, Y: 100, Width: 800, Height: 600}); d.ID != "2" {
		t.Errorf("matching: %v", d.ID)
	}
	// One off every display: the nearest its center.
	if d, _ := screen.Matching(ds, screen.Rect{X: -900, Y: 100, Width: 400, Height: 300}); d.ID != "1" {
		t.Errorf("matching off screen: %v", d.ID)
	}
}
