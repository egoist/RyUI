// Package nowplayingspec is the wire format of asuka/nowplaying between
// Go and the hosts' "nowPlaying" module:
//
//   - "set" (Info as JSON) shows what the app plays where the system
//     shows media, and enables the commands Info.Commands has; the host
//     takes the moment it gets it as Position's time, and lets the
//     system advance the position at Rate while State is "playing".
//   - "clear" takes it away: the app no longer shows in the system's
//     media controls, and its commands are off.
//   - "next" answers once commands came, with them (AppendCommand's:
//     u8 command, f64 value each, in the order they came).
//   - "stop" fails the waiting "next" with "stopped".
//
// JSON for "set": it's sent when what shows changes (a track, a state, a
// jump in the position), never per frame.
package nowplayingspec

import (
	"encoding/binary"
	"math"
)

// Info is what shows.
type Info struct {
	Title  string `json:"title,omitempty"`
	Artist string `json:"artist,omitempty"`
	Album  string `json:"album,omitempty"`
	// Artwork is an image's source as ui.AssetURL gives it: an asset's
	// path (or its URL under asuka dev), a file's path, an http(s) URL.
	Artwork string `json:"artwork,omitempty"`
	// Duration is in seconds; 0: not known (a live stream), so no seeking.
	Duration float64 `json:"duration,omitempty"`
	// Position is where playback is, in seconds, as the host gets it.
	Position float64 `json:"position"`
	// Rate is how fast it plays while playing (1: normal speed).
	Rate float64 `json:"rate"`
	// State is "playing", "paused" or "stopped".
	State string `json:"state"`
	// Commands are the commands enabled, a bit each (Command.Bit).
	Commands uint32 `json:"commands"`
	// Skip is the interval of SkipForward and SkipBackward, in seconds.
	Skip float64 `json:"skip,omitempty"`
}

// Has reports whether c is enabled.
func (i *Info) Has(c Command) bool { return i.Commands&c.Bit() != 0 }

// Command is what the system asks of the app.
type Command uint8

// The commands; their numbers are the wire's.
const (
	Play Command = 1 + iota
	Pause
	TogglePlayPause
	Stop
	Next
	Previous
	// Seek's value is the position to move to, in seconds.
	Seek
	// SkipForward's and SkipBackward's value is how far, in seconds.
	SkipForward
	SkipBackward
)

// Bit is c's bit in Info.Commands.
func (c Command) Bit() uint32 { return 1 << c }

func (c Command) String() string {
	names := [...]string{"?", "play", "pause", "togglePlayPause", "stop", "next", "previous", "seek", "skipForward", "skipBackward"}
	if int(c) < len(names) {
		return names[c]
	}
	return "?"
}

// AppendCommand appends a command as "next" answers with it.
func AppendCommand(b []byte, c Command, value float64) []byte {
	return binary.LittleEndian.AppendUint64(append(b, byte(c)), math.Float64bits(value))
}

// Commands calls f with each command of a "next" answer.
func Commands(b []byte, f func(c Command, value float64)) {
	for ; len(b) >= 9; b = b[9:] {
		f(Command(b[0]), math.Float64frombits(binary.LittleEndian.Uint64(b[1:])))
	}
}

// PositionAfter is where playback is t seconds after the host got i: it
// advances at Rate while playing, within the duration (if known).
func (i *Info) PositionAfter(t float64) float64 {
	p := i.Position
	if i.State == "playing" && t > 0 {
		p += t * i.Rate
	}
	if i.Duration > 0 {
		p = min(p, i.Duration)
	}
	return max(p, 0)
}
