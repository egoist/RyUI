// Package record records video from the camera.View on screen (its Mode
// camera.Video), with sound unless muted. An app that imports it is given
// the microphone's permission and purpose string (ryui.json's
// ios.usage.NSMicrophoneUsageDescription says it better); apps that only
// take photos go without.
package record

import (
	"encoding/binary"
	"time"

	"github.com/egoist/ryui/ui"
)

// Options configures a recording.
type Options struct {
	// Mute records without sound (and without asking for the microphone).
	Mute bool
	// MaxDuration stops the recording after it, if set.
	MaxDuration time.Duration
}

// Start records the camera.View on screen until Stop (or MaxDuration),
// then calls done on the main thread with the video's file (in the app's
// caches: move it to keep it). It asks for the microphone the first time,
// unless muted; refused, the video has no sound. One at a time. Main
// thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Start(o Options, done func(path string, err error)) {
	var args [5]byte
	if o.Mute {
		args[0] = 1
	}
	binary.LittleEndian.PutUint32(args[1:], uint32(o.MaxDuration/time.Millisecond))
	ui.Call("camera", "record", args[:], func(b []byte, err error) { done(string(b), err) })
}

// Stop ends the recording; Start's done gets its file. Main thread.
func Stop() {
	ui.Call("camera", "stopRecording", nil, func([]byte, error) {})
}
