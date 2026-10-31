//go:build darwin && !ios

package darwin

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/egoist/nui/internal/boot"
	. "github.com/egoist/nui/internal/darwin"
	"github.com/egoist/nui/internal/remote"
)

func init() {
	// The app's data (nui/store, files it keeps): Application Support.
	if os.Getenv("NUI_DATA") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			name := boot.Name
			if name == "" {
				name = filepath.Base(os.Args[0])
			}
			// Made by what writes there (nui/store, nui/sqlite), not here.
			os.Setenv("NUI_DATA", filepath.Join(home, "Library", "Application Support", name))
		}
	}
}

// restoreState gives the app's signals the values the last run saved,
// under nui dev (NUI_STATE), and saves them when nui dev stops the app
// to run the new code.
func restoreState() {
	path := os.Getenv("NUI_STATE")
	if path == "" {
		return
	}
	if err := remote.KeepState(path); err != nil {
		fmt.Fprintln(os.Stderr, "nui: restoring state:", err)
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
	path := os.Getenv("NUI_STATE")
	if path == "" {
		return
	}
	if err := remote.SaveState(path); err != nil {
		fmt.Fprintln(os.Stderr, "nui: saving state:", err)
	}
}

var _ = Load
