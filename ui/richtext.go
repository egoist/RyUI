package ui

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"

	"github.com/egoist/nui/layout"
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
	// Inline makes the span a component in the text instead (Text is
	// ignored): see Embed.
	Inline *Inline
}

// Inline is a component in a RichText's text, where its span is: laid
// out at its own size (at most the width of the screen), it sits on the
// baseline and wraps with the words, as an image in a line of text.
type Inline struct {
	// Key is which component it is from one Text to the next: the span
	// with the same key keeps its node, and its state. Keys are unique
	// in a Rich; a span with a key used before it is left out.
	Key string
	// Build makes the component, once for its key.
	Build func() Node
}

// Embed is a span that is a component: an image, a badge, a button.
func Embed(key string, build func() Node) Span {
	return Span{Inline: &Inline{Key: key, Build: build}}
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
		if s.Inline != nil {
			b.WriteRune(objectChar)
			continue
		}
		b.WriteString(s.Text)
	}
	return b.String()
}

// objectChar stands for an inline component in the text (its selection,
// its offsets).
const objectChar = '\uFFFC'

// richState is a RichText's inline components.
type richState struct {
	e     *element
	owner *signal.Owner
	kids  []*inlineKid // in the order of their spans
	byKey map[string]*inlineKid
	sent  []byte // the Inlines prop, as last sent
	dirty bool   // the components changed: send it again
}

type inlineKid struct {
	key     string
	el      *element // the component's holder: a view, a layout root
	dispose func()
	width   float32 // the width it was laid out within
}

// sync makes the components the spans' Inlines, in their order: new keys
// are built, gone ones disposed.
func (st *richState) sync(r Rich) {
	var keys []string
	for _, s := range r {
		if s.Inline != nil {
			keys = append(keys, s.Inline.Key)
		}
	}
	if len(keys) == 0 && len(st.kids) == 0 {
		return
	}
	if st.byKey == nil {
		st.byKey = map[string]*inlineKid{}
		st.e.r.inlines = append(st.e.r.inlines, st)
	}
	same := len(keys) == len(st.kids)
	for i := 0; same && i < len(keys); i++ {
		same = st.kids[i].key == keys[i]
	}
	if same {
		return
	}
	wanted := make(map[string]bool, len(keys))
	for _, k := range keys {
		wanted[k] = true
	}
	for _, k := range st.kids {
		if !wanted[k.key] {
			delete(st.byKey, k.key)
			k.dispose()
			st.e.removeKid(k.el, true)
		} else {
			st.e.removeKid(k.el, false)
		}
	}
	st.kids = st.kids[:0]
	for _, s := range r {
		in := s.Inline
		if in == nil || (len(st.kids) > 0 && st.has(in.Key)) {
			continue
		}
		k := st.byKey[in.Key]
		if k == nil {
			k = st.build(in)
			st.byKey[in.Key] = k
		}
		st.kids = append(st.kids, k)
		st.e.appendKid(k.el)
	}
	st.dirty = true
}

func (st *richState) has(key string) bool {
	for _, k := range st.kids {
		if k.key == key {
			return true
		}
	}
	return false
}

func (st *richState) build(in *Inline) *inlineKid {
	k := &inlineKid{key: in.Key}
	r := st.e.r
	signal.RunWithOwner(st.owner, func() {
		signal.Root(func(d func()) {
			k.dispose = d
			withRoot(r, func() Node {
				el := newElement(ops.KindView, true)
				el.inline = true
				st := layout.DefaultStyle
				st.FlexDirection = layout.Column
				st.AlignItems = layout.AlignStart
				el.box.SetStyle(st)
				if n := in.Build(); n.e != nil {
					el.appendKid(n.e)
				}
				k.el = el
				return Node{}
			})
		})
	})
	return k
}

// layout lays out the components at their own size, within width, and
// tells the host their sizes if they changed (the RichText measures again).
func (st *richState) layout(width float32, p *layout.Pass) {
	for _, k := range st.kids {
		if !k.el.box.Dirty() && k.width == width {
			continue
		}
		k.width = width
		// Its max-content size; within width when that's wider.
		if s := k.el.box.Layout(layout.Size{W: layout.MaxContentSpace, H: layout.MaxContentSpace}, p); s.W > width {
			k.el.box.Layout(layout.Size{W: width, H: layout.MaxContentSpace}, p)
		}
		st.dirty = true
	}
	if !st.dirty {
		return
	}
	st.dirty = false
	le := binary.LittleEndian
	b := le.AppendUint32(st.e.r.inlineBuf[:0], uint32(len(st.kids)))
	for _, k := range st.kids {
		b = le.AppendUint32(b, uint32(k.el.id))
		b = le.AppendUint32(b, math.Float32bits(k.el.box.W))
		b = le.AppendUint32(b, math.Float32bits(k.el.box.H))
	}
	st.e.r.inlineBuf = b
	if bytes.Equal(b, st.sent) {
		return
	}
	st.sent = append(st.sent[:0], b...)
	st.e.r.batch.SetBytes(st.e.id, ops.PropRichTextInlines, st.sent)
	st.e.remeasure()
}

func (st *richState) disposeAll() {
	for _, k := range st.kids {
		k.dispose()
	}
	st.kids = nil
	st.byKey = nil
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
	st := &richState{e: e, owner: signal.CurrentOwner()}
	signal.RenderEffect(func() {
		r := p.Text.Get()
		signal.Untrack(func() struct{} { st.sync(r); return struct{}{} })
		b := appendRich(nil, r)
		if bytes.Equal(b, last) {
			return
		}
		last = b
		e.r.batch.SetBytes(e.id, ops.PropRichTextContent, b)
		e.remeasure()
	})
	signal.OnCleanup(st.disposeAll)
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
	var seen map[string]bool // the inline components' keys: each is shown once
	for _, s := range r {
		if in := s.Inline; in != nil {
			if seen[in.Key] {
				continue
			}
			if seen == nil {
				seen = map[string]bool{}
			}
			seen[in.Key] = true
		}
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
		text := s.Text
		if s.Inline != nil {
			k.flags |= 16
			text = string(objectChar)
		}
		i, ok := index[k]
		if !ok {
			i = uint32(len(styles))
			index[k] = i
			styles = append(styles, k)
		}
		for n, part := range strings.Split(text, "\n") {
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

// SelectableProps configures a Selectable.
type SelectableProps struct {
	Style Styler
}

// Selectable makes the RichText views inside it selectable as one text,
// across them: a long press selects a word, the platform's handles extend
// it over any of them, and the edit menu copies it. Markdown views are
// one.
func Selectable(p SelectableProps, children ...Node) Node {
	e := newElement(ops.KindSelectable, true)
	e.setStyle(p.Style, false)
	addKids(e, children)
	return Node{e}
}

// OpenURL asks the system to open url: a web address in the browser,
// another scheme in the app that handles it. Main thread.
func OpenURL(url string) {
	r := activeRoot()
	if r == nil {
		return
	}
	r.batch.Command(r.el.id, ops.CmdOpenURL, []byte(url))
}
