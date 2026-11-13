//go:build linux && !android

package linux

import (
	"encoding/binary"
	"math"
	"os"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sdk/power on Linux, through the system's services over D-Bus:
// UPower for the battery (its OnBattery, and the display device: the
// batteries as one, its percentage and whether it charges), GIO's power
// profile monitor for the power saver profile (power-profiles-daemon,
// through the portal in a sandbox), logind for sleep (PrepareForSleep)
// and the session's lock (LockedHint), and the screen saver's
// ActiveChanged (GNOME's, KDE's), which comes as the screen locks. Linux
// has no thermal state for apps: nominal. The idle time is GNOME's idle
// monitor's (Mutter), else the screen saver's (KDE), else not known.
//
// The state is u8 battery, on battery, level (255 not known), charging,
// low power, thermal; events 1 suspend, 2 resume, 3 lock, 4 unlock.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		ms := powerw.w.methods(h.startPower)
		ms["idle"] = h.idleTime
		m["power"] = ms
	})
}

type powerWatch struct {
	w       watched
	started bool
	reading int  // UPower reads in progress
	again   bool // read again once they're done

	onBattery, present bool
	percentage         float64
	charging           bool
	saver              bool
	profiles           Ptr // GPowerProfileMonitor

	locked, lockKnown bool
}

var powerw powerWatch

const (
	upowerDest    = "org.freedesktop.UPower"
	upowerPath    = "/org/freedesktop/UPower"
	upowerDisplay = "/org/freedesktop/UPower/devices/DisplayDevice"
	login1Dest    = "org.freedesktop.login1"
	login1Path    = "/org/freedesktop/login1"
	propsIface    = "org.freedesktop.DBus.Properties"
)

// startPower follows the power's sources, once.
func (h *Host) startPower() error {
	p := &powerw
	if p.started {
		return nil
	}
	p.started = true
	if Desk.PowerProfileMonitorDup != nil {
		p.profiles = Desk.PowerProfileMonitorDup()
		if p.profiles != 0 {
			p.saver = Desk.PowerProfileSaverOn(p.profiles)
			Connect(p.profiles, "notify::power-saver-enabled", func(uintptr) {
				p.saver = Desk.PowerProfileSaverOn(p.profiles)
				h.powerChanged()
			})
		}
	}
	if conn, err := Bus(BusSystem); err == nil {
		for _, path := range []string{upowerPath, upowerDisplay} {
			DBusSubscribe(conn, upowerDest, propsIface, "PropertiesChanged", path, func(DBusSignal) { h.readUPower() })
		}
		DBusSubscribe(conn, login1Dest, "org.freedesktop.login1.Manager", "PrepareForSleep", login1Path, func(s DBusSignal) {
			if a, _ := VariantGo(s.Params).([]any); len(a) > 0 {
				if sleeping, _ := a[0].(bool); sleeping {
					p.w.emit(1)
				} else {
					p.w.emit(2)
				}
			}
		})
		h.followSession(conn)
		h.readUPower()
	} else {
		h.powerChanged()
	}
	if conn, err := Bus(BusSession); err == nil {
		for _, iface := range []string{"org.gnome.ScreenSaver", "org.freedesktop.ScreenSaver"} {
			DBusSubscribe(conn, "", iface, "ActiveChanged", "", func(s DBusSignal) {
				if a, _ := VariantGo(s.Params).([]any); len(a) > 0 {
					on, _ := a[0].(bool)
					h.setLocked(on)
				}
			})
		}
	}
	return nil
}

// followSession finds the app's logind session and follows its
// LockedHint.
func (h *Host) followSession(conn Ptr) {
	follow := func(path string) {
		DBusSubscribe(conn, login1Dest, propsIface, "PropertiesChanged", path, func(s DBusSignal) {
			a, _ := VariantGo(s.Params).([]any)
			if len(a) < 3 {
				return
			}
			if changed, _ := a[1].(map[string]any); changed != nil {
				if v, ok := changed["LockedHint"].(bool); ok {
					h.setLocked(v)
					return
				}
			}
			if inv, _ := a[2].([]any); len(inv) > 0 {
				for _, k := range inv {
					if k == "LockedHint" {
						h.readLockedHint(conn, path)
					}
				}
			}
		})
		h.readLockedHint(conn, path)
	}
	DBusCall(conn, login1Dest, login1Path, "org.freedesktop.login1.Manager", "GetSessionByPID", VTuple(VU32(uint32(os.Getpid()))), 2000, func(r Ptr, err error) {
		if err == nil {
			if a, _ := VariantGo(r).([]any); len(a) > 0 {
				if path, _ := a[0].(string); path != "" {
					follow(path)
					return
				}
			}
		}
		// A process outside the session (a user service): the session
		// the environment names.
		if id := os.Getenv("XDG_SESSION_ID"); id != "" {
			DBusCall(conn, login1Dest, login1Path, "org.freedesktop.login1.Manager", "GetSession", VTuple(VStr(id)), 2000, func(r Ptr, err error) {
				if err != nil {
					return
				}
				if a, _ := VariantGo(r).([]any); len(a) > 0 {
					if path, _ := a[0].(string); path != "" {
						follow(path)
					}
				}
			})
		}
	})
}

