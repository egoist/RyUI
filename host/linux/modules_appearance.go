//go:build linux && !android

package linux

import (
	"math"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sdk/appearance on Linux: the settings portal's appearance
// (org.freedesktop.appearance: accent-color, GNOME 47's and KDE's, an
// sRGB triple; contrast, 1 for high), read once and followed by its
// SettingChanged. Without the portal (or a contrast in it), high contrast
// is libadwaita's (its style manager's, which reads the portal or a
// HighContrast theme), and there's no accent. Linux desktops have no
// Reduce Transparency: always off. The host's own accent (asuka.json's)
// isn't the system's, and isn't reported.
//
// The state is u8 has accent, red, green, blue, increase contrast,
// reduce transparency.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["appearance"] = appw.w.methods(h.startAppearance)
	})
}

type appearanceWatch struct {
	w       watched
	started bool

	accent        [3]byte
	hasAccent     bool
	contrast      bool // the portal's
	contrastKnown bool // the portal says
	reading       int
}

var appw appearanceWatch

const (
	settingsIface = "org.freedesktop.portal.Settings"
	appearanceNS  = "org.freedesktop.appearance"
)

func (h *Host) startAppearance() error {
	a := &appw
	if a.started {
		return nil
	}
	a.started = true
	Connect(AdwStyleManagerGetDefault(), "notify::high-contrast", func(uintptr) { h.appearanceChanged() })
	conn, err := Bus(BusSession)
	if err != nil {
		h.appearanceChanged()
		return nil
	}
	DBusSubscribe(conn, portalDest, settingsIface, "SettingChanged", portalPath, func(s DBusSignal) {
		args, _ := VariantGo(s.Params).([]any)
		if len(args) < 3 {
			return
		}
		if ns, _ := args[0].(string); ns != appearanceNS {
			return
		}
		key, _ := args[1].(string)
		a.setting(key, args[2])
		h.appearanceChanged()
	})
	for _, key := range []string{"accent-color", "contrast"} {
		key := key
		a.reading++
		h.readSetting(conn, key, func(v any) {
			a.setting(key, v)
			a.reading--
			if a.reading == 0 {
				h.appearanceChanged()
			}
		})
	}
	return nil
}

// readSetting reads one of the portal's appearance settings (ReadOne,
// version 2; Read, which wraps the value in one more variant, before
// it): nil when it has none.
func (h *Host) readSetting(conn Ptr, key string, done func(any)) {
	params := func() Ptr { return VTuple(VStr(appearanceNS), VStr(key)) }
	value := func(r Ptr) any {
		a, _ := VariantGo(r).([]any) // variants are unwrapped: one level or two
		if len(a) == 0 {
			return nil
		}
		return a[0]
	}
	DBusCall(conn, portalDest, portalPath, settingsIface, "ReadOne", params(), 2000, func(r Ptr, err error) {
		if err == nil {
			done(value(r))
			return
		}
		DBusCall(conn, portalDest, portalPath, settingsIface, "Read", params(), 2000, func(r Ptr, err error) {
			if err != nil {
				done(nil)
				return
			}
			done(value(r))
		})
	})
}

// setting takes a setting's value (nil: none).
func (a *appearanceWatch) setting(key string, v any) {
	switch key {
	case "accent-color":
		a.hasAccent = false
		rgb, _ := v.([]any)
		if len(rgb) != 3 {
			return
		}
		var c [3]byte
		for i, x := range rgb {
			f, ok := x.(float64)
			// Out of the range: the user set none.
			if !ok || f < 0 || f > 1 || math.IsNaN(f) {
				return
			}
			c[i] = byte(math.Round(f * 255))
		}
		a.accent, a.hasAccent = c, true
	case "contrast":
		u, ok := v.(uint32)
		a.contrast, a.contrastKnown = u == 1, ok
	}
}

// appearanceState is the module's state now.
func appearanceState() []byte {
	a := &appw
	s := make([]byte, 6)
	if a.hasAccent {
		s[0], s[1], s[2], s[3] = 1, a.accent[0], a.accent[1], a.accent[2]
	}
	if a.contrastKnown {
		s[4] = b2u(a.contrast)
	} else {
		s[4] = b2u(AdwStyleManagerGetHighContrast(AdwStyleManagerGetDefault()))
	}
	return s
}

func (h *Host) appearanceChanged() {
	if appw.reading > 0 {
		return
	}
	s := appearanceState()
	if string(s) != string(appw.w.state) {
		appw.w.set(s)
	}
}
