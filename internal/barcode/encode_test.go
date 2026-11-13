package barcode

import (
	"fmt"
	"image"
	"image/color"
	"strings"
)

// Encoders the tests draw codes with.

// bitWriter appends bits, most significant first.
type bitWriter struct {
	b []uint8
	n int
}

func (w *bitWriter) put(v, n int) {
	for i := n - 1; i >= 0; i-- {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		if v>>i&1 != 0 {
			w.b[w.n/8] |= 0x80 >> (w.n % 8)
		}
		w.n++
	}
}

// qrSeg is a QR code segment: a mode (1 numeric, 2 alphanumeric, 4
// bytes, 8 kanji, 7 an ECI designator) and its data.
type qrSeg struct {
	mode int
	data string // for kanji: the text, in JIS X 0208's characters
	eci  int
}

func qrCountBits(mode, v int) int {
	i := 0
	if v >= 27 {
		i = 2
	} else if v >= 10 {
		i = 1
	}
	switch mode {
	case 1:
		return [3]int{10, 12, 14}[i]
	case 2:
		return [3]int{9, 11, 13}[i]
	case 4:
		return [3]int{8, 16, 16}[i]
	case 8:
		return [3]int{8, 10, 12}[i]
	}
	panic("mode")
}

// sjisOf is r in Shift JIS.
func sjisOf(r rune) (byte, byte, bool) {
	for row := 0; row < 94; row++ {
		for cell := 0; cell < 94; cell++ {
			if jis(row, cell) != r {
				continue
			}
			r1, c1 := row+1, cell+1
			var s1, s2 int
			if r1 <= 62 {
				s1 = (r1+1)/2 + 0x80
			} else {
				s1 = (r1+1)/2 + 0xC0
			}
			if r1%2 == 1 {
				s2 = c1 + 0x3F
				if s2 >= 0x7F {
					s2++
				}
			} else {
				s2 = c1 + 0x9E
			}
			return byte(s1), byte(s2), true
		}
	}
	return 0, 0, false
}

// qrBits is segments as a QR code's data bits for version v.
func qrBits(segs []qrSeg, v int) *bitWriter {
	w := &bitWriter{}
	for _, s := range segs {
		switch s.mode {
		case -5:
			w.put(5, 4)
		case 7:
			w.put(7, 4)
			switch {
			case s.eci < 128:
				w.put(s.eci, 8)
			case s.eci < 16384:
				w.put(0x8000|s.eci, 16)
			default:
				w.put(0xC00000|s.eci, 24)
			}
		case 1:
			w.put(1, 4)
			w.put(len(s.data), qrCountBits(1, v))
			d := s.data
			for len(d) >= 3 {
				n := int(d[0]-'0')*100 + int(d[1]-'0')*10 + int(d[2]-'0')
				w.put(n, 10)
				d = d[3:]
			}
			if len(d) == 2 {
				w.put(int(d[0]-'0')*10+int(d[1]-'0'), 7)
			} else if len(d) == 1 {
				w.put(int(d[0]-'0'), 4)
			}
		case 2:
			w.put(2, 4)
			w.put(len(s.data), qrCountBits(2, v))
			d := s.data
			for len(d) >= 2 {
				w.put(strings.IndexByte(qrAlnum, d[0])*45+strings.IndexByte(qrAlnum, d[1]), 11)
				d = d[2:]
			}
			if len(d) == 1 {
				w.put(strings.IndexByte(qrAlnum, d[0]), 6)
			}
		case 4:
			w.put(4, 4)
			w.put(len(s.data), qrCountBits(4, v))
			for i := 0; i < len(s.data); i++ {
				w.put(int(s.data[i]), 8)
			}
		case 8:
			rs := []rune(s.data)
			w.put(8, 4)
			w.put(len(rs), qrCountBits(8, v))
			for _, r := range rs {
				b1, b2, ok := sjisOf(r)
				if !ok {
					panic(fmt.Sprintf("no Shift JIS for %q", r))
				}
				c := int(b1)<<8 | int(b2)
				if c >= 0x8140 && c <= 0x9FFC {
					c -= 0x8140
				} else {
					c -= 0xC140
				}
				w.put((c>>8)*0xC0+c&0xFF, 13)
			}
		}
	}
	return w
}

