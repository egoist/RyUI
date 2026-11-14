//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	. "github.com/egoist/asuka/internal/darwin"
)

// The Bluetooth module's parts that need no device: the state without
// asking, the event queue, the scans merged, the advertisement data,
// the attributes' handles, the errors, the app's own services. A real
// scan (and the peripheral role) runs with ASUKA_BLUETOOTH_SCAN=1.

// btCall calls a method of a module, answering at once or not.
func btCall(b *btModule, m string, args any) (out []byte, err error, answered *bool) {
	var raw []byte
	switch a := args.(type) {
	case nil:
		raw = []byte("{}")
	case string:
		raw = []byte(a)
	default:
		raw, _ = json.Marshal(a)
	}
	done := false
	b.module()[m](raw, func(r []byte, e error) { out, err, done = r, e, true })
	return out, err, &done
}

// btAwait calls a method and runs the main run loop until it answers
// (false if it doesn't, in a minute).
func btAwait(b *btModule, m string, args any) ([]byte, error, bool) {
	raw, _ := json.Marshal(args)
	if args == nil {
		raw = []byte("{}")
	}
	var out []byte
	var err error
	done := false
	b.module()[m](raw, func(r []byte, e error) { out, err, done = r, e, true })
	end := time.Now().Add(time.Minute)
	spin(func() bool { return done || time.Now().After(end) })
	for !done && time.Now().Before(end) {
		spin(func() bool { return done || time.Now().After(end) })
	}
	return out, err, done
}

// withBT runs f with a module of its own as the package's (the
// delegate's) one.
func withBT(f func(b *btModule)) {
	old := bt
	b := &btModule{centralState: -1, pmState: -1}
	bt = b
	defer func() { bt = old }()
	f(b)
}

func TestBluetoothStateAsksNothing(t *testing.T) {
	onMain(func() {
		if !btLoad() {
			t.Skip("no Core Bluetooth")
		}
		auth := btAuthorization
		defer func() { btAuthorization = auth }()
		withBT(func(b *btModule) {
			for _, c := range []struct {
				auth int
				want string
			}{{cbNotDetermined, spec.StateNotAsked}, {cbDenied, spec.StateDenied}, {cbRestricted, spec.StateDenied}} {
				btAuthorization = func() int { return c.auth }
				out, err, done := btCall(b, "state", nil)
				if !*done || err != nil || string(out) != `{"state":"`+c.want+`"}` {
					t.Errorf("authorization %d: state %q, %v (answered %v)", c.auth, out, err, *done)
				}
			}
			// Denied: "request" doesn't ask again (macOS wouldn't).
			out, err, done := btCall(b, "request", nil)
			if !*done || err != nil || string(out) != `{"state":"denied"}` {
				t.Errorf("request denied: %q, %v", out, err)
			}
			if b.central != 0 {
				t.Error("the central manager was made: it would have asked")
			}
		})
	})
}

func TestBluetoothFixedAnswers(t *testing.T) {
	onMain(func() {
		withBT(func(b *btModule) {
			for m, want := range map[string]string{"restore": "{}", "stopBackgroundScan": "{}", "stopScan": "{}", "disconnect": "{}"} {
				if out, err, _ := btCall(b, m, `{"id":3,"conn":3}`); err != nil || string(out) != want {
					t.Errorf("%s: %q, %v", m, out, err)
				}
			}
			if _, err, _ := btCall(b, "choose", nil); err == nil || !strings.HasPrefix(err.Error(), "not available: ") {
				t.Errorf("choose: %v", err)
			}
			// A connection that isn't: "disconnected"; bad arguments fail.
			for _, m := range []string{"services", "read", "write", "readDescriptor", "writeDescriptor", "subscribe", "unsubscribe", "mtu", "requestMtu", "rssi", "priority"} {
				if _, err, _ := btCall(b, m, `{"conn":9,"handle":1}`); errText(err) != "disconnected" {
					t.Errorf("%s of no connection: %v", m, err)
				}
			}
			if _, err, _ := btCall(b, "scan", `{"id":1,"services":["nope"]}`); !strings.HasPrefix(errText(err), "bad args: ") {
				t.Errorf("scan for a bad UUID: %v", err)
			}
			if _, err, _ := btCall(b, "scan", `[1]`); !strings.HasPrefix(errText(err), "bad args: ") {
				t.Errorf("scan with bad args: %v", err)
			}
			if out, err, _ := btCall(b, "connected", nil); err != nil || string(out) != "[]" {
				t.Errorf("connected with no services: %q, %v", out, err)
			}
			if out, err, _ := btCall(b, "respond", `{"server":1,"request":5}`); err != nil || string(out) != "{}" {
				t.Errorf("respond to no request: %q, %v", out, err)
			}
			if _, err, _ := btCall(b, "notify", `{"server":1,"handle":1,"value":""}`); errText(err) != "not found" {
				t.Errorf("notify of no server: %v", err)
			}
		})
	})
}

