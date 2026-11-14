package bluetooth_test

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/egoist/asuka/host/fake"
	spec "github.com/egoist/asuka/internal/bluetoothspec"
	"github.com/egoist/asuka/sdk/bluetooth"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
	"github.com/egoist/asuka/uitest"
)

func TestUUID(t *testing.T) {
	for _, s := range []string{"180d", "180D", "0x180d", "0000180d", "0000180d-0000-1000-8000-00805f9b34fb", "0000180D00001000800000805F9B34FB", "{0000180d-0000-1000-8000-00805f9b34fb}"} {
		u, err := bluetooth.ParseUUID(s)
		if err != nil || u != bluetooth.HeartRate {
			t.Errorf("%q: %v, %v", s, u, err)
		}
	}
	if s := bluetooth.HeartRate.String(); s != "0000180d-0000-1000-8000-00805f9b34fb" {
		t.Errorf("String: %s", s)
	}
	if n, ok := bluetooth.BatteryLevel.Short(); !ok || n != 0x2a19 {
		t.Errorf("Short: %x %v", n, ok)
	}
	nus := bluetooth.MustParseUUID("6E400001-B5A3-F393-E0A9-E50E24DCCA9E")
	if _, ok := nus.Short(); ok || nus.String() != "6e400001-b5a3-f393-e0a9-e50e24dcca9e" {
		t.Errorf("128-bit: %v", nus)
	}
	if bluetooth.UUID32(0x12345678).String() != "12345678-0000-1000-8000-00805f9b34fb" {
		t.Errorf("UUID32: %v", bluetooth.UUID32(0x12345678))
	}
	for _, bad := range []string{"", "18", "180g", "0000180d-0000-1000-8000-00805f9b34f", "0000180d+0000-1000-8000-00805f9b34fb"} {
		if _, err := bluetooth.ParseUUID(bad); err == nil {
			t.Errorf("%q parsed", bad)
		}
	}
	b, _ := json.Marshal(map[bluetooth.UUID]int{bluetooth.Battery: 1})
	var back map[bluetooth.UUID]int
	if err := json.Unmarshal(b, &back); err != nil || back[bluetooth.Battery] != 1 {
		t.Errorf("JSON: %s %v %v", b, back, err)
	}
}

func hr() *fake.Peripheral {
	return &fake.Peripheral{ID: "AA:00", Name: "HR", RSSI: -50, LocalName: "HR strap", Advertised: []string{"180d"},
		ServiceData:      map[string][]byte{"180d": {1, 2}},
		ManufacturerData: map[uint16][]byte{0x004c: {9}},
		TxPower:          new(int),
		MTU:              185,
		Services: []*fake.Service{
			{UUID: "180d", Characteristics: []*fake.Characteristic{
				{UUID: "2a37", Properties: int(bluetooth.Notify | bluetooth.Indicate)},
				{UUID: "2a39", Properties: int(bluetooth.Write | bluetooth.WriteWithoutResponse),
					Descriptors: []*fake.Descriptor{{UUID: "2901", Value: []byte("Reset")}}},
			}},
			{UUID: "180f", Characteristics: []*fake.Characteristic{{UUID: "2a19", Properties: int(bluetooth.Read), Value: []byte{90}}}},
		}}
}

// mount mounts a component on a fake host with a Bluetooth adapter.
func mount(t *testing.T, root func() ui.Node, setup func(bt *fake.Bluetooth)) (*uitest.App, *fake.Bluetooth) {
	var bt *fake.Bluetooth
	a := uitest.Mount(t, root, uitest.Setup(func(h *fake.Host) {
		bt = h.Bluetooth()
		if setup != nil {
			setup(bt)
		}
	}))
	return a, bt
}

func blank() ui.Node { return ui.View(ui.ViewProps{}) }

