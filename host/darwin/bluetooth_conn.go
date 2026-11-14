//go:build darwin && !ios

package darwin

import (
	"errors"
	"fmt"
	"slices"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	. "github.com/egoist/asuka/internal/darwin"
)

// The central role's connections: a btPeer is a CBPeripheral, with the
// Go connections (Conn) that use it (Core Bluetooth has one link to a
// peripheral for the app, however many Go makes), its GATT attributes
// by the handles "services" gave them, and the calls waiting for the
// peripheral's answers, in the order they were made (Core Bluetooth
// answers each attribute's in order).

type btPeer struct {
	obj ID
	id  string

	up            bool // connected (didConnectPeripheral, until didDisconnect)
	asked         bool // connectPeripheral sent, not answered
	conns         []uint32
	connecting    map[uint32]func([]byte, error)
	disconnecting []func([]byte, error)

	// The attributes by handle, and their handles; the tree "services"
	// answers while it holds.
	attrs      map[uint32]btAttr
	handles    map[ID]uint32
	nextHandle uint32
	tree       []spec.Service
	treeOK     bool

	// Discovering: those waiting, the answers still to come, the first
	// error, whether the services changed meanwhile (discover again).
	discovering []*btWait
	pending     int
	discErr     error
	again       bool

	waits     map[btOp][]*btWait
	rssiWaits []*btWait
	writes    []btQueuedWrite        // writes without response waiting for room
	subs      map[ID]map[uint32]bool // a characteristic's subscribed connections
}

// Attribute kinds.
const (
	attrService = iota
	attrChar
	attrDesc
)

type btAttr struct {
	obj  ID
	kind int
	svc  uint32 // its service's handle (a service's own)
	char uint32 // a descriptor's characteristic's handle
}

// Operations that wait for a peripheral's answer, per attribute.
const (
	opRead = iota
	opWrite
	opNotify
	opReadDesc
	opWriteDesc
)

type btOp struct {
	obj ID
	op  int
}

// btWait is a call waiting for a peripheral's answer. reply is nil once
// its connection went (its answer, still to come, is dropped).
type btWait struct {
	conn  uint32
	reply func([]byte, error)
	// issued: a setNotifyValue sent for it (a notify op's answer is the
	// issued one's, and the followers' that came after it).
	issued bool
}

type btQueuedWrite struct {
	char ID
	data ID // retained
	wait *btWait
}

// peerFor is the peer of a peripheral, made (and the peripheral kept)
// the first time.
func (b *btModule) peerFor(p ID) *btPeer {
	if peer := b.peerByObj[p]; peer != nil {
		return peer
	}
	if b.peers == nil {
		b.peers, b.peerByObj, b.conns = map[string]*btPeer{}, map[ID]*btPeer{}, map[uint32]*btPeer{}
	}
	peer := &btPeer{obj: Retain(p), id: peerID(p), connecting: map[uint32]func([]byte, error){},
		attrs: map[uint32]btAttr{}, handles: map[ID]uint32{}, waits: map[btOp][]*btWait{}, subs: map[ID]map[uint32]bool{}}
	Send(p, "setDelegate:", uintptr(b.delegate))
	b.peers[btIDKey(peer.id)] = peer
	b.peerByObj[p] = peer
	return peer
}

func (p *btPeer) device() spec.Device {
	return spec.Device{ID: p.id, Name: GoString(Send(p.obj, "name"))}
}

// lookup finds a device by its ID: one heard or found already, else
// one Core Bluetooth still knows.
func (b *btModule) lookup(id string) *btPeer {
	if p := b.peers[btIDKey(id)]; p != nil {
		return p
	}
	ps := b.retrieve([]string{id})
	if len(ps) == 0 {
		return nil
	}
	return ps[0]
}

