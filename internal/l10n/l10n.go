// Package l10n holds RyUI's own words: the few that hosts' platforms don't
// supply where RyUI shows them (an alert's only button, a search field's
// placeholder, a desktop back button), in the languages it is translated
// to. They are a library's catalog of the namespace ryui (see
// i18n.NewLibraryCatalog): an app changes them, or gives them its other
// languages, with keys "ryui.<key>" in its own catalogs, as "ryui.ok" in
// its de.json. A language they lack says English.
package l10n

import (
	"embed"
	"io/fs"

	"github.com/egoist/ryui/i18n"
	"github.com/egoist/ryui/internal/i18nlink"
)

// Namespace is that of the keys with which apps change RyUI's words:
// "ryui.<key>".
const Namespace = "ryui"

//go:embed messages/*.json
var files embed.FS

// FS returns the catalogs, which `ryui generate` checks the keys
// "ryui.<key>" of apps against.
func FS() fs.FS {
	sub, err := fs.Sub(files, "messages")
	if err != nil {
		panic(err)
	}
	return sub
}

// Catalog holds the words. It doesn't link the formatting of dates, which
// no message of RyUI's needs and every app would carry.
var Catalog = func() *i18n.Catalog {
	c, err := i18nlink.NewLibraryCatalog(FS(), "en", Namespace)
	if err != nil {
		panic(err)
	}
	return c.(*i18n.Catalog)
}()

// OK says the only button of an alert that names none, in locale.
func OK(locale string) string { return Catalog.Format(locale, "ok", nil) }

// Search says a search field's placeholder, in locale.
func Search(locale string) string { return Catalog.Format(locale, "search", nil) }

// Back says a back button's title and label, in locale.
func Back(locale string) string { return Catalog.Format(locale, "back", nil) }

// Decrease says what a stepper's minus button does, in locale, for a screen
// reader.
func Decrease(locale string) string { return Catalog.Format(locale, "decrease", nil) }

// Increase says what a stepper's plus button does, in locale, for a screen
// reader.
func Increase(locale string) string { return Catalog.Format(locale, "increase", nil) }

// About says the app menu's item that shows the about panel, of the app
// named app, in locale.
func About(locale, app string) string {
	return Catalog.Format(locale, "about", map[string]any{"app": app})
}

// Hide says the app menu's item that hides the app named app, in locale.
func Hide(locale, app string) string {
	return Catalog.Format(locale, "hide", map[string]any{"app": app})
}

// Quit says the app menu's item that quits the app named app, in locale.
func Quit(locale, app string) string {
	return Catalog.Format(locale, "quit", map[string]any{"app": app})
}

// HideOthers says the app menu's item that hides the other apps, in locale.
func HideOthers(locale string) string { return Catalog.Format(locale, "hideOthers", nil) }

// Zoom says the Window menu's item that zooms the window, in locale.
func Zoom(locale string) string { return Catalog.Format(locale, "zoom", nil) }
