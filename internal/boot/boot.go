// Package boot connects app, ui and the platform hosts without import
// cycles.
package boot

// Runner, set by a platform host (mobile/), mounts the app's root
// component (a func() ui.Node).
var Runner func(root any)

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
