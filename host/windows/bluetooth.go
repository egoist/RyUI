//go:build windows

package windows

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	. "github.com/egoist/asuka/internal/windows"
)

// asuka/sdk/bluetooth on Windows: the "bluetooth" module of
// internal/bluetoothspec, through Windows.Devices.Bluetooth (WinRT, from
// internal/windows/bluetooth.go), as host/fake's does it.
//
//   - The state is the default adapter's radio's (Windows.Devices.Radios):
//     no adapter, or one without LE: "unsupported"; "request" asks for the
//     radio's access, which an unpackaged app has.
//   - One advertisement watcher (active, so scan responses come, with the
//     names) serves every scan; a device's advertisement and its scan
//     response, which Windows delivers apart, are put together, and a
//     scan without duplicates gets the device once it has both (or once
//     it's clear no response comes).
//   - A connection is the device's GattSession with MaintainConnection,
//     which is what holds one on Windows: discovering the services
//     (uncached) connects. Windows doesn't say why a link went: a
//     disconnection's reason is "off" with the radio off, else "unknown".
//   - Handles are the attributes' ATT handles.
//   - The chooser is the system's device picker (DevicePicker), which
//     filters by name only.
//   - As a peripheral, each served service is a GattServiceProvider,
//     published (connectable, not discoverable) as it's served;
//     advertising a service makes its provider discoverable, with the
//     service's data. Windows lets an app's own advertisement (the
//     publisher) have only manufacturer data: no name, no service UUIDs.
//
// WinRT calls back on its own threads: every callback goes to the main
// thread (Post) before it touches the module's state.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["bluetooth"] = newBluetooth(h).methods()
	})
}

type bluetooth struct {
	h         *Host
	queue     []spec.Event
	waiting   func([]byte, error)
	following bool

	adapter  *BTAdapter
	loading  []func()
	denied   bool
	state    string // the last "state" sent
	watcher  *BTWatcher
	scans    map[uint32]*btScan
	heard    map[uint64]*btHeard
	conns    map[uint32]*btConn
	adverts  map[uint32]*btAdvert
	servers  map[uint32]*btServer
	requests map[uint32]*btServerRequest
	nextReq  uint32
}

type btScan struct {
	args spec.ScanArgs
	seen map[uint64]bool
}

// btConn is a connection, from "connect" to its end.
type btConn struct {
	id        uint32
	dev       *BTDevice
	up        bool // connected ("connect" answered)
	closed    bool
	auto      bool
	connected func([]byte, error) // "connect"'s answer, while connecting

	services    []*BTService
	chars       map[uint32]*BTCharacteristic
	descs       map[uint32]*BTDescriptor
	tree        []spec.Service
	stale       bool // the services changed since tree
	changes     int
	discovering []func([]byte, error)
	subscribed  map[uint32]bool
	pending     map[int]func(error)
	nextPending int
}

type btServer struct {
	id        uint32
	providers []*btProvider
	chars     map[uint32]*btLocal // by Go's handle
	ready     bool
	closed    bool
	reply     func([]byte, error) // "serve"'s answer, until it's served
}

type btProvider struct {
	p      *BTProvider
	uuid   string
	server *btServer
	advert *btAdvert // the advertisement it's discoverable for
}

type btLocal struct {
	lc          *BTLocalChar
	handle      uint32
	subscribers []string
	clients     map[string]*BTClient
}

type btServerRequest struct {
	server uint32
	r      *BTRequest
}

type btAdvert struct {
	id        uint32
	args      spec.AdvertiseArgs
	publisher *BTPublisher
	services  []string
	bound     map[string]*btProvider
	reply     func([]byte, error) // until it's advertising
	waiting   int                 // the parts not started yet
}

func newBluetooth(h *Host) *bluetooth {
	return &bluetooth{h: h, scans: map[uint32]*btScan{}, heard: map[uint64]*btHeard{},
		conns: map[uint32]*btConn{}, adverts: map[uint32]*btAdvert{}, servers: map[uint32]*btServer{},
		requests: map[uint32]*btServerRequest{}}
}

func btOK(reply func([]byte, error)) { reply([]byte("{}"), nil) }

func btJSON(reply func([]byte, error), v any) {
	b, err := json.Marshal(v)
	reply(b, err)
}

func btFail(reply func([]byte, error), msg string) { reply(nil, errors.New(msg)) }

// btWhy is a WinRT failure as the wire has it.
func btWhy(err error) string {
	if errors.Is(err, ErrBTCanceled) {
		return "canceled"
	}
	var hr AsyncHRESULT
	if errors.As(err, &hr) {
		return btGattError(-1, -1, uint32(hr))
	}
	if err != nil {
		if m := err.Error(); strings.HasPrefix(m, "not available") {
			return m
		}
		return "failed: " + err.Error()
	}
	return "failed"
}

func btResult(r GattResult) string {
	var hr uint32
	var h AsyncHRESULT
	if errors.As(r.Err, &h) {
		hr = uint32(h)
	}
	if r.Status == -1 && r.Err != nil && hr == 0 {
		return btWhy(r.Err)
	}
	return btGattError(r.Status, r.Protocol, hr)
}

// post runs f on the main thread.
func (b *bluetooth) post(f func()) { b.h.Post(f) }

// --- Events.

// send queues an event for "next": every advertisement, following or
// not (a scan Go starts right after a "stop" hears its first before the
// next "next"); "stop" drops those queued then.
func (b *bluetooth) send(e spec.Event) {
	b.queue = append(b.queue, e)
}

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

// --- The adapter.

// withAdapter calls f once the adapter's known (b.adapter nil: none).
func (b *bluetooth) withAdapter(f func()) {
	if b.adapter != nil {
		f()
		return
	}
	b.loading = append(b.loading, f)
	if len(b.loading) > 1 {
		return
	}
	BTDefaultAdapter(func(a *BTAdapter) {
		b.post(func() {
			if a != nil && b.adapter == nil {
				b.adapter = a
				a.OnStateChanged(func() { b.post(b.radioChanged) })
			}
			if b.state == "" {
				b.state = b.current()
			}
			fs := b.loading
			b.loading = nil
			for _, f := range fs {
				f()
			}
		})
	})
}

