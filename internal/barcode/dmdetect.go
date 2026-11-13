package barcode

import (
	"cmp"
	"image"
	"math"
	"slices"
	"sync"
)

// A Data Matrix symbol is found by its finder: the solid L of its left
// column and bottom row, whose two outer edges are long straight sides
// of the convex hull of the dark region they're part of. The opposite
// edges alternate (the timing pattern); which sizes fit, and where the
// fourth corner is, are what makes them alternate best.

// run is a horizontal run of dark pixels, [x0, x1) on row y.
type run struct {
	x0, x1, y int32
	parent    int32
}

// regions holds the buffers labeling reuses, frame after frame.
type regions struct {
	runs     []run
	rowStart []int32
	count    []int32
	grouped  []run
	spans    [][2]int32
	pts      []point
	hull     []point
	sides    []side
}

var regionPool = sync.Pool{New: func() any { return new(regions) }}

// label groups b's dark regions (8-connected) of four rows or more:
// each span of r.grouped is one, its runs in row order.
func (r *regions) label(b *bitImage) {
	runs := r.runs[:0]
	if cap(r.rowStart) < b.h+1 {
		r.rowStart = make([]int32, b.h+1)
	}
	rowStart := r.rowStart[:b.h+1]
	for y := 0; y < b.h; y++ {
		rowStart[y] = int32(len(runs))
		row := b.pix[y*b.w : (y+1)*b.w]
		for x := 0; x < b.w; {
			if row[x] == 0 {
				x++
				continue
			}
			s := x
			for x < b.w && row[x] != 0 {
				x++
			}
			runs = append(runs, run{int32(s), int32(x), int32(y), int32(len(runs))})
		}
	}
	rowStart[b.h] = int32(len(runs))
	r.runs = runs
	find := func(i int32) int32 {
		for runs[i].parent != i {
			runs[i].parent = runs[runs[i].parent].parent
			i = runs[i].parent
		}
		return i
	}
	for y := 1; y < b.h; y++ {
		p, pe := rowStart[y-1], rowStart[y]
		c, ce := rowStart[y], rowStart[y+1]
		for c < ce && p < pe {
			if runs[p].x0 <= runs[c].x1 && runs[c].x0 <= runs[p].x1 {
				a, bb := find(p), find(c)
				if a != bb {
					// The root is the region's first run.
					if a < bb {
						runs[bb].parent = a
					} else {
						runs[a].parent = bb
					}
				}
			}
			if runs[p].x1 < runs[c].x1 {
				p++
			} else {
				c++
			}
		}
	}
	// Count each region's runs, give each region its span, then place
	// the runs in order.
	if cap(r.count) < len(runs) {
		r.count = make([]int32, len(runs))
	}
	count := r.count[:len(runs)]
	clear(count)
	for i := range runs {
		root := find(int32(i))
		runs[i].parent = root
		count[root]++
	}
	r.spans = r.spans[:0]
	total := int32(0)
	for i := range runs {
		if runs[i].parent == int32(i) && count[i] >= 4 {
			r.spans = append(r.spans, [2]int32{total, total})
			n := count[i]
			// From here on, count[root] is the region's index.
			count[i] = int32(len(r.spans) - 1)
			total += n
		} else if runs[i].parent == int32(i) {
			count[i] = -1
		}
	}
	if cap(r.grouped) < int(total) {
		r.grouped = make([]run, total)
	}
	r.grouped = r.grouped[:total]
	for i := range runs {
		k := count[runs[i].parent]
		if k < 0 {
			continue
		}
		sp := &r.spans[k]
		r.grouped[sp[1]] = runs[i]
		sp[1]++
	}
}

