// Counter is the smallest RyUI app: a signal, a Text that reads it and a
// Button that writes it.
//
//	go run ./cmd/ryui dev -ios examples/counter
package main

import (
	"fmt"

	"github.com/egoist/ryui/app"
	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

func Counter() ui.Node {
	count := signal.New(0)
	return ui.SafeArea(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 16, Padding: ui.All(24)},
		ui.Text(signal.Func(func() string {
			return fmt.Sprintf("Count: %d", count.Get())
		}), ui.Style{Font: ui.LargeTitle.Weighted(ui.Bold)}),
		ui.Row(ui.Style{Gap: 12},
			ui.Button(ui.ButtonProps{
				Title:   ui.Val("Reset"),
				Variant: ui.ButtonGray,
				OnPress: func() { count.Set(0) },
			}),
			ui.Button(ui.ButtonProps{
				Title:   ui.Val("Add"),
				Variant: ui.ButtonFilled,
				OnPress: func() { count.Update(func(n int) int { return n + 1 }) },
			}),
		),
		ui.Show(signal.Func(func() bool { return count.Get() >= 10 }),
			func() ui.Node {
				return ui.StaticText("That's a lot", ui.Style{Color: ui.SecondaryLabel})
			}),
	)
}

func main() { app.Run(Counter) }
