package barcode

// gf256 is a Galois field of 256 elements, with a code's generator base:
// QR codes use the primitive polynomial 0x11D and roots from α^0, Data
// Matrix 0x12D and roots from α^1.
type gf256 struct {
	exp  [512]uint8
	log  [256]uint8
	base int
}

var (
	qrField = newGF256(0x11D, 0)
	dmField = newGF256(0x12D, 1)
)

func newGF256(poly, base int) *gf256 {
	f := &gf256{base: base}
	x := 1
	for i := 0; i < 255; i++ {
		f.exp[i] = uint8(x)
		f.log[x] = uint8(i)
		x <<= 1
		if x >= 256 {
			x ^= poly
		}
	}
	for i := 255; i < 512; i++ {
		f.exp[i] = f.exp[i-255]
	}
	return f
}

func (f *gf256) mul(a, b uint8) uint8 {
	if a == 0 || b == 0 {
		return 0
	}
	return f.exp[int(f.log[a])+int(f.log[b])]
}

func (f *gf256) div(a, b uint8) uint8 {
	if a == 0 {
		return 0
	}
	return f.exp[int(f.log[a])+255-int(f.log[b])]
}

// pow is α^e.
func (f *gf256) pow(e int) uint8 {
	e %= 255
	if e < 0 {
		e += 255
	}
	return f.exp[e]
}

// generator is the generator polynomial for n check codewords, highest
// coefficient first.
func (f *gf256) generator(n int) []uint8 {
	g := []uint8{1}
	for i := 0; i < n; i++ {
		r := f.pow(i + f.base)
		next := make([]uint8, len(g)+1)
		for j, c := range g {
			next[j] ^= c
			next[j+1] ^= f.mul(c, r)
		}
		g = next
	}
	return g
}

// rsEncode is the n check codewords for data.
func (f *gf256) rsEncode(data []uint8, n int) []uint8 {
	g := f.generator(n)
	rem := make([]uint8, n)
	for _, d := range data {
		c := d ^ rem[0]
		copy(rem, rem[1:])
		rem[n-1] = 0
		if c != 0 {
			for j := 0; j < n; j++ {
				rem[j] ^= f.mul(g[j+1], c)
			}
		}
	}
	return rem
}

// rsDecode corrects up to nCheck/2 wrong codewords of block (data then
// nCheck check codewords) in place, and reports whether it could.
func (f *gf256) rsDecode(block []uint8, nCheck int) bool {
	n := len(block)
	var synBuf [256]uint8
	syn := synBuf[:nCheck]
	errs := false
	for j := 0; j < nCheck; j++ {
		x := f.pow(j + f.base)
		var s uint8
		for _, c := range block {
			s = f.mul(s, x) ^ c
		}
		syn[j] = s
		if s != 0 {
			errs = true
		}
	}
	if !errs {
		return true
	}
	// Berlekamp–Massey: the error locator Λ, lowest coefficient first.
	var lamBuf, prevBuf, tmpBuf [257]uint8
	lam := lamBuf[:nCheck+1]
	prev := prevBuf[:nCheck+1]
	lam[0], prev[0] = 1, 1
	l, m := 0, 1
	var b uint8 = 1
	for k := 0; k < nCheck; k++ {
		d := syn[k]
		for i := 1; i <= l; i++ {
			d ^= f.mul(lam[i], syn[k-i])
		}
		if d == 0 {
			m++
			continue
		}
		coef := f.div(d, b)
		if 2*l <= k {
			tmp := tmpBuf[:nCheck+1]
			copy(tmp, lam)
			for i := 0; i+m <= nCheck; i++ {
				lam[i+m] ^= f.mul(coef, prev[i])
			}
			copy(prev, tmp)
			l = k + 1 - l
			b = d
			m = 1
		} else {
			for i := 0; i+m <= nCheck; i++ {
				lam[i+m] ^= f.mul(coef, prev[i])
			}
			m++
		}
	}
	if l == 0 || 2*l > nCheck {
		return false
	}
	// Ω = S·Λ mod x^nCheck.
	var omBuf [256]uint8
	om := omBuf[:nCheck]
	for i := 0; i < nCheck; i++ {
		var v uint8
		for j := 0; j <= i && j <= l; j++ {
			v ^= f.mul(lam[j], syn[i-j])
		}
		om[i] = v
	}
	// Chien search over the block's positions, then Forney.
	found := 0
	for pos := 0; pos < n; pos++ {
		// The locator of position pos is X = α^(n-1-pos); test Λ(X⁻¹).
		e := n - 1 - pos
		xinv := f.pow(-e)
		var v uint8
		xp := uint8(1)
		for i := 0; i <= l; i++ {
			v ^= f.mul(lam[i], xp)
			xp = f.mul(xp, xinv)
		}
		if v != 0 {
			continue
		}
		var num, den uint8
		xp = 1
		for i := 0; i < nCheck; i++ {
			num ^= f.mul(om[i], xp)
			xp = f.mul(xp, xinv)
		}
		// Λ' keeps the odd terms: Σ Λ_i x^(i-1), i odd.
		xinv2 := f.mul(xinv, xinv)
		xp = 1
		for i := 1; i <= l; i += 2 {
			den ^= f.mul(lam[i], xp)
			xp = f.mul(xp, xinv2)
		}
		if den == 0 {
			return false
		}
		mag := f.div(num, den)
		// Times X^(1-base).
		mag = f.mul(mag, f.pow(e*(1-f.base)))
		block[pos] ^= mag
		found++
	}
	if found != l {
		return false
	}
	// Check: every syndrome is zero now.
	for j := 0; j < nCheck; j++ {
		x := f.pow(j + f.base)
		var s uint8
		for _, c := range block {
			s = f.mul(s, x) ^ c
		}
		if s != 0 {
			return false
		}
	}
	return true
}
