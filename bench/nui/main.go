// The nui side of the benchmarks (bench/README.md): the same scenarios,
// frame by frame, as bench/swiftui and bench/compose, written as a nui
// app would be. The meter is the native module in ios/ and android/.
//
//	go run ./bench/run -ios        # builds this and the others, runs them, compares
package main

import (
	"strconv"
	"time"

	"github.com/egoist/nui/app"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

const rows = 1000      // the mount and update scenarios' rows
const listRows = 10000 // the scroll scenario's

type phase int

const (
	ready phase = iota
	mounted
	list
)

func call(method, arg string) { ui.Call("bench", method, []byte(arg), nil) }

func App() ui.Node {
	ph := signal.New(ready)
	values := make([]*signal.Signal[int], rows)
	for i := range values {
		values[i] = signal.New(0)
	}
	tick := signal.New(0) // every row shows it: update-all changes them all
	var scroller ui.Node
	frame := 0
	// The schedule, in frames; the same in each app.
	ui.OnFrame(func(time.Duration) {
		frame++
		switch f := frame; {
		case f == 2:
			call("startup", "")
			call("memory", "memory-idle")
		case f == 29: // a frame ahead: what's measured starts with the next
			call("begin", "mount")
		case f == 30:
			ph.Set(mounted)
		case f == 36:
			call("end", "")
			call("memory", "memory-mount")
		case f == 60:
			call("begin", "update-one")
		case f > 60 && f <= 300:
			v := values[(f-61)%rows]
			v.Set(v.Peek() + 1)
		case f == 301:
			call("end", "")
		case f == 330:
			call("begin", "update-all")
		case f > 330 && f <= 450:
			tick.Set(tick.Peek() + 1)
		case f == 451:
			call("end", "")
		case f == 480:
			ph.Set(list)
		case f == 500:
			call("begin", "scroll")
		case f > 500 && f <= 800:
			ui.ScrollTo(scroller, 0, float32(f-500)*8, false)
		case f == 801:
			call("end", "")
			call("memory", "memory-list")
		case f == 830:
			call("done", "")
		}
	})
	items := make([]int, listRows)
	for i := range items {
		items[i] = i
	}
	return ui.SafeArea(ui.Style{Grow: 1}, ui.Dynamic(func() ui.Node {
		switch ph.Get() {
		case mounted:
			kids := make([]ui.Node, rows)
			for i := range kids {
				v := values[i]
				kids[i] = ui.Row(ui.Style{Padding: ui.Symmetric(8, 16)},
					ui.StaticText("Row "+strconv.Itoa(i), ui.Style{Grow: 1}),
					ui.Text(signal.Func(func() string { return strconv.Itoa(v.Get() + tick.Get()) })))
			}
			return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}}, kids...)
		case list:
			scroller = ui.List(ui.ListProps{}, ui.Val(items), func(i int) int { return i },
				func(i signal.Accessor[int], _ signal.Accessor[int]) ui.Node {
					return ui.Column(ui.Style{Padding: ui.Symmetric(8, 16), Gap: 2},
						ui.Text(signal.Func(func() string { return "Row " + strconv.Itoa(i.Get()) }), ui.Style{Font: ui.Headline}),
						ui.Text(signal.Func(func() string { return "Detail " + strconv.Itoa(i.Get()*7%1000) }), ui.Style{Color: ui.SecondaryLabel}))
				})
			return scroller
		}
		return ui.StaticText("Ready", ui.Style{Font: ui.LargeTitle})
	}))
}

func main() { app.Run(App) }
