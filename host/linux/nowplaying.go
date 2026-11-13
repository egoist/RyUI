//go:build linux && !android

package linux

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	spec "github.com/egoist/asuka/internal/nowplayingspec"
)

// asuka/nowplaying on Linux: an MPRIS player (org.mpris.MediaPlayer2 and
// its .Player) on the session bus, which GNOME Shell's and KDE Plasma's
// media controls, their lock screens, the desktops' media keys
// (gsd-media-keys, KDE's), and playerctl show and drive. The player is
// exported on a session bus connection of its own, with the bus name
// org.mpris.MediaPlayer2.<app>.instance<pid>; "clear" closes it, so the
// name goes and the desktops drop the player at once. Clients advance the
// position at the rate themselves (they read Position, which the host
// extrapolates, when they need it) and hear of jumps through Seeked. The
// module's methods are internal/nowplayingspec's.

const (
	mprisPath   = "/org/mpris/MediaPlayer2"
	mprisRoot   = "org.mpris.MediaPlayer2"
	mprisPlayer = "org.mpris.MediaPlayer2.Player"
)

const mprisXML = `<node>
<interface name="org.mpris.MediaPlayer2">
<method name="Raise"/>
<method name="Quit"/>
<property name="CanQuit" type="b" access="read"/>
<property name="CanRaise" type="b" access="read"/>
<property name="HasTrackList" type="b" access="read"/>
<property name="Identity" type="s" access="read"/>
<property name="DesktopEntry" type="s" access="read"/>
<property name="SupportedUriSchemes" type="as" access="read"/>
<property name="SupportedMimeTypes" type="as" access="read"/>
</interface>
<interface name="org.mpris.MediaPlayer2.Player">
<method name="Next"/>
<method name="Previous"/>
<method name="Pause"/>
<method name="PlayPause"/>
<method name="Stop"/>
<method name="Play"/>
<method name="Seek"><arg direction="in" name="Offset" type="x"/></method>
<method name="SetPosition"><arg direction="in" name="TrackId" type="o"/><arg direction="in" name="Position" type="x"/></method>
<method name="OpenUri"><arg direction="in" name="Uri" type="s"/></method>
<signal name="Seeked"><arg name="Position" type="x"/></signal>
<property name="PlaybackStatus" type="s" access="read"/>
<property name="Rate" type="d" access="read"/>
<property name="Metadata" type="a{sv}" access="read"/>
<property name="Volume" type="d" access="read"/>
<property name="Position" type="x" access="read"/>
<property name="MinimumRate" type="d" access="read"/>
<property name="MaximumRate" type="d" access="read"/>
<property name="CanGoNext" type="b" access="read"/>
<property name="CanGoPrevious" type="b" access="read"/>
<property name="CanPlay" type="b" access="read"/>
<property name="CanPause" type="b" access="read"/>
<property name="CanSeek" type="b" access="read"/>
<property name="CanControl" type="b" access="read"/>
</interface>
</node>`

// mpris is the player, main thread only.
type mpris struct {
	conn     Ptr // its own session bus connection; 0 while nothing shows
	regs     [2]uint32
	name     string
	info     *spec.Info
	at       time.Time // when info came
	track    int       // counts the tracks, for their ids
	artURL   string
	waiting  func([]byte, error)
	queue    []byte
	follows  bool
	ownedErr error // why the name isn't owned, if it isn't
}

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["nowPlaying"] = h.nowPlayingModule() })
}

func (h *Host) mpris() *mpris {
	if h.np == nil {
		h.np = &mpris{}
	}
	return h.np
}

