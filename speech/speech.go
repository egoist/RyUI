// Package speech reads text aloud with the platform's voices (expo-speech):
// AVSpeechSynthesizer on iOS, TextToSpeech on Android. Utterances queue:
// each Speak plays after the ones before it.
//
//	speech.Speak("Turn left in 200 meters", speech.Options{}, nil)
//
// On iOS other apps' sound ducks while it speaks, and it speaks with the
// silent switch on, as a navigation app's voice does.
package speech

import (
	"encoding/json"
	"errors"
	"strconv"
	"sync/atomic"

	"github.com/egoist/ryui/ui"
)

// Options are how an utterance sounds.
type Options struct {
	// Language is a BCP 47 tag ("en-US", "zh-CN"); default: the user's.
	Language string
	// Voice is a voice's ID (Voices); it wins over Language.
	Voice string
	// Pitch is from 0.5 to 2 (default 1).
	Pitch float64
	// Rate is the speed, from 0.5 (half) to 2 (twice); default 1, the
	// platform's normal speaking rate.
	Rate float64
	// Volume is from 0 to 1 (default 1).
	Volume float64
}

// ErrStopped: Stop ended the utterance before it was spoken through.
var ErrStopped = errors.New("speech: stopped")

var next atomic.Int64

// Speak queues text to be spoken; done (if not nil) is called on the main
// thread once it's been spoken (nil), or with ErrStopped, or why it
// couldn't. Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Speak(text string, o Options, done func(err error)) {
	b, _ := json.Marshal(map[string]any{
		"id": strconv.FormatInt(next.Add(1), 10), "text": text, "language": o.Language, "voice": o.Voice,
		"pitch": or(o.Pitch, 1), "rate": or(o.Rate, 1), "volume": or(o.Volume, 1),
	})
	ui.Call("speech", "speak", b, func(_ []byte, err error) {
		if err != nil && err.Error() == "stopped" {
			err = ErrStopped
		}
		if done != nil {
			done(err)
		}
	})
}

func or(v, def float64) float64 {
	if v <= 0 {
		return def
	}
	return v
}

// Stop stops speaking, and drops what's queued. Main thread.
func Stop() { ui.Call("speech", "stop", nil, nil) }

// Pause pauses speaking (at the word); Resume goes on. Main thread.
func Pause() { ui.Call("speech", "pause", nil, nil) }

// Resume goes on with paused speech. Main thread.
func Resume() { ui.Call("speech", "resume", nil, nil) }

// Speaking reports whether it's speaking (or paused mid-utterance). Not
// on the main thread.
func Speaking() bool {
	b, err := ui.CallWait("speech", "speaking", nil)
	return err == nil && len(b) > 0 && b[0] == 1
}

// Quality is a voice's.
type Quality uint8

const (
	Default  Quality = iota
	Enhanced         // larger, better sounding (downloaded in Settings)
	Premium          // the best the platform has
)

// Voice is one the device has.
type Voice struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Language string  `json:"language"` // BCP 47
	Quality  Quality `json:"quality"`
	// Network: it speaks through the network (Android's online voices).
	Network bool `json:"network"`
}

// Voices are the voices the device has. Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Voices() ([]Voice, error) {
	b, err := ui.CallWait("speech", "voices", nil)
	if err != nil {
		return nil, err
	}
	var vs []Voice
	err = json.Unmarshal(b, &vs)
	return vs, err
}
