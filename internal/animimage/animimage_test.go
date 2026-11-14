package animimage

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"testing"
	"time"
)

var (
	red  = color.NRGBA{255, 0, 0, 255}
	blue = color.NRGBA{0, 0, 255, 255}
	none = color.NRGBA{}
)

func solid(r image.Rectangle, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(r)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func at(p *Player, x, y int) color.NRGBA { return p.Canvas().NRGBAAt(x, y) }

func TestGIF(t *testing.T) {
	pal := color.Palette{color.NRGBA{}, red, blue}
	f0 := image.NewPaletted(image.Rect(0, 0, 4, 4), pal)
	for i := range f0.Pix {
		f0.Pix[i] = 1
	}
	f1 := image.NewPaletted(image.Rect(2, 2, 4, 4), pal) // a corner, over the first
	for i := range f1.Pix {
		f1.Pix[i] = 2
	}
	var b bytes.Buffer
	if err := gif.EncodeAll(&b, &gif.GIF{Image: []*image.Paletted{f0, f1}, Delay: []int{5, 0}, Disposal: []byte{0, gif.DisposalBackground},
		LoopCount: 2, Config: image.Config{Width: 4, Height: 4}}); err != nil {
		t.Fatal(err)
	}
	if !IsAnimated(b.Bytes()) {
		t.Error("IsAnimated: false")
	}
	a, err := Decode(b.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	// LoopCount 2 repeats twice: three plays. A delay of 0 is 100 ms.
	if a.Width != 4 || a.Height != 4 || len(a.Frames) != 2 || a.Loops != 3 || a.Frames[0].Delay != 50*time.Millisecond || a.Frames[1].Delay != 100*time.Millisecond {
		t.Fatalf("%dx%d, %d frames, %d loops, %v %v", a.Width, a.Height, len(a.Frames), a.Loops, a.Frames[0].Delay, a.Frames[1].Delay)
	}
	p := NewPlayer(a)
	p.Next()
	if at(p, 3, 3) != red {
		t.Fatal("frame 0")
	}
	p.Next()
	if at(p, 3, 3) != blue || at(p, 0, 0) != red {
		t.Fatal("frame 1 over frame 0")
	}
	// Frame 1's rectangle is cleared before the next loop's frame 0.
	p.Next()
	if p.Frame() != 0 || at(p, 3, 3) != red {
		t.Fatal("the second loop")
	}
	p.Next()
	p.Next()
	p.Next()
	if _, done := p.Next(); !done {
		t.Fatal("three plays, then done")
	}
	if at(p, 3, 3) != blue {
		t.Fatal("the last frame stays")
	}
}

// apng makes an APNG of frames (rectangles of the canvas), the first the
// default image, from image/png's encodings.
func apng(t *testing.T, w, h int, plays int, frames []*image.NRGBA, dispose, blend []byte) []byte {
	out := append([]byte(nil), pngSignature...)
	seq := uint32(0)
	actl := binary.BigEndian.AppendUint32(nil, uint32(len(frames)))
	actl = binary.BigEndian.AppendUint32(actl, uint32(plays))
	for i, f := range frames {
		var b bytes.Buffer
		if err := png.Encode(&b, f); err != nil {
			t.Fatal(err)
		}
		r := f.Bounds()
		fc := binary.BigEndian.AppendUint32(nil, seq)
		seq++
		for _, v := range []int{r.Dx(), r.Dy(), r.Min.X, r.Min.Y} {
			fc = binary.BigEndian.AppendUint32(fc, uint32(v))
		}
		fc = binary.BigEndian.AppendUint16(fc, 3)
		fc = binary.BigEndian.AppendUint16(fc, 100) // 30 ms
		fc = append(fc, dispose[i], blend[i])
		for c := range pngChunks(b.Bytes()) {
			switch {
			case c.typ == "IHDR" && i == 0:
				hdr := append([]byte(nil), c.data...)
				binary.BigEndian.PutUint32(hdr, uint32(w))
				binary.BigEndian.PutUint32(hdr[4:], uint32(h))
				out = appendChunk(out, "IHDR", hdr)
				out = appendChunk(out, "acTL", actl)
				out = appendChunk(out, "fcTL", fc)
			case c.typ == "IDAT" && i == 0:
				out = appendChunk(out, "IDAT", c.data)
			case c.typ == "IDAT":
				if fc != nil {
					out = appendChunk(out, "fcTL", fc)
					fc = nil
				}
				out = appendChunk(out, "fdAT", append(binary.BigEndian.AppendUint32(nil, seq), c.data...))
				seq++
			}
		}
	}
	return appendChunk(out, "IEND", nil)
}

func TestAPNG(t *testing.T) {
	// Every frame has the IHDR's color type: RGBA (a pixel not quite opaque
	// keeps image/png from writing the first as RGB).
	first := solid(image.Rect(0, 0, 4, 4), red)
	first.SetNRGBA(3, 0, color.NRGBA{255, 0, 0, 254})
	data := apng(t, 4, 4, 0, []*image.NRGBA{first, solid(image.Rect(1, 1, 3, 3), color.NRGBA{0, 0, 255, 128})},
		[]byte{0, 2}, []byte{0, 1})
	if !IsAnimated(data) {
		t.Error("IsAnimated: false")
	}
	// The APNG's first frame is a PNG any decoder reads.
	if img, err := png.Decode(bytes.NewReader(data)); err != nil || img.Bounds().Dx() != 4 {
		t.Fatalf("as a PNG: %v", err)
	}
	a, err := Decode(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Frames) != 2 || a.Loops != 0 || a.Frames[1].Delay != 30*time.Millisecond || !a.Frames[1].Blend || a.Frames[1].Disposal != DisposePrevious {
		t.Fatalf("%+v", a)
	}
	p := NewPlayer(a)
	p.Next()
	p.Next()
	// Blended: half blue over red.
	if c := at(p, 1, 1); c.R < 120 || c.R > 135 || c.B < 120 || c.B > 135 || c.A != 255 {
		t.Fatalf("blended: %v", c)
	}
	// Disposed to what was there before it, then the loop starts again.
	p.Next()
	if at(p, 1, 1) != red || p.Frame() != 0 {
		t.Fatalf("disposed to previous: %v", at(p, 1, 1))
	}
	p.Seek(1)
	if p.Frame() != 1 || at(p, 1, 1) == red {
		t.Fatal("Seek")
	}
}

// webp makes an animated WebP of frames whose bitstreams are fake (a
// VP8L chunk of one color byte), decoded by fakeStill.
func webp(loops int, frames []struct {
	r     image.Rectangle
	c     byte
	flags byte
}) []byte {
	var body []byte
	body = append(body, "WEBPVP8X"...)
	body = binary.LittleEndian.AppendUint32(body, 10)
	body = append(body, 0x12, 0, 0, 0, 3, 0, 0, 3, 0, 0) // animation, alpha; 4×4
	body = append(body, "ANIM"...)
	body = binary.LittleEndian.AppendUint32(body, 6)
	body = append(body, 0, 0, 0, 0, byte(loops), 0)
	for _, f := range frames {
		var fr []byte
		put24 := func(v int) { fr = append(fr, byte(v), byte(v>>8), byte(v>>16)) }
		put24(f.r.Min.X / 2)
		put24(f.r.Min.Y / 2)
		put24(f.r.Dx() - 1)
		put24(f.r.Dy() - 1)
		put24(40)
		fr = append(fr, f.flags)
		fr = append(fr, "VP8L"...)
		fr = binary.LittleEndian.AppendUint32(fr, 1)
		fr = append(fr, f.c, 0) // padded
		body = append(body, "ANMF"...)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(fr)))
		body = append(body, fr...)
	}
	return append(append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...), body...)
}

