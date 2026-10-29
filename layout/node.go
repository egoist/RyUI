package layout

// Constraints is what a measured leaf (text, a native control) is asked:
// its content box's known size on each axis, NaN when free, and the
// space available on each axis: a definite amount, MaxContentSpace (lay
// out without limit) or MinContentSpace (as narrow as it can be, e.g.
// wrapping at every word).
type Constraints struct {
	Width, Height  float32
	AvailW, AvailH float32
}

// MeasureFunc returns the size of a leaf's content box.
type MeasureFunc func(c Constraints) Size

// BaselineFunc returns a leaf's first baseline, from the top of its
// border box, given its final size.
type BaselineFunc func(s Size) float32

type runMode uint8

const (
	modeSize   runMode = iota // only the size is needed
	modeLayout                // size and place the subtree
)

type sizingMode uint8

const (
	sizeInherent sizingMode = iota // apply the node's own size styles
	sizeContent                    // ignore them: the size of the content
)

type input struct {
	mode   runMode
	sizing sizingMode
	// collapse: whether the node's top/bottom margins may collapse with
	// its block children (it sits in a block formatting context).
	collapse [2]bool
	known    Size // border-box size the parent already decided; NaN: free
	parent   Size // the containing block, for percentages; NaN: indefinite
	avail    Size // definite, MaxContentSpace or MinContentSpace
}

// marginSet is a set of adjoining vertical margins.
type marginSet struct{ pos, neg float32 }

func (m marginSet) add(v float32) marginSet {
	if v >= 0 {
		m.pos = maxf(m.pos, v)
	} else {
		m.neg = minf(m.neg, v)
	}
	return m
}

func (m marginSet) join(o marginSet) marginSet {
	return marginSet{maxf(m.pos, o.pos), minf(m.neg, o.neg)}
}

func (m marginSet) value() float32 { return m.pos + m.neg }

type output struct {
	size     Size
	baseline float32 // first baseline from the top; NaN: none
	// Block layout: margins that collapse through this box's edges.
	top, bottom      marginSet
	collapsesThrough bool
}

const measureSlots = 8

type cacheEntry struct {
	mode   runMode
	sizing sizingMode
	coll   [2]bool
	known  Size
	avail  Size
	out    output
}

func sameF(a, b float32) bool { return a == b || (a != a && b != b) }

func sameAvail(a, b float32) bool {
	if isInf(a) || isInf(b) {
		return a == b
	}
	d := a - b
	return d < 0.0001 && d > -0.0001
}

func (e *cacheEntry) matches(in *input) bool {
	if e.sizing != in.sizing || e.coll != in.collapse {
		return false
	}
	if !(sameF(in.known.W, e.known.W) || in.known.W == e.out.size.W) ||
		!(sameF(in.known.H, e.known.H) || in.known.H == e.out.size.H) {
		return false
	}
	return (isDef(in.known.W) || sameAvail(e.avail.W, in.avail.W)) &&
		(isDef(in.known.H) || sameAvail(e.avail.H, in.avail.H))
}

type cache struct {
	final   cacheEntry
	hasFin  bool
	slots   [measureSlots]cacheEntry
	n, next uint8
}

func (c *cache) get(in *input) (output, bool) {
	if c.hasFin && c.final.matches(in) {
		return c.final.out, true
	}
	if in.mode == modeLayout {
		return output{}, false
	}
	for i := uint8(0); i < c.n; i++ {
		if c.slots[i].matches(in) {
			return c.slots[i].out, true
		}
	}
	return output{}, false
}

func (c *cache) put(in *input, out output) {
	e := cacheEntry{in.mode, in.sizing, in.collapse, in.known, in.avail, out}
	if in.mode == modeLayout {
		c.final, c.hasFin = e, true
		return
	}
	c.slots[c.next] = e
	c.next = (c.next + 1) % measureSlots
	if c.n < measureSlots {
		c.n++
	}
}

func (c *cache) clear() { c.hasFin, c.n, c.next = false, 0, 0 }

