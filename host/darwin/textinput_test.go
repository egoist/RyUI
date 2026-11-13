//go:build darwin && !ios

package darwin

import (
	"testing"

	"github.com/egoist/asuka/fonts"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A field's selection set from code moves its editor's; InsertText goes
// in at it, as typing, and the field reports the text and where the
// cursor went.
func TestTextFieldSelectionAndInsert(t *testing.T) {
	for _, multiline := range []bool{false, true} {
		text := signal.New("hello world")
		sel := signal.New(ui.TextRange{})
		var field ui.Node
		h, r := mount(t, func() ui.Node {
			field = ui.TextField(ui.TextFieldProps{Value: text, Selection: sel, Multiline: multiline})
			return field
		})
		onMain(func() {
			n := find(h, ops.KindTextField)[0]
			Send(Send(n.view, "window"), "makeKeyAndOrderFront:", 0)
			Send(Send(n.view, "window"), "makeFirstResponder:", uintptr(n.view))
			if n.tracking != 0 {
				Send(Send(n.view, "window"), "makeFirstResponder:", uintptr(n.tracking))
			}
			h.enter(func() {
				sel.Set(ui.TextRange{Start: 5, End: 5})
				r.Flush()
			})
			tv := h.editing(n)
			if tv == 0 {
				fatal(t, "the field isn't being edited")
			}
			if got := MsgRange(tv, Sel("selectedRange")); got.Location != 5 || got.Length != 0 {
				fatalf(t, "multiline %v: the editor's selection is %+v", multiline, got)
			}
			h.enter(func() {
				ui.InsertText(field, ",")
				r.Flush()
			})
		})
		// The selection is reported a turn later.
		onMain(func() {
			h.runPosted()
			if text.Peek() != "hello, world" {
				fatalf(t, "multiline %v: the text is %q", multiline, text.Peek())
			}
			if got := sel.Peek(); got != (ui.TextRange{Start: 6, End: 6}) {
				fatalf(t, "multiline %v: the selection is %+v", multiline, got)
			}
		})
	}
}

// A Rich field shows its styles, formats its selection from code (the
// text view's undo takes it back), and reports the user's styled text in
// the core's encoding.
func TestTextFieldRich(t *testing.T) {
	rich := signal.NewFunc(ui.Rich{ui.Plain("Hello "), ui.Strong("world"), ui.Plain("\nbye")}, nil)
	var formats []ui.TextFormats
	var field ui.Node
	h, r := mount(t, func() ui.Node {
		field = ui.TextField(ui.TextFieldProps{Rich: rich, OnFormats: func(f ui.TextFormats) { formats = append(formats, f) }})
		return field
	})
	onMain(func() {
		n := find(h, ops.KindTextField)[0]
		tv := n.tracking
		if tv == 0 || GoString(Send(tv, "string")) != "Hello world\nbye" {
			fatalf(t, "the text is %q", GoString(Send(tv, "string")))
		}
		Send(Send(tv, "window"), "makeFirstResponder:", uintptr(tv))
		// "Hello" italic, from code.
		Send(tv, "setSelectedRange:", 0, 5)
		h.enter(func() {
			ui.ToggleFormat(field, ui.FormatItalic)
			r.Flush()
		})
		got := rich.Peek()
		if len(got) < 3 || got[0].Text != "Hello" || !got[0].Italic || got[2].Text != "world\n" || got[2].Font.Weight != fonts.Bold {
			fatalf(t, "rich %+v", got)
		}
		if len(formats) == 0 || !formats[len(formats)-1].Has(ui.FormatItalic) {
			fatalf(t, "formats %+v", formats)
		}
		// Undone, as the text view's own changes are.
		Send(Send(tv, "undoManager"), "undo")
		if got := rich.Peek(); got[0].Italic {
			fatalf(t, "after undo %+v", got)
		}
	})
}

// A one-line field's padding is inside it: its cell's drawing rect (its
// text's, and its field editor's) is inset by it.
func TestTextFieldPadding(t *testing.T) {
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Align: ui.AlignStart},
			ui.TextField(ui.TextFieldProps{Value: signal.New("hi"), Style: ui.Style{Width: ui.Pt(200), Padding: ui.Edges{Left: 30, Top: 4}}}),
			ui.TextField(ui.TextFieldProps{Value: signal.New("hi"), Style: ui.Style{Width: ui.Pt(200)}}))
	})
	onMain(func() {
		fs := find(h, ops.KindTextField)
		rect := func(n *node) Rect {
			return MsgRectToRect(Send(n.view, "cell"), Sel("drawingRectForBounds:"), MsgRect(n.view, Sel("bounds")))
		}
		var padded, plain *node
		for _, n := range fs {
			if n.inset[3] == 30 {
				padded = n
			} else {
				plain = n
			}
		}
		if padded == nil || plain == nil {
			fatalf(t, "insets %v %v", fs[0].inset, fs[1].inset)
		}
		a, b := rect(padded), rect(plain)
		if a.Origin.X-b.Origin.X != 30 || a.Size.Width != b.Size.Width-30 {
			fatalf(t, "padded %+v, plain %+v", a, b)
		}
	})
}
