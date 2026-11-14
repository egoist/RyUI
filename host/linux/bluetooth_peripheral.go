//go:build linux && !android

package linux

import (
	"errors"
	"fmt"
	"strconv"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	"github.com/egoist/asuka/internal/bluez"
	. "github.com/egoist/asuka/internal/gtk"
)

// The peripheral role: the app's objects, which BlueZ calls. An
// advertisement is an LEAdvertisement1 registered with the adapter's
// LEAdvertisingManager1; a server an application (an ObjectManager over
// its services, characteristics and descriptors) registered with its
// GattManager1. BlueZ calls a characteristic's ReadValue and WriteValue
// as centrals read and write it: the call is answered once Go answers
// the "read" or "write" event ("respond").

const btObjectsXML = `<node>
 <interface name="org.freedesktop.DBus.ObjectManager">
  <method name="GetManagedObjects"><arg name="objects" type="a{oa{sa{sv}}}" direction="out"/></method>
  <signal name="InterfacesAdded"><arg name="object" type="o"/><arg name="interfaces" type="a{sa{sv}}"/></signal>
  <signal name="InterfacesRemoved"><arg name="object" type="o"/><arg name="interfaces" type="as"/></signal>
 </interface>
 <interface name="org.bluez.LEAdvertisement1">
  <method name="Release"/>
  <property name="Type" type="s" access="read"/>
  <property name="ServiceUUIDs" type="as" access="read"/>
  <property name="ManufacturerData" type="a{qv}" access="read"/>
  <property name="ServiceData" type="a{sv}" access="read"/>
  <property name="LocalName" type="s" access="read"/>
  <property name="Includes" type="as" access="read"/>
  <property name="Discoverable" type="b" access="read"/>
 </interface>
 <interface name="org.bluez.GattService1">
  <property name="UUID" type="s" access="read"/>
  <property name="Primary" type="b" access="read"/>
 </interface>
 <interface name="org.bluez.GattCharacteristic1">
  <method name="ReadValue"><arg name="options" type="a{sv}" direction="in"/><arg name="value" type="ay" direction="out"/></method>
  <method name="WriteValue"><arg name="value" type="ay" direction="in"/><arg name="options" type="a{sv}" direction="in"/></method>
  <method name="StartNotify"/>
  <method name="StopNotify"/>
  <method name="Confirm"/>
  <property name="UUID" type="s" access="read"/>
  <property name="Service" type="o" access="read"/>
  <property name="Flags" type="as" access="read"/>
  <property name="Value" type="ay" access="read"/>
 </interface>
 <interface name="org.bluez.GattDescriptor1">
  <method name="ReadValue"><arg name="options" type="a{sv}" direction="in"/><arg name="value" type="ay" direction="out"/></method>
  <method name="WriteValue"><arg name="value" type="ay" direction="in"/><arg name="options" type="a{sv}" direction="in"/></method>
  <property name="UUID" type="s" access="read"/>
  <property name="Characteristic" type="o" access="read"/>
  <property name="Flags" type="as" access="read"/>
 </interface>
</node>`

// btBasePath is where the app's objects are, under its own name on the
// bus (BlueZ tells them apart by their owner).
const btBasePath = "/dev/asuka/bluetooth"

// btPseudoCentral is the central of a subscription: BlueZ calls
// StartNotify as the first central subscribes and StopNotify as the
// last one leaves, without saying which.
var btPseudoCentral = spec.Device{ID: "00:00:00:00:00:00", Name: "BlueZ"}

// btAdvert is an advertisement of the app's.
type btAdvert struct {
	id      uint32
	path    string
	adapter string
	args    spec.AdvertiseArgs
	reg     uint32
	on      bool // registered
	stopped bool // stopAdvertise came while registering
}

func (b *bluetooth) peripheralAdapter(iface string, reply func([]byte, error)) (string, bool) {
	if s := b.current(); s != spec.StateOn {
		btFail(reply, s)
		return "", false
	}
	a := b.adapter()
	if b.props(a, iface) == nil {
		what := "advertise"
		if iface == ifGattManager {
			what = "serve GATT services"
		}
		btFail(reply, "not available: the adapter can't "+what+" (BlueZ has no "+iface+" on it)")
		return "", false
	}
	return a, true
}

