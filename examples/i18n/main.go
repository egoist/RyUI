// Command i18n shows a translated app: messages with plurals and dates in
// their catalogs (locales), a menu of its languages that changes them at
// once, and a layout that mirrors in Arabic. Try it in a language with
//
//	asuka dev --platform ios --lang ar examples/i18n
//	asuka dev --platform ios --lang en-XA examples/i18n   (a pseudo locale)
package main

import (
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/examples/i18n/locales"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/i18n"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func main() { app.Run(root) }

func root() ui.Node {
	return ui.Tabs(ui.TabsProps{},
		ui.Tab{Title: locales.Live.Inbox(), Icon: ui.IconHome, Content: inboxTab},
		ui.Tab{Title: locales.Live.Settings(), Icon: ui.IconSettings, Content: settingsTab},
	)
}

func inboxTab() ui.Node {
	return ui.Stack(inbox, ui.ScreenOptions{Title: locales.Live.Inbox()})
}

func inbox() ui.Node {
	nav := ui.UseNavigator()
	count := signal.New(3)
	seen := time.Date(2026, 3, 14, 15, 9, 0, 0, time.Local)
	return ui.SafeArea(ui.Style{Grow: 1}, ui.Scroll(ui.ScrollProps{},
		ui.Column(ui.Style{Padding: ui.All(16), Gap: 12},
			ui.Text(locales.Live.Greeting(ui.Val("Ada")), ui.Style{Font: fonts.Title2}),
			ui.Text(locales.Live.Unread(count)),
			// Start and End mirror; Row does by itself.
			ui.Row(ui.Style{Gap: 8},
				ui.Button(ui.ButtonProps{Title: locales.Live.More(), OnPress: func() { count.Update(func(n int) int { return n + 1 }) }}),
				ui.Button(ui.ButtonProps{Title: locales.Live.Less(), OnPress: func() { count.Update(func(n int) int { return max(n-1, 0) }) }}),
			),
			ui.Text(locales.Live.LastSeen(ui.Val(seen)), ui.Style{Color: colors.SecondaryLabel}),
			ui.Text(locales.Live.Storage(ui.Val(0.42)), ui.Style{Color: colors.SecondaryLabel}),
			// In the language's way, and the user's regional formats.
			ui.Text(signal.Func(func() string {
				return i18n.FormatNumber(ui.FormatLocale(), 1234567.891, i18n.NumberOptions{})
			}), ui.Style{Color: colors.SecondaryLabel}),
			ui.View(ui.ViewProps{Style: ui.Style{Background: colors.SecondaryBackground, CornerRadius: 8, Padding: ui.Edges{Top: 8, Bottom: 8, Start: 24, End: 8}}},
				ui.Text(locales.Live.Wide()),
			),
			ui.WithDirection(ui.LeftToRight, func() ui.Node {
				return ui.Row(ui.Style{Gap: 8, Padding: ui.Symmetric(8, 0)},
					ui.Symbol(ui.IconForward),
					ui.Text(locales.Live.AlwaysLtr(), ui.Style{Shrink: 1}),
				)
			}),
			ui.Scroll(ui.ScrollProps{Horizontal: true, ContentStyle: ui.Style{Gap: 8}},
				chips()...,
			),
			ui.Button(ui.ButtonProps{Title: locales.Live.Next(), OnPress: func() {
				nav.Push(detail, ui.ScreenOptions{Title: locales.Live.Next(),
					Actions: []ui.Action{{Title: locales.Live.Archive(), Icon: ui.IconDownload}},
				})
			}}),
			ui.Button(ui.ButtonProps{Title: locales.Live.DeleteMessage(), Icon: ui.IconTrash, OnPress: func() {
				ui.Alert(ui.AlertProps{Title: locales.AlertTitle(), Message: locales.AlertBody(), Actions: []ui.Action{
					{Title: locales.Live.DeleteMessage(), Role: ui.ActionDestructive},
					{Title: locales.Live.Less(), Role: ui.ActionCancel},
				}})
			}}),
		),
	))
}

func chips() []ui.Node {
	var out []ui.Node
	for _, n := range []int{1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144, 233} {
		out = append(out, ui.View(ui.ViewProps{Style: ui.Style{Background: colors.SecondaryBackground, CornerRadius: 12, Padding: ui.Symmetric(6, 12)}},
			ui.Text(signal.Func(func() string { return i18n.FormatNumber(ui.FormatLocale(), n, i18n.NumberOptions{}) })),
		))
	}
	return out
}

func detail() ui.Node {
	return ui.SafeArea(ui.Style{Padding: ui.All(16), Gap: 12, Align: ui.AlignStart},
		ui.Text(locales.Live.Wide()),
		ui.Row(ui.Style{Gap: 8, Align: ui.AlignCenter},
			ui.Text(locales.Live.Next()),
			ui.Symbol("chevron.forward"), // mirrors, as chevron.right wouldn't
		),
	)
}

func settingsTab() ui.Node {
	return ui.Stack(settings, ui.ScreenOptions{Title: locales.Live.Settings()})
}

func settings() ui.Node {
	choices := append([]string{""}, ui.Languages()...)
	return ui.List(ui.ListProps{Appearance: ui.ListInsetGrouped}, ui.Val(choices), func(s string) string { return s },
		func(tag signal.Accessor[string], _ signal.Accessor[int]) ui.Node {
			return ui.View(ui.ViewProps{
				OnPress: func() { ui.SetLocale(tag.Get()) },
				Style:   ui.Style{Direction: ui.RowDir, Justify: ui.JustifySpaceBetween, Padding: ui.Symmetric(12, 16)},
			},
				ui.Text(signal.Func(func() string {
					if t := tag.Get(); t != "" {
						return i18n.Autonym(t)
					}
					return locales.SystemLanguage()
				})),
				ui.Show(signal.Func(func() bool { return tag.Get() == ui.Locale() }), func() ui.Node {
					return ui.Symbol("checkmark", ui.Style{Color: colors.Accent})
				}),
			)
		})
}