func (h *Host) nowPlayingModule() map[string]method {
	return map[string]method{
		"set": func(args []byte, reply func([]byte, error)) {
			var info spec.Info
			if err := json.Unmarshal(args, &info); err != nil {
				reply(nil, err)
				return
			}
			reply(nil, h.mprisSet(&info))
		},
		"clear": func(_ []byte, reply func([]byte, error)) {
			h.mprisClose()
			reply(nil, nil)
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			p := h.mpris()
			if w := p.waiting; w != nil {
				w(nil, errors.New("stopped")) // one poll at a time
			}
			p.follows, p.waiting = true, reply
			p.flush()
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			p := h.mpris()
			w := p.waiting
			p.follows, p.waiting, p.queue = false, nil, nil
			if w != nil {
				w(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

func (p *mpris) flush() {
	if p.waiting == nil || len(p.queue) == 0 {
		return
	}
	w, b := p.waiting, p.queue
	p.waiting, p.queue = nil, nil
	w(b, nil)
}

// command hands Go a command the desktop sent, if enabled.
func (p *mpris) command(c spec.Command, v float64) {
	if !p.follows || p.info == nil || !p.info.Has(c) {
		return
	}
	p.queue = spec.AppendCommand(p.queue, c, v)
	p.flush()
}

// mprisSet shows info: the player is exported (and its name owned) when
// something first shows; after, what changed is announced.
func (h *Host) mprisSet(info *spec.Info) error {
	p := h.mpris()
	old := p.info
	expected := p.position()
	if old == nil || old.Title != info.Title || old.Artist != info.Artist || old.Album != info.Album || old.Duration != info.Duration {
		p.track++
	}
	p.info, p.at = info, time.Now()
	p.artURL = ""
	if info.Artwork != "" {
		p.artURL = h.audioURI(info.Artwork)
	}
	if p.conn == 0 {
		if err := h.mprisOpen(); err != nil {
			p.info = nil
			return err
		}
		return nil
	}
	p.changed(mprisPlayer, "PlaybackStatus", "Rate", "Metadata", "CanGoNext", "CanGoPrevious", "CanPlay", "CanPause", "CanSeek")
	// Clients advance the position at the rate, and hear only of jumps.
	if d := info.Position - expected; old != nil && math.Abs(d) > 0.25 {
		DBusEmit(p.conn, mprisPath, mprisPlayer, "Seeked", VTuple(VI64(micros(info.Position))))
	}
	return nil
}

// mprisOpen exports the player on a connection of its own and owns its
// name there.
func (h *Host) mprisOpen() error {
	p := h.mpris()
	conn, err := privateSessionBus()
	if err != nil {
		return err
	}
	for i, iface := range []string{mprisRoot, mprisPlayer} {
		reg, err := DBusRegister(conn, mprisPath, mprisXML, iface, DBusObject{
			Method:   func(c DBusInvocation) { h.mprisMethod(c) },
			Property: func(name string) Ptr { return p.property(name) },
		})
		if err != nil {
			if i == 1 {
				DBusUnregister(conn, p.regs[0])
			}
			closeBus(conn)
			return err
		}
		p.regs[i] = reg
	}
	p.conn = conn
	p.name = fmt.Sprintf("%s.%s.instance%d", mprisRoot, busNameElement(appName()), os.Getpid())
	// DBUS_NAME_FLAG_DO_NOT_QUEUE: another player of the same app and pid
	// can't be; the name is the connection's until it closes.
	DBusCall(conn, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName",
		VTuple(VStr(p.name), VU32(4)), 5000, func(_ Ptr, err error) { p.ownedErr = err })
	return nil
}

// mprisClose takes the player off the bus: its connection closes, and
// its name with it.
func (h *Host) mprisClose() {
	p := h.mpris()
	p.info = nil
	if p.conn == 0 {
		return
	}
	DBusUnregister(p.conn, p.regs[0])
	DBusUnregister(p.conn, p.regs[1])
	closeBus(p.conn)
	p.conn, p.regs, p.name = 0, [2]uint32{}, ""
}

// busNameElement makes s an element of a bus name: ASCII letters,
// digits, _ and -, not starting with a digit.
func busNameElement(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_', r == '-':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if b.Len() == 0 {
				b.WriteByte('_')
			}
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "asuka"
	}
	return b.String()
}

// position is where playback is now, in seconds.
func (p *mpris) position() float64 {
	if p.info == nil {
		return 0
	}
	return p.info.PositionAfter(time.Since(p.at).Seconds())
}

func micros(seconds float64) int64 { return int64(math.Round(seconds * 1e6)) }

func (p *mpris) trackID() string {
	return fmt.Sprintf("/dev/asuka/track/%d", p.track)
}

func (p *mpris) has(c spec.Command) bool { return p.info != nil && p.info.Has(c) }

// property is a property's value, a floating variant (0: none).
func (p *mpris) property(name string) Ptr {
	switch name {
	case "CanQuit", "HasTrackList":
		return VBool(false)
	case "CanRaise":
		return VBool(true)
	case "Identity":
		return VStr(appName())
	case "DesktopEntry":
		return VStr(appID(appName()))
	case "SupportedUriSchemes", "SupportedMimeTypes":
		return VStrings(nil)
	case "PlaybackStatus":
		s := "Stopped"
		if p.info != nil {
			switch p.info.State {
			case "playing":
				s = "Playing"
			case "paused":
				s = "Paused"
			}
		}
		return VStr(s)
	case "Rate":
		if p.info != nil && p.info.Rate > 0 {
			return VDouble(p.info.Rate)
		}
		return VDouble(1)
	case "Volume":
		return VDouble(1)
	case "MinimumRate":
		return VDouble(0.25)
	case "MaximumRate":
		return VDouble(4)
	case "Position":
		return VI64(micros(p.position()))
	case "Metadata":
		return p.metadata()
	case "CanGoNext":
		return VBool(p.has(spec.Next))
	case "CanGoPrevious":
		return VBool(p.has(spec.Previous))
	case "CanPlay":
		return VBool(p.has(spec.Play))
	case "CanPause":
		return VBool(p.has(spec.Pause))
	case "CanSeek":
		return VBool(p.has(spec.Seek) || p.has(spec.SkipForward) || p.has(spec.SkipBackward))
	case "CanControl":
		return VBool(true)
	}
	return 0
}

func (p *mpris) metadata() Ptr {
	m := map[string]Ptr{"mpris:trackid": VPath(p.trackID())}
	if i := p.info; i != nil {
		if i.Title != "" {
			m["xesam:title"] = VStr(i.Title)
		}
		if i.Artist != "" {
			m["xesam:artist"] = VStrings([]string{i.Artist})
		}
		if i.Album != "" {
			m["xesam:album"] = VStr(i.Album)
		}
		if i.Duration > 0 {
			m["mpris:length"] = VI64(micros(i.Duration))
		}
		if p.artURL != "" {
			m["mpris:artUrl"] = VStr(p.artURL)
		}
	}
	return VDict(m)
}

// changed emits PropertiesChanged with the properties' values.
func (p *mpris) changed(iface string, props ...string) {
	m := make(map[string]Ptr, len(props))
	for _, name := range props {
		m[name] = p.property(name)
	}
	DBusEmit(p.conn, mprisPath, "org.freedesktop.DBus.Properties", "PropertiesChanged",
		VTuple(VStr(iface), VDict(m), VStrings(nil)))
}

// mprisMethod answers a client's call: the player's commands go to Go.
func (h *Host) mprisMethod(c DBusInvocation) {
	p := h.mpris()
	switch c.Method {
	case "Raise":
		c.Return(0)
		h.afterCore(h.showMain)
		return
	case "Quit":
		c.Return(0)
		return
	case "OpenUri":
		c.Error("org.freedesktop.DBus.Error.NotSupported", "the player opens no URIs")
		return
	}
	args, _ := VariantGo(c.Params).([]any)
	c.Return(0)
	switch c.Method {
	case "Play":
		p.command(spec.Play, 0)
	case "Pause":
		p.command(spec.Pause, 0)
	case "PlayPause":
		p.command(spec.TogglePlayPause, 0)
	case "Stop":
		p.command(spec.Stop, 0)
	case "Next":
		p.command(spec.Next, 0)
	case "Previous":
		p.command(spec.Previous, 0)
	case "Seek":
		// By an offset, in µs: a skip where the app skips, else a seek.
		if len(args) != 1 {
			return
		}
		off, _ := args[0].(int64)
		s := float64(off) / 1e6
		switch {
		case s > 0 && p.has(spec.SkipForward):
			p.command(spec.SkipForward, s)
		case s < 0 && p.has(spec.SkipBackward):
			p.command(spec.SkipBackward, -s)
		case s != 0 && p.has(spec.Seek):
			to := max(p.position()+s, 0)
			if p.info.Duration > 0 && to > p.info.Duration {
				p.command(spec.Next, 0) // past the end: the next track, as the specification says
				return
			}
			p.command(spec.Seek, to)
		}
	case "SetPosition":
		// Ignored for another track, and out of the track.
		if len(args) != 2 {
			return
		}
		id, _ := args[0].(string)
		us, _ := args[1].(int64)
		s := float64(us) / 1e6
		if p.info != nil && id == p.trackID() && s >= 0 && (p.info.Duration == 0 || s <= p.info.Duration) {
			p.command(spec.Seek, s)
		}
	}
}
