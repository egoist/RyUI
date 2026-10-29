package layout

// Flexbox, following CSS Flexible Box Layout §9.

type flexItem struct {
	node      *Node
	size      Size // style size, border box; NaN: auto
	min, max  Size
	margin    Rect[float32]
	autoM     Rect[bool]
	pb        Size
	align     Align
	grow      float32
	shrink    float32
	basis     float32 // flex base size, border box
	inner     float32 // basis minus padding and border
	minMain   float32 // resolved minimum main size; NaN until needed
	knownX    float32 // cross size known when measuring the content
	hypo      Size    // hypothetical border-box size
	target    float32 // main size being resolved
	frozen    bool
	violation float32
	cross     float32 // used cross size
	baseline  float32 // ascent from the top of the margin box (rows)
	posM      float32
	posX      float32
	content   bool // basis came from measuring content
}

type flexLine struct {
	start, end int
	cross      float32
	pos        float32
	ascent     float32
}

func (it *flexItem) outerMain(m int) float32 { return it.target + sumAxis(it.margin, m) }

func (n *Node) flex(in *input, p *Pass) output {
	st := &n.Style
	m := 1
	if st.FlexDirection.isRow() {
		m = 0
	}
	x := 1 - m
	reverse := st.FlexDirection.isReverse()
	wrap := st.FlexWrap != NoWrap
	wrapReverse := st.FlexWrap == WrapReverse

	pad, bor := n.paddingBorder(in.parent.W)
	gutter := n.scrollGutter()
	inset := Rect[float32]{pad.Top + bor.Top, pad.Right + bor.Right + gutter.W, pad.Bottom + bor.Bottom + gutter.H, pad.Left + bor.Left}
	pbs := sumBoth(inset)
	pbOnly := Size{pad.Left + pad.Right + bor.Left + bor.Right, pad.Top + pad.Bottom + bor.Top + bor.Bottom}
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
		return output{size: Size{maxf(size.W, pbOnly.W), maxf(size.H, pbOnly.H)}, baseline: nan}
	}

	innerSize := Size{maxf(0, size.W-pbs.W), maxf(0, size.H-pbs.H)} // NaN stays NaN
	if isNaN(size.W) {
		innerSize.W = nan
	}
	if isNaN(size.H) {
		innerSize.H = nan
	}
	var avail Size
	for a := 0; a < 2; a++ {
		v := in.avail.get(a)
		if isDef(innerSize.get(a)) {
			v = innerSize.get(a)
		} else if definite(v) {
			v = clampN(v-sumAxis(margin, a), minS.get(a), maxS.get(a)) - pbs.get(a)
			v = maxf(v, 0)
		}
		avail.set(a, v)
	}
	gaps := Size{or0(st.ColumnGap.resolve(innerSize.W)), or0(st.RowGap.resolve(innerSize.H))}
	gapM, gapX := gaps.get(m), gaps.get(x)

	// 1. Collect the items.
	base := len(p.flexItems)
	lineBase := len(p.flexLines)
	defer func() {
		clear(p.flexItems[base:])
		p.flexItems = p.flexItems[:base]
		p.flexLines = p.flexLines[:lineBase]
	}()
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
		it := flexItem{node: ch, minMain: nan, baseline: nan}
		it.size = ch.styleSize(innerSize)
		it.min, it.max = ch.styleMin(innerSize), ch.styleMax(innerSize)
		it.margin = ch.margins(innerSize.W)
		it.autoM = Rect[bool]{cs.Margin.Top.IsAuto(), cs.Margin.Right.IsAuto(), cs.Margin.Bottom.IsAuto(), cs.Margin.Left.IsAuto()}
		cpad, cbor := ch.paddingBorder(innerSize.W)
		it.pb = Size{cpad.Left + cpad.Right + cbor.Left + cbor.Right, cpad.Top + cpad.Bottom + cbor.Top + cbor.Bottom}
		it.align = cs.AlignSelf
		if it.align == AlignAuto {
			it.align = st.AlignItems
		}
		if it.align == AlignAuto {
			it.align = AlignStretch
		}
		it.grow, it.shrink = cs.FlexGrow, cs.FlexShrink
		p.flexItems = append(p.flexItems, it)
	}
	items := p.flexItems[base:]

	// 2. Flex base sizes and hypothetical main sizes (§9.2.3).
	for i := range items {
		it := &items[i]
		ch := it.node
		availX := avail.get(x)
		if definite(availX) {
			availX = clampN(availX, it.min.get(x)+sumAxis(it.margin, x), it.max.get(x)+sumAxis(it.margin, x))
		}
		it.knownX = it.size.get(x)
		if isNaN(it.knownX) && it.align == AlignStretch && definite(availX) && !it.autoM.start(x) && !it.autoM.end(x) {
			it.knownX = availX - sumAxis(it.margin, x)
		}
		b := ch.Style.FlexBasis.resolve(innerSize.get(m))
		if isDef(b) {
			b += ch.boxAdjust(innerSize.W).get(m)
		}
		ratio := ch.Style.AspectRatio
		switch {
		case isDef(b):
		case ratio > 0 && isDef(it.size.get(x)):
			if m == 0 {
				b = it.size.get(x) * ratio
			} else {
				b = it.size.get(x) / ratio
			}
		case isDef(it.size.get(m)):
			b = it.size.get(m)
		default:
			known := Size{nan, nan}
			known.set(x, it.knownX)
			ca := Size{}
			ca.set(m, MaxContentSpace)
			if in.avail.get(m) == MinContentSpace {
				ca.set(m, MinContentSpace)
			}
			ca.set(x, availX)
			b = ch.measureSize(input{sizing: sizeContent, known: known, parent: innerSize, avail: ca}, p).get(m)
			it.content = true
		}
		it.basis = maxf(b, it.pb.get(m))
		it.inner = maxf(0, it.basis-it.pb.get(m))
		h := it.basis
		if !it.content {
			h = maxf(h, it.minMainSize(m, x, innerSize, avail, p))
		}
		h = maybeMin(h, it.max.get(m))
		if isDef(it.min.get(m)) {
			h = maxf(h, it.min.get(m))
		}
		it.hypo.set(m, maxf(h, it.pb.get(m)))
	}

	// 3. Lines. A column without a definite height wraps only at its
	// max-height: its auto height is its max-content height.
	breakAt := avail.get(m)
	if m == 1 && isNaN(innerSize.H) {
		breakAt = MaxContentSpace
		if v := maxS.H; isDef(v) {
			breakAt = maxf(0, v-pbs.H)
		}
	}
	if !wrap || breakAt == MaxContentSpace {
		p.flexLines = append(p.flexLines, flexLine{start: 0, end: len(items)})
	} else {
		start, used := 0, float32(0)
		for i := range items {
			outer := items[i].hypo.get(m) + sumAxis(items[i].margin, m)
			if i > start && (breakAt == MinContentSpace || used+gapM+outer > breakAt+0.0001) {
				p.flexLines = append(p.flexLines, flexLine{start: start, end: i})
				start, used = i, outer
			} else if i > start {
				used += gapM + outer
			} else {
				used = outer
			}
		}
		p.flexLines = append(p.flexLines, flexLine{start: start, end: len(items)})
	}
	lines := p.flexLines[lineBase:]

	// 4. The container's inner main size.
	innerM := innerSize.get(m)
	if isNaN(innerM) {
		innerM = n.flexIntrinsicMain(items, lines, m, x, gapM, wrap, innerSize, avail, in, p)
		outer := clampN(innerM+pbs.get(m), minS.get(m), maxS.get(m))
		innerM = maxf(0, outer-pbs.get(m))
	}

	// 5. Resolve flexible lengths (§9.7).
	for li := range lines {
		resolveFlexible(items[lines[li].start:lines[li].end], innerM, gapM, m, x, innerSize, avail, p)
	}

	// 6. Hypothetical cross sizes.
	isBaselineRow := m == 0
	for i := range items {
		it := &items[i]
		known := Size{nan, nan}
		known.set(m, it.target)
		known.set(x, it.size.get(x))
		if r := it.node.Style.AspectRatio; r > 0 && isNaN(known.get(x)) {
			if m == 0 {
				known.set(x, clampN(it.target/r, it.min.get(x), it.max.get(x)))
			} else {
				known.set(x, clampN(it.target*r, it.min.get(x), it.max.get(x)))
			}
		}
		ca := Size{}
		ca.set(m, innerM)
		ca.set(x, avail.get(x))
		var cross float32
		if !wrap && isDef(innerSize.get(x)) && it.align == AlignStretch && isNaN(known.get(x)) && !it.autoM.start(x) && !it.autoM.end(x) {
			// Stretched to the line, which is the container's cross size:
			// no need to measure.
			cross = innerSize.get(x) - sumAxis(it.margin, x)
		} else if isBaselineRow && it.align == AlignBaseline {
			out := it.node.compute(input{mode: modeLayout, known: known, parent: innerSize, avail: ca}, p)
			cross = out.size.get(x)
			bl := out.baseline
			if isNaN(bl) {
				bl = out.size.H
			}
			it.baseline = bl + it.margin.Top
		} else {
			cross = it.node.measureSize(input{known: known, parent: innerSize, avail: ca}, p).get(x)
		}
		cross = clampN(cross, it.min.get(x), it.max.get(x))
		it.hypo.set(x, maxf(cross, it.pb.get(x)))
	}

	// 7. Line cross sizes.
	innerX := innerSize.get(x)
	for li := range lines {
		l := &lines[li]
		if !wrap && isDef(innerX) {
			l.cross = innerX
			continue
		}
		maxCross, maxAsc, maxDesc := float32(0), float32(0), float32(0)
		for i := l.start; i < l.end; i++ {
			it := &items[i]
			outer := it.hypo.get(x) + sumAxis(it.margin, x)
			if isBaselineRow && it.align == AlignBaseline && !it.autoM.Top && !it.autoM.Bottom {
				maxAsc = maxf(maxAsc, it.baseline)
				maxDesc = maxf(maxDesc, outer-it.baseline)
			} else {
				maxCross = maxf(maxCross, outer)
			}
		}
		l.ascent = maxAsc
		l.cross = maxf(maxCross, maxAsc+maxDesc)
		if !wrap {
			l.cross = clampN(l.cross+pbs.get(x), minS.get(x), maxS.get(x)) - pbs.get(x)
		}
	}
	for li := range lines { // baseline ascent for lines with a definite size
		if lines[li].ascent == 0 {
			for i := lines[li].start; i < lines[li].end; i++ {
				if items[i].align == AlignBaseline && isBaselineRow {
					lines[li].ascent = maxf(lines[li].ascent, items[i].baseline)
				}
			}
		}
	}

	// 8. The container's cross size, and align-content: stretch.
	totalX := float32(0)
	for li := range lines {
		totalX += lines[li].cross
	}
	totalX += gapX * float32(len(lines)-1)
	if isNaN(innerX) {
		innerX = maxf(0, clampN(totalX+pbs.get(x), minS.get(x), maxS.get(x))-pbs.get(x))
	}
	ac := st.AlignContent
	if wrap && (ac == ContentNormal || ac == ContentStretch) && innerX > totalX && len(lines) > 0 {
		extra := (innerX - totalX) / float32(len(lines))
		for li := range lines {
			lines[li].cross += extra
		}
	}

	// 9. Used cross sizes.
	for li := range lines {
		l := &lines[li]
		for i := l.start; i < l.end; i++ {
			it := &items[i]
			it.cross = it.hypo.get(x)
			if it.align == AlignStretch && isNaN(it.size.get(x)) && !it.autoM.start(x) && !it.autoM.end(x) {
				c := clampN(l.cross-sumAxis(it.margin, x), it.min.get(x), it.max.get(x))
				it.cross = maxf(c, it.pb.get(x))
			}
		}
	}

	outSize := Size{}
	outSize.set(m, innerM+pbs.get(m))
	outSize.set(x, innerX+pbs.get(x))
	outSize.W = maxf(outSize.W, pbOnly.W)
	outSize.H = maxf(outSize.H, pbOnly.H)
	if isDef(size.W) {
		outSize.W = maxf(size.W, pbOnly.W)
	}
	if isDef(size.H) {
		outSize.H = maxf(size.H, pbOnly.H)
	}
	if in.mode == modeSize {
		return output{size: outSize, baseline: nan}
	}

	// 10. Main-axis alignment.
	for li := range lines {
		l := &lines[li]
		line := items[l.start:l.end]
		used := gapM * float32(len(line)-1)
		autos := 0
		for i := range line {
			used += line[i].outerMain(m)
			if line[i].autoM.start(m) {
				autos++
			}
			if line[i].autoM.end(m) {
				autos++
			}
		}
		free := innerM - used
		lead, between := float32(0), gapM
		perAuto := float32(0)
		if autos > 0 && free > 0 {
			perAuto = free / float32(autos)
		} else if autos == 0 {
			jc := st.JustifyContent
			if reverse {
				switch jc {
				case ContentStart:
					jc = ContentFlexEnd
				case ContentEnd:
					jc = ContentFlexStart
				}
			}
			l2, b2 := distribute(jc, free, len(line), false)
			lead, between = l2, gapM+b2
		}
		pos := lead
		for i := range line {
			it := &line[i]
			if it.autoM.start(m) {
				pos += perAuto
			}
			pos += it.margin.start(m)
			it.posM = pos
			pos += it.target + it.margin.end(m)
			if it.autoM.end(m) {
				pos += perAuto
			}
			pos += between
		}
		if reverse {
			for i := range line {
				it := &line[i]
				it.posM = innerM - it.posM - it.target
			}
		}
	}

	// 11. Lines along the cross axis (align-content) and items in them.
	free := innerX - totalX
	if wrap && (ac == ContentNormal || ac == ContentStretch) && free > 0 {
		free = 0 // already given to the lines
	}
	lead, between := float32(0), gapX
	if wrap {
		ac2 := ac
		if wrapReverse {
			switch ac2 {
			case ContentFlexStart:
				ac2 = ContentFlexEnd
			case ContentFlexEnd:
				ac2 = ContentFlexStart
			}
		}
		l2, b2 := distribute(ac2, free, len(lines), true)
		lead, between = l2, gapX+b2
	}
	pos := lead
	for li := range lines {
		lines[li].pos = pos
		pos += lines[li].cross + between
	}
	if wrapReverse {
		for li := range lines {
			lines[li].pos = innerX - lines[li].pos - lines[li].cross
		}
	}
	containerBaseline := float32(nan)
	for li := range lines {
		l := &lines[li]
		for i := l.start; i < l.end; i++ {
			it := &items[i]
			outer := it.cross + sumAxis(it.margin, x)
			free := l.cross - outer
			off := it.margin.start(x)
			switch {
			case it.autoM.start(x) && it.autoM.end(x):
				off += maxf(0, free) / 2
			case it.autoM.start(x):
				off += maxf(0, free)
			case it.autoM.end(x):
			default:
				align := it.align
				if wrapReverse {
					switch align {
					case AlignFlexStart, AlignStretch:
						align = AlignFlexEnd
					case AlignFlexEnd:
						align = AlignFlexStart
					}
				}
				switch align {
				case AlignEnd, AlignFlexEnd:
					off += free
				case AlignCenter:
					off += free / 2
				case AlignBaseline:
					if isBaselineRow {
						off = l.ascent - it.baseline + it.margin.start(x)
					}
				}
			}
			it.posX = l.pos + off
		}
	}

	// 12. Final layout of each item.
	for i := range items {
		it := &items[i]
		known := Size{}
		known.set(m, it.target)
		known.set(x, it.cross)
		ca := Size{}
		ca.set(m, innerM)
		ca.set(x, innerX)
		out := it.node.compute(input{mode: modeLayout, known: known, parent: innerSize, avail: ca}, p)
		var px, py float32
		if m == 0 {
			px, py = inset.Left+it.posM, inset.Top+it.posX
		} else {
			px, py = inset.Left+it.posX, inset.Top+it.posM
		}
		px, py = relativeOffset(it.node, px, py, innerSize)
		p.place(it.node, px, py, out.size)
		if isNaN(containerBaseline) && i < lines[0].end {
			bl := out.baseline
			if isNaN(bl) {
				bl = out.size.H
			}
			if m == 1 || it.align == AlignBaseline || i == 0 {
				containerBaseline = py + bl
			}
		}
	}
	if len(items) == 0 {
		containerBaseline = nan
	}

	// 13. Absolutely positioned children.
	n.layoutAbsolute(outSize, bor, inset, m, reverse, p)
	return output{size: outSize, baseline: containerBaseline}
}

