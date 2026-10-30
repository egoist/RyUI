package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/svg"
	"github.com/egoist/nui/ui"
)

// detail is an account: every window, the forecasts, which windows its
// card shows, the account, and what can be done with it.
func detail(id string) ui.Node {
	acc := signal.Func(func() Account { a, _ := find(id); return a })
	windows := signal.Func(func() []Window { return acc.Get().Windows })
	first := signal.Func(func() Window {
		if ws := windows.Get(); len(ws) > 0 {
			return ws[0]
		}
		return Window{}
	})
	// The forecasts worth showing: the weeks', once something's used.
	long := signal.Func(func() []Window {
		var ws []Window
		for _, w := range windows.Get() {
			if w.Period() >= 24*time.Hour && w.Used > 0 && !w.Resets.IsZero() {
				ws = append(ws, w)
			}
		}
		return ws
	})
	renaming, signing := signal.New(false), signal.New(false)
	big := ui.LargeTitle.Weighted(ui.Bold)
	big.Size = 44
	return ui.View(ui.ViewProps{Style: ui.Style{Grow: 1}},
		ui.Scroll(ui.ScrollProps{
			Style:      ui.Style{Grow: 1, Background: page()},
			Refreshing: signal.Func(func() bool { return slices.Contains(busy.Get(), id) }),
			OnRefresh:  func() { refresh(id) },
		}, ui.SafeArea(ui.Style{Padding: ui.Symmetric(8, 16), Gap: 12},
			ui.Row(ui.Style{Gap: 20, Align: ui.AlignCenter, Padding: ui.Symmetric(12, 8)},
				badge("claude", 80),
				ui.Column(ui.Style{Grow: 1, Shrink: 1},
					ui.Text(signal.Func(func() string { return first.Get().Title() }), ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel}),
					ui.Text(signal.Func(func() string { return quota(first.Get().Used) }), ui.Style{Font: big, Lines: 1}),
				),
			),

			section("Usage"),
			card(ui.Style{Padding: ui.All(cardPadding()), Gap: 24},
				ui.For(windows, func(w Window) string { return w.Key }, func(w signal.Accessor[Window], _ signal.Accessor[int]) ui.Node {
					return windowRow(w, false)
				}),
			),

			ui.Show(signal.Func(func() bool { return len(long.Get()) > 0 }), func() ui.Node {
				return ui.Column(ui.Style{Gap: 12},
					section("Forecast"),
					ui.For(long, func(w Window) string { return w.Key }, func(w signal.Accessor[Window], _ signal.Accessor[int]) ui.Node {
						return forecastCard(w)
					}),
					note("Projected from the average rate since each window started: busy and quiet hours both change it."),
				)
			}),

			section("On the home screen"),
			group(ui.For(windows, func(w Window) string { return w.Key }, func(w signal.Accessor[Window], _ signal.Accessor[int]) ui.Node {
				key := signal.Untrack(w.Get).Key
				on := signal.New(signal.Untrack(acc.Get).Shown(key))
				signal.Effect(func() {
					shown := on.Get()
					a := signal.Untrack(acc.Get)
					if a.ID == "" || a.Shown(key) == shown {
						return
					}
					a.Hidden = slices.DeleteFunc(slices.Clone(a.Hidden), func(k string) bool { return k == key })
					if !shown {
						a.Hidden = append(a.Hidden, key)
					}
					save(a)
				})
				return row(ui.StaticText(signal.Untrack(w.Get).Title(), ui.Style{Grow: 1, Shrink: 1}), ui.Toggle(ui.ToggleProps{Value: on}))
			})),
			note("The home screen's card shows up to three of these; this page shows them all."),

			section("Account"),
			group(
				field("Email", signal.Func(func() string { return acc.Get().Email })),
				field("Organization", signal.Func(func() string { return acc.Get().Org })),
				field("Extra usage", signal.Func(func() string { return map[bool]string{true: "On", false: "Off"}[acc.Get().Extra] })),
				field("Last updated", signal.Func(func() string {
					if t := acc.Get().Updated; !t.IsZero() {
						return ago(t, clock.Get())
					}
					return "Never"
				})),
			),

			section("Actions"),
			actions(id, acc, renaming, signing),
		)),
		renameSheet(renaming, id),
		addSheet(signing, id),
	)
}

