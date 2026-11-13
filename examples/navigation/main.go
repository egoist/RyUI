// Navigation shows the app structure components: Tabs holding Stacks
// (native push, pop and back gestures), a refreshable List, bar buttons,
// a bottom toolbar and a primary action (a floating button on Android),
// a Sheet with detents, a full-screen Sheet, Alerts (one with text
// fields), menus and haptics.
//
//	go run ./cmd/asuka dev --platform ios examples/navigation
//	go run ./cmd/asuka dev --platform android examples/navigation
package main

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

type message struct {
	id      int
	from    string
	subject string
	body    string
	starred bool
	unread  bool
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
	screen := ui.UseEnvironment() // the root's: the whole screen
	var start []message
	for i := 20; i > 0; i-- {
		m := newMessage(i)
		m.unread = i > 17
		start = append(start, m)
	}
	messages := signal.NewFunc(start, nil)
	// The Inbox tab's badge counts the unread messages: opening one reads
	// it, and a refresh brings a new one.
	unread := signal.Func(func() string {
		n := 0
		for _, m := range messages.Get() {
			if m.unread {
				n++
			}
		}
		if n == 0 {
			return ""
		}
		return strconv.Itoa(n)
	})
	return ui.Tabs(ui.TabsProps{Selected: tab},
		ui.Tab{Title: ui.Val("Inbox"), Icon: ui.IconList, Badge: unread, Content: func() ui.Node { return inbox(messages, screen) }},
		ui.Tab{Title: ui.Val("Settings"), Icon: ui.IconSettings, Content: settings},
	)
}

func inbox(messages *signal.Signal[[]message], screen signal.Accessor[ui.Environment]) ui.Node {
	next := 21
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

	compose := func() {
		messages.Update(func(ms []message) []message { return append([]message{newMessage(next)}, ms...) })
		next++
		ui.Haptic(ui.HapticSuccess)
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
						m := newMessage(next)
						m.unread = true
						messages.Update(func(ms []message) []message { return append([]message{m}, ms...) })
						next++
						refreshing.Set(false)
					})
				}()
			},
		}, messages, func(m message) int { return m.id }, func(m signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
			open := func() {
				msg := m.Get() // in a handler: not tracked
				if msg.unread {
					update(msg.id, func(x *message) { x.unread = false })
				}
				star := func() {
					update(msg.id, func(x *message) { x.starred = !x.starred })
					ui.Haptic(ui.HapticSelection)
				}
				starred := signal.Func(func() bool {
					for _, x := range messages.Get() {
						if x.id == msg.id {
							return x.starred
						}
					}
					return false
				})
				nav.Push(func() ui.Node { return detail(msg, update, remove, screen) }, ui.ScreenOptions{
					Title:   ui.Val(msg.from),
					Actions: []ui.Action{{Title: ui.Val("Star"), OnPress: star}},
					// Mail's bottom toolbar: delete, star, reply's menu; compose
					// at its trailing end (Android: a FAB docked in the bar).
					Toolbar: []ui.Action{
						{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { confirmDelete(msg, remove, nav) }},
						{Title: signal.Func(func() string {
							if starred.Get() {
								return "Unstar"
							}
							return "Star"
						}), Icon: ui.IconStar, Checked: starred, OnPress: star},
						{Title: ui.Val("Reply"), Icon: "arrowshape.turn.up.left", Children: []ui.Action{
							{Title: ui.Val("Reply"), Icon: "arrowshape.turn.up.left", OnPress: func() { ui.Haptic(ui.HapticLight) }},
							{Title: ui.Val("Forward"), Icon: "arrowshape.turn.up.forward", OnPress: func() { ui.Haptic(ui.HapticLight) }},
						}},
						{Title: ui.Val("Mark as unread"), Icon: "envelope", Disabled: ui.Val(true)},
					},
					Primary: &ui.Action{Title: ui.Val("Compose"), Icon: "square.and.pencil", OnPress: compose},
				})
			}
			return ui.View(ui.ViewProps{
				OnPress: open,
				Style:   ui.Style{Direction: ui.RowDir, Align: ui.AlignCenter, Gap: 12, Padding: ui.Symmetric(10, 16)},
			},
				ui.Column(ui.Style{Grow: 1, Gap: 2},
					ui.Text(signal.Func(func() string { return m.Get().from }), ui.Style{Font: fonts.Headline}),
					ui.Text(signal.Func(func() string { return m.Get().subject }), ui.Style{Font: fonts.Subheadline, Color: colors.SecondaryLabel}),
				),
				ui.Text(signal.Func(func() string {
					if m.Get().unread {
						return "●"
					}
					return ""
				}), ui.Style{Color: colors.Blue}),
				ui.Text(signal.Func(func() string {
					if m.Get().starred {
						return "★"
					}
					return ""
				}), ui.Style{Color: colors.Orange}),
			)
		})
	}, ui.ScreenOptions{
		Title: ui.Val("Inbox"),
		// Android: an extended FAB, shrinking as the list scrolls down.
		Primary:         &ui.Action{Title: ui.Val("Compose"), Icon: "square.and.pencil", OnPress: compose},
		PrimaryExtended: true,
	})
}

