//go:build linux && !android

package linux

import (
	"os"
	"strings"
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func fieldNode(t *testing.T, h *Host) *node {
	fs := find(h, ops.KindTextField)
	if len(fs) != 1 {
		fatalf(t, "%d fields", len(fs))
	}
	return fs[0]
}

// gtkSelection is the field's selection as GTK has it, in characters.
func gtkSelection(n *node) (int, int) {
	f := n.field
	if f.editable != 0 {
		var s, e int32
		if !EditableGetSelectionBounds(f.editable, &s, &e) {
			p := EditableGetPosition(f.editable)
			return int(p), int(p)
		}
		return int(s), int(e)
	}
	var a, b TextIter
	TextBufferGetSelectionBounds(f.buffer, &a, &b)
	return int(TextIterGetOffset(&a)), int(TextIterGetOffset(&b))
}

// selectChars selects characters [s, e) as the user does.
func selectChars(n *node, s, e int) {
	f := n.field
	if f.editable != 0 {
		EditableSelectRegion(f.editable, int32(s), int32(e))
		return
	}
	var a, b TextIter
	TextBufferGetIterAtOffset(f.buffer, &a, int32(e))
	TextBufferGetIterAtOffset(f.buffer, &b, int32(s))
	TextBufferSelectRange(f.buffer, &a, &b)
}

// The selection crosses as UTF-16 (the core's) and characters (GTK's):
// an emoji is two units and one character. Set from code it lands on the
// right characters; the user's is reported in the core's offsets;
// InsertText goes in at it and is reported as typing.
func TestFieldSelectionUTF16(t *testing.T) {
	for _, multiline := range []bool{false, true} {
		text := signal.New("a😀b")
		sel := signal.New(ui.TextRange{})
		var field ui.Node
		h, r := mount(t, func() ui.Node {
			field = ui.TextField(ui.TextFieldProps{Value: text, Selection: sel, Multiline: multiline})
			return ui.Column(ui.Style{Padding: ui.All(16)}, field)
		})
		var n *node
		onMain(func() {
			n = fieldNode(t, h)
			h.focusField(n)
			// "b": bytes 5..6, UTF-16 3..4, characters 2..3.
			h.enter(func() { sel.Set(ui.TextRange{Start: 5, End: 6}); r.Flush() })
			if s, e := gtkSelection(n); s != 2 || e != 3 {
				fatalf(t, "multiline %v: GTK's selection is %d..%d", multiline, s, e)
			}
			// The user selects the emoji: bytes 1..5.
			selectChars(n, 1, 2)
		})
		settle(t, h, func() bool { return sel.Peek() == ui.TextRange{Start: 1, End: 5} })
		onMain(func() {
			if n.field.selStart != 1 || n.field.selEnd != 3 {
				fatalf(t, "multiline %v: reported %d..%d", multiline, n.field.selStart, n.field.selEnd)
			}
			h.enter(func() { ui.InsertText(field, "👍🏽"); r.Flush() })
		})
		settle(t, h, func() bool {
			return text.Peek() == "a👍🏽b" && sel.Peek() == ui.TextRange{Start: 9, End: 9}
		})
	}
}

// A multiline field is as tall as its text, as the text view lays it
// out: it grows with lines typed, and stops at its line limit.
func TestFieldMultilineGrows(t *testing.T) {
	text := signal.New("one")
	var field ui.Node
	h, r := mount(t, func() ui.Node {
		field = ui.TextField(ui.TextFieldProps{Value: text, Multiline: true, Placeholder: ui.Val("Message"), Style: ui.Style{Lines: 4}})
		return ui.Column(ui.Style{Padding: ui.All(16), Width: ui.Pt(240)}, field)
	})
	var n *node
	var one float32
	onMain(func() { n = fieldNode(t, h) })
	// The text view's own height (its layout at its width, its margins).
	viewHeight := func() float32 {
		var mn, nat int32
		Call(FnWidgetMeasure, n.field.view, OrientationVertical, uintptr(Call(FnWidgetGetWidth, n.field.view)), Addr(&mn), Addr(&nat), 0, 0)
		return float32(nat)
	}
	settle(t, h, func() bool { return n.placed && near(viewHeight(), n.frame[3], 1) })
	onMain(func() {
		one = n.frame[3]
		if WidgetGetVisible(n.field.hint) {
			fatalf(t, "the placeholder shows over text")
		}
		h.focusField(n)
		var e TextIter
		TextBufferGetEndIter(n.field.buffer, &e)
		TextBufferSelectRange(n.field.buffer, &e, &e)
		h.enter(func() { ui.InsertText(field, "\ntwo\nthree"); r.Flush() })
	})
	settle(t, h, func() bool {
		return text.Peek() == "one\ntwo\nthree" && n.frame[3] > one+20 && near(viewHeight(), n.frame[3], 1)
	})
	three := n.frame[3]
	// Past its four lines it scrolls.
	onMain(func() { h.enter(func() { ui.InsertText(field, "\nfour\nfive\nsix"); r.Flush() }) })
	settle(t, h, func() bool {
		return strings.HasSuffix(text.Peek(), "six") && n.frame[3] > three && n.frame[3] < viewHeight()-10
	})
	// Empty: the placeholder shows.
	onMain(func() { h.enter(func() { text.Set(""); r.Flush() }) })
	settle(t, h, func() bool { return WidgetGetVisible(n.field.hint) && near(n.frame[3], one, 1) })
}

// A Rich field shows its styles as tags, reports what it has as sent
// (the same encoding), formats its selection with Format and Ctrl+I,
// takes the style before the cursor in what's typed, and reports
// Formats.
func TestFieldRich(t *testing.T) {
	rich := signal.NewFunc(ui.Rich{ui.Plain("Hello "), ui.Strong("world"), ui.Plain("\nbye")}, nil)
	var formats []ui.TextFormats
	var field ui.Node
	h, r := mount(t, func() ui.Node {
		field = ui.TextField(ui.TextFieldProps{Rich: rich, OnFormats: func(f ui.TextFormats) { formats = append(formats, f) }})
		return ui.Column(ui.Style{Padding: ui.All(16)}, field)
	})
	var n *node
	onMain(func() {
		n = fieldNode(t, h)
		if n.field.view == 0 || h.fieldText(n) != "Hello world\nbye" {
			fatalf(t, "the text is %q", h.fieldText(n))
		}
		// What it has is what was sent.
		if got, want := h.encodeRichField(n), ops.AppendRich(nil, []ops.RichRun{
			{Text: "Hello "}, {Style: ops.RichStyle{Weight: ops.FontWeightBold}, Text: "world"}, {Text: "\nbye"},
		}); string(got) != string(want) {
			fatalf(t, "encoded %v, want %v", got, want)
		}
		h.focusField(n)
		// "Hello" italic, from code.
		selectChars(n, 0, 5)
		h.enter(func() { ui.ToggleFormat(field, ui.FormatItalic); r.Flush() })
	})
	settle(t, h, func() bool {
		got := rich.Peek()
		return len(got) >= 3 && got[0].Text == "Hello" && got[0].Italic && got[2].Text == "world\n" && got[2].Font.Weight == ui.Bold
	})
	onMain(func() {
		if len(formats) == 0 || !formats[len(formats)-1].Has(ui.FormatItalic) {
			fatalf(t, "formats %+v", formats)
		}
		// Ctrl+I takes it off again.
		if !h.richKey(n, 'i', ModControl) {
			fatalf(t, "Ctrl+I isn't taken")
		}
	})
	settle(t, h, func() bool { got := rich.Peek(); return len(got) > 0 && got[0].Text == "Hello " && !got[0].Italic })
	// Typed after "world": bold, as what's before it.
	onMain(func() {
		selectChars(n, 11, 11)
		h.enter(func() { ui.InsertText(field, "!"); r.Flush() })
	})
	settle(t, h, func() bool {
		got := rich.Peek()
		return len(got) >= 2 && got[1].Text == "world!\n" && got[1].Font.Weight == ui.Bold
	})
	// A link on "bye", then the cursor in it: Formats has the link.
	onMain(func() {
		selectChars(n, 13, 16)
		h.enter(func() { ui.SetLink(field, "https://example.com"); r.Flush() })
		selectChars(n, 14, 14)
	})
	settle(t, h, func() bool {
		got := rich.Peek()
		last := formats[len(formats)-1]
		return len(got) > 0 && got[len(got)-1].Link == "https://example.com" && got[len(got)-1].Text == "bye" && last.Link == "https://example.com"
	})
	// Set from code: replaced, with no event back.
	onMain(func() {
		h.enter(func() { rich.Set(ui.Rich{ui.Plain("new "), ui.Code("code")}); r.Flush() })
	})
	settle(t, h, func() bool { return h.fieldText(n) == "new code" })
	onMain(func() {
		var it TextIter
		TextBufferGetIterAtOffset(n.field.buffer, &it, 5)
		if s, _ := h.styleAt(n, &it); s.Flags&2 == 0 {
			fatalf(t, "code's style %+v", s)
		}
	})
}

// A field that takes images gets a pasted image as a PNG file, reported
// with Pasted instead of going into the text; text pastes as text.
func TestFieldPasteImage(t *testing.T) {
	var pasted []ui.SharedItem
	text := signal.New("")
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{Padding: ui.All(16)},
			ui.TextField(ui.TextFieldProps{Value: text, PasteTypes: []string{"image/*"}, OnPaste: func(items []ui.SharedItem) { pasted = append(pasted, items...) }}))
	})
	var n *node
	onMain(func() {
		n = fieldNode(t, h)
		h.focusField(n)
		px := []byte{255, 0, 0, 255, 0, 255, 0, 255, 0, 0, 255, 255, 255, 255, 255, 255}
		b := BytesNew(&px[0], uintptr(len(px)))
		tex := MemoryTextureNew(2, 2, MemoryR8G8B8A8, b, 8)
		BytesUnref(b)
		cb := DisplayGetClipboard(DisplayGetDefault())
		Call(Sym("gdk_clipboard_set_texture"), cb, tex)
		ObjectUnref(tex)
		s := CString("paste-clipboard")
		Call(Sym("g_signal_emit_by_name"), EditableGetDelegate(n.field.editable), Addr(s))
	})
	settle(t, h, func() bool { return len(pasted) == 1 })
	onMain(func() {
		it := pasted[0]
		if it.MIME != "image/png" || !strings.HasSuffix(it.Path, ".png") || text.Peek() != "" {
			fatalf(t, "pasted %+v, text %q", it, text.Peek())
		}
		if b, err := os.ReadFile(it.Path); err != nil || len(b) < 8 || string(b[1:4]) != "PNG" {
			fatalf(t, "the file: %v", err)
		}
		os.RemoveAll(it.Path[:strings.LastIndexByte(it.Path, '/')])
		// Text isn't the field's content: it pastes.
		ClipboardSetText(DisplayGetClipboard(DisplayGetDefault()), "plain")
		s := CString("paste-clipboard")
		Call(Sym("g_signal_emit_by_name"), EditableGetDelegate(n.field.editable), Addr(s))
	})
	settle(t, h, func() bool { return text.Peek() == "plain" && len(pasted) == 1 })
}
