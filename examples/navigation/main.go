// Navigation shows the app structure components: Tabs holding Stacks
// (native push, pop and back gestures), a refreshable List, bar buttons,
// a Sheet with detents, an Alert, menus and haptics.
//
//	go run ./cmd/ryui dev -ios examples/navigation
//	go run ./cmd/ryui dev -android examples/navigation
package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

type message struct {
	id      int
	from    string
	subject string
	body    string
	starred bool
}

var senders = []string{"Ada", "Grace", "Linus", "Margaret", "Ken", "Barbara"}

func newMessage(id int) message {
	return message{
		id:      id,
		from:    senders[id%len(senders)],
		subject: fmt.Sprintf("Message %d", id),
		body:    "This is a message. Open it to see the detail screen, with bar buttons, a sheet, a menu and an alert.",
	}
}

func App() ui.Node {
	tab := signal.New(0)
	return ui.Tabs(ui.TabsProps{Selected: tab},
		ui.Tab{Title: "Inbox", Icon: ui.IconList, Content: inbox},
		ui.Tab{Title: "Settings", Icon: ui.IconSettings, Content: settings},
	)
}

func inbox() ui.Node {
	var start []message
	for i := 20; i > 0; i-- {
		start = append(start, newMessage(i))
	}
	next := 21
	messages := signal.NewFunc(start, nil)
	refreshing := signal.New(false)

	update := func(id int, f func(*message)) {
		messages.Update(func(ms []message) []message {
			ms = slices.Clone(ms)
			for i := range ms {
				if ms[i].id == id {
					f(&ms[i])
				}
			}
			return ms
		})
	}
	remove := func(id int) {
		messages.Update(func(ms []message) []message {
			return slices.DeleteFunc(slices.Clone(ms), func(m message) bool { return m.id == id })
		})
	}

	return ui.Stack(func() ui.Node {
		nav := ui.UseNavigator()
		return ui.List(ui.ListProps{
			Refreshing: refreshing,
			OnRefresh: func() {
				refreshing.Set(true)
				go func() { // pretend to fetch
					time.Sleep(800 * time.Millisecond)
					ui.Post(func() {
						messages.Update(func(ms []message) []message { return append([]message{newMessage(next)}, ms...) })
						next++
						refreshing.Set(false)
					})
				}()
			},
		}, messages, func(m message) int { return m.id }, func(m signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
			open := func() {
				msg := m.Get() // in a handler: not tracked
				nav.Push(func() ui.Node { return detail(msg, update, remove) }, ui.ScreenOptions{
					Title: ui.Val(msg.from),
					Actions: []ui.Action{{Title: "Star", OnPress: func() {
						update(msg.id, func(x *message) { x.starred = !x.starred })
						ui.Haptic(ui.HapticSelection)
					}}},
				})
			}
			return ui.View(ui.ViewProps{
				OnPress: open,
				Style:   ui.Style{Direction: ui.RowDir, Align: ui.AlignCenter, Gap: 12, Padding: ui.Symmetric(10, 16)},
			},
				ui.Column(ui.Style{Grow: 1, Gap: 2},
					ui.Text(signal.Func(func() string { return m.Get().from }), ui.Style{Font: ui.Headline}),
					ui.Text(signal.Func(func() string { return m.Get().subject }), ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel}),
				),
				ui.Text(signal.Func(func() string {
					if m.Get().starred {
						return "★"
					}
					return ""
				}), ui.Style{Color: ui.Orange}),
			)
		})
	}, ui.ScreenOptions{Title: ui.Val("Inbox")})
}

