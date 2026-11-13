//go:build linux && !android

package linux

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/egoist/ryui/internal/devstate"
)

// Under ryui dev (RYUI_STATE), the app's signals get the values the last
// run saved, and they're saved when ryui dev stops the app (SIGINT) to
// run the new code, as on macOS. Kept from init: before the app's
// components make their signals.
func init() {
	path := os.Getenv("RYUI_STATE")
	if path == "" {
		return
	}
	if err := devstate.Keep(path); err != nil {
		fmt.Fprintln(os.Stderr, "ryui: restoring state:", err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		h := theHost
		if h == nil || h.app == 0 {
			os.Exit(0)
		}
		h.Post(func() {
			if err := devstate.Save(path); err != nil {
				fmt.Fprintln(os.Stderr, "ryui: saving state:", err)
			}
			os.Exit(0)
		})
	}()
}
