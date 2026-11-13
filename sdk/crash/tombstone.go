package crash

import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

// A tombstone: what Android's crash dumper (debuggerd) records of a
// native crash, the trace of a REASON_CRASH_NATIVE exit (Android 12 and
// later), in protocol buffers: AOSP's
// system/core/debuggerd/proto/tombstone.proto. Only what a report needs
// is read.
type tombstone struct {
	fingerprint string
	pid, tid    uint64
	command     []string
	uptime      uint64
	signal      tsSignal
	abort       string
	causes      []string
	threads     map[uint64]tsThread
	hasSignal   bool
}

type tsSignal struct {
	number       int64
	name, code   string
	codeNumber   int64
	faultAddr    uint64
	hasFaultAddr bool
	senderPID    int64
	hasSender    bool
}

type tsThread struct {
	id     uint64
	name   string
	frames []tsFrame
	notes  []string
}

type tsFrame struct {
	relPC, pc      uint64
	function, file string
	functionOffset uint64
	buildID        string
}

// pbReader reads protocol buffers' wire format.
type pbReader struct {
	b   []byte
	err bool
}

func (r *pbReader) varint() uint64 {
	v, n := binary.Uvarint(r.b)
	if n <= 0 {
		r.err = true
		r.b = nil
		return 0
	}
	r.b = r.b[n:]
	return v
}

// field reads the next field: its number, and its value (a varint) or its
// bytes (length-delimited). Fixed-width ones are skipped.
func (r *pbReader) field() (num int, v uint64, b []byte, ok bool) {
	if len(r.b) == 0 || r.err {
		return 0, 0, nil, false
	}
	key := r.varint()
	num, wire := int(key>>3), key&7
	switch wire {
	case 0:
		v = r.varint()
	case 1:
		if len(r.b) < 8 {
			r.err = true
			return 0, 0, nil, false
		}
		v = binary.LittleEndian.Uint64(r.b)
		r.b = r.b[8:]
	case 2:
		n := r.varint()
		if n > uint64(len(r.b)) {
			r.err = true
			return 0, 0, nil, false
		}
		b, r.b = r.b[:n], r.b[n:]
	case 5:
		if len(r.b) < 4 {
			r.err = true
			return 0, 0, nil, false
		}
		v = uint64(binary.LittleEndian.Uint32(r.b))
		r.b = r.b[4:]
	default:
		r.err = true
		return 0, 0, nil, false
	}
	return num, v, b, !r.err
}

// decodeTombstone reads a tombstone; false if b isn't one.
func decodeTombstone(b []byte) (*tombstone, bool) {
	if len(b) == 0 {
		return nil, false
	}
	t := &tombstone{threads: map[uint64]tsThread{}}
	r := &pbReader{b: b}
	for {
		num, v, sub, ok := r.field()
		if !ok {
			break
		}
		switch num {
		case 2:
			t.fingerprint = string(sub)
		case 5:
			t.pid = v
		case 6:
			t.tid = v
		case 9:
			t.command = append(t.command, string(sub))
		case 10:
			t.signal, t.hasSignal = decodeSignal(sub), true
		case 14:
			t.abort = string(sub)
		case 15:
			t.causes = append(t.causes, decodeCause(sub))
		case 16:
			// map<uint32, Thread>: an entry each, key 1 and value 2.
			e := &pbReader{b: sub}
			var th tsThread
			var key uint64
			for {
				n, v, b, ok := e.field()
				if !ok {
					break
				}
				switch n {
				case 1:
					key = v
				case 2:
					th = decodeThread(b)
				}
			}
			t.threads[key] = th
		case 20:
			t.uptime = v
		}
	}
	if r.err || t.pid == 0 && len(t.threads) == 0 {
		return nil, false
	}
	return t, true
}

func decodeSignal(b []byte) tsSignal {
	var s tsSignal
	r := &pbReader{b: b}
	for {
		num, v, sub, ok := r.field()
		if !ok {
			return s
		}
		switch num {
		case 1:
			s.number = int64(int32(v))
		case 2:
			s.name = string(sub)
		case 3:
			s.codeNumber = int64(int32(v))
		case 4:
			s.code = string(sub)
		case 5:
			s.hasSender = v != 0
		case 7:
			s.senderPID = int64(int32(v))
		case 8:
			s.hasFaultAddr = v != 0
		case 9:
			s.faultAddr = v
		}
	}
}

