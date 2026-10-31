// Package app starts a RyUI app.
//
//	func main() { app.Run(Counter) }
//
// On iOS and Android the platform host starts the app and calls main;
// Run mounts the root component and returns.
package app

import (
	"fmt"
	"os"

	"github.com/egoist/ryui/internal/boot"
	"github.com/egoist/ryui/internal/remote"
	"github.com/egoist/ryui/ui"
)

// Run mounts root as the app's screen. The root fills the screen; use
// ui.SafeArea to keep content clear of the notch, the home indicator and
// the keyboard.
func Run(root func() ui.Node) {
	sheet := ui.ShareSheetRoot(shareScreen)
	// A share sheet's process (an iOS share extension, Android's share
	// activity): the sheet is the root.
	if os.Getenv("RYUI_SHARE_SHEET") == "1" {
		root = sheet
	}
	if boot.Runner != nil {
		boot.Runner(root)
		return
	}
	// `ryui dev` runs the app here and its views in a player app.
	if addr := os.Getenv("RYUI_REMOTE"); addr != "" {
		if err := remote.Run(addr, root, sheet); err != nil {
			fmt.Fprintln(os.Stderr, "ryui:", err)
			os.Exit(1)
		}
		return
	}
	// A desktop host runs the app in this process.
	if desktop != nil {
		desktop(root)
		return
	}
	panic("ryui: no host on this platform yet; run the app with `ryui dev -ios` (or -android)")
}

var shareScreen func(*ui.ShareSheet) ui.Node

// ShareSheet shows screen when another app shares with this one, over
// that app, instead of opening this one: the app's own sheet, as "Save
// to Raindrop" or "Add to Notes". Call it before Run. ryui.json's
// shareTarget says what the app takes; screen gets it in s.Items, and
// ends the sheet with s.Close, s.Cancel or s.OpenApp.
//
// It runs in a process of its own, with a root of its own: on iOS the
// app's share extension, which iOS shows as a sheet; on Android an
// activity shown as a bottom sheet over the other app. What the app
// keeps (ryui/store, sqlite in store.Dir, ryui/secure) is the same
// there as in the app.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui).
//
//go:noinline
func ShareSheet(screen func(s *ui.ShareSheet) ui.Node) { shareScreen = screen }

// desktop runs the app on a desktop's own host (macOS, Windows).
var desktop func(root func() ui.Node)

// RunWindow is Run with the main window's options: its title and size on
// a desktop (macOS, Windows). A phone's app fills the screen and ignores
// them.
func RunWindow(o ui.WindowOptions, root func() ui.Node) {
	ui.SetWindowOptions(o)
	Run(root)
}

// State is the app's lifecycle state.
type State int

const (
	Foreground State = iota
	Background
	MemoryWarning
)

// OnLifecycle calls f when the app moves to the foreground or background,
// or the system is low on memory. Main thread.
func OnLifecycle(f func(State)) {
	boot.Lifecycle = append(boot.Lifecycle, func(s int) { f(State(s)) })
}
