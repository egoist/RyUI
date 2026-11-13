//go:build windows

package windows

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/devstate"
)

func init() {
	// The app's data (asuka/sdk/store, files it keeps): Local AppData.
	if os.Getenv("ASUKA_DATA") == "" {
		if base, err := os.UserConfigDir(); err == nil {
			if d := os.Getenv("LOCALAPPDATA"); d != "" {
				base = d
			}
			name := boot.Name
			if name == "" {
				name = appName()
			}
			// Made by what writes there (asuka/sdk/store, asuka/sqlite, the
			// window's placement), not here.
			os.Setenv("ASUKA_DATA", filepath.Join(base, name))
		}
	}
}

// restoreState gives the app's signals the values the last run saved,
// under asuka dev (ASUKA_STATE), and saves them when asuka dev stops the app
// to run the new code. Windows can't send a process SIGINT, so asuka dev
// closes the app's standard input instead (ASUKA_STOP=stdin); Ctrl+C in
// its console works too.
func restoreState() {
	path := os.Getenv("ASUKA_STATE")
	if path == "" {
		return
	}
	if err := devstate.Keep(path); err != nil {
		fmt.Fprintln(os.Stderr, "asuka: restoring state:", err)
	}
	stop := make(chan struct{}, 2)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		stop <- struct{}{}
	}()
	if os.Getenv("ASUKA_STOP") == "stdin" {
		go func() {
			io.Copy(io.Discard, os.Stdin)
			stop <- struct{}{}
		}()
	}
	go func() {
		<-stop
		theHost.Post(func() {
			saveState()
			os.Exit(0)
		})
	}()
}

var saved bool

func saveState() {
	path := os.Getenv("ASUKA_STATE")
	if path == "" || saved {
		return
	}
	saved = true
	if err := devstate.Save(path); err != nil {
		fmt.Fprintln(os.Stderr, "asuka: saving state:", err)
	}
}
