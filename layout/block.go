package layout

// Block layout: CSS 2's block formatting context without floats or
// inline content. Children stack vertically; auto widths fill the
// container; vertical margins collapse between siblings, through empty
// boxes, and with the container's own edges.

type blockItem struct {
	node     *Node
	margin   Rect[float32]
	autoM    Rect[bool]
	size     Size
	min, max Size
	pb       Size
}

func (n *Node) block(in *input, p *Pass) output {
	st := &n.Style
	pad, bor := n.paddingBorder(in.parent.W)
	gutter := n.scrollGutter()
	inset := Rect[float32]{pad.Top + bor.Top, pad.Right + bor.Right + gutter.W, pad.Bottom + bor.Bottom + gutter.H, pad.Left + bor.Left}
	pbs := Size{pad.Left + pad.Right + bor.Left + bor.Right, pad.Top + pad.Bottom + bor.Top + bor.Bottom}
	insetW, insetH := inset.Left+inset.Right, inset.Top+inset.Bottom

	min, max := n.styleMin(in.parent), n.styleMax(in.parent)
	size := in.known
	if in.sizing == sizeInherent {
		ss := n.styleSize(in.parent)
		ss.W, ss.H = clampN(ss.W, min.W, max.W), clampN(ss.H, min.H, max.H)
		for a := 0; a < 2; a++ {
			if isNaN(size.get(a)) {
				if lo, hi := min.get(a), max.get(a); isDef(lo) && isDef(hi) && hi <= lo {
					size.set(a, lo)
				} else {
					size.set(a, ss.get(a))
				}
			}
		}
	}
	if isDef(size.W) {
		size.W = maxf(size.W, pbs.W)
	}
	if isDef(size.H) {
		size.H = maxf(size.H, pbs.H)
	}
	if in.mode == modeSize && isDef(size.W) && isDef(size.H) {
		return output{size: size, baseline: nan}
	}
	newBFC := n.isScrollContainer() || st.AlignContent != ContentNormal
	adjoinTop := in.collapse[0] && !newBFC && st.Position != Absolute && pad.Top == 0 && bor.Top == 0
	adjoinBottom := in.collapse[1] && !newBFC && st.Position != Absolute && pad.Bottom == 0 && bor.Bottom == 0
	collapseBottom := adjoinBottom && isNaN(size.H)
	blocksThrough := st.Display != DisplayBlock || !in.collapse[0] || newBFC || st.Position == Absolute ||
		pad.Top > 0 || pad.Bottom > 0 || bor.Top > 0 || bor.Bottom > 0

	base := len(p.blocks)
	defer func() {
		clear(p.blocks[base:])
		p.blocks = p.blocks[:base]
	}()
	contentSize := Size{nan, nan}
	if isDef(size.W) {
		contentSize.W = maxf(0, size.W-insetW)
	}
	if isDef(size.H) {
		contentSize.H = maxf(0, size.H-insetH)
	}
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
		it := blockItem{node: ch}
		it.size = ch.styleSize(contentSize)
		it.min, it.max = ch.styleMin(contentSize), ch.styleMax(contentSize)
		it.margin = ch.margins(contentSize.W)
		it.autoM = Rect[bool]{cs.Margin.Top.IsAuto(), cs.Margin.Right.IsAuto(), cs.Margin.Bottom.IsAuto(), cs.Margin.Left.IsAuto()}
		cpad, cbor := ch.paddingBorder(contentSize.W)
		it.pb = Size{cpad.Left + cpad.Right + cbor.Left + cbor.Right, cpad.Top + cpad.Bottom + cbor.Top + cbor.Bottom}
		p.blocks = append(p.blocks, it)
	}
	items := p.blocks[base:]

	// The container's width.
	if isNaN(size.W) {
		availW := in.avail.W
		if definite(availW) {
			availW -= insetW
		}
		w := float32(0)
		for i := range items {
			it := &items[i]
			known := Size{clampN(it.size.W, it.min.W, it.max.W), clampN(it.size.H, it.min.H, it.max.H)}
			mw := it.margin.Left + it.margin.Right
			cw := known.W
			if isNaN(cw) {
				a := availW
				if definite(a) {
					a -= mw
				}
				cw = it.node.measureSize(input{known: known, parent: Size{nan, nan}, avail: Size{a, MinContentSpace}, collapse: [2]bool{true, true}}, p).W
			}
			w = maxf(w, maxf(cw, it.pb.W)+mw)
		}
		size.W = maxf(clampN(w+insetW, min.W, max.W), pbs.W)
		contentSize.W = maxf(0, size.W-insetW)
	}
	if in.mode == modeSize && isDef(size.H) {
		return output{size: size, baseline: nan}
	}
	innerW := contentSize.W
	percentH := contentSize.H
	if isNaN(percentH) && in.sizing == sizeInherent {
		if h := n.styleSize(in.parent).H; isDef(h) {
			percentH = maybeMax(h, min.H) - insetH
		}
	}
	parent := Size{innerW, percentH}

	// Lay out the in-flow children.
	final := in.mode == modeLayout
	y := inset.Top
	firstTop, active := marginSet{}, marginSet{}
	collapsingFirst := true
	allThrough := true
	firstBaseline := float32(nan)
	absY := inset.Top
	ii := 0
	for _, ch := range n.children {
		if ch.Style.Position == Absolute {
			ch.staticY = absY
			continue
		}
		if ch.Style.Display == DisplayNone {
			continue
		}
		it := &items[ii]
		ii++
		mx := it.margin.Left + it.margin.Right
		known := Size{it.size.W, nan}
		if isNaN(known.W) {
			known.W = clampN(innerW-mx, it.min.W, it.max.W)
		}
		cin := input{known: known, parent: parent, avail: Size{innerW - mx, MinContentSpace}, collapse: [2]bool{true, true}}
		if final {
			cin.mode = modeLayout
		}
		out := ch.compute(cin, p)
		top := out.top.add(it.margin.Top)
		bottom := out.bottom.add(it.margin.Bottom)
		freeX := maxf(0, innerW-out.size.W-mx)
		left := it.margin.Left
		switch {
		case it.autoM.Left && it.autoM.Right:
			left += freeX / 2
		case it.autoM.Left:
			left += freeX
		}
		yOff := active.join(top).value()
		if collapsingFirst && adjoinTop {
			yOff = 0
		}
		if final {
			px, py := relativeOffset(ch, inset.Left+left, y+yOff, parent)
			p.place(ch, px, py, out.size)
			if isNaN(firstBaseline) && isDef(out.baseline) {
				firstBaseline = py + out.baseline
			}
		}
		if collapsingFirst {
			if out.collapsesThrough {
				firstTop = firstTop.join(top).join(bottom)
			} else {
				firstTop = firstTop.join(top)
				collapsingFirst = false
			}
		}
		if out.collapsesThrough {
			active = active.join(top).join(bottom)
			absY = y + out.size.H + yOff
		} else {
			allThrough = false
			y += out.size.H + yOff
			active = bottom
			absY = y + active.value()
		}
	}
	lastBottom := active
	bottomOff := lastBottom.value()
	if collapseBottom {
		bottomOff = 0
	}
	contentH := maxf(0, y+inset.Bottom+bottomOff)
	if isNaN(size.H) {
		size.H = maxf(clampN(contentH, min.H, max.H), pbs.H)
	}
	if isDef(min.H) && min.H > 0 && min.H >= size.H {
		collapseBottom = false
	}
	out := output{size: size, baseline: firstBaseline}
	if adjoinTop {
		out.top = firstTop
	}
	if collapseBottom {
		out.bottom = lastBottom
	}
	out.collapsesThrough = !blocksThrough && allThrough && size.H == 0 && (isNaN(min.H) || min.H <= 0)
	if !final {
		return out
	}

	// Absolutely positioned children, from their static position.
	cw := size.W - insetW
	ch2 := size.H - insetH
	for _, c := range n.children {
		if c.Style.Position != Absolute || c.Style.Display == DisplayNone {
			continue
		}
		p.placeAbsoluteAligned(c, bor.Left, bor.Top, size.W-bor.Left-bor.Right-gutter.W, size.H-bor.Top-bor.Bottom-gutter.H,
			inset.Left, c.staticY, cw, ch2, [2]float32{0, 0})
	}
	return out
}
