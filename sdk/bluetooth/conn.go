package bluetooth

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// ConnectionState is where a connection is.
type ConnectionState uint8

const (
	Connecting ConnectionState = iota
	Connected
	Disconnected
)

func (s ConnectionState) String() string {
	return [...]string{"connecting", "connected", "disconnected", "?"}[min(int(s), 3)]
}

// ConnectOptions are how to connect.
type ConnectOptions struct {
	// AutoConnect waits for the device to come in range, however long it
	// takes, as a background reconnection does (Android's autoConnect,
	// slower to connect; iOS and macOS always wait). Without it, Android
	// gives up after about 30 seconds.
	AutoConnect bool
	// Timeout gives up connecting after it (ErrTimeout): 0 waits as the
	// platform does.
	Timeout time.Duration
}

// DisconnectReason is why a connection ended.
type DisconnectReason uint8

const (
	ReasonUnknown DisconnectReason = iota
	// ReasonRemote: the device ended it (it was turned off, or asked).
	ReasonRemote
	// ReasonTimeout: its signal was lost (out of range).
	ReasonTimeout
	// ReasonFailed: the link failed (to be made, or later).
	ReasonFailed
	// ReasonOff: Bluetooth was turned off.
	ReasonOff
	// ReasonLocal: this device ended it (the system did).
	ReasonLocal
	// ReasonClosed: the app closed it (Close, its owner's disposal).
	ReasonClosed
)

func (r DisconnectReason) String() string {
	return [...]string{"unknown", "the device disconnected", "the connection timed out", "the connection failed",
		"Bluetooth turned off", "this device disconnected", "closed", "?"}[min(int(r), 7)]
}

func reason(s string) DisconnectReason {
	switch s {
	case spec.ReasonRemote:
		return ReasonRemote
	case spec.ReasonTimeout:
		return ReasonTimeout
	case spec.ReasonFailed:
		return ReasonFailed
	case spec.ReasonOff:
		return ReasonOff
	case spec.ReasonLocal:
		return ReasonLocal
	}
	return ReasonUnknown
}

// DisconnectError is why a connection ended; errors.Is(err,
// ErrDisconnected) holds for it.
type DisconnectError struct {
	Reason DisconnectReason
	// Code is the platform's status (the HCI reason, Core Bluetooth's
	// error code, Android's GATT status), 0 if none.
	Code int
	// Message is the platform's words, if any.
	Message string
}

func (e *DisconnectError) Error() string {
	s := "bluetooth: disconnected: " + e.Reason.String()
	if e.Message != "" {
		s += " (" + e.Message + ")"
	}
	return s
}

func (e *DisconnectError) Is(target error) bool { return target == ErrDisconnected }

// ATTError is an Attribute Protocol error code: what a device answers a
// request it refuses with, or what the app's server answers (its
// OnRead's and OnWrite's error).
type ATTError uint8

// The codes (Core Specification, Vol 3, Part F, 3.4.1.1).
const (
	InvalidHandle                 ATTError = 0x01
	ReadNotPermitted              ATTError = 0x02
	WriteNotPermitted             ATTError = 0x03
	InvalidPDU                    ATTError = 0x04
	InsufficientAuthentication    ATTError = 0x05
	RequestNotSupported           ATTError = 0x06
	InvalidOffset                 ATTError = 0x07
	InsufficientAuthorization     ATTError = 0x08
	PrepareQueueFull              ATTError = 0x09
	AttributeNotFound             ATTError = 0x0a
	AttributeNotLong              ATTError = 0x0b
	InsufficientEncryptionKeySize ATTError = 0x0c
	InvalidAttributeValueLength   ATTError = 0x0d
	UnlikelyError                 ATTError = 0x0e
	InsufficientEncryption        ATTError = 0x0f
	UnsupportedGroupType          ATTError = 0x10
	InsufficientResources         ATTError = 0x11
)

