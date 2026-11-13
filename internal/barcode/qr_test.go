package barcode

import (
	"fmt"
	"image"
	"math/rand"
	"testing"
)

var levelNames = [4]string{ecM: "M", ecL: "L", ecH: "H", ecQ: "Q"}

// randomText is n random letters and digits.
func randomText(rng *rand.Rand, n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 -_/:."
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rng.Intn(len(chars))]
	}
	return string(b)
}

// qrFull is a byte-mode code of version v at level ec, as full as it
// gets.
func qrFull(rng *rand.Rand, v, ec, mask int) (*grid, string) {
	capBytes := qrDataCodewords(v, ec) - 1 - (qrCountBits(4, v)+7)/8
	text := randomText(rng, capBytes)
	return qrEncode([]qrSeg{{mode: 4, data: text}}, v, ec, mask), text
}

func expectOne(t *testing.T, img image.Image, kind Kind, text string, kinds ...Kind) []Code {
	t.Helper()
	codes := Decode(img, kinds...)
	if len(codes) != 1 || codes[0].Kind != kind || codes[0].Text != text {
		var got []string
		for _, c := range codes {
			got = append(got, fmt.Sprintf("%s %q %v", c.Kind, c.Text, c.Bounds))
		}
		t.Errorf("want %s %q, got %d: %v", kind, text, len(codes), got)
	}
	return codes
}

func TestQRVersions(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for v := 1; v <= 40; v++ {
		ec := []int{ecL, ecM, ecQ, ecH}[v%4]
		g, text := qrFull(rng, v, ec, v%8)
		img := gridImage(g, 3, 4)
		t.Run(fmt.Sprintf("v%d-%s", v, levelNames[ec]), func(t *testing.T) {
			codes := expectOne(t, img, QR, text, QR)
			if len(codes) == 1 {
				want := image.Rect(12, 12, 12+3*g.w, 12+3*g.w)
				if b := codes[0].Bounds; abs(b.Min.X-want.Min.X) > 3 || abs(b.Max.X-want.Max.X) > 3 || abs(b.Min.Y-want.Min.Y) > 3 || abs(b.Max.Y-want.Max.Y) > 3 {
					t.Errorf("bounds %v, want about %v", b, want)
				}
			}
		})
	}
}

func TestQRLevelsAndMasks(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for _, ec := range []int{ecL, ecM, ecQ, ecH} {
		for mask := 0; mask < 8; mask++ {
			v := 2 + mask
			g, text := qrFull(rng, v, ec, mask)
			t.Run(fmt.Sprintf("%s-mask%d", levelNames[ec], mask), func(t *testing.T) {
				expectOne(t, gridImage(g, 4, 4), QR, text, QR)
			})
		}
	}
}

func TestQRModes(t *testing.T) {
	cases := []struct {
		name string
		segs []qrSeg
		want string
	}{
		{"numeric", []qrSeg{{mode: 1, data: "0123456789012345678"}}, "0123456789012345678"},
		{"numeric2", []qrSeg{{mode: 1, data: "12345"}}, "12345"},
		{"alphanumeric", []qrSeg{{mode: 2, data: "HELLO WORLD $%*+-./:"}}, "HELLO WORLD $%*+-./:"},
		{"utf8", []qrSeg{{mode: 4, data: "héllo wörld ✓ 你好"}}, "héllo wörld ✓ 你好"},
		{"latin1", []qrSeg{{mode: 4, data: "caf\xe9 na\xefve"}}, "café naïve"},
		{"eci-utf8", []qrSeg{{mode: 7, eci: 26}, {mode: 4, data: "Grüße"}}, "Grüße"},
		{"eci-latin1", []qrSeg{{mode: 7, eci: 3}, {mode: 4, data: "\xc3\xa9"}}, "Ã©"},
		{"kanji", []qrSeg{{mode: 8, data: "点茗漢字"}}, "点茗漢字"},
		{"eci-sjis", []qrSeg{{mode: 7, eci: 20}, {mode: 4, data: "\x93\x5f\xb1"}}, "点ｱ"},
		{"mixed", []qrSeg{{mode: 2, data: "ABC"}, {mode: 1, data: "123"}, {mode: 4, data: "xyz"}, {mode: 8, data: "漢"}}, "ABC123xyz漢"},
		{"gs1", []qrSeg{{mode: 5}, {mode: 2, data: "01%%23%10"}}, "01%23\x1d10"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g := qrEncodeWithFNC1(c.segs, 4, ecM, 3)
			expectOne(t, gridImage(g, 4, 4), QR, c.want)
		})
	}
}

// qrEncodeWithFNC1 is qrEncode that also writes FNC1 (mode 5).
func qrEncodeWithFNC1(segs []qrSeg, v, ec, mask int) *grid {
	if len(segs) > 0 && segs[0].mode == 5 {
		// FNC1 in first position: the mode indicator alone, which the
		// encoder writes as a zero-length ECI-free prefix.
		return qrEncode(append([]qrSeg{{mode: -5}}, segs[1:]...), v, ec, mask)
	}
	return qrEncode(segs, v, ec, mask)
}