// Scans stop with their owner; the follow stops with the last.
func TestScanOwner(t *testing.T) {
	show := signal.New(true)
	var ads []bluetooth.Advertisement
	var scanErr error
	a, bt := mount(t, func() ui.Node {
		return ui.Show(show, func() ui.Node {
			bluetooth.Scan(bluetooth.ScanOptions{Services: []bluetooth.UUID{bluetooth.HeartRate}, Duplicates: true},
				func(ad bluetooth.Advertisement, err error) {
					if err != nil {
						scanErr = err
						return
					}
					ads = append(ads, ad)
				})
			return blank()
		})
	}, func(bt *fake.Bluetooth) { bt.Add(hr()) })
	a.Until("an advertisement", func() bool { return len(ads) == 1 })
	ad := ads[0]
	if ad.Device.ID != "AA:00" || ad.Device.Name != "HR" || ad.LocalName != "HR strap" || ad.RSSI != -50 ||
		len(ad.Services) != 1 || ad.Services[0] != bluetooth.HeartRate || string(ad.ServiceData[bluetooth.HeartRate]) != "\x01\x02" ||
		string(ad.ManufacturerData[0x004c]) != "\x09" || !ad.HasTxPower || !ad.Connectable || ad.Time.IsZero() {
		t.Errorf("advertisement %+v", ad)
	}
	// Another device, not advertising the service: not heard.
	bt.Add(&fake.Peripheral{ID: "BB:00", Name: "Lamp"})
	hrp := hr()
	hrp.ID = "CC:00"
	bt.Add(hrp)
	a.Until("the second strap", func() bool { return len(ads) == 2 })
	hrp.Advertise() // duplicates: heard again
	a.Until("the strap again", func() bool { return len(ads) == 3 })
	if len(bt.Scans) != 1 {
		t.Errorf("scans %v", bt.Scans)
	}
	show.Set(false)
	a.Settle()
	if len(bt.Scans) != 0 || bt.Following() || scanErr != nil {
		t.Errorf("after the owner went: scans %v, following %v, %v", bt.Scans, bt.Following(), scanErr)
	}
}

// A scan that can't start says why, once.
func TestScanOff(t *testing.T) {
	var errs []error
	a, _ := mount(t, func() ui.Node {
		bluetooth.Scan(bluetooth.ScanOptions{}, func(_ bluetooth.Advertisement, err error) { errs = append(errs, err) })
		return blank()
	}, func(bt *fake.Bluetooth) { bt.State = "off" })
	a.Until("the error", func() bool { return len(errs) > 0 })
	if len(errs) != 1 || !errors.Is(errs[0], bluetooth.ErrOff) {
		t.Errorf("errors %v", errs)
	}
}

// The state follows the adapter, and the permission asked.
func TestState(t *testing.T) {
	var st signal.Accessor[bluetooth.State]
	a, bt := mount(t, func() ui.Node {
		st = bluetooth.Use()
		return ui.Text(signal.Func(func() string { return st.Get().String() }))
	}, func(bt *fake.Bluetooth) { bt.State = "notAsked" })
	a.WaitFor("not asked")
	var got bluetooth.State
	go func() {
		s, _ := bluetooth.Request()
		ui.Post(func() { got = s })
	}()
	a.Until("allowed", func() bool { return got == bluetooth.On })
	bt.SetState("off")
	a.WaitFor("off")
	bt.SetState("on")
	a.WaitFor("on")
}

// call runs f off the main thread and waits for it, settling the app.
func call[T any](a *uitest.App, f func() (T, error)) (T, error) {
	var v T
	var err error
	done := false
	go func() {
		v2, err2 := f()
		ui.Post(func() { v, err, done = v2, err2, true })
	}()
	a.Until("the call", func() bool { return done })
	return v, err
}

