//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ebitengine/purego/objc"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/sdk/bluetooth on a Mac, with Core Bluetooth as on iOS: the
// "bluetooth" module of internal/bluetoothspec. One CBCentralManager
// serves the central role (scans, connections and their GATT calls:
// bluetooth_conn.go), one CBPeripheralManager the peripheral role
// (advertising, the app's GATT server: bluetooth_server.go), one
// delegate object both, on the main queue. macOS asks the user once,
// with the app's NSBluetoothAlwaysUsageDescription, as the central
// manager is made: "state" doesn't make it until the app is allowed,
// "request" does.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["bluetooth"] = bt.module() })
}

// The module's state: the main thread's.
var bt = &btModule{centralState: -1, pmState: -1}

type btModule struct {
	delegate ID

	// The central manager, made once allowed (or on "request"); its
	// state (CBManagerState, -1 until its first update) and what waits
	// for that.
	central        ID
	centralState   int
	centralWaiting []func()
	word           string // the state last told ("state" answered, or an event)

	// The events Go follows ("next").
	queue     []spec.Event
	waiting   func([]byte, error)
	following bool

	// Go's scans, and the central's one scan serving them.
	scans        map[uint32]*btScan
	scanning     bool
	scanServices []string // nil: every device
	scanDup      bool

	// Peripherals, by identifier and by object, and Go's connections.
	peers     map[string]*btPeer
	peerByObj map[ID]*btPeer
	conns     map[uint32]*btPeer

	// The peripheral role (bluetooth_server.go).
	pm          ID
	pmState     int
	pmWaiting   []func()
	advs        []*btAdv
	advStarting bool
	advDirty    bool
	advInFlight []*btAdv
	servers     map[uint32]*btServer
	localChars  map[ID]*btLocalChar
	serveQueue  []*btServeJob
	requests    map[uint32]*btRequest
	nextRequest uint32
	notifyQ     []*btNotification
}

// Seams: what asks the system, which tests replace.
var (
	// btAuthorization is CBManager's authorization (0 not determined, 1
	// restricted, 2 denied, 3 allowed), which asks nothing.
	btAuthorization = func() int {
		if !btLoad() {
			return 3
		}
		c := Class("CBManager")
		if !RespondsTo(c, "authorization") {
			return 3 // before macOS 10.15: no prompt
		}
		return SendInt(c, "authorization")
	}
)

// CBManagerAuthorization.
const (
	cbNotDetermined = 0
	cbRestricted    = 1
	cbDenied        = 2
)

// CBManagerState.
const (
	cbStateUnknown      = 0
	cbStateResetting    = 1
	cbStateUnsupported  = 2
	cbStateUnauthorized = 3
	cbStatePoweredOff   = 4
	cbStatePoweredOn    = 5
)

// btStateWord is a CBManagerState as the wire has it.
func btStateWord(s int) string {
	switch s {
	case cbStateUnsupported:
		return spec.StateUnsupported
	case cbStateUnauthorized:
		return spec.StateDenied
	case cbStatePoweredOff:
		return spec.StateOff
	case cbStatePoweredOn:
		return spec.StateOn
	}
	return spec.StateUnknown
}

var btLoaded, btTried bool

// btLoad loads Core Bluetooth and defines the delegate, once.
func btLoad() bool {
	if btTried {
		return btLoaded
	}
	btTried = true
	if framework("CoreBluetooth") == 0 || !HasClass("CBCentralManager") {
		return false
	}
	btDefineDelegate()
	btLoaded = true
	return true
}

func btConst(name string) ID { return Const(framework("CoreBluetooth"), name) }

// The module.

func btOK(reply func([]byte, error)) { reply([]byte("{}"), nil) }

func btJSON(reply func([]byte, error), v any) {
	out, err := json.Marshal(v)
	reply(out, err)
}

func btFail(reply func([]byte, error), s string) { reply(nil, errors.New(s)) }

// btArgs decodes a call's arguments, failing it if they're bad.
func btArgs(args []byte, v any, reply func([]byte, error)) bool {
	if len(args) == 0 {
		args = []byte("{}")
	}
	if err := json.Unmarshal(args, v); err != nil {
		btFail(reply, "bad args: "+err.Error())
		return false
	}
	return true
}

