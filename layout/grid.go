package layout

// Grid, following CSS Grid Layout: placement (§8) and the track sizing
// algorithm (§12). Not supported: named lines and areas, subgrid,
// masonry, baseline alignment of grid items.

type track struct {
	minB, maxB Breadth
	base       float32
	limit      float32
	planned    float32
	offset     float32
	collapsed  bool // an empty auto-fit track
	autoFit    bool
}

func (t *track) isFlex() bool { return t.maxB.Kind == BreadthFr }

func (t *track) minIntrinsic() bool {
	switch t.minB.Kind {
	case BreadthAuto, BreadthMinContent, BreadthMaxContent:
		return true
	}
	return false
}

func (t *track) maxIntrinsic() bool {
	switch t.maxB.Kind {
	case BreadthAuto, BreadthMinContent, BreadthMaxContent, BreadthFitContent:
		return true
	}
	return false
}

type gridItem struct {
	node     *Node
	span     [2][2]int // per axis: [start, end) track indices
	margin   Rect[float32]
	autoM    Rect[bool]
	size     Size
	min, max Size
	pb       Size
	align    [2]Align // justify-self, align-self
	flex     [2]bool  // crosses a flexible track
	minC     [2]float32
	maxC     [2]float32
	minimum  [2]float32 // minimum contribution
	area     [2][2]float32
	placed   bool
}

func (it *gridItem) spanLen(a int) int { return it.span[a][1] - it.span[a][0] }

// expandTracks returns how many explicit tracks a template makes, given
// the inner size (NaN when indefinite) for auto-repeat.
func expandCount(tpl []GridTrack, inner, gap float32) (count, reps int) {
	fixed := float32(0)
	n := 0
	var auto *GridTrack
	for i := range tpl {
		t := &tpl[i]
		if t.Count < 0 {
			auto = t
			continue
		}
		c := max(t.Count, 1)
		for range c {
			for _, s := range t.Sizes {
				fixed += fixedBreadth(s, inner)
				n++
			}
		}
	}
	if auto == nil {
		return n, 0
	}
	reps = 1
	if isDef(inner) && len(auto.Sizes) > 0 {
		per := float32(0)
		ok := true
		for _, s := range auto.Sizes {
			v := fixedBreadth(s, inner)
			if v <= 0 && !isFixed(s.Max, inner) && !isFixed(s.Min, inner) {
				ok = false
			}
			per += v
		}
		if ok {
			per += gap * float32(len(auto.Sizes))
			free := inner - fixed - gap*float32(n)
			if n == 0 {
				free += gap
			}
			if per > 0 {
				r := int((free + 0.0001) / per)
				if n == 0 && r == 0 {
					r = 1
				}
				reps = max(1, r)
			}
		}
	}
	return n + reps*len(auto.Sizes), reps
}

func isFixed(b Breadth, basis float32) bool {
	return b.Kind == BreadthPoint || (b.Kind == BreadthPercent && isDef(basis))
}

func fixedBreadth(s TrackSize, basis float32) float32 {
	if isFixed(s.Max, basis) {
		v := resolveBreadth(s.Max, basis)
		if isFixed(s.Min, basis) {
			v = maxf(v, resolveBreadth(s.Min, basis))
		}
		return v
	}
	if isFixed(s.Min, basis) {
		return resolveBreadth(s.Min, basis)
	}
	return 0
}

func resolveBreadth(b Breadth, basis float32) float32 {
	switch b.Kind {
	case BreadthPoint:
		return b.V
	case BreadthPercent, BreadthFitContentPercent:
		return b.V * basis / 100
	case BreadthFitContent:
		return b.V
	}
	return 0
}

// resolveLine turns a grid line into a zero-based line, relative to the
// start of the explicit grid.
func resolveLine(l GridLine, explicit int) int {
	v := explicit + 1 + int(l.N)
	if l.N > 0 {
		v = int(l.N) - 1
	}
	return clampLine(v)
}

// Browsers limit grids to lines within ±maxLines.
const maxLines = 10000

func clampLine(v int) int { return min(max(v, -maxLines), maxLines) }

// placement resolves one axis: (start, end) when definite, or the span.
func placement(pl Placement, explicit int) (start, end int, definite bool, span int) {
	s, e := pl.Start, pl.End
	if s.Kind == LineIndex && s.N == 0 {
		s = GridLine{}
	}
	if e.Kind == LineIndex && e.N == 0 {
		e = GridLine{}
	}
	spanOf := func(l GridLine) int {
		if l.Kind == LineSpan && l.N > 0 {
			return min(int(l.N), maxLines)
		}
		return 1
	}
	switch {
	case s.Kind == LineIndex && e.Kind == LineIndex:
		a, b := resolveLine(s, explicit), resolveLine(e, explicit)
		if b < a {
			a, b = b, a
		}
		if a == b {
			if a == maxLines {
				a--
			}
			b = a + 1
		}
		return a, b, true, b - a
	case s.Kind == LineIndex:
		a := min(resolveLine(s, explicit), maxLines-1)
		return a, min(a+spanOf(e), maxLines), true, spanOf(e)
	case e.Kind == LineIndex:
		b := max(resolveLine(e, explicit), -maxLines+1)
		return max(b-spanOf(s), -maxLines), b, true, spanOf(s)
	}
	return 0, 0, false, spanOf(s)
}

