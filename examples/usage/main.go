// Usage follows a subscription's quotas: Claude's 5-hour window and its
// week, how much is left of each, and, at the rate the week has gone so
// far, how much will be left when it resets. You sign in with Claude
// (OAuth in the system's sign-in sheet, ryui/webauth; see auth.go).
// Accounts are kept with ryui/store, their tokens in the keychain
// (ryui/secure). A demo account shows it without one.
//
// It draws its own meters and a forecast chart (ui.Shape, dashed), tints
// with semantic colors at an opacity (Color.Opacity), and has an
// appearance setting (ui.SetColorScheme).
//
//	go run ./cmd/ryui dev -android examples/usage
package main

import (
	"cmp"
	"errors"
	"slices"
	"time"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/secure"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/store"
	"github.com/egoist/ryui/ui"
)

var (
	accounts = store.Signal("accounts", []Account(nil))
	// showUsed shows how much of each quota is used, rather than left.
	showUsed = store.Signal("showUsed", false)
	scheme   = store.Signal("colorScheme", int(ui.ColorSchemeSystem))
	// every is how often, in minutes, the accounts refresh while the app
	// is open.
	every = store.Signal("refreshEvery", 5)

	// clock is now, to the half minute: what "updated 5 min ago" and the
	// forecasts read.
	clock = signal.New(time.Now())
	// busy are the accounts being refreshed.
	busy = signal.NewFunc([]string(nil), nil)

	nav        *ui.Navigator
	foreground = true
)

func App() ui.Node {
	useLook()
	ui.SetColorScheme(ui.ColorScheme(scheme.Peek()))
	app.OnLifecycle(func(s app.State) {
		switch s {
		case app.Foreground:
			foreground = true
			clock.Set(time.Now())
			refreshStale()
		case app.Background:
			foreground = false
		}
	})
	go func() {
		for range time.Tick(30 * time.Second) {
			ui.Post(func() {
				if foreground {
					clock.Set(time.Now())
					refreshStale()
				}
			})
		}
	}()
	refreshStale()
	adding := signal.New(false)
	return ui.Stack(func() ui.Node {
		nav = ui.UseNavigator()
		return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}}, home(adding), addSheet(adding, ""))
	}, ui.ScreenOptions{Title: ui.Val("Usage"), Actions: []ui.Action{
		{Title: "Add account", Icon: ui.IconPlus, OnPress: func() { adding.Set(true) }},
		{Title: "Refresh", Icon: ui.IconRefresh, OnPress: refreshAll},
		{Title: "Settings", Icon: ui.IconSettings, OnPress: func() {
			nav.Push(settings, ui.ScreenOptions{Title: ui.Val("Settings")})
		}},
	}})
}

// home is the accounts, a card each, or a way to add one.
func home(adding *signal.Signal[bool]) ui.Node {
	refreshing := signal.Func(func() bool { return len(busy.Get()) > 0 })
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1, Background: page()}, Refreshing: refreshing, OnRefresh: refreshAll},
		ui.SafeArea(ui.Style{Padding: ui.Symmetric(8, 16), Gap: 12},
			ui.For(accounts, func(a Account) string { return a.ID }, func(a signal.Accessor[Account], _ signal.Accessor[int]) ui.Node {
				return accountCard(a)
			}),
			ui.Show(signal.Func(func() bool { return len(accounts.Get()) == 0 }), func() ui.Node {
				return ui.Column(ui.Style{Padding: ui.Symmetric(80, 24), Gap: 16, Align: ui.AlignCenter},
					badge("claude", 72),
					ui.StaticText("Follow your plan's limits", ui.Style{Font: ui.Title3.Weighted(ui.Semibold), TextAlign: ui.TextCenter}),
					ui.StaticText("See how much of your 5-hour window and your week is left, and whether the week will last.",
						ui.Style{Color: ui.SecondaryLabel, TextAlign: ui.TextCenter}),
					ui.Button(ui.ButtonProps{Title: ui.Val("Add an account"), Icon: ui.IconPlus, Variant: ui.ButtonFilled,
						OnPress: func() { adding.Set(true) }}),
				)
			}),
		),
	)
}

// accountCard is an account on the home screen: up to three of its windows,
// those it shows there.
func accountCard(a signal.Accessor[Account]) ui.Node {
	id := signal.Untrack(a.Get).ID
	shown := signal.Func(func() []Window {
		acc := a.Get()
		var ws []Window
		for _, w := range acc.Windows {
			if acc.Shown(w.Key) && len(ws) < 3 {
				ws = append(ws, w)
			}
		}
		return ws
	})
	return ui.View(ui.ViewProps{
		Label: "Account details",
		Role:  ui.RoleButton,
		OnPress: func() {
			nav.Push(func() ui.Node { return detail(id) }, ui.ScreenOptions{
				Title:   signal.Func(func() string { acc, _ := find(id); return acc.Title() }),
				Actions: []ui.Action{{Title: "Refresh", Icon: ui.IconRefresh, OnPress: func() { refresh(id) }}},
			})
		},
		Style: ui.StyleFunc(func() ui.Style {
			return ui.Style{Padding: ui.All(cardPadding()), Gap: 18, Background: ui.SecondaryGroupedBackground, CornerRadius: radius()}
		}),
	},
		ui.Row(ui.Style{Gap: 16, Align: ui.AlignCenter},
			badge(signal.Untrack(a.Get).Service, map[bool]float32{true: 44, false: 52}[ios]),
			ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 2},
				ui.Text(signal.Func(func() string { return a.Get().Title() }), ui.Style{Font: titleFont(), Lines: 1}),
				ui.Text(signal.Func(func() string { return status(a.Get()) }), ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel, Lines: 1}),
			),
			chevron(),
		),
		ui.For(shown, func(w Window) string { return w.Key }, func(w signal.Accessor[Window], _ signal.Accessor[int]) ui.Node {
			return windowRow(w, true)
		}),
		ui.Show(signal.Func(func() bool { return a.Get().Error != "" }), func() ui.Node {
			return ui.Text(signal.Func(func() string { return "Couldn't refresh: " + a.Get().Error }),
				ui.Style{Font: ui.Footnote, Color: ui.Red})
		}),
	)
}

