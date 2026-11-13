// Package liveactivity shows what's happening now, live, where the system
// shows it outside the app (expo-live-activity): a delivery on its way,
// a ride, a game's score, a timer. Each platform's own:
//
//   - iOS: a Live Activity (ActivityKit, iOS 16.2): on the lock screen,
//     in the Dynamic Island and, on an iPhone without one, as a banner,
//     drawn by a template in the app's widget extension, which the build
//     adds (and the app's Info.plist's NSSupportsLiveActivities).
//   - Android: an ongoing notification, which Android 16 promotes to a
//     Live Update: at the top of the shade and the lock screen, and a
//     chip in the status bar with Value. Before Android 16, an ongoing
//     notification of the same.
//
// The desktops and the web have none: Enabled is false there, and Start
// fails with ErrNotAvailable.
//
//	a, err := liveactivity.Start(liveactivity.Content{
//		Title:    "Order #1042",
//		Subtitle: "Ana is picking it up",
//		Value:    "12 min",
//		Symbol:   "bag.fill",
//		Until:    eta,
//		URL:      "myapp://orders/1042",
//	}, liveactivity.Options{})
//	...
//	a.Update(liveactivity.Content{Title: "Order #1042", Subtitle: "Ana is 2 stops away", Value: "4 min", Until: eta})
//	...
//	a.End(liveactivity.Content{Title: "Order #1042", Subtitle: "Delivered"}, liveactivity.Dismiss{})
//
// An activity shows its Content with the platform's template, which
// leaves out what's empty: the title and the subtitle, the value (large,
// at the side; the Dynamic Island's compact trailing view; Android's
// status bar chip), the symbol (in the tint), a progress bar and a
// countdown that runs without the app. Tapping it opens the app, with its
// URL for ui.OnOpenURL.
//
// An activity lasts until the app ends it, the user dismisses it, or the
// system ends it (iOS: after 8 hours; the user can turn Live Activities
// off for the app in Settings). Observe says when one ends, and gives the
// push tokens a server updates them with (see Options.Push).
//
// # Updates from a server
//
// With Options.Push, a server updates an activity, and ends it, while the
// app isn't running. Observe gives the activity's token (PushToken): send
// it and the activity's ID to the server. Its message carries the Content
// as JSON (Content's MarshalJSON, from Go):
//
//   - iOS: an APNs push to the token, of the liveactivity type (the
//     apns-push-type header; apns-topic: the app's ID with
//     ".push-type.liveactivity"), with
//     {"aps": {"timestamp": <seconds since 1970>, "event": "update", "content-state": <Content>}}
//     ("event": "end" ends it, "dismissal-date" when it goes). Its
//     "alert" ({"title", "body"}) lights the screen up, as Update's
//     Alert does.
//   - Android: a Firebase Cloud Messaging data message to the token (the
//     app's, as push.Register's), with the data
//     {"asuka.liveActivity": "update", "asuka.id": <the ID>, "asuka.content": <Content as a string>}
//     ("end" ends it; "asuka.alertTitle" and "asuka.alertBody" alert).
//
// A server can start one too: iOS 17.2's push-to-start token
// (PushToStartToken; "event": "start", with "attributes-type":
// "AsukaActivityAttributes" and "attributes": {"id": <a new ID>}), or on
// Android a data message with "asuka.liveActivity": "start". Observe then
// gives Started.
//
// The app needs package push for that (import it): iOS's Push
// Notifications capability, and Android's Firebase.
package liveactivity

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/ui"
)

// Content is what an activity shows. Each field is optional; the template
// leaves out what's empty.
type Content struct {
	// Title is its heading: "Order #1042".
	Title string
	// Subtitle is a line under it: "Ana is 2 stops away".
	Subtitle string
	// Value is its figure: "4 min", "2–1", "$12.40". Large at the side; the
	// Dynamic Island's compact view; Android 16's status bar chip (keep it
	// short: it's cut to fit).
	Value string
	// Symbol is an SF Symbol name, as ui.Symbol takes it (Android: Asuka's
	// Material set for the same names), in the tint.
	Symbol ui.Icon
	// Progress, with ShowProgress, is a bar, from 0 to 1.
	Progress     float64
	ShowProgress bool
	// Until counts down to a time, live, without the app ("12:34"), and
	// then up from it. With Since and ShowProgress, the bar fills from
	// Since to Until by itself instead of standing at Progress (Android:
	// a minute at a time).
	Since, Until time.Time
	// Tint colors the symbol, the bar and the value (colors.RGB,
	// colors.Hex; default: the app's accent).
	Tint ui.Color
	// URL is what tapping it opens the app with (one of its scheme's).
	URL string
}

