//go:build linux && !android

package linux

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// palette is libadwaita's colors for the current style (light, dark, high
// contrast), read from the theme's named colors where it has them.
type palette struct {
	fg, bg, view, card, header RGBA
	accent, accentBg, onAccent RGBA
	red, orange, yellow, green RGBA
	blue, purple, pink, gray   RGBA
}

func hexRGBA(v uint32) RGBA {
	return RGBA{R: float32(v>>16&0xff) / 255, G: float32(v>>8&0xff) / 255, B: float32(v&0xff) / 255, A: 1}
}

// over is c composited over an opaque bg.
func over(c, bg RGBA) RGBA {
	a := c.A
	return RGBA{R: c.R*a + bg.R*(1-a), G: c.G*a + bg.G*(1-a), B: c.B*a + bg.B*(1-a), A: 1}
}

func withAlpha(c RGBA, a float32) RGBA { c.A *= a; return c }

// defaultPalette is libadwaita 1.5's own, for when the theme names none.
func defaultPalette(dark bool) palette {
	var p palette
	if dark {
		p.fg = hexRGBA(0xffffff)
		p.bg = hexRGBA(0x242424)
		p.view = hexRGBA(0x1e1e1e)
		p.card = over(RGBA{R: 1, G: 1, B: 1, A: 0.08}, p.bg)
		p.header = hexRGBA(0x303030)
		p.accent = hexRGBA(0x78aeed)
		p.red, p.orange, p.yellow = hexRGBA(0xff7b63), hexRGBA(0xffa348), hexRGBA(0xf8e45c)
		p.green, p.blue, p.purple = hexRGBA(0x8ff0a4), hexRGBA(0x99c1f1), hexRGBA(0xdc8add)
		p.pink, p.gray = hexRGBA(0xf5a3cf), hexRGBA(0x9a9996)
	} else {
		p.fg = RGBA{A: 0.8}
		p.bg = hexRGBA(0xfafafa)
		p.view = hexRGBA(0xffffff)
		p.card = hexRGBA(0xffffff)
		p.header = hexRGBA(0xebebeb)
		p.accent = hexRGBA(0x1c71d8)
		p.red, p.orange, p.yellow = hexRGBA(0xc01c28), hexRGBA(0xe66100), hexRGBA(0xc88800)
		p.green, p.blue, p.purple = hexRGBA(0x26a269), hexRGBA(0x1c71d8), hexRGBA(0x813d9c)
		p.pink, p.gray = hexRGBA(0xc4317c), hexRGBA(0x77767b)
	}
	p.accentBg = hexRGBA(0x3584e4)
	p.onAccent = hexRGBA(0xffffff)
	return p
}

// readPalette reads the theme's named colors through a widget's style.
func (h *Host) readPalette(w Ptr) palette {
	p := defaultPalette(h.dark)
	if w == 0 || StyleContextLookupColor == nil {
		h.applyAccent(&p)
		return p
	}
	ctx := WidgetGetStyleContext(w)
	look := func(name string, dst *RGBA) {
		var c RGBA
		if StyleContextLookupColor(ctx, name, &c) {
			*dst = c
		}
	}
	look("window_fg_color", &p.fg)
	look("window_bg_color", &p.bg)
	look("view_bg_color", &p.view)
	look("card_bg_color", &p.card)
	if p.card.A < 1 {
		p.card = over(p.card, p.bg)
	}
	look("headerbar_bg_color", &p.header)
	look("accent_color", &p.accent)
	look("accent_bg_color", &p.accentBg)
	look("accent_fg_color", &p.onAccent)
	look("destructive_color", &p.red)
	look("success_color", &p.green)
	look("warning_color", &p.yellow)
	h.applyAccent(&p)
	return p
}

// applyAccent puts the app's accent in the palette (asuka.json's, as on
// every platform).
func (h *Host) applyAccent(p *palette) {
	if r, g, b, _, ok := h.accent.RGBA(); ok {
		c := RGBA{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: 1}
		p.accent, p.accentBg = c, c
		if 0.299*float32(r)+0.587*float32(g)+0.114*float32(b) > 160 {
			p.onAccent = RGBA{A: 0.8}
		} else {
			p.onAccent = hexRGBA(0xffffff)
		}
	}
}

