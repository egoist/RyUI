//go:build linux && !android

package linux

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/internal/notifyspec"
	"github.com/egoist/ryui/ui"
)

// The native modules that run headless: the keyring (with
// gnome-keyring-daemon, unlocked, on the test's session bus), the
// network monitor, notify's protocol.

type callResult struct {
	b    []byte
	err  error
	done bool
}

// callModule calls a module's method on the main thread and waits for
// its answer (the main loop runs meanwhile).
func callModule(t *testing.T, h *Host, module, name string, args []byte) ([]byte, error) {
	t.Helper()
	var r callResult
	onMain(func() {
		m := h.module(module)[name]
		if m == nil {
			fatalf(t, "no %s.%s", module, name)
		}
		m(args, func(b []byte, err error) { r = callResult{b, err, true} })
	})
	for i := 0; i < 20000 && !waitDone(&r); i++ {
		time.Sleep(time.Millisecond)
	}
	if !waitDone(&r) {
		t.Fatalf("%s.%s never answered", module, name)
	}
	return r.b, r.err
}

func waitDone(r *callResult) bool {
	var d bool
	onMain(func() { d = r.done })
	return d
}

func emptyRoot() ui.Node { return ui.Column(ui.Style{}) }

// startKeyring starts gnome-keyring-daemon's secrets on the session bus,
// with a login keyring of its own, unlocked: the default collection.
func startKeyring(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("gnome-keyring-daemon"); err != nil {
		t.Skip("no gnome-keyring-daemon")
	}
	// Its keyrings in a folder of the test's: a login keyring made now,
	// the default collection.
	home := t.TempDir()
	cmd := exec.Command("gnome-keyring-daemon", "--unlock", "--components=secrets", "--replace")
	cmd.Env = append(os.Environ(), "HOME="+home, "XDG_DATA_HOME="+home+"/.local/share")
	cmd.Stdin = strings.NewReader("test")
	if err := cmd.Run(); err != nil { // it forks the daemon and exits
		t.Skip("gnome-keyring-daemon:", err)
	}
}