// convexHull is the convex hull of a region's pixels (their corners).
func (r *regions) convexHull(rs []run) []point {
	// At each row boundary, the leftmost and rightmost pixel corners of
	// the rows either side: sorted by y, then x.
	pts := r.pts[:0]
	i := 0
	prevLo, prevHi := int32(math.MaxInt32), int32(math.MinInt32)
	for i < len(rs) {
		y := rs[i].y
		lo, hi := rs[i].x0, rs[i].x1
		for i < len(rs) && rs[i].y == y {
			lo = min(lo, rs[i].x0)
			hi = max(hi, rs[i].x1)
			i++
		}
		fy := float64(y)
		pts = append(pts, point{float64(min(lo, prevLo)), fy}, point{float64(max(hi, prevHi)), fy})
		prevLo, prevHi = lo, hi
	}
	last := float64(rs[len(rs)-1].y + 1)
	pts = append(pts, point{float64(prevLo), last}, point{float64(prevHi), last})
	r.pts = pts
	cross := func(o, a, b point) float64 { return (a.x-o.x)*(b.y-o.y) - (a.y-o.y)*(b.x-o.x) }
	h := r.hull[:0]
	for _, p := range pts {
		for len(h) >= 2 && cross(h[len(h)-2], h[len(h)-1], p) >= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, p)
	}
	lower := len(h) + 1
	for i := len(pts) - 2; i >= 0; i-- {
		p := pts[i]
		for len(h) >= lower && cross(h[len(h)-2], h[len(h)-1], p) >= 0 {
			h = h[:len(h)-1]
		}
		h = append(h, p)
	}
	r.hull = h
	return h[:len(h)-1]
}

// side is a straight stretch of a hull: consecutive edges in nearly the
// same direction.
type side struct {
	a, b point
	len  float64
}

func hullSides(h []point, out []side) []side {
	n := len(h)
	if n < 3 {
		return nil
	}
	// Start after the sharpest turn, so no side is split at the seam.
	start, sharpest := 0, 2.0
	for i := 0; i < n; i++ {
		p, q, r := h[(i+n-1)%n], h[i], h[(i+1)%n]
		d1x, d1y := q.x-p.x, q.y-p.y
		d2x, d2y := r.x-q.x, r.y-q.y
		c := (d1x*d2x + d1y*d2y) / (math.Hypot(d1x, d1y) * math.Hypot(d2x, d2y))
		if c < sharpest {
			start, sharpest = i, c
		}
	}
	const maxTurn = 0.985 // cos 10°
	out = out[:0]
	cur := side{a: h[start], b: h[(start+1)%n]}
	for k := 1; k < n; k++ {
		q := h[(start+k+1)%n]
		dx, dy := cur.b.x-cur.a.x, cur.b.y-cur.a.y
		ex, ey := q.x-cur.b.x, q.y-cur.b.y
		c := (dx*ex + dy*ey) / (math.Hypot(dx, dy) * math.Hypot(ex, ey))
		if c >= maxTurn {
			cur.b = q
			continue
		}
		cur.len = dist(cur.a, cur.b)
		out = append(out, cur)
		cur = side{a: cur.b, b: q}
	}
	cur.len = dist(cur.a, cur.b)
	out = append(out, cur)
	return out
}

// intersect is where the lines through a and b cross.
func intersect(a, b side) (point, bool) {
	d1x, d1y := a.b.x-a.a.x, a.b.y-a.a.y
	d2x, d2y := b.b.x-b.a.x, b.b.y-b.a.y
	den := d1x*d2y - d1y*d2x
	if math.Abs(den) < 1e-9 {
		return point{}, false
	}
	t := ((b.a.x-a.a.x)*d2y - (b.a.y-a.a.y)*d2x) / den
	return point{a.a.x + t*d1x, a.a.y + t*d1y}, true
}

// lCorner is a candidate finder: its corner and the far ends of the
// bottom (p1) and left (p2) legs, and the hull's farthest point from
// the corner (p3), likely the symbol's fourth corner.
type lCorner struct{ p0, p1, p2, p3 point }

