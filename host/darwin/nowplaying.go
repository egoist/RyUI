//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	spec "github.com/egoist/asuka/internal/nowplayingspec"
)

// asuka/sdk/nowplaying on a Mac: MediaPlayer's MPNowPlayingInfoCenter, which
// Control Center's and the menu bar's Now Playing show, and
// MPRemoteCommandCenter, whose commands they, the keyboard's media keys
// and AirPods send. Both are the process's: the module's state is the
// package's. The framework is loaded when an app first shows something.
// The module's methods are internal/nowplayingspec's.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["nowPlaying"] = h.nowPlayingModule() })
}

// remoteCommands are MPRemoteCommandCenter's commands, by the command
// each sends.
var remoteCommands = []struct {
	getter string
	cmd    spec.Command
}{
	{"playCommand", spec.Play},
	{"pauseCommand", spec.Pause},
	{"togglePlayPauseCommand", spec.TogglePlayPause},
	{"stopCommand", spec.Stop},
	{"nextTrackCommand", spec.Next},
	{"previousTrackCommand", spec.Previous},
	{"changePlaybackPositionCommand", spec.Seek},
	{"skipForwardCommand", spec.SkipForward},
	{"skipBackwardCommand", spec.SkipBackward},
}

// MPNowPlayingPlaybackState.
const (
	mpPlaybackUnknown = 0
	mpPlaybackPlaying = 1
	mpPlaybackPaused  = 2
	mpPlaybackStopped = 3
)

// nowPlaying is the module's state, main thread only.
var nowPlaying struct {
	lib     uintptr
	loaded  bool
	keys    map[string]ID // MediaPlayer's NSString keys
	targets bool          // the commands have their handlers
	info    *spec.Info    // what shows; nil: nothing

	artSrc string // the artwork's source
	art    ID     // its MPMediaItemArtwork (+1), once loaded

	following bool
	waiting   func([]byte, error)
	queue     []byte
}

var msgInitArtwork func(obj ID, sel SEL, size Size, handler objc.Block) ID

// mediaPlayer loads MediaPlayer.framework and the keys it exports.
func mediaPlayer() bool {
	np := &nowPlaying
	if np.loaded {
		return np.lib != 0
	}
	np.loaded = true
	np.lib = Dlopen("/System/Library/Frameworks/MediaPlayer.framework/MediaPlayer")
	if np.lib == 0 {
		return false
	}
	purego.RegisterFunc(&msgInitArtwork, MsgSendAddr())
	np.keys = map[string]ID{}
	for _, k := range []string{
		"MPMediaItemPropertyTitle", "MPMediaItemPropertyArtist", "MPMediaItemPropertyAlbumTitle",
		"MPMediaItemPropertyPlaybackDuration", "MPMediaItemPropertyArtwork", "MPNowPlayingInfoPropertyMediaType",
		"MPNowPlayingInfoPropertyElapsedPlaybackTime", "MPNowPlayingInfoPropertyPlaybackRate",
		"MPNowPlayingInfoPropertyDefaultPlaybackRate",
	} {
		np.keys[k] = Const(np.lib, k)
	}
	return true
}

