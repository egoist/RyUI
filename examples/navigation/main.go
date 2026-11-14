// Navigation shows the app structure components, routed: every screen
// has an address, as on the web. Tabs hold stacks (native push, pop and
// back gestures), a message's screen is /inbox/:id (a link of the app's
// scheme, asukanav://inbox/18, opens it, with the inbox under it), its
// reply a full-screen sheet of its own address, a guard keeps the
// account screen to a signed-in user (back there after signing in),
// and the browser's address bar, back and forward follow it all. Also: a
// refreshable List, bar buttons, a bottom toolbar and a primary action
// (a floating button on Android), a Sheet with detents opened from code,
// Alerts (one with text fields), menus and haptics.
//
//	go run ./cmd/asuka dev --platform ios examples/navigation
//	go run ./cmd/asuka dev --platform android examples/navigation
//	go run ./cmd/asuka dev --platform web examples/navigation
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

// mail is the inbox's state, which its screens share.
type mail struct {
	messages *signal.Signal[[]message]
	next     int
	screen   signal.Accessor[ui.Environment] // the root's: the whole screen
}

func (m *mail) find(id int) (message, bool) {
	for _, x := range m.messages.Peek() {
		if x.id == id {
			return x, true
		}
	}
	return message{}, false
}

func (m *mail) update(id int, f func(*message)) {
	m.messages.Update(func(ms []message) []message {
		ms = slices.Clone(ms)
		for i := range ms {
			if ms[i].id == id {
				f(&ms[i])
			}
		}
		return ms
	})
}

func (m *mail) remove(id int) {
	m.messages.Update(func(ms []message) []message {
		return slices.DeleteFunc(slices.Clone(ms), func(x message) bool { return x.id == id })
	})
}

func (m *mail) compose() {
	m.messages.Update(func(ms []message) []message { return append([]message{newMessage(m.next)}, ms...) })
	m.next++
	ui.Haptic(ui.HapticSuccess)
}

// starred follows a message's star.
func (m *mail) starred(id int) signal.Accessor[bool] {
	return signal.Func(func() bool {
		for _, x := range m.messages.Get() {
			if x.id == id {
				return x.starred
			}
		}
		return false
	})
}

func (m *mail) star(id int) {
	m.update(id, func(x *message) { x.starred = !x.starred })
	ui.Haptic(ui.HapticSelection)
}

func App() ui.Node {
	var start []message
	for i := 20; i > 0; i-- {
		msg := newMessage(i)
		msg.unread = i > 17
		start = append(start, msg)
	}
	m := &mail{messages: signal.NewFunc(start, nil), next: 21, screen: ui.UseEnvironment()}
	account := signal.New("") // who's signed in ("": nobody)
	// The Inbox tab's badge counts the unread messages: opening one reads
	// it, and a refresh brings a new one.
	unread := signal.Func(func() string {
		n := 0
		for _, msg := range m.messages.Get() {
			if msg.unread {
				n++
			}
		}
		if n == 0 {
			return ""
		}
		return strconv.Itoa(n)
	})
	compose := &ui.Action{Title: ui.Val("Compose"), Icon: "square.and.pencil", OnPress: m.compose}
	return ui.Router(ui.RouterProps{},
		ui.Route{Path: "/", Redirect: "/inbox"},
		ui.Route{Layout: ui.LayoutTabs, Children: []ui.Route{
			{Path: "/inbox", Title: ui.Val("Inbox"), Icon: ui.IconList, Badge: unread, Layout: ui.LayoutStack, Children: []ui.Route{
				{Screen: func(ui.Params) ui.Node { return inbox(m) }, Options: func(ui.Params) ui.ScreenOptions {
					// Android: an extended FAB, shrinking as the list scrolls down.
					return ui.ScreenOptions{Title: ui.Val("Inbox"), Primary: compose, PrimaryExtended: true}
				}},
				{Path: ":id", Screen: func(p ui.Params) ui.Node { return detail(m, p.Int("id")) }, Options: func(p ui.Params) ui.ScreenOptions {
					return detailOptions(m, p.Int("id"), compose)
				}},
				// A reply takes the screen until it's sent or given up.
				{Path: ":id/reply", Sheet: &ui.SheetRoute{FullScreen: true}, Screen: func(p ui.Params) ui.Node { return composer(m, p.Int("id")) }},
			}},
			{Path: "/settings", Title: ui.Val("Settings"), Icon: ui.IconSettings, Layout: ui.LayoutStack, Children: []ui.Route{
				{Screen: func(ui.Params) ui.Node { return settings(account) }, Title: ui.Val("Settings")},
				{Path: "about", Screen: about, Title: ui.Val("About")},
				// Only a signed-in user sees their account: the others sign in
				// first, and come back here after.
				{Path: "account", Screen: func(ui.Params) ui.Node { return accountScreen(account) }, Title: ui.Val("Account"),
					Guard: func(to ui.Href) ui.Href {
						if account.Get() == "" {
							return ui.Href("/settings/sign-in").With("next", string(to))
						}
						return ""
					}},
				{Path: "sign-in", Sheet: &ui.SheetRoute{Detents: ui.DetentMedium | ui.DetentLarge, Grabber: true}, Screen: func(p ui.Params) ui.Node {
					return signIn(account, ui.Href(p.Get("next")))
				}},
			}},
		}},
	)
}

