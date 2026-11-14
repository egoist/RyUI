package bluetooth

import (
	"strconv"
	"time"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	"github.com/egoist/asuka/signal"
)

// Device is a peripheral: one a scan heard, the chooser gave, or the
// platform knows.
type Device struct {
	// ID is the platform's name for it, which Known finds it by again:
	// on iOS and macOS a UUID Core Bluetooth made for the app (not the
	// device's address, which it doesn't give); on Android, Windows and
	// Linux its address ("C4:7C:8D:6A:1F:20"); on the web the browser's
	// ID for it, the site's.
	ID string
	// Name is its name, as the system knows it (its GAP name, or the one
	// it advertises), if it has one.
	Name string
}

// Advertisement is what a scan heard from a device.
type Advertisement struct {
	Device Device
	// RSSI is the signal's strength as received, in dBm (-30: close by;
	// -90: far).
	RSSI int
	// LocalName is the name it advertises (Device.Name may be one the
	// system remembers instead).
	LocalName string
	// Services are the services it advertises (most advertise a few, or
	// none: connect to find them all).
	Services []UUID
	// Solicited are the services it looks for on a central.
	Solicited []UUID
	// ServiceData is data for a service: the advertisement's service data.
	ServiceData map[UUID][]byte
	// ManufacturerData is data by the company's identifier (the Bluetooth
	// SIG's: 0x004c is Apple's), without the identifier's bytes.
	ManufacturerData map[uint16][]byte
	// TxPower is its transmit power, in dBm, if it advertises it
	// (HasTxPower): with RSSI, how far it is.
	TxPower    int
	HasTxPower bool
	// Connectable: it takes connections (true where the platform doesn't
	// say: BlueZ, the web).
	Connectable bool
	// Time is when it was heard.
	Time time.Time
}

func advertisement(w *spec.Advertisement) Advertisement {
	a := Advertisement{Device: Device(w.Device), RSSI: w.RSSI, LocalName: w.LocalName,
		Services: parseUUIDs(w.Services), Solicited: parseUUIDs(w.Solicited), Connectable: true,
		Time: time.UnixMilli(w.Time)}
	if a.Device.Name == "" {
		a.Device.Name = w.LocalName
	}
	if len(w.ServiceData) > 0 {
		a.ServiceData = make(map[UUID][]byte, len(w.ServiceData))
		for k, v := range w.ServiceData {
			if u, err := ParseUUID(k); err == nil {
				a.ServiceData[u] = v
			}
		}
	}
	if len(w.ManufacturerData) > 0 {
		a.ManufacturerData = make(map[uint16][]byte, len(w.ManufacturerData))
		for k, v := range w.ManufacturerData {
			if n, err := strconv.ParseUint(k, 10, 16); err == nil {
				a.ManufacturerData[uint16(n)] = v
			}
		}
	}
	if w.TxPower != nil {
		a.TxPower, a.HasTxPower = *w.TxPower, true
	}
	if w.Connectable != nil {
		a.Connectable = *w.Connectable
	}
	return a
}

// ScanMode is how hard Android scans: its battery against how soon it
// hears a device. The other platforms decide themselves.
type ScanMode uint8

const (
	// ScanDefault: LowLatency while the app is on screen, LowPower for a
	// background scan.
	ScanDefault ScanMode = iota
	ScanLowPower
	ScanBalanced
	ScanLowLatency
	// ScanOpportunistic hears only what other apps' scans do.
	ScanOpportunistic
)

func (m ScanMode) wire() string {
	return [...]string{"", spec.ModeLowPower, spec.ModeBalanced, spec.ModeLowLatency, spec.ModeOpportunistic, ""}[min(int(m), 5)]
}

// ScanOptions are what a scan looks for.
type ScanOptions struct {
	// Services: only devices that advertise one of them (none: every
	// device). iOS scans in the background only for services.
	Services []UUID
	// Duplicates: each advertisement as it's heard, to follow a device's
	// RSSI or its changing data (a beacon's), not just its first. It costs
	// battery: scan so only while the user watches.
	Duplicates bool
	// Mode is Android's (ScanMode).
	Mode ScanMode
	// Background keeps scanning with the app away (asuka.json's
	// bluetooth.background "central"): on iOS, for Services only, and
	// slower, each device once; on Android a scan the system runs, which
	// goes on after the app's process is gone (stop ends it while the
	// app runs): what it hears then comes to OnRestore, until a later
	// run of the app calls StopBackgroundScan. Android pauses a scan
	// without Services while the screen is off. The desktops and the web
	// scan only while the app runs.
	Background bool
}