// current is the state now.
func (b *bluetooth) current() string {
	switch {
	case b.adapter == nil || !b.adapter.LowEnergy:
		return spec.StateUnsupported
	case b.denied:
		return spec.StateDenied
	}
	return btRadioState(b.adapter.State())
}

// on fails reply unless the adapter's on.
func (b *bluetooth) on(reply func([]byte, error)) bool {
	if s := b.current(); s != spec.StateOn {
		btFail(reply, s)
		return false
	}
	return true
}

func (b *bluetooth) radioChanged() {
	s := b.current()
	if s == b.state {
		return
	}
	b.state = s
	b.send(spec.Event{Type: spec.EventState, State: s})
	if s != spec.StateOn {
		// As the other platforms do: the scans and the connections end.
		b.endScans(s)
		for _, c := range b.conns {
			if c.up {
				b.lost(c, spec.ReasonOff)
			} else {
				b.endConn(c, s)
			}
		}
	}
	b.flush()
}

// --- Scanning.

func (b *bluetooth) scan(a spec.ScanArgs, reply func([]byte, error)) {
	if !b.on(reply) {
		return
	}
	b.scans[a.ID] = &btScan{args: a, seen: map[uint64]bool{}}
	if b.watcher == nil {
		var w *BTWatcher
		var err error
		w, err = NewBTWatcher(func(r BTReceived) {
			b.post(func() {
				if b.watcher == w {
					b.received(r)
				}
			})
		}, func(e int32) {
			b.post(func() {
				if b.watcher == w {
					b.watcherStopped(e)
				}
			})
		})
		if err == nil {
			b.watcher = w
			if err = w.Start(); err != nil {
				b.watcher = nil
				w.Close()
			}
		}
		if err != nil {
			delete(b.scans, a.ID)
			btFail(reply, btWhy(err))
			return
		}
	}
	btOK(reply)
}

func (b *bluetooth) stopScan(id uint32) {
	delete(b.scans, id)
	if len(b.scans) == 0 && b.watcher != nil {
		b.watcher.Close()
		b.watcher = nil
		clear(b.heard)
	}
}

// endScans ends every scan with why.
func (b *bluetooth) endScans(why string) {
	for id := range b.scans {
		b.send(spec.Event{Type: spec.EventScanStopped, Scan: id, Error: why})
	}
	clear(b.scans)
	if b.watcher != nil {
		b.watcher.Close()
		b.watcher = nil
	}
	clear(b.heard)
}

func (b *bluetooth) watcherStopped(e int32) {
	why := btError(e)
	if s := b.current(); s != spec.StateOn {
		why = s
	} else if why == "" {
		why = "failed: Windows stopped the scan"
	}
	b.endScans(why)
	b.flush()
}

func (b *bluetooth) received(r BTReceived) {
	h := b.heard[r.Address]
	if h == nil {
		h = &btHeard{}
		b.heard[r.Address] = h
	}
	secs := make([]btSection, len(r.Sections))
	for i, s := range r.Sections {
		secs[i] = btSection{typ: s.Type, data: s.Data}
	}
	ad := btParseSections(secs)
	if ad.name == "" {
		ad.name = r.LocalName
	}
	if ad.txPower == nil && r.TxPower != nil {
		t := *r.TxPower
		ad.txPower = &t
	}
	h.take(btPacket{ad: ad, rssi: r.RSSI, time: r.Time, response: r.ScanResponse, scannable: r.Scannable,
		typ: r.Type, connectable: r.Connectable})
	var a *spec.Advertisement
	for id, s := range b.scans {
		if !s.args.Duplicates && (s.seen[r.Address] || !h.complete()) {
			continue
		}
		if a == nil {
			a = h.advertisement(btAddress(r.Address))
		}
		if !btMatches(a, s.args.Services) {
			continue
		}
		s.seen[r.Address] = true
		b.send(spec.Event{Type: spec.EventAdvertisement, Scan: id, Advertisement: a})
	}
	b.flush()
}

// --- Centrals' devices.

func (b *bluetooth) known(ids []string, reply func([]byte, error)) {
	out := []spec.Device{}
	var next func(i int)
	next = func(i int) {
		if i == len(ids) {
			btJSON(reply, out)
			return
		}
		addr, ok := btParseAddress(ids[i])
		if !ok {
			next(i + 1)
			return
		}
		BTDeviceFromAddress(addr, func(d *BTDevice, _ error) {
			b.post(func() {
				if d != nil {
					out = append(out, spec.Device{ID: btAddress(addr), Name: d.Name()})
					d.Close()
				}
				next(i + 1)
			})
		})
	}
	next(0)
}

func (b *bluetooth) connectedDevices(services []string, reply func([]byte, error)) {
	aqs := BTSelectorConnected()
	if aqs == "" {
		btJSON(reply, []spec.Device{})
		return
	}
	want := map[string]bool{}
	for _, s := range services {
		want[spec.Normalize(s)] = true
	}
	BTFindDevices(aqs, func(ids []string, err error) {
		b.post(func() {
			if err != nil {
				btFail(reply, btWhy(err))
				return
			}
			out := []spec.Device{}
			var next func(i int)
			next = func(i int) {
				if i == len(ids) {
					btJSON(reply, out)
					return
				}
				BTDeviceFromID(ids[i], func(d *BTDevice, _ error) {
					b.post(func() {
						if d == nil {
							next(i + 1)
							return
						}
						dev := spec.Device{ID: btAddress(d.Address()), Name: d.Name()}
						if len(want) == 0 {
							out = append(out, dev)
							d.Close()
							next(i + 1)
							return
						}
						// Its services as Windows has them cached: no connection.
						d.Services(false, func(svcs []*BTService, _ GattResult) {
							b.post(func() {
								match := false
								for _, s := range svcs {
									match = match || want[spec.FormatUUID(s.UUID)]
									s.Close()
								}
								if match {
									out = append(out, dev)
								}
								d.Close()
								next(i + 1)
							})
						})
					})
				})
			}
			next(0)
		})
	})
}

