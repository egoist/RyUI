package barcode

import (
	"cmp"
	"image"
	"math"
	"slices"
)

// finder is a QR code's finder pattern found: its center, module size,
// and how many scans confirmed it.
type finder struct {
	x, y, size float64
	count      int
}

func (f *finder) near(size, y, x float64) bool {
	if math.Abs(y-f.y) <= size && math.Abs(x-f.x) <= size {
		d := math.Abs(size - f.size)
		return d <= 1 || d <= f.size
	}
	return false
}

func (f *finder) combine(y, x, size float64) {
	n := float64(f.count)
	f.x = (n*f.x + x) / (n + 1)
	f.y = (n*f.y + y) / (n + 1)
	f.size = (n*f.size + size) / (n + 1)
	f.count++
}

// qrFinder finds finder patterns: dark, light, dark, light, dark runs in
// the ratio 1:1:3:1:1 across rows, confirmed down the column, across the
// row again and along a diagonal, as ZXing's FinderPatternFinder does.
type qrFinder struct {
	b       *bitImage
	centers []finder
}

func foundCross(c *[5]int) bool {
	total := 0
	for _, n := range c {
		if n == 0 {
			return false
		}
		total += n
	}
	if total < 7 {
		return false
	}
	m := float64(total) / 7
	v := m / 2
	return math.Abs(m-float64(c[0])) < v && math.Abs(m-float64(c[1])) < v &&
		math.Abs(3*m-float64(c[2])) < 3*v && math.Abs(m-float64(c[3])) < v && math.Abs(m-float64(c[4])) < v
}

func foundDiagonal(c *[5]int) bool {
	total := 0
	for _, n := range c {
		if n == 0 {
			return false
		}
		total += n
	}
	if total < 7 {
		return false
	}
	m := float64(total) / 7
	v := m / 1.333
	return math.Abs(m-float64(c[0])) < v && math.Abs(m-float64(c[1])) < v &&
		math.Abs(3*m-float64(c[2])) < 3*v && math.Abs(m-float64(c[3])) < v && math.Abs(m-float64(c[4])) < v
}

func centerFromEnd(c *[5]int, end int) float64 {
	return float64(end-c[4]-c[3]) - float64(c[2])/2
}

func (f *qrFinder) find() {
	b := f.b
	skip := 2
	if b.h < 100 {
		skip = 1
	}
	var c [5]int
	for y := skip - 1; y < b.h; y += skip {
		c = [5]int{}
		state := 0
		row := b.pix[y*b.w : (y+1)*b.w]
		for x := 0; x < b.w; x++ {
			if row[x] != 0 {
				if state&1 == 1 {
					state++
				}
				c[state]++
				continue
			}
			if state&1 == 1 {
				c[state]++
				continue
			}
			if state != 4 {
				state++
				c[state]++
				continue
			}
			if foundCross(&c) && f.handle(&c, y, x) {
				c = [5]int{}
				state = 0
				continue
			}
			c = [5]int{c[2], c[3], c[4], 1, 0}
			state = 3
		}
		if foundCross(&c) {
			f.handle(&c, y, b.w)
		}
	}
}

func (f *qrFinder) handle(c *[5]int, y, x int) bool {
	total := c[0] + c[1] + c[2] + c[3] + c[4]
	cx := centerFromEnd(c, x)
	cy := f.crossCheck(int(cx), y, c[2], total, true)
	if math.IsNaN(cy) {
		return false
	}
	cx = f.crossCheck(int(cx), int(cy), c[2], total, false)
	if math.IsNaN(cx) || !f.crossDiagonal(int(cx), int(cy)) {
		return false
	}
	size := float64(total) / 7
	for i := range f.centers {
		if f.centers[i].near(size, cy, cx) {
			f.centers[i].combine(cy, cx, size)
			return true
		}
	}
	f.centers = append(f.centers, finder{cx, cy, size, 1})
	return true
}