func (n *Node) grid(in *input, p *Pass) output {
	st := &n.Style
	pad, bor := n.paddingBorder(in.parent.W)
	gutter := n.scrollGutter()
	inset := Rect[float32]{pad.Top + bor.Top, pad.Right + bor.Right + gutter.W, pad.Bottom + bor.Bottom + gutter.H, pad.Left + bor.Left}
	pbs := sumBoth(inset)
	margin := n.margins(in.parent.W)

	size := in.known
	minS, maxS := Size{nan, nan}, Size{nan, nan}
	if in.sizing == sizeInherent {
		ss := n.styleSize(in.parent)
		size = Size{orF(size.W, ss.W), orF(size.H, ss.H)}
		minS, maxS = n.styleMin(in.parent), n.styleMax(in.parent)
	}
	size.W, size.H = clampN(size.W, minS.W, maxS.W), clampN(size.H, minS.H, maxS.H)
	if in.mode == modeSize && isDef(size.W) && isDef(size.H) {
		return output{size: Size{maxf(size.W, pbs.W), maxf(size.H, pbs.H)}, baseline: nan}
	}
	inner := Size{nan, nan}
	var avail Size
	for a := 0; a < 2; a++ {
		if s := size.get(a); isDef(s) {
			inner.set(a, maxf(0, s-pbs.get(a)))
		}
		v := in.avail.get(a)
		if isDef(inner.get(a)) {
			v = inner.get(a)
		} else if definite(v) {
			v = maxf(0, clampN(v-sumAxis(margin, a), minS.get(a), maxS.get(a))-pbs.get(a))
		}
		avail.set(a, v)
	}
	// For auto-repeat, an indefinite size falls back to max (then min).
	repeatBasis := inner
	for a := 0; a < 2; a++ {
		if isNaN(repeatBasis.get(a)) {
			if v := maxS.get(a); isDef(v) {
				repeatBasis.set(a, maxf(0, v-pbs.get(a)))
			} else if v := minS.get(a); isDef(v) {
				repeatBasis.set(a, maxf(0, v-pbs.get(a)))
			}
		}
	}
	gaps := [2]float32{or0(st.ColumnGap.resolve(inner.W)), or0(st.RowGap.resolve(inner.H))}
	templates := [2][]GridTrack{st.GridTemplateColumns, st.GridTemplateRows}
	autos := [2][]TrackSize{st.GridAutoColumns, st.GridAutoRows}
	var explicit, reps [2]int
	for a := 0; a < 2; a++ {
		explicit[a], reps[a] = expandCount(templates[a], repeatBasis.get(a), gaps[a])
	}

	// Items and placement.
	ibase := len(p.gridItems)
	tbase := len(p.tracks)
	obase := len(p.occupied)
	defer func() {
		clear(p.gridItems[ibase:])
		p.gridItems = p.gridItems[:ibase]
		p.tracks = p.tracks[:tbase]
		p.occupied = p.occupied[:obase]
	}()
	lo, hi := [2]int{0, 0}, explicit
	maxSpan := [2]int{1, 1}
	for _, ch := range n.children {
		cs := &ch.Style
		if cs.Display == DisplayNone {
			if in.mode == modeLayout {
				p.place(ch, 0, 0, Size{})
				ch.hide(p)
			}
			continue
		}
		if cs.Position == Absolute {
			continue
		}
		it := gridItem{node: ch}
		pls := [2]Placement{cs.GridColumn, cs.GridRow}
		for a := 0; a < 2; a++ {
			s, e, def, span := placement(pls[a], explicit[a])
			if def {
				it.span[a] = [2]int{s, e}
				lo[a], hi[a] = min(lo[a], s), max(hi[a], e)
			} else {
				it.span[a] = [2]int{-1 << 30, span}
				maxSpan[a] = max(maxSpan[a], span)
			}
		}
		p.gridItems = append(p.gridItems, it)
	}
	items := p.gridItems[ibase:]
	flowCol := st.GridAutoFlow.isColumn()
	dense := st.GridAutoFlow.isDense()
	pa, sa := 0, 1 // the cursor walks the primary axis; the secondary grows
	if flowCol {
		pa, sa = 1, 0
	}
	// Items locked in the secondary axis or fully auto also need the
	// primary axis to fit their span.
	hi[pa] = max(hi[pa], lo[pa]+maxSpan[pa])
	off := [2]int{-lo[0], -lo[1]}
	for i := range items {
		for a := 0; a < 2; a++ {
			if items[i].span[a][0] != -1<<30 {
				items[i].span[a][0] += off[a]
				items[i].span[a][1] += off[a]
			}
		}
	}
	cols := hi[pa] - lo[pa] // the primary axis's track count is fixed
	// Placed areas, checked for overlap (sparse: grids may be huge).
	fits := func(r, c, rs, cs int) bool {
		if c+cs > cols {
			return false
		}
		for _, o := range p.occupied[obase:] {
			if r < o.r1 && o.r0 < r+rs && c < o.c1 && o.c0 < c+cs {
				return false
			}
		}
		return true
	}
	mark := func(it *gridItem) {
		p.occupied = append(p.occupied, area{it.span[sa][0], it.span[sa][1], it.span[pa][0], it.span[pa][1]})
		it.placed = true
	}
	auto := func(it *gridItem, a int) bool { return it.span[a][0] == -1<<30 }
	// 1. Fully definite items.
	for i := range items {
		if !auto(&items[i], 0) && !auto(&items[i], 1) {
			mark(&items[i])
		}
	}
	// 2. Items locked to a secondary track.
	cursorFor := map[int]int(nil)
	for i := range items {
		it := &items[i]
		if it.placed || auto(it, sa) || !auto(it, pa) {
			continue
		}
		span := it.span[pa][1]
		r := it.span[sa][0]
		rs := it.spanLen(sa)
		start := 0
		if !dense {
			if cursorFor == nil {
				cursorFor = map[int]int{}
			}
			start = cursorFor[r]
		}
		c := start
		for !fits(r, c, rs, span) {
			c++
			if c+span > cols {
				// Doesn't fit: extend the implicit primary axis would be
				// needed; place at the end.
				break
			}
		}
		it.span[pa] = [2]int{c, c + span}
		if c+span > cols {
			it.span[pa] = [2]int{0, span}
		}
		mark(it)
		if !dense {
			cursorFor[r] = it.span[pa][1]
		}
	}
	// 4. The rest.
	cr, cc := 0, 0
	for i := range items {
		it := &items[i]
		if it.placed {
			continue
		}
		if dense {
			cr, cc = 0, 0
		}
		if !auto(it, pa) {
			c := it.span[pa][0]
			span := it.spanLen(pa)
			if !dense && c < cc {
				cr++
			}
			r := cr
			rs := it.span[sa][1]
			for !fits(r, c, rs, span) {
				r++
			}
			it.span[sa] = [2]int{r, r + rs}
			cr, cc = r, c
			mark(it)
			continue
		}
		span := min(it.span[pa][1], cols)
		rs := it.span[sa][1]
		r, c := cr, cc
		for {
			if c+span > cols {
				r, c = r+1, 0
				continue
			}
			if fits(r, c, rs, span) {
				break
			}
			c++
		}
		it.span[pa] = [2]int{c, c + span}
		it.span[sa] = [2]int{r, r + rs}
		mark(it)
		cr, cc = r, c+span
	}
	counts := [2]int{}
	counts[pa] = cols
	counts[sa] = hi[sa] - lo[sa]
	for i := range items {
		counts[sa] = max(counts[sa], items[i].span[sa][1])
	}

	// Tracks.
	var tracks [2][]track
	for a := 0; a < 2; a++ {
		start := len(p.tracks)
		before := off[a]
		for i := 0; i < counts[a]; i++ {
			p.tracks = append(p.tracks, track{})
		}
		ts := p.tracks[start:]
		// Explicit tracks.
		k := before
		for ti := range templates[a] {
			t := &templates[a][ti]
			c := t.Count
			autoFit := c == RepeatAutoFit
			if c < 0 {
				c = reps[a]
			}
			c = max(c, 1)
			for range c {
				for _, s := range t.Sizes {
					if k < len(ts) {
						ts[k].minB, ts[k].maxB, ts[k].autoFit = s.Min, s.Max, autoFit
					}
					k++
				}
			}
		}
		// Implicit tracks cycle through grid-auto-rows/columns.
		al := autos[a]
		for i := range ts {
			if i >= before && i < before+explicit[a] {
				continue
			}
			s := TrackSize{}
			if len(al) > 0 {
				if i >= before+explicit[a] {
					s = al[(i-before-explicit[a])%len(al)]
				} else {
					j := (before - i) % len(al)
					s = al[(len(al)-j)%len(al)]
				}
			}
			ts[i].minB, ts[i].maxB = s.Min, s.Max
		}
		tracks[a] = ts
	}
	// Collapse empty auto-fit tracks.
	for a := 0; a < 2; a++ {
		for ti := range tracks[a] {
			t := &tracks[a][ti]
			if !t.autoFit {
				continue
			}
			used := false
			for i := range items {
				if items[i].span[a][0] <= ti && ti < items[i].span[a][1] {
					used = true
					break
				}
			}
			t.collapsed = !used
		}
	}

	// Item styles.
	for i := range items {
		it := &items[i]
		ch := it.node
		cs := &ch.Style
		it.size = ch.styleSize(inner)
		it.min, it.max = ch.styleMin(inner), ch.styleMax(inner)
		it.margin = ch.margins(inner.W)
		it.autoM = Rect[bool]{cs.Margin.Top.IsAuto(), cs.Margin.Right.IsAuto(), cs.Margin.Bottom.IsAuto(), cs.Margin.Left.IsAuto()}
		cpad, cbor := ch.paddingBorder(inner.W)
		it.pb = Size{cpad.Left + cpad.Right + cbor.Left + cbor.Right, cpad.Top + cpad.Bottom + cbor.Top + cbor.Bottom}
		it.align[0] = cs.JustifySelf
		if it.align[0] == AlignAuto {
			it.align[0] = st.JustifyItems
		}
		it.align[1] = cs.AlignSelf
		if it.align[1] == AlignAuto {
			it.align[1] = st.AlignItems
		}
		for a := 0; a < 2; a++ {
			it.minC[a], it.maxC[a], it.minimum[a] = nan, nan, nan
			for ti := it.span[a][0]; ti < it.span[a][1]; ti++ {
				if tracks[a][ti].isFlex() {
					it.flex[a] = true
				}
			}
		}
	}

	// Size columns, then rows.
	for a := 0; a < 2; a++ {
		n.sizeTracks(a, tracks[a], items, tracks, inner, avail, gaps[a], p)
	}

	// The container's size.
	content := Size{}
	for a := 0; a < 2; a++ {
		content.set(a, tracksTotal(tracks[a], gaps[a]))
	}
	outSize := size
	for a := 0; a < 2; a++ {
		if isNaN(outSize.get(a)) {
			v := clampN(content.get(a)+pbs.get(a), minS.get(a), maxS.get(a))
			outSize.set(a, v)
		}
		outSize.set(a, maxf(outSize.get(a), pbs.get(a)))
	}
	if in.mode == modeSize {
		return output{size: outSize, baseline: nan}
	}
	innerFinal := Size{outSize.W - pbs.W, outSize.H - pbs.H}

	// Align the tracks (justify-content, align-content).
	contents := [2]Content{st.JustifyContent, st.AlignContent}
	for a := 0; a < 2; a++ {
		ts := tracks[a]
		free := innerFinal.get(a) - content.get(a)
		visible := 0
		for i := range ts {
			if !ts[i].collapsed {
				visible++
			}
		}
		lead, between := distribute(contents[a], free, visible, true)
		pos := inset.start(a) + lead
		first := true
		for i := range ts {
			t := &ts[i]
			if t.collapsed {
				t.offset = pos
				continue
			}
			if !first {
				pos += gaps[a] + between
			}
			first = false
			t.offset = pos
			pos += t.base
		}
	}

	// Lay out the items in their areas.
	baseline := float32(nan)
	for i := range items {
		it := &items[i]
		var areaPos, areaSize [2]float32
		for a := 0; a < 2; a++ {
			ts := tracks[a]
			s, e := it.span[a][0], it.span[a][1]-1
			areaPos[a] = ts[s].offset
			areaSize[a] = ts[e].offset + ts[e].base - ts[s].offset
		}
		known := Size{nan, nan}
		var pos [2]float32
		final := n.gridItemSize(it, areaSize, inner, p)
		for a := 0; a < 2; a++ {
			sz := final.get(a)
			free := areaSize[a] - sz - sumAxis(it.margin, a)
			o := it.margin.start(a)
			as, ae := it.autoM.start(a), it.autoM.end(a)
			switch {
			case as && ae:
				o += free / 2
			case as:
				o += free
			case ae:
			default:
				switch it.align[a] {
				case AlignCenter:
					o += free / 2
				case AlignEnd, AlignFlexEnd:
					o += free
				}
			}
			pos[a] = areaPos[a] + o
			known.set(a, sz)
		}
		out := it.node.compute(input{mode: modeLayout, known: known, parent: Size{areaSize[0], areaSize[1]}, avail: Size{areaSize[0], areaSize[1]}}, p)
		px, py := relativeOffset(it.node, pos[0], pos[1], Size{areaSize[0], areaSize[1]})
		p.place(it.node, px, py, out.size)
		if isNaN(baseline) && it.span[1][0] == 0 {
			bl := out.baseline
			if isNaN(bl) {
				bl = out.size.H
			}
			baseline = py + bl
		}
	}

	// Absolutely positioned children: their grid area, or the padding box.
	for _, ch := range n.children {
		cs := &ch.Style
		if cs.Position != Absolute || cs.Display == DisplayNone {
			continue
		}
		ax, ay := bor.Left, bor.Top
		aw, ah := outSize.W-bor.Left-bor.Right-gutter.W, outSize.H-bor.Top-bor.Bottom-gutter.H
		area := [2][2]float32{{ax, ax + aw}, {ay, ay + ah}}
		pls := [2]Placement{cs.GridColumn, cs.GridRow}
		for a := 0; a < 2; a++ {
			ts := tracks[a]
			lineAt := func(l GridLine) (float32, bool) {
				if l.Kind != LineIndex || l.N == 0 {
					return 0, false
				}
				k := resolveLine(l, explicit[a]) + off[a]
				switch {
				case k < 0 || k > len(ts):
					return 0, false
				case k == len(ts):
					if k == 0 {
						return 0, false
					}
					return ts[k-1].offset + ts[k-1].base, true
				}
				return ts[k].offset, true
			}
			if v, ok := lineAt(pls[a].Start); ok {
				area[a][0] = v
			}
			if v, ok := lineAt(pls[a].End); ok {
				area[a][1] = v
			}
		}
		p.placeAbsoluteAligned(ch, area[0][0], area[1][0], area[0][1]-area[0][0], area[1][1]-area[1][0],
			area[0][0], area[1][0], area[0][1]-area[0][0], area[1][1]-area[1][0], [2]float32{})
	}
	return output{size: outSize, baseline: baseline}
}

