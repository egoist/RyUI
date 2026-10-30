// Package sqlite is an app's SQLite database: a file in its data
// directory, opened with the settings an app wants (write-ahead logging,
// foreign keys, a busy timeout), queries that scan into structs, a
// transaction helper, versioned migrations, writes off the main thread
// (ExecAsync, TxAsync), and live queries: signals that run their query
// again, off the main thread, when a table it reads is written.
//
// SQLite is modernc.org/sqlite, a translation of its C to Go: it needs no
// cgo, so the same code runs on iOS, Android and, under nui dev, the Mac.
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

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/store"
	"github.com/egoist/nui/ui"

	msqlite "modernc.org/sqlite"
)

// DB is an open database.
type DB struct {
	db      *sql.DB
	mu      sync.Mutex
	watches map[*watch]bool
	dirty   map[string]bool // the tables written since the live queries last heard
}

// The driver nui's databases use: modernc.org/sqlite's, with hooks on
// each connection that note the tables it reads and writes (its own, so
// they're on no one else's connections).
var (
	driver   = &msqlite.Driver{}
	opened   sync.Map // DSN → *DB, for the connection hook
	nextDB   atomic.Int64
	conns    sync.Map // driver connection → *connState
	register sync.Once
)

// connState is a connection's: the tables its transaction has written,
// and, while a live query runs on it, those read.
type connState struct {
	d       *DB
	written map[string]bool
	reads   map[string]bool // nil: not recording
}

func registerDriver() {
	sql.Register("nui-sqlite", driver)
	driver.RegisterConnectionHook(func(c msqlite.ExecQuerierContext, dsn string) error {
		v, ok := opened.Load(dsn)
		if !ok {
			return nil
		}
		st := &connState{d: v.(*DB), written: map[string]bool{}}
		conns.Store(c, st)
		if h, ok := c.(msqlite.HookRegisterer); ok {
			// Every row written (by triggers and foreign keys' actions too).
			h.RegisterPreUpdateHook(func(u msqlite.SQLitePreUpdateData) { st.written[u.TableName] = true })
			h.RegisterCommitHook(func() int32 {
				st.d.wrote(st.written)
				clear(st.written)
				return 0
			})
			h.RegisterRollbackHook(func() { clear(st.written) })
		}
		if a, ok := c.(msqlite.AuthorizerRegisterer); ok {
			return a.RegisterAuthorizer(func(action msqlite.AuthorizerActionCode, arg1, _, _, _ string) msqlite.AuthorizerReturnCode {
				switch action {
				case msqlite.AuthRead:
					if st.reads != nil && !strings.HasPrefix(arg1, "sqlite_") {
						st.reads[arg1] = true
					}
				case msqlite.AuthInsert, msqlite.AuthUpdate, msqlite.AuthDelete:
					// Also a delete of every row, which SQLite may do without
					// visiting them (its truncate optimization).
					if !strings.HasPrefix(arg1, "sqlite_") {
						st.written[arg1] = true
					}
				}
				return msqlite.AuthorizerOK
			})
		}
		return nil
	})
}

// Open opens (creating it if need be) the database name: a file in the
// app's data directory (store.Dir), an absolute path, or ":memory:".
func Open(name string) (*DB, error) {
	path := name
	if name != ":memory:" && !filepath.IsAbs(name) {
		dir := store.Dir()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		path = filepath.Join(dir, name)
	}
	q := url.Values{}
	for _, p := range []string{"journal_mode(WAL)", "foreign_keys(1)", "busy_timeout(5000)", "synchronous(NORMAL)"} {
		q.Add("_pragma", p)
	}
	// Which DB a connection is: its hook is given the DSN (SQLite and the
	// driver ignore a parameter they don't know).
	q.Set("_nui", strconv.FormatInt(nextDB.Add(1), 10))
	dsn := "file:" + path + "?" + q.Encode()
	if name == ":memory:" {
		dsn = "file::memory:?" + q.Encode()
	}
	register.Do(registerDriver)
	d := &DB{watches: map[*watch]bool{}, dirty: map[string]bool{}}
	opened.Store(dsn, d)
	db, err := sql.Open("nui-sqlite", dsn)
	if err != nil {
		opened.Delete(dsn)
		return nil, err
	}
	if name == ":memory:" {
		// One connection: each would have its own memory database.
		db.SetMaxOpenConns(1)
	} else {
		// A few connections, kept: a closed one's hooks state would linger.
		db.SetMaxOpenConns(4)
		db.SetMaxIdleConns(4)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		opened.Delete(dsn)
		return nil, err
	}
	d.db = db
	return d, nil
}

// wrote notes tables a commit wrote; the live queries hear of them when
// the write returns (the commit is done then: a query run at once from
// the hook might not see it).
func (d *DB) wrote(tables map[string]bool) {
	d.mu.Lock()
	for t := range tables {
		d.dirty[t] = true
	}
	d.mu.Unlock()
}