// Node is a box in the layout tree.
type Node struct {
	Style Style
	// Context is for the tree's owner (ui keeps its element here).
	Context any

	measure  MeasureFunc
	baseline BaselineFunc
	parent   *Node
	children []*Node

	// The last layout: the border box relative to the parent's border
	// box, in points, unrounded.
	X, Y, W, H float32

	dirty    bool
	cache    cache
	reported [4]float32
	staticY  float32 // block layout's static position for absolute children
}

// New makes a node with DefaultStyle.
func New() *Node {
	return &Node{Style: DefaultStyle, dirty: true, reported: [4]float32{nan, nan, nan, nan}}
}

// SetStyle replaces the style and marks the node dirty.
func (n *Node) SetStyle(s Style) {
	n.Style = s
	n.MarkDirty()
}

// SetMeasure makes n a leaf measured by f (nil: an empty box).
func (n *Node) SetMeasure(f MeasureFunc) {
	n.measure = f
	n.MarkDirty()
}

// SetBaseline sets how a measured leaf reports its baseline.
func (n *Node) SetBaseline(f BaselineFunc) { n.baseline = f }

// MarkDirty drops the cached layouts of n and its ancestors: call it
// when n's style, children or measured content changed.
func (n *Node) MarkDirty() {
	for p := n; p != nil; p = p.parent {
		if p.dirty && p != n {
			return
		}
		p.dirty = true
		p.cache.clear()
	}
}

// Dirty reports whether n must be laid out again.
func (n *Node) Dirty() bool { return n.dirty }

// Parent returns n's parent, or nil.
func (n *Node) Parent() *Node { return n.parent }

// Children returns n's children. Don't modify the slice.
func (n *Node) Children() []*Node { return n.children }

// SetChildren replaces n's children with a copy of children.
func (n *Node) SetChildren(children []*Node) {
	for _, c := range n.children {
		if c.parent == n {
			c.parent = nil
		}
	}
	n.children = append(n.children[:0], children...)
	clear(n.children[len(n.children):cap(n.children)])
	for _, c := range n.children {
		if c.parent != nil && c.parent != n {
			c.parent.removeChild(c)
		}
		c.parent = n
		c.ForceReport()
	}
	n.MarkDirty()
}

// AppendChild adds c as n's last child.
func (n *Node) AppendChild(c *Node) {
	if c.parent != nil {
		c.parent.removeChild(c)
	}
	c.parent = n
	c.ForceReport()
	n.children = append(n.children, c)
	n.MarkDirty()
}

// RemoveChild removes c from n's children.
func (n *Node) RemoveChild(c *Node) {
	if c.parent == n {
		n.removeChild(c)
		c.parent = nil
	}
}

func (n *Node) removeChild(c *Node) {
	for i, x := range n.children {
		if x == c {
			copy(n.children[i:], n.children[i+1:])
			n.children[len(n.children)-1] = nil
			n.children = n.children[:len(n.children)-1]
			n.MarkDirty()
			return
		}
	}
}

// ForceReport makes the next layout report n's frame as changed.
func (n *Node) ForceReport() { n.reported[0] = nan }

// Pass carries one layout run's state and scratch memory. Reuse it so
// layout allocates nothing in the steady state.
type Pass struct {
	// Changed lists the nodes whose frame changed. Layout appends; the
	// caller resets it.
	Changed []*Node
	// Measures counts MeasureFunc calls.
	Measures int

	flexItems []flexItem
	flexLines []flexLine
	gridItems []gridItem
	tracks    []track
	occupied  []area
	blocks    []blockItem
}

type area struct{ r0, r1, c0, c1 int }

// Layout lays out the tree under n within the available space (a
// definite size, MaxContentSpace or MinContentSpace per axis). n's own
// size comes from its style resolved against avail; its X and Y are 0.
func (n *Node) Layout(avail Size, p *Pass) Size {
	parent := Size{nan, nan}
	if definite(avail.W) {
		parent.W = avail.W
	}
	if definite(avail.H) {
		parent.H = avail.H
	}
	known := n.styleSize(parent)
	min, max := n.styleMin(parent), n.styleMax(parent)
	known.W = clampN(known.W, min.W, max.W)
	known.H = clampN(known.H, min.H, max.H)
	pad, bor := n.paddingBorder(parent.W)
	if isDef(known.W) {
		known.W = maxf(known.W, pad.Left+pad.Right+bor.Left+bor.Right)
	}
	if isDef(known.H) {
		known.H = maxf(known.H, pad.Top+pad.Bottom+bor.Top+bor.Bottom)
	}
	out := n.compute(input{mode: modeLayout, known: known, parent: parent, avail: avail}, p)
	m := n.margins(parent.W)
	p.place(n, m.Left, m.Top, out.size)
	return out.size
}