func tracksTotal(ts []track, gap float32) float32 {
	s := float32(0)
	visible := 0
	for i := range ts {
		if ts[i].collapsed {
			continue
		}
		s += ts[i].base
		visible++
	}
	if visible > 1 {
		s += gap * float32(visible-1)
	}
	return s
}

// gridItemSize is an item's border-box size in its area: stretched, or
// fit to its content.
func (n *Node) gridItemSize(it *gridItem, area [2]float32, inner Size, p *Pass) Size {
	sz := Size{it.size.W, it.size.H}
	ratio := it.node.Style.AspectRatio
	for a := 0; a < 2; a++ {
		if isDef(sz.get(a)) {
			continue
		}
		al := it.align[a]
		stretch := al == AlignStretch || (al == AlignAuto && ratio <= 0)
		if stretch && !it.autoM.start(a) && !it.autoM.end(a) {
			sz.set(a, clampN(area[a]-sumAxis(it.margin, a), it.min.get(a), it.max.get(a)))
		}
	}
	sz = applyAspect(sz, ratio)
	if isNaN(sz.W) || isNaN(sz.H) {
		m := it.node.measureSize(input{known: sz, parent: Size{area[0], area[1]}, avail: Size{area[0] - sumAxis(it.margin, 0), area[1] - sumAxis(it.margin, 1)}}, p)
		if isNaN(sz.W) {
			sz.W = clampN(m.W, it.min.W, it.max.W)
		}
		if isNaN(sz.H) {
			sz.H = clampN(m.H, it.min.H, it.max.H)
		}
	}
	sz.W, sz.H = maxf(sz.W, it.pb.W), maxf(sz.H, it.pb.H)
	return sz
}

