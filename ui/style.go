package ui

import (
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
)

// Dim is a length: Auto (the zero value), points or a percentage.
type Dim = layout.Length

// Pt is a length in points (iOS points, Android dp).
func Pt(v float32) Dim { return layout.Pt(v) }

// Pct is a percentage of the parent's size.
func Pct(v float32) Dim { return layout.Pct(v) }

// Auto is the zero Dim.
var Auto = layout.Auto

// Edges are per-side lengths in points.
type Edges struct{ Top, Right, Bottom, Left float32 }

// All is the same length on every side.
func All(v float32) Edges { return Edges{v, v, v, v} }

// Symmetric is v vertically and h horizontally.
func Symmetric(v, h float32) Edges { return Edges{v, h, v, h} }

// Direction is the axis a container stacks its children on.
type Direction uint8

const (
	ColumnDir Direction = iota // top to bottom (the default)
	RowDir                     // leading to trailing
)

// Justify places children along the main axis.
type Justify uint8

const (
	JustifyStart Justify = iota
	JustifyCenter
	JustifyEnd
	JustifySpaceBetween
	JustifySpaceAround
	JustifySpaceEvenly
)

// Align places children across the main axis.
type Align uint8

const (
	AlignAuto Align = iota // Stretch for a container, the container's for AlignSelf
	AlignStretch
	AlignStart
	AlignCenter
	AlignEnd
	AlignBaseline
)

// NoShrink, as a Style's Shrink, keeps a child from shrinking.
const NoShrink = -1

// Color is the platform's default (the zero value), a semantic color or
// RGBA.
type Color = ops.Color

// RGB makes an opaque color.
func RGB(r, g, b uint8) Color { return ops.RGBA(r, g, b, 255) }

// RGBA makes a color with alpha.
func RGBA(r, g, b, a uint8) Color { return ops.RGBA(r, g, b, a) }

// Hex makes an opaque color from 0xRRGGBB.
func Hex(v uint32) Color { return RGB(uint8(v>>16), uint8(v>>8), uint8(v)) }

func semantic(c ops.SemanticColor) Color { return ops.SemanticColorValue(c) }

// Semantic colors, resolved by the host (UIKit's semantic colors,
// Material 3's color roles), so dark mode needs no app code.
var (
	Label               = semantic(ops.SemanticColorLabel)
	SecondaryLabel      = semantic(ops.SemanticColorSecondaryLabel)
	TertiaryLabel       = semantic(ops.SemanticColorTertiaryLabel)
	SystemBackground    = semantic(ops.SemanticColorBackground)
	SecondaryBackground = semantic(ops.SemanticColorSecondaryBackground)
	GroupedBackground   = semantic(ops.SemanticColorGroupedBackground)
	Separator           = semantic(ops.SemanticColorSeparator)
	Accent              = semantic(ops.SemanticColorAccent)
	Red                 = semantic(ops.SemanticColorRed)
	Orange              = semantic(ops.SemanticColorOrange)
	Yellow              = semantic(ops.SemanticColorYellow)
	Green               = semantic(ops.SemanticColorGreen)
	Blue                = semantic(ops.SemanticColorBlue)
	Purple              = semantic(ops.SemanticColorPurple)
	Pink                = semantic(ops.SemanticColorPink)
	Gray                = semantic(ops.SemanticColorGray)
	Clear               = semantic(ops.SemanticColorClear)
)

// Weight is a font weight.
type Weight = ops.FontWeight

const (
	Ultralight = ops.FontWeightUltralight
	Thin       = ops.FontWeightThin
	Light      = ops.FontWeightLight
	Regular    = ops.FontWeightRegular
	Medium     = ops.FontWeightMedium
	Semibold   = ops.FontWeightSemibold
	Bold       = ops.FontWeightBold
	Heavy      = ops.FontWeightHeavy
	Black      = ops.FontWeightBlack
)

// Font is a semantic text style (scaled by Dynamic Type / font scale)
// or a system font of a size, with an optional weight.
type Font struct {
	Style  ops.TextStyle
	Size   float32 // points; 0: the text style's size
	Weight Weight
}

// Weighted returns f with weight w.
func (f Font) Weighted(w Weight) Font { f.Weight = w; return f }

// System is the system font at size points.
func System(size float32) Font { return Font{Size: size} }