// crossCheck counts the runs through (x, y) down the column (or along
// the row) and is the pattern's center on that line, or NaN.
func (f *qrFinder) crossCheck(x, y, maxCount, origTotal int, vertical bool) float64 {
	b := f.b
	at := func(i int) bool {
		if vertical {
			return b.at(x, i)
		}
		return b.at(i, y)
	}
	start, limit := x, b.w
	if vertical {
		start, limit = y, b.h
	}
	var c [5]int
	i := start
	for i >= 0 && at(i) {
		c[2]++
		i--
	}
	if i < 0 {
		return math.NaN()
	}
	for i >= 0 && !at(i) && c[1] <= maxCount {
		c[1]++
		i--
	}
	if i < 0 || c[1] > maxCount {
		return math.NaN()
	}
	for i >= 0 && at(i) && c[0] <= maxCount {
		c[0]++
		i--
	}
	if c[0] > maxCount {
		return math.NaN()
	}
	i = start + 1
	for i < limit && at(i) {
		c[2]++
		i++
	}
	if i == limit {
		return math.NaN()
	}
	for i < limit && !at(i) && c[3] < maxCount {
		c[3]++
		i++
	}
	if i == limit || c[3] >= maxCount {
		return math.NaN()
	}
	for i < limit && at(i) && c[4] < maxCount {
		c[4]++
		i++
	}
	if c[4] >= maxCount {
		return math.NaN()
	}
	total := c[0] + c[1] + c[2] + c[3] + c[4]
	if 5*abs(total-origTotal) >= 2*origTotal {
		return math.NaN()
	}
	if !foundCross(&c) {
		return math.NaN()
	}
	return centerFromEnd(&c, i)
}

