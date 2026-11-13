package ui_test

import (
	"testing"

	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A Sheet's and a Window's content is their one child, as the schema
// says, even a plain Column of several (which makes no view of its own)
// or a Show: it goes in a View that fills them.
func TestOneChildContent(t *testing.T) {
	h := fake.New()
	open := signal.New(true)
	r := ui.Mount(h, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Sheet(ui.SheetProps{Open: open, Content: func() ui.Node {
				return ui.Column(ui.Style{}, ui.StaticText("a"), ui.StaticText("b"))
			}}),
			ui.Window(ui.WindowProps{Open: open, Content: func() ui.Node {
				return ui.Show(open, func() ui.Node { return ui.StaticText("c") })
			}}),
		)
	})
	defer r.Dispose()
	r.Flush()
	for _, k := range []ops.Kind{ops.KindSheet, ops.KindWindow} {
		vs := h.OfKind(k)
		if len(vs) != 1 {
			t.Fatalf("%d %ss", len(vs), k)
		}
		v := vs[0]
		if len(v.Children) != 1 || v.Children[0].Kind != ops.KindView {
			t.Fatalf("a %s's children: %d", k, len(v.Children))
		}
		c := v.Children[0]
		if c.Frame[2] != v.Frame[2] && v.Frame[2] != 0 {
			t.Fatalf("a %s's content %v doesn't fill it %v", k, c.Frame, v.Frame)
		}
	}
	sheet := h.OfKind(ops.KindSheet)[0].Children[0]
	if len(sheet.Children) != 2 {
		t.Fatalf("the sheet's texts: %d", len(sheet.Children))
	}
	open.Set(false)
	r.Flush()
	if len(h.OfKind(ops.KindSheet)) != 0 || len(h.OfKind(ops.KindWindow)) != 0 {
		t.Fatal("still open")
	}
}
