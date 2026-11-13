package sqlite

import (
	"encoding/binary"
	"math"
	"time"
)

// The calls csrc/asq.h describes.
const (
	opOpen      = 1
	opClose     = 2
	opExec      = 3
	opQuery     = 4
	opNext      = 5
	opCloseRows = 6
	opTake      = 7
	opInterrupt = 8
	opSerialize = 9
	opRestore   = 10

	flagReads = 1
)

// library is SQLite on this platform: asq_call, and what it can do.
type library interface {
	// call is asq_call: its answer's length, or minus the length of one
	// kept for opTake.
	call(op, conn, arg uintptr, in, out []byte) int
	// conns is how many connections a database file has: readers beside a
	// writer (write-ahead logging), or one.
	conns() int
	// wal reports whether databases use write-ahead logging.
	wal() bool
	// waits reports whether SQLite waits out another connection's lock
	// itself (its busy timeout); if not, a statement that finds the
	// database locked is run again from Go.
	waits() bool
}

// conn is a connection: its handle, and its buffers, reused call to call.
type conn struct {
	lib   library
	d     *DB
	h     uintptr
	in    []byte
	out   []byte
	reads map[string]bool // recording: the tables read
}

// The answer buffer's first size: big enough for most queries' rows.
const outSize = 64 << 10

// bigBuffer is the size past which a buffer isn't kept for the next call.
const bigBuffer = 1 << 20

// answer reads a call's answer.
type answer struct {
	b []byte
	p int
}

func (a *answer) u8() byte {
	if a.p >= len(a.b) {
		return 0
	}
	v := a.b[a.p]
	a.p++
	return v
}

func (a *answer) u32() uint32 {
	if a.p+4 > len(a.b) {
		a.p = len(a.b)
		return 0
	}
	v := binary.LittleEndian.Uint32(a.b[a.p:])
	a.p += 4
	return v
}

func (a *answer) u64() uint64 {
	if a.p+8 > len(a.b) {
		a.p = len(a.b)
		return 0
	}
	v := binary.LittleEndian.Uint64(a.b[a.p:])
	a.p += 8
	return v
}

func (a *answer) bytes() []byte {
	n := int(a.u32())
	if a.p+n > len(a.b) || n < 0 {
		a.p = len(a.b)
		return nil
	}
	v := a.b[a.p : a.p+n : a.p+n]
	a.p += n
	return v
}

func (a *answer) string() string { return string(a.bytes()) }

// openConn opens a connection to path.
func openConn(lib library, d *DB, path string, busy time.Duration) (*conn, error) {
	c := &conn{lib: lib, d: d, out: make([]byte, outSize)}
	in := binary.LittleEndian.AppendUint32(nil, uint32(busy/time.Millisecond))
	in = appendString(in, path)
	a, err := c.do(opOpen, 0, in)
	if err != nil {
		return nil, err
	}
	c.h = uintptr(a.u64())
	return c, nil
}

// do makes a call and reads its answer's header and tables: an error
// SQLite returned, or the op's own answer to read.
func (c *conn) do(op, arg uintptr, in []byte) (answer, error) {
	if len(c.out) > bigBuffer {
		// The last answer was a big one, read by now: back to the usual
		// buffer, rather than keep its size for the connection's life.
		c.out = make([]byte, outSize)
	}
	n := c.lib.call(op, c.h, arg, in, c.out)
	if n < 0 {
		// Too long for the buffer: SQLite kept it.
		c.out = make([]byte, -n+outSize)
		if op == opOpen {
			n = c.lib.call(op, c.h, arg, in, c.out)
		} else {
			n = c.lib.call(opTake, c.h, 0, nil, c.out)
		}
		if n < 0 {
			return answer{}, &Error{Code: codeMisuse, Message: "sqlite: answer lost"}
		}
	}
	a := answer{b: c.out[:n]}
	ok := a.u8() == 1
	tables := int(a.u32())
	if tables < 5 || tables > n {
		return answer{}, &Error{Code: codeMisuse, Message: "sqlite: bad answer"}
	}
	t := answer{b: a.b, p: tables}
	if w := t.u32(); w > 0 && c.d != nil {
		written := make([]string, w)
		for i := range written {
			written[i] = t.string()
		}
		c.d.wrote(written)
	}
	if r := t.u32(); r > 0 && c.reads != nil {
		for range r {
			c.reads[string(t.bytes())] = true
		}
	}
	a.b = a.b[:tables]
	if !ok {
		code := int(a.u32())
		autocommit := a.u8() == 1
		return answer{}, &Error{Code: code, Message: a.string(), autocommit: autocommit}
	}
	return a, nil
}