// forecastCard is a window's forecast: its pace, what the reset will
// leave, and the chart.
func forecastCard(w signal.Accessor[Window]) ui.Node {
	f := signal.Func(func() Forecast { return w.Get().Forecast(clock.Get()) })
	color := func() ui.Color { return paceColor(f.Get().Pace) }
	headline := ui.Title.Weighted(ui.Bold)
	return card(ui.Style{Padding: ui.All(cardPadding()), Gap: 4},
		ui.Row(ui.Style{Gap: 12, Align: ui.AlignCenter},
			ui.Text(signal.Func(func() string { return w.Get().Title() }), ui.Style{Grow: 1, Shrink: 1, Color: ui.SecondaryLabel}),
			// The pace, a chip in its color.
			ui.Dynamic(func() ui.Node {
				c := color()
				return ui.Row(ui.Style{Gap: 6, Align: ui.AlignCenter, Padding: ui.Symmetric(6, 14), Background: c.Opacity(0.16), CornerRadius: 999},
					svg.View(svg.Props{Icon: paceIcon(f.Get().Pace), Color: ui.Val(c), Style: ui.Style{Width: ui.Pt(18), Height: ui.Pt(18)}}),
					ui.Text(signal.Func(func() string { return f.Get().Pace.String() }), ui.Style{Font: ui.Subheadline.Weighted(ui.Semibold), Color: c}),
				)
			}),
		),
		ui.Text(signal.Func(func() string { return upper(outlook(f.Get(), clock.Get())) }), ui.StyleFunc(func() ui.Style {
			return ui.Style{Font: headline, Color: color(), Margin: ui.Edges{Top: 8}}
		})),
		ui.Text(signal.Func(func() string { return "Resets " + when(w.Get().Resets, clock.Get()) }), ui.Style{Color: ui.SecondaryLabel}),
		ui.View(ui.ViewProps{Style: ui.Style{Margin: ui.Edges{Top: 20}}}, chart(w)),
	)
}

// actions are what can be done with an account: on iOS rows of tinted
// titles, removing it apart in red; on Android rows with an icon and
// what each does.
func actions(id string, acc signal.Accessor[Account], renaming, signing *signal.Signal[bool]) ui.Node {
	confirm := func() {
		ui.Alert(ui.AlertProps{
			Title:   "Remove this account?",
			Message: "Its token is deleted from this device.",
			Actions: []ui.Action{
				{Title: "Cancel", Role: ui.ActionCancel},
				{Title: "Remove", Role: ui.ActionDestructive, OnPress: func() { nav.Pop(); remove(id) }},
			},
		})
	}
	rows := []ui.Node{
		action(ui.IconRefresh, "Refresh Now", "Get the latest usage", func() { refresh(id) }),
		action("pencil", "Rename", "Give this account a name", func() { renaming.Set(true) }),
		ui.Show(signal.Func(func() bool { return !acc.Get().Demo }), func() ui.Node {
			return action("key", "Sign In Again", "Sign in to this account with Claude again", func() { signing.Set(true) })
		}),
		link("Open Claude", "Manage your plan and billing", services["claude"].Manage),
	}
	if !ios {
		return group(append(rows, action(ui.IconTrash, "Remove", "Forget this account and its token", confirm))...)
	}
	return ui.Column(ui.Style{Gap: 20},
		group(rows...),
		group(ui.View(ui.ViewProps{OnPress: confirm, Label: "Remove Account", Role: ui.RoleButton,
			Style: ui.Style{Padding: ui.Symmetric(11, 16), MinHeight: ui.Pt(48), Justify: ui.JustifyCenter, Align: ui.AlignCenter}},
			ui.StaticText("Remove Account", ui.Style{Color: ui.Red}))),
	)
}

// cardPadding is a card's padding: UIKit's cell margins on iOS.
func cardPadding() float32 {
	if ios {
		return 16
	}
	return 20
}