func (e ATTError) Error() string {
	names := map[ATTError]string{
		InvalidHandle: "invalid handle", ReadNotPermitted: "read not permitted", WriteNotPermitted: "write not permitted",
		InvalidPDU: "invalid PDU", InsufficientAuthentication: "insufficient authentication",
		RequestNotSupported: "request not supported", InvalidOffset: "invalid offset",
		InsufficientAuthorization: "insufficient authorization", PrepareQueueFull: "prepare queue full",
		AttributeNotFound: "attribute not found", AttributeNotLong: "attribute not long",
		InsufficientEncryptionKeySize: "insufficient encryption key size",
		InvalidAttributeValueLength:   "invalid attribute value length", UnlikelyError: "unlikely error",
		InsufficientEncryption: "insufficient encryption", UnsupportedGroupType: "unsupported group type",
		InsufficientResources: "insufficient resources",
	}
	if n, ok := names[e]; ok {
		return "bluetooth: " + n
	}
	return fmt.Sprintf("bluetooth: ATT error 0x%02x", uint8(e))
}

// ATTRequestError is a device's refusal of a request: errors.As gives
// its Code, and errors.Is(err, ReadNotPermitted) holds for its code.
type ATTRequestError struct {
	Code    ATTError
	Message string // the platform's words
}

func (e *ATTRequestError) Error() string {
	if e.Message != "" {
		return e.Code.Error() + " (" + e.Message + ")"
	}
	return e.Code.Error()
}

func (e *ATTRequestError) Is(target error) bool { t, ok := target.(ATTError); return ok && t == e.Code }

// Connection is a connection to a device: Connect's. Its methods that
// wait for the device (Services, Read...) aren't on the main thread;
// they wait for it to connect first, and fail with its error if it
// doesn't.
type Connection struct {
	id     uint32
	device Device
	state  *signal.Signal[ConnectionState]
	timer  *time.Timer
	// subs are the subscriptions, by characteristic handle (main thread).
	subs map[uint32]*subscription

	mu       sync.Mutex
	ready    chan struct{} // closed once connected or done
	done     chan struct{} // closed once disconnected
	err      error         // why it's done (or never was)
	isUp     bool
	services []Service // nil until discovered (again)
}

type subscription struct {
	fs      map[*func([]byte, error)]bool
	pending bool // the host hasn't answered "subscribe" yet
}

// Connect connects to the device and keeps the connection until Close,
// the current owner's disposal, or the device's going (State says so,
// Err says why). Main thread.
//
// Connect to a Device a scan heard, Choose gave, Known or ConnectedDevices
// found. A connection lost isn't made again: Connect again (with
// AutoConnect, to wait for the device to come back).
func Connect(d Device, o ConnectOptions) *Connection {
	c := &Connection{id: newID(), device: d, state: signal.New(Connecting),
		subs: map[uint32]*subscription{}, ready: make(chan struct{}), done: make(chan struct{})}
	conns[c.id] = c
	retain()
	send("connect", spec.ConnectArgs{Conn: c.id, Device: d.ID, AutoConnect: o.AutoConnect}, func(_ []byte, err error) {
		if err != nil {
			c.finish(err, false)
			return
		}
		c.mu.Lock()
		up := !c.isDone()
		if up {
			c.isUp = true
			close(c.ready)
		}
		c.mu.Unlock()
		if up {
			c.stopTimer()
			c.state.Set(Connected)
		}
	})
	if o.Timeout > 0 {
		c.timer = time.AfterFunc(o.Timeout, func() {
			ui.Post(func() {
				c.mu.Lock()
				connecting := !c.isUp && !c.isDone()
				c.mu.Unlock()
				if connecting {
					send("disconnect", spec.Conn{Conn: c.id}, nil)
					c.finish(ErrTimeout, false)
				}
			})
		})
	}
	signal.OnCleanup(c.Close)
	return c
}

func (c *Connection) isDone() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}

func (c *Connection) stopTimer() {
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}

// finish ends the connection with err (main thread). notify: tell the
// subscriptions.
func (c *Connection) finish(err error, notify bool) {
	c.mu.Lock()
	if c.isDone() {
		c.mu.Unlock()
		return
	}
	c.err = err
	if !c.isUp {
		close(c.ready)
	}
	c.isUp = false
	close(c.done)
	c.mu.Unlock()
	c.stopTimer()
	delete(conns, c.id)
	release()
	c.state.Set(Disconnected)
	subs := c.subs
	c.subs = map[uint32]*subscription{}
	if notify {
		for _, s := range subs {
			for _, f := range keys(s.fs) {
				(*f)(nil, err)
			}
		}
	}
}

