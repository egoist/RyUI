// Counter is the smallest Asuka app: a signal, a Text that reads it and a
// Button that writes it.
//
//	go run ./cmd/asuka dev --platform ios examples/counter
package main

import (
	"fmt"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func Counter() ui.Node {
	count := signal.New(0)
	return ui.SafeArea(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter, Gap: 16, Padding: ui.All(24)},
		ui.Text(signal.Func(func() string {
			return fmt.Sprintf("Count: %d", count.Get())
		}), ui.Style{Font: fonts.LargeTitle.Weighted(fonts.Bold)}),
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
				return ui.StaticText("That's a lot", ui.Style{Color: colors.SecondaryLabel})
			}),
	)
}

func main() { app.Run(Counter) }
