// Package greeter is a native package (asuka native): a module that says
// hello, and a view that says what it shows, each platform's own code
// implementing them (Swift on iOS and macOS, Kotlin on Android,
// JavaScript on the web, Go on Windows and Linux). The GUI tests' plugin
// app (e2e/testdata/pluginapp) uses it on every platform.
package greeter

import (
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Greeter greets.
//
//asuka:native
type Greeter interface {
	// Hello is the platform's greeting to name.
	Hello(name string) (string, error)
}

// BadgeProps are a Badge's: the name it shows, and what it says it shows.
//
//asuka:view
type BadgeProps struct {
	Name  signal.Accessor[string]
	Shown func(text string)
	Style ui.Style
}
