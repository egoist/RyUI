//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"path/filepath"
	"strings"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/ryui/internal/darwin"
)

// More native modules on a Mac, as iOS has them: network (NWPathMonitor),
// speech (AVSpeechSynthesizer), audio (AVPlayer), biometric (Touch ID,
// LocalAuthentication) and review (the App Store's prompt).

// Network.

// netMonitor is ryui/network's: the path NWPathMonitor reports, and who
// waits for it.
type netMonitor struct {
	started bool
	path    []byte // the last state: u8 connected, type, metered, constrained
	current []func([]byte, error)
	next    []netWait
	block   objc.Block
}

type netWait struct {
	last  []byte
	reply func([]byte, error)
}

var nw struct {
	create         func() uintptr
	setHandler     func(m uintptr, block objc.Block)
	setQueue       func(m, q uintptr)
	start          func(m uintptr)
	status         func(p uintptr) int32
	usesType       func(p uintptr, t int32) bool
	isExpensive    func(p uintptr) bool
	isConstrained  func(p uintptr) bool
	mainQueue      uintptr
	loaded, failed bool
}

func loadNetwork() bool {
	if nw.loaded {
		return !nw.failed
	}
	nw.loaded = true
	lib := Dlopen("/System/Library/Frameworks/Network.framework/Network")
	sys := Dlopen("/usr/lib/libSystem.B.dylib")
	if lib == 0 || sys == 0 {
		nw.failed = true
		return false
	}
	q, err := purego.Dlsym(sys, "_dispatch_main_q")
	if err != nil {
		nw.failed = true
		return false
	}
	nw.mainQueue = q
	purego.RegisterLibFunc(&nw.create, lib, "nw_path_monitor_create")
	purego.RegisterLibFunc(&nw.setHandler, lib, "nw_path_monitor_set_update_handler")
	purego.RegisterLibFunc(&nw.setQueue, lib, "nw_path_monitor_set_queue")
	purego.RegisterLibFunc(&nw.start, lib, "nw_path_monitor_start")
	purego.RegisterLibFunc(&nw.status, lib, "nw_path_get_status")
	purego.RegisterLibFunc(&nw.usesType, lib, "nw_path_uses_interface_type")
	purego.RegisterLibFunc(&nw.isExpensive, lib, "nw_path_is_expensive")
	purego.RegisterLibFunc(&nw.isConstrained, lib, "nw_path_is_constrained")
	return true
}

// encodePath is a path as ryui/network's reply.
func encodePath(p uintptr) []byte {
	const satisfied = 1
	const other, wifi, cellular, wired = 0, 1, 2, 3
	if nw.status(p) != satisfied {
		return []byte{0, 0, 0, 0}
	}
	t := byte(4)
	switch {
	case nw.usesType(p, wifi):
		t = 1
	case nw.usesType(p, cellular):
		t = 2
	case nw.usesType(p, wired):
		t = 3
	}
	return []byte{1, t, b2u(nw.isExpensive(p)), b2u(nw.isConstrained(p))}
}

func (h *Host) startNetwork() error {
	m := &h.net
	if m.started {
		return nil
	}
	if !loadNetwork() {
		return errors.New("ryui/network: the Network framework isn't there")
	}
	m.started = true
	mon := nw.create()
	// Called on the main queue, which the main run loop serves.
	m.block = NewBlock(func(_ objc.Block, path uintptr) {
		s := encodePath(path)
		m.path = s
		cs := m.current
		m.current = nil
		for _, c := range cs {
			c(s, nil)
		}
		var keep []netWait
		for _, w := range m.next {
			if string(w.last) != string(s) {
				w.reply(s, nil)
			} else {
				keep = append(keep, w)
			}
		}
		m.next = keep
	})
	nw.setHandler(mon, m.block)
	nw.setQueue(mon, nw.mainQueue)
	nw.start(mon)
	return nil
}

