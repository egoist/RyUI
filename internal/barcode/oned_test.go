package barcode

import (
	"fmt"
	"image"
	"math"
	"testing"
)

type barcodeCase struct {
	name    string
	modules []bool
	kind    Kind
	text    string
}

func barcodeCases() []barcodeCase {
	return []barcodeCase{
		{"ean13", ean13Modules("590123412345"), EAN13, "5901234123457"},
		{"ean13-b", ean13Modules("978030640615"), EAN13, "9780306406157"},
		{"upca", ean13Modules("003600029145"), UPCA, "036000291452"},
		{"ean8", ean8Modules("9638507"), EAN8, "96385074"},
		{"upce", upceModules("01234565"), UPCE, "01234565"},
		{"code128-b", code128Modules(code128B("Hello, World!")), Code128, "Hello, World!"},
		{"code128-c", code128Modules([]int{c128StartC, 12, 34, 56, 78, 90}), Code128, "1234567890"},
		{"code128-gs1", code128Modules([]int{c128StartC, c128FNC1, 1, 9, 50, 12, 34, 56, 78, 90, 12, c128FNC1, 10, 100, 'A' - 32, 'B' - 32}), Code128, "010950123456789012\x1d10AB"},
		{"code128-a", code128Modules([]int{c128StartA, 'A' - 32, 'B' - 32, 64 + 9, 98, 'z' - 32, 'C' - 32}), Code128, "AB\tzC"},
		{"code128-fnc4", code128Modules([]int{c128StartB, 'a' - 32, 100, 0x69 - 32, 'b' - 32}), Code128, "aéb"},
		{"code39", code39Modules("CODE 39-$/+%.", 3), Code39, "CODE 39-$/+%."},
		{"code39-r2", code39Modules("A1B2", 2), Code39, "A1B2"},
	}
}

func TestBarcodesUpright(t *testing.T) {
	for _, c := range barcodeCases() {
		t.Run(c.name, func(t *testing.T) {
			img := barsImage(c.modules, 2, 60, 12)
			codes := expectOne(t, rotated(img, 0, 1, 10), c.kind, c.text)
			if len(codes) == 1 {
				b := codes[0].Bounds
				w := float64(len(c.modules)) * 2
				if d := float64(b.Dx()) - w; d < -6 || d > 6 {
					t.Errorf("bounds %v: %d wide, want about %g", b, b.Dx(), w)
				}
			}
		})
	}
}

func TestBarcodesTurned(t *testing.T) {
	for _, c := range barcodeCases() {
		for _, deg := range []float64{7, 33, 90, 135, 180, 271} {
			t.Run(fmt.Sprintf("%s-%gdeg", c.name, deg), func(t *testing.T) {
				img := barsImage(c.modules, 2.3, int(float64(len(c.modules))*0.9)+10, 12)
				expectOne(t, rotated(img, deg, 1, 10), c.kind, c.text)
			})
		}
	}
}

func TestBarcodesSmallBlurNoise(t *testing.T) {
	for _, c := range barcodeCases() {
		for i, sc := range []float64{1.5, 1.7, 3.3} {
			t.Run(fmt.Sprintf("%s-x%g", c.name, sc), func(t *testing.T) {
				w := float64(len(c.modules)) * sc
				img := barsImage(c.modules, sc, int(w*0.5)+10, 12)
				out := rotated(img, []float64{0, 180, 12}[i], 1, 20)
				out = noisy(blur(out, 0.3*sc), 6, int64(i))
				out = lit(out, 0.5, 15, 240)
				expectOne(t, out, c.kind, c.text)
			})
		}
	}
}

func TestBarcodesInClutter(t *testing.T) {
	for i, c := range barcodeCases() {
		t.Run(c.name, func(t *testing.T) {
			img := barsImage(c.modules, 2.5, 90, 12)
			w, h := float64(img.Bounds().Dx()), float64(img.Bounds().Dy())
			deg := float64(i * 37 % 360)
			side := max(480, int(math.Hypot(w, h))+60)
			out := placed(side+160, side, img, rotatedCorners(w, h, deg, float64(side+160)/2, float64(side)/2), int64(i))
			expectOne(t, blur(out, 0.6), c.kind, c.text)
		})
	}
}

func TestUPCANaming(t *testing.T) {
	img := barsImage(ean13Modules("003600029145"), 2, 60, 12)
	expectOne(t, img, UPCA, "036000291452")
	expectOne(t, img, UPCA, "036000291452", UPCA)
	expectOne(t, img, EAN13, "0036000291452", EAN13)
	expectOne(t, img, UPCA, "036000291452", EAN13, UPCA)
	if codes := Decode(img, EAN8, Code128, QR); len(codes) != 0 {
		t.Errorf("other kinds: %v", codes)
	}
	// An EAN-13 that isn't a UPC-A isn't one when only UPC-A is wanted.
	img = barsImage(ean13Modules("590123412345"), 2, 60, 12)
	if codes := Decode(img, UPCA); len(codes) != 0 {
		t.Errorf("UPC-A only: %v", codes)
	}
	expectOne(t, img, EAN13, "5901234123457", EAN13)
}

func TestKinds(t *testing.T) {
	// A QR code, a Data Matrix and a Code 128 in one image.
	img := canvas(900, 400, 255)
	q := gridImage(qrEncode([]qrSeg{{mode: 4, data: "qr"}}, 2, ecM, 1), 5, 4)
	d := gridImage(dmSymbol(dmEncodeASCII("dm"), dmSizeFor(2, false)), 8, 2)
	c := barsImage(code128Modules(code128B("bars")), 3, 100, 12)
	warpOnto(img, q, rotatedCorners(float64(q.Bounds().Dx()), float64(q.Bounds().Dy()), 0, 120, 200))
	warpOnto(img, d, rotatedCorners(float64(d.Bounds().Dx()), float64(d.Bounds().Dy()), 0, 330, 200))
	warpOnto(img, c, rotatedCorners(float64(c.Bounds().Dx()), float64(c.Bounds().Dy()), 0, 640, 200))
	all := Decode(img)
	if len(all) != 3 {
		t.Fatalf("all kinds: %v", all)
	}
	for _, k := range []Kind{QR, DataMatrix, Code128} {
		got := Decode(img, k)
		if len(got) != 1 || got[0].Kind != k {
			t.Errorf("%s: %v", k, got)
		}
	}
	if got := Decode(img, Code39, EAN13); len(got) != 0 {
		t.Errorf("none wanted: %v", got)
	}
	if got := Decode(img, Kind("aztec")); len(got) != 0 {
		t.Errorf("unknown kind: %v", got)
	}
	// Top to bottom, then left to right.
	if all[0].Kind != QR || all[1].Kind != Code128 || all[2].Kind != DataMatrix {
		t.Errorf("order: %v", all)
	}
	_ = image.Rectangle{}
}

func TestBarcodeTwice(t *testing.T) {
	m := ean13Modules("590123412345")
	b := barsImage(m, 2, 80, 12)
	w, h := float64(b.Bounds().Dx()), float64(b.Bounds().Dy())
	img := canvas(800, 300, 255)
	warpOnto(img, b, rotatedCorners(w, h, 0, 200, 150))
	warpOnto(img, b, rotatedCorners(w, h, 10, 600, 150))
	codes := Decode(img)
	if len(codes) != 2 || codes[0].Text != "5901234123457" || codes[1].Text != "5901234123457" || codes[0].Bounds.Overlaps(codes[1].Bounds) {
		t.Fatalf("got %v", codes)
	}
}
