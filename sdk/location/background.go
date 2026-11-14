package location

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/locationspec"
	"github.com/egoist/asuka/ui"
)

// Accuracy is how exact the background updates are, against the power
// they use.
type Accuracy uint8

const (
	// AccuracyHigh: the best the device has, GPS outdoors (iOS's best;
	// Android's high accuracy).
	AccuracyHigh Accuracy = iota
	// AccuracyNavigation: the best, with the other sensors' help, for
	// turn by turn directions (iOS's best for navigation).
	AccuracyNavigation
	// AccuracyBalanced: to about 100 meters, from Wi-Fi and cell towers
	// mostly (a block).
	AccuracyBalanced
	// AccuracyLow: to about a kilometer (a town).
	AccuracyLow
	// AccuracyPassive: only the positions the device finds for others
	// (Android's passive; iOS's three kilometers), at almost no power.
	AccuracyPassive
)

// Activity is what the user does as the device moves: iOS tunes its
// updates to it (and pauses them when it ends, with AutoPause). Android
// has no such setting.
type Activity uint8

const (
	ActivityOther Activity = iota
	// ActivityAutomotive: driving, on roads.
	ActivityAutomotive
	// ActivityOtherNavigation: on a boat, a train, off roads.
	ActivityOtherNavigation
	// ActivityFitness: walking, running, cycling.
	ActivityFitness
	// ActivityAirborne: in a plane.
	ActivityAirborne
)

// Updates are the background updates' options.
type Updates struct {
	Accuracy Accuracy
	// Distance is how far the device moves between two updates, in
	// meters (0: any move).
	Distance float64
	// Interval is how often updates come, about (Android's; default 10
	// seconds). iOS sends them as the device moves.
	Interval time.Duration
	Activity Activity
	// Defer lets updates wait up to this long to come together, in one
	// Batch: the app wakes less often, and Android keeps its radio and
	// CPU asleep meanwhile (its batched delivery). 0: each as it comes.
	Defer time.Duration
	// Significant gets only the significant changes, of about 500
	// meters, from cell towers and Wi-Fi: almost no power, and on iOS
	// (with Always) they start the app when it isn't running, even after
	// the user ended it. Accuracy, Distance, Interval and Activity don't
	// apply.
	Significant bool
	// AutoPause lets iOS pause the updates when the device doesn't move
	// for a while (by Activity: a car parked); they resume as it moves,
	// with the app on screen.
	AutoPause bool
	// Indicator shows iOS's background location indicator (the blue
	// pill in the status bar) while the app gets updates in the
	// background, as iOS does anyway for an app that has WhenInUse only.
	Indicator bool
	// Notification is the notification Android shows while the updates
	// run: with it, a foreground service of the location type runs, and
	// updates come at their rate with the app out of sight, WhenInUse
	// enough. Without it (Title ""), Android gives a backgrounded app a
	// few updates an hour, and only with Always. iOS shows none.
	Notification Notification
}

// Notification is Android's foreground service's notification.
type Notification struct{ Title, Text string }

func (o Updates) wire() locationspec.Updates {
	return locationspec.Updates{
		Accuracy: int(o.Accuracy), Distance: max(o.Distance, 0), Interval: o.Interval.Milliseconds(),
		Activity: int(o.Activity), Defer: o.Defer.Milliseconds(), Significant: o.Significant,
		AutoPause: o.AutoPause, Indicator: o.Indicator, Title: o.Notification.Title, Text: o.Notification.Text,
	}
}

func updatesFrom(w locationspec.Updates) Updates {
	return Updates{
		Accuracy: Accuracy(w.Accuracy), Distance: w.Distance, Interval: time.Duration(w.Interval) * time.Millisecond,
		Activity: Activity(w.Activity), Defer: time.Duration(w.Defer) * time.Millisecond, Significant: w.Significant,
		AutoPause: w.AutoPause, Indicator: w.Indicator, Notification: Notification{Title: w.Title, Text: w.Text},
	}
}

