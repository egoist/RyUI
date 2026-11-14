// Package animimage plays animated images where the platform has no
// player of its own (Windows' WIC and Linux's GDK decode a still, not an
// animation): GIF (image/gif), APNG and animated WebP, their frames read
// here and each frame's picture decoded by Go's PNG decoder or, for
// WebP's VP8 and VP8L bitstreams, by a decoder the host gives (WIC's,
// GDK's). A Player composites frame after frame onto a canvas as the
// formats say (disposal, blending), so only the canvas and the frames'
// own rectangles are kept, not every whole frame.
package animimage

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/draw"
	"image/gif"
	"image/png"
	"time"
)

// Disposal is what happens to a frame's rectangle before the next.
type Disposal uint8

const (
	DisposeNone       Disposal = iota // left as it is
	DisposeBackground                 // cleared to transparent
	DisposePrevious                   // restored to what it was before
)

// Frame is one picture of an animation: drawn at its rectangle of the
// canvas, for Delay.
type Frame struct {
	Image    image.Image // its pixels, its bounds its place on the canvas
	Delay    time.Duration
	Disposal Disposal
	// Blend draws it over what's there (alpha); else it replaces it.
	Blend bool
}

// Animation is an animated image.
type Animation struct {
	Width, Height int
	Frames        []Frame
	// Loops is how many times it plays (0: forever).
	Loops int
}

// StillDecoder decodes a still image file (a single-frame WebP).
type StillDecoder func(data []byte) (image.Image, error)

// ErrNotAnimated is a file that's a still image (or not one of these).
var ErrNotAnimated = errors.New("animimage: not an animated image")

// Decode reads an animated GIF, APNG or WebP; ErrNotAnimated for a still
// one (one frame) or another format. still decodes WebP's frames (nil:
// a WebP isn't read).
func Decode(data []byte, still StillDecoder) (*Animation, error) {
	switch {
	case bytes.HasPrefix(data, []byte("GIF8")):
		return decodeGIF(data)
	case bytes.HasPrefix(data, pngSignature):
		return decodeAPNG(data)
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		if still == nil {
			return nil, ErrNotAnimated
		}
		return decodeWebP(data, still)
	}
	return nil, ErrNotAnimated
}

// IsAnimated reports whether data looks like an animated GIF, APNG or
// WebP, from its headers, without decoding it.
func IsAnimated(data []byte) bool {
	switch {
	case bytes.HasPrefix(data, []byte("GIF8")):
		// More than one image descriptor is likelier than not; Decode says.
		return bytes.Count(data, []byte{0x21, 0xF9, 0x04}) > 1
	case bytes.HasPrefix(data, pngSignature):
		for c := range pngChunks(data) {
			switch c.typ {
			case "acTL":
				return len(c.data) >= 4 && binary.BigEndian.Uint32(c.data) > 1
			case "IDAT":
				return false
			}
		}
	case len(data) >= 21 && string(data[:4]) == "RIFF" && string(data[8:16]) == "WEBPVP8X":
		return data[20]&2 != 0
	}
	return false
}

// delay is a frame's time, as browsers show it: 10 ms or less is 100 ms.
func delay(d time.Duration) time.Duration {
	if d <= 10*time.Millisecond {
		return 100 * time.Millisecond
	}
	return d
}

func decodeGIF(data []byte) (*Animation, error) {
	g, err := gif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if len(g.Image) < 2 {
		return nil, ErrNotAnimated
	}
	a := &Animation{Width: g.Config.Width, Height: g.Config.Height}
	switch {
	case g.LoopCount == 0:
		a.Loops = 0
	case g.LoopCount < 0:
		a.Loops = 1
	default:
		a.Loops = g.LoopCount + 1
	}
	if a.Width == 0 || a.Height == 0 {
		for _, im := range g.Image {
			b := im.Bounds()
			a.Width, a.Height = max(a.Width, b.Max.X), max(a.Height, b.Max.Y)
		}
	}
	for i, im := range g.Image {
		f := Frame{Image: im, Blend: true}
		if i < len(g.Delay) {
			f.Delay = time.Duration(g.Delay[i]) * 10 * time.Millisecond
		}
		f.Delay = delay(f.Delay)
		if i < len(g.Disposal) {
			switch g.Disposal[i] {
			case gif.DisposalBackground:
				f.Disposal = DisposeBackground
			case gif.DisposalPrevious:
				f.Disposal = DisposePrevious
			}
		}
		a.Frames = append(a.Frames, f)
	}
	return a, nil
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

type pngChunk struct {
	typ  string
	data []byte
}

// pngChunks yields a PNG's chunks, in order, up to a malformed one.
func pngChunks(data []byte) func(func(pngChunk) bool) {
	return func(yield func(pngChunk) bool) {
		p := len(pngSignature)
		for p+12 <= len(data) {
			n := int(binary.BigEndian.Uint32(data[p:]))
			if n < 0 || p+12+n > len(data) {
				return
			}
			if !yield(pngChunk{string(data[p+4 : p+8]), data[p+8 : p+8+n]}) {
				return
			}
			p += 12 + n
		}
	}
}

func appendChunk(b []byte, typ string, data []byte) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(data)))
	start := len(b)
	b = append(b, typ...)
	b = append(b, data...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b[start:]))
}

