//go:build linux && !android

package linux

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sdk/audio and asuka/sdk/audio/record on Linux, with GStreamer (which GTK's
// media plays with too): a player is a playbin (its pitch kept at any
// rate by scaletempo), a recording a pipeline from the default source
// (PipeWire's, PulseAudio's) through an AAC encoder into an MPEG-4 file,
// or ADTS frames for Stream.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["audio"] = h.audioModule()
		m["audioRecord"] = h.recordModule()
	})
}

// gobjectType is an instance's GType (GTypeInstance's class's).
func gobjectType(o Ptr) GType { return *At[GType](*At[Ptr](o)) }

func setObjectProperty(o Ptr, name string, v Ptr) {
	var val Value
	ValueInit(&val, gobjectType(v))
	Sys.ValueSetObject(&val, v)
	ObjectSetProperty(o, name, &val)
	ValueUnset(&val)
}

func setDoubleProperty(o Ptr, name string, d float64) {
	var val Value
	ValueInit(&val, TypeDouble)
	Sys.ValueSetDouble(&val, d)
	ObjectSetProperty(o, name, &val)
	ValueUnset(&val)
}

func setIntProperty(o Ptr, name string, i int32) {
	var val Value
	ValueInit(&val, TypeInt)
	Sys.ValueSetInt(&val, i)
	ObjectSetProperty(o, name, &val)
	ValueUnset(&val)
}

// Players.

type audioPlayer struct {
	pipe, bus   Ptr
	handlers    []uint64
	loop, once  bool
	playing     bool
	ended       bool
	rate        float64
	prerolled   bool
	pendingSeek float64 // < 0: none
	rateDue     bool    // the rate waits for the pipeline to preroll
}

var players = map[string]*audioPlayer{}

// playerSink is the players' sound's element, as gst-launch writes it
// ("": playbin's own choice, the desktop's sound server; tests play into
// a fakesink).
var playerSink string

// audioModule is asuka/sdk/audio's: players by ID (each call's args begin with
// it, a u32-length string): load (the source), play, playOnce, pause,
// seek (f64 seconds), volume (f64), rate (f64), loop (u8), release;
// status answers f64 position, f64 duration, u8 playing, u8 ended.
func (h *Host) audioModule() map[string]method {
	f64 := func(b []byte) float64 {
		if len(b) < 8 {
			return 0
		}
		return math.Float64frombits(binary.LittleEndian.Uint64(b))
	}
	m := func(f func(id string, p *audioPlayer, rest []byte) error) method {
		return func(args []byte, reply func([]byte, error)) {
			id, rest := readString(args)
			reply(nil, f(id, players[id], rest))
		}
	}
	return map[string]method{
		"load": m(func(id string, _ *audioPlayer, rest []byte) error { return h.loadAudio(id, string(rest)) }),
		"play": m(func(id string, p *audioPlayer, _ []byte) error { p.play(); return nil }),
		"playOnce": m(func(id string, p *audioPlayer, _ []byte) error {
			if p != nil {
				p.once = true
				p.play()
			}
			return nil
		}),
		"pause": m(func(id string, p *audioPlayer, _ []byte) error {
			if p != nil {
				p.playing = false
				Gst.ElementSetState(p.pipe, GstStatePaused)
			}
			return nil
		}),
		"seek": m(func(id string, p *audioPlayer, rest []byte) error {
			if p != nil {
				p.ended = false
				p.seek(f64(rest))
			}
			return nil
		}),
		"volume": m(func(id string, p *audioPlayer, rest []byte) error {
			if p != nil {
				setDoubleProperty(p.pipe, "volume", min(max(f64(rest), 0), 1))
			}
			return nil
		}),
		"loop": m(func(id string, p *audioPlayer, rest []byte) error {
			if p != nil {
				p.loop = len(rest) > 0 && rest[0] == 1
			}
			return nil
		}),
		"rate": m(func(id string, p *audioPlayer, rest []byte) error {
			if p != nil {
				p.rate = min(max(f64(rest), 0.25), 4)
				if p.prerolled {
					p.seek(p.position())
				} else {
					p.rateDue = true
				}
			}
			return nil
		}),
		"release": m(func(id string, p *audioPlayer, _ []byte) error { releaseAudio(id); return nil }),
		"status": func(args []byte, reply func([]byte, error)) {
			id, _ := readString(args)
			p := players[id]
			out := make([]byte, 0, 18)
			var pos, dur float64
			playing, ended := false, false
			if p != nil {
				pos = p.position()
				var d int64
				if Gst.QueryDuration(p.pipe, GstFormatTime, &d) && d > 0 {
					dur = float64(d) / 1e9
				}
				playing, ended = p.playing, p.ended
				if ended && dur > 0 {
					pos = dur
				}
			}
			out = binary.LittleEndian.AppendUint64(out, math.Float64bits(pos))
			out = binary.LittleEndian.AppendUint64(out, math.Float64bits(dur))
			out = append(out, b2u(playing), b2u(ended))
			reply(out, nil)
		},
	}
}

