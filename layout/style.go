package layout

import "math"

// Unit is how a Length is measured.
type Unit uint8

const (
	UnitAuto Unit = iota // the zero Length
	UnitPoint
	UnitPercent
)

// Length is Auto, points (iOS points, Android dp) or a percentage of the
// containing block. Where Auto isn't allowed (padding, border, gap) it
// means 0.
type Length struct {
	V float32
	U Unit
}

// Auto is the zero Length.
var Auto = Length{}

// Pt is a length in points.
func Pt(v float32) Length { return Length{v, UnitPoint} }

// Pct is a percentage of the containing block's size along the same
// axis (its width for margin and padding).
func Pct(v float32) Length { return Length{v, UnitPercent} }

// IsAuto reports whether l is Auto.
func (l Length) IsAuto() bool { return l.U == UnitAuto }

// resolve returns l against basis, NaN when Auto or a percentage of an
// unknown basis.
func (l Length) resolve(basis float32) float32 {
	switch l.U {
	case UnitPoint:
		return l.V
	case UnitPercent:
		return l.V * basis / 100 // NaN basis gives NaN
	}
	return nan
}

func (l Length) resolve0(basis float32) float32 { return or0(l.resolve(basis)) }

// Rect holds a value per side.
type Rect[T any] struct{ Top, Right, Bottom, Left T }

// All makes a Rect with v on every side.
func All[T any](v T) Rect[T] { return Rect[T]{v, v, v, v} }

// Display picks a node's layout algorithm for its children.
type Display uint8

const (
	DisplayFlex Display = iota
	DisplayBlock
	DisplayGrid
	DisplayNone
)

// Position takes a node in or out of flow.
type Position uint8

const (
	Relative Position = iota // in flow; insets offset it visually
	Absolute                 // out of flow, placed by insets in its parent's padding box
)

// BoxSizing says what Width and Height measure.
type BoxSizing uint8

const (
	BorderBox BoxSizing = iota // padding and border included (the default here)
	ContentBox
)

// Overflow affects layout through automatic minimum sizes and scrollbar
// space: a Hidden, Clip or Scroll node's minimum size is 0 rather than
// its content's.
type Overflow uint8

const (
	OverflowVisible Overflow = iota
	OverflowClip
	OverflowHidden
	OverflowScroll
)

// FlexDirection is a flex container's main axis.
type FlexDirection uint8

const (
	Row FlexDirection = iota
	Column
	RowReverse
	ColumnReverse
)

func (d FlexDirection) isRow() bool     { return d == Row || d == RowReverse }
func (d FlexDirection) isReverse() bool { return d == RowReverse || d == ColumnReverse }

// FlexWrap lets a flex container break items into lines.
type FlexWrap uint8

const (
	NoWrap FlexWrap = iota
	Wrap
	WrapReverse
)

// Align positions an item within its line or grid area (align-items,
// align-self, justify-items, justify-self). AlignAuto means "normal"
// for containers and "the container's value" for self-alignment.
type Align uint8

const (
	AlignAuto Align = iota
	AlignStart
	AlignEnd
	AlignFlexStart
	AlignFlexEnd
	AlignCenter
	AlignBaseline
	AlignStretch
)

// Content distributes space between lines, tracks or items
// (align-content, justify-content). ContentNormal behaves as Stretch for
// align-content and as Start for justify-content.
type Content uint8

const (
	ContentNormal Content = iota
	ContentStart
	ContentEnd
	ContentFlexStart
	ContentFlexEnd
	ContentCenter
	ContentStretch
	ContentSpaceBetween
	ContentSpaceAround
	ContentSpaceEvenly
)

// GridAutoFlow says how auto-placed grid items fill the grid.
type GridAutoFlow uint8

const (
	FlowRow GridAutoFlow = iota
	FlowColumn
	FlowRowDense
	FlowColumnDense
)

func (f GridAutoFlow) isColumn() bool { return f == FlowColumn || f == FlowColumnDense }
func (f GridAutoFlow) isDense() bool  { return f == FlowRowDense || f == FlowColumnDense }

// Breadth is one side of a grid track's sizing function.
type Breadth struct {
	Kind BreadthKind
	V    float32 // points, percent or fr
}

type BreadthKind uint8

const (
	BreadthAuto BreadthKind = iota
	BreadthPoint
	BreadthPercent
	BreadthFr
	BreadthMinContent
	BreadthMaxContent
	BreadthFitContent // fit-content(V points); see FitContentPct
	BreadthFitContentPercent
)

// TrackSize is a grid track's sizing function: minmax(Min, Max).
type TrackSize struct{ Min, Max Breadth }

// Track sizes.
func Px(v float32) TrackSize      { b := Breadth{BreadthPoint, v}; return TrackSize{b, b} }
func Percent(v float32) TrackSize { b := Breadth{BreadthPercent, v}; return TrackSize{b, b} }
func Fr(v float32) TrackSize      { return TrackSize{Breadth{Kind: BreadthAuto}, Breadth{BreadthFr, v}} }
func AutoTrack() TrackSize        { return TrackSize{} }
func MinContent() TrackSize       { b := Breadth{Kind: BreadthMinContent}; return TrackSize{b, b} }
func MaxContent() TrackSize       { b := Breadth{Kind: BreadthMaxContent}; return TrackSize{b, b} }
func FitContent(v float32) TrackSize {
	return TrackSize{Breadth{Kind: BreadthAuto}, Breadth{BreadthFitContent, v}}
}
func FitContentPct(v float32) TrackSize {
	return TrackSize{Breadth{Kind: BreadthAuto}, Breadth{BreadthFitContentPercent, v}}
}
func Minmax(min, max Breadth) TrackSize { return TrackSize{min, max} }

