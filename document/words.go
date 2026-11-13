package document

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/asuka/i18n"
	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/i18nlink"
	"github.com/egoist/asuka/ui"
)

// The package's words: its menus' items, its questions and their
// buttons, and untitled documents' names, in the language the app shows.
// They are a library's catalog (i18n.NewLibraryCatalog) of the namespace
// "document": an app changes them, or gives them its other languages,
// with keys "document.<key>" in its own catalogs ("document.untitled":
// "Sans titre"). A language they lack says English.

//go:embed messages/*.json
var files embed.FS

var catalog = func() *i18n.Catalog {
	sub, err := fs.Sub(files, "messages")
	if err != nil {
		panic(err)
	}
	c, err := i18nlink.NewLibraryCatalog(sub, "en", "document")
	if err != nil {
		panic(err)
	}
	return c.(*i18n.Catalog)
}()

// say is the word key in the language the app shows.
func say(key string) string { return catalog.Format(ui.Locale(), key, nil) }

// sayWith is the message key with args, in the language the app shows.
func sayWith(key string, args map[string]any) string {
	return catalog.Format(ui.Locale(), key, args)
}

// untitledName is the name of untitled document n: "Untitled", "Untitled
// 2".
func untitledName(n int) string {
	if n <= 1 {
		return say("untitled")
	}
	return sayWith("untitledNumbered", map[string]any{"n": n})
}

// untitledNumber is the number of an untitled document by its name, as
// untitledName writes it in the app's language or in English (the
// language may have changed since), or 0.
func untitledNumber(name string) int {
	for _, locale := range []string{ui.Locale(), "en"} {
		if name == catalog.Format(locale, "untitled", nil) {
			return 1
		}
		format := catalog.Format(locale, "untitledNumbered", map[string]any{"n": "\x00"})
		before, after, ok := strings.Cut(format, "\x00")
		if !ok || !strings.HasPrefix(name, before) || !strings.HasSuffix(name, after) || len(name) < len(before)+len(after) {
			continue
		}
		if n, err := strconv.Atoi(name[len(before) : len(name)-len(after)]); err == nil && n > 1 {
			return n
		}
	}
	return 0
}

// appName is the app's name, as the hosts name it: asuka.json's, else
// the executable's.
func appName() string {
	if boot.Name != "" {
		return boot.Name
	}
	name := filepath.Base(os.Args[0])
	return strings.TrimSuffix(strings.TrimSuffix(name, ".exe"), ".EXE")
}
