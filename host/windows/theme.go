//go:build windows && amd64

package windows

import (
	"math"
	"strings"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// rgba is a resolved color.
type rgba struct{ r, g, b, a uint8 }

func (c rgba) ref() uint32 { return RGB(c.r, c.g, c.b) }
func (c rgba) argb() ARGB  { return MakeARGB(c.a, c.r, c.g, c.b) }

func (c rgba) withAlpha(a float32) rgba {
	c.a = uint8(float32(c.a)*min(max(a, 0), 1) + 0.5)
	return c
}

// over is c drawn over an opaque background: GDI text has no alpha.
func (c rgba) over(bg rgba) rgba {
	if c.a == 255 {
		return c
	}
	a := float32(c.a) / 255
	mix := func(x, y uint8) uint8 { return uint8(float32(x)*a + float32(y)*(1-a) + 0.5) }
	return rgba{mix(c.r, bg.r), mix(c.g, bg.g), mix(c.b, bg.b), 255}
}

func hex(v uint32) rgba { return rgba{uint8(v >> 16), uint8(v >> 8), uint8(v), 255} }

// palette is Windows 11's colors (WinUI's Fluent theme resources), light
// or dark, with the system's accent.
type palette struct {
	dark                       bool
	label, secondary, tertiary rgba
	disabled                   rgba
	bg, card, layer            rgba
	separator, stroke          rgba
	control, controlHot        rgba // a standard button's fill
	controlPressed             rgba
	controlStroke              rgba
	subtleHot, subtlePressed   rgba // a pressable view's highlight
	accent, accentHot          rgba
	accentPressed              rgba
	onAccent                   rgba
	accentText                 rgba // links, plain buttons
	red, orange, yellow        rgba
	green, blue, purple, pink  rgba
	gray                       rgba
	knob                       rgba // a switch's or slider's thumb
	track                      rgba // an unfilled slider or progress track
	fieldBg, fieldFocusBg      rgba
}

func newPalette(dark bool, accent ops.Color) palette {
	// The system accent and its shades: Settings, Personalization, Colors.
	shades, ok := AccentPalette()
	ac := func(i int) rgba {
		v := shades[i]
		return rgba{uint8(v), uint8(v >> 8), uint8(v >> 16), 255}
	}
	if !ok {
		def := [7]uint32{0x99EBFF, 0x4CC2FF, 0x0091F8, 0x0078D4, 0x0067C0, 0x003E92, 0x001A68}
		ac = func(i int) rgba { return hex(def[i]) }
	}
	var p palette
	p.dark = dark
	if dark {
		p.label = hex(0xFFFFFF)
		p.secondary = hex(0xCCCCCC)
		p.tertiary = hex(0x969696)
		p.disabled = hex(0x787878)
		p.bg = hex(0x202020)
		p.card = hex(0x2B2B2B)
		p.layer = hex(0x2C2C2C)
		p.separator = hex(0x383838)
		p.stroke = hex(0x303030)
		p.control = hex(0x2D2D2D)
		p.controlHot = hex(0x323232)
		p.controlPressed = hex(0x272727)
		p.controlStroke = hex(0x3A3A3A)
		p.subtleHot = rgba{255, 255, 255, 0x0F}
		p.subtlePressed = rgba{255, 255, 255, 0x0A}
		p.accent = ac(1) // Light2
		p.accentHot = ac(1).withAlpha(0.9)
		p.accentPressed = ac(1).withAlpha(0.8)
		p.onAccent = hex(0x000000)
		p.accentText = ac(0) // Light3
		p.red, p.orange, p.yellow = hex(0xFF99A4), hex(0xFFA552), hex(0xFCE100)
		p.green, p.blue, p.purple, p.pink = hex(0x6CCB5F), hex(0x60CDFF), hex(0xB4A0FF), hex(0xF7A1D1)
		p.gray = hex(0x9A9A9A)
		p.knob = hex(0x454545)
		p.track = rgba{255, 255, 255, 0x8B}
		p.fieldBg = hex(0x2D2D2D)
		p.fieldFocusBg = hex(0x1F1F1F)
	} else {
		p.label = hex(0x1A1A1A)
		p.secondary = hex(0x5D5D5D)
		p.tertiary = hex(0x8A8A8A)
		p.disabled = hex(0xA0A0A0)
		p.bg = hex(0xF3F3F3)
		p.card = hex(0xFBFBFB)
		p.layer = hex(0xF9F9F9)
		p.separator = hex(0xE5E5E5)
		p.stroke = hex(0xE5E5E5)
		p.control = hex(0xFEFEFE)
		p.controlHot = hex(0xF9F9F9)
		p.controlPressed = hex(0xF5F5F5)
		p.controlStroke = hex(0xD0D0D0)
		p.subtleHot = rgba{0, 0, 0, 0x09}
		p.subtlePressed = rgba{0, 0, 0, 0x06}
		p.accent = ac(4) // Dark1
		p.accentHot = ac(4).withAlpha(0.9)
		p.accentPressed = ac(4).withAlpha(0.8)
		p.onAccent = hex(0xFFFFFF)
		p.accentText = ac(5) // Dark2
		p.red, p.orange, p.yellow = hex(0xC42B1C), hex(0xF7630C), hex(0xC19C00)
		p.green, p.blue, p.purple, p.pink = hex(0x0F7B0F), hex(0x0067C0), hex(0x8764B8), hex(0xE3008C)
		p.gray = hex(0x8A8A8A)
		p.knob = hex(0xFFFFFF)
		p.track = rgba{0, 0, 0, 0x72}
		p.fieldBg = hex(0xFDFDFD)
		p.fieldFocusBg = hex(0xFFFFFF)
	}
	if r, g, b, _, ok := accent.RGBA(); ok {
		// The app's accent replaces the system's, as ryui.json's does on
		// iOS and Android.
		a := rgba{r, g, b, 255}
		p.accent, p.accentHot, p.accentPressed = a, a.withAlpha(0.9), a.withAlpha(0.8)
		p.accentText = a
		lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
		if lum > 160 {
			p.onAccent = hex(0x000000)
		} else {
			p.onAccent = hex(0xFFFFFF)
		}
	}
	return p
}

// resolve is a color of RyUI's: semantic colors are the theme's; ok is
// false for the platform's default (0).
func (h *Host) resolve(c ops.Color) (rgba, bool) {
	if s, ok := c.Semantic(); ok {
		base, ok := h.semantic(s)
		if !ok {
			return rgba{}, false
		}
		if a := c.Alpha(); a != 255 {
			base.a = uint8(uint32(base.a) * uint32(a) / 255)
		}
		return base, true
	}
	if r, g, b, a, ok := c.RGBA(); ok {
		return rgba{r, g, b, a}, true
	}
	return rgba{}, false
}

func (h *Host) semantic(s ops.SemanticColor) (rgba, bool) {
	p := &h.theme
	switch s {
	case ops.SemanticColorLabel:
		return p.label, true
	case ops.SemanticColorSecondaryLabel:
		return p.secondary, true
	case ops.SemanticColorTertiaryLabel:
		return p.tertiary, true
	case ops.SemanticColorBackground, ops.SemanticColorGroupedBackground:
		return p.bg, true
	case ops.SemanticColorSecondaryBackground, ops.SemanticColorSecondaryGroupedBackground:
		return p.card, true
	case ops.SemanticColorSeparator:
		return p.separator, true
	case ops.SemanticColorAccent:
		return p.accentText, true
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
		return rgba{}, true
	}
	return rgba{}, false
}

// brush is a cached solid brush (WM_CTLCOLOR* return one).
func (h *Host) brush(c rgba) uintptr {
	k := c.ref()
	if b := h.brushes[k]; b != 0 {
		return b
	}
	b := CreateSolidBrush(k)
	h.brushes[k] = b
	return b
}

// backdrop is the opaque color behind a view: its nearest ancestor's
// background, or the window's.
func (h *Host) backdrop(n *node) rgba {
	bg := h.theme.bg
	var chain []rgba
	for p := n; p != nil; p = p.parent {
		if c, ok := h.resolve(p.background); ok && c.a > 0 {
			chain = append(chain, c)
			if c.a == 255 {
				break
			}
		}
		if p.kind == ops.KindScreen || p.kind == ops.KindSheet || p.kind == ops.KindWindow {
			if len(chain) == 0 {
				chain = append(chain, h.theme.bg)
			}
			break
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		bg = chain[i].over(bg)
	}
	return bg
}

// Fonts.

// The text styles on Windows 11's type ramp (WinUI's): Caption 12,
// Body 14, Body Strong 14 semibold, Body Large 18, Subtitle 20 semibold,
// Title 28 semibold, Title Large 40 semibold, in Segoe UI Variable.
// iOS's styles map to the nearest: LargeTitle is Title Large, Title is
// Title, Title2 Subtitle, Title3 Body Large, Headline Body Strong, Body
// and Callout Body, Subheadline 13, Footnote and Caption Caption,
// Caption2 11.
var styleSizes = [...]struct {
	size   float32
	weight int32
}{
	ops.TextStyleDefault:     {14, 400},
	ops.TextStyleLargeTitle:  {40, 600},
	ops.TextStyleTitle:       {28, 600},
	ops.TextStyleTitle2:      {20, 600},
	ops.TextStyleTitle3:      {18, 400},
	ops.TextStyleHeadline:    {14, 600},
	ops.TextStyleSubheadline: {13, 400},
	ops.TextStyleBody:        {14, 400},
	ops.TextStyleCallout:     {14, 400},
	ops.TextStyleFootnote:    {12, 400},
	ops.TextStyleCaption:     {12, 400},
	ops.TextStyleCaption2:    {11, 400},
}

var weights = [...]int32{
	ops.FontWeightUltralight: 200,
	ops.FontWeightThin:       250,
	ops.FontWeightLight:      300,
	ops.FontWeightRegular:    400,
	ops.FontWeightMedium:     500,
	ops.FontWeightSemibold:   600,
	ops.FontWeightBold:       700,
	ops.FontWeightHeavy:      800,
	ops.FontWeightBlack:      900,
}

type fontKey struct {
	family    string
	px        int32
	weight    int32
	italic    bool
	underline bool
	strike    bool
}

// fontSpec is a text style, size and weight resolved: points and a GDI
// weight.
type fontSpec struct {
	size      float32
	weight    int32
	family    string
	italic    bool
	underline bool
	strike    bool
	mono      bool
	k         float32 // pixels per point: its window's (0: the main window's)
}

func (h *Host) spec(style, weight int32, size float32, family string) fontSpec {
	if style < 0 || int(style) >= len(styleSizes) {
		style = 0
	}
	s := fontSpec{size: styleSizes[style].size, weight: styleSizes[style].weight, family: family}
	if size > 0 {
		s.size = size
	}
	if weight > 0 && int(weight) < len(weights) {
		s.weight = weights[weight]
	}
	if fs := h.env.FontScale; fs > 0 {
		s.size *= fs
	}
	return s
}

func (h *Host) nodeSpec(n *node) fontSpec {
	s := h.spec(n.textStyle, n.fontWeight, n.fontSize, n.fontFamily)
	s.k = h.scaleOf(n)
	return s
}

var variable = map[bool]bool{}

// hasVariable reports whether Segoe UI Variable (Windows 11) is
// installed.
func (h *Host) hasVariable() bool {
	if v, ok := variable[true]; ok {
		return v
	}
	v := FontInstalled(h.measureDC, "Segoe UI Variable Text")
	variable[true] = v
	return v
}

// face picks the font family for a size: Segoe UI Variable's optical
// sizes (Small, Text, Display) where it's installed, else Segoe UI.
func (h *Host) face(s fontSpec) string {
	switch strings.ToLower(s.family) {
	case "monospace", "mono":
		return "Cascadia Mono"
	case "serif":
		return "Georgia"
	case "rounded", "":
	default:
		if f := h.customFamily(s.family); f != "" {
			return f
		}
		return s.family
	}
	if s.mono {
		return "Cascadia Mono"
	}
	if !h.hasVariable() {
		return "Segoe UI"
	}
	switch {
	case s.size < 12.5:
		return "Segoe UI Variable Small"
	case s.size < 20:
		return "Segoe UI Variable Text"
	}
	return "Segoe UI Variable Display"
}

// font is the HFONT for a spec at its window's DPI, cached (by its size in
// pixels: windows at two DPIs have fonts of their own).
func (h *Host) font(s fontSpec) uintptr {
	scale := s.k
	if scale <= 0 {
		scale = h.scale
	}
	k := fontKey{family: h.face(s), px: int32(math.Round(float64(s.size * scale))), weight: s.weight, italic: s.italic, underline: s.underline, strike: s.strike}
	if f := h.fonts[k]; f != 0 {
		return f
	}
	lf := LogFont{Height: -k.px, Weight: k.weight, CharSet: 1, Quality: CLEARTYPE_QUALITY}
	if k.italic {
		lf.Italic = 1
	}
	if k.underline {
		lf.Underline = 1
	}
	if k.strike {
		lf.StrikeOut = 1
	}
	copy(lf.FaceName[:31], UTF16Units(k.family))
	f := CreateFont(&lf)
	h.fonts[k] = f
	return f
}

func (h *Host) nodeFont(n *node) uintptr { return h.font(h.nodeSpec(n)) }

// bodyFont is the controls' font: Body, 14, in n's window.
func (h *Host) bodyFont(n *node) uintptr {
	s := h.spec(0, 0, 0, "")
	s.k = h.scaleOf(n)
	return h.font(s)
}

// clearFonts drops the fonts (the DPI or text size changed).
func (h *Host) clearFonts() {
	for k, f := range h.fonts {
		DeleteObject(f)
		delete(h.fonts, k)
	}
}

// icons is Segoe Fluent Icons (Windows 11; Segoe MDL2 Assets before it).
func (h *Host) iconFace() string {
	if Windows11() {
		return "Segoe Fluent Icons"
	}
	return "Segoe MDL2 Assets"
}

func (h *Host) iconFont(n *node, size float32) uintptr {
	return h.font(fontSpec{size: size, weight: 400, family: h.iconFace(), k: h.scaleOf(n)})
}