// btUUIDs normalizes UUIDs, failing the call for one that isn't (Core
// Bluetooth raises an exception for it).
func btUUIDs(ss []string, reply func([]byte, error)) ([]string, bool) {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		u, ok := spec.ParseUUID(s)
		if !ok {
			btFail(reply, "bad args: not a UUID: "+s)
			return nil, false
		}
		out = append(out, spec.FormatUUID(u))
	}
	return out, true
}

// cbUUID is a CBUUID of a UUID ParseUUID took (autoreleased).
func cbUUID(s string) ID {
	return Send(Class("CBUUID"), "UUIDWithString:", uintptr(String(spec.Short(s))))
}

// cbUUIDs is an NSArray of CBUUIDs (0 for none).
func cbUUIDs(ss []string) ID {
	if len(ss) == 0 {
		return 0
	}
	ids := make([]ID, len(ss))
	for i, s := range ss {
		ids[i] = cbUUID(s)
	}
	return Array(ids...)
}

// uuidString is a CBUUID as the wire has it ("180D" → 128-bit).
func uuidString(u ID) string { return spec.Normalize(GoString(Send(u, "UUIDString"))) }

// attrUUID is a CBAttribute's (service, characteristic, descriptor).
func attrUUID(a ID) string { return uuidString(Send(a, "UUID")) }

func (b *btModule) module() map[string]method {
	return map[string]method{
		"state":   b.state,
		"request": b.request,
		"next": func(_ []byte, reply func([]byte, error)) {
			b.waiting = reply
			b.following = true
			b.flush()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			b.following = false
			if w := b.waiting; w != nil {
				b.waiting = nil
				btFail(w, "stopped")
			}
			b.dropAdvertisements()
			btOK(reply)
		},
		"scan":               b.scan,
		"stopScan":           b.stopScan,
		"stopBackgroundScan": func(_ []byte, reply func([]byte, error)) { btOK(reply) },
		"choose": func(_ []byte, reply func([]byte, error)) {
			btFail(reply, "not available: macOS has no device chooser for apps (scan instead)")
		},
		"known":           b.known,
		"connected":       b.connectedDevices,
		"connect":         b.connect,
		"disconnect":      b.disconnect,
		"services":        b.services,
		"read":            b.read,
		"write":           b.write,
		"readDescriptor":  b.readDescriptor,
		"writeDescriptor": b.writeDescriptor,
		"subscribe":       func(args []byte, reply func([]byte, error)) { b.subscribe(args, true, reply) },
		"unsubscribe":     func(args []byte, reply func([]byte, error)) { b.subscribe(args, false, reply) },
		"mtu":             b.mtu,
		"requestMtu":      b.mtu,
		"rssi":            b.rssi,
		"priority": func(args []byte, reply func([]byte, error)) {
			var a spec.Priority
			if btArgs(args, &a, reply) {
				if _, ok := b.connOf(a.Conn, reply); ok {
					btOK(reply)
				}
			}
		},
		"advertise":     b.advertise,
		"stopAdvertise": b.stopAdvertise,
		"serve":         b.serve,
		"unserve":       b.unserve,
		"respond":       b.respond,
		"notify":        b.notify,
		// No state restoration on a Mac: an app runs until it quits.
		"restore": func(_ []byte, reply func([]byte, error)) { btOK(reply) },
	}
}

// emit queues an event for "next", answering one that waits. (Every
// advertisement, following or not: a scan Go starts right after a "stop"
// hears its first before the next "next".)
func (b *btModule) emit(e spec.Event) {
	b.queue = append(b.queue, e)
	b.flush()
}

// dropAdvertisements drops the queued advertisements, which no scan of
// Go's wants: when Go stops following, every scan it had has stopped.
func (b *btModule) dropAdvertisements() {
	q := b.queue[:0]
	for _, e := range b.queue {
		if e.Type != spec.EventAdvertisement {
			q = append(q, e)
		}
	}
	b.queue = q
}

