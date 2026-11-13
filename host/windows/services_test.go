//go:build windows

package windows

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/ryui/ui"
)

// The modules of Windows's services answer: the network's state, SAPI's
// voices and speech, a toast, audio from a file, location (a position,
// or why there's none).
func TestServices(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		b, err, _ := await(h, "network", "current", nil)
		if err != nil || len(b) != 4 {
			fatalf("network.current: %v, %v", b, err)
		}
		t.Logf("network: %v", b)
		if b[0] == 1 && b[1] == 0 {
			fatalf("connected over nothing: %v", b)
		}
		// next with another state answers at once; stop fails a wait.
		if nb, err, ok := await(h, "network", "next", []byte{9, 9, 9, 9}); !ok || err != nil || !bytes.Equal(nb, b) {
			fatalf("network.next: %v, %v", nb, err)
		}
		var waited error
		h.asyncModule("network")["next"](b, func(_ []byte, e error) { waited = e })
		await(h, "network", "stop", nil)
		if waited == nil || waited.Error() != "stopped" {
			fatalf("network.next after stop: %v", waited)
		}

		vb, err, _ := await(h, "speech", "voices", nil)
		var voices []struct{ ID, Name, Language string }
		if err != nil || json.Unmarshal(vb, &voices) != nil {
			fatalf("speech.voices: %s, %v", vb, err)
		}
		t.Logf("%d voices, the first %+v", len(voices), voices)
		if len(voices) > 0 {
			args, _ := json.Marshal(map[string]any{"text": "Hi", "language": voices[0].Language, "pitch": 1.2, "rate": 2, "volume": 0})
			if _, err, ok := await(h, "speech", "speak", args); !ok || err != nil {
				fatalf("speech.speak: %v (answered %v)", err, ok)
			}
		}

		if b, err, _ := await(h, "notify", "request", nil); err != nil || len(b) != 1 {
			fatalf("notify.request: %v, %v", b, err)
		}
		spec, _ := json.Marshal(map[string]any{"id": "ryui-test", "title": "RyUI", "body": "A test toast", "silent": true})
		if _, err, _ := await(h, "notify", "schedule", spec); err != nil {
			fatalf("notify.schedule: %v", err)
		}
		spec, _ = json.Marshal(map[string]any{"id": "ryui-later", "title": "Later", "seconds": 3600})
		if _, err, _ := await(h, "notify", "schedule", spec); err != nil {
			fatalf("notify.schedule in an hour: %v", err)
		}
		await(h, "notify", "cancel", []byte("ryui-later"))
		await(h, "notify", "removeAll", nil)

		// A second of a tone, as a WAV file.
		wav := filepath.Join(t.TempDir(), "tone.wav")
		os.WriteFile(wav, toneWAV(8000, 1), 0o644)
		id := binary.LittleEndian.AppendUint32(nil, 1)
		id = append(id, 'a')
		if _, err, _ := await(h, "audio", "load", append(id, wav...)); err != nil {
			fatalf("audio.load: %v", err)
		}
		vol := binary.LittleEndian.AppendUint64(nil, math.Float64bits(0))
		await(h, "audio", "volume", append(id, vol...))
		st, err, _ := await(h, "audio", "status", id)
		if err != nil || len(st) != 18 {
			fatalf("audio.status: %v, %v", st, err)
		}
		if d := math.Float64frombits(binary.LittleEndian.Uint64(st[8:])); d < 0.9 || d > 1.1 {
			fatalf("audio's duration: %v, want 1", d)
		}
		await(h, "audio", "play", id)
		if st, _, _ := await(h, "audio", "status", id); st[16] != 1 {
			t.Logf("audio.status after play: not playing (%v)", st)
		}
		await(h, "audio", "release", id)
		if _, err, _ := await(h, "audio", "load", append(id, `C:\nonexistent\a.mp3`...)); err == nil {
			fatalf("audio.load of a missing file: no error")
		}

		lb, err, ok := await(h, "location", "current", nil)
		t.Logf("location.current: %s, %v", lb, err)
		if !ok || err == nil && !strings.Contains(string(lb), "Lat") {
			fatalf("location.current: %s, %v (answered %v)", lb, err, ok)
		}
	})
}

// toneWAV is seconds of a 440 Hz tone, 16-bit mono PCM at rate.
func toneWAV(rate, seconds int) []byte {
	n := rate * seconds
	le := binary.LittleEndian
	b := []byte("RIFF")
	b = le.AppendUint32(b, uint32(36+2*n))
	b = append(b, "WAVEfmt "...)
	b = le.AppendUint32(b, 16)
	b = le.AppendUint16(b, 1)
	b = le.AppendUint16(b, 1)
	b = le.AppendUint32(b, uint32(rate))
	b = le.AppendUint32(b, uint32(rate*2))
	b = le.AppendUint16(b, 2)
	b = le.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = le.AppendUint32(b, uint32(2*n))
	for i := range n {
		b = le.AppendUint16(b, uint16(int16(8000*math.Sin(2*math.Pi*440*float64(i)/float64(rate)))))
	}
	return b
}