// Transition is a region's: the device came in, went out, or stayed.
type Transition uint8

const (
	Enter Transition = 1 << iota
	Exit
	// Dwell: the device stayed inside for the region's Dwell, after an
	// Enter. Android's geofences have it; so do the regions computed from
	// positions (Linux, the web). iOS, macOS and Windows don't report it.
	Dwell
)

func (t Transition) String() string {
	switch t {
	case Enter:
		return "enter"
	case Exit:
		return "exit"
	case Dwell:
		return "dwell"
	}
	return "unknown"
}

// Region is a circle on the map the system watches for the app.
type Region struct {
	// ID names it: Monitor with an ID monitored replaces that one.
	ID                  string
	Latitude, Longitude float64
	// Radius is in meters. The systems are reliable from about 100 to
	// 200 meters (iOS asks for 100 at least; Android, 100 to 150); they
	// cap it too (iOS, macOS: Support's MaxRadius).
	Radius float64
	// Events are the transitions reported: Enter, Exit and Dwell
	// together (0: Enter | Exit).
	Events Transition
	// Dwell is how long the device stays inside before a Dwell event
	// (Android's loitering delay; default 5 minutes).
	Dwell time.Duration
}

func (r Region) wire() locationspec.Region {
	ev := r.Events
	if ev == 0 {
		ev = Enter | Exit
	}
	dwell := r.Dwell
	if ev&Dwell != 0 && dwell <= 0 {
		dwell = 5 * time.Minute
	}
	return locationspec.Region{ID: r.ID, Lat: r.Latitude, Lon: r.Longitude, Radius: r.Radius, Events: int(ev), Dwell: dwell.Milliseconds()}
}

func regionFrom(w locationspec.Region) Region {
	return Region{ID: w.ID, Latitude: w.Lat, Longitude: w.Lon, Radius: w.Radius, Events: Transition(w.Events), Dwell: time.Duration(w.Dwell) * time.Millisecond}
}

var (
	// ErrNeedsAlways is the error of what needs the Always authorization
	// (RequestAlways) without it: regions on iOS and Android.
	ErrNeedsAlways = errors.New("location: needs the Always authorization (RequestAlways)")
	// ErrTooManyRegions is Monitor's past the system's limit
	// (Support.MaxRegions): stop monitoring another first.
	ErrTooManyRegions = errors.New("location: too many regions")
)

// hostErr is the error a host's answer means.
func hostErr(err error) error {
	if err == nil {
		return nil
	}
	switch msg := err.Error(); {
	case strings.Contains(msg, "denied"):
		return ErrDenied
	case msg == "always":
		return ErrNeedsAlways
	case strings.HasPrefix(msg, "too many regions"):
		return fmt.Errorf("%w%s", ErrTooManyRegions, strings.TrimPrefix(msg, "too many regions"))
	}
	return err
}

// Support is what the system does for the app itself, and so with the
// app not running. What it doesn't do, this package does from the
// positions it gets while the app runs (Watch's): the background updates
// and the regions on macOS, Windows and Linux, the regions on Linux and
// the web.
type Support struct {
	// Updates: the system delivers background updates (iOS, Android).
	Updates bool
	// Regions: the system monitors the regions (iOS, Android, macOS,
	// Windows).
	Regions bool
	// MaxRegions is how many regions it monitors at once, for the app:
	// iOS and macOS 20, Android 100. 0: no limit stated (Windows; the
	// regions computed here, as many as the app has).
	MaxRegions int
	// MaxRadius is the largest radius it takes, in meters (iOS and
	// macOS's maximumRegionMonitoringDistance); 0: no limit stated.
	MaxRadius float64
	// Dwell: Dwell events come (Android; the computed regions).
	Dwell bool
	// Relaunch: the system starts the app, without a screen, for an
	// event when it isn't running (iOS, Android; a page on the web, a
	// desktop app, only get events while they run).
	Relaunch bool
}

var (
	capsMu  sync.Mutex
	capsGot bool
	capsVal locationspec.Capabilities
	capsMax float64
)

