package native

import (
	"encoding/binary"
	"errors"
	"math"
	"sync"
	"time"
)

// The wire format of typed native modules (//asuka:native) and views
// (//asuka:view): values in declaration order, little-endian, each
// language's side generated from one declaration, so neither needs
// reflection. Booleans and integers are their size (int and uint: 8
// bytes); floats are IEEE 754; strings and []byte are a u32 length and
// their bytes; time.Time is i64 seconds and i32 nanoseconds since the
// Unix epoch; time.Duration is i64 nanoseconds; a slice is a u32 count
// and its elements; a map[string]T a u32 count and its keys and values; a
// *T a u8 (0: nil) and, when set, its value; a struct its fields; a
// native object a u64 handle (0: nil); a func a u64 callback.

// errShort is a Reader's error for an answer shorter than what it reads.
var errShort = errors.New("native: malformed answer")

// Writer encodes values. Generated code gets one with NewWriter; the call
// it's passed to takes it back.
type Writer struct{ b []byte }

var writers = sync.Pool{New: func() any { return &Writer{b: make([]byte, 0, 256)} }}

// NewWriter returns an empty Writer from the pool.
func NewWriter() *Writer {
	w := writers.Get().(*Writer)
	w.b = w.b[:0]
	return w
}

// free returns w to the pool, unless it grew large.
func (w *Writer) free() {
	if cap(w.b) <= 64<<10 {
		writers.Put(w)
	}
}

// Bytes is what w holds.
func (w *Writer) Bytes() []byte { return w.b }

func (w *Writer) Bool(v bool) {
	if v {
		w.b = append(w.b, 1)
	} else {
		w.b = append(w.b, 0)
	}
}
func (w *Writer) Int8(v int8)   { w.b = append(w.b, byte(v)) }
func (w *Writer) Uint8(v uint8) { w.b = append(w.b, v) }
func (w *Writer) Int16(v int16) { w.b = binary.LittleEndian.AppendUint16(w.b, uint16(v)) }
func (w *Writer) Uint16(v uint16) {
	w.b = binary.LittleEndian.AppendUint16(w.b, v)
}
func (w *Writer) Int32(v int32) { w.b = binary.LittleEndian.AppendUint32(w.b, uint32(v)) }
func (w *Writer) Uint32(v uint32) {
	w.b = binary.LittleEndian.AppendUint32(w.b, v)
}
func (w *Writer) Int64(v int64) { w.b = binary.LittleEndian.AppendUint64(w.b, uint64(v)) }
func (w *Writer) Uint64(v uint64) {
	w.b = binary.LittleEndian.AppendUint64(w.b, v)
}
func (w *Writer) Int(v int)   { w.Int64(int64(v)) }
func (w *Writer) Uint(v uint) { w.Uint64(uint64(v)) }
func (w *Writer) Float32(v float32) {
	w.b = binary.LittleEndian.AppendUint32(w.b, math.Float32bits(v))
}
func (w *Writer) Float64(v float64) {
	w.b = binary.LittleEndian.AppendUint64(w.b, math.Float64bits(v))
}
func (w *Writer) String(s string) {
	w.Uint32(uint32(len(s)))
	w.b = append(w.b, s...)
}

// Data writes a []byte.
func (w *Writer) Data(b []byte) {
	w.Uint32(uint32(len(b)))
	w.b = append(w.b, b...)
}

func (w *Writer) Time(t time.Time) {
	w.Int64(t.Unix())
	w.Int32(int32(t.Nanosecond()))
}
func (w *Writer) Duration(d time.Duration) { w.Int64(int64(d)) }

// Len writes a slice's or a map's length.
func (w *Writer) Len(n int) { w.Uint32(uint32(n)) }

// Present writes whether a *T is set.
func (w *Writer) Present(set bool) { w.Bool(set) }

// Object writes a native object (nil: 0).
func (w *Writer) Object(o *Object) {
	if o == nil {
		w.Uint64(0)
		return
	}
	w.Uint64(o.handle)
}

