// Package devtunnel carries the player's connections to asuka dev from
// an iPhone, which can't reach the computer's loopback as the simulator
// does: over the USB cable when there is one, else over the local
// network.
//
// Over USB it does what adb reverse does for Android, the other way
// round: an app on the phone can't open a connection to the computer
// through the cable, but the computer can open one to a port on the
// phone (usbmuxd, package usbmux). So the computer keeps a few idle
// connections open to the player's tunnel port (Serve). When the player
// needs a connection (the app's, or one for an asset), it takes an idle
// one and names what it's for in its first byte; the computer connects
// it there and answers with a byte, and the two pipe. The player closes
// a tunnel left idle for a second, and the computer opens another: the
// newest listener on the port gets the new ones (SO_REUSEPORT), so the
// player's share extension, while it runs, takes them from the app.
//
// Over the network the player dials the computer's addresses itself,
// and asks iOS for the local network (NSLocalNetworkUsageDescription).
package devtunnel

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// What a tunnel carries: its first byte.
const (
	KindRemote byte = 'r' // the app's connection (internal/remote)
	KindAssets byte = 'a' // one to its assets' HTTP server
	ack        byte = 1
)

// Spec is where the player finds asuka dev on a phone: what asuka dev
// passes it, as JSON, in ASUKA_REMOTE.
type Spec struct {
	// Tunnel is the phone's port the computer's tunnels arrive on (0:
	// none, the network only).
	Tunnel int `json:"tunnel,omitempty"`
	// Remote and Assets are the computer's addresses on the network, in
	// the order to try: the app's and its assets'.
	Remote []string `json:"remote,omitempty"`
	Assets []string `json:"assets,omitempty"`
	// AssetsPort is the phone's port, on its loopback, that the app's
	// asset URLs name: the player forwards it to the computer's.
	AssetsPort int `json:"assetsPort,omitempty"`
}

// ParseSpec reads a Spec: ok is false if s isn't one (an address, as
// on the simulator).
func ParseSpec(s string) (spec Spec, ok bool) {
	if !strings.HasPrefix(strings.TrimSpace(s), "{") {
		return Spec{}, false
	}
	if err := json.Unmarshal([]byte(s), &spec); err != nil {
		return Spec{}, false
	}
	return spec, true
}

func (s Spec) String() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// timing is how long things take; each pool, dialer and server keeps
// the one it was made with (the tests shorten them).
type timing struct {
	// maxIdle is how long the player keeps a tunnel it hasn't used.
	maxIdle time.Duration
	// waitTunnel is how long the player waits for a tunnel when they've
	// been coming (the cable is in), or when it has just started.
	waitTunnel time.Duration
	// dialTimeout bounds a dial over the network, and the computer's
	// answer on a tunnel.
	dialTimeout time.Duration
	// retry is how long the computer waits to try again for a tunnel.
	retry time.Duration
}

var timings = timing{maxIdle: time.Second, waitTunnel: 3 * time.Second, dialTimeout: 3 * time.Second, retry: 250 * time.Millisecond}

// Pool is the player's end of the tunnels: the ones the computer opened,
// idle until taken.
type Pool struct {
	ln    net.Listener
	mu    sync.Mutex
	idle  []idleConn // oldest first
	last  time.Time  // when one last came
	start time.Time
	more  chan struct{} // signaled as one comes
	done  bool          // closed
	t     timing
}

type idleConn struct {
	c    net.Conn
	came time.Time
}

// Listen takes the tunnels that come to port, on the loopback.
func Listen(port int) (*Pool, error) {
	ln, err := listenShared("127.0.0.1:" + strconv.Itoa(port))
	if err != nil {
		return nil, err
	}
	p := &Pool{ln: ln, start: time.Now(), more: make(chan struct{}, 1), t: timings}
	go p.accept()
	go p.reap()
	return p, nil
}