func TestConnection(t *testing.T) {
	p := hr()
	var c *bluetooth.Connection
	show := signal.New(true)
	a, _ := mount(t, func() ui.Node {
		return ui.Show(show, func() ui.Node {
			c = bluetooth.Connect(bluetooth.Device{ID: "AA:00"}, bluetooth.ConnectOptions{})
			return ui.Text(signal.Func(func() string { return c.State().Get().String() }))
		})
	}, func(bt *fake.Bluetooth) { bt.Add(p) })
	a.WaitFor("connected")
	if _, err := call(a, func() (int, error) { return 0, c.Wait() }); err != nil {
		t.Fatal(err)
	}
	ss, err := call(a, c.Services)
	if err != nil || len(ss) != 2 || ss[0].UUID != bluetooth.HeartRate || !ss[0].Primary || len(ss[0].Characteristics) != 2 {
		t.Fatalf("services %+v, %v", ss, err)
	}
	m := ss[0].Characteristics[0]
	if m.Properties != bluetooth.Notify|bluetooth.Indicate || m.Service != bluetooth.HeartRate {
		t.Errorf("measurement %+v", m)
	}
	if _, ok := m.Descriptor(bluetooth.ClientCharacteristicConfiguration); !ok {
		t.Errorf("no CCCD: %+v", m.Descriptors)
	}
	level, _ := call(a, func() (bluetooth.Characteristic, error) {
		return c.Characteristic(bluetooth.Battery, bluetooth.BatteryLevel)
	})
	if v, err := call(a, level.Read); err != nil || len(v) != 1 || v[0] != 90 {
		t.Errorf("read %v, %v", v, err)
	}
	if _, err := call(a, func() (bluetooth.Characteristic, error) {
		return c.Characteristic(bluetooth.Battery, bluetooth.HeartRateMeasurement)
	}); !errors.Is(err, bluetooth.ErrNotFound) {
		t.Errorf("a characteristic it hasn't: %v", err)
	}
	// Writes, and the device's refusal.
	cp := ss[0].Characteristics[1]
	if _, err := call(a, func() (int, error) { return 0, cp.Write([]byte{1}) }); err != nil {
		t.Error(err)
	}
	if _, err := call(a, func() (int, error) { return 0, cp.WriteWithoutResponse([]byte{2}) }); err != nil {
		t.Error(err)
	}
	if w := p.Characteristic("180d", "2a39").Writes; len(w) != 2 || w[1][0] != 2 {
		t.Errorf("writes %v", w)
	}
	if _, err := call(a, func() (int, error) { return 0, level.Write([]byte{1}) }); !errors.Is(err, bluetooth.WriteNotPermitted) {
		t.Errorf("a write it refuses: %v", err)
	}
	var att *bluetooth.ATTRequestError
	if _, err := call(a, func() (int, error) { return 0, level.Write([]byte{1}) }); !errors.As(err, &att) || att.Code != bluetooth.WriteNotPermitted {
		t.Errorf("ATT error %v", err)
	}
	d, _ := cp.Descriptor(bluetooth.UserDescription)
	if v, err := call(a, d.Read); err != nil || string(v) != "Reset" {
		t.Errorf("descriptor %q, %v", v, err)
	}
	if mtu, err := call(a, func() (bluetooth.MTU, error) { return c.RequestMTU(517) }); err != nil || mtu.MTU != 185 || mtu.WriteWithoutResponse != 182 {
		t.Errorf("mtu %+v, %v", mtu, err)
	}
	if rssi, err := call(a, c.RSSI); err != nil || rssi != -50 {
		t.Errorf("rssi %d, %v", rssi, err)
	}
	if _, err := call(a, func() (int, error) { return 0, c.SetPriority(bluetooth.PriorityHigh) }); err != nil {
		t.Error(err)
	}

	// Notifications, shared by two subscriptions; the last one's stop
	// turns them off.
	var got1, got2 [][]byte
	stop1 := m.Subscribe(func(v []byte, err error) { got1 = append(got1, v) }, bluetooth.SubscribeOptions{Indicate: true})
	stop2 := m.Subscribe(func(v []byte, err error) { got2 = append(got2, v) })
	ch := p.Characteristic("180d", "2a37")
	a.Until("subscribed", func() bool { return ch.Subscribed })
	p.Notify("180d", "2a37", []byte{0, 70})
	a.Until("the notification", func() bool { return len(got1) == 1 && len(got2) == 1 })
	stop1()
	a.Settle()
	if !ch.Subscribed {
		t.Error("unsubscribed with a subscriber left")
	}
	stop2()
	a.Until("unsubscribed", func() bool { return !ch.Subscribed })

	// A subscription ends with the connection, with its error.
	var subErr error
	m.Subscribe(func(v []byte, err error) { subErr = err })
	a.Until("subscribed again", func() bool { return ch.Subscribed })
	p.Disconnect("remote")
	a.WaitFor("disconnected")
	var de *bluetooth.DisconnectError
	if !errors.As(c.Err(), &de) || de.Reason != bluetooth.ReasonRemote || !errors.Is(c.Err(), bluetooth.ErrDisconnected) {
		t.Errorf("err %v", c.Err())
	}
	if !errors.Is(subErr, bluetooth.ErrDisconnected) {
		t.Errorf("the subscription's error: %v", subErr)
	}
	select {
	case <-c.Done():
	default:
		t.Error("not done")
	}
	if _, err := call(a, level.Read); !errors.Is(err, bluetooth.ErrDisconnected) {
		t.Errorf("a read after: %v", err)
	}

	// Connected again, then gone with the owner.
	show.Set(false)
	show.Set(true)
	a.WaitFor("connected")
	if !p.Connected() {
		t.Error("not connected")
	}
	show.Set(false)
	a.Settle()
	if p.Connected() {
		t.Error("still connected after the owner went")
	}
}

