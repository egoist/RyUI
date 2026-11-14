//go:build linux && !android

package linux

import (
	"errors"
	"sort"
	"strings"
	"time"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	"github.com/egoist/asuka/internal/bluez"
	. "github.com/egoist/asuka/internal/gtk"
)

// The central role: scans through the adapter's discovery, connections
// through the devices' Connect, their GATT attributes through BlueZ's
// objects of them.

// btScan is a scan of the app's.
type btScan struct {
	args     spec.ScanArgs
	services []string // normalized
	reply    func([]byte, error)
	running  bool            // answered: discovery runs with a filter that has it
	seen     map[string]bool // the devices it heard (by path)
}

// btDiscovery is the adapter's discovery, as the app's scans want it:
// one call to BlueZ at a time.
type btDiscovery struct {
	busy    bool
	on      bool   // StartDiscovery succeeded
	filter  string // the filter set ("": none)
	adapter string
}

func (b *bluetooth) scan(a spec.ScanArgs, reply func([]byte, error)) {
	if s := b.current(); s != spec.StateOn {
		btFail(reply, s)
		return
	}
	s := &btScan{args: a, reply: reply, seen: map[string]bool{}}
	for _, u := range a.Services {
		s.services = append(s.services, spec.Normalize(u))
	}
	b.scans[a.ID] = s
	b.reconcile()
}

func (b *bluetooth) stopScan(id uint32) {
	s := b.scans[id]
	if s == nil {
		return
	}
	delete(b.scans, id)
	if s.reply != nil {
		r := s.reply
		s.reply = nil
		btOK(r)
	}
	b.reconcile()
}

// endScan ends a scan the platform stopped ("scanStopped", with why).
func (b *bluetooth) endScan(id uint32, why string) {
	s := b.scans[id]
	if s == nil {
		return
	}
	delete(b.scans, id)
	if s.reply != nil {
		r := s.reply
		s.reply = nil
		btFail(r, why)
		return
	}
	b.queue = append(b.queue, spec.Event{Type: spec.EventScanStopped, Scan: id, Error: why})
}

// reconcile brings the adapter's discovery to what the scans want: the
// union of their filters set, discovery started while any runs, stopped
// once none does; then the scans waiting start.
func (b *bluetooth) reconcile() {
	d := &b.disc
	if d.busy {
		return
	}
	var args []spec.ScanArgs
	for _, id := range btKeys(b.scans) {
		args = append(args, b.scans[id].args)
	}
	if len(args) == 0 {
		if d.on {
			d.busy = true
			DBusCall(b.conn, bluezName, d.adapter, ifAdapter, "StopDiscovery", 0, btCallTimeout, func(_ Ptr, _ error) {
				d.busy, d.on = false, false
				b.reconcile()
			})
		}
		return
	}
	adapter := b.adapter()
	if adapter == "" {
		b.failPendingScans(spec.StateUnsupported)
		return
	}
	if d.adapter != adapter {
		*d = btDiscovery{adapter: adapter}
	}
	uuids, dup := bluez.Filter(args)
	key := strings.Join(uuids, ",")
	if dup {
		key += "+dup"
	}
	if key != d.filter {
		filter := map[string]Ptr{"Transport": VStr("le"), "DuplicateData": VBool(dup)}
		if len(uuids) > 0 {
			filter["UUIDs"] = VStrings(uuids)
		}
		d.busy = true
		DBusCall(b.conn, bluezName, adapter, ifAdapter, "SetDiscoveryFilter", VTuple(VDict(filter)), btCallTimeout, func(_ Ptr, err error) {
			d.busy = false
			// A filter refused is the attempt's still: the scans running
			// keep the one before, and each scan matches its own
			// advertisements anyway.
			d.filter = key
			if err != nil {
				b.failPendingScans(bluez.ClientError(err, false))
			}
			b.reconcile()
			b.flush()
		})
		return
	}
	if !d.on {
		d.busy = true
		DBusCall(b.conn, bluezName, adapter, ifAdapter, "StartDiscovery", 0, btCallTimeout, func(_ Ptr, err error) {
			d.busy = false
			if err != nil {
				name, _ := bluez.Error(err)
				if name != "org.bluez.Error.InProgress" { // already discovering: ours
					b.failPendingScans(bluez.ClientError(err, false))
					b.reconcile()
					b.flush()
					return
				}
			}
			d.on = true
			b.reconcile()
			b.flush()
		})
		return
	}
	for _, id := range btKeys(b.scans) {
		s := b.scans[id]
		if s.running {
			continue
		}
		s.running = true
		r := s.reply
		s.reply = nil
		btOK(r)
		// The devices heard in this discovery (another app's, another
		// scan's) are heard: BlueZ tells only what changes.
		b.heardNow(id, s)
	}
	b.flush()
}

