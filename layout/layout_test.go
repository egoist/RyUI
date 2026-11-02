package layout

import (
	"fmt"
	"strings"
	"testing"
)

func box(s Style, kids ...*Node) *Node {
	n := New()
	n.Style = s
	n.SetChildren(kids)
	return n
}

func col(kids ...*Node) *Node {
	s := DefaultStyle
	s.FlexDirection = Column
	return box(s, kids...)
}

func row(kids ...*Node) *Node { return box(DefaultStyle, kids...) }

// label measures like text: 8pt per character, 20pt lines, wrapping.
func label(chars *int) *Node {
	n := New()
	n.SetMeasure(func(c Constraints) Size {
		w := float32(*chars * 8)
		if isDef(c.Width) {
			w = c.Width
		} else if definite(c.AvailW) {
			w = minf(w, c.AvailW)
		} else if c.AvailW == MinContentSpace {
			w = 8
		}
		lines := 1
		if w > 0 {
			per := int(w / 8)
			lines = max(1, (*chars+per-1)/max(per, 1))
		}
		return Size{w, float32(lines * 20)}
	})
	return n
}

func frames(ns ...*Node) string {
	var b []string
	for _, n := range ns {
		b = append(b, fmt.Sprintf("%g,%g %gx%g", n.X, n.Y, n.W, n.H))
	}
	return strings.Join(b, " | ")
}

func screen(n *Node, w, h float32, p *Pass) {
	n.Style.Width, n.Style.Height = Pct(100), Pct(100)
	n.Layout(Size{w, h}, p)
}

func TestScreenColumn(t *testing.T) {
	a, b := 5, 50
	ta, tb := label(&a), label(&b)
	spacer := New()
	spacer.Style.FlexGrow = 1
	root := col(ta, spacer, tb)
	root.Style.Padding = All(Pt(10))
	var p Pass
	screen(root, 200, 400, &p)
	if got := frames(root, ta, spacer, tb); got != "0,0 200x400 | 10,10 180x20 | 10,30 180x300 | 10,330 180x60" {
		t.Fatal(got)
	}
}

func TestRelayoutWithoutChangesDoesNothing(t *testing.T) {
	n := make([]int, 20)
	var kids []*Node
	for i := range n {
		n[i] = 5 + i
		kids = append(kids, row(label(&n[i]), label(&n[i])))
	}
	root := col(kids...)
	var p Pass
	screen(root, 320, 640, &p)
	if p.Measures == 0 || len(p.Changed) == 0 {
		t.Fatal("first layout did nothing")
	}
	p = Pass{}
	screen(root, 320, 640, &p)
	if p.Measures != 0 || len(p.Changed) != 0 {
		t.Fatalf("second layout: %d measures, %d changed", p.Measures, len(p.Changed))
	}
}

func TestChangingOneTextMeasuresIt(t *testing.T) {
	n := make([]int, 100)
	var kids []*Node
	for i := range n {
		n[i] = 3
		kids = append(kids, label(&n[i]))
	}
	root := col(kids...)
	var p Pass
	screen(root, 320, 4000, &p)

	n[50] = 6 // same height
	kids[50].MarkDirty()
	p = Pass{}
	screen(root, 320, 4000, &p)
	if p.Measures > 2 {
		t.Fatalf("%d measures", p.Measures)
	}
	if len(p.Changed) != 0 { // stretched: its frame didn't change
		t.Fatalf("changed %s", frames(p.Changed...))
	}

	n[50] = 100 // two lines: it and what follows move
	kids[50].MarkDirty()
	p = Pass{}
	screen(root, 320, 4000, &p)
	if len(p.Changed) != 50 || p.Changed[0] != kids[50] {
		t.Fatalf("%d changed", len(p.Changed))
	}
}

func TestSteadyStateLayoutAllocatesNothing(t *testing.T) {
	var chars []int
	for i := 0; i < 50; i++ {
		chars = append(chars, i%7+1)
	}
	var kids []*Node
	for i := range chars {
		g := New()
		g.Style.FlexGrow = 1
		kids = append(kids, row(label(&chars[i]), g, label(&chars[i])))
	}
	grid := New()
	grid.Style.Display = DisplayGrid
	grid.Style.GridTemplateColumns = []GridTrack{Repeat(3, Fr(1))}
	for i := 0; i < 9; i++ {
		grid.AppendChild(label(&chars[i]))
	}
	blk := New()
	blk.Style.Display = DisplayBlock
	blk.AppendChild(label(&chars[0]))
	root := col(append(kids, grid, blk)...)
	var p Pass
	screen(root, 320, 2000, &p)
	w := float32(320)
	allocs := testing.AllocsPerRun(200, func() {
		w = 680 - w // alternate widths so everything recomputes
		p.Changed = p.Changed[:0]
		screen(root, w, 2000, &p)
	})
	if allocs != 0 {
		t.Fatalf("%v allocations per layout", allocs)
	}
}

