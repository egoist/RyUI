package ui_test

import (
	"testing"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A canvas draws at its laid-out size, again when what it read changes,
// and sends only a drawing that differs; drawing again allocates nothing.
func TestCanvas(t *testing.T) {
	level := signal.New(float32(0.5))
	other := signal.New(0)
	draws := 0
	h, r := mount(t, func() ui.Node {
		return ui.Canvas(func(g *ui.Graphics) {
			draws++
			other.Get()
			w, ht := g.Size()
			g.Fill(g.Path().RoundedRect(0, 0, w, ht, 8), colors.SecondaryBackground)
			g.Fill(g.Path().Rect(0, 0, w*level.Get(), ht), colors.Hex(0x34c759))
			g.Stroke(g.Path().MoveTo(0, ht).LineTo(w, 0), colors.Label, ui.StrokeStyle{Width: 2, Dash: []float32{4, 2}})
			g.Text("50%", w/2, ht/2, ui.TextStyle{Align: ui.TextCenter})
		}, ui.Style{Width: ui.Pt(200), Height: ui.Pt(40)})
	})
	c := h.OfKind(ops.KindCanvas)[0]
	first := append([]byte(nil), c.Props[ops.PropCanvasCommands].Bytes...)
	if draws != 1 || len(first) == 0 || ops.CanvasOp(first[0]) != ops.CanvasOpFill {
		t.Fatalf("draws %d, commands %v", draws, first)
	}
	h.Reset()
	other.Set(1) // draws again, the same drawing: nothing sent
	r.Flush()
	if draws != 2 || h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("draws %d, sets %d", draws, h.Count(ops.OpSetProp))
	}
	level.Set(0.75)
	r.Flush()
	if h.Count(ops.OpSetProp) != 1 {
		t.Fatalf("sets %d", h.Count(ops.OpSetProp))
	}
	if a := testing.AllocsPerRun(50, func() {
		other.Set(other.Peek() + 1)
		r.Flush()
	}); a > 0 {
		t.Fatalf("%v allocations per redraw", a)
	}
}