// relativeOffset applies a relatively positioned node's insets.
func relativeOffset(ch *Node, x, y float32, parent Size) (float32, float32) {
	ins := &ch.Style.Inset
	if ch.Style.Position != Relative || (ins.Left.IsAuto() && ins.Right.IsAuto() && ins.Top.IsAuto() && ins.Bottom.IsAuto()) {
		return x, y
	}
	if l := ins.Left.resolve(parent.W); isDef(l) {
		x += l
	} else if r := ins.Right.resolve(parent.W); isDef(r) {
		x -= r
	}
	if t := ins.Top.resolve(parent.H); isDef(t) {
		y += t
	} else if b := ins.Bottom.resolve(parent.H); isDef(b) {
		y -= b
	}
	return x, y
}

// layoutAbsolute places a flex container's absolutely positioned
// children: by their insets in the padding box, else where they would be
// as the container's only item.
func (n *Node) layoutAbsolute(size Size, bor, inset Rect[float32], m int, reverse bool, p *Pass) {
	st := &n.Style
	for _, ch := range n.children {
		if ch.Style.Position != Absolute || ch.Style.Display == DisplayNone {
			continue
		}
		aw := size.W - bor.Left - bor.Right - n.scrollGutter().W
		ah := size.H - bor.Top - bor.Bottom - n.scrollGutter().H
		cw, ch2 := size.W-inset.Left-inset.Right, size.H-inset.Top-inset.Bottom
		var frac [2]float32
		jc := st.JustifyContent
		fm := float32(0)
		switch jc {
		case ContentCenter, ContentSpaceAround, ContentSpaceEvenly:
			fm = 0.5
		case ContentEnd:
			fm = 1
		case ContentFlexEnd:
			fm = 1
			if reverse {
				fm = 0
			}
		case ContentFlexStart, ContentNormal, ContentStart, ContentStretch, ContentSpaceBetween:
			if reverse && jc != ContentStart {
				fm = 1
			}
		}
		align := ch.Style.AlignSelf
		if align == AlignAuto {
			align = st.AlignItems
		}
		fx := float32(0)
		switch align {
		case AlignCenter:
			fx = 0.5
		case AlignEnd:
			fx = 1
		case AlignFlexEnd:
			if st.FlexWrap != WrapReverse {
				fx = 1
			}
		case AlignFlexStart, AlignStretch, AlignAuto, AlignBaseline:
			if st.FlexWrap == WrapReverse {
				fx = 1
			}
		}
		frac[m], frac[1-m] = fm, fx
		p.placeAbsoluteAligned(ch, bor.Left, bor.Top, aw, ah, inset.Left, inset.Top, cw, ch2, frac)
	}
}