func TestSecureStore(t *testing.T) {
	startKeyring(t)
	h, _ := mount(t, emptyRoot)
	set := func(key string, v []byte) error {
		args := binary.LittleEndian.AppendUint32(nil, uint32(len(key)))
		args = append(append(args, key...), v...)
		_, err := callModule(t, h, "secureStore", "set", args)
		return err
	}
	get := func(key string) ([]byte, error) { return callModule(t, h, "secureStore", "get", []byte(key)) }

	if b, err := get("token"); err != nil || len(b) != 1 || b[0] != 0 {
		t.Fatalf("before set: %v %v", b, err)
	}
	// A value with a NUL and invalid UTF-8: kept as bytes.
	value := []byte("s3cret\x00\xff!")
	if err := set("token", value); err != nil {
		t.Fatal("set:", err)
	}
	b, err := get("token")
	if err != nil || len(b) < 1 || b[0] != 1 || string(b[1:]) != string(value) {
		t.Fatalf("get: %q %v", b, err)
	}
	if err := set("token", []byte("two")); err != nil {
		t.Fatal("set again:", err)
	}
	if b, _ := get("token"); string(b) != "\x01two" {
		t.Fatalf("replaced: %q", b)
	}
	// An empty value is a value.
	if err := set("empty", nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := get("empty"); string(b) != "\x01" {
		t.Fatalf("empty: %q", b)
	}
	if _, err := callModule(t, h, "secureStore", "delete", []byte("token")); err != nil {
		t.Fatal("delete:", err)
	}
	if b, _ := get("token"); string(b) != "\x00" {
		t.Fatalf("after delete: %q", b)
	}
	// Deleting what isn't there is no error.
	if _, err := callModule(t, h, "secureStore", "delete", []byte("token")); err != nil {
		t.Fatal("delete again:", err)
	}
	callModule(t, h, "secureStore", "delete", []byte("empty"))
}

func TestNetwork(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	b, err := callModule(t, h, "network", "current", nil)
	if err != nil || len(b) != 4 {
		t.Fatalf("current: %v %v", b, err)
	}
	switch {
	case b[0] == 0 && (b[1] != 0 || b[2] != 0):
		t.Fatalf("offline with a type or metered: %v", b)
	case b[0] == 1 && (b[1] < 1 || b[1] > 4):
		t.Fatalf("online without a type: %v", b)
	case b[3] != 0:
		t.Fatalf("constrained: %v", b)
	}
	// next answers at once when the state isn't the one given...
	other := []byte{1 - b[0], 0, 0, 0}
	if nb, err := callModule(t, h, "network", "next", other); err != nil || string(nb) != string(b) {
		t.Fatalf("next of another state: %v %v", nb, err)
	}
	// ...and waits while it is, until stopped.
	var r callResult
	onMain(func() {
		h.module("network")["next"](b, func(b []byte, err error) { r = callResult{b, err, true} })
	})
	if waitDone(&r) {
		t.Fatal("next of the same state answered at once")
	}
	if _, err := callModule(t, h, "network", "stop", nil); err != nil {
		t.Fatal(err)
	}
	if !waitDone(&r) || r.err == nil || r.err.Error() != "stopped" {
		t.Fatalf("next after stop: %+v", r)
	}
}

func TestNotify(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	// GNotification's way (an app with a desktop file, a sandboxed one);
	// TestNotifyFreedesktop tests the other.
	onMain(func() {
		notifyBackend = "gio"
		h.installNotifications()
		notes.fdo = false
	})
	defer func() { notifyBackend = "" }()
	call := func(name string, args []byte) []byte {
		t.Helper()
		b, err := callModule(t, h, "notify", name, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return b
	}
	showing := func() []notifyspec.Shown {
		t.Helper()
		var list []notifyspec.Shown
		if err := json.Unmarshal(call("showing", nil), &list); err != nil {
			t.Fatal(err)
		}
		return list
	}
	if b := call("request", nil); string(b) != "\x01" {
		t.Fatalf("request: %q", b)
	}
	call("categories", []byte(`[{"id":"msg","actions":[{"id":"like","title":"Like"},{"id":"open","title":"Open","open":true}]}]`))
	call("schedule", []byte(`{"id":"a","title":"Hi","subtitle":"Sub","body":"There","url":"https://example.com/","data":{"k":"v"},"category":"msg","thread":"t1"}`))
	call("schedule", []byte(`{"id":"b","title":"Two","thread":"t2"}`))
	list := showing()
	if len(list) != 2 {
		t.Fatalf("showing: %+v", list)
	}
	byID := map[string]notifyspec.Shown{}
	for _, s := range list {
		byID[s.ID] = s
	}
	if a := byID["a"]; a.Title != "Hi" || a.Subtitle != "Sub" || a.Body != "There" || a.Data["k"] != "v" || a.Category != "msg" || a.Thread != "t1" || a.Time == 0 {
		t.Fatalf("a: %+v", a)
	}

	// A button: the app's action, with the notification and the button.
	activate := func(target string) {
		onMain(func() {
			name := CString(notifyAction)
			Call(Sym("g_action_group_activate_action"), h.app, Addr(name), VStr(target))
		})
	}
	activate(`{"id":"a","action":"like"}`)
	var resp notifyspec.Response
	if err := json.Unmarshal(call("nextAction", nil), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Action != "like" || resp.Notification.ID != "a" || resp.Notification.Data["k"] != "v" || resp.Token == "" || resp.Seconds != actionSeconds {
		t.Fatalf("response: %+v", resp)
	}
	call("actionDone", []byte(resp.Token))
	onMain(func() {
		if len(notes.pendingActions) != 0 {
			fatalf(t, "a background action still pending after actionDone")
		}
	})
	// A tapped notification is no longer showing.
	if l := showing(); len(l) != 1 || l[0].ID != "b" {
		t.Fatalf("after the tap: %+v", l)
	}
	call("removeThread", []byte("t2"))
	if l := showing(); len(l) != 0 {
		t.Fatalf("after removeThread: %+v", l)
	}

	// Due later: a timer of the app's, which cancel stops.
	call("schedule", []byte(`{"id":"later","title":"Later","seconds":0.05}`))
	call("schedule", []byte(`{"id":"never","title":"Never","seconds":0.05}`))
	if l := showing(); len(l) != 0 {
		t.Fatalf("shown before it's due: %+v", l)
	}
	call("cancel", []byte("never"))
	settle(t, h, func() bool { return notes.showing["later"] != nil })
	onMain(func() {
		if len(notes.timers) != 0 || notes.showing["never"] != nil {
			fatalf(t, "timers %v, never %v", notes.timers, notes.showing["never"])
		}
	})
	call("removeAll", nil)

	// In front, the app decides how it shows: none here.
	active := appActive
	appActive = func(*Host) bool { return true }
	defer func() { appActive = active }()
	call("present", []byte{1})
	call("schedule", []byte(`{"id":"p","title":"Present"}`))
	var pr notifyspec.Present
	if err := json.Unmarshal(call("nextPresent", nil), &pr); err != nil || pr.Notification.ID != "p" || pr.Token == "" {
		t.Fatalf("present: %+v %v", pr, err)
	}
	call("presented", append([]byte{2}, pr.Token...))
	if l := showing(); len(l) != 0 {
		t.Fatalf("shown though presented as none: %+v", l)
	}
	call("present", []byte{0})
	call("schedule", []byte(`{"id":"q","title":"Shown"}`))
	if l := showing(); len(l) != 1 || l[0].ID != "q" {
		t.Fatalf("not presenting: %+v", l)
	}
	call("cancel", []byte("q"))

	// Waiting for actions ends with stop.
	var r callResult
	onMain(func() {
		h.module("notify")["nextAction"](nil, func(b []byte, err error) { r = callResult{b, err, true} })
	})
	call("stop", []byte("nextAction"))
	if !waitDone(&r) || r.err == nil {
		t.Fatalf("nextAction after stop: %+v", r)
	}
	// Android's channels: accepted, nothing to do.
	call("channels", []byte(`[]`))
}

// writeWAV writes a second's mono 16-bit tone (440 Hz) as a WAV file.
func writeWAV(t *testing.T, path string, seconds float64) {
	t.Helper()
	const rate = 8000
	n := int(rate * seconds)
	le := binary.LittleEndian
	b := []byte("RIFF")
	b = le.AppendUint32(b, uint32(36+2*n))
	b = append(b, "WAVEfmt "...)
	b = le.AppendUint32(b, 16)
	b = le.AppendUint16(b, 1) // PCM
	b = le.AppendUint16(b, 1)
	b = le.AppendUint32(b, rate)
	b = le.AppendUint32(b, rate*2)
	b = le.AppendUint16(b, 2)
	b = le.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = le.AppendUint32(b, uint32(2*n))
	for i := 0; i < n; i++ {
		v := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/rate))
		b = le.AppendUint16(b, uint16(v))
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLibraryAndClipboard(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	dir := t.TempDir()
	dirs := userSpecialDir
	userSpecialDir = func(id int32) string { return filepath.Join(dir, strconv.Itoa(int(id))) }
	defer func() { userSpecialDir = dirs }()

	// A picture: into Pictures, in the app's folder; a second of the same
	// name gets a number.
	img := filepath.Join(t.TempDir(), "dot.png")
	var buf bytes.Buffer
	m := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range m.Pix {
		m.Pix[i] = 200
	}
	png.Encode(&buf, m)
	os.WriteFile(img, buf.Bytes(), 0o644)
	save := func(args string) string {
		b, err := callModule(t, h, "library", "save", []byte(args))
		if err != nil {
			t.Fatal("save:", err)
		}
		return string(b)
	}
	args := `{"path":` + strconv.Quote(img) + `,"collection":3,"name":"Dot.png","folder":"MyApp","mime":"image/png"}`
	first, second := save(args), save(args)
	want := filepath.Join(dir, strconv.Itoa(userDirPictures), "MyApp")
	if first != filepath.Join(want, "Dot.png") || second != filepath.Join(want, "Dot (2).png") {
		t.Fatalf("saved to %q, %q", first, second)
	}
	if b, err := os.ReadFile(second); err != nil || !bytes.Equal(b, buf.Bytes()) {
		t.Fatalf("the copy: %v", err)
	}

	// The clipboard: an image copied, then read back as a PNG.
	if _, err := callModule(t, h, "clipboard", "copyImage", []byte(img)); err != nil {
		t.Fatal("copyImage:", err)
	}
	if b, err := callModule(t, h, "clipboard", "has", nil); err != nil || len(b) != 3 || b[1] != 1 {
		t.Fatalf("has after copyImage: %v %v", b, err)
	}
	b, err := callModule(t, h, "clipboard", "image", nil)
	if err != nil || len(b) == 0 {
		t.Fatalf("image: %q %v", b, err)
	}
	f, err := os.Open(string(b))
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(f)
	f.Close()
	if err != nil || got.Bounds().Dx() != 4 {
		t.Fatalf("the clipboard's image: %v %v", got, err)
	}
	if _, err := callModule(t, h, "clipboard", "copyImage", []byte(filepath.Join(dir, "none.png"))); err == nil {
		t.Fatal("copyImage of no file: no error")
	}

	// Watching: next answers at the next change.
	var r callResult
	onMain(func() {
		h.module("clipboard")["next"](nil, func(b []byte, err error) { r = callResult{b, err, true} })
	})
	if waitDone(&r) {
		t.Fatal("next answered before a change")
	}
	onMain(func() { ClipboardSetText(modClipboard(), "changed") })
	settle(t, h, func() bool { return r.done })
	if r.err != nil {
		t.Fatal("next:", r.err)
	}
	if b, _ := callModule(t, h, "clipboard", "has", nil); len(b) != 3 || b[0] != 1 || b[1] != 0 {
		t.Fatalf("has after text: %v", b)
	}

	// Shared files: on the clipboard as a file list.
	le := binary.LittleEndian
	share := le.AppendUint32(nil, 0)
	for i := 0; i < 3; i++ {
		share = le.AppendUint32(share, 0)
	}
	share = le.AppendUint32(share, 1)
	share = le.AppendUint32(share, uint32(len(img)))
	share = append(share, img...)
	if _, err := callModule(t, h, "share", "files", share); err != nil {
		t.Fatal("share:", err)
	}
	if b, _ := callModule(t, h, "clipboard", "has", nil); len(b) != 3 || b[2] != 1 {
		t.Fatalf("has after sharing a file: %v", b)
	}
}

func TestAudio(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	if err := LoadGst(); err != nil {
		t.Skip(err)
	}
	sink := playerSink
	playerSink = "fakesink sync=true"
	defer func() { playerSink = sink }()
	wav := filepath.Join(t.TempDir(), "tone.wav")
	writeWAV(t, wav, 0.5)
	id := func(rest []byte) []byte {
		b := binary.LittleEndian.AppendUint32(nil, 1)
		return append(append(b, '1'), rest...)
	}
	call := func(name string, rest []byte) []byte {
		t.Helper()
		b, err := callModule(t, h, "audio", name, id(rest))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return b
	}
	type status struct {
		pos, dur       float64
		playing, ended bool
	}
	read := func() status {
		b := call("status", nil)
		if len(b) != 18 {
			t.Fatalf("status: %v", b)
		}
		le := binary.LittleEndian
		return status{math.Float64frombits(le.Uint64(b)), math.Float64frombits(le.Uint64(b[8:])), b[16] == 1, b[17] == 1}
	}
	call("load", []byte(wav))
	// Prerolled, the duration's known.
	for i := 0; i < 2000 && read().dur == 0; i++ {
		time.Sleep(time.Millisecond)
	}
	if s := read(); math.Abs(s.dur-0.5) > 0.05 || s.playing || s.ended {
		t.Fatalf("loaded: %+v", s)
	}
	call("rate", binary.LittleEndian.AppendUint64(nil, math.Float64bits(2)))
	call("volume", binary.LittleEndian.AppendUint64(nil, math.Float64bits(0.5)))
	call("play", nil)
	if s := read(); !s.playing {
		t.Fatalf("playing: %+v", s)
	}
	for i := 0; i < 5000 && !read().ended; i++ {
		time.Sleep(time.Millisecond)
	}
	if s := read(); !s.ended || s.playing {
		t.Fatalf("at the end: %+v", s)
	}
	// Play again from the start; release lets it go.
	call("seek", binary.LittleEndian.AppendUint64(nil, math.Float64bits(0.1)))
	call("release", nil)
	onMain(func() {
		if len(players) != 0 {
			fatalf(t, "players after release: %d", len(players))
		}
	})
	if s := read(); s.dur != 0 || s.playing {
		t.Fatalf("status of a released player: %+v", s)
	}
}

func TestAudioRecord(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	if err := LoadGst(); err != nil {
		t.Skip(err)
	}
	src := recordSource
	recordSource = "audiotestsrc is-live=true wave=sine volume=0.5"
	defer func() { recordSource = src }()
	for _, stream := range []bool{false, true} {
		path := filepath.Join(t.TempDir(), "rec")
		args := []byte{0, 0, 0, 0, 0, 0} // voice, no max, no flags
		if stream {
			args[5] = 2
		}
		args = binary.LittleEndian.AppendUint32(args, 0)
		args = binary.LittleEndian.AppendUint32(args, uint32(len(path)))
		args = append(args, path...)
		var r callResult
		onMain(func() {
			h.module("audioRecord")["start"](args, func(b []byte, err error) { r = callResult{b, err, true} })
		})
		if waitDone(&r) {
			t.Fatalf("start answered at once: %q %v", r.b, r.err)
		}
		// Recording: the meter moves (the tone is loud), the time runs.
		for i := 0; i < 3000; i++ {
			var ok bool
			onMain(func() { ok = rec == nil || rec.level > 0 })
			if ok {
				break
			}
			time.Sleep(time.Millisecond)
		}
		onMain(func() {
			if rec == nil || rec.level == 0 {
				fatalf(t, "no level: recording %v, answered %+v", rec != nil, r)
			}
		})
		st, err := callModule(t, h, "audioRecord", "status", nil)
		if err != nil || len(st) != 6 || st[1] != 1 {
			t.Fatalf("status: %v %v", st, err)
		}
		callModule(t, h, "audioRecord", "pause", nil)
		if st, _ := callModule(t, h, "audioRecord", "status", nil); st[1] != 0 {
			t.Fatalf("paused: %v", st)
		}
		callModule(t, h, "audioRecord", "resume", nil)
		callModule(t, h, "audioRecord", "stop", nil)
		for i := 0; i < 5000 && !waitDone(&r); i++ {
			time.Sleep(time.Millisecond)
		}
		if !r.done || r.err != nil || string(r.b) != path {
			t.Fatalf("recorded: %q %v", r.b, r.err)
		}
		b, err := os.ReadFile(path)
		if err != nil || len(b) < 64 {
			t.Fatalf("the file: %d bytes, %v", len(b), err)
		}
		if stream {
			if b[0] != 0xFF || b[1]&0xF0 != 0xF0 {
				t.Fatalf("not ADTS: % x", b[:4])
			}
		} else if string(b[4:8]) != "ftyp" {
			t.Fatalf("not MPEG-4: % x", b[:8])
		}
		onMain(func() {
			if rec != nil {
				fatalf(t, "still recording")
			}
		})
	}
}

func TestSpeech(t *testing.T) {
	if _, err := exec.LookPath("speech-dispatcher"); err != nil {
		t.Skip("no speech-dispatcher")
	}
	// A daemon of the test's, started as libspeechd starts one.
	t.Setenv("SPEECHD_ADDRESS", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	h, _ := mount(t, emptyRoot)
	b, err := callModule(t, h, "speech", "voices", nil)
	if err != nil {
		t.Fatal("voices:", err)
	}
	var voices []speechVoice
	if err := json.Unmarshal(b, &voices); err != nil || len(voices) == 0 {
		t.Fatalf("voices: %s %v", b, err)
	}
	for _, v := range voices {
		if v.ID == "" || v.Language == "" {
			t.Fatalf("a voice: %+v", v)
		}
	}
	var r callResult
	onMain(func() {
		h.module("speech")["speak"]([]byte(`{"text":"Hello there.\n.A line with a dot","language":"en-US","rate":1.5,"pitch":1,"volume":1}`),
			func(b []byte, err error) { r = callResult{b, err, true} })
	})
	if b, _ := callModule(t, h, "speech", "speaking", nil); string(b) != "\x01" {
		t.Fatalf("speaking: %q", b)
	}
	// Queued (the VM has no sound to finish it in): stop ends it.
	for i := 0; i < 3000; i++ {
		var queued bool
		onMain(func() { queued = len(speech.replies) > 0 || r.done })
		if queued {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := callModule(t, h, "speech", "stop", nil); err != nil {
		t.Fatal("stop:", err)
	}
	if !waitDone(&r) || (r.err != nil && r.err.Error() != "stopped") {
		t.Fatalf("speak after stop: %+v", r)
	}
	if b, _ := callModule(t, h, "speech", "speaking", nil); string(b) != "\x00" {
		t.Fatalf("speaking after stop: %q", b)
	}
}

func TestSSIPScale(t *testing.T) {
	for _, c := range []struct {
		x    float64
		want int
	}{{1, 0}, {2, 100}, {0.5, -100}, {4, 100}, {0, 0}, {1.5, 58}} {
		if got := ssipScale(c.x); got != c.want {
			t.Errorf("ssipScale(%v) = %d, want %d", c.x, got, c.want)
		}
	}
	vs := ssipVoices([]string{"English (America)\ten-us\tnone", "German\tde\tnone", "OK VOICE LIST SENT"})
	if len(vs) != 2 || vs[0].Language != "en-US" || vs[1].ID != "German" {
		t.Fatalf("voices: %+v", vs)
	}
	if levelOf("level, rms=(GValueArray)< -6.02, -12 >, peak=(GValueArray)< 0 >") != byte(math.Pow(10, -6.02/20)*255) {
		t.Fatal("levelOf: the loudest channel's RMS")
	}
	if levelOf("level, rms=(double){ -70 }") != 0 {
		t.Fatal("levelOf: silence")
	}
}

func TestLocation(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	// The VM has no location service: current answers, with an error
	// (or a position, where there's one), and stops what it started.
	b, err := callModule(t, h, "location", "current", nil)
	t.Logf("current: %s %v", b, err)
	if err != nil && err.Error() != "denied" && !strings.Contains(err.Error(), "location") {
		t.Fatalf("current: %v", err)
	}
	if err == nil {
		var p struct{ Lat, Lon, Acc float64 }
		if json.Unmarshal(b, &p) != nil {
			t.Fatalf("a bad position: %s", b)
		}
	}
	onMain(func() {
		if loc.src != nil || len(loc.current) != 0 {
			fatalf(t, "still locating after current answered")
		}
	})
	if _, err := callModule(t, h, "location", "stop", nil); err != nil {
		t.Fatal(err)
	}
	// Positions as the module answers them.
	pos := encodePosition(map[string]any{"Latitude": 48.85, "Longitude": 2.35, "Accuracy": 20.0, "Altitude": -1.7976931348623157e308,
		"Timestamp": []any{uint64(1700000000), uint64(500000)}})
	if string(pos) != `{"acc":20,"alt":0,"lat":48.85,"lon":2.35,"t":1700000000500}` {
		t.Fatalf("position: %s", pos)
	}
	if n := placeName(GeocodePlace{Name: "Rue X", Town: "Paris", Country: "France"}); n != "Rue X, Paris" {
		t.Fatalf("place name: %q", n)
	}
}

func TestPrintPDF(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	var werr error
	onMain(func() { werr = LoadWebKitPrint() })
	if werr != nil {
		t.Skip(werr)
	}
	b, err := callModule(t, h, "print", "pdf", []byte(`{"html":"<!doctype html><title>Invoice 7</title><h1>Hello, PDF</h1><p>Some text.</p>","title":"Invoice 7","width":612,"height":792,"top":36,"right":36,"bottom":36,"left":36}`))
	if err != nil {
		t.Fatal("pdf:", err)
	}
	path := string(b)
	if filepath.Base(path) != "Invoice 7.pdf" {
		t.Fatalf("named %q", path)
	}
	pdf, err := os.ReadFile(path)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("not a PDF: %v", err)
	}
	// The page is the size asked for (612 by 792 points).
	if !bytes.Contains(pdf, []byte("/MediaBox [ 0 0 612 792 ]")) && !bytes.Contains(pdf, []byte("/MediaBox [0 0 612 792]")) {
		t.Errorf("no 612 by 792 MediaBox")
	}
	if _, err := callModule(t, h, "print", "pdf", []byte(`{"html":"x","width":100,"height":100,"left":60,"right":60}`)); err == nil {
		t.Fatal("margins wider than the page: no error")
	}
}

func TestModulesLinuxLacks(t *testing.T) {
	h, _ := mount(t, emptyRoot)
	if b, err := callModule(t, h, "biometric", "check", nil); err != nil || string(b) != "\x00\x00\x00" {
		t.Fatalf("biometric.check: %v %v", b, err)
	}
	if _, err := callModule(t, h, "biometric", "authenticate", nil); err == nil || err.Error() != "not available" {
		t.Fatalf("biometric.authenticate: %v", err)
	}
	if b, err := callModule(t, h, "tracking", "status", nil); err != nil || string(b) != "\x03" {
		t.Fatalf("tracking.status: %v %v", b, err)
	}
	if b, err := callModule(t, h, "integrity", "supported", nil); err != nil || string(b) != "\x00" {
		t.Fatalf("integrity.supported: %v %v", b, err)
	}
	if _, err := callModule(t, h, "ageRange", "request", nil); err == nil || err.Error() != "not available" {
		t.Fatalf("ageRange.request: %v", err)
	}
	if _, err := callModule(t, h, "push", "register", nil); err == nil {
		t.Fatal("push.register: no error")
	}
	if _, err := callModule(t, h, "review", "request", nil); err != nil {
		t.Fatal("review.request:", err)
	}
	if b, err := callModule(t, h, "compose", "can", nil); err != nil || len(b) != 4 || b[1] != 0 || b[2] != 0 {
		t.Fatalf("compose.can: %v %v", b, err)
	}
	if _, err := callModule(t, h, "compose", "message", nil); err == nil || err.Error() != "unavailable" {
		t.Fatalf("compose.message: %v", err)
	}
}

// fdoCall calls the test's notification server and waits for its reply.
func fdoCall(t *testing.T, method string, params func() Ptr) any {
	t.Helper()
	var r struct {
		v    any
		err  error
		done bool
	}
	onMain(func() {
		conn, err := Bus(BusSession)
		if err != nil {
			fatalf(t, "%v", err)
		}
		var p Ptr
		if params != nil {
			p = params()
		}
		DBusCall(conn, fdoDest, fdoPath, "dev.ryui.Test", method, p, -1, func(reply Ptr, err error) {
			r.v, r.err, r.done = VariantGo(reply), err, true
		})
	})
	for i := 0; i < 5000; i++ {
		var d bool
		onMain(func() { d = r.done })
		if d {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !r.done || r.err != nil {
		t.Fatalf("%s: %v", method, r.err)
	}
	return r.v
}

func TestNotifyFreedesktop(t *testing.T) {
	if exec.Command("python3", "-c", "from gi.repository import Gio").Run() != nil {
		t.Skip("no python3-gi for the test's notification server")
	}
	server := exec.Command("python3", "testdata/notifications.py")
	in, _ := server.StdinPipe()
	out, _ := server.StdoutPipe()
	if err := server.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { in.Close(); server.Wait() }()
	ready := make([]byte, 6)
	if _, err := io.ReadFull(out, ready); err != nil || string(ready) != "ready\n" {
		t.Fatalf("the server: %q %v", ready, err)
	}
	h, _ := mount(t, emptyRoot)
	onMain(func() {
		h.installNotifications()
		notes.fdo = true
		if err := h.installFdo(); err != nil {
			fatalf(t, "%v", err)
		}
	})
	defer onMain(func() { notes.fdo = false })
	call := func(name string, args []byte) []byte {
		t.Helper()
		b, err := callModule(t, h, "notify", name, args)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return b
	}
	type note struct {
		id            uint32
		summary, body string
		actions       []string
	}
	shown := func() []note {
		t.Helper()
		v, _ := fdoCall(t, "Shown", nil).([]any)
		var out []note
		if len(v) == 0 {
			return nil
		}
		for _, x := range v[0].([]any) {
			f := x.([]any)
			var acts []string
			for _, a := range f[3].([]any) {
				acts = append(acts, a.(string))
			}
			out = append(out, note{f[0].(uint32), f[1].(string), f[2].(string), acts})
		}
		return out
	}
	call("categories", []byte(`[{"id":"msg","actions":[{"id":"like","title":"Like"}]}]`))
	call("schedule", []byte(`{"id":"a","title":"Hi","body":"1 < 2","category":"msg","data":{"k":"v"}}`))
	settle(t, h, func() bool { return notes.showing["a"] != nil })
	ns := shown()
	if len(ns) != 1 || ns[0].summary != "Hi" || ns[0].body != "1 &lt; 2" || strings.Join(ns[0].actions, ",") != "default,,like,Like" {
		t.Fatalf("the server shows %+v", ns)
	}
	// The same ID again replaces it.
	call("schedule", []byte(`{"id":"a","title":"Hi again","category":"msg","data":{"k":"v"}}`))
	settle(t, h, func() bool { return notes.showing["a"] != nil && notes.showing["a"].shown.Title == "Hi again" })
	if again := shown(); len(again) != 1 || again[0].summary != "Hi again" || again[0].id != ns[0].id {
		t.Fatalf("replaced: %+v (was %+v)", again, ns)
	}
	// A button the user presses.
	fdoCall(t, "Invoke", func() Ptr { return VTuple(VU32(ns[0].id), VStr("like")) })
	var resp notifyspec.Response
	if err := json.Unmarshal(call("nextAction", nil), &resp); err != nil || resp.Action != "like" || resp.Notification.ID != "a" || resp.Notification.Data["k"] != "v" {
		t.Fatalf("response: %+v %v", resp, err)
	}
	settle(t, h, func() bool { return notes.showing["a"] == nil && len(notes.fdoIDs) == 0 })
	// Withdrawn: the server closes it.
	call("schedule", []byte(`{"id":"b","title":"Bye"}`))
	settle(t, h, func() bool { return notes.showing["b"] != nil })
	if len(shown()) != 1 {
		t.Fatal("b isn't shown")
	}
	call("cancel", []byte("b"))
	if ns := shown(); len(ns) != 0 {
		t.Fatalf("after cancel: %+v", ns)
	}
}

// fileChooser finds GTK's own file chooser dialog (shown without
// portals), 0 if none is up.
func fileChooser() Ptr {
	list := WindowListToplevels()
	defer Call(Sym("g_list_free"), list)
	for l := list; l != 0; l = *At[Ptr](l + 8) {
		w := *At[Ptr](l)
		if TypeCheckInstanceIsA(w, TypeFromName("GtkFileChooserDialog")) {
			return w
		}
	}
	return 0
}

func TestPickFiles(t *testing.T) {
	if !strings.Contains(os.Getenv("GDK_DEBUG"), "no-portals") {
		t.Skip("run with GDK_DEBUG=no-portals: GTK's own dialog, which the test can answer")
	}
	h, _ := mount(t, emptyRoot)
	src := filepath.Join(t.TempDir(), "notes.txt")
	os.WriteFile(src, []byte("hello"), 0o644)
	pick := func(choose bool, answer func(d Ptr)) callResult {
		var r callResult
		onMain(func() {
			h.module("files")["pick"]([]byte("\x00text/plain"), func(b []byte, err error) { r = callResult{b, err, true} })
		})
		var d Ptr
		settle(t, h, func() bool { d = fileChooser(); return d != 0 })
		if choose {
			// The file selected, once the chooser's loaded its folder.
			settle(t, h, func() bool {
				f := Sys.FileNewForPath(src)
				Call(Sym("gtk_file_chooser_set_file"), d, f, 0)
				ObjectUnref(f)
				got := Call(Sym("gtk_file_chooser_get_file"), d)
				if got == 0 {
					return false
				}
				ObjectUnref(got)
				return true
			})
		}
		onMain(func() { answer(d) })
		for i := 0; i < 5000 && !waitDone(&r); i++ {
			time.Sleep(time.Millisecond)
		}
		return r
	}
	// Cancelled: no files, no error.
	r := pick(false, func(d Ptr) { Call(Sym("gtk_dialog_response"), d, uintptr(0xFFFFFFFA)) }) // GTK_RESPONSE_CANCEL
	if !r.done || r.err != nil || len(r.b) != 0 {
		t.Fatalf("cancelled: %+v", r)
	}
	// Chosen: a copy in the caches, described.
	r = pick(true, func(d Ptr) {
		Call(Sym("gtk_dialog_response"), d, uintptr(0xFFFFFFFD)) // GTK_RESPONSE_ACCEPT
	})
	var files []pickedFile
	if !r.done || r.err != nil || json.Unmarshal(r.b, &files) != nil || len(files) != 1 {
		t.Fatalf("chosen: %q %+v", r.b, r)
	}
	f := files[0]
	if f.Name != "notes.txt" || f.Size != 5 || f.Path == src || !strings.HasPrefix(f.Type, "text/plain") {
		t.Fatalf("the copy: %+v", f)
	}
	if b, _ := os.ReadFile(f.Path); string(b) != "hello" {
		t.Fatalf("the copy's content: %q", b)
	}
}
