// Package markdown shows Markdown as native views, parsing incrementally:
// text appended to it (a language model's reply streaming in) parses only
// the block being written, and only that block's views update.
//
//	markdown.View(markdown.Props{Text: reply, Streaming: writing})
package markdown

import (
	"strconv"
	"strings"

	"github.com/egoist/nui/markdown/parser"
	"github.com/egoist/nui/signal"
	"github.com/egoist/nui/ui"
)

// Props configures a View.
type Props struct {
	Text signal.Accessor[string]
	// Streaming tells text still being written: its end shows what it
	// will most likely become (emphasis closed, a link being written as a
	// link) rather than half-written syntax. Turn it off when the text is
	// complete.
	Streaming signal.Accessor[bool]
	// Extensions is the syntax read beyond CommonMark: parser.Default
	// (GitHub's, footnotes, math) when zero.
	Extensions parser.Extensions
	// OnLink handles a tapped link, given its destination: ui.OpenURL
	// when nil.
	OnLink func(url string)
	// Code, when set, shows code blocks in place of the default
	// (monospaced on the secondary background, scrolling sideways).
	Code func(language, code string) ui.Node
	// NotSelectable makes the text not selectable; by default the whole
	// view is one selection, across its paragraphs.
	NotSelectable bool
	// Theme is the look: DefaultTheme where it leaves fields zero.
	Theme *Theme
	// Style is the view's own (its Gap: Theme.BlockSpacing by default).
	Style ui.Styler
}

// block is what a view of a block depends on: when it's equal, the
// view stays as it is.
type block struct {
	i          int
	start, end int
	kind       parser.Kind
	rev        uint32 // the inlines' revision; for containers, their blocks' summed
	healed     bool
	level      int
	task       parser.Task
}

type view struct {
	p  Props
	d  *parser.Document
	th Theme
}

// View shows Markdown.
func View(p Props) ui.Node {
	ext := p.Extensions
	if ext == 0 {
		ext = parser.Default
	}
	if p.OnLink == nil {
		p.OnLink = ui.OpenURL
	}
	v := &view{p: p, d: parser.New(ext), th: resolve(p.Theme, DefaultTheme())}
	th := &v.th
	top := signal.NewMemoFunc(func() []block {
		src := ""
		if p.Text != nil {
			src = p.Text.Get()
		}
		streaming := p.Streaming != nil && p.Streaming.Get()
		v.d.SetStreaming(streaming)
		v.d.Set(src)
		return v.children(0)
	}, blocksEqual)
	content := v.blocks(top, false, 0)
	style := p.Style
	if style == nil {
		style = ui.Style{Gap: th.BlockSpacing}
	}
	if p.NotSelectable {
		return ui.Column(style, content)
	}
	return ui.Selectable(ui.SelectableProps{Style: style}, content)
}

func blocksEqual(a, b []block) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// children describes block i's children.
func (v *view) children(i int) []block {
	var out []block
	for c := range v.d.Children(i) {
		out = append(out, v.describe(c))
	}
	return out
}

func (v *view) describe(i int) block {
	d := v.d
	b := d.Block(i)
	bl := block{i: i, start: b.Start, end: b.End, kind: b.Kind, level: b.Level, task: b.Task}
	switch b.Kind {
	case parser.Paragraph, parser.Heading, parser.TableCell:
		d.Inlines(i)
		bl.rev, bl.healed = b.Revision(), b.Healed()
	default:
		for j := i + 1; j < b.Next; j++ {
			c := d.Block(j)
			if c.Kind == parser.Paragraph || c.Kind == parser.Heading || c.Kind == parser.TableCell {
				d.Inlines(j)
				bl.rev += c.Revision()
				bl.healed = bl.healed || c.Healed()
			}
		}
	}
	return bl
}

// blocks shows a list of blocks, keyed by where each starts: appending
// to the text changes only the last.
func (v *view) blocks(list signal.Accessor[[]block], tight bool, quote int) ui.Node {
	return ui.For(list, func(b block) [2]int { return [2]int{b.start, int(b.kind)} },
		func(b signal.Accessor[block], _ signal.Accessor[int]) ui.Node {
			return v.block(b, tight, quote)
		})
}