// directionTree has a flex row, a column, a grid, a block and an absolute
// child, symmetric from side to side but for what runs from start to end.
func directionTree() (*Node, []*Node) {
	sized := func(w, h float32) *Node {
		n := New()
		n.Style.Width, n.Style.Height = Pt(w), Pt(h)
		return n
	}
	a, b := sized(30, 10), sized(50, 10)
	r := row(a, b)
	r.Style.ColumnGap = Pt(4)

	c0 := sized(20, 10)
	c0.Style.AlignSelf = AlignFlexEnd
	c1 := sized(20, 10)
	c := col(c0, c1)
	c.Style.Padding = All(Pt(3))

	g0, g1, g2 := New(), sized(10, 10), New()
	g0.Style.Height, g2.Style.Height = Pt(10), Pt(10)
	g1.Style.JustifySelf = AlignEnd
	g := box(DefaultStyle, g0, g1, g2)
	g.Style.Display = DisplayGrid
	g.Style.GridTemplateColumns = []GridTrack{T(Px(20)), T(Px(40))}

	b0 := sized(40, 10)
	b0.Style.Display = DisplayBlock
	bl := box(DefaultStyle, b0)
	bl.Style.Display = DisplayBlock

	abs := sized(16, 16)
	abs.Style.Position = Absolute
	abs.Style.Inset.Left = Pt(5)

	root := col(r, c, g, bl, abs)
	root.Style.Width, root.Style.Height = Pt(300), Pt(400)
	root.Style.Padding = All(Pt(8))
	return root, []*Node{a, b, r, c0, c1, c, g0, g1, g2, g, b0, bl}
}

func setDirection(n *Node, d Direction) {
	s := n.Style
	s.Direction = d
	n.SetStyle(s)
}

func TestFlippingDirectionMirrorsFromCleanCaches(t *testing.T) {
	root, nodes := directionTree()
	var p Pass
	root.Layout(Size{300, 400}, &p)
	ltr := map[*Node][4]float32{}
	for _, n := range nodes {
		ltr[n] = [4]float32{n.X, n.Y, n.W, n.H}
	}

	setDirection(root, DirRTL)
	if !nodes[0].Dirty() || !nodes[5].Dirty() || !nodes[10].Dirty() {
		t.Fatal("changing the direction left descendants clean")
	}
	p = Pass{}
	root.Layout(Size{300, 400}, &p)
	if len(p.Changed) == 0 {
		t.Fatal("nothing moved")
	}
	for i, n := range nodes {
		l := ltr[n]
		want := [4]float32{n.parent.W - l[0] - l[2], l[1], l[2], l[3]}
		if got := [4]float32{n.X, n.Y, n.W, n.H}; got != want {
			t.Errorf("node %d: got %v, want %v (left to right: %v)", i, got, want, l)
		}
	}

	// As a tree laid out only right to left has it: nothing stale.
	fresh, freshNodes := directionTree()
	fresh.Style.Direction = DirRTL
	var q Pass
	fresh.Layout(Size{300, 400}, &q)
	for i, n := range nodes {
		f := freshNodes[i]
		if [4]float32{n.X, n.Y, n.W, n.H} != [4]float32{f.X, f.Y, f.W, f.H} {
			t.Errorf("node %d: flipped %s, fresh %s", i, frames(n), frames(f))
		}
	}

	setDirection(root, DirLTR)
	root.Layout(Size{300, 400}, &p)
	for i, n := range nodes {
		if got := [4]float32{n.X, n.Y, n.W, n.H}; got != ltr[n] {
			t.Errorf("node %d back to left to right: got %v, want %v", i, got, ltr[n])
		}
	}
}