// contribution measures an item's outer size along axis a under the
// given available space (min- or max-content), knowing the other axis's
// track sizes when they are sized.
func (n *Node) contribution(it *gridItem, a int, space float32, tracks [2][]track, inner Size, gaps [2]float32, p *Pass) float32 {
	o := 1 - a
	known := Size{nan, nan}
	if v := it.size.get(a); isDef(v) {
		return maxf(clampN(v, it.min.get(a), it.max.get(a)), it.pb.get(a)) + sumAxis(it.margin, a)
	}
	ca := Size{}
	ca.set(a, space)
	other := float32(nan)
	if a == 1 { // columns are sized: the item's width follows from its area
		other = 0
		for ti := it.span[0][0]; ti < it.span[0][1]; ti++ {
			other += tracks[0][ti].base
		}
		other += gaps[0] * float32(it.spanLen(0)-1)
		w := it.size.W
		if isNaN(w) && it.align[0] != AlignStart && it.align[0] != AlignEnd && it.align[0] != AlignCenter && !it.autoM.Left && !it.autoM.Right {
			w = clampN(other-sumAxis(it.margin, 0), it.min.W, it.max.W)
		}
		known.W = w
		ca.W = maxf(0, other-sumAxis(it.margin, 0))
	} else {
		known.set(o, it.size.get(o))
		ca.set(o, MaxContentSpace)
		if isDef(inner.get(o)) {
			ca.set(o, inner.get(o))
		}
	}
	known = applyAspect(known, it.node.Style.AspectRatio)
	v := it.node.measureSize(input{known: known, parent: inner, avail: ca}, p).get(a)
	v = clampN(v, it.min.get(a), it.max.get(a))
	return maxf(v, it.pb.get(a)) + sumAxis(it.margin, a)
}

