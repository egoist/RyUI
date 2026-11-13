// Web SQLite is a web e2e's app (e2e/websqlite_test.go): asuka/sdk/sqlite in
// a browser, driven by the test through window.sqliteTest (hooks_js.go),
// and a live query's count on screen.
package main

import (
	"fmt"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/sdk/sqlite"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

var db *sqlite.DB

func main() {
	// Opened in main, before app.Run, as apps do: main may wait.
	var err error
	if db, err = sqlite.Open("e2e.db"); err != nil {
		panic(err)
	}
	if err := db.Migrate(`create table t (id integer primary key, tab text not null, n integer not null)`); err != nil {
		panic(err)
	}
	hooks()
	app.Run(App)
}

func App() ui.Node {
	rows := sqlite.Watch[int](db, `select count(*) from t`)
	return ui.SafeArea(ui.Style{Padding: ui.All(12), Gap: 6},
		ui.StaticText("status: ready"),
		ui.Text(signal.Func(func() string {
			r := rows.Get()
			if r.Err != nil {
				return "rows: " + r.Err.Error()
			}
			if r.Loading || len(r.Rows) == 0 {
				return "rows: loading"
			}
			return fmt.Sprintf("rows: %d", r.Rows[0])
		})),
	)
}
