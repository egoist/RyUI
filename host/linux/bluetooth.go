//go:build linux && !android

package linux

import (
	"encoding/json"
	"errors"
	"sort"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	"github.com/egoist/asuka/internal/bluez"
	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sdk/bluetooth on Linux: BlueZ, on the system bus (org.bluez),
// through GDBus, every call asynchronous. The objects BlueZ exports
// (its adapters, the devices it knows, a connected device's GATT
// attributes) are read once with GetManagedObjects and followed with
// InterfacesAdded, InterfacesRemoved and PropertiesChanged; what the
// app does goes through their methods: Adapter1's discovery (the
// scans), Device1's Connect and Disconnect, GattCharacteristic1's and
// GattDescriptor1's ReadValue, WriteValue, StartNotify, StopNotify. As
// a peripheral, the app exports its own objects for BlueZ to call: an
// LEAdvertisement1 it registers with LEAdvertisingManager1, and an
// application (an ObjectManager over GattService1,
// GattCharacteristic1 and GattDescriptor1 objects) it registers with
// GattManager1. The wire is internal/bluetoothspec's; internal/bluez
// maps BlueZ's errors, flags and properties to it.
//
// No bluetoothd on the bus (or no adapter) is "unsupported"; a bus that
// won't let the app talk to it (a Flatpak without
// --system-talk-name=org.bluez) is "denied". Linux asks no permission:
// "request" is "state".

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["bluetooth"] = h.bluetoothModule()
	})
}

const (
	bluezName       = "org.bluez"
	ifAdapter       = "org.bluez.Adapter1"
	ifDevice        = "org.bluez.Device1"
	ifGattService   = "org.bluez.GattService1"
	ifGattChar      = "org.bluez.GattCharacteristic1"
	ifGattDesc      = "org.bluez.GattDescriptor1"
	ifAdvManager    = "org.bluez.LEAdvertisingManager1"
	ifGattManager   = "org.bluez.GattManager1"
	ifAdvert        = "org.bluez.LEAdvertisement1"
	ifProperties    = "org.freedesktop.DBus.Properties"
	ifObjectManager = "org.freedesktop.DBus.ObjectManager"

	// btCallTimeout outlasts BlueZ's own (ATT's 30 seconds), which
	// answers first.
	btCallTimeout = 35000
	// btNoTimeout is G_MAXINT: no timeout but BlueZ's (a connection's).
	btNoTimeout = 0x7fffffff
)

// btObject is an object's interfaces and their properties.
type btObject map[string]map[string]any

// bluetooth is the module's state, the main thread's.
type bluetooth struct {
	h       *Host
	conn    Ptr
	subs    []uint32
	started bool   // subscribed to BlueZ's signals
	loading bool   // GetManagedObjects asked
	loaded  bool   // and answered
	err     string // why BlueZ can't be used: "unsupported", "denied" ("": it can)
	ready   []func()
	objects map[string]btObject
	state   string // the state last told ("": none yet)

	queue   []spec.Event
	waiting func([]byte, error)

	scans    map[uint32]*btScan
	disc     btDiscovery
	conns    map[uint32]*btConn
	devs     map[string]*btDev
	adverts  map[uint32]*btAdvert
	servers  map[uint32]*btServer
	requests map[uint32]*btRequest
	nextReq  uint32
	attrQs   map[string][]func(next func())
	changed  map[string]bool // devices whose GATT objects changed, to tell at idle
}

var blue bluetooth

// btBus is the bus BlueZ is on: the system's (a test's BlueZ: the
// session's).
var btBus = func() (Ptr, error) { return Bus(BusSystem) }

func (h *Host) bt() *bluetooth {
	b := &blue
	if b.h == nil {
		*b = bluetooth{h: h, objects: map[string]btObject{}, scans: map[uint32]*btScan{},
			conns: map[uint32]*btConn{}, devs: map[string]*btDev{}, adverts: map[uint32]*btAdvert{},
			servers: map[uint32]*btServer{}, requests: map[uint32]*btRequest{},
			attrQs: map[string][]func(func()){}, changed: map[string]bool{}}
	}
	return b
}