// retrieve is retrievePeripheralsWithIdentifiers: (IDs that aren't UUIDs
// left out).
func (b *btModule) retrieve(ids []string) []*btPeer {
	var uuids []ID
	for _, id := range ids {
		u := Send(Send(Class("NSUUID"), "alloc"), "initWithUUIDString:", uintptr(String(id)))
		if u != 0 {
			uuids = append(uuids, Autorelease(u))
		}
	}
	if len(uuids) == 0 {
		return nil
	}
	var out []*btPeer
	for _, p := range ArrayItems(Send(b.central, "retrievePeripheralsWithIdentifiers:", uintptr(Array(uuids...)))) {
		out = append(out, b.peerFor(p))
	}
	return out
}

func (b *btModule) known(args []byte, reply func([]byte, error)) {
	var a spec.KnownArgs
	if !btArgs(args, &a, reply) {
		return
	}
	b.onCentral(reply, func() {
		out := []spec.Device{}
		for _, p := range b.retrieve(a.IDs) {
			out = append(out, p.device())
		}
		btJSON(reply, out)
	})
}

func (b *btModule) connectedDevices(args []byte, reply func([]byte, error)) {
	var a spec.ConnectedArgs
	if !btArgs(args, &a, reply) {
		return
	}
	services, ok := btUUIDs(a.Services, reply)
	if !ok {
		return
	}
	if len(services) == 0 {
		// Core Bluetooth lists only those with one of the services.
		btJSON(reply, []spec.Device{})
		return
	}
	b.onCentral(reply, func() {
		out := []spec.Device{}
		for _, p := range ArrayItems(Send(b.central, "retrieveConnectedPeripheralsWithServices:", uintptr(cbUUIDs(services)))) {
			out = append(out, b.peerFor(p).device())
		}
		btJSON(reply, out)
	})
}

// Connecting.

func (b *btModule) connect(args []byte, reply func([]byte, error)) {
	var a spec.ConnectArgs
	if !btArgs(args, &a, reply) {
		return
	}
	b.onCentral(reply, func() {
		p := b.lookup(a.Device)
		if p == nil {
			btFail(reply, "not found")
			return
		}
		if b.conns[a.Conn] != nil {
			btFail(reply, fmt.Sprintf("bad args: connection %d is in use", a.Conn))
			return
		}
		b.conns[a.Conn] = p
		if p.up && len(p.disconnecting) == 0 {
			p.conns = append(p.conns, a.Conn)
			btOK(reply)
			return
		}
		p.connecting[a.Conn] = reply
		// Disconnecting: connected again once it has.
		if !p.asked && len(p.disconnecting) == 0 {
			p.asked = true
			Send(b.central, "connectPeripheral:options:", uintptr(p.obj), 0)
		}
	})
}

func (b *btModule) disconnect(args []byte, reply func([]byte, error)) {
	var a spec.Conn
	if !btArgs(args, &a, reply) {
		return
	}
	p := b.conns[a.Conn]
	if p == nil {
		btOK(reply)
		return
	}
	delete(b.conns, a.Conn)
	if r, ok := p.connecting[a.Conn]; ok {
		delete(p.connecting, a.Conn)
		btFail(r, "canceled")
		if len(p.connecting) == 0 && p.asked {
			Send(b.central, "cancelPeripheralConnection:", uintptr(p.obj))
			p.asked = false
		}
		btOK(reply)
		return
	}
	p.conns = slices.DeleteFunc(p.conns, func(c uint32) bool { return c == a.Conn })
	p.dropConn(a.Conn)
	if len(p.conns) > 0 || !p.up {
		btOK(reply)
		return
	}
	// The last: the link ends, answered once it has (no event for it).
	p.disconnecting = append(p.disconnecting, reply)
	Send(b.central, "cancelPeripheralConnection:", uintptr(p.obj))
}

func (p *btPeer) didConnect() {
	p.up, p.asked = true, false
	if len(p.connecting) == 0 {
		// Canceled meanwhile.
		Send(bt.central, "cancelPeripheralConnection:", uintptr(p.obj))
		return
	}
	for _, c := range sortedKeys(p.connecting) {
		r := p.connecting[c]
		p.conns = append(p.conns, c)
		btOK(r)
	}
	clear(p.connecting)
}

