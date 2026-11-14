// Package usbmux talks to usbmuxd, the Mac's daemon that reaches the
// iPhones plugged in over USB (and those it knows on the network): it
// lists them, and opens a TCP connection to a port on one, as Xcode,
// Finder and libimobiledevice's iproxy do. asuka dev reaches the player
// on an iPhone this way (package devtunnel).
//
// The protocol is usbmuxd's plist one: a 16-byte header (length,
// version 1, message 8, tag; little-endian) and an XML property list.
// Once a Connect succeeds, the socket carries the device's port.
package usbmux

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

// Socket is usbmuxd's socket.
const Socket = "/var/run/usbmuxd"

// A Device is one usbmuxd reaches.
type Device struct {
	ID   int    // usbmuxd's, for Dial (it changes as the device comes and goes)
	UDID string // the device's
	// Connection is "USB" or "Network".
	Connection string
}

// Client reaches usbmuxd at Path (Socket if empty).
type Client struct {
	Path string
	// Timeout bounds each request (default 5s); a connection, once made,
	// has none.
	Timeout time.Duration
}

var tag atomic.Uint32

func (c *Client) open() (net.Conn, error) {
	path := c.Path
	if path == "" {
		path = Socket
	}
	conn, err := net.DialTimeout("unix", path, c.timeout())
	if err != nil {
		return nil, fmt.Errorf("usbmuxd: %w", err)
	}
	return conn, nil
}

func (c *Client) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 5 * time.Second
}

// request sends one message and reads its answer.
func (c *Client) request(conn net.Conn, msg map[string]any) (map[string]any, error) {
	msg["ClientVersionString"] = "asuka"
	msg["ProgName"] = "asuka"
	msg["kLibUSBMuxVersion"] = 3
	conn.SetDeadline(time.Now().Add(c.timeout()))
	defer conn.SetDeadline(time.Time{})
	if err := writeMessage(conn, tag.Add(1), msg); err != nil {
		return nil, fmt.Errorf("usbmuxd: %w", err)
	}
	reply, err := readMessage(conn)
	if err != nil {
		return nil, fmt.Errorf("usbmuxd: %w", err)
	}
	return reply, nil
}

// Devices lists the devices usbmuxd reaches.
func (c *Client) Devices() ([]Device, error) {
	conn, err := c.open()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	reply, err := c.request(conn, map[string]any{"MessageType": "ListDevices"})
	if err != nil {
		return nil, err
	}
	list, _ := reply["DeviceList"].([]any)
	var out []Device
	for _, e := range list {
		m, _ := e.(map[string]any)
		props, _ := m["Properties"].(map[string]any)
		if props == nil {
			continue
		}
		d := Device{ID: asInt(props["DeviceID"]), Connection: asString(props["ConnectionType"])}
		d.UDID = asString(props["SerialNumber"])
		if u := asString(props["UDID"]); u != "" {
			d.UDID = u
		}
		if d.ID == 0 {
			d.ID = asInt(m["DeviceID"])
		}
		out = append(out, d)
	}
	return out, nil
}

// SameUDID reports whether two UDIDs name the same device: usbmuxd may
// give a newer device's without the dash devicectl shows.
func SameUDID(a, b string) bool {
	norm := func(s string) string { return strings.ToUpper(strings.ReplaceAll(s, "-", "")) }
	return a != "" && norm(a) == norm(b)
}

// Find returns the device udid names, preferring its USB connection to
// its network one; ok is false if usbmuxd doesn't reach it.
func (c *Client) Find(udid string) (d Device, ok bool, err error) {
	devs, err := c.Devices()
	if err != nil {
		return Device{}, false, err
	}
	for _, x := range devs {
		if !SameUDID(x.UDID, udid) {
			continue
		}
		if !ok || x.Connection == "USB" {
			d, ok = x, true
		}
	}
	return d, ok, nil
}

// ErrRefused is Dial's error when nothing listens on the device's port.
var ErrRefused = errors.New("usbmuxd: connection refused by the device")

// Dial connects to port on the device usbmuxd knows as id.
func (c *Client) Dial(id, port int) (net.Conn, error) {
	conn, err := c.open()
	if err != nil {
		return nil, err
	}
	// The port, in network byte order, as an integer.
	p := int(uint16(port)>>8 | uint16(port)<<8)
	reply, err := c.request(conn, map[string]any{"MessageType": "Connect", "DeviceID": id, "PortNumber": p})
	if err != nil {
		conn.Close()
		return nil, err
	}
	switch n := asInt(reply["Number"]); n {
	case 0:
		return conn, nil
	case 3:
		conn.Close()
		return nil, ErrRefused
	default:
		conn.Close()
		return nil, fmt.Errorf("usbmuxd: connect failed (%d)", n)
	}
}

func asInt(v any) int {
	switch v := v.(type) {
	case int64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

const headerSize = 16

func writeMessage(w io.Writer, tag uint32, msg map[string]any) error {
	body := encodePlist(msg)
	h := make([]byte, headerSize, headerSize+len(body))
	binary.LittleEndian.PutUint32(h[0:], uint32(headerSize+len(body)))
	binary.LittleEndian.PutUint32(h[4:], 1) // plist
	binary.LittleEndian.PutUint32(h[8:], 8) // a plist message
	binary.LittleEndian.PutUint32(h[12:], tag)
	_, err := w.Write(append(h, body...))
	return err
}

func readMessage(r io.Reader) (map[string]any, error) {
	var h [headerSize]byte
	if _, err := io.ReadFull(r, h[:]); err != nil {
		return nil, err
	}
	n := binary.LittleEndian.Uint32(h[0:])
	if n < headerSize || n > 16<<20 {
		return nil, fmt.Errorf("a message of %d bytes", n)
	}
	body := make([]byte, n-headerSize)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	v, err := decodePlist(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("a message that isn't a dictionary")
	}
	return m, nil
}
