// Package l10n holds Asuka's own words: the few that hosts' platforms don't
// supply where Asuka shows them (an alert's only button, a search field's
// placeholder, a desktop back button), in the languages it is translated
// to. They are a library's catalog of the namespace asuka (see
// i18n.NewLibraryCatalog): an app changes them, or gives them its other
// languages, with keys "asuka.<key>" in its own catalogs, as "asuka.ok" in
// its de.json. A language they lack says English.
package l10n

import (
	"embed"
	"io/fs"

	"github.com/egoist/asuka/i18n"
	"github.com/egoist/asuka/internal/i18nlink"
	"github.com/egoist/asuka/ops"
)

// Namespace is that of the keys with which apps change Asuka's words:
// "asuka.<key>".
const Namespace = "asuka"

//go:embed messages/*.json
var files embed.FS

// FS returns the catalogs, which `asuka generate` checks the keys
// "asuka.<key>" of apps against.
func FS() fs.FS {
	sub, err := fs.Sub(files, "messages")
	if err != nil {
		panic(err)
	}
	return sub
}

// Catalog holds the words. It doesn't link the formatting of dates, which
// no message of Asuka's needs and every app would carry.
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

// MoreOptions says what a bar's overflow button opens (the actions that
// don't fit), in locale, for its tooltip and a screen reader.
func MoreOptions(locale string) string { return Catalog.Format(locale, "moreOptions", nil) }

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

// Cancel says the button that closes a sheet or an alert without doing
// what it offers, in locale.
func Cancel(locale string) string { return Catalog.Format(locale, "cancel", nil) }

// Close is the label of a button that closes what it's on: a snackbar's
// on a desktop (macOS, Windows, the web).
func Close(locale string) string { return Catalog.Format(locale, "close", nil) }

// TakePhoto says the button that takes a photo (the camera's sheet on
// the Mac), in locale.
func TakePhoto(locale string) string { return Catalog.Format(locale, "takePhoto", nil) }

// AddToContacts says the button that adds a contact (on the Mac, which
// has no new contact screen), in locale.
func AddToContacts(locale string) string { return Catalog.Format(locale, "addToContacts", nil) }

// AddToCalendar says the button that adds an event (on the Mac, which
// has no event editor), in locale.
func AddToCalendar(locale string) string { return Catalog.Format(locale, "addToCalendar", nil) }

// NewEvent says the title of an event that has none, in locale.
func NewEvent(locale string) string { return Catalog.Format(locale, "newEvent", nil) }

// Badge says a tab's or a sidebar row's badge for a screen reader, after
// its title ("Inbox, 3 items"), in locale: a count (ops.BadgeNumber) as
// so many items, other text as it is.
func Badge(locale, text string) string {
	if n, ok := ops.BadgeNumber(text); ok {
		return Catalog.Format(locale, "badgeCount", map[string]any{"count": n})
	}
	return text
}

// Badged says a title with its badge, if it has one, for a screen
// reader: "Inbox, 3 items".
func Badged(locale, title, badge string) string {
	if badge == "" {
		return title
	}
	return title + ", " + Badge(locale, badge)
}