// "next" answers with what's queued, in order; "stop" fails it with
// "stopped" and drops the advertisements queued then (not those after).
func TestBluetoothEvents(t *testing.T) {
	onMain(func() {
		withBT(func(b *btModule) {
			b.emit(spec.Event{Type: spec.EventState, State: spec.StateOff})
			b.emit(spec.Event{Type: spec.EventAdvertisement, Scan: 1, Advertisement: &spec.Advertisement{}})
			btCall(b, "stop", nil)
			out, err, done := btCall(b, "next", nil)
			var evs []spec.Event
			if !*done || err != nil || json.Unmarshal(out, &evs) != nil || len(evs) != 1 || evs[0].State != spec.StateOff {
				fatalf(t, "next: %s, %v (answered %v)", out, err, *done)
			}
			answered := false
			b.module()["next"](nil, func(r []byte, e error) { out, err, answered = r, e, true })
			if answered {
				fatalf(t, "next answered with nothing queued: %s", out)
			}
			b.emit(spec.Event{Type: spec.EventValue, Conn: 2, Handle: 3, Value: []byte{1}})
			if !answered || err != nil || !strings.Contains(string(out), `"value":"AQ=="`) {
				fatalf(t, "next after a value: %s, %v", out, err)
			}
			answered = false
			b.module()["next"](nil, func(r []byte, e error) { out, err, answered = r, e, true })
			if _, e, _ := btCall(b, "stop", nil); e != nil {
				t.Error(e)
			}
			if !answered || errText(err) != "stopped" {
				t.Errorf("next after stop: %v (answered %v)", err, answered)
			}
			// A scan that starts before the next "next" keeps what it hears.
			b.emit(spec.Event{Type: spec.EventAdvertisement, Scan: 1, Advertisement: &spec.Advertisement{}})
			if len(b.queue) != 1 {
				t.Errorf("an advertisement after stop: %+v", b.queue)
			}
		})
	})
}

func TestBluetoothScansMerged(t *testing.T) {
	hr, bat := spec.Normalize("180d"), spec.Normalize("180f")
	for _, c := range []struct {
		scans    map[uint32]*btScan
		services []string
		dup      bool
	}{
		{map[uint32]*btScan{1: {services: []string{hr}}}, []string{hr}, false},
		{map[uint32]*btScan{1: {services: []string{hr}}, 2: {services: []string{bat, hr}, dup: true}}, []string{hr, bat}, true},
		{map[uint32]*btScan{1: {services: []string{hr}}, 2: {}}, nil, false},
	} {
		s, d := mergeScans(c.scans)
		if !slices.Equal(s, c.services) || d != c.dup {
			t.Errorf("%v: %v, %v; want %v, %v", c.scans, s, d, c.services, c.dup)
		}
	}
	// Each scan its own: those with its services, once per device
	// unless it wants duplicates.
	withBT(func(b *btModule) {
		b.following = true
		b.scans = map[uint32]*btScan{
			1: {seen: map[string]bool{}},
			2: {services: []string{hr}, seen: map[string]bool{}},
			3: {services: []string{bat}, dup: true, seen: map[string]bool{}},
		}
		strap := &spec.Advertisement{Device: spec.Device{ID: "A"}, Services: []string{hr}}
		meter := &spec.Advertisement{Device: spec.Device{ID: "B"}, Services: []string{bat}}
		for range 2 {
			b.heard(strap)
			b.heard(meter)
		}
		var got []string
		for _, e := range b.queue {
			got = append(got, fmt.Sprintf("%d:%s", e.Scan, e.Advertisement.Device.ID))
		}
		if want := []string{"1:A", "2:A", "1:B", "3:B", "3:B"}; !slices.Equal(got, want) {
			t.Errorf("advertisements %v, want %v", got, want)
		}
	})
}