func (p *Pass) place(n *Node, x, y float32, s Size) {
	n.X, n.Y, n.W, n.H = x, y, s.W, s.H
	f := [4]float32{x, y, s.W, s.H}
	if f != n.reported {
		n.reported = f
		p.Changed = append(p.Changed, n)
	}
}

func (n *Node) compute(in input, p *Pass) output {
	if out, ok := n.cache.get(&in); ok {
		return out
	}
	var out output
	switch {
	case n.Style.Display == DisplayNone:
		out = output{baseline: nan}
		if in.mode == modeLayout {
			n.hide(p)
		}
	case len(n.children) == 0:
		out = n.leaf(&in, p)
	case n.Style.Display == DisplayBlock:
		out = n.block(&in, p)
	case n.Style.Display == DisplayGrid:
		out = n.grid(&in, p)
	default:
		out = n.flex(&in, p)
	}
	n.cache.put(&in, out)
	if in.mode == modeLayout {
		n.dirty = false
	}
	return out
}

// hide gives a display: none subtree zero frames.
func (n *Node) hide(p *Pass) {
	for _, c := range n.children {
		p.place(c, 0, 0, Size{})
		c.hide(p)
		c.dirty = false
	}
}

// measureSize returns the border-box size of child under in.
func (n *Node) measureSize(in input, p *Pass) Size {
	in.mode = modeSize
	return n.compute(in, p).size
}

// Resolved box model.

func resolveRect(r *Rect[Length], basis float32) Rect[float32] {
	return Rect[float32]{r.Top.resolve0(basis), r.Right.resolve0(basis), r.Bottom.resolve0(basis), r.Left.resolve0(basis)}
}

func (r Rect[T]) start(axis int) T {
	if axis == 0 {
		return r.Left
	}
	return r.Top
}

func (r Rect[T]) end(axis int) T {
	if axis == 0 {
		return r.Right
	}
	return r.Bottom
}

func sumAxis(r Rect[float32], axis int) float32 { return r.start(axis) + r.end(axis) }

func sumBoth(r Rect[float32]) Size { return Size{r.Left + r.Right, r.Top + r.Bottom} }

func (n *Node) paddingBorder(parentW float32) (pad, border Rect[float32]) {
	return resolveRect(&n.Style.Padding, parentW), resolveRect(&n.Style.Border, parentW)
}

func (n *Node) margins(parentW float32) Rect[float32] {
	return resolveRect(&n.Style.Margin, parentW)
}

func (n *Node) boxAdjust(parentW float32) Size {
	if n.Style.BoxSizing != ContentBox {
		return Size{}
	}
	pad, bor := n.paddingBorder(parentW)
	return Size{pad.Left + pad.Right + bor.Left + bor.Right, pad.Top + pad.Bottom + bor.Top + bor.Bottom}
}

func applyAspect(s Size, ratio float32) Size {
	if ratio > 0 {
		if isDef(s.W) && isNaN(s.H) {
			s.H = s.W / ratio
		} else if isDef(s.H) && isNaN(s.W) {
			s.W = s.H * ratio
		}
	}
	return s
}

// styleSize is the node's Width/Height as border-box sizes (NaN: auto).
func (n *Node) styleSize(parent Size) Size {
	st := &n.Style
	s := applyAspect(Size{st.Width.resolve(parent.W), st.Height.resolve(parent.H)}, st.AspectRatio)
	adj := n.boxAdjust(parent.W)
	return Size{s.W + adj.W, s.H + adj.H}
}

func (n *Node) styleMin(parent Size) Size {
	st := &n.Style
	s := applyAspect(Size{st.MinWidth.resolve(parent.W), st.MinHeight.resolve(parent.H)}, st.AspectRatio)
	adj := n.boxAdjust(parent.W)
	return Size{s.W + adj.W, s.H + adj.H}
}

