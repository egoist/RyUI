//go:build windows

package windows

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"sync/atomic"

	. "github.com/egoist/ryui/internal/windows"
)

// ryui/audio/record on Windows: the default microphone (waveIn) encoded
// to AAC by Media Foundation as it records, in an MPEG-4 file (.m4a) or,
// streaming, ADTS (.aac). "start" (u8 quality: voice, music; u32 max
// milliseconds, 0: none; u8 flags, 1 background, 2 stream; u32-length
// title; u32-length path, empty for the app's caches) records until
// "stop" and answers with the file's path; "pause", "resume"; "level"
// answers u8, 0 to 255; "status" answers u8 level, u8 recording, u32
// milliseconds kept. Windows's AAC encoder takes 44.1 kHz: voice is mono
// at 96 kbit/s, music stereo at 128. A Windows app keeps recording out of
// sight: the background flag and the title change nothing. Settings'
// microphone privacy switch refused: "not allowed".

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) { m["audioRecord"] = h.recModule() })
}

// pcmSource is where the sound comes from: the microphone (a test's
// tone instead).
type pcmSource interface {
	Start() error
	Stop()
	Wait(ms uint32)
	Read(f func(pcm []byte))
	Close(f func(pcm []byte))
}

var openMicrophone = func(channels, rate int) (pcmSource, error) {
	if CapabilityDenied("microphone") {
		return nil, errNotAllowed
	}
	return OpenWaveIn(channels, rate)
}

type recording struct {
	cmds      chan byte // 'p' pause, 'r' resume, 's' stop
	level     atomic.Uint32
	recording atomic.Bool
	ms        atomic.Uint32
}

var recNow *recording

func (h *Host) recModule() map[string]asyncMethod {
	cmd := func(c byte) asyncMethod {
		return func(_ []byte, reply func([]byte, error)) {
			if r := recNow; r != nil {
				select {
				case r.cmds <- c:
				default:
				}
			}
			reply(nil, nil)
		}
	}
	level := func() byte {
		if r := recNow; r != nil && r.recording.Load() {
			return byte(r.level.Load())
		}
		return 0
	}
	return map[string]asyncMethod{
		"start": func(args []byte, reply func([]byte, error)) {
			if recNow != nil {
				reply(nil, errors.New("already recording"))
				return
			}
			if len(args) < 6 {
				reply(nil, errors.New("bad arguments"))
				return
			}
			quality, maxMs, flags := args[0], binary.LittleEndian.Uint32(args[1:]), args[5]
			path := ""
			if rest := args[6:]; len(rest) >= 4 {
				_, rest = readString(rest) // the title
				if len(rest) >= 4 {
					path, _ = readString(rest)
				}
			}
			stream := flags&2 != 0
			if path == "" {
				ext := ".m4a"
				if stream {
					ext = ".aac"
				}
				dir := filepath.Join(h.cacheDir(), "ryui-audio")
				os.MkdirAll(dir, 0o755)
				f, err := os.CreateTemp(dir, "*"+ext)
				if err != nil {
					reply(nil, err)
					return
				}
				path = f.Name()
				f.Close()
			}
			channels, bps := 1, 12000
			if quality == 1 {
				channels, bps = 2, 16000
			}
			r := &recording{cmds: make(chan byte, 8)}
			recNow = r
			go WithCOM(func() {
				err := r.run(path, channels, bps, stream, maxMs)
				h.Post(func() {
					if recNow == r {
						recNow = nil
					}
					if err != nil {
						os.Remove(path)
						reply(nil, err)
						return
					}
					reply([]byte(path), nil)
				})
			})
		},
		"stop":   cmd('s'),
		"pause":  cmd('p'),
		"resume": cmd('r'),
		"level":  func(_ []byte, reply func([]byte, error)) { reply([]byte{level()}, nil) },
		"status": func(_ []byte, reply func([]byte, error)) {
			out := []byte{level(), 0}
			ms := uint32(0)
			if r := recNow; r != nil {
				out[1] = b2u(r.recording.Load())
				ms = r.ms.Load()
			}
			reply(binary.LittleEndian.AppendUint32(out, ms), nil)
		},
	}
}

// run records until stopped (or max milliseconds), on a thread of its
// own in COM's multithreaded apartment.
func (r *recording) run(path string, channels, bps int, stream bool, maxMs uint32) error {
	const rate = 44100
	src, err := openMicrophone(channels, rate)
	if err != nil {
		return err
	}
	w, err := NewAudioWriter(path, channels, rate, bps, stream)
	if err != nil {
		src.Close(func([]byte) {})
		return err
	}
	frameBytes := 2 * channels
	limit := int64(maxMs) * rate / 1000 * int64(frameBytes) // bytes; 0: none
	var kept int64
	var werr error
	write := func(pcm []byte) {
		if werr != nil || (limit > 0 && kept >= limit) {
			return
		}
		if limit > 0 && kept+int64(len(pcm)) > limit {
			pcm = pcm[:limit-kept]
		}
		werr = w.Write(pcm)
		kept += int64(len(pcm))
		r.ms.Store(uint32(kept / int64(frameBytes) * 1000 / rate))
		r.level.Store(uint32(pcmLevel(pcm)))
	}
	if err := src.Start(); err != nil {
		src.Close(func([]byte) {})
		w.Close()
		return err
	}
	r.recording.Store(true)
loop:
	for werr == nil && (limit == 0 || kept < limit) {
		select {
		case c := <-r.cmds:
			switch c {
			case 's':
				break loop
			case 'p':
				if r.recording.Load() {
					src.Stop()
					r.recording.Store(false)
				}
			case 'r':
				if !r.recording.Load() {
					src.Start()
					r.recording.Store(true)
				}
			}
		default:
		}
		src.Wait(50)
		src.Read(write)
	}
	r.recording.Store(false)
	src.Close(write)
	if err := w.Close(); werr == nil {
		werr = err
	}
	return werr
}

// pcmLevel is 16-bit PCM's level, 0 to 255: its RMS amplitude, silence
// below -60 dB (as AVAudioRecorder's average power reads).
func pcmLevel(pcm []byte) byte {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[2*i:]))) / 32768
		sum += v * v
	}
	rms := math.Sqrt(sum / float64(n))
	if rms < 0.001 {
		return 0
	}
	return byte(min(rms, 1) * 255)
}
