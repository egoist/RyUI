//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"errors"
	"math"
	"unsafe"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/audio/record on a Mac: AVAudioRecorder, as on iOS. "start" (u8
// quality: voice, music; u32 max milliseconds, 0: none; u8 flags, 1
// background, 2 stream; u32-length title; u32-length path, empty for the
// app's caches) records until "stop" and answers with the file's path;
// "pause", "resume"; "level" answers u8, 0 to 255; "status" answers u8
// level, u8 recording, u32 milliseconds kept. A Mac has no audio session
// to set (other apps' sound goes on) and no interruptions, and an app
// keeps recording out of sight: the background flag and the title change
// nothing. macOS asks the user once, with the app's
// NSMicrophoneUsageDescription.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["audioRecord"] = recModule(h) })
}

var rec struct {
	recorder ID
	done     func([]byte, error)
	delegate ID // AsukaAudioRecorderDelegate
}

// kAudioFormatMPEG4AAC, 'aac '; AVAudioQualityHigh.
const (
	recFormatAAC   = 0x61616320
	recQualityHigh = 0x60
)

// recArgs is start's arguments.
type recArgs struct {
	quality     byte
	maxMillis   uint32
	flags       byte
	title, path string
}

func decodeRecArgs(b []byte) (a recArgs, ok bool) {
	if len(b) < 6 {
		return a, false
	}
	a.quality = b[0]
	a.maxMillis = binary.LittleEndian.Uint32(b[1:])
	a.flags = b[5]
	rest := b[6:]
	if len(rest) == 0 {
		return a, true
	}
	if len(rest) < 4 {
		return a, false
	}
	a.title, rest = readString(rest)
	if rest == nil {
		return a, false
	}
	if len(rest) > 0 {
		if len(rest) < 4 {
			return a, false
		}
		a.path, rest = readString(rest)
		if rest == nil {
			return a, false
		}
	}
	return a, true
}

func recModule(h *Host) map[string]method {
	return map[string]method{
		"start": func(args []byte, reply func([]byte, error)) {
			if rec.recorder != 0 {
				reply(nil, errors.New("already recording"))
				return
			}
			a, ok := decodeRecArgs(args)
			if !ok {
				reply(nil, errors.New("bad arguments"))
				return
			}
			if !cameraLoad() || !HasClass("AVAudioRecorder") {
				reply(nil, errors.New("couldn't record"))
				return
			}
			cameraAuthorize(h, cameraAudioMedia, func(ok bool) {
				if !ok {
					reply(nil, errNotAllowed)
					return
				}
				if rec.recorder != 0 {
					reply(nil, errors.New("already recording"))
					return
				}
				recStart(a, reply)
			})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			if rec.recorder != 0 {
				Send(rec.recorder, "stop") // the delegate answers start
			}
			reply(nil, nil)
		},
		"pause": func(_ []byte, reply func([]byte, error)) {
			if rec.recorder != 0 {
				Send(rec.recorder, "pause")
			}
			reply(nil, nil)
		},
		"resume": func(_ []byte, reply func([]byte, error)) {
			if rec.recorder != 0 {
				Send(rec.recorder, "record")
			}
			reply(nil, nil)
		},
		"level": func(_ []byte, reply func([]byte, error)) { reply([]byte{recLevel()}, nil) },
		"status": func(_ []byte, reply func([]byte, error)) {
			recording := rec.recorder != 0 && SendBool(rec.recorder, "isRecording")
			out := []byte{recLevel(), b2u(recording)}
			ms := 0.0
			if rec.recorder != 0 {
				ms = max(MsgFloat(rec.recorder, Sel("currentTime"))*1000, 0)
			}
			reply(binary.LittleEndian.AppendUint32(out, uint32(ms)), nil)
		},
	}
}

// recLevel is the sound's level now, 0 to 255.
func recLevel() byte {
	r := rec.recorder
	if r == 0 || !SendBool(r, "isRecording") {
		return 0
	}
	Send(r, "updateMeters")
	db := float64(msgAveragePower(r, Sel("averagePowerForChannel:"), 0)) // -160...0
	level := 0.0
	if db > -60 {
		level = math.Pow(10, db/20)
	}
	return byte(min(max(level, 0), 1) * 255)
}

func recStart(a recArgs, reply func([]byte, error)) {
	recDelegate()
	music := a.quality == 1
	rate, channels, bitRate := 22050.0, 1, 64_000
	if music {
		rate, channels, bitRate = 44100, 2, 128_000
	}
	settings := Dict(
		avConst("AVFormatIDKey"), NumberInt(recFormatAAC),
		avConst("AVSampleRateKey"), NumberFloat(rate),
		avConst("AVNumberOfChannelsKey"), NumberInt(channels),
		avConst("AVEncoderBitRateKey"), NumberInt(bitRate),
		avConst("AVEncoderAudioQualityKey"), NumberInt(recQualityHigh),
	)
	path := a.path
	if path == "" {
		// An .aac is ADTS, which AudioFile writes frame by frame.
		ext := ".m4a"
		if a.flags&2 != 0 {
			ext = ".aac"
		}
		var err error
		if path, err = cacheFile("asuka-audio", ext); err != nil {
			reply(nil, err)
			return
		}
	}
	var nsErr ID
	r := Send(Send(Class("AVAudioRecorder"), "alloc"), "initWithURL:settings:error:",
		uintptr(FileURL(path)), uintptr(settings), uintptr(unsafe.Pointer(&nsErr)))
	if r == 0 {
		reply(nil, errorText(nsErr, "couldn't record"))
		return
	}
	Send(r, "setDelegate:", uintptr(rec.delegate))
	Send(r, "setMeteringEnabled:", 1)
	var ok bool
	if a.maxMillis > 0 {
		ok = msgRecordFor(r, Sel("recordForDuration:"), float64(a.maxMillis)/1000)
	} else {
		ok = SendBool(r, "record")
	}
	if !ok {
		Release(r)
		reply(nil, errors.New("couldn't record"))
		return
	}
	rec.recorder = r
	rec.done = reply
}

// recDelegate makes the recorders' delegate, once.
func recDelegate() ID {
	if rec.delegate == 0 {
		DefineClass("AsukaAudioRecorderDelegate", "NSObject", []string{"AVAudioRecorderDelegate"}, []objc.MethodDef{
			Method("audioRecorderDidFinishRecording:successfully:", func(self ID, _ SEL, r ID, ok bool) {
				path := GoString(Send(Send(r, "url"), "path"))
				theHost.Post(func() { recFinished(r, path, ok, nil) })
			}),
			Method("audioRecorderEncodeErrorDidOccur:error:", func(self ID, _ SEL, r, err ID) {
				fail := errorText(err, "the recording failed")
				theHost.Post(func() { recFinished(r, "", false, fail) })
			}),
		})
		rec.delegate = New("AsukaAudioRecorderDelegate")
	}
	return rec.delegate
}

// recFinished is the recorder done: stopped, at its maximum, or failed.
func recFinished(r ID, path string, ok bool, fail error) {
	if r != rec.recorder {
		return
	}
	done := rec.done
	rec.done = nil
	rec.recorder = 0
	Send(r, "setDelegate:", 0)
	Release(r)
	if done == nil {
		return
	}
	if ok {
		done([]byte(path), nil)
		return
	}
	if fail == nil {
		fail = errors.New("the recording failed")
	}
	done(nil, fail)
}
