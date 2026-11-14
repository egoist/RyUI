package fake

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"time"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
)

// Bluetooth is a simulated Bluetooth LE adapter, with simulated
// peripherals around it, as the hosts' "bluetooth" module
// (internal/bluetoothspec) runs one (package bluetooth), for tests:
// Host.Bluetooth installs it. UUIDs are strings in any form
// bluetooth.ParseUUID takes ("180d", or 128-bit); properties are
// bluetooth.Properties' bits; states and reasons the wire's words
// ("on", "off", "denied", "notAsked", "unsupported"; "remote",
// "timeout", "failed", "off").
//
//	uitest.Mount(t, App, uitest.Setup(func(h *fake.Host) {
//		bt := h.Bluetooth()
//		bt.Add(&fake.Peripheral{ID: "hr-1", Name: "Polar H10", RSSI: -60,
//			Advertised: []string{"180d"},
//			Services: []*fake.Service{{UUID: "180d", Characteristics: []*fake.Characteristic{
//				{UUID: "2a37", Properties: int(bluetooth.Notify)},
//			}}}})
//	}))
type Bluetooth struct {
	// State is the adapter's, a bluetoothspec State* constant ("on"
	// by default); SetState changes it as the app runs.
	State string
	// Granted is the state Request leaves NotAsked in ("on" by default):
	// "denied" for a user who says no.
	Granted string
	// Chooser picks a device in the chooser ("choose"), from those that
	// match its filters: nil (the default) picks the first; it returns
	// nil for a user who closes it. ChooserAvailable false makes "choose"
	// fail as on a platform without one (iOS, a desktop).
	Chooser          func(matching []*Peripheral) *Peripheral
	ChooserAvailable bool
	// NoPeripheralRole makes advertising and serving fail, as on the web.
	NoPeripheralRole bool
	// Restored is what "restore" answers once.
	Restored *spec.Restored

	// What the app did: its scans running (by ID), the advertisements
	// it sent (Advertising, by ID), its servers (by ID), and the
	// notifications its servers sent (Notified).
	Scans       map[uint32]spec.ScanArgs
	Advertising map[uint32]spec.AdvertiseArgs
	Servers     map[uint32]*LocalServer
	Notified    []spec.Notify

	h           *Host
	mu          sync.Mutex
	peripherals []*Peripheral
	conns       map[uint32]*connection
	queue       []spec.Event
	waiting     func([]byte, error)
	nextHandle  uint32
	nextRequest uint32
	requests    map[uint32]func(spec.Respond)
}

// Peripheral is a simulated device.
type Peripheral struct {
	ID, Name string
	RSSI     int
	// What it advertises.
	LocalName        string
	Advertised       []string
	ServiceData      map[string][]byte
	ManufacturerData map[uint16][]byte
	TxPower          *int
	NotConnectable   bool
	// Services are its GATT services.
	Services []*Service
	// SystemConnected: the system is connected to it already (another
	// app's): "connected" lists it.
	SystemConnected bool
	// ConnectError, if set, is what connecting to it fails with ("timeout").
	ConnectError string
	// MTU is the MTU it agrees to (23 if 0); RequestMTU's at most.
	MTU int

	b *Bluetooth
}

// Service is a simulated peripheral's service.
type Service struct {
	UUID            string
	Secondary       bool
	Characteristics []*Characteristic
	handle          uint32
}

// Characteristic is a simulated service's.
type Characteristic struct {
	UUID       string
	Properties int // bluetoothspec Prop* bits
	// Value is what a read gets, and what a write leaves.
	Value []byte
	// OnWrite, if set, takes writes (an ATT error code to refuse one, 0
	// to take it), after Value is set.
	OnWrite func(value []byte, withResponse bool) int
	// ReadError, if set, refuses reads with the ATT error code.
	ReadError   int
	Descriptors []*Descriptor
	// Subscribed: the app turned its notifications on.
	Subscribed bool
	// Writes are the values the app wrote, in order.
	Writes [][]byte
	handle uint32
	svc    *Service
	cccd   *Descriptor // the Client Characteristic Configuration, for one that notifies
}

// Descriptor is a simulated characteristic's.
type Descriptor struct {
	UUID   string
	Value  []byte
	handle uint32
}

type connection struct {
	p   *Peripheral
	up  bool
	mtu int
}