func (h *Host) readLockedHint(conn Ptr, path string) {
	DBusCall(conn, login1Dest, path, propsIface, "Get", VTuple(VStr("org.freedesktop.login1.Session"), VStr("LockedHint")), 2000, func(r Ptr, err error) {
		if err != nil {
			return
		}
		if a, _ := VariantGo(r).([]any); len(a) > 0 {
			if v, ok := a[0].(bool); ok {
				h.setLocked(v)
			}
		}
	})
}

// setLocked notes the screen locked or unlocked: an event when it
// changes (the first state known isn't one).
func (h *Host) setLocked(on bool) {
	p := &powerw
	if p.lockKnown && p.locked == on {
		return
	}
	known := p.lockKnown
	p.locked, p.lockKnown = on, true
	if !known {
		return
	}
	if on {
		p.w.emit(3)
	} else {
		p.w.emit(4)
	}
}

// readUPower reads UPower's OnBattery and its display device.
func (h *Host) readUPower() {
	p := &powerw
	if p.reading > 0 {
		p.again = true
		return
	}
	conn, err := Bus(BusSystem)
	if err != nil {
		h.powerChanged()
		return
	}
	p.reading = 2
	done := func() {
		p.reading--
		if p.reading > 0 {
			return
		}
		if p.again {
			p.again = false
			h.readUPower()
			return
		}
		h.powerChanged()
	}
	getAll := func(path, iface string, f func(map[string]any)) {
		DBusCall(conn, upowerDest, path, propsIface, "GetAll", VTuple(VStr(iface)), 2000, func(r Ptr, err error) {
			var m map[string]any
			if err == nil {
				if a, _ := VariantGo(r).([]any); len(a) > 0 {
					m, _ = a[0].(map[string]any)
				}
			}
			f(m)
			done()
		})
	}
	getAll(upowerPath, "org.freedesktop.UPower", func(m map[string]any) {
		p.onBattery, _ = m["OnBattery"].(bool)
	})
	getAll(upowerDisplay, "org.freedesktop.UPower.Device", func(m map[string]any) {
		p.present, _ = m["IsPresent"].(bool)
		// Type: 2 battery, 3 UPS; the display device of a desktop
		// without one isn't present.
		if t, _ := m["Type"].(uint32); t != 2 && t != 3 {
			p.present = false
		}
		p.percentage, _ = m["Percentage"].(float64)
		st, _ := m["State"].(uint32)
		p.charging = st == 1 || st == 5 // charging, pending charge
	})
}

// powerState is the module's state now.
func powerState() []byte {
	p := &powerw
	s := []byte{0, 0, 255, 0, b2u(p.saver), 0}
	if p.present {
		s[0] = 1
		s[1] = b2u(p.onBattery)
		s[2] = byte(math.Round(min(max(p.percentage, 0), 100)))
		s[3] = b2u(p.charging)
	}
	return s
}

func (h *Host) powerChanged() {
	s := powerState()
	if string(s) != string(powerw.w.state) {
		powerw.w.set(s)
	}
}

// idleTime is "idle": u64 milliseconds since the user's last input, from
// GNOME's idle monitor or the screen saver (KDE); nothing where neither
// answers.
func (h *Host) idleTime(_ []byte, reply func([]byte, error)) {
	conn, err := Bus(BusSession)
	if err != nil {
		reply(nil, nil)
		return
	}
	ms := func(v uint64) []byte { return binary.LittleEndian.AppendUint64(nil, v) }
	DBusCall(conn, "org.gnome.Mutter.IdleMonitor", "/org/gnome/Mutter/IdleMonitor/Core", "org.gnome.Mutter.IdleMonitor", "GetIdletime", 0, 2000, func(r Ptr, err error) {
		if err == nil {
			if a, _ := VariantGo(r).([]any); len(a) > 0 {
				if v, ok := a[0].(uint64); ok {
					reply(ms(v), nil)
					return
				}
			}
		}
		DBusCall(conn, "org.freedesktop.ScreenSaver", "/org/freedesktop/ScreenSaver", "org.freedesktop.ScreenSaver", "GetSessionIdleTime", 0, 2000, func(r Ptr, err error) {
			if err == nil {
				if a, _ := VariantGo(r).([]any); len(a) > 0 {
					if v, ok := a[0].(uint32); ok {
						reply(ms(uint64(v)*1000), nil)
						return
					}
				}
			}
			reply(nil, nil)
		})
	})
}