func (b *bluetooth) advertise(a spec.AdvertiseArgs, reply func([]byte, error)) {
	adapter, ok := b.peripheralAdapter(ifAdvManager, reply)
	if !ok {
		return
	}
	if old := b.adverts[a.ID]; old != nil {
		b.stopAdvertise(a.ID)
	}
	ad := &btAdvert{id: a.ID, path: fmt.Sprintf("%s/advertisement%d", btBasePath, a.ID), adapter: adapter, args: a}
	reg, err := DBusRegister(b.conn, ad.path, btObjectsXML, ifAdvert, DBusObject{
		Method: func(c DBusInvocation) {
			c.Return(0)
			if c.Method == "Release" && b.adverts[ad.id] == ad {
				// BlueZ stopped it (the adapter went, its instances
				// were taken).
				b.endAdvert(ad.id, "BlueZ released the advertisement")
				b.flush()
			}
		},
		Property: ad.property,
	})
	if err != nil {
		btFail(reply, err.Error())
		return
	}
	ad.reg = reg
	b.adverts[a.ID] = ad
	DBusCall(b.conn, bluezName, adapter, ifAdvManager, "RegisterAdvertisement", VTuple(VPath(ad.path), VDict(nil)), btCallTimeout, func(_ Ptr, err error) {
		if err != nil {
			if b.adverts[ad.id] == ad {
				delete(b.adverts, ad.id)
			}
			DBusUnregister(b.conn, ad.reg)
			_, msg := bluez.Error(err)
			why := bluez.ClientError(err, false)
			if msg != "" && why == msg {
				why = "BlueZ can't advertise it: " + msg // too much data, too many advertisements
			}
			btFail(reply, why)
			return
		}
		ad.on = true
		btOK(reply)
		if ad.stopped || b.adverts[ad.id] != ad {
			b.unregisterAdvert(ad)
		}
	})
}

// property is the advertisement's: what BlueZ puts in it.
func (ad *btAdvert) property(name string) Ptr {
	a := ad.args
	switch name {
	case "Type":
		if a.Connectable {
			return VStr("peripheral")
		}
		return VStr("broadcast")
	case "ServiceUUIDs":
		uuids := make([]string, len(a.Services))
		for i, u := range a.Services {
			uuids[i] = spec.Short(u)
		}
		return VStrings(uuids)
	case "ManufacturerData":
		var items []Ptr
		for _, k := range btSortedKeys(a.ManufacturerData) {
			id, err := strconv.ParseUint(k, 10, 16)
			if err != nil {
				continue
			}
			items = append(items, VEntry(VU16(uint16(id)), VVariant(VByteArray(a.ManufacturerData[k]))))
		}
		return VArray("{qv}", items...)
	case "ServiceData":
		var items []Ptr
		for _, k := range btSortedKeys(a.ServiceData) {
			items = append(items, VEntry(VStr(spec.Short(k)), VVariant(VByteArray(a.ServiceData[k]))))
		}
		return VArray("{sv}", items...)
	case "LocalName":
		if a.LocalName == "" {
			return 0
		}
		return VStr(a.LocalName)
	case "Includes":
		var inc []string
		if a.IncludeTxPower {
			inc = append(inc, "tx-power")
		}
		return VStrings(inc)
	case "Discoverable":
		// A connectable advertisement is general discoverable, as the
		// other platforms' are (iOS's scanners want the flag); a
		// broadcast has no flags.
		if !a.Connectable {
			return 0
		}
		return VBool(true)
	}
	return 0
}

func (b *bluetooth) stopAdvertise(id uint32) {
	ad := b.adverts[id]
	if ad == nil {
		return
	}
	delete(b.adverts, id)
	if !ad.on {
		ad.stopped = true // unregistered once registered
		return
	}
	b.unregisterAdvert(ad)
}

func (b *bluetooth) unregisterAdvert(ad *btAdvert) {
	ad.on = false
	DBusCall(b.conn, bluezName, ad.adapter, ifAdvManager, "UnregisterAdvertisement", VTuple(VPath(ad.path)), btCallTimeout, func(_ Ptr, _ error) {
		DBusUnregister(b.conn, ad.reg)
	})
}

