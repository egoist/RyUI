package location

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/internal/locationspec"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/egoist/asuka/uitest"
)

// mount mounts root on a fake host with a Location, set up by setup
// before the app starts.
func mount(t *testing.T, root func() ui.Node, setup func(l *fake.Location)) (*uitest.App, *fake.Location) {
	t.Helper()
	reset()
	var loc *fake.Location
	a := uitest.Mount(t, root, uitest.Setup(func(h *fake.Host) {
		loc = h.Location()
		if setup != nil {
			setup(loc)
		}
	}))
	return a, loc
}

// do runs f off the main thread (the package's calls that wait), and
// waits for it, the app's posts running meanwhile.
func do(t *testing.T, a *uitest.App, f func()) {
	t.Helper()
	var mu sync.Mutex
	done := false
	go func() {
		f()
		mu.Lock()
		done = true
		mu.Unlock()
		ui.Post(func() {}) // wakes Until
	}()
	a.Until("the call", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return done
	})
}

func text(s string) func() ui.Node { return func() ui.Node { return ui.StaticText(s) } }

// Request asks once: WhenInUse, then Always on its own question; the
// authorization follows what the user allows, in a signal that goes
// with its component.
func TestAuthorization(t *testing.T) {
	var auth signal.Accessor[Authorization]
	a, loc := mount(t, func() ui.Node {
		auth = UseAuthorization()
		return ui.Text(signal.Func(func() string { return auth.Get().Level.String() }))
	}, func(l *fake.Location) { l.Answer, l.AnswerPrecise = fake.LocationAlways, false })
	a.WaitFor("not asked")
	var got, again, always, precise Authorization
	do(t, a, func() {
		got, _ = Request()
		again, _ = Request()
	})
	if got.Level != WhenInUse || got.Precise || again != got {
		t.Fatalf("request: %+v, again %+v", got, again)
	}
	a.WaitFor("when in use")
	do(t, a, func() { always, _ = RequestAlways() })
	loc.AnswerPrecise = true
	do(t, a, func() { precise, _ = RequestPrecise("") })
	if always.Level != Always || always.Precise || !precise.Precise {
		t.Fatalf("always %+v, precise %+v", always, precise)
	}
	if want := []string{"whenInUse", "always", "precise:default"}; !slices.Equal(loc.Asked, want) {
		t.Fatalf("asked %v, want %v", loc.Asked, want)
	}
	a.WaitFor("always")
	loc.Allow(fake.LocationDenied, false) // in Settings
	a.WaitFor("denied")
}

// A crossing and updates the system delivered before the app set its
// handlers (it started the app for them) wait for them, and are reported
// done once handled; a Defer holds an update's until it's done.
func TestEventsBeforeHandlers(t *testing.T) {
	a, loc := mount(t, text("x"), func(l *fake.Location) {
		l.Level, l.Precise = fake.LocationAlways, true
		l.Seconds = 25
		l.Cross("home", fake.RegionExit)
	})
	var crossings []Crossing
	var batches []Batch
	var finish func()
	ui.Post(func() {
		OnCrossing(func(c Crossing) { crossings = append(crossings, c) })
	})
	a.Until("the crossing", func() bool { return len(crossings) == 1 && loc.Pending() == 0 })
	if c := crossings[0]; c.Region != "home" || c.Kind != Exit || c.Time.IsZero() {
		t.Fatalf("crossing %+v", c)
	}
	do(t, a, func() {
		if err := StartUpdates(Updates{Distance: 100, Notification: Notification{Title: "Tracking"}}); err != nil {
			t.Error(err)
		}
	})
	if loc.Updates == nil || loc.Updates.Distance != 100 || loc.Updates.Title != "Tracking" {
		t.Fatalf("updates %+v", loc.Updates)
	}
	loc.Move(35.6812, 139.7671)
	loc.Move(35.6813, 139.7671) // 11 m: filtered out
	loc.Move(35.6900, 139.7671) // 1 km
	a.Settle()
	if loc.Pending() != 2 {
		t.Fatalf("pending %d, want the 2 updates held for a handler", loc.Pending())
	}
	ui.Post(func() {
		OnUpdates(func(b Batch) {
			batches = append(batches, b)
			if len(batches) == 2 {
				ctx, done := b.Defer()
				if d, ok := ctx.Deadline(); !ok || time.Until(d) > 25*time.Second {
					t.Errorf("deadline %v %v", d, ok)
				}
				finish = done
			}
		})
	})
	a.Until("the updates", func() bool { return len(batches) == 2 })
	if p := batches[1].Positions; len(p) != 1 || p[0].Latitude != 35.69 || p[0].Speed != -1 {
		t.Fatalf("batch %+v", p)
	}
	a.Until("the first done", func() bool { return loc.Pending() == 1 })
	finish()
	a.Until("the deferred done", func() bool { return loc.Pending() == 0 })
	var o Updates
	var on bool
	do(t, a, func() {
		o, on, _ = Updating()
		StopUpdates()
	})
	if !on || o.Distance != 100 || loc.Updates != nil {
		t.Fatalf("updating %+v %v, then %+v", o, on, loc.Updates)
	}
}