// LocalServer is one of the app's GATT servers, as the fake host holds it.
type LocalServer struct {
	Services []spec.LocalService
}

// Bluetooth gives the host a simulated adapter (the "bluetooth" module),
// on, with no peripherals.
func (h *Host) Bluetooth() *Bluetooth {
	b := &Bluetooth{h: h, State: spec.StateOn, Granted: spec.StateOn, ChooserAvailable: true,
		Scans: map[uint32]spec.ScanArgs{}, Advertising: map[uint32]spec.AdvertiseArgs{},
		Servers: map[uint32]*LocalServer{}, conns: map[uint32]*connection{}, requests: map[uint32]func(spec.Respond){}}
	if h.AsyncModules == nil {
		h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){}
	}
	h.AsyncModules["bluetooth"] = b.call
	return b
}

// Add puts a peripheral in range: the scans running hear it.
func (b *Bluetooth) Add(p *Peripheral) {
	b.mu.Lock()
	p.b = b
	b.peripherals = append(b.peripherals, p)
	for _, s := range p.Services {
		b.nextHandle++
		s.handle = b.nextHandle
		for _, c := range s.Characteristics {
			b.nextHandle++
			c.handle, c.svc = b.nextHandle, s
			if c.Properties&(spec.PropNotify|spec.PropIndicate) != 0 {
				b.nextHandle++
				c.cccd = &Descriptor{UUID: "2902", Value: []byte{0, 0}, handle: b.nextHandle}
			}
			for _, d := range c.Descriptors {
				b.nextHandle++
				d.handle = b.nextHandle
			}
		}
	}
	b.advertiseLocked(p, false)
	b.flushLocked()
	b.mu.Unlock()
	b.h.RunPosted()
}

// Remove takes a peripheral out of range: a connection to it ends
// (ReasonTimeout).
func (b *Bluetooth) Remove(p *Peripheral) {
	b.mu.Lock()
	b.peripherals = slices.DeleteFunc(b.peripherals, func(x *Peripheral) bool { return x == p })
	b.mu.Unlock()
	p.Disconnect(spec.ReasonTimeout)
}

// Advertise has the peripheral advertise again (its RSSI, its data,
// changed): the scans that want duplicates hear it.
func (p *Peripheral) Advertise() {
	b := p.b
	b.mu.Lock()
	b.advertiseLocked(p, true)
	b.flushLocked()
	b.mu.Unlock()
	b.h.RunPosted()
}

// Connected reports whether the app is connected to it.
func (p *Peripheral) Connected() bool {
	b := p.b
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, c := range b.conns {
		if c.p == p && c.up {
			return true
		}
	}
	return false
}

// Disconnect ends the app's connections to it, for reason (a
// bluetoothspec Reason* constant).
func (p *Peripheral) Disconnect(reason string) {
	b := p.b
	if b == nil {
		return
	}
	b.mu.Lock()
	for id, c := range b.conns {
		if c.p == p {
			delete(b.conns, id)
			b.queue = append(b.queue, spec.Event{Type: spec.EventDisconnected, Conn: id, Reason: reason, Error: "the peripheral went"})
		}
	}
	for _, s := range p.Services {
		for _, c := range s.Characteristics {
			c.Subscribed = false
		}
	}
	b.flushLocked()
	b.mu.Unlock()
	b.h.RunPosted()
}

// Characteristic finds one of its characteristics.
func (p *Peripheral) Characteristic(service, char string) *Characteristic {
	for _, s := range p.Services {
		if spec.Normalize(s.UUID) != spec.Normalize(service) {
			continue
		}
		for _, c := range s.Characteristics {
			if spec.Normalize(c.UUID) == spec.Normalize(char) {
				return c
			}
		}
	}
	return nil
}

// Notify sends the characteristic's new value to the app, if it
// subscribed (and sets Value).
func (p *Peripheral) Notify(service, char string, value []byte) {
	b := p.b
	c := p.Characteristic(service, char)
	if c == nil {
		panic(fmt.Sprintf("fake: no characteristic %s of %s", char, service))
	}
	b.mu.Lock()
	c.Value = value
	if c.Subscribed {
		for id, conn := range b.conns {
			if conn.p == p && conn.up {
				b.queue = append(b.queue, spec.Event{Type: spec.EventValue, Conn: id, Handle: c.handle, Value: append([]byte(nil), value...)})
			}
		}
	}
	b.flushLocked()
	b.mu.Unlock()
	b.h.RunPosted()
}

