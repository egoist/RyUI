//go:build darwin && !ios

package darwin

import (
	"fmt"
	"slices"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	. "github.com/egoist/asuka/internal/darwin"
)

// The peripheral role, with a CBPeripheralManager: advertising (Core
// Bluetooth advertises one thing for the app: the running
// advertisements merged, their services' union and the newest name; it
// takes only the name and the services) and the app's GATT servers,
// whose centrals' requests Go answers ("respond").

type btAdv struct {
	id       uint32
	name     string
	services []string
	reply    func([]byte, error) // until it's advertised
}

type btServer struct {
	id       uint32
	services []ID // added (retained)
	chars    map[uint32]*btLocalChar
	closed   bool
}

type btLocalChar struct {
	server *btServer
	handle uint32
	obj    ID // CBMutableCharacteristic (retained)
	props  int
	// value is a fixed value the host answers reads with (one Core
	// Bluetooth can't cache: the characteristic isn't read-only).
	value       []byte
	subscribers map[string]ID // CBCentral by identifier (retained)
}

// btServeJob adds a server's services, one at a time (Core Bluetooth
// answers each with didAddService); one job at a time.
type btServeJob struct {
	server   *btServer
	services []ID // retained; those before next are the server's now
	next     int
	reply    func([]byte, error)
}

// btRequest is a central's request Go answers: a read, or one write of
// a batch (didReceiveWriteRequests:), which Core Bluetooth takes one
// answer for, to its first request, once Go answered every write.
type btRequest struct {
	server *btServer
	read   ID // CBATTRequest (retained)
	batch  *btBatch
}

type btBatch struct {
	first ID // the batch's first CBATTRequest (retained)
	left  int
	code  int // the first error Go answered
}

type btNotification struct {
	char     *btLocalChar
	data     ID // retained
	centrals ID // NSArray of CBCentral (retained), 0: every subscriber
	reply    func([]byte, error)
}

// ATT error codes the host answers with itself.
const (
	attInvalidOffset     = 0x07
	attAttributeNotFound = 0x0a
	attUnlikelyError     = 0x0e
)

// withPeripheral makes the peripheral manager if it isn't, then calls f
// once it's on, or fails the call with its state.
func (b *btModule) withPeripheral(reply func([]byte, error), f func()) {
	if !btLoad() {
		btFail(reply, spec.StateUnsupported)
		return
	}
	g := func() {
		if w := btStateWord(b.pmState); w != spec.StateOn {
			btFail(reply, w)
			return
		}
		f()
	}
	if b.pm == 0 {
		b.pmWaiting = append(b.pmWaiting, g)
		b.pm = Send(Send(Class("CBPeripheralManager"), "alloc"), "initWithDelegate:queue:options:",
			uintptr(b.delegate), mainQueue(), 0)
		return
	}
	if b.pmState < 0 {
		b.pmWaiting = append(b.pmWaiting, g)
		return
	}
	g()
}

// pmUpdated is peripheralManagerDidUpdateState:. Below on, Core
// Bluetooth stopped advertising and cleared the services.
func (b *btModule) pmUpdated(m ID) {
	if m != b.pm {
		return
	}
	b.pmState = SendInt(m, "state")
	if word := btStateWord(b.pmState); word != spec.StateOn {
		for _, a := range b.advs {
			if a.reply != nil {
				btFail(a.reply, word)
			} else {
				b.emit(spec.Event{Type: spec.EventAdvertiseStopped, ID: a.id, Error: word})
			}
		}
		b.advs, b.advInFlight, b.advStarting, b.advDirty = nil, nil, false, false
		for _, id := range sortedKeys(b.servers) {
			s := b.servers[id]
			b.dropServer(s, false)
			b.emit(spec.Event{Type: spec.EventServerStopped, Server: id, Error: word})
		}
		jobs := b.serveQueue
		b.serveQueue = nil
		for _, j := range jobs {
			for _, s := range j.services[j.next:] {
				Release(s)
			}
			btFail(j.reply, word)
		}
		for _, n := range b.notifyQ {
			n.release()
			btFail(n.reply, word)
		}
		b.notifyQ = nil
	}
	ws := b.pmWaiting
	b.pmWaiting = nil
	for _, w := range ws {
		w()
	}
}

// Advertising.

func (b *btModule) advertise(args []byte, reply func([]byte, error)) {
	var a spec.AdvertiseArgs
	if !btArgs(args, &a, reply) {
		return
	}
	services, ok := btUUIDs(a.Services, reply)
	if !ok {
		return
	}
	b.withPeripheral(reply, func() {
		b.advs = append(b.advs, &btAdv{id: a.ID, name: a.LocalName, services: services, reply: reply})
		b.updateAdvertising()
	})
}

