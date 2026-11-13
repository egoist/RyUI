package ui_test

import (
	"encoding/binary"
	"testing"

	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

func selBytes(start, end uint32) []byte {
	b := binary.LittleEndian.AppendUint32(nil, start)
	return binary.LittleEndian.AppendUint32(b, end)
}

// A field's selection: the user's moves write the signal in bytes of the
// text (the host counts UTF-16), writing it moves the field's, and a
// move never round-trips.
func TestTextFieldSelection(t *testing.T) {
	text := signal.New("héllo 👋 world")
	sel := signal.New(ui.TextRange{})
	var field ui.Node
	h, r := mount(t, func() ui.Node {
		field = ui.TextField(ui.TextFieldProps{Value: text, Selection: sel})
		return field
	})
	f := h.OfKind(ops.KindTextField)[0]
	if !f.Listens[ops.EventSelectionChanged] {
		t.Fatal("not listening for the selection")
	}
	// After the emoji: UTF-16 offset 9 ("héllo " 6, 👋 2, then 1 more).
	h.Reset()
	h.Dispatch(ops.Event{ID: f.ID, Kind: ops.EventSelectionChanged, Bytes: selBytes(8, 8)})
	if got := sel.Peek(); got != (ui.TextRange{11, 11}) || text.Peek()[:11] != "héllo 👋" {
		t.Fatalf("selection %+v", got)
	}
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("the user's move went back\n%s", h.Log())
	}
	// "world" selected from code.
	sel.Set(ui.TextRange{12, 17})
	r.Flush()
	if b := f.Props[ops.PropTextFieldSelection].Bytes; string(b) != string(selBytes(9, 14)) {
		t.Fatalf("selection prop %v", b)
	}
	// Inserting at the cursor is the host's, as typing.
	ui.InsertText(field, "🌍")
	r.Flush()
	if c := h.Commands(ops.CmdInsertText); len(c) != 1 || string(c[0].Value.Bytes) != "🌍" {
		t.Fatalf("insert %v", c)
	}
}

// Pastes of the field's types go to OnPaste.
func TestTextFieldPaste(t *testing.T) {
	var got []ui.SharedItem
	h, _ := mount(t, func() ui.Node {
		return ui.TextField(ui.TextFieldProps{Value: signal.New(""), PasteTypes: []string{"image/*"},
			OnPaste: func(items []ui.SharedItem) { got = items }})
	})
	f := h.OfKind(ops.KindTextField)[0]
	if b := f.Props[ops.PropTextFieldPasteTypes].Bytes; len(b) != 4+4+7 {
		t.Fatalf("paste types %v", b)
	}
	b := ops.AppendItems(nil, []ops.Item{{Kind: ops.ShareKindImage, Path: "/tmp/a.gif", Name: "a.gif", MIME: "image/gif", Size: 10}})
	h.Dispatch(ops.Event{ID: f.ID, Kind: ops.EventPasted, Bytes: b})
	if len(got) != 1 || got[0].Path != "/tmp/a.gif" || got[0].MIME != "image/gif" {
		t.Fatalf("pasted %+v", got)
	}
}

