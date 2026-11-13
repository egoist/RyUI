//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// wav writes a second of a quiet tone as a 16-bit mono WAV file.
func wav(t *testing.T) string {
	const rate = 8000
	le := binary.LittleEndian
	var data []byte
	for i := range rate {
		data = le.AppendUint16(data, uint16(int16(800*math.Sin(float64(i)*2*math.Pi*440/rate))))
	}
	b := []byte("RIFF")
	b = le.AppendUint32(b, uint32(36+len(data)))
	b = append(b, "WAVEfmt "...)
	b = le.AppendUint32(b, 16)
	b = le.AppendUint16(b, 1) // PCM
	b = le.AppendUint16(b, 1)
	b = le.AppendUint32(b, rate)
	b = le.AppendUint32(b, rate*2)
	b = le.AppendUint16(b, 2)
	b = le.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = le.AppendUint32(b, uint32(len(data)))
	b = append(b, data...)
	p := filepath.Join(t.TempDir(), "tone.wav")
	os.WriteFile(p, b, 0o644)
	return p
}

// The network, audio, speech and biometric modules answer as iOS's do.
func TestServiceModules(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	tone := wav(t)
	onMain(func() {
		b, err := await(h, "network", "current", nil)
		if err != nil || len(b) != 4 || b[1] > 4 {
			fatalf(t, "network.current: %v, %v", b, err)
		}
		var stopped error
		h.module("network")["next"](b, func(_ []byte, err error) { stopped = err })
		await(h, "network", "stop", nil)
		if stopped == nil || stopped.Error() != "stopped" {
			fatalf(t, "network.stop: %v", stopped)
		}

		id := func(s string, rest ...byte) []byte {
			return append(append(binary.LittleEndian.AppendUint32(nil, uint32(len(s))), s...), rest...)
		}
		if _, err := await(h, "audio", "load", id("a", []byte(tone)...)); err != nil {
			fatal(t, "audio.load:", err)
		}
		vol := binary.LittleEndian.AppendUint64(nil, math.Float64bits(0))
		await(h, "audio", "volume", id("a", vol...))
		await(h, "audio", "play", id("a"))
		status := func() (pos, dur float64, playing, ended bool) {
			b, _ := await(h, "audio", "status", id("a"))
			if len(b) < 18 {
				fatalf(t, "audio.status: %v", b)
			}
			return math.Float64frombits(binary.LittleEndian.Uint64(b)), math.Float64frombits(binary.LittleEndian.Uint64(b[8:])), b[16] == 1, b[17] == 1
		}
		if !runUntil(func() bool { _, d, _, _ := status(); return d > 0.9 }) {
			fatal(t, "the tone's duration never came")
		}
		if !runUntil(func() bool { _, _, _, e := status(); return e }) {
			_, d, p, _ := status()
			fatalf(t, "the tone didn't end (duration %v, playing %v)", d, p)
		}
		await(h, "audio", "release", id("a"))
		if len(h.players) != 0 {
			fatal(t, "a player kept")
		}

		b, err = await(h, "speech", "voices", nil)
		var voices []map[string]any
		if err != nil || json.Unmarshal(b, &voices) != nil || len(voices) == 0 {
			fatalf(t, "speech.voices: %d voices, %v", len(voices), err)
		}
		if b, err := await(h, "speech", "speaking", nil); err != nil || len(b) != 1 || b[0] != 0 {
			fatalf(t, "speech.speaking: %v, %v", b, err)
		}

		// Spoken through (silently), and a stop ends what's queued.
		if _, err := await(h, "speech", "speak", []byte(`{"text":"ok","volume":0,"rate":2,"pitch":1}`)); err != nil {
			fatal(t, "speech.speak:", err)
		}
		var spoken error
		h.module("speech")["speak"]([]byte(`{"text":"a longer sentence to stop","volume":0,"rate":1,"pitch":1}`), func(_ []byte, err error) { spoken = err })
		await(h, "speech", "stop", nil)
		if spoken == nil || spoken.Error() != "stopped" || len(h.speech.replies) != 0 {
			fatalf(t, "speech.stop: %v", spoken)
		}

		if b, err := await(h, "biometric", "check", nil); err != nil || len(b) != 3 {
			fatalf(t, "biometric.check: %v, %v", b, err)
		}
	})
}

// A CLLocation encodes as iOS's does: its coordinate (a struct returned
// in registers), accuracy and time.
func TestEncodeLocation(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		h.locationManager()
		l := MsgFloat2ID(Send(Class("CLLocation"), "alloc"), Sel("initWithLatitude:longitude:"), 52.52, 13.405)
		defer Release(l)
		var o struct{ Lat, Lon, Acc float64 }
		if err := json.Unmarshal(encodeLocation(l), &o); err != nil || o.Lat != 52.52 || o.Lon != 13.405 {
			fatalf(t, "encoded %+v, %v", o, err)
		}
	})
}

// A Scroll pulled past its top by more than refreshPull and let go of
// refreshes: OnRefresh, and the spinner above the content until
// Refreshing goes false. A small pull doesn't.
func TestPullToRefresh(t *testing.T) {
	refreshing := signal.New(false)
	pulls := 0
	h, _ := mount(t, func() ui.Node {
		return ui.Scroll(ui.ScrollProps{Refreshing: refreshing, OnRefresh: func() { pulls++; refreshing.Set(true) }},
			ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(2000)}}))
	})
	onMain(func() {
		n := find(h, ops.KindScroll)[0]
		center := Send(Class("NSNotificationCenter"), "defaultCenter")
		defer func() { overTopOf = overTop }()
		pull := func(by float64) {
			// The rubber band, which only a live scroll's events stretch.
			overTopOf = func(ID) float64 { return by }
			Send(center, "postNotificationName:object:", uintptr(String("NSScrollViewDidLiveScrollNotification")), uintptr(n.view))
			overTopOf = overTop
			Send(center, "postNotificationName:object:", uintptr(String("NSScrollViewDidEndLiveScrollNotification")), uintptr(n.view))
		}
		if overTop(n.view) != 0 {
			fatalf(t, "at rest, %v past the top", overTop(n.view))
		}
		inCore(h, func() { pull(20) })
		if pulls != 0 {
			fatal(t, "a small pull refreshed")
		}
		inCore(h, func() { pull(80) })
		if pulls != 1 || n.refresh == nil || !n.refresh.refreshing || Send(n.refresh.spinner, "superview") != n.view {
			fatalf(t, "a long pull: %d refreshes, spinner shown %v", pulls, n.refresh != nil && n.refresh.refreshing)
		}
		inCore(h, func() { refreshing.Set(false) })
		if n.refresh.refreshing || Send(n.refresh.spinner, "superview") != 0 {
			fatal(t, "the spinner stayed")
		}
	})
}