func (n *Node) styleMax(parent Size) Size {
	st := &n.Style
	adj := n.boxAdjust(parent.W)
	return Size{st.MaxWidth.resolve(parent.W) + adj.W, st.MaxHeight.resolve(parent.H) + adj.H}
}

// scrollGutter is the space a scroll container reserves for scrollbars:
// a vertical scroller takes width.
func (n *Node) scrollGutter() Size {
	var g Size
	if n.Style.OverflowY == OverflowScroll {
		g.W = n.Style.ScrollbarWidth
	}
	if n.Style.OverflowX == OverflowScroll {
		g.H = n.Style.ScrollbarWidth
	}
	return g
}

func (n *Node) isScrollContainer() bool {
	return n.Style.OverflowX != OverflowVisible || n.Style.OverflowY != OverflowVisible
}

func (n *Node) leaf(in *input, p *Pass) output {
	st := &n.Style
	pad, bor := n.paddingBorder(in.parent.W)
	pb := Size{pad.Left + pad.Right + bor.Left + bor.Right, pad.Top + pad.Bottom + bor.Top + bor.Bottom}
	gutter := n.scrollGutter()
	inset := Size{pb.W + gutter.W, pb.H + gutter.H}

	size, min, max := in.known, Size{nan, nan}, Size{nan, nan}
	ratio := float32(0)
	if in.sizing == sizeInherent {
		ss := n.styleSize(in.parent)
		size = Size{orF(size.W, ss.W), orF(size.H, ss.H)}
		min, max = n.styleMin(in.parent), n.styleMax(in.parent)
		ratio = st.AspectRatio
	}
	blocksCollapse := st.Display != DisplayBlock || n.isScrollContainer() || st.Position == Absolute ||
		pad.Top > 0 || pad.Bottom > 0 || bor.Top > 0 || bor.Bottom > 0 ||
		(isDef(size.H) && size.H > 0) || (isDef(min.H) && min.H > 0)

	if in.mode == modeSize && blocksCollapse && isDef(size.W) && isDef(size.H) {
		s := Size{clampN(size.W, min.W, max.W), clampN(size.H, min.H, max.H)}
		return output{size: Size{maxf(s.W, pb.W), maxf(s.H, pb.H)}, baseline: nan}
	}

	avail := in.avail
	for a := 0; a < 2; a++ {
		v := avail.get(a)
		if k := in.known.get(a); isDef(k) {
			v = k
		}
		if s := size.get(a); isDef(s) {
			v = s
		}
		if definite(v) {
			v = clampN(v, min.get(a), max.get(a)) - inset.get(a)
		}
		avail.set(a, v)
	}
	measured := Size{}
	if n.measure != nil {
		known := Size{in.known.W - inset.W, in.known.H - inset.H}
		if isDef(known.W) && isDef(known.H) {
			measured = known
		} else {
			p.Measures++
			measured = n.measure(Constraints{Width: known.W, Height: known.H, AvailW: avail.W, AvailH: avail.H})
			if isDef(known.W) {
				measured.W = known.W
			}
			if isDef(known.H) {
				measured.H = known.H
			}
		}
	}
	s := Size{orF(in.known.W, size.W), orF(in.known.H, size.H)}
	if isNaN(s.W) {
		s.W = measured.W + inset.W
	}
	if isNaN(s.H) {
		s.H = measured.H + inset.H
	}
	s.W, s.H = clampN(s.W, min.W, max.W), clampN(s.H, min.H, max.H)
	if ratio > 0 {
		s.H = maxf(s.H, s.W/ratio)
	}
	s.W, s.H = maxf(s.W, pb.W), maxf(s.H, pb.H)
	out := output{size: s, baseline: nan}
	if n.baseline != nil {
		out.baseline = n.baseline(s)
	}
	out.collapsesThrough = !blocksCollapse && s.H == 0 && measured.H == 0
	return out
}