func (c *Connection) disconnected(e *DisconnectError) { c.finish(e, true) }

// Device is the device it's to.
func (c *Connection) Device() Device { return c.device }

// State is where it is: Connecting, Connected, Disconnected. Main thread.
func (c *Connection) State() signal.Accessor[ConnectionState] { return c.state }

// Wait waits until it's connected, and returns nil, or the error it
// never was for. Not on the main thread.
func (c *Connection) Wait() error {
	<-c.ready
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.isUp {
		return nil
	}
	return c.err
}

// Done is closed once the connection's over (Err says why). Any goroutine.
func (c *Connection) Done() <-chan struct{} { return c.done }

// Err is why the connection's over: a *DisconnectError (the device went;
// ReasonClosed: the app closed it), ErrTimeout, ErrCanceled, or the
// error it couldn't connect with. nil while it's connecting or
// connected. Any goroutine.
func (c *Connection) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Close disconnects (or stops connecting). Its subscriptions end, and
// the calls waiting on it fail. Main thread.
func (c *Connection) Close() {
	c.mu.Lock()
	done := c.isDone()
	c.mu.Unlock()
	if done {
		return
	}
	send("disconnect", spec.Conn{Conn: c.id}, nil)
	c.finish(&DisconnectError{Reason: ReasonClosed}, false)
}

// up waits for the connection, for a call.
func (c *Connection) up() error {
	if err := c.Wait(); err != nil {
		return err
	}
	return nil
}

// call is a blocking call on the connection: its error the connection's
// if it went meanwhile.
func (c *Connection) call(method string, args, out any) error {
	if err := c.up(); err != nil {
		return err
	}
	err := callJSON(method, args, out)
	if errors.Is(err, ErrDisconnected) {
		<-c.done
		return c.Err()
	}
	return err
}

func (c *Connection) callRaw(method string, args any) ([]byte, error) {
	if err := c.up(); err != nil {
		return nil, err
	}
	b, err := call(method, args)
	if errors.Is(err, ErrDisconnected) {
		<-c.done
		return nil, c.Err()
	}
	return b, err
}

// Properties are what a characteristic does.
type Properties uint8

const (
	Broadcast             Properties = spec.PropBroadcast
	Read                  Properties = spec.PropRead
	WriteWithoutResponse  Properties = spec.PropWriteNoResponse
	Write                 Properties = spec.PropWrite
	Notify                Properties = spec.PropNotify
	Indicate              Properties = spec.PropIndicate
	SignedWrite           Properties = spec.PropSignedWrite
	HasExtendedProperties Properties = spec.PropExtendedProperties
)

func (p Properties) String() string {
	names := []string{"broadcast", "read", "write without response", "write", "notify", "indicate", "signed write", "extended properties"}
	var out []string
	for i, n := range names {
		if p&(1<<i) != 0 {
			out = append(out, n)
		}
	}
	return strings.Join(out, ", ")
}

// Service is a device's service.
type Service struct {
	UUID UUID
	// Primary: a primary service (not one only others include).
	Primary         bool
	Characteristics []Characteristic
}

// Characteristic finds the service's characteristic u.
func (s Service) Characteristic(u UUID) (Characteristic, bool) {
	for _, ch := range s.Characteristics {
		if ch.UUID == u {
			return ch, true
		}
	}
	return Characteristic{}, false
}

// Characteristic is a value of a device's service: what the app reads,
// writes and subscribes to.
type Characteristic struct {
	UUID        UUID
	Service     UUID
	Properties  Properties
	Descriptors []Descriptor
	conn        *Connection
	handle      uint32
}

// Descriptor is a characteristic's descriptor: its description, its
// format. (The Client Characteristic Configuration descriptor is
// Subscribe's to write.)
type Descriptor struct {
	UUID   UUID
	conn   *Connection
	handle uint32
}

