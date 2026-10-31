package main

import (
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

var schemes = []string{"System", "Light", "Dark"}

// settings is how quotas show, the app's appearance and how often it
// refreshes.
func settings() ui.Node {
	shown := signal.New(0)
	if showUsed.Peek() {
		shown.Set(1)
	}
	signal.Effect(func() { showUsed.Set(shown.Get() == 1) })

	intervals := []int{3, 5, 10}
	interval := signal.New(1)
	for i, n := range intervals {
		if n == every.Peek() {
			interval.Set(i)
		}
	}
	signal.Effect(func() { every.Set(intervals[interval.Get()]) })

	appearance := func(s int) ui.Action {
		return ui.Action{Title: schemes[s], OnPress: func() {
			scheme.Set(s)
			ui.SetColorScheme(ui.ColorScheme(s))
		}}
	}
	return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1, Background: page()}},
		ui.SafeArea(ui.Style{Padding: ui.Symmetric(8, 16), Gap: 12},
			section("Display"),
			group(
				stacked(
					ui.StaticText("Show quotas as"),
					ui.Segmented(ui.SegmentedProps{Segments: []string{"Left", "Used"}, Selected: shown}),
				),
				row(
					ui.StaticText("Appearance", ui.Style{Grow: 1}),
					ui.Button(ui.ButtonProps{
						Title: signal.Func(func() string { return schemes[scheme.Get()] }),
						Menu:  []ui.Action{appearance(0), appearance(1), appearance(2)},
					}),
				),
			),

			section("Refresh"),
			group(
				stacked(
					ui.StaticText("Refresh every"),
					ui.Segmented(ui.SegmentedProps{Segments: []string{"3 min", "5 min", "10 min"}, Selected: interval}),
				),
			),
			note("While the app is open. Pull down on a page to refresh now."),
			note("The numbers are Claude's own, as /usage shows them in Claude Code. The forecast is this app's guess."),
		),
	)
}
