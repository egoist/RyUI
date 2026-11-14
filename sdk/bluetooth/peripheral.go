package bluetooth

import (
	"errors"
	"strconv"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	"github.com/egoist/asuka/signal"
)

// AdvertiseMode is how often Android advertises: its battery against how
// soon centrals hear it. The other platforms decide themselves.
type AdvertiseMode uint8

const (
	AdvertiseBalanced AdvertiseMode = iota
	AdvertiseLowPower
	AdvertiseLowLatency
)

// AdvertiseOptions are what the app advertises, as a peripheral.
type AdvertiseOptions struct {
	// LocalName is the name it advertises (none: nothing; an advertisement
	// has 31 bytes, and the name may not fit with the rest).
	LocalName string
	// Services are the services it advertises (those of its Server, so
	// centrals find it).
	Services []UUID
	// ServiceData and ManufacturerData, as Advertisement's: Android's,
	// Windows' and Linux's; iOS and macOS advertise only the name and
	// the services.
	ServiceData      map[UUID][]byte
	ManufacturerData map[uint16][]byte
	// NotConnectable: a beacon, which centrals don't connect to (iOS and
	// macOS: always connectable).
	NotConnectable bool
	// IncludeTxPower adds the transmit power (Android, Linux).
	IncludeTxPower bool
	// Mode is Android's (AdvertiseMode).
	Mode AdvertiseMode
}

// Advertise advertises the app as a peripheral, until stop or the
// current owner's disposal. If it can't (ErrNotAvailable on the web,
// which has no peripheral role; too much data; ErrOff), or the platform
// stops it, stopped gets the error, once. Main thread.
func Advertise(o AdvertiseOptions, stopped func(error)) (stop func()) {
	id := newID()
	if stopped == nil {
		stopped = func(error) {}
	}
	adverts[id] = stopped
	retain()
	w := spec.AdvertiseArgs{ID: id, LocalName: o.LocalName, Services: uuids(o.Services),
		Connectable: !o.NotConnectable, IncludeTxPower: o.IncludeTxPower,
		Mode: [...]string{spec.ModeBalanced, spec.ModeLowPower, spec.ModeLowLatency}[min(int(o.Mode), 2)]}
	if len(o.ServiceData) > 0 {
		w.ServiceData = map[string][]byte{}
		for k, v := range o.ServiceData {
			w.ServiceData[k.String()] = v
		}
	}
	if len(o.ManufacturerData) > 0 {
		w.ManufacturerData = map[string][]byte{}
		for k, v := range o.ManufacturerData {
			w.ManufacturerData[strconv.Itoa(int(k))] = v
		}
	}
	end := func(err error) {
		if f, ok := adverts[id]; ok {
			delete(adverts, id)
			release()
			if err != nil {
				f(err)
			}
		}
	}
	send("advertise", w, func(_ []byte, err error) {
		if err != nil {
			end(err)
		}
	})
	stop = once(func() {
		if _, ok := adverts[id]; ok {
			send("stopAdvertise", spec.ID{ID: id}, nil)
			end(nil)
		}
	})
	signal.OnCleanup(stop)
	return stop
}

// Permissions are what a centrals may do with a local characteristic's
// value.
type Permissions uint8

const (
	Readable Permissions = spec.PermRead
	Writable Permissions = spec.PermWrite
	// ReadEncrypted and WriteEncrypted need the link encrypted: the
	// central pairs first (the system asks the user).
	ReadEncrypted  Permissions = spec.PermReadEncrypted
	WriteEncrypted Permissions = spec.PermWriteEncrypted
)

// ServerRequest is a central's read or write of a local characteristic.
type ServerRequest struct {
	// Central is the device asking.
	Central Device
	// Offset is where in the value a write starts (most start at 0).
	Offset int
}

// LocalService is a service the app serves, as a peripheral.
type LocalService struct {
	UUID UUID
	// Secondary: a service only others include (rare).
	Secondary       bool
	Characteristics []*LocalCharacteristic
}

// LocalCharacteristic is one of a local service's values.
type LocalCharacteristic struct {
	UUID UUID
	// Properties: Read, Write, WriteWithoutResponse, Notify, Indicate.
	Properties Properties
	// Permissions: Readable, Writable, or their encrypted kinds.
	Permissions Permissions
	// Value is a fixed value, which the platform answers reads with by
	// itself (iOS wants the characteristic read-only then): a name, a
	// version. Without one, OnRead answers.
	Value []byte
	// OnRead answers a read with the value (all of it: the platform reads
	// a long one in parts, each from an offset), or an error: an ATTError
	// to answer with, else UnlikelyError. Nil: ReadNotPermitted. Main
	// thread.
	OnRead func(r ServerRequest) ([]byte, error)
	// OnWrite takes a write, or refuses it with an error, as OnRead does.
	// A long write comes whole on iOS, macOS and Android; on Windows and
	// Linux, a part at a time, each at its ServerRequest.Offset. Nil:
	// WriteNotPermitted. Main thread.
	OnWrite func(r ServerRequest, value []byte) error
	// OnSubscribe says a central subscribed to its notifications or
	// indications (on), or no longer does. Main thread.
	OnSubscribe func(central Device, on bool)
	// Descriptors are its descriptors, fixed values (UserDescription...);
	// the platform has the CCCD itself.
	Descriptors []LocalDescriptor

	server      *Server
	handle      uint32
	subscribers []Device
}