// SetState changes the adapter's state: an "off" ends the scans and the
// connections, as a platform's does.
func (b *Bluetooth) SetState(state string) {
	b.mu.Lock()
	b.State = state
	b.queue = append(b.queue, spec.Event{Type: spec.EventState, State: state})
	if state != spec.StateOn {
		for id := range b.Scans {
			b.queue = append(b.queue, spec.Event{Type: spec.EventScanStopped, Scan: id, Error: state})
		}
		clear(b.Scans)
		for id := range b.conns {
			b.queue = append(b.queue, spec.Event{Type: spec.EventDisconnected, Conn: id, Reason: spec.ReasonOff})
		}
		clear(b.conns)
	}
	b.flushLocked()
	b.mu.Unlock()
	b.h.RunPosted()
}

// Following reports whether the core waits for events ("next").
func (b *Bluetooth) Following() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.waiting != nil
}

// CentralRead has a central read the app's local characteristic
// (served by any of its servers), and calls done with the value or the
// ATT error code the app answered with.
func (b *Bluetooth) CentralRead(central, char string, done func(value []byte, code int)) {
	b.request(central, char, func(server, handle, req uint32) spec.Event {
		return spec.Event{Type: spec.EventRead, Server: server, Handle: handle, Request: req}
	}, func(r spec.Respond) { done(r.Value, r.Error) })
}

// CentralWrite has a central write the app's local characteristic, with
// a response (done gets the app's ATT error code, 0 if it took it) or
// without (done is called at once, with 0).
func (b *Bluetooth) CentralWrite(central, char string, value []byte, response bool, done func(code int)) {
	b.request(central, char, func(server, handle, req uint32) spec.Event {
		return spec.Event{Type: spec.EventWrite, Server: server, Handle: handle, Request: req, Value: value, Response: response}
	}, func(r spec.Respond) {
		if done != nil {
			done(r.Error)
		}
	})
	if !response && done != nil {
		done(0)
	}
}

// CentralSubscribe has a central subscribe to the app's local
// characteristic's notifications (on), or stop.
func (b *Bluetooth) CentralSubscribe(central, char string, on bool) {
	b.request(central, char, func(server, handle, _ uint32) spec.Event {
		return spec.Event{Type: spec.EventSubscription, Server: server, Handle: handle, On: on}
	}, nil)
}

func (b *Bluetooth) request(central, char string, event func(server, handle, req uint32) spec.Event, answer func(spec.Respond)) {
	b.mu.Lock()
	server, handle := b.findLocal(char)
	if server == 0 {
		b.mu.Unlock()
		panic("fake: the app serves no characteristic " + char)
	}
	b.nextRequest++
	req := b.nextRequest
	e := event(server, handle, req)
	e.Central = &spec.Device{ID: central}
	if answer != nil && (e.Type == spec.EventRead || e.Response) {
		b.requests[req] = answer
	}
	b.queue = append(b.queue, e)
	b.flushLocked()
	b.mu.Unlock()
	b.h.RunPosted()
}

func (b *Bluetooth) findLocal(char string) (uint32, uint32) {
	for id, s := range b.Servers {
		for _, svc := range s.Services {
			for _, c := range svc.Characteristics {
				if c.UUID == spec.Normalize(char) {
					return id, c.Handle
				}
			}
		}
	}
	return 0, 0
}

// advertiseLocked sends p's advertisement to the scans that hear it.
func (b *Bluetooth) advertiseLocked(p *Peripheral, again bool) {
	if b.State != spec.StateOn {
		return
	}
	for id, s := range b.Scans {
		if again && !s.Duplicates {
			continue
		}
		if !matches(p, s.Services) {
			continue
		}
		b.queue = append(b.queue, spec.Event{Type: spec.EventAdvertisement, Scan: id, Advertisement: p.advertisement()})
	}
}

func matches(p *Peripheral, services []string) bool {
	if len(services) == 0 {
		return true
	}
	for _, s := range services {
		for _, a := range p.Advertised {
			if spec.Normalize(a) == spec.Normalize(s) {
				return true
			}
		}
	}
	return false
}