func TestBluetoothAdvertisementData(t *testing.T) {
	onMain(func() {
		if !btLoad() {
			t.Skip("no Core Bluetooth")
		}
		custom := "6e400001-b5a3-f393-e0a9-e50e24dcca9e"
		data := Dict(
			btConst("CBAdvertisementDataLocalNameKey"), String("Polar H10"),
			btConst("CBAdvertisementDataServiceUUIDsKey"), Array(cbUUID("180d"), cbUUID(custom)),
			btConst("CBAdvertisementDataOverflowServiceUUIDsKey"), Array(cbUUID("180f"), cbUUID("180d")),
			btConst("CBAdvertisementDataSolicitedServiceUUIDsKey"), Array(cbUUID("1812")),
			btConst("CBAdvertisementDataServiceDataKey"), Dict(cbUUID("feaa"), Data([]byte{1, 2, 3})),
			btConst("CBAdvertisementDataManufacturerDataKey"), Data([]byte{0x4c, 0x00, 0x02, 0x15}),
			btConst("CBAdvertisementDataTxPowerLevelKey"), NumberInt(-8),
			btConst("CBAdvertisementDataIsConnectable"), NumberBool(true),
		)
		a := decodeAdvertisement(spec.Device{ID: "X", Name: "H10"}, data, -60)
		want := spec.Advertisement{Device: spec.Device{ID: "X", Name: "H10"}, RSSI: -60, LocalName: "Polar H10",
			Services:         []string{spec.Normalize("180d"), custom, spec.Normalize("180f")},
			Solicited:        []string{spec.Normalize("1812")},
			ServiceData:      map[string][]byte{spec.Normalize("feaa"): {1, 2, 3}},
			ManufacturerData: map[string][]byte{"76": {0x02, 0x15}},
		}
		if a.TxPower == nil || *a.TxPower != -8 || a.Connectable == nil || !*a.Connectable || a.Time == 0 {
			t.Errorf("tx power %v, connectable %v, time %d", a.TxPower, a.Connectable, a.Time)
		}
		a.TxPower, a.Connectable, a.Time = nil, nil, 0
		g, _ := json.Marshal(a)
		w, _ := json.Marshal(want)
		if string(g) != string(w) {
			t.Errorf("advertisement\n%s\nwant\n%s", g, w)
		}
		if a := decodeAdvertisement(spec.Device{ID: "Y"}, Dict(), -70); a.Services != nil || a.ManufacturerData != nil || a.TxPower != nil {
			t.Errorf("an empty advertisement: %+v", a)
		}
	})
}

func newBTPeer() *btPeer {
	return &btPeer{connecting: map[uint32]func([]byte, error){}, attrs: map[uint32]btAttr{}, handles: map[ID]uint32{},
		waits: map[btOp][]*btWait{}, subs: map[ID]map[uint32]bool{}}
}

