package record

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// frame makes an ADTS frame of AAC-LC, 22050 Hz, mono, with a payload.
func frame(payload []byte) []byte {
	n := len(payload) + 7
	h := []byte{0xFF, 0xF1, 1<<6 | 7<<2, 1<<6 | byte(n>>11), byte(n >> 3), byte(n<<5) | 0x1F, 0xFC}
	return append(h, payload...)
}

func stream(t *testing.T, content []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "r.aac")
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFinalizeIndexesEveryFrame(t *testing.T) {
	var b bytes.Buffer
	for i := range 100 {
		b.Write(frame(bytes.Repeat([]byte{byte(i)}, 10+i)))
	}
	dst := filepath.Join(t.TempDir(), "o.m4a")
	d, err := Finalize(stream(t, b.Bytes()), dst)
	if err != nil {
		t.Fatal(err)
	}
	if want := 100 * 1024 * time.Second / 22050; d != want {
		t.Errorf("duration %v, want %v", d, want)
	}
	out, _ := os.ReadFile(dst)
	if !bytes.Contains(out, []byte("moov")) || !bytes.Contains(out, []byte("mdat")) {
		t.Fatal("not an MP4")
	}
	// The sample table's first size, and the audio itself where stco says it is.
	stsz := bytes.Index(out, []byte("stsz"))
	if got := binary.BigEndian.Uint32(out[stsz+12:]); got != 100 {
		t.Errorf("%d frames in stsz", got)
	}
	if got := binary.BigEndian.Uint32(out[stsz+16:]); got != 10 {
		t.Errorf("first frame is %d bytes", got)
	}
	stco := bytes.Index(out, []byte("stco"))
	at := binary.BigEndian.Uint32(out[stco+12:])
	if !bytes.Equal(out[at:at+10], bytes.Repeat([]byte{0}, 10)) || out[at+10] != 1 {
		t.Errorf("stco points at %x", out[at:at+11])
	}
	if string(out[at-4:at]) != "mdat" {
		t.Errorf("stco %d isn't the start of mdat", at)
	}
}

func TestFinalizeKeepsWhatACrashLeaves(t *testing.T) {
	var b bytes.Buffer
	for range 10 {
		b.Write(frame(make([]byte, 50)))
	}
	whole := b.Bytes()
	cut := whole[:len(whole)-20] // the last frame is cut short
	d, err := Finalize(stream(t, cut), filepath.Join(t.TempDir(), "o.m4a"))
	if err != nil {
		t.Fatal(err)
	}
	if want := 9 * 1024 * time.Second / 22050; d != want {
		t.Errorf("duration %v, want the 9 whole frames' %v", d, want)
	}
}

func TestFinalizeSkipsGarbage(t *testing.T) {
	var b bytes.Buffer
	b.Write(frame(make([]byte, 20)))
	b.Write([]byte{0x00, 0xFF, 0x12, 0xFF})
	b.Write(frame(make([]byte, 20)))
	info, err := scanADTS(bytes.NewReader(b.Bytes()), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(info.sizes) != 2 {
		t.Errorf("%d frames", len(info.sizes))
	}
}

func TestFinalizeEmpty(t *testing.T) {
	if _, err := Finalize(stream(t, nil), filepath.Join(t.TempDir(), "o.m4a")); !errors.Is(err, ErrEmptyStream) {
		t.Errorf("%v", err)
	}
	if _, err := Finalize(stream(t, []byte{1, 2, 3}), filepath.Join(t.TempDir(), "o.m4a")); !errors.Is(err, ErrEmptyStream) {
		t.Errorf("%v", err)
	}
}

// What the platform's own tools make of it, on a Mac: real AAC, real reader.
func TestFinalizeMakesAFileAppleReads(t *testing.T) {
	for _, tool := range []string{"say", "afconvert", "afinfo"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool, " not here")
		}
	}
	dir := t.TempDir()
	aiff, aac, m4a := filepath.Join(dir, "s.aiff"), filepath.Join(dir, "s.aac"), filepath.Join(dir, "s.m4a")
	if err := exec.Command("say", "-o", aiff, "Testing one two three, this is a recording.").Run(); err != nil {
		t.Skip("say: ", err)
	}
	if out, err := exec.Command("afconvert", "-f", "adts", "-d", "aac", "-b", "64000", "-c", "1", aiff, aac).CombinedOutput(); err != nil {
		t.Skip("afconvert: ", string(out))
	}
	d, err := Finalize(aac, m4a)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("afinfo", m4a).CombinedOutput()
	if err != nil {
		t.Fatalf("afinfo: %s", out)
	}
	m := regexp.MustCompile(`estimated duration: ([0-9.]+)`).FindSubmatch(out)
	if m == nil {
		t.Fatalf("afinfo said: %s", out)
	}
	got, _ := strconv.ParseFloat(string(m[1]), 64)
	if diff := got - d.Seconds(); diff > .1 || diff < -.1 {
		t.Errorf("afinfo says %vs, Finalize %v", got, d)
	}
	if !bytes.Contains(out, []byte("aac")) {
		t.Errorf("afinfo said: %s", out)
	}
	// And it decodes to something, end to end.
	wav := filepath.Join(dir, "back.wav")
	if out, err := exec.Command("afconvert", "-f", "WAVE", "-d", "LEI16", m4a, wav).CombinedOutput(); err != nil {
		t.Fatalf("decoding: %s", out)
	}
	if st, err := os.Stat(wav); err != nil || st.Size() < 10000 {
		t.Errorf("decoded %v %v", st, err)
	}
}