func decodeCause(b []byte) string {
	r := &pbReader{b: b}
	for {
		num, _, sub, ok := r.field()
		if !ok {
			return ""
		}
		if num == 1 {
			return string(sub)
		}
	}
}

func decodeThread(b []byte) tsThread {
	var t tsThread
	r := &pbReader{b: b}
	for {
		num, v, sub, ok := r.field()
		if !ok {
			return t
		}
		switch num {
		case 1:
			t.id = uint64(int32(v))
		case 2:
			t.name = string(sub)
		case 4:
			t.frames = append(t.frames, decodeFrame(sub))
		case 7:
			t.notes = append(t.notes, string(sub))
		}
	}
}

func decodeFrame(b []byte) tsFrame {
	var f tsFrame
	r := &pbReader{b: b}
	for {
		num, v, sub, ok := r.field()
		if !ok {
			return f
		}
		switch num {
		case 1:
			f.relPC = v
		case 2:
			f.pc = v
		case 4:
			f.function = string(sub)
		case 5:
			f.functionOffset = v
		case 6:
			f.file = string(sub)
		case 8:
			f.buildID = string(sub)
		}
	}
}

func (t *tombstone) message() string {
	if !t.hasSignal {
		return "Native crash"
	}
	s := t.signal
	msg := s.name
	if msg == "" {
		msg = signals[int(s.number)]
	}
	if msg == "" {
		msg = fmt.Sprintf("signal %d", s.number)
	}
	if s.code != "" {
		msg += " (" + s.code + ")"
	}
	if t.abort != "" {
		msg += ": " + t.abort
	} else if len(t.causes) > 0 && t.causes[0] != "" {
		msg += ": " + t.causes[0]
	}
	return msg
}

// order is the threads, the crashed one first.
func (t *tombstone) order() []tsThread {
	var out []tsThread
	if th, ok := t.threads[t.tid]; ok {
		out = append(out, th)
	}
	var ids []uint64
	for id := range t.threads {
		if id != t.tid {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		out = append(out, t.threads[id])
	}
	return out
}

func (t *tombstone) stacks() []Thread {
	var out []Thread
	for _, th := range t.order() {
		x := Thread{Name: th.name, Crashed: th.id == t.tid}
		for _, f := range th.frames {
			x.Frames = append(x.Frames, Frame{Function: f.function, Binary: f.file, BinaryID: f.buildID, Address: f.pc, Offset: f.relPC})
		}
		out = append(out, x)
	}
	return out
}

// text is the tombstone as debuggerd prints it, near enough.
func (t *tombstone) text() string {
	var b strings.Builder
	if t.fingerprint != "" {
		fmt.Fprintf(&b, "Build fingerprint: '%s'\n", t.fingerprint)
	}
	if t.uptime > 0 {
		fmt.Fprintf(&b, "Process uptime: %ds\n", t.uptime)
	}
	name := ""
	if th, ok := t.threads[t.tid]; ok {
		name = th.name
	}
	fmt.Fprintf(&b, "pid: %d, tid: %d, name: %s  >>> %s <<<\n", t.pid, t.tid, name, strings.Join(t.command, " "))
	if t.hasSignal {
		s := t.signal
		fmt.Fprintf(&b, "signal %d (%s), code %d (%s)", s.number, s.name, s.codeNumber, s.code)
		if s.hasSender {
			fmt.Fprintf(&b, " from pid %d", s.senderPID)
		}
		if s.hasFaultAddr {
			fmt.Fprintf(&b, ", fault addr 0x%x", s.faultAddr)
		}
		b.WriteString("\n")
	}
	for _, c := range t.causes {
		fmt.Fprintf(&b, "Cause: %s\n", c)
	}
	if t.abort != "" {
		fmt.Fprintf(&b, "Abort message: '%s'\n", t.abort)
	}
	for i, th := range t.order() {
		if i == 0 {
			b.WriteString("\nbacktrace:\n")
		} else {
			fmt.Fprintf(&b, "\n\"%s\" tid=%d\n", th.name, th.id)
		}
		for _, n := range th.notes {
			fmt.Fprintf(&b, "    NOTE: %s\n", n)
		}
		for j, f := range th.frames {
			fmt.Fprintf(&b, "      #%02d pc %016x  %s", j, f.relPC, f.file)
			if f.function != "" {
				fmt.Fprintf(&b, " (%s+%d)", f.function, f.functionOffset)
			}
			if f.buildID != "" {
				fmt.Fprintf(&b, " (BuildId: %s)", f.buildID)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}