func confirmDelete(msg message, remove func(int), nav *ui.Navigator) {
	ui.Alert(ui.AlertProps{
		Title:   "Delete this message?",
		Message: "You can't undo this.",
		Actions: []ui.Action{
			{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
			{Title: ui.Val("Delete"), Role: ui.ActionDestructive, OnPress: func() {
				remove(msg.id)
				ui.Haptic(ui.HapticSuccess)
				nav.Pop()
			}},
		},
	})
}

func detail(msg message, update func(int, func(*message)), remove func(int), screen signal.Accessor[ui.Environment]) ui.Node {
	nav := ui.UseNavigator()
	sheet := signal.New(false)
	reply := signal.New(false)
	note := signal.New("")
	confirmDelete := func() { confirmDelete(msg, remove, nav) }
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}},
		ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 16},
			ui.StaticText(msg.subject, ui.Style{Font: fonts.Title2.Weighted(fonts.Bold)}),
			ui.StaticText(msg.body, ui.Style{Color: colors.SecondaryLabel}),
			ui.Text(signal.Func(func() string {
				if n := note.Get(); n != "" {
					return "Note: " + n
				}
				return "No note yet."
			}), ui.Style{Font: fonts.Footnote, Color: colors.TertiaryLabel}),
			ui.Row(ui.Style{Gap: 12, Wrap: true},
				ui.Button(ui.ButtonProps{Title: ui.Val("Add a note"), Variant: ui.ButtonFilled, OnPress: func() { sheet.Set(true) }}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Write a Reply"), Variant: ui.ButtonGray, OnPress: func() { reply.Set(true) }}),
				ui.Button(ui.ButtonProps{Title: ui.Val("More"), Variant: ui.ButtonGray, Menu: []ui.Action{
					{Title: ui.Val("Star"), OnPress: func() { update(msg.id, func(x *message) { x.starred = !x.starred }) }},
					{Title: ui.Val("Delete…"), Role: ui.ActionDestructive, OnPress: confirmDelete},
				}}),
			),
			ui.Menu(ui.MenuProps{LongPress: true, Actions: []ui.Action{
				{Title: ui.Val("Reply"), OnPress: func() { ui.Haptic(ui.HapticLight) }},
				{Title: ui.Val("Delete…"), Role: ui.ActionDestructive, OnPress: confirmDelete},
			}, Style: ui.Style{Padding: ui.All(16), Background: colors.SecondaryBackground, CornerRadius: 12}},
				ui.StaticText("Long-press here for a context menu"),
			),
			ui.Sheet(ui.SheetProps{Open: sheet, Detents: ui.DetentMedium | ui.DetentLarge, Grabber: true, Content: func() ui.Node {
				draft := signal.New(note.Peek())
				return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
					ui.StaticText("Note", ui.Style{Font: fonts.Title3.Weighted(fonts.Semibold)}),
					ui.TextField(ui.TextFieldProps{Value: draft, Placeholder: ui.Val("Write a note"), ReturnKey: ui.ReturnDone,
						OnSubmit: func() { note.Set(draft.Peek()); sheet.Set(false) }}),
					ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, OnPress: func() {
						note.Set(draft.Peek())
						sheet.Set(false)
					}}),
				)
			}}),
			ui.Sheet(ui.SheetProps{Open: reply, FullScreen: true, Content: func() ui.Node {
				return composer(msg, reply, screen)
			}}),
		),
	)
}

