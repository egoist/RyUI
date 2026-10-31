package main

import (
	"testing"

	"github.com/egoist/ryui/sqlite"
)

// The search's SQL: a match anywhere, any case; % and _ are text.
func TestSearchQuery(t *testing.T) {
	t.Setenv("RYUI_DATA", t.TempDir()) // not the test binary's own data
	openDB()
	t.Cleanup(func() { db.Close() }) // Windows can't remove an open file
	for _, s := range []string{"Buy milk", "Call Ada", "100% done", "a_b"} {
		db.Exec(`insert into notes (text) values (?)`, s)
	}
	q := `select * from notes where ?1 = '' or instr(lower(text), lower(?1)) > 0 order by done, id desc`
	for in, want := range map[string]int{"": 4, "MILK": 1, "a": 2, "%": 1, "_": 1, "zzz": 0} {
		rows, err := sqlite.Query[note](db, q, in)
		if err != nil || len(rows) != want {
			t.Errorf("%q: %d rows (%v), want %d", in, len(rows), err, want)
		}
	}
}
