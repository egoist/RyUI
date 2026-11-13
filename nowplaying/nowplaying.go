// Package nowplaying shows what the app plays where the system shows
// media, and takes the commands the system sends back: the lock screen,
// Control Center and the notification shade, the media keys of keyboards,
// headsets and cars.
//
//	nowplaying.Show(nowplaying.Props{
//		Title:    track.Title, // accessors: it follows them
//		Artist:   track.Artist,
//		Artwork:  track.Cover, // an asset, a file, an http(s) URL
//		Duration: duration,
//		State:    state,    // Playing, Paused, Stopped
//		Position: position, // seconds, read when playback jumps
//		OnPlay:   play,
//		OnPause:  pause,
//		OnNext:   next,
//		OnSeek:   func(at float64) { seek(at) },
//	})
//
// Show is called in a component: what shows follows its accessors, and
// goes when the component's owner is disposed. While several are
// mounted, the last one shows, and when it's disposed, the one before.
//
// The system advances the position by itself while State is Playing, at
// Rate: Position is read when it changes, and published again only when
// playback jumps (a seek, a stall) or the state or rate changes, so it
// may follow a player's status polled a few times a second at no cost.
//
// A command is enabled when its handler is set; the others are greyed
// out or hidden. Play, Pause and TogglePlayPause stand in for each other
// (a headset's one button toggles; a lock screen shows play or pause):
// any of them enables all three. Seeking is enabled when OnSeek is set
// and Duration is known. OnSkipForward and OnSkipBackward skip by
// SkipInterval (15 seconds by default); on iOS and macOS, Control Center
// shows them in place of next and previous.
//
// For a player of package audio, set Player: the state, position and
// duration come from it (its Status, read once a second; a command run
// on it shows at once), and play, pause, stop, seek and, with SkipInterval,
// skip run on it, for the handlers not set. Its rate isn't reported:
// after SetRate, set Rate too.
//
//	music := audio.Load("music/spring.mp3")
//	signal.OnCleanup(music.Release)
//	nowplaying.Show(nowplaying.Props{
//		Title:  ui.Val("Spring"),
//		Artist: ui.Val("Vivaldi"),
//		Player: music,
//		OnNext: playNext,
//	})
//
// Each platform's own:
//
//   - iOS: MPNowPlayingInfoCenter and MPRemoteCommandCenter: the lock
//     screen, Control Center, the Dynamic Island, AirPods and CarPlay's
//     Now Playing. The app is given the audio background mode
//     (UIBackgroundModes) so it keeps playing with the screen locked, and
//     the audio session is the playback category; iOS shows only the app
//     whose audio session plays.
//   - Android: a MediaSession, with its media-style notification, which
//     the shade, the lock screen, Bluetooth headsets, Android Auto and
//     Wear OS show and drive; while Playing, a foreground service of the
//     mediaPlayback type keeps the app running out of sight (Android 14's
//     rules), which the build declares for apps that import this
//     package. Paused, the notification stays and may be swiped away.
//   - macOS: MPNowPlayingInfoCenter and MPRemoteCommandCenter: Control
//     Center's Now Playing, the menu bar's, the keyboard's media keys,
//     AirPods.
//   - Windows: the window's SystemMediaTransportControls: the media
//     controls of the volume flyout (Windows 10) or quick settings
//     (Windows 11), the lock screen, and the keyboard's media keys.
//   - Linux: an MPRIS player (org.mpris.MediaPlayer2) on the session bus,
//     which GNOME's and KDE's media controls, their lock screens, the
//     media keys and playerctl show and drive. The app's bus name is
//     owned only while it shows something.
//   - The web: the page's Media Session (navigator.mediaSession): the
//     browser's media controls, the media keys, a phone's lock screen.
//     A browser shows it only while the page plays audio.
package nowplaying

