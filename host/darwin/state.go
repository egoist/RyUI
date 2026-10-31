//go:build darwin && !ios

package darwin

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/egoist/ryui/internal/boot"
	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/internal/remote"
)

func init() {
	// The app's data (ryui/store, files it keeps): Application Support.
	if os.Getenv("RYUI_DATA") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			name := boot.Name
			if name == "" {
				name = filepath.Base(os.Args[0])
			}
			// Made by what writes there (ryui/store, ryui/sqlite), not here.
			os.Setenv("RYUI_DATA", filepath.Join(home, "Library", "Application Support", name))
		}
	}
}

// restoreState gives the app's signals the values the last run saved,
// under ryui dev (RYUI_STATE), and saves them when ryui dev stops the app
// to run the new code.
func restoreState() {
	path := os.Getenv("RYUI_STATE")
	if path == "" {
		return
	}
	if err := remote.KeepState(path); err != nil {
		fmt.Fprintln(os.Stderr, "ryui: restoring state:", err)
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
	path := os.Getenv("RYUI_STATE")
	if path == "" {
		return
	}
	if err := remote.SaveState(path); err != nil {
		fmt.Fprintln(os.Stderr, "ryui: saving state:", err)
	}
}

var _ = Load