func (b *btModule) stopAdvertise(args []byte, reply func([]byte, error)) {
	var a spec.ID
	if !btArgs(args, &a, reply) {
		return
	}
	i := slices.IndexFunc(b.advs, func(x *btAdv) bool { return x.id == a.ID })
	if i >= 0 {
		ad := b.advs[i]
		b.advs = slices.Delete(b.advs, i, i+1)
		if ad.reply != nil {
			btFail(ad.reply, "canceled")
		}
		b.updateAdvertising()
	}
	btOK(reply)
}

// mergeAdvertising is what Core Bluetooth advertises for them: the
// newest name, every service once.
func mergeAdvertising(advs []*btAdv) (name string, services []string) {
	for _, a := range advs {
		if a.name != "" {
			name = a.name
		}
		for _, s := range a.services {
			if !slices.Contains(services, s) {
				services = append(services, s)
			}
		}
	}
	return name, services
}

// updateAdvertising advertises what the advertisements want now (once
// the one starting has).
func (b *btModule) updateAdvertising() {
	if b.pm == 0 {
		return
	}
	if b.advStarting {
		b.advDirty = true
		return
	}
	if SendBool(b.pm, "isAdvertising") {
		Send(b.pm, "stopAdvertising")
	}
	if len(b.advs) == 0 {
		return
	}
	name, services := mergeAdvertising(b.advs)
	var nameObj ID
	if name != "" {
		nameObj = String(name)
	}
	d := Dict(btConst("CBAdvertisementDataLocalNameKey"), nameObj,
		btConst("CBAdvertisementDataServiceUUIDsKey"), cbUUIDs(services))
	b.advInFlight, b.advStarting = slices.Clone(b.advs), true
	Send(b.pm, "startAdvertising:", uintptr(d))
}

// advertisingStarted is peripheralManagerDidStartAdvertising:error:.
// Failing, the new advertisements fail (and the others start again
// without them); the others' failing again stops them.
func (b *btModule) advertisingStarted(err ID) {
	b.advStarting = false
	flight := b.advInFlight
	b.advInFlight = nil
	running := func(a *btAdv) bool { return slices.Contains(b.advs, a) }
	if err != 0 {
		e := btError(err, "couldn't advertise")
		var fresh []*btAdv
		for _, a := range flight {
			if a.reply != nil && running(a) {
				fresh = append(fresh, a)
			}
		}
		if len(fresh) == 0 {
			fresh = slices.DeleteFunc(flight, func(a *btAdv) bool { return !running(a) })
		}
		b.advs = slices.DeleteFunc(b.advs, func(a *btAdv) bool { return slices.Contains(fresh, a) })
		for _, a := range fresh {
			if r := a.reply; r != nil {
				a.reply = nil
				r(nil, e)
			} else {
				b.emit(spec.Event{Type: spec.EventAdvertiseStopped, ID: a.id, Error: e.Error()})
			}
		}
		b.advDirty = false
		b.updateAdvertising()
		return
	}
	for _, a := range flight {
		if r := a.reply; r != nil && running(a) {
			a.reply = nil
			btOK(r)
		}
	}
	if b.advDirty {
		b.advDirty = false
		b.updateAdvertising()
	}
}

// Serving.

// localProperties are a local characteristic's Core Bluetooth takes:
// the extended properties are its own (Broadcast it can't).
func localProperties(props int) int { return props &^ spec.PropExtendedProperties }

// fixedValue reports whether Core Bluetooth can keep a characteristic's
// fixed value itself: it must be read-only.
func fixedValue(c spec.LocalCharacteristic) bool {
	return c.Value != nil && localProperties(c.Properties)&^spec.PropRead == 0 &&
		c.Permissions&^(spec.PermRead|spec.PermReadEncrypted) == 0
}

