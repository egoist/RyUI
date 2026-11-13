// The Asuka side of the benchmarks (bench/README.md): the same scenarios,
// frame by frame, as bench/swiftui and bench/compose, written as a Asuka
// app would be. The meter is a native package's module (meter/).
//
//	go run ./bench/run -ios        # builds this and the others, runs them, compares
package main

import (
	"strconv"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/bench/asuka/meter"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

const rows = 1000      // the mount and update scenarios' rows
const listRows = 10000 // the scroll scenario's

type phase int

const (
	ready phase = iota
	mounted
	list
)

// noMeter is the meter where the package has none (the desktop, the web):
// the scenarios run, unmeasured.
type noMeter struct{}

func (noMeter) Begin(string)  {}
func (noMeter) End()          {}
func (noMeter) Startup()      {}
func (noMeter) Memory(string) {}
func (noMeter) Done()         {}

func App() ui.Node {
	var m meter.Meter = noMeter{}
	if mm, err := meter.NewMeter(); err == nil {
		m = mm
	}
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
			m.Startup()
			m.Memory("memory-idle")
		case f == 29: // a frame ahead: what's measured starts with the next
			m.Begin("mount")
		case f == 30:
			ph.Set(mounted)
		case f == 36:
			m.End()
			m.Memory("memory-mount")
		case f == 60:
			m.Begin("update-one")
		case f > 60 && f <= 300:
			v := values[(f-61)%rows]
			v.Set(v.Peek() + 1)
		case f == 301:
			m.End()
		case f == 330:
			m.Begin("update-all")
		case f > 330 && f <= 450:
			tick.Set(tick.Peek() + 1)
		case f == 451:
			m.End()
		case f == 480:
			ph.Set(list)
		case f == 500:
			m.Begin("scroll")
		case f > 500 && f <= 800:
			ui.ScrollTo(scroller, 0, float32(f-500)*8, false)
		case f == 801:
			m.End()
			m.Memory("memory-list")
		case f == 830:
			m.Done()
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
						ui.Text(signal.Func(func() string { return "Row " + strconv.Itoa(i.Get()) }), ui.Style{Font: fonts.Headline}),
						ui.Text(signal.Func(func() string { return "Detail " + strconv.Itoa(i.Get()*7%1000) }), ui.Style{Color: colors.SecondaryLabel}))
				})
			return scroller
		}
		return ui.StaticText("Ready", ui.Style{Font: fonts.LargeTitle})
	}))
}

func main() { app.Run(App) }