// SQL is the database/sql handle, for what this package doesn't cover.
// Its writes are noted, and the live queries that read their tables run
// again at the next Exec or Tx, or Changed.
func (d *DB) SQL() *sql.DB { return d.db }

// Close closes the database.
func (d *DB) Close() error { return d.db.Close() }

// Exec runs a statement (insert, update, delete, create...), and updates
// the live queries.
func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	r, err := d.db.Exec(query, args...)
	d.notify()
	return r, err
}

// ExecAsync runs a statement off the main thread, then calls done (if
// it isn't nil) on the main thread; with no done, an error is logged.
func (d *DB) ExecAsync(query string, args []any, done func(sql.Result, error)) {
	go func() {
		r, err := d.Exec(query, args...)
		if done != nil {
			ui.Post(func() { done(r, err) })
		} else if err != nil {
			fmt.Fprintln(os.Stderr, "sqlite:", err)
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
			fmt.Fprintln(os.Stderr, "sqlite:", err)
		}
	}()
}

// Tx runs f in a transaction: committed if f returns nil, rolled back if
// it returns an error (which Tx returns) or panics.
func (d *DB) Tx(f func(tx *Tx) error) (err error) {
	t, err := d.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			t.Rollback()
			panic(p)
		}
		if err != nil {
			t.Rollback()
			return
		}
		err = t.Commit()
		d.notify()
	}()
	return f(&Tx{t})
}

// Tx is a transaction.
type Tx struct{ tx *sql.Tx }

// Exec runs a statement in the transaction.
func (t *Tx) Exec(query string, args ...any) (sql.Result, error) { return t.tx.Exec(query, args...) }

// Migrate brings the schema up to date: steps are the database's
// versions in order, each SQL (statements separated by semicolons) run
// once, in a transaction, recorded in PRAGMA user_version. Append steps;
// never change one that has shipped.
func (d *DB) Migrate(steps ...string) error {
	var v int
	if err := d.db.QueryRow(`pragma user_version`).Scan(&v); err != nil {
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

// querier is what Query reads from: a DB or a Tx.
type querier interface {
	query(q string, args ...any) (*sql.Rows, error)
}

func (d *DB) query(q string, args ...any) (*sql.Rows, error) { return d.db.Query(q, args...) }
func (t *Tx) query(q string, args ...any) (*sql.Rows, error) { return t.tx.Query(q, args...) }

// Query runs a query and scans its rows into Ts: a struct's fields by
// column name (a field's `db` tag, or its name, case-insensitively;
// columns with no field are skipped), or, for any other T, the first
// column.
func Query[T any](from querier, query string, args ...any) ([]T, error) {
	rows, err := from.query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var out []T
	var zero T
	t := reflect.TypeOf(&zero).Elem()
	isStruct := t.Kind() == reflect.Struct && t.String() != "time.Time"
	var fields []int // per column, the field index, or -1
	if isStruct {
		byName := map[string]int{}
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			name := f.Tag.Get("db")
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			byName[strings.ToLower(name)] = i
		}
		for _, c := range cols {
			i, ok := byName[strings.ToLower(c)]
			if !ok {
				i, ok = byName[strings.ToLower(strings.ReplaceAll(c, "_", ""))]
			}
			if !ok {
				i = -1
			}
			fields = append(fields, i)
		}
	}
	dest := make([]any, len(cols))
	var skip any
	for rows.Next() {
		var v T
		rv := reflect.ValueOf(&v).Elem()
		for i := range dest {
			switch {
			case !isStruct && i == 0:
				dest[i] = rv.Addr().Interface()
			case isStruct && fields[i] >= 0:
				dest[i] = rv.Field(fields[i]).Addr().Interface()
			default:
				dest[i] = &skip
			}
		}
		if err := rows.Scan(dest...); err != nil {
			return out, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// QueryOne is Query's first row; ok is false if there's none.
func QueryOne[T any](from querier, query string, args ...any) (v T, ok bool, err error) {
	rows, err := Query[T](from, query, args...)
	if err != nil || len(rows) == 0 {
		return v, false, err
	}
	return rows[0], true, nil
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

// queryReading runs Query on a connection of its own, noting the tables
// it reads (SQLite's authorizer sees them as the query is prepared: a
// view's tables too).
func queryReading[T any](d *DB, query string, args []any) ([]T, map[string]bool, error) {
	ctx := context.Background()
	c, err := d.db.Conn(ctx)
	if err != nil {
		return nil, nil, err
	}
	defer c.Close()
	var st *connState
	c.Raw(func(dc any) error {
		if v, ok := conns.Load(dc); ok {
			st = v.(*connState)
			st.reads = map[string]bool{}
		}
		return nil
	})
	rows, err := Query[T](connQuerier{c}, query, args...)
	var tables map[string]bool
	if st != nil {
		c.Raw(func(any) error { tables, st.reads = st.reads, nil; return nil })
	}
	return rows, tables, err
}

type connQuerier struct{ c *sql.Conn }

func (q connQuerier) query(s string, args ...any) (*sql.Rows, error) {
	return q.c.QueryContext(context.Background(), s, args...)
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