// placeAbsoluteAligned lays out an absolutely positioned child against
// its containing block (ax, ay, aw, ah: the padding box or a grid area,
// relative to the parent's border box). An axis whose insets are both
// auto takes its static position: frac (0 start, 0.5 center, 1 end) of
// the way along the content box (cx, cy, cw, ch).
func (p *Pass) placeAbsoluteAligned(c *Node, ax, ay, aw, ah, cx, cy, cw, ch float32, frac [2]float32) {
	st := &c.Style
	area := Size{aw, ah}
	m := c.margins(aw)
	ins := Rect[float32]{st.Inset.Top.resolve(ah), st.Inset.Right.resolve(aw), st.Inset.Bottom.resolve(ah), st.Inset.Left.resolve(aw)}
	size := c.styleSize(area)
	min, max := c.styleMin(area), c.styleMax(area)
	autoM := [2][2]bool{{st.Margin.Left.IsAuto(), st.Margin.Right.IsAuto()}, {st.Margin.Top.IsAuto(), st.Margin.Bottom.IsAuto()}}
	for a := 0; a < 2; a++ {
		if isNaN(size.get(a)) && isDef(ins.start(a)) && isDef(ins.end(a)) {
			size.set(a, maxf(0, area.get(a)-ins.start(a)-ins.end(a)-sumAxis(m, a)))
		}
	}
	size = applyAspect(size, st.AspectRatio)
	size.W, size.H = clampN(size.W, min.W, max.W), clampN(size.H, min.H, max.H)
	avail := Size{}
	for a := 0; a < 2; a++ {
		avail.set(a, maxf(0, area.get(a)-or0(ins.start(a))-or0(ins.end(a))-sumAxis(m, a)))
	}
	final := size
	if isNaN(final.W) || isNaN(final.H) {
		measured := c.measureSize(input{known: size, parent: area, avail: avail}, p)
		final = Size{orF(size.W, measured.W), orF(size.H, measured.H)}
		final.W, final.H = clampN(final.W, min.W, max.W), clampN(final.H, min.H, max.H)
	}
	cpad, cbor := c.paddingBorder(aw)
	final.W = maxf(final.W, cpad.Left+cpad.Right+cbor.Left+cbor.Right)
	final.H = maxf(final.H, cpad.Top+cpad.Bottom+cbor.Top+cbor.Bottom)
	c.compute(input{mode: modeLayout, known: final, parent: area, avail: avail}, p)

	pos := [2]float32{}
	origin := [2]float32{ax, ay}
	content := [2][2]float32{{cx, cw}, {cy, ch}}
	for a := 0; a < 2; a++ {
		s, e := ins.start(a), ins.end(a)
		ms, me := m.start(a), m.end(a)
		sz := final.get(a)
		if isDef(s) && isDef(e) {
			free := area.get(a) - s - e - sz - ms - me
			switch {
			case autoM[a][0] && autoM[a][1]:
				half := free / 2
				if half < 0 && a == 0 {
					half = 0
				}
				ms += half
			case autoM[a][0]:
				ms += free
			}
		}
		switch {
		case isDef(s):
			pos[a] = origin[a] + s + ms
		case isDef(e):
			pos[a] = origin[a] + area.get(a) - e - me - sz
		default:
			pos[a] = content[a][0] + frac[a]*(content[a][1]-sz-ms-me) + ms
		}
	}
	p.place(c, pos[0], pos[1], final)
}

// Round snaps a laid-out tree to a pixel grid of scale pixels per point,
// the way browsers do: edges are rounded in absolute coordinates so
// neighbors never gap or overlap. It rewrites X, Y, W and H.
func Round(n *Node, scale float32) { round(n, 0, 0, scale) }

func roundTo(v, scale float32) float32 {
	return float32(int64Round(float64(v*scale))) / scale
}

func int64Round(v float64) float64 {
	if v < 0 {
		return -float64(int64(-v + 0.5))
	}
	return float64(int64(v + 0.5))
}

func round(n *Node, ax, ay, scale float32) {
	ax += n.X
	ay += n.Y
	x, y := roundTo(n.X, scale), roundTo(n.Y, scale)
	w := roundTo(ax+n.W, scale) - roundTo(ax, scale)
	h := roundTo(ay+n.H, scale) - roundTo(ay, scale)
	for _, c := range n.children {
		round(c, ax, ay, scale)
	}
	n.X, n.Y, n.W, n.H = x, y, w, h
}
