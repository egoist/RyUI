// Package bluetooth talks to Bluetooth Low Energy devices
// (react-native-ble-plx, Web Bluetooth): heart rate straps, sensors,
// locks, a board of the app's own. Each platform's own API: Core
// Bluetooth on iOS and macOS; Android's BluetoothLeScanner,
// BluetoothGatt and BluetoothGattServer; Windows.Devices.Bluetooth on
// Windows; BlueZ over D-Bus on Linux; Web Bluetooth in a browser that
// has it (Chrome, Edge; not Safari or Firefox).
//
// As a central, the app scans for devices (Scan), or lets the user pick
// one in the system's chooser (Choose: the browser's, which is the web's
// only way; Android's companion device dialog), connects (Connect), and
// reads, writes and subscribes to its characteristics:
//
//	stop := bluetooth.Scan(bluetooth.ScanOptions{Services: []bluetooth.UUID{bluetooth.HeartRate}},
//		func(a bluetooth.Advertisement, err error) { found.Set(append(found.Get(), a)) })
//	...
//	c := bluetooth.Connect(a.Device, bluetooth.ConnectOptions{})
//	go func() {
//		ch, err := c.Characteristic(bluetooth.HeartRate, bluetooth.HeartRateMeasurement)
//		if err != nil {
//			return
//		}
//		ui.Post(func() {
//			ch.Subscribe(func(v []byte, err error) { bpm.Set(heartRate(v)) })
//		})
//	}()
//
// As a peripheral (not on the web), it advertises (Advertise) and serves
// services of its own (Serve), answering centrals' reads and writes and
// notifying their subscribers.
//
// What lasts (a scan, a connection, a subscription, advertising, a
// server) starts on the main thread and runs until it's stopped or the
// owner it started under is disposed: a component's, so a screen's scan
// stops with the screen. What waits for a device or the user (Current,
// Request, Choose, a connection's reads and writes) isn't on the main
// thread: call it from a goroutine.
//
// The app needs the user's permission: Request asks for it (iOS's and
// macOS's prompt, with asuka.json's ios.usage
// NSBluetoothAlwaysUsageDescription; Android 12's nearby devices
// permission, the location permission before). An app that imports the
// package is given what it declares; asuka.json's "bluetooth" asks for
// the background modes (docs/bluetooth.md).
package bluetooth

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// State is the adapter's, and whether the app may use it.
type State uint8

const (
	// Unknown: not known yet (the adapter starting, or resetting).
	Unknown State = iota
	// Unsupported: no Bluetooth LE here (the simulator, a browser without
	// Web Bluetooth, a computer without an adapter).
	Unsupported
	// NotAsked: the app hasn't asked for the permission yet: Request.
	NotAsked
	// Denied: the user didn't allow it (or a policy doesn't): they allow
	// it in the app's settings.
	Denied
	// Off: Bluetooth is turned off.
	Off
	// On: ready.
	On
)

func (s State) String() string {
	return [...]string{"unknown", "unsupported", "not asked", "denied", "off", "on", "?"}[min(int(s), 6)]
}

func state(s string) State {
	switch s {
	case spec.StateUnsupported:
		return Unsupported
	case spec.StateNotAsked:
		return NotAsked
	case spec.StateDenied:
		return Denied
	case spec.StateOff:
		return Off
	case spec.StateOn:
		return On
	}
	return Unknown
}

// The errors the package's calls fail with (compare with errors.Is).
var (
	// ErrNotAvailable: the platform hasn't that part (the web has no
	// peripheral role and no scan; the chooser is the web's and Android's).
	ErrNotAvailable = errors.New("bluetooth: not available")
	// ErrOff, ErrDenied, ErrUnsupported: the adapter's state stopped it.
	ErrOff         = errors.New("bluetooth: off")
	ErrDenied      = errors.New("bluetooth: not allowed")
	ErrUnsupported = errors.New("bluetooth: unsupported")
	// ErrCanceled: the user closed the chooser, or the app closed the
	// connection while it was connecting.
	ErrCanceled = errors.New("bluetooth: canceled")
	// ErrDisconnected: the connection ended (a *DisconnectError says why).
	ErrDisconnected = errors.New("bluetooth: disconnected")
	// ErrNotFound: the device has no such service, characteristic or
	// descriptor, or the platform no longer knows the device.
	ErrNotFound = errors.New("bluetooth: not found")
	// ErrTimeout: it took longer than the app allowed (ConnectOptions.Timeout).
	ErrTimeout = errors.New("bluetooth: timeout")
)

