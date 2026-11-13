package barcode

import (
	"image"
	"math"
	"slices"
)

// Barcodes are read along scan lines across the image in several
// directions, each line both ways: its gray levels become the widths of
// its dark and light runs, between edges where the level swings, and
// readers match those against each symbology's patterns, as ZXing's
// OneDReader does. A code counts once two lines agree on it.

const (
	scanDirections = 12   // 15° apart: lines within 7.5° of across the bars
	scanSpacing    = 6.0  // pixels between parallel lines
	minAgree       = 2    // lines that must read a code alike
	minSwing       = 14   // gray levels: the least swing that makes an edge
	swingShare     = 0.18 // and the least share of the contrast around it
)

// read1D is a code read on one line: its kind, text and ends.
type read1D struct {
	kind Kind
	text string
	a, b point
}

// scanner holds a line's buffers, reused line after line.
type scanner struct {
	lum   *luminance
	want  kindSet
	sig   []float32
	thr   []float32
	blo   []float32
	ext   []extreme
	runs  []float32
	pos   []float32
	rruns []float32
	rpos  []float32
	reads []read1D
}

type extreme struct {
	pos int
	v   float32
}

func decode1D(l *luminance, want kindSet) []Code {
	s := &scanner{lum: l, want: want}
	w, h := float64(l.w), float64(l.h)
	for k := 0; k < scanDirections; k++ {
		th := float64(k) * math.Pi / scanDirections
		dx, dy := math.Cos(th), math.Sin(th)
		if math.Abs(dx) < 1e-9 {
			dx = 0
		}
		if math.Abs(dy) < 1e-9 {
			dy = 0
		}
		nx, ny := -dy, dx
		lo, hi := math.Inf(1), math.Inf(-1)
		for _, c := range [4]point{{0, 0}, {w - 1, 0}, {0, h - 1}, {w - 1, h - 1}} {
			o := c.x*nx + c.y*ny
			lo, hi = math.Min(lo, o), math.Max(hi, o)
		}
		for o := lo + scanSpacing/2; o <= hi; o += scanSpacing {
			s.scanLine(o*nx, o*ny, dx, dy)
		}
	}
	return s.agree()
}

// scanLine reads the line through (px, py) in direction (dx, dy).
func (s *scanner) scanLine(px, py, dx, dy float64) {
	l := s.lum
	w, h := float64(l.w-1), float64(l.h-1)
	t0, t1 := math.Inf(-1), math.Inf(1)
	clip := func(p, d, max float64) bool {
		if d == 0 {
			return p >= 0 && p <= max
		}
		a, b := -p/d, (max-p)/d
		if a > b {
			a, b = b, a
		}
		t0, t1 = math.Max(t0, a), math.Min(t1, b)
		return true
	}
	if !clip(px, dx, w) || !clip(py, dy, h) {
		return
	}
	t0 = math.Ceil(t0 - 1e-9)
	n := int(t1-t0) + 1
	if n < 40 {
		return
	}
	if cap(s.sig) < n {
		s.sig = make([]float32, n)
	}
	sig := s.sig[:n]
	x0, y0 := px+t0*dx, py+t0*dy
	switch {
	case dy == 0:
		row := l.pix[int(math.Round(y0))*l.w:]
		xs := int(math.Round(x0))
		for i := range sig {
			sig[i] = float32(row[xs+i])
		}
	case dx == 0:
		xs, ys := int(math.Round(x0)), int(math.Round(y0))
		for i := range sig {
			sig[i] = float32(l.pix[(ys+i)*l.w+xs])
		}
	default:
		fdx, fdy := float32(dx), float32(dy)
		fx, fy := float32(x0), float32(y0)
		for i := range sig {
			x, y := fx+float32(i)*fdx, fy+float32(i)*fdy
			ix, iy := int(x), int(y)
			if ix >= l.w-1 {
				ix = l.w - 2
			}
			if iy >= l.h-1 {
				iy = l.h - 2
			}
			if ix < 0 {
				ix = 0
			}
			if iy < 0 {
				iy = 0
			}
			ax, ay := x-float32(ix), y-float32(iy)
			o := iy*l.w + ix
			p := l.pix[o : o+l.w+2]
			top := float32(p[0]) + ax*(float32(p[1])-float32(p[0]))
			bot := float32(p[l.w]) + ax*(float32(p[l.w+1])-float32(p[l.w]))
			sig[i] = top + ay*(bot-top)
		}
	}
	firstDark := s.edges(sig)
	if len(s.runs) < 20 {
		return
	}
	at := func(t float32) point {
		return point{x0 + float64(t)*dx, y0 + float64(t)*dy}
	}
	s.decodeRuns(s.runs, s.pos, firstDark, func(i, j int, kind Kind, text string) {
		s.reads = append(s.reads, read1D{kind, text, at(s.pos[i]), at(s.pos[j])})
	})
	// And the other way.
	m := len(s.runs)
	s.rruns = s.rruns[:0]
	s.rpos = s.rpos[:0]
	for k := m - 1; k >= 0; k-- {
		s.rruns = append(s.rruns, s.runs[k])
		s.rpos = append(s.rpos, s.pos[k]+s.runs[k])
	}
	lastDark := firstDark == (m%2 == 1)
	s.decodeRuns(s.rruns, s.rpos, lastDark, func(i, j int, kind Kind, text string) {
		s.reads = append(s.reads, read1D{kind, text, at(s.rpos[i]), at(s.rpos[j])})
	})
}

