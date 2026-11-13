package barcode

// EAN and UPC: digits of seven modules in two bars and two spaces,
// between guards; left-hand digits in odd (L) or even (G) parity, whose
// pattern says EAN-13's first digit (UPC-E's number system and check
// digit).

const (
	upcMaxAvgVariance        = 0.48
	upcMaxIndividualVariance = 0.7
)

// upcL is each digit's L widths, space first; G is them reversed, and R
// is L's widths, bar first.
var upcL = [10][4]uint8{
	{3, 2, 1, 1}, {2, 2, 2, 1}, {2, 1, 2, 2}, {1, 4, 1, 1}, {1, 1, 3, 2},
	{1, 2, 3, 1}, {1, 1, 1, 4}, {1, 3, 1, 2}, {1, 2, 1, 3}, {3, 1, 1, 2},
}

// eanFirstDigit is, for EAN-13's first digit, which of the six left
// digits are G (bit 5 the first).
var eanFirstDigit = [10]uint8{0x00, 0x0B, 0x0D, 0x0E, 0x13, 0x19, 0x1C, 0x15, 0x16, 0x1A}

// upceParity is UPC-E's parity patterns by number system and check
// digit.
var upceParity = [2][10]uint8{
	{0x38, 0x34, 0x32, 0x31, 0x2C, 0x26, 0x23, 0x2A, 0x29, 0x25},
	{0x07, 0x0B, 0x0D, 0x0E, 0x13, 0x19, 0x1C, 0x15, 0x16, 0x1A},
}

var (
	guard3 = []uint8{1, 1, 1}
	guard5 = []uint8{1, 1, 1, 1, 1}
	guard6 = []uint8{1, 1, 1, 1, 1, 1}
)

// upcDigit matches four runs to a digit: 0–9 for L (or R), 10–19 for G.
func upcDigit(r []float32, g bool) int {
	best, bestV := -1, float32(upcMaxAvgVariance)
	var rev [4]uint8
	for d := 0; d < 10; d++ {
		if v := matchVariance(r, upcL[d][:], upcMaxIndividualVariance); v < bestV {
			best, bestV = d, v
		}
		if g {
			p := upcL[d]
			rev = [4]uint8{p[3], p[2], p[1], p[0]}
			if v := matchVariance(r, rev[:], upcMaxIndividualVariance); v < bestV {
				best, bestV = d+10, v
			}
		}
	}
	return best
}

func guardOK(r []float32, p []uint8) bool {
	return matchVariance(r, p, upcMaxIndividualVariance) < upcMaxAvgVariance
}

// eanCheck reports whether digits' last is their check digit.
func eanCheck(digits []byte) bool {
	n := len(digits)
	sum := 0
	for i := n - 2; i >= 0; i-- {
		d := int(digits[i] - '0')
		if (n-2-i)%2 == 0 {
			d *= 3
		}
		sum += d
	}
	return (10-sum%10)%10 == int(digits[n-1]-'0')
}

// decodeUPCEAN reads an EAN-13 (UPC-A among them, as EAN-13), EAN-8 or
// UPC-E whose start guard is at runs[i]: its kind, text and the index of
// the quiet zone after it.
func decodeUPCEAN(runs []float32, i int, want kindSet) (Kind, string, int, bool) {
	if i+3 > len(runs) || !guardOK(runs[i:i+3], guard3) {
		return "", "", 0, false
	}
	m := sum32(runs[i:i+3]) / 3
	if runs[i-1] < quietModules*m {
		return "", "", 0, false
	}
	if want&(kEAN13|kUPCA) != 0 {
		if text, j, ok := decodeEAN13(runs, i+3, m); ok && evenModules(runs[i:j], 6, 6) {
			return EAN13, text, j, true
		}
	}
	if want&kEAN8 != 0 {
		if text, j, ok := decodeEAN8(runs, i+3, m); ok && evenModules(runs[i:j], 4, 4) {
			return EAN8, text, j, true
		}
	}
	if want&kUPCE != 0 {
		if text, j, ok := decodeUPCE(runs, i+3, m); ok && evenModules(runs[i:j], 6, 0) {
			return UPCE, text, j, true
		}
	}
	return "", "", 0, false
}

