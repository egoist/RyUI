// Launcher is a Spotlight of its own: an app in the background, with no
// Dock icon and no window, whose panel a global shortcut (⌥Space, Alt+Space)
// summons over whatever app the user is in, without taking that app out
// of the front. It searches the apps installed as the user types, Enter
// opens the first, a click any; Escape, or a click elsewhere, puts the
// panel away. Its tray icon (the menu bar on a Mac) opens it too, and
// quits it.
//
//	go run ./cmd/asuka dev examples/launcher
package main

import (
	"slices"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/apps"
	"github.com/egoist/asuka/hotkey"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/tray"
	"github.com/egoist/asuka/ui"
)

func main() {
	// asuka.json says so for a build; this does it for go run too.
	app.RunInBackground()
	app.RunWindow(ui.WindowOptions{
		Title:      ui.Val("Launcher"),
		Panel:      true, // floats over every app, takes the keyboard without activating the app
		Hidden:     true, // until the shortcut shows it
		FitContent: true, // as tall as its results
		MinWidth:   640, MaxWidth: 640, MaxHeight: 440,
	}, Launcher)
}

// shown is how many results show at most.
const shown = 8

func Launcher() ui.Node {
	w := ui.UseWindow()
	query := signal.New("")
	installed := signal.NewFunc([]apps.App(nil), slices.Equal[[]apps.App])
	results := signal.NewMemoFunc(func() []apps.App {
		q := query.Get()
		if q == "" {
			return nil
		}
		r := apps.Search(installed.Get(), q)
		if len(r) > shown {
			r = r[:shown]
		}
		return r
	}, slices.Equal[[]apps.App])
	var field ui.Node

	// Read the apps again each time it shows: one may have come or gone.
	refresh := func() {
		apps.List(func(list []apps.App, err error) {
			if err == nil {
				installed.Set(list)
			}
		})
	}
	show := func() {
		query.Set("")
		refresh()
		w.Show()
		ui.Focus(field)
	}
	toggle := func() {
		if signal.Untrack(w.Visible().Get) {
			w.Hide()
			return
		}
		show()
	}
	open := func(a apps.App) {
		w.Hide() // first, so the app the user was in has the keyboard until a's takes it
		apps.Open(a, nil)
	}
	// Escape and a click elsewhere close the panel: hide it instead, so
	// it shows again at once.
	w.OnCloseRequest(func() bool { w.Hide(); return false })

	hotkey.Register(hotkey.Props{
		Shortcut:    ui.Val(ui.Shortcut{Key: "space", Mods: ui.Alt}),
		Description: ui.Val("Show the launcher"),
		OnPress:     toggle,
	})
	tray.Show(tray.Props{
		ID:      "launcher",
		Symbol:  ui.Val("magnifyingglass"),
		Tooltip: ui.Val("Launcher"),
		Menu: ui.Val([]ui.MenuItem{
			{Title: "Show Launcher", OnPress: show},
			ui.MenuSeparator,
			ui.MenuQuit,
		}),
	})

	field = ui.TextField(ui.TextFieldProps{
		Value:       query,
		Placeholder: ui.Val("Search apps"),
		Plain:       true,
		Autocorrect: ui.AutocorrectNo,
		Style:       ui.Style{Grow: 1, Font: ui.Title2},
		OnSubmit: func() {
			if r := signal.Untrack(results.Get); len(r) > 0 {
				open(r[0])
			}
		},
	})
	return ui.Column(ui.Style{},
		ui.Row(ui.Style{Padding: ui.Edges{Top: 14, Bottom: 14, Left: 18, Right: 18}, Gap: 12, Align: ui.AlignCenter},
			ui.Symbol("magnifyingglass", ui.Style{Font: ui.Title2, Color: ui.SecondaryLabel}),
			field,
		),
		ui.Show(signal.Func(func() bool { return len(results.Get()) > 0 }), func() ui.Node {
			return ui.Column(ui.Style{Padding: ui.Edges{Bottom: 8, Left: 8, Right: 8}},
				ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(1), Background: ui.Separator, Margin: ui.Edges{Bottom: 6, Left: 10, Right: 10}}}),
				ui.For(results, func(a apps.App) string { return a.Path }, func(a signal.Accessor[apps.App], i signal.Accessor[int]) ui.Node {
					return row(a, i, open)
				}),
			)
		}),
	)
}

// row is an app among the results: its icon and name, the first one
// highlighted (Enter opens it), the one under the pointer too.
func row(a signal.Accessor[apps.App], i signal.Accessor[int], open func(apps.App)) ui.Node {
	icon := signal.New("")
	signal.Effect(func() {
		apps.Icon(a.Get(), 32, func(path string, err error) {
			if err == nil {
				icon.Set(path)
			}
		})
	})
	first := func() bool { return i.Get() == 0 }
	base := ui.Style{Padding: ui.Edges{Top: 6, Bottom: 6, Left: 10, Right: 10}, Gap: 12, Align: ui.AlignCenter,
		Direction: ui.RowDir, CornerRadius: 8,
		Hovered: &ui.Style{Background: ui.Separator}}
	return ui.View(ui.ViewProps{
		Style:   base.When(first, ui.Style{Background: ui.Separator}),
		OnPress: func() { open(signal.Untrack(a.Get)) },
		Label:   signal.Func(func() string { return a.Get().Name }),
	},
		ui.Image(ui.ImageProps{Source: icon, Style: ui.Style{Width: ui.Pt(32), Height: ui.Pt(32)}}),
		ui.Text(signal.Func(func() string { return a.Get().Name }), ui.Style{Font: ui.Body}),
	)
}