func (b *bluetooth) choose(a spec.ChooseArgs, reply func([]byte, error)) {
	if !b.on(reply) {
		return
	}
	var bases []string
	for _, paired := range []bool{false, true} {
		if s := BTSelectorPaired(paired); s != "" {
			bases = append(bases, s)
		}
	}
	if len(bases) == 0 {
		btFail(reply, "not available: this Windows has no Bluetooth LE device picker")
		return
	}
	hwnd := b.h.inUseWindow()
	r := ClientRect(hwnd)
	k := float32(DPI(hwnd)) / 96
	if k <= 0 {
		k = 1
	}
	// Beside the window's center (its DIPs): the picker places itself.
	at := [4]float32{float32(r.W()) / k / 2, float32(r.H()) / k / 3, 1, 1}
	BTPickDevice(hwnd, btChooserSelectors(bases, a.Filters), at, func(id string, err error) {
		b.post(func() {
			switch {
			case err != nil:
				btFail(reply, btWhy(err))
			case id == "":
				btFail(reply, "canceled")
			default:
				BTDeviceFromID(id, func(d *BTDevice, err error) {
					b.post(func() {
						if d == nil {
							btFail(reply, "not found")
							return
						}
						dev := spec.Device{ID: btAddress(d.Address()), Name: d.Name()}
						d.Close()
						btJSON(reply, dev)
					})
				})
			}
		})
	})
}

// --- Connections.

func (b *bluetooth) connect(a spec.ConnectArgs, reply func([]byte, error)) {
	if !b.on(reply) {
		return
	}
	addr, ok := btParseAddress(a.Device)
	if !ok {
		btFail(reply, "not found")
		return
	}
	if old := b.conns[a.Conn]; old != nil {
		b.endConn(old, "canceled")
	}
	c := &btConn{id: a.Conn, auto: a.AutoConnect, connected: reply, chars: map[uint32]*BTCharacteristic{},
		descs: map[uint32]*BTDescriptor{}, subscribed: map[uint32]bool{}, pending: map[int]func(error){}}
	b.conns[a.Conn] = c
	BTDeviceFromAddress(addr, func(d *BTDevice, err error) {
		b.post(func() {
			if c.closed {
				if d != nil {
					d.Close()
				}
				return
			}
			if d == nil {
				b.endConn(c, "not found")
				return
			}
			c.dev = d
			d.OnConnectionChanged(func() { b.post(func() { b.connectionChanged(c) }) })
			d.OnServicesChanged(func() { b.post(func() { b.servicesChanged(c) }) })
			d.OpenSession(func(session BTSession, err error) {
				b.post(func() {
					if c.closed {
						session.Close()
						return
					}
					if err != nil {
						b.endConn(c, btWhy(err))
						return
					}
					d.Attach(session)
					// Discovering from the device connects to it.
					d.Services(true, func(svcs []*BTService, res GattResult) {
						b.post(func() {
							if c.closed {
								for _, s := range svcs {
									s.Close()
								}
								return
							}
							switch e := btResult(res); {
							case e == "":
								if c.services == nil {
									c.services = svcs
								} else {
									for _, s := range svcs {
										s.Close()
									}
								}
								if d.Connected() {
									b.connectedNow(c)
								} // else ConnectionStatusChanged says
							case e == "disconnected" && c.auto:
								// MaintainConnection connects once it's in range.
							case e == "disconnected":
								b.endConn(c, "timeout: the device didn't answer")
							default:
								b.endConn(c, e)
							}
						})
					})
				})
			})
		})
	})
}

func (b *bluetooth) connectedNow(c *btConn) {
	if c.up || c.closed {
		return
	}
	c.up = true
	if r := c.connected; r != nil {
		c.connected = nil
		btOK(r)
	}
}

func (b *bluetooth) connectionChanged(c *btConn) {
	if c.closed || c.dev == nil {
		return
	}
	switch connected := c.dev.Connected(); {
	case !c.up && connected:
		b.connectedNow(c)
	case c.up && !connected:
		reason := spec.ReasonUnknown // Windows doesn't say
		if b.current() != spec.StateOn {
			reason = spec.ReasonOff
		}
		b.lost(c, reason)
		b.flush()
	}
}

// lost ends a connection the device (or the radio) ended.
func (b *bluetooth) lost(c *btConn, reason string) {
	b.endConn(c, "disconnected")
	b.send(spec.Event{Type: spec.EventDisconnected, Conn: c.id, Reason: reason, Error: "the device disconnected"})
}

// endConn closes a connection: a "connect" waiting fails with why; every
// other call waiting, with "disconnected".
func (b *bluetooth) endConn(c *btConn, why string) {
	if c.closed {
		return
	}
	c.closed = true
	if b.conns[c.id] == c {
		delete(b.conns, c.id)
	}
	if r := c.connected; r != nil {
		c.connected = nil
		btFail(r, why)
	}
	gone := errors.New("disconnected")
	for _, f := range c.pending {
		f(gone)
	}
	clear(c.pending)
	for _, r := range c.discovering {
		r(nil, gone)
	}
	c.discovering = nil
	c.release()
	if c.dev != nil {
		c.dev.Close()
		c.dev = nil
	}
}

// release lets the discovered attributes go.
func (c *btConn) release() {
	for _, ch := range c.chars {
		ch.Release()
	}
	for _, d := range c.descs {
		d.Release()
	}
	for _, s := range c.services {
		s.Close()
	}
	c.chars, c.descs, c.services, c.tree = map[uint32]*BTCharacteristic{}, map[uint32]*BTDescriptor{}, nil, nil
}

