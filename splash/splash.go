// Package splash keeps the launch screen up while the app gets ready
// (expo-splash-screen): ryui.json's splash (an image in the middle of a
// color) shows from the tap on the icon until the app's first frame, or,
// if the app holds it as it starts, until it hides it: once its data is
// loaded, its fonts in, the signed-in user known.
//
//	"splash": {"image": "logo.png", "size": 120, "background": "#ffffff", "darkBackground": "#000000"}
//
// iOS: the launch screen (UILaunchScreen), then a copy of it over the app.
// Android: Android 12's splash screen (the image in the middle of the
// icon's circle); before, the window's background.
package splash

import "github.com/egoist/ryui/ui"

// Hold keeps the splash up after the first frame, until Hide: call it as
// the app starts (in its root component), before the first frame. Main
// thread.
func Hold() {
	ui.Call("splash", "hold", nil, nil)
}

// Hide takes the splash away (a short fade). Main thread.
func Hide() {
	ui.Call("splash", "hide", nil, nil)
}