// qrEncode draws segments as a version v code at level ec with mask:
// its modules.
func qrEncode(segs []qrSeg, v, ec, mask int) *grid {
	w := qrBits(segs, v)
	capBits := qrDataCodewords(v, ec) * 8
	if w.n > capBits {
		panic(fmt.Sprintf("%d bits don't fit version %d level %d (%d)", w.n, v, ec, capBits))
	}
	w.put(0, min(4, capBits-w.n))
	if w.n%8 != 0 {
		w.put(0, 8-w.n%8)
	}
	for pad := 0xEC; len(w.b) < capBits/8; pad ^= 0xEC ^ 0x11 {
		w.put(pad, 8)
	}
	data := w.b
	blocks, check, shortData, shortCount := qrBlockLayout(v, ec)
	var bs, eccs [][]uint8
	k := 0
	for b := 0; b < blocks; b++ {
		n := shortData
		if b >= shortCount {
			n++
		}
		bs = append(bs, data[k:k+n])
		eccs = append(eccs, qrField.rsEncode(data[k:k+n], check))
		k += n
	}
	var raw []uint8
	for i := 0; i <= shortData; i++ {
		for b := range bs {
			if i < len(bs[b]) {
				raw = append(raw, bs[b][i])
			}
		}
	}
	for i := 0; i < check; i++ {
		for b := range bs {
			raw = append(raw, eccs[b][i])
		}
	}
	n := qrSize(v)
	g := newGrid(n, n)
	fn := qrFunction(v)
	finderAt := func(cx, cy int) {
		for dy := -4; dy <= 4; dy++ {
			for dx := -4; dx <= 4; dx++ {
				x, y := cx+dx, cy+dy
				if x < 0 || y < 0 || x >= n || y >= n {
					continue
				}
				d := max(abs(dx), abs(dy))
				g.set(x, y, d != 2 && d != 4)
			}
		}
	}
	finderAt(3, 3)
	finderAt(n-4, 3)
	finderAt(3, n-4)
	for i := 8; i < n-8; i++ {
		g.set(i, 6, i%2 == 0)
		g.set(6, i, i%2 == 0)
	}
	al := qrAlignment(v)
	for i, cx := range al {
		for j, cy := range al {
			if (i == 0 && j == 0) || (i == 0 && j == len(al)-1) || (i == len(al)-1 && j == 0) {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					g.set(cx+dx, cy+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	// Format bits.
	f := qrFormatBits(ec<<3 | mask)
	bit := func(i int) bool { return f>>i&1 != 0 }
	for i := 0; i <= 5; i++ {
		g.set(8, i, bit(i))
	}
	g.set(8, 7, bit(6))
	g.set(8, 8, bit(7))
	g.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		g.set(14-i, 8, bit(i))
	}
	for i := 0; i < 8; i++ {
		g.set(n-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		g.set(8, n-15+i, bit(i))
	}
	g.set(8, n-8, true)
	if v >= 7 {
		vb := qrVersionBits(v)
		for i := 0; i < 18; i++ {
			on := vb>>i&1 != 0
			a, b := n-11+i%3, i/3
			g.set(a, b, on)
			g.set(b, a, on)
		}
	}
	// Data.
	i := 0
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < n; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				up := (right+1)&2 == 0
				y := vert
				if up {
					y = n - 1 - vert
				}
				if fn.at(x, y) {
					continue
				}
				on := false
				if i < len(raw)*8 {
					on = raw[i>>3]>>(7-i&7)&1 != 0
					i++
				}
				if qrMasked(mask, x, y) {
					on = !on
				}
				g.set(x, y, on)
			}
		}
	}
	return g
}

// qrCodewordModules is, for each raw codeword of version v, its modules.
func qrCodewordModules(v int) [][][2]int {
	n := qrSize(v)
	fn := qrFunction(v)
	out := make([][][2]int, qrRawModules(v)/8)
	i := 0
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < n; vert++ {
			for j := 0; j < 2; j++ {
				x := right - j
				y := vert
				if (right+1)&2 == 0 {
					y = n - 1 - vert
				}
				if fn.at(x, y) {
					continue
				}
				if i/8 < len(out) {
					out[i/8] = append(out[i/8], [2]int{x, y})
				}
				i++
			}
		}
	}
	return out
}

// qrBlockOf is the block of each raw codeword of version v at level ec.
func qrBlockOf(v, ec int) []int {
	blocks, check, shortData, shortCount := qrBlockLayout(v, ec)
	var out []int
	for i := 0; i <= shortData; i++ {
		for b := 0; b < blocks; b++ {
			if i < shortData || b >= shortCount {
				out = append(out, b)
			}
		}
	}
	for i := 0; i < check; i++ {
		for b := 0; b < blocks; b++ {
			out = append(out, b)
		}
	}
	return out
}