func (p *Peripheral) advertisement() *spec.Advertisement {
	a := &spec.Advertisement{Device: p.device(), RSSI: p.RSSI, LocalName: p.LocalName, TxPower: p.TxPower, Time: time.Now().UnixMilli()}
	for _, s := range p.Advertised {
		a.Services = append(a.Services, spec.Normalize(s))
	}
	if len(p.ServiceData) > 0 {
		a.ServiceData = map[string][]byte{}
		for k, v := range p.ServiceData {
			a.ServiceData[spec.Normalize(k)] = v
		}
	}
	if len(p.ManufacturerData) > 0 {
		a.ManufacturerData = map[string][]byte{}
		for k, v := range p.ManufacturerData {
			a.ManufacturerData[strconv.Itoa(int(k))] = v
		}
	}
	c := !p.NotConnectable
	a.Connectable = &c
	return a
}

func (p *Peripheral) device() spec.Device { return spec.Device{ID: p.ID, Name: p.Name} }

// flushLocked answers a waiting "next" with the queue.
func (b *Bluetooth) flushLocked() {
	if b.waiting == nil || len(b.queue) == 0 {
		return
	}
	out, _ := json.Marshal(b.queue)
	b.queue = nil
	w := b.waiting
	b.waiting = nil
	w(out, nil)
}

func (b *Bluetooth) peripheral(id string) *Peripheral {
	for _, p := range b.peripherals {
		if p.ID == id {
			return p
		}
	}
	return nil
}

func btOK(reply func([]byte, error)) { reply([]byte("{}"), nil) }

func btReply(reply func([]byte, error), v any) {
	out, _ := json.Marshal(v)
	reply(out, nil)
}