func TestExplicitDirectionInsideAnother(t *testing.T) {
	sized := func() *Node {
		n := New()
		n.Style.Width, n.Style.Height = Pt(10), Pt(10)
		return n
	}
	a, b := sized(), sized()
	inner := row(a, b)
	inner.Style.Direction = DirLTR
	inner.Style.Width = Pt(150)
	deepA, deepB := sized(), sized()
	deep := row(deepA, deepB)
	deep.Style.Direction = DirRTL
	deep.Style.Width = Pt(50)
	inherits := sized()
	mid := row(inherits, deep)
	mid.Style.Width = Pt(100)
	inner.AppendChild(mid)
	root := row(inner)
	root.Style.Direction = DirRTL
	root.Style.Width = Pt(200)
	var p Pass
	root.Layout(Size{200, 100}, &p)
	if inner.X != 50 { // placed by its right-to-left parent
		t.Errorf("inner at %v", inner.X)
	}
	if a.X != 0 || b.X != 10 { // its own children run left to right
		t.Errorf("a, b at %v, %v", a.X, b.X)
	}
	if inherits.X != 0 || deep.X != 10 { // inherited from inner
		t.Errorf("inherits, deep at %v, %v", inherits.X, deep.X)
	}
	if deepA.X != 40 || deepB.X != 30 { // right to left again
		t.Errorf("deepA, deepB at %v, %v", deepA.X, deepB.X)
	}
}

func TestDirectionFollowsANewParent(t *testing.T) {
	sized := func() *Node {
		n := New()
		n.Style.Width, n.Style.Height = Pt(10), Pt(10)
		return n
	}
	a, b := sized(), sized()
	moved := row(a, b) // inherits
	moved.Style.Width = Pt(50)
	moved.Style.Height = Pt(10)
	rtl := row(moved)
	rtl.Style.Direction = DirRTL
	ltr := row()
	ltr.Style.Direction = DirLTR
	var pr, pl Pass
	rtl.Layout(Size{100, 100}, &pr)
	if a.X != 40 || b.X != 30 {
		t.Fatalf("right to left: %v, %v", a.X, b.X)
	}
	ltr.Layout(Size{100, 100}, &pl)

	ltr.AppendChild(moved)
	pr, pl = Pass{}, Pass{}
	ltr.Layout(Size{100, 100}, &pl)
	if a.X != 0 || b.X != 10 {
		t.Fatalf("moved left to right: %v, %v", a.X, b.X)
	}
	rtl.AppendChild(moved)
	pl = Pass{}
	rtl.Layout(Size{100, 100}, &pl)
	if a.X != 40 || b.X != 30 {
		t.Fatalf("moved back: %v, %v", a.X, b.X)
	}

	// Laid out by itself, with no parent's direction to read.
	rtl.RemoveChild(moved)
	moved.Layout(Size{50, 10}, &pl)
	if a.X != 0 || b.X != 10 {
		t.Fatalf("no parent: %v, %v", a.X, b.X)
	}
}

func TestLayoutRootWithParentResolvesDirection(t *testing.T) {
	c := New()
	c.Style.Width, c.Style.Height = Pt(10), Pt(10)
	inner := row(c)
	inner.Style.Width = Pt(100)
	mid := col(inner)
	top := col(mid)
	top.Style.Direction = DirRTL // never laid out itself
	var p Pass
	inner.Layout(Size{100, 100}, &p)
	if c.X != 90 {
		t.Fatalf("at %v", c.X)
	}
}

func TestStyleWriteOfDirectionDoesNotAnswerFromCache(t *testing.T) {
	c := New()
	c.Style.Width, c.Style.Height = Pt(10), Pt(10)
	inner := row(c)
	root := col(inner)
	root.Style.Width = Pt(100)
	var p Pass
	root.Layout(Size{100, 100}, &p)
	root.Style.Direction = DirRTL
	root.MarkDirty() // inner and c stay clean: their caches say nothing of it
	root.Layout(Size{100, 100}, &p)
	if c.X != 90 {
		t.Fatalf("at %v", c.X)
	}
}

func TestSameDirectionKeepsCaches(t *testing.T) {
	n := 5
	leaf := label(&n)
	root := row(leaf)
	root.Style.Direction = DirRTL
	var p Pass
	root.Layout(Size{200, 100}, &p)
	if p.Measures == 0 {
		t.Fatal("nothing measured")
	}
	s := root.Style
	s.Padding.Left = Pt(0) // a change, but not of direction
	root.SetStyle(s)
	if leaf.Dirty() {
		t.Fatal("the leaf was invalidated")
	}
	p = Pass{}
	root.Layout(Size{200, 100}, &p)
	if p.Measures != 0 {
		t.Fatalf("%d measures", p.Measures)
	}
}

