// Package testapp is an app for internal/remote's tests: app code, not
// the framework's, so its state is kept across reloads.
package testapp

import (
	"fmt"

	"github.com/egoist/ryui/signal"
	"github.com/egoist/ryui/ui"
)

// Counter is a button that counts its taps.
func Counter() ui.Node {
	n := signal.New(0)
	return ui.Button(ui.ButtonProps{
		Title:   signal.Func(func() string { return fmt.Sprint("count ", n.Get()) }),
		OnPress: func() { n.Set(n.Peek() + 1) },
	})
}