// capabilities is the host's, asked once. Not on the main thread.
func capabilities() locationspec.Capabilities {
	capsMu.Lock()
	defer capsMu.Unlock()
	if !capsGot {
		b, err := ui.CallWait("locationBackground", "capabilities", nil)
		var c struct {
			locationspec.Capabilities
			MaxRadius float64 `json:"maxRadius"`
		}
		if err == nil && json.Unmarshal(b, &c) == nil {
			capsVal, capsMax = c.Capabilities, c.MaxRadius
		}
		capsGot = true
	}
	return capsVal
}

// Supported is what the system does here. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Supported() Support {
	c := capabilities()
	s := Support{Updates: c.Updates, Regions: c.Regions, MaxRegions: c.Limit, Dwell: c.Dwell, Relaunch: c.Relaunch}
	if c.Regions {
		capsMu.Lock()
		s.MaxRadius = capsMax
		capsMu.Unlock()
	} else {
		s.MaxRegions, s.Dwell = 0, true
	}
	return s
}

// StartUpdates starts the background updates, which OnUpdates' handlers
// get: they go on with the app out of sight, and across launches (on
// iOS and Android, the system starts the app for them), until
// StopUpdates; starting them again changes their options. It asks for
// permission (Request) if the app hasn't. Start them with the app on
// screen: Android starts its foreground service only then. Not on the
// main thread.
//
// iOS: Core Location with allowsBackgroundLocationUpdates (iOS 17's
// CLLocationUpdate.liveUpdates, for AccuracyHigh and AccuracyNavigation,
// with a CLBackgroundActivitySession), or its significant-change
// service. An app that calls it is given UIBackgroundModes' location.
// Android: the Fused Location Provider (Google Play services; Android's
// location manager on a device without them), delivered to a
// PendingIntent, so they come with the app not running; with
// o.Notification, in a foreground service of the location type. Elsewhere
// the updates are computed from the positions while the app runs.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func StartUpdates(o Updates) error {
	events.start()
	if !capabilities().Updates {
		return computed.startUpdates(o)
	}
	b, _ := json.Marshal(o.wire())
	_, err := ui.CallWait("locationBackground", "start", b)
	return hostErr(err)
}

// StopUpdates stops the background updates. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func StopUpdates() error {
	if !capabilities().Updates {
		computed.stopUpdates()
		return nil
	}
	_, err := ui.CallWait("locationBackground", "stop", nil)
	return hostErr(err)
}

// Updating returns the background updates' options, and whether they
// run (they were started, at a launch before this one perhaps). Not on
// the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Updating() (Updates, bool, error) {
	if !capabilities().Updates {
		w, ok := computed.updating()
		return updatesFrom(w), ok, nil
	}
	b, err := ui.CallWait("locationBackground", "updating", nil)
	if err != nil {
		return Updates{}, false, hostErr(err)
	}
	var w *locationspec.Updates
	if err := json.Unmarshal(b, &w); err != nil || w == nil {
		return Updates{}, false, err
	}
	return updatesFrom(*w), true, nil
}

// Monitor watches region r for the app, until StopMonitoring: its
// transitions go to OnCrossing's handlers, with the app in the
// background, or not running (iOS and Android start it). If the device
// is inside as monitoring starts, an Enter comes. It holds across
// launches. Monitoring r.ID again changes that region. It needs Always on
// iOS and Android (ErrNeedsAlways); past the system's limit it fails
// with ErrTooManyRegions. Not on the main thread.
//
// iOS: Core Location's CLMonitor (iOS 17), CLCircularRegion before; 20
// regions at most. Android: Google Play services' GeofencingClient (the
// location manager's proximity alerts on a device without them); 100 at
// most. macOS: Core Location's region monitoring, while the app runs; 20
// at most. Windows: Windows.Devices.Geolocation.Geofencing's
// GeofenceMonitor, while the app runs. Linux (GeoClue has no geofences)
// and the web: computed from the positions while the app (the page)
// runs.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Monitor(r Region) error {
	if r.ID == "" {
		return errors.New("location: a region needs an ID")
	}
	if !(r.Radius > 0) || math.Abs(r.Latitude) > 90 || math.Abs(r.Longitude) > 180 {
		return fmt.Errorf("location: region %q: bad center or radius", r.ID)
	}
	events.start()
	if !capabilities().Regions {
		return computed.monitor(r)
	}
	b, _ := json.Marshal(r.wire())
	_, err := ui.CallWait("locationBackground", "monitor", b)
	return hostErr(err)
}

