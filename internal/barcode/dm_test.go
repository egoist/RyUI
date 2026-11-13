package barcode

import (
	"fmt"
	"math/rand"
	"testing"
)

// dmFull is a symbol of size s full of ASCII text.
func dmFull(rng *rand.Rand, s *dmSize) (*grid, string) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ -./"
	// Letters take a codeword each; leave a few out so it pads too.
	n := s.data - rng.Intn(min(3, s.data))
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rng.Intn(len(chars))]
	}
	text := string(b)
	return dmSymbol(dmEncodeASCII(text), s), text
}

func TestDataMatrixSizes(t *testing.T) {
	rng := rand.New(rand.NewSource(10))
	for i := range dmSizes {
		s := &dmSizes[i]
		g, text := dmFull(rng, s)
		t.Run(s.String(), func(t *testing.T) {
			expectOne(t, gridImage(g, 4, 2), DataMatrix, text, DataMatrix)
		})
	}
}

func TestDataMatrixTransformed(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	angles := []float64{0, 7, 33, 90, 135, 180, 271}
	sizes := []int{0, 3, 8, 9, 13, 16, 24, 26, 29}
	for i, si := range sizes {
		s := &dmSizes[si]
		g, text := dmFull(rng, s)
		src := gridImage(g, 6, 2)
		for k, deg := range []float64{angles[i%len(angles)], angles[(i+3)%len(angles)]} {
			sc := []float64{0.75, 1.3}[k]
			t.Run(fmt.Sprintf("%v-%gdeg-x%g", s, deg, sc), func(t *testing.T) {
				expectOne(t, rotated(src, deg, sc, 20), DataMatrix, text)
			})
		}
	}
	g, text := dmFull(rng, &dmSizes[6])
	src := gridImage(g, 8, 2)
	for _, deg := range angles {
		t.Run(fmt.Sprintf("22x22-%gdeg-photo", deg), func(t *testing.T) {
			img := rotated(src, deg, 1, 30)
			img = lit(noisy(blur(img, 1.1), 8, int64(deg)), 0.45, 20, 235)
			expectOne(t, img, DataMatrix, text)
		})
	}
}

func TestDataMatrixPerspective(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	for _, si := range []int{2, 7, 10, 27} {
		s := &dmSizes[si]
		g, text := dmFull(rng, s)
		src := gridImage(g, 6, 2)
		w, h := float64(src.Bounds().Dx()), float64(src.Bounds().Dy())
		corners := [4]point{{60 + 0.1*w, 40}, {60 + 0.93*w, 40 + 0.05*h}, {60 + w, 40 + 0.97*h}, {60, 40 + 0.9*h}}
		t.Run(s.String(), func(t *testing.T) {
			img := placed(int(w)+120, int(h)+100, src, corners, int64(si))
			expectOne(t, blur(img, 0.7), DataMatrix, text)
		})
	}
}

func TestDataMatrixDamaged(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	for _, si := range []int{4, 11, 17} {
		s := &dmSizes[si]
		g, text := dmFull(rng, s)
		// Flip a few data modules: well within what the check codewords
		// correct.
		nr, nc := s.mapRows(), s.mapCols()
		place := dmPlacement(nr, nc)
		perBlock := map[int]int{}
		for _, cw := range rng.Perm(s.total()) {
			b := cw % s.blocks
			if perBlock[b] >= s.check/2-1 {
				continue
			}
			perBlock[b]++
			p := int(place[8*cw+rng.Intn(8)])
			y, x := s.symbolPos(p/nc, p%nc)
			g.set(x, y, !g.at(x, y))
		}
		t.Run(s.String(), func(t *testing.T) {
			expectOne(t, gridImage(g, 4, 2), DataMatrix, text)
		})
	}
}

// c40Pack packs C40, Text or X12 values three to two codewords.
func c40Pack(vals ...int) []uint8 {
	var out []uint8
	for i := 0; i+2 < len(vals); i += 3 {
		v := 1600*vals[i] + 40*vals[i+1] + vals[i+2] + 1
		out = append(out, uint8(v>>8), uint8(v))
	}
	return out
}

func edifactPack(text string) []uint8 {
	var bits []int
	for _, c := range []byte(text) {
		bits = append(bits, int(c)&0x3F)
	}
	bits = append(bits, 0x1F)
	var out []uint8
	acc, n := 0, 0
	for _, v := range bits {
		acc = acc<<6 | v
		n += 6
		for n >= 8 {
			out = append(out, uint8(acc>>(n-8)))
			n -= 8
		}
	}
	if n > 0 {
		out = append(out, uint8(acc<<(8-n)))
	}
	return out
}