func (n *Node) itemMinContent(it *gridItem, a int, tracks [2][]track, inner Size, gaps [2]float32, p *Pass) float32 {
	if isNaN(it.minC[a]) {
		it.minC[a] = n.contribution(it, a, MinContentSpace, tracks, inner, gaps, p)
	}
	return it.minC[a]
}

func (n *Node) itemMaxContent(it *gridItem, a int, tracks [2][]track, inner Size, gaps [2]float32, p *Pass) float32 {
	if isNaN(it.maxC[a]) {
		it.maxC[a] = n.contribution(it, a, MaxContentSpace, tracks, inner, gaps, p)
	}
	return it.maxC[a]
}

// itemMinimum is the minimum contribution: the outer min-size, or for an
// auto min-size the automatic minimum (min-content, or 0 for scroll
// containers), capped when every spanned track has a fixed max.
func (n *Node) itemMinimum(it *gridItem, a int, ts []track, tracks [2][]track, inner Size, gaps [2]float32, p *Pass) float32 {
	if isDef(it.minimum[a]) {
		return it.minimum[a]
	}
	var v float32
	switch {
	case isDef(it.min.get(a)):
		v = it.min.get(a) + sumAxis(it.margin, a)
	case it.node.isScrollContainer():
		v = it.pb.get(a) + sumAxis(it.margin, a)
	default:
		v = n.itemMinContent(it, a, tracks, inner, gaps, p)
		allFixed := true
		limit := gaps[a] * float32(it.spanLen(a)-1)
		for ti := it.span[a][0]; ti < it.span[a][1]; ti++ {
			t := &ts[ti]
			if t.maxB.Kind != BreadthPoint && !(t.maxB.Kind == BreadthPercent && isDef(inner.get(a))) {
				allFixed = false
				break
			}
			limit += resolveBreadth(t.maxB, inner.get(a))
		}
		if allFixed && it.size.get(a) != it.size.get(a) {
			v = minf(v, maxf(limit, it.pb.get(a)+sumAxis(it.margin, a)))
		}
	}
	it.minimum[a] = v
	return v
}

