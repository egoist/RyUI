package imagehash

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"testing"
)

// The reference implementations' outputs (testdata/gen.mjs: blurhash
// 2.0.5's encode and decode, thumbhash 0.1.1's rgbaToThumbHash and
// thumbHashToRGBA) for pictures and hashes of every shape: a hash
// decodes to the same pixels here, byte for byte.

type golden struct {
	Name, Hash string
	W, H       int
	RGBA       string
}

func load(t *testing.T, name string) []golden {
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var gs []golden
	if err := json.Unmarshal(b, &gs); err != nil {
		t.Fatal(err)
	}
	return gs
}

func compare(t *testing.T, what string, got []byte, want string) {
	t.Helper()
	w, err := base64.StdEncoding.DecodeString(want)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(w) {
		t.Fatalf("%s: %d bytes, want %d", what, len(got), len(w))
	}
	for i := range got {
		if got[i] != w[i] {
			t.Fatalf("%s: byte %d is %d, want %d", what, i, got[i], w[i])
		}
	}
}

func TestBlurHash(t *testing.T) {
	gs := load(t, "blurhash.json")
	if len(gs) < 50 {
		t.Fatalf("%d cases", len(gs))
	}
	for _, g := range gs {
		px, err := BlurHash(g.Hash, g.W, g.H, 1)
		if err != nil {
			t.Fatalf("%s %q: %v", g.Name, g.Hash, err)
		}
		compare(t, g.Name+" "+g.Hash, px, g.RGBA)
	}
}

func TestThumbHash(t *testing.T) {
	for _, g := range load(t, "thumbhash.json") {
		hash, err := ParseThumbHash(g.Hash)
		if err != nil {
			t.Fatalf("%s %q: %v", g.Name, g.Hash, err)
		}
		w, h, px, err := ThumbHash(hash)
		if err != nil {
			t.Fatalf("%s: %v", g.Name, err)
		}
		if w != g.W || h != g.H {
			t.Fatalf("%s: %dx%d, want %dx%d", g.Name, w, h, g.W, g.H)
		}
		compare(t, g.Name, px, g.RGBA)
	}
}

func TestBadHashes(t *testing.T) {
	for _, h := range []string{"", "LEHV6", "LEHV6nWB2yk8pyo0adR*.7kCMdn", "LEHV6nWB2yk8pyo0adR*.7kCMdn\x01", "\"EHV6nWB2yk8pyo0adR*.7kCMdnj"} {
		if _, err := BlurHash(h, 8, 8, 1); err == nil {
			t.Errorf("BlurHash(%q): no error", h)
		}
	}
	if _, err := ParseThumbHash("!!"); err == nil {
		t.Error("ParseThumbHash(!!): no error")
	}
	if _, _, _, err := ThumbHash([]byte{1, 2}); err == nil {
		t.Error("ThumbHash of 2 bytes: no error")
	}
	// URL-safe base64, unpadded, decodes as the standard form does.
	a, err1 := ParseThumbHash("1QcSHQRnh493V4dIh4eXh1h4kJUI")
	b, err2 := ParseThumbHash("1QcSHQRnh493V4dIh4eXh1h4kJUI==")
	if err1 != nil || err2 != nil || string(a) != string(b) {
		t.Errorf("padding: %v %v", err1, err2)
	}
}
