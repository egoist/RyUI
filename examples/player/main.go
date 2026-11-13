// Player plays an album and shows what plays where the system shows
// media (asuka/nowplaying): the lock screen, Control Center and the
// notification shade, the menu bar's Now Playing, the volume flyout, the
// desktops' media controls, the browser's; their buttons and the media
// keys of keyboards and headsets play, pause, skip, seek and change track.
//
//	go run ./cmd/asuka dev --platform ios examples/player
//	go run ./cmd/asuka dev examples/player   # macOS, Windows, Linux
package main

import (
	"fmt"
	"time"

	"github.com/egoist/asuka/app"
	"github.com/egoist/asuka/audio"
	"github.com/egoist/asuka/nowplaying"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

type track struct{ title, file, cover string }

const (
	artist = "Asuka Ensemble"
	album  = "A Day in Sines"
	cover  = 280 // the artwork's side, in points
)

var tracks = []track{
	{"Morning", "music/morning.mp3", "covers/morning.jpg"},
	{"Afternoon", "music/afternoon.mp3", "covers/afternoon.jpg"},
	{"Evening", "music/evening.mp3", "covers/evening.jpg"},
}

func Player() ui.Node {
	index := signal.New(0)
	// playing carries playback from a track to the next.
	playing := signal.New(false)
	next := func() { index.Set((index.Peek() + 1) % len(tracks)) }
	previous := func() { index.Set((index.Peek() + len(tracks) - 1) % len(tracks)) }
	return ui.SafeArea(ui.Style{Justify: ui.JustifyCenter, Align: ui.AlignCenter, Padding: ui.All(24)},
		ui.Dynamic(func() ui.Node {
			t := tracks[index.Get()]
			return Track(t, playing, next, previous)
		}),
	)
}

// Track plays t: the system's controls follow its player, and drive it.
func Track(t track, playing *signal.Signal[bool], next, previous func()) ui.Node {
	music := audio.Load(t.file)
	signal.OnCleanup(music.Release)
	status := follow(music, next)
	signal.Effect(func() { playing.Set(status.Get().Playing) })
	if playing.Peek() {
		music.Play()
	}
	nowplaying.Show(nowplaying.Props{
		Title:        ui.Val(t.title),
		Artist:       ui.Val(artist),
		Album:        ui.Val(album),
		Artwork:      ui.Val(t.cover),
		Player:       music, // its state, position and duration; play, pause, stop, seek, skip
		OnNext:       next,
		OnPrevious:   previous,
		SkipInterval: 10,
	})

	toggle := func() {
		s := status.Peek()
		if s.Playing {
			music.Pause()
		} else {
			music.Play()
		}
		s.Playing = !s.Playing
		status.Set(s)
	}
	return ui.Column(ui.Style{Align: ui.AlignCenter, Gap: 16},
		ui.Image(ui.ImageProps{Source: ui.Val(t.cover),
			Style: ui.Style{Width: ui.Pt(cover), Height: ui.Pt(cover), CornerRadius: 16}}),
		ui.Column(ui.Style{Align: ui.AlignCenter, Gap: 4},
			ui.StaticText(t.title, ui.Style{Font: ui.Title2.Weighted(ui.Bold)}),
			ui.StaticText(artist+" — "+album, ui.Style{Color: ui.SecondaryLabel}),
		),
		ui.Progress(signal.Func(func() float64 {
			s := status.Get()
			if s.Duration <= 0 {
				return 0
			}
			return s.Position / s.Duration
		}), ui.Style{Width: ui.Pt(cover)}),
		ui.Text(signal.Func(func() string {
			s := status.Get()
			return clock(s.Position) + " / " + clock(s.Duration)
		}), ui.Style{Color: ui.SecondaryLabel, Font: ui.Footnote}),
		ui.Row(ui.Style{Gap: 12},
			ui.Button(ui.ButtonProps{Title: ui.Val("Previous"), Variant: ui.ButtonGray, OnPress: previous}),
			ui.Button(ui.ButtonProps{
				Title: signal.Func(func() string {
					if status.Get().Playing {
						return "Pause"
					}
					return "Play"
				}),
				Variant: ui.ButtonFilled,
				OnPress: toggle,
			}),
			ui.Button(ui.ButtonProps{Title: ui.Val("Next"), Variant: ui.ButtonGray, OnPress: next}),
		),
	)
}

// follow reads the player's status a few times a second, for the
// progress bar; at the end, it plays the next track.
func follow(p *audio.Player, next func()) *signal.Signal[audio.Status] {
	status := signal.New(audio.Status{})
	done := false
	signal.OnCleanup(func() { done = true })
	var read func()
	read = func() {
		p.Status(func(s audio.Status) {
			if done {
				return
			}
			if s.Ended {
				next()
				return
			}
			status.Set(s)
			time.AfterFunc(250*time.Millisecond, func() { ui.Post(read) })
		})
	}
	read()
	return status
}

func clock(seconds float64) string {
	s := int(seconds)
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func main() { app.Run(Player) }
