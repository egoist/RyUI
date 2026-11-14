// Bluetooth finds Bluetooth LE devices nearby (asuka/sdk/bluetooth): it
// scans for them (on the web, the browser's chooser finds one), connects
// to the one tapped, and reads its battery and its heart rate, which it
// follows as the device notifies it. As a peripheral, it's a heart rate
// monitor itself: it serves the Heart Rate and Battery services and
// advertises them, for another phone running it to find.
//
//	go run ./cmd/asuka dev --platform ios examples/bluetooth
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/sdk/bluetooth"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// found is a device a scan heard, as the list shows it.
type found struct {
	Device bluetooth.Device
	Name   string
	RSSI   int
	Heart  bool // it advertises the Heart Rate service
}

func App() ui.Node {
	return ui.Stack(Scanner, ui.ScreenOptions{Title: ui.Val("Bluetooth")})
}

// Scanner is the first screen: the adapter's state, the scan and what it
// finds, and the monitor.
func Scanner() ui.Node {
	nav := ui.UseNavigator()
	state := bluetooth.Use()
	devices := signal.NewFunc[[]found](nil, slices.Equal)
	scanning := signal.New(false)
	monitoring := signal.New(false)
	status := signal.New("")
	var stop func()

	open := func(d bluetooth.Device, name string) {
		nav.Push(func() ui.Node { return DeviceScreen(d) }, ui.ScreenOptions{Title: ui.Val(name)})
	}
	heard := func(a bluetooth.Advertisement, err error) {
		if err != nil {
			scanning.Set(false)
			status.Set(scanError(err))
			return
		}
		f := found{Device: a.Device, Name: a.Device.Name, RSSI: a.RSSI, Heart: slices.Contains(a.Services, bluetooth.HeartRate)}
		if f.Name == "" {
			f.Name = "Unnamed (" + a.Device.ID + ")"
		}
		ds := slices.Clone(devices.Peek())
		if i := slices.IndexFunc(ds, func(x found) bool { return x.Device.ID == a.Device.ID }); i >= 0 {
			ds[i] = f
		} else {
			ds = append(ds, f)
		}
		// The nearest first.
		slices.SortStableFunc(ds, func(x, y found) int { return y.RSSI - x.RSSI })
		devices.Set(ds)
	}
	toggleScan := func() {
		if scanning.Peek() {
			stop()
			scanning.Set(false)
			return
		}
		status.Set("")
		go func() {
			s, err := bluetooth.Request()
			ui.Post(func() {
				if err != nil || s != bluetooth.On {
					status.Set(fmt.Sprintf("Bluetooth is %v", s))
					return
				}
				devices.Set(nil)
				scanning.Set(true)
				// Each device again as it advertises: its RSSI as it moves.
				stop = bluetooth.Scan(bluetooth.ScanOptions{Duplicates: true}, heard)
			})
		}()
	}
	choose := func() {
		go func() {
			d, err := bluetooth.Choose(bluetooth.ChooseOptions{
				Filters:          []bluetooth.Filter{{Services: []bluetooth.UUID{bluetooth.HeartRate}}, {Services: []bluetooth.UUID{bluetooth.Battery}}},
				OptionalServices: []bluetooth.UUID{bluetooth.Battery, bluetooth.DeviceInformation},
			})
			ui.Post(func() {
				switch {
				case errors.Is(err, bluetooth.ErrCanceled):
				case err != nil:
					status.Set(err.Error())
				default:
					open(d, d.Name)
				}
			})
		}()
	}

	return ui.SafeArea(ui.Style{Grow: 1, Padding: ui.All(16), Gap: 12},
		ui.Text(signal.Func(func() string { return "Bluetooth: " + state.Get().String() }),
			ui.Style{Color: colors.SecondaryLabel}),
		ui.Row(ui.Style{Gap: 12},
			ui.Button(ui.ButtonProps{
				Title: signal.Func(func() string {
					if scanning.Get() {
						return "Stop"
					}
					return "Scan"
				}),
				Variant: ui.ButtonFilled,
				OnPress: toggleScan,
			}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Choose"), Variant: ui.ButtonGray, OnPress: choose}),
			ui.Button(ui.ButtonProps{
				Title: signal.Func(func() string {
					if monitoring.Get() {
						return "Stop monitor"
					}
					return "Be a monitor"
				}),
				Variant: ui.ButtonGray,
				OnPress: func() { monitoring.Set(!monitoring.Peek()) },
			}),
		),
		ui.Text(status, ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote}),
		ui.Show(monitoring, func() ui.Node { return Monitor(status) }),
		ui.List(ui.ListProps{}, devices, func(f found) string { return f.Device.ID },
			func(f signal.Accessor[found], _ signal.Accessor[int]) ui.Node {
				return ui.View(ui.ViewProps{
					OnPress: func() { d := f.Get(); open(d.Device, d.Name) },
					Style:   ui.Style{Direction: ui.RowDir, Gap: 12, Padding: ui.Symmetric(12, 16), Align: ui.AlignCenter},
				},
					ui.Text(signal.Func(func() string {
						if f.Get().Heart {
							return "♥ " + f.Get().Name
						}
						return f.Get().Name
					}), ui.Style{Grow: 1}),
					ui.Text(signal.Func(func() string { return fmt.Sprintf("%d dBm", f.Get().RSSI) }),
						ui.Style{Color: colors.SecondaryLabel}),
				)
			}),
	)
}

func scanError(err error) string {
	switch {
	case errors.Is(err, bluetooth.ErrNotAvailable):
		return "This platform doesn't scan: Choose a device"
	case errors.Is(err, bluetooth.ErrOff):
		return "Bluetooth is off"
	case errors.Is(err, bluetooth.ErrDenied):
		return "The app isn't allowed Bluetooth: allow it in Settings"
	}
	return err.Error()
}

