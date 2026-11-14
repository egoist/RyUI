package devtunnel

import (
	"bufio"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func fast(t *testing.T) {
	old := timings
	timings = timing{maxIdle: 200 * time.Millisecond, waitTunnel: time.Second, dialTimeout: time.Second, retry: 10 * time.Millisecond}
	t.Cleanup(func() { timings = old })
}

// greeter answers each line with name and the line.
func greeter(t *testing.T, name string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					io.WriteString(c, name+":"+l)
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func ask(t *testing.T, c net.Conn, line string) string {
	t.Helper()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(c, line+"\n"); err != nil {
		t.Fatal(err)
	}
	l, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	return l[:len(l)-1]
}

func port(ln net.Listener) int { return ln.Addr().(*net.TCPAddr).Port }

func serve(t *testing.T, tunnelPort int, dials *atomic.Int32) *Server {
	t.Helper()
	s := &Server{
		Dial: func() (net.Conn, error) {
			dials.Add(1)
			return net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(tunnelPort))
		},
		Targets: map[byte]string{KindRemote: greeter(t, "app"), KindAssets: greeter(t, "assets")},
	}
	s.Start()
	t.Cleanup(s.Close)
	return s
}

func TestTunnels(t *testing.T) {
	fast(t)
	p, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var dials atomic.Int32
	used := make(chan byte, 8)
	s := serve(t, port(p.ln), &dials)
	s.Used = func(k byte) { used <- k }
	r, err := p.Take(KindRemote, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	a, err := p.Take(KindAssets, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if got := ask(t, r, "hi"); got != "app:hi" {
		t.Fatalf("the app's tunnel: %q", got)
	}
	if got := ask(t, a, "logo.png"); got != "assets:logo.png" {
		t.Fatalf("the assets' tunnel: %q", got)
	}
	if k1, k2 := <-used, <-used; k1+k2 != KindRemote+KindAssets {
		t.Fatalf("used: %c %c", k1, k2)
	}
	// Idle ones are closed and opened again; the ones in use stay.
	before := dials.Load()
	time.Sleep(4 * timings.maxIdle)
	if dials.Load() <= before {
		t.Fatal("idle tunnels weren't renewed")
	}
	if got := ask(t, r, "still"); got != "app:still" {
		t.Fatalf("a tunnel in use, after the idle ones were renewed: %q", got)
	}
	// Closing the computer's end ends the tunnels.
	s.Close()
	r.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := r.Read(make([]byte, 1)); err == nil {
		t.Fatal("a tunnel outlived its server")
	}
}

// The newest listener on the port takes the new tunnels: the share
// extension's, while it runs, then the app's again.
func TestNewestListener(t *testing.T) {
	fast(t)
	app, err := Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	var dials atomic.Int32
	serve(t, port(app.ln), &dials)
	if c, err := app.Take(KindRemote, time.Second); err != nil {
		t.Fatal(err)
	} else {
		c.Close()
	}
	ext, err := Listen(port(app.ln))
	if err != nil {
		t.Fatal(err)
	}
	c, err := ext.Take(KindRemote, 2*time.Second)
	if err != nil {
		t.Fatal("the extension got no tunnel:", err)
	}
	if got := ask(t, c, "share"); got != "app:share" {
		t.Fatalf("through the extension's tunnel: %q", got)
	}
	c.Close()
	ext.Close()
	c, err = app.Take(KindRemote, 2*time.Second)
	if err != nil {
		t.Fatal("the app got no tunnel once the extension closed:", err)
	}
	c.Close()
}

func TestDialerFallsBackToNetwork(t *testing.T) {
	fast(t)
	free, _ := net.Listen("tcp", "127.0.0.1:0")
	tunnel := port(free)
	free.Close()
	lan := greeter(t, "lan")
	d, err := NewDialer(Spec{Tunnel: tunnel, Remote: []string{"127.0.0.1:1", lan}})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	start := time.Now()
	c, err := d.Remote()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := ask(t, c, "x"); got != "lan:x" {
		t.Fatalf("over the network: %q", got)
	}
	if time.Since(start) > 3*timings.waitTunnel {
		t.Fatalf("took %v", time.Since(start))
	}
}

func TestDialerForwardsAssets(t *testing.T) {
	fast(t)
	free, _ := net.Listen("tcp", "127.0.0.1:0")
	assetsPort := port(free)
	free.Close()
	d, err := NewDialer(Spec{Tunnel: 0, Assets: []string{greeter(t, "lan-assets")}, AssetsPort: assetsPort})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	c, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(assetsPort))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := ask(t, c, "font.ttf"); got != "lan-assets:font.ttf" {
		t.Fatalf("the assets' port: %q", got)
	}
	// And through a tunnel when one comes.
	tun, _ := net.Listen("tcp", "127.0.0.1:0")
	tp := port(tun)
	tun.Close()
	d2, err := NewDialer(Spec{Tunnel: tp, AssetsPort: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	var dials atomic.Int32
	serve(t, tp, &dials)
	r, err := d2.Remote()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got := ask(t, r, "y"); got != "app:y" {
		t.Fatalf("the app over the tunnel: %q", got)
	}
}

func TestParseSpec(t *testing.T) {
	if _, ok := ParseSpec("127.0.0.1:7461"); ok {
		t.Fatal("an address read as a spec")
	}
	s := Spec{Tunnel: 7463, Remote: []string{"mac.local:7461"}, Assets: []string{"mac.local:7462"}, AssetsPort: 7462}
	got, ok := ParseSpec(s.String())
	if !ok || got.Tunnel != 7463 || got.Remote[0] != "mac.local:7461" || got.AssetsPort != 7462 {
		t.Fatalf("round trip: %+v %v", got, ok)
	}
}