// endAdvert ends an advertisement the platform stopped.
func (b *bluetooth) endAdvert(id uint32, why string) {
	ad := b.adverts[id]
	if ad == nil {
		return
	}
	delete(b.adverts, id)
	DBusUnregister(b.conn, ad.reg)
	b.queue = append(b.queue, spec.Event{Type: spec.EventAdvertiseStopped, ID: id, Error: why})
}

// btServer is a GATT server of the app's: its application's objects.
type btServer struct {
	id      uint32
	root    string
	adapter string
	regs    []uint32
	objects []*btLocal // in order: services, each followed by its characteristics and theirs descriptors
	chars   map[uint32]*btLocal
	on      bool
	stopped bool
}

// btLocal is one of a server's objects.
type btLocal struct {
	path, iface string
	uuid        string
	primary     bool                      // a service's
	parent      string                    // a characteristic's service, a descriptor's characteristic
	char        *spec.LocalCharacteristic // a characteristic's
	handle      uint32                    // Go's
	value       []byte                    // a descriptor's (fixed); a characteristic's last notified
	flags       []string
	notifying   bool
}

func (b *bluetooth) serve(a spec.ServeArgs, reply func([]byte, error)) {
	adapter, ok := b.peripheralAdapter(ifGattManager, reply)
	if !ok {
		return
	}
	if old := b.servers[a.ID]; old != nil {
		b.unserve(a.ID)
	}
	s := &btServer{id: a.ID, root: fmt.Sprintf("%s/server%d", btBasePath, a.ID), adapter: adapter, chars: map[uint32]*btLocal{}}
	for i := range a.Services {
		svc := &a.Services[i]
		sp := fmt.Sprintf("%s/service%d", s.root, i)
		s.objects = append(s.objects, &btLocal{path: sp, iface: ifGattService, uuid: spec.Normalize(svc.UUID), primary: svc.Primary})
		for j := range svc.Characteristics {
			ch := &svc.Characteristics[j]
			cp := fmt.Sprintf("%s/char%d", sp, j)
			l := &btLocal{path: cp, iface: ifGattChar, uuid: spec.Normalize(ch.UUID), parent: sp, char: ch,
				handle: ch.Handle, flags: bluez.Flags(ch.Properties, ch.Permissions)}
			s.objects = append(s.objects, l)
			s.chars[ch.Handle] = l
			for k, d := range ch.Descriptors {
				flags := []string{"read"}
				if ch.Permissions&spec.PermReadEncrypted != 0 {
					flags = []string{"encrypt-read"}
				}
				s.objects = append(s.objects, &btLocal{path: fmt.Sprintf("%s/desc%d", cp, k), iface: ifGattDesc,
					uuid: spec.Normalize(d.UUID), parent: cp, value: d.Value, flags: flags})
			}
		}
	}
	fail := func(err error) {
		for _, r := range s.regs {
			DBusUnregister(b.conn, r)
		}
		btFail(reply, err.Error())
	}
	reg, err := DBusRegister(b.conn, s.root, btObjectsXML, ifObjectManager, DBusObject{
		Method: func(c DBusInvocation) {
			if c.Method != "GetManagedObjects" {
				c.Error("org.freedesktop.DBus.Error.UnknownMethod", "no such method")
				return
			}
			c.Return(VTuple(s.managedObjects()))
		},
	})
	if err != nil {
		fail(err)
		return
	}
	s.regs = append(s.regs, reg)
	for _, o := range s.objects {
		o := o
		reg, err := DBusRegister(b.conn, o.path, btObjectsXML, o.iface, DBusObject{
			Method:   func(c DBusInvocation) { b.localMethod(s, o, c) },
			Property: func(name string) Ptr { return o.property(name) },
		})
		if err != nil {
			fail(err)
			return
		}
		s.regs = append(s.regs, reg)
	}
	b.servers[a.ID] = s
	DBusCall(b.conn, bluezName, adapter, ifGattManager, "RegisterApplication", VTuple(VPath(s.root), VDict(nil)), btCallTimeout, func(_ Ptr, err error) {
		if err != nil {
			if b.servers[s.id] == s {
				delete(b.servers, s.id)
			}
			b.failRequests(s)
			_, msg := bluez.Error(err)
			why := bluez.ClientError(err, false)
			if msg != "" && why == msg {
				why = "BlueZ can't serve them: " + msg
			}
			fail(errors.New(why))
			return
		}
		s.on = true
		btOK(reply)
		if s.stopped || b.servers[s.id] != s {
			b.unregisterServer(s)
		}
	})
}

