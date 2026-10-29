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