// audioURI is a source as a URI: a URL, a file, an asset.
func (h *Host) audioURI(source string) string {
	switch {
	case strings.Contains(source, "://"):
		return source
	case !filepath.IsAbs(source):
		source = filepath.Join(h.assets, filepath.FromSlash(source))
	}
	if abs, err := filepath.Abs(source); err == nil {
		source = abs
	}
	f := Sys.FileNewForPath(source)
	defer ObjectUnref(f)
	return GoStringFree(Sys.FileGetURI(f))
}

func (h *Host) loadAudio(id, source string) error {
	releaseAudio(id)
	if err := LoadGst(); err != nil {
		return errors.New("asuka/sdk/audio: " + err.Error())
	}
	pipe := Gst.ElementFactoryMake("playbin", 0)
	if pipe == 0 {
		return errors.New("asuka/sdk/audio: GStreamer's playbin isn't installed (gst-plugins-base)")
	}
	ObjectRefSink(pipe)
	SetStringProperty(pipe, "uri", h.audioURI(source))
	if playerSink != "" {
		var gerr Ptr
		if sink := Gst.ParseLaunch(playerSink, &gerr); sink != 0 {
			setObjectProperty(pipe, "audio-sink", sink)
		}
		if gerr != 0 {
			ErrorFree(gerr)
		}
	}
	// Sound only: a video's picture goes nowhere.
	if sink := Gst.ElementFactoryMake("fakesink", 0); sink != 0 {
		setObjectProperty(pipe, "video-sink", sink)
	}
	// A voice keeps its pitch at any rate.
	if GstHas("scaletempo") {
		if f := Gst.ElementFactoryMake("scaletempo", 0); f != 0 {
			setObjectProperty(pipe, "audio-filter", f)
		}
	}
	p := &audioPlayer{pipe: pipe, rate: 1, pendingSeek: -1}
	p.bus = Gst.ElementGetBus(pipe)
	Gst.BusAddSignalWatch(p.bus)
	p.handlers = append(p.handlers,
		Connect(p.bus, "message::async-done", func(uintptr) {
			if p.prerolled {
				return
			}
			p.prerolled = true
			switch {
			case p.pendingSeek >= 0:
				s := p.pendingSeek
				p.pendingSeek = -1
				p.seek(s)
			case p.rateDue && p.rate != 1:
				p.seek(p.position())
			}
			p.rateDue = false
		}),
		Connect(p.bus, "message::eos", func(uintptr) { h.audioEnded(id, p) }),
		Connect(p.bus, "message::error", func(msg uintptr) {
			p.playing = false
			p.ended = true
		}),
	)
	players[id] = p
	Gst.ElementSetState(pipe, GstStatePaused) // preroll: the duration's known
	return nil
}

func (p *audioPlayer) position() float64 {
	var pos int64
	if Gst.QueryPosition(p.pipe, GstFormatTime, &pos) && pos > 0 {
		return float64(pos) / 1e9
	}
	return 0
}

// seek goes to a time, at the player's rate.
func (p *audioPlayer) seek(sec float64) {
	if !p.prerolled {
		p.pendingSeek = sec
		return
	}
	Gst.Seek(p.pipe, p.rate, GstFormatTime, GstSeekFlagFlush|GstSeekFlagAccurate,
		GstSeekTypeSet, int64(max(sec, 0)*1e9), GstSeekTypeNone, -1)
}

func (p *audioPlayer) play() {
	if p == nil {
		return
	}
	if p.ended {
		p.ended = false
		p.seek(0)
	}
	p.playing = true
	Gst.ElementSetState(p.pipe, GstStatePlaying)
}

// audioEnded is a player's end: again (loop), let go (once), or ended.
func (h *Host) audioEnded(id string, p *audioPlayer) {
	switch {
	case p.loop:
		p.seek(0)
	case p.once:
		releaseAudio(id)
	default:
		p.ended = true
		p.playing = false
		Gst.ElementSetState(p.pipe, GstStatePaused)
	}
}

