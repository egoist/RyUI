// Package boot connects app, ui and the platform hosts without import
// cycles.
package boot

// Runner, set by a platform host (mobile/), mounts the app's root
// component (a func() ui.Node).
var Runner func(root any)

// Lifecycle handlers get app.State values.
var Lifecycle []func(state int)

// AssetBase, when set (a player running the app over nui dev), is the URL
// the app's assets are served from: ui.Image loads asset paths from it.
var AssetBase string