// sub is block b's children, following it.
func (v *view) sub(b signal.Accessor[block]) signal.Accessor[[]block] {
	return signal.NewMemoFunc(func() []block { return v.children(b.Get().i) }, blocksEqual)
}

func (v *view) block(b signal.Accessor[block], tight bool, quote int) ui.Node {
	d := v.d
	th := &v.th
	first := signal.Untrack(b.Get)
	color := th.Text
	if quote > 0 {
		color = th.QuoteText
	}
	switch first.kind {
	case parser.Paragraph:
		return v.paragraph(b, color)
	case parser.Heading:
		h := th.Headings[min(max(first.level, 1), 6)-1]
		if quote == 0 {
			color = h.Color
		}
		return ui.RichText(ui.RichTextProps{OnLink: v.p.OnLink, Role: ui.RoleHeader, Style: ui.Style{Font: h.Font, Color: color, Margin: ui.Edges{Top: h.Top}},
			Text: signal.Func(func() ui.Rich { return v.spans(b.Get().i) })})
	case parser.ThematicBreak:
		return ui.View(ui.ViewProps{Style: ui.Style{Height: ui.Pt(1), Background: th.Rule, Margin: ui.Symmetric(6, 0)}})
	case parser.CodeBlock, parser.HTMLBlock, parser.MathBlock:
		code := signal.Func(func() string {
			bl := b.Get()
			return expandTabs(strings.TrimSuffix(d.Lines(bl.i), "\n"))
		})
		if first.kind == parser.CodeBlock && v.p.Code != nil {
			return ui.Dynamic(func() ui.Node {
				bl := b.Get()
				return v.p.Code(d.Block(bl.i).Language(), strings.TrimSuffix(d.Lines(bl.i), "\n"))
			})
		}
		align := ui.TextStart
		if first.kind == parser.MathBlock {
			align = ui.TextCenter
		}
		box := ui.Style{Background: th.CodeBlockBackground, CornerRadius: th.CodeBlockRadius, Clip: true}
		if th.CodeBlockBorder != 0 {
			box.BorderWidth, box.BorderColor = 1, th.CodeBlockBorder
		}
		return ui.View(ui.ViewProps{Style: box},
			ui.Scroll(ui.ScrollProps{Horizontal: true},
				ui.View(ui.ViewProps{Style: ui.Style{Padding: ui.All(th.CodeBlockPadding)}},
					ui.RichText(ui.RichTextProps{Style: ui.Style{Font: th.CodeFont, Color: th.Code, TextAlign: align},
						Text: signal.Func(func() ui.Rich {
							return ui.Rich{{Text: code.Get(), Mono: true}}
						})}))))
	case parser.Quote:
		return ui.Row(ui.Style{Gap: 12},
			ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(th.QuoteBarWidth), Background: th.QuoteBar, CornerRadius: th.QuoteBarWidth / 2}}),
			ui.Column(ui.Style{Grow: 1, Gap: th.BlockSpacing}, v.blocks(v.sub(b), false, quote+1)))
	case parser.List:
		blk := d.Block(first.i)
		gap := th.BlockSpacing
		if blk.Tight {
			gap = th.ItemSpacing
		}
		ordered, number := blk.Ordered, blk.Number
		items := v.sub(b)
		return ui.Column(ui.Style{Gap: gap}, ui.For(items, func(it block) int { return it.start },
			func(it signal.Accessor[block], index signal.Accessor[int]) ui.Node {
				checked := signal.Func(func() bool { return it.Get().task == parser.Checked })
				marker := signal.Func(func() string {
					if t := it.Get().task; t != parser.NoTask {
						if t == parser.Checked {
							return "☑"
						}
						return "☐"
					}
					if ordered {
						return strconv.Itoa(number+index.Get()) + "."
					}
					return "•"
				})
				// The marker's room is the indent, but for the gap after it.
				markerStyle := ui.StyleFunc(func() ui.Style {
					c := th.Muted
					if checked.Get() {
						c = th.Checkbox
					}
					return ui.Style{Width: ui.Pt(max(th.ListIndent-6, 0)), Shrink: ui.NoShrink, TextAlign: ui.TextEnd, Color: c, Font: th.Font}
				})
				return ui.Row(ui.Style{Gap: 6, Align: ui.AlignStart},
					ui.Text(marker, markerStyle),
					ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: gap}, v.blocks(v.sub(it), blk.Tight, quote)))
			}))
	case parser.Table:
		rows := v.sub(b)
		return ui.Scroll(ui.ScrollProps{Horizontal: true},
			ui.Column(ui.Style{BorderWidth: 1, BorderColor: th.TableBorder, CornerRadius: th.TableRadius, Clip: true},
				ui.For(rows, func(r block) int { return r.start }, func(r signal.Accessor[block], ri signal.Accessor[int]) ui.Node {
					header := d.Block(signal.Untrack(r.Get).i).Header
					cells := v.sub(r)
					// Striped by its index among the rows.
					bg := ui.StyleFunc(func() ui.Style {
						switch {
						case header:
							return ui.Style{Background: th.TableHeader}
						case th.TableStripe != 0 && ri.Get()%2 == 0:
							return ui.Style{Background: th.TableStripe}
						}
						return ui.Style{}
					})
					return ui.Row(bg,
						ui.For(cells, func(c block) int { return c.start }, func(c signal.Accessor[block], ci signal.Accessor[int]) ui.Node {
							font := th.TableFont
							if header && (v.p.Theme == nil || v.p.Theme.TableFont.Weight == 0) {
								font = font.Weighted(ui.Semibold)
							}
							pad := th.TableCellPadding
							return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(th.TableColumnWidth), Padding: ui.Symmetric(pad[1], pad[0]), BorderWidth: 0.5, BorderColor: th.TableBorder}},
								ui.RichText(ui.RichTextProps{OnLink: v.p.OnLink, Style: ui.Style{Font: font, Color: th.Text, TextAlign: tableAlign(d, signal.Untrack(r.Get).i, signal.Untrack(ci.Get))},
									Text: signal.Func(func() ui.Rich { return v.spans(c.Get().i) })}))
						}))
				})))
	case parser.FootnoteDefinition:
		label := d.Block(first.i).Label()
		return ui.Row(ui.Style{Gap: 6, Align: ui.AlignStart},
			ui.StaticText(label+".", ui.Style{Font: th.FootnoteFont, Color: th.Muted}),
			ui.Column(ui.Style{Grow: 1, Shrink: 1, Gap: 6}, v.blocks(v.sub(b), true, quote+1)))
	}
	return ui.Column(ui.Style{Gap: th.BlockSpacing}, v.blocks(v.sub(b), tight, quote))
}