func releaseAudio(id string) {
	p := players[id]
	if p == nil {
		return
	}
	delete(players, id)
	for _, hd := range p.handlers {
		Disconnect(p.bus, hd)
	}
	Gst.BusRemoveSignalWat(p.bus)
	Gst.ObjectUnref(p.bus)
	Gst.ElementSetState(p.pipe, GstStateNull)
	Gst.ObjectUnref(p.pipe)
}

// Recording.

// recordSource is the pipeline's source (tests use a test tone).
var recordSource = "autoaudiosrc"

type recording struct {
	pipe, bus Ptr
	handlers  []uint64
	path      string
	reply     func([]byte, error)
	level     byte
	paused    bool
	kept      time.Duration // before the last start
	since     time.Time     // recording since (zero: paused)
	timer     uint32
	stopping  bool
}

var rec *recording

// aacEncoders are GStreamer's AAC encoders, best first.
var aacEncoders = []string{"fdkaacenc", "avenc_aac", "voaacenc", "faac"}

// recordModule is asuka/sdk/audio/record's: "start" (u8 quality: voice,
// music; u32 max milliseconds, 0: none; u8 flags, 1 background (a
// desktop app records out of sight anyway), 2 stream; u32-length title;
// u32-length path, empty for the app's caches) records until "stop" and
// answers with the file's path; "pause", "resume"; "level" answers u8, 0
// to 255; "status" answers u8 level, u8 recording, u32 milliseconds kept.
func (h *Host) recordModule() map[string]method {
	return map[string]method{
		"start": func(args []byte, reply func([]byte, error)) {
			if rec != nil {
				reply(nil, errors.New("already recording"))
				return
			}
			if len(args) < 6 {
				reply(nil, errors.New("audioRecord.start: bad arguments"))
				return
			}
			quality := args[0]
			maxMS := binary.LittleEndian.Uint32(args[1:])
			stream := args[5]&2 != 0
			_, rest := readString(args[6:]) // the notification's title: none on Linux
			path, _ := readString(rest)
			if err := h.startRecording(quality == 1, maxMS, stream, path, reply); err != nil {
				reply(nil, err)
			}
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			if r := rec; r != nil {
				r.stop()
			}
			reply(nil, nil)
		},
		"pause": func(_ []byte, reply func([]byte, error)) {
			if r := rec; r != nil && !r.paused && !r.stopping {
				r.paused = true
				r.kept += time.Since(r.since)
				r.since = time.Time{}
				r.level = 0
				Gst.ElementSetState(r.pipe, GstStatePaused)
			}
			reply(nil, nil)
		},
		"resume": func(_ []byte, reply func([]byte, error)) {
			if r := rec; r != nil && r.paused && !r.stopping {
				r.paused = false
				r.since = time.Now()
				Gst.ElementSetState(r.pipe, GstStatePlaying)
			}
			reply(nil, nil)
		},
		"level": func(_ []byte, reply func([]byte, error)) {
			var l byte
			if r := rec; r != nil && !r.paused {
				l = r.level
			}
			reply([]byte{l}, nil)
		},
		"status": func(_ []byte, reply func([]byte, error)) {
			out := []byte{0, 0}
			var kept time.Duration
			if r := rec; r != nil {
				if !r.paused {
					out[0] = r.level
				}
				out[1] = b2u(!r.paused && !r.stopping)
				kept = r.elapsed()
			}
			reply(binary.LittleEndian.AppendUint32(out, uint32(kept/time.Millisecond)), nil)
		},
	}
}

func (r *recording) elapsed() time.Duration {
	if r.since.IsZero() {
		return r.kept
	}
	return r.kept + time.Since(r.since)
}