// sizeTracks runs the track sizing algorithm on axis a.
func (n *Node) sizeTracks(a int, ts []track, items []gridItem, tracks [2][]track, inner, avail Size, gap float32, p *Pass) {
	basis := inner.get(a)
	gaps := [2]float32{or0(n.Style.ColumnGap.resolve(inner.W)), or0(n.Style.RowGap.resolve(inner.H))}
	space := avail.get(a)
	// §12.4 Initialize.
	for i := range ts {
		t := &ts[i]
		if t.minB.Kind == BreadthPercent && isNaN(basis) {
			t.minB = Breadth{}
		}
		if (t.maxB.Kind == BreadthPercent) && isNaN(basis) {
			t.maxB = Breadth{}
		}
		if t.maxB.Kind == BreadthFitContentPercent {
			if isNaN(basis) {
				t.maxB = Breadth{Kind: BreadthMaxContent}
			} else {
				t.maxB = Breadth{BreadthFitContent, t.maxB.V * basis / 100}
			}
		}
		if t.minB.Kind == BreadthFr { // fr isn't a valid minimum
			t.minB = Breadth{}
		}
		if t.collapsed {
			t.base, t.limit = 0, 0
			continue
		}
		t.base = 0
		if isFixed(t.minB, basis) {
			t.base = resolveBreadth(t.minB, basis)
		}
		t.limit = Inf
		if isFixed(t.maxB, basis) {
			t.limit = resolveBreadth(t.maxB, basis)
		}
		if t.limit < t.base {
			t.limit = t.base
		}
	}
	underMin := space == MinContentSpace
	underMax := space == MaxContentSpace

	// §12.5 Intrinsic sizes. Items spanning one non-flexible track:
	maxSpan := 1
	for i := range items {
		it := &items[i]
		l := it.spanLen(a)
		if l > maxSpan && !it.flex[a] {
			maxSpan = l
		}
		if l != 1 || it.flex[a] {
			continue
		}
		t := &ts[it.span[a][0]]
		if t.collapsed {
			continue
		}
		switch t.minB.Kind {
		case BreadthMinContent:
			t.base = maxf(t.base, n.itemMinContent(it, a, tracks, inner, gaps, p))
		case BreadthMaxContent:
			t.base = maxf(t.base, n.itemMaxContent(it, a, tracks, inner, gaps, p))
		case BreadthAuto:
			var v float32
			switch {
			case underMin:
				v = n.itemMinContent(it, a, tracks, inner, gaps, p)
				v = maxf(minf(v, limitOf(t)), n.itemMinimum(it, a, ts, tracks, inner, gaps, p))
			case underMax:
				v = n.itemMaxContent(it, a, tracks, inner, gaps, p)
				v = maxf(minf(v, limitOf(t)), n.itemMinimum(it, a, ts, tracks, inner, gaps, p))
			default:
				v = n.itemMinimum(it, a, tracks[a], tracks, inner, gaps, p)
			}
			t.base = maxf(t.base, v)
		}
		switch t.maxB.Kind {
		case BreadthMinContent:
			t.limit = growLimit(t.limit, n.itemMinContent(it, a, tracks, inner, gaps, p))
		case BreadthMaxContent, BreadthAuto:
			t.limit = growLimit(t.limit, n.itemMaxContent(it, a, tracks, inner, gaps, p))
		case BreadthFitContent:
			mc := n.itemMaxContent(it, a, tracks, inner, gaps, p)
			t.limit = growLimit(t.limit, minf(mc, maxf(t.maxB.V, n.itemMinContent(it, a, tracks, inner, gaps, p))))
		}
		if t.limit < t.base {
			t.limit = t.base
		}
	}
	for i := range ts { // fit-content limits growth to its argument
		if t := &ts[i]; t.maxB.Kind == BreadthFitContent && t.limit != Inf {
			t.limit = maxf(t.base, minf(t.limit, t.maxB.V))
		}
	}

	// Items spanning several tracks, by increasing span.
	for span := 2; span <= maxSpan; span++ {
		type phase struct {
			base    bool
			tracksF func(t *track) bool
			contrib func(it *gridItem) float32
		}
		phases := [...]phase{
			{true, func(t *track) bool { return t.minIntrinsic() }, func(it *gridItem) float32 {
				if underMin {
					return n.itemMinContent(it, a, tracks, inner, gaps, p)
				}
				if underMax {
					return n.itemMaxContent(it, a, tracks, inner, gaps, p)
				}
				return n.itemMinimum(it, a, ts, tracks, inner, gaps, p)
			}},
			{true, func(t *track) bool { return t.minB.Kind == BreadthMinContent || t.minB.Kind == BreadthMaxContent }, func(it *gridItem) float32 {
				return n.itemMinContent(it, a, tracks, inner, gaps, p)
			}},
			{true, func(t *track) bool {
				return t.minB.Kind == BreadthMaxContent || (underMax && t.minB.Kind == BreadthAuto)
			}, func(it *gridItem) float32 {
				return n.itemMaxContent(it, a, tracks, inner, gaps, p)
			}},
			{false, func(t *track) bool { return t.maxIntrinsic() }, func(it *gridItem) float32 {
				return n.itemMinContent(it, a, tracks, inner, gaps, p)
			}},
			{false, func(t *track) bool {
				return t.maxB.Kind == BreadthMaxContent || t.maxB.Kind == BreadthAuto || t.maxB.Kind == BreadthFitContent
			}, func(it *gridItem) float32 {
				return n.itemMaxContent(it, a, tracks, inner, gaps, p)
			}},
		}
		for _, ph := range phases {
			for i := range ts {
				ts[i].planned = 0
			}
			any := false
			for i := range items {
				it := &items[i]
				if it.spanLen(a) != span || it.flex[a] {
					continue
				}
				s, e := it.span[a][0], it.span[a][1]
				affected := 0
				used := gap * float32(span-1)
				for ti := s; ti < e; ti++ {
					t := &ts[ti]
					if ph.base {
						used += t.base
					} else {
						used += limitOrBase(t)
					}
					if ph.tracksF(t) && !t.collapsed {
						affected++
					}
				}
				if affected == 0 {
					continue
				}
				extra := ph.contrib(it) - used
				if extra <= 0 {
					continue
				}
				any = true
				distributeExtra(ts[s:e], extra, ph.base, ph.tracksF)
			}
			if !any {
				continue
			}
			for i := range ts {
				t := &ts[i]
				if t.planned == 0 {
					continue
				}
				if ph.base {
					t.base += t.planned
					if t.limit < t.base {
						t.limit = t.base
					}
				} else {
					t.limit = limitOrBase(t) + t.planned
				}
			}
		}
	}
	// Items crossing flexible tracks grow the flexible tracks' bases.
	for i := range ts {
		ts[i].planned = 0
	}
	for i := range items {
		it := &items[i]
		if !it.flex[a] {
			continue
		}
		s, e := it.span[a][0], it.span[a][1]
		used := gap * float32(e-s-1)
		frSum := float32(0)
		for ti := s; ti < e; ti++ {
			used += ts[ti].base
			if ts[ti].isFlex() {
				frSum += ts[ti].maxB.V
			}
		}
		var c float32
		switch {
		case underMin:
			c = n.itemMinContent(it, a, tracks, inner, gaps, p)
		case underMax:
			c = n.itemMaxContent(it, a, tracks, inner, gaps, p)
		default:
			c = n.itemMinimum(it, a, ts, tracks, inner, gaps, p)
		}
		extra := c - used
		if extra <= 0 {
			continue
		}
		for ti := s; ti < e; ti++ {
			t := &ts[ti]
			if !t.isFlex() || !t.minIntrinsic() {
				continue
			}
			share := extra / float32(countFlex(ts[s:e]))
			if frSum > 0 {
				share = extra * t.maxB.V / frSum
			}
			t.planned = maxf(t.planned, share)
		}
	}
	for i := range ts {
		t := &ts[i]
		t.base += t.planned
		if t.limit < t.base {
			t.limit = t.base
		}
	}
	for i := range ts {
		if ts[i].limit == Inf {
			ts[i].limit = ts[i].base
		}
	}

	// §12.6 Maximize tracks.
	free := float32(nan)
	if definite(space) {
		free = space - tracksTotal(ts, gap)
	}
	switch {
	case isDef(free) && free > 0:
		for range len(ts) + 1 {
			grow := 0
			for i := range ts {
				if ts[i].limit > ts[i].base && !ts[i].collapsed {
					grow++
				}
			}
			if grow == 0 || free <= 0.0001 {
				break
			}
			share := free / float32(grow)
			for i := range ts {
				t := &ts[i]
				if t.limit > t.base && !t.collapsed {
					inc := minf(share, t.limit-t.base)
					t.base += inc
					free -= inc
				}
			}
		}
	case underMax:
		for i := range ts {
			ts[i].base = ts[i].limit
		}
	}

	// §12.7 Expand flexible tracks.
	hasFlex := false
	for i := range ts {
		if ts[i].isFlex() && !ts[i].collapsed {
			hasFlex = true
			break
		}
	}
	if hasFlex {
		var fr float32
		if definite(space) {
			fr = findFr(ts, space-gapsTotal(ts, gap), -1, -1)
		} else if !underMin {
			for i := range ts {
				t := &ts[i]
				if t.isFlex() && !t.collapsed {
					f := t.maxB.V
					if f > 1 {
						fr = maxf(fr, t.base/f)
					} else {
						fr = maxf(fr, t.base)
					}
				}
			}
			for i := range items {
				it := &items[i]
				if !it.flex[a] {
					continue
				}
				s, e := it.span[a][0], it.span[a][1]
				c := n.itemMaxContent(it, a, tracks, inner, gaps, p)
				fr = maxf(fr, findFr(ts, c-gap*float32(e-s-1), s, e))
			}
		}
		for i := range ts {
			t := &ts[i]
			if t.isFlex() && !t.collapsed {
				t.base = maxf(t.base, fr*t.maxB.V)
			}
		}
	}

	// §12.8 Stretch auto tracks.
	c := n.Style.JustifyContent
	if a == 1 {
		c = n.Style.AlignContent
	}
	if (c == ContentNormal || c == ContentStretch) && definite(space) {
		free := space - tracksTotal(ts, gap)
		autos := 0
		for i := range ts {
			if ts[i].maxB.Kind == BreadthAuto && !ts[i].collapsed {
				autos++
			}
		}
		if free > 0 && autos > 0 {
			share := free / float32(autos)
			for i := range ts {
				if ts[i].maxB.Kind == BreadthAuto && !ts[i].collapsed {
					ts[i].base += share
				}
			}
		}
	}
}

