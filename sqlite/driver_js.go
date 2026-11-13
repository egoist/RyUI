//go:build js && wasm

package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"
	"unsafe"
)

// In a browser, the driver is SQLite's own WebAssembly build, which the
// page loads beside the app (platform/web/src/sqlite.js; ryui build
// -platform web ships it with the apps that import this package). Each
// call crosses into JavaScript once: its arguments read from this
// program's memory, its answer written into it. A query's rows come a
// chunk at a time. The tables a commit wrote and a query read come back
// with each answer, for the live queries, as modernc.org/sqlite's hooks
// note them elsewhere.
//
// A database file has one connection: the page has one thread, which a
// connection can't block waiting for another's lock. Two databases
// opened on one file lock each other out as two processes would: a
// statement that finds the file locked is run again once the other has
// had its turn, for as long as the busy timeout says.

// fileConns is how many connections a database file has.
const fileConns = 1

// The calls sqlite.js answers.
const (
	opOpen      = 1
	opClose     = 2
	opExec      = 3
	opQuery     = 4
	opNext      = 5
	opCloseRows = 6
	opTake      = 7
)

const codeBusy = 5

var (
	webOnce sync.Once
	webCall js.Value
	webErr  error
)

// web is the page's SQLite: the host's.
func web() (js.Value, error) {
	webOnce.Do(func() {
		name := os.Getenv("RYUI_WEB_HOST")
		if name == "" {
			name = "ryuiHost"
		}
		h := js.Global().Get(name)
		if h.IsUndefined() {
			webErr = errors.New("sqlite: no web host on this page")
			return
		}
		api := h.Get("sqlite")
		if api.IsUndefined() || api.IsNull() {
			webErr = errors.New("sqlite: the page didn't load SQLite (ryui build -platform web ships it with the apps that import ryui/sqlite; a page of your own passes start its sqlite option)")
			return
		}
		webCall = api.Get("call").Call("bind", api)
	})
	return webCall, webErr
}

func registerDriver() { sql.Register("ryui-sqlite", webDriver{}) }

type webDriver struct{}

// Open opens a connection to the DSN Open made: file:<path>?_pragma=...
func (webDriver) Open(dsn string) (driver.Conn, error) {
	call, err := web()
	if err != nil {
		return nil, err
	}
	rest := strings.TrimPrefix(dsn, "file:")
	path, query := rest, ""
	if i := strings.LastIndexByte(rest, '?'); i >= 0 {
		path, query = rest[:i], rest[i+1:]
	}
	c := &webConn{call: call, busy: 5 * time.Second}
	ans, err := c.do(opOpen, 0, appendString(nil, path), &c.out)
	if err != nil {
		return nil, err
	}
	c.id = ans.u32()
	c.st = &connState{written: map[string]bool{}}
	if v, ok := opened.Load(dsn); ok {
		c.st.d = v.(*DB)
	}
	conns.Store(c, c.st)
	q, _ := url.ParseQuery(query)
	for _, p := range q["_pragma"] {
		name, arg, _ := strings.Cut(strings.TrimSuffix(p, ")"), "(")
		switch strings.ToLower(name) {
		case "journal_mode":
			// The rollback journal: write-ahead logging needs memory shared
			// between connections, which a page's file system hasn't.
			continue
		case "busy_timeout":
			// Waited out in Go (busy), not in SQLite, which can't let the
			// other connection run.
			if ms, err := strconv.Atoi(arg); err == nil {
				c.busy = time.Duration(ms) * time.Millisecond
			}
			continue
		}
		if _, err := c.exec("pragma "+name+" = "+arg, nil); err != nil {
			c.Close()
			return nil, err
		}
	}
	return c, nil
}

// webConn is a connection: its id in sqlite.js, and its buffers.
type webConn struct {
	call   js.Value
	id     uint32
	st     *connState
	busy   time.Duration
	in     []byte // the call's arguments
	out    []byte // an answer
	closed bool
}

// sqliteError is an error SQLite returned, as modernc.org/sqlite's Error
// (its message, and its Code).
type sqliteError struct {
	msg        string
	code       int
	autocommit bool // the connection isn't in a transaction
}

func (e *sqliteError) Error() string { return e.msg }
func (e *sqliteError) Code() int     { return e.code }

// answer is an answer's body.
type answer struct {
	b []byte
	p int
}

func (a *answer) u8() byte { v := a.b[a.p]; a.p++; return v }

func (a *answer) u32() uint32 {
	v := binary.LittleEndian.Uint32(a.b[a.p:])
	a.p += 4
	return v
}

func (a *answer) u64() uint64 {
	v := binary.LittleEndian.Uint64(a.b[a.p:])
	a.p += 8
	return v
}

func (a *answer) bytes() []byte {
	n := int(a.u32())
	v := a.b[a.p : a.p+n]
	a.p += n
	return v
}

