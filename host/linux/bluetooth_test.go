//go:build linux && !android

package linux

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	"github.com/egoist/asuka/internal/bluez"
	. "github.com/egoist/asuka/internal/gtk"
)

// The bluetooth module against a BlueZ of the test's own: org.bluez on
// the session bus (a connection of its own), with an adapter, a heart
// rate monitor to scan, connect to and read, and the calls BlueZ makes
// of the app's advertisement and GATT application.

const fakeBluezXML = `<node>
 <interface name="org.bluez.Adapter1">
  <method name="SetDiscoveryFilter"><arg name="filter" type="a{sv}" direction="in"/></method>
  <method name="StartDiscovery"/>
  <method name="StopDiscovery"/>
 </interface>
 <interface name="org.bluez.Device1">
  <method name="Connect"/>
  <method name="Disconnect"/>
 </interface>
 <interface name="org.bluez.LEAdvertisingManager1">
  <method name="RegisterAdvertisement"><arg name="advertisement" type="o" direction="in"/><arg name="options" type="a{sv}" direction="in"/></method>
  <method name="UnregisterAdvertisement"><arg name="advertisement" type="o" direction="in"/></method>
 </interface>
 <interface name="org.bluez.GattManager1">
  <method name="RegisterApplication"><arg name="application" type="o" direction="in"/><arg name="options" type="a{sv}" direction="in"/></method>
  <method name="UnregisterApplication"><arg name="application" type="o" direction="in"/></method>
 </interface>
</node>`

type objPath string

// gv is a Go value as a GVariant (floating).
func gv(v any) Ptr {
	switch x := v.(type) {
	case string:
		return VStr(x)
	case objPath:
		return VPath(string(x))
	case bool:
		return VBool(x)
	case int16:
		return Call(Sym("g_variant_new_int16"), uintptr(int64(x)))
	case uint16:
		return VU16(x)
	case []string:
		return VStrings(x)
	case []byte:
		return VByteArray(x)
	case map[string]any:
		m := map[string]Ptr{}
		for k, v := range x {
			m[k] = gv(v)
		}
		return VDict(m)
	}
	panic(fmt.Sprintf("gv: %T", v))
}

const (
	fakeAdapter = "/org/bluez/hci0"
	fakeDevice  = fakeAdapter + "/dev_AA_BB_CC_DD_EE_FF"
	fakeService = fakeDevice + "/service000a"
	fakeChar    = fakeService + "/char000b"
	fakeDesc    = fakeChar + "/desc000d"
	fakeAddr    = "AA:BB:CC:DD:EE:FF"
)

type fakeBluez struct {
	conn    Ptr
	objects map[string]map[string]map[string]any
	calls   []string
	filter  map[string]any
	written []byte
	options map[string]any
	// The app's, as BlueZ read them.
	advert  map[string]any
	app     map[string]any
	appRead []byte
	appErr  error
	sender  string
}

func (f *fakeBluez) emit(path, iface, name string, params Ptr) {
	if err := DBusEmit(f.conn, path, iface, name, params); err != nil {
		panic(err)
	}
}

// set changes properties, as BlueZ tells.
func (f *fakeBluez) set(path, iface string, props map[string]any) {
	for k, v := range props {
		f.objects[path][iface][k] = v
	}
	f.emit(path, ifProperties, "PropertiesChanged", VTuple(VStr(iface), gv(props), VArray("s")))
}

// add adds an object, as BlueZ tells.
func (f *fakeBluez) add(path string, ifs map[string]map[string]any) {
	f.objects[path] = ifs
	f.emit("/", ifObjectManager, "InterfacesAdded", VTuple(VPath(path), f.ifaces(ifs)))
}

func (f *fakeBluez) ifaces(ifs map[string]map[string]any) Ptr {
	var names []string
	for n := range ifs {
		names = append(names, n)
	}
	sort.Strings(names)
	var items []Ptr
	for _, n := range names {
		items = append(items, VEntry(VStr(n), gv(ifs[n])))
	}
	return VArray("{sa{sv}}", items...)
}

