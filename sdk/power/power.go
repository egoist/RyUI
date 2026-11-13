// Package power is the device's power: whether it runs on its battery
// and how full that is, Low Power Mode and how hot it runs (expo-battery,
// Electron's powerMonitor), the computer going to sleep and waking, the
// screen locking and unlocking, and how long the user has been away from
// the keyboard and the mouse.
//
// An app pauses its sync before the computer sleeps and catches up as it
// wakes, hides what's private as the screen locks, does less on a low
// battery or in Low Power Mode, and finds the user away (a chat's
// status) from IdleTime.
//
// Keeping the display on is device.KeepAwake; keeping the app working
// out of sight is asuka/sdk/background.
package power

import (
	"encoding/binary"
	"errors"
	"time"

	"github.com/egoist/asuka/internal/follow"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Thermal is how hot the device runs: how much the system slows it to
// cool it (Apple's thermal state, Android's thermal status).
type Thermal uint8

const (
	// Nominal: no slowing, or the system doesn't say (Windows, Linux).
	Nominal Thermal = iota
	// Fair: warm; the system does some background work later.
	Fair
	// Serious: hot; the system slows the device. An app does less: a
	// lower frame rate, a smaller video.
	Serious
	// Critical: the system slows it all it can. An app does only what
	// the user is waiting for.
	Critical
)

func (t Thermal) String() string {
	return [...]string{"nominal", "fair", "serious", "critical"}[min(int(t), 3)]
}

// State is the device's power now.
type State struct {
	// Battery: the device has a battery (a laptop, a phone, a UPS a
	// desktop reports as one); false for a desktop on the mains.
	Battery bool
	// OnBattery: it runs on its battery, unplugged.
	OnBattery bool
	// Level is how full the battery is, 0 to 1; -1 without one, or where
	// the system doesn't say.
	Level float32
	// Charging: plugged in and charging (false once full).
	Charging bool
	// LowPower: the user (or the system, on a low battery) asked apps to
	// use less power: Low Power Mode on a Mac and an iPhone, Battery
	// Saver on Android, Energy Saver on Windows, the power saver profile
	// on Linux. An app refreshes less, animates less, defers work.
	LowPower bool
	// Thermal is how hot it runs.
	Thermal Thermal
}

// stateLen is the state's length on the wire.
const stateLen = 6

var errBadReply = errors.New("power: a bad reply")

// decodeState reads the host's state: u8 battery, u8 on battery, u8
// level (0 to 100; 255 not known), u8 charging, u8 low power, u8
// thermal.
func decodeState(b []byte) (State, error) {
	if len(b) < stateLen {
		return State{}, errBadReply
	}
	s := State{Battery: b[0] == 1, OnBattery: b[1] == 1, Level: -1, Charging: b[3] == 1, LowPower: b[4] == 1, Thermal: Thermal(min(b[5], 3))}
	if b[2] <= 100 {
		s.Level = float32(b[2]) / 100
	}
	return s, nil
}

func encodeState(s State) []byte {
	b2 := func(v bool) byte {
		if v {
			return 1
		}
		return 0
	}
	level := byte(255)
	if s.Level >= 0 {
		level = byte(min(s.Level, 1)*100 + 0.5)
	}
	return []byte{b2(s.Battery), b2(s.OnBattery), level, b2(s.Charging), b2(s.LowPower), byte(s.Thermal)}
}

// Current is the power now. Not on the main thread.
func Current() (State, error) {
	b, err := ui.CallWait("power", "current", nil)
	if err != nil {
		return State{}, err
	}
	return decodeState(b)
}

// Event is something that happens to the device's power.
type Event uint8

const (
	// Suspend: the computer is going to sleep (the lid closed, the
	// user's Sleep, idle). The app has a moment: it saves and pauses
	// what goes over the network.
	Suspend Event = iota + 1
	// Resume: it woke.
	Resume
	// Lock: the screen locked (or the screen saver started, on Linux
	// desktops that lock with it), or a phone's screen locked with a
	// passcode.
	Lock
	// Unlock: the user unlocked it.
	Unlock
)

func (e Event) String() string {
	switch e {
	case Suspend:
		return "suspend"
	case Resume:
		return "resume"
	case Lock:
		return "lock"
	case Unlock:
		return "unlock"
	}
	return "unknown"
}

// The host's "next" answers with the state, then the events it saw since
// (a byte each, Event's values).
var module = follow.Module{Name: "power", StateLen: stateLen}

// Watch calls f, on the main thread, with the power now and again each
// time it changes, until stop.
func Watch(f func(State)) (stop func()) {
	var last State
	first := true
	return module.Watch(func(b []byte) {
		s, err := decodeState(b)
		if err != nil || !first && s == last {
			return
		}
		first, last = false, s
		f(s)
	})
}

// Use is the power as a signal, for a component: it follows changes
// while the component lives. Main thread.
func Use() signal.Accessor[State] {
	s := signal.New(State{Level: -1})
	signal.OnCleanup(Watch(s.Set))
	return s
}

// OnEvent calls f, on the main thread, with each Event as it happens,
// until stop.
func OnEvent(f func(Event)) (stop func()) {
	return module.Watch(func(b []byte) {
		if len(b) < stateLen {
			return
		}
		for _, e := range b[stateLen:] {
			if e >= byte(Suspend) && e <= byte(Unlock) {
				f(Event(e))
			}
		}
	})
}

// on calls f at each e.
func on(e Event, f func()) (stop func()) {
	return OnEvent(func(got Event) {
		if got == e {
			f()
		}
	})
}

// OnSuspend calls f, on the main thread, as the computer goes to sleep,
// until stop: macOS, Windows, Linux (logind). Phones don't say: an app
// there is suspended as it goes to the background (ui.UseLifecycle).
func OnSuspend(f func()) (stop func()) { return on(Suspend, f) }

// OnResume calls f, on the main thread, as the computer wakes, until
// stop.
func OnResume(f func()) (stop func()) { return on(Resume, f) }

// OnLock calls f, on the main thread, as the screen locks, until stop:
// macOS, Windows, Linux (logind's lock, or the screen saver), iOS (with
// a passcode: as protected data goes), Android (the screen off on a
// secure lock screen).
func OnLock(f func()) (stop func()) { return on(Lock, f) }

// OnUnlock calls f, on the main thread, as the user unlocks the screen,
// until stop.
func OnUnlock(f func()) (stop func()) { return on(Unlock, f) }

// ErrIdleUnknown is IdleTime's error where the system doesn't say: a
// phone, a browser, a Linux desktop that reports neither GNOME's idle
// monitor nor the screen saver's idle time.
var ErrIdleUnknown = errors.New("power: the idle time isn't known here")

// IdleTime is how long since the user last used the keyboard, the mouse
// or the touchpad, in any app: to show them away, to pause what they'd
// watch. Not on the main thread.
func IdleTime() (time.Duration, error) {
	// The host's reply: u64 milliseconds; none where it doesn't know.
	b, err := ui.CallWait("power", "idle", nil)
	if err != nil {
		return 0, err
	}
	if len(b) < 8 {
		return 0, ErrIdleUnknown
	}
	return time.Duration(binary.LittleEndian.Uint64(b)) * time.Millisecond, nil
}