// edges splits sig into runs at the edges between its swings, and
// reports whether the first run is dark. A swing counts when it's a
// good part of the contrast around it, so noise in flat parts and uneven
// light don't make edges.
func (s *scanner) edges(sig []float32) bool {
	n := len(sig)
	const blk = 16
	nb := (n + blk - 1) / blk
	if cap(s.thr) < nb {
		s.thr = make([]float32, nb)
	}
	thr := s.thr[:nb]
	// Each block's range, then the range of the five blocks around it.
	if cap(s.blo) < 2*nb {
		s.blo = make([]float32, 2*nb)
	}
	blo, bhi := s.blo[:nb], s.blo[nb:2*nb]
	for b := 0; b < nb; b++ {
		seg := sig[b*blk : min((b+1)*blk, n)]
		mn, mx := seg[0], seg[0]
		for _, v := range seg {
			mn = min(mn, v)
			mx = max(mx, v)
		}
		blo[b], bhi[b] = mn, mx
	}
	for b := 0; b < nb; b++ {
		mn, mx := blo[b], bhi[b]
		for j := max(0, b-2); j <= min(nb-1, b+2); j++ {
			mn = min(mn, blo[j])
			mx = max(mx, bhi[j])
		}
		thr[b] = max(minSwing, swingShare*(mx-mn))
	}
	// Alternate extremes, each swing at least the threshold.
	ext := s.ext[:0]
	hiV, loV := sig[0], sig[0]
	hiP, loP := 0, 0
	dir := 0
	for k := 1; k < n; k++ {
		v := sig[k]
		t := thr[k/blk]
		switch dir {
		case 0:
			if v > hiV {
				hiV, hiP = v, k
			}
			if v < loV {
				loV, loP = v, k
			}
			if hiV-loV >= t {
				if hiP > loP {
					ext = append(ext, extreme{loP, loV})
					dir = 1
				} else {
					ext = append(ext, extreme{hiP, hiV})
					dir = -1
					loV, loP = v, k
				}
			}
		case 1:
			if v > hiV {
				hiV, hiP = v, k
			} else if hiV-v >= t {
				ext = append(ext, extreme{hiP, hiV})
				dir = -1
				loV, loP = v, k
			}
		default:
			if v < loV {
				loV, loP = v, k
			} else if v-loV >= t {
				ext = append(ext, extreme{loP, loV})
				dir = 1
				hiV, hiP = v, k
			}
		}
	}
	// The last extreme, if the level got there.
	if len(ext) > 0 {
		last := ext[len(ext)-1]
		t := thr[(n-1)/blk]
		if dir == 1 && hiV-last.v >= t {
			ext = append(ext, extreme{hiP, hiV})
		} else if dir == -1 && last.v-loV >= t {
			ext = append(ext, extreme{loP, loV})
		}
	}
	s.ext = ext
	s.runs = s.runs[:0]
	s.pos = s.pos[:0]
	if len(ext) < 2 {
		return false
	}
	// An edge where the level crosses the middle of two extremes.
	prev := float32(0)
	s.pos = append(s.pos, 0)
	for i := 0; i+1 < len(ext); i++ {
		a, b := ext[i], ext[i+1]
		mid := (a.v + b.v) / 2
		e := float32(a.pos)
		for k := a.pos; k < b.pos; k++ {
			v0, v1 := sig[k]-mid, sig[k+1]-mid
			if (v0 <= 0) != (v1 <= 0) {
				e = float32(k) + v0/(v0-v1)
				break
			}
		}
		s.runs = append(s.runs, e-prev)
		s.pos = append(s.pos, e)
		prev = e
	}
	s.runs = append(s.runs, float32(n-1)-prev)
	// The first run is dark if its extreme is a trough.
	return ext[0].v < ext[1].v
}