// composer is a reply, in a full-screen sheet: it takes the screen until
// it's sent or cancelled. Going back (Android's back, Escape) with a
// draft asks first. It shows its environment, and the screen's: on a
// phone they're the same, the whole screen and its safe area.
func composer(msg message, open *signal.Signal[bool], screen signal.Accessor[ui.Environment]) ui.Node {
	env := ui.UseEnvironment()
	draft := signal.New("")
	discard := func() {
		ui.Alert(ui.AlertProps{
			Title: "Discard this reply?",
			Actions: []ui.Action{
				{Title: ui.Val("Keep Editing"), Role: ui.ActionCancel},
				{Title: ui.Val("Discard"), Role: ui.ActionDestructive, OnPress: func() { open.Set(false) }},
			},
		})
	}
	cancel := func() {
		if draft.Peek() == "" {
			open.Set(false)
		} else {
			discard()
		}
	}
	ui.OnBack(func() bool {
		if draft.Peek() == "" {
			return false
		}
		discard()
		return true
	})
	describe := func(what string, e ui.Environment) string {
		return fmt.Sprintf("%s: %.0f × %.0f, safe area %.0f %.0f %.0f %.0f", what, e.Width, e.Height, e.SafeTop, e.SafeRight, e.SafeBottom, e.SafeLeft)
	}
	return ui.SafeArea(ui.Style{Grow: 1, Padding: ui.Symmetric(8, 16), Gap: 12},
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 8},
			ui.Button(ui.ButtonProps{Title: ui.Val("Cancel"), Variant: ui.ButtonPlain, OnPress: cancel}),
			ui.StaticText("Reply", ui.Style{Grow: 1, Font: fonts.Headline, TextAlign: ui.TextCenter}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Send"), Variant: ui.ButtonFilled,
				Disabled: signal.Func(func() bool { return draft.Get() == "" }),
				OnPress: func() {
					ui.Haptic(ui.HapticSuccess)
					open.Set(false)
				}}),
		),
		ui.StaticText("To: "+msg.from, ui.Style{Color: colors.SecondaryLabel}),
		ui.TextField(ui.TextFieldProps{Value: draft, Placeholder: ui.Val("Message"), Multiline: true, Style: ui.Style{Grow: 1}}),
		ui.Text(signal.Func(func() string { return describe("This sheet", env.Get()) }), ui.Style{Font: fonts.Footnote, Color: colors.TertiaryLabel}),
		ui.Text(signal.Func(func() string { return describe("The screen", screen.Get()) }), ui.Style{Font: fonts.Footnote, Color: colors.TertiaryLabel}),
		ui.Text(signal.Func(func() string {
			e, s := env.Get(), screen.Get()
			if e.Width == s.Width && e.Height == s.Height && e.SafeTop == s.SafeTop && e.SafeRight == s.SafeRight && e.SafeBottom == s.SafeBottom && e.SafeLeft == s.SafeLeft {
				return "Covers the screen"
			}
			return ""
		}), ui.Style{Font: fonts.Footnote, Color: colors.Green}),
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
		// An alert with text fields: Sign In is disabled until both are
		// filled in, and follows them as they're typed.
		account := signal.New("")
		signIn := func() {
			user := signal.New(account.Peek())
			pass := signal.New("")
			ui.Alert(ui.AlertProps{
				Title:   "Sign in",
				Message: "Enter your account's name and password.",
				Fields: []ui.AlertField{
					{Text: user, Placeholder: "Name", ContentType: ui.ContentUsername},
					{Text: pass, Placeholder: "Password", Secure: true, ContentType: ui.ContentPassword},
				},
				Actions: []ui.Action{
					{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
					{Title: ui.Val("Sign In"), Disabled: signal.Func(func() bool { return user.Get() == "" || pass.Get() == "" }),
						OnPress: func() { account.Set(user.Peek()) }},
				},
			})
		}
		return ui.SafeArea(ui.Style{Padding: ui.Symmetric(12, 20), Gap: 16},
			row("Notifications", notifications),
			row("Haptics", haptics),
			ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 12},
				ui.Text(signal.Func(func() string {
					if a := account.Get(); a != "" {
						return "Signed in as " + a
					}
					return "Not signed in"
				}), ui.Style{Grow: 1, Color: colors.SecondaryLabel}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Sign In…"), OnPress: signIn}),
			),
			ui.Button(ui.ButtonProps{Title: ui.Val("Try a haptic"), Variant: ui.ButtonTinted, OnPress: func() {
				if haptics.Peek() {
					ui.Haptic(ui.HapticSuccess)
				}
			}}),
			ui.Button(ui.ButtonProps{Title: ui.Val("About"), OnPress: func() {
				nav.Push(func() ui.Node {
					return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 8},
						ui.StaticText("asuka", ui.Style{Font: fonts.LargeTitle.Weighted(fonts.Bold)}),
						ui.StaticText("Native widgets, driven by Go and fine-grained signals.", ui.Style{Color: colors.SecondaryLabel}),
					)
				}, ui.ScreenOptions{Title: ui.Val("About")})
			}}),
		)
	}, ui.ScreenOptions{Title: ui.Val("Settings")})
}

func main() { app.Run(App) }
