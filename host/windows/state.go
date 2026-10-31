//go:build windows && amd64

package windows

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/egoist/ryui/internal/boot"
	"github.com/egoist/ryui/internal/remote"
)

func init() {
	// The app's data (ryui/store, files it keeps): Local AppData.
	if os.Getenv("RYUI_DATA") == "" {
		if base, err := os.UserConfigDir(); err == nil {
			if d := os.Getenv("LOCALAPPDATA"); d != "" {
				base = d
			}
			name := boot.Name
			if name == "" {
				name = appName()
			}
			// Made by what writes there (ryui/store, ryui/sqlite, the
			// window's placement), not here.
			os.Setenv("RYUI_DATA", filepath.Join(base, name))
		}
	}
}

// restoreState gives the app's signals the values the last run saved,
// under ryui dev (RYUI_STATE), and saves them when ryui dev stops the app
// to run the new code. Windows can't send a process SIGINT, so ryui dev
// closes the app's standard input instead (RYUI_STOP=stdin); Ctrl+C in
// its console works too.
func restoreState() {
	path := os.Getenv("RYUI_STATE")
	if path == "" {
		return
	}
	if err := remote.KeepState(path); err != nil {
		fmt.Fprintln(os.Stderr, "ryui: restoring state:", err)
	}
	stop := make(chan struct{}, 2)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		stop <- struct{}{}
	}()
	if os.Getenv("RYUI_STOP") == "stdin" {
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
	path := os.Getenv("RYUI_STATE")
	if path == "" || saved {
		return
	}
	saved = true
	if err := remote.SaveState(path); err != nil {
		fmt.Fprintln(os.Stderr, "ryui: saving state:", err)
	}
}
