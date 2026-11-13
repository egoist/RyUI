// Tray is a focus timer that lives in the tray (the menu bar on a Mac):
// its icon counts down beside it; a click on it opens a flyout under it
// (above it on a taskbar at the bottom) with the timer, its lengths and a
// Reset that asks first, in a dialog over the flyout; its menu (a right
// click) starts and pauses the timer, picks how long it runs, and opens a
// window with the timer in it.
// It's a background app (asuka.json's "background"): no window and, on a
// Mac, no Dock icon at launch. The Dock icon's menu (a jump list's tasks
// on Windows, the launcher's quicklist on Linux) starts and pauses it
// too.
//
//	go run ./cmd/asuka dev examples/tray
package main

import (
	"fmt"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/tray"
	"github.com/egoist/asuka/ui"
)

func main() {
	// asuka.json says so for a build; this does it for go run too.
	app.RunInBackground()
	app.RunWindow(ui.WindowOptions{Title: ui.Val("Focus"), Width: 320, Height: 220}, Focus)
}

// lengths are the timer's choices, in minutes.
var lengths = []int{15, 25, 50}

func Focus() ui.Node {
	minutes := signal.New(25)
	left := signal.New(25 * time.Minute)
	running := signal.New(false)
	open := signal.New(false)

	// run counts the timer's starts: a tick of an earlier run (paused,
	// then started again within the second) stops there.
	run := 0
	var tick func(r int)
	tick = func(r int) {
		time.AfterFunc(time.Second, func() {
			ui.Post(func() {
				if r != run || !running.Peek() {
					return
				}
				if l := left.Peek() - time.Second; l > 0 {
					left.Set(l)
					tick(r)
					return
				}
				running.Set(false)
				left.Set(time.Duration(minutes.Peek()) * time.Minute)
			})
		})
	}
	toggle := func() {
		if running.Peek() {
			running.Set(false)
			return
		}
		running.Set(true)
		run++
		tick(run)
	}
	setLength := func(m int) {
		minutes.Set(m)
		if !running.Peek() {
			left.Set(time.Duration(m) * time.Minute)
		}
	}
	clock := signal.Func(func() string {
		l := left.Get()
		return fmt.Sprintf("%02d:%02d", int(l.Minutes()), int(l.Seconds())%60)
	})
	startTitle := signal.Func(func() string {
		if running.Get() {
			return "Pause"
		}
		return "Start"
	})

	flyout := signal.New(false)
	icon := tray.Show(tray.Props{
		ID:      "timer",
		OnClick: func() { flyout.Set(!flyout.Peek()) },
		Symbol:  signal.Func(func() string { return map[bool]string{true: "timer", false: "moon.zzz"}[running.Get()] }),
		Title:   clock,
		Tooltip: ui.Val("Focus"),
		Menu: signal.Func(func() []ui.MenuItem {
			var length []ui.MenuItem
			for _, m := range lengths {
				length = append(length, ui.MenuItem{
					Title:   fmt.Sprintf("%d minutes", m),
					Checked: minutes.Get() == m,
					OnPress: func() { setLength(m) },
				})
			}
			return []ui.MenuItem{
				{Title: startTitle.Get(), OnPress: toggle},
				{Title: "Reset", Disabled: running.Get(), OnPress: func() { setLength(minutes.Peek()) }},
				{Title: "Length", Submenu: length},
				ui.MenuSeparator,
				{Title: "Open Timer", OnPress: func() { open.Set(true) }},
				ui.MenuSeparator,
				ui.MenuQuit,
			}
		}),
	})
	tray.DockMenu(signal.Func(func() []ui.MenuItem {
		return []ui.MenuItem{{Title: startTitle.Get(), OnPress: toggle}}
	}))

	panel := func() ui.Node {
		return ui.Column(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 16, Padding: ui.All(24), Grow: 1},
			ui.Text(clock, ui.Style{Font: ui.LargeTitle.Weighted(ui.Bold)}),
			ui.Button(ui.ButtonProps{Title: startTitle, Variant: ui.ButtonFilled, OnPress: toggle}),
		)
	}
	confirm := signal.New(false)
	// The flyout: as big as what it shows, under the icon; it closes as
	// the user clicks elsewhere or presses Escape, and stays while its
	// dialog asks.
	flyoutContent := func() ui.Node {
		// The lengths, as a segmented control that follows minutes.
		var names []string
		for _, m := range lengths {
			names = append(names, fmt.Sprintf("%d min", m))
		}
		chosen := signal.New(0)
		signal.Effect(func() {
			for i, m := range lengths {
				if m == minutes.Get() {
					chosen.Set(i)
				}
			}
		})
		signal.Effect(func() {
			if m := lengths[chosen.Get()]; m != signal.Untrack(minutes.Get) {
				setLength(m)
			}
		})
		return ui.Column(ui.Style{Gap: 14, Padding: ui.All(18), Align: ui.AlignCenter},
			ui.Text(clock, ui.Style{Font: ui.LargeTitle.Weighted(ui.Bold)}),
			ui.Segmented(ui.SegmentedProps{Segments: ui.Val(names), Selected: chosen}),
			ui.Row(ui.Style{Gap: 8},
				ui.Button(ui.ButtonProps{Title: startTitle, Variant: ui.ButtonFilled, OnPress: toggle}),
				ui.Button(ui.ButtonProps{Title: ui.Val("Reset…"), OnPress: func() { confirm.Set(true) }}),
			),
			// A dialog over the flyout: modal to it, above it.
			ui.Window(ui.WindowProps{Open: confirm, Modal: true, Title: ui.Val("Reset the timer?"), Width: 300, Height: 110,
				DisableResize: true, Content: func() ui.Node {
					return ui.Column(ui.Style{Gap: 16, Padding: ui.All(18), Grow: 1},
						ui.StaticText("Reset the timer?"),
						ui.Row(ui.Style{Gap: 8, Justify: ui.JustifyEnd},
							ui.Button(ui.ButtonProps{Title: ui.Val("Cancel"), OnPress: func() { confirm.Set(false) }}),
							ui.Button(ui.ButtonProps{Title: ui.Val("Reset"), Variant: ui.ButtonFilled, OnPress: func() {
								running.Set(false)
								setLength(minutes.Peek())
								confirm.Set(false)
							}}),
						),
					)
				}}),
		)
	}
	return ui.Column(ui.Style{Grow: 1},
		panel(),
		ui.Window(ui.WindowProps{Open: open, Title: ui.Val("Focus"), Width: 320, Height: 220, Content: panel}),
		ui.Window(ui.WindowProps{Open: flyout, Flyout: &ui.Flyout{Anchor: icon.Anchor(), Placement: ui.PlacementBottom, Gap: 4},
			FitContent: true, MinWidth: 260, Content: flyoutContent}),
	)
}
