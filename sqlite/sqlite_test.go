package sqlite_test

import (
	"errors"
	"testing"
	"time"

	"github.com/egoist/nui/host/fake"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/sqlite"
	"github.com/egoist/nui/ui"
)

type note struct {
	ID   int64
	Text string
	Done bool
	Due  string `db:"due_on"`
}

func open(t *testing.T) *sqlite.DB {
	db, err := sqlite.Open(":memory:")
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
