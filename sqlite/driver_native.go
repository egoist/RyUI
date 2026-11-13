//go:build !js

package sqlite

import (
	"database/sql"
	"strings"

	msqlite "modernc.org/sqlite"
)

// The driver RyUI's databases use: modernc.org/sqlite's, with hooks on
// each connection that note the tables it reads and writes (its own, so
// they're on no one else's connections).
var driver = &msqlite.Driver{}

// fileConns is how many connections a database file has: readers run
// beside a writer (write-ahead logging).
const fileConns = 4

func registerDriver() {
	sql.Register("ryui-sqlite", driver)
	driver.RegisterConnectionHook(func(c msqlite.ExecQuerierContext, dsn string) error {
		v, ok := opened.Load(dsn)
		if !ok {
			return nil
		}
		st := &connState{d: v.(*DB), written: map[string]bool{}}
		conns.Store(c, st)
		if h, ok := c.(msqlite.HookRegisterer); ok {
			// Every row written (by triggers and foreign keys' actions too).
			h.RegisterPreUpdateHook(func(u msqlite.SQLitePreUpdateData) { st.written[u.TableName] = true })
			h.RegisterCommitHook(func() int32 {
				st.d.wrote(st.written)
				clear(st.written)
				return 0
			})
			h.RegisterRollbackHook(func() { clear(st.written) })
		}
		if a, ok := c.(msqlite.AuthorizerRegisterer); ok {
			return a.RegisterAuthorizer(func(action msqlite.AuthorizerActionCode, arg1, _, _, _ string) msqlite.AuthorizerReturnCode {
				switch action {
				case msqlite.AuthRead:
					if st.reads != nil && !strings.HasPrefix(arg1, "sqlite_") {
						st.reads[arg1] = true
					}
				case msqlite.AuthInsert, msqlite.AuthUpdate, msqlite.AuthDelete:
					// Also a delete of every row, which SQLite may do without
					// visiting them (its truncate optimization).
					if !strings.HasPrefix(arg1, "sqlite_") {
						st.written[arg1] = true
					}
				}
				return msqlite.AuthorizerOK
			})
		}
		return nil
	})
}