// op registers a call waiting on the connection: the answer it returns
// answers once, and not after the connection ends (which answers it
// "disconnected").
func (c *btConn) op(reply func([]byte, error)) func([]byte, error) {
	n := c.nextPending
	c.nextPending++
	c.pending[n] = func(err error) { reply(nil, err) }
	return func(v []byte, err error) {
		if _, ok := c.pending[n]; !ok {
			return
		}
		delete(c.pending, n)
		reply(v, err)
	}
}

// up is a connection that's connected, or fails reply "disconnected".
func (b *bluetooth) up(id uint32, reply func([]byte, error)) *btConn {
	c := b.conns[id]
	if c == nil || !c.up {
		btFail(reply, "disconnected")
		return nil
	}
	return c
}

func (b *bluetooth) servicesChanged(c *btConn) {
	if c.closed {
		return
	}
	c.changes++
	if c.tree == nil {
		return // nothing discovered yet: nothing to say
	}
	c.stale = true
	b.send(spec.Event{Type: spec.EventServicesChanged, Conn: c.id})
	b.flush()
}

func (b *bluetooth) services(c *btConn, reply func([]byte, error)) {
	if c.tree != nil && !c.stale {
		btJSON(reply, c.tree)
		return
	}
	c.discovering = append(c.discovering, reply)
	if len(c.discovering) > 1 {
		return
	}
	if c.tree == nil && c.services != nil {
		svcs := c.services // connecting discovered them, uncached
		c.services = nil
		b.discover(c, svcs)
		return
	}
	c.dev.Services(true, func(svcs []*BTService, res GattResult) {
		b.post(func() {
			if c.closed {
				for _, s := range svcs {
					s.Close()
				}
				return
			}
			if e := btResult(res); e != "" {
				for _, s := range svcs {
					s.Close()
				}
				b.discovered(c, nil, e)
				return
			}
			b.discover(c, svcs)
		})
	})
}

// discover discovers the services' characteristics and descriptors, one
// at a time. A service Windows doesn't let the app use (HID's) comes
// without them.
func (b *bluetooth) discover(c *btConn, svcs []*BTService) {
	changes := c.changes
	tree := []spec.Service{}
	chars := map[uint32]*BTCharacteristic{}
	descs := map[uint32]*BTDescriptor{}
	abandon := func() {
		for _, s := range svcs {
			s.Close()
		}
		for _, ch := range chars {
			ch.Release()
		}
		for _, d := range descs {
			d.Release()
		}
	}
	var service func(i int)
	service = func(i int) {
		if c.closed {
			abandon()
			return
		}
		if i == len(svcs) {
			old := c.chars
			for _, ch := range old {
				ch.StopValues()
			}
			oldDescs, oldSvcs := c.descs, c.services
			for _, ch := range old {
				ch.Release()
			}
			for _, d := range oldDescs {
				d.Release()
			}
			for _, s := range oldSvcs {
				s.Close()
			}
			c.services, c.chars, c.descs, c.tree = svcs, chars, descs, tree
			c.stale = c.changes != changes
			for h := range c.subscribed {
				if ch := chars[h]; ch != nil {
					b.follow(c, h, ch)
				} else {
					delete(c.subscribed, h)
				}
			}
			b.discovered(c, tree, "")
			return
		}
		s := svcs[i]
		ws := spec.Service{Handle: uint32(s.Handle), UUID: spec.FormatUUID(s.UUID), Primary: true}
		s.Characteristics(true, func(cs []*BTCharacteristic, res GattResult) {
			b.post(func() {
				if c.closed {
					for _, ch := range cs {
						ch.Release()
					}
					abandon()
					return
				}
				if e := btResult(res); e == "disconnected" {
					for _, ch := range cs {
						ch.Release()
					}
					abandon()
					b.discovered(c, nil, e)
					return
				}
				var char func(j int)
				char = func(j int) {
					if j == len(cs) {
						tree = append(tree, ws)
						service(i + 1)
						return
					}
					ch := cs[j]
					wc := spec.Characteristic{Handle: uint32(ch.Handle), UUID: spec.FormatUUID(ch.UUID), Properties: int(ch.Props & 0xff)}
					chars[wc.Handle] = ch
					ch.Descriptors(true, func(ds []*BTDescriptor, res GattResult) {
						b.post(func() {
							if c.closed {
								for _, d := range ds {
									d.Release()
								}
								abandon()
								return
							}
							for _, d := range ds {
								descs[uint32(d.Handle)] = d
								wc.Descriptors = append(wc.Descriptors, spec.Descriptor{Handle: uint32(d.Handle), UUID: spec.FormatUUID(d.UUID)})
							}
							ws.Characteristics = append(ws.Characteristics, wc)
							char(j + 1)
						})
					})
				}
				char(0)
			})
		})
	}
	service(0)
}

func (b *bluetooth) discovered(c *btConn, tree []spec.Service, why string) {
	rs := c.discovering
	c.discovering = nil
	for _, r := range rs {
		if why != "" {
			btFail(r, why)
		} else {
			btJSON(r, tree)
		}
	}
}

// follow sends ch's notifications as "value" events.
func (b *bluetooth) follow(c *btConn, h uint32, ch *BTCharacteristic) {
	ch.OnValue(func(v []byte) {
		b.post(func() {
			if c.closed || c.chars[h] != ch {
				return
			}
			b.send(spec.Event{Type: spec.EventValue, Conn: c.id, Handle: h, Value: v})
			b.flush()
		})
	})
}

func (b *bluetooth) char(a spec.Attr, reply func([]byte, error)) (*btConn, *BTCharacteristic) {
	c := b.up(a.Conn, reply)
	if c == nil {
		return nil, nil
	}
	ch := c.chars[a.Handle]
	if ch == nil {
		btFail(reply, "not found")
		return nil, nil
	}
	return c, ch
}