func (b *btModule) flush() {
	if b.waiting == nil || len(b.queue) == 0 {
		return
	}
	out, err := json.Marshal(b.queue)
	b.queue = nil
	w := b.waiting
	b.waiting = nil
	w(out, err)
}

// The state.

// stateNow is the state without asking: from the authorization until
// the central manager is made, then its state.
func (b *btModule) stateNow() string {
	if b.central == 0 || b.centralState < 0 {
		switch btAuthorization() {
		case cbNotDetermined:
			return spec.StateNotAsked
		case cbRestricted, cbDenied:
			return spec.StateDenied
		}
		return spec.StateUnknown
	}
	return btStateWord(b.centralState)
}

func (b *btModule) tell(reply func([]byte, error), word string) {
	b.word = word
	btJSON(reply, spec.State{State: word})
}

func (b *btModule) state(_ []byte, reply func([]byte, error)) {
	if !btLoad() {
		b.tell(reply, spec.StateUnsupported)
		return
	}
	if b.central == 0 {
		switch btAuthorization() {
		case cbNotDetermined:
			b.tell(reply, spec.StateNotAsked)
			return
		case cbRestricted, cbDenied:
			b.tell(reply, spec.StateDenied)
			return
		}
	}
	// Allowed (or asked already): the central manager's state, once it
	// has one (making it asks nothing now).
	b.withCentral(func() { b.tell(reply, b.stateNow()) })
}

func (b *btModule) request(_ []byte, reply func([]byte, error)) {
	if !btLoad() {
		b.tell(reply, spec.StateUnsupported)
		return
	}
	if a := btAuthorization(); b.central == 0 && (a == cbRestricted || a == cbDenied) {
		b.tell(reply, spec.StateDenied)
		return
	}
	// Making the manager shows the prompt; its first update comes once
	// the user answered (another, if it came before).
	var answer func()
	answer = func() {
		if btAuthorization() == cbNotDetermined && (b.centralState == cbStateUnknown || b.centralState == cbStateResetting) {
			b.centralWaiting = append(b.centralWaiting, answer)
			return
		}
		b.tell(reply, b.stateNow())
	}
	b.withCentral(answer)
}

// withCentral makes the central manager if it isn't, then calls f once
// it has a state.
func (b *btModule) withCentral(f func()) {
	if b.central == 0 {
		b.centralWaiting = append(b.centralWaiting, f)
		b.central = Send(Send(Class("CBCentralManager"), "alloc"), "initWithDelegate:queue:options:",
			uintptr(b.delegate), mainQueue(), 0)
		return
	}
	if b.centralState < 0 {
		b.centralWaiting = append(b.centralWaiting, f)
		return
	}
	f()
}

// onCentral calls f once the central manager is on, or fails the call
// with its state.
func (b *btModule) onCentral(reply func([]byte, error), f func()) {
	if !btLoad() {
		btFail(reply, spec.StateUnsupported)
		return
	}
	b.withCentral(func() {
		if w := btStateWord(b.centralState); w != spec.StateOn {
			btFail(reply, w)
			return
		}
		f()
	})
}

// centralUpdated is centralManagerDidUpdateState:.
func (b *btModule) centralUpdated(m ID) {
	if m != b.central {
		return
	}
	b.centralState = SendInt(m, "state")
	word := btStateWord(b.centralState)
	if word != b.word {
		b.word = word
		b.emit(spec.Event{Type: spec.EventState, State: word})
	}
	if word != spec.StateOn {
		// Core Bluetooth stopped the scan and dropped the connections
		// (telling no one).
		for _, id := range sortedKeys(b.scans) {
			b.emit(spec.Event{Type: spec.EventScanStopped, Scan: id, Error: word})
		}
		clear(b.scans)
		b.scanning = false
		reason := spec.ReasonUnknown
		if word == spec.StateOff {
			reason = spec.ReasonOff
		}
		for _, p := range b.peerByObj {
			p.lost(word, spec.Event{Type: spec.EventDisconnected, Reason: reason, Error: "Bluetooth is " + word})
		}
	}
	ws := b.centralWaiting
	b.centralWaiting = nil
	for _, w := range ws {
		w()
	}
}