func (r *regions) lCorners(h []point, out []lCorner) []lCorner {
	ss := hullSides(h, r.sides)
	r.sides = ss
	n := len(ss)
	out = out[:0]
	const minLeg = 12
	for i := 0; i < n; i++ {
		a := ss[i]
		if a.len < minLeg {
			continue
		}
		// The next long side; a blurred corner may be cut by short ones.
		skipped := 0.0
		for skip := 1; skip <= 4 && skip < n; skip++ {
			b := ss[(i+skip)%n]
			if b.len < minLeg {
				skipped += b.len
				if skipped > 0.25*a.len {
					break
				}
				continue
			}
			if skipped > 0.25*b.len {
				break
			}
			r := a.len / b.len
			if r > 6 || r < 1.0/6 {
				break
			}
			dax, day := (a.b.x-a.a.x)/a.len, (a.b.y-a.a.y)/a.len
			dbx, dby := (b.b.x-b.a.x)/b.len, (b.b.y-b.a.y)/b.len
			if math.Abs(dax*dbx+day*dby) > 0.5 {
				break
			}
			p0, ok := intersect(a, b)
			if !ok {
				break
			}
			endA, endB := a.a, b.b
			ux, uy := endA.x-p0.x, endA.y-p0.y
			vx, vy := endB.x-p0.x, endB.y-p0.y
			c := lCorner{p0: p0, p1: endA, p2: endB}
			if ux*vy-uy*vx >= 0 {
				c.p1, c.p2 = endB, endA
			}
			// Along the diagonal, beyond both legs' far ends.
			dx, dy := c.p1.x+c.p2.x-2*p0.x, c.p1.y+c.p2.y-2*p0.y
			best := math.Inf(-1)
			for _, q := range h {
				if v := (q.x-p0.x)*dx + (q.y-p0.y)*dy; v > best {
					best, c.p3 = v, q
				}
			}
			out = append(out, c)
			break
		}
	}
	return out
}

// decodeDataMatrixCodes finds and reads the Data Matrix symbols in b.
func decodeDataMatrixCodes(b *bitImage) []Code {
	var codes []Code
	var found [][4]point
	r := regionPool.Get().(*regions)
	defer regionPool.Put(r)
	r.label(b)
	var ls []lCorner
	for _, sp := range r.spans {
		comp := r.grouped[sp[0]:sp[1]]
		x0, y0, x1, y1 := comp[0].x0, comp[0].y, comp[0].x1, comp[len(comp)-1].y+1
		for _, r := range comp {
			x0 = min(x0, r.x0)
			x1 = max(x1, r.x1)
		}
		if max(x1-x0, y1-y0) < 12 {
			continue
		}
		cx, cy := float64(x0+x1)/2, float64(y0+y1)/2
		if insideAny(found, point{cx, cy}) {
			continue
		}
		ls = r.lCorners(r.convexHull(comp), ls)
		for _, l := range ls {
			text, quad, ok := dmRead(b, l)
			if ok {
				found = append(found, quad)
				bx0, by0, bx1, by1 := boundsOf(quad[:]...)
				codes = append(codes, Code{DataMatrix, text, image.Rect(bx0, by0, bx1, by1)})
				break
			}
		}
	}
	return codes
}

func insideAny(qs [][4]point, p point) bool {
	for _, q := range qs {
		in := true
		for i := 0; i < 4; i++ {
			a, b := q[i], q[(i+1)%4]
			if (b.x-a.x)*(p.y-a.y)-(b.y-a.y)*(p.x-a.x) > 0 {
				in = false
				break
			}
		}
		if in {
			return true
		}
		// Either winding.
		in = true
		for i := 0; i < 4; i++ {
			a, b := q[i], q[(i+1)%4]
			if (b.x-a.x)*(p.y-a.y)-(b.y-a.y)*(p.x-a.x) < 0 {
				in = false
				break
			}
		}
		if in {
			return true
		}
	}
	return false
}

// legThickness is the module size the L's leg from p0 to p1 shows: how
// far its dark goes inward (toward in), at its thinner places.
func legThickness(b *bitImage, p0, p1 point, in point) float64 {
	var ts []float64
	l := dist(p0, p1)
	ix, iy := in.x, in.y
	for k := 1; k < 16; k++ {
		f := float64(k) / 16
		x := p0.x + f*(p1.x-p0.x)
		y := p0.y + f*(p1.y-p0.y)
		dark := func(t float64) bool { return b.at(int(math.Floor(x+t*ix)), int(math.Floor(y+t*iy))) }
		// The hull's line touches the outermost pixels; the edge may be
		// a pixel or two in.
		t := 0.125
		for t < 3 && !dark(t) {
			t += 0.25
		}
		if t >= 3 {
			continue
		}
		t0 := t
		for t < l/4 && dark(t) {
			t += 0.25
		}
		ts = append(ts, t-t0)
	}
	if len(ts) < 6 {
		return 0
	}
	slices.Sort(ts)
	return ts[len(ts)/5]
}

