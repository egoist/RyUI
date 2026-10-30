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
