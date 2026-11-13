// Updater is e2e/updater_test.go's app: built at two versions, the first
// updates itself to the second and relaunches into it. With --report
// <file>, each launch writes its version there; the first checks, installs
// and relaunches with update's API, the second quits. Without it, it's an
// app with the update window and "Check for Updates…" in its menu
// (--check: the window at once).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/sdk/update"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func main() {
	app.Run(func() ui.Node {
		if len(os.Args) == 3 && os.Args[1] == "--report" {
			// Once the app runs: Quit and Relaunch need its host.
			signal.Effect(func() { go selfUpdate(os.Args[2]) })
		}
		if len(os.Args) == 2 && os.Args[1] == "--check" {
			// The update window, as "Check for Updates…" opens it.
			signal.Effect(update.CheckForUpdates)
		}
		return ui.Column(ui.Style{Padding: ui.All(24), Gap: 12},
			update.Window(update.Options{}),
			ui.MenuBar(signal.Static([]ui.MenuBarMenu{update.AppMenu()})),
			ui.StaticText("Version "+update.Version()),
			ui.Button(ui.ButtonProps{Title: ui.Val("Check for Updates…"), OnPress: update.CheckForUpdates}),
		)
	})
}

// selfUpdate reports this launch's version, then updates the app and
// relaunches it, or quits once it's the new version.
func selfUpdate(file string) {
	report := func(format string, args ...any) {
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprintf(f, format+"\n", args...)
			f.Close()
		}
	}
	report("launched %s", update.Version())
	r, err := update.Check(context.Background())
	switch {
	case err != nil:
		report("check failed: %v", err)
		app.Quit()
	case r == nil:
		report("up to date")
		app.Quit()
	default:
		report("found %s (%d bytes)", r.Version, r.Size)
		if err := r.Install(context.Background(), nil); err != nil {
			report("install failed: %v", err)
			app.Quit()
			return
		}
		report("installed %s", r.Version)
		update.Relaunch()
	}
}