// distribute returns the leading space and the extra space between n
// things for a content distribution of free space. Negative free space
// falls back to start, as browsers do.
func distribute(c Content, free float32, n int, forLines bool) (lead, between float32) {
	if n == 0 {
		return 0, 0
	}
	if free < 0 {
		switch c {
		case ContentSpaceBetween:
			c = ContentStart
		case ContentSpaceAround, ContentSpaceEvenly, ContentStretch, ContentNormal:
			c = ContentStart
		}
	}
	switch c {
	case ContentCenter:
		return free / 2, 0
	case ContentEnd, ContentFlexEnd:
		return free, 0
	case ContentSpaceBetween:
		if n > 1 {
			return 0, free / float32(n-1)
		}
	case ContentSpaceAround:
		return free / float32(2*n), free / float32(n)
	case ContentSpaceEvenly:
		return free / float32(n+1), free / float32(n+1)
	}
	return 0, 0
}

// minMainSize is the item's resolved minimum main size: its min-width or
// min-height, or else its automatic minimum (its min-content size,
// capped by its size and max-size; 0 for scroll containers).
func (it *flexItem) minMainSize(m, x int, parent, avail Size, p *Pass) float32 {
	if isDef(it.minMain) {
		return it.minMain
	}
	v := it.min.get(m)
	switch {
	case isDef(v):
	case it.node.isScrollContainer():
		v = 0
	default:
		known := Size{nan, nan}
		known.set(x, it.knownX)
		ca := Size{}
		ca.set(m, MinContentSpace)
		ca.set(x, avail.get(x))
		v = it.node.measureSize(input{sizing: sizeContent, known: known, parent: parent, avail: ca}, p).get(m)
		v = maybeMin(v, it.size.get(m))
		v = maybeMin(v, it.max.get(m))
	}
	it.minMain = maxf(v, it.pb.get(m))
	return it.minMain
}