// wire is Content as the hosts (and a server's push) take it: times in
// ms since 1970, the tint as #rrggbbaa.
type wire struct {
	Title        string  `json:"title,omitempty"`
	Subtitle     string  `json:"subtitle,omitempty"`
	Value        string  `json:"value,omitempty"`
	Symbol       string  `json:"symbol,omitempty"`
	Progress     float64 `json:"progress,omitempty"`
	ShowProgress bool    `json:"showProgress,omitempty"`
	SinceMs      int64   `json:"sinceMs,omitempty"`
	UntilMs      int64   `json:"untilMs,omitempty"`
	Tint         string  `json:"tint,omitempty"`
	URL          string  `json:"url,omitempty"`
}

// MarshalJSON is c as the hosts take it, and a server's push carries it
// (the package's doc): {"title", "subtitle", "value", "symbol",
// "progress", "showProgress", "sinceMs", "untilMs", "tint", "url"}, the
// times in milliseconds since 1970, the tint as "#rrggbbaa", what's empty
// left out.
func (c Content) MarshalJSON() ([]byte, error) {
	w := wire{Title: c.Title, Subtitle: c.Subtitle, Value: c.Value, Symbol: string(c.Symbol),
		Progress: c.Progress, ShowProgress: c.ShowProgress, URL: c.URL}
	if !c.Since.IsZero() {
		w.SinceMs = c.Since.UnixMilli()
	}
	if !c.Until.IsZero() {
		w.UntilMs = c.Until.UnixMilli()
	}
	if r, g, b, a, ok := c.Tint.RGBA(); ok {
		w.Tint = fmt.Sprintf("#%02x%02x%02x%02x", r, g, b, a)
	}
	return json.Marshal(w)
}

// UnmarshalJSON reads what MarshalJSON writes.
func (c *Content) UnmarshalJSON(b []byte) error {
	var w wire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*c = Content{Title: w.Title, Subtitle: w.Subtitle, Value: w.Value, Symbol: ui.Icon(w.Symbol),
		Progress: w.Progress, ShowProgress: w.ShowProgress, URL: w.URL}
	if w.SinceMs != 0 {
		c.Since = time.UnixMilli(w.SinceMs)
	}
	if w.UntilMs != 0 {
		c.Until = time.UnixMilli(w.UntilMs)
	}
	if t, ok := colors.ParseHex(w.Tint); ok {
		c.Tint = t
	}
	return nil
}

// Options are how an activity starts.
type Options struct {
	// Push asks for the activity's push token (Observe's PushToken), so a
	// server can update it (the package's doc). The app needs package
	// push for it.
	Push bool
	// Relevance orders the app's activities when several run: the
	// Dynamic Island shows the most relevant (iOS's relevance score;
	// Android sorts the notifications by it). Default 0.
	Relevance float64
}

// Alert is what an update says as it lights the screen up and plays the
// notification sound: iOS's alert (an Apple Watch shows its title and
// body; the Dynamic Island expands), Android's notification alerting
// again (its title and body shown with it).
type Alert struct {
	Title, Body string
}

// Dismiss says when an ended activity goes.
type Dismiss struct {
	// Now takes it away at once. Otherwise it stays, showing its last
	// content, until At, or until the system takes it (iOS: within 4
	// hours; Android: the user swipes it away, 4 hours at most).
	Now bool
	// At is when it goes (at most 4 hours from now).
	At time.Time
}

// Activity is a live activity the app started (or a server did).
type Activity struct {
	// ID names it: the server's updates name it so on Android.
	ID string
}

// ErrNotAvailable: the platform has no live activities (the desktops,
// the web, iOS before 16.2), or the user turned them off for the app.
var ErrNotAvailable = errors.New("liveactivity: not available")

// The module's name.
const module = "liveActivity"

func newID() string {
	var b [8]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// call is ui.CallWait, its error ErrNotAvailable when the host has no
// live activities.
func call(method string, args []byte) ([]byte, error) {
	b, err := ui.CallWait(module, method, args)
	if err != nil && (strings.HasPrefix(err.Error(), "not available") || strings.HasPrefix(err.Error(), "no module")) {
		return nil, ErrNotAvailable
	}
	return b, err
}

// Enabled reports whether the app can start a live activity: the
// platform has them and the user allows them (iOS: Settings' Live
// Activities for the app; Android: its notifications). Not on the main
// thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Enabled() bool {
	b, err := call("enabled", nil)
	return err == nil && len(b) > 0 && b[0] == 1
}

// Start starts an activity showing c. Start it while the app is on
// screen, as the user does what it follows: iOS doesn't let a
// backgrounded app start one, nor does Android 12 a foreground service's
// worth of work. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Start(c Content, o Options) (*Activity, error) {
	a := &Activity{ID: newID()}
	b, _ := json.Marshal(struct {
		ID        string  `json:"id"`
		Content   Content `json:"content"`
		Push      bool    `json:"push,omitempty"`
		Relevance float64 `json:"relevance,omitempty"`
	}{a.ID, c, o.Push, o.Relevance})
	if _, err := call("start", b); err != nil {
		return nil, err
	}
	return a, nil
}

