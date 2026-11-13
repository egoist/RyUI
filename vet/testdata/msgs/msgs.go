// Package msgs is a catalog's functions, as asuka generate writes them.
package msgs

import (
	"github.com/egoist/asuka/i18n"
	"github.com/egoist/asuka/signal"
)

type Messages struct{ locale string }

func In(locale string) Messages { return Messages{locale} }

func (m Messages) Title() string { return "Title in " + m.locale }

// Title says the message in the language the app shows.
func Title() string { return In(i18n.Locale()).Title() } // want Title:"reads the language the app shows"

type LiveMessages struct{}

var Live LiveMessages

func (LiveMessages) Title() signal.Accessor[string] { return signal.Func(Title) }