// status is when an account was last updated, or that it's updating.
func status(a Account) string {
	if slices.Contains(busy.Get(), a.ID) {
		return "Updating…"
	}
	if a.Updated.IsZero() {
		return "Not updated yet"
	}
	return "Updated " + ago(a.Updated, clock.Get())
}

// windowRow is a window's name, what's left (or used), its meter and
// when it resets; with forecast, what the reset will leave.
func windowRow(w signal.Accessor[Window], forecast bool) ui.Node {
	return ui.Column(ui.Style{Gap: 10},
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignBaseline},
			ui.Text(signal.Func(func() string { return w.Get().Title() }), ui.Style{Grow: 1, Shrink: 1, Font: ui.Headline, Lines: 1}),
			ui.Text(signal.Func(func() string { return quota(w.Get().Used) }), ui.Style{Color: ui.SecondaryLabel}),
		),
		meter(w),
		ui.Show(signal.Func(func() bool { return !w.Get().Resets.IsZero() }), func() ui.Node {
			return ui.RichText(ui.RichTextProps{Style: ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel}, Text: signal.Func(func() ui.Rich {
				win, now := w.Get(), clock.Get()
				r := ui.Rich{ui.Plain("Resets " + when(win.Resets, now))}
				if f := win.Forecast(now); forecast && f.Pace != PaceUnknown {
					r = append(r, ui.Plain(" · "), ui.Plain(outlook(f, now)).Colored(paceColor(f.Pace)))
				}
				return r
			})})
		}),
	)
}

// find is the account with id.
func find(id string) (Account, bool) {
	for _, a := range accounts.Get() {
		if a.ID == id {
			return a, true
		}
	}
	return Account{}, false
}

// save puts a in the accounts, in its place, or adds it.
func save(a Account) {
	accounts.Update(func(as []Account) []Account {
		as = slices.Clone(as)
		for i := range as {
			if as[i].ID == a.ID {
				as[i] = a
				return as
			}
		}
		return append(as, a)
	})
}

func remove(id string) {
	accounts.Update(func(as []Account) []Account {
		return slices.DeleteFunc(slices.Clone(as), func(a Account) bool { return a.ID == id })
	})
	secure.Remove("token."+id, nil)
}

func setBusy(id string, on bool) {
	busy.Update(func(ids []string) []string {
		ids = slices.DeleteFunc(slices.Clone(ids), func(s string) bool { return s == id })
		if on {
			ids = append(ids, id)
		}
		return ids
	})
}

// refresh reads an account's usage again: off the main thread, with its
// token from the keychain.
func refresh(id string) {
	a, ok := find(id)
	if !ok || slices.Contains(busy.Peek(), id) {
		return
	}
	done := func(fresh Account, err error) {
		setBusy(id, false)
		cur, ok := find(id)
		if !ok {
			return // removed while it refreshed
		}
		if err != nil {
			cur.Error = err.Error()
		} else {
			fresh.Name, fresh.Hidden, fresh.Updated, fresh.Error = cur.Name, cur.Hidden, time.Now(), ""
			cur = fresh
		}
		ui.Animate(func() { save(cur) })
	}
	setBusy(id, true)
	if a.Demo {
		go func() {
			time.Sleep(600 * time.Millisecond) // as a request would
			ui.Post(func() {
				refreshDemo(&a, time.Now())
				done(a, nil)
			})
		}()
		return
	}
	secure.Load("token."+id, func(token string, ok bool, err error) {
		if !ok {
			done(a, cmp.Or(err, errors.New("signed out: sign in again")))
			return
		}
		go func() {
			kept, err := services[a.Service].Refresh(&a, token)
			if kept != token {
				// Refreshed tokens: the old refresh token may be spent.
				secure.Set("token."+id, kept)
			}
			ui.Post(func() { done(a, err) })
		}()
	})
}

func refreshAll() {
	for _, a := range accounts.Peek() {
		refresh(a.ID)
	}
}

// refreshStale refreshes the accounts last updated longer ago than the
// setting says.
func refreshStale() {
	n := every.Peek()
	for _, a := range accounts.Peek() {
		if time.Since(a.Updated) >= time.Duration(n)*time.Minute && a.Error == "" {
			refresh(a.ID)
		}
	}
}

// titleFont is a card's title: a headline on iOS, Material's title.
func titleFont() ui.Font {
	if ios {
		return ui.Headline
	}
	return ui.Title3.Weighted(ui.Semibold)
}

func main() { app.Run(App) }