func detail(msg message, update func(int, func(*message)), remove func(int)) ui.Node {
	nav := ui.UseNavigator()
	sheet := signal.New(false)
	note := signal.New("")
	confirmDelete := func() {
		ui.Alert(ui.AlertProps{
			Title:   "Delete this message?",
			Message: "You can't undo this.",
			Actions: []ui.Action{
				{Title: "Cancel", Role: ui.ActionCancel},
				{Title: "Delete", Role: ui.ActionDestructive, OnPress: func() {
					remove(msg.id)
					ui.Haptic(ui.HapticSuccess)
					nav.Pop()
				}},
			},
		})
	}
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16},
			ui.StaticText(msg.subject, ui.Style{Font: ui.Title2.Weighted(ui.Bold)}),
			ui.StaticText(msg.body, ui.Style{Color: ui.SecondaryLabel}),
			ui.Text(signal.Func(func() string {
				if n := note.Get(); n != "" {
					return "Note: " + n
				}
				return "No note yet."
			}), ui.Style{Font: ui.Footnote, Color: ui.TertiaryLabel}),
			ui.Row(ui.Style{Gap: 12, Wrap: true},
				ui.Button(ui.ButtonProps{Title: ui.Val("Add a note"), Variant: ui.ButtonFilled, OnPress: func() { sheet.Set(true) }}),
				ui.Button(ui.ButtonProps{Title: ui.Val("More"), Variant: ui.ButtonGray, Menu: []ui.Action{
					{Title: "Star", OnPress: func() { update(msg.id, func(x *message) { x.starred = !x.starred }) }},
					{Title: "Delete…", Role: ui.ActionDestructive, OnPress: confirmDelete},
				}}),
			),
			ui.Menu(ui.MenuProps{LongPress: true, Actions: []ui.Action{
				{Title: "Reply", OnPress: func() { ui.Haptic(ui.HapticLight) }},
				{Title: "Delete…", Role: ui.ActionDestructive, OnPress: confirmDelete},
			}, Style: ui.Style{Padding: ui.All(16), Background: ui.SecondaryBackground, CornerRadius: 12}},
				ui.StaticText("Long-press here for a context menu"),
			),
			ui.Sheet(ui.SheetProps{Open: sheet, Detents: ui.DetentMedium | ui.DetentLarge, Grabber: true, Content: func() ui.Node {
				draft := signal.New(note.Peek())
				return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
					ui.StaticText("Note", ui.Style{Font: ui.Title3.Weighted(ui.Semibold)}),
					ui.TextField(ui.TextFieldProps{Value: draft, Placeholder: ui.Val("Write a note"), ReturnKey: ui.ReturnDone,
						OnSubmit: func() { note.Set(draft.Peek()); sheet.Set(false) }}),
					ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, OnPress: func() {
						note.Set(draft.Peek())
						sheet.Set(false)
					}}),
				)
			}}),
		),
	)
}

func settings() ui.Node {
	return ui.Stack(func() ui.Node {
		nav := ui.UseNavigator()
		notifications := signal.New(true)
		haptics := signal.New(true)
		row := func(label string, v *signal.Signal[bool]) ui.Node {
			return ui.Row(ui.Style{Align: ui.AlignCenter},
				ui.StaticText(label, ui.Style{Grow: 1}),
				ui.Toggle(ui.ToggleProps{Value: v}),
			)
		}
		return ui.SafeArea(ui.Style{Padding: ui.Symmetric(12, 20), Gap: 16},
			row("Notifications", notifications),
			row("Haptics", haptics),
			ui.Button(ui.ButtonProps{Title: ui.Val("Try a haptic"), Variant: ui.ButtonTinted, OnPress: func() {
				if haptics.Peek() {
					ui.Haptic(ui.HapticSuccess)
				}
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("About"), OnPress: func() {
				nav.Push(func() ui.Node {
					return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 8},
						ui.StaticText("ryui", ui.Style{Font: ui.LargeTitle.Weighted(ui.Bold)}),
						ui.StaticText("Native widgets, driven by Go and fine-grained signals.", ui.Style{Color: ui.SecondaryLabel}),
					)
				}, ui.ScreenOptions{Title: ui.Val("About")})
			}}),
		)
	}, ui.ScreenOptions{Title: ui.Val("Settings")})
}

func main() { app.Run(App) }