// A connection that fails, or takes too long, says so.
func TestConnectFails(t *testing.T) {
	var bad, slow, closed *bluetooth.Connection
	a, _ := mount(t, func() ui.Node {
		bad = bluetooth.Connect(bluetooth.Device{ID: "nowhere"}, bluetooth.ConnectOptions{})
		slow = bluetooth.Connect(bluetooth.Device{ID: "DD:00"}, bluetooth.ConnectOptions{Timeout: time.Millisecond})
		closed = bluetooth.Connect(bluetooth.Device{ID: "DD:00"}, bluetooth.ConnectOptions{})
		closed.Close()
		return blank()
	}, func(bt *fake.Bluetooth) {
		bt.Add(&fake.Peripheral{ID: "DD:00", ConnectError: "timeout"})
	})
	a.Until("both done", func() bool {
		return bad.State().Get() == bluetooth.Disconnected && slow.State().Get() == bluetooth.Disconnected
	})
	if !errors.Is(bad.Err(), bluetooth.ErrNotFound) || !errors.Is(slow.Err(), bluetooth.ErrTimeout) {
		t.Errorf("errors %v, %v", bad.Err(), slow.Err())
	}
	var de *bluetooth.DisconnectError
	if !errors.As(closed.Err(), &de) || de.Reason != bluetooth.ReasonClosed {
		t.Errorf("closed: %v", closed.Err())
	}
	if _, err := call(a, func() ([]bluetooth.Service, error) { return bad.Services() }); !errors.Is(err, bluetooth.ErrNotFound) {
		t.Errorf("services of a failed connection: %v", err)
	}
}

func TestDevices(t *testing.T) {
	a, _ := mount(t, blank, func(bt *fake.Bluetooth) {
		bt.Add(hr())
		p := hr()
		p.ID, p.SystemConnected = "EE:00", true
		bt.Add(p)
	})
	ds, err := call(a, func() ([]bluetooth.Device, error) { return bluetooth.Known("AA:00", "gone") })
	if err != nil || len(ds) != 1 || ds[0].ID != "AA:00" {
		t.Errorf("known %v, %v", ds, err)
	}
	ds, err = call(a, func() ([]bluetooth.Device, error) { return bluetooth.ConnectedDevices(bluetooth.HeartRate) })
	if err != nil || len(ds) != 1 || ds[0].ID != "EE:00" {
		t.Errorf("connected %v, %v", ds, err)
	}
	d, err := call(a, func() (bluetooth.Device, error) {
		return bluetooth.Choose(bluetooth.ChooseOptions{Filters: []bluetooth.Filter{{NamePrefix: "H", Manufacturer: 0x004c, HasManufacturer: true}}})
	})
	if err != nil || d.ID != "AA:00" {
		t.Errorf("chose %v, %v", d, err)
	}
	_, err = call(a, func() (bluetooth.Device, error) {
		return bluetooth.Choose(bluetooth.ChooseOptions{Filters: []bluetooth.Filter{{Name: "Nobody"}}})
	})
	if !errors.Is(err, bluetooth.ErrCanceled) {
		t.Errorf("no match: %v", err)
	}
}