func resolveFlexible(line []flexItem, innerM, gap float32, m, x int, parent, avail Size, p *Pass) {
	used := gap * float32(len(line)-1)
	for i := range line {
		used += line[i].hypo.get(m) + sumAxis(line[i].margin, m)
	}
	growing := used < innerM
	if abs(used-innerM) < 0.0001 { // it fits exactly: nothing flexes
		for i := range line {
			line[i].target = line[i].hypo.get(m)
		}
		return
	}
	for i := range line {
		it := &line[i]
		it.target = it.hypo.get(m)
		it.frozen = (growing && it.grow == 0) || (!growing && it.shrink == 0) ||
			(growing && it.basis > it.hypo.get(m)) || (!growing && it.basis < it.hypo.get(m))
	}
	freeSpace := func() float32 {
		f := innerM - gap*float32(len(line)-1)
		for i := range line {
			it := &line[i]
			f -= sumAxis(it.margin, m)
			if it.frozen {
				f -= it.target
			} else {
				f -= it.basis
			}
		}
		return f
	}
	initial := freeSpace()
	for range len(line) + 1 {
		unfrozen := false
		for i := range line {
			if !line[i].frozen {
				unfrozen = true
				break
			}
		}
		if !unfrozen {
			break
		}
		remaining := freeSpace()
		sumGrow, sumShrink := float32(0), float32(0)
		for i := range line {
			if !line[i].frozen {
				sumGrow += line[i].grow
				sumShrink += line[i].shrink * line[i].inner
			}
		}
		sumFactors := sumGrow
		if !growing {
			sumFactors = 0
			for i := range line {
				if !line[i].frozen {
					sumFactors += line[i].shrink
				}
			}
		}
		if sumFactors < 1 {
			if v := initial * sumFactors; abs(v) < abs(remaining) {
				remaining = v
			}
		}
		for i := range line {
			it := &line[i]
			if it.frozen {
				continue
			}
			it.target = it.basis
			if remaining != 0 {
				if growing && sumGrow > 0 {
					it.target = it.basis + remaining*it.grow/sumGrow
				} else if !growing && sumShrink > 0 {
					it.target = it.basis + remaining*(it.shrink*it.inner)/sumShrink
				}
			}
		}
		total := float32(0)
		for i := range line {
			it := &line[i]
			if it.frozen {
				continue
			}
			c := maybeMin(it.target, it.max.get(m))
			// Growing past a content-based basis can't go below the
			// item's min-content size: skip measuring it.
			if !(growing && it.content && c >= it.basis) {
				c = maxf(c, it.minMainSize(m, x, parent, avail, p))
			} else if isDef(it.min.get(m)) {
				c = maxf(c, it.min.get(m))
			}
			it.violation = c - it.target
			it.target = c
			total += it.violation
		}
		for i := range line {
			it := &line[i]
			if it.frozen {
				continue
			}
			switch {
			case total == 0:
				it.frozen = true
			case total > 0 && it.violation > 0:
				it.frozen = true
			case total < 0 && it.violation < 0:
				it.frozen = true
			}
		}
	}
}

