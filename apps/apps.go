// Package apps finds the apps installed on the computer, as a launcher
// does: it lists them (List), draws their icons (Icon), starts them
// (Open), and ranks them against what the user types (Search).
//
// macOS lists the .app bundles where Spotlight finds them, Windows the
// Start menu's All apps (desktop and packaged apps alike), Linux the
// desktop entries a menu shows. Phones don't let an app list the others,
// and a page sees none: there List, Icon and Open answer ErrUnsupported.
//
//	apps.List(func(list []apps.App, err error) {
//		all.Set(list)
//	})
//	shown := apps.Search(all.Get(), query.Get())
package apps

import (
	"cmp"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"github.com/egoist/asuka/ui"
)

// ErrUnsupported: the platform has no apps to list (phones, the web).
var ErrUnsupported = errors.New("apps: not on this platform")

// App is an app installed on the computer.
type App struct {
	// Name is the name the system shows for it, in the user's language,
	// without an extension (Calculator, not Calculator.app).
	Name string
	// ID is the system's own for it: its bundle identifier on macOS (its
	// path where its bundle has none), its AppUserModelID on Windows (a
	// desktop app without one: its path under a known folder), its
	// desktop file ID on Linux (org.gnome.Nautilus.desktop).
	ID string
	// Path is where it is: its .app bundle on macOS, its item in the
	// shell's Applications folder on Windows (shell:AppsFolder\<ID>), its
	// desktop entry (a .desktop file) on Linux. Icon and Open take it.
	Path string
}

// wireApp is an App as the hosts send it.
type wireApp struct {
	Name string `json:"name"`
	ID   string `json:"id"`
	Path string `json:"path"`
}

// List gives done the installed apps, sorted by name (case and accents
// aside), each once: an app found twice (its ID the same) is the first
// place the platform looks. The hosts look off the main thread; done
// runs on it.
//
//   - macOS: the .app bundles in /Applications, /System/Applications,
//     ~/Applications and /System/Library/CoreServices/Applications, and
//     in the folders one level under them (Utilities, an app's own
//     folder), never inside a bundle; and Finder. An app a symbolic link
//     points to (/Applications/Safari.app) is listed at the link.
//   - Windows: the Start menu's All apps, the shell's Applications
//     folder (FOLDERID_AppsFolder): desktop apps by their shortcuts,
//     packaged apps by their AppUserModelIDs.
//   - Linux: the desktop entries in $XDG_DATA_HOME/applications, then
//     each of $XDG_DATA_DIRS's (and Flatpak's and Snap's exports), an
//     entry hiding a later one of its ID; entries of Type Application
//     that a menu shows (not NoDisplay or Hidden, OnlyShowIn and
//     NotShowIn against $XDG_CURRENT_DESKTOP, TryExec found), named in
//     the user's language (Name[de_DE], Name[de]).
//
// Main thread.
func List(done func([]App, error)) {
	ui.Call("apps", "list", nil, func(b []byte, err error) {
		if done == nil {
			return
		}
		if err != nil {
			done(nil, hostError(err))
			return
		}
		list, err := decodeList(b)
		done(list, err)
	})
}

// decodeList reads the hosts' list (JSON), then sorts it by name and
// takes out what's there twice.
func decodeList(b []byte) ([]App, error) {
	var w []wireApp
	if len(b) > 0 {
		if err := json.Unmarshal(b, &w); err != nil {
			return nil, errors.New("apps: the host's list: " + err.Error())
		}
	}
	list := make([]App, 0, len(w))
	seen := make(map[string]bool, len(w))
	for _, a := range w {
		if a.Name == "" || a.Path == "" {
			continue
		}
		key := a.ID
		if key == "" {
			key = a.Path
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		list = append(list, App(a))
	}
	sortByName(list)
	return list, nil
}

// sortByName sorts apps by their names, case and accents aside, then as
// written, then by path: the same apps always in the same order.
func sortByName(list []App) {
	keys := make(map[string]string, len(list))
	key := func(s string) string {
		k, ok := keys[s]
		if !ok {
			k = fold(s)
			keys[s] = k
		}
		return k
	}
	slices.SortStableFunc(list, func(a, b App) int {
		return cmp.Or(strings.Compare(key(a.Name), key(b.Name)), strings.Compare(a.Name, b.Name), strings.Compare(a.Path, b.Path))
	})
}

// Icon draws the app's icon as a PNG file, size points square at the
// main display's scale (size×2 pixels on a Retina display), and gives
// done its path, for ui.Image (give the image the size: it's drawn in
// pixels). The files are in the app's caches (app.CacheDir), one per
// app and size, drawn again once the app has changed.
//
//   - macOS: the icon Finder shows (NSWorkspace's iconForFile:).
//   - Windows: the icon the Start menu shows
//     (IShellItemImageFactory, at that many pixels).
//   - Linux: the entry's Icon from the icon theme (or the file it
//     names), application-x-executable where the theme has none.
//
// Main thread.
func Icon(app App, size float32, done func(path string, err error)) {
	answer := func(path string, err error) {
		if done != nil {
			done(path, err)
		}
	}
	switch {
	case app.Path == "":
		answer("", errors.New("apps: Icon of an app without a path"))
		return
	case !(size > 0):
		answer("", errors.New("apps: Icon of no size"))
		return
	}
	args, _ := json.Marshal(struct {
		Path string  `json:"path"`
		Size float32 `json:"size"`
	}{app.Path, size})
	ui.Call("apps", "icon", args, func(b []byte, err error) {
		if err != nil {
			answer("", hostError(err))
			return
		}
		answer(string(b), nil)
	})
}

// Open starts the app, or brings it to the front if it's running, as a
// click in the Dock or the Start menu does: NSWorkspace's
// openApplicationAtURL: on macOS, the shell's default verb on its item
// on Windows, GIO's launch of its desktop entry on Linux (with the
// display's launch context: the app gets the activation token, so it
// takes the focus). Hide a launcher's panel first, so the app the user
// was in keeps the keyboard until the opened one takes it. done (if not
// nil) runs once it's started, or with why it couldn't. Main thread.
func Open(app App, done func(error)) {
	if app.Path == "" {
		if done != nil {
			done(errors.New("apps: Open of an app without a path"))
		}
		return
	}
	ui.Call("apps", "open", []byte(app.Path), func(_ []byte, err error) {
		if done != nil {
			done(hostError(err))
		}
	})
}

// hostError maps the hosts' answers: "unsupported", and a host without
// the module (a phone's "no module method apps.list", the web's "isn't
// on the web", a desktop host's "isn't on ... yet", host/fake's "no
// module").
func hostError(err error) error {
	if err == nil {
		return nil
	}
	switch s := err.Error(); {
	case s == "unsupported", strings.HasPrefix(s, "no module method apps."), s == `no module "apps"`,
		strings.Contains(s, "apps.") && strings.Contains(s, "isn't on"):
		return ErrUnsupported
	}
	return err
}