// Update shows c instead; with an alert, it says so (Alert). An activity
// that ended takes no more. Main thread.
func (a *Activity) Update(c Content, alert ...Alert) {
	m := map[string]any{"id": a.ID, "content": c}
	if len(alert) > 0 {
		m["alert"] = map[string]string{"title": alert[0].Title, "body": alert[0].Body}
	}
	b, _ := json.Marshal(m)
	ui.Call(module, "update", b, nil)
}

// End ends the activity, showing final, and takes it away as d says. Main
// thread.
func (a *Activity) End(final Content, d Dismiss) {
	m := map[string]any{"id": a.ID, "content": final}
	switch {
	case d.Now:
		m["dismiss"] = "now"
	case !d.At.IsZero():
		m["dismiss"] = "at"
		m["atMs"] = d.At.UnixMilli()
	}
	b, _ := json.Marshal(m)
	ui.Call(module, "end", b, nil)
}

// Running are the app's activities that are running, with what each
// shows: those it started before it was last stopped, and a server's.
// Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Running() ([]*Activity, []Content, error) {
	b, err := call("running", nil)
	if err == ErrNotAvailable {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var rs []struct {
		ID      string  `json:"id"`
		Content Content `json:"content"`
	}
	if err := json.Unmarshal(b, &rs); err != nil {
		return nil, nil, err
	}
	as, cs := make([]*Activity, len(rs)), make([]Content, len(rs))
	for i, r := range rs {
		as[i], cs[i] = &Activity{ID: r.ID}, r.Content
	}
	return as, cs, nil
}

// EventType is what happened to an activity.
type EventType uint8

const (
	// Started: a server started it (a push).
	Started EventType = iota
	// Ended: the user dismissed it, the system ended it, a server's push
	// ended it, or the app did (End).
	Ended
	// PushToken: its push token, which the server sends its updates to
	// (Options.Push). It may change while the activity runs: send each.
	PushToken
	// PushToStartToken: the token a server starts activities with, the
	// app's (iOS 17.2; Android: Firebase's, as push.Register's). Activity
	// is nil.
	PushToStartToken
)

func (t EventType) String() string {
	return [...]string{"started", "ended", "pushToken", "pushToStartToken", "?"}[min(int(t), 4)]
}

// Event is something that happened to the app's activities.
type Event struct {
	Type     EventType
	Activity *Activity
	// Token is a PushToken's or a PushToStartToken's: hex on iOS (APNs'),
	// Firebase's on Android.
	Token string
}

var (
	mu        sync.Mutex
	observers = map[*func(Event)]bool{}
	polling   bool
)

// Observe calls f, on the main thread, with what happens to the app's
// activities (Event), until stop. What happened before is kept for the
// first observer: an activity's token, an end while the app was away.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Observe(f func(Event)) (stop func()) {
	fp := &f
	mu.Lock()
	observers[fp] = true
	poll := !polling
	polling = true
	mu.Unlock()
	if poll {
		go follow()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			delete(observers, fp)
			last := len(observers) == 0
			mu.Unlock()
			if last {
				ui.Post(func() { ui.Call(module, "stop", nil, nil) })
			}
		})
	}
}

// eventWire is an Event as the host sends it.
type eventWire struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Token string `json:"token"`
}

func (w eventWire) event() (Event, bool) {
	var e Event
	switch w.Type {
	case "started":
		e.Type = Started
	case "ended":
		e.Type = Ended
	case "pushToken":
		e.Type = PushToken
	case "pushToStartToken":
		e.Type = PushToStartToken
	default:
		return e, false
	}
	if w.ID != "" {
		e.Activity = &Activity{ID: w.ID}
	}
	e.Token = w.Token
	return e, true
}

// follow asks the host for each event ("next" answers with one), while
// someone observes.
func follow() {
	for {
		b, err := ui.CallWait(module, "next", nil)
		mu.Lock()
		fs := make([]func(Event), 0, len(observers))
		for f := range observers {
			fs = append(fs, *f)
		}
		// "stopped": the last observer went as another came: ask again.
		if len(fs) == 0 || err != nil && err.Error() != "stopped" {
			polling = false
			mu.Unlock()
			return
		}
		mu.Unlock()
		if err != nil {
			continue
		}
		var w eventWire
		if json.Unmarshal(b, &w) != nil {
			continue
		}
		e, ok := w.event()
		if !ok {
			continue
		}
		ui.Post(func() {
			for _, f := range fs {
				f(e)
			}
		})
	}
}