func (f *qrFinder) crossDiagonal(x, y int) bool {
	b := f.b
	var c [5]int
	i := 0
	for y >= i && x >= i && b.at(x-i, y-i) {
		c[2]++
		i++
	}
	if c[2] == 0 {
		return false
	}
	for y >= i && x >= i && !b.at(x-i, y-i) {
		c[1]++
		i++
	}
	if c[1] == 0 {
		return false
	}
	for y >= i && x >= i && b.at(x-i, y-i) {
		c[0]++
		i++
	}
	if c[0] == 0 {
		return false
	}
	i = 1
	for y+i < b.h && x+i < b.w && b.at(x+i, y+i) {
		c[2]++
		i++
	}
	for y+i < b.h && x+i < b.w && !b.at(x+i, y+i) {
		c[3]++
		i++
	}
	if c[3] == 0 {
		return false
	}
	for y+i < b.h && x+i < b.w && b.at(x+i, y+i) {
		c[4]++
		i++
	}
	if c[4] == 0 {
		return false
	}
	return foundDiagonal(&c)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// orderFinders is three finders as bottom left, top left and top right.
func orderFinders(p [3]*finder) (bl, tl, tr *finder) {
	d := func(a, b *finder) float64 { return math.Hypot(a.x-b.x, a.y-b.y) }
	d01, d12, d02 := d(p[0], p[1]), d(p[1], p[2]), d(p[0], p[2])
	var a, b, c *finder
	switch {
	case d12 >= d01 && d12 >= d02:
		b, a, c = p[0], p[1], p[2]
	case d02 >= d12 && d02 >= d01:
		b, a, c = p[1], p[0], p[2]
	default:
		b, a, c = p[2], p[0], p[1]
	}
	if (c.x-b.x)*(a.y-b.y)-(c.y-b.y)*(a.x-b.x) < 0 {
		a, c = c, a
	}
	return a, b, c
}

// finderTriples is the sets of three finders that could be one code's,
// best first.
func finderTriples(cs []finder) [][3]int {
	var idx []int
	for i := range cs {
		if cs[i].count >= 2 {
			idx = append(idx, i)
		}
	}
	if len(idx) < 3 {
		// A small code may be confirmed once per finder.
		idx = idx[:0]
		for i := range cs {
			idx = append(idx, i)
		}
	}
	slices.SortFunc(idx, func(a, b int) int { return cmp.Compare(cs[a].size, cs[b].size) })
	type cand struct {
		t     [3]int
		score float64
	}
	var out []cand
	compatible := func(a, b float64) bool {
		return math.Abs(a-b) <= 0.5 || math.Abs(a-b)/math.Min(a, b) < 0.4
	}
	for i := 0; i < len(idx); i++ {
		p := &cs[idx[i]]
		for j := i + 1; j < len(idx); j++ {
			q := &cs[idx[j]]
			if !compatible(p.size, q.size) {
				break
			}
			for k := j + 1; k < len(idx); k++ {
				r := &cs[idx[k]]
				if !compatible(q.size, r.size) {
					break
				}
				bl, tl, tr := orderFinders([3]*finder{p, q, r})
				dA := math.Hypot(bl.x-tl.x, bl.y-tl.y)
				dB := math.Hypot(tr.x-tl.x, tr.y-tl.y)
				dC := math.Hypot(tr.x-bl.x, tr.y-bl.y)
				size := (p.size + q.size + r.size) / 3
				modules := (dA + dB) / (2 * size)
				if modules > 180 || modules < 9 {
					continue
				}
				vAB := math.Abs(dA-dB) / math.Min(dA, dB)
				if vAB >= 0.5 {
					continue
				}
				dCpy := math.Hypot(dA, dB)
				vC := math.Abs(dC-dCpy) / math.Min(dC, dCpy)
				if vC >= 0.25 {
					continue
				}
				out = append(out, cand{[3]int{idx[i], idx[j], idx[k]}, vAB + vC})
			}
		}
	}
	slices.SortStableFunc(out, func(a, b cand) int { return cmp.Compare(a.score, b.score) })
	ts := make([][3]int, len(out))
	for i, c := range out {
		ts[i] = c.t
	}
	return ts
}

// decodeQRCodes finds and reads the QR codes in b.
func decodeQRCodes(b *bitImage) []Code {
	f := &qrFinder{b: b}
	f.find()
	if len(f.centers) < 3 {
		return nil
	}
	used := make([]bool, len(f.centers))
	var codes []Code
	for _, t := range finderTriples(f.centers) {
		if used[t[0]] || used[t[1]] || used[t[2]] {
			continue
		}
		bl, tl, tr := orderFinders([3]*finder{&f.centers[t[0]], &f.centers[t[1]], &f.centers[t[2]]})
		text, corners, ok := qrDetectDecode(b, bl, tl, tr)
		if !ok {
			continue
		}
		used[t[0]], used[t[1]], used[t[2]] = true, true, true
		x0, y0, x1, y1 := boundsOf(corners[:]...)
		codes = append(codes, Code{QR, text, image.Rect(x0, y0, x1, y1)})
	}
	return codes
}

// qrDetectDecode reads the code whose finders are given: its text and
// corners.
func qrDetectDecode(b *bitImage, bl, tl, tr *finder) (string, [4]point, bool) {
	size := (moduleSizeOneWay(b, tl, tr) + moduleSizeOneWay(b, tl, bl)) / 2
	if size < 1 || math.IsNaN(size) {
		return "", [4]point{}, false
	}
	dA := math.Hypot(tl.x-tr.x, tl.y-tr.y) / size
	dB := math.Hypot(tl.x-bl.x, tl.y-bl.y) / size
	dim := int(math.Round(dA)+math.Round(dB))/2 + 7
	switch dim & 3 {
	case 0:
		dim++
	case 2:
		dim--
	case 3:
		dim -= 2
	}
	tried := map[int]bool{}
	try := []int{dim, dim + 4, dim - 4}
	for len(try) > 0 {
		d := try[0]
		try = try[1:]
		if d < 21 || d > 177 || tried[d] {
			continue
		}
		tried[d] = true
		v := (d - 17) / 4
		for _, t := range qrTransforms(b, bl, tl, tr, v, size) {
			g, ok := sampleGrid(b, &t, d, d)
			if !ok {
				continue
			}
			if v >= 7 {
				if rv := qrReadVersion(g); rv != 0 && rv != v {
					// The version information knows better.
					try = append([]int{qrSize(rv)}, try...)
					break
				}
			}
			text, ok := qrDecodeGrid(g, v)
			if !ok {
				// A mirrored code reads transposed.
				text, ok = qrDecodeGrid(transpose(g), v)
			}
			if ok {
				n := float64(d)
				var cs [4]point
				for i, c := range [4][2]float64{{0, 0}, {n, 0}, {n, n}, {0, n}} {
					cs[i].x, cs[i].y = t.apply(c[0], c[1])
				}
				return text, cs, true
			}
		}
	}
	return "", [4]point{}, false
}

func transpose(g *grid) *grid {
	t := newGrid(g.h, g.w)
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			t.bits[x*t.w+y] = g.bits[y*g.w+x]
		}
	}
	return t
}

