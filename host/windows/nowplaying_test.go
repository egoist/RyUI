//go:build windows

package windows

import (
	"slices"
	"testing"

	spec "github.com/egoist/asuka/internal/nowplayingspec"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/nowplaying"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// What shows is the window's SystemMediaTransportControls' (read back
// from them): its status, title and buttons; a button pressed, as the
// system reports it, runs its handler, a disabled one nothing; disposed,
// the controls close.
func TestNowPlayingSMTC(t *testing.T) {
	shown := signal.New(true)
	state := signal.New(nowplaying.Paused)
	var got []string
	h, _ := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			nowplaying.Show(nowplaying.Props{
				Title:         ui.Val("Spring"),
				Artist:        ui.Val("Vivaldi"),
				Duration:      ui.Val(200.0),
				State:         state,
				OnPlay:        func() { got = append(got, "play"); state.Set(nowplaying.Playing) },
				OnPause:       func() { got = append(got, "pause"); state.Set(nowplaying.Paused) },
				OnSeek:        func(float64) { got = append(got, "seek") },
				OnSkipForward: func(float64) { got = append(got, "forward") },
			})
			return ui.StaticText("x")
		})
	})
	var failed error
	onMain(t, func() { failed = smtc.failed })
	if failed != nil {
		t.Skip("no media controls:", failed)
	}
	onMain(t, func() {
		s := smtc.controls.Shown()
		if !s.Enabled || s.Status != MediaPaused || !s.Play || !s.Pause || s.Next || !s.FastForward || s.Title != "Spring" {
			fatalf("shown %+v", s)
		}
		smtcButton(MediaButtonPlay)
		h.runPosted()
		if s := smtc.controls.Shown(); s.Status != MediaPlaying {
			fatalf("after Play: %+v", s)
		}
		smtcButton(MediaButtonPause)
		smtcButton(MediaButtonNext) // not enabled
		smtcButton(MediaButtonFastForward)
		smtcCommand(spec.Seek, 42) // the timeline's request
		h.runPosted()
		if want := []string{"play", "pause", "forward", "seek"}; !slices.Equal(got, want) {
			fatalf("got %v, want %v", got, want)
		}
		h.enter(func() { ui.Post(func() { shown.Set(false) }) })
		h.runPosted()
		if s := smtc.controls.Shown(); s.Enabled || s.Status != MediaClosed {
			fatalf("after dispose: %+v", s)
		}
		if smtc.following {
			fatalf("still following commands")
		}
	})
}