// The handles: one per attribute, the same each time, gone with their
// service once it changed.
func TestBluetoothHandles(t *testing.T) {
	onMain(func() {
		if !btLoad() {
			t.Skip("no Core Bluetooth")
		}
		svc := func(uuid string, primary bool, chars ...ID) ID {
			s := Autorelease(Send(Send(Class("CBMutableService"), "alloc"), "initWithType:primary:", uintptr(cbUUID(uuid)), Bool(primary)))
			Send(s, "setCharacteristics:", uintptr(Array(chars...)))
			return s
		}
		char := func(uuid string, props int, descs ...ID) ID {
			c := Autorelease(Send(Send(Class("CBMutableCharacteristic"), "alloc"), "initWithType:properties:value:permissions:",
				uintptr(cbUUID(uuid)), uintptr(props), 0, 1))
			if len(descs) > 0 {
				Send(c, "setDescriptors:", uintptr(Array(descs...)))
			}
			return c
		}
		desc := Autorelease(Send(Send(Class("CBMutableDescriptor"), "alloc"), "initWithType:value:", uintptr(cbUUID("2901")), uintptr(String("Rate"))))
		hrs := svc("180d", true, char("2a37", 0x100), char("2a38", 0x02, desc))
		bas := svc("180f", false, char("2a19", 0x12))
		p := newBTPeer()
		tree := p.buildTree(Array(hrs, bas))
		g, _ := json.Marshal(tree)
		n := spec.Normalize
		want := fmt.Sprintf(`[{"handle":1,"uuid":"%s","primary":true,"characteristics":[{"handle":2,"uuid":"%s","properties":16},`+
			`{"handle":3,"uuid":"%s","properties":2,"descriptors":[{"handle":4,"uuid":"%s"}]}]},`+
			`{"handle":5,"uuid":"%s","primary":false,"characteristics":[{"handle":6,"uuid":"%s","properties":18}]}]`,
			n("180d"), n("2a37"), n("2a38"), n("2901"), n("180f"), n("2a19"))
		if string(g) != want {
			t.Errorf("tree\n%s\nwant\n%s", g, want)
		}
		if again, _ := json.Marshal(p.buildTree(Array(hrs, bas))); string(again) != want {
			t.Errorf("handles changed: %s", again)
		}
		if a := p.attrs[4]; a.kind != attrDesc || a.char != 3 || a.svc != 1 {
			t.Errorf("descriptor's attribute %+v", a)
		}
		withBT(func(b *btModule) {
			b.following = true
			p.conns = []uint32{7}
			p.treeOK = true
			failed := ""
			p.wait(btOp{p.attrs[2].obj, opRead}, &btWait{conn: 7, reply: func(_ []byte, err error) { failed = errText(err) }})
			p.servicesModified(Array(hrs))
			if len(p.attrs) != 2 || p.attrs[5].obj != bas || p.attrs[6].kind != attrChar || p.treeOK {
				t.Errorf("after the heart rate service changed: %v (tree ok %v)", p.attrs, p.treeOK)
			}
			if failed != "not found" {
				t.Errorf("a read of a changed service's characteristic: %q", failed)
			}
			if len(b.queue) != 1 || b.queue[0].Type != spec.EventServicesChanged || b.queue[0].Conn != 7 {
				t.Errorf("events %+v", b.queue)
			}
			// Discovered again: the new attributes are new handles.
			p.buildTree(Array(svc("180d", true), bas))
			if p.handles[bas] != 5 || p.nextHandle != 7 {
				t.Errorf("handles after discovering again: %v", p.handles)
			}
		})
		p.forget()
	})
}

// The answers come in order, each attribute's; a call whose connection
// went is answered by no one; a notify op's answer is its followers'.
func TestBluetoothAnswers(t *testing.T) {
	onMain(func() {
		p := newBTPeer()
		var got []string
		w := func(name string, issued bool) *btWait {
			return &btWait{issued: issued, reply: func(b []byte, err error) {
				got = append(got, fmt.Sprintf("%s:%s:%s", name, b, errText(err)))
			}}
		}
		c := ID(0x1234)
		p.wait(btOp{c, opRead}, w("r1", false))
		dead := w("r2", false)
		dead.reply = nil
		p.wait(btOp{c, opRead}, dead)
		p.wait(btOp{c, opRead}, w("r3", false))
		p.wait(btOp{c, opNotify}, w("on", true))
		p.wait(btOp{c, opNotify}, w("on2", false))
		p.wait(btOp{c, opNotify}, w("off", true))
		p.answer(btOp{c, opRead}, []byte("a"), 0)
		p.answer(btOp{c, opRead}, []byte("b"), 0)
		p.answer(btOp{c, opRead}, nil, 0)
		p.answer(btOp{c, opNotify}, nil, 0)
		attErr := Send(Class("NSError"), "errorWithDomain:code:userInfo:", uintptr(String("CBATTErrorDomain")), 3,
			uintptr(Dict(String("NSLocalizedDescription"), String("Writing is not permitted."))))
		p.answer(btOp{c, opNotify}, nil, attErr)
		if p.answer(btOp{c, opRead}, nil, 0) {
			t.Error("an answer with nothing waiting")
		}
		want := []string{"r1:a:", "r3::", "on:{}:", "on2:{}:", "off::att 3: Writing is not permitted."}
		if !slices.Equal(got, want) {
			t.Errorf("answers %q, want %q", got, want)
		}
	})
}