func (p *btPeer) didFailToConnect(err ID) {
	p.asked = false
	e := btError(err, "couldn't connect")
	for _, c := range sortedKeys(p.connecting) {
		r := p.connecting[c]
		delete(bt.conns, c)
		r(nil, e)
	}
	clear(p.connecting)
}

func (p *btPeer) didDisconnect(err ID) {
	reason, code, msg := btReason(err)
	p.lost("", spec.Event{Type: spec.EventDisconnected, Reason: reason, Code: code, Error: msg})
	// Connects that came while it was disconnecting.
	if len(p.connecting) > 0 && bt.central != 0 {
		p.asked = true
		Send(bt.central, "connectPeripheral:options:", uintptr(p.obj), 0)
	}
}

// lost is the link gone: each connection's event (ev, its Conn set),
// the calls waiting failed, the attributes forgotten; connects waiting
// fail with failConnecting, if any.
func (p *btPeer) lost(failConnecting string, ev spec.Event) {
	p.up = false
	if failConnecting != "" {
		p.asked = false
		for _, c := range sortedKeys(p.connecting) {
			r := p.connecting[c]
			delete(bt.conns, c)
			btFail(r, failConnecting)
		}
		clear(p.connecting)
	}
	for _, c := range p.conns {
		delete(bt.conns, c)
		e := ev
		e.Conn = c
		bt.emit(e)
	}
	p.conns = nil
	gone := errors.New("disconnected")
	p.failWaits(func(*btWait) bool { return true }, gone)
	p.forget()
	ds := p.disconnecting
	p.disconnecting = nil
	for _, d := range ds {
		btOK(d)
	}
}

// failWaits fails the waiting calls that match (their answers dropped).
// After a disconnection no answer comes: the queues are emptied.
func (p *btPeer) failWaits(match func(*btWait) bool, err error) {
	fail := func(w *btWait) {
		if w.reply != nil && match(w) {
			r := w.reply
			w.reply = nil
			r(nil, err)
		}
	}
	for _, ws := range p.waits {
		for _, w := range ws {
			fail(w)
		}
	}
	for _, w := range p.rssiWaits {
		fail(w)
	}
	for _, w := range p.discovering {
		fail(w)
	}
	for _, w := range p.writes {
		fail(w.wait)
	}
}

// forget drops the attributes and what waits (the link is gone).
func (p *btPeer) forget() {
	for _, a := range p.attrs {
		Release(a.obj)
	}
	clear(p.attrs)
	clear(p.handles)
	clear(p.waits)
	clear(p.subs)
	p.rssiWaits, p.discovering = nil, nil
	for _, w := range p.writes {
		Release(w.data)
	}
	p.writes = nil
	p.tree, p.treeOK, p.pending, p.discErr, p.again = nil, false, 0, nil, false
}

// dropConn is a connection Go closed while others use the link: its
// calls fail, its subscriptions end.
func (p *btPeer) dropConn(conn uint32) {
	p.failWaits(func(w *btWait) bool { return w.conn == conn }, errors.New("disconnected"))
	for c, m := range p.subs {
		if !m[conn] {
			continue
		}
		delete(m, conn)
		if len(m) == 0 && len(p.conns) > 0 && p.up {
			p.waits[btOp{c, opNotify}] = append(p.waits[btOp{c, opNotify}], &btWait{issued: true})
			Send(p.obj, "setNotifyValue:forCharacteristic:", 0, uintptr(c))
		}
	}
}

// connOf is a connection that's up, or the call fails "disconnected".
func (b *btModule) connOf(conn uint32, reply func([]byte, error)) (*btPeer, bool) {
	p := b.conns[conn]
	if p == nil || !slices.Contains(p.conns, conn) {
		btFail(reply, "disconnected")
		return nil, false
	}
	return p, true
}

// attrOf is a connection's attribute of a kind, or the call fails.
func (b *btModule) attrOf(conn, handle uint32, kind int, reply func([]byte, error)) (*btPeer, btAttr, bool) {
	p, ok := b.connOf(conn, reply)
	if !ok {
		return nil, btAttr{}, false
	}
	a, ok := p.attrs[handle]
	if !ok || a.kind != kind {
		btFail(reply, "not found")
		return nil, btAttr{}, false
	}
	return p, a, true
}