func abs(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// flexIntrinsicMain is the inner main size of a container whose main
// size is not known (§9.9.1, as browsers implement it: each item adds its
// own flex fraction rather than the line's largest).
func (n *Node) flexIntrinsicMain(items []flexItem, lines []flexLine, m, x int, gap float32, wrap bool, parent, avail Size, in *input, p *Pass) float32 {
	am := avail.get(m)
	itemLen := func(it *flexItem) float32 {
		return maxf(maybeMax(it.basis, it.min.get(m))+sumAxis(it.margin, m), it.pb.get(m))
	}
	if definite(am) || (am == MinContentSpace && wrap) {
		longest := float32(0)
		for _, l := range lines {
			s := gap * float32(l.end-l.start-1)
			for i := l.start; i < l.end; i++ {
				s += itemLen(&items[i])
			}
			longest = maxf(longest, s)
		}
		if definite(am) && len(lines) > 1 {
			longest = maxf(longest, am)
		}
		return longest
	}
	result := float32(0)
	for _, l := range lines {
		sum, largest := float32(0), float32(0)
		for i := l.start; i < l.end; i++ {
			it := &items[i]
			mm := sumAxis(it.margin, m)
			smin, spref, smax := it.min.get(m), it.size.get(m), it.max.get(m)
			clampBasis := it.basis
			if m == 0 {
				clampBasis = maybeMax(it.basis, spref)
			}
			// Under max-content the content contribution is at least the
			// min-content size, so only min-content sizing needs it.
			minMain := it.pb.get(m)
			if am == MinContentSpace || !it.content {
				minMain = it.minMainSize(m, x, parent, avail, p)
			}
			lo := minMain
			if it.shrink == 0 {
				lo = maxf(maybeMax(clampBasis, smin), minMain)
			} else if isDef(smin) {
				lo = maxf(smin, minMain)
			}
			hi := Inf
			if it.grow == 0 {
				hi = maybeMin(clampBasis, smax)
			} else if isDef(smax) {
				hi = smax
			}
			var contrib float32
			switch {
			case isDef(spref) && (hi <= lo || hi <= spref):
				contrib = maxf(minf(spref, hi), lo) + mm
			case hi <= lo:
				contrib = lo + mm
			case it.node.isScrollContainer():
				contrib = it.basis + mm
			case isDef(spref):
				v := maxf(spref, it.pb.get(m))
				if m == 1 {
					v = maxf(v, it.basis)
				}
				contrib = clampN(v+mm, smin, smax)
			default:
				ax := avail.get(x)
				if definite(ax) {
					ax = maxf(0, orF(parent.get(x), ax)-sumAxis(it.margin, x))
					ax = clampN(ax, it.min.get(x), it.max.get(x))
				}
				known := Size{nan, nan}
				known.set(x, clampN(it.size.get(x), it.min.get(x), it.max.get(x)))
				if it.align == AlignStretch && isNaN(known.get(x)) && definite(ax) {
					known.set(x, ax)
				}
				ca := Size{}
				ca.set(m, am)
				ca.set(x, ax)
				v := it.node.measureSize(input{sizing: sizeContent, known: known, parent: parent, avail: ca}, p).get(m)
				if r := it.node.Style.AspectRatio; r > 0 && isDef(known.get(x)) {
					if m == 0 {
						v = maxf(v, known.get(x)*r)
					} else {
						v = maxf(v, known.get(x)/r)
					}
				}
				if m == 1 {
					v = maxf(v, it.basis)
				}
				contrib = clampN(v+mm, smin, smax)
			}
			diff := contrib - mm - it.basis
			size := it.basis
			switch {
			case diff > 0:
				size += diff
			case diff < 0:
				if scaled := maxf(1, it.shrink) * it.inner; scaled != 0 {
					size += diff
				}
			}
			size += mm
			largest = maxf(largest, size)
			sum += size
		}
		sum += gap * float32(l.end-l.start-1)
		if wrap && m == 0 {
			sum = maxf(sum, largest)
		}
		result = maxf(result, sum)
	}
	return result
}