// evenModules reports whether a code's guards and digits (left digits,
// then right ones after a middle guard, if any) are all about as wide
// as their modules say: one module size throughout, give or take what
// perspective does.
func evenModules(runs []float32, left, right int) bool {
	total := 3 + 7*left + 7*right + 3
	if right > 0 {
		total += 5
	} else {
		total += 3 // UPC-E's end guard is six modules
	}
	u := sum32(runs) / float32(total)
	ok := func(r []float32, modules int) bool {
		w := sum32(r) / float32(modules)
		return w > 0.7*u && w < 1.4*u
	}
	k := 0
	group := func(n, modules int) bool {
		g := ok(runs[k:k+n], modules)
		k += n
		return g
	}
	if !group(3, 3) {
		return false
	}
	for d := 0; d < left; d++ {
		if !group(4, 7) {
			return false
		}
	}
	if right == 0 {
		return group(6, 6)
	}
	if !group(5, 5) {
		return false
	}
	for d := 0; d < right; d++ {
		if !group(4, 7) {
			return false
		}
	}
	return group(3, 3)
}

// quietModules is the least quiet zone, in modules, each side: more
// than any space inside a code, so the middle of one isn't the end of
// another.
const quietModules = 5

// quietAfter reports whether runs[k] is light and wide enough to end a
// code of module m.
func quietAfter(runs []float32, k int, m float32) bool {
	return k < len(runs) && runs[k] >= quietModules*m
}

func decodeEAN13(runs []float32, k int, m float32) (string, int, bool) {
	if k+56 > len(runs) {
		return "", 0, false
	}
	var digits [13]byte
	parity := 0
	for d := 0; d < 6; d++ {
		v := upcDigit(runs[k:k+4], true)
		if v < 0 {
			return "", 0, false
		}
		if v >= 10 {
			parity |= 1 << (5 - d)
			v -= 10
		}
		digits[1+d] = byte('0' + v)
		k += 4
	}
	first := -1
	for d, p := range eanFirstDigit {
		if int(p) == parity {
			first = d
		}
	}
	if first < 0 || !guardOK(runs[k:k+5], guard5) {
		return "", 0, false
	}
	digits[0] = byte('0' + first)
	k += 5
	for d := 0; d < 6; d++ {
		v := upcDigit(runs[k:k+4], false)
		if v < 0 {
			return "", 0, false
		}
		digits[7+d] = byte('0' + v)
		k += 4
	}
	if !guardOK(runs[k:k+3], guard3) || !quietAfter(runs, k+3, m) || !eanCheck(digits[:]) {
		return "", 0, false
	}
	return string(digits[:]), k + 3, true
}

func decodeEAN8(runs []float32, k int, m float32) (string, int, bool) {
	if k+40 > len(runs) {
		return "", 0, false
	}
	var digits [8]byte
	for d := 0; d < 8; d++ {
		if d == 4 {
			if !guardOK(runs[k:k+5], guard5) {
				return "", 0, false
			}
			k += 5
		}
		v := upcDigit(runs[k:k+4], false)
		if v < 0 {
			return "", 0, false
		}
		digits[d] = byte('0' + v)
		k += 4
	}
	if !guardOK(runs[k:k+3], guard3) || !quietAfter(runs, k+3, m) || !eanCheck(digits[:]) {
		return "", 0, false
	}
	return string(digits[:]), k + 3, true
}

func decodeUPCE(runs []float32, k int, m float32) (string, int, bool) {
	if k+30 > len(runs) {
		return "", 0, false
	}
	var digits [8]byte
	parity := 0
	for d := 0; d < 6; d++ {
		v := upcDigit(runs[k:k+4], true)
		if v < 0 {
			return "", 0, false
		}
		if v >= 10 {
			parity |= 1 << (5 - d)
			v -= 10
		}
		digits[1+d] = byte('0' + v)
		k += 4
	}
	if !guardOK(runs[k:k+6], guard6) || !quietAfter(runs, k+6, m) {
		return "", 0, false
	}
	ok := false
	for sys := 0; sys < 2 && !ok; sys++ {
		for c := 0; c < 10; c++ {
			if int(upceParity[sys][c]) == parity {
				digits[0], digits[7] = byte('0'+sys), byte('0'+c)
				ok = true
				break
			}
		}
	}
	if !ok || !eanCheck([]byte(upceToUPCA(string(digits[:])))) {
		return "", 0, false
	}
	return string(digits[:]), k + 6, true
}

// upceToUPCA expands a UPC-E's eight digits to the UPC-A's twelve.
func upceToUPCA(e string) string {
	d := e[1:7]
	var mid string
	switch last := d[5]; last {
	case '0', '1', '2':
		mid = d[0:2] + string(last) + "0000" + d[2:5]
	case '3':
		mid = d[0:3] + "00000" + d[3:5]
	case '4':
		mid = d[0:4] + "00000" + d[4:5]
	default:
		mid = d[0:5] + "0000" + string(last)
	}
	return e[0:1] + mid + e[7:8]
}
