//go:build darwin && !ios

package darwin

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/nowplaying"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// npPump runs the main run loop, and what's posted, until ok (or a
// safety net's worth of turns, not what passes).
func npPump(h *Host, ok func() bool) bool {
	app := Send(Class("NSApplication"), "sharedApplication")
	mode := Const(Foundation(), "NSDefaultRunLoopMode")
	for i := 0; i < 2000; i++ {
		h.runPosted()
		if ok() {
			return true
		}
		until := MsgFloatID(Class("NSDate"), Sel("dateWithTimeIntervalSinceNow:"), 0.005)
		if ev := Send(app, "nextEventMatchingMask:untilDate:inMode:dequeue:", ^uintptr(0), uintptr(until), uintptr(mode), 1); ev != 0 {
			Send(app, "sendEvent:", uintptr(ev))
		}
	}
	return ok()
}

// What shows is MPNowPlayingInfoCenter's info (its artwork once read),
// the commands with handlers are MPRemoteCommandCenter's enabled ones,
// and invoking them, as Control Center and the media keys do, runs the
// handlers; disposed, the info and the commands go.
func TestNowPlaying(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for i := range img.Pix {
		img.Pix[i] = 0x80
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	png.Encode(&buf, img)
	cover := filepath.Join(dir, "cover.png")
	if err := os.WriteFile(cover, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}

	shown := signal.New(true)
	state := signal.New(nowplaying.Paused)
	var got []string
	h, _ := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			nowplaying.Show(nowplaying.Props{
				Title:          ui.Val("Spring"),
				Artist:         ui.Val("Vivaldi"),
				Album:          ui.Val("The Four Seasons"),
				Artwork:        ui.Val(cover),
				Duration:       ui.Val(200.0),
				Position:       ui.Val(12.0),
				State:          state,
				OnPlay:         func() { got = append(got, "play"); state.Set(nowplaying.Playing) },
				OnPause:        func() { got = append(got, "pause"); state.Set(nowplaying.Paused) },
				OnSeek:         func(at float64) { got = append(got, "seek "+strconv.FormatFloat(at, 'g', -1, 64)) },
				OnSkipForward:  func(s float64) { got = append(got, "forward "+strconv.FormatFloat(s, 'g', -1, 64)) },
				OnSkipBackward: func(s float64) { got = append(got, "back") },
				SkipInterval:   30,
			})
			return ui.StaticText("x")
		})
	})
	key := func(name string) uintptr { return uintptr(Const(nowPlaying.lib, name)) }
	info := func() ID {
		return Send(Send(Class("MPNowPlayingInfoCenter"), "defaultCenter"), "nowPlayingInfo")
	}
	num := func(name string) float64 { return MsgFloat(Send(info(), "objectForKey:", key(name)), Sel("doubleValue")) }
	commands := func() ID { return Send(Class("MPRemoteCommandCenter"), "sharedCommandCenter") }
	invoke := func(getter string, event func(cmd ID) ID) {
		cmd := Send(commands(), getter)
		done := NewBlock(func(_ objc.Block, status ID) {})
		defer done.Release()
		Send(cmd, "invokeCommandWithEvent:completion:", uintptr(event(cmd)), uintptr(done))
	}
	plain := func(cmd ID) ID { return Send(cmd, "newCommandEvent") }

	onMain(func() {
		d := info()
		if d == 0 {
			fatal(t, "no nowPlayingInfo")
		}
		if s := GoString(Send(d, "objectForKey:", key("MPMediaItemPropertyTitle"))); s != "Spring" {
			fatalf(t, "title %q", s)
		}
		if s := GoString(Send(d, "objectForKey:", key("MPMediaItemPropertyArtist"))); s != "Vivaldi" {
			fatalf(t, "artist %q", s)
		}
		if s := GoString(Send(d, "objectForKey:", key("MPMediaItemPropertyAlbumTitle"))); s != "The Four Seasons" {
			fatalf(t, "album %q", s)
		}
		if v := num("MPMediaItemPropertyPlaybackDuration"); v != 200 {
			fatalf(t, "duration %v", v)
		}
		if v := num("MPNowPlayingInfoPropertyElapsedPlaybackTime"); v != 12 {
			fatalf(t, "elapsed %v", v)
		}
		if v := num("MPNowPlayingInfoPropertyPlaybackRate"); v != 0 {
			fatalf(t, "rate while paused %v", v)
		}
		if s := SendInt(Send(Class("MPNowPlayingInfoCenter"), "defaultCenter"), "playbackState"); s != mpPlaybackPaused {
			fatalf(t, "playbackState %d", s)
		}
		enabled := map[string]bool{}
		for _, rc := range remoteCommands {
			enabled[rc.getter] = SendBool(Send(commands(), rc.getter), "isEnabled")
		}
		for getter, want := range map[string]bool{
			"playCommand": true, "pauseCommand": true, "togglePlayPauseCommand": true, "changePlaybackPositionCommand": true,
			"skipForwardCommand": true, "skipBackwardCommand": true,
			"nextTrackCommand": false, "previousTrackCommand": false, "stopCommand": false,
		} {
			if enabled[getter] != want {
				fatalf(t, "%s enabled %v, want %v", getter, enabled[getter], want)
			}
		}
		intervals := ArrayItems(Send(Send(commands(), "skipForwardCommand"), "preferredIntervals"))
		if len(intervals) != 1 || MsgFloat(intervals[0], Sel("doubleValue")) != 30 {
			fatalf(t, "skip intervals %v", intervals)
		}
		// The artwork, once its file is read.
		if !npPump(h, func() bool { return Send(info(), "objectForKey:", key("MPMediaItemPropertyArtwork")) != 0 }) {
			fatal(t, "no artwork")
		}
		art := Send(info(), "objectForKey:", key("MPMediaItemPropertyArtwork"))
		if b := MsgRect(art, Sel("bounds")); b.Size.Width != 64 || b.Size.Height != 64 {
			fatalf(t, "artwork bounds %+v", b)
		}
		if im := MsgSizeID(art, Sel("imageWithSize:"), Size{Width: 32, Height: 32}); im == 0 {
			fatal(t, "the artwork hands out no image")
		}

		// The commands, as the system invokes them.
		invoke("playCommand", plain)
		if !npPump(h, func() bool { return len(got) == 1 }) {
			fatalf(t, "play: %v", got)
		}
		npPump(h, func() bool { return num("MPNowPlayingInfoPropertyPlaybackRate") == 1 })
		if v := num("MPNowPlayingInfoPropertyPlaybackRate"); v != 1 {
			fatalf(t, "rate while playing %v", v)
		}
		if s := SendInt(Send(Class("MPNowPlayingInfoCenter"), "defaultCenter"), "playbackState"); s != mpPlaybackPlaying {
			fatalf(t, "playbackState %d", s)
		}
		invoke("togglePlayPauseCommand", plain) // playing: pauses
		invoke("changePlaybackPositionCommand", func(cmd ID) ID {
			return MsgFloatID(cmd, Sel("newCommandEventWithPositionTime:"), 42)
		})
		invoke("skipForwardCommand", func(cmd ID) ID { return MsgFloatID(cmd, Sel("newCommandEventWithInterval:"), 30) })
		invoke("nextTrackCommand", plain) // not enabled: nothing
		want := []string{"play", "pause", "seek 42", "forward 30"}
		if !npPump(h, func() bool { return len(got) >= len(want) }) || !slices.Equal(got, want) {
			fatalf(t, "got %v, want %v", got, want)
		}
	})

	onMain(func() {
		h.enter(func() { ui.Post(func() { shown.Set(false) }) })
		npPump(h, func() bool { return info() == 0 })
		if info() != 0 {
			fatal(t, "still showing")
		}
		if SendBool(Send(commands(), "playCommand"), "isEnabled") {
			fatal(t, "play still enabled")
		}
		if nowPlaying.following {
			fatal(t, "still following commands")
		}
	})
}
