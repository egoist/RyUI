// Package barcode reads barcodes and QR codes in images, in Go alone:
// QR codes, Data Matrix, EAN-13, EAN-8, UPC-A, UPC-E, Code 128 and
// Code 39, upright or turned by any angle. The Windows host reads
// scan.File's images, vision's codes and the camera's frames with it.
package barcode

import (
	"image"
	"image/color"
	"slices"
	"sync"
)

// Kind is a code's symbology, as asuka/scan names them.
type Kind string

const (
	QR         Kind = "qr"
	DataMatrix Kind = "datamatrix"
	EAN13      Kind = "ean13"
	EAN8       Kind = "ean8"
	UPCA       Kind = "upca"
	UPCE       Kind = "upce"
	Code128    Kind = "code128"
	Code39     Kind = "code39"
)

// Code is a code found: its kind, what it says, where (pixels of the
// image given).
type Code struct {
	Kind   Kind
	Text   string
	Bounds image.Rectangle
}

// kindSet is the kinds wanted.
type kindSet uint16

const (
	kQR kindSet = 1 << iota
	kDataMatrix
	kEAN13
	kEAN8
	kUPCA
	kUPCE
	kCode128
	kCode39

	kAll = kQR | kDataMatrix | kEAN13 | kEAN8 | kUPCA | kUPCE | kCode128 | kCode39
	k1D  = kEAN13 | kEAN8 | kUPCA | kUPCE | kCode128 | kCode39
)

func (k Kind) bit() kindSet {
	switch k {
	case QR:
		return kQR
	case DataMatrix:
		return kDataMatrix
	case EAN13:
		return kEAN13
	case EAN8:
		return kEAN8
	case UPCA:
		return kUPCA
	case UPCE:
		return kUPCE
	case Code128:
		return kCode128
	case Code39:
		return kCode39
	}
	return 0
}

// Decode finds the codes of the kinds given (every kind it reads, if
// none) in img.
func Decode(img image.Image, kinds ...Kind) []Code {
	want := kAll
	if len(kinds) > 0 {
		want = 0
		for _, k := range kinds {
			want |= k.bit()
		}
	}
	if want == 0 {
		return nil
	}
	lum := luminanceOf(img)
	defer lum.release()
	if lum.w == 0 || lum.h == 0 {
		return nil
	}
	var codes []Code
	if want&(kQR|kDataMatrix) != 0 {
		bin := binarize(lum)
		if want&kQR != 0 {
			codes = append(codes, decodeQRCodes(bin)...)
		}
		if want&kDataMatrix != 0 {
			codes = append(codes, decodeDataMatrixCodes(bin)...)
		}
		bin.release()
	}
	if want&k1D != 0 {
		codes = append(codes, decode1D(lum, want)...)
	}
	codes = dedupe(codes)
	off := img.Bounds().Min
	for i := range codes {
		codes[i].Bounds = codes[i].Bounds.Add(off)
	}
	slices.SortStableFunc(codes, func(a, b Code) int {
		if a.Bounds.Min.Y != b.Bounds.Min.Y {
			return a.Bounds.Min.Y - b.Bounds.Min.Y
		}
		return a.Bounds.Min.X - b.Bounds.Min.X
	})
	return codes
}

// dedupe drops a code found again in the same place.
func dedupe(codes []Code) []Code {
	out := codes[:0]
	for _, c := range codes {
		dup := false
		for i, o := range out {
			if o.Kind == c.Kind && o.Text == c.Text && overlaps(o.Bounds, c.Bounds) {
				out[i].Bounds = o.Bounds.Union(c.Bounds)
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, c)
		}
	}
	return out
}

// overlaps reports whether a and b share at least half the smaller's
// area.
func overlaps(a, b image.Rectangle) bool {
	in := a.Intersect(b)
	if in.Empty() {
		return false
	}
	area := func(r image.Rectangle) int { return r.Dx() * r.Dy() }
	m := area(a)
	if area(b) < m {
		m = area(b)
	}
	return 2*area(in) >= m
}

// luminance is an image's gray levels, one byte a pixel, row by row.
type luminance struct {
	w, h int
	pix  []uint8
}

var lumPool sync.Pool

func newLuminance(w, h int) *luminance {
	l, _ := lumPool.Get().(*luminance)
	if l == nil {
		l = &luminance{}
	}
	l.w, l.h = w, h
	if cap(l.pix) < w*h {
		l.pix = make([]uint8, w*h)
	}
	l.pix = l.pix[:w*h]
	return l
}

func (l *luminance) release() { lumPool.Put(l) }

// luminanceOf converts img to gray levels once, quickly for the types
// cameras and decoders give.
func luminanceOf(img image.Image) *luminance {
	r := img.Bounds()
	w, h := r.Dx(), r.Dy()
	l := newLuminance(w, h)
	switch m := img.(type) {
	case *image.Gray:
		for y := 0; y < h; y++ {
			o := m.PixOffset(r.Min.X, r.Min.Y+y)
			copy(l.pix[y*w:(y+1)*w], m.Pix[o:o+w])
		}
	case *image.YCbCr:
		for y := 0; y < h; y++ {
			o := m.YOffset(r.Min.X, r.Min.Y+y)
			copy(l.pix[y*w:(y+1)*w], m.Y[o:o+w])
		}
	case *image.NRGBA:
		for y := 0; y < h; y++ {
			p := m.Pix[m.PixOffset(r.Min.X, r.Min.Y+y):]
			row := l.pix[y*w : (y+1)*w]
			for x := range row {
				q := p[4*x : 4*x+4 : 4*x+4]
				g := gray(uint32(q[0]), uint32(q[1]), uint32(q[2]))
				// Transparent pixels are composed over white.
				a := uint32(q[3])
				row[x] = uint8((g*a + 255*(255-a)) / 255)
			}
		}
	case *image.RGBA:
		for y := 0; y < h; y++ {
			p := m.Pix[m.PixOffset(r.Min.X, r.Min.Y+y):]
			row := l.pix[y*w : (y+1)*w]
			for x := range row {
				q := p[4*x : 4*x+4 : 4*x+4]
				a := uint32(q[3])
				// Premultiplied: over white adds the uncovered part.
				row[x] = uint8(gray(uint32(q[0]), uint32(q[1]), uint32(q[2])) + 255 - a)
			}
		}
	default:
		for y := 0; y < h; y++ {
			row := l.pix[y*w : (y+1)*w]
			for x := range row {
				c := color.NRGBA64Model.Convert(img.At(r.Min.X+x, r.Min.Y+y)).(color.NRGBA64)
				g := gray(uint32(c.R>>8), uint32(c.G>>8), uint32(c.B>>8))
				a := uint32(c.A >> 8)
				row[x] = uint8((g*a + 255*(255-a)) / 255)
			}
		}
	}
	return l
}

// gray is the luma of 8-bit r, g and b (BT.601).
func gray(r, g, b uint32) uint32 {
	return (299*r + 587*g + 114*b + 500) / 1000
}