func (p *btPeer) wait(op btOp, w *btWait) { p.waits[op] = append(p.waits[op], w) }

// answer is the peripheral's answer to an attribute's oldest call (a
// notify op's: and the followers after it); false if none waited.
func (p *btPeer) answer(op btOp, value []byte, err ID) bool {
	ws := p.waits[op]
	if len(ws) == 0 {
		return false
	}
	n := 1
	if op.op == opNotify {
		for n < len(ws) && !ws[n].issued {
			n++
		}
	}
	done := ws[:n]
	if n == len(ws) {
		delete(p.waits, op)
	} else {
		p.waits[op] = ws[n:]
	}
	var e error
	if err != 0 {
		e = btError(err, "failed")
	}
	for _, w := range done {
		if w.reply == nil {
			continue
		}
		switch {
		case e != nil:
			w.reply(nil, e)
		case op.op == opRead || op.op == opReadDesc:
			if value == nil {
				value = []byte{}
			}
			w.reply(value, nil)
		default:
			btOK(w.reply)
		}
	}
	return true
}

// Services.

func (b *btModule) services(args []byte, reply func([]byte, error)) {
	var a spec.Conn
	if !btArgs(args, &a, reply) {
		return
	}
	p, ok := b.connOf(a.Conn, reply)
	if !ok {
		return
	}
	if p.treeOK {
		btJSON(reply, p.tree)
		return
	}
	p.discovering = append(p.discovering, &btWait{conn: a.Conn, reply: reply})
	if p.pending == 0 {
		p.discover()
	}
}

func (p *btPeer) discover() {
	p.pending, p.discErr, p.again = 1, nil, false
	Send(p.obj, "discoverServices:", 0)
}

func (p *btPeer) didDiscoverServices(err ID) {
	if p.pending <= 0 {
		return
	}
	p.pending--
	if err != 0 {
		p.discErr = btError(err, "couldn't discover the services")
	} else {
		for _, s := range ArrayItems(Send(p.obj, "services")) {
			p.pending++
			Send(p.obj, "discoverCharacteristics:forService:", 0, uintptr(s))
		}
	}
	p.discovered()
}

func (p *btPeer) didDiscoverCharacteristics(s, err ID) {
	if p.pending <= 0 {
		return
	}
	p.pending--
	if err != 0 {
		if p.discErr == nil {
			p.discErr = btError(err, "couldn't discover the characteristics")
		}
	} else {
		for _, c := range ArrayItems(Send(s, "characteristics")) {
			p.pending++
			Send(p.obj, "discoverDescriptorsForCharacteristic:", uintptr(c))
		}
	}
	p.discovered()
}

func (p *btPeer) didDiscoverDescriptors(err ID) {
	if p.pending <= 0 {
		return
	}
	p.pending--
	if err != 0 && p.discErr == nil {
		p.discErr = btError(err, "couldn't discover the descriptors")
	}
	p.discovered()
}

// discovered answers those waiting once every answer came.
func (p *btPeer) discovered() {
	if p.pending > 0 {
		return
	}
	if p.again {
		p.discover()
		return
	}
	ws := p.discovering
	p.discovering = nil
	if p.discErr == nil {
		p.tree, p.treeOK = p.buildTree(Send(p.obj, "services")), true
	}
	for _, w := range ws {
		if w.reply == nil {
			continue
		}
		if p.discErr != nil {
			w.reply(nil, p.discErr)
		} else {
			btJSON(w.reply, p.tree)
		}
	}
}

// handle is an attribute's handle, given the first time it's seen (the
// attribute kept).
func (p *btPeer) handle(obj ID, kind int, svc, char uint32) uint32 {
	if h, ok := p.handles[obj]; ok {
		return h
	}
	p.nextHandle++
	h := p.nextHandle
	if kind == attrService {
		svc = h
	}
	p.attrs[h] = btAttr{obj: Retain(obj), kind: kind, svc: svc, char: char}
	p.handles[obj] = h
	return h
}