func (h *Host) startRecording(music bool, maxMS uint32, stream bool, path string, reply func([]byte, error)) error {
	if err := LoadGst(); err != nil {
		return errors.New("asuka/sdk/audio/record: " + err.Error())
	}
	enc := ""
	for _, e := range aacEncoders {
		if GstHas(e) {
			enc = e
			break
		}
	}
	if enc == "" {
		return errors.New("asuka/sdk/audio/record: no AAC encoder (install GStreamer's libav or bad plugins)")
	}
	ext := ".m4a"
	if stream {
		ext = ".aac"
	}
	if path == "" {
		dir := filepath.Join(h.modCacheDir(), "asuka-audio")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		path = filepath.Join(dir, randomName()+ext)
	} else if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	caps, bitrate := "audio/x-raw,rate=22050,channels=1", 64000
	if music {
		caps, bitrate = "audio/x-raw,rate=44100,channels=2", 128000
	}
	mux := "mp4mux"
	if stream {
		mux = "audio/mpeg,stream-format=adts"
	}
	desc := recordSource + " ! queue ! audioconvert ! audioresample ! " + caps +
		" ! level name=meter interval=100000000 post-messages=true ! " + enc + " name=enc ! aacparse ! " + mux + " ! filesink name=sink"
	var gerr Ptr
	pipe := Gst.ParseLaunch(desc, &gerr)
	if pipe == 0 {
		return errors.New("asuka/sdk/audio/record: " + GError(gerr).Error())
	}
	if gerr != 0 { // a warning: an element it could make without
		ErrorFree(gerr)
	}
	ObjectRefSink(pipe)
	if e := Gst.BinGetByName(pipe, "enc"); e != 0 {
		if enc == "faac" || enc == "fdkaacenc" || enc == "voaacenc" || enc == "avenc_aac" {
			setIntProperty(e, "bitrate", int32(bitrate))
		}
		Gst.ObjectUnref(e)
	}
	if s := Gst.BinGetByName(pipe, "sink"); s != 0 {
		SetStringProperty(s, "location", path)
		Gst.ObjectUnref(s)
	}
	r := &recording{pipe: pipe, path: path, reply: reply, since: time.Now()}
	r.bus = Gst.ElementGetBus(pipe)
	Gst.BusAddSignalWatch(r.bus)
	r.handlers = append(r.handlers,
		Connect(r.bus, "message::element", func(msg uintptr) {
			if name, text := GstMessageStructure(msg); name == "level" {
				r.level = levelOf(text)
			}
		}),
		Connect(r.bus, "message::eos", func(uintptr) { r.finish(nil) }),
		Connect(r.bus, "message::error", func(msg uintptr) { r.finish(GstMessageError(msg)) }),
	)
	if Gst.ElementSetState(pipe, GstStatePlaying) == GstStateChangeFailure {
		r.release()
		return errors.New("asuka/sdk/audio/record: the microphone can't record")
	}
	rec = r
	if maxMS > 0 {
		r.timer = TimeoutAdd(maxMS, func() bool {
			r.timer = 0
			r.stop()
			return false
		})
	}
	return nil
}

// levelRMS finds a level message's RMS values (dB, a channel each): an
// array, "rms=(GValueArray)< -20.1, -20.3 >" ("(double){ ... }" in some
// versions).
var levelRMS = regexp.MustCompile(`rms=\([^)]*\)\s*[<{]([^>}]*)[>}]`)

// levelOf is a level message's loudest channel's RMS as iOS's meter
// reads: 0 below -60 dB, else its amplitude, 0 to 255.
func levelOf(text string) byte {
	m := levelRMS.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	db := math.Inf(-1)
	for _, f := range strings.Split(m[1], ",") {
		if v, err := strconv.ParseFloat(strings.TrimSpace(f), 64); err == nil && v > db {
			db = v
		}
	}
	if db <= -60 || math.IsNaN(db) {
		return 0
	}
	return byte(min(max(math.Pow(10, db/20), 0), 1) * 255)
}

// stop ends the recording: the encoder and the muxer finish the file
// (the end of the stream), then start's reply gets it.
func (r *recording) stop() {
	if r.stopping {
		return
	}
	r.stopping = true
	if r.timer != 0 {
		SourceRemove(r.timer)
		r.timer = 0
	}
	if !r.since.IsZero() {
		r.kept += time.Since(r.since)
		r.since = time.Time{}
	}
	if r.paused {
		// A paused pipeline doesn't carry the end through: playing, it
		// does at once.
		Gst.ElementSetState(r.pipe, GstStatePlaying)
	}
	Gst.SendEvent(r.pipe, Gst.EventNewEOS())
}

func (r *recording) finish(err error) {
	if rec == r {
		rec = nil
	}
	reply := r.reply
	r.reply = nil
	if r.timer != 0 {
		SourceRemove(r.timer)
		r.timer = 0
	}
	r.release()
	if reply == nil {
		return
	}
	if err != nil {
		reply(nil, errors.New("the recording failed: "+err.Error()))
		return
	}
	reply([]byte(r.path), nil)
}

func (r *recording) release() {
	for _, hd := range r.handlers {
		Disconnect(r.bus, hd)
	}
	r.handlers = nil
	if r.bus != 0 {
		Gst.BusRemoveSignalWat(r.bus)
		Gst.ObjectUnref(r.bus)
		r.bus = 0
	}
	if r.pipe != 0 {
		Gst.ElementSetState(r.pipe, GstStateNull)
		Gst.ObjectUnref(r.pipe)
		r.pipe = 0
	}
}