// qrTransforms maps version v's modules to the image, through its
// finders and its bottom right alignment pattern if one is found (then
// without it, in case it was a look-alike).
func qrTransforms(b *bitImage, bl, tl, tr *finder, v int, size float64) []perspective {
	dim := float64(qrSize(v))
	brx := tr.x - tl.x + bl.x
	bry := tr.y - tl.y + bl.y
	m := dim - 3.5
	plain := quadToQuad(3.5, 3.5, m, 3.5, m, m, 3.5, m,
		tl.x, tl.y, tr.x, tr.y, brx, bry, bl.x, bl.y)
	if v < 2 {
		return []perspective{plain}
	}
	// Where the alignment pattern would be, and the module's axes there.
	ex, ey := plain.apply(dim-6.5, dim-6.5)
	x0, y0 := plain.apply(dim-6.5, dim-6.5)
	x1, y1 := plain.apply(dim-5.5, dim-6.5)
	x2, y2 := plain.apply(dim-6.5, dim-5.5)
	axes := [2]point{{x1 - x0, y1 - y0}, {x2 - x0, y2 - y0}}
	for allow := 4; allow <= 16; allow <<= 1 {
		if ax, ay, ok := findAlignment(b, size, ex, ey, float64(allow), axes); ok {
			t := quadToQuad(3.5, 3.5, m, 3.5, dim-6.5, dim-6.5, 3.5, m,
				tl.x, tl.y, tr.x, tr.y, ax, ay, bl.x, bl.y)
			return []perspective{t, plain}
		}
	}
	return []perspective{plain}
}

// moduleSizeOneWay estimates the module size from the finder a's run
// toward b and back.
func moduleSizeOneWay(img *bitImage, a, b *finder) float64 {
	x := runBothWays(img, int(a.x), int(a.y), int(b.x), int(b.y))
	y := runBothWays(img, int(b.x), int(b.y), int(a.x), int(a.y))
	switch {
	case math.IsNaN(x):
		return y / 7
	case math.IsNaN(y):
		return x / 7
	}
	return (x + y) / 14
}

func runBothWays(img *bitImage, fx, fy, tx, ty int) float64 {
	r := bwbRun(img, fx, fy, tx, ty)
	scale := 1.0
	ox := fx - (tx - fx)
	if ox < 0 {
		scale = float64(fx) / float64(fx-ox)
		ox = 0
	} else if ox >= img.w {
		scale = float64(img.w-1-fx) / float64(ox-fx)
		ox = img.w - 1
	}
	oy := int(float64(fy) - float64(ty-fy)*scale)
	scale = 1
	if oy < 0 {
		scale = float64(fy) / float64(fy-oy)
		oy = 0
	} else if oy >= img.h {
		scale = float64(img.h-1-fy) / float64(oy-fy)
		oy = img.h - 1
	}
	ox = int(float64(fx) + float64(ox-fx)*scale)
	r += bwbRun(img, fx, fy, ox, oy)
	return r - 1
}

// bwbRun is the length of the dark, light, dark runs from (fx, fy)
// toward (tx, ty).
func bwbRun(img *bitImage, fx, fy, tx, ty int) float64 {
	steep := abs(ty-fy) > abs(tx-fx)
	if steep {
		fx, fy = fy, fx
		tx, ty = ty, tx
	}
	dx, dy := abs(tx-fx), abs(ty-fy)
	e := -dx / 2
	xs, ys := 1, 1
	if fx > tx {
		xs = -1
	}
	if fy > ty {
		ys = -1
	}
	state := 0
	xl := tx + xs
	for x, y := fx, fy; x != xl; x += xs {
		rx, ry := x, y
		if steep {
			rx, ry = y, x
		}
		if (state == 1) == img.at(rx, ry) {
			if state == 2 {
				return math.Hypot(float64(x-fx), float64(y-fy))
			}
			state++
		}
		e += dy
		if e > 0 {
			if y == ty {
				break
			}
			y += ys
			e -= dx
		}
	}
	if state == 2 {
		return math.Hypot(float64(tx+xs-fx), float64(ty-fy))
	}
	return math.NaN()
}