func (a *answer) string() string { return string(a.bytes()) }

// do makes a call: in its arguments; the answer in *out (grown to fit).
// It notes the tables the answer says were written and read.
func (c *webConn) do(op int, arg uint32, in []byte, out *[]byte) (answer, error) {
	if cap(*out) < 4096 {
		*out = make([]byte, 4096)
	}
	b := (*out)[:cap(*out)]
	n := c.call.Invoke(op, c.id, arg, addr(in), len(in), addr(b), len(b)).Int()
	if n < 0 {
		b = make([]byte, -n)
		*out = b
		n = c.call.Invoke(opTake, 0, 0, 0, 0, addr(b), len(b)).Int()
	}
	runtime.KeepAlive(in)
	runtime.KeepAlive(b)
	a := answer{b: b[:n]}
	ok := a.u8() == 1
	if w := a.u32(); w > 0 {
		tables := make(map[string]bool, w)
		for range w {
			tables[a.string()] = true
		}
		if c.st != nil && c.st.d != nil {
			c.st.d.wrote(tables)
		}
	}
	for r := a.u32(); r > 0; r-- {
		t := a.string()
		if c.st != nil && c.st.reads != nil {
			c.st.reads[t] = true
		}
	}
	if !ok {
		code := int(a.u32())
		auto := a.u8() == 1
		return a, &sqliteError{msg: a.string(), code: code, autocommit: auto}
	}
	return a, nil
}

func addr(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	return int(uintptr(unsafe.Pointer(&b[0])))
}

// flags are a call's: 1, record the tables read.
func (c *webConn) flags() byte {
	if c.st != nil && c.st.reads != nil {
		return 1
	}
	return 0
}

// request encodes a statement's call: flags, the SQL, the arguments.
func (c *webConn) request(query string, args []driver.NamedValue) ([]byte, error) {
	b := append(c.in[:0], c.flags())
	b = appendString(b, query)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(args)))
	for _, a := range args {
		b = appendString(b, a.Name)
		switch v := a.Value.(type) {
		case nil:
			b = append(b, 0)
		case int64:
			b = binary.LittleEndian.AppendUint64(append(b, 1), uint64(v))
		case bool:
			n := uint64(0)
			if v {
				n = 1
			}
			b = binary.LittleEndian.AppendUint64(append(b, 1), n)
		case float64:
			b = binary.LittleEndian.AppendUint64(append(b, 2), math.Float64bits(v))
		case string:
			b = appendString(append(b, 3), v)
		case []byte:
			if v == nil {
				b = append(b, 0)
			} else {
				b = appendString(append(b, 4), string(v))
			}
		case time.Time:
			// As modernc.org/sqlite writes it.
			b = appendString(append(b, 3), v.String())
		default:
			return nil, fmt.Errorf("sqlite: unsupported type %T", v)
		}
	}
	c.in = b
	return b, nil
}

func appendString(b []byte, s string) []byte {
	b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}

// retry calls f again while the database is locked by another connection
// and the statement may run again (outside a transaction, or a commit),
// letting the other run meanwhile, for as long as the busy timeout.
func (c *webConn) retry(commit bool, f func() (answer, error)) (answer, error) {
	start := time.Now()
	wait := time.Millisecond
	for {
		a, err := f()
		var e *sqliteError
		if err == nil || !errors.As(err, &e) || e.code&0xff != codeBusy || !commit && !e.autocommit || time.Since(start) >= c.busy {
			return a, err
		}
		time.Sleep(wait)
		wait = min(2*wait, 100*time.Millisecond)
	}
}

func (c *webConn) exec(query string, args []driver.NamedValue) (driver.Result, error) {
	commit := strings.EqualFold(strings.TrimSpace(query), "commit")
	a, err := c.retry(commit, func() (answer, error) {
		in, err := c.request(query, args)
		if err != nil {
			return answer{}, err
		}
		return c.do(opExec, 0, in, &c.out)
	})
	if err != nil {
		return nil, err
	}
	return webResult{int64(a.u64()), int64(a.u64())}, nil
}

func (c *webConn) query(query string, args []driver.NamedValue) (driver.Rows, error) {
	r := &webRows{c: c}
	a, err := c.retry(false, func() (answer, error) {
		in, err := c.request(query, args)
		if err != nil {
			return answer{}, err
		}
		return c.do(opQuery, 0, in, &r.buf)
	})
	if err != nil {
		return nil, err
	}
	r.cursor = a.u32()
	n := int(a.u32())
	r.cols = make([]string, n)
	r.decl = make([]string, n)
	for i := range n {
		r.cols[i] = a.string()
		r.decl[i] = strings.ToUpper(a.string())
	}
	r.chunk(a)
	return r, nil
}

// ExecContext implements driver.ExecerContext.
func (c *webConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.exec(query, args)
}