// request encodes a statement's call: flags, the SQL, the arguments.
func (c *conn) request(flags byte, query string, args []any) ([]byte, error) {
	if cap(c.in) > bigBuffer {
		c.in = nil
	}
	b := append(c.in[:0], flags)
	b = appendString(b, query)
	b, err := appendArgs(b, args)
	c.in = b
	return b, err
}

func appendString(b []byte, s string) []byte {
	b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}

func (c *conn) flags() byte {
	if c.reads != nil {
		return flagReads
	}
	return 0
}

// retry calls f again while the database is locked by another connection
// of this process, where SQLite can't wait itself (the web: a page has
// one thread), and the statement may run again (outside a transaction).
func (c *conn) retry(f func() (answer, error)) (answer, error) {
	if c.lib.waits() {
		return f()
	}
	start := time.Now()
	wait := time.Millisecond
	for {
		a, err := f()
		e, ok := err.(*Error)
		if !ok || e.Code&0xff != codeBusy || !e.autocommit || time.Since(start) >= c.d.busy {
			return a, err
		}
		time.Sleep(wait)
		wait = min(2*wait, 100*time.Millisecond)
	}
}

// exec runs a statement, or several (a script).
func (c *conn) exec(query string, args []any) (Changes, error) {
	in, err := c.request(c.flags(), query, args)
	if err != nil {
		return Changes{}, err
	}
	a, err := c.retry(func() (answer, error) { return c.do(opExec, 0, in) })
	if err != nil {
		return Changes{}, err
	}
	return Changes{LastInsertID: int64(a.u64()), RowsAffected: int64(a.u64())}, nil
}

// rows are a query's rows, a chunk at a time.
type rows struct {
	c      *conn
	cursor uint32
	cols   [][]byte // names, in the first answer's buffer until it's reused
	left   int      // rows left in the chunk
	done   bool
	a      answer
}

// query runs a query and returns its first chunk of rows. The columns'
// names are valid until next is called.
func (c *conn) query(query string, args []any, r *rows) error {
	in, err := c.request(c.flags(), query, args)
	if err != nil {
		return err
	}
	a, err := c.retry(func() (answer, error) { return c.do(opQuery, 0, in) })
	if err != nil {
		return err
	}
	*r = rows{c: c, cursor: a.u32()}
	n := int(a.u32())
	r.cols = make([][]byte, n)
	for i := range n {
		r.cols[i] = a.bytes()
		a.bytes() // its declared type
	}
	r.chunk(a)
	return nil
}

func (r *rows) chunk(a answer) {
	r.done = a.u8() == 1
	r.left = int(a.u32())
	r.a = a
}

// next makes the next row's values readable from r.a; false at the end.
func (r *rows) next() (bool, error) {
	for r.left == 0 {
		if r.done {
			return false, nil
		}
		a, err := r.c.do(opNext, uintptr(r.cursor), []byte{r.c.flags()})
		if err != nil {
			r.done = true
			return false, err
		}
		r.chunk(a)
	}
	r.left--
	return true, nil
}

// close closes the rows SQLite still has (none once they're all read).
func (r *rows) close() {
	if !r.done {
		r.done = true
		r.c.do(opCloseRows, uintptr(r.cursor), []byte{0})
	}
}

// value reads a column's value: its type (0 null, 1 integer, 2 real,
// 3 text, 4 blob), and it.
func (a *answer) value() (typ byte, i int64, f float64, b []byte) {
	typ = a.u8()
	switch typ {
	case 1:
		i = int64(a.u64())
	case 2:
		f = math.Float64frombits(a.u64())
	case 3, 4:
		b = a.bytes()
	}
	return
}

// skipValue skips a column's value.
func (a *answer) skipValue() {
	switch a.u8() {
	case 1, 2:
		a.p += 8
	case 3, 4:
		n := int(a.u32())
		a.p += n
	}
}

func (c *conn) close() {
	if c.h != 0 {
		c.lib.call(opClose, c.h, 0, nil, c.out)
		c.h = 0
	}
}