// hostError is the error a host's message names (internal/bluetoothspec).
func hostError(err error) error {
	if err == nil {
		return nil
	}
	m := err.Error()
	why := func(base error, prefix string) error {
		if rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(m, prefix), ":")); rest != "" {
			return fmt.Errorf("%w: %s", base, rest)
		}
		return base
	}
	switch {
	case strings.HasPrefix(m, "not available"), strings.HasPrefix(m, "no module"):
		return why(ErrNotAvailable, "not available")
	case strings.HasPrefix(m, "off"):
		return ErrOff
	case strings.HasPrefix(m, "denied"):
		return why(ErrDenied, "denied")
	case strings.HasPrefix(m, spec.StateNotAsked):
		return fmt.Errorf("%w: not asked yet (Request asks)", ErrDenied)
	case strings.HasPrefix(m, "unsupported"):
		return ErrUnsupported
	case strings.HasPrefix(m, "canceled"):
		return ErrCanceled
	case strings.HasPrefix(m, "disconnected"):
		return ErrDisconnected
	case strings.HasPrefix(m, "not found"):
		return why(ErrNotFound, "not found")
	case strings.HasPrefix(m, "timeout"):
		return ErrTimeout
	case strings.HasPrefix(m, "att "):
		code, rest, _ := strings.Cut(m[4:], ":")
		if n, err := strconv.Atoi(code); err == nil && n > 0 && n < 256 {
			return &ATTRequestError{Code: ATTError(n), Message: strings.TrimSpace(rest)}
		}
	}
	return errors.New("bluetooth: " + m)
}

const module = "bluetooth"

// call is a blocking call of the module (not on the main thread).
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures):
// every call that waits goes through it.
//
//go:noinline
func call(method string, args any) ([]byte, error) {
	var b []byte
	if args != nil {
		b, _ = json.Marshal(args)
	} else {
		b = []byte("{}")
	}
	r, err := ui.CallWait(module, method, b)
	return r, hostError(err)
}

// callJSON is call, its reply decoded into out.
func callJSON(method string, args, out any) error {
	b, err := call(method, args)
	if err != nil || out == nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("bluetooth: a bad reply to %s: %v", method, err)
	}
	return nil
}

// send is a call of the module from the main thread; done (if any) gets
// its reply there.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures):
// every call from the main thread goes through it.
//
//go:noinline
func send(method string, args any, done func([]byte, error)) {
	b, _ := json.Marshal(args)
	ui.Call(module, method, b, func(r []byte, err error) {
		if done != nil {
			done(r, hostError(err))
		}
	})
}

// Current is the adapter's state now, without asking for the permission.
// Not on the main thread.
func Current() (State, error) {
	var s spec.State
	if err := callJSON("state", nil, &s); err != nil {
		if errors.Is(err, ErrNotAvailable) {
			return Unsupported, nil
		}
		return Unknown, err
	}
	return state(s.State), nil
}

// Request asks for the permission, the first time (after, it's the
// user's answer then, or what they've set in Settings since), and
// reports the state. Ask once the app is on screen, as the user does
// what needs it. The web asks for each device, in the chooser: there it
// asks nothing. Not on the main thread.
func Request() (State, error) {
	var s spec.State
	if err := callJSON("request", nil, &s); err != nil {
		if errors.Is(err, ErrNotAvailable) {
			return Unsupported, nil
		}
		return Unknown, err
	}
	return state(s.State), nil
}

// Watch calls f with the state now, then each time it changes (the user
// turns Bluetooth off, allows the app), until stop or the current
// owner's disposal. Main thread.
func Watch(f func(State)) (stop func()) {
	fp := &f
	stateWatchers[fp] = true
	retain()
	send("state", struct{}{}, func(b []byte, err error) {
		if !stateWatchers[fp] {
			return
		}
		var s spec.State
		if err != nil {
			if errors.Is(err, ErrNotAvailable) {
				f(Unsupported)
			}
			return
		}
		if json.Unmarshal(b, &s) == nil {
			f(state(s.State))
		}
	})
	stop = once(func() {
		delete(stateWatchers, fp)
		release()
	})
	signal.OnCleanup(stop)
	return stop
}

// Use is the state as a signal, for a component: it follows the state
// while the component lives. Main thread.
func Use() signal.Accessor[State] {
	s := signal.New(Unknown)
	Watch(s.Set)
	return s
}

// once makes f run once however often it's called.
func once(f func()) func() {
	done := false
	return func() {
		if !done {
			done = true
			f()
		}
	}
}

// Following the host's events: one "next" at a time, from the main
// thread, while anything listens (live); everything here is the main
// thread's.