func TestSteadyStateRTLLayoutAllocatesNothing(t *testing.T) {
	var chars []int
	for i := 0; i < 50; i++ {
		chars = append(chars, i%7+1)
	}
	var kids []*Node
	for i := range chars {
		g := New()
		g.Style.FlexGrow = 1
		g.Style.Margin.Left = Auto
		kids = append(kids, row(label(&chars[i]), g, label(&chars[i])))
	}
	grid := New()
	grid.Style.Display = DisplayGrid
	grid.Style.GridTemplateColumns = []GridTrack{Repeat(3, Fr(1))}
	grid.Style.ColumnGap = Pt(4)
	for i := 0; i < 9; i++ {
		c := label(&chars[i])
		c.Style.JustifySelf = AlignSelfEnd
		c.Style.Direction = DirRTL
		grid.AppendChild(c)
	}
	absolute := New()
	absolute.Style.Position = Absolute
	absolute.Style.Width, absolute.Style.Height = Pt(10), Pt(10)
	absolute.Style.GridColumn = Placement{Line(1), Line(2)}
	grid.AppendChild(absolute)
	blk := New()
	blk.Style.Display = DisplayBlock
	blk.Style.OverflowY = OverflowScroll
	blk.Style.ScrollbarWidth = 15
	blk.AppendChild(label(&chars[0]))
	column := col(label(&chars[1]), label(&chars[2]))
	column.Style.AlignItems = AlignFlexEnd
	column.Style.FlexWrap = WrapReverse
	root := col(append(kids, grid, blk, column)...)
	root.Style.Direction = DirRTL
	var p Pass
	screen(root, 320, 2000, &p)
	if root.Children()[0].Children()[0].X <= 0 {
		t.Fatal("not laid out right to left")
	}
	w := float32(320)
	allocs := testing.AllocsPerRun(200, func() {
		w = 680 - w // alternate widths so everything recomputes
		p.Changed = p.Changed[:0]
		screen(root, w, 2000, &p)
	})
	if allocs != 0 {
		t.Fatalf("%v allocations per layout", allocs)
	}
	// Nor does changing direction.
	allocs = testing.AllocsPerRun(50, func() {
		if root.Style.Direction == DirRTL {
			setDirection(root, DirLTR)
		} else {
			setDirection(root, DirRTL)
		}
		p.Changed = p.Changed[:0]
		screen(root, w, 2000, &p)
	})
	if allocs != 0 {
		t.Fatalf("%v allocations per flip", allocs)
	}
}

func TestDisplayNoneHides(t *testing.T) {
	c := 4
	a, b := label(&c), label(&c)
	b.Style.Display = DisplayNone
	root := col(a, b)
	var p Pass
	screen(root, 100, 100, &p)
	if got := frames(a, b); got != "0,0 100x20 | 0,0 0x0" {
		t.Fatal(got)
	}
}

func BenchmarkRelayoutOneText(b *testing.B) {
	chars := make([]int, 200)
	var kids []*Node
	for i := range chars {
		chars[i] = i%7 + 1
		g := New()
		g.Style.FlexGrow = 1
		kids = append(kids, row(label(&chars[i]), g, label(&chars[i])))
	}
	root := col(kids...)
	var p Pass
	screen(root, 320, 20000, &p)
	b.ReportAllocs()
	for b.Loop() {
		kids[100].Children()[0].MarkDirty()
		p.Changed = p.Changed[:0]
		screen(root, 320, 20000, &p)
	}
}

func BenchmarkFullLayout(b *testing.B) {
	chars := make([]int, 200)
	var kids []*Node
	for i := range chars {
		chars[i] = i%7 + 1
		g := New()
		g.Style.FlexGrow = 1
		kids = append(kids, row(label(&chars[i]), g, label(&chars[i])))
	}
	root := col(kids...)
	var p Pass
	w := float32(320)
	b.ReportAllocs()
	for b.Loop() {
		w = 680 - w
		p.Changed = p.Changed[:0]
		screen(root, w, 20000, &p)
	}
}