// Close stops taking tunnels, and closes the idle ones.
func (p *Pool) Close() error {
	err := p.ln.Close()
	p.mu.Lock()
	p.done = true
	for _, ic := range p.idle {
		ic.c.Close()
	}
	p.idle = nil
	p.mu.Unlock()
	return err
}

func (p *Pool) accept() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		now := time.Now()
		p.mu.Lock()
		p.idle = append(p.idle, idleConn{c, now})
		p.last = now
		p.mu.Unlock()
		select {
		case p.more <- struct{}{}:
		default:
		}
	}
}

// reap closes the tunnels left idle, for the computer to open new ones.
func (p *Pool) reap() {
	for {
		time.Sleep(p.t.maxIdle / 4)
		cut := time.Now().Add(-p.t.maxIdle)
		p.mu.Lock()
		n := 0
		for n < len(p.idle) && p.idle[n].came.Before(cut) {
			p.idle[n].c.Close()
			n++
		}
		p.idle = p.idle[n:]
		closed := p.done
		p.mu.Unlock()
		if closed {
			return
		}
	}
}

// recent reports whether tunnels have been coming: the cable is in, or
// the player has just started (they may be about to).
func (p *Pool) recent() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	return now.Sub(p.last) < 3*p.t.maxIdle || now.Sub(p.start) < p.t.waitTunnel
}

// Take returns a tunnel to what kind names, waiting for one at most
// wait.
func (p *Pool) Take(kind byte, wait time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(wait)
	for {
		p.mu.Lock()
		var c net.Conn
		if n := len(p.idle); n > 0 {
			c = p.idle[n-1].c // the newest: the least likely to be closing
			p.idle = p.idle[:n-1]
		}
		p.mu.Unlock()
		if c != nil {
			if connect(c, kind, p.t.dialTimeout) == nil {
				return c, nil
			}
			c.Close() // closed at the other end meanwhile: the next
			continue
		}
		left := time.Until(deadline)
		if left <= 0 {
			return nil, errors.New("no tunnel from asuka dev")
		}
		t := time.NewTimer(left)
		select {
		case <-p.more:
		case <-t.C:
		}
		t.Stop()
	}
}

// connect names what the tunnel c is for, and waits for the computer to
// connect it there.
func connect(c net.Conn, kind byte, timeout time.Duration) error {
	c.SetDeadline(time.Now().Add(timeout))
	defer c.SetDeadline(time.Time{})
	if _, err := c.Write([]byte{kind}); err != nil {
		return err
	}
	var b [1]byte
	if _, err := io.ReadFull(c, b[:]); err != nil {
		return err
	}
	if b[0] != ack {
		return errors.New("refused")
	}
	return nil
}

// Dialer is the player's way to asuka dev on a phone, as its Spec says:
// a tunnel if one comes, else the network.
type Dialer struct {
	t    timing
	spec Spec
	pool *Pool
	lns  []net.Listener
}

// NewDialer listens for the spec's tunnels and serves its assets' port.
func NewDialer(spec Spec) (*Dialer, error) {
	d := &Dialer{spec: spec, t: timings}
	if spec.Tunnel > 0 {
		p, err := Listen(spec.Tunnel)
		if err != nil {
			return nil, err
		}
		d.pool = p
	}
	if spec.AssetsPort > 0 {
		ln, err := listenShared("127.0.0.1:" + strconv.Itoa(spec.AssetsPort))
		if err != nil {
			d.Close()
			return nil, err
		}
		d.lns = append(d.lns, ln)
		go d.forward(ln, KindAssets, spec.Assets)
	}
	return d, nil
}

// Close stops listening.
func (d *Dialer) Close() {
	if d.pool != nil {
		d.pool.Close()
	}
	for _, ln := range d.lns {
		ln.Close()
	}
}

// Remote returns a connection to the app's Go code.
func (d *Dialer) Remote() (net.Conn, error) { return d.dial(KindRemote, d.spec.Remote) }

