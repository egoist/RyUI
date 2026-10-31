// Package record records sound from the microphone (expo-audio's
// recorder): a voice note, a podcast, speech for a transcription
// service. The file is AAC in an MPEG-4 container (.m4a), which
// ryui/audio plays. An app that imports it is given the microphone's
// permission and purpose string (ryui.json's
// ios.usage.NSMicrophoneUsageDescription says it better).
//
// It records while the app is in the foreground; on iOS the audio
// session is set to record and play while it does (other apps' sound
// stops), and goes back after.
package record

import (
	"encoding/binary"
	"time"

	"github.com/egoist/ryui/ui"
)

// Quality is how the sound is kept.
type Quality uint8

const (
	// Voice is speech: mono, 22.05 kHz, 64 kbit/s (about 0.5 MB a minute).
	Voice Quality = iota
	// Music is music and ambience: stereo where the microphone is, 44.1
	// kHz, 128 kbit/s (about 1 MB a minute).
	Music
)

// Options configures a recording.
type Options struct {
	Quality Quality
	// MaxDuration stops the recording after it, if set.
	MaxDuration time.Duration
}

// Start records from the microphone until Stop (or MaxDuration), then
// calls done on the main thread with the recording's file (in the app's
// caches: move it to keep it). It asks for the microphone the first time;
// refused, done gets the error "not allowed". One at a time. Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Start(o Options, done func(path string, err error)) {
	var args [5]byte
	args[0] = byte(o.Quality)
	binary.LittleEndian.PutUint32(args[1:], uint32(o.MaxDuration/time.Millisecond))
	ui.Call("audioRecord", "start", args[:], func(b []byte, err error) { done(string(b), err) })
}

// Stop ends the recording; Start's done gets its file. Main thread.
func Stop() { ui.Call("audioRecord", "stop", nil, nil) }

// Pause pauses the recording; Resume goes on with it, in the same file.
// Main thread.
func Pause() { ui.Call("audioRecord", "pause", nil, nil) }

// Resume goes on with a paused recording. Main thread.
func Resume() { ui.Call("audioRecord", "resume", nil, nil) }

// Level is the sound's level now, from 0 (silence) to 1 (full scale), for
// a meter: call it from a goroutine, a few times a second, while
// recording. Not on the main thread.
func Level() (float64, error) {
	b, err := ui.CallWait("audioRecord", "level", nil)
	if err != nil || len(b) < 1 {
		return 0, err
	}
	return float64(b[0]) / 255, nil
}
