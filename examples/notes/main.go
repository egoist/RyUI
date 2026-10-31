// Notes keeps notes in SQLite: a live query feeds the list, so adding,
// checking off and deleting (swipe a note) show as soon as they're
// written.
//
//	go run ./cmd/ryui dev -ios examples/notes
package main

import (
	"database/sql"
	"fmt"
	"net/url"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/sqlite"
	"github.com/egoist/ryui/ui"
)

type note struct {
	ID   int64
	Text string
	Done bool
}

var db *sqlite.DB

func openDB() {
	var err error
	if db, err = sqlite.Open("notes.db"); err != nil {
		panic(err)
	}
	if err := db.Migrate(
		`create table notes (id integer primary key, text text not null, done bool not null default 0)`,
	); err != nil {
		panic(err)
	}
}

// write runs a statement off the main thread; a failure shows.
func write(query string, args ...any) {
	db.ExecAsync(query, args, func(_ sql.Result, err error) {
		if err != nil {
			ui.Alert(ui.AlertProps{Title: "Couldn't save", Message: err.Error()})
		}
	})
}

func App() ui.Node {
	query := signal.New("")
	// ryuinotes://add?text=... adds a note (ryui.json's scheme).
	ui.OnOpenURL(func(raw string) {
		if u, err := url.Parse(raw); err == nil && u.Host == "add" {
			if t := u.Query().Get("text"); t != "" {
				write(`insert into notes (text) values (?)`, t)
			}
		}
	})
	return ui.Stack(func() ui.Node {
		// The search is the query's: SQLite filters, and the rows follow
		// what's typed (WatchWith runs the query again as it changes).
		notes := sqlite.WatchWith[note](db,
			`select * from notes where ?1 = '' or instr(lower(text), lower(?1)) > 0 order by done, id desc`,
			func() []any { return []any{query.Get()} })
		draft := signal.New("")
		add := func() {
			if t := draft.Peek(); t != "" {
				write(`insert into notes (text) values (?)`, t)
				draft.Set("")
			}
		}
		rows := signal.Func(func() []note { return notes.Get().Rows })
		stats := signal.New(false)
		return ui.SafeArea(ui.Style{Grow: 1},
			// On a desktop: the menu bar's commands, with shortcuts, and a
			// window of statistics. A phone has neither.
			ui.MenuBar(signal.Static([]ui.MenuBarMenu{
				{Title: "File", Items: []ui.MenuItem{{Title: "Add Note", Shortcut: ui.Cmd("enter"), OnPress: add}}},
				{Title: "Edit", Items: []ui.MenuItem{{Title: "Clear Completed", Shortcut: ui.Cmd("k").With(ui.Shift), Destructive: true,
					OnPress: func() { write(`delete from notes where done`) }}}},
				{Title: "Window", Items: []ui.MenuItem{{Title: "Statistics", Shortcut: ui.Cmd("i"), OnPress: func() { stats.Set(true) }}}},
			})),
			ui.Window(ui.WindowProps{Open: stats, Title: ui.Val("Statistics"), Width: 280, Height: 140, Content: func() ui.Node {
				return ui.Column(ui.Style{Padding: ui.All(20), Gap: 6},
					ui.Text(signal.Func(func() string { return fmt.Sprintf("%d notes", len(rows.Get())) }), ui.Style{Font: ui.Title2}),
					ui.Text(signal.Func(func() string {
						done := 0
						for _, n := range rows.Get() {
							if n.Done {
								done++
							}
						}
						return fmt.Sprintf("%d done", done)
					}), ui.Style{Color: ui.SecondaryLabel}))
			}}),
			ui.Row(ui.Style{Gap: 8, Padding: ui.Symmetric(8, 16), Align: ui.AlignEnd},
				ui.TextField(ui.TextFieldProps{Value: draft, Placeholder: ui.Val("New note"), ReturnKey: ui.ReturnDone,
					OnSubmit: add, Style: ui.Style{Grow: 1}}),
				ui.Button(ui.ButtonProps{Icon: ui.IconPlus, Label: "Add", Variant: ui.ButtonFilled, OnPress: add,
					Disabled: signal.Func(func() bool { return draft.Get() == "" })}),
			),
			ui.Show(signal.Func(func() bool { r := notes.Get(); return !r.Loading && len(r.Rows) == 0 }), func() ui.Node {
				return ui.Column(ui.Style{Grow: 1, Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 8},
					ui.Symbol(ui.IconDocument, ui.Style{Font: ui.LargeTitle, Color: ui.TertiaryLabel}),
					ui.Text(signal.Func(func() string {
						if query.Get() != "" {
							return "No notes match"
						}
						return "No notes yet"
					}), ui.Style{Color: ui.SecondaryLabel}))
			}),
			ui.List(ui.ListProps{SwipeActions: func(i int) []ui.Action {
				return []ui.Action{{Title: "Delete", Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() {
					if l := signal.Untrack(rows.Get); i < len(l) {
						write(`delete from notes where id = ?`, l[i].ID)
					}
				}}}
			}}, rows, func(n note) int64 { return n.ID }, func(n signal.Accessor[note], _ signal.Accessor[int]) ui.Node {
				return ui.View(ui.ViewProps{
					OnPress: func() { m := signal.Untrack(n.Get); write(`update notes set done = ? where id = ?`, !m.Done, m.ID) },
					Style:   ui.Style{Direction: ui.RowDir, Gap: 12, Padding: ui.Symmetric(14, 20), Align: ui.AlignCenter},
				},
					ui.Dynamic(func() ui.Node {
						if n.Get().Done {
							return ui.Symbol("checkmark.circle.fill", ui.Style{Color: ui.Accent})
						}
						return ui.Symbol("circle", ui.Style{Color: ui.TertiaryLabel})
					}),
					ui.Text(signal.Func(func() string { return n.Get().Text }), ui.StyleFunc(func() ui.Style {
						if n.Get().Done {
							return ui.Style{Grow: 1, Color: ui.SecondaryLabel}
						}
						return ui.Style{Grow: 1}
					})),
				)
			}),
		)
	}, ui.ScreenOptions{Title: ui.Val("Notes"), Search: &ui.Search{Text: query, Placeholder: "Search notes"}})
}

func main() {
	openDB()
	app.RunWindow(ui.WindowOptions{Width: 420, Height: 640, MinWidth: 320, MinHeight: 360}, App)
}