func (b *bluetooth) desc(a spec.Attr, reply func([]byte, error)) (*btConn, *BTDescriptor) {
	c := b.up(a.Conn, reply)
	if c == nil {
		return nil, nil
	}
	d := c.descs[a.Handle]
	if d == nil {
		btFail(reply, "not found")
		return nil, nil
	}
	return c, d
}

// readDone and writeDone answer a GATT operation on the main thread.
func (b *bluetooth) readDone(done func([]byte, error)) func([]byte, GattResult) {
	return func(v []byte, res GattResult) {
		b.post(func() {
			if e := btResult(res); e != "" {
				done(nil, errors.New(e))
				return
			}
			if v == nil {
				v = []byte{}
			}
			done(v, nil)
		})
	}
}

func (b *bluetooth) writeDone(done func([]byte, error)) func(GattResult) {
	return func(res GattResult) {
		b.post(func() {
			if e := btResult(res); e != "" {
				done(nil, errors.New(e))
				return
			}
			btOK(done)
		})
	}
}

func (b *bluetooth) subscribe(a spec.Subscribe, on bool, reply func([]byte, error)) {
	c, ch := b.char(spec.Attr{Conn: a.Conn, Handle: a.Handle}, reply)
	if ch == nil {
		return
	}
	v := int32(CCCDNone)
	if on {
		cccd, ok := btCCCD(int(ch.Props), a.Indicate)
		if !ok {
			btFail(reply, btATT(6))
			return
		}
		v = cccd
		b.follow(c, a.Handle, ch)
	} else {
		ch.StopValues()
		delete(c.subscribed, a.Handle)
	}
	done := c.op(reply)
	ch.WriteCCCD(v, func(res GattResult) {
		b.post(func() {
			e := btResult(res)
			switch {
			case on && e != "":
				if c.chars[a.Handle] == ch && !c.subscribed[a.Handle] {
					ch.StopValues()
				}
				done(nil, errors.New(e))
			case on:
				c.subscribed[a.Handle] = true
				btOK(done)
			case e != "" && e != "disconnected":
				done(nil, errors.New(e))
			default:
				btOK(done)
			}
		})
	})
}

// --- The peripheral role.

func (b *bluetooth) peripheral(reply func([]byte, error)) bool {
	if b.adapter == nil || !b.adapter.LowEnergy {
		btFail(reply, spec.StateUnsupported)
		return false
	}
	if !b.adapter.Peripheral {
		btFail(reply, "not available: this Bluetooth adapter has no peripheral role")
		return false
	}
	return b.on(reply)
}

// protection is a GattProtectionLevel: encrypted's bit set in perm, the
// link must be encrypted.
func protection(perm, encrypted int) int32 {
	if perm&encrypted != 0 {
		return GattEncryptionRequired
	}
	return GattPlain
}

func (b *bluetooth) serve(a spec.ServeArgs, reply func([]byte, error)) {
	if !b.peripheral(reply) {
		return
	}
	if old := b.servers[a.ID]; old != nil {
		b.closeServer(old)
	}
	s := &btServer{id: a.ID, chars: map[uint32]*btLocal{}, reply: reply}
	b.servers[a.ID] = s
	fail := func(why string) {
		if !s.closed {
			s.reply = nil
			b.closeServer(s)
			btFail(reply, why)
		}
	}
	var service func(i int)
	service = func(i int) {
		if s.closed {
			return
		}
		if i == len(a.Services) {
			for _, p := range s.providers {
				p.p.OnStatus(func(st, e int32) { b.post(func() { b.providerStatus(p, st, e) }) })
				// Published: centrals connected to the computer find it.
				if err := p.p.Advertise(true, false, nil); err != nil {
					fail(btWhy(err))
					return
				}
			}
			s.ready = true
			s.reply = nil
			btOK(reply)
			b.bind()
			return
		}
		ls := a.Services[i]
		u, ok := spec.ParseUUID(ls.UUID)
		if !ok {
			fail("failed: bad service UUID " + ls.UUID)
			return
		}
		BTCreateProvider(u, func(p *BTProvider, e int32, err error) {
			b.post(func() {
				if s.closed {
					if p != nil {
						p.Close()
					}
					return
				}
				if p == nil {
					why := btError(e)
					if err != nil || why == "" {
						why = btWhy(err)
					}
					fail(why)
					return
				}
				prov := &btProvider{p: p, uuid: spec.FormatUUID(u), server: s}
				s.providers = append(s.providers, prov)
				var char func(j int)
				char = func(j int) {
					if s.closed {
						return
					}
					if j == len(ls.Characteristics) {
						service(i + 1)
						return
					}
					lc := ls.Characteristics[j]
					cu, ok := spec.ParseUUID(lc.UUID)
					if !ok {
						fail("failed: bad characteristic UUID " + lc.UUID)
						return
					}
					params := BTLocalCharParams{UUID: cu, Props: uint32(lc.Properties & 0xff),
						ReadProt:  protection(lc.Permissions, spec.PermReadEncrypted),
						WriteProt: protection(lc.Permissions, spec.PermWriteEncrypted),
						Static:    lc.Value}
					var descs []spec.LocalDescriptor
					for _, d := range lc.Descriptors {
						switch btWindowsDescriptor(d.UUID) {
						case "user":
							params.UserDescription, params.HasUserDescription = string(d.Value), true
						case "format":
							if len(d.Value) == 7 {
								params.Formats = append(params.Formats, [7]byte(d.Value))
							}
						case "":
							descs = append(descs, d)
						}
					}
					p.AddCharacteristic(params, func(c *BTLocalChar, e int32, err error) {
						b.post(func() {
							if s.closed {
								return
							}
							if c == nil {
								why := btError(e)
								if err != nil || why == "" {
									why = btWhy(err)
								}
								fail(why)
								return
							}
							l := &btLocal{lc: c, handle: lc.Handle, clients: map[string]*BTClient{}}
							s.chars[lc.Handle] = l
							c.OnRequests(func(r *BTRequest) {
								b.post(func() { b.request(s, l, r, false) })
							}, func(r *BTRequest) {
								b.post(func() { b.request(s, l, r, true) })
							})
							c.OnSubscribers(func(cl []*BTClient) { b.post(func() { b.subscribers(s, l, cl) }) })
							var desc func(k int)
							desc = func(k int) {
								if s.closed {
									return
								}
								if k == len(descs) {
									char(j + 1)
									return
								}
								du, ok := spec.ParseUUID(descs[k].UUID)
								if !ok {
									fail("failed: bad descriptor UUID " + descs[k].UUID)
									return
								}
								c.AddDescriptor(du, descs[k].Value, func(e int32, err error) {
									b.post(func() {
										if why := btError(e); err != nil || why != "" {
											if why == "" {
												why = btWhy(err)
											}
											fail(why)
											return
										}
										desc(k + 1)
									})
								})
							}
							desc(0)
						})
					})
				}
				char(0)
			})
		})
	}
	service(0)
}

