//go:build windows

package windows

import (
	"encoding/binary"

	. "github.com/egoist/asuka/internal/windows"
)

// asuka/power on Windows: the battery and the power source
// (GetSystemPowerStatus; Low Power is battery saver, and Windows 11's
// energy saver, which it reports the same way; Windows has no thermal
// state for apps: nominal), read again as WM_POWERBROADCAST says they
// changed (PBT_APMPOWERSTATUSCHANGE, and PBT_POWERSETTINGCHANGE for the
// power source, the level and the savers). Sleep and waking are
// PBT_APMSUSPEND and PBT_APMRESUMEAUTOMATIC (or PBT_APMRESUMESUSPEND
// alone), which RegisterSuspendResumeNotification sends on PCs with
// modern standby too; locking is the session's WM_WTSSESSION_CHANGE
// (WTSRegisterSessionNotification). Sleeping and waking are reported
// once each, however many of the messages come. The idle time is
// GetLastInputInfo's, the session's last input.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		p := power.methods()
		p["idle"] = func(_ []byte, reply func([]byte, error)) {
			ms, ok := IdleMillis()
			if !ok {
				reply(nil, nil)
				return
			}
			reply(binary.LittleEndian.AppendUint64(nil, uint64(ms)), nil)
		}
		m["power"] = p
	})
	power.start = watchPower
}

var power = watched{read: readPower}

// powerAsleep: suspending was reported, waking not yet. Both the
// broadcast and RegisterSuspendResumeNotification's message can come.
var powerAsleep bool

// asuka/power's events.
const (
	powerSuspend = 1
	powerResume  = 2
	powerLock    = 3
	powerUnlock  = 4
)

func watchPower() {
	onSystemMessage(func(m uint32, wp, _ uintptr) {
		switch m {
		case WM_POWERBROADCAST:
			powerBroadcast(wp)
		case WM_WTSSESSION_CHANGE:
			sessionChange(wp)
		}
	})
}

func powerBroadcast(event uintptr) {
	switch event {
	case PBT_APMPOWERSTATUSCHANGE, PBT_POWERSETTINGCHANGE:
		power.changed()
	case PBT_APMSUSPEND:
		if !powerAsleep {
			powerAsleep = true
			power.emit(powerSuspend)
		}
	case PBT_APMRESUMEAUTOMATIC, PBT_APMRESUMESUSPEND:
		if powerAsleep {
			powerAsleep = false
			power.emit(powerResume)
			power.changed() // unplugged while asleep
		}
	}
}

func sessionChange(event uintptr) {
	switch event {
	case WTS_SESSION_LOCK:
		power.emit(powerLock)
	case WTS_SESSION_UNLOCK:
		power.emit(powerUnlock)
	}
}

// readPower is asuka/power's state: u8 battery, on battery, level (255:
// not known), charging, low power, thermal.
func readPower() []byte {
	s, ok := SystemPowerStatus()
	if !ok {
		return []byte{0, 0, 255, 0, 0, 0}
	}
	return powerState(s)
}

func powerState(s PowerStatus) []byte {
	battery := s.BatteryFlag != 128 && s.BatteryFlag != 255
	level := byte(255)
	if battery && s.BatteryPercent <= 100 {
		level = s.BatteryPercent
	}
	return []byte{
		b2u(battery),
		b2u(battery && s.ACLine == 0),
		level,
		b2u(battery && s.BatteryFlag&8 != 0),
		b2u(s.SystemStatus&1 != 0),
		0,
	}
}
