package remote

import (
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/egoist/nui/host"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
)

// Proxy is the player's side: the host.Core its platform host calls,
// forwarding to the core over the connection, and applying what the core
// sends to the platform host. It runs on the player's main thread; Post
// wakes it when frames arrive.
type Proxy struct {
	local host.Host

	mu   sync.Mutex
	p    *peer // nil while disconnected
	wake bool

	live map[ops.NodeID]bool // the views the core made, to clear them for the next session
	root ops.NodeID
	env  []byte // the root's last environment event (ID 0), to send again on connect
}

// NewProxy makes a proxy for local, the player's platform host.
func NewProxy(local host.Host) *Proxy {
	return &Proxy{local: local, live: map[ops.NodeID]bool{}}
}

// serve answers the core's requests, on the main thread.
func (x *Proxy) serve(method byte, b []byte) []byte {
	switch method {
	case mApply:
		var batch ops.Batch
		if err := batch.Decode(b); err != nil {
			fmt.Fprintln(os.Stderr, "nui:", err)
			return nil
		}
		for i := range batch.Ops {
			switch op := &batch.Ops[i]; op.Code {
			case ops.OpCreate:
				x.live[op.ID] = true
			case ops.OpDestroy:
				delete(x.live, op.ID)
			case ops.OpSetRoot:
				x.root = op.ID
			}
		}
		x.local.Apply(&batch)
	case mMeasure:
		if len(b) < 20 {
			return encodeSize(0, 0)
		}
		s := x.local.Measure(ops.NodeID(le.Uint32(b)), layout.Constraints{Width: f32(b, 4), Height: f32(b, 8), AvailW: f32(b, 12), AvailH: f32(b, 16)})
		return encodeSize(s.W, s.H)
	}
	return nil
}

func (x *Proxy) peer() *peer {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.p
}

// call forwards a request on the main thread; disconnected, it does
// nothing.
func (x *Proxy) call(method byte, b []byte) []byte {
	p := x.peer()
	if p == nil {
		return nil
	}
	r, err := p.call(method, b)
	if err != nil {
		x.disconnected(p)
	}
	return r
}

// Dispatch implements host.Core.
func (x *Proxy) Dispatch(ev ops.Event) {
	if ev.Kind == ops.EventEnvironment && (ev.ID == 0 || ev.ID == x.root) {
		e := ev
		e.ID = 0 // any session's root
		x.env = ops.AppendEvent(nil, &e)
	}
	x.call(mEvent, ops.AppendEvent(nil, &ev))
}

// SetEnvironment sets the root's environment the proxy sends the core on
// connecting (one the host reported before the player started).
func (x *Proxy) SetEnvironment(e ops.Environment) {
	x.env = ops.AppendEvent(nil, &ops.Event{Kind: ops.EventEnvironment, Env: e})
}

func (x *Proxy) ListCount(list ops.NodeID) int {
	r := x.call(mListCount, le.AppendUint32(nil, uint32(list)))
	if len(r) < 8 {
		return 0
	}
	return int(int64(le.Uint64(r)))
}

func (x *Proxy) ListBind(list ops.NodeID, index int, cell ops.NodeID) {
	b := le.AppendUint32(nil, uint32(list))
	b = le.AppendUint64(b, uint64(index))
	x.call(mListBind, le.AppendUint32(b, uint32(cell)))
}

func (x *Proxy) ListUnbind(list ops.NodeID, cell ops.NodeID) {
	x.call(mListUnbind, le.AppendUint32(le.AppendUint32(nil, uint32(list)), uint32(cell)))
}

// Reattach forwards an Activity's recreation (Android).
func (x *Proxy) Reattach() { x.call(mAttach, nil) }

// drain serves what arrived while the main thread was idle.
func (x *Proxy) drain() {
	x.mu.Lock()
	x.wake = false
	p := x.p
	x.mu.Unlock()
	if p == nil {
		return
	}
	for {
		select {
		case f, ok := <-p.in:
			if !ok {
				x.disconnected(p)
				return
			}
			if f.kind == kRequest {
				p.handle(f)
			}
		default:
			return
		}
	}
}

func (x *Proxy) notify() {
	x.mu.Lock()
	if x.wake {
		x.mu.Unlock()
		return
	}
	x.wake = true
	x.mu.Unlock()
	x.local.Post(x.drain)
}

func (x *Proxy) disconnected(p *peer) {
	x.mu.Lock()
	if x.p == p {
		x.p = nil
	}
	x.mu.Unlock()
}

// Attach starts a session over conn, on the main thread: it clears the
// views of the last session, and sends the root's environment, which
// makes the core mount the app.
func (x *Proxy) Attach(conn net.Conn) {
	if len(x.live) > 0 {
		var b ops.Batch
		for id := range x.live {
			b.Destroy(id)
		}
		clear(x.live)
		x.root = 0
		x.local.Apply(&b)
	}
	p := newPeer(conn)
	p.serve = x.serve
	x.mu.Lock()
	x.p = p
	x.mu.Unlock()
	go p.read(x.notify)
	if x.env != nil {
		x.call(mEvent, x.env)
	}
}

// Connect keeps a connection to addr, reconnecting when it drops (the
// core restarted). Call it on its own goroutine.
func (x *Proxy) Connect(addr string) {
	for {
		conn, err := net.Dial("tcp", addr)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		done := make(chan struct{})
		x.local.Post(func() { x.Attach(conn); close(done) })
		<-done
		for x.peer() != nil {
			time.Sleep(100 * time.Millisecond)
		}
		conn.Close()
	}
}
