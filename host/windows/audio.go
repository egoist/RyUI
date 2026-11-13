//go:build windows

package windows

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	. "github.com/egoist/asuka/internal/windows"
)

// asuka/audio on Windows: each player an MCI mpegvideo device (DirectShow's
// playback), opened on its source's file (an asset, a path, a URL
// downloaded to the caches first). A play asks to be told when it ends
// (MM_MCINOTIFY, to the message window): looping plays again, playOnce
// lets the player go.

type mciPlayer struct {
	alias        string
	id           string
	open         bool
	loop, once   bool
	ended        bool
	playing      bool
	pending      []func() // what was asked before its file was open
	rate, volume float64
}

func (p *mciPlayer) do(f func()) {
	if p.open {
		f()
	} else {
		p.pending = append(p.pending, f)
	}
}

func (h *Host) mci(p *mciPlayer, cmd string, notify bool) string {
	var hwnd uintptr
	if notify {
		hwnd = h.msgWin
		cmd += " notify"
	}
	out, _ := MCI(strings.ReplaceAll(cmd, "@", p.alias), hwnd)
	return out
}

// audioModule is asuka/audio's: players by ID (each call's args begin with
// it, a u32-length string): load (the source), play, playOnce, pause,
// seek (f64 seconds), volume (f64), rate (f64), loop (u8), release;
// status answers f64 position, f64 duration, u8 playing, u8 ended.
func (h *Host) audioModule() map[string]asyncMethod {
	s := &h.svc
	if s.players == nil {
		s.players = map[string]*mciPlayer{}
	}
	split := func(b []byte) (string, []byte) {
		if len(b) < 4 || int(binary.LittleEndian.Uint32(b)) > len(b)-4 {
			return "", nil
		}
		n := int(binary.LittleEndian.Uint32(b))
		return string(b[4 : 4+n]), b[4+n:]
	}
	f64 := func(b []byte) float64 {
		if len(b) < 8 {
			return 0
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(b))
	}
	m := func(f func(id string, p *mciPlayer, rest []byte)) asyncMethod {
		return func(args []byte, reply func([]byte, error)) {
			id, rest := split(args)
			f(id, s.players[id], rest)
			reply(nil, nil)
		}
	}
	return map[string]asyncMethod{
		"load": func(args []byte, reply func([]byte, error)) {
			id, rest := split(args)
			h.loadAudio(id, string(rest), reply)
		},
		"play": m(func(_ string, p *mciPlayer, _ []byte) { h.playAudio(p) }),
		"playOnce": m(func(_ string, p *mciPlayer, _ []byte) {
			if p != nil {
				p.once = true
				h.playAudio(p)
			}
		}),
		"pause": m(func(_ string, p *mciPlayer, _ []byte) {
			if p != nil {
				p.do(func() {
					h.mci(p, "pause @", false)
					p.playing = false
				})
			}
		}),
		"seek": m(func(_ string, p *mciPlayer, rest []byte) {
			if p != nil {
				ms := int(f64(rest) * 1000)
				p.do(func() {
					p.ended = false
					was := p.playing
					h.mci(p, "seek @ to "+strconv.Itoa(max(ms, 0)), false)
					if was {
						h.mci(p, "play @", true)
					}
				})
			}
		}),
		"volume": m(func(_ string, p *mciPlayer, rest []byte) {
			if p != nil {
				p.volume = min(max(f64(rest), 0), 1)
				p.do(func() { h.mci(p, "setaudio @ volume to "+strconv.Itoa(int(p.volume*1000)), false) })
			}
		}),
		"loop": m(func(_ string, p *mciPlayer, rest []byte) {
			if p != nil {
				p.loop = len(rest) > 0 && rest[0] == 1
			}
		}),
		"rate": m(func(_ string, p *mciPlayer, rest []byte) {
			if p != nil {
				p.rate = f64(rest)
				p.do(func() { h.mci(p, "set @ speed "+strconv.Itoa(int(max(p.rate, 0.1)*1000)), false) })
			}
		}),
		"release": m(func(id string, _ *mciPlayer, _ []byte) { h.releaseAudio(id) }),
		"status": func(args []byte, reply func([]byte, error)) {
			id, _ := split(args)
			p := s.players[id]
			var pos, dur float64
			playing, ended := false, false
			if p != nil && p.open {
				pos, _ = strconv.ParseFloat(h.mci(p, "status @ position", false), 64)
				dur, _ = strconv.ParseFloat(h.mci(p, "status @ length", false), 64)
				pos, dur = pos/1000, dur/1000
				playing = h.mci(p, "status @ mode", false) == "playing"
				ended = p.ended
			}
			out := binary.LittleEndian.AppendUint64(nil, math.Float64bits(pos))
			out = binary.LittleEndian.AppendUint64(out, math.Float64bits(dur))
			out = append(out, b2u(playing), b2u(ended))
			reply(out, nil)
		},
	}
}

// loadAudio opens a player on source's file, once it's local.
func (h *Host) loadAudio(id, src string, reply func([]byte, error)) {
	s := &h.svc
	h.releaseAudio(id)
	s.aliases++
	p := &mciPlayer{alias: fmt.Sprintf("asuka%d", s.aliases), id: id, rate: 1, volume: 1}
	s.players[id] = p
	h.localFiles([]source{{Source: src}}, func(paths []string, err error) {
		if s.players[id] != p {
			reply(nil, nil) // released, or loaded again, meanwhile
			return
		}
		if err == nil {
			_, err = MCI(`open "`+paths[0]+`" type mpegvideo alias `+p.alias, 0)
		}
		if err != nil {
			delete(s.players, id)
			reply(nil, fmt.Errorf("asuka/audio: %s: %w", src, err))
			return
		}
		p.open = true
		h.mci(p, "set @ time format milliseconds", false)
		for _, f := range p.pending {
			f()
		}
		p.pending = nil
		reply(nil, nil)
	})
}

func (h *Host) playAudio(p *mciPlayer) {
	if p == nil {
		return
	}
	p.do(func() {
		if p.ended {
			p.ended = false
			h.mci(p, "seek @ to start", false)
		}
		p.playing = true
		h.mci(p, "play @", true)
	})
}

func (h *Host) releaseAudio(id string) {
	s := &h.svc
	if p := s.players[id]; p != nil {
		delete(s.players, id)
		if p.open {
			h.mci(p, "close @", false)
		}
	}
}

// audioNotified follows an MCI notification: a play that ended (looping:
// played again; once: let go).
func (h *Host) audioNotified(result uintptr, device uintptr) {
	if result != MCI_NOTIFY_SUCCESSFUL {
		return
	}
	for id, p := range h.svc.players {
		if !p.open || !p.playing {
			continue
		}
		if h.mci(p, "status @ mode", false) == "playing" {
			continue
		}
		pos, _ := strconv.ParseFloat(h.mci(p, "status @ position", false), 64)
		dur, _ := strconv.ParseFloat(h.mci(p, "status @ length", false), 64)
		if dur > 0 && pos < dur-50 {
			continue // paused or stopped, not at its end
		}
		switch {
		case p.loop:
			h.mci(p, "seek @ to start", false)
			h.mci(p, "play @", true)
		case p.once:
			h.releaseAudio(id)
		default:
			p.playing, p.ended = false, true
		}
	}
}
