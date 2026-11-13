// Package appdir finds the app's own directories on each platform: its
// data (asuka/sdk/store's, the hosts' saved state), configuration, caches and
// logs. app's DataDir, ConfigDir, CacheDir and LogDir make them; store.Dir
// is Data.
//
// Each can be set with the environment: ASUKA_DATA (which the hosts set
// as they start, from the app's name or ID: so every other directory is
// named as it is), ASUKA_CONFIG, ASUKA_CACHE and ASUKA_LOGS (asuka dev
// sets them all to its work directory, so a run under development keeps
// its files apart from the installed app's).
package appdir

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/egoist/asuka/mobile/shareddir"
)

// goos is runtime.GOOS (tests replace it).
var goos = runtime.GOOS

// Data is the app's data directory (on Android its files directory, as
// ASUKA_DATA says; on iOS Application Support in its container, or in
// its App Group with a share sheet, which runs in its share extension;
// on macOS Application Support/<Name>; on Windows Local AppData\<Name>;
// on Linux the XDG data directory's folder for its ID), or, running
// elsewhere (the asuka dev player's Mac), the user's config directory.
func Data() string {
	if d := os.Getenv("ASUKA_DATA"); d != "" {
		return d
	}
	if shared := shareddir.Dir(); shared != "" {
		return sharedDir(shared)
	}
	// Android sets ASUKA_DATA once the library is loaded, after the
	// packages' init: a store.Signal in a package variable reads the
	// store before it. The files directory is the package's, for the
	// process's user: what Context.getFilesDir returns.
	if goos == "android" {
		if pkg := androidPackage(); pkg != "" {
			return fmt.Sprintf("/data/user/%d/%s/files", os.Getuid()/100000, pkg)
		}
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = os.TempDir()
	}
	return filepath.Join(d, "asuka", filepath.Base(os.Args[0]))
}

// name is the app's folder's name: the data directory's, which the host
// named (macOS and Windows: the app's name; Linux: its ID).
func name() string { return filepath.Base(Data()) }

// Config is where the app keeps its settings: on Windows Roaming
// AppData\<Name> (it follows the user to another PC of the domain's), on
// Linux the XDG config directory's folder for its ID; on the other
// platforms, the data directory (macOS's Preferences is the defaults'
// own, and a phone's app has one place for its files).
func Config() string {
	if d := os.Getenv("ASUKA_CONFIG"); d != "" {
		return d
	}
	switch goos {
	case "windows":
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, name())
		}
	case "linux":
		if d := xdg("XDG_CONFIG_HOME", ".config"); d != "" {
			return filepath.Join(d, name())
		}
	}
	return Data()
}

// Cache is where the app keeps what it can make again, which the system
// may clear: on macOS Caches/<Name>, on iOS the container's Caches, on
// Android its cache directory, on Windows the Cache folder of its Local
// AppData (Local AppData is the caches' place there too), on Linux the
// XDG cache directory's folder for its ID, in a browser the page's /tmp.
func Cache() string {
	if d := os.Getenv("ASUKA_CACHE"); d != "" {
		return d
	}
	switch goos {
	case "android":
		// The files directory's sibling: Context.getCacheDir.
		return filepath.Join(filepath.Dir(Data()), "cache")
	case "ios":
		if d, err := os.UserCacheDir(); err == nil {
			return d
		}
	case "windows":
		return filepath.Join(Data(), "Cache")
	case "linux":
		if d := xdg("XDG_CACHE_HOME", ".cache"); d != "" {
			return filepath.Join(d, name())
		}
	case "js":
		return filepath.Join("/tmp", name())
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, name())
	}
	return filepath.Join(os.TempDir(), name())
}

// Logs is where the app writes its logs: on macOS Logs/<Name> (Console
// shows them), on Linux the XDG state directory's folder for its ID
// (logs are state, by the spec); elsewhere a logs folder in the data
// directory (Windows's Local AppData\<Name>\Logs).
func Logs() string {
	if d := os.Getenv("ASUKA_LOGS"); d != "" {
		return d
	}
	switch goos {
	case "darwin":
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, "Library", "Logs", name())
		}
	case "linux":
		if d := xdg("XDG_STATE_HOME", filepath.Join(".local", "state")); d != "" {
			return filepath.Join(d, name())
		}
	case "windows":
		return filepath.Join(Data(), "Logs")
	}
	return filepath.Join(Data(), "logs")
}

// xdg is an XDG base directory: the variable's, if it's an absolute
// path (the spec ignores a relative one), else home's fallback.
func xdg(env, fallback string) string {
	if d := os.Getenv(env); filepath.IsAbs(d) {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, fallback)
	}
	return ""
}

// androidPackage is the app's package, from the process's command line
// ("pkg" or "pkg:service"), or "".
func androidPackage() string {
	b, err := os.ReadFile("/proc/self/cmdline")
	if err != nil {
		return ""
	}
	pkg, _, _ := strings.Cut(string(b), "\x00")
	pkg, _, _ = strings.Cut(pkg, ":") // a process of the app's, "pkg:sync"
	if pkg == "" || strings.Contains(pkg, "/") {
		return ""
	}
	return pkg
}

var migrate sync.Once

// sharedDir is the data directory in an iOS app's App Group (shared,
// its Application Support), where the app's data moves from its
// container once, the first time it has a share sheet.
func sharedDir(shared string) string {
	dir := filepath.Join(shared, "asuka", "app")
	migrate.Do(func() {
		if _, err := os.Stat(dir); err == nil {
			return
		}
		if base, err := os.UserConfigDir(); err == nil {
			old := filepath.Join(base, "asuka", filepath.Base(os.Args[0]))
			if _, err := os.Stat(old); err == nil {
				os.MkdirAll(filepath.Dir(dir), 0o700)
				os.Rename(old, dir)
			}
		}
	})
	return dir
}
