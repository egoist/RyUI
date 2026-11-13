//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/sdk/transcribe on a Mac, with the Speech framework as on iOS: "check"
// (the language) answers JSON onDevice, language; "file" (JSON path,
// language) answers JSON text, segments, language. On the device only:
// it never falls back to Apple's servers. macOS asks the user once, with
// the app's NSSpeechRecognitionUsageDescription, which needs the app's
// bundle (asuka build --platform mac).

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["transcribe"] = h.transcribeModule()
	})
}

// SFSpeechRecognizerAuthorizationStatus.
const (
	trNotDetermined = 0
	trAuthorized    = 3
)

// trAuthorize asks the first time, then calls then with whether it's
// allowed, on the main thread. A test replaces it: an app without a
// purpose string is ended by macOS for asking.
var trAuthorize = func(h *Host, then func(bool)) {
	c := Class("SFSpeechRecognizer")
	if s := SendInt(c, "authorizationStatus"); s != trNotDetermined {
		then(s == trAuthorized)
		return
	}
	var done objc.Block
	done = NewBlock(func(_ objc.Block, status int) {
		h.Post(func() {
			done.Release()
			then(status == trAuthorized)
		})
	})
	Send(c, "requestAuthorization:", uintptr(done))
}

// trRunning is the transcription under way (one at a time).
var trRunning bool

func trLoad() bool { return framework("Speech") != 0 && HasClass("SFSpeechRecognizer") }

// trRecognizer is the language's recognizer (+1), or the user's; 0 if
// there's none for it.
func trRecognizer(language string) ID {
	if language == "" {
		return New("SFSpeechRecognizer")
	}
	loc := Send(Class("NSLocale"), "localeWithLocaleIdentifier:", uintptr(String(language)))
	return Send(Send(Class("SFSpeechRecognizer"), "alloc"), "initWithLocale:", uintptr(loc))
}

type trSegment struct {
	Text     string  `json:"text"`
	Start    float64 `json:"start"`
	Duration float64 `json:"duration"`
}

type trTranscription struct {
	text     string
	segments []trSegment
}

func (h *Host) transcribeModule() map[string]method {
	return map[string]method{
		"check": func(args []byte, reply func([]byte, error)) {
			out := map[string]any{"onDevice": false, "language": ""}
			if trLoad() {
				if r := trRecognizer(string(args)); r != 0 {
					out["onDevice"] = SendBool(r, "supportsOnDeviceRecognition")
					out["language"] = GoString(Send(Send(r, "locale"), "localeIdentifier"))
					Release(r)
				}
			}
			b, err := json.Marshal(out)
			reply(b, err)
		},
		"file": func(args []byte, reply func([]byte, error)) {
			var o struct{ Path, Language string }
			json.Unmarshal(args, &o)
			if !trLoad() {
				reply(nil, errors.New("not available"))
				return
			}
			trAuthorize(h, func(ok bool) {
				if !ok {
					reply(nil, errNotAllowed)
					return
				}
				h.transcribe(o.Path, o.Language, reply)
			})
		},
	}
}

func (h *Host) transcribe(path, language string, reply func([]byte, error)) {
	if trRunning {
		reply(nil, errors.New("already transcribing"))
		return
	}
	rec := trRecognizer(language)
	if rec == 0 || !SendBool(rec, "supportsOnDeviceRecognition") {
		Release(rec)
		reply(nil, errors.New("not available"))
		return
	}
	req := Send(Send(Class("SFSpeechURLRecognitionRequest"), "alloc"), "initWithURL:", uintptr(FileURL(path)))
	Send(req, "setRequiresOnDeviceRecognition:", 1)
	if RespondsTo(req, "setAddsPunctuation:") {
		Send(req, "setAddsPunctuation:", 1)
	}
	Send(req, "setShouldReportPartialResults:", 0)
	locale := GoString(Send(Send(rec, "locale"), "localeIdentifier"))
	trRunning = true
	// What's been recognized. A long file can come in several results,
	// each on from the last, or a result can replace the one before.
	var done []trTranscription
	add := func(t trTranscription) {
		if n := len(done); n > 0 {
			last := done[n-1].segments
			if len(last) > 0 && len(t.segments) > 0 && t.segments[0].Start >= last[len(last)-1].Start+last[len(last)-1].Duration-0.01 {
				done = append(done, t)
				return
			}
			done = done[:n-1]
		}
		done = append(done, t)
	}
	finished := false
	var handler objc.Block
	handler = NewBlock(func(_ objc.Block, result, nserr ID) {
		var t *trTranscription
		final := false
		if result != 0 {
			best := Send(result, "bestTranscription")
			t = &trTranscription{text: GoString(Send(best, "formattedString")), segments: []trSegment{}}
			for _, s := range ArrayItems(Send(best, "segments")) {
				t.segments = append(t.segments, trSegment{Text: GoString(Send(s, "substring")),
					Start: MsgFloat(s, Sel("timestamp")), Duration: MsgFloat(s, Sel("duration"))})
			}
			final = SendBool(result, "isFinal")
		}
		var domain string
		var code int
		var failure error
		if nserr != 0 {
			domain, code = GoString(Send(nserr, "domain")), SendInt(nserr, "code")
			failure = errorText(nserr, "")
		}
		h.Post(func() {
			if finished {
				return
			}
			if t != nil {
				add(*t)
			}
			if failure == nil && !final {
				return
			}
			finished = true
			trRunning = false
			handler.Release()
			Release(req)
			Release(rec)
			hasText := false
			var texts []string
			segments := []trSegment{}
			for _, d := range done {
				if d.text != "" {
					hasText = true
					texts = append(texts, d.text)
				}
				segments = append(segments, d.segments...)
			}
			// No speech in it (kAFAssistantErrorDomain 1110, or 203) is an
			// empty text.
			silence := domain == "kAFAssistantErrorDomain" && (code == 1110 || code == 203)
			if failure != nil && !silence && !hasText {
				// The recognizer can't start here (kLSRErrorDomain): its
				// models aren't on this Mac (yet).
				if domain == "kLSRErrorDomain" {
					failure = errors.New("not available")
				}
				reply(nil, failure)
				return
			}
			b, err := json.Marshal(map[string]any{"text": strings.Join(texts, " "), "language": locale, "segments": segments})
			reply(b, err)
		})
	})
	Send(rec, "recognitionTaskWithRequest:resultHandler:", uintptr(req), uintptr(handler))
}