// gridImage draws g with quiet modules around it, scale pixels a module.
func gridImage(g *grid, scale, quiet int) *image.Gray {
	w, h := (g.w+2*quiet)*scale, (g.h+2*quiet)*scale
	img := image.NewGray(image.Rect(0, 0, w, h))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			if !g.at(x, y) {
				continue
			}
			for yy := 0; yy < scale; yy++ {
				for xx := 0; xx < scale; xx++ {
					img.SetGray((x+quiet)*scale+xx, (y+quiet)*scale+yy, color.Gray{0})
				}
			}
		}
	}
	return img
}

// Data Matrix.

// dmASCII is text in ASCII encodation.
func dmEncodeASCII(text string) []uint8 {
	var cw []uint8
	b := []byte(text)
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case i+1 < len(b) && isDigit(c) && isDigit(b[i+1]):
			cw = append(cw, uint8(130+int(c-'0')*10+int(b[i+1]-'0')))
			i++
		case c < 128:
			cw = append(cw, c+1)
		default:
			cw = append(cw, 235, c-127)
		}
	}
	return cw
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// dmSizeFor is the smallest size (square, or rectangular) that holds n
// data codewords.
func dmSizeFor(n int, rect bool) *dmSize {
	for i := range dmSizes {
		s := &dmSizes[i]
		if (s.rows != s.cols) == rect && s.data >= n {
			return s
		}
	}
	return nil
}

// dmSymbol draws data codewords cw as a symbol of size s: its modules.
func dmSymbol(cw []uint8, s *dmSize) *grid {
	if len(cw) > s.data {
		panic("too much data")
	}
	data := append([]uint8(nil), cw...)
	if len(data) < s.data {
		data = append(data, 129)
	}
	for len(data) < s.data {
		p := len(data) + 1
		v := 129 + (149*p)%253 + 1
		if v > 254 {
			v -= 254
		}
		data = append(data, uint8(v))
	}
	nb := s.blocks
	raw := make([]uint8, s.total())
	copy(raw, data)
	for b := 0; b < nb; b++ {
		var block []uint8
		for i := b; i < s.data; i += nb {
			block = append(block, data[i])
		}
		ecc := dmField.rsEncode(block, s.check)
		i0 := s.data + (b-s.data%nb+nb)%nb
		for k, e := range ecc {
			raw[i0+k*nb] = e
		}
	}
	nr, nc := s.mapRows(), s.mapCols()
	place := dmPlacement(nr, nc)
	m := make([]bool, nr*nc)
	used := make([]bool, nr*nc)
	for i := 0; i < 8*len(raw); i++ {
		p := place[i]
		used[p] = true
		m[p] = raw[i>>3]&(0x80>>(i&7)) != 0
	}
	if !used[nr*nc-1] {
		m[nr*nc-1] = true
		m[(nr-2)*nc+nc-2] = true
	}
	g := newGrid(s.cols, s.rows)
	for r := 0; r < nr; r++ {
		for c := 0; c < nc; c++ {
			y, x := s.symbolPos(r, c)
			g.set(x, y, m[r*nc+c])
		}
	}
	rh, cw2 := s.regRows+2, s.regCols+2
	for y := 0; y < s.rows; y++ {
		for x := 0; x < s.cols; x++ {
			ry, rx := y%rh, x%cw2
			switch {
			case ry == rh-1 || rx == 0:
				g.set(x, y, true)
			case ry == 0:
				g.set(x, y, rx%2 == 0)
			case rx == cw2-1:
				g.set(x, y, (rh-1-ry)%2 == 0)
			}
		}
	}
	return g
}

// One-dimensional codes, as modules: true for a bar.

func widthsModules(ws []uint8, bar bool, out []bool) []bool {
	for _, w := range ws {
		for i := 0; i < int(w); i++ {
			out = append(out, bar)
		}
		bar = !bar
	}
	return out
}

func checkDigit(d string) byte {
	sum := 0
	for i := len(d) - 1; i >= 0; i-- {
		v := int(d[i] - '0')
		if (len(d)-1-i)%2 == 0 {
			v *= 3
		}
		sum += v
	}
	return byte('0' + (10-sum%10)%10)
}