func TestBluetoothErrors(t *testing.T) {
	onMain(func() {
		nsErr := func(domain string, code int) ID {
			return Send(Class("NSError"), "errorWithDomain:code:userInfo:", uintptr(String(domain)), uintptr(code),
				uintptr(Dict(String("NSLocalizedDescription"), String("words"))))
		}
		for _, c := range []struct {
			domain string
			code   int
			want   string
		}{
			{"CBATTErrorDomain", 5, "att 5: words"},
			{"CBErrorDomain", 7, "disconnected"},
			{"CBErrorDomain", 6, "timeout"},
			{"CBErrorDomain", 5, "canceled"},
			{"CBErrorDomain", 12, "not found: words"},
			{"CBErrorDomain", 13, "words"},
		} {
			if e := btError(nsErr(c.domain, c.code), "x"); e.Error() != c.want {
				t.Errorf("%s %d: %q, want %q", c.domain, c.code, e, c.want)
			}
		}
		for _, c := range []struct {
			err    ID
			reason string
			code   int
		}{
			{0, spec.ReasonLocal, 0},
			{nsErr("CBErrorDomain", 6), spec.ReasonTimeout, 6},
			{nsErr("CBErrorDomain", 7), spec.ReasonRemote, 7},
			{nsErr("CBErrorDomain", 10), spec.ReasonFailed, 10},
			{nsErr("CBErrorDomain", 0), spec.ReasonUnknown, 0},
		} {
			if r, code, _ := btReason(c.err); r != c.reason || code != c.code {
				t.Errorf("disconnected by %v: %s %d, want %s %d", c.err, r, code, c.reason, c.code)
			}
		}
	})
}

