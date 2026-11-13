// Plugin app is the GUI tests' app of native packages: the greeter
// (e2e/testdata/greeter) and the kitchen (internal/nativegen's testdata),
// each implemented on every platform. It calls their modules as it
// starts, and shows the greeter's view, which says what it shows.
package main

import (
	"fmt"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/e2e/testdata/greeter"
	"github.com/egoist/asuka/internal/nativegen/testdata/kitchen"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func Plugin() ui.Node {
	greeting := signal.New(say(func() (string, error) {
		g, err := greeter.NewGreeter()
		if err != nil {
			return "", err
		}
		return g.Hello("Asuka")
	}))
	cooked := signal.New(say(func() (string, error) {
		k, err := kitchen.NewKitchen()
		if err != nil {
			return "", err
		}
		items, err := k.Items("salt")
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("kitchen %s: %s %g%s, %d tags", k.Name(), items[0].Name, items[0].Weight, items[0].Unit, len(items[0].Tags)), nil
	}))
	shown := signal.New("no badge")
	return ui.SafeArea(ui.Style{Gap: 16, Padding: ui.All(24)},
		ui.Text(greeting),
		ui.Text(cooked),
		greeter.Badge(greeter.BadgeProps{Name: ui.Val("Asuka"), Shown: func(s string) { shown.Set(s) }, Style: ui.Style{Height: ui.Pt(32)}}),
		ui.Text(signal.Func(func() string { return "shown: " + shown.Get() })),
	)
}

// say is what f says, or its error.
func say(f func() (string, error)) string {
	s, err := f()
	if err != nil {
		return "error: " + err.Error()
	}
	return s
}

func main() { app.Run(Plugin) }
