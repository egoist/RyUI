// Package sqlite is an app's SQLite database: a file in its data
// directory, opened with the settings an app wants (write-ahead logging,
// foreign keys, a busy timeout), queries that scan into structs, a
// transaction helper, versioned migrations, writes off the main thread
// (ExecAsync, TxAsync), and live queries: signals that run their query
// again, off the main thread, when a table it reads is written.
//
// SQLite is SQLite's own C, one version built the same way for every
// platform (internal/sqlitebuild), with a small library of Asuka's
// (csrc/asq.c) that runs a whole statement, and returns its rows a chunk
// at a time, in one call from Go: through purego on macOS, Linux, iOS
// and Android, syscall on Windows, and JavaScript in a browser, where
// it's built to WebAssembly. No cgo.
//
//	db, err := sqlite.Open("notes.db")
//	db.Migrate(
//		`create table notes (id integer primary key, text text not null, done bool not null default 0)`,
//	)
//	db.ExecAsync(`insert into notes (text) values (?)`, []any{"Buy milk"}, nil)
//	notes := sqlite.Watch[Note](db, `select * from notes order by id`)
//
// Exec, Tx and Query block: on the main thread, use ExecAsync and
// TxAsync, and Watch, which fetch off it (or a goroutine, then ui.Post).
// A live query learns the tables it reads (views' too) as SQLite prepares
// it, and runs again when a write to one of them commits, including the
// rows triggers and foreign keys' actions write.
package sqlite

//go:generate go run ./internal/build

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/asuka/sdk/store"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// DB is an open database: one connection that writes, and, for a file,
// a few that read beside it (write-ahead logging).
type DB struct {
	lib  library
	path string
	busy time.Duration

	writer  *conn
	wlock   chan struct{} // held while the writer is in use
	readers chan *conn    // idle readers; nil: the writer reads too
	nreader int           // readers opened; under mu
	closed  atomic.Bool

	mu      sync.Mutex
	watches map[*watch]bool
	dirty   map[string]bool // the tables written since the live queries last heard
}

// Changes is what a statement did.
type Changes struct {
	// LastInsertID is the rowid of the row the connection last inserted.
	LastInsertID int64
	// RowsAffected is how many rows the statement inserted, updated or
	// deleted.
	RowsAffected int64
}

// LastInsertId is LastInsertID, as database/sql's Result has it.
func (c Changes) LastInsertId() (int64, error) { return c.LastInsertID, nil }

// The busy timeout: how long a statement waits for another connection's
// lock (another process's, or this one's writer) before ErrBusy.
const busyTimeout = 5 * time.Second

var (
	libOnce sync.Once
	theLib  library
	libErr  error
)

// Open opens (creating it if need be) the database name: a file in the
// app's data directory (store.Dir), an absolute path, or ":memory:".
//
// Never inlined: asuka build looks for its symbol (cmd/asuka), to ship
// SQLite's library with the apps that use it.
//
//go:noinline
func Open(name string) (*DB, error) {
	libOnce.Do(func() { theLib, libErr = loadLibrary() })
	if libErr != nil {
		return nil, fmt.Errorf("sqlite: %w", libErr)
	}
	path := name
	memory := name == ":memory:"
	if !memory && !filepath.IsAbs(name) {
		dir := store.Dir()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		path = filepath.Join(dir, name)
	}
	d := &DB{lib: theLib, path: path, busy: busyTimeout, wlock: make(chan struct{}, 1),
		watches: map[*watch]bool{}, dirty: map[string]bool{}}
	w, err := d.open(true)
	if err != nil {
		return nil, err
	}
	d.writer = w
	if n := theLib.conns(); !memory && n > 1 {
		// :memory: has one connection: each would have its own database.
		d.readers = make(chan *conn, n-1)
	}
	return d, nil
}

// open opens a connection, with the database's settings.
func (d *DB) open(writer bool) (*conn, error) {
	busy := d.busy
	if !d.lib.waits() {
		busy = 0
	}
	c, err := openConn(d.lib, d, d.path, busy)
	if err != nil {
		return nil, err
	}
	pragmas := []string{"pragma foreign_keys = on"}
	if writer && d.path != ":memory:" && d.lib.wal() {
		pragmas = append(pragmas, "pragma journal_mode = wal", "pragma synchronous = normal")
	}
	for _, p := range pragmas {
		if _, err := c.exec(p, nil); err != nil {
			c.close()
			return nil, err
		}
	}
	return c, nil
}