// start connects to BlueZ, once, then calls then (every call does it
// first: nothing's asked of BlueZ before the app uses Bluetooth).
func (b *bluetooth) start(then func()) {
	if b.loaded {
		then()
		return
	}
	b.ready = append(b.ready, then)
	if b.loading {
		return
	}
	b.loading = true
	if !b.started {
		conn, err := btBus()
		if err != nil {
			// No system bus: no BlueZ (a container, a sandbox without it).
			b.err = spec.StateUnsupported
			if bluez.BusState(err) == spec.StateDenied {
				b.err = spec.StateDenied
			}
			b.loadDone()
			return
		}
		b.conn = conn
		b.started = true
		b.subscribe()
	}
	b.load()
}

// subscribe follows BlueZ: its objects and their properties, the
// devices' disconnection reasons, and bluetoothd coming and going.
func (b *bluetooth) subscribe() {
	c := b.conn
	b.subs = append(b.subs,
		DBusSubscribe(c, bluezName, ifObjectManager, "InterfacesAdded", "", func(s DBusSignal) { b.added(s.Params) }),
		DBusSubscribe(c, bluezName, ifObjectManager, "InterfacesRemoved", "", func(s DBusSignal) { b.removed(s.Params) }),
		DBusSubscribe(c, bluezName, ifProperties, "PropertiesChanged", "", func(s DBusSignal) { b.propsChanged(s.Path, s.Params) }),
		// Device1's (and Bearer.LE1's) Disconnected(reason, message),
		// BlueZ 5.82 on: sent before Connected turns false.
		DBusSubscribe(c, bluezName, "", "Disconnected", "", func(s DBusSignal) { b.disconnectedSignal(s.Path, s.Params) }),
		DBusSubscribe(c, "org.freedesktop.DBus", "org.freedesktop.DBus", "NameOwnerChanged", "/org/freedesktop/DBus", func(s DBusSignal) {
			a, _ := VariantGo(s.Params).([]any)
			if len(a) < 3 {
				return
			}
			if name, _ := a[0].(string); name != bluezName {
				return
			}
			if owner, _ := a[2].(string); owner == "" {
				b.bluezGone()
			} else {
				b.reload()
			}
		}),
	)
}

// load reads BlueZ's objects.
func (b *bluetooth) load() {
	DBusCall(b.conn, bluezName, "/", ifObjectManager, "GetManagedObjects", 0, 10000, func(r Ptr, err error) {
		b.objects = map[string]btObject{}
		b.err = ""
		if err != nil {
			b.err = bluez.BusState(err)
			if b.err == "" {
				b.err = spec.StateUnsupported
			}
		} else if a, _ := VariantGoBytes(r).([]any); len(a) > 0 {
			objs, _ := a[0].(map[string]any)
			for path, ifs := range objs {
				b.objects[path] = btIfaces(ifs)
			}
		}
		b.loadDone()
	})
}

func (b *bluetooth) loadDone() {
	b.loading, b.loaded = false, true
	b.updateState()
	rs := b.ready
	b.ready = nil
	for _, f := range rs {
		f()
	}
}

// reload reads BlueZ's objects again: bluetoothd started (again).
func (b *bluetooth) reload() {
	if b.loading || !b.started {
		return
	}
	b.loaded = false
	b.loading = true
	b.load()
}

// bluezGone: bluetoothd left the bus; everything of it is gone.
func (b *bluetooth) bluezGone() {
	if !b.loaded {
		return
	}
	b.objects = map[string]btObject{}
	b.err = spec.StateUnsupported
	b.updateState()
}

func btIfaces(v any) btObject {
	m, _ := v.(map[string]any)
	o := btObject{}
	for iface, props := range m {
		p, _ := props.(map[string]any)
		if p == nil {
			p = map[string]any{}
		}
		o[iface] = p
	}
	return o
}

// added is InterfacesAdded(o path, a{sa{sv}} interfaces).
func (b *bluetooth) added(params Ptr) {
	if !b.loaded {
		return // GetManagedObjects, asked, has it
	}
	a, _ := VariantGoBytes(params).([]any)
	if len(a) < 2 {
		return
	}
	path, _ := a[0].(string)
	ifs := btIfaces(a[1])
	o := b.objects[path]
	if o == nil {
		o = btObject{}
		b.objects[path] = o
	}
	for iface, props := range ifs {
		o[iface] = props
	}
	switch {
	case ifs[ifAdapter] != nil:
		b.updateState()
	case ifs[ifDevice] != nil:
		b.heard(path, ifs[ifDevice], true)
		if ifs[ifDevice]["Connected"] == true {
			b.deviceConnected(path)
		}
	case ifs[ifGattService] != nil || ifs[ifGattChar] != nil || ifs[ifGattDesc] != nil:
		b.gattChanged(path)
	}
	b.flush()
}

