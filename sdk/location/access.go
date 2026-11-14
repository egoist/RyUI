package location

import (
	"errors"

	followpkg "github.com/egoist/asuka/internal/follow"
	"github.com/egoist/asuka/internal/locationspec"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Level is how much the user lets the app know where the device is.
type Level uint8

const (
	// NotAsked: the app hasn't asked yet.
	NotAsked Level = iota
	// Denied: the user said no (or turned it off in Settings). Only
	// Settings can change it: OpenSettings takes the user there.
	Denied
	// Restricted: the user can't allow it (parental controls, a device
	// policy). iOS's and macOS's.
	Restricted
	// WhenInUse: while the app is in use, on screen, or with updates it
	// started there that go on in the background (iOS's background
	// activity session, Android's foreground service).
	WhenInUse
	// Always: also with the app in the background, which the regions and
	// the significant changes need, and to start the app for their
	// events. A desktop app, which runs out of sight, has it once
	// allowed.
	Always
)

func (l Level) String() string {
	switch l {
	case NotAsked:
		return "not asked"
	case Denied:
		return "denied"
	case Restricted:
		return "restricted"
	case WhenInUse:
		return "when in use"
	case Always:
		return "always"
	}
	return "unknown"
}

// Allowed reports whether the app may know where the device is, now.
func (l Level) Allowed() bool { return l == WhenInUse || l == Always }

// Authorization is what the user allowed.
type Authorization struct {
	Level Level
	// Precise: positions to a few meters. false: approximate ones, within
	// a few kilometers: the user turned iOS's Precise Location off, or
	// chose Android's Approximate.
	Precise bool
	// Off: location services are off for the whole device (Settings,
	// Privacy, Location); the app gets no position whatever its Level.
	Off bool
}

func decodeAccess(b []byte) (Authorization, error) {
	if len(b) < 3 {
		return Authorization{}, errors.New("location: bad authorization")
	}
	return Authorization{Level: Level(b[0]), Precise: b[1] == 1, Off: b[2] == 1}, nil
}

func access(b []byte, err error) (Authorization, error) {
	if err != nil {
		return Authorization{}, err
	}
	return decodeAccess(b)
}

// Authorized is what the user has allowed, without asking. Not on the
// main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Authorized() (Authorization, error) {
	return access(ui.CallWait("locationAccess", "current", nil))
}

// Request asks for the position while the app is in use, the first time
// (Current and Watch ask the same), and returns what the user allowed:
// on Android they choose Precise or Approximate as they answer, on iOS
// they can turn Precise Location off. Asking again after Denied doesn't
// show the question: only Settings changes it. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Request() (Authorization, error) {
	return access(ui.CallWait("locationAccess", "request", []byte{locationspec.WhenInUse}))
}

// RequestAlways asks for the position with the app in the background
// too, which regions (Monitor) and significant changes need. It asks for
// WhenInUse first if the user hasn't allowed it. iOS shows its question
// once (the user can also be offered Always later by the system, which
// asks them whether to keep it); Android 11 and later take the user to
// the app's location settings, to choose "Allow all the time". It
// returns what the user allowed. Not on the main thread.
//
// An app that calls it is given what Always needs (asuka.json's
// ios.usage.NSLocationAlwaysAndWhenInUseUsageDescription says why;
// Android's ACCESS_BACKGROUND_LOCATION, which Google Play asks about).
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func RequestAlways() (Authorization, error) {
	return access(ui.CallWait("locationAccess", "request", []byte{locationspec.Always}))
}

// RequestPrecise asks the user who allowed only approximate positions
// for precise ones. On iOS it's for now only (temporary full accuracy,
// until the app leaves the foreground), and purpose is the key of the
// reason shown, one of asuka.json's ios.locationPurposes ("default" is
// there for you, "To show exactly where you are."). On Android the user
// is asked to change Approximate to Precise for good, and purpose isn't
// used. It returns what the user allowed. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func RequestPrecise(purpose string) (Authorization, error) {
	if purpose == "" {
		purpose = "default"
	}
	return access(ui.CallWait("locationAccess", "precise", []byte(purpose)))
}

var accessModule = followpkg.Module{Name: "locationAccess"}

// WatchAuthorization calls f, on the main thread, with what the user
// has allowed now and again each time it changes (in Settings, or as
// they answer), until stop.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func WatchAuthorization(f func(Authorization)) (stop func()) {
	return accessModule.Watch(func(b []byte) {
		if a, err := decodeAccess(b); err == nil {
			f(a)
		}
	})
}

// UseAuthorization is the authorization as a signal, for a component:
// it follows changes while the component lives. Main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func UseAuthorization() signal.Accessor[Authorization] {
	s := signal.New(Authorization{})
	signal.OnCleanup(WatchAuthorization(s.Set))
	return s
}

// OpenSettings opens the system's location settings for the app, where
// the user changes what they allowed (iOS: the app's page in Settings;
// Android: the app's location permission; macOS, Windows: the privacy
// settings' Location Services; Linux: GNOME's Location settings). Not on
// the web. Main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func OpenSettings() {
	ui.Call("locationAccess", "settings", nil, nil)
}
