//go:build darwin && !ios

package darwin

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/devstate"
)

func init() {
	// The app's data (asuka/sdk/store, files it keeps): Application Support.
	if os.Getenv("ASUKA_DATA") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			name := boot.Name
			if name == "" {
				name = filepath.Base(os.Args[0])
			}
			// Made by what writes there (asuka/sdk/store, asuka/sdk/sqlite), not here.
			os.Setenv("ASUKA_DATA", filepath.Join(home, "Library", "Application Support", name))
		}
	}
}

// restoreState gives the app's signals the values the last run saved,
// under asuka dev (ASUKA_STATE), and saves them when asuka dev stops the app
// to run the new code.
func restoreState() {
	path := os.Getenv("ASUKA_STATE")
	if path == "" {
		return
	}
	if err := devstate.Keep(path); err != nil {
		fmt.Fprintln(os.Stderr, "asuka: restoring state:", err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		theHost.Post(func() {
			saveState()
			os.Exit(0)
		})
	}()
}

func saveState() {
	path := os.Getenv("ASUKA_STATE")
	if path == "" {
		return
	}
	if err := devstate.Save(path); err != nil {
		fmt.Fprintln(os.Stderr, "asuka: saving state:", err)
	}
}

var _ = Load