import (
	"encoding/json"
	"math"
	"time"

	"github.com/egoist/asuka/audio"
	spec "github.com/egoist/asuka/internal/nowplayingspec"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// State is where playback is.
type State uint8

const (
	// Paused: it can play from where it is (the zero value).
	Paused State = iota
	// Playing: the position advances at the rate.
	Playing
	// Stopped: it has nothing playing, but still shows; dispose the
	// component to take it away.
	Stopped
)

func (s State) String() string {
	return [...]string{"paused", "playing", "stopped", "?"}[min(int(s), 3)]
}

// Props is what shows and what the commands do.
type Props struct {
	// Title, Artist and Album describe what plays.
	Title, Artist, Album signal.Accessor[string]
	// Artwork is its cover: an image of the app's assets
	// ("covers/spring.jpg"), a file's path, or an http(s) URL, as
	// ui.Image's Source takes. A square image of 512 pixels or more
	// looks sharp everywhere.
	Artwork signal.Accessor[string]
	// Duration is its length in seconds; 0 (or nil): not known, as a live
	// stream's, which can't be sought.
	Duration signal.Accessor[float64]
	// State is whether it plays (nil: Paused, or with Player, the
	// player's).
	State signal.Accessor[State]
	// Position is where it is, in seconds. The system advances it while
	// Playing: it need only change when playback jumps, though it may
	// follow a player's status (see the package's doc).
	Position signal.Accessor[float64]
	// Rate is how fast it plays: 1 (or nil, or 0) is normal speed.
	Rate signal.Accessor[float64]

	// The commands, each on the main thread. Each enables its command.
	OnPlay, OnPause, OnTogglePlayPause, OnStop, OnNext, OnPrevious func()
	// OnSeek moves playback to position, in seconds (a scrubber dragged).
	OnSeek func(position float64)
	// OnSkipForward and OnSkipBackward move it by seconds: SkipInterval,
	// or what a desktop's controls ask (MPRIS's Seek).
	OnSkipForward, OnSkipBackward func(seconds float64)
	// SkipInterval is how far a skip goes, in seconds: 15 by default.
	// With Player, a SkipInterval enables skipping on the player.
	SkipInterval float64

	// Player is a player of package audio to follow and drive (see the
	// package's doc).
	Player *audio.Player
}

// The module's name (see internal/nowplayingspec for its methods).
const module = "nowPlaying"

// session is a Show mounted.
type session struct {
	p      Props
	info   spec.Info
	dead   bool
	status *signal.Signal[audio.Status] // the Player's, as last read
	readAt time.Time                    // when status was read
	stop   func()                       // cancels the Player's next status read
	seq    int                          // counts assume's
}

var (
	shown []*session // mounted, in order; the last shows

	sent    spec.Info // as last sent
	sentAt  time.Time
	showing bool // something is set

	following bool
	gen       int // a "next" of another generation is stale

	clearing int // counts the clears put off; a Show cancels the one pending
)

// Seams for tests: the clock, and the Player's next status read.
var (
	now   = time.Now
	after = func(d time.Duration, f func()) (stop func()) {
		t := time.AfterFunc(d, func() { ui.Post(f) })
		return func() { t.Stop() }
	}
)

// statusEvery is how often a Player's status is read.
const statusEvery = time.Second

// Show shows what the app plays while the component that calls it is
// mounted, following p's accessors (see the package's doc). Main thread.
//
// Never inlined: asuka build looks for its symbol (cmd/asuka iosFeatures).
//
//go:noinline
func Show(p Props) {
	s := &session{p: p}
	if p.Player != nil {
		s.status = signal.New(audio.Status{})
		s.readStatus()
	}
	if len(shown) == 0 {
		clearing++ // what the last one showed is replaced, not cleared
	}
	shown = append(shown, s)
	follow()
	signal.RenderEffect(func() {
		s.info = s.compute()
		publish()
	})
	signal.OnCleanup(func() {
		s.dead = true
		if s.stop != nil {
			s.stop()
		}
		for i, x := range shown {
			if x == s {
				shown = append(shown[:i], shown[i+1:]...)
				break
			}
		}
		if len(shown) > 0 {
			publish() // the one before
			return
		}
		// The last one: cleared once what replaces it, if anything, has
		// mounted (a track's component giving way to the next one's), so
		// the system's controls stay the app's in between (Android's
		// session, Linux's bus name, the headset's media button).
		unfollow()
		clearing++
		c := clearing
		ui.Post(func() {
			if c == clearing && len(shown) == 0 {
				publish()
			}
		})
	})
}

// readStatus reads the Player's status, then again in a second. A read
// a command overtook (assume) is dropped.
func (s *session) readStatus() {
	seq := s.seq
	s.p.Player.Status(func(st audio.Status) {
		if s.dead || seq != s.seq {
			return
		}
		s.readAt = now()
		s.status.Set(st)
		s.schedule()
	})
}

// schedule reads the Player's status in a second.
func (s *session) schedule() {
	if s.stop != nil {
		s.stop()
	}
	s.stop = after(statusEvery, func() {
		s.stop = nil
		if !s.dead {
			s.readStatus()
		}
	})
}

func str(a signal.Accessor[string]) string {
	if a == nil {
		return ""
	}
	return a.Get()
}

// compute is what s shows now, reading (and so following) its accessors.
func (s *session) compute() spec.Info {
	p := s.p
	info := spec.Info{Title: str(p.Title), Artist: str(p.Artist), Album: str(p.Album), Rate: 1}
	if a := str(p.Artwork); a != "" {
		info.Artwork = ui.AssetURL(a)
	}
	var st audio.Status
	if s.status != nil {
		st = s.status.Get()
	}
	if p.Duration != nil {
		info.Duration = p.Duration.Get()
	} else {
		info.Duration = st.Duration
	}
	if p.Rate != nil {
		if r := p.Rate.Get(); r > 0 {
			info.Rate = r
		}
	}
	state := Paused
	if p.State != nil {
		state = p.State.Get()
	} else if st.Playing && !st.Ended {
		state = Playing
	}
	info.State = state.String()
	if p.Position != nil {
		info.Position = p.Position.Get()
	} else if st.Ended && st.Duration > 0 {
		info.Position = st.Duration
	} else {
		info.Position = st.Position
	}
	if !finite(info.Duration) || info.Duration < 0 {
		info.Duration = 0
	}
	if !finite(info.Position) || info.Position < 0 {
		info.Position = 0
	}
	if info.Duration > 0 {
		info.Position = min(info.Position, info.Duration)
	}
	if !finite(info.Rate) {
		info.Rate = 1
	}

	player := p.Player != nil
	var c uint32
	if p.OnPlay != nil || p.OnPause != nil || p.OnTogglePlayPause != nil || player {
		c |= spec.Play.Bit() | spec.Pause.Bit() | spec.TogglePlayPause.Bit()
	}
	if p.OnStop != nil || player {
		c |= spec.Stop.Bit()
	}
	if p.OnNext != nil {
		c |= spec.Next.Bit()
	}
	if p.OnPrevious != nil {
		c |= spec.Previous.Bit()
	}
	if (p.OnSeek != nil || player) && info.Duration > 0 {
		c |= spec.Seek.Bit()
	}
	if p.OnSkipForward != nil || player && p.SkipInterval > 0 {
		c |= spec.SkipForward.Bit()
	}
	if p.OnSkipBackward != nil || player && p.SkipInterval > 0 {
		c |= spec.SkipBackward.Bit()
	}
	info.Commands = c
	if c&(spec.SkipForward.Bit()|spec.SkipBackward.Bit()) != 0 {
		info.Skip = s.skip()
	}
	return info
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

func (s *session) skip() float64 {
	if s.p.SkipInterval > 0 {
		return s.p.SkipInterval
	}
	return 15
}

// publish sends what shows now to the host, if it changed: a position
// only when it isn't where the system has it by now (it advances it).
func publish() {
	if len(shown) == 0 {
		if showing {
			showing = false
			sent = spec.Info{}
			ui.Call(module, "clear", nil, nil)
		}
		return
	}
	info := shown[len(shown)-1].info
	t := now()
	if showing && same(info, sent, t.Sub(sentAt).Seconds()) {
		return
	}
	b, err := json.Marshal(info)
	if err != nil {
		return
	}
	sent, sentAt, showing = info, t, true
	ui.Call(module, "set", b, nil)
}

// same reports whether info shows what was sent elapsed seconds ago:
// the same but for a position the system has advanced to by itself
// (half a second off at most while playing, for a player's position
// read with some delay).
func same(info, was spec.Info, elapsed float64) bool {
	p := info.Position
	info.Position = was.Position
	if info != was {
		return false
	}
	tolerance := 0.01
	if was.State == "playing" {
		tolerance = 0.5
	}
	return math.Abs(p-was.PositionAfter(elapsed)) <= tolerance
}

// follow asks the host for the commands, a "next" at a time, while
// something shows. A host without the module fails it: nothing to ask.
func follow() {
	if following {
		return
	}
	following = true
	gen++
	poll(gen)
}

func poll(g int) {
	ui.Call(module, "next", nil, func(b []byte, err error) {
		if g != gen {
			return
		}
		if err != nil {
			following = false
			return
		}
		spec.Commands(b, command)
		if g == gen {
			poll(g)
		}
	})
}

func unfollow() {
	if !following || len(shown) > 0 {
		return
	}
	following = false
	gen++
	ui.Call(module, "stop", nil, nil)
}

// command runs a command the system sent on what shows, if it's enabled.
func command(c spec.Command, v float64) {
	if len(shown) == 0 {
		return
	}
	s := shown[len(shown)-1]
	if !s.info.Has(c) {
		return
	}
	p, pl := s.p, s.p.Player
	playing := s.info.State == "playing"
	play := func() {
		switch {
		case p.OnPlay != nil:
			p.OnPlay()
		case p.OnTogglePlayPause != nil:
			if !playing {
				p.OnTogglePlayPause()
			}
		case pl != nil:
			pl.Play()
			s.assume(func(st *audio.Status) { st.Playing, st.Ended = true, false })
		}
	}
	pause := func() {
		switch {
		case p.OnPause != nil:
			p.OnPause()
		case p.OnTogglePlayPause != nil:
			if playing {
				p.OnTogglePlayPause()
			}
		case pl != nil:
			pl.Pause()
			s.assume(func(st *audio.Status) { st.Playing = false })
		}
	}
	seek := func(to float64, handler func(float64), arg float64) {
		if handler != nil {
			handler(arg)
			return
		}
		if pl == nil {
			return
		}
		to = max(to, 0)
		if s.info.Duration > 0 {
			to = min(to, s.info.Duration)
		}
		pl.Seek(to)
		s.assume(func(st *audio.Status) { st.Position, st.Ended = to, false })
	}
	switch c {
	case spec.Play:
		play()
	case spec.Pause:
		pause()
	case spec.TogglePlayPause:
		switch {
		case p.OnTogglePlayPause != nil:
			p.OnTogglePlayPause()
		case playing:
			pause()
		default:
			play()
		}
	case spec.Stop:
		switch {
		case p.OnStop != nil:
			p.OnStop()
		case pl != nil:
			pl.Pause()
			pl.Seek(0)
			s.assume(func(st *audio.Status) { st.Playing, st.Ended, st.Position = false, false, 0 })
		}
	case spec.Next:
		p.OnNext()
	case spec.Previous:
		p.OnPrevious()
	case spec.Seek:
		seek(v, p.OnSeek, v)
	case spec.SkipForward:
		if v <= 0 {
			v = s.skip()
		}
		seek(s.position()+v, p.OnSkipForward, v)
	case spec.SkipBackward:
		if v <= 0 {
			v = s.skip()
		}
		seek(s.position()-v, p.OnSkipBackward, v)
	}
}

// assume changes the Player's status as a command it ran on it does,
// until its status is read again, a second later (a stream may take a
// moment to start).
func (s *session) assume(f func(*audio.Status)) {
	if s.status == nil {
		return
	}
	st := s.status.Peek()
	st.Position = s.position()
	f(&st)
	s.seq++
	s.readAt = now()
	s.status.Set(st)
	s.schedule()
}

// position is where the Player is now: as last read, advanced since.
func (s *session) position() float64 {
	if s.status == nil {
		return s.info.Position
	}
	st := s.status.Peek()
	p := st.Position
	if st.Playing && !st.Ended {
		p += now().Sub(s.readAt).Seconds() * s.info.Rate
	}
	if st.Duration > 0 {
		p = min(p, st.Duration)
	}
	return max(p, 0)
}
