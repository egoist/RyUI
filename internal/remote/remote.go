// Package remote runs a nui app's Go code on a development machine and
// its views in a player app on a simulator, emulator or device, over a
// socket (`nui dev`). The core's side is a host.Host whose calls cross
// the socket; the player's side is a host.Core that does the same the
// other way. Both ends are in Go: the player links this package into its
// mobile library, in place of the app.
//
// Calls are synchronous both ways and nest, as they do in process: while
// a side waits for its reply it serves the other side's requests (a
// list binding cells while it applies ops, a measure while it lays
// out). One difference is held back: the core doesn't take events while
// it waits (hosts never send them in the middle of an Apply), so an
// event the player sent meanwhile is served once the core's call
// returns.
package remote

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sync"
	"time"
)

// Methods.
const (
	mApply      = 1 // core → host: a batch
	mMeasure    = 2 // core → host: u32 id, f32 width, height, avail width, avail height → f32 width, height
	mEvent      = 3 // host → core: an event
	mListCount  = 4 // host → core: u32 list → i64
	mListBind   = 5 // host → core: u32 list, i64 index, u32 cell
	mListUnbind = 6 // host → core: u32 list, u32 cell
	mAttach     = 7 // host → core: the Activity was recreated
)

// Frame kinds.
const (
	kRequest = 0
	kReply   = 1
)

var le = binary.LittleEndian

var errClosed = errors.New("remote: connection closed")

// callTimeout is how long a call waits for its reply.
const callTimeout = 10 * time.Second

// frame is a message: u32 length (of the rest), u8 kind, u32 call ID,
// u8 method (requests), payload.
type frame struct {
	kind    byte
	id      uint32
	method  byte
	payload []byte
}

// peer is one end of a connection. Its requests are served, and its
// calls wait, on the goroutine that owns it (the main thread or the
// core's loop); a reader goroutine feeds frames to in.
type peer struct {
	rw     io.ReadWriteCloser
	in     chan frame // closed when the connection is
	wmu    sync.Mutex
	wbuf   []byte
	nextID uint32
	calls  []uint32 // the IDs of the calls waiting, innermost last

	// serve answers a request; defer reports whether to hold it until
	// no call is waiting.
	serve  func(method byte, payload []byte) []byte
	defer_ func(method byte) bool
	held   []frame
}

func newPeer(rw io.ReadWriteCloser) *peer {
	p := &peer{rw: rw, in: make(chan frame, 64)}
	return p
}

// read feeds frames to p.in until the connection ends; notify (if any)
// is called after each frame (the player wakes its main thread).
func (p *peer) read(notify func()) {
	defer func() {
		close(p.in)
		if notify != nil {
			notify() // to notice the end
		}
	}()
	var hdr [4]byte
	for {
		if _, err := io.ReadFull(p.rw, hdr[:]); err != nil {
			return
		}
		n := le.Uint32(hdr[:])
		if n < 5 || n > 256<<20 {
			return
		}
		b := make([]byte, n)
		if _, err := io.ReadFull(p.rw, b); err != nil {
			return
		}
		f := frame{kind: b[0], id: le.Uint32(b[1:5])}
		rest := b[5:]
		if f.kind == kRequest {
			if len(rest) < 1 {
				return
			}
			f.method, rest = rest[0], rest[1:]
		}
		f.payload = rest
		if debug {
			fmt.Fprintf(os.Stderr, "NUIDBG recv kind=%d id=%d m=%d n=%d\n", f.kind, f.id, f.method, len(f.payload))
		}
		p.in <- f
		if notify != nil {
			notify()
		}
	}
}

var debug = os.Getenv("NUI_DEBUG_REMOTE") != ""

func (p *peer) send(kind byte, id uint32, method byte, payload []byte) error {
	if debug {
		fmt.Fprintf(os.Stderr, "NUIDBG send kind=%d id=%d m=%d n=%d calls=%v\n", kind, id, method, len(payload), p.calls)
	}
	p.wmu.Lock()
	defer p.wmu.Unlock()
	n := 5 + len(payload)
	if kind == kRequest {
		n++
	}
	b := le.AppendUint32(p.wbuf[:0], uint32(n))
	b = append(b, kind)
	b = le.AppendUint32(b, id)
	if kind == kRequest {
		b = append(b, method)
	}
	b = append(b, payload...)
	p.wbuf = b
	_, err := p.rw.Write(b)
	return err
}

// call sends a request and waits for its reply, serving the other
// side's requests meanwhile.
func (p *peer) call(method byte, payload []byte) ([]byte, error) {
	p.nextID++
	id := p.nextID
	if err := p.send(kRequest, id, method, payload); err != nil {
		return nil, err
	}
	p.calls = append(p.calls, id)
	defer func() {
		p.calls = p.calls[:len(p.calls)-1]
		if len(p.calls) == 0 {
			p.serveHeld()
		}
	}()
	for {
		var f frame
		var ok bool
		select {
		case f, ok = <-p.in:
		case <-time.After(callTimeout):
			// The other side is gone without closing (a player killed
			// behind adb reverse): don't wait forever on a main thread.
			p.rw.Close()
			return nil, errClosed
		}
		if !ok {
			return nil, errClosed
		}
		if f.kind == kReply {
			if f.id != id {
				return nil, fmt.Errorf("remote: reply %d to call %d", f.id, id)
			}
			return f.payload, nil
		}
		p.handle(f)
	}
}

// handle serves a request, or holds it.
func (p *peer) handle(f frame) {
	if len(p.calls) > 0 && p.defer_ != nil && p.defer_(f.method) {
		p.held = append(p.held, f)
		return
	}
	reply := p.serve(f.method, f.payload)
	p.send(kReply, f.id, 0, reply)
}

func (p *peer) serveHeld() {
	for len(p.held) > 0 && len(p.calls) == 0 {
		f := p.held[0]
		p.held = p.held[1:]
		p.handle(f)
	}
}

// Encodings of the payloads.

func encodeMeasure(id uint32, w, h, aw, ah float32) []byte {
	b := make([]byte, 0, 20)
	b = le.AppendUint32(b, id)
	for _, f := range [...]float32{w, h, aw, ah} {
		b = le.AppendUint32(b, math.Float32bits(f))
	}
	return b
}

func f32(b []byte, i int) float32 { return math.Float32frombits(le.Uint32(b[i:])) }

func encodeSize(w, h float32) []byte {
	b := make([]byte, 0, 8)
	b = le.AppendUint32(b, math.Float32bits(w))
	return le.AppendUint32(b, math.Float32bits(h))
}
