//go:build linux && !android

package linux

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/egoist/asuka/internal/devstate"
	"github.com/egoist/asuka/ops"
)

// Under asuka dev (ASUKA_STATE), the app's signals get the values the last
// run saved, and they're saved when asuka dev stops the app (closing its
// control connection, or SIGINT) to run the new code, as on macOS. Kept
// from init: before the app's components make their signals. The run
// starts while the one before it still runs, and waits here, before GTK
// starts, until that one has stopped (devstate.Wait).
func init() {
	path := os.Getenv("ASUKA_STATE")
	if path == "" {
		return
	}
	stopped := devstate.Wait()
	if err := devstate.Keep(path); err != nil {
		fmt.Fprintln(os.Stderr, "asuka: restoring state:", err)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-stop:
		case <-stopped:
		}
		h := theHost
		if h == nil || h.app == 0 {
			os.Exit(0)
		}
		h.Post(func() {
			if err := devstate.Save(path); err != nil {
				fmt.Fprintln(os.Stderr, "asuka: saving state:", err)
			}
			h.lifecycle(ops.LifecycleBackground) // the store written, as quitting does
			os.Exit(0)
		})
	}()
}