// lineStats samples b on the segment from a to c: the share of dark
// and the number of changes.
func lineStats(b *bitImage, a, c point) (dark float64, changes int) {
	l := dist(a, c)
	n := int(l*2) + 1
	prev := false
	nd := 0
	for i := 0; i <= n; i++ {
		f := float64(i) / float64(n)
		v := b.at(int(math.Floor(a.x+f*(c.x-a.x))), int(math.Floor(a.y+f*(c.y-a.y))))
		if v {
			nd++
		}
		if i > 0 && v != prev {
			changes++
		}
		prev = v
	}
	return float64(nd) / float64(n+1), changes
}

// dmRead tries the candidate finder l: the symbol's text and corners.
func dmRead(b *bitImage, l lCorner) (string, [4]point, bool) {
	var none [4]point
	lenA, lenB := dist(l.p0, l.p1), dist(l.p0, l.p2)
	ua := point{(l.p1.x - l.p0.x) / lenA, (l.p1.y - l.p0.y) / lenA}
	ub := point{(l.p2.x - l.p0.x) / lenB, (l.p2.y - l.p0.y) / lenB}
	ma := legThickness(b, l.p0, l.p1, ub)
	mb := legThickness(b, l.p0, l.p2, ua)
	if ma < 1 || mb < 1 {
		return "", none, false
	}
	m := (ma + mb) / 2
	if ma > 1.6*mb || mb > 1.6*ma {
		m = math.Min(ma, mb)
	}
	// The legs are solid half a module in.
	in := func(p point, u point, k float64) point { return point{p.x + k*u.x, p.y + k*u.y} }
	if d, _ := lineStats(b, in(l.p0, ub, m/2), in(l.p1, ub, m/2)); d < 0.85 {
		return "", none, false
	}
	if d, _ := lineStats(b, in(l.p0, ua, m/2), in(l.p2, ua, m/2)); d < 0.85 {
		return "", none, false
	}
	// The fourth corner: the parallelogram's, or the hull's if the
	// symbol is seen in perspective; the opposite edges alternate.
	var p3s []point
	ct, cr := 0, 0
	// Changes along the near half of each edge, where the guess of
	// the corner matters least.
	half := func(a, c point) point { return point{a.x + 0.5*(c.x-a.x), a.y + 0.5*(c.y-a.y)} }
	for _, p3 := range [2]point{{l.p1.x + l.p2.x - l.p0.x, l.p1.y + l.p2.y - l.p0.y}, l.p3} {
		ta, tc := point{l.p2.x - ub.x*m/2, l.p2.y - ub.y*m/2}, point{p3.x - ub.x*m/2, p3.y - ub.y*m/2}
		ra, rc := point{l.p1.x - ua.x*m/2, l.p1.y - ua.y*m/2}, point{p3.x - ua.x*m/2, p3.y - ua.y*m/2}
		_, t := lineStats(b, ta, half(ta, tc))
		_, r := lineStats(b, ra, half(ra, rc))
		t, r = 2*t, 2*r
		if dist(p3, l.p0) > 0.5*(lenA+lenB) {
			p3s = append(p3s, p3)
			ct, cr = max(ct, t), max(cr, r)
		}
	}
	if ct < 4 || cr < 4 {
		return "", none, false
	}
	// Which sizes fit: their timing pattern read along the near part of
	// each timing edge, where the guess of the fourth corner matters
	// least.
	type cand struct {
		s     *dmSize
		score float64
	}
	var cands []cand
	for i := range dmSizes {
		s := &dmSizes[i]
		w, h := float64(s.cols), float64(s.rows)
		if r := (lenA / lenB) / (w / h); r < 0.7 || r > 1.43 {
			continue
		}
		if mw := lenA / w; mw < 1.2 || mw > 3*m || mw < m/3 {
			continue
		}
		ax, ay := (l.p1.x-l.p0.x)/w, (l.p1.y-l.p0.y)/w
		cx, cy := (l.p2.x-l.p0.x)/h, (l.p2.y-l.p0.y)/h
		// Each edge from its own end, shifted a little either way: the
		// ends are only so exact.
		edge := func(o point, along, in point, n int) int {
			best := 0
			for _, sh := range [5]float64{0, -0.25, 0.25, -0.5, 0.5} {
				ok := 0
				for k := 0; k < n; k++ {
					f := float64(k) + 0.5 + sh
					x := o.x + f*along.x + 0.5*in.x
					y := o.y + f*along.y + 0.5*in.y
					if b.at(int(math.Floor(x)), int(math.Floor(y))) == (k%2 == 0) {
						ok++
					}
				}
				best = max(best, ok)
			}
			return best
		}
		// A big symbol's ends are a module or so off: its near quarter
		// still reads.
		nt := min(s.cols/2+2, max(12, s.cols/4+2))
		nr := min(s.rows/2+2, max(12, s.rows/4+2))
		ok := edge(l.p2, point{ax, ay}, point{-cx, -cy}, nt) + edge(l.p1, point{cx, cy}, point{-ax, -ay}, nr)
		n := nt + nr
		if sc := float64(ok) / float64(n); sc >= 0.7 {
			cands = append(cands, cand{s, sc})
		}
	}
	slices.SortStableFunc(cands, func(a, b cand) int { return cmp.Compare(b.score, a.score) })
	if len(cands) > 4 {
		cands = cands[:4]
	}
	// And the sizes nearest what the legs' thickness and the changes
	// along the timing edges say, keeping the legs' ratio.
	ests := [4][2]float64{
		{lenA / m, lenB / m},
		{float64(ct + 1), float64(cr + 1)},
		{float64(ct + 1), float64(ct+1) * lenB / lenA},
		{float64(cr+1) * lenA / lenB, float64(cr + 1)},
	}
	var near []cand
	for i := range dmSizes {
		s := &dmSizes[i]
		w, h := float64(s.cols), float64(s.rows)
		d := math.Inf(1)
		for _, e := range ests {
			d = math.Min(d, math.Abs(w-e[0])/w+math.Abs(h-e[1])/h)
		}
		if d <= 0.4 && !slices.ContainsFunc(cands, func(c cand) bool { return c.s == s }) {
			near = append(near, cand{s, -d})
		}
	}
	slices.SortStableFunc(near, func(a, b cand) int { return cmp.Compare(b.score, a.score) })
	cands = append(cands, near[:min(len(near), 3)]...)
	type fit struct {
		t     perspective
		quad  [4]point
		score int
	}
	for _, c := range cands {
		// Each guess of the fourth corner may settle elsewhere: the best
		// first, and each one that reads well enough.
		var fits [2]fit
		for i, p3 := range p3s {
			t, q, score := dmFit(b, l, p3, c.s)
			fits[i] = fit{t, q, score}
		}
		fs := fits[:len(p3s)]
		if len(fs) == 2 && fs[1].score > fs[0].score {
			fs[0], fs[1] = fs[1], fs[0]
		}
		for i, f := range fs {
			if float64(f.score) < 0.8*float64(len(dmFixed(c.s))) || (i == 1 && dist(f.quad[1], fs[0].quad[1]) < 0.5) {
				continue
			}
			g, ok := sampleGrid(b, &f.t, c.s.cols, c.s.rows)
			if !ok {
				continue
			}
			if text, ok := dmDecodeGrid(g, c.s); ok {
				return text, f.quad, true
			}
		}
	}
	return "", none, false
}

