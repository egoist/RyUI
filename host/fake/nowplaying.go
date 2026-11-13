package fake

import (
	"encoding/json"
	"errors"

	spec "github.com/egoist/asuka/internal/nowplayingspec"
)

// NowPlaying is the system's media controls, as the hosts' "nowPlaying"
// module keeps them (package nowplaying), for tests: Host.NowPlaying
// installs it.
type NowPlaying struct {
	// Info is what shows, nil when nothing does.
	Info *NowPlayingInfo
	// Sets and Clears count the calls.
	Sets, Clears int

	h         *Host
	following bool // between "next" and "stop"
	waiting   func([]byte, error)
	queue     []byte
}

// NowPlayingInfo is what shows, as the core last set it.
type NowPlayingInfo struct {
	Title, Artist, Album, Artwork string
	// Duration, Position (as of the set) and Skip are in seconds.
	Duration, Position, Rate, Skip float64
	// State is "playing", "paused" or "stopped".
	State string
	// Commands are the commands enabled, by name: "play", "pause",
	// "togglePlayPause", "stop", "next", "previous", "seek",
	// "skipForward", "skipBackward".
	Commands []string
}

// Enabled reports whether command (a name, as Commands has) is enabled.
func (i *NowPlayingInfo) Enabled(command string) bool {
	for _, c := range i.Commands {
		if c == command {
			return true
		}
	}
	return false
}

// NowPlaying gives the host media controls (the "nowPlaying" module), as
// every host has.
func (h *Host) NowPlaying() *NowPlaying {
	n := &NowPlaying{h: h}
	if h.AsyncModules == nil {
		h.AsyncModules = map[string]func(string, []byte, func([]byte, error)){}
	}
	h.AsyncModules["nowPlaying"] = n.call
	return n
}

func (n *NowPlaying) call(method string, args []byte, reply func([]byte, error)) {
	switch method {
	case "set":
		var in spec.Info
		if err := json.Unmarshal(args, &in); err != nil {
			reply(nil, err)
			return
		}
		info := &NowPlayingInfo{Title: in.Title, Artist: in.Artist, Album: in.Album, Artwork: in.Artwork,
			Duration: in.Duration, Position: in.Position, Rate: in.Rate, Skip: in.Skip, State: in.State}
		for c := spec.Play; c <= spec.SkipBackward; c++ {
			if in.Has(c) {
				info.Commands = append(info.Commands, c.String())
			}
		}
		n.Info = info
		n.Sets++
		reply(nil, nil)
	case "clear":
		n.Info = nil
		n.Clears++
		reply(nil, nil)
	case "next":
		n.following = true
		if len(n.queue) > 0 {
			b := n.queue
			n.queue = nil
			reply(b, nil)
			return
		}
		n.waiting = reply
	case "stop":
		if w := n.waiting; w != nil {
			n.waiting = nil
			w(nil, errors.New("stopped"))
		}
		n.queue, n.following = nil, false
		reply(nil, nil)
	default:
		reply(nil, errors.New("no module method nowPlaying."+method))
	}
}

// Following reports whether the core waits for commands ("next").
func (n *NowPlaying) Following() bool { return n.waiting != nil }

// Send sends a command, as the system does when it's pressed (by name, as
// NowPlayingInfo.Commands has; value: a seek's position, a skip's
// seconds), and runs what it posts. The core gets every command while
// it follows them (as a host's controls, which a "stop" turns off); it
// runs only those it enabled.
func (n *NowPlaying) Send(command string, value float64) {
	if !n.following {
		return
	}
	for c := spec.Play; c <= spec.SkipBackward; c++ {
		if c.String() == command {
			n.queue = spec.AppendCommand(n.queue, c, value)
		}
	}
	if w := n.waiting; w != nil && len(n.queue) > 0 {
		b := n.queue
		n.waiting, n.queue = nil, nil
		w(b, nil)
	}
	n.h.RunPosted()
}
