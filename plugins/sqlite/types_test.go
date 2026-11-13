package sqlite_test

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/asuka/plugins/sqlite"
)

type status string

type stamps struct {
	Created time.Time
	Updated *time.Time
}

type everything struct {
	stamps
	ID      int64
	Name    string
	Small   int8
	Count   uint32
	Ratio   float64
	Ratio32 float32
	OK      bool
	Data    []byte
	Status  status
	Note    *string
	Maybe   sql.NullString
	MaybeN  sql.NullInt64
	Any     any
	Ignored string `db:"-"`
	Renamed int    `db:"other_name"`
	private int
}

func TestTypes(t *testing.T) {
	db := open(t)
	if _, err := db.Exec(`create table e (id integer primary key, name, small, count, ratio, ratio32, ok, data, status,
		note, maybe, maybe_n, "any", ignored, other_name, private, created, updated)`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 13, 20, 5, 6, 123456789, time.FixedZone("x", 8*3600))
	note := "a note"
	_, err := db.Exec(`insert into e values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		1, "名前 ✓", int8(-5), uint32(4000000000), 0.25, float32(1.5), true, []byte{0, 1, 2}, status("open"),
		&note, sql.NullString{String: "yes", Valid: true}, sql.NullInt64{}, 42, "x", 7, 9, now, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := sqlite.QueryOne[everything](db, `select * from e`)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	want := everything{stamps: stamps{Created: now}, ID: 1, Name: "名前 ✓", Small: -5, Count: 4000000000, Ratio: 0.25, Ratio32: 1.5, OK: true,
		Data: []byte{0, 1, 2}, Status: "open", Note: &note, Maybe: sql.NullString{String: "yes", Valid: true}, Any: int64(42), Renamed: 7}
	if !got.Created.Equal(want.Created) || got.Updated != nil {
		t.Errorf("times %v %v, want %v nil", got.Created, got.Updated, want.Created)
	}
	got.Created, want.Created = time.Time{}, time.Time{}
	if fmt.Sprint(got.Note != nil && *got.Note == note) != "true" {
		t.Errorf("note %v", got.Note)
	}
	got.Note, want.Note = nil, nil
	if !bytes.Equal(got.Data, want.Data) {
		t.Errorf("data %v", got.Data)
	}
	got.Data, want.Data = nil, nil
	if fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	// Conversions, and NULLs to zero values.
	type conv struct {
		S string
		I int
		F float64
		B bool
		T time.Time
	}
	cases := []struct {
		sql  string
		want conv
	}{
		{`select 12 as s, '34' as i, '1.5' as f, 'true' as b, 0 as t`, conv{S: "12", I: 34, F: 1.5, B: true, T: time.Unix(0, 0).UTC()}},
		{`select null as s, null as i, null as f, null as b, null as t`, conv{}},
		{`select 2.5 as s, 3.0 as i, 2 as f, 1 as b, '2024-01-02T03:04:05Z' as t`, conv{S: "2.5", I: 3, F: 2, B: true, T: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)}},
		// As modernc.org/sqlite wrote times (time.Time's String).
		{`select '' as s, 0 as i, 0 as f, 0 as b, '2024-01-02 03:04:05.5 +0000 UTC' as t`, conv{T: time.Date(2024, 1, 2, 3, 4, 5, 5e8, time.UTC)}},
		{`select '' as s, 0 as i, 0 as f, 0 as b, datetime('2024-01-02 03:04:05') as t`, conv{T: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)}},
	}
	for _, c := range cases {
		got, _, err := sqlite.QueryOne[conv](db, c.sql)
		if err != nil || !got.T.Equal(c.want.T) {
			t.Errorf("%s: %+v %v", c.sql, got, err)
			continue
		}
		got.T, c.want.T = time.Time{}, time.Time{}
		if got != c.want {
			t.Errorf("%s: %+v, want %+v", c.sql, got, c.want)
		}
	}
	for _, bad := range []string{`select 1.5 as i`, `select 'x' as i`, `select 300 as small`, `select -1 as count`, `select 'nope' as t`} {
		type bads struct {
			I     int
			Small int8
			Count uint
			T     time.Time
		}
		if _, _, err := sqlite.QueryOne[bads](db, bad); err == nil {
			t.Errorf("%s: no error", bad)
		}
	}

	// Scalars, and the extremes.
	n, _, err := sqlite.QueryOne[int64](db, `select ?`, int64(math.MinInt64))
	if err != nil || n != math.MinInt64 {
		t.Errorf("min int64 %d %v", n, err)
	}
	if _, err := sqlite.Query[int](db, `select ?`, uint64(math.MaxUint64)); err == nil {
		t.Error("uint64 beyond int64: no error")
	}
	s, _, _ := sqlite.QueryOne[*string](db, `select null`)
	if s != nil {
		t.Errorf("null into *string: %v", *s)
	}
	e, _, _ := sqlite.QueryOne[string](db, `select ''`)
	if e != "" {
		t.Errorf("empty string %q", e)
	}
	// A blob is a []byte, empty or not; an empty one isn't NULL.
	var empty []byte = []byte{}
	isNull, _, _ := sqlite.QueryOne[bool](db, `select ? is null`, empty)
	if isNull {
		t.Error("an empty []byte is NULL")
	}

	// A struct field that can't be scanned is an error only if selected.
	type odd struct {
		A  int
		Ch chan int
	}
	if _, _, err := sqlite.QueryOne[odd](db, `select 1 as a`); err != nil {
		t.Error(err)
	}
	if _, _, err := sqlite.QueryOne[odd](db, `select 1 as ch`); err == nil {
		t.Error("a chan field selected: no error")
	}
}

func TestArgs(t *testing.T) {
	db := open(t)
	v, _, err := sqlite.QueryOne[string](db, `select :a || @b || $c || ?4`,
		sqlite.Named("c", "3"), sqlite.Named("a", "1"), sqlite.Named("b", "2"), "4")
	if err != nil || v != "1234" {
		t.Errorf("named %q %v", v, err)
	}
	v, _, err = sqlite.QueryOne[string](db, `select ?2 || ?1`, "a", "b")
	if err != nil || v != "ba" {
		t.Errorf("numbered %q %v", v, err)
	}
	if _, _, err := sqlite.QueryOne[string](db, `select ?, ?`, "one"); err == nil || !strings.Contains(err.Error(), "missing argument with index 2") {
		t.Errorf("missing: %v", err)
	}
	if _, _, err := sqlite.QueryOne[string](db, `select :x`); err == nil || !strings.Contains(err.Error(), `missing named argument "x"`) {
		t.Errorf("missing named: %v", err)
	}
	if _, err := db.Exec(`select ?`, struct{}{}); err == nil {
		t.Error("a struct argument: no error")
	}
}

func TestErrors(t *testing.T) {
	db := open(t)
	db.Exec(`create table u (k text unique not null)`)
	db.Exec(`insert into u values ('a')`)
	_, err := db.Exec(`insert into u values ('a')`)
	if !errors.Is(err, sqlite.ErrConstraint) || !errors.Is(err, sqlite.ErrConstraintUnique) || errors.Is(err, sqlite.ErrConstraintNotNull) {
		t.Errorf("unique: %v", err)
	}
	var e *sqlite.Error
	if !errors.As(err, &e) || e.Code != 2067 || !strings.Contains(err.Error(), "UNIQUE constraint failed: u.k") || !strings.Contains(err.Error(), "SQLITE_CONSTRAINT_UNIQUE") {
		t.Errorf("error %v", err)
	}
	if _, err := db.Exec(`insert into u values (null)`); !errors.Is(err, sqlite.ErrConstraintNotNull) {
		t.Errorf("not null: %v", err)
	}
	if _, err := sqlite.Query[int](db, `select * from nowhere`); !errors.Is(err, sqlite.ErrError) || !strings.Contains(err.Error(), "no such table: nowhere") {
		t.Errorf("no table: %v", err)
	}
	// A failing statement leaves the connection usable.
	if n, _, err := sqlite.QueryOne[int](db, `select count(*) from u`); err != nil || n != 1 {
		t.Errorf("after errors: %d %v", n, err)
	}
}

// Answers longer than the buffer: rows in several chunks, and a row
// longer than a chunk.
func TestBigAnswers(t *testing.T) {
	db := open(t)
	db.Exec(`create table big (id integer primary key, b blob)`)
	err := db.Tx(func(tx *sqlite.Tx) error {
		for i := range 5000 {
			if _, err := tx.Exec(`insert into big (b) values (?)`, bytes.Repeat([]byte{byte(i)}, 100)); err != nil {
				return err
			}
		}
		_, err := tx.Exec(`insert into big (b) values (?)`, bytes.Repeat([]byte{7}, 3<<20))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		ID int64
		B  []byte
	}
	rows, err := sqlite.Query[row](db, `select * from big order by id`)
	if err != nil || len(rows) != 5001 {
		t.Fatalf("%d rows, %v", len(rows), err)
	}
	for i, r := range rows[:5000] {
		if r.ID != int64(i+1) || len(r.B) != 100 || r.B[99] != byte(i) {
			t.Fatalf("row %d: %d %d", i, r.ID, len(r.B))
		}
	}
	if last := rows[5000]; len(last.B) != 3<<20 || last.B[1<<20] != 7 {
		t.Fatalf("the big row: %d bytes", len(last.B))
	}
	// QueryOne stops at the first row, and the query's next run is fine.
	for range 3 {
		r, ok, err := sqlite.QueryOne[row](db, `select * from big order by id`)
		if !ok || err != nil || r.ID != 1 {
			t.Fatalf("one %v %v %v", r.ID, ok, err)
		}
	}
	// Serialized, the database is bigger than any buffer too.
	data, err := db.Serialize()
	if err != nil || len(data) < 3<<20 {
		t.Fatalf("serialized %d %v", len(data), err)
	}
}

func TestScriptsAndCache(t *testing.T) {
	db := open(t)
	if _, err := db.Exec(`create table s (n); insert into s values (1); insert into s values (2);`); err != nil {
		t.Fatal(err)
	}
	// A script's rows are the first statement's that has some.
	ns, err := sqlite.Query[int](db, `insert into s values (3); select n from s order by n`)
	if err != nil || len(ns) != 3 {
		t.Fatalf("%v %v", ns, err)
	}
	// More statements than the cache keeps, then the first again.
	for i := range 200 {
		n, _, err := sqlite.QueryOne[int](db, fmt.Sprintf(`select %d + ?`, i), 1)
		if err != nil || n != i+1 {
			t.Fatalf("%d: %d %v", i, n, err)
		}
	}
	n, _, err := sqlite.QueryOne[int](db, `select 0 + ?`, 5)
	if err != nil || n != 5 {
		t.Fatalf("%d %v", n, err)
	}
	// A schema change: the cached statement is prepared again.
	db.Exec(`alter table s add column m default 9`)
	type sm struct{ N, M int }
	rows, err := sqlite.Query[sm](db, `select * from s order by n`)
	if err != nil || len(rows) != 3 || rows[0].M != 9 {
		t.Fatalf("%v %v", rows, err)
	}
}

// A file's readers read beside a transaction, and see its writes once
// it commits; the transaction sees its own.
func TestReadersAndTx(t *testing.T) {
	if runtime.GOOS == "js" {
		t.Skip("a browser's database has one connection: reads wait for the transaction")
	}
	db := openNamed(t, filepath.Join(t.TempDir(), "r.db"))
	db.Exec(`insert into notes (text) values ('one')`)
	inTx := make(chan struct{})
	finish := make(chan struct{})
	done := make(chan error)
	go func() {
		done <- db.Tx(func(tx *sqlite.Tx) error {
			tx.Exec(`insert into notes (text) values ('two')`)
			n, _, err := sqlite.QueryOne[int](tx, `select count(*) from notes`)
			if n != 2 {
				return fmt.Errorf("the transaction sees %d, %v", n, err)
			}
			close(inTx)
			<-finish
			return nil
		})
	}()
	<-inTx
	if n, _, err := sqlite.QueryOne[int](db, `select count(*) from notes`); n != 1 || err != nil {
		t.Errorf("a reader during the transaction sees %d, %v", n, err)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if n, _, _ := sqlite.QueryOne[int](db, `select count(*) from notes`); n != 2 {
		t.Errorf("after: %d", n)
	}
}

// A write waits for the writer as SQLite waits for a lock: ErrBusy when
// the busy timeout passes (a db.Exec inside db.Tx's function).
func TestWriterBusy(t *testing.T) {
	db := open(t)
	sqlite.SetBusy(db, 50*time.Millisecond)
	err := db.Tx(func(tx *sqlite.Tx) error {
		_, err := db.Exec(`insert into notes (text) values ('x')`)
		return err
	})
	if !errors.Is(err, sqlite.ErrBusy) {
		t.Fatalf("%v", err)
	}
	// After a Tx, a Tx's Exec says it's done.
	var kept *sqlite.Tx
	db.Tx(func(tx *sqlite.Tx) error { kept = tx; return nil })
	if _, err := kept.Exec(`select 1`); !errors.Is(err, sqlite.ErrMisuse) {
		t.Errorf("a done Tx: %v", err)
	}
}

func TestSerializeRestore(t *testing.T) {
	src := openNamed(t, filepath.Join(t.TempDir(), "a.db"))
	src.Exec(`insert into notes (text) values ('kept')`)
	data, err := src.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	dst := openNamed(t, filepath.Join(t.TempDir(), "b.db"))
	dst.Exec(`insert into notes (text) values ('replaced'), ('also')`)
	if err := dst.Restore(data); err != nil {
		t.Fatal(err)
	}
	texts, err := sqlite.Query[string](dst, `select text from notes`)
	if err != nil || len(texts) != 1 || texts[0] != "kept" {
		t.Fatalf("%v %v", texts, err)
	}
	if err := dst.Restore([]byte("not a database")); err == nil {
		t.Error("restoring garbage: no error")
	}
}

func TestClose(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	sqlite.QueryOne[int](db, `select 1`) // a reader
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Errorf("closing twice: %v", err)
	}
	if _, err := db.Exec(`select 1`); !errors.Is(err, sqlite.ErrMisuse) {
		t.Errorf("after close: %v", err)
	}
	if _, err := sqlite.Query[int](db, `select 1`); !errors.Is(err, sqlite.ErrMisuse) {
		t.Errorf("query after close: %v", err)
	}
}

// Many goroutines read and write at once (run with -race).
func TestConcurrent(t *testing.T) {
	for _, name := range []string{":memory:", filepath.Join(t.TempDir(), "c.db")} {
		db := openNamed(t, name)
		var wg sync.WaitGroup
		errs := make(chan error, 64)
		for g := range 8 {
			wg.Go(func() {
				for i := range 50 {
					if g%2 == 0 {
						if _, err := db.Exec(`insert into notes (text) values (?)`, fmt.Sprint(g, i)); err != nil {
							errs <- err
							return
						}
					} else if _, err := sqlite.Query[note](db, `select * from notes order by id desc limit 10`); err != nil {
						errs <- err
						return
					}
				}
			})
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Errorf("%s: %v", name, err)
		}
		if n, _, _ := sqlite.QueryOne[int](db, `select count(*) from notes`); n != 200 {
			t.Errorf("%s: %d notes", name, n)
		}
	}
}