var (
	live          int
	following     bool
	stateWatchers = map[*func(State)]bool{}
	scans         = map[uint32]*scan{}
	conns         = map[uint32]*Connection{}
	servers       = map[uint32]*Server{}
	adverts       = map[uint32]func(error){}
	restorers     = map[*func(Restored)]bool{}
	nextID        uint32
)

func newID() uint32 {
	nextID++
	return nextID
}

// retain notes something more listens, and follows if nothing did.
func retain() {
	live++
	if !following {
		following = true
		follow()
	}
}

// release notes something stopped listening; the last stops the follow.
func release() {
	live--
	if live == 0 {
		send("stop", struct{}{}, nil)
	}
}

func follow() {
	send("next", struct{}{}, func(b []byte, err error) {
		if err == nil {
			var evs []spec.Event
			if json.Unmarshal(b, &evs) == nil {
				for i := range evs {
					dispatch(&evs[i])
				}
			}
		}
		// "stopped": the last listener went, maybe with another come since.
		if live > 0 && (err == nil || err.Error() == "bluetooth: stopped") {
			follow()
			return
		}
		following = false
	})
}

func dispatch(e *spec.Event) {
	switch e.Type {
	case spec.EventState:
		s := state(e.State)
		for _, f := range keys(stateWatchers) {
			if stateWatchers[f] {
				(*f)(s)
			}
		}
	case spec.EventAdvertisement:
		if s := scans[e.Scan]; s != nil && e.Advertisement != nil {
			s.found(advertisement(e.Advertisement), nil)
		}
	case spec.EventScanStopped:
		if s := scans[e.Scan]; s != nil {
			s.end(hostError(errors.New(e.Error)))
		}
	case spec.EventDisconnected:
		if c := conns[e.Conn]; c != nil {
			c.disconnected(&DisconnectError{Reason: reason(e.Reason), Code: e.Code, Message: e.Error})
		}
	case spec.EventValue:
		if c := conns[e.Conn]; c != nil {
			c.value(e.Handle, e.Value)
		}
	case spec.EventServicesChanged:
		if c := conns[e.Conn]; c != nil {
			c.mu.Lock()
			c.services = nil
			c.mu.Unlock()
		}
	case spec.EventRead, spec.EventWrite, spec.EventSubscription:
		if s := servers[e.Server]; s != nil {
			s.event(e)
		}
	case spec.EventServerStopped:
		if s := servers[e.Server]; s != nil {
			s.end(hostError(errors.New(e.Error)))
		}
	case spec.EventAdvertiseStopped:
		if f := adverts[e.ID]; f != nil {
			delete(adverts, e.ID)
			release()
			if f != nil {
				f(hostError(errors.New(e.Error)))
			}
		}
	case spec.EventRestored:
		askRestored()
	}
}

func keys[K comparable, V any](m map[K]V) []K {
	out := make([]K, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// Restored is what the system brought back as it launched the app for
// Bluetooth while the app wasn't running.
type Restored struct {
	// Devices are those iOS kept connected (or connecting) for the app
	// (state restoration: asuka.json's bluetooth.background "central"):
	// Connect to them to use them again, which is immediate.
	Devices []Device
	// ScanServices are the services of the scan iOS kept running (start
	// it again: Scan with them, Background).
	ScanServices []UUID
	// Advertisements are what Android's background scan heard while the
	// app wasn't running (ScanOptions.Background).
	Advertisements []Advertisement
}

// OnRestore calls f, on the main thread, with what the system restored
// for the app (Restored): at launch, and each time after that it
// relaunches the app for Bluetooth, until stop or the current owner's
// disposal. Call it as the app starts (in its root component).
func OnRestore(f func(Restored)) (stop func()) {
	fp := &f
	restorers[fp] = true
	retain()
	askRestored()
	stop = once(func() {
		delete(restorers, fp)
		release()
	})
	signal.OnCleanup(stop)
	return stop
}

func askRestored() {
	send("restore", struct{}{}, func(b []byte, err error) {
		var w spec.Restored
		if err != nil || json.Unmarshal(b, &w) != nil {
			return
		}
		r := Restored{ScanServices: parseUUIDs(w.ScanServices)}
		for _, d := range w.Devices {
			r.Devices = append(r.Devices, Device(d))
		}
		for i := range w.Advertisements {
			r.Advertisements = append(r.Advertisements, advertisement(&w.Advertisements[i]))
		}
		if len(r.Devices) == 0 && len(r.ScanServices) == 0 && len(r.Advertisements) == 0 {
			return
		}
		for _, f := range keys(restorers) {
			if restorers[f] {
				(*f)(r)
			}
		}
	})
}
