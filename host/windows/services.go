//go:build windows

package windows

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	. "github.com/egoist/asuka/internal/windows"
)

// Native modules backed by Windows's services: the share UI (share), the
// network (network, the Network List Manager), speech (SAPI), location (WinRT's
// Geolocator) and audio (MCI). Each answers with an error saying why when
// Windows can't.

const (
	wmSpeech = WM_APP + 9 // SAPI's events (sapi.go)
)

// services is their state. Main thread.
type services struct {
	net      *NetworkList
	netState []byte
	netNext  []netWait

	voice  *Voice
	spoken map[uint32]func([]byte, error) // speech by its stream

	notes *notifier // notify.go

	geo      *Geolocator
	geoNext  []func([]byte, error)
	geoFixes [][]byte // positions no one asked for yet

	players map[string]*mciPlayer
	aliases int
}

type netWait struct {
	last  []byte
	reply func([]byte, error)
}

// servicesModule is the module name's methods, if it's one of these.
func (h *Host) servicesModule(name string) map[string]asyncMethod {
	switch name {
	case "share":
		return map[string]asyncMethod{"files": h.shareFiles}
	case "network":
		return h.networkModule()
	case "speech":
		return h.speechModule()
	case "location":
		return h.locationModule()
	case "audio":
		return h.audioModule()
	}
	return nil
}

// Share.

// inUseWindow is the top-level window the user works in.
func (h *Host) inUseWindow() uintptr {
	if h.inUse != 0 && IsWindowVisible(h.inUse) {
		return h.inUse
	}
	return h.window
}

// shareText shows Windows's share UI for text, a link if it's one; where
// there's none (before Windows 10) the text is copied.
func (h *Host) shareText(text string) {
	s := Share{Title: h.appName, Text: text}
	if u, err := url.Parse(strings.TrimSpace(text)); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && !strings.ContainsAny(strings.TrimSpace(text), " \n") {
		s.Text, s.Link = "", strings.TrimSpace(text)
	}
	ShowShare(h.inUseWindow(), s, func(err error) {
		if err != nil {
			fmt.Fprintf(os.Stderr, "asuka: the share UI: %v; the text is copied instead\n", err)
			SetClipboardText(h.window, text)
		}
	})
}

// shareFiles is the "share" module's "files" (args: u32 view, string
// MIME, string UTI, string title, u32 count, then the paths): Windows's
// share UI for the files; it answers once the UI shows (Windows doesn't
// say when it's done).
func (h *Host) shareFiles(args []byte, reply func([]byte, error)) {
	if len(args) < 20 {
		reply(nil, errors.New("share.files: bad arguments"))
		return
	}
	rest := args[4:]
	_, rest = readString(rest) // MIME
	_, rest = readString(rest) // UTI
	title, rest := readString(rest)
	if len(rest) < 4 {
		reply(nil, errors.New("share.files: bad arguments"))
		return
	}
	n := int(binary.LittleEndian.Uint32(rest))
	rest = rest[4:]
	var paths []string
	for i := 0; i < n && len(rest) >= 4; i++ {
		var p string
		p, rest = readString(rest)
		if _, err := os.Stat(p); err != nil {
			reply(nil, fmt.Errorf("no file at %s", p))
			return
		}
		paths = append(paths, p)
	}
	if title == "" {
		title = filepath.Base(paths[0])
	}
	ShowShare(h.inUseWindow(), Share{Title: title, Files: paths}, func(err error) {
		h.Post(func() { reply(nil, err) })
	})
}

// Network.

func (h *Host) startNetwork() error {
	s := &h.svc
	if s.net != nil {
		return nil
	}
	l, err := NewNetworkList(h.networkChanged)
	if err != nil {
		return fmt.Errorf("asuka/sdk/network: %w", err)
	}
	s.net = l
	s.netState = encodeNet(l.State())
	return nil
}

func encodeNet(st NetState) []byte {
	return []byte{b2u(st.Connected), st.Type, b2u(st.Metered), b2u(st.Constrained)}
}

func b2u(b bool) byte {
	if b {
		return 1
	}
	return 0
}