// Breadths for Minmax.
func BPx(v float32) Breadth  { return Breadth{BreadthPoint, v} }
func BPct(v float32) Breadth { return Breadth{BreadthPercent, v} }
func BFr(v float32) Breadth  { return Breadth{BreadthFr, v} }
func BAuto() Breadth         { return Breadth{} }
func BMinContent() Breadth   { return Breadth{Kind: BreadthMinContent} }
func BMaxContent() Breadth   { return Breadth{Kind: BreadthMaxContent} }

// Repeat counts for GridTrack.
const (
	RepeatAutoFill = -1
	RepeatAutoFit  = -2
)

// GridTrack is an entry of grid-template-rows/columns: one track, or
// repeat(Count, Sizes...) when Count != 0 (Count may be RepeatAutoFill
// or RepeatAutoFit).
type GridTrack struct {
	Count int
	Sizes []TrackSize
}

// T is a single track.
func T(s TrackSize) GridTrack { return GridTrack{Sizes: []TrackSize{s}} }

// Repeat repeats sizes count times (or RepeatAutoFill/RepeatAutoFit).
func Repeat(count int, sizes ...TrackSize) GridTrack { return GridTrack{Count: count, Sizes: sizes} }

// GridLine is one end of a grid item's placement.
type GridLine struct {
	Kind GridLineKind
	N    int16 // the line (1-based, negative from the end) or the span
}

type GridLineKind uint8

const (
	LineAuto GridLineKind = iota
	LineIndex
	LineSpan
)

// Line is grid line n (1 is the first; -1 the last explicit one).
func Line(n int) GridLine { return GridLine{LineIndex, int16(n)} }

// Span spans n tracks.
func Span(n int) GridLine { return GridLine{LineSpan, int16(n)} }

// Placement is an item's grid-row or grid-column.
type Placement struct{ Start, End GridLine }

// Style is everything layout reads from a node, with CSS's meaning. Start
// from DefaultStyle: the zero Style differs from CSS's initial values in
// FlexShrink.
type Style struct {
	Display   Display
	Position  Position
	BoxSizing BoxSizing
	OverflowX Overflow
	OverflowY Overflow
	// ScrollbarWidth is the space a scroll container reserves for its
	// scrollbars (0 for overlay scrollbars, as on mobile).
	ScrollbarWidth float32

	Width, Height       Length
	MinWidth, MinHeight Length  // Auto: the automatic minimum size
	MaxWidth, MaxHeight Length  // Auto: none
	AspectRatio         float32 // width / height, when > 0

	Margin  Rect[Length] // Auto allowed
	Padding Rect[Length]
	Border  Rect[Length]
	Inset   Rect[Length] // top/right/bottom/left; Auto: not set

	ColumnGap, RowGap Length

	AlignItems     Align
	AlignSelf      Align
	JustifyItems   Align
	JustifySelf    Align
	AlignContent   Content
	JustifyContent Content

	FlexDirection FlexDirection
	FlexWrap      FlexWrap
	FlexGrow      float32
	FlexShrink    float32
	FlexBasis     Length

	GridTemplateColumns []GridTrack
	GridTemplateRows    []GridTrack
	GridAutoColumns     []TrackSize
	GridAutoRows        []TrackSize
	GridAutoFlow        GridAutoFlow
	GridColumn          Placement
	GridRow             Placement
}

// DefaultStyle has CSS's initial values (flex-shrink: 1, margins 0)
// with border-box sizing and display: flex. Note that a zero Length is
// Auto, so a zero Style has auto margins.
var DefaultStyle = Style{FlexShrink: 1, Margin: All(Pt(0))}

func (s *Style) isScroll(axis int) bool {
	o := s.OverflowX
	if axis == 1 {
		o = s.OverflowY
	}
	return o != OverflowVisible
}

// Size is a width and a height. In inputs, NaN means unknown.
type Size struct{ W, H float32 }

func (s Size) get(axis int) float32 {
	if axis == 0 {
		return s.W
	}
	return s.H
}

func (s *Size) set(axis int, v float32) {
	if axis == 0 {
		s.W = v
	} else {
		s.H = v
	}
}

// Available space on an axis: a definite amount, or one of these.
var (
	MaxContentSpace = float32(math.Inf(1))
	MinContentSpace = float32(math.Inf(-1))
)

// Inf is positive infinity.
var Inf = float32(math.Inf(1))

var nan = float32(math.NaN())

// None is an unknown size.
var None = nan

func isNaN(f float32) bool    { return f != f }
func isDef(f float32) bool    { return f == f }
func isInf(f float32) bool    { return f > math.MaxFloat32 || f < -math.MaxFloat32 }
func definite(a float32) bool { return a == a && !isInf(a) }

func or0(f float32) float32 {
	if f != f {
		return 0
	}
	return f
}

func orF(f, d float32) float32 {
	if f != f {
		return d
	}
	return f
}

// maybeMin and maybeMax ignore a NaN bound.
func maybeMin(v, bound float32) float32 {
	if bound == bound && bound < v {
		return bound
	}
	return v
}

func maybeMax(v, bound float32) float32 {
	if bound == bound && bound > v {
		return bound
	}
	return v
}

// clampN clamps v between lo and hi, ignoring NaN bounds; max wins
// after min as in CSS (min beats max).
func clampN(v, lo, hi float32) float32 {
	if v != v {
		return v
	}
	v = maybeMin(v, hi)
	return maybeMax(v, lo)
}

func maxf(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func minf(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}