// Services are the device's services, with their characteristics and
// descriptors, discovered the first time (and after the device says
// they changed). Not on the main thread.
func (c *Connection) Services() ([]Service, error) {
	c.mu.Lock()
	ss := c.services
	c.mu.Unlock()
	if ss != nil {
		return ss, nil
	}
	var w []spec.Service
	if err := c.call("services", spec.Conn{Conn: c.id}, &w); err != nil {
		return nil, err
	}
	ss = make([]Service, 0, len(w))
	for _, s := range w {
		su, _ := ParseUUID(s.UUID)
		svc := Service{UUID: su, Primary: s.Primary}
		for _, ch := range s.Characteristics {
			cu, _ := ParseUUID(ch.UUID)
			x := Characteristic{UUID: cu, Service: su, Properties: Properties(ch.Properties), conn: c, handle: ch.Handle}
			for _, d := range ch.Descriptors {
				du, _ := ParseUUID(d.UUID)
				x.Descriptors = append(x.Descriptors, Descriptor{UUID: du, conn: c, handle: d.Handle})
			}
			svc.Characteristics = append(svc.Characteristics, x)
		}
		ss = append(ss, svc)
	}
	c.mu.Lock()
	c.services = ss
	c.mu.Unlock()
	return ss, nil
}

// Service finds the device's service u: ErrNotFound if it hasn't it. Not
// on the main thread.
func (c *Connection) Service(u UUID) (Service, error) {
	ss, err := c.Services()
	if err != nil {
		return Service{}, err
	}
	for _, s := range ss {
		if s.UUID == u {
			return s, nil
		}
	}
	return Service{}, fmt.Errorf("%w: service %v", ErrNotFound, u)
}

// Characteristic finds the characteristic char of the device's service:
// ErrNotFound if it hasn't it. Not on the main thread.
func (c *Connection) Characteristic(service, char UUID) (Characteristic, error) {
	s, err := c.Service(service)
	if err != nil {
		return Characteristic{}, err
	}
	if ch, ok := s.Characteristic(char); ok {
		return ch, nil
	}
	return Characteristic{}, fmt.Errorf("%w: characteristic %v of %v", ErrNotFound, char, service)
}

// MTU is a connection's ATT MTU, and the longest values it writes.
type MTU struct {
	// MTU is the ATT MTU, in bytes (23 at first; most phones then agree
	// on 185 to 517).
	MTU int
	// WriteWithResponse and WriteWithoutResponse are the longest values a
	// write takes (iOS's maximumWriteValueLength; elsewhere MTU - 3). A
	// longer write with response goes in parts (a long write), where the
	// device takes that.
	WriteWithResponse, WriteWithoutResponse int
}

// MTU is the connection's MTU now. Not on the main thread.
func (c *Connection) MTU() (MTU, error) {
	var m spec.MTU
	err := c.call("mtu", spec.Conn{Conn: c.id}, &m)
	return MTU(m), err
}

// RequestMTU asks for an MTU of n bytes (up to 517) and returns the one
// agreed: Android asks the device; elsewhere the system has agreed on
// one already, which it returns. Not on the main thread.
func (c *Connection) RequestMTU(n int) (MTU, error) {
	var m spec.MTU
	err := c.call("requestMtu", spec.RequestMTU{Conn: c.id, MTU: min(max(n, 23), 517)}, &m)
	return MTU(m), err
}

// RSSI reads the connection's signal strength, in dBm. Not on the main
// thread.
func (c *Connection) RSSI() (int, error) {
	var r spec.RSSI
	err := c.call("rssi", spec.Conn{Conn: c.id}, &r)
	return r.RSSI, err
}

// Priority is a connection's interval, Android's: how fast it exchanges
// data against its battery.
type Priority uint8

const (
	PriorityBalanced Priority = iota
	// PriorityHigh: a short interval, for a transfer (then Balanced again).
	PriorityHigh
	PriorityLowPower
)

// SetPriority asks for a connection priority (Android's
// requestConnectionPriority); elsewhere the system decides, and it does
// nothing. Not on the main thread.
func (c *Connection) SetPriority(p Priority) error {
	names := [...]string{spec.PriorityBalanced, spec.PriorityHigh, spec.PriorityLowPower}
	return c.call("priority", spec.Priority{Conn: c.id, Priority: names[min(int(p), 2)]}, nil)
}