// resolve is a color of Asuka's: semantic colors are the theme's; ok is
// false for the platform's default (0).
func (h *Host) resolve(c ops.Color) (RGBA, bool) {
	if s, ok := c.Semantic(); ok {
		base, ok := h.semantic(s)
		if !ok {
			return RGBA{}, false
		}
		if a := c.Alpha(); a != 255 {
			base.A *= float32(a) / 255
		}
		return base, true
	}
	if r, g, b, a, ok := c.RGBA(); ok {
		return RGBA{R: float32(r) / 255, G: float32(g) / 255, B: float32(b) / 255, A: float32(a) / 255}, true
	}
	return RGBA{}, false
}

func (h *Host) semantic(s ops.SemanticColor) (RGBA, bool) {
	p := &h.theme
	switch s {
	case ops.SemanticColorLabel:
		return p.fg, true
	case ops.SemanticColorSecondaryLabel:
		return withAlpha(p.fg, 0.55), true // .dim-label
	case ops.SemanticColorTertiaryLabel:
		return withAlpha(p.fg, 0.35), true
	case ops.SemanticColorBackground, ops.SemanticColorGroupedBackground:
		return p.bg, true
	case ops.SemanticColorSecondaryBackground, ops.SemanticColorSecondaryGroupedBackground:
		return p.card, true
	case ops.SemanticColorSeparator:
		return withAlpha(p.fg, 0.15), true
	case ops.SemanticColorAccent:
		return p.accent, true
	case ops.SemanticColorRed:
		return p.red, true
	case ops.SemanticColorOrange:
		return p.orange, true
	case ops.SemanticColorYellow:
		return p.yellow, true
	case ops.SemanticColorGreen:
		return p.green, true
	case ops.SemanticColorBlue:
		return p.blue, true
	case ops.SemanticColorPurple:
		return p.purple, true
	case ops.SemanticColorPink:
		return p.pink, true
	case ops.SemanticColorGray:
		return p.gray, true
	case ops.SemanticColorClear:
		return RGBA{}, true
	case ops.SemanticColorOnAccent:
		return p.onAccent, true
	}
	return RGBA{}, false
}

// cssColor is c for a style sheet.
func cssColor(c RGBA) string {
	return fmt.Sprintf("rgba(%d,%d,%d,%.3f)", int(c.R*255+0.5), int(c.G*255+0.5), int(c.B*255+0.5), c.A)
}

// The host's style sheet: the app's accent, and classes for what
// libadwaita has no style of (a tinted button).
const hostCSS = `
button.asuka-tinted { background-color: alpha(@accent_bg_color, 0.15); color: @accent_color; }
button.asuka-tinted:hover { background-color: alpha(@accent_bg_color, 0.2); }
button.asuka-tinted:active { background-color: alpha(@accent_bg_color, 0.3); }
button.asuka-plain { color: @accent_color; }
entry.asuka-plain, text.asuka-plain { background: none; box-shadow: none; outline: none; padding: 0; min-height: 0; }
asuka-view.asuka-focus-ring:focus-visible { outline: 2px solid alpha(@accent_color, 0.5); outline-offset: -2px; }
scrolledwindow.asuka-field { background-color: alpha(currentColor, 0.1); border-radius: 6px; outline: 0 solid transparent; outline-offset: 2px; transition: outline-color 200ms ease-out, outline-width 200ms ease-out, outline-offset 200ms ease-out; }
scrolledwindow.asuka-field:focus-within { outline-color: alpha(@accent_color, 0.5); outline-width: 2px; outline-offset: -2px; }
scrolledwindow.asuka-field:disabled { filter: opacity(0.5); }
textview.asuka-clear, textview.asuka-clear > text { background: none; }
`

// loadTheme reads the style (dark, the palette, fonts) and adds the
// host's style sheet with the app's accent.
func (h *Host) loadTheme() {
	if boot.Accent != "" {
		h.accent = parseAccent(boot.Accent)
	}
	sm := AdwStyleManagerGetDefault()
	h.dark = AdwStyleManagerGetDark(sm)
	h.updateCSS()
	h.base = systemFont()
	h.fontScale = textScale()
}