func tableAlign(d *parser.Document, row, col int) ui.TextAlign {
	t := d.Block(row).Parent
	if t < 0 {
		return ui.TextStart
	}
	al := d.Block(t).Align
	if col >= len(al) {
		return ui.TextStart
	}
	switch al[col] {
	case parser.AlignCenter:
		return ui.TextCenter
	case parser.AlignRight:
		return ui.TextEnd
	}
	return ui.TextStart
}

// paragraph shows a paragraph: its text, or the images it is made of.
func (v *view) paragraph(b signal.Accessor[block], color ui.Color) ui.Node {
	d := v.d
	// What the paragraph is decides its views: images alone, or text.
	// Only a change of that makes them again.
	kind := signal.NewMemo(func() string {
		var key strings.Builder
		for _, im := range imagesOnly(d.Inlines(b.Get().i)) {
			key.WriteString(im.dest + "\x00" + im.alt + "\x00")
		}
		return key.String()
	})
	return ui.Dynamic(func() ui.Node {
		kind.Get()
		images := imagesOnly(d.Inlines(signal.Untrack(b.Get).i))
		return signal.Untrack(func() ui.Node {
			if images != nil {
				var kids []ui.Node
				for _, im := range images {
					kids = append(kids, ui.Image(ui.ImageProps{Source: ui.Val(im.dest), Label: im.alt,
						Style: ui.Style{AlignSelf: ui.AlignStart, CornerRadius: v.th.ImageRadius, Clip: true}}))
				}
				return ui.Column(ui.Style{Gap: 8}, kids...)
			}
			return ui.RichText(ui.RichTextProps{OnLink: v.p.OnLink, Style: ui.Style{Font: v.th.Font, Color: color},
				Text: signal.Func(func() ui.Rich { return v.spans(b.Get().i) })})
		})
	})
}

type image struct{ dest, alt string }