func eanDigit(d byte, parity byte, out []bool) []bool {
	p := upcL[d-'0']
	switch parity {
	case 'L':
		return widthsModules(p[:], false, out)
	case 'G':
		return widthsModules([]uint8{p[3], p[2], p[1], p[0]}, false, out)
	}
	return widthsModules(p[:], true, out)
}

// ean13Modules draws 12 or 13 digits (the check digit added if missing).
func ean13Modules(d string) []bool {
	if len(d) == 12 {
		d += string(checkDigit(d))
	}
	m := widthsModules(guard3, true, nil)
	par := eanFirstDigit[d[0]-'0']
	for i := 0; i < 6; i++ {
		p := byte('L')
		if par>>(5-i)&1 != 0 {
			p = 'G'
		}
		m = eanDigit(d[1+i], p, m)
	}
	m = widthsModules(guard5, false, m)
	for i := 0; i < 6; i++ {
		m = eanDigit(d[7+i], 'R', m)
	}
	return widthsModules(guard3, true, m)
}

func ean8Modules(d string) []bool {
	if len(d) == 7 {
		d += string(checkDigit(d))
	}
	m := widthsModules(guard3, true, nil)
	for i := 0; i < 4; i++ {
		m = eanDigit(d[i], 'L', m)
	}
	m = widthsModules(guard5, false, m)
	for i := 4; i < 8; i++ {
		m = eanDigit(d[i], 'R', m)
	}
	return widthsModules(guard3, true, m)
}

// upceModules draws a UPC-E's 8 digits (number system, six, check).
func upceModules(d string) []bool {
	m := widthsModules(guard3, true, nil)
	par := upceParity[d[0]-'0'][d[7]-'0']
	for i := 0; i < 6; i++ {
		p := byte('L')
		if par>>(5-i)&1 != 0 {
			p = 'G'
		}
		m = eanDigit(d[1+i], p, m)
	}
	return widthsModules(guard6, false, m)
}

// code128Modules draws symbol values (start first, without check and
// stop).
func code128Modules(vals []int) []bool {
	sum := vals[0]
	for i, v := range vals[1:] {
		sum += (i + 1) * v
	}
	vals = append(append([]int(nil), vals...), sum%103)
	var m []bool
	for _, v := range vals {
		m = widthsModules(c128Patterns[v][:], true, m)
	}
	return widthsModules([]uint8{2, 3, 3, 1, 1, 1, 2}, true, m)
}

// code128B is text in code set B.
func code128B(text string) []int {
	vals := []int{c128StartB}
	for _, c := range []byte(text) {
		vals = append(vals, int(c)-32)
	}
	return vals
}

// code39Modules draws text between asterisks, wide elements ratio:1.
func code39Modules(text string, ratio int) []bool {
	var m []bool
	for i, c := range "*" + text + "*" {
		p := code39Asterisk
		if c != '*' {
			p = int(code39Patterns[strings.IndexRune(code39Alphabet, c)])
		}
		if i > 0 {
			m = append(m, false)
		}
		for k := 0; k < 9; k++ {
			w := 1
			if p>>(8-k)&1 != 0 {
				w = ratio
			}
			for j := 0; j < w; j++ {
				m = append(m, k%2 == 0)
			}
		}
	}
	return m
}

// barsImage draws modules scale pixels wide (any scale: edges are
// anti-aliased), height pixels tall, with quiet modules each side.
func barsImage(m []bool, scale float64, height, quiet int) *image.Gray {
	total := float64(len(m)+2*quiet) * scale
	w := int(total + 0.999)
	img := image.NewGray(image.Rect(0, 0, w, height))
	row := make([]uint8, w)
	for x := 0; x < w; x++ {
		// The share of this pixel covered by bars.
		var cov float64
		x0, x1 := float64(x), float64(x+1)
		for k := int(x0/scale) - quiet - 1; k <= int(x1/scale)-quiet+1; k++ {
			if k < 0 || k >= len(m) || !m[k] {
				continue
			}
			a := float64(k+quiet) * scale
			b := a + scale
			lo, hi := max(a, x0), min(b, x1)
			if hi > lo {
				cov += hi - lo
			}
		}
		row[x] = uint8(255 - 255*min(cov, 1) + 0.5)
	}
	for y := 0; y < height; y++ {
		copy(img.Pix[y*img.Stride:], row)
	}
	return img
}