func limitOf(t *track) float32 {
	if isFixed(t.maxB, nan) {
		return t.limit
	}
	return Inf
}

func limitOrBase(t *track) float32 {
	if t.limit == Inf {
		return t.base
	}
	return t.limit
}

func growLimit(limit, v float32) float32 {
	if limit == Inf {
		return v
	}
	return maxf(limit, v)
}

func countFlex(ts []track) int {
	c := 0
	for i := range ts {
		if ts[i].isFlex() {
			c++
		}
	}
	return max(c, 1)
}

func gapsTotal(ts []track, gap float32) float32 {
	v := 0
	for i := range ts {
		if !ts[i].collapsed {
			v++
		}
	}
	if v <= 1 {
		return 0
	}
	return gap * float32(v-1)
}

// distributeExtra spreads extra over the tracks f selects, equally, not
// past their growth limits for bases while any can still grow, and
// records the largest increase per track in planned.
func distributeExtra(ts []track, extra float32, base bool, f func(*track) bool) {
	var inc [64]float32
	var incs []float32
	if len(ts) <= len(inc) {
		incs = inc[:len(ts)]
	} else {
		incs = make([]float32, len(ts))
	}
	cur := func(t *track) float32 {
		if base {
			return t.base
		}
		return limitOrBase(t)
	}
	for range len(ts) + 1 {
		growable := 0
		for i := range ts {
			t := &ts[i]
			if !f(t) || t.collapsed {
				continue
			}
			if base && t.limit != Inf && cur(t)+incs[i] >= t.limit {
				continue
			}
			if !base && t.maxB.Kind == BreadthFitContent && cur(t)+incs[i] >= t.maxB.V {
				continue
			}
			growable++
		}
		if growable == 0 || extra <= 0.0001 {
			break
		}
		share := extra / float32(growable)
		for i := range ts {
			t := &ts[i]
			if !f(t) || t.collapsed {
				continue
			}
			room := Inf
			if base && t.limit != Inf {
				room = t.limit - cur(t) - incs[i]
			}
			if !base && t.maxB.Kind == BreadthFitContent {
				room = t.maxB.V - cur(t) - incs[i]
			}
			if room <= 0 {
				continue
			}
			d := minf(share, room)
			incs[i] += d
			extra -= d
		}
	}
	if extra > 0.0001 { // past the limits
		k := 0
		for i := range ts {
			t := &ts[i]
			if !f(t) || t.collapsed {
				continue
			}
			if base && !(t.maxB.Kind == BreadthMaxContent || t.maxB.Kind == BreadthAuto) {
				continue
			}
			k++
		}
		if k == 0 {
			for i := range ts {
				if f(&ts[i]) && !ts[i].collapsed {
					k++
				}
			}
		}
		for i := range ts {
			t := &ts[i]
			if !f(t) || t.collapsed {
				continue
			}
			if base && k > 0 && !(t.maxB.Kind == BreadthMaxContent || t.maxB.Kind == BreadthAuto) && hasMaxContentish(ts, f) {
				continue
			}
			incs[i] += extra / float32(k)
		}
	}
	for i := range ts {
		ts[i].planned = maxf(ts[i].planned, incs[i])
	}
}

