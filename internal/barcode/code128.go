package barcode

import "strings"

// Code 128: symbols of three bars and three spaces, eleven modules; the
// stop symbol has a last bar of two more. Code sets A, B and C, a
// weighted check symbol modulo 103.

const (
	c128MaxAvgVariance        = 0.25
	c128MaxIndividualVariance = 0.7

	c128FNC1   = 102
	c128StartA = 103
	c128StartB = 104
	c128StartC = 105
	c128Stop   = 106
)

var c128Patterns = [107][6]uint8{
	{2, 1, 2, 2, 2, 2}, {2, 2, 2, 1, 2, 2}, {2, 2, 2, 2, 2, 1}, {1, 2, 1, 2, 2, 3}, {1, 2, 1, 3, 2, 2},
	{1, 3, 1, 2, 2, 2}, {1, 2, 2, 2, 1, 3}, {1, 2, 2, 3, 1, 2}, {1, 3, 2, 2, 1, 2}, {2, 2, 1, 2, 1, 3},
	{2, 2, 1, 3, 1, 2}, {2, 3, 1, 2, 1, 2}, {1, 1, 2, 2, 3, 2}, {1, 2, 2, 1, 3, 2}, {1, 2, 2, 2, 3, 1},
	{1, 1, 3, 2, 2, 2}, {1, 2, 3, 1, 2, 2}, {1, 2, 3, 2, 2, 1}, {2, 2, 3, 2, 1, 1}, {2, 2, 1, 1, 3, 2},
	{2, 2, 1, 2, 3, 1}, {2, 1, 3, 2, 1, 2}, {2, 2, 3, 1, 1, 2}, {3, 1, 2, 1, 3, 1}, {3, 1, 1, 2, 2, 2},
	{3, 2, 1, 1, 2, 2}, {3, 2, 1, 2, 2, 1}, {3, 1, 2, 2, 1, 2}, {3, 2, 2, 1, 1, 2}, {3, 2, 2, 2, 1, 1},
	{2, 1, 2, 1, 2, 3}, {2, 1, 2, 3, 2, 1}, {2, 3, 2, 1, 2, 1}, {1, 1, 1, 3, 2, 3}, {1, 3, 1, 1, 2, 3},
	{1, 3, 1, 3, 2, 1}, {1, 1, 2, 3, 1, 3}, {1, 3, 2, 1, 1, 3}, {1, 3, 2, 3, 1, 1}, {2, 1, 1, 3, 1, 3},
	{2, 3, 1, 1, 1, 3}, {2, 3, 1, 3, 1, 1}, {1, 1, 2, 1, 3, 3}, {1, 1, 2, 3, 3, 1}, {1, 3, 2, 1, 3, 1},
	{1, 1, 3, 1, 2, 3}, {1, 1, 3, 3, 2, 1}, {1, 3, 3, 1, 2, 1}, {3, 1, 3, 1, 2, 1}, {2, 1, 1, 3, 3, 1},
	{2, 3, 1, 1, 3, 1}, {2, 1, 3, 1, 1, 3}, {2, 1, 3, 3, 1, 1}, {2, 1, 3, 1, 3, 1}, {3, 1, 1, 1, 2, 3},
	{3, 1, 1, 3, 2, 1}, {3, 3, 1, 1, 2, 1}, {3, 1, 2, 1, 1, 3}, {3, 1, 2, 3, 1, 1}, {3, 3, 2, 1, 1, 1},
	{3, 1, 4, 1, 1, 1}, {2, 2, 1, 4, 1, 1}, {4, 3, 1, 1, 1, 1}, {1, 1, 1, 2, 2, 4}, {1, 1, 1, 4, 2, 2},
	{1, 2, 1, 1, 2, 4}, {1, 2, 1, 4, 2, 1}, {1, 4, 1, 1, 2, 2}, {1, 4, 1, 2, 2, 1}, {1, 1, 2, 2, 1, 4},
	{1, 1, 2, 4, 1, 2}, {1, 2, 2, 1, 1, 4}, {1, 2, 2, 4, 1, 1}, {1, 4, 2, 1, 1, 2}, {1, 4, 2, 2, 1, 1},
	{2, 4, 1, 2, 1, 1}, {2, 2, 1, 1, 1, 4}, {4, 1, 3, 1, 1, 1}, {2, 4, 1, 1, 1, 2}, {1, 3, 4, 1, 1, 1},
	{1, 1, 1, 2, 4, 2}, {1, 2, 1, 1, 4, 2}, {1, 2, 1, 2, 4, 1}, {1, 1, 4, 2, 1, 2}, {1, 2, 4, 1, 1, 2},
	{1, 2, 4, 2, 1, 1}, {4, 1, 1, 2, 1, 2}, {4, 2, 1, 1, 1, 2}, {4, 2, 1, 2, 1, 1}, {2, 1, 2, 1, 4, 1},
	{2, 1, 4, 1, 2, 1}, {4, 1, 2, 1, 2, 1}, {1, 1, 1, 1, 4, 3}, {1, 1, 1, 3, 4, 1}, {1, 3, 1, 1, 4, 1},
	{1, 1, 4, 1, 1, 3}, {1, 1, 4, 3, 1, 1}, {4, 1, 1, 1, 1, 3}, {4, 1, 1, 3, 1, 1}, {1, 1, 3, 1, 4, 1},
	{1, 1, 4, 1, 3, 1}, {3, 1, 1, 1, 4, 1}, {4, 1, 1, 1, 3, 1}, {2, 1, 1, 4, 1, 2}, {2, 1, 1, 2, 1, 4},
	{2, 1, 1, 2, 3, 2}, {2, 3, 3, 1, 1, 1},
}