// Read reads the characteristic's value. Not on the main thread.
func (ch Characteristic) Read() ([]byte, error) {
	if ch.conn == nil {
		return nil, ErrNotFound
	}
	return ch.conn.callRaw("read", spec.Attr{Conn: ch.conn.id, Handle: ch.handle})
}

// Write writes the value, with a response: it returns once the device
// took it, or refused (an *ATTRequestError). Not on the main thread.
func (ch Characteristic) Write(v []byte) error { return ch.write(v, true) }

// WriteWithoutResponse writes the value without the device's answer: it
// returns once the platform sent it (waiting for room, so a stream of
// them goes as fast as the link does). Not on the main thread.
func (ch Characteristic) WriteWithoutResponse(v []byte) error { return ch.write(v, false) }

func (ch Characteristic) write(v []byte, response bool) error {
	if ch.conn == nil {
		return ErrNotFound
	}
	return ch.conn.call("write", spec.Write{Conn: ch.conn.id, Handle: ch.handle, Value: v, Response: response}, nil)
}

// Descriptor finds the characteristic's descriptor u.
func (ch Characteristic) Descriptor(u UUID) (Descriptor, bool) {
	for _, d := range ch.Descriptors {
		if d.UUID == u {
			return d, true
		}
	}
	return Descriptor{}, false
}

// Read reads the descriptor's value. Not on the main thread.
func (d Descriptor) Read() ([]byte, error) {
	if d.conn == nil {
		return nil, ErrNotFound
	}
	return d.conn.callRaw("readDescriptor", spec.Attr{Conn: d.conn.id, Handle: d.handle})
}

// Write writes the descriptor's value. Not on the main thread.
func (d Descriptor) Write(v []byte) error {
	if d.conn == nil {
		return ErrNotFound
	}
	return d.conn.call("writeDescriptor", spec.Write{Conn: d.conn.id, Handle: d.handle, Value: v, Response: true}, nil)
}

// SubscribeOptions are how to subscribe.
type SubscribeOptions struct {
	// Indicate asks for indications (each acknowledged) where the
	// characteristic has both; else it's whichever it has.
	Indicate bool
}

// Subscribe turns the characteristic's notifications (or indications) on
// and calls f, on the main thread, with each value it sends, until stop,
// the current owner's disposal or the connection's end. If it can't
// subscribe, or the connection ends, f gets the error once (nil value),
// and nothing after. Several subscriptions to one characteristic share
// its notifications. Main thread.
func (ch Characteristic) Subscribe(f func(value []byte, err error), o ...SubscribeOptions) (stop func()) {
	c := ch.conn
	fp := &f
	if c == nil || c.isDone() {
		err := error(ErrNotFound)
		if c != nil {
			err = c.Err()
		}
		f(nil, err)
		return func() {}
	}
	s := c.subs[ch.handle]
	if s == nil {
		s = &subscription{fs: map[*func([]byte, error)]bool{}, pending: true}
		c.subs[ch.handle] = s
		indicate := len(o) > 0 && o[0].Indicate
		go func() {
			err := c.up()
			ui.Post(func() {
				if c.subs[ch.handle] != s {
					return // stopped meanwhile
				}
				if err != nil {
					return // finish told them
				}
				send("subscribe", spec.Subscribe{Conn: c.id, Handle: ch.handle, Indicate: indicate}, func(_ []byte, err error) {
					s.pending = false
					if err == nil || c.subs[ch.handle] != s {
						return
					}
					delete(c.subs, ch.handle)
					for _, f := range keys(s.fs) {
						(*f)(nil, err)
					}
				})
			})
		}()
	}
	s.fs[fp] = true
	stop = once(func() {
		if c.subs[ch.handle] != s || !s.fs[fp] {
			return
		}
		delete(s.fs, fp)
		if len(s.fs) == 0 {
			delete(c.subs, ch.handle)
			if !c.isDone() {
				send("unsubscribe", spec.Attr{Conn: c.id, Handle: ch.handle}, nil)
			}
		}
	})
	signal.OnCleanup(stop)
	return stop
}

// value delivers a notification (main thread).
func (c *Connection) value(handle uint32, v []byte) {
	s := c.subs[handle]
	if s == nil {
		return
	}
	for _, f := range keys(s.fs) {
		if s.fs[f] {
			(*f)(v, nil)
		}
	}
}