func hasMaxContentish(ts []track, f func(*track) bool) bool {
	for i := range ts {
		if f(&ts[i]) && (ts[i].maxB.Kind == BreadthMaxContent || ts[i].maxB.Kind == BreadthAuto) {
			return true
		}
	}
	return false
}

// findFr finds the size of an fr given the space to fill (over the
// tracks [s, e), or all tracks when s < 0): tracks whose base is larger
// than their share count as inflexible.
func findFr(ts []track, space float32, s, e int) float32 {
	if s < 0 {
		s, e = 0, len(ts)
	}
	var inflex [64]bool
	var fl []bool
	if e-s <= len(inflex) {
		fl = inflex[:e-s]
	} else {
		fl = make([]bool, e-s)
	}
	for range e - s + 1 {
		left := space
		sum := float32(0)
		for i := s; i < e; i++ {
			t := &ts[i]
			if t.collapsed {
				continue
			}
			if t.isFlex() && !fl[i-s] {
				sum += t.maxB.V
			} else {
				left -= t.base
			}
		}
		if sum < 1 {
			sum = 1
		}
		fr := left / sum
		if fr < 0 {
			fr = 0
		}
		changed := false
		for i := s; i < e; i++ {
			t := &ts[i]
			if t.isFlex() && !fl[i-s] && !t.collapsed && fr*t.maxB.V < t.base {
				fl[i-s] = true
				changed = true
			}
		}
		if !changed {
			return fr
		}
	}
	return 0
}