// Func writes a callback (nil: 0, which the native side calls as a
// no-op).
func (w *Writer) Func(c *Callback) {
	if c == nil {
		w.Uint64(0)
		return
	}
	w.Uint64(c.id)
}

// Take returns a copy of what w holds, and gives w back to the pool.
func (w *Writer) Take() []byte {
	b := append([]byte(nil), w.b...)
	w.free()
	return b
}

// Reader decodes values. After a short read every read returns the zero
// value, and Err says why.
type Reader struct {
	b   []byte
	p   int
	err error
	buf *[]byte // the pooled buffer b is in, if any
}

// NewReader reads b (for tests, and hosts' callbacks).
func NewReader(b []byte) *Reader { return &Reader{b: b} }

func (r *Reader) take(n int) []byte {
	if r.err != nil {
		return nil
	}
	if n < 0 || len(r.b)-r.p < n {
		r.err = errShort
		return nil
	}
	v := r.b[r.p : r.p+n]
	r.p += n
	return v
}

// Err is the first read's error past what r holds.
func (r *Reader) Err() error { return r.err }

// Done ends reading an answer: it and its buffer go back to the pool.
// It returns Err.
func (r *Reader) Done() error {
	err := r.err
	if r.buf != nil {
		*r.buf = r.b[:0]
		answers.Put(r.buf)
		r.buf = nil
		*r = Reader{}
		readers.Put(r)
	}
	return err
}

var readers = sync.Pool{New: func() any { return new(Reader) }}

// answerReader is a pooled Reader of an answer in a pooled buffer.
func answerReader(b []byte, buf *[]byte) *Reader {
	r := readers.Get().(*Reader)
	r.b, r.buf = b, buf
	return r
}

func (r *Reader) Bool() bool {
	if b := r.take(1); b != nil {
		return b[0] != 0
	}
	return false
}
func (r *Reader) Int8() int8 {
	if b := r.take(1); b != nil {
		return int8(b[0])
	}
	return 0
}
func (r *Reader) Uint8() uint8 {
	if b := r.take(1); b != nil {
		return b[0]
	}
	return 0
}
func (r *Reader) Int16() int16 { return int16(r.Uint16()) }
func (r *Reader) Uint16() uint16 {
	if b := r.take(2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}
func (r *Reader) Int32() int32 { return int32(r.Uint32()) }
func (r *Reader) Uint32() uint32 {
	if b := r.take(4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}
func (r *Reader) Int64() int64 { return int64(r.Uint64()) }
func (r *Reader) Uint64() uint64 {
	if b := r.take(8); b != nil {
		return binary.LittleEndian.Uint64(b)
	}
	return 0
}
func (r *Reader) Int() int         { return int(r.Int64()) }
func (r *Reader) Uint() uint       { return uint(r.Uint64()) }
func (r *Reader) Float32() float32 { return math.Float32frombits(r.Uint32()) }
func (r *Reader) Float64() float64 { return math.Float64frombits(r.Uint64()) }
func (r *Reader) String() string   { return string(r.take(int(r.Uint32()))) }

// Data reads a []byte, a copy of its own (nil for none, as Go's other
// empty values).
func (r *Reader) Data() []byte {
	b := r.take(int(r.Uint32()))
	if len(b) == 0 {
		return nil
	}
	return append([]byte(nil), b...)
}

func (r *Reader) Time() time.Time {
	s := r.Int64()
	ns := r.Int32()
	return time.Unix(s, int64(ns))
}
func (r *Reader) Duration() time.Duration { return time.Duration(r.Int64()) }

// Len reads a slice's or a map's length, bounded by what's left (each
// element is at least a byte), so a malformed one can't allocate much.
func (r *Reader) Len() int {
	n := int(r.Uint32())
	if r.err == nil && n > len(r.b)-r.p {
		r.err = errShort
		return 0
	}
	return n
}

// Present reads whether a *T is set.
func (r *Reader) Present() bool { return r.Bool() }

// Handle reads a native object's handle (0: nil).
func (r *Reader) Handle() uint64 { return r.Uint64() }

// Rest is what's left to read.
func (r *Reader) Rest() []byte {
	if r.err != nil {
		return nil
	}
	return r.b[r.p:]
}