func (b *Bluetooth) call(method string, args []byte, answer func([]byte, error)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fail := func(s string) { answer(nil, errors.New(s)) }
	decode := func(v any) bool {
		if err := json.Unmarshal(args, v); err != nil {
			fail("bad args: " + err.Error())
			return false
		}
		return true
	}
	on := func() bool {
		if b.State != spec.StateOn {
			fail(b.State)
			return false
		}
		return true
	}
	attr := func(conn, handle uint32) (*connection, *Characteristic, *Descriptor, bool) {
		c := b.conns[conn]
		if c == nil || !c.up {
			fail("disconnected")
			return nil, nil, nil, false
		}
		for _, s := range c.p.Services {
			for _, ch := range s.Characteristics {
				if ch.handle == handle {
					return c, ch, nil, true
				}
				if ch.cccd != nil && ch.cccd.handle == handle {
					return c, ch, ch.cccd, true
				}
				for _, d := range ch.Descriptors {
					if d.handle == handle {
						return c, ch, d, true
					}
				}
			}
		}
		fail("not found")
		return nil, nil, nil, false
	}
	switch method {
	case "state":
		btReply(answer, spec.State{State: b.State})
	case "request":
		if b.State == spec.StateNotAsked {
			b.State = b.Granted
		}
		btReply(answer, spec.State{State: b.State})
	case "next":
		b.waiting = answer
		b.flushLocked()
	case "stop":
		if w := b.waiting; w != nil {
			b.waiting = nil
			w(nil, errors.New("stopped"))
		}
		btOK(answer)
	case "scan":
		var a spec.ScanArgs
		if !decode(&a) || !on() {
			return
		}
		b.Scans[a.ID] = a
		btOK(answer)
		for _, p := range b.peripherals {
			if matches(p, a.Services) {
				b.queue = append(b.queue, spec.Event{Type: spec.EventAdvertisement, Scan: a.ID, Advertisement: p.advertisement()})
			}
		}
		b.flushLocked()
	case "stopScan":
		var a spec.ID
		if decode(&a) {
			delete(b.Scans, a.ID)
			btOK(answer)
		}
	case "stopBackgroundScan":
		btOK(answer)
	case "choose":
		var a spec.ChooseArgs
		if !decode(&a) {
			return
		}
		if !b.ChooserAvailable {
			fail("not available: this platform has no device chooser")
			return
		}
		if !on() {
			return
		}
		var ms []*Peripheral
		for _, p := range b.peripherals {
			if chosen(p, a.Filters) {
				ms = append(ms, p)
			}
		}
		var p *Peripheral
		if b.Chooser != nil {
			b.mu.Unlock()
			p = b.Chooser(ms)
			b.mu.Lock()
		} else if len(ms) > 0 {
			p = ms[0]
		}
		if p == nil {
			fail("canceled")
			return
		}
		btReply(answer, p.device())
	case "known":
		var a spec.KnownArgs
		if !decode(&a) {
			return
		}
		out := []spec.Device{}
		for _, id := range a.IDs {
			if p := b.peripheral(id); p != nil {
				out = append(out, p.device())
			}
		}
		btReply(answer, out)
	case "connected":
		var a spec.ConnectedArgs
		if !decode(&a) {
			return
		}
		out := []spec.Device{}
		for _, p := range b.peripherals {
			if p.SystemConnected && matches(p, a.Services) {
				out = append(out, p.device())
			}
		}
		btReply(answer, out)
	case "connect":
		var a spec.ConnectArgs
		if !decode(&a) || !on() {
			return
		}
		p := b.peripheral(a.Device)
		switch {
		case p == nil:
			fail("not found")
		case p.ConnectError != "":
			fail(p.ConnectError)
		default:
			b.conns[a.Conn] = &connection{p: p, up: true, mtu: max(p.MTU, 23)}
			btOK(answer)
		}
	case "disconnect":
		var a spec.Conn
		if decode(&a) {
			if c := b.conns[a.Conn]; c != nil {
				delete(b.conns, a.Conn)
				for _, s := range c.p.Services {
					for _, ch := range s.Characteristics {
						ch.Subscribed = false
					}
				}
			}
			btOK(answer)
		}
	case "services":
		var a spec.Conn
		if !decode(&a) {
			return
		}
		c := b.conns[a.Conn]
		if c == nil {
			fail("disconnected")
			return
		}
		out := []spec.Service{}
		for _, s := range c.p.Services {
			ws := spec.Service{Handle: s.handle, UUID: spec.Normalize(s.UUID), Primary: !s.Secondary}
			for _, ch := range s.Characteristics {
				wc := spec.Characteristic{Handle: ch.handle, UUID: spec.Normalize(ch.UUID), Properties: ch.Properties}
				if ch.cccd != nil {
					wc.Descriptors = append(wc.Descriptors, spec.Descriptor{Handle: ch.cccd.handle, UUID: spec.Normalize("2902")})
				}
				for _, d := range ch.Descriptors {
					wc.Descriptors = append(wc.Descriptors, spec.Descriptor{Handle: d.handle, UUID: spec.Normalize(d.UUID)})
				}
				ws.Characteristics = append(ws.Characteristics, wc)
			}
			out = append(out, ws)
		}
		btReply(answer, out)
	case "read":
		var a spec.Attr
		if !decode(&a) {
			return
		}
		if _, ch, _, found := attr(a.Conn, a.Handle); found {
			switch {
			case ch.Properties&spec.PropRead == 0:
				fail("att 2: read not permitted")
			case ch.ReadError != 0:
				fail(fmt.Sprintf("att %d: refused", ch.ReadError))
			default:
				answer(append([]byte{}, ch.Value...), nil)
			}
		}
	case "write":
		var a spec.Write
		if !decode(&a) {
			return
		}
		if _, ch, _, found := attr(a.Conn, a.Handle); found {
			want := spec.PropWriteNoResponse
			if a.Response {
				want = spec.PropWrite
			}
			if ch.Properties&want == 0 {
				fail("att 3: write not permitted")
				return
			}
			ch.Value = append([]byte(nil), a.Value...)
			ch.Writes = append(ch.Writes, ch.Value)
			if ch.OnWrite != nil {
				if code := ch.OnWrite(ch.Value, a.Response); code != 0 {
					fail(fmt.Sprintf("att %d: refused", code))
					return
				}
			}
			btOK(answer)
		}
	case "readDescriptor", "writeDescriptor":
		var a spec.Write
		if !decode(&a) {
			return
		}
		if _, _, d, found := attr(a.Conn, a.Handle); found {
			if d == nil {
				fail("not found")
				return
			}
			if method == "writeDescriptor" {
				d.Value = append([]byte(nil), a.Value...)
				btOK(answer)
				return
			}
			answer(append([]byte{}, d.Value...), nil)
		}
	case "subscribe", "unsubscribe":
		var a spec.Subscribe
		if !decode(&a) {
			return
		}
		if _, ch, _, found := attr(a.Conn, a.Handle); found {
			if method == "subscribe" && ch.Properties&(spec.PropNotify|spec.PropIndicate) == 0 {
				fail("att 6: the characteristic doesn't notify")
				return
			}
			ch.Subscribed = method == "subscribe"
			ch.cccd.Value = []byte{0, 0}
			if ch.Subscribed {
				ch.cccd.Value = []byte{1, 0}
				if a.Indicate && ch.Properties&spec.PropIndicate != 0 || ch.Properties&spec.PropNotify == 0 {
					ch.cccd.Value = []byte{2, 0}
				}
			}
			btOK(answer)
		}
	case "mtu", "requestMtu":
		var a spec.RequestMTU
		if !decode(&a) {
			return
		}
		c := b.conns[a.Conn]
		if c == nil {
			fail("disconnected")
			return
		}
		if method == "requestMtu" {
			c.mtu = max(23, min(a.MTU, max(c.p.MTU, 23), 517))
		}
		btReply(answer, spec.MTU{MTU: c.mtu, WriteWithResponse: c.mtu - 3, WriteWithoutResponse: c.mtu - 3})
	case "rssi":
		var a spec.Conn
		if !decode(&a) {
			return
		}
		c := b.conns[a.Conn]
		if c == nil {
			fail("disconnected")
			return
		}
		btReply(answer, spec.RSSI{RSSI: c.p.RSSI})
	case "priority":
		var a spec.Priority
		if !decode(&a) {
			return
		}
		if b.conns[a.Conn] == nil {
			fail("disconnected")
			return
		}
		btOK(answer)
	case "advertise":
		var a spec.AdvertiseArgs
		if !decode(&a) {
			return
		}
		if b.NoPeripheralRole {
			fail("not available: no peripheral role here")
			return
		}
		if !on() {
			return
		}
		b.Advertising[a.ID] = a
		btOK(answer)
	case "stopAdvertise":
		var a spec.ID
		if decode(&a) {
			delete(b.Advertising, a.ID)
			btOK(answer)
		}
	case "serve":
		var a spec.ServeArgs
		if !decode(&a) {
			return
		}
		if b.NoPeripheralRole {
			fail("not available: no peripheral role here")
			return
		}
		if !on() {
			return
		}
		for i := range a.Services {
			a.Services[i].UUID = spec.Normalize(a.Services[i].UUID)
			for j := range a.Services[i].Characteristics {
				c := &a.Services[i].Characteristics[j]
				c.UUID = spec.Normalize(c.UUID)
			}
		}
		b.Servers[a.ID] = &LocalServer{Services: a.Services}
		btOK(answer)
	case "unserve":
		var a spec.ID
		if decode(&a) {
			delete(b.Servers, a.ID)
			btOK(answer)
		}
	case "respond":
		var a spec.Respond
		if !decode(&a) {
			return
		}
		f := b.requests[a.Request]
		delete(b.requests, a.Request)
		btOK(answer)
		if f != nil {
			b.mu.Unlock()
			f(a)
			b.mu.Lock()
		}
	case "notify":
		var a spec.Notify
		if !decode(&a) {
			return
		}
		b.Notified = append(b.Notified, a)
		btOK(answer)
	case "restore":
		r := b.Restored
		b.Restored = nil
		if r == nil {
			r = &spec.Restored{}
		}
		btReply(answer, r)
	default:
		fail("no module method bluetooth." + method)
	}
}

// chosen reports whether p matches one of the chooser's filters.
func chosen(p *Peripheral, filters []spec.Filter) bool {
	if len(filters) == 0 {
		return true
	}
	for _, f := range filters {
		ok := true
		for _, s := range f.Services {
			if !matches(p, []string{s}) {
				ok = false
			}
		}
		name := p.Name
		if p.LocalName != "" {
			name = p.LocalName
		}
		if f.Name != "" && name != f.Name {
			ok = false
		}
		if f.NamePrefix != "" && (len(name) < len(f.NamePrefix) || name[:len(f.NamePrefix)] != f.NamePrefix) {
			ok = false
		}
		if f.Manufacturer != nil {
			if _, has := p.ManufacturerData[uint16(*f.Manufacturer)]; !has {
				ok = false
			}
		}
		if ok {
			return true
		}
	}
	return false
}