// dmModule is a finder or timing module: where, and whether it's dark.
type dmModule struct {
	x, y int16
	dark bool
}

var (
	fixedMu    sync.Mutex
	fixedCache = map[*dmSize][]dmModule{}
)

// dmFixed is the finder and timing modules of each data region of s,
// and the quiet zone's around the symbol.
func dmFixed(s *dmSize) []dmModule {
	fixedMu.Lock()
	defer fixedMu.Unlock()
	if f, ok := fixedCache[s]; ok {
		return f
	}
	var out []dmModule
	rh, rw := s.regRows+2, s.regCols+2
	for y := 0; y < s.rows; y++ {
		for x := 0; x < s.cols; x++ {
			ry, rx := y%rh, x%rw
			switch {
			case ry == rh-1 || rx == 0:
				out = append(out, dmModule{int16(x), int16(y), true})
			case ry == 0:
				out = append(out, dmModule{int16(x), int16(y), rx%2 == 0})
			case rx == rw-1:
				out = append(out, dmModule{int16(x), int16(y), (rh-1-ry)%2 == 0})
			}
		}
	}
	// And the quiet zone around: light.
	for x := -1; x <= s.cols; x++ {
		out = append(out, dmModule{int16(x), -1, false}, dmModule{int16(x), int16(s.rows), false})
	}
	for y := 0; y < s.rows; y++ {
		out = append(out, dmModule{-1, int16(y), false}, dmModule{int16(s.cols), int16(y), false})
	}
	fixedCache[s] = out
	return out
}

