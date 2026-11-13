// Package appearance is how the user set the system to look, beyond what
// ui.UseEnvironment carries (dark mode, Reduce Motion, the text size, a
// screen reader): the accent color the system's own controls take, and
// the accessibility settings Increase Contrast and Reduce Transparency.
//
// The host already follows them where it draws: a control in the
// system's accent (ui.Accent is it, unless asuka.json sets the app's),
// materials opaque with Reduce Transparency. An app reads them for what
// it draws itself: a chart in the accent, borders on its own cards with
// Increase Contrast, an opaque backdrop instead of a blur.
package appearance

import (
	"errors"

	"github.com/egoist/asuka/internal/follow"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Settings are the system's appearance settings now.
type Settings struct {
	// Accent is the system's accent color, as an RGB color (ui.RGB): the
	// one the user picked (macOS's Accent color, Windows's accent color,
	// GNOME's accent color through the settings portal, Android 12's
	// wallpaper color, the browser's AccentColor where it has one). 0
	// where the system has none: iOS (an app's tint is its own), Linux
	// desktops whose settings portal has none, most browsers.
	Accent ui.Color
	// IncreaseContrast: the user asked for more contrast: macOS's and
	// iOS's Increase Contrast, Windows's contrast themes, GNOME's high
	// contrast (the settings portal), Android's contrast level above
	// standard (Android 14) or high contrast text, the browser's
	// prefers-contrast: more.
	IncreaseContrast bool
	// ReduceTransparency: the user asked for opaque surfaces, not
	// blurs over what's behind: macOS's and iOS's Reduce Transparency,
	// Windows's transparency effects off, the browser's
	// prefers-reduced-transparency. Linux desktops and Android have no
	// such setting: false.
	ReduceTransparency bool
}

// settingsLen is the settings' length on the wire.
const settingsLen = 6

var errBadReply = errors.New("appearance: a bad reply")

// decode reads the host's reply: u8 has accent, u8 red, green, blue
// (sRGB), u8 increase contrast, u8 reduce transparency.
func decode(b []byte) (Settings, error) {
	if len(b) < settingsLen {
		return Settings{}, errBadReply
	}
	s := Settings{IncreaseContrast: b[4] == 1, ReduceTransparency: b[5] == 1}
	if b[0] == 1 {
		s.Accent = ui.RGB(b[1], b[2], b[3])
	}
	return s, nil
}

func encode(s Settings) []byte {
	b := make([]byte, settingsLen)
	if r, g, bl, _, ok := s.Accent.RGBA(); ok {
		b[0], b[1], b[2], b[3] = 1, r, g, bl
	}
	if s.IncreaseContrast {
		b[4] = 1
	}
	if s.ReduceTransparency {
		b[5] = 1
	}
	return b
}

// Current is the settings now. Not on the main thread.
func Current() (Settings, error) {
	b, err := ui.CallWait("appearance", "current", nil)
	if err != nil {
		return Settings{}, err
	}
	return decode(b)
}

var module = follow.Module{Name: "appearance"}

// Watch calls f, on the main thread, with the settings now and again
// each time the user changes one, until stop.
func Watch(f func(Settings)) (stop func()) {
	return module.Watch(func(b []byte) {
		if s, err := decode(b); err == nil {
			f(s)
		}
	})
}

// Use is the settings as a signal, for a component: it follows changes
// while the component lives. Main thread.
func Use() signal.Accessor[Settings] {
	s := signal.New(Settings{})
	signal.OnCleanup(Watch(s.Set))
	return s
}
