// Services: what the platform does beyond views, a button each: sign-in,
// biometrics, push, files, the screen, the stores' services, a map,
// recording and work in the background.
package main

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/egoist/ryui/agerange"
	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/appleauth"
	"github.com/egoist/ryui/audio"
	"github.com/egoist/ryui/audio/record"
	"github.com/egoist/ryui/biometric"
	"github.com/egoist/ryui/calendar"
	"github.com/egoist/ryui/contacts"
	"github.com/egoist/ryui/device"
	"github.com/egoist/ryui/imageedit"
	"github.com/egoist/ryui/integrity"
	"github.com/egoist/ryui/maps"
	"github.com/egoist/ryui/periodic"
	"github.com/egoist/ryui/push"
	"github.com/egoist/ryui/review"
	"github.com/egoist/ryui/screen"
	"github.com/egoist/ryui/sensors"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/speech"
	"github.com/egoist/ryui/splash"
	"github.com/egoist/ryui/tracking"
	"github.com/egoist/ryui/ui"
	"github.com/egoist/ryui/widget"
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
	status := signal.New("Tap a service.")
	// ryui.json's shortcuts and the widget open the app with these.
	ui.OnOpenURL(func(u string) {
		status.Set("Opened with " + u)
		if u == "ryuiservices://speak" {
			speech.Speak("Hello from RyUI", speech.Options{}, nil)
		}
	})

	return ui.Stack(func() ui.Node {
		nav := ui.UseNavigator()
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
		lastImage := signal.New("")
		tilting := signal.New(false)
		var stopTilt func()
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

				section("Sensors and speech"),
				ui.Button(ui.ButtonProps{Title: signal.Func(func() string {
					if tilting.Get() {
						return "Stop watching the tilt"
					}
					return "Watch the tilt"
				}), Variant: ui.ButtonTinted, OnPress: func() {
					if stopTilt != nil {
						stopTilt()
						stopTilt = nil
						tilting.Set(false)
						return
					}
					tilting.Set(true)
					go func() {
						if !sensors.Available(sensors.Motion) {
							ui.Post(func() { status.Set("No motion sensor here") })
						}
					}()
					stopTilt = sensors.WatchMotion(100*time.Millisecond, func(m sensors.MotionReading) {
						a := m.Attitude
						status.Set(fmt.Sprintf("pitch %.0f°, roll %.0f°, heading %.0f°", a.Pitch*180/3.14159, a.Roll*180/3.14159, a.Heading))
					})
				}}),
				item("Which sensors", run(func() string {
					var have []string
					for s := sensors.Accelerometer; s <= sensors.Pedometer; s++ {
						have = append(have, fmt.Sprintf("%s %v", s, sensors.Available(s)))
					}
					return strings.Join(have, ", ")
				})),
				item("Simulate motion (iOS simulator)", func() {
					simulateMotion(status)
				}),
				item("Steps today", run(func() string {
					now := time.Now()
					s, err := sensors.StepsBetween(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), now)
					if err != nil {
						return err.Error()
					}
					return fmt.Sprintf("%d steps today", s.Steps)
				})),
				item("Say hello", func() {
					speech.Speak("Hello from RyUI. This sentence is long enough to pause.", speech.Options{}, func(err error) {
						if err != nil {
							status.Set("speech: " + err.Error())
						} else {
							status.Set("Spoken")
						}
					})
				}),
				item("Pause, resume, stop speech", func() {
					// Speaks, pauses a second in, resumes, then stops: done gets ErrStopped.
					var steps []string
					began := time.Now()
					speech.Speak("This sentence is long enough that it is still being spoken when the demo stops it: it goes on, and on, about nothing in particular, for a good while longer.", speech.Options{}, func(err error) {
						steps = append(steps, fmt.Sprint("done: ", err, " at ", time.Since(began).Round(100*time.Millisecond)))
						status.Set(strings.Join(steps, "; "))
					})
					time.AfterFunc(time.Second, func() {
						ui.Post(func() { speech.Pause(); steps = append(steps, "paused") })
						go func() {
							paused := speech.Speaking()
							time.Sleep(time.Second)
							ui.Post(func() {
								speech.Resume()
								steps = append(steps, fmt.Sprint("resumed (speaking while paused: ", paused, ")"))
							})
							time.Sleep(time.Second)
							ui.Post(func() { speech.Stop(); steps = append(steps, "stopped") })
						}()
					})
				}),
				item("Voices", run(func() string {
					vs, err := speech.Voices()
					if err != nil {
						return err.Error()
					}
					return fmt.Sprintf("%d voices", len(vs))
				})),

				section("Images, people, events"),
				item("Shrink a photo", func() {
					device.PickPhotos(1, func(paths []string, err error) {
						if err != nil || len(paths) == 0 {
							return
						}
						go func() {
							r, err := imageedit.Edit(paths[0], []imageedit.Action{imageedit.Resize(320, 0), imageedit.Rotate(90)}, imageedit.Options{Quality: 0.8})
							ui.Post(func() {
								if err != nil {
									status.Set(err.Error())
									return
								}
								lastImage.Set(r.Path)
								status.Set(fmt.Sprintf("%d×%d: %s", r.Width, r.Height, r.Path))
							})
						}()
					})
				}),
				item("Video frame at 1.5 s", run(func() string {
					// assets/clip.mp4: 320×180, its rotation 90° (as a phone's portrait video).
					r, err := imageedit.VideoFrame("clip.mp4", 1500*time.Millisecond, nil, imageedit.Options{Format: imageedit.PNG})
					if err != nil {
						return "video frame: " + err.Error()
					}
					return fmt.Sprintf("frame %d×%d: %s", r.Width, r.Height, r.Path)
				})),
				item("Copy that image, then paste it", func() {
					if lastImage.Get() == "" {
						status.Set("Shrink a photo first")
						return
					}
					device.CopyImage(lastImage.Get(), func(err error) {
						if err != nil {
							status.Set("copy: " + err.Error())
							return
						}
						run(paste)()
					})
				}),
				item("Pick a contact", func() {
					contacts.Pick(func(c *contacts.Contact, err error) {
						switch {
						case err != nil:
							status.Set(err.Error())
						case c == nil:
							status.Set("No contact")
						default:
							status.Set(fmt.Sprintf("%s, %d phones", c.Name, len(c.Phones)))
						}
					})
				}),
				item("Add an event", func() {
					start := time.Now().Add(time.Hour).Truncate(time.Hour)
					calendar.Add(calendar.Event{Title: "RyUI demo", Location: "Online", Start: start, End: start.Add(30 * time.Minute)},
						func(saved bool, err error) { status.Set(fmt.Sprint("event saved: ", saved, " ", err)) })
				}),
				item("Calendars", run(func() string {
					if a, err := calendar.Request(); err != nil || a != calendar.Full {
						return fmt.Sprint("calendar access: ", a, " ", err)
					}
					cs, err := calendar.Calendars()
					if err != nil {
						return err.Error()
					}
					var names []string
					for _, c := range cs {
						names = append(names, fmt.Sprintf("%s %s writable:%v", c.Title, c.Color, c.Writable))
					}
					return fmt.Sprintf("%d calendars: %s", len(cs), strings.Join(names, ", "))
				})),
				item("Today's events", run(func() string {
					if a, err := calendar.Request(); err != nil || a != calendar.Full {
						return fmt.Sprint("calendar access: ", a, " ", err)
					}
					now := time.Now()
					es, err := calendar.Events(now.Add(-12*time.Hour), now.Add(12*time.Hour))
					if err != nil {
						return err.Error()
					}
					var out []string
					for _, e := range es {
						if e.AllDay {
							out = append(out, fmt.Sprintf("%s (all day %s)", e.Title, e.Start.Format("Jan 2")))
						} else {
							out = append(out, fmt.Sprintf("%s %s–%s %s", e.Title, e.Start.Local().Format("15:04"), e.End.Local().Format("15:04"), e.Location))
						}
					}
					return fmt.Sprintf("%d events around now: %s", len(es), strings.Join(out, "; "))
				})),
				item("Update the widget", func() {
					widget.Set("energy", widget.Entry{
						Title: "Energy", Symbol: "bolt.fill", Value: "72%", Body: "Moving well today", Caption: time.Now().Format("15:04"),
						Progress: 0.72, ShowProgress: true, Until: time.Now().Add(25 * time.Minute), URL: "ryuiservices://speak",
					})
					status.Set("Widget updated")
				}),

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

// simulateMotion feeds readings in Core Motion's terms through the iOS
// host's conversions (the simulator has no motion sensors) and shows what
// the Go side gets: a face-up accelerometer, then device motion face up
// with the top to the east while rising, then upright.
func simulateMotion(status *signal.Signal[string]) {
	var got []string
	stopAcc := sensors.WatchAccelerometer(10*time.Millisecond, func(v sensors.Vector) {
		got = append(got, fmt.Sprintf("acc z %+.1fg", v.Z))
	})
	stopMotion := sensors.WatchMotion(10*time.Millisecond, func(m sensors.MotionReading) {
		a := m.Attitude
		got = append(got, fmt.Sprintf("heading %.0f° pitch %.0f° roll %.0f° up %+.1fg rot %.1f",
			a.Heading, a.Pitch*180/math.Pi, a.Roll*180/math.Pi, m.Acceleration.Z, m.Rotation.Z))
	})
	steps := []string{
		`{"kind": 0, "acceleration": [0, 0, -1]}`,
		`{"kind": 3, "gravity": [0, 0, -1], "user": [0, 0, -0.5], "rotation": [0, 0, 0.3], "field": [-20, 0, -40]}`,
		`{"kind": 3, "gravity": [0, -1, 0], "field": [0, -40, -20]}`,
	}
	var next func(i int)
	next = func(i int) {
		if i == len(steps) {
			time.AfterFunc(300*time.Millisecond, func() {
				ui.Post(func() {
					stopAcc()
					stopMotion()
					status.Set(strings.Join(got, "; "))
				})
			})
			return
		}
		ui.Call("sensors", "simulate", []byte(steps[i]), func(_ []byte, err error) {
			if err != nil {
				got = append(got, "simulate: "+err.Error())
			}
			time.AfterFunc(300*time.Millisecond, func() { ui.Post(func() { next(i + 1) }) })
		})
	}
	next(0)
}

// paste reads the clipboard's image back.
func paste() string {
	kinds, err := device.ClipboardHas()
	if err != nil {
		return err.Error()
	}
	if !kinds.Image {
		return fmt.Sprintf("No image on the clipboard (%+v)", kinds)
	}
	p, ok, err := device.ClipboardImage()
	if err != nil || !ok {
		return fmt.Sprint("paste: ", err)
	}
	return "Pasted " + p
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
