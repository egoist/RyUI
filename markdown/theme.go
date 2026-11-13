package markdown

import (
	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/fonts"
	"github.com/egoist/asuka/ui"
)

// Theme is how Markdown looks: fonts, colors and spacing for each kind
// of block and inline. What it leaves zero takes DefaultTheme's value, so
// that a theme sets only what it changes:
//
//	markdown.Props{Theme: &markdown.Theme{Link: colors.Purple, CodeFont: fonts.Font{Size: 13}}}
//
// Fonts are text styles, so that they follow the user's text size; a
// font's fields left zero are the default's (a Weight alone keeps the
// default's style). Colors are the platform's semantic colors by default,
// so dark mode needs nothing. Lengths are in points.
type Theme struct {
	// Font is body text's: fonts.Body.
	Font fonts.Font
	// Text colors text (colors.Label when zero), Muted what is secondary
	// (list markers, footnotes; colors.SecondaryLabel), Link links (the
	// accent color), and Strong strong text (Text).
	Text, Muted, Link, Strong ui.Color
	// StrongWeight is the weight of strong text: fonts.Bold.
	StrongWeight fonts.Weight

	// Code colors code spans (Text), with CodeBackground behind them
	// (colors.SecondaryBackground).
	Code, CodeBackground ui.Color

	// BlockSpacing is the space between blocks: 12. ItemSpacing is that
	// between the items of a tight list: 4.
	BlockSpacing, ItemSpacing float32
	// Headings are the styles of headings of levels 1 to 6.
	Headings [6]HeadingStyle
	// ListIndent is how far list items are from their list's edge, their
	// markers in that room: 28.
	ListIndent float32
	// Checkbox colors the checked boxes of task lists (the accent color);
	// unchecked ones are Muted.
	Checkbox ui.Color

	// QuoteBar colors the bar along block quotes (colors.Separator),
	// QuoteBarWidth wide (3), and QuoteText their text (Muted).
	QuoteBar, QuoteText ui.Color
	QuoteBarWidth       float32

	// Rule colors thematic breaks: colors.Separator.
	Rule ui.Color

	// CodeFont is code blocks' (monospaced): fonts.Callout.
	// CodeBlockBackground (colors.SecondaryBackground) and CodeBlockBorder
	// (none) color code blocks, with corners of CodeBlockRadius (10) and
	// CodeBlockPadding of room inside (12).
	CodeFont                             fonts.Font
	CodeBlockBackground, CodeBlockBorder ui.Color
	CodeBlockRadius, CodeBlockPadding    float32

	// TableFont is table cells' (fonts.Callout; the header's semibold).
	// TableBorder colors a table's lines (colors.Separator), TableHeader the
	// background of its header row (colors.SecondaryBackground), and
	// TableStripe that of every other row (none). TableCellPadding is the
	// room in cells, across and down: 10 and 8. TableColumnWidth is a
	// column's width: 140. TableRadius rounds the table's corners: 8.
	TableFont                             fonts.Font
	TableBorder, TableHeader, TableStripe ui.Color
	TableCellPadding                      [2]float32
	TableColumnWidth, TableRadius         float32

	// ImageRadius rounds images alone in a paragraph: 8.
	ImageRadius float32
	// FootnoteFont is footnote labels': fonts.Footnote.
	FootnoteFont fonts.Font
}

// HeadingStyle is how headings of one level look.
type HeadingStyle struct {
	// Font is theirs; Color colors them (Theme.Text when zero).
	Font  fonts.Font
	Color ui.Color
	// Top is the room above them, more than Theme.BlockSpacing: 4.
	Top float32
}

