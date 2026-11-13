// Package fonts is a ui.Style's Font: the platform's semantic text styles,
// scaled by Dynamic Type and Android's font scale, the system font at a
// size, and the app's own families, in a weight.
//
//	ui.Style{Font: fonts.Headline}
//	ui.Style{Font: fonts.Body.Weighted(fonts.Semibold)}
//	ui.Style{Font: fonts.Custom("Inter", fonts.Title)}
package fonts

import "github.com/egoist/asuka/ops"

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
	// Family is a font of the app's: a font file's name in its assets
	// (assets/fonts/Inter-Bold.ttf is "Inter-Bold"), a family of them
	// ("Inter": Inter-Regular, Inter-Bold..., in Weight), a generic
	// family ("serif", "monospace", "rounded": the system font's designs
	// on iOS; Android has no rounded one), or a system family
	// ("Georgia"). It's scaled as its text style is (Dynamic Type,
	// Android's font scale). Empty, or one the platform hasn't: the
	// system font.
	Family string
}

// Weighted returns f with weight w.
func (f Font) Weighted(w Weight) Font { f.Weight = w; return f }

// Custom is f in the font family (see Font.Family), scaled as f is:
// Custom("Inter-Bold", Title).
func Custom(family string, f Font) Font { f.Family = family; return f }

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