// removed is InterfacesRemoved(o path, as interfaces).
func (b *bluetooth) removed(params Ptr) {
	if !b.loaded {
		return
	}
	a, _ := VariantGo(params).([]any)
	if len(a) < 2 {
		return
	}
	path, _ := a[0].(string)
	o := b.objects[path]
	if o == nil {
		return
	}
	gatt, adapter, device := false, false, false
	for _, iface := range bluez.Strings(a[1]) {
		if o[iface] == nil {
			continue
		}
		delete(o, iface)
		switch iface {
		case ifAdapter:
			adapter = true
		case ifDevice:
			device = true
		case ifGattService, ifGattChar, ifGattDesc:
			gatt = true
		}
	}
	if len(o) == 0 {
		delete(b.objects, path)
	}
	if device {
		b.deviceDisconnected(path)
	}
	if adapter {
		b.updateState()
	}
	if gatt {
		b.gattChanged(path)
	}
	b.flush()
}

// propsChanged is PropertiesChanged(s interface, a{sv} changed, as
// invalidated) of the object at path.
func (b *bluetooth) propsChanged(path string, params Ptr) {
	if !b.loaded {
		return
	}
	a, _ := VariantGoBytes(params).([]any)
	if len(a) < 3 {
		return
	}
	iface, _ := a[0].(string)
	changed, _ := a[1].(map[string]any)
	o := b.objects[path]
	if o == nil {
		o = btObject{}
		b.objects[path] = o
	}
	p := o[iface]
	if p == nil {
		p = map[string]any{}
		o[iface] = p
	}
	for k, v := range changed {
		p[k] = v
	}
	invalidated := bluez.Strings(a[2])
	for _, k := range invalidated {
		delete(p, k)
	}
	switch iface {
	case ifAdapter:
		if _, ok := changed["Powered"]; ok || btHas(changed, "PowerState") || btHasAny(invalidated, "Powered") {
			b.updateState()
		}
	case ifDevice:
		if v, ok := changed["Connected"]; ok {
			if v == true {
				b.deviceConnected(path)
			} else {
				b.deviceDisconnected(path)
			}
		}
		if v, ok := changed["ServicesResolved"]; ok && v == true {
			b.servicesResolved(path)
		}
		b.heard(path, changed, false)
	case ifGattChar:
		if v, ok := changed["Value"]; ok {
			b.notified(path, bluez.Bytes(v))
		}
	}
	b.flush()
}

func btHas(m map[string]any, k string) bool { _, ok := m[k]; return ok }

func btHasAny(ss []string, k string) bool {
	for _, s := range ss {
		if s == k {
			return true
		}
	}
	return false
}

// adapter is the adapter the app uses: the first (hci0), "" if none.
func (b *bluetooth) adapter() string {
	var paths []string
	for p, o := range b.objects {
		if o[ifAdapter] != nil {
			paths = append(paths, p)
		}
	}
	if len(paths) == 0 {
		return ""
	}
	sort.Strings(paths)
	return paths[0]
}

func (b *bluetooth) props(path, iface string) map[string]any {
	if o := b.objects[path]; o != nil {
		return o[iface]
	}
	return nil
}

// current is the adapter's state.
func (b *bluetooth) current() string {
	if b.err != "" {
		return b.err
	}
	a := b.adapter()
	if a == "" {
		return spec.StateUnsupported
	}
	p := b.props(a, ifAdapter)
	if on, _ := p["Powered"].(bool); !on {
		return spec.StateOff
	}
	// BlueZ 5.66 on: turning off ("on-disabling") is off already.
	if s, _ := p["PowerState"].(string); s != "" && s != "on" {
		return spec.StateOff
	}
	return spec.StateOn
}

