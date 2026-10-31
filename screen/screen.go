// Package screen is what an app sets about the screen it runs on: the
// orientations it may turn to (expo-screen-orientation), the system bars
// (expo-status-bar, expo-navigation-bar), whether what it shows may be
// captured, and the screenshots the user takes (expo-screen-capture).
//
// Each setting holds until it's set again; they're the app's, not a
// screen's: a screen that sets one resets it as it goes (signal.OnCleanup).
// All are main-thread calls.
package screen

import (
	"sync"

	"github.com/egoist/ryui/ui"
)

// Orientation is what the app may turn to.
type Orientation uint8

const (
	// Any is ryui.json's orientations (default: all the device turns to):
	// unlocked.
	Any Orientation = iota
	// Portrait is upright (and upside down, where the platform allows it:
	// iPad, Android's sensor).
	Portrait
	// PortraitUp is upright only.
	PortraitUp
	// Landscape is either way on its side, as the device is held.
	Landscape
	// LandscapeLeft is on its side with the top on the left (the home
	// button, or the bottom edge, on the right).
	LandscapeLeft
	// LandscapeRight is on its side with the top on the right.
	LandscapeRight
)

// Lock turns the app to o and keeps it there, whichever way the device is
// held (a video player's landscape, a scanner's portrait); Lock(Any)
// unlocks it. An orientation ryui.json's orientations leave out stays
// out. iPad apps in Split View or Stage Manager, and Android's large
// screens (Android 16 and later), are whatever shape their window is: the
// lock doesn't hold there.
func Lock(o Orientation) {
	ui.Call("screen", "lock", []byte{byte(o)}, nil)
}

// BarStyle is the color of the status bar's text and icons.
type BarStyle uint8

const (
	// Auto follows the app's color scheme: dark icons in light mode,
	// light in dark mode.
	Auto BarStyle = iota
	// Light is light text and icons, for a dark picture or header under
	// them.
	Light
	// Dark is dark text and icons, for a light one.
	Dark
)

// Bars is how the system bars show.
type Bars struct {
	// Style is the status bar's text and icons (on Android, the
	// navigation bar's buttons too).
	Style BarStyle
	// HideStatus hides the status bar (the time, the battery): a game, a
	// photo full screen. On Android a swipe from the edge shows it a
	// moment.
	HideStatus bool
	// HideNavigation hides Android's navigation bar (its buttons or
	// gesture handle) until a swipe from the edge shows it a moment. iOS
	// has none; its home indicator stays.
	HideNavigation bool
}

// SetBars sets how the system bars show. The safe area follows: hiding a
// bar gives its room to the app (ui.UseEnvironment).
func SetBars(b Bars) {
	var flags byte
	if b.HideStatus {
		flags |= 1
	}
	if b.HideNavigation {
		flags |= 2
	}
	ui.Call("screen", "bars", []byte{byte(b.Style), flags}, nil)
}

// PreventCapture keeps what the app shows out of screenshots and
// recordings while on is true: a bank's balance, a ticket's code, paid
// video. Android: the window is secure (FLAG_SECURE): screenshots and
// recordings are black, and the recent apps show it blank. iOS has no
// way to stop a screenshot: while the screen is recorded, mirrored or on
// AirPlay, the app is covered; OnScreenshot says when one is taken.
func PreventCapture(on bool) {
	arg := []byte{0}
	if on {
		arg[0] = 1
	}
	ui.Call("screen", "preventCapture", arg, nil)
}

var (
	mu       sync.Mutex
	watchers = map[*func()]bool{}
	running  bool
)

// OnScreenshot calls f, on the main thread, each time the user takes a
// screenshot of the app, until stop: to say what's in it is private, to
// offer to share it. iOS; Android 14 and later (DETECT_SCREEN_CAPTURE,
// which an app that imports this package is given). A screenshot that
// PreventCapture kept out isn't reported.
func OnScreenshot(f func()) (stop func()) {
	fp := &f
	mu.Lock()
	watchers[fp] = true
	start := !running
	running = true
	mu.Unlock()
	if start {
		go follow()
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			delete(watchers, fp)
			last := len(watchers) == 0
			mu.Unlock()
			if last {
				ui.Post(func() { ui.Call("screen", "stopScreenshots", nil, nil) })
			}
		})
	}
}

// follow waits for each screenshot ("screenshot" answers when one's
// taken), until no one watches.
func follow() {
	for {
		_, err := ui.CallWait("screen", "screenshot", nil)
		mu.Lock()
		fs := make([]func(), 0, len(watchers))
		for f := range watchers {
			fs = append(fs, *f)
		}
		// Stopped as another began watching: wait on for that one.
		if len(fs) == 0 || err != nil && err.Error() != "stopped" {
			running = false
			mu.Unlock()
			return
		}
		mu.Unlock()
		if err != nil {
			continue
		}
		ui.Post(func() {
			for _, f := range fs {
				f()
			}
		})
	}
}