// decodeAPNG reads an APNG's frames: each is a PNG of its own (the IHDR
// at its size, the chunks before the image data, its IDAT or fdATs as
// IDAT), decoded by image/png.
func decodeAPNG(data []byte) (*Animation, error) {
	var ihdr []byte
	var shared []pngChunk // chunks before the image data, for every frame
	type raw struct {
		fctl []byte
		idat [][]byte
	}
	var frames []raw
	var cur *raw
	plays, numFrames := 0, 0
	seenIDAT := false
	idatIsFrame := false
	for c := range pngChunks(data) {
		switch c.typ {
		case "IHDR":
			ihdr = c.data
		case "acTL":
			if len(c.data) >= 8 {
				numFrames = int(binary.BigEndian.Uint32(c.data))
				plays = int(binary.BigEndian.Uint32(c.data[4:]))
			}
		case "fcTL":
			if len(c.data) < 26 {
				return nil, errors.New("animimage: a short fcTL")
			}
			frames = append(frames, raw{fctl: c.data})
			cur = &frames[len(frames)-1]
			if !seenIDAT {
				idatIsFrame = true
			}
		case "IDAT":
			seenIDAT = true
			if idatIsFrame && cur != nil {
				cur.idat = append(cur.idat, c.data)
			}
		case "fdAT":
			if cur != nil && len(c.data) >= 4 {
				cur.idat = append(cur.idat, c.data[4:])
			}
		case "IEND":
		default:
			// PLTE, tRNS, gAMA, iCCP...: what every frame's pixels need.
			if !seenIDAT {
				shared = append(shared, pngChunk{c.typ, c.data})
			}
		}
	}
	if ihdr == nil || numFrames < 2 || len(frames) < 2 {
		return nil, ErrNotAnimated
	}
	a := &Animation{Width: int(binary.BigEndian.Uint32(ihdr)), Height: int(binary.BigEndian.Uint32(ihdr[4:])), Loops: plays}
	for _, f := range frames {
		if len(f.idat) == 0 {
			continue
		}
		fc := f.fctl
		w, h := binary.BigEndian.Uint32(fc[4:]), binary.BigEndian.Uint32(fc[8:])
		x, y := int(binary.BigEndian.Uint32(fc[12:])), int(binary.BigEndian.Uint32(fc[16:]))
		num, den := binary.BigEndian.Uint16(fc[20:]), binary.BigEndian.Uint16(fc[22:])
		if den == 0 {
			den = 100
		}
		hdr := append([]byte(nil), ihdr...)
		binary.BigEndian.PutUint32(hdr, w)
		binary.BigEndian.PutUint32(hdr[4:], h)
		b := append([]byte(nil), pngSignature...)
		b = appendChunk(b, "IHDR", hdr)
		for _, s := range shared {
			b = appendChunk(b, s.typ, s.data)
		}
		for _, d := range f.idat {
			b = appendChunk(b, "IDAT", d)
		}
		b = appendChunk(b, "IEND", nil)
		img, err := png.Decode(bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		frame := Frame{
			Image: moved(img, x, y),
			Delay: delay(time.Duration(num) * time.Second / time.Duration(den)),
			Blend: fc[25] == 1,
		}
		switch fc[24] {
		case 1:
			frame.Disposal = DisposeBackground
		case 2:
			frame.Disposal = DisposePrevious
		}
		a.Frames = append(a.Frames, frame)
	}
	if len(a.Frames) < 2 {
		return nil, ErrNotAnimated
	}
	return a, nil
}

// moved is img with its bounds at x, y.
func moved(img image.Image, x, y int) image.Image {
	b := img.Bounds()
	if b.Min.X == x && b.Min.Y == y {
		return img
	}
	out := image.NewNRGBA(image.Rect(x, y, x+b.Dx(), y+b.Dy()))
	draw.Draw(out, out.Rect, img, b.Min, draw.Src)
	return out
}

func u24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

// decodeWebP reads an animated WebP's ANMF frames, each made a still
// WebP of its own (its ALPH and VP8, or its VP8L) for still to decode.
func decodeWebP(data []byte, still StillDecoder) (*Animation, error) {
	if len(data) < 30 || string(data[12:16]) != "VP8X" || data[20]&2 == 0 {
		return nil, ErrNotAnimated
	}
	a := &Animation{Width: u24(data[24:]) + 1, Height: u24(data[27:]) + 1}
	p := 12
	for p+8 <= len(data) {
		typ := string(data[p : p+4])
		n := int(binary.LittleEndian.Uint32(data[p+4:]))
		if n < 0 || p+8+n > len(data) {
			break
		}
		body := data[p+8 : p+8+n]
		switch typ {
		case "ANIM":
			if len(body) >= 6 {
				a.Loops = int(binary.LittleEndian.Uint16(body[4:]))
			}
		case "ANMF":
			if len(body) < 16 {
				return nil, errors.New("animimage: a short ANMF")
			}
			x, y := u24(body)*2, u24(body[3:])*2
			w, h := u24(body[6:])+1, u24(body[9:])+1
			d := time.Duration(u24(body[12:])) * time.Millisecond
			flags := body[15]
			img, err := still(stillWebP(body[16:], w, h))
			if err != nil {
				return nil, err
			}
			f := Frame{Image: moved(img, x, y), Delay: delay(d), Blend: flags&2 == 0}
			if flags&1 != 0 {
				f.Disposal = DisposeBackground
			}
			a.Frames = append(a.Frames, f)
		}
		p += 8 + n + n&1
	}
	if len(a.Frames) < 2 {
		return nil, ErrNotAnimated
	}
	return a, nil
}

// stillWebP is a frame's chunks as a WebP file: with an ALPH chunk, an
// extended one (VP8X with the alpha flag) of its size.
func stillWebP(chunks []byte, w, h int) []byte {
	alpha := len(chunks) >= 4 && string(chunks[:4]) == "ALPH"
	var body []byte
	body = append(body, "WEBP"...)
	if alpha {
		x := make([]byte, 10)
		x[0] = 0x10
		x[4], x[5], x[6] = byte(w-1), byte((w-1)>>8), byte((w-1)>>16)
		x[7], x[8], x[9] = byte(h-1), byte((h-1)>>8), byte((h-1)>>16)
		body = append(body, "VP8X"...)
		body = binary.LittleEndian.AppendUint32(body, 10)
		body = append(body, x...)
	}
	body = append(body, chunks...)
	out := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(out, body...)
}

// Player composites an animation's frames in order onto its canvas.
type Player struct {
	a      *Animation
	canvas *image.NRGBA
	saved  *image.NRGBA // DisposePrevious's: the rectangle before the frame
	next   int          // the frame Next draws
	loop   int          // loops finished
	last   int          // the frame drawn, and its disposal pending
}

// NewPlayer is a Player at its first frame (Next draws it).
func NewPlayer(a *Animation) *Player {
	return &Player{a: a, canvas: image.NewNRGBA(image.Rect(0, 0, a.Width, a.Height)), last: -1}
}

// Canvas is what the frames drawn so far show: RGBA, not premultiplied.
// It's drawn on by Next.
func (p *Player) Canvas() *image.NRGBA { return p.canvas }

// Frame is the index of the frame on the canvas (-1 before the first).
func (p *Player) Frame() int { return p.last }

// Next draws the next frame on the canvas and returns how long it shows;
// done once the animation has played its loops (the canvas keeps its
// last frame).
func (p *Player) Next() (d time.Duration, done bool) {
	fs := p.a.Frames
	if p.next >= len(fs) {
		p.loop++
		if p.a.Loops > 0 && p.loop >= p.a.Loops {
			return 0, true
		}
		p.next = 0
	}
	if p.last >= 0 {
		p.dispose(fs[p.last])
	}
	if p.next == 0 {
		clear(p.canvas.Pix) // a loop starts on a clear canvas
	}
	f := fs[p.next]
	r := f.Image.Bounds().Intersect(p.canvas.Rect)
	if f.Disposal == DisposePrevious {
		p.saved = image.NewNRGBA(r)
		draw.Draw(p.saved, r, p.canvas, r.Min, draw.Src)
	}
	op := draw.Src
	if f.Blend {
		op = draw.Over
	}
	draw.Draw(p.canvas, r, f.Image, r.Min, op)
	p.last = p.next
	p.next++
	return f.Delay, false
}

// Seek draws frame i (from the start, on a clear canvas).
func (p *Player) Seek(i int) {
	clear(p.canvas.Pix)
	p.next, p.last, p.loop, p.saved = 0, -1, 0, nil
	for p.next <= i && p.next < len(p.a.Frames) {
		p.Next()
	}
}

func (p *Player) dispose(f Frame) {
	r := f.Image.Bounds().Intersect(p.canvas.Rect)
	switch f.Disposal {
	case DisposeBackground:
		draw.Draw(p.canvas, r, image.Transparent, image.Point{}, draw.Src)
	case DisposePrevious:
		if p.saved != nil {
			draw.Draw(p.canvas, r, p.saved, r.Min, draw.Src)
			p.saved = nil
		}
	}
}