// networkModule is ryui/network's: "current", and, while watching, "next"
// (answered once the state isn't the one given) and "stop".
func (h *Host) networkModule() map[string]method {
	m := &h.net
	return map[string]method{
		"current": func(_ []byte, reply func([]byte, error)) {
			if err := h.startNetwork(); err != nil {
				reply(nil, err)
				return
			}
			if m.path != nil {
				reply(m.path, nil)
				return
			}
			m.current = append(m.current, reply)
		},
		"next": func(args []byte, reply func([]byte, error)) {
			if err := h.startNetwork(); err != nil {
				reply(nil, err)
				return
			}
			if m.path == nil {
				m.current = append(m.current, reply)
				return
			}
			if len(args) == 0 || string(args) != string(m.path) {
				reply(m.path, nil)
				return
			}
			m.next = append(m.next, netWait{last: append([]byte(nil), args...), reply: reply})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			ws := m.next
			m.next = nil
			for _, w := range ws {
				w.reply(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

// Speech.

type speech struct {
	synth   ID
	replies map[ID]func([]byte, error) // by utterance
}

var avFoundation uintptr

func loadAVFoundation() uintptr {
	if avFoundation == 0 {
		avFoundation = Dlopen("/System/Library/Frameworks/AVFoundation.framework/AVFoundation")
	}
	return avFoundation
}

// AVSpeechUtterance's rates: AVSpeechUtteranceMinimumSpeechRate,
// DefaultSpeechRate and MaximumSpeechRate.
const minRate, defaultRate, maxRate = 0, 0.5, 1

// speechModule is ryui/speech's: "speak" (JSON: text, language, voice,
// pitch, rate, volume) answers once spoken, or fails with "stopped";
// "stop", "pause", "resume", "speaking" (u8), "voices" (JSON).
func (h *Host) speechModule() map[string]method {
	s := &h.speech
	synth := func() ID {
		if s.synth == 0 {
			loadAVFoundation()
			s.synth = New("AVSpeechSynthesizer")
			Send(s.synth, "setDelegate:", uintptr(h.target))
			s.replies = map[ID]func([]byte, error){}
		}
		return s.synth
	}
	ok := func(reply func([]byte, error)) { reply(nil, nil) }
	return map[string]method{
		"speak": func(args []byte, reply func([]byte, error)) {
			var o struct {
				Text, Language, Voice string
				Pitch, Rate, Volume   float64
			}
			if err := json.Unmarshal(args, &o); err != nil {
				reply(nil, errors.New("bad args"))
				return
			}
			sy := synth()
			u := Retain(Send(Class("AVSpeechUtterance"), "speechUtteranceWithString:", uintptr(String(o.Text))))
			var voice ID
			if o.Voice != "" {
				voice = Send(Class("AVSpeechSynthesisVoice"), "voiceWithIdentifier:", uintptr(String(o.Voice)))
			}
			if voice == 0 && o.Language != "" {
				voice = Send(Class("AVSpeechSynthesisVoice"), "voiceWithLanguage:", uintptr(String(o.Language)))
			}
			if voice != 0 {
				Send(u, "setVoice:", uintptr(voice))
			}
			MsgSetFloat32(u, Sel("setPitchMultiplier:"), float32(min(max(o.Pitch, 0.5), 2)))
			rate := min(max(float32(o.Rate)*defaultRate, minRate), maxRate)
			MsgSetFloat32(u, Sel("setRate:"), rate)
			MsgSetFloat32(u, Sel("setVolume:"), float32(min(max(o.Volume, 0), 1)))
			s.replies[u] = reply
			Send(sy, "speakUtterance:", uintptr(u))
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			rs := s.replies
			s.replies = map[ID]func([]byte, error){}
			if s.synth != 0 {
				Send(s.synth, "stopSpeakingAtBoundary:", 0) // immediate
			}
			for u, r := range rs {
				Release(u)
				r(nil, errors.New("stopped"))
			}
			ok(reply)
		},
		"pause": func(_ []byte, reply func([]byte, error)) {
			Send(synth(), "pauseSpeakingAtBoundary:", 1) // at the word
			ok(reply)
		},
		"resume": func(_ []byte, reply func([]byte, error)) {
			Send(synth(), "continueSpeaking")
			ok(reply)
		},
		"speaking": func(_ []byte, reply func([]byte, error)) {
			sy := synth()
			reply([]byte{b2u(SendBool(sy, "isSpeaking") || SendBool(sy, "isPaused"))}, nil)
		},
		"voices": func(_ []byte, reply func([]byte, error)) {
			loadAVFoundation()
			type voice struct {
				ID       string `json:"id"`
				Name     string `json:"name"`
				Language string `json:"language"`
				Quality  int    `json:"quality"`
				Network  bool   `json:"network"`
			}
			vs := []voice{}
			for _, v := range ArrayItems(Send(Class("AVSpeechSynthesisVoice"), "speechVoices")) {
				q := 0
				switch SendInt(v, "quality") { // 1 default, 2 enhanced, 3 premium
				case 2:
					q = 1
				case 3:
					q = 2
				}
				vs = append(vs, voice{ID: GoString(Send(v, "identifier")), Name: GoString(Send(v, "name")), Language: GoString(Send(v, "language")), Quality: q})
			}
			b, err := json.Marshal(vs)
			reply(b, err)
		},
	}
}

// spoken ends an utterance: spoken through, or cancelled.
func (h *Host) spoken(u ID, done bool) {
	r := h.speech.replies[u]
	if r == nil {
		return
	}
	delete(h.speech.replies, u)
	Release(u)
	if done {
		r(nil, nil)
	} else {
		r(nil, errors.New("stopped"))
	}
}

// Audio.

type audioPlayer struct {
	player, item ID
	loop, once   bool
	ended        bool
	rate         float32
}

var (
	msgCMTime func(obj ID, sel SEL) cmTime
	msgSeekTo func(obj ID, sel SEL, t, before, after cmTime)
)

func cmSeconds(t cmTime) float64 {
	const valid, indefinite = 1, 1 << 4
	if t.Flags&valid == 0 || t.Flags&indefinite != 0 || t.Timescale <= 0 {
		return 0
	}
	return float64(t.Value) / float64(t.Timescale)
}

// audioModule is ryui/audio's: players by ID (each call's args begin with
// it, a u32-length string): load (the source), play, playOnce, pause,
// seek (f64 seconds), volume (f64), rate (f64), loop (u8), release;
// status answers f64 position, f64 duration, u8 playing, u8 ended.
func (h *Host) audioModule() map[string]method {
	if msgCMTime == nil {
		loadAVFoundation()
		purego.RegisterFunc(&msgCMTime, MsgSendStretAddr())
		purego.RegisterFunc(&msgSeekTo, MsgSendAddr())
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
	if h.players == nil {
		h.players = map[string]*audioPlayer{}
	}
	m := func(f func(id string, p *audioPlayer, rest []byte)) method {
		return func(args []byte, reply func([]byte, error)) {
			id, rest := split(args)
			f(id, h.players[id], rest)
			reply(nil, nil)
		}
	}
	return map[string]method{
		"load": m(func(id string, _ *audioPlayer, rest []byte) { h.loadAudio(id, string(rest)) }),
		"play": m(func(id string, p *audioPlayer, _ []byte) { h.playAudio(p) }),
		"playOnce": m(func(id string, p *audioPlayer, _ []byte) {
			if p != nil {
				p.once = true
				h.playAudio(p)
			}
		}),
		"pause": m(func(id string, p *audioPlayer, _ []byte) {
			if p != nil {
				Send(p.player, "pause")
			}
		}),
		"seek": m(func(id string, p *audioPlayer, rest []byte) {
			if p != nil {
				p.ended = false
				msgSeekTo(p.player, Sel("seekToTime:toleranceBefore:toleranceAfter:"), cmTime{Value: int64(f64(rest) * 600), Timescale: 600, Flags: 1}, cmTime{Flags: 1, Timescale: 1}, cmTime{Flags: 1, Timescale: 1})
			}
		}),
		"volume": m(func(id string, p *audioPlayer, rest []byte) {
			if p != nil {
				MsgSetFloat32(p.player, Sel("setVolume:"), float32(f64(rest)))
			}
		}),
		"loop": m(func(id string, p *audioPlayer, rest []byte) {
			if p != nil {
				p.loop = len(rest) > 0 && rest[0] == 1
			}
		}),
		"rate": m(func(id string, p *audioPlayer, rest []byte) {
			if p != nil {
				p.rate = float32(f64(rest))
				if MsgFloat32(p.player, Sel("rate")) > 0 {
					MsgSetFloat32(p.player, Sel("setRate:"), p.rate)
				}
			}
		}),
		"release": m(func(id string, p *audioPlayer, _ []byte) { h.releaseAudio(id) }),
		"status": func(args []byte, reply func([]byte, error)) {
			id, _ := split(args)
			p := h.players[id]
			out := make([]byte, 0, 18)
			var pos, dur float64
			playing, ended := false, false
			if p != nil {
				pos = cmSeconds(msgCMTime(p.player, Sel("currentTime")))
				dur = cmSeconds(msgCMTime(p.item, Sel("duration")))
				playing = SendInt(p.player, "timeControlStatus") == 2
				ended = p.ended
			}
			out = binary.LittleEndian.AppendUint64(out, math.Float64bits(pos))
			out = binary.LittleEndian.AppendUint64(out, math.Float64bits(dur))
			out = append(out, b2u(playing), b2u(ended))
			reply(out, nil)
		},
	}
}

func (h *Host) loadAudio(id, source string) {
	h.releaseAudio(id)
	var u ID
	switch {
	case strings.Contains(source, "://"):
		u = URL(source)
	case filepath.IsAbs(source):
		u = FileURL(source)
	default:
		abs, _ := filepath.Abs(filepath.Join(h.assets, filepath.FromSlash(source)))
		u = FileURL(abs)
	}
	if u == 0 {
		return
	}
	item := Retain(Send(Class("AVPlayerItem"), "playerItemWithURL:", uintptr(u)))
	// A voice keeps its pitch at any rate.
	Send(item, "setAudioTimePitchAlgorithm:", uintptr(String("AVAudioTimePitchAlgorithmTimeDomain")))
	p := &audioPlayer{item: item, player: Retain(Send(Class("AVPlayer"), "playerWithPlayerItem:", uintptr(item))), rate: 1}
	h.players[id] = p
	center := Send(Class("NSNotificationCenter"), "defaultCenter")
	Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("audioEnded:")),
		uintptr(String("AVPlayerItemDidPlayToEndTimeNotification")), uintptr(item))
}

func (h *Host) playAudio(p *audioPlayer) {
	if p == nil {
		return
	}
	if p.ended {
		p.ended = false
		msgSeekTo(p.player, Sel("seekToTime:toleranceBefore:toleranceAfter:"), cmTime{Flags: 1, Timescale: 600}, cmTime{Flags: 1, Timescale: 1}, cmTime{Flags: 1, Timescale: 1})
	}
	MsgSetFloat32(p.player, Sel("playImmediatelyAtRate:"), p.rate)
}

// audioEnded is a player's item played to its end.
func (h *Host) audioEnded(item ID) {
	for id, p := range h.players {
		if p.item != item {
			continue
		}
		switch {
		case p.loop:
			msgSeekTo(p.player, Sel("seekToTime:toleranceBefore:toleranceAfter:"), cmTime{Flags: 1, Timescale: 600}, cmTime{Flags: 1, Timescale: 1}, cmTime{Flags: 1, Timescale: 1})
			MsgSetFloat32(p.player, Sel("playImmediatelyAtRate:"), p.rate)
		case p.once:
			h.releaseAudio(id)
		default:
			p.ended = true
		}
		return
	}
}

func (h *Host) releaseAudio(id string) {
	p := h.players[id]
	if p == nil {
		return
	}
	delete(h.players, id)
	Send(p.player, "pause")
	Send(Send(Class("NSNotificationCenter"), "defaultCenter"), "removeObserver:name:object:", uintptr(h.target), 0, uintptr(p.item))
	Release(p.player)
	Release(p.item)
}

// Biometric.

// biometricModule is ryui/biometric's: "check" answers u8 kind (none,
// fingerprint...), u8 enrolled, u8 passcode; "authenticate" (u8 flags: 1
// no password; string reason; string cancel) shows Touch ID's prompt, and
// fails with "canceled", "not available" or "failed".
func (h *Host) biometricModule() map[string]method {
	load := func() bool {
		return Dlopen("/System/Library/Frameworks/LocalAuthentication.framework/LocalAuthentication") != 0 && HasClass("LAContext")
	}
	const biometrics, owner = 1, 2 // LAPolicy
	return map[string]method{
		"check": func(_ []byte, reply func([]byte, error)) {
			if !load() {
				reply([]byte{0, 0, 0}, nil)
				return
			}
			c := New("LAContext")
			defer Release(c)
			enrolled := SendBool(c, "canEvaluatePolicy:error:", biometrics, 0)
			kind := byte(0)
			if SendInt(c, "biometryType") == 1 { // touchID
				kind = 1
			}
			c2 := New("LAContext")
			defer Release(c2)
			passcode := SendBool(c2, "canEvaluatePolicy:error:", owner, 0)
			reply([]byte{kind, b2u(enrolled), b2u(passcode)}, nil)
		},
		"authenticate": func(args []byte, reply func([]byte, error)) {
			if len(args) < 9 || !load() {
				reply(nil, errors.New("not available"))
				return
			}
			flags := args[0]
			reason, rest := readString(args[1:])
			cancel, _ := readString(rest)
			c := New("LAContext")
			if cancel != "" {
				Send(c, "setLocalizedCancelTitle:", uintptr(String(cancel)))
			}
			policy := uintptr(owner)
			if flags&1 != 0 {
				policy = biometrics
				Send(c, "setLocalizedFallbackTitle:", uintptr(String("")))
			}
			if !SendBool(c, "canEvaluatePolicy:error:", policy, 0) {
				Release(c)
				reply(nil, errors.New("not available"))
				return
			}
			if reason == "" {
				reason = " "
			}
			var done objc.Block
			done = NewBlock(func(_ objc.Block, ok bool, err ID) {
				code := 0
				if err != 0 {
					code = SendInt(err, "code")
				}
				h.Post(func() {
					done.Release()
					Release(c)
					switch {
					case ok:
						reply(nil, nil)
					case code == -2 || code == -3 || code == -4 || code == -9:
						reply(nil, errors.New("canceled"))
					case code == -5 || code == -6 || code == -7 || code == -8:
						reply(nil, errors.New("not available"))
					default:
						reply(nil, errors.New("failed"))
					}
				})
			})
			Send(c, "evaluatePolicy:localizedReason:reply:", policy, uintptr(String(reason)), uintptr(done))
		},
	}
}

// reviewModule is ryui/review's: the App Store's rating prompt, which it
// shows or not.
func reviewModule() map[string]method {
	return map[string]method{
		"request": func(_ []byte, reply func([]byte, error)) {
			Dlopen("/System/Library/Frameworks/StoreKit.framework/StoreKit")
			if HasClass("SKStoreReviewController") {
				Send(Class("SKStoreReviewController"), "requestReview")
			}
			reply(nil, nil)
		},
	}
}