// QueryContext implements driver.QueryerContext.
func (c *webConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.query(query, args)
}

// Prepare implements driver.Conn: sqlite.js prepares each SQL once, and
// keeps its statement for the next time.
func (c *webConn) Prepare(query string) (driver.Stmt, error) { return &webStmt{c, query}, nil }

// Close implements driver.Conn.
func (c *webConn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	conns.Delete(c)
	_, err := c.do(opClose, 0, nil, &c.out)
	return err
}

// Begin implements driver.Conn.
func (c *webConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

// BeginTx implements driver.ConnBeginTx.
func (c *webConn) BeginTx(ctx context.Context, _ driver.TxOptions) (driver.Tx, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if _, err := c.exec("begin", nil); err != nil {
		return nil, err
	}
	return webTx{c}, nil
}

// IsValid implements driver.Validator.
func (c *webConn) IsValid() bool { return !c.closed }

type webTx struct{ c *webConn }

func (t webTx) Commit() error {
	_, err := t.c.exec("commit", nil)
	if err != nil {
		// Still in the transaction (a commit SQLITE_BUSY ended): rolled
		// back, as database/sql expects the connection to be done with it.
		t.c.exec("rollback", nil)
	}
	return err
}

func (t webTx) Rollback() error {
	_, err := t.c.exec("rollback", nil)
	return err
}

type webResult struct{ id, changes int64 }

func (r webResult) LastInsertId() (int64, error) { return r.id, nil }
func (r webResult) RowsAffected() (int64, error) { return r.changes, nil }

type webStmt struct {
	c     *webConn
	query string
}

func (s *webStmt) Close() error  { return nil }
func (s *webStmt) NumInput() int { return -1 }

func (s *webStmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.c.exec(s.query, named(args))
}

func (s *webStmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.c.query(s.query, named(args))
}

func (s *webStmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	return s.c.ExecContext(ctx, s.query, args)
}

func (s *webStmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	return s.c.QueryContext(ctx, s.query, args)
}

func named(args []driver.Value) []driver.NamedValue {
	out := make([]driver.NamedValue, len(args))
	for i, v := range args {
		out[i] = driver.NamedValue{Ordinal: i + 1, Value: v}
	}
	return out
}

// webRows are a query's rows, a chunk at a time.
type webRows struct {
	c          *webConn
	cursor     uint32
	cols, decl []string
	buf        []byte // the chunk's answer
	a          answer
	left       int // rows left in the chunk
	done       bool
	closed     bool
}

func (r *webRows) chunk(a answer) {
	r.done = a.u8() == 1
	r.left = int(a.u32())
	r.a = a
}

func (r *webRows) Columns() []string { return r.cols }

// ColumnTypeDatabaseTypeName implements driver.RowsColumnTypeDatabaseTypeName.
func (r *webRows) ColumnTypeDatabaseTypeName(i int) string { return r.decl[i] }

func (r *webRows) Next(dest []driver.Value) error {
	for r.left == 0 {
		if r.done || r.closed {
			return io.EOF
		}
		a, err := r.c.do(opNext, r.cursor, []byte{r.c.flags()}, &r.buf)
		if err != nil {
			r.done = true
			return err
		}
		r.chunk(a)
	}
	r.left--
	a := &r.a
	for i := range dest {
		switch a.u8() {
		case 0:
			dest[i] = nil
		case 1:
			dest[i] = int64(a.u64())
		case 2:
			dest[i] = math.Float64frombits(a.u64())
		case 3:
			s := a.string()
			switch r.decl[i] {
			case "DATE", "DATETIME", "TIMESTAMP":
				dest[i] = parseTime(s)
			default:
				dest[i] = s
			}
		case 4:
			dest[i] = append([]byte{}, a.bytes()...)
		}
	}
	return nil
}

func (r *webRows) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.done || r.c.closed {
		return nil
	}
	_, err := r.c.do(opCloseRows, r.cursor, []byte{0}, &r.buf)
	return err
}

// parseTimeFormats are modernc.org/sqlite's, which reads a date column's
// text as a time if it can.
var parseTimeFormats = []string{
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04",
	"2006-01-02T15:04",
	"2006-01-02",
}

// parseTime is s as a time, as modernc.org/sqlite reads it (time.Time's
// String, as it writes times, among them), or s.
func parseTime(s string) any {
	t := s
	if i := strings.Index(t, "m="); i > 0 {
		t = t[:i]
	}
	if v, err := time.Parse("2006-01-02 15:04:05.999999999 -0700 MST", strings.TrimSpace(t)); err == nil {
		return v
	}
	ts := strings.TrimSuffix(s, "Z")
	for _, f := range parseTimeFormats {
		if v, err := time.Parse(f, ts); err == nil {
			return v
		}
	}
	return s
}
