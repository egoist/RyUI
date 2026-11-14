package usbmux

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeMuxd is usbmuxd for the tests: it lists devs, and connects a
// Connect to ports[port] (a local listener's address), refusing others.
func fakeMuxd(t *testing.T, devs []map[string]any, ports map[int]string) (path string, seen chan map[string]any) {
	t.Helper()
	dir, err := os.MkdirTemp("", "mux") // short: a socket's path has a limit
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path = filepath.Join(dir, "s")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	seen = make(chan map[string]any, 16)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				msg, err := readMessage(c)
				if err != nil {
					return
				}
				seen <- msg
				switch msg["MessageType"] {
				case "ListDevices":
					var list []any
					for _, d := range devs {
						list = append(list, map[string]any{"DeviceID": d["DeviceID"], "MessageType": "Attached", "Properties": d})
					}
					writeMessage(c, 1, map[string]any{"DeviceList": list})
				case "Connect":
					raw := int(msg["PortNumber"].(int64))
					port := int(uint16(raw)>>8 | uint16(raw)<<8)
					addr, ok := ports[port]
					if !ok {
						writeMessage(c, 1, map[string]any{"MessageType": "Result", "Number": 3})
						return
					}
					up, err := net.Dial("tcp", addr)
					if err != nil {
						writeMessage(c, 1, map[string]any{"MessageType": "Result", "Number": 3})
						return
					}
					defer up.Close()
					writeMessage(c, 1, map[string]any{"MessageType": "Result", "Number": 0})
					go io.Copy(up, c)
					io.Copy(c, up)
				}
			}()
		}
	}()
	return path, seen
}

func TestDevices(t *testing.T) {
	path, seen := fakeMuxd(t, []map[string]any{
		{"DeviceID": 7, "ConnectionType": "Network", "SerialNumber": "00008140000E55192283801C"},
		{"DeviceID": 3, "ConnectionType": "USB", "SerialNumber": "00008140000E55192283801C"},
		{"DeviceID": 9, "ConnectionType": "USB", "SerialNumber": "x", "UDID": "00008030-001A2D0C0C38802E"},
	}, nil)
	c := &Client{Path: path}
	devs, err := c.Devices()
	if err != nil {
		t.Fatal(err)
	}
	want := []Device{
		{ID: 7, UDID: "00008140000E55192283801C", Connection: "Network"},
		{ID: 3, UDID: "00008140000E55192283801C", Connection: "USB"},
		{ID: 9, UDID: "00008030-001A2D0C0C38802E", Connection: "USB"},
	}
	if !reflect.DeepEqual(devs, want) {
		t.Fatalf("devices: %+v", devs)
	}
	if m := <-seen; m["MessageType"] != "ListDevices" || m["kLibUSBMuxVersion"] != int64(3) {
		t.Fatalf("request: %v", m)
	}
	// devicectl's UDID has a dash usbmuxd's may not; USB is preferred.
	d, ok, err := c.Find("00008140-000E55192283801C")
	if err != nil || !ok || d.ID != 3 {
		t.Fatalf("find: %+v %v %v", d, ok, err)
	}
	if _, ok, _ := c.Find("00008030-0000"); ok {
		t.Fatal("found a device that isn't there")
	}
}

func TestDial(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { io.Copy(c, c); c.Close() }()
		}
	}()
	path, seen := fakeMuxd(t, nil, map[int]string{7463: echo.Addr().String()})
	c := &Client{Path: path}
	conn, err := c.Dial(3, 7463)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	m := <-seen
	// 7463 is 0x1D27; in network byte order, 0x271D.
	if m["MessageType"] != "Connect" || m["DeviceID"] != int64(3) || m["PortNumber"] != int64(0x271D) {
		t.Fatalf("request: %v", m)
	}
	conn.Write([]byte("hello"))
	buf := make([]byte, 5)
	if _, err := io.ReadFull(conn, buf); err != nil || !bytes.Equal(buf, []byte("hello")) {
		t.Fatalf("through the connection: %q %v", buf, err)
	}
	if _, err := c.Dial(3, 7000); err != ErrRefused {
		t.Fatalf("a closed port: %v", err)
	}
}

func TestPlist(t *testing.T) {
	in := map[string]any{"a": "x<y", "n": 42, "t": true, "f": false, "l": []any{"p", 1}, "d": map[string]any{"z": []byte{1, 2}}}
	v, err := decodePlist(bytes.NewReader(encodePlist(in)))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"a": "x<y", "n": int64(42), "t": true, "f": false, "l": []any{"p", int64(1)}, "d": map[string]any{"z": []byte{1, 2}}}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("round trip: %#v", v)
	}
}