// c128Symbol matches six runs to a symbol, or -1.
func c128Symbol(r []float32) int {
	best, bestV := -1, float32(c128MaxAvgVariance)
	for c := range c128Patterns {
		if v := matchVariance(r, c128Patterns[c][:], c128MaxIndividualVariance); v < bestV {
			best, bestV = c, v
		}
	}
	return best
}

// decodeCode128 reads a Code 128 whose start symbol is at runs[i]: its
// text and the index of the quiet zone after it.
func decodeCode128(runs []float32, i int) (string, int, bool) {
	if i+6 > len(runs) {
		return "", 0, false
	}
	start, bestV := -1, float32(c128MaxAvgVariance)
	for c := c128StartA; c <= c128StartC; c++ {
		if v := matchVariance(runs[i:i+6], c128Patterns[c][:], c128MaxIndividualVariance); v < bestV {
			start, bestV = c, v
		}
	}
	if start < 0 {
		return "", 0, false
	}
	width := sum32(runs[i : i+6])
	if runs[i-1] < width/2 {
		return "", 0, false
	}
	var codes []int
	k := i + 6
	for {
		if k+6 > len(runs) {
			return "", 0, false
		}
		c := c128Symbol(runs[k : k+6])
		if c < 0 || (c >= c128StartA && c <= c128StartC) {
			return "", 0, false
		}
		w := sum32(runs[k : k+6])
		// Symbols keep their width.
		if w < 0.7*width || w > 1.4*width {
			return "", 0, false
		}
		if c == c128Stop {
			m := w / 11
			if k+7 >= len(runs) {
				return "", 0, false
			}
			last := runs[k+6]
			if last < 1.3*m || last > 2.7*m || runs[k+7] < w/2 {
				return "", 0, false
			}
			k += 7
			break
		}
		codes = append(codes, c)
		k += 6
	}
	if len(codes) < 2 {
		return "", 0, false
	}
	sum := start
	for n, c := range codes[:len(codes)-1] {
		sum += (n + 1) * c
	}
	if sum%103 != codes[len(codes)-1] {
		return "", 0, false
	}
	text, ok := c128Text(start, codes[:len(codes)-1])
	if !ok || text == "" {
		return "", 0, false
	}
	return text, k, true
}

// c128Text interprets the symbols after the start one.
func c128Text(start int, codes []int) (string, bool) {
	const (
		setA = iota
		setB
		setC
	)
	set := start - c128StartA
	var out strings.Builder
	upper, shiftUpper := false, false
	put := func(c int) {
		if upper != shiftUpper {
			c += 128
		}
		shiftUpper = false
		out.WriteRune(rune(c))
	}
	for n := 0; n < len(codes); n++ {
		c := codes[n]
		cur := set
		for {
			switch cur {
			case setA, setB:
				if cur == setA && c < 64 {
					put(' ' + c)
					break
				}
				if cur == setA && c < 96 {
					put(c - 64)
					break
				}
				if cur == setB && c < 96 {
					put(' ' + c)
					break
				}
				switch c {
				case c128FNC1:
					if n > 0 {
						out.WriteByte(0x1D)
					}
				case 96, 97: // FNC3, FNC2
				case 98: // shift: the next symbol in the other set
					if n+1 >= len(codes) {
						return "", false
					}
					n++
					c = codes[n]
					if cur == setA {
						cur = setB
					} else {
						cur = setA
					}
					continue
				case 99:
					set = setC
				case 100:
					if cur == setA {
						set = setB
					} else {
						fnc4(&upper, &shiftUpper)
					}
				case 101:
					if cur == setB {
						set = setA
					} else {
						fnc4(&upper, &shiftUpper)
					}
				}
			case setC:
				if c < 100 {
					out.WriteByte(byte('0' + c/10))
					out.WriteByte(byte('0' + c%10))
					break
				}
				switch c {
				case c128FNC1:
					if n > 0 {
						out.WriteByte(0x1D)
					}
				case 100:
					set = setB
				case 101:
					set = setA
				}
			}
			break
		}
	}
	return out.String(), true
}

// fnc4 is FNC4: once, the next character is extended (+128); twice,
// every one is until it's done twice again.
func fnc4(upper, shiftUpper *bool) {
	switch {
	case !*upper && *shiftUpper:
		*upper, *shiftUpper = true, false
	case *upper && *shiftUpper:
		*upper, *shiftUpper = false, false
	default:
		*shiftUpper = true
	}
}