// fakeStill decodes webp's fake frames: a still WebP holding one VP8L
// chunk of a color byte, at the size its caller made it.
func fakeStill(data []byte) (image.Image, error) {
	if string(data[:4]) != "RIFF" || string(data[8:16]) != "WEBPVP8L" {
		return nil, ErrNotAnimated
	}
	c := red
	if data[20] == 'b' {
		c = blue
	}
	return solid(image.Rect(0, 0, 2, 2), c), nil
}

func TestWebP(t *testing.T) {
	data := webp(1, []struct {
		r     image.Rectangle
		c     byte
		flags byte
	}{{image.Rect(0, 0, 2, 2), 'r', 1}, {image.Rect(2, 2, 4, 4), 'b', 2}})
	if !IsAnimated(data) {
		t.Error("IsAnimated: false")
	}
	a, err := Decode(data, fakeStill)
	if err != nil {
		t.Fatal(err)
	}
	if a.Width != 4 || a.Height != 4 || len(a.Frames) != 2 || a.Loops != 1 || a.Frames[0].Delay != 40*time.Millisecond ||
		a.Frames[0].Disposal != DisposeBackground || a.Frames[1].Blend {
		t.Fatalf("%+v", a)
	}
	p := NewPlayer(a)
	p.Next()
	if at(p, 0, 0) != red || at(p, 3, 3) != none {
		t.Fatal("frame 0")
	}
	p.Next()
	// Frame 0 was disposed to the background; frame 1 is at 2, 2.
	if at(p, 0, 0) != none || at(p, 3, 3) != blue {
		t.Fatal("frame 1")
	}
	if _, done := p.Next(); !done {
		t.Fatal("one play")
	}
	// Without a decoder for its frames, a WebP isn't read here.
	if _, err := Decode(data, nil); err != ErrNotAnimated {
		t.Fatal(err)
	}
}

