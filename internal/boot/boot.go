// Package boot connects app, ui and the platform hosts without import
// cycles.
package boot

import "sync"

// Runner, set by a platform host (mobile/), mounts the app's root
// component (a func() ui.Node).
var Runner func(root any)

// Remote, set by app/remote (which ryui dev links into the app it runs
// on this machine for a player), serves the app's root and share sheet
// (each a func() ui.Node) to ryui dev's players at addr.
var Remote func(addr string, root, sheet any) error

// Lifecycle handlers get app.State values.
var Lifecycle []func(state int)

// Accent is the app's accent color from ryui.json ("#rrggbb" or
// "#rrggbbaa"), and DynamicColor "1" when its Android colors are the
// wallpaper's: set by the RyUI CLI when it builds the app (-ldflags -X).
var Accent, DynamicColor string

// AssetBase, when set (a player running the app over ryui dev), is the URL
// the app's assets are served from: ui.Image loads asset paths from it.
var AssetBase string

// Name is the app's name from ryui.json, set by the RyUI CLI (-ldflags
// -X): a desktop host's window title and menus.
var Name string

// ID is ryui.json's app ID (com.example.app), set by the RyUI CLI
// (-ldflags -X) where the platform has no bundle to read it from (Linux:
// the application's ID, which names its desktop file and its D-Bus
// name).
var ID string

// Scheme is ryui.json's URL scheme (myapp), set by the RyUI CLI
// (-ldflags -X) where the platform has no bundle or desktop file to
// declare it in (Windows: the host registers it for the user as the app
// starts, and passes on the URLs it's opened with).
var Scheme string

// Version and Build are ryui.json's, set by the RyUI CLI (-ldflags -X)
// where the platform has no bundle to read them from (Windows).
var Version, Build string

// Lang is the language `ryui dev -lang` shows (a BCP 47 tag, a pseudo locale
// too), and Direction "rtl" or "ltr" what `ryui dev -dir` lays the app out
// in, whatever the language: set by the RyUI CLI for one run (-ldflags -X),
// and they keep nothing. The environment's RYUI_LOCALE and RYUI_DIRECTION
// do the same for an app run with go run.
var Lang, Direction string

// SaveLocale and SavedLocale, when set (the app's Go code running over ryui
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

// ShaderGLSL, set where the app links the shader compiler (package
// shader, on js/wasm), turns a ui.Shader's code into the GLSL ES 3.00
// fragment shader the web host draws it with.
var ShaderGLSL func(code []byte) (string, error)