// updateState tells a change of state, and ends what it ends: the scans
// and the connections when it's no longer on; the advertisements and
// the servers too when BlueZ (or the adapter) went.
func (b *bluetooth) updateState() {
	s := b.current()
	if s == b.state {
		return
	}
	told := b.state != ""
	b.state = s
	if told {
		b.queue = append(b.queue, spec.Event{Type: spec.EventState, State: s})
	}
	if s != spec.StateOn {
		for _, id := range btKeys(b.scans) {
			b.endScan(id, s)
		}
		b.disc = btDiscovery{busy: b.disc.busy} // BlueZ forgets the discovery
		reason := spec.ReasonOff
		if s != spec.StateOff {
			reason = spec.ReasonUnknown
		}
		for _, id := range btKeys(b.conns) {
			c := b.conns[id]
			if c.up {
				b.queue = append(b.queue, spec.Event{Type: spec.EventDisconnected, Conn: id, Reason: reason, Error: "Bluetooth is " + s})
			}
			b.endConn(c, s)
		}
	}
	if s == spec.StateUnsupported || s == spec.StateDenied {
		for _, id := range btKeys(b.adverts) {
			b.endAdvert(id, s)
		}
		for _, id := range btKeys(b.servers) {
			b.endServer(id, s)
		}
	}
	b.flush()
}

