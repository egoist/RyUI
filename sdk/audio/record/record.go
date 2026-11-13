// Package record records sound from the microphone (expo-audio's
// recorder): a voice note, a podcast, speech for a transcription
// service. The file is AAC in an MPEG-4 container (.m4a), which
// asuka/sdk/audio plays. An app that imports it is given the microphone's
// permission and purpose string (asuka.json's
// ios.usage.NSMicrophoneUsageDescription says it better).
//
// It records while the app is in the foreground, and with Options's
// Background out of sight too. On iOS the audio session is set to record
// and play while it does (other apps' sound stops), and goes back after.
package record

import (
	"encoding/binary"
	"time"

	"github.com/egoist/asuka/ui"
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
	// Background keeps recording when the app goes out of sight or the
	// screen locks. On iOS the app needs "audio" in asuka.json's
	// ios.backgroundModes, and the system shows the microphone's indicator.
	// On Android it's a foreground service of the microphone type, with an
	// ongoing notification saying BackgroundTitle and a button to stop.
	Background bool
	// BackgroundTitle is that notification's title (default: "Recording").
	BackgroundTitle string
	// Path is where to record (default: a new file in the app's caches). Use
	// it with Stream to find the file again after a crash.
	Path string
	// Stream records AAC as a stream of frames (ADTS, .aac), each whole in
	// itself, so what's been recorded survives the app or the phone dying
	// mid-recording, where an MPEG-4 file is unreadable until it's closed. It
	// seeks badly: Finalize makes it an .m4a for keeping, after a Stop or a
	// crash alike.
	Stream bool
}

// Start records from the microphone until Stop (or MaxDuration), then
// calls done on the main thread with the recording's file (in the app's
// caches: move it to keep it). It asks for the microphone the first time;
// refused, done gets the error "not allowed". One at a time. Main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Start(o Options, done func(path string, err error)) {
	args := make([]byte, 6, 14+len(o.BackgroundTitle)+len(o.Path))
	args[0] = byte(o.Quality)
	binary.LittleEndian.PutUint32(args[1:], uint32(o.MaxDuration/time.Millisecond))
	if o.Background {
		args[5] |= 1
	}
	if o.Stream {
		args[5] |= 2
	}
	args = binary.LittleEndian.AppendUint32(args, uint32(len(o.BackgroundTitle)))
	args = append(args, o.BackgroundTitle...)
	args = binary.LittleEndian.AppendUint32(args, uint32(len(o.Path)))
	args = append(args, o.Path...)
	ui.Call("audioRecord", "start", args, func(b []byte, err error) { done(string(b), err) })
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

// Status is the recording now, as the platform has it.
type Status struct {
	// Level is the sound's level, from 0 (silence) to 1 (full scale).
	Level float64
	// Recording is true while sound is being kept: false when paused, by
	// Pause or the system (a phone call), or when there's no recording.
	Recording bool
	// Elapsed is how much has been kept, pauses left out.
	Elapsed time.Duration
}

// CurrentStatus gets the recording's status and calls done with it on the
// main thread: read it a few times a second to draw a meter and a timer.
// Main thread.
func CurrentStatus(done func(Status)) {
	ui.Call("audioRecord", "status", nil, func(b []byte, err error) {
		var s Status
		if err == nil && len(b) >= 6 {
			s.Level = float64(b[0]) / 255
			s.Recording = b[1] == 1
			s.Elapsed = time.Duration(binary.LittleEndian.Uint32(b[2:])) * time.Millisecond
		}
		done(s)
	})
}