// managedObjects is GetManagedObjects' a{oa{sa{sv}}}: the objects with
// their properties, as BlueZ reads the application.
func (s *btServer) managedObjects() Ptr {
	items := make([]Ptr, 0, len(s.objects))
	for _, o := range s.objects {
		props := map[string]Ptr{}
		for _, name := range o.propertyNames() {
			if v := o.property(name); v != 0 {
				props[name] = v
			}
		}
		items = append(items, VEntry(VPath(o.path), VArray("{sa{sv}}", VEntry(VStr(o.iface), VDict(props)))))
	}
	return VArray("{oa{sa{sv}}}", items...)
}

func (o *btLocal) propertyNames() []string {
	switch o.iface {
	case ifGattService:
		return []string{"UUID", "Primary"}
	case ifGattChar:
		return []string{"UUID", "Service", "Flags"}
	}
	return []string{"UUID", "Characteristic", "Flags"}
}

func (o *btLocal) property(name string) Ptr {
	switch name {
	case "UUID":
		return VStr(o.uuid)
	case "Primary":
		if o.iface == ifGattService {
			return VBool(o.primary)
		}
	case "Service":
		if o.iface == ifGattChar {
			return VPath(o.parent)
		}
	case "Characteristic":
		if o.iface == ifGattDesc {
			return VPath(o.parent)
		}
	case "Flags":
		if o.iface != ifGattService {
			return VStrings(o.flags)
		}
	case "Value":
		if o.iface == ifGattChar {
			v := o.value
			if o.char.Value != nil && v == nil {
				v = o.char.Value
			}
			return VByteArray(v)
		}
	}
	return 0
}

// btRequest is a central's read or write waiting for "respond".
type btRequest struct {
	server uint32
	inv    DBusInvocation
	write  bool
}

// localMethod is BlueZ's call of one of the server's objects.
func (b *bluetooth) localMethod(s *btServer, o *btLocal, c DBusInvocation) {
	args, _ := VariantGoBytes(c.Params).([]any)
	opts := func(i int) map[string]any {
		if len(args) > i {
			m, _ := args[i].(map[string]any)
			return m
		}
		return nil
	}
	central := func(m map[string]any) *spec.Device {
		p, _ := m["device"].(string)
		if p == "" {
			d := btPseudoCentral
			return &d
		}
		d := b.device(p).Spec()
		if d.ID == "" {
			d.ID = bluez.AddressFromPath(p)
		}
		return &d
	}
	switch c.Method {
	case "ReadValue":
		m := opts(0)
		offset, _ := bluez.Int(m["offset"])
		var fixed []byte
		switch {
		case o.iface == ifGattDesc:
			fixed = o.value
			if fixed == nil {
				fixed = []byte{}
			}
		case o.char.Value != nil:
			fixed = o.char.Value
		}
		if fixed != nil {
			if offset > len(fixed) {
				c.Error(bluez.ServerError(0x07, false))
				return
			}
			c.Return(VTuple(VByteArray(fixed[offset:])))
			return
		}
		if b.servers[s.id] != s {
			c.Error(bluez.ServerError(0x0e, false))
			return
		}
		b.nextReq++
		b.requests[b.nextReq] = &btRequest{server: s.id, inv: c}
		b.queue = append(b.queue, spec.Event{Type: spec.EventRead, Server: s.id, Request: b.nextReq,
			Central: central(m), Handle: o.handle, Offset: offset})
	case "WriteValue":
		if o.iface == ifGattDesc {
			c.Error(bluez.ServerError(0x03, true)) // the descriptors' values are fixed
			return
		}
		var value []byte
		if len(args) > 0 {
			value = bluez.Bytes(args[0])
		}
		m := opts(1)
		offset, _ := bluez.Int(m["offset"])
		// BlueZ doesn't wait for the app's answer to a write of a
		// characteristic that takes writes without response (any write
		// of it): it said yes already.
		t, _ := m["type"].(string)
		response := o.char.Properties&spec.PropWriteNoResponse == 0 && t != "command"
		if b.servers[s.id] != s {
			c.Error(bluez.ServerError(0x0e, true))
			return
		}
		e := spec.Event{Type: spec.EventWrite, Server: s.id, Central: central(m), Handle: o.handle,
			Offset: offset, Value: value, Response: response}
		if response {
			b.nextReq++
			b.requests[b.nextReq] = &btRequest{server: s.id, inv: c, write: true}
			e.Request = b.nextReq
		} else {
			c.Return(0)
		}
		b.queue = append(b.queue, e)
	case "StartNotify", "StopNotify":
		c.Return(0)
		on := c.Method == "StartNotify"
		if o.notifying == on || b.servers[s.id] != s {
			return
		}
		o.notifying = on
		d := btPseudoCentral
		b.queue = append(b.queue, spec.Event{Type: spec.EventSubscription, Server: s.id, Central: &d, Handle: o.handle, On: on})
	case "Confirm":
		c.Return(0) // an indication acknowledged
	default:
		c.Error("org.freedesktop.DBus.Error.UnknownMethod", "no such method")
		return
	}
	b.flush()
}