func (h *Host) nowPlayingModule() map[string]method {
	return map[string]method{
		"set": func(args []byte, reply func([]byte, error)) {
			var info spec.Info
			if err := json.Unmarshal(args, &info); err != nil {
				reply(nil, err)
				return
			}
			if !mediaPlayer() {
				reply(nil, errors.New("not available: no MediaPlayer framework"))
				return
			}
			h.nowPlayingSet(&info)
			reply(nil, nil)
		},
		"clear": func(_ []byte, reply func([]byte, error)) {
			if nowPlaying.lib != 0 {
				h.nowPlayingSet(nil)
			}
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			np := &nowPlaying
			if w := np.waiting; w != nil {
				w(nil, errors.New("stopped")) // one poll at a time
			}
			np.following, np.waiting = true, reply
			flushNowPlaying()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			np := &nowPlaying
			w := np.waiting
			np.following, np.waiting, np.queue = false, nil, nil
			if w != nil {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// remoteCommand is a command the system sent: to the waiting "next".
func remoteCommand(c spec.Command, v float64) {
	np := &nowPlaying
	if !np.following || np.info == nil || !np.info.Has(c) {
		return
	}
	np.queue = spec.AppendCommand(np.queue, c, v)
	flushNowPlaying()
}

func flushNowPlaying() {
	np := &nowPlaying
	if np.waiting == nil || len(np.queue) == 0 {
		return
	}
	w, b := np.waiting, np.queue
	np.waiting, np.queue = nil, nil
	w(b, nil)
}

// nowPlayingSet shows info (nil: nothing) and enables its commands.
func (h *Host) nowPlayingSet(info *spec.Info) {
	np := &nowPlaying
	np.info = info
	WithPool(func() {
		commands := Send(Class("MPRemoteCommandCenter"), "sharedCommandCenter")
		center := Send(Class("MPNowPlayingInfoCenter"), "defaultCenter")
		if commands == 0 || center == 0 {
			return
		}
		if !np.targets {
			np.targets = true
			for _, rc := range remoteCommands {
				c := rc.cmd
				// The command keeps its copy of the block for good.
				handler := NewBlock(func(_ objc.Block, event ID) int {
					var v float64
					switch c {
					case spec.Seek:
						v = MsgFloat(event, Sel("positionTime"))
					case spec.SkipForward, spec.SkipBackward:
						v = MsgFloat(event, Sel("interval"))
					}
					h.Post(func() { remoteCommand(c, v) })
					return 0 // MPRemoteCommandHandlerStatusSuccess
				})
				Send(Send(commands, rc.getter), "addTargetWithHandler:", uintptr(handler))
				handler.Release()
			}
		}
		for _, rc := range remoteCommands {
			cmd := Send(commands, rc.getter)
			Send(cmd, "setEnabled:", Bool(info != nil && info.Has(rc.cmd)))
			if info != nil && info.Skip > 0 && (rc.cmd == spec.SkipForward || rc.cmd == spec.SkipBackward) {
				Send(cmd, "setPreferredIntervals:", uintptr(Array(NumberFloat(info.Skip))))
			}
		}
		if info == nil {
			h.nowPlayingArtwork("")
			Send(center, "setNowPlayingInfo:", 0)
			Send(center, "setPlaybackState:", mpPlaybackStopped)
			return
		}
		h.nowPlayingArtwork(info.Artwork)
		h.nowPlayingInfo(center)
	})
}

// nowPlayingInfo sets the center's info and state from what shows.
func (h *Host) nowPlayingInfo(center ID) {
	np := &nowPlaying
	info := np.info
	d := Send(Class("NSMutableDictionary"), "dictionary")
	put := func(key string, v ID) {
		if k := np.keys[key]; k != 0 && v != 0 {
			Send(d, "setObject:forKey:", uintptr(v), uintptr(k))
		}
	}
	if info.Title != "" {
		put("MPMediaItemPropertyTitle", String(info.Title))
	}
	if info.Artist != "" {
		put("MPMediaItemPropertyArtist", String(info.Artist))
	}
	if info.Album != "" {
		put("MPMediaItemPropertyAlbumTitle", String(info.Album))
	}
	if info.Duration > 0 {
		put("MPMediaItemPropertyPlaybackDuration", NumberFloat(info.Duration))
	}
	put("MPNowPlayingInfoPropertyMediaType", NumberInt(1)) // MPNowPlayingInfoMediaTypeAudio
	put("MPNowPlayingInfoPropertyElapsedPlaybackTime", NumberFloat(info.Position))
	put("MPNowPlayingInfoPropertyDefaultPlaybackRate", NumberFloat(1))
	rate, state := 0.0, mpPlaybackPaused
	switch info.State {
	case "playing":
		rate, state = info.Rate, mpPlaybackPlaying
	case "stopped":
		state = mpPlaybackStopped
	}
	put("MPNowPlayingInfoPropertyPlaybackRate", NumberFloat(rate))
	put("MPMediaItemPropertyArtwork", np.art)
	Send(center, "setNowPlayingInfo:", uintptr(d))
	Send(center, "setPlaybackState:", uintptr(state))
}

// nowPlayingArtwork makes the artwork of src, reading its image off the
// main thread (an http(s) URL's, a file's) and showing it once it's in.
func (h *Host) nowPlayingArtwork(src string) {
	np := &nowPlaying
	if src == np.artSrc {
		return
	}
	np.artSrc = src
	if np.art != 0 {
		Release(np.art)
		np.art = 0
	}
	if src == "" {
		return
	}
	go func() {
		data, err := readImage(src, h.assets)
		if err != nil {
			return
		}
		h.Post(func() {
			if np.artSrc != src || np.art != 0 {
				return
			}
			WithPool(func() {
				if np.art = nowPlayingImage(data); np.art != 0 && np.info != nil {
					h.nowPlayingInfo(Send(Class("MPNowPlayingInfoCenter"), "defaultCenter"))
				}
			})
		})
	}()
}

// nowPlayingImage makes an artwork (+1) of an image's bytes, or 0. Its
// handler decodes the image each time the system asks for it (at a size
// it then draws it at), from the bytes it holds: the system may keep the
// artwork after the app moves on, and the bytes go with it.
func nowPlayingImage(data []byte) ID {
	img := Send(Send(Class("NSImage"), "alloc"), "initWithData:", uintptr(Data(data)))
	if img == 0 {
		return 0
	}
	size := MsgSize(img, Sel("size"))
	Release(img)
	// The handler takes a CGSize: two doubles.
	handler := NewBlock(func(_ objc.Block, w, hh float64) ID {
		return Autorelease(Send(Send(Class("NSImage"), "alloc"), "initWithData:", uintptr(Data(data))))
	})
	defer handler.Release()
	return msgInitArtwork(Send(Class("MPMediaItemArtwork"), "alloc"), Sel("initWithBoundsSize:requestHandler:"), size, handler)
}