// updateCSS sets the app's style sheet: the host's classes, and the
// accent (1.5's named colors, 1.6's variables).
func (h *Host) updateCSS() {
	css := hostCSS
	if r, g, b, _, ok := h.accent.RGBA(); ok {
		c := fmt.Sprintf("rgb(%d,%d,%d)", r, g, b)
		css += "@define-color accent_bg_color " + c + ";\n@define-color accent_color " + c + ";\n"
		css += ":root { --accent-bg-color: " + c + "; --accent-color: " + c + "; }\n"
	}
	d := DisplayGetDefault()
	if d == 0 {
		return
	}
	if h.css == 0 {
		h.css = CSSProviderNew()
		StyleContextAddProviderForDisplay(d, h.css, 700) // above the theme (600), below the user's
	}
	CSSProviderLoadFromString(h.css, css)
}

func parseAccent(s string) ops.Color {
	s = strings.TrimPrefix(s, "#")
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0
	}
	switch len(s) {
	case 6:
		return ops.RGBA(uint8(v>>16), uint8(v>>8), uint8(v), 255)
	case 8:
		return ops.RGBA(uint8(v>>24), uint8(v>>16), uint8(v>>8), uint8(v))
	}
	return 0
}

// baseFont is the desktop's interface font: GTK's gtk-font-name.
type baseFont struct {
	family string
	px     float32 // its size in points at 96 DPI (CSS pixels): Asuka's points
}

func systemFont() baseFont {
	f := baseFont{family: "Adwaita Sans", px: 11 * 96 / 72.0}
	s := SettingsGetDefault()
	if s == 0 {
		return f
	}
	name := GetStringProperty(s, "gtk-font-name") // "Cantarell 11"
	if i := strings.LastIndexByte(name, ' '); i > 0 {
		if pt, err := strconv.ParseFloat(name[i+1:], 32); err == nil && pt > 0 {
			f.family = name[:i]
			f.px = float32(pt * 96 / 72)
		}
	}
	return f
}

// textScale is the desktop's text scaling (GNOME's Large Text, KDE's
// font DPI): GTK's Xft DPI over 96.
func textScale() float32 {
	s := SettingsGetDefault()
	if s == 0 {
		return 1
	}
	dpi := GetIntProperty(s, "gtk-xft-dpi")
	if dpi <= 0 {
		return 1
	}
	return float32(dpi) / 1024 / 96
}

// The text styles as libadwaita's type classes, relative to the
// interface font (11 pt, 14.67 points): LargeTitle is .title-1 (181%,
// heavy), Title .title-2 (136%, heavy), Title2 .title-3 (136%, bold),
// Title3 .title-4 (118%, bold), Headline .heading (bold), Subheadline
// 91%, Body and Callout the body, Footnote and Caption .caption (82%),
// Caption2 75%.
var styleSizes = [...]struct {
	scale  float32
	weight int32
}{
	ops.TextStyleDefault:     {1, 400},
	ops.TextStyleLargeTitle:  {1.81, 800},
	ops.TextStyleTitle:       {1.36, 800},
	ops.TextStyleTitle2:      {1.36, 700},
	ops.TextStyleTitle3:      {1.18, 700},
	ops.TextStyleHeadline:    {1, 700},
	ops.TextStyleSubheadline: {0.91, 400},
	ops.TextStyleBody:        {1, 400},
	ops.TextStyleCallout:     {1, 400},
	ops.TextStyleFootnote:    {0.82, 400},
	ops.TextStyleCaption:     {0.82, 400},
	ops.TextStyleCaption2:    {0.75, 400},
}

var weights = [...]int32{
	ops.FontWeightUltralight: 200,
	ops.FontWeightThin:       100,
	ops.FontWeightLight:      300,
	ops.FontWeightRegular:    400,
	ops.FontWeightMedium:     500,
	ops.FontWeightSemibold:   600,
	ops.FontWeightBold:       700,
	ops.FontWeightHeavy:      800,
	ops.FontWeightBlack:      900,
}

// font is a node's text style resolved: its size in points (scaled by
// the desktop's text scaling) and weight.
func (h *Host) font(n *node) (size float32, weight int32) {
	st := n.textStyle
	if st < 0 || int(st) >= len(styleSizes) {
		st = 0
	}
	size = h.base.px * styleSizes[st].scale
	weight = styleSizes[st].weight
	if n.fontSize > 0 {
		size = n.fontSize
	}
	if n.fontWeight > 0 && int(n.fontWeight) < len(weights) {
		weight = weights[n.fontWeight]
	}
	if h.fontScale > 0 {
		size *= h.fontScale
	}
	return size, weight
}