// dmFit places the fourth corner (and nudges the others) where the
// timing pattern of size s reads best: the transform from modules to
// the image, the corners, and how many finder modules read right.
func dmFit(b *bitImage, l lCorner, p3 point, s *dmSize) (perspective, [4]point, int) {
	W, H := float64(s.cols), float64(s.rows)
	a := point{(l.p1.x - l.p0.x) / W, (l.p1.y - l.p0.y) / W}
	c := point{(l.p2.x - l.p0.x) / H, (l.p2.y - l.p0.y) / H}
	corners := [4]point{l.p2, p3, l.p1, l.p0} // top left, top right, bottom right, bottom left
	transform := func(q [4]point) perspective {
		return quadToQuad(0, 0, W, 0, W, H, 0, H, q[0].x, q[0].y, q[1].x, q[1].y, q[2].x, q[2].y, q[3].x, q[3].y)
	}
	fixed := dmFixed(s)
	// How many fixed modules read right at their centers.
	centers := func(q [4]point) int {
		t := transform(q)
		n := 0
		for _, f := range fixed {
			px, py := t.apply(float64(f.x)+0.5, float64(f.y)+0.5)
			if b.at(int(math.Floor(px)), int(math.Floor(py))) == f.dark {
				n++
			}
		}
		return n
	}
	// And what the search climbs: also near each center's corners, so
	// the modules end up in the middle of where they read right.
	score := func(q [4]point) int {
		t := transform(q)
		n := 0
		for _, f := range fixed {
			fx, fy := float64(f.x)+0.5, float64(f.y)+0.5
			for _, o := range [5][3]float64{{0, 0, 4}, {-0.3, -0.3, 1}, {0.3, -0.3, 1}, {-0.3, 0.3, 1}, {0.3, 0.3, 1}} {
				px, py := t.apply(fx+o[0], fy+o[1])
				if b.at(int(math.Floor(px)), int(math.Floor(py))) == f.dark {
					n += int(o[2])
				}
			}
		}
		return n
	}
	// The fourth corner first, where the outer timing pattern (the top
	// row and right column alone) reads best: within a fifth of the
	// symbol (up to seven modules) of the guess, half a module at a time.
	edge := func(q [4]point) int {
		t := transform(q)
		n := 0
		for x := 0; x < s.cols; x++ {
			px, py := t.apply(float64(x)+0.5, 0.5)
			if b.at(int(math.Floor(px)), int(math.Floor(py))) == (x%2 == 0) {
				n++
			}
		}
		for y := 0; y < s.rows; y++ {
			px, py := t.apply(W-0.5, float64(y)+0.5)
			if b.at(int(math.Floor(px)), int(math.Floor(py))) == ((s.rows-1-y)%2 == 0) {
				n++
			}
		}
		return n
	}
	bestQ := corners
	bestE := edge(corners)
	r := max(4, min(14, int(0.4*math.Max(W, H))))
	for i := -r; i <= r; i++ {
		for j := -r; j <= r; j++ {
			q := corners
			q[1] = point{p3.x + 0.5*(float64(i)*a.x+float64(j)*c.x), p3.y + 0.5*(float64(i)*a.y+float64(j)*c.y)}
			if v := edge(q); v > bestE {
				bestE, bestQ = v, q
			}
		}
	}
	best := score(bestQ)
	// Then each corner in turn, a quarter module at a time, then an
	// eighth.
	for _, step := range [2]float64{0.25, 0.125} {
		for pass := 0; pass < 8; pass++ {
			improved := false
			for k := 0; k < 4; k++ {
				for _, d := range [8][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1}} {
					q := bestQ
					q[k].x += step * (d[0]*a.x + d[1]*c.x)
					q[k].y += step * (d[0]*a.y + d[1]*c.y)
					if v := score(q); v > best {
						best, bestQ = v, q
						improved = true
					}
				}
			}
			if !improved {
				break
			}
		}
	}
	return transform(bestQ), bestQ, centers(bestQ)
}