// Monitor needs Always, and stops at the system's limit; a region the
// device is in as monitoring starts sends Enter.
func TestMonitor(t *testing.T) {
	a, loc := mount(t, text("x"), func(l *fake.Location) {
		l.Level = fake.LocationWhenInUse
		l.Limit = 2
	})
	var crossings []Crossing
	ui.Post(func() { OnCrossing(func(c Crossing) { crossings = append(crossings, c) }) })
	home := Region{ID: "home", Latitude: 35.6812, Longitude: 139.7671, Radius: 150}
	var err error
	do(t, a, func() { err = Monitor(home) })
	if !errors.Is(err, ErrNeedsAlways) {
		t.Fatalf("monitor with WhenInUse: %v", err)
	}
	loc.Allow(fake.LocationAlways, true)
	loc.Move(35.6812, 139.7672)
	var s Support
	var regions []Region
	do(t, a, func() {
		s = Supported()
		err = Monitor(home)
		if err == nil {
			err = Monitor(Region{ID: "work", Latitude: 35.0, Longitude: 139.0, Radius: 200, Events: Exit})
		}
		if err == nil {
			err = Monitor(Region{ID: "gym", Latitude: 35.1, Longitude: 139.1, Radius: 200})
		}
		regions, _ = Monitored()
	})
	if !errors.Is(err, ErrTooManyRegions) {
		t.Fatalf("a third region: %v", err)
	}
	if !s.Regions || s.MaxRegions != 2 || !s.Relaunch {
		t.Fatalf("support %+v", s)
	}
	if len(regions) != 2 || regions[0].Events != Enter|Exit || regions[1].Events != Exit {
		t.Fatalf("monitored %+v", regions)
	}
	a.Until("the enter", func() bool { return len(crossings) == 1 })
	loc.Move(35.0, 139.0)       // out of home, into work (which reports only exits)
	loc.Move(35.6812, 139.7671) // back home, out of work
	a.Until("four crossings", func() bool { return len(crossings) == 4 })
	got := []string{}
	for _, c := range crossings {
		got = append(got, c.Region+" "+c.Kind.String())
	}
	if want := []string{"home enter", "home exit", "home enter", "work exit"}; !slices.Equal(got, want) {
		t.Fatalf("crossings %v, want %v", got, want)
	}
	do(t, a, func() { StopMonitoringAll() })
	if len(loc.Regions) != 0 {
		t.Fatalf("regions left %+v", loc.Regions)
	}
}

// Where the system does neither (Linux, the web), the regions and the
// updates are computed from the positions while the app runs, and kept
// for the next launch.
func TestComputed(t *testing.T) {
	a, loc := mount(t, text("x"), func(l *fake.Location) {
		l.Computed = true
		l.Answer = fake.LocationWhenInUse
	})
	var crossings []Crossing
	var batches []Batch
	ui.Post(func() {
		OnCrossing(func(c Crossing) { crossings = append(crossings, c) })
		OnUpdates(func(b Batch) { batches = append(batches, b) })
	})
	var s Support
	do(t, a, func() {
		s = Supported()
		if err := Monitor(Region{ID: "park", Latitude: 10, Longitude: 10, Radius: 100, Events: Enter | Exit | Dwell, Dwell: 50 * time.Millisecond}); err != nil {
			t.Error(err)
		}
		if err := StartUpdates(Updates{Distance: 50, Defer: 30 * time.Millisecond}); err != nil {
			t.Error(err)
		}
	})
	if s.Regions || s.Updates || s.MaxRegions != 0 || !s.Dwell || s.Relaunch {
		t.Fatalf("support %+v", s)
	}
	if !slices.Equal(loc.Asked, []string{"whenInUse"}) {
		t.Fatalf("asked %v", loc.Asked)
	}
	watched := func() { a.Until("the next position asked", loc.Watched) }
	watched()
	loc.Move(10.01, 10) // 1 km out: unknown to outside, no event
	watched()
	loc.MoveTo(fakePos(10.0001, 10, 500)) // inside, but too coarse to tell
	watched()
	loc.Move(10.0002, 10) // inside; 11 m from the last update
	a.Until("enter, dwell", func() bool { return len(crossings) == 2 })
	watched()
	loc.Move(10.01, 10)
	a.Until("exit", func() bool { return len(crossings) == 3 })
	got := []string{}
	for _, c := range crossings {
		got = append(got, c.Kind.String())
	}
	if !slices.Equal(got, []string{"enter", "dwell", "exit"}) {
		t.Fatalf("crossings %v", got)
	}
	a.Until("batched updates", func() bool {
		n := 0
		for _, b := range batches {
			n += len(b.Positions)
		}
		return n == 3
	})
	if len(batches) == 3 {
		t.Fatalf("not batched: %d batches", len(batches))
	}
	var regions []Region
	do(t, a, func() { regions, _ = Monitored() })
	if len(regions) != 1 || regions[0].Dwell != 50*time.Millisecond {
		t.Fatalf("monitored %+v", regions)
	}
}

func fakePos(lat, lon, acc float64) (p locationspec.Position) {
	return locationspec.Position{Lat: lat, Lon: lon, Acc: acc, Spd: -1, Crs: -1, T: time.Now().UnixMilli()}
}

// A handler that panics doesn't keep its event from being done (it'd
// come again at each launch, and end each).
func TestHandlerPanics(t *testing.T) {
	a, loc := mount(t, text("x"), func(l *fake.Location) {
		l.Level = fake.LocationAlways
		l.Cross("home", fake.RegionEnter)
	})
	ran := false
	ui.Post(func() {
		OnCrossing(func(Crossing) { panic("oops") })
		OnCrossing(func(Crossing) { ran = true })
	})
	a.Until("done", func() bool { return loc.Pending() == 0 && len(loc.Done) == 1 })
	if !ran {
		t.Fatal("the second handler didn't run")
	}
}

func TestDistance(t *testing.T) {
	// Tokyo station to Osaka station: about 403 km.
	if d := distance(35.6812, 139.7671, 34.7025, 135.4959); d < 402e3 || d > 404e3 {
		t.Fatalf("distance %v", d)
	}
}
