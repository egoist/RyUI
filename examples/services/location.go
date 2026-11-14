package main

import (
	"fmt"
	"log"
	"time"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/location"
	"github.com/egoist/asuka/sdk/notify"
	"github.com/egoist/asuka/sdk/store"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// What a run tracker or a reminder by place does with the location out of
// sight: updates that go on in the background, a region around where the
// device is now, each event noted in a log kept across launches (the
// system starts the app for them without a screen: the log shows them
// when the user opens it) and a crossing told in a notification.

// locationLog is what the background updates and regions brought, newest
// first, kept in the store.
var locationLog = store.Signal("location.log", []string{})

func logLocation(line string) {
	log.Println("services: location:", line)
	l := append([]string{time.Now().Format("15:04:05 ") + line}, locationLog.Get()...)
	locationLog.Set(l[:min(len(l), 50)])
}

// setUpLocation sets the handlers: in main, so the events the system
// starts the app for find them before any screen.
func setUpLocation() {
	location.OnUpdates(func(b location.Batch) {
		p := b.Positions[len(b.Positions)-1]
		logLocation(fmt.Sprintf("%d positions, the last %.5f, %.5f (±%.0f m)", len(b.Positions), p.Latitude, p.Longitude, p.Accuracy))
	})
	location.OnCrossing(func(c location.Crossing) {
		logLocation(fmt.Sprintf("%s %s", c.Kind, c.Region))
		// Showing waits on the system: off the main thread, the app kept
		// running for it.
		_, done := c.Defer()
		go func() {
			defer done()
			notify.Show(notify.Notification{ID: "region", Title: "Here", Body: fmt.Sprintf("%s: %s", c.Kind, c.Region)})
		}()
	})
}

func locationScreen() ui.Node {
	auth := location.UseAuthorization()
	status := signal.New("")
	tracking := signal.New(false)
	run := func(f func() string) func() {
		return func() {
			status.Set("…")
			go func() {
				s := f()
				ui.Post(func() { status.Set(s) })
			}()
		}
	}
	go func() {
		_, on, _ := location.Updating()
		ui.Post(func() { tracking.Set(on) })
	}()
	item := func(title string, f func()) ui.Node {
		return ui.Button(ui.ButtonProps{Title: ui.Val(title), Variant: ui.ButtonTinted, OnPress: f})
	}
	describe := func(a location.Authorization, err error) string {
		if err != nil {
			return err.Error()
		}
		return fmt.Sprintf("%s, precise %v", a.Level, a.Precise)
	}
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 10},
			ui.Text(signal.Func(func() string {
				a := auth.Get()
				s := fmt.Sprintf("Allowed: %s; precise: %v", a.Level, a.Precise)
				if a.Off {
					s += "; location services are off"
				}
				return s
			}), ui.Style{Font: fonts.Headline}),
			ui.Text(status, ui.Style{Font: fonts.Footnote, Color: colors.SecondaryLabel}),
			item("Allow while in use", run(func() string { return describe(location.Request()) })),
			item("Allow always", run(func() string { return describe(location.RequestAlways()) })),
			item("Precise this time", run(func() string { return describe(location.RequestPrecise("route")) })),
			item("Location settings", location.OpenSettings),
			ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
				if tracking.Get() {
					return "Stop tracking"
				}
				return "Track in the background"
			}), Variant: ui.ButtonTinted, OnPress: run(func() string {
				if _, on, _ := location.Updating(); on {
					location.StopUpdates()
					ui.Post(func() { tracking.Set(false) })
					return "Stopped"
				}
				err := location.StartUpdates(location.Updates{
					Accuracy: location.AccuracyHigh, Distance: 20, Activity: location.ActivityFitness,
					Defer: 30 * time.Second, Indicator: true,
					Notification: location.Notification{Title: "Tracking your route", Text: "Services follows where you go"},
				})
				if err != nil {
					return err.Error()
				}
				ui.Post(func() { tracking.Set(true) })
				return "Tracking: leave the app, and come back"
			})}),
			item("Watch a region here (150 m)", run(func() string {
				p, err := location.Current()
				if err != nil {
					return err.Error()
				}
				err = location.Monitor(location.Region{ID: "here", Latitude: p.Latitude, Longitude: p.Longitude, Radius: 150,
					Events: location.Enter | location.Exit | location.Dwell, Dwell: 2 * time.Minute})
				if err != nil {
					return err.Error()
				}
				s := location.Supported()
				return fmt.Sprintf("Watching 150 m around %.5f, %.5f (system: %v, at most %d, relaunch %v)", p.Latitude, p.Longitude, s.Regions, s.MaxRegions, s.Relaunch)
			})),
			item("Stop watching regions", run(func() string {
				if err := location.StopMonitoringAll(); err != nil {
					return err.Error()
				}
				return "No regions"
			})),
			item("Clear the log", func() { locationLog.Set([]string{}) }),
			ui.StaticText("Log", ui.Style{Font: fonts.Headline, Margin: ui.Edges{Top: 8}}),
			ui.Text(signal.Func(func() string {
				l := locationLog.Get()
				if len(l) == 0 {
					return "Nothing yet."
				}
				s := ""
				for _, line := range l {
					s += line + "\n"
				}
				return s
			}), ui.Style{Font: fonts.Footnote}),
		),
	)
}