// cbProperties is a CBCharacteristicProperties as the core spec's bits
// (its notify and indicate that need encryption as theirs).
func cbProperties(props int) int {
	out := props & 0xff
	if props&0x100 != 0 {
		out |= spec.PropNotify
	}
	if props&0x200 != 0 {
		out |= spec.PropIndicate
	}
	return out
}

// buildTree is the services (an NSArray of CBService), with handles.
func (p *btPeer) buildTree(services ID) []spec.Service {
	out := []spec.Service{}
	for _, s := range ArrayItems(services) {
		hs := p.handle(s, attrService, 0, 0)
		ws := spec.Service{Handle: hs, UUID: attrUUID(s), Primary: SendBool(s, "isPrimary")}
		for _, c := range ArrayItems(Send(s, "characteristics")) {
			hc := p.handle(c, attrChar, hs, 0)
			wc := spec.Characteristic{Handle: hc, UUID: attrUUID(c), Properties: cbProperties(SendInt(c, "properties"))}
			for _, d := range ArrayItems(Send(c, "descriptors")) {
				wc.Descriptors = append(wc.Descriptors, spec.Descriptor{Handle: p.handle(d, attrDesc, hs, hc), UUID: attrUUID(d)})
			}
			ws.Characteristics = append(ws.Characteristics, wc)
		}
		out = append(out, ws)
	}
	return out
}

// servicesModified is peripheral:didModifyServices:: the invalidated
// services' attributes go (their handles fail "not found"), the tree is
// discovered again when asked, and Go hears of it.
func (p *btPeer) servicesModified(invalidated ID) {
	gone := map[uint32]bool{}
	for _, s := range ArrayItems(invalidated) {
		if h, ok := p.handles[s]; ok {
			gone[h] = true
		}
	}
	notFound := errors.New("not found")
	for h, a := range p.attrs {
		if !gone[a.svc] {
			continue
		}
		for _, op := range []int{opRead, opWrite, opNotify, opReadDesc, opWriteDesc} {
			for _, w := range p.waits[btOp{a.obj, op}] {
				if w.reply != nil {
					r := w.reply
					w.reply = nil
					r(nil, notFound)
				}
			}
			delete(p.waits, btOp{a.obj, op})
		}
		p.writes = slices.DeleteFunc(p.writes, func(w btQueuedWrite) bool {
			if w.char != a.obj {
				return false
			}
			if w.wait.reply != nil {
				w.wait.reply(nil, notFound)
			}
			Release(w.data)
			return true
		})
		delete(p.subs, a.obj)
		delete(p.handles, a.obj)
		delete(p.attrs, h)
		Release(a.obj)
	}
	p.tree, p.treeOK = nil, false
	if p.pending > 0 {
		p.again = true
	}
	for _, c := range p.conns {
		bt.emit(spec.Event{Type: spec.EventServicesChanged, Conn: c})
	}
}

// Reads, writes, subscriptions.

func (b *btModule) read(args []byte, reply func([]byte, error)) {
	var a spec.Attr
	if !btArgs(args, &a, reply) {
		return
	}
	p, at, ok := b.attrOf(a.Conn, a.Handle, attrChar, reply)
	if !ok {
		return
	}
	p.wait(btOp{at.obj, opRead}, &btWait{conn: a.Conn, reply: reply})
	Send(p.obj, "readValueForCharacteristic:", uintptr(at.obj))
}

// valueUpdated is peripheral:didUpdateValueForCharacteristic:error:: a
// read's answer, or a notification (Core Bluetooth tells them apart
// only by whether a read waits; while notifying, both).
func (p *btPeer) valueUpdated(c, err ID) {
	var v []byte
	if err == 0 {
		v = Bytes(Send(c, "value"))
		if v == nil {
			v = []byte{}
		}
	}
	read := p.answer(btOp{c, opRead}, v, err)
	if err != 0 || (read && !SendBool(c, "isNotifying")) {
		return
	}
	h, ok := p.handles[c]
	if !ok {
		return
	}
	for _, conn := range p.conns {
		if p.subs[c][conn] {
			bt.emit(spec.Event{Type: spec.EventValue, Conn: conn, Handle: h, Value: v})
		}
	}
}