// checkServe is what Core Bluetooth can't serve (it raises exceptions
// for them): broadcast characteristics, descriptors other than the user
// description and the presentation format. The UUIDs are normalized.
func checkServe(a *spec.ServeArgs) string {
	norm := func(s *string) bool {
		u, ok := spec.ParseUUID(*s)
		if ok {
			*s = spec.FormatUUID(u)
		}
		return ok
	}
	for i := range a.Services {
		s := &a.Services[i]
		if !norm(&s.UUID) {
			return "bad args: not a UUID: " + s.UUID
		}
		for j := range s.Characteristics {
			c := &s.Characteristics[j]
			if !norm(&c.UUID) {
				return "bad args: not a UUID: " + c.UUID
			}
			if c.Properties&spec.PropBroadcast != 0 {
				return "not available: macOS doesn't broadcast a characteristic's value (" + c.UUID + ")"
			}
			for k := range c.Descriptors {
				d := &c.Descriptors[k]
				if !norm(&d.UUID) {
					return "bad args: not a UUID: " + d.UUID
				}
				switch spec.Short(d.UUID) {
				case "2901", "2904", "2902": // the CCCD is Core Bluetooth's: left out
				default:
					return "not available: macOS serves only the user description (2901) and presentation format (2904) descriptors, not " + d.UUID
				}
			}
		}
	}
	return ""
}

func (b *btModule) serve(args []byte, reply func([]byte, error)) {
	var a spec.ServeArgs
	if !btArgs(args, &a, reply) {
		return
	}
	if msg := checkServe(&a); msg != "" {
		btFail(reply, msg)
		return
	}
	b.withPeripheral(reply, func() {
		if b.servers[a.ID] != nil {
			btFail(reply, fmt.Sprintf("bad args: server %d is serving", a.ID))
			return
		}
		s := &btServer{id: a.ID, chars: map[uint32]*btLocalChar{}}
		if b.servers == nil {
			b.servers, b.localChars, b.requests = map[uint32]*btServer{}, map[ID]*btLocalChar{}, map[uint32]*btRequest{}
		}
		b.servers[a.ID] = s
		job := &btServeJob{server: s, reply: reply}
		for _, ls := range a.Services {
			job.services = append(job.services, b.localService(s, ls))
		}
		b.serveQueue = append(b.serveQueue, job)
		if len(b.serveQueue) == 1 {
			b.addNext()
		}
	})
}

// localService makes a CBMutableService of the app's (retained), its
// characteristics the server's.
func (b *btModule) localService(s *btServer, ls spec.LocalService) ID {
	svc := Send(Send(Class("CBMutableService"), "alloc"), "initWithType:primary:", uintptr(cbUUID(ls.UUID)), Bool(ls.Primary))
	var chars []ID
	for _, c := range ls.Characteristics {
		props, perms := localProperties(c.Properties), c.Permissions&0x0f
		var value ID
		fixed := fixedValue(c)
		if fixed {
			value = Data(c.Value)
			if perms == 0 {
				perms = spec.PermRead
			}
		}
		ch := Send(Send(Class("CBMutableCharacteristic"), "alloc"), "initWithType:properties:value:permissions:",
			uintptr(cbUUID(c.UUID)), uintptr(props), uintptr(value), uintptr(perms))
		var descs []ID
		for _, d := range c.Descriptors {
			var v ID
			switch spec.Short(d.UUID) {
			case "2901":
				v = String(string(d.Value))
			case "2904":
				v = Data(d.Value)
			default:
				continue
			}
			descs = append(descs, Autorelease(Send(Send(Class("CBMutableDescriptor"), "alloc"), "initWithType:value:",
				uintptr(cbUUID(d.UUID)), uintptr(v))))
		}
		if len(descs) > 0 {
			Send(ch, "setDescriptors:", uintptr(Array(descs...)))
		}
		lc := &btLocalChar{server: s, handle: c.Handle, obj: ch, props: props, subscribers: map[string]ID{}}
		if !fixed && c.Value != nil {
			lc.value = append([]byte(nil), c.Value...)
		}
		s.chars[c.Handle] = lc
		b.localChars[ch] = lc
		chars = append(chars, ch)
	}
	if len(chars) > 0 {
		Send(svc, "setCharacteristics:", uintptr(Array(chars...)))
	}
	return svc
}

// addNext adds the next service of the first job (answering the jobs
// done).
func (b *btModule) addNext() {
	for len(b.serveQueue) > 0 {
		j := b.serveQueue[0]
		switch {
		case j.server.closed:
			for _, s := range j.services[j.next:] {
				Release(s)
			}
			btFail(j.reply, "canceled")
		case j.next == len(j.services):
			btOK(j.reply)
		default:
			Send(b.pm, "addService:", uintptr(j.services[j.next]))
			return
		}
		b.serveQueue = b.serveQueue[1:]
	}
}