func TestQRDamaged(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, c := range []struct{ v, ec int }{{2, ecH}, {7, ecM}, {10, ecQ}, {5, ecL}} {
		g, text := qrFull(rng, c.v, c.ec, 5)
		_, check, _, _ := qrBlockLayout(c.v, c.ec)
		mods := qrCodewordModules(c.v)
		blockOf := qrBlockOf(c.v, c.ec)
		// In each block, as many wrong codewords as it can correct, each
		// with one to eight modules flipped.
		perBlock := map[int]int{}
		for _, i := range rng.Perm(len(mods)) {
			b := blockOf[i]
			if perBlock[b] >= check/2 {
				continue
			}
			perBlock[b]++
			ms := mods[i]
			for _, k := range rng.Perm(len(ms))[:1+rng.Intn(len(ms))] {
				g.set(ms[k][0], ms[k][1], !g.at(ms[k][0], ms[k][1]))
			}
		}
		t.Run(fmt.Sprintf("v%d-%s", c.v, levelNames[c.ec]), func(t *testing.T) {
			expectOne(t, gridImage(g, 4, 4), QR, text, QR)
		})
	}
}

func TestQRTransformed(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	angles := []float64{0, 7, 33, 90, 135, 180, 271}
	for i, v := range []int{1, 2, 4, 6, 7, 10, 15, 22} {
		g, text := qrFull(rng, v, ecM, i%8)
		src := gridImage(g, 4, 4)
		deg := angles[i%len(angles)]
		for _, sc := range []float64{0.8, 1.37} {
			name := fmt.Sprintf("v%d-%gdeg-x%g", v, deg, sc)
			t.Run(name, func(t *testing.T) {
				img := rotated(src, deg, sc, 20)
				expectOne(t, img, QR, text)
			})
		}
	}
	// Every angle, with blur, noise and uneven light.
	g, text := qrFull(rng, 5, ecQ, 2)
	src := gridImage(g, 5, 4)
	for _, deg := range angles {
		t.Run(fmt.Sprintf("v5-%gdeg-photo", deg), func(t *testing.T) {
			img := rotated(src, deg, 1, 30)
			img = lit(noisy(blur(img, 1.1), 8, int64(deg)), 0.45, 20, 235)
			expectOne(t, img, QR, text)
		})
	}
}

func TestQRPerspective(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for i, v := range []int{2, 3, 7, 12} {
		g, text := qrFull(rng, v, ecM, i)
		src := gridImage(g, 4, 4)
		s := float64(src.Bounds().Dx())
		corners := [4]point{{60 + 0.12*s, 40}, {60 + 0.92*s, 40 + 0.06*s}, {60 + s, 40 + 0.97*s}, {60, 40 + 0.9*s}}
		t.Run(fmt.Sprintf("v%d", v), func(t *testing.T) {
			img := placed(int(s)+120, int(s)+100, src, corners, int64(v))
			expectOne(t, blur(img, 0.7), QR, text)
		})
	}
}

func TestQRSmallModules(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	g, text := qrFull(rng, 6, ecM, 1)
	src := gridImage(g, 6, 4)
	for _, deg := range []float64{0, 20, 45} {
		t.Run(fmt.Sprintf("%gdeg", deg), func(t *testing.T) {
			// Three pixels a module.
			expectOne(t, rotated(src, deg, 0.5, 10), QR, text)
		})
	}
}

func TestQRTwoCodes(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	g1, t1 := qrFull(rng, 3, ecM, 0)
	g2, t2 := qrFull(rng, 5, ecL, 4)
	a, b := gridImage(g1, 4, 4), gridImage(g2, 3, 4)
	img := canvas(700, 450, 240)
	clutter(img, 30, 7)
	warpOnto(img, a, rotatedCorners(float64(a.Bounds().Dx()), float64(a.Bounds().Dy()), 15, 180, 220))
	warpOnto(img, b, rotatedCorners(float64(b.Bounds().Dx()), float64(b.Bounds().Dy()), -40, 520, 230))
	codes := Decode(img)
	if len(codes) != 2 || codes[0].Text != t1 || codes[1].Text != t2 {
		t.Fatalf("got %v", codes)
	}
	// The same code twice is two codes.
	img = canvas(700, 400, 240)
	warpOnto(img, a, rotatedCorners(float64(a.Bounds().Dx()), float64(a.Bounds().Dy()), 0, 170, 200))
	warpOnto(img, a, rotatedCorners(float64(a.Bounds().Dx()), float64(a.Bounds().Dy()), 0, 520, 200))
	codes = Decode(img, QR)
	if len(codes) != 2 || codes[0].Text != t1 || codes[1].Text != t1 || codes[0].Bounds.Min.X > codes[1].Bounds.Min.X {
		t.Fatalf("got %v", codes)
	}
}

func TestQRMirrored(t *testing.T) {
	rng := rand.New(rand.NewSource(8))
	g, text := qrFull(rng, 3, ecQ, 6)
	expectOne(t, gridImage(transpose(g), 4, 4), QR, text, QR)
}

func TestQRLargeModules(t *testing.T) {
	g := qrEncode([]qrSeg{{mode: 2, data: "BIG"}}, 1, ecH, 4)
	for _, sc := range []int{16, 30} {
		t.Run(fmt.Sprint(sc), func(t *testing.T) {
			expectOne(t, gridImage(g, sc, 4), QR, "BIG")
			expectOne(t, rotated(gridImage(g, sc, 4), 30, 1, 10), QR, "BIG")
		})
	}
}
