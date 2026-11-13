package barcode

import (
	"strconv"
	"strings"
	"sync"
)

// dmSize is a Data Matrix (ECC 200) symbol size: its modules, its data
// regions' modules, its data codewords, and its blocks' check codewords
// and number.
type dmSize struct {
	rows, cols       int
	regRows, regCols int
	data             int
	check, blocks    int
}

var dmSizes = []dmSize{
	{10, 10, 8, 8, 3, 5, 1},
	{12, 12, 10, 10, 5, 7, 1},
	{14, 14, 12, 12, 8, 10, 1},
	{16, 16, 14, 14, 12, 12, 1},
	{18, 18, 16, 16, 18, 14, 1},
	{20, 20, 18, 18, 22, 18, 1},
	{22, 22, 20, 20, 30, 20, 1},
	{24, 24, 22, 22, 36, 24, 1},
	{26, 26, 24, 24, 44, 28, 1},
	{32, 32, 14, 14, 62, 36, 1},
	{36, 36, 16, 16, 86, 42, 1},
	{40, 40, 18, 18, 114, 48, 1},
	{44, 44, 20, 20, 144, 56, 1},
	{48, 48, 22, 22, 174, 68, 1},
	{52, 52, 24, 24, 204, 42, 2},
	{64, 64, 14, 14, 280, 56, 2},
	{72, 72, 16, 16, 368, 36, 4},
	{80, 80, 18, 18, 456, 48, 4},
	{88, 88, 20, 20, 576, 56, 4},
	{96, 96, 22, 22, 696, 68, 4},
	{104, 104, 24, 24, 816, 56, 6},
	{120, 120, 18, 18, 1050, 68, 6},
	{132, 132, 20, 20, 1304, 62, 8},
	{144, 144, 22, 22, 1558, 62, 10},
	{8, 18, 6, 16, 5, 7, 1},
	{8, 32, 6, 14, 10, 11, 1},
	{12, 26, 10, 24, 16, 14, 1},
	{12, 36, 10, 16, 22, 18, 1},
	{16, 36, 14, 16, 32, 24, 1},
	{16, 48, 14, 22, 49, 28, 1},
}

// mapRows and mapCols are the size of the symbol's data, its regions
// put together.
func (s *dmSize) mapRows() int { return s.rows / (s.regRows + 2) * s.regRows }
func (s *dmSize) mapCols() int { return s.cols / (s.regCols + 2) * s.regCols }

// symbolPos is where in the symbol the data module (r, c) is.
func (s *dmSize) symbolPos(r, c int) (int, int) {
	return r/s.regRows*(s.regRows+2) + 1 + r%s.regRows, c/s.regCols*(s.regCols+2) + 1 + c%s.regCols
}

// total is the symbol's codewords, data and check.
func (s *dmSize) total() int { return s.data + s.check*s.blocks }

var (
	placeMu    sync.Mutex
	placeCache = map[[2]int][]int32{}
)

// dmPlacement is where each bit of each codeword goes in the data
// modules (ISO/IEC 16022 Annex F): for codeword i, bit b (0 the most
// significant), entry 8i+b is row*cols+col.
func dmPlacement(nrow, ncol int) []int32 {
	placeMu.Lock()
	defer placeMu.Unlock()
	if p, ok := placeCache[[2]int{nrow, ncol}]; ok {
		return p
	}
	used := make([]bool, nrow*ncol)
	var out []int32
	module := func(row, col int) {
		if row < 0 {
			row += nrow
			col += 4 - (nrow+4)%8
		}
		if col < 0 {
			col += ncol
			row += 4 - (ncol+4)%8
		}
		used[row*ncol+col] = true
		out = append(out, int32(row*ncol+col))
	}
	utah := func(row, col int) {
		module(row-2, col-2)
		module(row-2, col-1)
		module(row-1, col-2)
		module(row-1, col-1)
		module(row-1, col)
		module(row, col-2)
		module(row, col-1)
		module(row, col)
	}
	corner := func(ps [8][2]int) {
		for _, p := range ps {
			module(p[0], p[1])
		}
	}
	row, col := 4, 0
	for {
		if row == nrow && col == 0 {
			corner([8][2]int{{nrow - 1, 0}, {nrow - 1, 1}, {nrow - 1, 2}, {0, ncol - 2}, {0, ncol - 1}, {1, ncol - 1}, {2, ncol - 1}, {3, ncol - 1}})
		}
		if row == nrow-2 && col == 0 && ncol%4 != 0 {
			corner([8][2]int{{nrow - 3, 0}, {nrow - 2, 0}, {nrow - 1, 0}, {0, ncol - 4}, {0, ncol - 3}, {0, ncol - 2}, {0, ncol - 1}, {1, ncol - 1}})
		}
		if row == nrow-2 && col == 0 && ncol%8 == 4 {
			corner([8][2]int{{nrow - 3, 0}, {nrow - 2, 0}, {nrow - 1, 0}, {0, ncol - 2}, {0, ncol - 1}, {1, ncol - 1}, {2, ncol - 1}, {3, ncol - 1}})
		}
		if row == nrow+4 && col == 2 && ncol%8 == 0 {
			corner([8][2]int{{nrow - 1, 0}, {nrow - 1, ncol - 1}, {0, ncol - 3}, {0, ncol - 2}, {0, ncol - 1}, {1, ncol - 3}, {1, ncol - 2}, {1, ncol - 1}})
		}
		for {
			if row < nrow && col >= 0 && !used[row*ncol+col] {
				utah(row, col)
			}
			row -= 2
			col += 2
			if row < 0 || col >= ncol {
				break
			}
		}
		row++
		col += 3
		for {
			if row >= 0 && col < ncol && !used[row*ncol+col] {
				utah(row, col)
			}
			row += 2
			col -= 2
			if row >= nrow || col < 0 {
				break
			}
		}
		row += 3
		col++
		if row >= nrow && col >= ncol {
			break
		}
	}
	placeCache[[2]int{nrow, ncol}] = out
	return out
}

