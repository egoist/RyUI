// Todo shows For (keyed rows), TextField, Toggle and SafeArea moving
// content above the keyboard.
//
//	go run ./cmd/ryui dev -ios examples/todo
package main

import (
	"fmt"
	"strings"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

type todo struct {
	id    int
	title string
	done  *signal.Signal[bool]
}

func App() ui.Node {
	nextID := 0
	newTodo := func(title string, done bool) *todo {
		nextID++
		return &todo{id: nextID, title: title, done: signal.New(done)}
	}
	todos := signal.NewFunc([]*todo{
		newTodo("Buy milk", false),
		newTodo("Write the iOS host", true),
		newTodo("Port the layout tests", false),
	}, nil)
	draft := signal.New("")

	add := func() {
		title := strings.TrimSpace(draft.Peek())
		if title == "" {
			return
		}
		todos.Update(func(ts []*todo) []*todo { return append(append([]*todo{}, ts...), newTodo(title, false)) })
		draft.Set("")
	}
	remove := func(id int) {
		todos.Update(func(ts []*todo) []*todo {
			var out []*todo
			for _, t := range ts {
				if t.id != id {
					out = append(out, t)
				}
			}
			return out
		})
	}
	left := signal.NewMemo(func() int {
		n := 0
		for _, t := range todos.Get() {
			if !t.done.Get() {
				n++
			}
		}
		return n
	})

	return ui.SafeArea(ui.Style{Padding: ui.Symmetric(8, 16), Gap: 12},
		ui.StaticText("Todos", ui.Style{Font: ui.LargeTitle.Weighted(ui.Bold)}),
		ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
			ui.For(todos, func(t *todo) int { return t.id }, func(item signal.Accessor[*todo], _ signal.Accessor[int]) ui.Node {
				t := item.Get() // a row's key is its todo, so the todo never changes
				return ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 12, Padding: ui.Symmetric(8, 0)},
					ui.Toggle(ui.ToggleProps{Value: t.done}),
					ui.Text(ui.Val(t.title), ui.StyleFunc(func() ui.Style {
						s := ui.Style{Grow: 1}
						if t.done.Get() {
							s.Color = ui.SecondaryLabel
						}
						return s
					})),
					ui.Button(ui.ButtonProps{
						Title:   ui.Val("Delete"),
						Style:   ui.Style{Color: ui.Red},
						OnPress: func() { remove(t.id) },
					}),
				)
			}),
		),
		ui.Text(signal.Func(func() string {
			n := left.Get()
			if n == 1 {
				return "1 thing left"
			}
			return fmt.Sprintf("%d things left", n)
		}), ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),
		ui.Row(ui.Style{Gap: 8, Align: ui.AlignCenter},
			ui.TextField(ui.TextFieldProps{
				Value:       draft,
				Placeholder: ui.Val("What needs doing?"),
				ReturnKey:   ui.ReturnDone,
				OnSubmit:    add,
				Style:       ui.Style{Grow: 1},
			}),
			ui.Button(ui.ButtonProps{
				Title:    ui.Val("Add"),
				Variant:  ui.ButtonFilled,
				Disabled: signal.Func(func() bool { return strings.TrimSpace(draft.Get()) == "" }),
				OnPress:  add,
			}),
		),
	)
}

func main() { app.Run(App) }
