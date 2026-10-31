package sqlite_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/egoist/ryui/sqlite"
	_ "modernc.org/sqlite"
)

// Inserting 1000 rows in a transaction: RyUI's driver, whose hooks note
// each row's table, against the plain one.
func BenchmarkInsert(b *testing.B) {
	run := func(b *testing.B, db *sql.DB, tx func(func(exec func(string, ...any) error) error) error) {
		db.Exec(`create table t (id integer primary key, v text)`)
		b.ResetTimer()
		for range b.N {
			err := tx(func(exec func(string, ...any) error) error {
				for i := range 1000 {
					if err := exec(`insert into t (v) values (?)`, i); err != nil {
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
	b.Run("ryui", func(b *testing.B) {
		d, err := sqlite.Open(filepath.Join(b.TempDir(), "n.db"))
		if err != nil {
			b.Fatal(err)
		}
		defer d.Close()
		run(b, d.SQL(), func(f func(func(string, ...any) error) error) error {
			return d.Tx(func(tx *sqlite.Tx) error {
				return f(func(q string, a ...any) error { _, err := tx.Exec(q, a...); return err })
			})
		})
	})
	b.Run("plain", func(b *testing.B) {
		db, err := sql.Open("sqlite", "file:"+filepath.Join(b.TempDir(), "p.db")+"?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
		if err != nil {
			b.Fatal(err)
		}
		defer db.Close()
		run(b, db, func(f func(func(string, ...any) error) error) error {
			tx, err := db.Begin()
			if err != nil {
				return err
			}
			if err := f(func(q string, a ...any) error { _, err := tx.Exec(q, a...); return err }); err != nil {
				tx.Rollback()
				return err
			}
			return tx.Commit()
		})
	})
}