// Semantic fonts.
var (
	LargeTitle  = Font{Style: ops.TextStyleLargeTitle}
	Title       = Font{Style: ops.TextStyleTitle}
	Title2      = Font{Style: ops.TextStyleTitle2}
	Title3      = Font{Style: ops.TextStyleTitle3}
	Headline    = Font{Style: ops.TextStyleHeadline}
	Subheadline = Font{Style: ops.TextStyleSubheadline}
	Body        = Font{Style: ops.TextStyleBody}
	Callout     = Font{Style: ops.TextStyleCallout}
	Footnote    = Font{Style: ops.TextStyleFootnote}
	Caption     = Font{Style: ops.TextStyleCaption}
	Caption2    = Font{Style: ops.TextStyleCaption2}
)

// TextAlign aligns text within its box.
type TextAlign = ops.TextAlign

const (
	TextStart   = ops.TextAlignStart
	TextCenter  = ops.TextAlignCenter
	TextEnd     = ops.TextAlignEnd
	TextJustify = ops.TextAlignJustify
)

// Grid sets a container's grid. Its children are placed in it by their
// GridColumn and GridRow.
type Grid struct {
	Columns, Rows         []layout.GridTrack
	AutoColumns, AutoRows []layout.TrackSize
	Flow                  layout.GridAutoFlow
}

// Style is how a node is laid out and drawn. Layout follows CSS
// flexbox (and grid, with Grid set) with React Native's defaults: column
// direction and border-box sizing. Visual props apply where the native
// widget supports them; an unset prop keeps the platform's default.
type Style struct {
	Direction Direction
	Wrap      bool
	Justify   Justify
	Align     Align // children's cross-axis alignment
	AlignSelf Align
	Gap       float32 // between children, and between lines

	Padding Edges
	Margin  Edges

	Width, Height       Dim
	MinWidth, MinHeight Dim
	MaxWidth, MaxHeight Dim
	AspectRatio         float32 // width / height

	// Grow is the child's share of the leftover space along its parent's
	// main axis. Shrink is how much it gives up when the line overflows:
	// 0 means the default, 1 (text wraps, controls keep their minimum);
	// NoShrink keeps its size. Basis is its starting size (Auto: its
	// content's).
	Grow   float32
	Shrink float32
	Basis  Dim

	// Absolute takes the node out of the flow; Top, Right, Bottom and
	// Left place it in its parent.
	Absolute                 bool
	Top, Right, Bottom, Left Dim

	Grid       *Grid
	GridColumn layout.Placement
	GridRow    layout.Placement

	Background   Color
	CornerRadius float32
	BorderWidth  float32 // drawn inside the box; insets the content
	BorderColor  Color
	Opacity      float32 // 0: opaque; Hidden hides
	Hidden       bool
	Clip         bool

	Font      Font
	Color     Color // text color, or tint for controls
	TextAlign TextAlign
	Lines     int // max lines of text; 0: no limit
}

// Styler is a Style or a StyleFunc.
type Styler interface {
	styler() (Style, func() Style)
}

func (s Style) styler() (Style, func() Style) { return s, nil }

// StyleFunc is a style that changes: it runs in an effect, and only the
// props that change are sent.
type StyleFunc func() Style

func (f StyleFunc) styler() (Style, func() Style) { return Style{}, f }

func (s *Style) visual() bool {
	return s.Background != 0 || s.CornerRadius != 0 || s.BorderWidth != 0 || s.BorderColor != 0 ||
		s.Opacity != 0 || s.Hidden || s.Clip
}

func edges(e Edges) layout.Rect[layout.Length] {
	return layout.Rect[layout.Length]{Top: layout.Pt(e.Top), Right: layout.Pt(e.Right), Bottom: layout.Pt(e.Bottom), Left: layout.Pt(e.Left)}
}

func align(a Align) layout.Align {
	switch a {
	case AlignStretch:
		return layout.AlignStretch
	case AlignStart:
		return layout.AlignFlexStart
	case AlignCenter:
		return layout.AlignCenter
	case AlignEnd:
		return layout.AlignFlexEnd
	case AlignBaseline:
		return layout.AlignBaseline
	}
	return layout.AlignAuto
}

