// Package boot connects app, ui and the platform hosts without import
// cycles.
package boot

import (
	"os"
	"slices"
	"sync"
)

// Runner, set by a platform host (mobile/), mounts the app's root
// component (a func() ui.Node).
var Runner func(root any)

// Remote, set by app/remote (which asuka dev links into the app it runs
// on this machine for a player), serves the app's root and share sheet
// (each a func() ui.Node) to asuka dev's players at addr.
var Remote func(addr string, root, sheet any) error

// Lifecycle handlers get app.State values.
var Lifecycle []func(state int)

// Quit, set by a desktop host as it starts, quits the app as the user's
// Quit does (⌘Q, closing the main window): QuitRequested first, which
// may cancel it. Safe from any goroutine. Nil where an app doesn't quit
// itself (a phone's, a browser's page).
var Quit func()

// QuitRequested, set by package app, runs app.OnQuit's handlers as the
// app is about to quit, on the main thread; ending says the session is
// ending (logout, shutdown), when the quit can't be canceled. It reports
// whether a handler canceled the quit (never while ending). A host asks
// it before it quits, however that started, and asks once: a quit
// already allowed only finishes.
var QuitRequested func(ending bool) (cancel bool)

// SecondInstance, set by package app, gets another launch of the app in
// the instance that runs (desktop: the app runs once): its arguments
// (os.Args[1:]) and working directory, on the main thread. The host has
// brought the app to the front, and passes the files and URLs among the
// arguments on to ui.OnOpenFiles and ui.OnOpenURL.
var SecondInstance func(args []string, dir string)

// MultipleInstances is "1" when asuka.json's "singleInstance" is false,
// set by the Asuka CLI (-ldflags -X): on Windows and Linux the app then
// runs as often as it's started, each launch its own (no
// OnSecondInstance).
var MultipleInstances string

// LoginArg is the argument a login entry starts the app with on
// Windows and Linux (asuka/login), so that it knows it was opened at
// login: the host takes it out of os.Args before main reads them.
const LoginArg = "--asuka-opened-at-login"

// OpenedAtLogin: the app was started with LoginArg (TakeLoginArg).
var OpenedAtLogin bool

// TakeLoginArg takes LoginArg out of os.Args, and remembers it
// (OpenedAtLogin): a host's init calls it, before main reads them.
func TakeLoginArg() {
	if i := slices.Index(os.Args, LoginArg); i > 0 {
		os.Args = slices.Delete(os.Args, i, i+1)
		OpenedAtLogin = true
	}
}

// Accent is the app's accent color from asuka.json ("#rrggbb" or
// "#rrggbbaa"), and DynamicColor "1" when its Android colors are the
// wallpaper's: set by the Asuka CLI when it builds the app (-ldflags -X).
var Accent, DynamicColor string

// AssetBase, when set (a player running the app over asuka dev), is the URL
// the app's assets are served from: ui.Image loads asset paths from it.
var AssetBase string

// Name is the app's name from asuka.json, set by the Asuka CLI (-ldflags
// -X): a desktop host's window title and menus.
var Name string

// ID is asuka.json's app ID (com.example.app), set by the Asuka CLI
// (-ldflags -X) where the platform has no bundle to read it from (Linux:
// the application's ID, which names its desktop file and its D-Bus
// name).
var ID string

// Scheme is asuka.json's URL scheme (myapp), set by the Asuka CLI
// (-ldflags -X) where the platform has no bundle or desktop file to
// declare it in (Windows: the host registers it for the user as the app
// starts, and passes on the URLs it's opened with).
var Scheme string

// FileTypes are asuka.json's fileTypes (internal/filetype's encoding),
// set by the Asuka CLI (-ldflags -X) where the platform has no bundle or
// desktop file to declare them in (Windows: the host registers them for
// the user as the app starts).
var FileTypes string

// Version and Build are asuka.json's, set by the Asuka CLI (-ldflags -X)
// where the platform has no bundle to read them from (Windows).
var Version, Build string

// Lang is the language `asuka dev --lang` shows (a BCP 47 tag, a pseudo locale
// too), and Direction "rtl" or "ltr" what `asuka dev --dir` lays the app out
// in, whatever the language: set by the Asuka CLI for one run (-ldflags -X),
// and they keep nothing. The environment's ASUKA_LOCALE and ASUKA_DIRECTION
// do the same for an app run with go run.
var Lang, Direction string

// SaveLocale and SavedLocale, when set (the app's Go code running over asuka
// dev), keep the language ui.SetLocale chose on this machine across
// reloads, instead of in the player, which runs every app.
var (
	SaveLocale  func(tag string)
	SavedLocale func() string
)

// afterMount are what waits for the first root (AfterMount).
var (
	mountMu    sync.Mutex
	mounted    bool
	afterMount []func()
)

// AfterMount runs f once a root is mounted, when ui.Post and ui.Call
// work: now, if one is. A package's setting made in main, before app.Run
// (push.Rewrite), starts its work with it.
func AfterMount(f func()) {
	mountMu.Lock()
	if !mounted {
		afterMount = append(afterMount, f)
		mountMu.Unlock()
		return
	}
	mountMu.Unlock()
	f()
}

// Mounted, from ui.Mount, runs what waits for it.
func Mounted() {
	mountMu.Lock()
	mounted = true
	fs := afterMount
	afterMount = nil
	mountMu.Unlock()
	for _, f := range fs {
		f()
	}
}

// Background is "1" for a desktop app that runs in the background
// (app.RunInBackground, or asuka.json's "background", set by the Asuka CLI
// with -ldflags -X): no window shows at launch, and on macOS it has no
// Dock icon (an accessory app). It keeps running with no window, as
// KeepRunning.
var Background string

// KeepRunning keeps a desktop app running when its last window closes
// (app.KeepRunning): closing the main window hides it.
var KeepRunning bool

// Reopen, when set (asuka/document), is asked when the user opens the
// app again while it has no window showing (a Mac's Dock icon clicked,
// applicationShouldHandleReopen:): true says it took care of it (made a
// document's window), and the host shows nothing of its own; false, or
// unset, shows the main window as before. Main thread.
var Reopen func() bool

// InBackground reports whether the app runs in the background
// (Background).
func InBackground() bool { return Background == "1" || Background == "true" }

// KeepsRunning reports whether the app keeps running with no window
// (KeepRunning, or InBackground).
func KeepsRunning() bool { return KeepRunning || InBackground() }

// ShaderGLSL, set where the app links the shader compiler (package
// shader, on js/wasm), turns a ui.Shader's code into the GLSL ES 3.00
// fragment shader the web host draws it with.
var ShaderGLSL func(code []byte) (string, error)