func (b *bluetooth) respond(a spec.Respond) {
	r := b.requests[a.Request]
	if r == nil || r.server != a.Server {
		return
	}
	delete(b.requests, a.Request)
	if a.Error != 0 {
		r.inv.Error(bluez.ServerError(a.Error, r.write))
		return
	}
	if r.write {
		r.inv.Return(0)
		return
	}
	r.inv.Return(VTuple(VByteArray(a.Value)))
}

// notify sends the value to the characteristic's subscribers: BlueZ
// notifies (or indicates) a PropertiesChanged of its Value to every
// central subscribed; it can't to some of them only.
func (b *bluetooth) notify(a spec.Notify, reply func([]byte, error)) {
	s := b.servers[a.Server]
	if s == nil {
		btFail(reply, "not found")
		return
	}
	o := s.chars[a.Handle]
	if o == nil {
		btFail(reply, "not found")
		return
	}
	o.value = append([]byte{}, a.Value...)
	changed := VArray("{sv}", VEntry(VStr("Value"), VVariant(VByteArray(o.value))))
	if err := DBusEmit(b.conn, o.path, ifProperties, "PropertiesChanged", VTuple(VStr(ifGattChar), changed, VArray("s"))); err != nil {
		btFail(reply, err.Error())
		return
	}
	btOK(reply)
}

func (b *bluetooth) unserve(id uint32) {
	s := b.servers[id]
	if s == nil {
		return
	}
	delete(b.servers, id)
	b.failRequests(s)
	if !s.on {
		s.stopped = true
		return
	}
	b.unregisterServer(s)
}

func (b *bluetooth) unregisterServer(s *btServer) {
	s.on = false
	DBusCall(b.conn, bluezName, s.adapter, ifGattManager, "UnregisterApplication", VTuple(VPath(s.root)), btCallTimeout, func(_ Ptr, _ error) {
		for _, r := range s.regs {
			DBusUnregister(b.conn, r)
		}
		s.regs = nil
	})
}

// endServer ends a server the platform stopped (BlueZ went).
func (b *bluetooth) endServer(id uint32, why string) {
	s := b.servers[id]
	if s == nil {
		return
	}
	delete(b.servers, id)
	b.failRequests(s)
	for _, r := range s.regs {
		DBusUnregister(b.conn, r)
	}
	s.regs = nil
	b.queue = append(b.queue, spec.Event{Type: spec.EventServerStopped, Server: id, Error: why})
}

// failRequests answers the server's requests waiting: it's gone.
func (b *bluetooth) failRequests(s *btServer) {
	for _, id := range btKeys(b.requests) {
		if r := b.requests[id]; r.server == s.id {
			delete(b.requests, id)
			r.inv.Error(bluez.ServerError(0x0e, r.write))
		}
	}
}

func btSortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}
