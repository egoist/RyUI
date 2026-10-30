// Package camera takes a photo with the system's camera (iOS's camera
// screen, Android's camera app) and gives the app the file. An app that
// imports it is given what it needs (iOS's camera purpose string, which
// nui.json's ios.usage.NSCameraUsageDescription says better).
package camera

import (
	"errors"

	"github.com/egoist/nui/ui"
)

// ErrNoCamera is the error on a device without one (the iOS simulator).
var ErrNoCamera = errors.New("camera: there's no camera")

// Take shows the camera and calls done, on the main thread, with the
// photo's file (a JPEG in the app's caches: move it to keep it), or ""
// if the user cancelled. Main thread.
func Take(done func(path string, err error)) {
	ui.Call("camera", "take", nil, func(b []byte, err error) {
		if err != nil && err.Error() == "no camera" {
			err = ErrNoCamera
		}
		done(string(b), err)
	})
}

// Facing is which camera a View shows.
type Facing int

const (
	Back Facing = iota
	Front
)

// ViewProps configures a View.
type ViewProps struct {
	Facing Facing
	Style  ui.Styler
	// OnError is called if the camera can't show (not allowed, none).
	OnError func(message string)
}

// View shows what the camera sees, live, filling its box (AVFoundation's
// preview; CameraX's PreviewView): for a capture screen of the app's
// own. It asks for the camera the first time. Capture takes a photo from
// it. One at a time.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func View(p ViewProps) ui.Node {
	facing := "back"
	if p.Facing == Front {
		facing = "front"
	}
	np := ui.NativeProps{Style: p.Style, Props: map[string]any{"facing": facing}}
	if p.OnError != nil {
		np.On = map[string]func(ui.NativeEvent){"error": func(e ui.NativeEvent) { p.OnError(e.String("message")) }}
	}
	return ui.Native("nui.camera", np)
}

// Capture takes a photo from the View on screen and calls done, on the
// main thread, with its file (a JPEG in the app's caches). Main thread.
func Capture(done func(path string, err error)) {
	ui.Call("camera", "capture", nil, func(b []byte, err error) { done(string(b), err) })
}