// DefaultTheme is the theme of a Markdown view that sets none: the
// platform's text styles and semantic colors, as its own text looks.
func DefaultTheme() *Theme {
	return &Theme{
		Font:                fonts.Body,
		Muted:               colors.SecondaryLabel,
		StrongWeight:        fonts.Bold,
		CodeBackground:      colors.SecondaryBackground,
		BlockSpacing:        12,
		ItemSpacing:         4,
		ListIndent:          28,
		Checkbox:            colors.Accent,
		QuoteBar:            colors.Separator,
		QuoteBarWidth:       3,
		Rule:                colors.Separator,
		CodeFont:            fonts.Callout,
		CodeBlockBackground: colors.SecondaryBackground,
		CodeBlockRadius:     10,
		CodeBlockPadding:    12,
		TableFont:           fonts.Callout,
		TableBorder:         colors.Separator,
		TableHeader:         colors.SecondaryBackground,
		TableCellPadding:    [2]float32{10, 8},
		TableColumnWidth:    140,
		TableRadius:         8,
		ImageRadius:         8,
		FootnoteFont:        fonts.Footnote,
		Headings: [6]HeadingStyle{
			{Font: fonts.Title.Weighted(fonts.Bold), Top: 4},
			{Font: fonts.Title2.Weighted(fonts.Bold), Top: 4},
			{Font: fonts.Title3.Weighted(fonts.Semibold), Top: 4},
			{Font: fonts.Headline, Top: 4},
			{Font: fonts.Subheadline.Weighted(fonts.Semibold), Top: 4},
			{Font: fonts.Footnote.Weighted(fonts.Semibold), Top: 4},
		},
	}
}

// resolve returns th with what it leaves zero taken from def.
func resolve(th, def *Theme) Theme {
	if th == nil {
		return *def
	}
	out := *th
	f := func(v *float32, d float32) {
		if *v == 0 {
			*v = d
		}
	}
	c := func(v *ui.Color, d ui.Color) {
		if *v == 0 {
			*v = d
		}
	}
	font := func(v *fonts.Font, d fonts.Font) {
		if v.Style == 0 {
			v.Style = d.Style
		}
		if v.Size == 0 {
			v.Size = d.Size
		}
		if v.Weight == 0 {
			v.Weight = d.Weight
		}
	}
	font(&out.Font, def.Font)
	c(&out.Text, def.Text)
	c(&out.Muted, def.Muted)
	c(&out.Link, def.Link)
	c(&out.Strong, out.Text)
	if out.StrongWeight == 0 {
		out.StrongWeight = def.StrongWeight
	}
	c(&out.Code, out.Text)
	c(&out.CodeBackground, def.CodeBackground)
	f(&out.BlockSpacing, def.BlockSpacing)
	f(&out.ItemSpacing, def.ItemSpacing)
	for i := range out.Headings {
		h, d := &out.Headings[i], def.Headings[i]
		font(&h.Font, d.Font)
		c(&h.Color, out.Text)
		f(&h.Top, d.Top)
	}
	f(&out.ListIndent, def.ListIndent)
	c(&out.Checkbox, def.Checkbox)
	c(&out.QuoteBar, def.QuoteBar)
	c(&out.QuoteText, out.Muted)
	f(&out.QuoteBarWidth, def.QuoteBarWidth)
	c(&out.Rule, def.Rule)
	font(&out.CodeFont, def.CodeFont)
	c(&out.CodeBlockBackground, def.CodeBlockBackground)
	c(&out.CodeBlockBorder, def.CodeBlockBorder)
	f(&out.CodeBlockRadius, def.CodeBlockRadius)
	f(&out.CodeBlockPadding, def.CodeBlockPadding)
	font(&out.TableFont, def.TableFont)
	c(&out.TableBorder, def.TableBorder)
	c(&out.TableHeader, def.TableHeader)
	c(&out.TableStripe, def.TableStripe)
	f(&out.TableCellPadding[0], def.TableCellPadding[0])
	f(&out.TableCellPadding[1], def.TableCellPadding[1])
	f(&out.TableColumnWidth, def.TableColumnWidth)
	f(&out.TableRadius, def.TableRadius)
	f(&out.ImageRadius, def.ImageRadius)
	font(&out.FootnoteFont, def.FootnoteFont)
	return out
}