func inbox(m *mail) ui.Node {
	rt := ui.UseRouter()
	refreshing := signal.New(false)
	return ui.List(ui.ListProps{
		Refreshing: refreshing,
		OnRefresh: func() {
			refreshing.Set(true)
			go func() { // pretend to fetch
				time.Sleep(800 * time.Millisecond)
				ui.Post(func() {
					msg := newMessage(m.next)
					msg.unread = true
					m.messages.Update(func(ms []message) []message { return append([]message{msg}, ms...) })
					m.next++
					refreshing.Set(false)
				})
			}()
		},
	}, m.messages, func(msg message) int { return msg.id }, func(msg signal.Accessor[message], _ signal.Accessor[int]) ui.Node {
		return ui.View(ui.ViewProps{
			OnPress: func() {
				v := msg.Get() // in a handler: not tracked
				if v.unread {
					m.update(v.id, func(x *message) { x.unread = false })
				}
				rt.Navigate(ui.HrefOf("/inbox/:id", v.id))
			},
			Style: ui.Style{Direction: ui.RowDir, Align: ui.AlignCenter, Gap: 12, Padding: ui.Symmetric(10, 16)},
		},
			ui.Column(ui.Style{Grow: 1, Gap: 2},
				ui.Text(signal.Func(func() string { return msg.Get().from }), ui.Style{Font: fonts.Headline}),
				ui.Text(signal.Func(func() string { return msg.Get().subject }), ui.Style{Font: fonts.Subheadline, Color: colors.SecondaryLabel}),
			),
			ui.Text(signal.Func(func() string {
				if msg.Get().unread {
					return "●"
				}
				return ""
			}), ui.Style{Color: colors.Blue}),
			ui.Text(signal.Func(func() string {
				if msg.Get().starred {
					return "★"
				}
				return ""
			}), ui.Style{Color: colors.Orange}),
		)
	})
}