// lockWriter takes the writer, waiting for it as SQLite waits for a lock:
// ErrBusy after the busy timeout.
func (d *DB) lockWriter() error {
	if blocked != nil {
		if err := blocked(); err != nil {
			return err
		}
	}
	if d.closed.Load() {
		return errClosed
	}
	select {
	case d.wlock <- struct{}{}:
	default:
		t := time.NewTimer(d.busy)
		defer t.Stop()
		select {
		case d.wlock <- struct{}{}:
		case <-t.C:
			return &Error{Code: codeBusy, Message: "database is locked (by this app's other write)"}
		}
	}
	if d.closed.Load() {
		<-d.wlock
		return errClosed
	}
	return nil
}

func (d *DB) unlockWriter() { <-d.wlock }

var errClosed = &Error{Code: codeMisuse, Message: "database is closed"}

// blocked, where it's set (the web), returns the error a call that would
// wait gets where it can't wait (the page's callbacks), before it waits
// for anything.
var blocked func() error

// reader takes a connection to read on: an idle reader, a new one, or,
// with none, the writer. release gives it back.
func (d *DB) reader() (c *conn, release func(), err error) {
	if d.readers == nil {
		if err := d.lockWriter(); err != nil {
			return nil, nil, err
		}
		return d.writer, d.unlockWriter, nil
	}
	if d.closed.Load() {
		return nil, nil, errClosed
	}
	select {
	case c = <-d.readers:
	default:
		d.mu.Lock()
		more := d.nreader < cap(d.readers)
		if more {
			d.nreader++
		}
		d.mu.Unlock()
		if more {
			if c, err = d.open(false); err != nil {
				d.mu.Lock()
				d.nreader--
				d.mu.Unlock()
				return nil, nil, err
			}
		} else {
			c = <-d.readers
		}
	}
	return c, func() { d.putReader(c) }, nil
}

func (d *DB) putReader(c *conn) {
	if d.closed.Load() {
		c.close()
		return
	}
	d.readers <- c
}

// wrote notes tables a commit wrote; the live queries hear of them when
// the call returns (the commit is done then: a query run at once from
// SQLite's hook might not see it).
func (d *DB) wrote(tables []string) {
	d.mu.Lock()
	for _, t := range tables {
		d.dirty[t] = true
	}
	d.mu.Unlock()
}

// Close closes the database, once what runs on it is done.
func (d *DB) Close() error {
	if err := d.lockWriter(); err != nil {
		if err == errClosed {
			return nil
		}
		return err
	}
	d.closed.Store(true)
	if d.readers != nil {
	drain:
		for {
			select {
			case c := <-d.readers:
				c.close()
			default:
				break drain
			}
		}
	}
	d.writer.close()
	d.unlockWriter()
	return nil
}

// Exec runs a statement (insert, update, delete, create...), or several
// separated by semicolons, and updates the live queries.
func (d *DB) Exec(query string, args ...any) (Changes, error) {
	if err := d.lockWriter(); err != nil {
		return Changes{}, err
	}
	r, err := d.writer.exec(query, args)
	d.unlockWriter()
	d.notify()
	return r, err
}

