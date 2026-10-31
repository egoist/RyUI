//go:build windows && amd64

package windows

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/egoist/nui/internal/boot"
	"github.com/egoist/nui/internal/remote"
)

func init() {
	// The app's data (nui/store, files it keeps): Local AppData.
	if os.Getenv("NUI_DATA") == "" {
		if base, err := os.UserConfigDir(); err == nil {
			if d := os.Getenv("LOCALAPPDATA"); d != "" {
				base = d
			}
			name := boot.Name
			if name == "" {
				name = appName()
			}
			d := filepath.Join(base, name)
			if os.MkdirAll(d, 0o755) == nil {
				os.Setenv("NUI_DATA", d)
			}
		}
	}
}

// restoreState gives the app's signals the values the last run saved,
// under nui dev (NUI_STATE), and saves them when nui dev stops the app
// to run the new code. Windows can't send a process SIGINT, so nui dev
// closes the app's standard input instead (NUI_STOP=stdin); Ctrl+C in
// its console works too.
func restoreState() {
	path := os.Getenv("NUI_STATE")
	if path == "" {
		return
	}
	if err := remote.KeepState(path); err != nil {
		fmt.Fprintln(os.Stderr, "nui: restoring state:", err)
	}
	stop := make(chan struct{}, 2)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() {
		<-sig
		stop <- struct{}{}
	}()
	if os.Getenv("NUI_STOP") == "stdin" {
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
	path := os.Getenv("NUI_STATE")
	if path == "" || saved {
		return
	}
	saved = true
	if err := remote.SaveState(path); err != nil {
		fmt.Fprintln(os.Stderr, "nui: saving state:", err)
	}
}