type scan struct {
	id    uint32
	found func(Advertisement, error)
	ended bool
}

// end ends the scan with err, telling the app (nil: the app stopped it).
func (s *scan) end(err error) {
	if s.ended {
		return
	}
	s.ended = true
	delete(scans, s.id)
	release()
	if err != nil {
		s.found(Advertisement{}, err)
	}
}

// Scan scans for devices, calling found, on the main thread, with each
// advertisement heard, until stop or the current owner's disposal. If
// the scan can't start, or the platform stops it (Bluetooth turned off,
// the permission taken back), found gets the error once, and nothing
// after it: ErrOff, ErrDenied, ErrUnsupported, and on the web
// ErrNotAvailable (Choose is the web's: browsers don't let pages scan).
// Ask for the permission first (Request). Main thread.
func Scan(o ScanOptions, found func(Advertisement, error)) (stop func()) {
	s := &scan{id: newID(), found: found}
	scans[s.id] = s
	retain()
	send("scan", spec.ScanArgs{ID: s.id, Services: uuids(o.Services), Duplicates: o.Duplicates,
		Mode: o.Mode.wire(), Background: o.Background}, func(_ []byte, err error) {
		if err != nil {
			s.end(err)
		}
	})
	stop = once(func() {
		if !s.ended {
			send("stopScan", spec.ID{ID: s.id}, nil)
			s.end(nil)
		}
	})
	signal.OnCleanup(stop)
	return stop
}

// StopBackgroundScan stops Android's background scan (ScanOptions.
// Background) that outlived the app's process: what a previous run of
// the app started and didn't stop. Elsewhere it does nothing. Not on the
// main thread.
func StopBackgroundScan() error {
	_, err := call("stopBackgroundScan", nil)
	return err
}

// Filter is what a device in the chooser must match: each of its parts
// that's set.
type Filter struct {
	// Services: the device advertises them all.
	Services []UUID
	// Name, or NamePrefix: its name is that, or starts so.
	Name, NamePrefix string
	// Manufacturer is a company identifier its manufacturer data has
	// (with HasManufacturer: 0 is a company's too).
	Manufacturer    uint16
	HasManufacturer bool
}

// ChooseOptions are what the chooser shows.
type ChooseOptions struct {
	// Filters: the devices that match one (none: every device nearby).
	Filters []Filter
	// OptionalServices are the services the app uses once connected
	// beyond those of the filters: a browser lets a page use only these.
	OptionalServices []UUID
}

// Choose shows the system's chooser of nearby devices, for the user to
// pick one, and returns it: on the web, the browser's (requestDevice: a
// page's only way to find a device, and the permission to use it, so
// call it as the user taps); on Android, the companion device dialog
// (CompanionDeviceManager), which also lets the app connect from the
// background. ErrCanceled if the user closes it; ErrNotAvailable on the
// other platforms, which have none: Scan there. Not on the main thread.
func Choose(o ChooseOptions) (Device, error) {
	args := spec.ChooseArgs{OptionalServices: uuids(o.OptionalServices)}
	for _, f := range o.Filters {
		w := spec.Filter{Services: uuids(f.Services), Name: f.Name, NamePrefix: f.NamePrefix}
		if f.HasManufacturer {
			m := int(f.Manufacturer)
			w.Manufacturer = &m
		}
		args.Filters = append(args.Filters, w)
	}
	var d spec.Device
	if err := callJSON("choose", args, &d); err != nil {
		return Device{}, err
	}
	return Device(d), nil
}

// Known are the devices of ids the platform still knows (IDs the app
// kept from before: Device.ID), to connect to without scanning: iOS
// remembers those the app has seen; Android, Windows and Linux take any
// address; the web, those the user allowed the site. Not on the main
// thread.
func Known(ids ...string) ([]Device, error) {
	var ds []spec.Device
	if err := callJSON("known", spec.KnownArgs{IDs: append([]string{}, ids...)}, &ds); err != nil {
		return nil, err
	}
	return devices(ds), nil
}

// ConnectedDevices are the devices the system is connected to (for another app,
// for itself: a watch, a keyboard), with one of services: connect to
// them too. iOS needs services; elsewhere none is every device. Not on
// the main thread.
func ConnectedDevices(services ...UUID) ([]Device, error) {
	var ds []spec.Device
	if err := callJSON("connected", spec.ConnectedArgs{Services: uuids(services)}, &ds); err != nil {
		return nil, err
	}
	return devices(ds), nil
}

func devices(ds []spec.Device) []Device {
	out := make([]Device, len(ds))
	for i, d := range ds {
		out[i] = Device(d)
	}
	return out
}