// layoutStyle maps s to CSS.
func (s *Style) layoutStyle(extraPad Edges) layout.Style {
	l := layout.DefaultStyle
	l.FlexDirection = layout.Column
	if s.Direction == RowDir {
		l.FlexDirection = layout.Row
	}
	if s.Wrap {
		l.FlexWrap = layout.Wrap
	}
	l.JustifyContent = [...]layout.Content{layout.ContentFlexStart, layout.ContentCenter, layout.ContentFlexEnd,
		layout.ContentSpaceBetween, layout.ContentSpaceAround, layout.ContentSpaceEvenly}[s.Justify]
	l.AlignItems = align(s.Align)
	l.AlignSelf = align(s.AlignSelf)
	l.ColumnGap, l.RowGap = layout.Pt(s.Gap), layout.Pt(s.Gap)
	pad := s.Padding
	pad.Top += extraPad.Top
	pad.Right += extraPad.Right
	pad.Bottom += extraPad.Bottom
	pad.Left += extraPad.Left
	l.Padding = edges(pad)
	l.Margin = edges(s.Margin)
	l.Border = layout.All(layout.Pt(s.BorderWidth))
	l.Width, l.Height = s.Width, s.Height
	l.MinWidth, l.MinHeight = s.MinWidth, s.MinHeight
	l.MaxWidth, l.MaxHeight = s.MaxWidth, s.MaxHeight
	l.AspectRatio = s.AspectRatio
	l.FlexGrow = s.Grow
	switch {
	case s.Shrink < 0:
		l.FlexShrink = 0
	case s.Shrink > 0:
		l.FlexShrink = s.Shrink
	}
	l.FlexBasis = s.Basis
	if s.Absolute {
		l.Position = layout.Absolute
	}
	l.Inset = layout.Rect[layout.Length]{Top: s.Top, Right: s.Right, Bottom: s.Bottom, Left: s.Left}
	if s.Clip {
		l.OverflowX, l.OverflowY = layout.OverflowHidden, layout.OverflowHidden
	}
	if g := s.Grid; g != nil {
		l.Display = layout.DisplayGrid
		l.GridTemplateColumns, l.GridTemplateRows = g.Columns, g.Rows
		l.GridAutoColumns, l.GridAutoRows = g.AutoColumns, g.AutoRows
		l.GridAutoFlow = g.Flow
	}
	l.GridColumn, l.GridRow = s.GridColumn, s.GridRow
	return l
}

// applyVisual sends the visual and text props of s that differ from
// prev (all set ones when prev is nil). It reports whether text props
// changed (the view must be measured again).
func (e *element) applyVisual(s, prev *Style, text bool) (remeasure bool) {
	if e.id == 0 {
		return false
	}
	var zero Style
	if prev == nil {
		prev = &zero
	}
	b := e.r.batch
	id := e.id
	if s.Background != prev.Background {
		b.SetColor(id, ops.PropBackground, s.Background)
	}
	if s.CornerRadius != prev.CornerRadius {
		b.SetFloat(id, ops.PropCornerRadius, float64(s.CornerRadius))
	}
	if s.BorderWidth != prev.BorderWidth {
		b.SetFloat(id, ops.PropBorderWidth, float64(s.BorderWidth))
	}
	if s.BorderColor != prev.BorderColor {
		b.SetColor(id, ops.PropBorderColor, s.BorderColor)
	}
	if s.Opacity != prev.Opacity {
		o := s.Opacity
		if o == 0 {
			o = 1
		}
		b.SetFloat(id, ops.PropOpacity, float64(o))
	}
	if s.Hidden != prev.Hidden {
		b.SetBool(id, ops.PropHidden, s.Hidden)
	}
	if s.Clip != prev.Clip {
		b.SetBool(id, ops.PropClip, s.Clip)
	}
	if s.Color != prev.Color {
		b.SetColor(id, ops.PropColor, s.Color)
	}
	if !text {
		return false
	}
	if s.Font.Style != prev.Font.Style {
		b.SetEnum(id, ops.PropTextStyle, int32(s.Font.Style))
		remeasure = true
	}
	if s.Font.Size != prev.Font.Size {
		b.SetFloat(id, ops.PropFontSize, float64(s.Font.Size))
		remeasure = true
	}
	if s.Font.Weight != prev.Font.Weight {
		b.SetEnum(id, ops.PropFontWeight, int32(s.Font.Weight))
		remeasure = true
	}
	if s.TextAlign != prev.TextAlign {
		b.SetEnum(id, ops.PropTextAlign, int32(s.TextAlign))
	}
	if s.Lines != prev.Lines {
		b.SetInt(id, ops.PropLineLimit, int64(s.Lines))
		remeasure = true
	}
	return remeasure
}
