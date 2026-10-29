// List10k shows 10,000 rows in a List: only the cells on screen have
// rows, and a recycled cell updates only the props that differ.
//
//	go run ./cmd/nui dev -ios examples/list10k
package main

import (
	"fmt"

	"github.com/egoist/nui/app"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

type item struct {
	id      int
	title   string
	detail  string
	starred bool
}

var words = []string{"amber", "basil", "cedar", "delta", "ember", "fjord", "garnet", "harbor", "indigo", "juniper"}

func App() ui.Node {
	all := make([]item, 10000)
	for i := range all {
		all[i] = item{
			id:     i,
			title:  fmt.Sprintf("Row %d", i+1),
			detail: fmt.Sprintf("%s %s · %d", words[i%len(words)], words[(i/10)%len(words)], i*7%1000),
		}
	}
	items := signal.NewFunc(all, nil)
	starred := signal.NewMemo(func() int {
		n := 0
		for _, it := range items.Get() {
			if it.starred {
				n++
			}
		}
		return n
	})
	toggle := func(id int) {
		items.Update(func(old []item) []item {
			next := append([]item{}, old...)
			next[id].starred = !next[id].starred
			return next
		})
	}

	return ui.SafeArea(ui.Style{},
		ui.Column(ui.Style{Padding: ui.Symmetric(8, 16), Gap: 2},
			ui.StaticText("10,000 rows", ui.Style{Font: ui.LargeTitle.Weighted(ui.Bold)}),
			ui.Text(signal.Func(func() string { return fmt.Sprintf("%d starred · tap a row to star it", starred.Get()) }),
				ui.Style{Font: ui.Subheadline, Color: ui.SecondaryLabel}),
		),
		ui.List(ui.ListProps{}, items, func(it item) int { return it.id },
			func(it signal.Accessor[item], _ signal.Accessor[int]) ui.Node {
				return ui.View(ui.ViewProps{
					Style:   ui.Style{Direction: ui.RowDir, Align: ui.AlignCenter, Gap: 12, Padding: ui.Symmetric(10, 16)},
					OnPress: func() { toggle(it.Get().id) },
				},
					ui.Column(ui.Style{Grow: 1, Gap: 2},
						ui.Text(signal.Func(func() string { return it.Get().title }), ui.Style{Font: ui.Body}),
						ui.Text(signal.Func(func() string { return it.Get().detail }), ui.Style{Font: ui.Footnote, Color: ui.SecondaryLabel}),
					),
					ui.Text(signal.Func(func() string {
						if it.Get().starred {
							return "★"
						}
						return "☆"
					}), ui.Style{Font: ui.Title3, Color: ui.Orange}),
				)
			}),
	)
}

func main() { app.Run(App) }
