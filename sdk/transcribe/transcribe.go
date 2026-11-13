// Package transcribe turns a recording into text, on the device: nothing
// leaves it. iOS's Speech framework, asked to stay on the device; Android's
// on-device recognizer (Android 13 and later). Where the device can't, it
// says so rather than sending the sound to a server.
//
//	go func() {
//		r, err := transcribe.File(path, transcribe.Options{})
//		ui.Post(func() { text.Set(r.Text) })
//	}()
//
// An app that imports it is given the permissions it needs: iOS's speech
// recognition, with its purpose string (asuka.json's
// ios.usage.NSSpeechRecognitionUsageDescription says it better), and on
// Android the microphone's, which its recognizer asks for.
package transcribe

import (
	"encoding/json"
	"errors"

	"github.com/egoist/asuka/ui"
)

var (
	// ErrNotAvailable says this device can't recognize this language on
	// the device: the simulator, an older phone, a language it hasn't.
	ErrNotAvailable = errors.New("transcribe: not available on this device")
	// ErrDenied says the user refused speech recognition.
	ErrDenied = errors.New("transcribe: not allowed")
	// ErrDownloading says the language's model is coming (Android): try
	// again later.
	ErrDownloading = errors.New("transcribe: the language's model is downloading")
)

// Options configures a transcription.
type Options struct {
	// Language is a BCP 47 tag ("en-US", "zh-Hans"); empty: the device's.
	Language string
}

// Segment is a word or a phrase, with where it is in the recording.
type Segment struct {
	Text string `json:"text"`
	// Start and Duration are in seconds.
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
}

// Result is what was said.
type Result struct {
	Text string `json:"text"`
	// Segments have the text's timing, where the platform gives it: iOS,
	// and Android 14 and later. Empty elsewhere.
	Segments []Segment `json:"segments"`
	// Language is the language it was recognized in.
	Language string `json:"language"`
}

// Availability says what the device can do.
type Availability struct {
	// OnDevice is true if it can transcribe this language here.
	OnDevice bool `json:"onDevice"`
	// Language is the language asked for, or the device's.
	Language string `json:"language"`
}

func fail(err error) error {
	if err == nil {
		return nil
	}
	switch err.Error() {
	case "not available":
		return ErrNotAvailable
	case "not allowed":
		return ErrDenied
	case "downloading":
		return ErrDownloading
	}
	return err
}

// Check says whether the device can transcribe a language, without asking
// the user anything. Not on the main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Check(language string) (Availability, error) {
	var a Availability
	b, err := ui.CallWait("transcribe", "check", []byte(language))
	if err != nil {
		return a, fail(err)
	}
	return a, json.Unmarshal(b, &a)
}

// File transcribes an audio file. It asks for what it needs the first
// time, and takes a while: a long recording, some of its own length. A
// recording with no speech in it is an empty Result. One at a time. Not on
// the main thread.
//
//go:noinline
func File(path string, o Options) (Result, error) {
	var r Result
	args, err := json.Marshal(map[string]string{"path": path, "language": o.Language})
	if err != nil {
		return r, err
	}
	b, err := ui.CallWait("transcribe", "file", args)
	if err != nil {
		return r, fail(err)
	}
	return r, json.Unmarshal(b, &r)
}