// ExecAsync runs a statement off the main thread, then calls done (if
// it isn't nil) on the main thread; with no done, an error is logged.
func (d *DB) ExecAsync(query string, args []any, done func(Changes, error)) {
	go func() {
		r, err := d.Exec(query, args...)
		if done != nil {
			ui.Post(func() { done(r, err) })
		} else if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
}

// TxAsync runs Tx off the main thread, then calls done (if it isn't nil)
// on the main thread with its error; with no done, an error is logged.
func (d *DB) TxAsync(f func(tx *Tx) error, done func(error)) {
	go func() {
		err := d.Tx(f)
		if done != nil {
			ui.Post(func() { done(err) })
		} else if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
}

// Tx runs f in a transaction: committed if f returns nil, rolled back if
// it returns an error (which Tx returns) or panics. It holds the writer:
// the database's other writes wait for it (reads don't).
func (d *DB) Tx(f func(tx *Tx) error) (err error) {
	if err := d.lockWriter(); err != nil {
		return err
	}
	defer d.unlockWriter()
	c := d.writer
	if _, err := c.exec("begin immediate", nil); err != nil {
		return err
	}
	tx := &Tx{c: c}
	defer func() {
		tx.done = true
		if p := recover(); p != nil {
			c.exec("rollback", nil)
			panic(p)
		}
		if err != nil {
			c.exec("rollback", nil)
			return
		}
		if _, err = c.exec("commit", nil); err != nil {
			c.exec("rollback", nil)
		}
		// After unlocking: a live query reads on another connection.
		defer d.notify()
	}()
	return f(tx)
}

// Tx is a transaction. Use it only in the function given Tx.
type Tx struct {
	c    *conn
	done bool
}

// Exec runs a statement in the transaction.
func (t *Tx) Exec(query string, args ...any) (Changes, error) {
	if t.done {
		return Changes{}, &Error{Code: codeMisuse, Message: "transaction is done"}
	}
	return t.c.exec(query, args)
}

// Migrate brings the schema up to date: steps are the database's
// versions in order, each SQL (statements separated by semicolons) run
// once, in a transaction, recorded in PRAGMA user_version. Append steps;
// never change one that has shipped.
func (d *DB) Migrate(steps ...string) error {
	v, _, err := QueryOne[int](d, `pragma user_version`)
	if err != nil {
		return err
	}
	for i := v; i < len(steps); i++ {
		err := d.Tx(func(tx *Tx) error {
			if _, err := tx.Exec(steps[i]); err != nil {
				return fmt.Errorf("sqlite: migration %d: %w", i+1, err)
			}
			_, err := tx.Exec(fmt.Sprintf(`pragma user_version = %d`, i+1))
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// Serialize is the database's bytes, as its file would hold them: a
// backup to write anywhere, or to give Restore.
func (d *DB) Serialize() ([]byte, error) {
	c, release, err := d.reader()
	if err != nil {
		return nil, err
	}
	defer release()
	a, err := c.do(opSerialize, 0, []byte{0})
	if err != nil {
		return nil, err
	}
	n := a.u64()
	if uint64(len(a.b)-a.p) < n {
		return nil, &Error{Code: codeMisuse, Message: "bad answer"}
	}
	return append([]byte(nil), a.b[a.p:a.p+int(n)]...), nil
}

// Restore replaces the database with one Serialize made, and runs every
// live query again.
func (d *DB) Restore(data []byte) error {
	if err := d.lockWriter(); err != nil {
		return err
	}
	in := append(d.writer.in[:0], 0)
	in = append(in, make([]byte, 8)...)
	putU64(in[1:], uint64(len(data)))
	in = append(in, data...)
	_, err := d.writer.do(opRestore, 0, in)
	d.writer.in = in[:0]
	d.unlockWriter()
	d.Changed()
	return err
}

func putU64(b []byte, v uint64) {
	for i := range 8 {
		b[i] = byte(v >> (8 * i))
	}
}

// querier is what Query reads from: a DB or a Tx.
type querier interface {
	acquire() (*conn, func(), error)
	db() *DB
}

func (d *DB) acquire() (*conn, func(), error) { return d.reader() }
func (d *DB) db() *DB                         { return d }

func (t *Tx) acquire() (*conn, func(), error) {
	if t.done {
		return nil, nil, &Error{Code: codeMisuse, Message: "transaction is done"}
	}
	return t.c, func() {}, nil
}
func (t *Tx) db() *DB { return t.c.d }

// Query runs a query and scans its rows into Ts: a struct's fields by
// column name (a field's `db` tag, or its name, case-insensitively and
// without the column's underscores; columns with no field are skipped;
// embedded structs' fields too), or, for any other T, the first column.
//
// A column's value converts to its field's type as it can: an integer to
// a string field as its digits, text to an int field if it's one; NULL
// to the zero value, or nil for a pointer field. A time.Time field reads
// text (TimeFormat, SQLite's date and time functions' and RFC 3339), or
// seconds since 1970. A field whose pointer is a Scanner (sql.NullString)
// scans the value itself.
func Query[T any](from querier, query string, args ...any) ([]T, error) {
	return queryN[T](from, query, args, 0)
}

// QueryOne is Query's first row; ok is false if there's none.
func QueryOne[T any](from querier, query string, args ...any) (v T, ok bool, err error) {
	rows, err := queryN[T](from, query, args, 1)
	if err != nil || len(rows) == 0 {
		return v, false, err
	}
	return rows[0], true, nil
}

func queryN[T any](from querier, query string, args []any, max int) ([]T, error) {
	c, release, err := from.acquire()
	if err != nil {
		return nil, err
	}
	out, err := queryOn[T](c, query, args, max)
	release()
	if _, inTx := from.(*Tx); !inTx {
		// A query may write (insert ... returning).
		from.db().notify()
	}
	return out, err
}

func queryOn[T any](c *conn, query string, args []any, max int) ([]T, error) {
	var r rows
	if err := c.query(query, args, &r); err != nil {
		return nil, err
	}
	out, err := scan[T](&r, max)
	r.close()
	return out, err
}

// Changed runs every live query again: for a change the database's
// hooks can't see (another process's, a file replaced).
func (d *DB) Changed() {
	d.mu.Lock()
	clear(d.dirty)
	ws := make([]*watch, 0, len(d.watches))
	for w := range d.watches {
		ws = append(ws, w)
	}
	d.mu.Unlock()
	for _, w := range ws {
		w.run()
	}
}

// notify runs the live queries that read a table written since they
// last heard (and those whose tables aren't known yet) again.
func (d *DB) notify() {
	d.mu.Lock()
	if len(d.dirty) == 0 {
		d.mu.Unlock()
		return
	}
	var ws []*watch
	for w := range d.watches {
		if w.reads(d.dirty) {
			ws = append(ws, w)
		}
	}
	clear(d.dirty)
	d.mu.Unlock()
	for _, w := range ws {
		w.run()
	}
}

// watch is a live query.
type watch struct {
	mu      sync.Mutex
	running bool
	again   bool
	fetch   func()
	query   string
	tables  map[string]bool // what its query read last; nil: not known
	fetches int             // how many times it fetched (for tests)
}

// reads reports whether w's query reads one of tables (or may: its
// tables aren't known).
func (w *watch) reads(tables map[string]bool) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.tables) == 0 {
		return true
	}
	for t := range tables {
		if w.tables[t] {
			return true
		}
	}
	return false
}

// queryReading runs Query noting the tables it reads (SQLite's
// authorizer tells them as it prepares the query: a view's tables too).
func queryReading[T any](d *DB, query string, args []any) ([]T, map[string]bool, error) {
	c, release, err := d.reader()
	if err != nil {
		return nil, nil, err
	}
	tables := map[string]bool{}
	c.reads = tables
	rows, err := queryOn[T](c, query, args, 0)
	c.reads = nil
	release()
	d.notify()
	return rows, tables, err
}

// run fetches again, off the main thread: once more after one in flight,
// however many changes come meanwhile.
func (w *watch) run() {
	w.mu.Lock()
	if w.running {
		w.again = true
		w.mu.Unlock()
		return
	}
	w.running = true
	w.mu.Unlock()
	go func() {
		for {
			w.fetch()
			w.mu.Lock()
			if !w.again {
				w.running = false
				w.mu.Unlock()
				return
			}
			w.again = false
			w.mu.Unlock()
		}
	}()
}

// Result is a live query's latest rows, or the error it last got.
type Result[T any] struct {
	Rows    []T
	Err     error
	Loading bool // nothing fetched yet
}

// Watch is a live query: a signal of its rows, fetched off the main
// thread now and again each time a write to a table it reads commits.
// Make it in a component: it stops with the component's owner.
func Watch[T any](d *DB, query string, args ...any) signal.Accessor[Result[T]] {
	return WatchWith[T](d, query, func() []any { return args })
}

// WatchWith is Watch with arguments that change: args reads signals (a
// search field's text, a page's size), and the query runs again when
// they do, as when the database changes (expo-sqlite's useLiveQuery
// with its dependencies). Until the new rows come, the old ones stay;
// rows for arguments that have since changed are dropped.
//
//	q := signal.New("")
//	notes := sqlite.WatchWith[Note](db, `select * from notes where text like ? order by id`,
//		func() []any { return []any{"%" + q.Get() + "%"} })
func WatchWith[T any](d *DB, query string, args func() []any) signal.Accessor[Result[T]] {
	s := signal.NewFunc(Result[T]{Loading: true}, nil)
	w := &watch{query: query}
	var mu sync.Mutex
	var cur []any // the arguments to fetch with
	gen := 0      // which arguments they are; main thread
	w.fetch = func() {
		mu.Lock()
		a, g := cur, gen
		mu.Unlock()
		rows, tables, err := queryReading[T](d, query, a)
		w.mu.Lock()
		if err == nil {
			w.tables = tables
		}
		w.fetches++
		w.mu.Unlock()
		ui.Post(func() {
			mu.Lock()
			stale := g != gen
			mu.Unlock()
			if !stale {
				s.Set(Result[T]{Rows: rows, Err: err})
			}
		})
	}
	d.mu.Lock()
	d.watches[w] = true
	d.mu.Unlock()
	signal.OnCleanup(func() {
		d.mu.Lock()
		delete(d.watches, w)
		d.mu.Unlock()
	})
	// Runs now, and again as what args reads changes.
	signal.RenderEffect(func() {
		a := args()
		mu.Lock()
		cur, gen = a, gen+1
		mu.Unlock()
		w.run()
	})
	return s
}