// decodeRuns tries every reader wanted at every dark run after a light
// one; found gets the run indexes of a code's first bar and of the quiet
// zone after it.
func (s *scanner) decodeRuns(runs, pos []float32, firstDark bool, found func(i, j int, kind Kind, text string)) {
	start := 1
	if firstDark {
		start = 2
	}
	for i := start; i+8 < len(runs); i += 2 {
		if runs[i-1] < 2*runs[i] {
			continue
		}
		if s.want&(kEAN13|kUPCA|kEAN8|kUPCE) != 0 {
			if kind, text, j, ok := decodeUPCEAN(runs, i, s.want); ok {
				found(i, j, kind, text)
				i = j - 1
				continue
			}
		}
		if s.want&kCode128 != 0 {
			if text, j, ok := decodeCode128(runs, i); ok {
				found(i, j, Code128, text)
				i = j - 1
				continue
			}
		}
		if s.want&kCode39 != 0 {
			if text, j, ok := decodeCode39(runs, i); ok {
				found(i, j, Code39, text)
				i = j - 1
				continue
			}
		}
	}
}

// agree groups the reads by code and place, and is the codes read on
// enough lines.
func (s *scanner) agree() []Code {
	type group struct {
		kind   Kind
		text   string
		cx, cy float64
		length float64
		n      int
		x0, y0 float64
		x1, y1 float64
	}
	var gs []group
	for _, r := range s.reads {
		mx, my := (r.a.x+r.b.x)/2, (r.a.y+r.b.y)/2
		l := dist(r.a, r.b)
		k := -1
		for i := range gs {
			g := &gs[i]
			if g.kind != r.kind || g.text != r.text {
				continue
			}
			if math.Hypot(g.cx-mx, g.cy-my) < 0.6*math.Max(l, g.length) {
				k = i
				break
			}
		}
		if k < 0 {
			gs = append(gs, group{kind: r.kind, text: r.text, x0: math.Inf(1), y0: math.Inf(1), x1: math.Inf(-1), y1: math.Inf(-1)})
			k = len(gs) - 1
		}
		g := &gs[k]
		f := float64(g.n)
		g.cx = (g.cx*f + mx) / (f + 1)
		g.cy = (g.cy*f + my) / (f + 1)
		g.length = (g.length*f + l) / (f + 1)
		g.n++
		for _, p := range [2]point{r.a, r.b} {
			g.x0, g.y0 = math.Min(g.x0, p.x), math.Min(g.y0, p.y)
			g.x1, g.y1 = math.Max(g.x1, p.x), math.Max(g.y1, p.y)
		}
	}
	// One place, one code: where reads of different codes overlap, the
	// code most lines read wins.
	slices.SortStableFunc(gs, func(a, b group) int { return b.n - a.n })
	var kept []int
	var out []Code
	for gi, g := range gs {
		if g.n < minAgree {
			continue
		}
		clash := false
		for _, k := range kept {
			o := &gs[k]
			if g.cx > o.x0 && g.cx < o.x1 && g.cy > o.y0 && g.cy < o.y1 {
				clash = true
				break
			}
		}
		if clash {
			continue
		}
		kept = append(kept, gi)
		kind, text := g.kind, g.text
		if kind == EAN13 {
			// A UPC-A is an EAN-13 that starts with 0.
			switch {
			case text[0] == '0' && s.want&kUPCA != 0:
				kind, text = UPCA, text[1:]
			case s.want&kEAN13 == 0:
				continue
			}
		}
		r := image.Rect(int(math.Floor(g.x0)), int(math.Floor(g.y0)), int(math.Ceil(g.x1))+1, int(math.Ceil(g.y1))+1)
		out = append(out, Code{kind, text, r})
	}
	return out
}

// matchVariance is how far counters are from pattern, scaled to the same
// width: the mean error per unit, or +Inf if one element is off by more
// than maxIndividual units.
func matchVariance(counters []float32, pattern []uint8, maxIndividual float32) float32 {
	var total float32
	var plen int
	for i, c := range counters {
		total += c
		plen += int(pattern[i])
	}
	if total < float32(plen)*0.8 {
		return float32(math.Inf(1))
	}
	unit := total / float32(plen)
	maxIndividual *= unit
	var v float32
	for i, c := range counters {
		d := c - float32(pattern[i])*unit
		if d < 0 {
			d = -d
		}
		if d > maxIndividual {
			return float32(math.Inf(1))
		}
		v += d
	}
	return v / total
}

func sum32(r []float32) float32 {
	var t float32
	for _, v := range r {
		t += v
	}
	return t
}
