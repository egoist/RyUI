//go:build windows

package windows

import (
	"fmt"
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A field's selection set from code moves the EDIT's (a multiline one's
// \r\n counted as the core's \n); InsertText goes in at it, as typing,
// and the field reports the text and where the cursor went.
func TestTextFieldSelectionAndInsert(t *testing.T) {
	for _, multiline := range []bool{false, true} {
		text := signal.New("hello\nworld")
		if !multiline {
			text.Set("hello world")
		}
		sel := signal.New(ui.TextRange{})
		var field ui.Node
		h, r := mount(t, func() ui.Node {
			field = ui.TextField(ui.TextFieldProps{Value: text, Selection: sel, Multiline: multiline})
			return field
		})
		onMain(t, func() {
			n := find(h, ops.KindTextField)[0]
			h.enter(func() {
				sel.Set(ui.TextRange{Start: 6, End: 6})
				r.Flush()
			})
			s, e := editSelection(n.ctl)
			want := uint32(6)
			if multiline {
				want = 7 // after "hello\r\n"
			}
			if s != want || e != want {
				fatalf("multiline %v: the EDIT's selection is %d-%d", multiline, s, e)
			}
			h.enter(func() {
				ui.InsertText(field, "big ")
				r.Flush()
			})
			h.runPosted()
			if got := text.Peek(); multiline && got != "hello\nbig world" || !multiline && got != "hello big world" {
				fatalf("multiline %v: the text is %q", multiline, got)
			}
			if got := sel.Peek(); got != (ui.TextRange{Start: 10, End: 10}) {
				fatalf("multiline %v: the selection is %+v", multiline, got)
			}
		})
	}
}

// A Rich field is a rich edit: its styled text goes in as RTF and comes
// back the same; Format sets the selection's character format.
func TestTextFieldRich(t *testing.T) {
	rich := signal.NewFunc(ui.Rich{ui.Plain("Hello "), ui.Strong("world"), ui.Plain("\n"), ui.Link("go", "https://go.dev")}, nil)
	var field ui.Node
	h, r := mount(t, func() ui.Node {
		field = ui.TextField(ui.TextFieldProps{Rich: rich})
		return field
	})
	onMain(t, func() {
		n := find(h, ops.KindTextField)[0]
		if !n.richText || h.richPlain(n) != "Hello world\ngo" {
			fatalf("rich %v, the text is %q", n.richText, h.richPlain(n))
		}
		runs := h.richRuns(n)
		if len(runs) < 3 || !runs[1].Bold || runs[len(runs)-1].Link != "https://go.dev" {
			fatalf("runs %s", fmt.Sprint(runs))
		}
		sel := [2]int32{0, 5}
		SendMessage(n.ctl, emExSetSel, 0, Ptr(&sel))
		h.enter(func() {
			ui.ToggleFormat(field, ui.FormatItalic)
			r.Flush()
		})
		if got := rich.Peek(); len(got) == 0 || got[0].Text != "Hello" || !got[0].Italic {
			fatalf("rich %+v", got)
		}
	})
}
