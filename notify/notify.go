// Package notify is local notifications: shown at a time the app sets,
// whether or not it's running then; tapping one opens the app with its
// URL (ui.OnOpenURL gets it). The user allows them first (Request). An
// app that imports it is given the permission it needs on Android.
package notify

import (
	"encoding/json"
	"time"

	"github.com/egoist/nui/ui"
)

// Notification is what to show, and when.
type Notification struct {
	// ID names it, to replace or Cancel it: scheduling one with an ID
	// that's waiting replaces that one.
	ID    string
	Title string
	Body  string
	// URL is what the app is opened with when the user taps it (one of
	// its scheme's, nui.json's scheme): ui.OnOpenURL gets it.
	URL string
	// After is how long from now it shows (at least a second).
	After time.Duration
}

// Request asks the user to allow notifications (the first time; after,
// it's their answer then, or what they've set in Settings since), and
// reports whether they're allowed. Not on the main thread.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func Request() (allowed bool, err error) {
	b, err := ui.CallWait("notify", "request", nil)
	return err == nil && len(b) > 0 && b[0] == 1, err
}

// Schedule sets n to show after n.After. Not on the main thread.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func Schedule(n Notification) error {
	after := max(n.After, time.Second)
	b, _ := json.Marshal(map[string]any{"id": n.ID, "title": n.Title, "body": n.Body, "url": n.URL, "seconds": after.Seconds()})
	_, err := ui.CallWait("notify", "schedule", b)
	return err
}

// Cancel takes away the notification id: waiting, or shown. Main thread.
func Cancel(id string) {
	ui.Call("notify", "cancel", []byte(id), nil)
}
