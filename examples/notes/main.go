// Notes keeps notes in SQLite: a live query feeds the list, so adding,
// checking off and deleting (swipe a note) show as soon as they're
// written.
//
//	go run ./cmd/nui dev -ios examples/notes
package main

import (
	"strings"

	"github.com/egoist/nui/app"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/sqlite"
	"github.com/egoist/nui/ui"
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

// write runs a statement off the main thread.
func write(query string, args ...any) {
	go func() {
		if _, err := db.Exec(query, args...); err != nil {
			ui.Post(func() { ui.Alert(ui.AlertProps{Title: "Couldn't save", Message: err.Error()}) })
		}
	}()
}

func App() ui.Node {
	query := signal.New("")
	return ui.Stack(func() ui.Node {
		notes := sqlite.Watch[note](db, `select * from notes order by done, id desc`)
		draft := signal.New("")
		add := func() {
			if t := draft.Peek(); t != "" {
				write(`insert into notes (text) values (?)`, t)
				draft.Set("")
			}
		}
		rows := signal.Func(func() []note {
			q := strings.ToLower(query.Get())
			var out []note
			for _, n := range notes.Get().Rows {
				if q == "" || strings.Contains(strings.ToLower(n.Text), q) {
					out = append(out, n)
				}
			}
			return out
		})
		return ui.SafeArea(ui.Style{Grow: 1},
			ui.Row(ui.Style{Gap: 8, Padding: ui.Symmetric(8, 16), Align: ui.AlignEnd},
				ui.TextField(ui.TextFieldProps{Value: draft, Placeholder: ui.Val("New note"), ReturnKey: ui.ReturnDone,
					OnSubmit: add, Style: ui.Style{Grow: 1}}),
				ui.Button(ui.ButtonProps{Icon: ui.IconPlus, Label: "Add", Variant: ui.ButtonFilled, OnPress: add,
					Disabled: signal.Func(func() bool { return draft.Get() == "" })}),
			),
			ui.Show(signal.Func(func() bool { r := notes.Get(); return !r.Loading && len(r.Rows) == 0 }), func() ui.Node {
				return ui.Column(ui.Style{Grow: 1, Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 8},
					ui.Symbol(ui.IconDocument, ui.Style{Font: ui.LargeTitle, Color: ui.TertiaryLabel}),
					ui.StaticText("No notes yet", ui.Style{Color: ui.SecondaryLabel}))
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
	app.Run(App)
}
