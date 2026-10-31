package markdown

import "github.com/egoist/ryui/ui"

// Theme is how Markdown looks: fonts, colors and spacing for each kind
// of block and inline. What it leaves zero takes DefaultTheme's value, so
// that a theme sets only what it changes:
//
//	markdown.Props{Theme: &markdown.Theme{Link: ui.Purple, CodeFont: ui.Font{Size: 13}}}
//
// Fonts are text styles, so that they follow the user's text size; a
// font's fields left zero are the default's (a Weight alone keeps the
// default's style). Colors are the platform's semantic colors by default,
// so dark mode needs nothing. Lengths are in points.
type Theme struct {
	// Font is body text's: ui.Body.
	Font ui.Font
	// Text colors text (ui.Label when zero), Muted what is secondary
	// (list markers, footnotes; ui.SecondaryLabel), Link links (the
	// accent color), and Strong strong text (Text).
	Text, Muted, Link, Strong ui.Color
	// StrongWeight is the weight of strong text: ui.Bold.
	StrongWeight ui.Weight

	// Code colors code spans (Text), with CodeBackground behind them
	// (ui.SecondaryBackground).
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

	// QuoteBar colors the bar along block quotes (ui.Separator),
	// QuoteBarWidth wide (3), and QuoteText their text (Muted).
	QuoteBar, QuoteText ui.Color
	QuoteBarWidth       float32

	// Rule colors thematic breaks: ui.Separator.
	Rule ui.Color

	// CodeFont is code blocks' (monospaced): ui.Callout.
	// CodeBlockBackground (ui.SecondaryBackground) and CodeBlockBorder
	// (none) color code blocks, with corners of CodeBlockRadius (10) and
	// CodeBlockPadding of room inside (12).
	CodeFont                             ui.Font
	CodeBlockBackground, CodeBlockBorder ui.Color
	CodeBlockRadius, CodeBlockPadding    float32

	// TableFont is table cells' (ui.Callout; the header's semibold).
	// TableBorder colors a table's lines (ui.Separator), TableHeader the
	// background of its header row (ui.SecondaryBackground), and
	// TableStripe that of every other row (none). TableCellPadding is the
	// room in cells, across and down: 10 and 8. TableColumnWidth is a
	// column's width: 140. TableRadius rounds the table's corners: 8.
	TableFont                             ui.Font
	TableBorder, TableHeader, TableStripe ui.Color
	TableCellPadding                      [2]float32
	TableColumnWidth, TableRadius         float32

	// ImageRadius rounds images alone in a paragraph: 8.
	ImageRadius float32
	// FootnoteFont is footnote labels': ui.Footnote.
	FootnoteFont ui.Font
}

// HeadingStyle is how headings of one level look.
type HeadingStyle struct {
	// Font is theirs; Color colors them (Theme.Text when zero).
	Font  ui.Font
	Color ui.Color
	// Top is the room above them, more than Theme.BlockSpacing: 4.
	Top float32
}

// DefaultTheme is the theme of a Markdown view that sets none: the
// platform's text styles and semantic colors, as its own text looks.
func DefaultTheme() *Theme {
	return &Theme{
		Font:                ui.Body,
		Muted:               ui.SecondaryLabel,
		StrongWeight:        ui.Bold,
		CodeBackground:      ui.SecondaryBackground,
		BlockSpacing:        12,
		ItemSpacing:         4,
		ListIndent:          28,
		Checkbox:            ui.Accent,
		QuoteBar:            ui.Separator,
		QuoteBarWidth:       3,
		Rule:                ui.Separator,
		CodeFont:            ui.Callout,
		CodeBlockBackground: ui.SecondaryBackground,
		CodeBlockRadius:     10,
		CodeBlockPadding:    12,
		TableFont:           ui.Callout,
		TableBorder:         ui.Separator,
		TableHeader:         ui.SecondaryBackground,
		TableCellPadding:    [2]float32{10, 8},
		TableColumnWidth:    140,
		TableRadius:         8,
		ImageRadius:         8,
		FootnoteFont:        ui.Footnote,
		Headings: [6]HeadingStyle{
			{Font: ui.Title.Weighted(ui.Bold), Top: 4},
			{Font: ui.Title2.Weighted(ui.Bold), Top: 4},
			{Font: ui.Title3.Weighted(ui.Semibold), Top: 4},
			{Font: ui.Headline, Top: 4},
			{Font: ui.Subheadline.Weighted(ui.Semibold), Top: 4},
			{Font: ui.Footnote.Weighted(ui.Semibold), Top: 4},
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
	font := func(v *ui.Font, d ui.Font) {
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
