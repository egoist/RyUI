package main

import (
	"strings"
	"sync"
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/uitest"
)

var setUp sync.Once

// The location screen on a simulated phone: the user allows Always, a
// region is watched around where the device is, and leaving it (with the
// app out of sight, as the system would start it) lands in the log.
func TestLocationScreen(t *testing.T) {
	locationLog.Set([]string{})
	var loc *fake.Location
	setUp.Do(setUpLocation) // main's, once a process
	a := uitest.Mount(t, locationScreen, uitest.Setup(func(h *fake.Host) {
		loc = h.Location()
		loc.Answer = fake.LocationAlways
		loc.Move(35.6812, 139.7671)
	}))
	a.Expect("Allowed: not asked; precise: false", "Nothing yet.")
	a.Tap("Allow always")
	a.WaitFor("Allowed: always; precise: true")
	a.Tap("Watch a region here (150 m)")
	a.WaitFor("Watching 150 m around 35.68120, 139.76710 (system: true, at most 20, relaunch true)")
	a.Until("the enter in the log", func() bool { return len(locationLog.Get()) == 1 })
	loc.Move(35.70, 139.7671) // 2 km north
	a.Until("the exit in the log", func() bool { return len(locationLog.Get()) == 2 })
	if l := locationLog.Get(); !strings.HasSuffix(l[0], "exit here") || !strings.HasSuffix(l[1], "enter here") {
		t.Fatalf("log %q", l)
	}
	// Done once the notification's shown (Defer's work).
	a.Until("the events done", func() bool { return loc.Pending() == 0 })
	a.Tap("Stop watching regions")
	a.WaitFor("No regions")
}
