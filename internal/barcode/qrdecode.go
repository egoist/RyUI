package barcode

import (
	"math/bits"
	"strings"
)

// QR code error correction levels, as their format bits name them.
const (
	ecM = 0
	ecL = 1
	ecH = 2
	ecQ = 3
)

// qrECCPerBlock and qrBlocks are, by level (L, M, Q, H) and version, the
// check codewords in each block and the number of blocks.
var (
	qrECCPerBlock = [4][41]int8{
		{-1, 7, 10, 15, 20, 26, 18, 20, 24, 30, 18, 20, 24, 26, 30, 22, 24, 28, 30, 28, 28, 28, 28, 30, 30, 26, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
		{-1, 10, 16, 26, 18, 24, 16, 18, 22, 22, 26, 30, 22, 22, 24, 24, 28, 28, 26, 26, 26, 26, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28, 28},
		{-1, 13, 22, 18, 26, 18, 24, 18, 22, 20, 24, 28, 26, 24, 20, 30, 24, 28, 28, 26, 30, 28, 30, 30, 30, 30, 28, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
		{-1, 17, 28, 22, 16, 22, 28, 26, 26, 24, 28, 24, 28, 22, 24, 24, 30, 28, 28, 26, 28, 30, 24, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30, 30},
	}
	qrBlocks = [4][41]int8{
		{-1, 1, 1, 1, 1, 1, 2, 2, 2, 2, 4, 4, 4, 4, 4, 6, 6, 6, 6, 7, 8, 8, 9, 9, 10, 12, 12, 12, 13, 14, 15, 16, 17, 18, 19, 19, 20, 21, 22, 24, 25},
		{-1, 1, 1, 1, 2, 2, 4, 4, 4, 5, 5, 5, 8, 9, 9, 10, 10, 11, 13, 14, 16, 17, 17, 18, 20, 21, 23, 25, 26, 28, 29, 31, 33, 35, 37, 38, 40, 43, 45, 47, 49},
		{-1, 1, 1, 2, 2, 4, 4, 6, 6, 8, 8, 8, 10, 12, 16, 12, 17, 16, 18, 21, 20, 23, 23, 25, 27, 29, 34, 34, 35, 38, 40, 43, 45, 48, 51, 53, 56, 59, 62, 65, 68},
		{-1, 1, 1, 2, 4, 4, 4, 5, 6, 8, 8, 11, 11, 16, 16, 18, 16, 19, 21, 25, 25, 25, 34, 30, 32, 35, 37, 40, 42, 45, 48, 51, 54, 57, 60, 63, 66, 70, 74, 77, 81},
	}
)

// levelIndex is a level's row in the tables above.
func levelIndex(ec int) int { return [4]int{1, 0, 3, 2}[ec] }

// qrSize is the modules on a side of version v.
func qrSize(v int) int { return 17 + 4*v }

// qrRawModules is the modules that carry codewords in version v.
func qrRawModules(v int) int {
	n := (16*v+128)*v + 64
	if v >= 2 {
		a := v/7 + 2
		n -= (25*a-10)*a - 55
		if v >= 7 {
			n -= 36
		}
	}
	return n
}

// qrAlignment is the rows (and columns) of version v's alignment
// pattern centers.
func qrAlignment(v int) []int {
	if v == 1 {
		return nil
	}
	n := v/7 + 2
	step := (v*8 + n*3 + 5) / (n*4 - 4) * 2
	out := make([]int, n)
	out[0] = 6
	pos := qrSize(v) - 7
	for i := n - 1; i >= 1; i-- {
		out[i] = pos
		pos -= step
	}
	return out
}

// qrBlockLayout is version v's blocks at level ec: how many, their check
// codewords, and the data codewords of the short ones (the long ones
// have one more), with how many are short.
func qrBlockLayout(v, ec int) (blocks, check, shortData, shortCount int) {
	li := levelIndex(ec)
	blocks = int(qrBlocks[li][v])
	check = int(qrECCPerBlock[li][v])
	raw := qrRawModules(v) / 8
	shortCount = blocks - raw%blocks
	shortData = raw/blocks - check
	return
}

// qrDataCodewords is the data codewords of version v at level ec.
func qrDataCodewords(v, ec int) int {
	b, c, _, _ := qrBlockLayout(v, ec)
	return qrRawModules(v)/8 - b*c
}

// qrFunction marks version v's function modules (finders, timing,
// alignment, format and version information).
func qrFunction(v int) *grid {
	n := qrSize(v)
	g := newGrid(n, n)
	region := func(x, y, w, h int) {
		for j := y; j < y+h; j++ {
			for i := x; i < x+w; i++ {
				g.bits[j*n+i] = 1
			}
		}
	}
	region(0, 0, 9, 9)
	region(n-8, 0, 8, 9)
	region(0, n-8, 9, 8)
	al := qrAlignment(v)
	last := len(al) - 1
	for i, cx := range al {
		for j, cy := range al {
			if (i == 0 && j == 0) || (i == 0 && j == last) || (i == last && j == 0) {
				continue
			}
			region(cx-2, cy-2, 5, 5)
		}
	}
	region(6, 9, 1, n-17)
	region(9, 6, n-17, 1)
	if v >= 7 {
		region(n-11, 0, 3, 6)
		region(0, n-11, 6, 3)
	}
	return g
}

// qrMasked reports whether mask m flips the module at (x, y).
func qrMasked(m, x, y int) bool {
	switch m {
	case 0:
		return (x+y)%2 == 0
	case 1:
		return y%2 == 0
	case 2:
		return x%3 == 0
	case 3:
		return (x+y)%3 == 0
	case 4:
		return (y/2+x/3)%2 == 0
	case 5:
		return x*y%2+x*y%3 == 0
	case 6:
		return (x*y%2+x*y%3)%2 == 0
	default:
		return ((x+y)%2+x*y%3)%2 == 0
	}
}

// qrFormatBits is the 15 masked format bits of 5 data bits.
func qrFormatBits(data int) int {
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	return (data<<10 | rem) ^ 0x5412
}

// qrVersionBits is the 18 version bits of version v.
func qrVersionBits(v int) int {
	rem := v
	for i := 0; i < 12; i++ {
		rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
	}
	return v<<12 | rem
}

// decodeFormat finds the format (level and mask) nearest either copy of
// its bits, if within 3 bits.
func decodeFormat(a, b int) (ec, mask int, ok bool) {
	best, bestD := 0, 99
	for d := 0; d < 32; d++ {
		f := qrFormatBits(d)
		for _, x := range [2]int{a, b} {
			if n := bits.OnesCount(uint(f ^ x)); n < bestD {
				best, bestD = d, n
			}
		}
	}
	if bestD > 3 {
		return 0, 0, false
	}
	return best >> 3, best & 7, true
}

// decodeVersion finds the version nearest version bits, if within 3.
func decodeVersion(a int) (int, bool) {
	best, bestD := 0, 99
	for v := 7; v <= 40; v++ {
		if n := bits.OnesCount(uint(qrVersionBits(v) ^ a)); n < bestD {
			best, bestD = v, n
		}
	}
	return best, bestD <= 3
}

// qrReadFormat reads a sampled code's format information.
func qrReadFormat(g *grid) (ec, mask int, ok bool) {
	n := g.w
	bit := func(acc, x, y int) int {
		acc <<= 1
		if g.at(x, y) {
			acc |= 1
		}
		return acc
	}
	a := 0
	for x := 0; x <= 5; x++ {
		a = bit(a, x, 8)
	}
	a = bit(a, 7, 8)
	a = bit(a, 8, 8)
	a = bit(a, 8, 7)
	for y := 5; y >= 0; y-- {
		a = bit(a, 8, y)
	}
	b := 0
	for y := n - 1; y >= n-7; y-- {
		b = bit(b, 8, y)
	}
	for x := n - 8; x < n; x++ {
		b = bit(b, x, 8)
	}
	return decodeFormat(a, b)
}

// qrReadVersion reads a sampled code's version information (versions 7
// and up): 0 if neither copy is readable.
func qrReadVersion(g *grid) int {
	n := g.w
	a, b := 0, 0
	for j := 5; j >= 0; j-- {
		for i := n - 9; i >= n-11; i-- {
			a <<= 1
			if g.at(i, j) {
				a |= 1
			}
			b <<= 1
			if g.at(j, i) {
				b |= 1
			}
		}
	}
	if v, ok := decodeVersion(a); ok {
		return v
	}
	if v, ok := decodeVersion(b); ok {
		return v
	}
	return 0
}

// qrDecodeGrid reads a sampled code of version v: its text.
func qrDecodeGrid(g *grid, v int) (string, bool) {
	ec, mask, ok := qrReadFormat(g)
	if !ok {
		return "", false
	}
	n := qrSize(v)
	fn := qrFunction(v)
	raw := make([]uint8, qrRawModules(v)/8)
	k := 0
	var cur uint8
	nbits := 0
	up := true
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for c := 0; c < n; c++ {
			y := c
			if up {
				y = n - 1 - c
			}
			for j := 0; j < 2; j++ {
				x := right - j
				if fn.bits[y*n+x] != 0 {
					continue
				}
				d := g.bits[y*n+x] != 0
				if qrMasked(mask, x, y) {
					d = !d
				}
				cur <<= 1
				if d {
					cur |= 1
				}
				nbits++
				if nbits == 8 {
					if k < len(raw) {
						raw[k] = cur
					}
					k++
					cur, nbits = 0, 0
				}
			}
		}
		up = !up
	}
	data, ok := qrCorrect(raw, v, ec)
	if !ok {
		return "", false
	}
	return qrDecodeData(data, v)
}

// qrCorrect deinterleaves raw into blocks, corrects each, and is the
// data codewords.
func qrCorrect(raw []uint8, v, ec int) ([]uint8, bool) {
	blocks, check, shortData, shortCount := qrBlockLayout(v, ec)
	lens := make([]int, blocks)
	total := 0
	for i := range lens {
		lens[i] = shortData
		if i >= shortCount {
			lens[i]++
		}
		total += lens[i]
	}
	bs := make([][]uint8, blocks)
	for i := range bs {
		bs[i] = make([]uint8, lens[i]+check)
	}
	k := 0
	for i := 0; i <= shortData; i++ {
		for b := range bs {
			if i < lens[b] {
				bs[b][i] = raw[k]
				k++
			}
		}
	}
	for i := 0; i < check; i++ {
		for b := range bs {
			bs[b][lens[b]+i] = raw[k]
			k++
		}
	}
	out := make([]uint8, 0, total)
	for b := range bs {
		if !qrField.rsDecode(bs[b], check) {
			return nil, false
		}
		out = append(out, bs[b][:lens[b]]...)
	}
	return out, true
}

// bitReader reads bits, most significant first.
type bitReader struct {
	b   []uint8
	pos int
}

func (r *bitReader) left() int { return 8*len(r.b) - r.pos }

func (r *bitReader) read(n int) int {
	v := 0
	for i := 0; i < n; i++ {
		v <<= 1
		if r.pos < 8*len(r.b) && r.b[r.pos>>3]&(0x80>>(r.pos&7)) != 0 {
			v |= 1
		}
		r.pos++
	}
	return v
}

const qrAlnum = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ $%*+-./:"

// qrDecodeData reads a QR code's segments: its text.
func qrDecodeData(data []uint8, v int) (string, bool) {
	r := &bitReader{b: data}
	var out strings.Builder
	var pending []byte // bytes of the current ECI's charset
	eci := eciNone
	fnc1 := false
	flush := func() {
		if len(pending) > 0 {
			out.WriteString(bytesText(pending, eci))
			pending = pending[:0]
		}
	}
	countBits := func(small, mid, large int) int {
		switch {
		case v <= 9:
			return small
		case v <= 26:
			return mid
		}
		return large
	}
	for r.left() >= 4 {
		mode := r.read(4)
		switch mode {
		case 0:
			flush()
			return out.String(), true
		case 1: // numeric
			n := r.read(countBits(10, 12, 14))
			for n >= 3 {
				if r.left() < 10 {
					return "", false
				}
				d := r.read(10)
				if d >= 1000 {
					return "", false
				}
				pending = append(pending, byte('0'+d/100), byte('0'+d/10%10), byte('0'+d%10))
				n -= 3
			}
			if n == 2 {
				if r.left() < 7 {
					return "", false
				}
				d := r.read(7)
				if d >= 100 {
					return "", false
				}
				pending = append(pending, byte('0'+d/10), byte('0'+d%10))
			} else if n == 1 {
				if r.left() < 4 {
					return "", false
				}
				d := r.read(4)
				if d >= 10 {
					return "", false
				}
				pending = append(pending, byte('0'+d))
			}
		case 2: // alphanumeric
			n := r.read(countBits(9, 11, 13))
			start := len(pending)
			for n >= 2 {
				if r.left() < 11 {
					return "", false
				}
				d := r.read(11)
				if d >= 45*45 {
					return "", false
				}
				pending = append(pending, qrAlnum[d/45], qrAlnum[d%45])
				n -= 2
			}
			if n == 1 {
				if r.left() < 6 {
					return "", false
				}
				d := r.read(6)
				if d >= 45 {
					return "", false
				}
				pending = append(pending, qrAlnum[d])
			}
			if fnc1 {
				// In GS1 mode, % is the separator and %% a percent sign.
				seg := pending[start:]
				var o []byte
				for i := 0; i < len(seg); i++ {
					if seg[i] == '%' {
						if i+1 < len(seg) && seg[i+1] == '%' {
							o = append(o, '%')
							i++
						} else {
							o = append(o, 0x1D)
						}
					} else {
						o = append(o, seg[i])
					}
				}
				pending = append(pending[:start], o...)
			}
		case 4: // bytes
			n := r.read(countBits(8, 16, 16))
			if r.left() < 8*n {
				return "", false
			}
			for i := 0; i < n; i++ {
				pending = append(pending, byte(r.read(8)))
			}
		case 8: // kanji
			n := r.read(countBits(8, 10, 12))
			if r.left() < 13*n {
				return "", false
			}
			flush()
			for i := 0; i < n; i++ {
				d := r.read(13)
				c := (d/0xC0)<<8 | d%0xC0
				if c < 0x1F00 {
					c += 0x8140
				} else {
					c += 0xC140
				}
				ru := sjisRune(byte(c>>8), byte(c))
				if ru == 0 {
					return "", false
				}
				out.WriteRune(ru)
			}
		case 7: // ECI
			flush()
			first := r.read(8)
			switch {
			case first&0x80 == 0:
				eci = first & 0x7F
			case first&0xC0 == 0x80:
				eci = (first&0x3F)<<8 | r.read(8)
			case first&0xE0 == 0xC0:
				eci = (first&0x1F)<<16 | r.read(16)
			default:
				return "", false
			}
		case 3: // structured append: its position and parity, ignored
			r.read(16)
		case 5: // FNC1, first position
			fnc1 = true
		case 9: // FNC1, second position: an application indicator
			r.read(8)
			fnc1 = true
		default:
			return "", false
		}
	}
	flush()
	return out.String(), true
}
