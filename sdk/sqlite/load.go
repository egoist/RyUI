//go:build !js

package sqlite

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/egoist/asuka/plugin"
)

// The plugin's manifest: its library, asqlite, for each platform.
//
//go:embed asuka-plugin.json
var manifest []byte

// loadLibrary finds and loads SQLite's library: ASUKA_SQLITE_LIB if set,
// else the plugin's (package plugin: the one asuka build shipped with the
// app; in go run, go test and asuka dev, this checkout's build or the
// published one, downloaded once).
func loadLibrary() (library, error) {
	p := os.Getenv("ASUKA_SQLITE_LIB")
	if p == "" {
		var err error
		if p, err = plugin.LibraryPath(manifest, "asqlite"); err != nil {
			return nil, err
		}
	}
	return openLib(p)
}

// versionOK checks the library is the SQLite this package was built for.
func versionOK(n int) error {
	if n != libVersion {
		return fmt.Errorf("the library is SQLite %d, not %d: it's another version of Asuka's", n, libVersion)
	}
	return nil
}