func (b *bluetooth) request(s *btServer, l *btLocal, r *BTRequest, write bool) {
	if s.closed {
		r.Fail(0x0e)
		return
	}
	e := spec.Event{Type: spec.EventRead, Server: s.id, Handle: l.handle, Offset: r.Offset,
		Central: &spec.Device{ID: btCentralID(r.Central)}}
	if write {
		e.Type, e.Value, e.Response = spec.EventWrite, r.Value, r.Response
	}
	if write && !r.Response {
		r.Drop() // no one answers a write without response
	} else {
		b.nextReq++
		e.Request = b.nextReq
		b.requests[e.Request] = &btServerRequest{server: s.id, r: r}
	}
	b.send(e)
	b.flush()
}

func (b *bluetooth) respond(a spec.Respond) {
	q := b.requests[a.Request]
	if q == nil {
		return
	}
	delete(b.requests, a.Request)
	if a.Error != 0 {
		q.r.Fail(byte(a.Error))
	} else {
		q.r.Respond(a.Value)
	}
}

func (b *bluetooth) subscribers(s *btServer, l *btLocal, clients []*BTClient) {
	if s.closed {
		for _, c := range clients {
			c.Release()
		}
		return
	}
	ids := make([]string, 0, len(clients))
	for _, c := range l.clients {
		c.Release()
	}
	l.clients = map[string]*BTClient{}
	for _, c := range clients {
		id := btCentralID(c.Central)
		ids = append(ids, id)
		if old := l.clients[id]; old != nil {
			old.Release()
		}
		l.clients[id] = c
	}
	on, off := btDiff(l.subscribers, ids)
	l.subscribers = ids
	for _, id := range on {
		b.send(spec.Event{Type: spec.EventSubscription, Server: s.id, Handle: l.handle, Central: &spec.Device{ID: id}, On: true})
	}
	for _, id := range off {
		b.send(spec.Event{Type: spec.EventSubscription, Server: s.id, Handle: l.handle, Central: &spec.Device{ID: id}})
	}
	b.flush()
}

func (b *bluetooth) notify(a spec.Notify, reply func([]byte, error)) {
	s := b.servers[a.Server]
	if s == nil || !s.ready {
		btFail(reply, "not found")
		return
	}
	l := s.chars[a.Handle]
	if l == nil {
		btFail(reply, "not found")
		return
	}
	var to []*BTClient // nil: every subscriber
	if len(a.Centrals) > 0 {
		to = []*BTClient{}
		for _, id := range a.Centrals {
			if c := l.clients[id]; c != nil {
				to = append(to, c)
			}
		}
	}
	l.lc.Notify(a.Value, to, func(err error) {
		b.post(func() {
			if err != nil {
				btFail(reply, btWhy(err))
				return
			}
			btOK(reply)
		})
	})
}

func (b *bluetooth) providerStatus(p *btProvider, st, e int32) {
	s := p.server
	if s.closed || st != ProviderAborted {
		return
	}
	why := btError(e)
	if why == "" {
		why = "failed: Windows stopped the service"
	}
	if ad := p.advert; ad != nil && b.adverts[ad.id] == ad {
		b.endAdvert(ad, why)
	}
	b.closeServer(s)
	b.send(spec.Event{Type: spec.EventServerStopped, Server: s.id, Error: why})
	b.flush()
}

func (b *bluetooth) closeServer(s *btServer) {
	if s.closed {
		return
	}
	s.closed = true
	if b.servers[s.id] == s {
		delete(b.servers, s.id)
	}
	if r := s.reply; r != nil {
		s.reply = nil
		btFail(r, "canceled")
	}
	for id, q := range b.requests {
		if q.server == s.id {
			q.r.Fail(0x0e) // Unlikely Error: the server went
			delete(b.requests, id)
		}
	}
	for _, l := range s.chars {
		for _, c := range l.clients {
			c.Release()
		}
	}
	for _, p := range s.providers {
		if ad := p.advert; ad != nil {
			delete(ad.bound, p.uuid)
			p.advert = nil
		}
		p.p.Close()
	}
	s.providers = nil
}

// bind makes the providers of the services adverts advertise
// discoverable, as their servers are ready.
func (b *bluetooth) bind() {
	for _, ad := range b.adverts {
		for _, u := range ad.services {
			if ad.bound[u] != nil {
				continue
			}
			p := b.provider(u)
			if p == nil {
				continue
			}
			var data []byte
			for k, v := range ad.args.ServiceData {
				if spec.Normalize(k) == u {
					data = v
				}
			}
			if err := p.p.Advertise(ad.args.Connectable, true, data); err != nil {
				b.endAdvert(ad, btWhy(err))
				break
			}
			p.advert = ad
			ad.bound[u] = p
		}
	}
}