func TestStills(t *testing.T) {
	var b bytes.Buffer
	png.Encode(&b, solid(image.Rect(0, 0, 2, 2), red))
	if IsAnimated(b.Bytes()) {
		t.Error("a PNG is animated")
	}
	if _, err := Decode(b.Bytes(), nil); err != ErrNotAnimated {
		t.Errorf("a PNG: %v", err)
	}
	b.Reset()
	gif.Encode(&b, solid(image.Rect(0, 0, 2, 2), red), nil)
	if _, err := Decode(b.Bytes(), nil); err != ErrNotAnimated {
		t.Errorf("a one-frame GIF: %v", err)
	}
}

// Files made by others' encoders: ffmpeg's APNG of its test pattern (3
// frames: testdata/f0..f2.png), and img2webp's WebP of the same frames
// (lossless, lossless and lossy, looping twice).
func TestRealFiles(t *testing.T) {
	data, err := os.ReadFile("testdata/test.apng")
	if err != nil {
		t.Fatal(err)
	}
	a, err := Decode(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Width != 24 || a.Height != 16 || len(a.Frames) != 3 || a.Loops != 0 || a.Frames[0].Delay != 100*time.Millisecond {
		t.Fatalf("apng: %dx%d, %d frames, %d loops, %v", a.Width, a.Height, len(a.Frames), a.Loops, a.Frames[0].Delay)
	}
	// Each frame composited is the picture ffmpeg was given, pixel for pixel.
	p := NewPlayer(a)
	for i := range 3 {
		p.Next()
		f, err := os.Open(fmt.Sprintf("testdata/f%d.png", i))
		if err != nil {
			t.Fatal(err)
		}
		want, err := png.Decode(f)
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
		for y := range 16 {
			for x := range 24 {
				r, g, b, _ := want.At(x, y).RGBA()
				if c := at(p, x, y); c.R != uint8(r>>8) || c.G != uint8(g>>8) || c.B != uint8(b>>8) || c.A != 255 {
					t.Fatalf("apng frame %d at %d,%d: %v, want %v", i, x, y, c, want.At(x, y))
				}
			}
		}
	}

	data, err = os.ReadFile("testdata/test.webp")
	if err != nil {
		t.Fatal(err)
	}
	var stills [][]byte
	a, err = Decode(data, func(b []byte) (image.Image, error) {
		// A well-formed still: RIFF's size, then VP8L or (VP8X, ALPH) VP8.
		if string(b[:4]) != "RIFF" || int(binary.LittleEndian.Uint32(b[4:])) != len(b)-8 || string(b[8:12]) != "WEBP" {
			t.Fatalf("a still isn't a WebP: % x", b[:16])
		}
		kind := string(b[12:16])
		if kind != "VP8L" && kind != "VP8 " && kind != "VP8X" {
			t.Fatalf("a still's first chunk: %q", kind)
		}
		stills = append(stills, b)
		return solid(image.Rect(0, 0, 24, 16), red), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Width != 24 || a.Height != 16 || len(a.Frames) != 3 || a.Loops != 2 || len(stills) != 3 ||
		a.Frames[0].Delay != 100*time.Millisecond || a.Frames[2].Delay != 50*time.Millisecond {
		t.Fatalf("webp: %dx%d, %d frames, %d loops, %v", a.Width, a.Height, len(a.Frames), a.Loops, a.Frames[2].Delay)
	}
}