func (f *fakeBluez) managed() Ptr {
	var paths []string
	for p := range f.objects {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var items []Ptr
	for _, p := range paths {
		items = append(items, VEntry(VPath(p), f.ifaces(f.objects[p])))
	}
	return VTuple(VArray("{oa{sa{sv}}}", items...))
}

func (f *fakeBluez) method(c DBusInvocation) {
	f.calls = append(f.calls, c.Method)
	args, _ := VariantGoBytes(c.Params).([]any)
	switch c.Method {
	case "GetManagedObjects":
		c.Return(f.managed())
		return
	case "SetDiscoveryFilter":
		f.filter, _ = args[0].(map[string]any)
	case "StartDiscovery":
		f.set(fakeAdapter, ifAdapter, map[string]any{"Discovering": true})
	case "StopDiscovery":
		f.set(fakeAdapter, ifAdapter, map[string]any{"Discovering": false})
	case "Connect":
		f.set(fakeDevice, ifDevice, map[string]any{"Connected": true})
		f.add(fakeService, map[string]map[string]any{ifGattService: {"UUID": spec.Normalize("180d"), "Primary": true, "Device": objPath(fakeDevice)}})
		f.add(fakeChar, map[string]map[string]any{ifGattChar: {"UUID": spec.Normalize("2a37"), "Service": objPath(fakeService),
			"Flags": []string{"read", "write", "notify"}, "MTU": uint16(185)}})
		f.add(fakeDesc, map[string]map[string]any{ifGattDesc: {"UUID": spec.Normalize("2902"), "Characteristic": objPath(fakeChar)}})
		f.set(fakeDevice, ifDevice, map[string]any{"ServicesResolved": true})
	case "ReadValue":
		c.Return(VTuple(VByteArray([]byte{1, 2, 3})))
		return
	case "WriteValue":
		f.written = bluez.Bytes(args[0])
		f.options, _ = args[1].(map[string]any)
	case "RegisterAdvertisement":
		path, _ := args[0].(string)
		DBusCall(f.conn, c.Sender, path, ifProperties, "GetAll", VTuple(VStr(ifAdvert)), 5000, func(r Ptr, err error) {
			if err != nil {
				c.Error("org.bluez.Error.Failed", err.Error())
				return
			}
			a, _ := VariantGoBytes(r).([]any)
			f.advert, _ = a[0].(map[string]any)
			c.Return(0)
		})
		return
	case "RegisterApplication":
		root, _ := args[0].(string)
		f.sender = c.Sender
		DBusCall(f.conn, c.Sender, root, ifObjectManager, "GetManagedObjects", 0, 5000, func(r Ptr, err error) {
			if err != nil {
				c.Error("org.bluez.Error.Failed", err.Error())
				return
			}
			a, _ := VariantGoBytes(r).([]any)
			f.app, _ = a[0].(map[string]any)
			c.Return(0)
		})
		return
	}
	c.Return(0)
}

// startFakeBluez exports the fake on a connection of its own to the
// session bus, owning org.bluez, and points the module at that bus.
func startFakeBluez(t *testing.T, h *Host) *fakeBluez {
	f := &fakeBluez{objects: map[string]map[string]map[string]any{
		fakeAdapter: {
			ifAdapter:     {"Address": "00:11:22:33:44:55", "Powered": true, "Discovering": false},
			ifAdvManager:  {},
			ifGattManager: {},
		},
		fakeDevice: {ifDevice: {"Address": fakeAddr, "Adapter": objPath(fakeAdapter), "Name": "Polar H10", "Alias": "Polar H10",
			"Connected": false, "ServicesResolved": false, "UUIDs": []string{spec.Normalize("180d")}}},
	}}
	var owned bool
	onMain(func() {
		conn, err := privateSessionBus()
		if err != nil {
			fatalf(t, "session bus: %v", err)
		}
		f.conn = conn
		reg := func(path, xml, iface string) {
			if _, err := DBusRegister(conn, path, xml, iface, DBusObject{Method: f.method}); err != nil {
				fatalf(t, "export %s: %v", iface, err)
			}
		}
		reg("/", btObjectsXML, ifObjectManager)
		reg(fakeAdapter, fakeBluezXML, ifAdapter)
		reg(fakeAdapter, fakeBluezXML, ifAdvManager)
		reg(fakeAdapter, fakeBluezXML, ifGattManager)
		reg(fakeDevice, fakeBluezXML, ifDevice)
		reg(fakeChar, btObjectsXML, ifGattChar)
		DBusCall(conn, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName",
			VTuple(VStr(bluezName), VU32(4)), 5000, func(_ Ptr, err error) {
				if err != nil {
					t.Errorf("RequestName: %v", err)
				}
				owned = true
			})
		btBus = func() (Ptr, error) { return Bus(BusSession) }
		b := &blue
		for _, id := range b.subs {
			DBusUnsubscribe(b.conn, id)
		}
		blue = bluetooth{}
		h.bt()
	})
	waitFor(t, "org.bluez owned", func() bool { return owned })
	t.Cleanup(func() {
		onMain(func() {
			for _, id := range blue.subs {
				DBusUnsubscribe(blue.conn, id)
			}
			blue = bluetooth{}
			btBus = func() (Ptr, error) { return Bus(BusSystem) }
			closeBus(f.conn)
		})
	})
	return f
}

func btCall(t *testing.T, h *Host, name string, args any) ([]byte, error) {
	t.Helper()
	b, _ := json.Marshal(args)
	return awaitResult(t, "bluetooth."+name, callAsync(h, "bluetooth", name, b))
}

func btMust(t *testing.T, h *Host, name string, args any, out any) []byte {
	t.Helper()
	r, err := btCall(t, h, name, args)
	if err != nil {
		t.Fatalf("bluetooth.%s: %v", name, err)
	}
	if out != nil {
		if err := json.Unmarshal(r, out); err != nil {
			t.Fatalf("bluetooth.%s: %v (%s)", name, err, r)
		}
	}
	return r
}

// btEvent waits for an event of the type, with "next".
func btEvent(t *testing.T, h *Host, typ string) spec.Event {
	t.Helper()
	for i := 0; i < 10; i++ {
		var evs []spec.Event
		btMust(t, h, "next", struct{}{}, &evs)
		for _, e := range evs {
			if e.Type == typ {
				return e
			}
		}
	}
	t.Fatalf("no %s event", typ)
	return spec.Event{}
}

func TestBluetoothCentral(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	f := startFakeBluez(t, h)

	var st spec.State
	btMust(t, h, "state", struct{}{}, &st)
	if st.State != spec.StateOn {
		t.Fatalf("state %q", st.State)
	}
	var known []spec.Device
	btMust(t, h, "known", spec.KnownArgs{IDs: []string{"aa:bb:cc:dd:ee:ff", "11:11:11:11:11:11"}}, &known)
	if !reflect.DeepEqual(known, []spec.Device{{ID: fakeAddr, Name: "Polar H10"}}) {
		t.Errorf("known %+v", known)
	}

	// A scan: the filter set, discovery started; the device heard as its
	// RSSI comes.
	btMust(t, h, "scan", spec.ScanArgs{ID: 1, Services: []string{"180d"}}, nil)
	onMain(func() {
		if got, ok := f.filter["Transport"]; !ok || got != "le" || !reflect.DeepEqual(f.filter["UUIDs"], []any{spec.Normalize("180d")}) {
			t.Errorf("discovery filter %v", f.filter)
		}
		f.set(fakeDevice, ifDevice, map[string]any{"RSSI": int16(-60)})
	})
	e := btEvent(t, h, spec.EventAdvertisement)
	if e.Scan != 1 || e.Advertisement == nil || e.Advertisement.Device.ID != fakeAddr || e.Advertisement.RSSI != -60 ||
		e.Advertisement.LocalName != "Polar H10" || e.Advertisement.Connectable != nil {
		t.Errorf("advertisement %+v %+v", e, e.Advertisement)
	}
	// A second scan, of duplicates: it hears the device heard already at
	// once, then each advertisement; the first, once per device, doesn't.
	btMust(t, h, "scan", spec.ScanArgs{ID: 2, Duplicates: true}, nil)
	onMain(func() {
		if f.filter["DuplicateData"] != true || f.filter["UUIDs"] != nil {
			t.Errorf("merged discovery filter %v", f.filter)
		}
	})
	if e := btEvent(t, h, spec.EventAdvertisement); e.Scan != 2 || e.Advertisement.Device.ID != fakeAddr {
		t.Errorf("second scan's advertisement %+v", e)
	}
	onMain(func() { f.set(fakeDevice, ifDevice, map[string]any{"RSSI": int16(-50)}) })
	var evs []spec.Event
	btMust(t, h, "next", struct{}{}, &evs)
	if len(evs) != 1 || evs[0].Scan != 2 || evs[0].Advertisement.RSSI != -50 {
		t.Errorf("an advertisement again: %+v", evs)
	}
	btMust(t, h, "stopScan", spec.ID{ID: 2}, nil)
	btMust(t, h, "stopScan", spec.ID{ID: 1}, nil)
	waitFor(t, "StopDiscovery", func() bool { return f.calls[len(f.calls)-1] == "StopDiscovery" })

	// Connected, its services, a read, a write, a notification.
	btMust(t, h, "connect", spec.ConnectArgs{Conn: 1, Device: fakeAddr}, nil)
	var svcs []spec.Service
	btMust(t, h, "services", spec.Conn{Conn: 1}, &svcs)
	if len(svcs) != 1 || len(svcs[0].Characteristics) != 1 || len(svcs[0].Characteristics[0].Descriptors) != 1 {
		t.Fatalf("services %+v", svcs)
	}
	ch := svcs[0].Characteristics[0]
	if svcs[0].UUID != spec.Normalize("180d") || !svcs[0].Primary || ch.UUID != spec.Normalize("2a37") ||
		ch.Properties != spec.PropRead|spec.PropWrite|spec.PropNotify || ch.Descriptors[0].UUID != spec.Normalize("2902") {
		t.Errorf("services %+v", svcs)
	}
	var again []spec.Service
	btMust(t, h, "services", spec.Conn{Conn: 1}, &again)
	if !reflect.DeepEqual(again, svcs) {
		t.Errorf("services again: other handles %+v", again)
	}
	if v := btMust(t, h, "read", spec.Attr{Conn: 1, Handle: ch.Handle}, nil); !bytes.Equal(v, []byte{1, 2, 3}) {
		t.Errorf("read %v", v)
	}
	if _, err := btCall(t, h, "read", spec.Attr{Conn: 1, Handle: 999}); !errIs(err, "not found") {
		t.Errorf("read of no handle: %v", err)
	}
	btMust(t, h, "write", spec.Write{Conn: 1, Handle: ch.Handle, Value: []byte{7, 0}, Response: true}, nil)
	onMain(func() {
		if !bytes.Equal(f.written, []byte{7, 0}) || f.options["type"] != "request" {
			t.Errorf("written %v %v", f.written, f.options)
		}
	})
	var mtu spec.MTU
	btMust(t, h, "mtu", spec.Conn{Conn: 1}, &mtu)
	if mtu != (spec.MTU{MTU: 185, WriteWithResponse: 182, WriteWithoutResponse: 182}) {
		t.Errorf("mtu %+v", mtu)
	}
	btMust(t, h, "subscribe", spec.Subscribe{Conn: 1, Handle: ch.Handle}, nil)
	onMain(func() { f.set(fakeChar, ifGattChar, map[string]any{"Value": []byte{9, 8}}) })
	e = btEvent(t, h, spec.EventValue)
	if e.Conn != 1 || e.Handle != ch.Handle || !bytes.Equal(e.Value, []byte{9, 8}) {
		t.Errorf("value %+v", e)
	}

	// Its services change (BlueZ's Service Changed): old handles are
	// gone; "services" again has the new.
	onMain(func() {
		f.add(fakeDevice+"/service0020", map[string]map[string]any{ifGattService: {"UUID": spec.Normalize("180f"),
			"Primary": true, "Device": objPath(fakeDevice)}})
	})
	if e := btEvent(t, h, spec.EventServicesChanged); e.Conn != 1 {
		t.Errorf("servicesChanged %+v", e)
	}
	if _, err := btCall(t, h, "read", spec.Attr{Conn: 1, Handle: ch.Handle}); !errIs(err, "not found") {
		t.Errorf("read of an old handle: %v", err)
	}
	btMust(t, h, "services", spec.Conn{Conn: 1}, &svcs)
	if len(svcs) != 2 || svcs[1].UUID != spec.Normalize("180f") {
		t.Errorf("services after the change %+v", svcs)
	}
	onMain(func() {
		if f.calls[len(f.calls)-1] != "StopNotify" {
			t.Errorf("the subscription not ended with the old handles: %v", f.calls)
		}
	})

	// The device goes: the reason BlueZ's Disconnected gave.
	onMain(func() {
		f.emit(fakeDevice, ifDevice, "Disconnected", VTuple(VStr("org.bluez.Reason.Remote"), VStr("Connection terminated by remote user")))
		f.set(fakeDevice, ifDevice, map[string]any{"Connected": false, "ServicesResolved": false})
	})
	e = btEvent(t, h, spec.EventDisconnected)
	if e.Conn != 1 || e.Reason != spec.ReasonRemote || e.Error != "Connection terminated by remote user" {
		t.Errorf("disconnected %+v", e)
	}
	if _, err := btCall(t, h, "read", spec.Attr{Conn: 1, Handle: ch.Handle}); !errIs(err, "disconnected") {
		t.Errorf("read after: %v", err)
	}

	// The adapter off: state, and scans fail with it.
	onMain(func() { f.set(fakeAdapter, ifAdapter, map[string]any{"Powered": false}) })
	if e := btEvent(t, h, spec.EventState); e.State != spec.StateOff {
		t.Errorf("state event %+v", e)
	}
	if _, err := btCall(t, h, "scan", spec.ScanArgs{ID: 2}); !errIs(err, "off") {
		t.Errorf("scan while off: %v", err)
	}
	if _, err := btCall(t, h, "choose", spec.ChooseArgs{}); err == nil || !strings.HasPrefix(err.Error(), "not available: ") {
		t.Errorf("choose: %v", err)
	}
}

func TestBluetoothPeripheral(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	f := startFakeBluez(t, h)

	// An advertisement: BlueZ reads its properties.
	btMust(t, h, "advertise", spec.AdvertiseArgs{ID: 1, LocalName: "asuka", Services: []string{"180d"},
		ManufacturerData: map[string][]byte{"76": {1, 2}}, Connectable: true, IncludeTxPower: true}, nil)
	onMain(func() {
		a := f.advert
		if a["Type"] != "peripheral" || a["LocalName"] != "asuka" || !reflect.DeepEqual(a["ServiceUUIDs"], []any{"180d"}) ||
			!reflect.DeepEqual(a["ManufacturerData"], map[string]any{"76": []byte{1, 2}}) ||
			!reflect.DeepEqual(a["Includes"], []any{"tx-power"}) || a["Discoverable"] != true {
			t.Errorf("advertisement as BlueZ reads it: %v", a)
		}
	})

	// A server: BlueZ reads its objects, then a central reads one, Go
	// answering.
	btMust(t, h, "serve", spec.ServeArgs{ID: 1, Services: []spec.LocalService{{UUID: "180d", Primary: true,
		Characteristics: []spec.LocalCharacteristic{
			{Handle: 7, UUID: "2a37", Properties: spec.PropRead | spec.PropNotify, Permissions: spec.PermRead},
			{Handle: 8, UUID: "2a38", Properties: spec.PropWrite, Permissions: spec.PermWriteEncrypted,
				Descriptors: []spec.LocalDescriptor{{UUID: "2901", Value: []byte("loc")}}},
		}}}}, nil)
	var char7, char8 string
	onMain(func() {
		for path, v := range f.app {
			ifs, _ := v.(map[string]any)
			if c, ok := ifs[ifGattChar].(map[string]any); ok {
				switch c["UUID"] {
				case spec.Normalize("2a37"):
					char7 = path
					if !reflect.DeepEqual(c["Flags"], []any{"read", "notify"}) {
						t.Errorf("flags %v", c["Flags"])
					}
				case spec.Normalize("2a38"):
					char8 = path
					if !reflect.DeepEqual(c["Flags"], []any{"encrypt-write"}) {
						t.Errorf("flags %v", c["Flags"])
					}
				}
			}
		}
		if len(f.app) != 4 || char7 == "" || char8 == "" {
			t.Errorf("the application as BlueZ reads it: %v", f.app)
		}
	})
	done := false
	onMain(func() {
		opts := VDict(map[string]Ptr{"device": VPath(fakeDevice), "offset": VU16(1), "mtu": VU16(185)})
		DBusCall(f.conn, f.sender, char7, ifGattChar, "ReadValue", VTuple(opts), 5000, func(r Ptr, err error) {
			done = true
			if err != nil {
				f.appErr = err
				return
			}
			v := Sys.VariantGetChildValue(r, 0)
			f.appRead = VariantBytes(v)
			Sys.VariantUnref(v)
		})
	})
	e := btEvent(t, h, spec.EventRead)
	if e.Server != 1 || e.Handle != 7 || e.Offset != 1 || e.Central == nil || e.Central.ID != fakeAddr {
		t.Errorf("read event %+v", e)
	}
	btMust(t, h, "respond", spec.Respond{Server: 1, Request: e.Request, Value: []byte{5, 6}}, nil)
	waitFor(t, "ReadValue answered", func() bool { return done })
	onMain(func() {
		if f.appErr != nil || !bytes.Equal(f.appRead, []byte{5, 6}) {
			t.Errorf("the central read %v, %v", f.appRead, f.appErr)
		}
	})

	// A write Go refuses: the ATT error BlueZ sends.
	done = false
	onMain(func() {
		opts := VDict(map[string]Ptr{"device": VPath(fakeDevice)})
		DBusCall(f.conn, f.sender, char8, ifGattChar, "WriteValue", VTuple(VByteArray([]byte{1}), opts), 5000, func(_ Ptr, err error) {
			done = true
			f.appErr = err
		})
	})
	e = btEvent(t, h, spec.EventWrite)
	if e.Handle != 8 || !e.Response || !bytes.Equal(e.Value, []byte{1}) {
		t.Errorf("write event %+v", e)
	}
	btMust(t, h, "respond", spec.Respond{Server: 1, Request: e.Request, Error: 0x08}, nil)
	waitFor(t, "WriteValue answered", func() bool { return done })
	onMain(func() {
		if name, _ := bluez.Error(f.appErr); name != "org.bluez.Error.NotAuthorized" {
			t.Errorf("refused write: %v", f.appErr)
		}
	})

	// A subscription, then a notification: PropertiesChanged of Value.
	var notified []byte
	onMain(func() {
		DBusSubscribe(f.conn, f.sender, ifProperties, "PropertiesChanged", char7, func(s DBusSignal) {
			a, _ := VariantGoBytes(s.Params).([]any)
			m, _ := a[1].(map[string]any)
			notified = bluez.Bytes(m["Value"])
		})
		DBusCall(f.conn, f.sender, char7, ifGattChar, "StartNotify", 0, 5000, func(Ptr, error) {})
	})
	e = btEvent(t, h, spec.EventSubscription)
	if e.Handle != 7 || !e.On || e.Central == nil {
		t.Errorf("subscription %+v", e)
	}
	btMust(t, h, "notify", spec.Notify{Server: 1, Handle: 7, Value: []byte{42}}, nil)
	waitFor(t, "the notification", func() bool { return bytes.Equal(notified, []byte{42}) })

	btMust(t, h, "unserve", spec.ID{ID: 1}, nil)
	waitFor(t, "UnregisterApplication", func() bool { return f.calls[len(f.calls)-1] == "UnregisterApplication" })

	// BlueZ releases the advertisement: "advertiseStopped".
	onMain(func() {
		DBusCall(f.conn, f.sender, btBasePath+"/advertisement1", ifAdvert, "Release", 0, 5000, func(Ptr, error) {})
	})
	if e := btEvent(t, h, spec.EventAdvertiseStopped); e.ID != 1 || e.Error == "" {
		t.Errorf("advertiseStopped %+v", e)
	}
}

func TestBluetoothNoBluez(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	onMain(func() {
		btBus = func() (Ptr, error) { return Bus(BusSession) } // where no org.bluez is
		blue = bluetooth{}
		h.bt()
	})
	defer onMain(func() {
		for _, id := range blue.subs {
			DBusUnsubscribe(blue.conn, id)
		}
		blue = bluetooth{}
		btBus = func() (Ptr, error) { return Bus(BusSystem) }
	})
	var st spec.State
	btMust(t, h, "state", struct{}{}, &st)
	if st.State != spec.StateUnsupported {
		t.Errorf("state without BlueZ: %q", st.State)
	}
	if _, err := btCall(t, h, "scan", spec.ScanArgs{ID: 1}); !errIs(err, "unsupported") {
		t.Errorf("scan without BlueZ: %v", err)
	}
	if _, err := btCall(t, h, "advertise", spec.AdvertiseArgs{ID: 1}); !errIs(err, "unsupported") {
		t.Errorf("advertise without BlueZ: %v", err)
	}
	var r spec.Restored
	btMust(t, h, "restore", struct{}{}, &r)
}
