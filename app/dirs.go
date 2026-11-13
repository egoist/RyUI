package app

import (
	"os"

	"github.com/egoist/asuka/internal/appdir"
)

// The app's own directories, made (with what's above them) as they're
// asked for. Each is the app's alone, named for it by the platform's
// rules: its name on macOS and Windows, its ID on Linux (a Flatpak's are
// in its sandbox's home), its container's on iOS and its package's on
// Android. A run under asuka dev has its own, in its work directory, so
// it never touches the installed app's.
//
//	dir, err := app.DataDir()
//	db, err := sql.Open("sqlite", filepath.Join(dir, "notes.db"))

// DataDir is where the app keeps its data, what it can't make again:
// asuka/store's file is there, and the hosts' saved state (the window's
// place). macOS: ~/Library/Application Support/<Name>. Windows: Local
// AppData\<Name>. Linux: $XDG_DATA_HOME/<id> (~/.local/share). iOS:
// Application Support in its container (its App Group's with a share
// sheet, which shares it). Android: its files directory. A browser:
// /data/<name>, kept in IndexedDB. It's store.Dir.
func DataDir() (string, error) { return made(appdir.Data()) }

// ConfigDir is where the app keeps its settings: Windows: Roaming
// AppData\<Name>, which follows the user to another PC of the domain's.
// Linux: $XDG_CONFIG_HOME/<id> (~/.config). Elsewhere it's DataDir.
func ConfigDir() (string, error) { return made(appdir.Config()) }

// CacheDir is where the app keeps what it can download or make again,
// which the system (or the user, cleaning up) may remove when it's out
// of space. macOS: ~/Library/Caches/<Name>. Windows: Local
// AppData\<Name>\Cache. Linux: $XDG_CACHE_HOME/<id> (~/.cache). iOS: the
// container's Caches. Android: its cache directory. A browser: a folder
// of /tmp, which lasts as long as the page.
func CacheDir() (string, error) { return made(appdir.Cache()) }

// LogDir is where the app writes its logs. macOS: ~/Library/Logs/<Name>,
// which Console shows. Linux: $XDG_STATE_HOME/<id> (~/.local/state).
// Windows: Local AppData\<Name>\Logs. Elsewhere, a logs folder in
// DataDir.
func LogDir() (string, error) { return made(appdir.Logs()) }

func made(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}