// dmDecodeGrid reads a sampled symbol of size s: its text.
func dmDecodeGrid(g *grid, s *dmSize) (string, bool) {
	nr, nc := s.mapRows(), s.mapCols()
	place := dmPlacement(nr, nc)
	n := s.total()
	if len(place) < 8*n {
		return "", false
	}
	raw := make([]uint8, n)
	for i := 0; i < 8*n; i++ {
		p := int(place[i])
		r, c := s.symbolPos(p/nc, p%nc)
		if g.bits[r*g.w+c] != 0 {
			raw[i>>3] |= 0x80 >> (i & 7)
		}
	}
	data, ok := dmCorrect(raw, s)
	if !ok {
		return "", false
	}
	return dmDecodeData(data)
}

// dmCorrect deinterleaves raw (codeword i belongs to block i mod
// blocks), corrects each block, and is the data codewords.
func dmCorrect(raw []uint8, s *dmSize) ([]uint8, bool) {
	nb := s.blocks
	data := make([]uint8, s.data)
	block := make([]uint8, 0, s.data/nb+1+s.check)
	for b := 0; b < nb; b++ {
		block = block[:0]
		for i := b; i < s.data; i += nb {
			block = append(block, raw[i])
		}
		nd := len(block)
		for i := s.data + (b-s.data%nb+nb)%nb; i < len(raw); i += nb {
			block = append(block, raw[i])
		}
		if len(block)-nd != s.check {
			return nil, false
		}
		if !dmField.rsDecode(block, s.check) {
			return nil, false
		}
		for i, j := b, 0; i < s.data; i, j = i+nb, j+1 {
			data[i] = block[j]
		}
	}
	return data, true
}

// Data Matrix encodations.
const (
	dmASCII = iota
	dmC40
	dmText
	dmX12
	dmEDIFACT
	dmBase256
)

// dmDecodeData reads a symbol's data codewords: its text.
func dmDecodeData(cw []uint8) (string, bool) {
	var out strings.Builder
	var pending []byte
	eci := eciNone
	flush := func() {
		if len(pending) > 0 {
			out.WriteString(bytesText(pending, eci))
			pending = pending[:0]
		}
	}
	var suffix string
	mode := dmASCII
	upper := false
	i := 0
	for i < len(cw) {
		switch mode {
		case dmASCII:
			c := int(cw[i])
			i++
			switch {
			case c == 0:
				return "", false
			case c <= 128:
				if upper {
					c += 128
					upper = false
				}
				pending = append(pending, byte(c-1))
			case c == 129: // pad: the end
				i = len(cw)
			case c <= 229:
				d := c - 130
				pending = append(pending, byte('0'+d/10), byte('0'+d%10))
			case c == 230:
				mode = dmC40
			case c == 231:
				mode = dmBase256
			case c == 232: // FNC1: GS1's leading one dropped
				if i > 1 {
					pending = append(pending, 0x1D)
				}
			case c == 233: // structured append: position and file id
				i += 3
			case c == 234: // reader programming
			case c == 235:
				upper = true
			case c == 236:
				pending = append(pending, "[)>\x1e05\x1d"...)
				suffix = "\x1e\x04" + suffix
			case c == 237:
				pending = append(pending, "[)>\x1e06\x1d"...)
				suffix = "\x1e\x04" + suffix
			case c == 238:
				mode = dmX12
			case c == 239:
				mode = dmText
			case c == 240:
				mode = dmEDIFACT
			case c == 241:
				flush()
				if i >= len(cw) {
					return "", false
				}
				c1 := int(cw[i])
				i++
				switch {
				case c1 <= 127:
					eci = c1 - 1
				case c1 <= 191:
					if i >= len(cw) {
						return "", false
					}
					eci = (c1-128)*254 + int(cw[i]) - 1 + 127
					i++
				default:
					if i+1 >= len(cw) {
						return "", false
					}
					eci = (c1-192)*64516 + (int(cw[i])-1)*254 + int(cw[i+1]) - 1 + 16383
					i += 2
				}
			case c == 254: // unlatch, harmless in ASCII
			default:
				return "", false
			}
		case dmC40, dmText:
			var ok bool
			i, ok = dmDecodeC40(cw, i, mode == dmText, &pending, &upper)
			if !ok {
				return "", false
			}
			mode = dmASCII
		case dmX12:
			var ok bool
			i, ok = dmDecodeX12(cw, i, &pending)
			if !ok {
				return "", false
			}
			mode = dmASCII
		case dmEDIFACT:
			i = dmDecodeEDIFACT(cw, i, &pending)
			mode = dmASCII
		case dmBase256:
			var ok bool
			i, ok = dmDecodeBase256(cw, i, &pending)
			if !ok {
				return "", false
			}
			mode = dmASCII
		}
	}
	flush()
	out.WriteString(suffix)
	return out.String(), true
}

