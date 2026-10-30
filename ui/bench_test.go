package ui_test

import (
	"strconv"
	"testing"

	"github.com/egoist/nui/host/fake"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

// The bench/ scenarios against the fake host: the core's share of each
// frame (signals, effects, ops, layout), without a platform's views.

type rowsApp struct {
	values []*signal.Signal[int]
	tick   *signal.Signal[int]
	show   *signal.Signal[bool]
}

func mountRows(b *testing.B, n int, shown bool) (*fake.Host, *ui.Root, *rowsApp) {
	a := &rowsApp{tick: signal.New(0), show: signal.New(shown)}
	for range n {
		a.values = append(a.values, signal.New(0))
	}
	h := fake.New()
	r := ui.Mount(h, func() ui.Node {
		return ui.Show(a.show, func() ui.Node {
			kids := make([]ui.Node, n)
			for i := range kids {
				v := a.values[i]
				kids[i] = ui.Row(ui.Style{Padding: ui.Symmetric(8, 16)},
					ui.StaticText("Row "+strconv.Itoa(i), ui.Style{Grow: 1}),
					ui.Text(signal.Func(func() string { return strconv.Itoa(v.Get() + a.tick.Get()) })))
			}
			return ui.Scroll(ui.ScrollProps{Style: ui.Style{Grow: 1}}, kids...)
		})
	})
	b.Cleanup(r.Dispose)
	return h, r, a
}

func frame(h *fake.Host) {
	h.Dispatch(ops.Event{ID: h.Root().ID, Kind: ops.EventFrame})
}

// Mounting 1,000 rows, and taking them down.
func BenchmarkMount1000(b *testing.B) {
	_, r, a := mountRows(b, 1000, false)
	b.ReportAllocs()
	for b.Loop() {
		a.show.Set(true)
		r.Flush()
		a.show.Set(false)
		r.Flush()
	}
}

// One of 1,000 rows' text changes.
func BenchmarkUpdateOne1000(b *testing.B) {
	h, r, a := mountRows(b, 1000, true)
	i := 0
	b.ReportAllocs()
	for b.Loop() {
		v := a.values[i%1000]
		v.Set(v.Peek() + 1)
		r.Flush()
		i++
	}
	_ = h
}

// All 1,000 rows' texts change.
func BenchmarkUpdateAll1000(b *testing.B) {
	_, r, a := mountRows(b, 1000, true)
	b.ReportAllocs()
	for b.Loop() {
		a.tick.Set(a.tick.Peek() + 1)
		r.Flush()
	}
}
