// Package tracking asks the user to let the app track them across other
// companies' apps and websites (expo-tracking-transparency): iOS's App
// Tracking Transparency prompt, which an app must show before it reads
// the advertising identifier (IDFA) or shares data with ad networks and
// data brokers. An app that imports it is given the prompt's purpose
// string (NSUserTrackingUsageDescription; say yours in ryui.json's
// ios.usage).
//
// Android has no such prompt: the user turns personalized ads off in
// Settings, and Status there is Allowed.
package tracking

import "github.com/egoist/ryui/ui"

// Status is the user's answer.
type Status uint8

const (
	// NotAsked: the prompt hasn't been shown.
	NotAsked Status = iota
	// Restricted: the device doesn't let the user allow it (a child's
	// account, a managed device): as Denied.
	Restricted
	// Denied: the user said no, or turned tracking off for every app.
	Denied
	// Allowed: the user said yes (Android: always).
	Allowed
)

func (s Status) String() string {
	return [...]string{"not asked", "restricted", "denied", "allowed"}[min(int(s), 3)]
}

// Request shows the prompt, the first time (after, it's the user's
// answer then, or what they've set in Settings since), and reports the
// answer. iOS shows it only while the app is active: ask once the app's
// on screen, not at launch. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Request() (Status, error) {
	return decode(ui.CallWait("tracking", "request", nil))
}

// Current is the answer now, without asking. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Current() (Status, error) {
	return decode(ui.CallWait("tracking", "status", nil))
}

func decode(b []byte, err error) (Status, error) {
	if err != nil || len(b) == 0 {
		return NotAsked, err
	}
	return Status(min(b[0], 3)), nil
}