// serviceAdded is peripheralManager:didAddService:error:.
func (b *btModule) serviceAdded(svc, err ID) {
	if len(b.serveQueue) == 0 {
		return
	}
	j := b.serveQueue[0]
	if j.next >= len(j.services) || j.services[j.next] != svc {
		return
	}
	j.next++
	switch {
	case err != 0:
		Release(svc)
		e := btError(err, "couldn't add the service")
		if !j.server.closed {
			b.dropServer(j.server, true)
		}
		for _, s := range j.services[j.next:] {
			Release(s)
		}
		j.next = len(j.services)
		b.serveQueue = b.serveQueue[1:]
		j.reply(nil, e)
	case j.server.closed:
		Send(b.pm, "removeService:", uintptr(svc))
		Release(svc)
	default:
		j.server.services = append(j.server.services, svc)
	}
	b.addNext()
}

func (b *btModule) unserve(args []byte, reply func([]byte, error)) {
	var a spec.ID
	if !btArgs(args, &a, reply) {
		return
	}
	if s := b.servers[a.ID]; s != nil {
		b.dropServer(s, true)
	}
	btOK(reply)
}

// dropServer closes a server: its services removed (from Core Bluetooth
// too, if remove), its requests refused, its notifications dropped.
func (b *btModule) dropServer(s *btServer, remove bool) {
	s.closed = true
	delete(b.servers, s.id)
	for _, svc := range s.services {
		if remove && b.pm != 0 {
			Send(b.pm, "removeService:", uintptr(svc))
		}
		Release(svc)
	}
	s.services = nil
	for _, id := range sortedKeys(b.requests) {
		if r := b.requests[id]; r.server == s {
			delete(b.requests, id)
			b.finish(r, nil, attUnlikelyError)
		}
	}
	b.notifyQ = slices.DeleteFunc(b.notifyQ, func(n *btNotification) bool {
		if n.char.server != s {
			return false
		}
		n.release()
		btFail(n.reply, "not found")
		return true
	})
	for _, c := range s.chars {
		delete(b.localChars, c.obj)
		for _, central := range c.subscribers {
			Release(central)
		}
		Release(c.obj)
	}
	clear(s.chars)
}

// Requests.

// readRequest is peripheralManager:didReceiveReadRequest:.
func (b *btModule) readRequest(r ID) {
	c := b.localChars[Send(r, "characteristic")]
	if c == nil {
		Send(b.pm, "respondToRequest:withResult:", uintptr(r), attAttributeNotFound)
		return
	}
	offset := SendInt(r, "offset")
	if c.value != nil {
		if offset > len(c.value) {
			Send(b.pm, "respondToRequest:withResult:", uintptr(r), attInvalidOffset)
			return
		}
		Send(r, "setValue:", uintptr(Data(c.value[offset:])))
		Send(b.pm, "respondToRequest:withResult:", uintptr(r), 0)
		return
	}
	b.nextRequest++
	id := b.nextRequest
	b.requests[id] = &btRequest{server: c.server, read: Retain(r)}
	b.emit(spec.Event{Type: spec.EventRead, Server: c.server.id, Request: id,
		Central: &spec.Device{ID: peerID(Send(r, "central"))}, Handle: c.handle, Offset: offset})
}

// btPiece is a write of a batch.
type btPiece struct {
	char    ID
	central string
	offset  int
	value   []byte
}

// mergeWrites puts a long write's parts together: a write that goes on
// where the last of the same characteristic and central ended is that
// one's.
func mergeWrites(ps []btPiece) []btPiece {
	var out []btPiece
	for _, p := range ps {
		merged := false
		for i := len(out) - 1; i >= 0; i-- {
			o := &out[i]
			if o.char == p.char && o.central == p.central {
				if p.offset == o.offset+len(o.value) {
					o.value = append(o.value, p.value...)
					merged = true
				}
				break
			}
		}
		if !merged {
			p.value = append([]byte{}, p.value...)
			out = append(out, p)
		}
	}
	return out
}

// writeResponse reports whether a central's write of a characteristic
// waits for an answer: Core Bluetooth doesn't say, so one that takes
// only writes without response doesn't.
func writeResponse(props int) bool {
	return props&spec.PropWrite != 0 || props&spec.PropWriteNoResponse == 0
}

