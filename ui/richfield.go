package ui

import (
	"encoding/binary"
	"math"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// TextFormat is styles of a Rich TextField's text, as bits.
type TextFormat uint8

const (
	FormatBold      TextFormat = 1 << iota // weight Bold (Semibold or more reads as bold)
	FormatItalic                           // Span.Italic
	FormatUnderline                        // Span.Underline
	FormatStrike                           // Span.Strike
	FormatMono                             // Span.Mono: code
)

// TextFormats is what styles a Rich TextField has where its cursor is
// (what's typed next), or that all its selection has.
type TextFormats struct {
	Format TextFormat
	Link   string
}

// Has reports whether every one of f is on.
func (t TextFormats) Has(f TextFormat) bool { return t.Format&f == f }

// FormatText turns styles on (on) or off for a Rich TextField's
// selection; with no selection, for what's typed next at the cursor.
func FormatText(field Node, f TextFormat, on bool) {
	how := byte(0)
	if on {
		how = 1
	}
	formatCmd(field, f, how)
}

// ToggleFormat turns styles on for a Rich TextField's selection unless
// all of it has them, then off (a toolbar's Bold button); with no
// selection, for what's typed next at the cursor.
func ToggleFormat(field Node, f TextFormat) { formatCmd(field, f, 2) }

func formatCmd(field Node, f TextFormat, how byte) {
	if e := field.e; e != nil && e.kind == ops.KindTextField {
		e.r.batch.Command(e.id, ops.CmdFormat, []byte{byte(f), how})
	}
}

// SetLink links a Rich TextField's selection to url ("" removes the
// link); with no selection, the link the cursor is in.
func SetLink(field Node, url string) {
	if e := field.e; e != nil && e.kind == ops.KindTextField {
		e.r.batch.Command(e.id, ops.CmdSetLink, []byte(url))
	}
}

// richField keeps a field's styled text and rich in step, as Value is
// kept: the user's edits write rich, and rich's changes replace the
// field's text, except with what the field last said it had.
//
// sent is called with the plain text of what's sent: Value follows it,
// as it follows the user's typing.
func richField(e *element, rich *signal.Signal[Rich], sent func(string)) {
	id := e.id
	var reported []byte
	known := false
	signal.RenderEffect(func() {
		r := rich.Get()
		b := appendRich(nil, r)
		if known && string(b) == string(reported) {
			return
		}
		reported, known = b, true
		e.r.batch.SetBytes(id, ops.PropTextFieldRich, b)
		e.remeasure()
		if sent != nil {
			signal.Untrack(func() struct{} { sent(r.String()); return struct{}{} })
		}
	})
	e.handlers().rich = func(b []byte) {
		reported, known = append([]byte(nil), b...), true
		rich.Set(decodeRich(b))
		e.remeasure() // its height is its text's
	}
	e.listen(ops.EventRichChanged)
}

// decodeRich reads what appendRich writes (an editing host's text): a
// span per run, each paragraph after the first starting a new line. It
// reads what it can of a short one.
func decodeRich(b []byte) Rich {
	r := reader{b: b}
	n := r.u32()
	styles := make([]Span, 0, min(n, 64))
	for range n {
		if r.bad {
			return nil
		}
		var s Span
		s.Font.Style = ops.TextStyle(r.u8())
		s.Font.Size = math.Float32frombits(r.u32())
		s.Font.Weight = ops.FontWeight(r.u8())
		flags := r.u8()
		s.Italic, s.Mono, s.Underline, s.Strike = flags&1 != 0, flags&2 != 0, flags&4 != 0, flags&8 != 0
		s.Color = Color(r.u64())
		s.Background = Color(r.u64())
		s.Link = r.str()
		styles = append(styles, s)
	}

	var out Rich
	paras := r.u32()
	for p := range paras {
		if r.bad {
			break
		}
		if p > 0 {
			// The line break: the last run's, else a span of its own.
			if k := len(out); k > 0 && out[k-1].Inline == nil {
				out[k-1].Text += "\n"
			} else {
				out = append(out, Span{Text: "\n"})
			}
		}
		runs := r.u32()
		for range runs {
			i := r.u32()
			text := r.str()
			if r.bad || int(i) >= len(styles) {
				break
			}
			s := styles[i]
			s.Text = text
			out = append(out, s)
		}
	}
	return out
}

// reader reads little-endian values, noting a short buffer.
type reader struct {
	b   []byte
	bad bool
}

func (r *reader) take(n int) []byte {
	if r.bad || n < 0 || n > len(r.b) {
		r.bad = true
		return nil
	}
	v := r.b[:n]
	r.b = r.b[n:]
	return v
}

func (r *reader) u8() byte {
	if v := r.take(1); v != nil {
		return v[0]
	}
	return 0
}

func (r *reader) u32() uint32 {
	if v := r.take(4); v != nil {
		return binary.LittleEndian.Uint32(v)
	}
	return 0
}

func (r *reader) u64() uint64 {
	if v := r.take(8); v != nil {
		return binary.LittleEndian.Uint64(v)
	}
	return 0
}

func (r *reader) str() string { return string(r.take(int(r.u32()))) }

// decodeFormats reads a Formats event.
func decodeFormats(b []byte) TextFormats {
	r := reader{b: b}
	f := TextFormats{Format: TextFormat(r.u8())}
	f.Link = r.str()
	return f
}