// networkChanged answers those waiting for the state to change.
func (h *Host) networkChanged() {
	s := &h.svc
	if s.net == nil {
		return
	}
	s.netState = encodeNet(s.net.State())
	var keep []netWait
	for _, w := range s.netNext {
		if string(w.last) != string(s.netState) {
			w.reply(s.netState, nil)
		} else {
			keep = append(keep, w)
		}
	}
	s.netNext = keep
}

// networkModule is asuka/sdk/network's: "current", and, while watching, "next"
// (answered once the state isn't the one given) and "stop".
func (h *Host) networkModule() map[string]asyncMethod {
	s := &h.svc
	return map[string]asyncMethod{
		"current": func(_ []byte, reply func([]byte, error)) {
			if err := h.startNetwork(); err != nil {
				reply(nil, err)
				return
			}
			s.netState = encodeNet(s.net.State())
			reply(s.netState, nil)
		},
		"next": func(args []byte, reply func([]byte, error)) {
			if err := h.startNetwork(); err != nil {
				reply(nil, err)
				return
			}
			if len(args) == 0 || string(args) != string(s.netState) {
				reply(s.netState, nil)
				return
			}
			s.netNext = append(s.netNext, netWait{last: append([]byte(nil), args...), reply: reply})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			ws := s.netNext
			s.netNext = nil
			for _, w := range ws {
				w.reply(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// Speech.

func (h *Host) speechVoice() (*Voice, error) {
	s := &h.svc
	if s.voice == nil {
		v, err := NewVoice(h.msgWin, wmSpeech)
		if err != nil {
			return nil, fmt.Errorf("asuka/sdk/speech: %w", err)
		}
		s.voice, s.spoken = v, map[uint32]func([]byte, error){}
	}
	return s.voice, nil
}

// speechEvents answers the speech whose streams ended.
func (h *Host) speechEvents() {
	s := &h.svc
	if s.voice == nil {
		return
	}
	for _, stream := range s.voice.Ended() {
		if r := s.spoken[stream]; r != nil {
			delete(s.spoken, stream)
			r(nil, nil)
		}
	}
}

// speechModule is asuka/sdk/speech's: "speak" (JSON: text, language, voice,
// pitch, rate, volume) answers once spoken, or fails with "stopped";
// "stop", "pause", "resume", "speaking" (u8), "voices" (JSON). The voice's
// pitch is SAPI's XML's; its rate and volume the voice's.
func (h *Host) speechModule() map[string]asyncMethod {
	s := &h.svc
	ok := func(reply func([]byte, error)) { reply(nil, nil) }
	return map[string]asyncMethod{
		"speak": func(args []byte, reply func([]byte, error)) {
			var o struct {
				Text, Language, Voice string
				Pitch, Rate, Volume   float64
			}
			if err := json.Unmarshal(args, &o); err != nil {
				reply(nil, errors.New("bad args"))
				return
			}
			v, err := h.speechVoice()
			if err != nil {
				reply(nil, err)
				return
			}
			id := o.Voice
			if id == "" && o.Language != "" {
				id = voiceFor(o.Language)
			}
			if id == "" || !v.SetVoice(id) {
				v.SetVoice("") // the default
			}
			if o.Rate <= 0 {
				o.Rate = 1
			}
			// SAPI's rate is -10 to 10, 10 three times as fast.
			v.SetRate(int(math.Round(10 * math.Log(o.Rate) / math.Log(3))))
			v.SetVolume(int(math.Round(min(max(o.Volume, 0), 1) * 100)))
			pitch := 0
			if o.Pitch > 0 {
				pitch = min(max(int(math.Round(10*math.Log2(o.Pitch))), -10), 10)
			}
			text := fmt.Sprintf(`<pitch absmiddle="%d">%s</pitch>`, pitch, html.EscapeString(o.Text))
			stream, err := v.Speak(text, true, false)
			if err != nil {
				reply(nil, err)
				return
			}
			s.spoken[stream] = reply
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			if s.voice != nil {
				s.voice.Stop()
				rs := s.spoken
				s.spoken = map[uint32]func([]byte, error){}
				for _, r := range rs {
					r(nil, errors.New("stopped"))
				}
			}
			ok(reply)
		},
		"pause": func(_ []byte, reply func([]byte, error)) {
			if v, err := h.speechVoice(); err == nil {
				v.Pause()
			}
			ok(reply)
		},
		"resume": func(_ []byte, reply func([]byte, error)) {
			if v, err := h.speechVoice(); err == nil {
				v.Resume()
			}
			ok(reply)
		},
		"speaking": func(_ []byte, reply func([]byte, error)) {
			reply([]byte{b2u(s.voice != nil && len(s.spoken) > 0)}, nil)
		},
		"voices": func(_ []byte, reply func([]byte, error)) {
			type voice struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Language string `json:"language"`
				Quality  int    `json:"quality"`
				Network  bool   `json:"network"`
			}
			vs := []voice{}
			for _, v := range Voices() {
				vs = append(vs, voice{ID: v.ID, Name: v.Name, Language: v.Language})
			}
			b, err := json.Marshal(vs)
			reply(b, err)
		},
	}
}

// voiceFor is the ID of an installed voice of a language (a BCP 47 tag):
// of the language and region, else of the language.
func voiceFor(lang string) string {
	lang = strings.ToLower(lang)
	base, _, _ := strings.Cut(lang, "-")
	match := ""
	for _, v := range Voices() {
		l := strings.ToLower(v.Language)
		if l == lang {
			return v.ID
		}
		if b, _, _ := strings.Cut(l, "-"); b == base && match == "" {
			match = v.ID
		}
	}
	return match
}

// Location.

// locationModule is asuka/sdk/location's: "current" (asking for permission
// first), "next" (each position as the device moves, while watched),
// "stop"; "name" and "places" are OpenStreetMap's (geocode.go).
// Positions are JSON: lat, lon, acc, alt, t.
func (h *Host) locationModule() map[string]asyncMethod {
	s := &h.svc
	start := func(then func(error)) {
		RequestLocation(func(allowed bool, err error) {
			h.Post(func() {
				switch {
				case err != nil:
					then(fmt.Errorf("location: %w", err))
				case !allowed:
					then(errors.New("location: denied (Settings, Privacy & security, Location)"))
				case s.geo == nil:
					g, err := NewGeolocator()
					s.geo = g
					then(err)
				default:
					then(nil)
				}
			})
		})
	}
	encode := func(f Fix) []byte {
		b, _ := json.Marshal(f)
		return b
	}
	return map[string]asyncMethod{
		"current": func(_ []byte, reply func([]byte, error)) {
			start(func(err error) {
				if err != nil {
					reply(nil, err)
					return
				}
				s.geo.Current(func(f Fix, err error) {
					h.Post(func() {
						if errors.Is(err, ErrLocationDenied) {
							err = errors.New("location: denied")
						}
						if err != nil {
							reply(nil, err)
							return
						}
						reply(encode(f), nil)
					})
				})
			})
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			if len(s.geoFixes) > 0 {
				b := s.geoFixes[0]
				s.geoFixes = s.geoFixes[1:]
				reply(b, nil)
				return
			}
			s.geoNext = append(s.geoNext, reply)
			start(func(err error) {
				if err != nil {
					rs := s.geoNext
					s.geoNext = nil
					for _, r := range rs {
						r(nil, err)
					}
					return
				}
				s.geo.Watch(func(f Fix) {
					b := encode(f)
					h.Post(func() {
						if len(s.geoNext) == 0 {
							s.geoFixes = append(s.geoFixes[max(len(s.geoFixes)-7, 0):], b)
							return
						}
						r := s.geoNext[0]
						s.geoNext = s.geoNext[1:]
						r(b, nil)
					})
				})
			})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			if s.geo != nil {
				s.geo.Stop()
			}
			rs := s.geoNext
			s.geoNext, s.geoFixes = nil, nil
			for _, r := range rs {
				r(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
		"name":   h.placeNameMethod, // geocode.go
		"places": h.placesMethod,
	}
}