// detailOptions are a message's screen's bar: its sender, Star; Mail's
// bottom toolbar (delete, star, reply's menu); compose at its trailing
// end (Android: a FAB docked in the bar).
func detailOptions(m *mail, id int, compose *ui.Action) ui.ScreenOptions {
	msg, ok := m.find(id)
	if !ok {
		return ui.ScreenOptions{Title: ui.Val("Message")}
	}
	rt := ui.UseRouter()
	starred := m.starred(id)
	return ui.ScreenOptions{
		Title:   ui.Val(msg.from),
		Actions: []ui.Action{{Title: ui.Val("Star"), OnPress: func() { m.star(id) }}},
		Toolbar: []ui.Action{
			{Title: ui.Val("Delete"), Icon: ui.IconTrash, Role: ui.ActionDestructive, OnPress: func() { confirmDelete(m, id, rt) }},
			{Title: signal.Func(func() string {
				if starred.Get() {
					return "Unstar"
				}
				return "Star"
			}), Icon: ui.IconStar, Checked: starred, OnPress: func() { m.star(id) }},
			{Title: ui.Val("Reply"), Icon: "arrowshape.turn.up.left", Children: []ui.Action{
				{Title: ui.Val("Reply"), Icon: "arrowshape.turn.up.left", OnPress: func() { ui.Haptic(ui.HapticLight) }},
				{Title: ui.Val("Forward"), Icon: "arrowshape.turn.up.forward", OnPress: func() { ui.Haptic(ui.HapticLight) }},
			}},
			{Title: ui.Val("Mark as unread"), Icon: "envelope", Disabled: ui.Val(true)},
		},
		Primary: compose,
	}
}

func confirmDelete(m *mail, id int, rt *ui.RouterHandle) {
	ui.Alert(ui.AlertProps{
		Title:   "Delete this message?",
		Message: "You can't undo this.",
		Actions: []ui.Action{
			{Title: ui.Val("Cancel"), Role: ui.ActionCancel},
			{Title: ui.Val("Delete"), Role: ui.ActionDestructive, OnPress: func() {
				m.remove(id)
				ui.Haptic(ui.HapticSuccess)
				rt.Back()
			}},
		},
	})
}

func detail(m *mail, id int) ui.Node {
	msg, ok := m.find(id)
	if !ok {
		return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
			ui.StaticText("There's no message "+strconv.Itoa(id)+"."),
			ui.NavLink(ui.NavLinkProps{Href: "/inbox"}, ui.StaticText("Back to the inbox", ui.Style{Color: colors.Blue})),
		)
	}
	// A sheet of the screen's own, opened from code (not a route).
	sheet := signal.New(false)
	note := signal.New("")
	rt := ui.UseRouter()
	confirm := func() { confirmDelete(m, id, rt) }
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
				// A route of its own: the address says it's open.
				ui.Button(ui.ButtonProps{Title: ui.Val("Write a Reply"), Variant: ui.ButtonGray, Href: ui.HrefOf("/inbox/:id/reply", id)}),
				ui.Button(ui.ButtonProps{Title: ui.Val("More"), Variant: ui.ButtonGray, Menu: []ui.Action{
					{Title: ui.Val("Star"), OnPress: func() { m.update(id, func(x *message) { x.starred = !x.starred }) }},
					{Title: ui.Val("Delete…"), Role: ui.ActionDestructive, OnPress: confirm},
				}}),
			),
			ui.Menu(ui.MenuProps{LongPress: true, Actions: []ui.Action{
				{Title: ui.Val("Reply"), OnPress: func() { ui.Haptic(ui.HapticLight) }},
				{Title: ui.Val("Delete…"), Role: ui.ActionDestructive, OnPress: confirm},
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
		),
	)
}