// StopMonitoring stops monitoring the region id. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func StopMonitoring(id string) error {
	if id == "" {
		return nil
	}
	return unmonitor(id)
}

// StopMonitoringAll stops monitoring every region. Not on the main
// thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func StopMonitoringAll() error { return unmonitor("") }

func unmonitor(id string) error {
	if !capabilities().Regions {
		computed.unmonitor(id)
		return nil
	}
	_, err := ui.CallWait("locationBackground", "unmonitor", []byte(id))
	return hostErr(err)
}

// Monitored is the regions monitored. One the system stopped (iOS when
// it can't monitor it; Android when location was turned off, until it's
// on again and the app starts) isn't. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Monitored() ([]Region, error) {
	if !capabilities().Regions {
		return computed.regions(), nil
	}
	b, err := ui.CallWait("locationBackground", "regions", nil)
	if err != nil {
		return nil, hostErr(err)
	}
	var ws []locationspec.Region
	if err := json.Unmarshal(b, &ws); err != nil {
		return nil, err
	}
	rs := make([]Region, len(ws))
	for i, w := range ws {
		rs[i] = regionFrom(w)
	}
	return rs, nil
}

// Batch is background updates the system delivered together.
type Batch struct {
	// Positions are oldest first.
	Positions []Position
	work      *work
}

// Defer, called in OnUpdates' f, keeps the app running for work that
// goes on after f returns (sending the positions to a server): the
// system lets the app finish until done is called, or ctx's deadline, a
// little before the system's (iOS gives about 30 seconds in the
// background, Android about 10). Any goroutine; done can be called more
// than once.
func (b Batch) Defer() (ctx context.Context, done func()) { return b.work.deferred() }

// Crossing is a region's transition.
type Crossing struct {
	// Region is its ID.
	Region string
	Kind   Transition
	// At is where the device was, when the system says (its Time is
	// zero otherwise).
	At Position
	// Time is when it happened.
	Time time.Time
	work *work
}

// Defer is Batch.Defer's, for a crossing.
func (c Crossing) Defer() (ctx context.Context, done func()) { return c.work.deferred() }

// OnUpdates calls f, on the main thread, with each Batch of background
// updates, until stop. Updates that came before any f (those the system
// started the app for, those of a launch that ended before they were
// handled) wait for the first: set it as the app starts, in main or the
// root component. An update is handed over again at a later launch until
// a handler returned (and its Defer is done).
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func OnUpdates(f func(Batch)) (stop func()) { return events.add(kindUpdates, f) }

// OnCrossing calls f, on the main thread, with each region's
// transition, until stop. As OnUpdates' handlers, a crossing that came
// before any f waits for the first.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func OnCrossing(f func(Crossing)) (stop func()) { return events.add(kindCrossings, f) }

const (
	kindUpdates = iota
	kindCrossings
)

// work is an event's: what holds it (the handlers running, Defer's),
// and the deadline. Once it's done, the host is told (a native event:
// its ID), and lets the app be suspended.
type work struct {
	id     string
	ctx    context.Context
	cancel func()
	mu     sync.Mutex
	held   int
}

func newWork(id string, seconds int) *work {
	w := &work{id: id, held: 1}
	if seconds > 0 {
		// A margin before the system's deadline, to report it done.
		w.ctx, w.cancel = context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second-time.Second/2)
	} else {
		w.ctx, w.cancel = context.WithCancel(context.Background())
	}
	return w
}