// LocalDescriptor is a local characteristic's descriptor, with its value.
type LocalDescriptor struct {
	UUID  UUID
	Value []byte
}

// Server is the app's GATT server: its services, which centrals that
// connect to it read and write (advertise them: Advertise).
type Server struct {
	id     uint32
	chars  map[uint32]*LocalCharacteristic
	ended  bool
	failed func(error)
}

// Serve serves the services, as a peripheral, until Close or the current
// owner's disposal. If the platform can't (ErrNotAvailable on the web;
// ErrOff), or stops serving, stopped gets the error, once. Main thread.
func Serve(services []*LocalService, stopped func(error)) *Server {
	s := &Server{id: newID(), chars: map[uint32]*LocalCharacteristic{}, failed: stopped}
	w := spec.ServeArgs{ID: s.id}
	var handle uint32
	for _, svc := range services {
		ws := spec.LocalService{UUID: svc.UUID.String(), Primary: !svc.Secondary}
		for _, ch := range svc.Characteristics {
			handle++
			ch.server, ch.handle, ch.subscribers = s, handle, nil
			s.chars[handle] = ch
			wc := spec.LocalCharacteristic{Handle: handle, UUID: ch.UUID.String(), Properties: int(ch.Properties),
				Permissions: int(ch.Permissions), Value: ch.Value}
			for _, d := range ch.Descriptors {
				wc.Descriptors = append(wc.Descriptors, spec.LocalDescriptor{UUID: d.UUID.String(), Value: d.Value})
			}
			ws.Characteristics = append(ws.Characteristics, wc)
		}
		w.Services = append(w.Services, ws)
	}
	servers[s.id] = s
	retain()
	send("serve", w, func(_ []byte, err error) {
		if err != nil {
			s.end(err)
		}
	})
	signal.OnCleanup(s.Close)
	return s
}

// end ends the server with err, telling the app (nil: the app closed it).
func (s *Server) end(err error) {
	if s.ended {
		return
	}
	s.ended = true
	delete(servers, s.id)
	release()
	if err != nil && s.failed != nil {
		s.failed(err)
	}
}

// Close stops serving: its services go. Main thread.
func (s *Server) Close() {
	if !s.ended {
		send("unserve", spec.ID{ID: s.id}, nil)
		s.end(nil)
	}
}

// attCode is err as an ATT error code.
func attCode(err error) int {
	var a ATTError
	if errors.As(err, &a) {
		return int(a)
	}
	var r *ATTRequestError
	if errors.As(err, &r) {
		return int(r.Code)
	}
	return int(UnlikelyError)
}

func (s *Server) event(e *spec.Event) {
	ch := s.chars[e.Handle]
	var central Device
	if e.Central != nil {
		central = Device(*e.Central)
	}
	respond := func(v []byte, code int) {
		send("respond", spec.Respond{Server: s.id, Request: e.Request, Value: v, Error: code}, nil)
	}
	switch e.Type {
	case spec.EventRead:
		switch {
		case ch == nil:
			respond(nil, int(InvalidHandle))
		case ch.OnRead == nil && ch.Value == nil:
			respond(nil, int(ReadNotPermitted))
		default:
			v, err := ch.Value, error(nil)
			if ch.OnRead != nil {
				v, err = ch.OnRead(ServerRequest{Central: central, Offset: e.Offset})
			}
			switch {
			case err != nil:
				respond(nil, attCode(err))
			case e.Offset > len(v):
				respond(nil, int(InvalidOffset))
			default:
				respond(v[e.Offset:], 0)
			}
		}
	case spec.EventWrite:
		code := 0
		switch {
		case ch == nil:
			code = int(InvalidHandle)
		case ch.OnWrite == nil:
			code = int(WriteNotPermitted)
		default:
			if err := ch.OnWrite(ServerRequest{Central: central, Offset: e.Offset}, e.Value); err != nil {
				code = attCode(err)
			}
		}
		if e.Response {
			respond(nil, code)
		}
	case spec.EventSubscription:
		if ch == nil {
			return
		}
		i := -1
		for j, d := range ch.subscribers {
			if d.ID == central.ID {
				i = j
			}
		}
		switch {
		case e.On && i < 0:
			ch.subscribers = append(ch.subscribers, central)
		case !e.On && i >= 0:
			ch.subscribers = append(ch.subscribers[:i:i], ch.subscribers[i+1:]...)
		default:
			return
		}
		if ch.OnSubscribe != nil {
			ch.OnSubscribe(central, e.On)
		}
	}
}

// Subscribers are the centrals subscribed to the characteristic's
// notifications now. Main thread.
func (ch *LocalCharacteristic) Subscribers() []Device {
	return append([]Device(nil), ch.subscribers...)
}

// Notify sends the characteristic's new value to its subscribers (as a
// notification, or an indication if it has Indicate and not Notify), or
// to those of them named: the platform queues it while the link is
// busy. Its value for reads is OnRead's still. Main thread.
func (ch *LocalCharacteristic) Notify(value []byte, to ...Device) {
	if ch.server == nil || ch.server.ended {
		return
	}
	w := spec.Notify{Server: ch.server.id, Handle: ch.handle, Value: value}
	for _, d := range to {
		w.Centrals = append(w.Centrals, d.ID)
	}
	send("notify", w, nil)
}