func TestBluetoothLocalServices(t *testing.T) {
	// A long write's parts put together; writes of others apart.
	a, c := ID(1), ID(2)
	got := mergeWrites([]btPiece{
		{char: a, central: "X", offset: 0, value: []byte("hel")},
		{char: c, central: "X", offset: 0, value: []byte("1")},
		{char: a, central: "X", offset: 3, value: []byte("lo")},
		{char: a, central: "Y", offset: 0, value: []byte("y")},
		{char: a, central: "X", offset: 9, value: []byte("!")},
	})
	if g := fmt.Sprintf("%v", got); g != "[{1 X 0 [104 101 108 108 111]} {2 X 0 [49]} {1 Y 0 [121]} {1 X 9 [33]}]" {
		t.Errorf("writes merged %s", g)
	}
	if !writeResponse(spec.PropWrite|spec.PropWriteNoResponse) || writeResponse(spec.PropWriteNoResponse) || !writeResponse(spec.PropRead) {
		t.Error("writeResponse")
	}
	name, services := mergeAdvertising([]*btAdv{{name: "A", services: []string{"1"}}, {services: []string{"2", "1"}}, {name: "C"}})
	if name != "C" || !slices.Equal(services, []string{"1", "2"}) {
		t.Errorf("advertising %q %v", name, services)
	}
	ro := spec.LocalCharacteristic{Value: []byte("v1"), Properties: spec.PropRead, Permissions: spec.PermRead}
	rw := spec.LocalCharacteristic{Value: []byte("v1"), Properties: spec.PropRead | spec.PropNotify, Permissions: spec.PermRead}
	if !fixedValue(ro) || fixedValue(rw) || fixedValue(spec.LocalCharacteristic{Properties: spec.PropRead}) {
		t.Error("fixedValue")
	}
	for _, d := range []struct {
		desc, want string
	}{{"2901", ""}, {"2902", ""}, {"2903", "not available: "}, {"zz", "bad args: "}} {
		args := spec.ServeArgs{Services: []spec.LocalService{{UUID: "180d", Characteristics: []spec.LocalCharacteristic{
			{UUID: "2a37", Descriptors: []spec.LocalDescriptor{{UUID: d.desc}}}}}}}
		if msg := checkServe(&args); !strings.HasPrefix(msg, d.want) || (d.want == "") != (msg == "") {
			t.Errorf("serving a %s descriptor: %q", d.desc, msg)
		}
		if d.want == "" && args.Services[0].UUID != spec.Normalize("180d") {
			t.Errorf("not normalized: %s", args.Services[0].UUID)
		}
	}
	if msg := checkServe(&spec.ServeArgs{Services: []spec.LocalService{{UUID: "180d", Characteristics: []spec.LocalCharacteristic{
		{UUID: "2a37", Properties: spec.PropBroadcast}}}}}); !strings.HasPrefix(msg, "not available: ") {
		t.Errorf("serving a broadcast characteristic: %q", msg)
	}

	onMain(func() {
		if !btLoad() {
			t.Skip("no Core Bluetooth")
		}
		withBT(func(b *btModule) {
			b.localChars, b.requests = map[ID]*btLocalChar{}, map[uint32]*btRequest{}
			s := &btServer{id: 1, chars: map[uint32]*btLocalChar{}}
			svc := b.localService(s, spec.LocalService{UUID: spec.Normalize("180a"), Primary: true, Characteristics: []spec.LocalCharacteristic{
				{Handle: 1, UUID: spec.Normalize("2a29"), Properties: spec.PropRead, Permissions: spec.PermRead, Value: []byte("Asuka"),
					Descriptors: []spec.LocalDescriptor{{UUID: spec.Normalize("2901"), Value: []byte("Maker")}, {UUID: spec.Normalize("2902")}}},
				{Handle: 2, UUID: spec.Normalize("2a19"), Properties: spec.PropRead | spec.PropNotify | spec.PropExtendedProperties, Permissions: spec.PermRead, Value: []byte{90}},
			}})
			defer Release(svc)
			cs := ArrayItems(Send(svc, "characteristics"))
			if len(cs) != 2 || !SendBool(svc, "isPrimary") || attrUUID(svc) != spec.Normalize("180a") {
				fatalf(t, "service: %d characteristics", len(cs))
			}
			if v := string(Bytes(Send(cs[0], "value"))); v != "Asuka" || b.localChars[cs[0]].value != nil {
				t.Errorf("a read-only fixed value: Core Bluetooth's %q, the host's %q", v, b.localChars[cs[0]].value)
			}
			if ds := ArrayItems(Send(cs[0], "descriptors")); len(ds) != 1 || GoString(Send(ds[0], "value")) != "Maker" {
				t.Errorf("descriptors %v", ds)
			}
			if Send(cs[1], "value") != 0 || string(b.localChars[cs[1]].value) != "Z" || SendInt(cs[1], "properties") != spec.PropRead|spec.PropNotify {
				t.Errorf("a fixed value that notifies: Core Bluetooth's %v, the host's %q, properties %#x",
					Send(cs[1], "value"), b.localChars[cs[1]].value, SendInt(cs[1], "properties"))
			}
			// A batch answered once each write of it is, with the first error.
			batch := &btBatch{left: 2}
			b.requests[1] = &btRequest{server: s, batch: batch}
			b.requests[2] = &btRequest{server: s, batch: batch}
			btCall(b, "respond", spec.Respond{Server: 1, Request: 2, Error: 3})
			btCall(b, "respond", spec.Respond{Server: 1, Request: 1})
			if batch.left != 0 || batch.code != 3 || len(b.requests) != 0 {
				t.Errorf("batch %+v, requests %v", batch, b.requests)
			}
			b.servers = map[uint32]*btServer{1: s}
			if _, err, _ := btCall(b, "notify", spec.Notify{Server: 1, Handle: 1}); !strings.HasPrefix(errText(err), "att 6: ") {
				t.Errorf("notify of a characteristic that doesn't: %v", err)
			}
			if out, err, _ := btCall(b, "notify", spec.Notify{Server: 1, Handle: 2, Value: []byte{1}}); err != nil || string(out) != "{}" || len(b.notifyQ) != 0 {
				t.Errorf("notify with no subscribers: %q %v", out, err)
			}
			b.dropServer(s, false)
			if len(b.localChars) != 0 || len(b.servers) != 0 {
				t.Errorf("after closing: %v, %v", b.localChars, b.servers)
			}
		})
	})
}