// writeRequests is peripheralManager:didReceiveWriteRequests:.
func (b *btModule) writeRequests(rs ID) {
	items := ArrayItems(rs)
	if len(items) == 0 {
		return
	}
	ps := make([]btPiece, len(items))
	for i, r := range items {
		ps[i] = btPiece{char: Send(r, "characteristic"), central: peerID(Send(r, "central")),
			offset: SendInt(r, "offset"), value: Bytes(Send(r, "value"))}
	}
	batch := &btBatch{first: Retain(items[0])}
	var evs []spec.Event
	for _, w := range mergeWrites(ps) {
		c := b.localChars[w.char]
		if c == nil {
			if batch.code == 0 {
				batch.code = attAttributeNotFound
			}
			continue
		}
		e := spec.Event{Type: spec.EventWrite, Server: c.server.id, Central: &spec.Device{ID: w.central},
			Handle: c.handle, Offset: w.offset, Value: w.value, Response: writeResponse(c.props)}
		if e.Response {
			b.nextRequest++
			e.Request = b.nextRequest
			b.requests[e.Request] = &btRequest{server: c.server, batch: batch}
			batch.left++
		}
		evs = append(evs, e)
	}
	for _, e := range evs {
		b.emit(e)
	}
	if batch.left == 0 {
		b.answerBatch(batch)
	}
}

func (b *btModule) answerBatch(batch *btBatch) {
	if b.pm != 0 {
		Send(b.pm, "respondToRequest:withResult:", uintptr(batch.first), uintptr(batch.code))
	}
	Release(batch.first)
}

func (b *btModule) respond(args []byte, reply func([]byte, error)) {
	var a spec.Respond
	if !btArgs(args, &a, reply) {
		return
	}
	if r := b.requests[a.Request]; r != nil {
		delete(b.requests, a.Request)
		b.finish(r, a.Value, a.Error)
	}
	btOK(reply)
}

// finish answers a request: a read's at once, a write's batch once every
// write of it is answered (with the first error).
func (b *btModule) finish(r *btRequest, value []byte, code int) {
	if r.read != 0 {
		if code == 0 {
			Send(r.read, "setValue:", uintptr(Data(value)))
		}
		if b.pm != 0 {
			Send(b.pm, "respondToRequest:withResult:", uintptr(r.read), uintptr(code))
		}
		Release(r.read)
		return
	}
	if code != 0 && r.batch.code == 0 {
		r.batch.code = code
	}
	r.batch.left--
	if r.batch.left == 0 {
		b.answerBatch(r.batch)
	}
}

// subscription is a central's subscribing to a characteristic, or
// unsubscribing.
func (b *btModule) subscription(central, ch ID, on bool) {
	c := b.localChars[ch]
	if c == nil {
		return
	}
	id := peerID(central)
	if old, ok := c.subscribers[id]; ok {
		Release(old)
		delete(c.subscribers, id)
	}
	if on {
		c.subscribers[id] = Retain(central)
	}
	b.emit(spec.Event{Type: spec.EventSubscription, Server: c.server.id, Central: &spec.Device{ID: id}, Handle: c.handle, On: on})
}

// Notifying.

func (n *btNotification) release() {
	Release(n.data)
	Release(n.centrals)
}

func (b *btModule) notify(args []byte, reply func([]byte, error)) {
	var a spec.Notify
	if !btArgs(args, &a, reply) {
		return
	}
	s := b.servers[a.Server]
	if s == nil || s.chars[a.Handle] == nil {
		btFail(reply, "not found")
		return
	}
	c := s.chars[a.Handle]
	if c.props&(spec.PropNotify|spec.PropIndicate) == 0 {
		btFail(reply, "att 6: the characteristic doesn't notify or indicate")
		return
	}
	var to []ID
	for _, id := range a.Centrals {
		for k, central := range c.subscribers {
			if btIDKey(k) == btIDKey(id) {
				to = append(to, central)
			}
		}
	}
	if len(c.subscribers) == 0 || (len(a.Centrals) > 0 && len(to) == 0) {
		btOK(reply) // no one to send it to
		return
	}
	n := &btNotification{char: c, data: Retain(Data(a.Value)), reply: reply}
	if len(to) > 0 {
		n.centrals = Retain(Array(to...))
	}
	b.notifyQ = append(b.notifyQ, n)
	b.sendNotifications()
}

// sendNotifications sends the notifications queued, in order, while
// Core Bluetooth has room (peripheralManagerIsReadyToUpdateSubscribers:
// says when it has again).
func (b *btModule) sendNotifications() {
	for len(b.notifyQ) > 0 && b.pm != 0 {
		n := b.notifyQ[0]
		if !SendBool(b.pm, "updateValue:forCharacteristic:onSubscribedCentrals:", uintptr(n.data), uintptr(n.char.obj), uintptr(n.centrals)) {
			return
		}
		b.notifyQ = b.notifyQ[1:]
		n.release()
		btOK(n.reply)
	}
}
