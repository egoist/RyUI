// Package audio plays sounds and music: a file in the app's assets, a
// file the app wrote, or an http(s) URL (a file or a stream), with the
// platform's player (AVPlayer; Media3's ExoPlayer). It plays with the
// silent switch on, as a music or game app's sound does, and mixes with
// nothing else's.
//
//	ding := audio.Load("sounds/ding.m4a")
//	ding.Play()
package audio

import (
	"encoding/binary"
	"math"
	"strconv"

	"github.com/egoist/ryui/ui"
)

// Player plays one sound. Main thread.
type Player struct{ id string }

var next int

// Load makes a player for source, ready to play. Release it when it's
// no longer needed.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Load(source string) *Player {
	next++
	p := &Player{id: strconv.Itoa(next)}
	p.call("load", []byte(ui.AssetURL(source)))
	return p
}

// Play plays a sound once, from the start, and lets it go when it ends.
func Play(source string) {
	p := Load(source)
	p.call("playOnce", nil)
}

func (p *Player) call(method string, arg []byte) {
	b := append(binary.LittleEndian.AppendUint32(nil, uint32(len(p.id))), p.id...)
	ui.Call("audio", method, append(b, arg...), nil)
}

func f64(v float64) []byte { return binary.LittleEndian.AppendUint64(nil, math.Float64bits(v)) }

// Play plays it from where it is.
func (p *Player) Play() { p.call("play", nil) }

// Pause pauses it where it is.
func (p *Player) Pause() { p.call("pause", nil) }

// Seek moves it to seconds from its start.
func (p *Player) Seek(seconds float64) { p.call("seek", f64(seconds)) }

// SetVolume sets its volume, from 0 to 1 (the default).
func (p *Player) SetVolume(v float64) { p.call("volume", f64(min(max(v, 0), 1))) }

// SetRate sets how fast it plays, from 0.25 to 4; 1 is the default. The
// pitch stays as it is, tuned for speech. It holds across Pause and Play.
func (p *Player) SetRate(r float64) { p.call("rate", f64(min(max(r, 0.25), 4))) }

// Status is where a player is.
type Status struct {
	// Position and Duration are in seconds. Duration is 0 until it's known.
	Position, Duration float64
	// Playing is true while sound is coming out of it.
	Playing bool
	// Ended is true once it has played to its end, until it's played or
	// sought again. Play after the end starts from the start.
	Ended bool
}

// Status gets where it is, and calls done on the main thread: read it a
// few times a second to follow playback.
func (p *Player) Status(done func(Status)) {
	b := append(binary.LittleEndian.AppendUint32(nil, uint32(len(p.id))), p.id...)
	ui.Call("audio", "status", b, func(r []byte, err error) {
		var s Status
		if err == nil && len(r) >= 18 {
			s.Position = math.Float64frombits(binary.LittleEndian.Uint64(r))
			s.Duration = math.Float64frombits(binary.LittleEndian.Uint64(r[8:]))
			s.Playing = r[16] == 1
			s.Ended = r[17] == 1
		}
		done(s)
	})
}

// SetLoop makes it start again when it ends, or not.
func (p *Player) SetLoop(on bool) {
	b := byte(0)
	if on {
		b = 1
	}
	p.call("loop", []byte{b})
}

// Release stops it and frees what it holds.
func (p *Player) Release() { p.call("release", nil) }
