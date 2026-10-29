package remote

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"slices"
	"sync/atomic"
	"syscall"

	"github.com/egoist/nui/host"
	"github.com/egoist/nui/internal/boot"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
	sig "github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

// Host is the core's side of a connection: a host.Host whose calls go
// to the player.
type Host struct {
	p      *peer
	env    host.Environment
	core   host.Core
	posts  chan func()
	closed bool
	mount  func() // the first environment mounts the app, before it's answered
}

func (h *Host) Apply(b *ops.Batch) {
	if h.closed {
		return
	}
	if _, err := h.p.call(mApply, b.Encode()); err != nil {
		h.closed = true
	}
}

func (h *Host) Measure(id ops.NodeID, c layout.Constraints) layout.Size {
	if h.closed {
		return layout.Size{}
	}
	r, err := h.p.call(mMeasure, encodeMeasure(uint32(id), c.Width, c.Height, c.AvailW, c.AvailH))
	if err != nil || len(r) < 8 {
		h.closed = true
		return layout.Size{}
	}
	return layout.Size{W: f32(r, 0), H: f32(r, 4)}
}

// Post runs f on the core's loop. Safe from any goroutine.
func (h *Host) Post(f func()) { h.posts <- f }

func (h *Host) Environment() host.Environment { return h.env }

func (h *Host) Connect(c host.Core) { h.core = c }

// serve answers the player's requests.
func (h *Host) serve(method byte, b []byte) []byte {
	switch method {
	case mEvent:
		ev, err := ops.DecodeEvent(b)
		if err != nil {
			fmt.Fprintln(os.Stderr, "nui:", err)
			return nil
		}
		if ev.Kind == ops.EventEnvironment && (ev.ID == 0 || h.core == nil) {
			h.env = ev.Env
			if h.core == nil && h.mount != nil && h.env.Width > 0 {
				h.mount()
				return nil
			}
		}
		if h.core != nil {
			h.core.Dispatch(ev)
		}
	case mListCount:
		n := 0
		if h.core != nil && len(b) >= 4 {
			n = h.core.ListCount(ops.NodeID(le.Uint32(b)))
		}
		return le.AppendUint64(nil, uint64(n))
	case mListBind:
		if h.core != nil && len(b) >= 16 {
			h.core.ListBind(ops.NodeID(le.Uint32(b)), int(int64(le.Uint64(b[4:]))), ops.NodeID(le.Uint32(b[12:])))
		}
	case mListUnbind:
		if h.core != nil && len(b) >= 8 {
			h.core.ListUnbind(ops.NodeID(le.Uint32(b)), ops.NodeID(le.Uint32(b[4:])))
		}
	case mAttach:
		if h.core != nil {
			ui.Reattach()
		}
	}
	return nil
}

// Server serves an app to every player connected, each with its own
// root (the app on an iPhone and an Android phone at once), on one loop:
// the core's main thread.
type Server struct {
	root     func() ui.Node
	posts    chan func()
	ready    chan *session // a session has frames to read, or ended
	sessions []*session
	// Closed, if set, is called on the loop when a session ends.
	Closed func()
}

type session struct {
	conn  net.Conn
	h     *Host
	r     *ui.Root
	woken atomic.Bool // a wake-up is in ready: at most one, so the reader never blocks
}

// NewServer makes a server for root; posts carries ui.Post's functions
// to the loop.
func NewServer(root func() ui.Node, posts chan func()) *Server {
	return &Server{root: root, posts: posts, ready: make(chan *session, 64)}
}

// add starts a session for a player connected over conn. It mounts the
// app once the player has sent its environment. Loop only.
func (s *Server) add(conn net.Conn) {
	p := newPeer(conn)
	ss := &session{conn: conn}
	h := &Host{p: p, posts: s.posts}
	ss.h = h
	p.serve = h.serve
	// Events wait while the core waits for a player; list requests
	// can't (the player is binding cells for the core's batch).
	p.defer_ = func(m byte) bool { return m == mEvent || m == mAttach }
	h.mount = func() { ss.r = ui.Mount(h, s.root) }
	s.sessions = append(s.sessions, ss)
	// The reader wakes the loop, but mustn't wait for it: the loop may be
	// deep in nested calls, waiting for the reader's next frame.
	go p.read(func() {
		if !ss.woken.Swap(true) {
			s.ready <- ss
		}
	})
}

// step serves what a session's player sent while the loop was idle.
func (s *Server) step(ss *session) {
	ss.woken.Store(false)
	p := ss.h.p
	for {
		select {
		case f, ok := <-p.in:
			if !ok {
				s.end(ss)
				return
			}
			if f.kind == kRequest {
				p.handle(f)
			}
		default:
			if ss.h.closed {
				s.end(ss)
			}
			return
		}
	}
}

func (s *Server) end(ss *session) {
	i := slices.Index(s.sessions, ss)
	if i < 0 {
		return
	}
	s.sessions = slices.Delete(s.sessions, i, i+1)
	ss.h.closed = true
	ss.conn.Close()
	// Save while the root's signals are alive.
	if s.Closed != nil {
		s.Closed()
	}
	if ss.r != nil {
		ss.r.Dispose()
	}
}

// Loop serves the connections arriving on conns, and runs posted
// functions, until conns closes. Call it on the goroutine that owns the
// core.
func (s *Server) Loop(conns <-chan net.Conn) {
	sig.BindThread()
	for {
		select {
		case c, ok := <-conns:
			if !ok {
				for len(s.sessions) > 0 {
					s.end(s.sessions[0])
				}
				return
			}
			s.add(c)
		case ss := <-s.ready:
			s.step(ss)
		case f := <-s.posts:
			f()
		}
	}
}

// Run listens on addr and serves root to the players that connect,
// keeping its signals' values across reloads (in the file NUI_STATE
// names): saved when a player disconnects and when the process is
// stopped, restored by the next run. It returns if listening fails.
func Run(addr string, root func() ui.Node) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	store := os.Getenv("NUI_STATE")
	if err := keepState(store); err != nil {
		fmt.Fprintln(os.Stderr, "nui: restoring state:", err)
	}
	if base := os.Getenv("NUI_ASSETS_URL"); base != "" {
		boot.AssetBase = base
		if a := os.Getenv("NUI_ASSETS_ADDR"); a != "" {
			go serveAssets(a, "assets")
		}
	}
	save := func() {
		sig.KeptState() // also for a player reconnecting to this run
		if store != "" {
			if err := saveState(store); err != nil {
				fmt.Fprintln(os.Stderr, "nui: saving state:", err)
			}
		}
	}
	posts := make(chan func(), 64)
	srv := NewServer(root, posts)
	srv.Closed = save
	// Stopped (nui dev reloading): save on the loop, then exit.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		posts <- func() { save(); os.Exit(0) }
	}()
	conns := make(chan net.Conn)
	go func() {
		defer close(conns)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			conns <- c
		}
	}()
	srv.Loop(conns)
	return nil
}