// findAlignment looks for an alignment pattern (light, dark, light runs
// 1:1:1, inside a dark ring) within allow modules of (ex, ey): the one
// nearest it.
func findAlignment(b *bitImage, size float64, ex, ey float64, allow float64, axes [2]point) (float64, float64, bool) {
	a := int(allow * size)
	left := max(0, int(ex)-a)
	right := min(b.w-1, int(ex)+a)
	if float64(right-left) < size*3 {
		return 0, 0, false
	}
	top := max(0, int(ey)-a)
	bottom := min(b.h-1, int(ey)+a)
	if float64(bottom-top) < size*3 {
		return 0, 0, false
	}
	var cands []finder
	cross := func(c *[3]int) bool {
		for _, n := range c {
			if math.Abs(size-float64(n)) >= 0.6*size {
				return false
			}
		}
		return true
	}
	// ring reports whether (x, y) is the middle of the pattern: dark,
	// in light, in dark, along the code's axes.
	ring := func(x, y float64) bool {
		ok := 0
		for i := -2; i <= 2; i++ {
			for j := -2; j <= 2; j++ {
				if (i == 0 && j == 0) || (abs(i) == 2 || abs(j) == 2) && (i+j)%2 != 0 {
					// The center's checked by the runs; on the outer
					// ring, the corners and middles are enough.
					continue
				}
				px := x + float64(i)*axes[0].x + float64(j)*axes[1].x
				py := y + float64(i)*axes[0].y + float64(j)*axes[1].y
				dark := max(abs(i), abs(j)) == 2
				if b.at(int(math.Floor(px)), int(math.Floor(py))) == dark {
					ok++
				}
			}
		}
		return ok >= 15 // of 16
	}
	check := func(c *[3]int, y, end int) {
		total := c[0] + c[1] + c[2]
		cx := float64(end-c[2]) - float64(c[1])/2
		cy := alignCrossCheck(b, y, int(cx), 2*c[1], total, cross)
		if math.IsNaN(cy) || !ring(cx, cy) {
			return
		}
		s := float64(total) / 3
		for i := range cands {
			if cands[i].near(s, cy, cx) {
				cands[i].combine(cy, cx, s)
				return
			}
		}
		cands = append(cands, finder{cx, cy, s, 1})
	}
	for y := top; y <= bottom; y++ {
		var c [3]int
		x := left
		for x < right && !b.at(x, y) {
			x++
		}
		state := 0
		for ; x < right; x++ {
			if b.at(x, y) {
				if state == 1 {
					c[1]++
				} else if state == 2 {
					if cross(&c) {
						check(&c, y, x)
					}
					c = [3]int{c[2], 1, 0}
					state = 1
				} else {
					state++
					c[state]++
				}
			} else {
				if state == 1 {
					state++
				}
				c[state]++
			}
		}
		if cross(&c) {
			check(&c, y, right)
		}
	}
	best, bestD := -1, math.Inf(1)
	for i, c := range cands {
		if d := math.Hypot(c.x-ex, c.y-ey); d < bestD {
			best, bestD = i, d
		}
	}
	if best < 0 {
		return 0, 0, false
	}
	return cands[best].x, cands[best].y, true
}

func alignCrossCheck(b *bitImage, startY, x, maxCount, origTotal int, cross func(*[3]int) bool) float64 {
	var c [3]int
	y := startY
	for y >= 0 && b.at(x, y) && c[1] <= maxCount {
		c[1]++
		y--
	}
	if y < 0 || c[1] > maxCount {
		return math.NaN()
	}
	for y >= 0 && !b.at(x, y) && c[0] <= maxCount {
		c[0]++
		y--
	}
	if c[0] > maxCount {
		return math.NaN()
	}
	y = startY + 1
	for y < b.h && b.at(x, y) && c[1] <= maxCount {
		c[1]++
		y++
	}
	if y == b.h || c[1] > maxCount {
		return math.NaN()
	}
	for y < b.h && !b.at(x, y) && c[2] <= maxCount {
		c[2]++
		y++
	}
	if c[2] > maxCount {
		return math.NaN()
	}
	total := c[0] + c[1] + c[2]
	if 5*abs(total-origTotal) >= 2*origTotal {
		return math.NaN()
	}
	if !cross(&c) {
		return math.NaN()
	}
	return float64(y-c[2]) - float64(c[1])/2
}
