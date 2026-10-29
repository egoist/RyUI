package ui

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"

	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
)

// Rich is styled text: spans in order; a "\n" in a span's text starts a
// new paragraph.
type Rich []Span

// Span is a run of text and how it looks. A field left zero comes from
// the RichText's style (its Font, Color).
type Span struct {
	Text      string
	Font      Font // each zero field (style, size, weight) is the RichText's
	Italic    bool
	Mono      bool  // monospaced, for code
	Color     Color // 0: the RichText's; a link's default is the accent color
	Underline bool
	Strike    bool
	// Background is drawn behind the span on each line it covers, with
	// rounded corners: highlights, inline code.
	Background Color
	// Link is reported to RichTextProps.OnLink when the span is tapped.
	Link string
}

// Plain is a span of text in the RichText's style.
func Plain(text string) Span { return Span{Text: text} }

// Strong is a bold span.
func Strong(text string) Span { return Span{Text: text, Font: Font{Weight: ops.FontWeightBold}} }

// Em is an italic span.
func Em(text string) Span { return Span{Text: text, Italic: true} }

// Code is monospaced text on the secondary background.
func Code(text string) Span { return Span{Text: text, Mono: true, Background: SecondaryBackground} }

// Link is a span that reports url when it's tapped.
func Link(text, url string) Span { return Span{Text: text, Link: url} }

// Colored returns s in color c.
func (s Span) Colored(c Color) Span { s.Color = c; return s }

// Highlighted returns s on background c.
func (s Span) Highlighted(c Color) Span { s.Background = c; return s }

// Weighted returns s with weight w.
func (s Span) Weighted(w Weight) Span { s.Font.Weight = w; return s }

// String returns the text without its styles.
func (r Rich) String() string {
	var b strings.Builder
	for _, s := range r {
		b.WriteString(s.Text)
	}
	return b.String()
}

// RichTextProps configures a RichText.
type RichTextProps struct {
	Text signal.Accessor[Rich]
	// Style's Font, Color, TextAlign and Lines are the text's defaults.
	Style  Styler
	OnLink func(url string)
}

// RichText shows styled paragraphs, typeset by the platform's engine
// (CoreText, StaticLayout) and drawn by a view of nui's own, so that
// what's measured is what's drawn. Each paragraph's layout is cached by
// its content and width: appending to a long text (a streaming reply)
// typesets only the paragraph that changed. For plain text, Text is the
// platform's label, with its native behavior.
func RichText(p RichTextProps) Node {
	e := newElement(ops.KindRichText, true)
	e.measure()
	e.setStyle(p.Style, true)
	if p.OnLink != nil {
		e.handlers().link = p.OnLink
		e.listen(ops.EventLink)
	}
	if p.Text == nil {
		return Node{e}
	}
	var last []byte
	signal.RenderEffect(func() {
		b := appendRich(nil, p.Text.Get())
		if bytes.Equal(b, last) {
			return
		}
		last = b
		e.r.batch.SetBytes(e.id, ops.PropRichTextContent, b)
		e.remeasure()
	})
	return Node{e}
}

// styleKey is a span's look, deduplicated in the encoding.
type styleKey struct {
	font              Font
	flags             byte
	color, background Color
	link              string
}

func appendRich(b []byte, r Rich) []byte {
	le := binary.LittleEndian
	var styles []styleKey
	index := map[styleKey]uint32{}
	type run struct {
		style uint32
		text  string
	}
	paras := [][]run{nil}
	for _, s := range r {
		k := styleKey{font: s.Font, color: s.Color, background: s.Background, link: s.Link}
		if s.Italic {
			k.flags |= 1
		}
		if s.Mono {
			k.flags |= 2
		}
		if s.Underline {
			k.flags |= 4
		}
		if s.Strike {
			k.flags |= 8
		}
		i, ok := index[k]
		if !ok {
			i = uint32(len(styles))
			index[k] = i
			styles = append(styles, k)
		}
		for n, part := range strings.Split(s.Text, "\n") {
			if n > 0 {
				paras = append(paras, nil)
			}
			if part != "" {
				last := &paras[len(paras)-1]
				*last = append(*last, run{i, part})
			}
		}
	}
	b = le.AppendUint32(b, uint32(len(styles)))
	for _, k := range styles {
		b = append(b, byte(k.font.Style))
		b = le.AppendUint32(b, math.Float32bits(k.font.Size))
		b = append(b, byte(k.font.Weight), k.flags)
		b = le.AppendUint64(b, uint64(k.color))
		b = le.AppendUint64(b, uint64(k.background))
		b = le.AppendUint32(b, uint32(len(k.link)))
		b = append(b, k.link...)
	}
	b = le.AppendUint32(b, uint32(len(paras)))
	for _, p := range paras {
		b = le.AppendUint32(b, uint32(len(p)))
		for _, r := range p {
			b = le.AppendUint32(b, r.style)
			b = le.AppendUint32(b, uint32(len(r.text)))
			b = append(b, r.text...)
		}
	}
	return b
}
