//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"syscall/js"
	"time"

	"github.com/egoist/asuka/sdk/sqlite"
	"github.com/egoist/asuka/ui"
)

// hooks gives the test window.sqliteTest(command, ...args): a Promise of
// the command's result, as text (an error's message, prefixed "error: ").
func hooks() {
	dbs := map[string]*sqlite.DB{"e2e.db": db}
	open := func(name string) (*sqlite.DB, error) {
		if d := dbs[name]; d != nil {
			return d, nil
		}
		d, err := sqlite.Open(name)
		if err == nil {
			dbs[name] = d
		}
		return d, err
	}
	run := func(cmd string, args []string) (string, error) {
		switch cmd {
		case "exec": // name, SQL, arguments
			d, err := open(args[0])
			if err != nil {
				return "", err
			}
			r, err := d.Exec(args[1], anys(args[2:])...)
			return fmt.Sprint(r.RowsAffected), err
		case "query": // name, SQL: its rows' first column, as JSON
			d, err := open(args[0])
			if err != nil {
				return "", err
			}
			rows, err := sqlite.Query[string](d, args[1])
			if err != nil {
				return "", err
			}
			b, _ := json.Marshal(rows)
			return string(b), nil
		case "insert": // tab, n: n rows, each its own statement
			var n int
			fmt.Sscan(args[1], &n)
			for i := range n {
				if _, err := db.Exec(`insert into t (tab, n) values (?, ?)`, args[0], i); err != nil {
					return "", fmt.Errorf("row %d: %w", i, err)
				}
			}
			return "ok", nil
		case "tx": // tab, n: n rows in one transaction
			var n int
			fmt.Sscan(args[1], &n)
			err := db.Tx(func(tx *sqlite.Tx) error {
				for i := range n {
					if _, err := tx.Exec(`insert into t (tab, n) values (?, ?)`, args[0], i); err != nil {
						return err
					}
				}
				return nil
			})
			return "ok", err
		case "bench": // a 1000-insert transaction, a 10,000-row query: their times in ms
			d, err := open("bench.db")
			if err != nil {
				return "", err
			}
			if _, err := d.Exec(`drop table if exists b; create table b (id integer primary key, s text, x real)`); err != nil {
				return "", err
			}
			start := time.Now()
			err = d.Tx(func(tx *sqlite.Tx) error {
				for i := range 1000 {
					if _, err := tx.Exec(`insert into b (s, x) values (?, ?)`, fmt.Sprintf("row %d", i), float64(i)/3); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return "", err
			}
			tx := time.Since(start)
			if _, err := d.Exec(`insert into b (s, x) select s, x from b; insert into b (s, x) select s, x from b; insert into b (s, x) select s, x from b; insert into b (s, x) select s, x from b; delete from b where id > 10000`); err != nil {
				return "", err
			}
			type row struct {
				ID int64
				S  string
				X  float64
			}
			start = time.Now()
			rows, err := sqlite.Query[row](d, `select * from b`)
			if err != nil {
				return "", err
			}
			q := time.Since(start)
			start = time.Now()
			for i := range 100 {
				if _, _, err := sqlite.QueryOne[int](d, `select count(*) from b where id = ?`, i); err != nil {
					return "", err
				}
			}
			one := time.Since(start) / 100
			ms := func(d time.Duration) string { return fmt.Sprintf("%.1f", float64(d)/1e6) }
			return fmt.Sprintf("tx1000 %s ms, query%d %s ms, a query of one row %d µs", ms(tx), len(rows), ms(q), one.Microseconds()), nil
		case "mainthread": // a call on the page's main thread: an error, not a deadlock
			c := make(chan error, 1)
			ui.Post(func() {
				_, err := db.Exec(`insert into t (tab, n) values ('main', 0)`)
				c <- err
			})
			if err := <-c; err != nil {
				return "", err
			}
			return "no error", nil
		case "async": // ExecAsync from the main thread, its done on it
			c := make(chan error, 1)
			ui.Post(func() {
				db.ExecAsync(`insert into t (tab, n) values ('async', 0)`, nil, func(_ sqlite.Changes, err error) { c <- err })
			})
			return "ok", <-c
		}
		return "", fmt.Errorf("unknown command %q", cmd)
	}
	js.Global().Set("sqliteTest", js.FuncOf(func(_ js.Value, a []js.Value) any {
		cmd := a[0].String()
		var args []string
		for _, v := range a[1:] {
			args = append(args, v.String())
		}
		return js.Global().Get("Promise").New(js.FuncOf(func(_ js.Value, p []js.Value) any {
			resolve := p[0]
			go func() {
				out, err := run(cmd, args)
				if err != nil {
					out = "error: " + err.Error()
				}
				resolve.Invoke(out)
			}()
			return nil
		}))
	}))
}

func anys(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = strings.TrimSpace(v)
	}
	return out
}
