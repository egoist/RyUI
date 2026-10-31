// Services: what the platform does beyond views, a button each: sign-in,
// biometrics, push, files, the screen, the stores' services, a map,
// recording and work in the background.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/egoist/ryui/agerange"
	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/appleauth"
	"github.com/egoist/ryui/audio"
	"github.com/egoist/ryui/audio/record"
	"github.com/egoist/ryui/biometric"
	"github.com/egoist/ryui/device"
	"github.com/egoist/ryui/integrity"
	"github.com/egoist/ryui/maps"
	"github.com/egoist/ryui/periodic"
	"github.com/egoist/ryui/push"
	"github.com/egoist/ryui/review"
	"github.com/egoist/ryui/screen"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/splash"
	"github.com/egoist/ryui/tracking"
	"github.com/egoist/ryui/ui"
)

func App() ui.Node {
	// Keep the splash a moment, as an app loading its data would.
	splash.Hold()
	time.AfterFunc(600*time.Millisecond, func() { ui.Post(splash.Hide) })

	periodic.Handle("refresh", func(ctx context.Context) error {
		log.Println("services: a periodic refresh")
		return nil
	})
	push.OnOpen(func(m push.Message) { log.Printf("services: opened push %q", m.Title) })

	return ui.Stack(func() ui.Node {
		nav := ui.UseNavigator()
		status := signal.New("Tap a service.")
		// run calls f off the main thread (it waits on the platform) and
		// shows what it says.
		run := func(f func() string) func() {
			return func() {
				status.Set("…")
				go func() {
					s := f()
					ui.Post(func() { status.Set(s) })
				}()
			}
		}
		item := func(title string, f func()) ui.Node {
			return ui.Button(ui.ButtonProps{Title: ui.Val(title), Variant: ui.ButtonTinted, OnPress: f})
		}
		section := func(title string) ui.Node {
			return ui.StaticText(title, ui.Style{Font: ui.Headline, Margin: ui.Edges{Top: 8}})
		}
		recording := signal.New(false)
		hidden := signal.New(false)
		return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 10},
				ui.Text(status, ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),

				section("Accounts"),
				appleauth.Button(appleauth.ButtonProps{Style: appleauth.Automatic, OnPress: run(func() string {
					c, err := appleauth.SignIn(appleauth.Options{Scopes: appleauth.FullName | appleauth.Email})
					if err != nil {
						return err.Error()
					}
					return "Signed in: " + c.User
				})}),
				item("Face ID / fingerprint", run(func() string {
					s, err := biometric.Check()
					if err != nil {
						return err.Error()
					}
					if err := biometric.Authenticate(biometric.Options{Reason: "Unlock the demo"}); err != nil {
						return fmt.Sprintf("%v (%s)", err, s.Kind)
					}
					return "It's you (" + s.Kind.String() + ")"
				})),
				item("Age range", run(func() string {
					r, err := agerange.Request(13, 18)
					if err != nil {
						return err.Error()
					}
					return fmt.Sprintf("%+v", r)
				})),

				section("Notifications"),
				item("Register for push", run(func() string {
					r, err := push.Register()
					if err != nil {
						return err.Error()
					}
					log.Printf("services: %s token %s", r.Service, r.Token)
					return fmt.Sprintf("%s token %.12s… (allowed: %v)", r.Service, r.Token, r.Allowed)
				})),

				section("Files and sound"),
				item("Pick files", func() {
					device.PickFiles(device.FileOptions{Multiple: true}, func(fs []device.File, err error) {
						switch {
						case err != nil:
							status.Set(err.Error())
						case len(fs) == 0:
							status.Set("No files")
						default:
							status.Set(fmt.Sprintf("%d files: %s (%d bytes)…", len(fs), fs[0].Name, fs[0].Size))
						}
					})
				}),
				ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
					if recording.Get() {
						return "Stop recording"
					}
					return "Record 10 seconds"
				}), Variant: ui.ButtonTinted, OnPress: func() {
					if recording.Get() {
						record.Stop()
						return
					}
					recording.Set(true)
					record.Start(record.Options{MaxDuration: 10 * time.Second}, func(path string, err error) {
						recording.Set(false)
						if err != nil {
							status.Set(err.Error())
							return
						}
						status.Set("Recorded; playing it")
						audio.Play(path)
					})
				}}),

				section("The screen"),
				item("Landscape", func() { screen.Lock(screen.Landscape) }),
				item("Unlock rotation", func() { screen.Lock(screen.Any) }),
				item("Hide or show the status bar", func() {
					hidden.Set(!hidden.Get())
					screen.SetBars(screen.Bars{HideStatus: hidden.Get()})
				}),
				item("Keep out of screenshots", func() {
					screen.PreventCapture(true)
					status.Set("Screenshots and recordings are blank now (Android)")
				}),

				section("The stores"),
				item("Ask for a rating", func() { review.Request(nil) }),
				item("Tracking permission", run(func() string {
					s, err := tracking.Request()
					if err != nil {
						return err.Error()
					}
					return "Tracking: " + s.String()
				})),
				item("App integrity", run(func() string {
					if !integrity.Supported() {
						return "Not supported here"
					}
					id, err := integrity.GenerateKey()
					if err != nil {
						return err.Error()
					}
					return "App Attest key " + id
				})),

				section("More"),
				item("Map", func() { nav.Push(mapScreen, ui.ScreenOptions{Title: ui.Val("Map")}) }),
				item("Refresh in the background, hourly", func() {
					periodic.Schedule("refresh", periodic.Options{Every: time.Hour, Network: true})
					status.Set("Scheduled")
				}),
			),
		)
	}, ui.ScreenOptions{Title: ui.Val("Services")})
}

func mapScreen() ui.Node {
	region := signal.New(maps.Region{Center: maps.Coordinate{Lat: 37.7749, Lng: -122.4194}, LatDelta: 0.08, LngDelta: 0.08})
	picked := signal.New("Tap a pin")
	pins := []maps.Marker{
		{ID: "ferry", At: maps.Coordinate{Lat: 37.7955, Lng: -122.3937}, Title: "Ferry Building"},
		{ID: "park", At: maps.Coordinate{Lat: 37.7694, Lng: -122.4862}, Title: "Golden Gate Park", Color: ui.Hex(0x2E7D32)},
	}
	return ui.Column(ui.Style{Grow: 1},
		maps.View(maps.Props{
			Region:         region,
			Markers:        ui.Val(pins),
			OnRegionChange: func(r maps.Region) { region.Set(r) },
			OnMarkerPress:  func(id string) { picked.Set("Picked " + id) },
			OnPress:        func(c maps.Coordinate) { picked.Set(fmt.Sprintf("%.4f, %.4f", c.Lat, c.Lng)) },
			OnError:        func(m string) { picked.Set(m) },
			Style:          ui.Style{Grow: 1},
		}),
		ui.SafeArea(ui.Style{Padding: ui.All(16)}, ui.Text(picked)),
	)
}

func main() { app.Run(App) }