// A real scan: ASUKA_BLUETOOTH_SCAN=1 go test ./host/darwin -run BluetoothReal -v
// (asks for the permission the first time: the terminal's). With
// ASUKA_BLUETOOTH_PERIPHERAL=1 it also serves and advertises a service.
func TestBluetoothReal(t *testing.T) {
	if os.Getenv("ASUKA_BLUETOOTH_SCAN") == "" {
		t.Skip("ASUKA_BLUETOOTH_SCAN=1 scans for real")
	}
	onMain(func() {
		if !btLoad() {
			fatal(t, "no Core Bluetooth")
		}
		b := bt
		out, err, _ := btAwait(b, "state", nil)
		t.Logf("state before asking: %s %v (authorization %d)", out, err, btAuthorization())
		out, err, done := btAwait(b, "request", nil)
		if !done {
			fatal(t, "request: no answer")
		}
		t.Logf("request: %s %v", out, err)
		var s spec.State
		json.Unmarshal(out, &s)
		if s.State != spec.StateOn {
			fatalf(t, "Bluetooth is %q", s.State)
		}
		var events []spec.Event
		var follow func()
		follow = func() {
			b.module()["next"]([]byte("{}"), func(r []byte, err error) {
				if err != nil {
					return
				}
				var evs []spec.Event
				json.Unmarshal(r, &evs)
				events = append(events, evs...)
				follow()
			})
		}
		follow()
		if _, err, _ := btCall(b, "scan", spec.ScanArgs{ID: 1}); err != nil {
			fatal(t, "scan: ", err)
		}
		wait := func(d time.Duration) {
			end := time.Now().Add(d)
			spin(func() bool { return time.Now().After(end) })
		}
		wait(4 * time.Second)
		// A second scan, once the first heard the devices around: it hears
		// them too (the one scan started again).
		if _, err, _ := btCall(b, "scan", spec.ScanArgs{ID: 2}); err != nil {
			fatal(t, "scan 2: ", err)
		}
		wait(4 * time.Second)
		btCall(b, "stopScan", spec.ID{ID: 1})
		btCall(b, "stopScan", spec.ID{ID: 2})
		if b.scanning {
			t.Error("still scanning")
		}
		heard := map[uint32]map[string]bool{1: {}, 2: {}}
		for _, e := range events {
			if e.Type != spec.EventAdvertisement {
				t.Logf("event %+v", e)
				continue
			}
			a := e.Advertisement
			heard[e.Scan][a.Device.ID] = true
			if e.Scan != 1 {
				continue
			}
			var extra []string
			if a.LocalName != "" {
				extra = append(extra, "localName="+a.LocalName)
			}
			if len(a.Services) > 0 {
				extra = append(extra, "services="+strings.Join(a.Services, ","))
			}
			for k, v := range a.ManufacturerData {
				extra = append(extra, fmt.Sprintf("manufacturer %s: %x", k, v))
			}
			for k, v := range a.ServiceData {
				extra = append(extra, fmt.Sprintf("serviceData %s: %x", k, v))
			}
			if a.TxPower != nil {
				extra = append(extra, fmt.Sprintf("tx=%d", *a.TxPower))
			}
			if a.Connectable != nil {
				extra = append(extra, fmt.Sprintf("connectable=%v", *a.Connectable))
			}
			t.Logf("heard %s %q rssi %d %s", a.Device.ID, a.Device.Name, a.RSSI, strings.Join(extra, " "))
		}
		both := 0
		for id := range heard[1] {
			if heard[2][id] {
				both++
			}
		}
		t.Logf("scan 1 heard %d devices, scan 2 %d (%d of scan 1's)", len(heard[1]), len(heard[2]), both)

		if id := os.Getenv("ASUKA_BLUETOOTH_CONNECT"); id != "" {
			btConnectReal(t, b, id, wait)
		}
		if os.Getenv("ASUKA_BLUETOOTH_PERIPHERAL") != "" {
			btPeripheralReal(t, b, wait)
		}
		btCall(b, "stop", nil)
	})
}