func base256(start int, data []byte) []uint8 {
	// start is the 1-based position of the length codeword.
	raw := append([]byte{byte(len(data))}, data...)
	var out []uint8
	for i, c := range raw {
		p := start + i
		out = append(out, uint8((int(c)+149*p%255+1)%256))
	}
	return out
}

func cat(parts ...[]uint8) []uint8 {
	var out []uint8
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func TestDataMatrixEncodations(t *testing.T) {
	cases := []struct {
		name string
		cw   []uint8
		want string
	}{
		{"ascii-digits", dmEncodeASCII("A123456b"), "A123456b"},
		{"upper-shift", []uint8{'a' + 1, 235, 0xE9 - 127}, "aé"},
		// C40: "AB1" then shift 2 "!" and shift 3 "a" (as 'a'-96 = 1).
		{"c40", cat([]uint8{230}, c40Pack(14, 15, 5, 1, 0, 2, 1, 3, 3), []uint8{254}, dmEncodeASCII("x")), "AB1!a  x"},
		{"text", cat([]uint8{239}, c40Pack(14, 15, 3, 1, 0, 3), []uint8{254}), "ab ! "},
		{"text-shift3", cat([]uint8{239}, c40Pack(3, 2, 1), []uint8{254}), " A"},
		{"x12", cat([]uint8{238}, c40Pack(14, 4, 3, 0, 1, 2), []uint8{254}), "A0 \r*>"},
		{"edifact", cat([]uint8{240}, edifactPack("ABC.12"), dmEncodeASCII("z")), "ABC.12z"},
		{"base256", cat([]uint8{231}, base256(2, []byte("héllo")), dmEncodeASCII("!")), "héllo!"},
		{"base256-latin1", cat([]uint8{231}, base256(2, []byte{'c', 0xE9})), "cé"},
		{"eci-utf8", cat([]uint8{241, 27}, []uint8{235, 0xC3 - 127, 235, 0xA9 - 127}), "é"},
		{"gs1", cat([]uint8{232}, dmEncodeASCII("0112345678901231"), []uint8{232}, dmEncodeASCII("10AB")), "0112345678901231\x1d10AB"},
		{"macro05", cat([]uint8{236}, dmEncodeASCII("X")), "[)>\x1e05\x1dX\x1e\x04"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := dmDecodeData(cat(c.cw, []uint8{129}))
			if !ok || got != c.want {
				t.Errorf("got %q %v, want %q", got, ok, c.want)
			}
			s := dmSizeFor(len(c.cw), false)
			expectOne(t, gridImage(dmSymbol(c.cw, s), 4, 2), DataMatrix, c.want, DataMatrix)
		})
	}
}

func TestDataMatrixTwo(t *testing.T) {
	rng := rand.New(rand.NewSource(14))
	g1, t1 := dmFull(rng, &dmSizes[5])
	g2, t2 := dmFull(rng, &dmSizes[26])
	a, b := gridImage(g1, 6, 2), gridImage(g2, 5, 2)
	img := canvas(800, 400, 240)
	clutter(img, 30, 9)
	warpOnto(img, a, rotatedCorners(float64(a.Bounds().Dx()), float64(a.Bounds().Dy()), 25, 180, 200))
	warpOnto(img, b, rotatedCorners(float64(b.Bounds().Dx()), float64(b.Bounds().Dy()), -60, 560, 210))
	codes := Decode(img)
	if len(codes) != 2 || codes[0].Text != t1 || codes[1].Text != t2 {
		t.Fatalf("got %v", codes)
	}
}

func TestDataMatrixLargeModules(t *testing.T) {
	g := dmSymbol(dmEncodeASCII("BIG"), dmSizeFor(3, false))
	for _, sc := range []int{16, 30} {
		t.Run(fmt.Sprint(sc), func(t *testing.T) {
			expectOne(t, gridImage(g, sc, 2), DataMatrix, "BIG")
			expectOne(t, rotated(gridImage(g, sc, 2), 30, 1, 10), DataMatrix, "BIG")
		})
	}
}

func TestDataMatrixSmallModules(t *testing.T) {
	rng := rand.New(rand.NewSource(15))
	g, text := dmFull(rng, &dmSizes[8])
	for _, deg := range []float64{0, 20, 45} {
		t.Run(fmt.Sprintf("%gdeg", deg), func(t *testing.T) {
			// Three pixels a module.
			expectOne(t, rotated(gridImage(g, 6, 2), deg, 0.5, 10), DataMatrix, text)
		})
	}
}
