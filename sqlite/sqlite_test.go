package sqlite_test

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/ryui/host/fake"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/sqlite"
	"github.com/egoist/ryui/ui"
)

type note struct {
	ID   int64
	Text string
	Done bool
	Due  string `db:"due_on"`
}

func open(t *testing.T) *sqlite.DB { return openNamed(t, ":memory:") }

func openNamed(t *testing.T, name string) *sqlite.DB {
	db, err := sqlite.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(
		`create table notes (id integer primary key, text text not null, done bool not null default 0)`,
		`alter table notes add column due_on text not null default ''`,
	); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestQueries(t *testing.T) {
	db := open(t)
	if _, err := db.Exec(`insert into notes (text, due_on) values (?, ?), (?, ?)`, "milk", "mon", "eggs", "tue"); err != nil {
		t.Fatal(err)
	}
	notes, err := sqlite.Query[note](db, `select * from notes order by id`)
	if err != nil || len(notes) != 2 || notes[1].Text != "eggs" || notes[1].Due != "tue" || notes[1].ID != 2 {
		t.Fatalf("%+v %v", notes, err)
	}
	n, ok, err := sqlite.QueryOne[int](db, `select count(*) from notes`)
	if err != nil || !ok || n != 2 {
		t.Fatalf("count %d %v %v", n, ok, err)
	}
	// Migrating again is a no-op.
	if err := db.Migrate(`create table notes (x)`, `select 1`); err != nil {
		t.Fatal(err)
	}

	// A transaction that fails leaves nothing.
	boom := errors.New("boom")
	err = db.Tx(func(tx *sqlite.Tx) error {
		tx.Exec(`delete from notes`)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if n, _, _ := sqlite.QueryOne[int](db, `select count(*) from notes`); n != 2 {
		t.Fatalf("rolled back to %d rows", n)
	}
}

// A live query fetches off the main thread and posts its rows; a write
// fetches again.
func TestWatch(t *testing.T) {
	db := open(t)
	h := fake.New()
	var live signal.Accessor[sqlite.Result[note]]
	r := ui.Mount(h, func() ui.Node {
		live = sqlite.Watch[note](db, `select * from notes`)
		return ui.StaticText("x")
	})
	t.Cleanup(r.Dispose)
	wait := func(n int) {
		t.Helper()
		// The fetch runs on a goroutine: wait for its rows (the limit is
		// a safety net, not what passes).
		for range 5000 {
			h.RunPosted()
			if res := signal.Untrack(live.Get); !res.Loading && len(res.Rows) == n {
				return
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("rows %+v, want %d", signal.Untrack(live.Get), n)
	}
	wait(0)
	db.Exec(`insert into notes (text) values ('milk')`)
	wait(1)
}

// A live query's arguments can change: it runs again with them, and rows
// for arguments since changed don't show.
func TestWatchWith(t *testing.T) {
	db := open(t)
	for _, text := range []string{"milk", "bread", "butter"} {
		db.Exec(`insert into notes (text) values (?)`, text)
	}
	h := fake.New()
	q := signal.New("b")
	var live signal.Accessor[sqlite.Result[note]]
	r := ui.Mount(h, func() ui.Node {
		live = sqlite.WatchWith[note](db, `select * from notes where text like ? order by id`,
			func() []any { return []any{q.Get() + "%"} })
		return ui.StaticText("x")
	})
	t.Cleanup(r.Dispose)
	wait := func(want ...string) {
		t.Helper()
		for range 5000 { // a safety net, not what passes
			h.RunPosted()
			res := signal.Untrack(live.Get)
			if !res.Loading && len(res.Rows) == len(want) {
				ok := true
				for i, n := range res.Rows {
					ok = ok && n.Text == want[i]
				}
				if ok {
					return
				}
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("rows %+v, want %v", signal.Untrack(live.Get), want)
	}
	wait("bread", "butter")
	q.Set("m")
	r.Flush()
	wait("milk")
	// Several changes before the rows come: the last one's show.
	q.Set("b")
	q.Set("bu")
	r.Flush()
	wait("butter")
	db.Exec(`insert into notes (text) values ('bun')`)
	wait("butter", "bun")
}

// A write runs again only the live queries that read a table it wrote:
// directly, through a view, a trigger, or a delete of every row; not a
// rolled back one. A query whose tables aren't known (it reads none)
// runs on every write.
func TestWatchRunsOnItsTables(t *testing.T) {
	t.Run("memory", func(t *testing.T) { watchTables(t, open(t)) })
	// A file's pool has several connections, each with its hooks.
	t.Run("file", func(t *testing.T) { watchTables(t, openNamed(t, filepath.Join(t.TempDir(), "t.db"))) })
}

func watchTables(t *testing.T, db *sqlite.DB) {
	if err := db.Migrate("", "", // open's two
		`create table tags (note integer, tag text);
		 create table log (what text);
		 create view open_notes as select * from notes where done = 0;
		 create trigger tagged after insert on tags begin insert into log values ('tagged'); end`,
	); err != nil {
		t.Fatal(err)
	}
	queries := map[string]string{
		"notes": `select * from notes`,
		"tags":  `select note from tags`,
		"view":  `select * from open_notes`,
		"log":   `select what from log`,
		"count": `select count(*) from notes`,
		"none":  `select 42`, // reads no table: what it reads isn't known
	}
	h := fake.New()
	r := ui.Mount(h, func() ui.Node {
		sqlite.Watch[note](db, queries["notes"])
		sqlite.Watch[int64](db, queries["tags"])
		sqlite.Watch[note](db, queries["view"])
		sqlite.Watch[string](db, queries["log"])
		sqlite.Watch[int](db, queries["count"])
		sqlite.Watch[int](db, queries["none"])
		return ui.StaticText("x")
	})
	t.Cleanup(r.Dispose)
	prev := map[string]int{}
	// expect waits for the fetches to settle and checks which ran again.
	expect := func(what string, ran ...string) {
		t.Helper()
		for range 5000 { // a safety net, not what passes
			h.RunPosted()
			if !sqlite.Running(db) {
				break
			}
			time.Sleep(time.Millisecond)
		}
		h.RunPosted()
		now := sqlite.Fetches(db)
		want := map[string]bool{}
		for _, k := range ran {
			want[k] = true
		}
		for k, q := range queries {
			if again := now[q] > prev[q]; again != want[k] {
				t.Errorf("%s: %s ran again: %v (%d → %d)", what, k, again, prev[q], now[q])
			}
		}
		prev = now
	}
	expect("first", "notes", "tags", "view", "log", "count", "none")
	db.Exec(`insert into tags values (1, 'home')`)
	expect("a tag (its trigger logs)", "tags", "log", "none")
	db.Exec(`insert into notes (text) values ('milk')`)
	expect("a note", "notes", "view", "count", "none")
	db.Tx(func(tx *sqlite.Tx) error {
		tx.Exec(`insert into notes (text) values ('bread')`)
		return errors.New("no")
	})
	expect("rolled back")
	db.Exec(`delete from tags`)
	expect("every tag", "tags", "none")
	// A write through the raw handle is noted, and heard at the next.
	db.SQL().Exec(`insert into log values ('raw')`)
	db.Exec(`insert into tags values (2, 'work')`)
	expect("raw, then a tag", "tags", "log", "none")
	db.Changed()
	expect("Changed", "notes", "tags", "view", "log", "count", "none")
}

// ExecAsync and TxAsync write off the main thread and report on it.
func TestAsyncWrites(t *testing.T) {
	db := open(t)
	h := fake.New()
	r := ui.Mount(h, func() ui.Node { return ui.StaticText("x") })
	t.Cleanup(r.Dispose)
	var id int64 = -1
	var txErr error = errors.New("pending")
	db.ExecAsync(`insert into notes (text) values (?)`, []any{"milk"}, func(res sqlResult, err error) {
		if err == nil {
			id, _ = res.LastInsertId()
		}
	})
	db.TxAsync(func(tx *sqlite.Tx) error {
		_, err := tx.Exec(`insert into notes (text) values ('bread')`)
		return err
	}, func(err error) { txErr = err })
	for range 5000 { // a safety net
		h.RunPosted()
		if id >= 0 && txErr == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if id < 1 || txErr != nil { // 1 or 2: the two run at once
		t.Fatalf("id %d, tx %v", id, txErr)
	}
	if n, _, _ := sqlite.QueryOne[int](db, `select count(*) from notes`); n != 2 {
		t.Fatalf("%d notes", n)
	}
}

type sqlResult = sql.Result