func (d *Dialer) dial(kind byte, addrs []string) (net.Conn, error) {
	if d.pool != nil {
		wait := 100 * time.Millisecond
		if d.pool.recent() {
			wait = d.t.waitTunnel
		}
		if c, err := d.pool.Take(kind, wait); err == nil {
			return c, nil
		}
	}
	err := errors.New("asuka dev is out of reach")
	for _, a := range addrs {
		c, e := net.DialTimeout("tcp", a, d.t.dialTimeout)
		if e == nil {
			return c, nil
		}
		err = e
	}
	return nil, err
}

// forward connects what comes to ln to the computer's kind (its assets).
func (d *Dialer) forward(ln net.Listener, kind byte, addrs []string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			up, err := d.dial(kind, addrs)
			if err != nil {
				c.Close()
				return
			}
			pipe(c, up)
		}()
	}
}

// pipe copies between a and b until either ends, then closes both.
func pipe(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		io.Copy(dst, src)
		done <- struct{}{}
	}
	go cp(a, b)
	go cp(b, a)
	<-done
	a.Close()
	b.Close()
	<-done
}

// Server is the computer's end: it keeps idle tunnels open to the
// player, and connects each one the player takes to its kind's target.
type Server struct {
	// Dial opens a tunnel: a connection to the player's tunnel port.
	Dial func() (net.Conn, error)
	// Targets are the local addresses, by kind.
	Targets map[byte]string
	// Idle is how many tunnels to keep waiting (default 2).
	Idle int
	// Used, if set, is told of each tunnel the player takes.
	Used func(kind byte)

	t      timing
	mu     sync.Mutex
	conns  map[net.Conn]bool
	closed bool
	stop   chan struct{}
}

// Start opens the idle tunnels, and keeps them coming until Close.
func (s *Server) Start() {
	s.t = timings
	s.conns = map[net.Conn]bool{}
	s.stop = make(chan struct{})
	n := s.Idle
	if n <= 0 {
		n = 2
	}
	for range n {
		go s.tunnel()
	}
}

// Close closes the tunnels, idle and in use.
func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.stop)
	for c := range s.conns {
		c.Close()
	}
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		c.Close()
		return false
	}
	s.conns[c] = true
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
	c.Close()
}

// tunnel keeps one tunnel waiting until the player takes it, then starts
// another in its place and serves this one.
func (s *Server) tunnel() {
	for {
		select {
		case <-s.stop:
			return
		default:
		}
		c, err := s.Dial()
		if err != nil {
			select {
			case <-s.stop:
				return
			case <-time.After(s.t.retry):
			}
			continue
		}
		if !s.track(c) {
			return
		}
		var b [1]byte
		if _, err := io.ReadFull(c, b[:]); err != nil {
			s.untrack(c) // the player closed it idle, or went
			continue
		}
		go s.tunnel()
		s.serve(c, b[0])
		return
	}
}

func (s *Server) serve(c net.Conn, kind byte) {
	defer s.untrack(c)
	target, ok := s.Targets[kind]
	if !ok {
		return
	}
	up, err := net.DialTimeout("tcp", target, s.t.dialTimeout)
	if err != nil {
		return
	}
	if !s.track(up) {
		return
	}
	defer s.untrack(up)
	if _, err := c.Write([]byte{ack}); err != nil {
		return
	}
	if s.Used != nil {
		s.Used(kind)
	}
	pipe(c, up)
}

// Relay connects what comes to ln to target until ln closes, telling
// used (if set) where each came from: the computer's end for a phone on
// the network.
func Relay(ln net.Listener, target string, used func(from net.Addr)) {
	t := timings
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func() {
			up, err := net.DialTimeout("tcp", target, t.dialTimeout)
			if err != nil {
				c.Close()
				return
			}
			if used != nil {
				used(c.RemoteAddr())
			}
			pipe(c, up)
		}()
	}
}