// provider is a ready server's provider of the service, not advertising
// for another advertisement.
func (b *bluetooth) provider(uuid string) *btProvider {
	for _, s := range b.servers {
		if !s.ready || s.closed {
			continue
		}
		for _, p := range s.providers {
			if p.uuid == uuid && p.advert == nil {
				return p
			}
		}
	}
	return nil
}

func (b *bluetooth) advertise(a spec.AdvertiseArgs, reply func([]byte, error)) {
	if !b.peripheral(reply) {
		return
	}
	var services []string
	add := func(s string) {
		s = spec.Normalize(s)
		for _, x := range services {
			if x == s {
				return
			}
		}
		services = append(services, s)
	}
	for _, s := range a.Services {
		add(s)
	}
	for k := range a.ServiceData {
		add(k)
	}
	md := map[uint16][]byte{}
	for k, v := range a.ManufacturerData {
		n, err := strconv.ParseUint(k, 10, 16)
		if err != nil {
			btFail(reply, "failed: bad company identifier "+k)
			return
		}
		md[uint16(n)] = v
	}
	if len(md) == 0 && len(services) == 0 {
		btFail(reply, "not available: Windows advertises an app's manufacturer data and the services it serves, not a name alone")
		return
	}
	if old := b.adverts[a.ID]; old != nil {
		b.endAdvert(old, "")
	}
	ad := &btAdvert{id: a.ID, args: a, services: services, bound: map[string]*btProvider{}, reply: reply}
	b.adverts[a.ID] = ad
	if len(md) > 0 {
		var pub *BTPublisher
		var err error
		pub, err = NewBTPublisher(md, a.IncludeTxPower, func(st, e int32) {
			b.post(func() { b.publisherStatus(ad, pub, st, e) })
		})
		if err == nil {
			ad.publisher = pub
			ad.waiting++
			err = pub.Start()
		}
		if err != nil {
			b.endAdvert(ad, btWhy(err))
			return
		}
	}
	b.bind()
	if b.adverts[a.ID] == ad && ad.waiting == 0 && ad.reply != nil {
		r := ad.reply
		ad.reply = nil
		btOK(r)
	}
}

func (b *bluetooth) publisherStatus(ad *btAdvert, pub *BTPublisher, st, e int32) {
	if b.adverts[ad.id] != ad || ad.publisher != pub {
		return
	}
	switch st {
	case PublisherStarted:
		if ad.waiting > 0 {
			ad.waiting--
			if ad.waiting == 0 && ad.reply != nil {
				r := ad.reply
				ad.reply = nil
				btOK(r)
			}
		}
	case PublisherAborted:
		why := btError(e)
		if s := b.current(); s != spec.StateOn {
			why = s
		} else if why == "" {
			why = "failed: Windows stopped advertising"
		}
		b.endAdvert(ad, why)
		b.flush()
	}
}

// endAdvert stops an advertisement: its "advertise" fails with why if it
// waits, else (why set) an "advertiseStopped" event says it.
func (b *bluetooth) endAdvert(ad *btAdvert, why string) {
	if b.adverts[ad.id] != ad {
		return
	}
	delete(b.adverts, ad.id)
	if ad.publisher != nil {
		ad.publisher.Close()
		ad.publisher = nil
	}
	for _, p := range ad.bound {
		p.advert = nil
		if !p.server.closed {
			p.p.Advertise(true, false, nil) // served still, not discoverable
		}
	}
	clear(ad.bound)
	switch {
	case ad.reply != nil:
		r := ad.reply
		ad.reply = nil
		if why == "" {
			why = "canceled"
		}
		btFail(r, why)
	case why != "":
		b.send(spec.Event{Type: spec.EventAdvertiseStopped, ID: ad.id, Error: why})
	}
	b.bind() // another advertisement may want those providers
}

// --- The module's methods.

