// Tray is a focus timer that lives in the tray (the menu bar on a Mac):
// its icon counts down beside it, its menu starts and pauses the timer
// and picks how long it runs, and opens a window with the timer in it.
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

	tray.Show(tray.Props{
		ID:      "timer",
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
	return ui.Column(ui.Style{Grow: 1},
		panel(),
		ui.Window(ui.WindowProps{Open: open, Title: ui.Val("Focus"), Width: 320, Height: 220, Content: panel}),
	)
}