func (b *btModule) write(args []byte, reply func([]byte, error)) {
	var a spec.Write
	if !btArgs(args, &a, reply) {
		return
	}
	p, at, ok := b.attrOf(a.Conn, a.Handle, attrChar, reply)
	if !ok {
		return
	}
	if a.Response {
		p.wait(btOp{at.obj, opWrite}, &btWait{conn: a.Conn, reply: reply})
		Send(p.obj, "writeValue:forCharacteristic:type:", uintptr(Data(a.Value)), uintptr(at.obj), 0)
		return
	}
	if cbProperties(SendInt(at.obj, "properties"))&spec.PropWriteNoResponse == 0 {
		btFail(reply, "att 3: the characteristic takes no write without response")
		return
	}
	if most := SendInt(p.obj, "maximumWriteValueLengthForType:", 1); len(a.Value) > most {
		btFail(reply, fmt.Sprintf("att 13: %d bytes is longer than the %d a write without response takes", len(a.Value), most))
		return
	}
	p.writes = append(p.writes, btQueuedWrite{char: at.obj, data: Retain(Data(a.Value)), wait: &btWait{conn: a.Conn, reply: reply}})
	p.sendWrites()
}

// sendWrites sends the writes without response while Core Bluetooth has
// room for them (peripheralIsReadyToSendWriteWithoutResponse: says when
// it has again).
func (p *btPeer) sendWrites() {
	canAsk := RespondsTo(p.obj, "canSendWriteWithoutResponse")
	for len(p.writes) > 0 && (!canAsk || SendBool(p.obj, "canSendWriteWithoutResponse")) {
		w := p.writes[0]
		p.writes = p.writes[1:]
		if w.wait.reply != nil {
			Send(p.obj, "writeValue:forCharacteristic:type:", uintptr(w.data), uintptr(w.char), 1)
			btOK(w.wait.reply)
		}
		Release(w.data)
	}
}

func (b *btModule) readDescriptor(args []byte, reply func([]byte, error)) {
	var a spec.Attr
	if !btArgs(args, &a, reply) {
		return
	}
	p, at, ok := b.attrOf(a.Conn, a.Handle, attrDesc, reply)
	if !ok {
		return
	}
	p.wait(btOp{at.obj, opReadDesc}, &btWait{conn: a.Conn, reply: reply})
	Send(p.obj, "readValueForDescriptor:", uintptr(at.obj))
}

// The Client Characteristic Configuration descriptor's UUID.
var cccdUUID = spec.Normalize("2902")

func (b *btModule) writeDescriptor(args []byte, reply func([]byte, error)) {
	var a spec.Write
	if !btArgs(args, &a, reply) {
		return
	}
	p, at, ok := b.attrOf(a.Conn, a.Handle, attrDesc, reply)
	if !ok {
		return
	}
	if attrUUID(at.obj) == cccdUUID {
		// Core Bluetooth writes the CCCD itself (and raises an exception
		// for a write of it): notifications on or off.
		c := p.attrs[at.char]
		p.setNotify(c.obj, a.Conn, len(a.Value) > 0 && a.Value[0]&3 != 0, reply)
		return
	}
	p.wait(btOp{at.obj, opWriteDesc}, &btWait{conn: a.Conn, reply: reply})
	Send(p.obj, "writeValue:forDescriptor:", uintptr(Data(a.Value)), uintptr(at.obj))
}