func (w *work) deferred() (context.Context, func()) {
	if w == nil {
		return context.Background(), func() {}
	}
	w.mu.Lock()
	w.held++
	w.mu.Unlock()
	var once sync.Once
	return w.ctx, func() { once.Do(w.release) }
}

func (w *work) release() {
	w.mu.Lock()
	w.held--
	last := w.held == 0
	w.mu.Unlock()
	if !last {
		return
	}
	w.cancel()
	if w.id != "" {
		id := w.id
		ui.Post(func() { ui.Call("locationBackground", "done", []byte(id), nil) })
	}
}

// dispatcher hands the events to the handlers of their kind, holding
// those of a kind no handler takes yet.
type dispatcher struct {
	mu       sync.Mutex
	started  bool
	handlers [2][]*any
	held     [2][]locationspec.Event
}

var events dispatcher

// start, at the first use of the package's background part, takes the
// host's events (once a root is mounted, when calls work) and resumes
// what's computed here.
func (d *dispatcher) start() {
	d.mu.Lock()
	start := !d.started
	d.started = true
	d.mu.Unlock()
	if start {
		boot.AfterMount(func() {
			go d.serve()
			go computed.resume()
		})
	}
}

func kindOf(e locationspec.Event) int {
	if e.Region != "" {
		return kindCrossings
	}
	return kindUpdates
}

func (d *dispatcher) add(kind int, f any) (stop func()) {
	p := &f
	d.mu.Lock()
	d.handlers[kind] = append(d.handlers[kind], p)
	held := d.held[kind]
	d.held[kind] = nil
	d.mu.Unlock()
	d.start()
	if len(held) > 0 {
		ui.Post(func() {
			for _, e := range held {
				d.deliver(e)
			}
		})
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			hs := d.handlers[kind]
			for i, h := range hs {
				if h == p {
					d.handlers[kind] = append(hs[:i:i], hs[i+1:]...)
					break
				}
			}
			d.mu.Unlock()
		})
	}
}

// serve takes the host's events ("next" answers with one) until the
// host has none to give (a host whose events are all computed here).
func (d *dispatcher) serve() {
	for {
		b, err := ui.CallWait("locationBackground", "next", nil)
		if err != nil {
			if err.Error() == "stopped" {
				continue
			}
			// No events from this host (its are computed here), or the
			// root went: a handler added later, under a root mounted
			// again, starts it again.
			d.mu.Lock()
			d.started = false
			d.mu.Unlock()
			return
		}
		var e locationspec.Event
		if json.Unmarshal(b, &e) != nil {
			continue
		}
		ui.Post(func() { d.deliver(e) })
	}
}

// deliver hands e to its handlers, or holds it for the first. Main
// thread.
func (d *dispatcher) deliver(e locationspec.Event) {
	kind := kindOf(e)
	d.mu.Lock()
	hs := make([]any, len(d.handlers[kind]))
	for i, h := range d.handlers[kind] {
		hs[i] = *h
	}
	if len(hs) == 0 {
		d.held[kind] = append(d.held[kind], e)
		d.mu.Unlock()
		return
	}
	d.mu.Unlock()
	w := newWork(e.ID, e.Seconds)
	defer w.release()
	if kind == kindUpdates {
		b := Batch{Positions: make([]Position, len(e.Positions)), work: w}
		for i, p := range e.Positions {
			b.Positions[i] = position(p)
		}
		for _, h := range hs {
			safely(func() { h.(func(Batch))(b) })
		}
		return
	}
	c := Crossing{Region: e.Region, Kind: Transition(e.Kind), Time: time.UnixMilli(e.T), work: w}
	if e.At != nil {
		c.At = position(*e.At)
	}
	for _, h := range hs {
		safely(func() { h.(func(Crossing))(c) })
	}
}

// safely runs a handler: one that panics is logged, and the event is
// done all the same, or it'd come again at each launch, and end each.
func safely(f func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("location: a handler panicked: %v\n%s", r, debug.Stack())
		}
	}()
	f()
}