const dmShift2 = "!\"#$%&'()*+,-./:;<=>?@[\\]^_"

// dmDecodeC40 reads C40 (or Text) from cw[i] until it unlatches.
func dmDecodeC40(cw []uint8, i int, text bool, out *[]byte, upper *bool) (int, bool) {
	shift := 0
	emit := func(c int) {
		if *upper {
			c += 128
			*upper = false
		}
		*out = append(*out, byte(c))
	}
	for i < len(cw) {
		if cw[i] == 254 {
			return i + 1, true
		}
		if i+1 >= len(cw) {
			// A last lone codeword is ASCII.
			return i, true
		}
		v := int(cw[i])<<8 | int(cw[i+1])
		i += 2
		v--
		vals := [3]int{v / 1600, v / 40 % 40, v % 40}
		for _, c := range vals {
			switch shift {
			case 0:
				switch {
				case c < 3:
					shift = c + 1
				case c == 3:
					emit(' ')
				case c < 14:
					emit('0' + c - 4)
				case c < 40:
					if text {
						emit('a' + c - 14)
					} else {
						emit('A' + c - 14)
					}
				default:
					return 0, false
				}
			case 1:
				emit(c)
				shift = 0
			case 2:
				switch {
				case c < 27:
					emit(int(dmShift2[c]))
				case c == 27:
					*out = append(*out, 0x1D)
				case c == 30:
					*upper = true
				default:
					return 0, false
				}
				shift = 0
			case 3:
				switch {
				case !text:
					emit(c + 96)
				case c == 0:
					emit('`')
				case c < 27:
					emit('A' + c - 1)
				default:
					emit(int("{|}~\x7f"[c-27]))
				}
				shift = 0
			}
		}
	}
	return i, true
}

// dmDecodeX12 reads ANSI X12 from cw[i] until it unlatches.
func dmDecodeX12(cw []uint8, i int, out *[]byte) (int, bool) {
	for i < len(cw) {
		if cw[i] == 254 {
			return i + 1, true
		}
		if i+1 >= len(cw) {
			return i, true
		}
		v := int(cw[i])<<8 | int(cw[i+1]) - 1
		i += 2
		for _, c := range [3]int{v / 1600, v / 40 % 40, v % 40} {
			switch {
			case c == 0:
				*out = append(*out, '\r')
			case c == 1:
				*out = append(*out, '*')
			case c == 2:
				*out = append(*out, '>')
			case c == 3:
				*out = append(*out, ' ')
			case c < 14:
				*out = append(*out, byte('0'+c-4))
			case c < 40:
				*out = append(*out, byte('A'+c-14))
			default:
				return 0, false
			}
		}
	}
	return i, true
}

// dmDecodeEDIFACT reads EDIFACT from cw[i] until it unlatches.
func dmDecodeEDIFACT(cw []uint8, i int, out *[]byte) int {
	for i < len(cw) {
		if len(cw)-i < 3 {
			// The last one or two codewords are ASCII.
			return i
		}
		v := int(cw[i])<<16 | int(cw[i+1])<<8 | int(cw[i+2])
		for k := 0; k < 4; k++ {
			c := v >> (18 - 6*k) & 0x3F
			if c == 0x1F {
				// Unlatch: the rest of this codeword's bits are padding.
				return i + (6*k+6+7)/8
			}
			if c&0x20 == 0 {
				c |= 0x40
			}
			*out = append(*out, byte(c))
		}
		i += 3
	}
	return i
}

// dmDecodeBase256 reads Base 256 bytes from cw[i].
func dmDecodeBase256(cw []uint8, i int, out *[]byte) (int, bool) {
	unrand := func(c uint8, pos int) int {
		r := int(c) - (149*pos%255 + 1)
		if r < 0 {
			r += 256
		}
		return r
	}
	if i >= len(cw) {
		return i, false
	}
	d1 := unrand(cw[i], i+1)
	i++
	var n int
	switch {
	case d1 == 0:
		n = len(cw) - i
	case d1 < 250:
		n = d1
	default:
		if i >= len(cw) {
			return i, false
		}
		n = 250*(d1-249) + unrand(cw[i], i+1)
		i++
	}
	if n < 0 || i+n > len(cw) {
		return i, false
	}
	for k := 0; k < n; k++ {
		*out = append(*out, byte(unrand(cw[i], i+1)))
		i++
	}
	return i, true
}

// dmString is a symbol size's name, for messages.
func (s *dmSize) String() string { return strconv.Itoa(s.rows) + "x" + strconv.Itoa(s.cols) }
