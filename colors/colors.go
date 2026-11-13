// Package colors is where a ui.Color comes from: the platform's semantic
// colors, which follow dark mode and the user's accent with no app code,
// and colors of the app's own (RGB, Hex).
//
//	ui.Style{Color: colors.SecondaryLabel, Background: colors.Hex(0x1e88e5)}
package colors

import (
	"strconv"
	"strings"

	"github.com/egoist/asuka/ops"
)

// RGB makes an opaque color.
func RGB(r, g, b uint8) ops.Color { return ops.RGBA(r, g, b, 255) }

// RGBA makes a color with alpha.
func RGBA(r, g, b, a uint8) ops.Color { return ops.RGBA(r, g, b, a) }

// Hex makes an opaque color from 0xRRGGBB.
func Hex(v uint32) ops.Color { return RGB(uint8(v>>16), uint8(v>>8), uint8(v)) }

// ParseHex parses "#rrggbb" or "#rrggbbaa" (the "#" optional).
func ParseHex(s string) (ops.Color, bool) {
	s = strings.TrimPrefix(s, "#")
	if len(s) != 6 && len(s) != 8 {
		return 0, false
	}
	var v [4]uint8
	v[3] = 255
	for i := 0; i < len(s)/2; i++ {
		n, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			return 0, false
		}
		v[i] = uint8(n)
	}
	return RGBA(v[0], v[1], v[2], v[3]), true
}

func semantic(c ops.SemanticColor) ops.Color { return ops.SemanticColorValue(c) }

// Semantic colors, resolved by the host (UIKit's semantic colors,
// Material 3's color roles), so dark mode needs no app code.
var (
	Label               = semantic(ops.SemanticColorLabel)
	SecondaryLabel      = semantic(ops.SemanticColorSecondaryLabel)
	TertiaryLabel       = semantic(ops.SemanticColorTertiaryLabel)
	Background          = semantic(ops.SemanticColorBackground)
	SecondaryBackground = semantic(ops.SemanticColorSecondaryBackground)
	GroupedBackground   = semantic(ops.SemanticColorGroupedBackground)
	// SecondaryGroupedBackground is a grouped list's rows, on
	// GroupedBackground (iOS's inset grouped cells; Material's surface
	// container).
	SecondaryGroupedBackground = semantic(ops.SemanticColorSecondaryGroupedBackground)
	Separator                  = semantic(ops.SemanticColorSeparator)
	Accent                     = semantic(ops.SemanticColorAccent)
	// OnAccent is what reads on Accent: an icon or text on a view filled with
	// it. White on iOS; Material's on-primary on Android, whose accent is light
	// in dark mode.
	OnAccent = semantic(ops.SemanticColorOnAccent)
	Red      = semantic(ops.SemanticColorRed)
	Orange   = semantic(ops.SemanticColorOrange)
	Yellow   = semantic(ops.SemanticColorYellow)
	Green    = semantic(ops.SemanticColorGreen)
	Blue     = semantic(ops.SemanticColorBlue)
	Purple   = semantic(ops.SemanticColorPurple)
	Pink     = semantic(ops.SemanticColorPink)
	Gray     = semantic(ops.SemanticColorGray)
	Clear    = semantic(ops.SemanticColorClear)
)
