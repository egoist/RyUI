// Package app starts a nui app.
//
//	func main() { app.Run(Counter) }
//
// On iOS and Android the platform host starts the app and calls main;
// Run mounts the root component and returns.
package app

import (
	"fmt"
	"os"

	"github.com/egoist/nui/internal/boot"
	"github.com/egoist/nui/internal/remote"
	"github.com/egoist/nui/ui"
)

// Run mounts root as the app's screen. The root fills the screen; use
// ui.SafeArea to keep content clear of the notch, the home indicator and
// the keyboard.
func Run(root func() ui.Node) {
	if boot.Runner != nil {
		boot.Runner(root)
		return
	}
	// `nui dev` runs the app here and its views in a player app.
	if addr := os.Getenv("NUI_REMOTE"); addr != "" {
		if err := remote.Run(addr, root); err != nil {
			fmt.Fprintln(os.Stderr, "nui:", err)
			os.Exit(1)
		}
		return
	}
	// A desktop host runs the app in this process.
	if desktop != nil {
		desktop(root)
		return
	}
	panic("nui: no host on this platform yet; run the app with `nui dev -ios` (or -android)")
}

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