// imagesOnly returns a paragraph's images when it holds nothing else
// (but spaces), or nil.
func imagesOnly(in []parser.Inline) []image {
	var out []image
	for i := 0; i < len(in); {
		n := &in[i]
		switch n.Kind {
		case parser.Image:
			out = append(out, image{n.Dest, plainText(in[i+1 : n.Next])})
			i = n.Next
			continue
		case parser.SoftBreak, parser.LineBreak:
		case parser.Text:
			if strings.TrimSpace(n.Text) != "" {
				return nil
			}
		default:
			return nil
		}
		i++
	}
	return out
}

func plainText(in []parser.Inline) string {
	var b strings.Builder
	for _, n := range in {
		switch n.Kind {
		case parser.Text, parser.Code, parser.Math:
			b.WriteString(n.Text)
		case parser.SoftBreak:
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// spans is the text of a paragraph, heading or table cell.
func (v *view) spans(i int) ui.Rich {
	in := v.d.Inlines(i)
	var out ui.Rich
	images := 0
	var walk func(from, to int, s ui.Span)
	walk = func(from, to int, s ui.Span) {
		for k := from; k < to; {
			n := &in[k]
			switch n.Kind {
			case parser.Text, parser.HTML:
				t := s
				t.Text = n.Text
				out = append(out, t)
			case parser.SoftBreak:
				t := s
				t.Text = " "
				out = append(out, t)
			case parser.LineBreak:
				t := s
				t.Text = "\n"
				out = append(out, t)
			case parser.Code, parser.Math:
				t := s
				t.Text, t.Mono = n.Text, true
				if !n.Display {
					t.Background = v.th.CodeBackground
					if t.Color == 0 || t.Color == v.th.Text {
						t.Color = v.th.Code
					}
				}
				out = append(out, t)
			case parser.Emphasis:
				t := s
				t.Italic = true
				walk(k+1, n.Next, t)
			case parser.Strong:
				t := s
				t.Font.Weight = v.th.StrongWeight
				if t.Link == "" && v.th.Strong != v.th.Text {
					t.Color = v.th.Strong
				}
				walk(k+1, n.Next, t)
			case parser.Strikethrough:
				t := s
				t.Strike = true
				walk(k+1, n.Next, t)
			case parser.Link:
				t := s
				t.Link, t.Color = n.Dest, v.th.Link
				if n.Pending {
					// Its destination is still being written.
					t.Link = ""
					if t.Color == 0 {
						t.Color = ui.Accent
					}
				}
				walk(k+1, n.Next, t)
			case parser.Image:
				alt := plainText(in[k+1 : n.Next])
				if n.Pending {
					// Its source is still being written: its text, for now.
					t := s
					t.Text = "[" + alt + "]"
					t.Color = ui.SecondaryLabel
					out = append(out, t)
					break
				}
				// In the line, at its own size: a badge, an icon. Its key is
				// its place among the paragraph's images, and its source.
				dest, link := n.Dest, s.Link
				out = append(out, ui.Embed(strconv.Itoa(images)+" "+link+" "+dest, func() ui.Node {
					img := ui.Image(ui.ImageProps{Source: ui.Val(dest), Label: alt})
					if link == "" {
						return img
					}
					// A linked image (a badge) follows its link.
					return ui.View(ui.ViewProps{Role: ui.RoleLink, Label: alt, OnPress: func() { v.p.OnLink(link) }}, img)
				}))
				images++
			case parser.FootnoteRef:
				t := s
				t.Text = "[" + n.Text + "]"
				t.Color = ui.Accent
				t.Font.Style = ui.Footnote.Style
				out = append(out, t)
			}
			if n.Next > k+1 {
				k = n.Next
			} else {
				k++
			}
		}
	}
	walk(0, len(in), ui.Span{})
	return out
}

// tabWidth is a code block's tab stop, in columns.
const tabWidth = 4

// expandTabs turns tabs into spaces to the next tab stop: the platforms'
// text engines draw a tab as a fixed width, not to a column.
func expandTabs(s string) string {
	if !strings.Contains(s, "\t") {
		return s
	}
	var b strings.Builder
	col := 0
	for _, r := range s {
		switch r {
		case '\t':
			n := tabWidth - col%tabWidth
			b.WriteString("    "[:n])
			col += n
		case '\n':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}
