package barcode

// Code 39: characters of five bars and four spaces, three of them wide,
// apart by a narrow space, between * start and stop characters. No
// check character is read.

const code39Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ-. $/+%"

// code39Patterns is each character's wide elements, bit 8 the first.
var code39Patterns = [43]uint16{
	0x034, 0x121, 0x061, 0x160, 0x031, 0x130, 0x070, 0x025, 0x124, 0x064,
	0x109, 0x049, 0x148, 0x019, 0x118, 0x058, 0x00D, 0x10C, 0x04C, 0x01C,
	0x103, 0x043, 0x142, 0x013, 0x112, 0x052, 0x007, 0x106, 0x046, 0x016,
	0x181, 0x0C1, 0x1C0, 0x091, 0x190, 0x0D0, 0x085, 0x184, 0x0C4, 0x0A8,
	0x0A2, 0x08A, 0x02A,
}

const code39Asterisk = 0x094

// code39Wide sorts nine runs into narrow and wide: the pattern of the
// three wide ones, or -1 if they don't split cleanly.
func code39Wide(r []float32) int {
	// The three widest must be clearly wider than the six others.
	var idx [9]int
	for i := range idx {
		idx[i] = i
	}
	for a := 0; a < 9; a++ {
		for b := a + 1; b < 9; b++ {
			if r[idx[b]] > r[idx[a]] {
				idx[a], idx[b] = idx[b], idx[a]
			}
		}
	}
	narrowMax, wideMin := r[idx[3]], r[idx[2]]
	var nsum, wsum float32
	for k := 3; k < 9; k++ {
		nsum += r[idx[k]]
	}
	for k := 0; k < 3; k++ {
		wsum += r[idx[k]]
	}
	n, w := nsum/6, wsum/3
	if w < 1.45*n || w > 4*n || wideMin < 1.15*narrowMax || narrowMax > 1.6*n || r[idx[0]] > 1.6*w {
		return -1
	}
	p := 0
	for k := 0; k < 3; k++ {
		p |= 1 << (8 - idx[k])
	}
	return p
}

func code39Char(p int) byte {
	for i, c := range code39Patterns {
		if int(c) == p {
			return code39Alphabet[i]
		}
	}
	return 0
}

// decodeCode39 reads a Code 39 whose start character is at runs[i]: its
// text and the index of the quiet zone after it.
func decodeCode39(runs []float32, i int) (string, int, bool) {
	// The start character: narrow, wide, narrow, narrow, wide (bars and
	// spaces from the first).
	if i+10 > len(runs) || runs[i+1] < 1.3*runs[i] || runs[i+4] < 1.3*runs[i+3] || code39Wide(runs[i:i+9]) != code39Asterisk {
		return "", 0, false
	}
	width := sum32(runs[i : i+9])
	if runs[i-1] < width/2 {
		return "", 0, false
	}
	narrow := width / 15
	var text []byte
	k := i + 9
	for {
		// The gap between characters is narrow.
		if k+10 > len(runs) || runs[k] > 3*narrow {
			return "", 0, false
		}
		k++
		w := sum32(runs[k : k+9])
		if w < 0.75*width || w > 1.33*width {
			return "", 0, false
		}
		p := code39Wide(runs[k : k+9])
		if p < 0 {
			return "", 0, false
		}
		if p == code39Asterisk {
			if k+9 >= len(runs) || runs[k+9] < w/2 || len(text) == 0 {
				return "", 0, false
			}
			return string(text), k + 9, true
		}
		c := code39Char(p)
		if c == 0 {
			return "", 0, false
		}
		text = append(text, c)
		width = (width + w) / 2
		k += 9
	}
}
