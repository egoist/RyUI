package nowplaying_test

import (
	"encoding/binary"
	"math"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/egoist/asuka/audio"
	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/nowplaying"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// clock is a test's clock, which only moves when told.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(seconds float64) { c.t = c.t.Add(time.Duration(seconds * float64(time.Second))) }
func newClock(t *testing.T) (*clock, func()) {
	c := &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	tick, restore := nowplaying.SetClock(c.now)
	t.Cleanup(restore)
	return c, tick
}

func mount(t *testing.T, root func() ui.Node) (*fake.Host, *fake.NowPlaying, *ui.Root) {
	t.Helper()
	h := fake.New()
	np := h.NowPlaying()
	r := ui.Mount(h, root)
	t.Cleanup(r.Dispose)
	settle(h, r)
	return h, np, r
}

func settle(h *fake.Host, r *ui.Root) {
	for range 10 {
		h.RunPosted()
		r.Flush()
	}
}

// What shows follows the accessors, is sent once per change, and enables
// exactly the commands with handlers.
func TestShow(t *testing.T) {
	newClock(t)
	title := signal.New("Spring")
	state := signal.New(nowplaying.Paused)
	h, np, r := mount(t, func() ui.Node {
		nowplaying.Show(nowplaying.Props{
			Title:    title,
			Artist:   ui.Val("Vivaldi"),
			Album:    ui.Val("The Four Seasons"),
			Artwork:  ui.Val("covers/spring.jpg"),
			Duration: ui.Val(200.0),
			State:    state,
			OnPlay:   func() {},
			OnPause:  func() {},
			OnNext:   func() {},
		})
		return ui.StaticText("x")
	})
	_ = h
	i := np.Info
	if i == nil || np.Sets != 1 {
		t.Fatalf("shown %+v, %d sets", i, np.Sets)
	}
	if i.Title != "Spring" || i.Artist != "Vivaldi" || i.Album != "The Four Seasons" || i.Artwork != "covers/spring.jpg" ||
		i.Duration != 200 || i.Position != 0 || i.Rate != 1 || i.State != "paused" {
		t.Fatalf("info %+v", i)
	}
	if want := []string{"play", "pause", "togglePlayPause", "next"}; !slices.Equal(i.Commands, want) {
		t.Fatalf("commands %v, want %v", i.Commands, want)
	}
	if !np.Following() {
		t.Fatal("not waiting for commands")
	}

	title.Set("Summer")
	r.Flush()
	if np.Sets != 2 || np.Info.Title != "Summer" {
		t.Fatalf("after a change: %d sets, %+v", np.Sets, np.Info)
	}
	r.Flush()
	state.Set(nowplaying.Playing)
	r.Flush()
	if np.Sets != 3 || np.Info.State != "playing" {
		t.Fatalf("playing: %d sets, %+v", np.Sets, np.Info)
	}
}

// The position is sent when playback jumps, not as it advances: the
// system advances it at the rate while playing.
func TestPosition(t *testing.T) {
	c, _ := newClock(t)
	pos := signal.New(10.0)
	state := signal.New(nowplaying.Playing)
	rate := signal.New(2.0)
	_, np, r := mount(t, func() ui.Node {
		nowplaying.Show(nowplaying.Props{Title: ui.Val("Talk"), Duration: ui.Val(600.0), State: state, Position: pos, Rate: rate})
		return ui.StaticText("x")
	})
	if np.Sets != 1 || np.Info.Position != 10 || np.Info.Rate != 2 {
		t.Fatalf("%d sets, %+v", np.Sets, np.Info)
	}
	// A player's status, read every second: where the system has it.
	for range 5 {
		c.add(1)
		pos.Set(pos.Peek() + 2.05)
		r.Flush()
	}
	if np.Sets != 1 {
		t.Fatalf("following playback sent it %d times", np.Sets)
	}
	// A seek.
	c.add(1)
	pos.Set(100)
	r.Flush()
	if np.Sets != 2 || np.Info.Position != 100 {
		t.Fatalf("a seek: %d sets, %+v", np.Sets, np.Info)
	}
	// A stall: the position stays while the time goes.
	c.add(1)
	r.Flush()
	pos.Set(100.1)
	r.Flush()
	if np.Sets != 3 {
		t.Fatalf("a stall: %d sets", np.Sets)
	}
	// Paused, it doesn't move; a small seek is sent.
	state.Set(nowplaying.Paused)
	r.Flush()
	sets := np.Sets
	c.add(5)
	pos.Set(100.1)
	r.Flush()
	if np.Sets != sets {
		t.Fatal("paused: sent without a change")
	}
	pos.Set(99)
	r.Flush()
	if np.Sets != sets+1 || np.Info.Position != 99 || np.Info.State != "paused" {
		t.Fatalf("paused seek: %d sets, %+v", np.Sets, np.Info)
	}
	// Beyond the duration, and nonsense, are clamped.
	pos.Set(1e9)
	r.Flush()
	if np.Info.Position != 600 {
		t.Fatalf("past the end: %v", np.Info.Position)
	}
	pos.Set(math.NaN())
	r.Flush()
	if np.Info.Position != 0 {
		t.Fatalf("NaN: %v", np.Info.Position)
	}
}

// Commands reach their handlers, only the enabled ones; play, pause and
// toggle stand in for each other; skips go by the interval.
func TestCommands(t *testing.T) {
	newClock(t)
	var got []string
	log := func(s string) func() { return func() { got = append(got, s) } }
	state := signal.New(nowplaying.Paused)
	dur := signal.New(0.0)
	_, np, r := mount(t, func() ui.Node {
		nowplaying.Show(nowplaying.Props{
			Title:    ui.Val("Episode"),
			State:    state,
			Duration: dur,
			OnTogglePlayPause: func() {
				got = append(got, "toggle")
				if state.Peek() == nowplaying.Playing {
					state.Set(nowplaying.Paused)
				} else {
					state.Set(nowplaying.Playing)
				}
			},
			OnPrevious:     log("previous"),
			OnStop:         log("stop"),
			OnSeek:         func(at float64) { got = append(got, "seek", fmtF(at)) },
			OnSkipForward:  func(s float64) { got = append(got, "forward", fmtF(s)) },
			OnSkipBackward: func(s float64) { got = append(got, "back", fmtF(s)) },
			SkipInterval:   30,
		})
		return ui.StaticText("x")
	})
	if want := []string{"play", "pause", "togglePlayPause", "stop", "previous", "skipForward", "skipBackward"}; !slices.Equal(np.Info.Commands, want) {
		t.Fatalf("commands %v, want %v (no seek without a duration)", np.Info.Commands, want)
	}
	if np.Info.Skip != 30 {
		t.Fatalf("skip %v", np.Info.Skip)
	}
	np.Send("play", 0)  // toggles: paused
	np.Send("play", 0)  // playing already: nothing
	np.Send("pause", 0) // toggles
	np.Send("next", 0)  // not enabled
	np.Send("seek", 42) // not enabled: no duration
	np.Send("previous", 0)
	np.Send("skipForward", 30)
	np.Send("skipBackward", 0) // no interval given: the props'
	np.Send("stop", 0)
	want := []string{"toggle", "toggle", "previous", "forward", "30", "back", "30", "stop"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	dur.Set(100)
	r.Flush()
	if !np.Info.Enabled("seek") {
		t.Fatalf("no seek with a duration: %v", np.Info.Commands)
	}
	got = nil
	np.Send("seek", 42)
	if !slices.Equal(got, []string{"seek", "42"}) {
		t.Fatalf("seek: %v", got)
	}
	if !np.Following() {
		t.Fatal("stopped waiting for commands")
	}
}

// Play and Pause handlers answer the toggle (a headset's button), as
// the state says.
func TestToggleFromPlayPause(t *testing.T) {
	newClock(t)
	var got []string
	state := signal.New(nowplaying.Playing)
	_, np, r := mount(t, func() ui.Node {
		nowplaying.Show(nowplaying.Props{
			State:   state,
			OnPlay:  func() { got = append(got, "play"); state.Set(nowplaying.Playing) },
			OnPause: func() { got = append(got, "pause"); state.Set(nowplaying.Paused) },
		})
		return ui.StaticText("x")
	})
	np.Send("togglePlayPause", 0)
	r.Flush()
	np.Send("togglePlayPause", 0)
	if !slices.Equal(got, []string{"pause", "play"}) {
		t.Fatalf("got %v", got)
	}
}

// Disposed, it goes: the controls clear and the core stops waiting for
// commands. The last mounted shows, then the one before.
func TestDispose(t *testing.T) {
	newClock(t)
	outer := signal.New(true)
	inner := signal.New(true)
	plays := map[string]int{}
	h, np, r := mount(t, func() ui.Node {
		return ui.Show(outer, func() ui.Node {
			nowplaying.Show(nowplaying.Props{Title: ui.Val("Album"), OnPlay: func() { plays["album"]++ }})
			return ui.Show(inner, func() ui.Node {
				nowplaying.Show(nowplaying.Props{Title: ui.Val("Preview"), OnPlay: func() { plays["preview"]++ }})
				return ui.StaticText("x")
			})
		})
	})
	if np.Info.Title != "Preview" {
		t.Fatalf("showing %+v", np.Info)
	}
	np.Send("play", 0)
	inner.Set(false)
	settle(h, r)
	if np.Info == nil || np.Info.Title != "Album" || np.Clears != 0 {
		t.Fatalf("after the inner one: %+v, %d clears", np.Info, np.Clears)
	}
	np.Send("play", 0)
	if plays["preview"] != 1 || plays["album"] != 1 {
		t.Fatalf("plays %v", plays)
	}
	outer.Set(false)
	settle(h, r)
	if np.Info != nil || np.Clears != 1 {
		t.Fatalf("after both: %+v, %d clears", np.Info, np.Clears)
	}
	if np.Following() {
		t.Fatal("still waiting for commands")
	}
	np.Send("play", 0)
	if plays["album"] != 1 {
		t.Fatal("a command after disposal ran")
	}
	// Shown again: it follows again.
	outer.Set(true)
	settle(h, r)
	if np.Info == nil || !np.Following() {
		t.Fatalf("shown again: %+v, following %v", np.Info, np.Following())
	}
	np.Send("play", 0)
	if plays["album"] != 2 {
		t.Fatalf("plays %v", plays)
	}
}

// fakePlayer is a host's "audio" module for one player.
type fakePlayer struct {
	calls    []string
	reads    int
	position float64
	duration float64
	playing  bool
	ended    bool
}

func (p *fakePlayer) module(method string, args []byte, reply func([]byte, error)) {
	n := binary.LittleEndian.Uint32(args)
	rest := args[4+n:]
	switch method {
	case "status":
		p.reads++
		le := binary.LittleEndian
		b := le.AppendUint64(nil, math.Float64bits(p.position))
		b = le.AppendUint64(b, math.Float64bits(p.duration))
		b = append(b, b2u(p.playing), b2u(p.ended))
		reply(b, nil)
		return
	case "play":
		p.playing, p.ended = true, false
	case "pause":
		p.playing = false
	case "seek":
		p.position = math.Float64frombits(binary.LittleEndian.Uint64(rest))
		p.calls = append(p.calls, "seek "+fmtF(p.position))
		reply(nil, nil)
		return
	}
	p.calls = append(p.calls, method)
	reply(nil, nil)
}

func b2u(b bool) byte {
	if b {
		return 1
	}
	return 0
}

func fmtF(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// With a Player, its status shows (read once a second) and the commands
// run on it, showing at once.
func TestPlayer(t *testing.T) {
	c, tick := newClock(t)
	fp := &fakePlayer{duration: 180}
	h := fake.New()
	np := h.NowPlaying()
	h.AsyncModules["audio"] = fp.module
	var next int
	r := ui.Mount(h, func() ui.Node {
		music := audio.Load("music/spring.mp3")
		nowplaying.Show(nowplaying.Props{Title: ui.Val("Spring"), Player: music, OnNext: func() { next++ }, SkipInterval: 10})
		return ui.StaticText("x")
	})
	t.Cleanup(r.Dispose)
	settle(h, r)
	i := np.Info
	if i == nil || i.Duration != 180 || i.State != "paused" || i.Position != 0 {
		t.Fatalf("info %+v", i)
	}
	want := []string{"play", "pause", "togglePlayPause", "stop", "next", "seek", "skipForward", "skipBackward"}
	if !slices.Equal(i.Commands, want) {
		t.Fatalf("commands %v, want %v", i.Commands, want)
	}

	np.Send("togglePlayPause", 0)
	settle(h, r)
	if np.Info.State != "playing" || !fp.playing {
		t.Fatalf("after toggle: %+v, player playing %v", np.Info, fp.playing)
	}
	// It plays: the status read each second agrees with the system.
	sets := np.Sets
	for range 3 {
		c.add(1)
		fp.position += 1
		tick()
		settle(h, r)
	}
	if np.Sets != sets {
		t.Fatalf("following the player sent %d times", np.Sets-sets)
	}
	np.Send("skipForward", 0)
	settle(h, r)
	if fp.position != 13 || np.Info.Position != 13 {
		t.Fatalf("skip: player at %v, shown at %v", fp.position, np.Info.Position)
	}
	np.Send("seek", 500) // past the end: the end
	settle(h, r)
	if fp.position != 180 {
		t.Fatalf("seek past the end: %v", fp.position)
	}
	np.Send("seek", 60)
	np.Send("pause", 0)
	settle(h, r)
	if np.Info.State != "paused" || np.Info.Position != 60 || fp.playing {
		t.Fatalf("after pause: %+v", np.Info)
	}
	np.Send("next", 0)
	if next != 1 {
		t.Fatal("next didn't run")
	}
	// The app plays it itself: shown within a second.
	fp.playing = true
	c.add(1)
	tick()
	settle(h, r)
	if np.Info.State != "playing" {
		t.Fatalf("app's play: %+v", np.Info)
	}
	// It ends: paused at the end.
	fp.playing, fp.ended, fp.position = false, true, 180
	c.add(1)
	tick()
	settle(h, r)
	if np.Info.State != "paused" || np.Info.Position != 180 {
		t.Fatalf("ended: %+v", np.Info)
	}
	np.Send("stop", 0)
	settle(h, r)
	if fp.position != 0 || np.Info.Position != 0 {
		t.Fatalf("stop: player %v, shown %+v", fp.position, np.Info)
	}
	// Disposed, it no longer reads the status.
	r.Dispose()
	n := fp.reads
	c.add(5)
	tick()
	h.RunPosted()
	if fp.reads != n {
		t.Fatalf("%d status reads after disposal", fp.reads-n)
	}
}
