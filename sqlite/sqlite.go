// Package sqlite is an app's SQLite database: a file in its data
// directory, opened with the settings an app wants (write-ahead logging,
// foreign keys, a busy timeout), queries that scan into structs, a
// transaction helper, versioned migrations, and live queries: signals
// that run their query again, off the main thread, when the database is
// written.
//
// SQLite is modernc.org/sqlite, a translation of its C to Go: it needs no
// cgo, so the same code runs on iOS, Android and, under nui dev, the Mac.
//
//	db, err := sqlite.Open("notes.db")
//	db.Migrate(
//		`create table notes (id integer primary key, text text not null, done bool not null default 0)`,
//	)
//	db.Exec(`insert into notes (text) values (?)`, "Buy milk")
//	notes := sqlite.Watch[Note](db, `select * from notes order by id`)
//
// Queries block: run them off the main thread (a goroutine, then
// ui.Post), or use Watch, which does.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/store"
	"github.com/egoist/nui/ui"

	_ "modernc.org/sqlite" // the "sqlite" driver
)

// DB is an open database.
type DB struct {
	db      *sql.DB
	mu      sync.Mutex
	watches map[*watch]bool
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
	dsn := "file:" + path + "?" + q.Encode()
	if name == ":memory:" {
		// One connection: each would have its own memory database.
		dsn = "file::memory:?" + q.Encode()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if name == ":memory:" {
		db.SetMaxOpenConns(1)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return &DB{db: db, watches: map[*watch]bool{}}, nil
}

// SQL is the database/sql handle, for what this package doesn't cover.
// Writes made through it don't update Watch's queries; call Changed.
func (d *DB) SQL() *sql.DB { return d.db }

// Close closes the database.
func (d *DB) Close() error { return d.db.Close() }

// Exec runs a statement (insert, update, delete, create...), and updates
// the live queries.
func (d *DB) Exec(query string, args ...any) (sql.Result, error) {
	r, err := d.db.Exec(query, args...)
	if err == nil {
		d.Changed()
	}
	return r, err
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
		if err = t.Commit(); err == nil {
			d.Changed()
		}
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

// Changed tells the live queries the database changed (Exec and Tx do).
func (d *DB) Changed() {
	d.mu.Lock()
	ws := make([]*watch, 0, len(d.watches))
	for w := range d.watches {
		ws = append(ws, w)
	}
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
// thread now and again each time the database changes through Exec or
// Tx. Make it in a component: it stops with the component's owner.
func Watch[T any](d *DB, query string, args ...any) signal.Accessor[Result[T]] {
	s := signal.NewFunc(Result[T]{Loading: true}, nil)
	w := &watch{}
	w.fetch = func() {
		rows, err := Query[T](d, query, args...)
		ui.Post(func() { s.Set(Result[T]{Rows: rows, Err: err}) })
	}
	d.mu.Lock()
	d.watches[w] = true
	d.mu.Unlock()
	signal.OnCleanup(func() {
		d.mu.Lock()
		delete(d.watches, w)
		d.mu.Unlock()
	})
	w.run()
	return s
}
