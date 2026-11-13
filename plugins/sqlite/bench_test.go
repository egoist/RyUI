package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/egoist/asuka/plugins/sqlite"
)

type benchRow struct {
	ID int64
	A  int64
	B  string
}

func benchDB(b *testing.B, rows int) *sqlite.DB {
	d, err := sqlite.Open(filepath.Join(b.TempDir(), "b.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`create table t (id integer primary key, a integer, b text)`); err != nil {
		b.Fatal(err)
	}
	err = d.Tx(func(tx *sqlite.Tx) error {
		for i := range rows {
			if _, err := tx.Exec(`insert into t (a, b) values (?, ?)`, i, "row number text"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		b.Fatal(err)
	}
	return d
}

// Inserting 1000 rows in a transaction.
func BenchmarkInsert(b *testing.B) {
	d := benchDB(b, 0)
	b.ReportAllocs()
	for b.Loop() {
		err := d.Tx(func(tx *sqlite.Tx) error {
			for i := range 1000 {
				if _, err := tx.Exec(`insert into t (a, b) values (?, ?)`, i, "row number text"); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Reading 10000 rows into structs.
func BenchmarkScan(b *testing.B) {
	d := benchDB(b, 10000)
	b.ReportAllocs()
	for b.Loop() {
		rows, err := sqlite.Query[benchRow](d, `select id, a, b from t`)
		if err != nil || len(rows) != 10000 {
			b.Fatal(len(rows), err)
		}
	}
}

// Reading one row by its id.
func BenchmarkPoint(b *testing.B) {
	d := benchDB(b, 10000)
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		i++
		if _, ok, err := sqlite.QueryOne[benchRow](d, `select id, a, b from t where id = ?`, i%10000+1); !ok {
			b.Fatal(err)
		}
	}
}