// descriptorBytes is a descriptor's value as bytes: Core Bluetooth gives
// an NSString (the user description), an NSNumber (the extended
// properties, the CCCD, the SCCD: 16 bits, little endian) or NSData.
func descriptorBytes(d ID) []byte {
	v := Send(d, "value")
	switch {
	case v == 0:
		return []byte{}
	case SendBool(v, "isKindOfClass:", uintptr(Class("NSData"))):
		return Bytes(v)
	case SendBool(v, "isKindOfClass:", uintptr(Class("NSString"))):
		return []byte(GoString(v))
	case SendBool(v, "isKindOfClass:", uintptr(Class("NSNumber"))):
		n := SendInt(v, "unsignedShortValue")
		return []byte{byte(n), byte(n >> 8)}
	}
	return []byte{}
}

func (b *btModule) subscribe(args []byte, on bool, reply func([]byte, error)) {
	var a spec.Subscribe
	if !btArgs(args, &a, reply) {
		return
	}
	p, at, ok := b.attrOf(a.Conn, a.Handle, attrChar, reply)
	if !ok {
		return
	}
	// Core Bluetooth picks notifications or indications itself (Indicate
	// can't choose).
	if on && cbProperties(SendInt(at.obj, "properties"))&(spec.PropNotify|spec.PropIndicate) == 0 {
		btFail(reply, "att 6: the characteristic doesn't notify or indicate")
		return
	}
	p.setNotify(at.obj, a.Conn, on, reply)
}

// setNotify subscribes a connection to a characteristic's notifications
// or ends its subscription: Core Bluetooth's turned on with the first,
// off after the last.
func (p *btPeer) setNotify(c ID, conn uint32, on bool, reply func([]byte, error)) {
	op := btOp{c, opNotify}
	pending := len(p.waits[op]) > 0
	m := p.subs[c]
	if on {
		if m == nil {
			m = map[uint32]bool{}
			p.subs[c] = m
		}
		first := len(m) == 0
		m[conn] = true
		failed := func(b []byte, err error) {
			if err != nil {
				delete(p.subs[c], conn)
			}
			reply(b, err)
		}
		switch {
		case !first && pending:
			p.wait(op, &btWait{conn: conn, reply: failed})
		case !first || (!pending && SendBool(c, "isNotifying")):
			btOK(reply)
		default:
			p.wait(op, &btWait{conn: conn, reply: failed, issued: true})
			Send(p.obj, "setNotifyValue:forCharacteristic:", 1, uintptr(c))
		}
		return
	}
	delete(m, conn)
	if len(m) > 0 || (!pending && !SendBool(c, "isNotifying")) {
		btOK(reply)
		return
	}
	p.wait(op, &btWait{conn: conn, reply: reply, issued: true})
	Send(p.obj, "setNotifyValue:forCharacteristic:", 0, uintptr(c))
}

// The link's.

func (b *btModule) mtu(args []byte, reply func([]byte, error)) {
	var a spec.Conn
	if !btArgs(args, &a, reply) {
		return
	}
	p, ok := b.connOf(a.Conn, reply)
	if !ok {
		return
	}
	// macOS negotiates the MTU itself: "requestMtu" answers what it is.
	without := SendInt(p.obj, "maximumWriteValueLengthForType:", 1)
	with := SendInt(p.obj, "maximumWriteValueLengthForType:", 0)
	btJSON(reply, spec.MTU{MTU: without + 3, WriteWithResponse: with, WriteWithoutResponse: without})
}

func (b *btModule) rssi(args []byte, reply func([]byte, error)) {
	var a spec.Conn
	if !btArgs(args, &a, reply) {
		return
	}
	p, ok := b.connOf(a.Conn, reply)
	if !ok {
		return
	}
	p.rssiWaits = append(p.rssiWaits, &btWait{conn: a.Conn, reply: reply})
	Send(p.obj, "readRSSI")
}

func (p *btPeer) rssiRead(rssi, err ID) {
	if len(p.rssiWaits) == 0 {
		return
	}
	w := p.rssiWaits[0]
	p.rssiWaits = p.rssiWaits[1:]
	if w.reply == nil {
		return
	}
	if err != 0 {
		w.reply(nil, btError(err, "couldn't read the RSSI"))
		return
	}
	btJSON(w.reply, spec.RSSI{RSSI: SendInt(rssi, "integerValue")})
}
