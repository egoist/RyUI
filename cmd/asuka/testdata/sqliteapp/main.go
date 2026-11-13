// An app that uses asuka/sqlite, without a window: the desktop builds'
// tests build it (desktop_test.go), and it runs from a build's folder to
// show the library the build ships is the one it loads. Each run adds a
// row to the database its argument names (else one in memory), and
// prints how many there are and SQLite's version.
package main

import (
	"fmt"
	"os"

	"github.com/egoist/asuka/plugins/sqlite"
)

func main() {
	name := ":memory:"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	if err := run(name); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(name string) error {
	db, err := sqlite.Open(name)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(`create table runs (id integer primary key, at text not null default (datetime('now')))`); err != nil {
		return err
	}
	if _, err := db.Exec(`insert into runs default values`); err != nil {
		return err
	}
	n, _, err := sqlite.QueryOne[int64](db, `select count(*) from runs`)
	if err != nil {
		return err
	}
	v, _, err := sqlite.QueryOne[string](db, `select sqlite_version()`)
	if err != nil {
		return err
	}
	fmt.Printf("runs %d, SQLite %s\n", n, v)
	return nil
}
