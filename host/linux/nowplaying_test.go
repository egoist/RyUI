//go:build linux && !android

package linux

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/nowplaying"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// What shows is an MPRIS player on the session bus, under its own name:
// a client (the test's connection, and gdbus, as the desktops' media
// controls and playerctl are) reads its metadata and state and drives it
// with the player's methods, which reach the handlers enabled; disposed,
// its name goes.
func TestNowPlayingMPRIS(t *testing.T) {
	dir := t.TempDir()
	cover := filepath.Join(dir, "cover.png")
	os.WriteFile(cover, []byte("\x89PNG"), 0o644)
	shown := signal.New(true)
	state := signal.New(nowplaying.Paused)
	title := signal.New("Spring")
	var got []string
	f := func(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }
	h, r := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			nowplaying.Show(nowplaying.Props{
				Title:          title,
				Artist:         ui.Val("Vivaldi"),
				Album:          ui.Val("The Four Seasons"),
				Artwork:        ui.Val(cover),
				Duration:       ui.Val(200.0),
				Position:       ui.Val(12.0),
				State:          state,
				OnPlay:         func() { got = append(got, "play"); state.Set(nowplaying.Playing) },
				OnPause:        func() { got = append(got, "pause"); state.Set(nowplaying.Paused) },
				OnSeek:         func(at float64) { got = append(got, "seek "+f(at)) },
				OnSkipForward:  func(s float64) { got = append(got, "forward "+f(s)) },
				OnSkipBackward: func(s float64) { got = append(got, "back "+f(s)) },
				SkipInterval:   30,
			})
			return ui.StaticText("x")
		})
	})
	var name string
	settle(t, h, func() bool {
		if h.np == nil || h.np.conn == 0 {
			return false
		}
		name = h.np.name
		return true
	})
	owned := func() bool {
		v, err := busCall(t, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "NameHasOwner", func() Ptr { return VTuple(VStr(name)) })
		return err == nil && v.([]any)[0] == true
	}
	for i := 0; i < 100 && !owned(); i++ {
		settle(t, h, func() bool { return true })
	}
	if !strings.HasPrefix(name, "org.mpris.MediaPlayer2.") || !owned() {
		t.Fatalf("the player's name %q isn't owned (%v)", name, h.np.ownedErr)
	}
	get := func(iface, prop string) any {
		v, err := busCall(t, name, mprisPath, "org.freedesktop.DBus.Properties", "Get", func() Ptr { return VTuple(VStr(iface), VStr(prop)) })
		if err != nil {
			t.Fatalf("Get %s: %v", prop, err)
		}
		return v.([]any)[0]
	}
	call := func(method string, params func() Ptr) {
		if _, err := busCall(t, name, mprisPath, mprisPlayer, method, params); err != nil {
			t.Fatalf("%s: %v", method, err)
		}
	}

	meta := get(mprisPlayer, "Metadata").(map[string]any)
	if meta["xesam:title"] != "Spring" || !slices.Equal(anyStrings(meta["xesam:artist"]), []string{"Vivaldi"}) ||
		meta["xesam:album"] != "The Four Seasons" || meta["mpris:length"] != int64(200e6) ||
		meta["mpris:artUrl"] != "file://"+cover || !strings.HasPrefix(meta["mpris:trackid"].(string), "/dev/asuka/track/") {
		t.Fatalf("metadata %v", meta)
	}
	for prop, want := range map[string]any{
		"PlaybackStatus": "Paused", "Position": int64(12e6), "Rate": 1.0,
		"CanPlay": true, "CanPause": true, "CanSeek": true, "CanGoNext": false, "CanGoPrevious": false, "CanControl": true,
	} {
		if v := get(mprisPlayer, prop); v != want {
			t.Errorf("%s: %v, want %v", prop, v, want)
		}
	}
	if v := get(mprisRoot, "Identity"); v != appName() {
		t.Errorf("Identity %v", v)
	}

	// gdbus, as another process: what the desktops read.
	if _, err := exec.LookPath("gdbus"); err == nil {
		out, err := exec.Command("gdbus", "call", "--session", "--dest", name, "--object-path", mprisPath,
			"--method", "org.freedesktop.DBus.Properties.GetAll", mprisPlayer).CombinedOutput()
		if err != nil || !strings.Contains(string(out), "'xesam:title': <'Spring'>") || !strings.Contains(string(out), "'PlaybackStatus': <'Paused'>") {
			t.Fatalf("gdbus GetAll: %v\n%s", err, out)
		}
		if out, err := exec.Command("gdbus", "call", "--session", "--dest", name, "--object-path", mprisPath,
			"--method", mprisPlayer+".Play").CombinedOutput(); err != nil {
			t.Fatalf("gdbus Play: %v\n%s", err, out)
		}
	} else {
		t.Log("no gdbus: Play from the test's connection")
		call("Play", nil)
	}
	settle(t, h, func() bool { return len(got) == 1 && h.np.info.State == "playing" })
	if v := get(mprisPlayer, "PlaybackStatus"); v != "Playing" {
		t.Fatalf("PlaybackStatus after Play: %v", v)
	}
	call("PlayPause", nil)                                                     // playing: pause
	call("Seek", func() Ptr { return VTuple(VI64(30e6)) })                     // a skip forward
	call("Seek", func() Ptr { return VTuple(VI64(-10e6)) })                    // back, by what's asked
	call("SetPosition", func() Ptr { return VTuple(VPath("/nope"), VI64(1)) }) // another track: ignored
	track := meta["mpris:trackid"].(string)
	call("SetPosition", func() Ptr { return VTuple(VPath(track), VI64(42e6)) })
	call("Next", nil) // not enabled
	want := []string{"play", "pause", "forward 30", "back 10", "seek 42"}
	settle(t, h, func() bool { return len(got) >= len(want) })
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	// A new title is a new track.
	onMain(func() { title.Set("Summer"); r.Flush() })
	settle(t, h, func() bool { return h.np.info.Title == "Summer" })
	meta = get(mprisPlayer, "Metadata").(map[string]any)
	if meta["xesam:title"] != "Summer" || meta["mpris:trackid"] == track {
		t.Fatalf("after a change: %v", meta)
	}

	// Disposed, the name goes.
	onMain(func() { shown.Set(false); r.Flush() })
	settle(t, h, func() bool { return h.np.conn == 0 })
	for i := 0; i < 100 && owned(); i++ {
		settle(t, h, func() bool { return true })
	}
	if owned() {
		t.Fatal("the player's name stayed")
	}
	if h.np.follows {
		t.Fatal("still following commands")
	}
}

func anyStrings(v any) []string {
	var out []string
	for _, x := range v.([]any) {
		out = append(out, x.(string))
	}
	return out
}