func (b *bluetooth) failPendingScans(why string) {
	for _, id := range btKeys(b.scans) {
		if s := b.scans[id]; !s.running {
			delete(b.scans, id)
			r := s.reply
			s.reply = nil
			btFail(r, why)
		}
	}
}

// heardNow tells a scan starting the devices the adapter hears now: a
// device with an RSSI was heard in the discovery running (BlueZ drops
// the RSSIs as discovery stops).
func (b *bluetooth) heardNow(id uint32, s *btScan) {
	var paths []string
	for p, o := range b.objects {
		if d := o[ifDevice]; d != nil && d["RSSI"] != nil {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	now := time.Now().UnixMilli()
	for _, p := range paths {
		d := bluez.ParseDevice(b.objects[p][ifDevice])
		if d.Adapter != b.disc.adapter || !d.Matches(s.services) {
			continue
		}
		s.seen[p] = true
		ad := d.Advertisement(now)
		b.queue = append(b.queue, spec.Event{Type: spec.EventAdvertisement, Scan: id, Advertisement: &ad})
	}
}

// heard tells the scans an advertisement of the device at path, if
// what changed is one: BlueZ caches the devices it ever heard, so a
// device is heard when its RSSI comes (or changes), or, for a scan that
// wants duplicates, what it advertises changes.
func (b *bluetooth) heard(path string, changed map[string]any, added bool) {
	if len(b.scans) == 0 || !b.disc.on {
		return
	}
	p := b.props(path, ifDevice)
	if p == nil || p["RSSI"] == nil {
		return
	}
	rssi := changed["RSSI"] != nil
	data := changed["ManufacturerData"] != nil || changed["ServiceData"] != nil || changed["UUIDs"] != nil ||
		changed["TxPower"] != nil || changed["Name"] != nil
	if !added && !rssi && !data {
		return
	}
	d := bluez.ParseDevice(p)
	if d.Adapter != b.disc.adapter {
		return
	}
	var ad *spec.Advertisement
	for _, id := range btKeys(b.scans) {
		s := b.scans[id]
		if !s.running || !d.Matches(s.services) {
			continue
		}
		if s.seen[path] && !s.args.Duplicates {
			continue
		}
		s.seen[path] = true
		if ad == nil {
			a := d.Advertisement(time.Now().UnixMilli())
			ad = &a
		}
		b.queue = append(b.queue, spec.Event{Type: spec.EventAdvertisement, Scan: id, Advertisement: ad})
	}
}

// devicePath is the object of the device with the address, on the
// adapter in use (another's if that one hasn't it), "" if none.
func (b *bluetooth) devicePath(addr string) string {
	adapter := b.adapter()
	found := ""
	for p, o := range b.objects {
		d := o[ifDevice]
		if d == nil {
			continue
		}
		if a, _ := d["Address"].(string); !strings.EqualFold(a, addr) {
			continue
		}
		if ad, _ := d["Adapter"].(string); ad == adapter {
			return p
		}
		if found == "" || p < found {
			found = p
		}
	}
	return found
}

func (b *bluetooth) device(path string) bluez.Device {
	return bluez.ParseDevice(b.props(path, ifDevice))
}

// known: the devices BlueZ knows of the addresses.
func (b *bluetooth) known(ids []string) []spec.Device {
	out := []spec.Device{}
	for _, id := range ids {
		if p := b.devicePath(id); p != "" {
			out = append(out, b.device(p).Spec())
		}
	}
	return out
}

// connectedDevices: those connected (by anyone), with one of the
// services if any are given.
func (b *bluetooth) connectedDevices(services []string) []spec.Device {
	for i, s := range services {
		services[i] = spec.Normalize(s)
	}
	var paths []string
	for p, o := range b.objects {
		if d := o[ifDevice]; d != nil && d["Connected"] == true {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	out := []spec.Device{}
	for _, p := range paths {
		d := b.device(p)
		if d.Matches(services) {
			out = append(out, d.Spec())
		}
	}
	return out
}

// btDev is a device the app's connections are to.
type btDev struct {
	path    string
	conns   map[uint32]*btConn
	calling bool // Device1.Connect asked
	ours    bool // the app connected it (Disconnect once its last connection goes)
	// The Disconnected signal's reason and words, for the disconnection
	// (Connected false) that follows.
	reason, words string
	notifying     map[string]bool // characteristics StartNotify'd (by path)
}

// btConn is one of the app's connections (Go's numbers), to a device.
type btConn struct {
	id      uint32
	dev     *btDev
	up      bool
	reply   func([]byte, error) // "connect"'s, while connecting
	auto    bool
	retry   uint32 // the next attempt's timer (AutoConnect)
	asked   bool   // "services" answered since the last change
	svcWait []func([]byte, error)
	handles map[string]uint32 // attribute paths' handles
	paths   map[uint32]string
	next    uint32
	calls   map[uint32]func([]byte, error) // the calls waiting on it
	nextOp  uint32
	subs    map[string]bool // characteristics subscribed (by path)
}

func (b *bluetooth) connect(a spec.ConnectArgs, reply func([]byte, error)) {
	if s := b.current(); s != spec.StateOn {
		btFail(reply, s)
		return
	}
	path := b.devicePath(a.Device)
	if path == "" {
		btFail(reply, "not found")
		return
	}
	if old := b.conns[a.Conn]; old != nil {
		b.endConn(old, "disconnected")
	}
	dev := b.devs[path]
	if dev == nil {
		dev = &btDev{path: path, conns: map[uint32]*btConn{}, notifying: map[string]bool{}}
		b.devs[path] = dev
	}
	c := &btConn{id: a.Conn, dev: dev, reply: reply, auto: a.AutoConnect, handles: map[string]uint32{},
		paths: map[uint32]string{}, calls: map[uint32]func([]byte, error){}, subs: map[string]bool{}}
	b.conns[a.Conn] = c
	dev.conns[a.Conn] = c
	if b.device(path).Connected {
		b.connUp(c) // connected already (by the system, another app, or another connection)
		return
	}
	b.connectDevice(dev)
}

// connectDevice asks BlueZ to connect the device, once for all its
// connections waiting.
func (b *bluetooth) connectDevice(dev *btDev) {
	if dev.calling {
		return
	}
	dev.calling = true
	DBusCall(b.conn, bluezName, dev.path, ifDevice, "Connect", 0, btNoTimeout, func(_ Ptr, err error) {
		dev.calling = false
		already := false
		if err != nil {
			if name, _ := bluez.Error(err); name == "org.bluez.Error.AlreadyConnected" {
				err, already = nil, true // by someone else, meanwhile
			} else if name == "org.bluez.Error.InProgress" {
				// Another's connect (another app's): Connected says if it
				// succeeds; if not, this one tries again.
				b.retryConnect(dev)
				return
			}
		}
		if err != nil {
			b.connectFailed(dev, err)
			return
		}
		dev.ours = !already
		if len(dev.conns) == 0 && dev.ours {
			// Every connection gave up as it connected.
			b.disconnectDevice(dev, nil)
			return
		}
		b.deviceConnected(dev.path)
		b.flush()
	})
}

// connectFailed fails the device's connections connecting, but those
// that wait for the device (AutoConnect), which try again.
func (b *bluetooth) connectFailed(dev *btDev, err error) {
	why := bluez.ConnectError(err)
	again := false
	for _, id := range btKeys(dev.conns) {
		c := dev.conns[id]
		if c.up {
			continue
		}
		if c.auto && bluez.Retry(err) && b.current() == spec.StateOn {
			again = true
			continue
		}
		b.endConn(c, why)
	}
	if again {
		// Not in range (or not answering): try again in a moment, as
		// Android's autoConnect waits for the device.
		b.retryConnect(dev)
	}
	if len(dev.conns) == 0 && b.devs[dev.path] == dev {
		delete(b.devs, dev.path)
	}
	b.flush()
}

// retryConnect connects the device again in a second, for its
// connections still connecting then.
func (b *bluetooth) retryConnect(dev *btDev) {
	var id uint32
	id = TimeoutAdd(1000, func() bool {
		waiting := false
		for _, c := range dev.conns {
			if c.retry == id {
				c.retry = 0
				waiting = true
			}
		}
		if waiting && b.devs[dev.path] == dev && !b.device(dev.path).Connected {
			b.connectDevice(dev)
		}
		return false
	})
	for _, c := range dev.conns {
		if !c.up {
			if c.retry != 0 {
				SourceRemove(c.retry)
			}
			c.retry = id
		}
	}
}

// connUp: the connection's up; "connect" answers.
func (b *bluetooth) connUp(c *btConn) {
	if c.up {
		return
	}
	c.up = true
	if c.retry != 0 {
		SourceRemove(c.retry)
		c.retry = 0
	}
	if r := c.reply; r != nil {
		c.reply = nil
		btOK(r)
	}
}

// deviceConnected: Connected turned true; the connections to it
// waiting are up.
func (b *bluetooth) deviceConnected(path string) {
	dev := b.devs[path]
	if dev == nil {
		return
	}
	dev.reason, dev.words = "", ""
	for _, id := range btKeys(dev.conns) {
		b.connUp(dev.conns[id])
	}
}

// deviceDisconnected: Connected turned false (or the device went): its
// connections end, with the reason the Disconnected signal gave.
func (b *bluetooth) deviceDisconnected(path string) {
	dev := b.devs[path]
	if dev == nil {
		return
	}
	reason := bluez.Reason(dev.reason)
	words := dev.words
	if b.current() == spec.StateOff {
		reason = spec.ReasonOff
	}
	if words == "" {
		words = "the device disconnected"
	}
	dev.reason, dev.words = "", ""
	dev.ours = false
	clear(dev.notifying)
	for _, id := range btKeys(dev.conns) {
		c := dev.conns[id]
		if !c.up {
			if dev.calling {
				continue // Connect answers
			}
			if p := b.props(path, ifDevice); p == nil {
				b.endConn(c, "not found")
			}
			continue
		}
		b.queue = append(b.queue, spec.Event{Type: spec.EventDisconnected, Conn: id, Reason: reason, Error: words})
		b.endConn(c, "disconnected")
	}
}

// disconnectedSignal is a device's Disconnected(s reason, s message).
func (b *bluetooth) disconnectedSignal(path string, params Ptr) {
	dev := b.devs[bluez.DevicePath(path)]
	if dev == nil {
		return
	}
	a, _ := VariantGo(params).([]any)
	if len(a) >= 2 {
		dev.reason, _ = a[0].(string)
		dev.words, _ = a[1].(string)
	}
}

// endConn forgets a connection: its calls waiting fail with why, as
// "connect" does if it was connecting; its subscriptions end.
func (b *bluetooth) endConn(c *btConn, why string) {
	if b.conns[c.id] == c {
		delete(b.conns, c.id)
	}
	dev := c.dev
	delete(dev.conns, c.id)
	if c.retry != 0 {
		SourceRemove(c.retry)
		c.retry = 0
	}
	if r := c.reply; r != nil {
		c.reply = nil
		btFail(r, why)
	}
	fail := why
	if fail != "disconnected" && c.up {
		fail = "disconnected"
	}
	c.up = false
	for _, id := range btKeys(c.calls) {
		f := c.calls[id]
		delete(c.calls, id)
		btFail(f, fail)
	}
	for _, f := range c.svcWait {
		btFail(f, fail)
	}
	c.svcWait = nil
	for p := range c.subs {
		delete(c.subs, p)
		b.dropNotify(dev, p)
	}
	if len(dev.conns) == 0 && !dev.calling && b.devs[dev.path] == dev {
		delete(b.devs, dev.path)
	}
}

func (b *bluetooth) disconnect(id uint32, reply func([]byte, error)) {
	c := b.conns[id]
	if c == nil {
		btOK(reply)
		return
	}
	dev := c.dev
	if r := c.reply; r != nil {
		c.reply = nil
		btFail(r, "canceled")
	}
	last := len(dev.conns) == 1
	b.endConn(c, "disconnected")
	if last && (dev.ours || dev.calling) {
		// The app's connection (or its attempt, which Disconnect
		// cancels): the system's own, another app's, stay.
		b.disconnectDevice(dev, reply)
		return
	}
	btOK(reply)
}

func (b *bluetooth) disconnectDevice(dev *btDev, reply func([]byte, error)) {
	DBusCall(b.conn, bluezName, dev.path, ifDevice, "Disconnect", 0, btCallTimeout, func(_ Ptr, _ error) {
		if reply != nil {
			btOK(reply)
		}
	})
}

// servicesResolved: BlueZ has the device's attributes; "services"
// waiting answer, and a connection that had them is told they changed.
func (b *bluetooth) servicesResolved(path string) {
	dev := b.devs[path]
	if dev == nil {
		return
	}
	for _, id := range btKeys(dev.conns) {
		c := dev.conns[id]
		if !c.up {
			continue
		}
		if c.asked && len(c.svcWait) == 0 {
			b.servicesChanged(c)
			continue
		}
		ws := c.svcWait
		c.svcWait = nil
		for _, f := range ws {
			btJSON(f, b.serviceTree(c))
		}
	}
}

func (b *bluetooth) services(id uint32, reply func([]byte, error)) {
	c := b.conns[id]
	if c == nil || !c.up {
		btFail(reply, "disconnected")
		return
	}
	if !b.device(c.dev.path).ServicesResolved {
		c.svcWait = append(c.svcWait, reply)
		return
	}
	btJSON(reply, b.serviceTree(c))
}

// handle is the connection's handle of the attribute at path.
func (c *btConn) handle(path string) uint32 {
	if h := c.handles[path]; h != 0 {
		return h
	}
	c.next++
	c.handles[path] = c.next
	c.paths[c.next] = path
	return c.next
}

// serviceTree is the device's attributes, in their order on the device
// (BlueZ's paths name their ATT handles, in hex of a fixed width).
func (b *bluetooth) serviceTree(c *btConn) []spec.Service {
	c.asked = true
	prefix := c.dev.path + "/"
	var svcs, chars, descs []string
	for p, o := range b.objects {
		if !strings.HasPrefix(p, prefix) {
			continue
		}
		switch {
		case o[ifGattService] != nil:
			svcs = append(svcs, p)
		case o[ifGattChar] != nil:
			chars = append(chars, p)
		case o[ifGattDesc] != nil:
			descs = append(descs, p)
		}
	}
	sort.Strings(svcs)
	sort.Strings(chars)
	sort.Strings(descs)
	out := []spec.Service{}
	index := map[string]int{}
	for _, p := range svcs {
		props := b.props(p, ifGattService)
		uuid, _ := props["UUID"].(string)
		primary, _ := props["Primary"].(bool)
		index[p] = len(out)
		out = append(out, spec.Service{Handle: c.handle(p), UUID: spec.Normalize(uuid), Primary: primary})
	}
	type at struct{ svc, char int }
	charAt := map[string]at{}
	for _, p := range chars {
		props := b.props(p, ifGattChar)
		svc, _ := props["Service"].(string)
		i, ok := index[svc]
		if !ok {
			continue
		}
		uuid, _ := props["UUID"].(string)
		charAt[p] = at{i, len(out[i].Characteristics)}
		out[i].Characteristics = append(out[i].Characteristics, spec.Characteristic{Handle: c.handle(p),
			UUID: spec.Normalize(uuid), Properties: bluez.Properties(bluez.Strings(props["Flags"]))})
	}
	for _, p := range descs {
		props := b.props(p, ifGattDesc)
		ch, _ := props["Characteristic"].(string)
		w, ok := charAt[ch]
		if !ok {
			continue
		}
		uuid, _ := props["UUID"].(string)
		cs := &out[w.svc].Characteristics[w.char]
		cs.Descriptors = append(cs.Descriptors, spec.Descriptor{Handle: c.handle(p), UUID: spec.Normalize(uuid)})
	}
	return out
}

// gattChanged: a GATT object under a device came or went; once the
// signals of it are in, a connection that has its services hears that
// they changed (BlueZ's Service Changed handling, re-discovery).
func (b *bluetooth) gattChanged(path string) {
	dp := bluez.DevicePath(path)
	dev := b.devs[dp]
	if dev == nil || b.changed[dp] || !b.device(dp).ServicesResolved {
		return // the first discovery's objects: ServicesResolved tells
	}
	b.changed[dp] = true
	IdleAdd(PriorityDefault, func() bool {
		delete(b.changed, dp)
		if b.devs[dp] != dev {
			return false
		}
		d := b.device(dp)
		if !d.Connected || !d.ServicesResolved || !b.hasServices(dp) {
			// A disconnection's (BlueZ takes the objects down before it
			// says Connected is false), or a re-discovery's first half.
			return false
		}
		for _, id := range btKeys(dev.conns) {
			if c := dev.conns[id]; c.up && c.asked {
				b.servicesChanged(c)
			}
		}
		b.flush()
		return false
	})
}

// hasServices reports whether BlueZ has GATT services of the device.
func (b *bluetooth) hasServices(dev string) bool {
	prefix := dev + "/"
	for p, o := range b.objects {
		if o[ifGattService] != nil && strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// servicesChanged tells the connection its handles are gone.
func (b *bluetooth) servicesChanged(c *btConn) {
	c.asked = false
	clear(c.handles)
	clear(c.paths)
	for p := range c.subs {
		delete(c.subs, p)
		b.dropNotify(c.dev, p)
	}
	b.queue = append(b.queue, spec.Event{Type: spec.EventServicesChanged, Conn: c.id})
}

// attr is the path of the connection's attribute with the handle, of
// the interface; it fails the call if there's none.
func (b *bluetooth) attr(conn, handle uint32, iface string, reply func([]byte, error)) (*btConn, string, bool) {
	c := b.conns[conn]
	if c == nil || !c.up {
		btFail(reply, "disconnected")
		return nil, "", false
	}
	p := c.paths[handle]
	if p == "" || b.props(p, iface) == nil {
		btFail(reply, "not found")
		return nil, "", false
	}
	return c, p, true
}

// call runs a call on the connection's attribute at path, after those
// before it on the attribute: answered by finish, unless the connection
// ended first (its calls then failed).
func (b *bluetooth) call(c *btConn, path string, reply func([]byte, error), start func(finish func([]byte, error))) {
	c.nextOp++
	id := c.nextOp
	c.calls[id] = reply
	b.enqueue(path, func(next func()) {
		if c.calls[id] == nil {
			next()
			return
		}
		start(func(r []byte, err error) {
			next()
			if f := c.calls[id]; f != nil {
				delete(c.calls, id)
				f(r, err)
			}
		})
	})
}

func (b *bluetooth) read(a spec.Attr, iface string, reply func([]byte, error)) {
	c, p, ok := b.attr(a.Conn, a.Handle, iface, reply)
	if !ok {
		return
	}
	b.call(c, p, reply, func(finish func([]byte, error)) {
		DBusCall(b.conn, bluezName, p, iface, "ReadValue", VTuple(VDict(nil)), btCallTimeout, func(r Ptr, err error) {
			if err != nil {
				finish(nil, errors.New(bluez.ClientError(err, false)))
				return
			}
			v := Sys.VariantGetChildValue(r, 0)
			out := VariantBytes(v)
			Sys.VariantUnref(v)
			if out == nil {
				out = []byte{}
			}
			finish(out, nil)
		})
	})
}

func (b *bluetooth) write(a spec.Write, iface string, reply func([]byte, error)) {
	c, p, ok := b.attr(a.Conn, a.Handle, iface, reply)
	if !ok {
		return
	}
	value := a.Value
	b.call(c, p, reply, func(finish func([]byte, error)) {
		opts := map[string]Ptr{}
		if iface == ifGattChar {
			kind := "command"
			if a.Response {
				kind = "request"
			}
			opts["type"] = VStr(kind)
		}
		DBusCall(b.conn, bluezName, p, iface, "WriteValue", VTuple(VByteArray(value), VDict(opts)), btCallTimeout, func(_ Ptr, err error) {
			if err != nil {
				finish(nil, errors.New(bluez.ClientError(err, true)))
				return
			}
			finish([]byte("{}"), nil)
		})
	})
}

// subscribeChar turns notifications on: StartNotify (BlueZ writes the
// CCCD; it enables notifications where the characteristic has both).
// The device's connections share BlueZ's one session of it.
func (b *bluetooth) subscribeChar(a spec.Subscribe, reply func([]byte, error)) {
	c, p, ok := b.attr(a.Conn, a.Handle, ifGattChar, reply)
	if !ok {
		return
	}
	props := bluez.Properties(bluez.Strings(b.props(p, ifGattChar)["Flags"]))
	if props&(spec.PropNotify|spec.PropIndicate) == 0 {
		btFail(reply, "att 6: the characteristic doesn't notify")
		return
	}
	dev := c.dev
	b.call(c, p, reply, func(finish func([]byte, error)) {
		if dev.notifying[p] {
			c.subs[p] = true
			finish([]byte("{}"), nil)
			return
		}
		DBusCall(b.conn, bluezName, p, ifGattChar, "StartNotify", 0, btCallTimeout, func(_ Ptr, err error) {
			if err != nil {
				finish(nil, errors.New(bluez.ClientError(err, true)))
				return
			}
			if b.conns[c.id] == c {
				dev.notifying[p] = true
				c.subs[p] = true
			}
			finish([]byte("{}"), nil)
		})
	})
}

func (b *bluetooth) unsubscribeChar(a spec.Attr, reply func([]byte, error)) {
	c := b.conns[a.Conn]
	if c == nil {
		btOK(reply)
		return
	}
	p := c.paths[a.Handle]
	if p == "" || !c.subs[p] {
		btOK(reply)
		return
	}
	delete(c.subs, p)
	b.dropNotify(c.dev, p)
	btOK(reply)
}

// dropNotify ends BlueZ's notification session of the characteristic
// once no connection to the device is subscribed.
func (b *bluetooth) dropNotify(dev *btDev, path string) {
	if !dev.notifying[path] {
		return
	}
	for _, c := range dev.conns {
		if c.subs[path] {
			return
		}
	}
	delete(dev.notifying, path)
	if b.props(path, ifGattChar) == nil {
		return // gone with the connection
	}
	b.enqueue(path, func(next func()) {
		if dev.notifying[path] {
			next() // subscribed again meanwhile
			return
		}
		DBusCall(b.conn, bluezName, path, ifGattChar, "StopNotify", 0, btCallTimeout, func(_ Ptr, _ error) { next() })
	})
}

// notified is a characteristic's new value (a notification, an
// indication, or a read's): the connections subscribed to it get it.
func (b *bluetooth) notified(path string, value []byte) {
	dev := b.devs[bluez.DevicePath(path)]
	if dev == nil || !dev.notifying[path] {
		return
	}
	for _, id := range btKeys(dev.conns) {
		c := dev.conns[id]
		if c.up && c.subs[path] {
			if h := c.handles[path]; h != 0 {
				b.queue = append(b.queue, spec.Event{Type: spec.EventValue, Conn: id, Handle: h, Value: value})
			}
		}
	}
}

// mtu is the connection's ATT MTU: BlueZ's characteristics have it
// (MTU, BlueZ 5.62 on); 23 (the least) where they don't say.
func (b *bluetooth) mtu(id uint32, reply func([]byte, error)) {
	c := b.conns[id]
	if c == nil || !c.up {
		btFail(reply, "disconnected")
		return
	}
	mtu := 0
	prefix := c.dev.path + "/"
	for p, o := range b.objects {
		if ch := o[ifGattChar]; ch != nil && strings.HasPrefix(p, prefix) {
			if n, ok := bluez.Int(ch["MTU"]); ok && n > mtu {
				mtu = n
			}
		}
	}
	mtu = max(mtu, 23)
	btJSON(reply, spec.MTU{MTU: mtu, WriteWithResponse: mtu - 3, WriteWithoutResponse: mtu - 3})
}

// rssi: BlueZ has a device's RSSI only while discovering (from its
// advertisements), none for a connection's link.
func (b *bluetooth) rssi(id uint32, reply func([]byte, error)) {
	c := b.conns[id]
	if c == nil || !c.up {
		btFail(reply, "disconnected")
		return
	}
	if n, ok := bluez.Int(b.props(c.dev.path, ifDevice)["RSSI"]); ok {
		btJSON(reply, spec.RSSI{RSSI: n})
		return
	}
	btFail(reply, "not available: BlueZ reads no connected device's signal strength")
}
