//go:build e2e

package e2e

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

// devtools is a page in a headless Chrome, driven through the Chrome
// DevTools Protocol over a WebSocket: just enough of both to capture
// the page.
type devtools struct {
	conn net.Conn
	r    *bufio.Reader
	id   int
}

// devtoolsPage opens url in a headless Chrome at a device pixel ratio.
func devtoolsPage(t *testing.T, browser, pageURL, scale string) *devtools {
	t.Helper()
	c := exec.Command(browser, "--headless=new", "--no-first-run", "--no-default-browser-check", "--disable-gpu",
		"--user-data-dir="+t.TempDir(), "--window-size=430,932", "--force-device-scale-factor="+scale,
		"--hide-scrollbars", "--remote-debugging-port=0", pageURL)
	stderr, err := c.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Process.Kill(); c.Wait() })
	// Chrome says where its DevTools listen.
	addr := ""
	sc := bufio.NewScanner(stderr)
	for sc.Scan() {
		if m := regexp.MustCompile(`DevTools listening on ws://([^/]+)/`).FindStringSubmatch(sc.Text()); m != nil {
			addr = m[1]
			break
		}
	}
	if addr == "" {
		t.Fatal("Chrome printed no DevTools address")
	}
	go io.Copy(io.Discard, stderr)
	// The page's target.
	var ws string
	for range 50 {
		var targets []struct{ Type, URL, WebSocketDebuggerURL string }
		if resp, err := http.Get("http://" + addr + "/json/list"); err == nil {
			json.NewDecoder(resp.Body).Decode(&targets)
			resp.Body.Close()
		}
		for _, tg := range targets {
			if tg.Type == "page" && strings.HasPrefix(tg.URL, pageURL) {
				ws = tg.WebSocketDebuggerURL
			}
		}
		if ws != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if ws == "" {
		t.Fatal("no page target in Chrome")
	}
	d, err := dialWebSocket(ws)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.conn.Close() })
	return d
}

func dialWebSocket(raw string) (*devtools, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 16)
	rand.Read(key)
	fmt.Fprintf(conn, "GET %s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		u.RequestURI(), u.Host, base64.StdEncoding.EncodeToString(key))
	r := bufio.NewReader(conn)
	resp, err := http.ReadResponse(r, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, fmt.Errorf("WebSocket handshake: %s", resp.Status)
	}
	return &devtools{conn: conn, r: r}, nil
}

// send writes one masked text frame, as a client must.
func (d *devtools) send(msg []byte) error {
	h := []byte{0x81}
	switch n := len(msg); {
	case n < 126:
		h = append(h, 0x80|byte(n))
	case n < 1<<16:
		h = append(h, 0x80|126)
		h = binary.BigEndian.AppendUint16(h, uint16(n))
	default:
		h = append(h, 0x80|127)
		h = binary.BigEndian.AppendUint64(h, uint64(n))
	}
	mask := make([]byte, 4)
	rand.Read(mask)
	h = append(h, mask...)
	body := make([]byte, len(msg))
	for i, c := range msg {
		body[i] = c ^ mask[i%4]
	}
	_, err := d.conn.Write(append(h, body...))
	return err
}

// receive reads one message, joining its frames.
func (d *devtools) receive() ([]byte, error) {
	var msg []byte
	for {
		var h [2]byte
		if _, err := io.ReadFull(d.r, h[:]); err != nil {
			return nil, err
		}
		n := uint64(h[1] & 0x7f)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(d.r, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(d.r, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(d.r, body); err != nil {
			return nil, err
		}
		switch op := h[0] & 0x0f; op {
		case 0x8:
			return nil, errors.New("the WebSocket closed")
		case 0x9, 0xa: // ping, pong: Chrome sends none to clients that don't ask
			continue
		}
		msg = append(msg, body...)
		if h[0]&0x80 != 0 {
			return msg, nil
		}
	}
}

// call sends a DevTools command and returns its result.
func (d *devtools) call(method string, params any, result any) error {
	d.id++
	req, _ := json.Marshal(map[string]any{"id": d.id, "method": method, "params": params})
	if err := d.send(req); err != nil {
		return err
	}
	d.conn.SetReadDeadline(time.Now().Add(within))
	for {
		b, err := d.receive()
		if err != nil {
			return err
		}
		var resp struct {
			ID     int
			Result json.RawMessage
			Error  *struct{ Message string }
		}
		if json.Unmarshal(b, &resp) != nil || resp.ID != d.id {
			continue // an event
		}
		if resp.Error != nil {
			return fmt.Errorf("%s: %s", method, resp.Error.Message)
		}
		return json.Unmarshal(resp.Result, result)
	}
}

// screenshot captures the page's viewport as a PNG.
func (d *devtools) screenshot() ([]byte, error) {
	var r struct{ Data string }
	if err := d.call("Page.captureScreenshot", map[string]any{"format": "png"}, &r); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(r.Data)
}