func (b *bluetooth) methods() map[string]asyncMethod {
	decode := func(args []byte, v any, reply func([]byte, error)) bool {
		if len(args) == 0 {
			args = []byte("{}")
		}
		if err := json.Unmarshal(args, v); err != nil {
			btFail(reply, "bad args: "+err.Error())
			return false
		}
		return true
	}
	adapter := func(f func(args []byte, reply func([]byte, error))) asyncMethod {
		return func(args []byte, reply func([]byte, error)) {
			b.withAdapter(func() { f(args, reply) })
		}
	}
	conn := func(f func(c *btConn, args []byte, reply func([]byte, error))) asyncMethod {
		return func(args []byte, reply func([]byte, error)) {
			var a spec.Conn
			if !decode(args, &a, reply) {
				return
			}
			if c := b.up(a.Conn, reply); c != nil {
				f(c, args, reply)
			}
		}
	}
	return map[string]asyncMethod{
		"state": adapter(func(_ []byte, reply func([]byte, error)) {
			btJSON(reply, spec.State{State: b.current()})
		}),
		"request": adapter(func(_ []byte, reply func([]byte, error)) {
			if b.adapter == nil || !b.adapter.LowEnergy {
				btJSON(reply, spec.State{State: spec.StateUnsupported})
				return
			}
			BTRequestAccess(func(st int32) {
				b.post(func() {
					b.denied = st == RadioAccessDeniedByUser || st == RadioAccessDeniedBySystem
					b.radioChanged()
					btJSON(reply, spec.State{State: b.current()})
				})
			})
		}),
		"next": func(_ []byte, reply func([]byte, error)) {
			b.following = true
			b.waiting = reply
			b.flush()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			b.following = false
			if w := b.waiting; w != nil {
				b.waiting = nil
				w(nil, errors.New("stopped"))
			}
			q := b.queue[:0]
			for _, e := range b.queue {
				if e.Type != spec.EventAdvertisement {
					q = append(q, e)
				}
			}
			b.queue = q
			btOK(reply)
		},
		"scan": adapter(func(args []byte, reply func([]byte, error)) {
			var a spec.ScanArgs
			if decode(args, &a, reply) {
				b.scan(a, reply)
			}
		}),
		"stopScan": func(args []byte, reply func([]byte, error)) {
			var a spec.ID
			if decode(args, &a, reply) {
				b.stopScan(a.ID)
				btOK(reply)
			}
		},
		"stopBackgroundScan": func(_ []byte, reply func([]byte, error)) { btOK(reply) },
		"choose": adapter(func(args []byte, reply func([]byte, error)) {
			var a spec.ChooseArgs
			if decode(args, &a, reply) {
				b.choose(a, reply)
			}
		}),
		"known": func(args []byte, reply func([]byte, error)) {
			var a spec.KnownArgs
			if decode(args, &a, reply) {
				b.known(a.IDs, reply)
			}
		},
		"connected": func(args []byte, reply func([]byte, error)) {
			var a spec.ConnectedArgs
			if decode(args, &a, reply) {
				b.connectedDevices(a.Services, reply)
			}
		},
		"connect": adapter(func(args []byte, reply func([]byte, error)) {
			var a spec.ConnectArgs
			if decode(args, &a, reply) {
				b.connect(a, reply)
			}
		}),
		"disconnect": func(args []byte, reply func([]byte, error)) {
			var a spec.Conn
			if !decode(args, &a, reply) {
				return
			}
			if c := b.conns[a.Conn]; c != nil {
				b.endConn(c, "canceled")
			}
			btOK(reply)
		},
		"services": conn(func(c *btConn, _ []byte, reply func([]byte, error)) {
			b.services(c, c.op(reply))
		}),
		"read": func(args []byte, reply func([]byte, error)) {
			var a spec.Attr
			if !decode(args, &a, reply) {
				return
			}
			if c, ch := b.char(a, reply); ch != nil {
				ch.Read(b.readDone(c.op(reply)))
			}
		},
		"write": func(args []byte, reply func([]byte, error)) {
			var a spec.Write
			if !decode(args, &a, reply) {
				return
			}
			if c, ch := b.char(spec.Attr{Conn: a.Conn, Handle: a.Handle}, reply); ch != nil {
				ch.Write(a.Value, a.Response, b.writeDone(c.op(reply)))
			}
		},
		"readDescriptor": func(args []byte, reply func([]byte, error)) {
			var a spec.Attr
			if !decode(args, &a, reply) {
				return
			}
			if c, d := b.desc(a, reply); d != nil {
				d.Read(b.readDone(c.op(reply)))
			}
		},
		"writeDescriptor": func(args []byte, reply func([]byte, error)) {
			var a spec.Write
			if !decode(args, &a, reply) {
				return
			}
			if c, d := b.desc(spec.Attr{Conn: a.Conn, Handle: a.Handle}, reply); d != nil {
				d.Write(a.Value, b.writeDone(c.op(reply)))
			}
		},
		"subscribe": func(args []byte, reply func([]byte, error)) {
			var a spec.Subscribe
			if decode(args, &a, reply) {
				b.subscribe(a, true, reply)
			}
		},
		"unsubscribe": func(args []byte, reply func([]byte, error)) {
			var a spec.Subscribe
			if decode(args, &a, reply) {
				b.subscribe(a, false, reply)
			}
		},
		// Windows negotiates the MTU itself: the session's MaxPduSize.
		"mtu": conn(func(c *btConn, _ []byte, reply func([]byte, error)) {
			m := c.dev.MaxPDU()
			btJSON(reply, spec.MTU{MTU: m, WriteWithResponse: m - 3, WriteWithoutResponse: m - 3})
		}),
		"requestMtu": conn(func(c *btConn, _ []byte, reply func([]byte, error)) {
			m := c.dev.MaxPDU()
			btJSON(reply, spec.MTU{MTU: m, WriteWithResponse: m - 3, WriteWithoutResponse: m - 3})
		}),
		"rssi": conn(func(_ *btConn, _ []byte, reply func([]byte, error)) {
			btFail(reply, "not available: Windows doesn't read a connected device's signal strength")
		}),
		// Windows 11's preferred connection parameters; before, the system
		// decides.
		"priority": conn(func(c *btConn, args []byte, reply func([]byte, error)) {
			var a spec.Priority
			json.Unmarshal(args, &a)
			which := BTPriorityBalanced
			switch a.Priority {
			case spec.PriorityHigh:
				which = BTPriorityThroughput
			case spec.PriorityLowPower:
				which = BTPriorityPower
			}
			c.dev.RequestPriority(which)
			btOK(reply)
		}),
		"advertise": adapter(func(args []byte, reply func([]byte, error)) {
			var a spec.AdvertiseArgs
			if decode(args, &a, reply) {
				b.advertise(a, reply)
			}
		}),
		"stopAdvertise": func(args []byte, reply func([]byte, error)) {
			var a spec.ID
			if !decode(args, &a, reply) {
				return
			}
			if ad := b.adverts[a.ID]; ad != nil {
				b.endAdvert(ad, "")
			}
			btOK(reply)
		},
		"serve": adapter(func(args []byte, reply func([]byte, error)) {
			var a spec.ServeArgs
			if decode(args, &a, reply) {
				b.serve(a, reply)
			}
		}),
		"unserve": func(args []byte, reply func([]byte, error)) {
			var a spec.ID
			if !decode(args, &a, reply) {
				return
			}
			if s := b.servers[a.ID]; s != nil {
				b.closeServer(s)
				b.bind()
			}
			btOK(reply)
		},
		"respond": func(args []byte, reply func([]byte, error)) {
			var a spec.Respond
			if decode(args, &a, reply) {
				b.respond(a)
				btOK(reply)
			}
		},
		"notify": func(args []byte, reply func([]byte, error)) {
			var a spec.Notify
			if decode(args, &a, reply) {
				b.notify(a, reply)
			}
		},
		"restore": func(_ []byte, reply func([]byte, error)) { btOK(reply) },
	}
}