// composer is a reply, in a full-screen sheet that's a route
// (/inbox/:id/reply): it takes the screen until it's sent or cancelled.
// Going back (Android's back, Escape, the browser's back) with a draft
// asks first. It shows its environment, and the screen's: on a phone
// they're the same, the whole screen and its safe area.
func composer(m *mail, id int) ui.Node {
	msg, _ := m.find(id)
	rt := ui.UseRouter()
	env := ui.UseEnvironment()
	draft := signal.New("")
	discard := func() {
		ui.Alert(ui.AlertProps{
			Title: "Discard this reply?",
			Actions: []ui.Action{
				{Title: ui.Val("Keep Editing"), Role: ui.ActionCancel},
				{Title: ui.Val("Discard"), Role: ui.ActionDestructive, OnPress: func() { rt.Dismiss() }},
			},
		})
	}
	cancel := func() {
		if draft.Peek() == "" {
			rt.Dismiss()
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
					rt.Dismiss()
				}}),
		),
		ui.StaticText("To: "+msg.from, ui.Style{Color: colors.SecondaryLabel}),
		ui.TextField(ui.TextFieldProps{Value: draft, Placeholder: ui.Val("Message"), Multiline: true, Style: ui.Style{Grow: 1}}),
		ui.Text(signal.Func(func() string { return describe("This sheet", env.Get()) }), ui.Style{Font: fonts.Footnote, Color: colors.TertiaryLabel}),
		ui.Text(signal.Func(func() string { return describe("The screen", m.screen.Get()) }), ui.Style{Font: fonts.Footnote, Color: colors.TertiaryLabel}),
		ui.Text(signal.Func(func() string {
			e, s := env.Get(), m.screen.Get()
			if e.Width == s.Width && e.Height == s.Height && e.SafeTop == s.SafeTop && e.SafeRight == s.SafeRight && e.SafeBottom == s.SafeBottom && e.SafeLeft == s.SafeLeft {
				return "Covers the screen"
			}
			return ""
		}), ui.Style{Font: fonts.Footnote, Color: colors.Green}),
	)
}

func settings(account *signal.Signal[string]) ui.Node {
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
		ui.Row(ui.Style{Align: ui.AlignCenter, Gap: 12},
			ui.Text(signal.Func(func() string {
				if a := account.Get(); a != "" {
					return "Signed in as " + a
				}
				return "Not signed in"
			}), ui.Style{Grow: 1, Color: colors.SecondaryLabel}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Account"), Href: "/settings/account"}),
		),
		ui.Button(ui.ButtonProps{Title: ui.Val("Try a haptic"), Variant: ui.ButtonTinted, OnPress: func() {
			if haptics.Peek() {
				ui.Haptic(ui.HapticSuccess)
			}
		}}),
		ui.Button(ui.ButtonProps{Title: ui.Val("About"), Href: "/settings/about"}),
	)
}

func about(ui.Params) ui.Node {
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 8},
		ui.StaticText("asuka", ui.Style{Font: fonts.LargeTitle.Weighted(fonts.Bold)}),
		ui.StaticText("Native widgets, driven by Go and fine-grained signals.", ui.Style{Color: colors.SecondaryLabel}),
		// A link to another tab's screen: the inbox shows, the message on it.
		ui.NavLink(ui.NavLinkProps{Href: "/inbox/20"}, ui.StaticText("Read the newest message", ui.Style{Color: colors.Blue})),
	)
}

// signIn is a sheet that signs the user in: an alert with text fields,
// Sign In disabled until both are filled in, following them as they're
// typed. Once signed in, the user goes where they were going (next).
func signIn(account *signal.Signal[string], next ui.Href) ui.Node {
	rt := ui.UseRouter()
	ask := func() {
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
					OnPress: func() {
						account.Set(user.Peek())
						if next == "" {
							next = "/settings"
						}
						rt.Replace(next)
					}},
			},
		})
	}
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
		ui.StaticText("Your account", ui.Style{Font: fonts.Title3.Weighted(fonts.Semibold)}),
		ui.StaticText("Sign in to see it.", ui.Style{Color: colors.SecondaryLabel}),
		ui.Button(ui.ButtonProps{Title: ui.Val("Sign In…"), Variant: ui.ButtonFilled, OnPress: ask}),
	)
}

func accountScreen(account *signal.Signal[string]) ui.Node {
	return ui.SafeArea(ui.Style{Padding: ui.All(20), Gap: 12},
		ui.Text(signal.Func(func() string { return "Signed in as " + account.Get() }), ui.Style{Font: fonts.Title3}),
		// Signing out: the guard no longer lets the user in here, and the
		// router leaves for the sign-in sheet on its own.
		ui.Button(ui.ButtonProps{Title: ui.Val("Sign Out"), Variant: ui.ButtonTinted, OnPress: func() { account.Set("") }}),
	)
}

func main() { app.Run(App) }