// field is a name and its value, shown when it has one.
func field(name string, value signal.Accessor[string]) ui.Node {
	return ui.Show(signal.Func(func() bool { return value.Get() != "" }), func() ui.Node {
		return row(
			ui.StaticText(name, ui.Style{Grow: 1, Shrink: 0}),
			ui.Text(value, ui.Style{Shrink: 1, Color: ui.SecondaryLabel, TextAlign: ui.TextEnd, Lines: 1}),
		)
	})
}

// action is a row that does something.
func action(icon ui.Icon, title, detail string, press func()) ui.Node {
	return actionRow(ui.Symbol(icon, ui.Style{Color: ui.Accent}), title, detail, press, nil)
}

// link is an action row that opens a page in the browser.
func link(title, detail, url string) ui.Node {
	ext := func() ui.Node {
		size := float32(22)
		if ios {
			size = 18
		}
		return svg.View(svg.Props{Icon: iconExternal, Color: ui.Val(ui.TertiaryLabel), Style: ui.Style{Width: ui.Pt(size), Height: ui.Pt(size)}})
	}
	return actionRow(svg.View(svg.Props{Icon: iconExternal, Color: ui.Val(ui.Accent), Style: ui.Style{Width: ui.Pt(20), Height: ui.Pt(20)}}),
		title, detail, func() { ui.OpenURL(url) }, ext)
}

// actionRow is an action: on iOS its title in the tint, as a button in
// a grouped list is; on Android an icon in a disc, its title and what
// it does.
func actionRow(icon ui.Node, title, detail string, press func(), trailing func() ui.Node) ui.Node {
	var kids []ui.Node
	if ios {
		kids = []ui.Node{ui.StaticText(title, ui.Style{Grow: 1, Shrink: 1, Color: ui.Accent})}
	} else {
		kids = []ui.Node{
			ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(48), Height: ui.Pt(48), CornerRadius: 24, Background: ui.Accent.Opacity(0.14),
				Align: ui.AlignCenter, Justify: ui.JustifyCenter}}, icon),
			ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 2},
				ui.StaticText(title, ui.Style{Font: ui.Body.Weighted(ui.Medium)}),
				ui.StaticText(detail, ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel}),
			),
		}
	}
	if trailing != nil {
		kids = append(kids, trailing())
	}
	style := ui.Style{Direction: ui.RowDir, Gap: 16, Align: ui.AlignCenter, Padding: ui.Symmetric(14, 20)}
	if ios {
		style = ui.Style{Direction: ui.RowDir, Gap: 12, Align: ui.AlignCenter, Padding: ui.Symmetric(11, 16), MinHeight: ui.Pt(48)}
	}
	return cellWith(ui.ViewProps{OnPress: press, Label: title, Hint: detail, Role: ui.RoleButton, Style: style}, kids...)
}

// renameSheet names an account.
func renameSheet(open *signal.Signal[bool], id string) ui.Node {
	return ui.Sheet(ui.SheetProps{Open: open, Detents: ui.DetentMedium, Grabber: true, Content: func() ui.Node {
		a, _ := find(id)
		name := signal.New(a.Name)
		done := func() {
			a, ok := find(id)
			if ok {
				a.Name = strings.TrimSpace(name.Peek())
				save(a)
			}
			open.Set(false)
		}
		return ui.SafeArea(ui.Style{Padding: ui.All(24), Gap: 16},
			ui.StaticText("Rename", ui.Style{Font: ui.Title2.Weighted(ui.Bold)}),
			ui.TextField(ui.TextFieldProps{Value: name, Placeholder: ui.Val(services[a.Service].Name),
				Autocapitalize: ui.CapitalizeWords, ReturnKey: ui.ReturnDone, OnSubmit: done}),
			note(fmt.Sprintf("Empty, it's called %s.", services[a.Service].Name)),
			ui.Button(ui.ButtonProps{Title: ui.Val("Save"), Variant: ui.ButtonFilled, OnPress: done}),
		)
	}})
}

func upper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