func btKeys[V any](m map[uint32]V) []uint32 {
	out := make([]uint32, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// flush answers a waiting "next" with the events queued.
func (b *bluetooth) flush() {
	if b.waiting == nil || len(b.queue) == 0 {
		return
	}
	out, _ := json.Marshal(b.queue)
	b.queue = nil
	w := b.waiting
	b.waiting = nil
	w(out, nil)
}

// enqueue runs f once the calls on the attribute at path before it are
// done (it calls next when it is): BlueZ refuses a second read (or
// write) of an attribute while one runs.
func (b *bluetooth) enqueue(path string, f func(next func())) {
	q := b.attrQs[path]
	b.attrQs[path] = append(q, f)
	if len(q) == 0 {
		b.runQueue(path)
	}
}

func (b *bluetooth) runQueue(path string) {
	q := b.attrQs[path]
	if len(q) == 0 {
		delete(b.attrQs, path)
		return
	}
	done := false
	q[0](func() {
		if done {
			return
		}
		done = true
		rest := b.attrQs[path][1:]
		if len(rest) == 0 {
			delete(b.attrQs, path)
			return
		}
		b.attrQs[path] = rest
		b.runQueue(path)
	})
}

func btOK(reply func([]byte, error)) { reply([]byte("{}"), nil) }

func btJSON(reply func([]byte, error), v any) {
	out, err := json.Marshal(v)
	reply(out, err)
}

func btFail(reply func([]byte, error), s string) { reply(nil, errors.New(s)) }

// bluetoothModule: the methods of internal/bluetoothspec.
func (h *Host) bluetoothModule() map[string]method {
	b := h.bt()
	// with decodes the args into v and runs f once BlueZ is read.
	with := func(v any, f func(reply func([]byte, error))) method {
		return func(args []byte, reply func([]byte, error)) {
			if v != nil && len(args) > 0 {
				if err := json.Unmarshal(args, v); err != nil {
					btFail(reply, "bad args: "+err.Error())
					return
				}
			}
			b.start(func() { f(reply) })
		}
	}
	state := func(_ []byte, reply func([]byte, error)) {
		b.start(func() { btJSON(reply, spec.State{State: b.current()}) })
	}
	notAvailable := func(why string) method {
		return func(_ []byte, reply func([]byte, error)) { btFail(reply, "not available: "+why) }
	}
	return map[string]method{
		"state":   state,
		"request": state, // no permission to ask for
		"next": func(_ []byte, reply func([]byte, error)) {
			b.start(func() {
				b.waiting = reply
				b.flush()
			})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			if w := b.waiting; w != nil {
				b.waiting = nil
				w(nil, errors.New("stopped"))
			}
			btOK(reply)
		},
		"scan": func(args []byte, reply func([]byte, error)) {
			var a spec.ScanArgs
			with(&a, func(reply func([]byte, error)) { b.scan(a, reply) })(args, reply)
		},
		"stopScan": func(args []byte, reply func([]byte, error)) {
			var a spec.ID
			with(&a, func(reply func([]byte, error)) { b.stopScan(a.ID); btOK(reply) })(args, reply)
		},
		"stopBackgroundScan": func(_ []byte, reply func([]byte, error)) { btOK(reply) },
		"choose":             notAvailable("Linux has no device chooser for apps (scan instead)"),
		"known": func(args []byte, reply func([]byte, error)) {
			var a spec.KnownArgs
			with(&a, func(reply func([]byte, error)) { btJSON(reply, b.known(a.IDs)) })(args, reply)
		},
		"connected": func(args []byte, reply func([]byte, error)) {
			var a spec.ConnectedArgs
			with(&a, func(reply func([]byte, error)) { btJSON(reply, b.connectedDevices(a.Services)) })(args, reply)
		},
		"connect": func(args []byte, reply func([]byte, error)) {
			var a spec.ConnectArgs
			with(&a, func(reply func([]byte, error)) { b.connect(a, reply) })(args, reply)
		},
		"disconnect": func(args []byte, reply func([]byte, error)) {
			var a spec.Conn
			with(&a, func(reply func([]byte, error)) { b.disconnect(a.Conn, reply) })(args, reply)
		},
		"services": func(args []byte, reply func([]byte, error)) {
			var a spec.Conn
			with(&a, func(reply func([]byte, error)) { b.services(a.Conn, reply) })(args, reply)
		},
		"read": func(args []byte, reply func([]byte, error)) {
			var a spec.Attr
			with(&a, func(reply func([]byte, error)) { b.read(a, ifGattChar, reply) })(args, reply)
		},
		"readDescriptor": func(args []byte, reply func([]byte, error)) {
			var a spec.Attr
			with(&a, func(reply func([]byte, error)) { b.read(a, ifGattDesc, reply) })(args, reply)
		},
		"write": func(args []byte, reply func([]byte, error)) {
			var a spec.Write
			with(&a, func(reply func([]byte, error)) { b.write(a, ifGattChar, reply) })(args, reply)
		},
		"writeDescriptor": func(args []byte, reply func([]byte, error)) {
			var a spec.Write
			with(&a, func(reply func([]byte, error)) { b.write(a, ifGattDesc, reply) })(args, reply)
		},
		"subscribe": func(args []byte, reply func([]byte, error)) {
			var a spec.Subscribe
			with(&a, func(reply func([]byte, error)) { b.subscribeChar(a, reply) })(args, reply)
		},
		"unsubscribe": func(args []byte, reply func([]byte, error)) {
			var a spec.Attr
			with(&a, func(reply func([]byte, error)) { b.unsubscribeChar(a, reply) })(args, reply)
		},
		"mtu": func(args []byte, reply func([]byte, error)) {
			var a spec.Conn
			with(&a, func(reply func([]byte, error)) { b.mtu(a.Conn, reply) })(args, reply)
		},
		"requestMtu": func(args []byte, reply func([]byte, error)) {
			// BlueZ exchanges the MTU itself as it connects: the answer is
			// what it is.
			var a spec.RequestMTU
			with(&a, func(reply func([]byte, error)) { b.mtu(a.Conn, reply) })(args, reply)
		},
		"rssi": func(args []byte, reply func([]byte, error)) {
			var a spec.Conn
			with(&a, func(reply func([]byte, error)) { b.rssi(a.Conn, reply) })(args, reply)
		},
		"priority": func(args []byte, reply func([]byte, error)) {
			var a spec.Priority
			with(&a, func(reply func([]byte, error)) {
				if b.conns[a.Conn] == nil {
					btFail(reply, "disconnected")
					return
				}
				btOK(reply) // BlueZ decides the connection's parameters
			})(args, reply)
		},
		"advertise": func(args []byte, reply func([]byte, error)) {
			var a spec.AdvertiseArgs
			with(&a, func(reply func([]byte, error)) { b.advertise(a, reply) })(args, reply)
		},
		"stopAdvertise": func(args []byte, reply func([]byte, error)) {
			var a spec.ID
			with(&a, func(reply func([]byte, error)) { b.stopAdvertise(a.ID); btOK(reply) })(args, reply)
		},
		"serve": func(args []byte, reply func([]byte, error)) {
			var a spec.ServeArgs
			with(&a, func(reply func([]byte, error)) { b.serve(a, reply) })(args, reply)
		},
		"unserve": func(args []byte, reply func([]byte, error)) {
			var a spec.ID
			with(&a, func(reply func([]byte, error)) { b.unserve(a.ID); btOK(reply) })(args, reply)
		},
		"respond": func(args []byte, reply func([]byte, error)) {
			var a spec.Respond
			with(&a, func(reply func([]byte, error)) { b.respond(a); btOK(reply) })(args, reply)
		},
		"notify": func(args []byte, reply func([]byte, error)) {
			var a spec.Notify
			with(&a, func(reply func([]byte, error)) { b.notify(a, reply) })(args, reply)
		},
		// Linux restores nothing: the app's process is all there is.
		"restore": func(_ []byte, reply func([]byte, error)) { btJSON(reply, spec.Restored{}) },
	}
}
