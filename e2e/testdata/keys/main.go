// Keys is a web e2e's app (e2e/keys_test.go): a search field whose
// surroundings take Up, Down and Enter (ui.KeyMap, a launcher's), a
// field with an Escape of its own, the window's keys (ui.UseKeys), a
// list's rows inside a view with keys, a sheet whose keys go on to the
// window's, and a panel (a Window) with keys of its own and a sheet of
// its own, each on a line the test reads.
package main

import (
	"fmt"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

var fruits = []string{"apple", "banana", "cherry"}

func App() ui.Node {
	sel := signal.New(0)
	opened := signal.New("none")
	submitted := signal.New(0)
	escaped := signal.New(0)
	spaces := signal.New(0)
	saved := signal.New(0)
	mainEscapes := signal.New(0)
	panelKeys := signal.New("")
	panelOpen := signal.New(false)
	listKeys := signal.New("")
	sheetKeys := signal.New("")
	sheetOpen := signal.New(false)
	panelSheet := signal.New(false)
	count := func(s *signal.Signal[int]) func() { return func() { s.Set(s.Peek() + 1) } }
	line := func(f func() string) ui.Node { return ui.Text(signal.Func(f)) }

	// The window's: with nothing focused (a viewer's Space), and from
	// anywhere in it, last (⌘S, Ctrl+S).
	ui.UseKeys(ui.KeyMap{
		{Key: "space"}:  count(spaces),
		ui.Cmd("s"):     count(saved),
		{Key: "escape"}: count(mainEscapes),
	})
	move := func(by int) func() {
		return func() { sel.Set((sel.Peek() + by + len(fruits)) % len(fruits)) }
	}
	field := ui.TextField(ui.TextFieldProps{
		Value:       signal.New(""),
		Placeholder: ui.Val("Search"),
		OnSubmit:    count(submitted),
		Keys:        ui.KeyMap{{Key: "escape"}: count(escaped)},
	})
	return ui.SafeArea(ui.Style{Padding: ui.All(12), Gap: 6},
		ui.StaticText("status: ready"),
		ui.View(ui.ViewProps{
			Style: ui.Style{Gap: 6},
			Keys: ui.KeyMap{
				{Key: "down"}:  move(1),
				{Key: "up"}:    move(-1),
				{Key: "enter"}: func() { opened.Set(fruits[sel.Peek()]) },
			},
		},
			field,
			line(func() string { return "selected: " + fruits[sel.Get()] }),
		),
		line(func() string { return "opened: " + opened.Get() }),
		line(func() string { return fmt.Sprintf("submitted: %d", submitted.Get()) }),
		line(func() string { return fmt.Sprintf("field escape: %d", escaped.Get()) }),
		line(func() string { return fmt.Sprintf("window space: %d", spaces.Get()) }),
		line(func() string { return fmt.Sprintf("window save: %d", saved.Get()) }),
		line(func() string { return fmt.Sprintf("window escape: %d", mainEscapes.Get()) }),
		line(func() string { return "panel keys:" + panelKeys.Get() }),
		line(func() string { return "list keys:" + listKeys.Get() }),
		line(func() string { return "sheet keys:" + sheetKeys.Get() }),
		// A list's rows: a key in a row's field goes on to the view around
		// the list.
		ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(100)}, Keys: ui.KeyMap{
			{Key: "pagedown"}: func() { listKeys.Set(listKeys.Peek() + " pagedown") },
		}}, ui.List(ui.ListProps{}, signal.Static([]int{1, 2}), func(i int) int { return i },
			func(signal.Accessor[int], signal.Accessor[int]) ui.Node {
				return ui.Row(ui.Style{Height: ui.Pt(40)}, ui.TextField(ui.TextFieldProps{Value: signal.New(""), Placeholder: ui.Val("Row")}))
			})),
		ui.Button(ui.ButtonProps{Title: ui.Val("Open sheet"), OnPress: func() { sheetOpen.Set(true) }}),
		// The sheet's keys come first, then the window's.
		ui.Sheet(ui.SheetProps{Open: sheetOpen, Content: func() ui.Node {
			return ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(12)}, Keys: ui.KeyMap{
				{Key: "escape"}: func() { sheetKeys.Set(sheetKeys.Peek() + " escape") },
			}},
				ui.TextField(ui.TextFieldProps{Value: signal.New(""), Placeholder: ui.Val("In the sheet")}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Close sheet"), OnPress: func() { sheetOpen.Set(false) }}),
			)
		}}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Open panel"), OnPress: func() { panelOpen.Set(true) }}),
		ui.Window(ui.WindowProps{Open: panelOpen, Panel: true, Width: 320, Height: 120, Content: func() ui.Node {
			// The panel's own: a key in it goes no further than these.
			ui.UseKeys(ui.KeyMap{
				{Key: "escape"}: func() { panelKeys.Set(panelKeys.Peek() + " escape") },
				{Key: "down"}:   func() { panelKeys.Set(panelKeys.Peek() + " down") },
			})
			return ui.Column(ui.Style{Padding: ui.All(12)},
				ui.TextField(ui.TextFieldProps{Value: signal.New(""), Placeholder: ui.Val("In the panel")}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Open panel sheet"), OnPress: func() { panelSheet.Set(true) }}),
				// Presented from the panel: its keys go on to the panel's.
				ui.Sheet(ui.SheetProps{Open: panelSheet, Content: func() ui.Node {
					return ui.Column(ui.Style{Padding: ui.All(12)},
						ui.TextField(ui.TextFieldProps{Value: signal.New(""), Placeholder: ui.Val("In the panel's sheet")}))
				}}),
			)
		}}),
	)
}

func main() { app.Run(App) }