// DeviceScreen connects to the device while it's on screen (the
// connection's owner is the screen's: going back disconnects), and shows
// its battery and its heart rate.
func DeviceScreen(d bluetooth.Device) ui.Node {
	c := bluetooth.Connect(d, bluetooth.ConnectOptions{Timeout: 20 * time.Second})
	battery := signal.New("")
	bpm := signal.New("")
	info := signal.New("")
	subscribe := func(ch bluetooth.Characteristic) {
		ch.Subscribe(func(v []byte, err error) {
			if err != nil {
				bpm.Set(err.Error())
				return
			}
			if n, ok := HeartRate(v); ok {
				bpm.Set(fmt.Sprintf("%d bpm", n))
			}
		})
	}
	go func() {
		ss, err := c.Services()
		if err != nil {
			ui.Post(func() { info.Set(err.Error()) })
			return
		}
		m, _ := c.MTU()
		ui.Post(func() { info.Set(fmt.Sprintf("%d services, MTU %d", len(ss), m.MTU)) })
		if ch, err := c.Characteristic(bluetooth.Battery, bluetooth.BatteryLevel); err == nil {
			if v, err := ch.Read(); err == nil && len(v) > 0 {
				ui.Post(func() { battery.Set(fmt.Sprintf("%d%%", v[0])) })
			}
			if ch.Properties&bluetooth.Notify != 0 {
				ui.Post(func() {
					ch.Subscribe(func(v []byte, err error) {
						if err == nil && len(v) > 0 {
							battery.Set(fmt.Sprintf("%d%%", v[0]))
						}
					})
				})
			}
		}
		if ch, err := c.Characteristic(bluetooth.HeartRate, bluetooth.HeartRateMeasurement); err == nil {
			ui.Post(func() { subscribe(ch) })
		}
	}()
	row := func(label string, value signal.Accessor[string]) ui.Node {
		return ui.Show(signal.Func(func() bool { return value.Get() != "" }), func() ui.Node {
			return ui.Row(ui.Style{Gap: 12},
				ui.StaticText(label, ui.Style{Grow: 1}),
				ui.Text(value, ui.Style{Color: colors.SecondaryLabel}))
		})
	}
	return ui.SafeArea(ui.Style{Grow: 1, Padding: ui.All(16), Gap: 12},
		ui.Text(signal.Func(func() string {
			s := c.State().Get()
			if s == bluetooth.Disconnected {
				if err := c.Err(); err != nil {
					return "Disconnected: " + err.Error()
				}
			}
			return s.String()
		}), ui.Style{Font: fonts.Headline}),
		row("Battery", battery),
		row("Heart rate", bpm),
		ui.Text(info, ui.Style{Color: colors.SecondaryLabel, Font: fonts.Footnote}),
	)
}

// HeartRate is the beats per minute of a Heart Rate Measurement (the
// Heart Rate service's 0x2a37): a flags byte, whose bit 0 says the value
// is 16 bits, then the value.
func HeartRate(v []byte) (int, bool) {
	switch {
	case len(v) >= 3 && v[0]&1 != 0:
		return int(binary.LittleEndian.Uint16(v[1:])), true
	case len(v) >= 2 && v[0]&1 == 0:
		return int(v[1]), true
	}
	return 0, false
}

// Monitor is the app as a heart rate monitor, while it's on screen: the
// Heart Rate and Battery services served and advertised, a beat a second
// notified to the centrals that subscribe.
func Monitor(status *signal.Signal[string]) ui.Node {
	beat := 72
	subscribers := signal.New(0)
	measurement := &bluetooth.LocalCharacteristic{UUID: bluetooth.HeartRateMeasurement, Properties: bluetooth.Notify}
	measurement.OnSubscribe = func(bluetooth.Device, bool) { subscribers.Set(len(measurement.Subscribers())) }
	services := []*bluetooth.LocalService{
		{UUID: bluetooth.HeartRate, Characteristics: []*bluetooth.LocalCharacteristic{
			measurement,
			// On the chest.
			{UUID: bluetooth.BodySensorLocation, Properties: bluetooth.Read, Permissions: bluetooth.Readable, Value: []byte{1}},
		}},
		{UUID: bluetooth.Battery, Characteristics: []*bluetooth.LocalCharacteristic{{
			UUID: bluetooth.BatteryLevel, Properties: bluetooth.Read, Permissions: bluetooth.Readable,
			OnRead: func(bluetooth.ServerRequest) ([]byte, error) { return []byte{87}, nil },
		}}},
	}
	failed := func(err error) { status.Set("Monitor: " + err.Error()) }
	bluetooth.Serve(services, failed)
	bluetooth.Advertise(bluetooth.AdvertiseOptions{LocalName: "Asuka HR", Services: []bluetooth.UUID{bluetooth.HeartRate}}, failed)
	t := time.NewTicker(time.Second)
	quit := make(chan struct{})
	go func() {
		for {
			select {
			case <-quit:
				return
			case <-t.C:
				ui.Post(func() {
					beat = 60 + (beat-60+7)%40
					measurement.Notify([]byte{0, byte(beat)})
				})
			}
		}
	}()
	signal.OnCleanup(func() { t.Stop(); close(quit) })
	return ui.Text(signal.Func(func() string {
		return fmt.Sprintf("Advertising as Asuka HR: %d subscribed", subscribers.Get())
	}), ui.Style{Color: colors.SecondaryLabel})
}

func main() { app.Run(App) }
