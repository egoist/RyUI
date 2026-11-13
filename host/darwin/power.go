//go:build darwin && !ios

package darwin

import (
	"encoding/binary"

	"github.com/ebitengine/purego"

	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/sdk/power on a Mac. The battery is IOKit's power sources
// (IOPSCopyPowerSourcesInfo: the internal battery's capacity and
// whether it charges; the source the Mac runs on), followed through
// IOPSNotificationCreateRunLoopSource; Low Power Mode and the thermal
// state are NSProcessInfo's, and their notifications. Sleep and wake are
// NSWorkspace's (NSWorkspaceWillSleepNotification, DidWake), the lock
// screen the distributed notifications loginwindow posts
// (com.apple.screenIsLocked, com.apple.screenIsUnlocked), the idle time
// Quartz's (CGEventSourceSecondsSinceLastEventType, any input, the
// session's).

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		ms := powerWatch.methods()
		ms["idle"] = func(_ []byte, reply func([]byte, error)) {
			s, ok := idleSeconds()
			if !ok {
				reply(nil, nil)
				return
			}
			reply(binary.LittleEndian.AppendUint64(nil, uint64(s*1000)), nil)
		}
		m["power"] = ms
	})
}

// The events, asuka/sdk/power's Event values.
const (
	powerSuspend = 1 + iota
	powerResume
	powerLock
	powerUnlock
)

var powerWatch = watched{
	read: readPower,
	start: func(w *watched) {
		observeNamed(0, "NSProcessInfoPowerStateDidChangeNotification", w.changed)
		observeNamed(0, "NSProcessInfoThermalStateDidChangeNotification", w.changed)
		ws := workspaceCenter()
		observeNamed(ws, "NSWorkspaceWillSleepNotification", func() { w.event(powerSuspend) })
		observeNamed(ws, "NSWorkspaceDidWakeNotification", func() { w.event(powerResume) })
		dc := Send(Class("NSDistributedNotificationCenter"), "defaultCenter")
		observeNamed(dc, "com.apple.screenIsLocked", func() { w.event(powerLock) })
		observeNamed(dc, "com.apple.screenIsUnlocked", func() { w.event(powerUnlock) })
		watchPowerSources(w.changed)
	},
}

var iops struct {
	loaded       bool
	copyInfo     func() uintptr
	copyList     func(blob uintptr) uintptr
	description  func(blob, ps uintptr) uintptr
	providing    func(blob uintptr) uintptr
	notification func(callback, ctx uintptr) uintptr
	idle         func(state int32, event uint32) float64
	addSource    func(rl, src, mode uintptr)
	mainLoop     func() uintptr
	changed      func()
}

func loadIOPS() {
	p := &iops
	if p.loaded {
		return
	}
	p.loaded = true
	if lib := Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit"); lib != 0 {
		purego.RegisterLibFunc(&p.copyInfo, lib, "IOPSCopyPowerSourcesInfo")
		purego.RegisterLibFunc(&p.copyList, lib, "IOPSCopyPowerSourcesList")
		purego.RegisterLibFunc(&p.description, lib, "IOPSGetPowerSourceDescription")
		purego.RegisterLibFunc(&p.providing, lib, "IOPSGetProvidingPowerSourceType")
		purego.RegisterLibFunc(&p.notification, lib, "IOPSNotificationCreateRunLoopSource")
	}
	purego.RegisterLibFunc(&p.idle, CoreGraphics(), "CGEventSourceSecondsSinceLastEventType")
	purego.RegisterLibFunc(&p.addSource, CoreFoundation(), "CFRunLoopAddSource")
	purego.RegisterLibFunc(&p.mainLoop, CoreFoundation(), "CFRunLoopGetMain")
}

var powerSourcesChanged = purego.NewCallback(func(ctx uintptr) {
	if f := iops.changed; f != nil {
		WithPool(f)
	}
})

// watchPowerSources calls f, on the main thread, as a power source
// changes (plugged in, its capacity, charging).
func watchPowerSources(f func()) {
	loadIOPS()
	p := &iops
	if p.notification == nil || p.changed != nil {
		return
	}
	p.changed = f
	src := p.notification(powerSourcesChanged, 0)
	if src == 0 {
		return
	}
	mode := Const(CoreFoundation(), "kCFRunLoopCommonModes")
	p.addSource(p.mainLoop(), src, uintptr(mode))
}

// readPower is the state: u8 battery, u8 on battery, u8 level (255: not
// known), u8 charging, u8 low power, u8 thermal.
func readPower() []byte {
	loadIOPS()
	s := []byte{0, 0, 255, 0, 0, 0}
	pi := Send(Class("NSProcessInfo"), "processInfo")
	if RespondsTo(pi, "isLowPowerModeEnabled") {
		s[4] = b2u(SendBool(pi, "isLowPowerModeEnabled"))
	}
	if RespondsTo(pi, "thermalState") {
		s[5] = byte(min(max(SendInt(pi, "thermalState"), 0), 3))
	}
	p := &iops
	if p.copyInfo == nil {
		return s
	}
	blob := p.copyInfo()
	if blob == 0 {
		return s
	}
	defer CFRelease(blob)
	list := p.copyList(blob)
	if list == 0 {
		return s
	}
	defer CFRelease(list)
	for _, ps := range ArrayItems(ID(list)) {
		d := ID(p.description(blob, uintptr(ps)))
		if d == 0 {
			continue
		}
		get := func(k string) ID { return Send(d, "objectForKey:", uintptr(String(k))) }
		if GoString(get("Type")) != "InternalBattery" {
			continue
		}
		if present := get("Is Present"); present != 0 && !SendBool(present, "boolValue") {
			continue
		}
		s[0] = 1
		s[1] = b2u(GoString(get("Power Source State")) == "Battery Power")
		cur, max := get("Current Capacity"), get("Max Capacity")
		if cur != 0 && max != 0 {
			if m := SendInt(max, "intValue"); m > 0 {
				s[2] = byte(min(max0(SendInt(cur, "intValue"))*100/m, 100))
			}
		}
		if c := get("Is Charging"); c != 0 && SendBool(c, "boolValue") {
			s[3] = 1
		}
		break
	}
	if s[0] == 0 {
		// A UPS, or none: what the Mac runs on.
		s[1] = b2u(GoString(ID(p.providing(blob))) == "Battery Power")
	}
	return s
}

func max0(v int) int { return max(v, 0) }

// idleSeconds is the time since the user's last input (keyboard, mouse,
// trackpad, tablet) in this login session.
func idleSeconds() (float64, bool) {
	loadIOPS()
	if iops.idle == nil {
		return 0, false
	}
	const combinedSessionState = 0
	const anyInput = ^uint32(0)
	return iops.idle(combinedSessionState, anyInput), true
}