// The app's server answers reads (from the offset) and writes, knows
// its subscribers, and notifies them.
func TestServer(t *testing.T) {
	var writes [][]byte
	var subs []bool
	name := &bluetooth.LocalCharacteristic{UUID: bluetooth.DeviceName, Properties: bluetooth.Read, Permissions: bluetooth.Readable,
		OnRead: func(r bluetooth.ServerRequest) ([]byte, error) { return []byte("Asuka"), nil }}
	ctl := &bluetooth.LocalCharacteristic{UUID: bluetooth.HeartRateControlPoint, Properties: bluetooth.Write | bluetooth.Notify, Permissions: bluetooth.Writable,
		OnWrite: func(r bluetooth.ServerRequest, v []byte) error {
			if len(v) == 0 {
				return bluetooth.InvalidAttributeValueLength
			}
			writes = append(writes, v)
			return nil
		},
		OnSubscribe: func(c bluetooth.Device, on bool) { subs = append(subs, on) }}
	fixed := &bluetooth.LocalCharacteristic{UUID: bluetooth.ModelNumber, Properties: bluetooth.Read, Permissions: bluetooth.Readable, Value: []byte("M1")}
	show := signal.New(true)
	var stopped error
	a, bt := mount(t, func() ui.Node {
		return ui.Show(show, func() ui.Node {
			bluetooth.Serve([]*bluetooth.LocalService{{UUID: bluetooth.GenericAccess, Characteristics: []*bluetooth.LocalCharacteristic{name, ctl, fixed}}},
				func(err error) { stopped = err })
			bluetooth.Advertise(bluetooth.AdvertiseOptions{LocalName: "Asuka", ManufacturerData: map[uint16][]byte{0xffff: {1}}}, nil)
			return blank()
		})
	}, nil)
	a.Until("serving", func() bool { return len(bt.Servers) == 1 && len(bt.Advertising) == 1 })
	for _, ad := range bt.Advertising {
		if !ad.Connectable || string(ad.ManufacturerData["65535"]) != "\x01" {
			t.Errorf("advertising %+v", ad)
		}
	}
	type read struct {
		v    []byte
		code int
	}
	var reads []read
	bt.CentralRead("phone", "2a00", func(v []byte, code int) { reads = append(reads, read{v, code}) })
	bt.CentralRead("phone", "2a24", func(v []byte, code int) { reads = append(reads, read{v, code}) })
	bt.CentralRead("phone", "2a39", func(v []byte, code int) { reads = append(reads, read{v, code}) })
	a.Until("the reads", func() bool { return len(reads) == 3 })
	if string(reads[0].v) != "Asuka" || string(reads[1].v) != "M1" || reads[2].code != int(bluetooth.ReadNotPermitted) {
		t.Errorf("reads %+v", reads)
	}
	var codes []int
	bt.CentralWrite("phone", "2a39", []byte{1}, true, func(code int) { codes = append(codes, code) })
	bt.CentralWrite("phone", "2a39", nil, true, func(code int) { codes = append(codes, code) })
	a.Until("the writes", func() bool { return len(codes) == 2 })
	if codes[0] != 0 || codes[1] != int(bluetooth.InvalidAttributeValueLength) || len(writes) != 1 {
		t.Errorf("writes %v %v", codes, writes)
	}
	bt.CentralSubscribe("phone", "2a39", true)
	a.Until("subscribed", func() bool { return len(ctl.Subscribers()) == 1 })
	ctl.Notify([]byte{5})
	a.Until("notified", func() bool { return len(bt.Notified) == 1 })
	bt.CentralSubscribe("phone", "2a39", false)
	a.Until("unsubscribed", func() bool { return len(ctl.Subscribers()) == 0 })
	if len(subs) != 2 || !subs[0] || subs[1] {
		t.Errorf("subscriptions %v", subs)
	}
	show.Set(false)
	a.Settle()
	if len(bt.Servers) != 0 || len(bt.Advertising) != 0 || stopped != nil {
		t.Errorf("after the owner went: %v %v %v", bt.Servers, bt.Advertising, stopped)
	}
}

// Where there's no peripheral role, serving and advertising say so.
func TestNoPeripheralRole(t *testing.T) {
	var errs []error
	a, _ := mount(t, func() ui.Node {
		bluetooth.Serve(nil, func(err error) { errs = append(errs, err) })
		bluetooth.Advertise(bluetooth.AdvertiseOptions{}, func(err error) { errs = append(errs, err) })
		return blank()
	}, func(bt *fake.Bluetooth) { bt.NoPeripheralRole = true })
	a.Until("both failed", func() bool { return len(errs) == 2 })
	for _, err := range errs {
		if !errors.Is(err, bluetooth.ErrNotAvailable) {
			t.Errorf("%v", err)
		}
	}
}

func TestRestore(t *testing.T) {
	var got []bluetooth.Restored
	a, _ := mount(t, func() ui.Node {
		bluetooth.OnRestore(func(r bluetooth.Restored) { got = append(got, r) })
		return blank()
	}, func(bt *fake.Bluetooth) {
		bt.Restored = &spec.Restored{Devices: []spec.Device{{ID: "AA:00"}}, ScanServices: []string{"180d"}}
	})
	a.Until("restored", func() bool { return len(got) == 1 })
	if r := got[0]; len(r.Devices) != 1 || r.Devices[0].ID != "AA:00" || len(r.ScanServices) != 1 || r.ScanServices[0] != bluetooth.HeartRate {
		t.Errorf("restored %+v", r)
	}
}

// Without the module (a host built without it), the state is
// unsupported and calls aren't available.
func TestNoModule(t *testing.T) {
	a := uitest.Mount(t, blank)
	s, err := call(a, bluetooth.Current)
	if s != bluetooth.Unsupported || err != nil {
		t.Errorf("state %v, %v", s, err)
	}
	if _, err := call(a, func() (bluetooth.Device, error) { return bluetooth.Choose(bluetooth.ChooseOptions{}) }); !errors.Is(err, bluetooth.ErrNotAvailable) {
		t.Errorf("choose %v", err)
	}
}