func sortedKeys[V any](m map[uint32]V) []uint32 {
	ks := make([]uint32, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// Errors.

// btError is an NSError as the wire has it: the device's ATT errors
// ("att <n>: ..."), Core Bluetooth's that Go reads, else its words.
func btError(err ID, fallback string) error {
	if err == 0 {
		return errors.New(fallback)
	}
	domain := GoString(Send(err, "domain"))
	code := SendInt(err, "code")
	msg := GoString(Send(err, "localizedDescription"))
	switch domain {
	case "CBATTErrorDomain":
		return fmt.Errorf("att %d: %s", code, msg)
	case "CBErrorDomain":
		switch code {
		case 3, 7: // notConnected, peripheralDisconnected
			return errors.New("disconnected")
		case 5: // operationCancelled
			return errors.New("canceled")
		case 6, 15: // connectionTimeout, encryptionTimedOut
			return errors.New("timeout")
		case 2, 12: // invalidHandle, unknownDevice
			return errors.New("not found: " + msg)
		}
	}
	if msg == "" {
		msg = fallback
	}
	return errors.New(msg)
}

// btReason is a disconnection's reason, from didDisconnectPeripheral's
// error (none: this Mac ended it, not at the app's asking).
func btReason(err ID) (reason string, code int, msg string) {
	if err == 0 {
		return spec.ReasonLocal, 0, ""
	}
	code = SendInt(err, "code")
	msg = GoString(Send(err, "localizedDescription"))
	reason = spec.ReasonUnknown
	if GoString(Send(err, "domain")) == "CBErrorDomain" {
		switch code {
		case 6: // connectionTimeout
			reason = spec.ReasonTimeout
		case 7: // peripheralDisconnected
			reason = spec.ReasonRemote
		case 10: // connectionFailed
			reason = spec.ReasonFailed
		}
	}
	return reason, code, msg
}

// Scanning.

type btScan struct {
	services []string // none: every device
	dup      bool
	seen     map[string]bool
}

// mergeScans is the one scan that serves them all: the union of their
// services (nil, every device, if one wants every device), with
// duplicates if one wants them.
func mergeScans(scans map[uint32]*btScan) (services []string, dup bool) {
	all := false
	for _, id := range sortedKeys(scans) {
		s := scans[id]
		dup = dup || s.dup
		if len(s.services) == 0 {
			all = true
		}
		for _, u := range s.services {
			if !slices.Contains(services, u) {
				services = append(services, u)
			}
		}
	}
	if all {
		services = nil
	}
	return services, dup
}

// scanMatches reports whether an advertisement is one a scan wants.
func scanMatches(s *btScan, a *spec.Advertisement) bool {
	if len(s.services) == 0 {
		return true
	}
	for _, u := range a.Services {
		if slices.Contains(s.services, u) {
			return true
		}
	}
	return false
}

func (b *btModule) scan(args []byte, reply func([]byte, error)) {
	var a spec.ScanArgs
	if !btArgs(args, &a, reply) {
		return
	}
	services, ok := btUUIDs(a.Services, reply)
	if !ok {
		return
	}
	b.onCentral(reply, func() {
		if b.scans == nil {
			b.scans = map[uint32]*btScan{}
		}
		b.scans[a.ID] = &btScan{services: services, dup: a.Duplicates, seen: map[string]bool{}}
		// Started again: the new scan hears the devices the running one
		// heard already.
		b.updateScan(true)
		btOK(reply)
	})
}

func (b *btModule) stopScan(args []byte, reply func([]byte, error)) {
	var a spec.ID
	if !btArgs(args, &a, reply) {
		return
	}
	if _, ok := b.scans[a.ID]; ok {
		delete(b.scans, a.ID)
		b.updateScan(false)
	}
	btOK(reply)
}

// updateScan scans for what the scans want now (again, if restart).
func (b *btModule) updateScan(restart bool) {
	if b.central == 0 {
		return
	}
	if len(b.scans) == 0 {
		if b.scanning {
			Send(b.central, "stopScan")
			b.scanning = false
		}
		return
	}
	services, dup := mergeScans(b.scans)
	if b.scanning && !restart && dup == b.scanDup && slices.Equal(services, b.scanServices) {
		return
	}
	if b.scanning {
		Send(b.central, "stopScan")
	}
	opts := Dict(btConst("CBCentralManagerScanOptionAllowDuplicatesKey"), NumberBool(dup))
	Send(b.central, "scanForPeripheralsWithServices:options:", uintptr(cbUUIDs(services)), uintptr(opts))
	b.scanning, b.scanServices, b.scanDup = true, services, dup
}

// discovered is centralManager:didDiscoverPeripheral:advertisementData:RSSI:.
func (b *btModule) discovered(p, data, rssi ID) {
	peer := b.peerFor(p)
	b.heard(decodeAdvertisement(peer.device(), data, SendInt(rssi, "integerValue")))
}

// heard gives an advertisement to the scans that want it: those whose
// services it has, once per device unless they want duplicates.
func (b *btModule) heard(a *spec.Advertisement) {
	for _, id := range sortedKeys(b.scans) {
		s := b.scans[id]
		if !scanMatches(s, a) || (!s.dup && s.seen[a.Device.ID]) {
			continue
		}
		s.seen[a.Device.ID] = true
		b.emit(spec.Event{Type: spec.EventAdvertisement, Scan: id, Advertisement: a})
	}
}

// decodeAdvertisement reads Core Bluetooth's advertisement data.
func decodeAdvertisement(d spec.Device, data ID, rssi int) *spec.Advertisement {
	a := &spec.Advertisement{Device: d, RSSI: rssi, Time: time.Now().UnixMilli()}
	if data == 0 {
		return a
	}
	get := func(key string) ID { return Send(data, "objectForKey:", uintptr(btConst(key))) }
	a.LocalName = GoString(get("CBAdvertisementDataLocalNameKey"))
	for _, k := range []string{"CBAdvertisementDataServiceUUIDsKey", "CBAdvertisementDataOverflowServiceUUIDsKey"} {
		for _, u := range ArrayItems(get(k)) {
			if s := uuidString(u); !slices.Contains(a.Services, s) {
				a.Services = append(a.Services, s)
			}
		}
	}
	for _, u := range ArrayItems(get("CBAdvertisementDataSolicitedServiceUUIDsKey")) {
		a.Solicited = append(a.Solicited, uuidString(u))
	}
	if sd := get("CBAdvertisementDataServiceDataKey"); sd != 0 {
		for _, k := range ArrayItems(Send(sd, "allKeys")) {
			if a.ServiceData == nil {
				a.ServiceData = map[string][]byte{}
			}
			a.ServiceData[uuidString(k)] = Bytes(Send(sd, "objectForKey:", uintptr(k)))
		}
	}
	if md := Bytes(get("CBAdvertisementDataManufacturerDataKey")); len(md) >= 2 {
		company := int(md[0]) | int(md[1])<<8
		a.ManufacturerData = map[string][]byte{strconv.Itoa(company): md[2:]}
	}
	if tx := get("CBAdvertisementDataTxPowerLevelKey"); tx != 0 {
		n := SendInt(tx, "integerValue")
		a.TxPower = &n
	}
	if c := get("CBAdvertisementDataIsConnectable"); c != 0 {
		v := SendBool(c, "boolValue")
		a.Connectable = &v
	}
	return a
}

// The delegate: the central manager's, the peripherals', the peripheral
// manager's.

func btDefineDelegate() {
	DefineClass("AsukaBluetoothDelegate", "NSObject",
		[]string{"CBCentralManagerDelegate", "CBPeripheralDelegate", "CBPeripheralManagerDelegate"}, []objc.MethodDef{
			// CBCentralManagerDelegate.
			Method("centralManagerDidUpdateState:", func(self ID, _ SEL, m ID) { bt.centralUpdated(m) }),
			Method("centralManager:didDiscoverPeripheral:advertisementData:RSSI:", func(self ID, _ SEL, m, p, data, rssi ID) {
				bt.discovered(p, data, rssi)
			}),
			Method("centralManager:didConnectPeripheral:", func(self ID, _ SEL, m, p ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.didConnect()
				}
			}),
			Method("centralManager:didFailToConnectPeripheral:error:", func(self ID, _ SEL, m, p, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.didFailToConnect(err)
				}
			}),
			Method("centralManager:didDisconnectPeripheral:error:", func(self ID, _ SEL, m, p, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.didDisconnect(err)
				}
			}),

			// CBPeripheralDelegate.
			Method("peripheral:didDiscoverServices:", func(self ID, _ SEL, p, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.didDiscoverServices(err)
				}
			}),
			Method("peripheral:didDiscoverCharacteristicsForService:error:", func(self ID, _ SEL, p, s, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.didDiscoverCharacteristics(s, err)
				}
			}),
			Method("peripheral:didDiscoverDescriptorsForCharacteristic:error:", func(self ID, _ SEL, p, c, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.didDiscoverDescriptors(err)
				}
			}),
			Method("peripheral:didModifyServices:", func(self ID, _ SEL, p, invalidated ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.servicesModified(invalidated)
				}
			}),
			Method("peripheral:didUpdateValueForCharacteristic:error:", func(self ID, _ SEL, p, c, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.valueUpdated(c, err)
				}
			}),
			Method("peripheral:didWriteValueForCharacteristic:error:", func(self ID, _ SEL, p, c, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.answer(btOp{c, opWrite}, nil, err)
				}
			}),
			Method("peripheral:didUpdateNotificationStateForCharacteristic:error:", func(self ID, _ SEL, p, c, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.answer(btOp{c, opNotify}, nil, err)
				}
			}),
			Method("peripheral:didUpdateValueForDescriptor:error:", func(self ID, _ SEL, p, d, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					var v []byte
					if err == 0 {
						v = descriptorBytes(d)
					}
					peer.answer(btOp{d, opReadDesc}, v, err)
				}
			}),
			Method("peripheral:didWriteValueForDescriptor:error:", func(self ID, _ SEL, p, d, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.answer(btOp{d, opWriteDesc}, nil, err)
				}
			}),
			Method("peripheral:didReadRSSI:error:", func(self ID, _ SEL, p, rssi, err ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.rssiRead(rssi, err)
				}
			}),
			Method("peripheralIsReadyToSendWriteWithoutResponse:", func(self ID, _ SEL, p ID) {
				if peer := bt.peerByObj[p]; peer != nil {
					peer.sendWrites()
				}
			}),

			// CBPeripheralManagerDelegate.
			Method("peripheralManagerDidUpdateState:", func(self ID, _ SEL, m ID) { bt.pmUpdated(m) }),
			Method("peripheralManagerDidStartAdvertising:error:", func(self ID, _ SEL, m, err ID) { bt.advertisingStarted(err) }),
			Method("peripheralManager:didAddService:error:", func(self ID, _ SEL, m, s, err ID) { bt.serviceAdded(s, err) }),
			Method("peripheralManager:central:didSubscribeToCharacteristic:", func(self ID, _ SEL, m, central, c ID) {
				bt.subscription(central, c, true)
			}),
			Method("peripheralManager:central:didUnsubscribeFromCharacteristic:", func(self ID, _ SEL, m, central, c ID) {
				bt.subscription(central, c, false)
			}),
			Method("peripheralManager:didReceiveReadRequest:", func(self ID, _ SEL, m, r ID) { bt.readRequest(r) }),
			Method("peripheralManager:didReceiveWriteRequests:", func(self ID, _ SEL, m, rs ID) { bt.writeRequests(rs) }),
			Method("peripheralManagerIsReadyToUpdateSubscribers:", func(self ID, _ SEL, m ID) { bt.sendNotifications() }),
		})
	bt.delegate = New("AsukaBluetoothDelegate")
}

// centralID is a CBCentral's (or a CBPeripheral's) identifier.
func peerID(obj ID) string { return GoString(Send(Send(obj, "identifier"), "UUIDString")) }

// btIDKey is a device ID as the peers map keys it (Core Bluetooth's
// upper case).
func btIDKey(id string) string { return strings.ToUpper(id) }