func btConnectReal(t *testing.T, b *btModule, id string, wait func(time.Duration)) {
	call := func(m string, args any) ([]byte, error) {
		out, err, done := btAwait(b, m, args)
		if !done {
			return nil, errors.New("no answer")
		}
		return out, err
	}
	if _, err := call("connect", spec.ConnectArgs{Conn: 100, Device: id}); err != nil {
		t.Errorf("connect %s: %v", id, err)
		return
	}
	out, err := call("services", spec.Conn{Conn: 100})
	t.Logf("services: %s %v", out, err)
	var tree []spec.Service
	json.Unmarshal(out, &tree)
	for _, s := range tree {
		for _, c := range s.Characteristics {
			if c.Properties&spec.PropRead != 0 {
				v, err := call("read", spec.Attr{Conn: 100, Handle: c.Handle})
				t.Logf("read %s: %q %v", c.UUID, v, err)
			}
		}
	}
	out, err = call("mtu", spec.Conn{Conn: 100})
	t.Logf("mtu: %s %v", out, err)
	out, err = call("rssi", spec.Conn{Conn: 100})
	t.Logf("rssi: %s %v", out, err)
	out, err = call("disconnect", spec.Conn{Conn: 100})
	t.Logf("disconnect: %s %v", out, err)
}

func btPeripheralReal(t *testing.T, b *btModule, wait func(time.Duration)) {
	call := func(m string, args any) ([]byte, error) {
		out, err, done := btAwait(b, m, args)
		if !done {
			return nil, errors.New("no answer")
		}
		return out, err
	}
	svc := "6e400001-b5a3-f393-e0a9-e50e24dcca9e"
	out, err := call("serve", spec.ServeArgs{ID: 50, Services: []spec.LocalService{{UUID: svc, Primary: true, Characteristics: []spec.LocalCharacteristic{
		{Handle: 1, UUID: "6e400002-b5a3-f393-e0a9-e50e24dcca9e", Properties: spec.PropWrite | spec.PropWriteNoResponse, Permissions: spec.PermWrite},
		{Handle: 2, UUID: "6e400003-b5a3-f393-e0a9-e50e24dcca9e", Properties: spec.PropNotify | spec.PropRead, Permissions: spec.PermRead},
		{Handle: 3, UUID: spec.Normalize("2a29"), Properties: spec.PropRead, Permissions: spec.PermRead, Value: []byte("Asuka")},
	}}}})
	t.Logf("serve: %s %v", out, err)
	out, err = call("advertise", spec.AdvertiseArgs{ID: 51, LocalName: "Asuka Test", Services: []string{svc}, Connectable: true})
	t.Logf("advertise: %s %v (advertising %v)", out, err, SendBool(b.pm, "isAdvertising"))
	out, err = call("advertise", spec.AdvertiseArgs{ID: 52, Services: []string{spec.Normalize("180f")}})
	t.Logf("a second advertisement: %s %v", out, err)
	wait(2 * time.Second)
	out, err = call("notify", spec.Notify{Server: 50, Handle: 2, Value: []byte{1}})
	t.Logf("notify (no subscribers): %s %v", out, err)
	call("stopAdvertise", spec.ID{ID: 52})
	call("stopAdvertise", spec.ID{ID: 51})
	wait(500 * time.Millisecond)
	out, err = call("unserve", spec.ID{ID: 50})
	t.Logf("unserve: %s %v (advertising %v)", out, err, SendBool(b.pm, "isAdvertising"))
}