// A Rich field: its styled text goes to the host in RichText's encoding,
// the user's edits come back the same way, and an edit never goes back.
func TestTextFieldRich(t *testing.T) {
	rich := signal.NewFunc(ui.Rich{ui.Plain("Hello "), ui.Strong("world"), ui.Plain("\nsecond "), ui.Link("line", "https://go.dev")}, nil)
	var got []ui.TextFormats
	var field ui.Node
	h, r := mount(t, func() ui.Node {
		field = ui.TextField(ui.TextFieldProps{Rich: rich, OnFormats: func(f ui.TextFormats) { got = append(got, f) }})
		return field
	})
	f := h.OfKind(ops.KindTextField)[0]
	if !f.Bool(ops.PropTextFieldMultiline) {
		t.Fatal("a rich field isn't multiline")
	}
	sent := f.Props[ops.PropTextFieldRich].Bytes
	if len(sent) == 0 {
		t.Fatal("no rich text sent")
	}
	// The user makes "Hello" italic: the host reports the new text.
	edited := ui.Rich{ui.Em("Hello"), ui.Plain(" "), ui.Strong("world")}
	b := ui.EncodeRich(edited)
	h.Reset()
	h.Dispatch(ops.Event{ID: f.ID, Kind: ops.EventRichChanged, Bytes: b})
	if h.Count(ops.OpSetProp) != 0 {
		t.Fatalf("the user's edit went back\n%s", h.Log())
	}
	if s := rich.Peek(); len(s) != 3 || !s[0].Italic || s[0].Text != "Hello" || s[2].Font.Weight != fonts.Bold {
		t.Fatalf("rich %+v", s)
	}
	// Decoding is canonical: the paragraphs, the link.
	two := ui.EncodeRich(ui.Rich{ui.Plain("a\n"), ui.Plain("\n"), ui.Link("b", "u")})
	h.Dispatch(ops.Event{ID: f.ID, Kind: ops.EventRichChanged, Bytes: two})
	if string(ui.EncodeRich(rich.Peek())) != string(two) {
		t.Fatalf("not canonical: %+v", rich.Peek())
	}
	// Code replaces it.
	rich.Set(ui.Rich{ui.Plain("new")})
	r.Flush()
	if string(f.Props[ops.PropTextFieldRich].Bytes) != string(ui.EncodeRich(ui.Rich{ui.Plain("new")})) {
		t.Fatal("not sent")
	}
	// Formatting is the host's; the styles at the cursor come back.
	ui.ToggleFormat(field, ui.FormatBold|ui.FormatItalic)
	ui.SetLink(field, "https://go.dev")
	r.Flush()
	if c := h.Commands(ops.CmdFormat); len(c) != 1 || c[0].Value.Bytes[0] != 3 || c[0].Value.Bytes[1] != 2 {
		t.Fatalf("format %v", c)
	}
	if c := h.Commands(ops.CmdSetLink); len(c) != 1 || string(c[0].Value.Bytes) != "https://go.dev" {
		t.Fatalf("link %v", c)
	}
	fb := append([]byte{byte(ui.FormatBold)}, 0, 0, 0, 0)
	h.Dispatch(ops.Event{ID: f.ID, Kind: ops.EventFormats, Bytes: fb})
	if len(got) != 1 || !got[0].Has(ui.FormatBold) || got[0].Has(ui.FormatItalic) {
		t.Fatalf("formats %+v", got)
	}
}

// The Go hosts encode styled text as the core does: ops.AppendRich and
// the core's encoding agree, byte for byte, and DecodeRich reads it back.
func TestRichEncodingsAgree(t *testing.T) {
	r := ui.Rich{ui.Plain("Hi,\n\nThe "), ui.Strong("1.0"), ui.Plain(" is "), ui.Em("out"), ui.Plain(".\n"), ui.Link("notes", "https://x"), ui.Code("go")}
	var runs []ops.RichRun
	for _, s := range r {
		var st ops.RichStyle
		st.TextStyle, st.Size, st.Weight = s.Font.Style, s.Font.Size, s.Font.Weight
		if s.Italic {
			st.Flags |= 1
		}
		if s.Mono {
			st.Flags |= 2
		}
		st.Color, st.Background, st.Link = s.Color, s.Background, s.Link
		runs = append(runs, ops.RichRun{Style: st, Text: s.Text})
	}
	a, b := ui.EncodeRich(r), ops.AppendRich(nil, runs)
	if string(a) != string(b) {
		t.Fatalf("the core's\n%v\nops'\n%v", a, b)
	}
	if again := ops.AppendRich(nil, ops.DecodeRich(b)); string(again) != string(b) {
		t.Fatalf("decoded and encoded again\n%v", again)
	}
}

// Value follows a Rich field's text when code sets it, as when the user
// types it, and the selection is read against it.
func TestTextFieldRichValue(t *testing.T) {
	rich := signal.NewFunc(ui.Rich{ui.Plain("draft")}, nil)
	value := signal.New("")
	_, r := mount(t, func() ui.Node { return ui.TextField(ui.TextFieldProps{Rich: rich, Value: value}) })
	if value.Peek() != "draft" {
		t.Fatalf("value %q", value.Peek())
	}
	rich.Set(nil)
	r.Flush()
	if value.Peek() != "" {
		t.Fatalf("value %q after clearing", value.Peek())
	}
}
