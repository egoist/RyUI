//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"math"

	"github.com/ebitengine/purego"

	. "github.com/egoist/nui/internal/darwin"
	"github.com/egoist/nui/ops"
)

var le = binary.LittleEndian

// nsColor is an NSColor for c (0 for the platform's default).
func (h *Host) nsColor(c ops.Color) ID {
	if s, ok := c.Semantic(); ok {
		base := h.semantic(s)
		if base == 0 {
			return 0
		}
		if a := c.Alpha(); a != 255 {
			return MsgFloatID(base, Sel("colorWithAlphaComponent:"), float64(a)/255)
		}
		return base
	}
	if r, g, b, a, ok := c.RGBA(); ok {
		return MsgColor(Class("NSColor"), Sel("colorWithSRGBRed:green:blue:alpha:"),
			float64(r)/255, float64(g)/255, float64(b)/255, float64(a)/255)
	}
	return 0
}

// semantic maps nui's semantic colors to AppKit's.
func (h *Host) semantic(s ops.SemanticColor) ID {
	var name string
	switch s {
	case ops.SemanticColorLabel:
		name = "labelColor"
	case ops.SemanticColorSecondaryLabel:
		name = "secondaryLabelColor"
	case ops.SemanticColorTertiaryLabel:
		name = "tertiaryLabelColor"
	case ops.SemanticColorBackground, ops.SemanticColorGroupedBackground:
		name = "windowBackgroundColor"
	case ops.SemanticColorSecondaryBackground, ops.SemanticColorSecondaryGroupedBackground:
		name = "controlBackgroundColor"
	case ops.SemanticColorSeparator:
		name = "separatorColor"
	case ops.SemanticColorAccent:
		if h.accent != 0 {
			if _, _, _, _, ok := h.accent.RGBA(); ok {
				return h.nsColor(h.accent)
			}
		}
		name = "controlAccentColor"
	case ops.SemanticColorRed:
		name = "systemRedColor"
	case ops.SemanticColorOrange:
		name = "systemOrangeColor"
	case ops.SemanticColorYellow:
		name = "systemYellowColor"
	case ops.SemanticColorGreen:
		name = "systemGreenColor"
	case ops.SemanticColorBlue:
		name = "systemBlueColor"
	case ops.SemanticColorPurple:
		name = "systemPurpleColor"
	case ops.SemanticColorPink:
		name = "systemPinkColor"
	case ops.SemanticColorGray:
		name = "systemGrayColor"
	case ops.SemanticColorClear:
		name = "clearColor"
	default:
		return 0
	}
	return Send(Class("NSColor"), name)
}

// accentColor is the app's accent, or the system's.
func (h *Host) accentColor() ID { return h.semantic(ops.SemanticColorAccent) }

// cgColor resolves an NSColor for a layer, in the current appearance.
func cgColor(c ID) uintptr {
	if c == 0 {
		return 0
	}
	return uintptr(Send(c, "CGColor"))
}

func (h *Host) layer(n *node) ID {
	if !SendBool(n.view, "wantsLayer") {
		Send(n.view, "setWantsLayer:", 1)
	}
	return Send(n.view, "layer")
}

// applyLayerColors sets the layer's colors from n's props: a layer's
// CGColor is fixed, so they're set again when the appearance changes.
func (h *Host) applyLayerColors(n *node) {
	if n.view == 0 {
		return
	}
	if n.background != 0 || n.borderColor != 0 || n.shadow != nil {
		if n.kind == ops.KindButton && n.background != 0 {
			Send(n.view, "setBezelColor:", uintptr(h.nsColor(n.background)))
		} else if n.background != 0 || n.borderColor != 0 {
			l := h.layer(n)
			if n.background != 0 {
				Send(l, "setBackgroundColor:", cgColor(h.nsColor(n.background)))
			}
			if n.borderColor != 0 {
				Send(l, "setBorderColor:", cgColor(h.nsColor(n.borderColor)))
			}
		}
		if n.shadow != nil {
			h.setShadow(n, n.shadow)
		}
	}
	if n.gradient != nil {
		h.setGradient(n, n.gradient)
	}
	if n.shape != nil {
		n.shape.colors(h, n)
	}
}

func (h *Host) setCommon(n *node, p ops.PropID, v *ops.Value) {
	if n.view == 0 {
		h.setNavCommon(n, p, v)
		return
	}
	switch p {
	case ops.PropBackground:
		n.background = ops.Color(v.Int)
		if n.kind == ops.KindButton {
			Send(n.view, "setBezelColor:", uintptr(h.nsColor(n.background)))
			return
		}
		h.withAppearance(func() {
			Send(h.layer(n), "setBackgroundColor:", cgColor(h.nsColor(n.background)))
		})
		if n.list != nil {
			n.list.background(h, n)
		}
	case ops.PropCornerRadius:
		n.cornerRadius = v.Float
		h.applyCorner(n)
	case ops.PropBorderWidth:
		n.borderWidth = v.Float
		MsgSetFloat(h.layer(n), Sel("setBorderWidth:"), v.Float)
	case ops.PropBorderColor:
		n.borderColor = ops.Color(v.Int)
		h.withAppearance(func() {
			Send(h.layer(n), "setBorderColor:", cgColor(h.nsColor(n.borderColor)))
		})
	case ops.PropOpacity:
		t := n.view
		if h.animating {
			t = Send(t, "animator")
		}
		MsgSetFloat(t, Sel("setAlphaValue:"), v.Float)
	case ops.PropHidden:
		Send(n.view, "setHidden:", Bool(v.Int != 0))
	case ops.PropClip:
		h.layer(n)
		if RespondsTo(n.view, "setClipsToBounds:") {
			Send(n.view, "setClipsToBounds:", Bool(v.Int != 0))
		}
		Send(Send(n.view, "layer"), "setMasksToBounds:", Bool(v.Int != 0))
	case ops.PropA11yLabel:
		n.a11yLabel = v.Str
		Send(n.view, "setAccessibilityLabel:", uintptr(String(v.Str)))
		if v.Str != "" && n.kind == ops.KindView {
			Send(n.view, "setAccessibilityElement:", 1)
		}
	case ops.PropA11yHint:
		Send(n.view, "setAccessibilityHelp:", uintptr(String(v.Str)))
	case ops.PropA11yRole:
		if r := a11yRole(ops.Role(v.Int)); r != "" {
			Send(n.view, "setAccessibilityRole:", uintptr(String(r)))
			Send(n.view, "setAccessibilityElement:", 1)
		}
	case ops.PropA11yHidden:
		Send(n.view, "setAccessibilityElement:", Bool(v.Int == 0))
		if v.Int != 0 {
			hideA11y(n.view)
		}
	case ops.PropA11yActions:
		h.setA11yActions(n, decodeStrings(v.Bytes))
	case ops.PropA11yAdjustable:
		n.adjustable = v.Int != 0
		if n.adjustable {
			Send(n.view, "setAccessibilityElement:", 1)
			Send(n.view, "setAccessibilityRole:", uintptr(String("AXIncrementor")))
		}
	case ops.PropColor:
		n.color = ops.Color(v.Int)
		h.applyColor(n)
	case ops.PropTextStyle:
		n.textStyle = int32(v.Int)
		h.applyFont(n)
	case ops.PropFontSize:
		n.fontSize = float32(v.Float)
		h.applyFont(n)
	case ops.PropFontWeight:
		n.fontWeight = int32(v.Int)
		h.applyFont(n)
	case ops.PropFontFamily:
		n.fontFamily = v.Str
		h.applyFont(n)
	case ops.PropTextAlign:
		n.textAlign = int32(v.Int)
		if n.rich != nil {
			n.rich.invalidate(h, n)
		} else if RespondsTo(n.view, "setAlignment:") {
			Send(n.view, "setAlignment:", uintptr(textAlignment(n.textAlign)))
		}
	case ops.PropLineLimit:
		n.lineLimit = int(v.Int)
		h.applyLineLimit(n)
	case ops.PropDisabled:
		if RespondsTo(n.view, "setEnabled:") {
			Send(n.view, "setEnabled:", Bool(v.Int == 0))
		}
		if n.web != nil || n.kind == ops.KindView {
			MsgSetFloat(n.view, Sel("setAlphaValue:"), map[bool]float64{true: 0.5, false: 1}[v.Int != 0])
		}
	case ops.PropMenu:
		n.menuActions = decodeActions(v.Bytes)
		h.setMenu(n)
	case ops.PropMenuTrigger:
		n.menuTrigger = int32(v.Int)
		h.setMenu(n)
	case ops.PropTransform:
		if len(v.Bytes) == 0 {
			n.transform = nil
		} else {
			n.transform = append(n.transform[:0], v.Bytes...)
		}
		h.setTransform(n, n.transform)
	case ops.PropShadow:
		n.shadow = append(n.shadow[:0], v.Bytes...)
		h.withAppearance(func() { h.setShadow(n, n.shadow) })
	case ops.PropGradient:
		n.gradient = append(n.gradient[:0], v.Bytes...)
		h.withAppearance(func() { h.setGradient(n, n.gradient) })
	case ops.PropMaterial:
		h.setMaterial(n, ops.Material(v.Int))
	case ops.PropSwipeActions:
		if n.cell != nil {
			n.cell.swipe = decodeActions(v.Bytes)
		}
	case ops.PropTransition:
		n.transition = nil
		if len(v.Bytes) >= 13 {
			n.transition = append([]byte(nil), v.Bytes...)
		}
	case ops.PropMatch:
		// Matched views: not yet on macOS.
	}
}

func a11yRole(r ops.Role) string {
	switch r {
	case ops.RoleButton:
		return "AXButton"
	case ops.RoleHeader:
		return "AXHeading"
	case ops.RoleImage:
		return "AXImage"
	case ops.RoleLink:
		return "AXLink"
	case ops.RoleText:
		return "AXStaticText"
	case ops.RoleSearch:
		return "AXTextField"
	}
	return ""
}

func hideA11y(v ID) {
	for _, s := range ArrayItems(Send(v, "subviews")) {
		Send(s, "setAccessibilityElement:", 0)
		hideA11y(s)
	}
}

func textAlignment(a int32) int {
	switch ops.TextAlign(a) {
	case ops.TextAlignCenter:
		return 1 // NSTextAlignmentCenter
	case ops.TextAlignEnd:
		return 2 // right
	case ops.TextAlignJustify:
		return 3
	}
	return 4 // natural
}

// applyCorner rounds the layer, at most half the shorter side (999: a
// capsule), as CSS and the other hosts clamp it.
func (h *Host) applyCorner(n *node) {
	if n.view == 0 || n.cornerRadius == 0 && !SendBool(n.view, "wantsLayer") {
		return
	}
	r := min(n.cornerRadius, n.frame.Size.Width/2, n.frame.Size.Height/2)
	if n.frame.Size.Width == 0 {
		r = n.cornerRadius
	}
	l := h.layer(n)
	MsgSetFloat(l, Sel("setCornerRadius:"), r)
	if n.effect != 0 {
		Send(n.effect, "setWantsLayer:", 1)
		MsgSetFloat(Send(n.effect, "layer"), Sel("setCornerRadius:"), r)
		Send(Send(n.effect, "layer"), "setMasksToBounds:", 1)
	}
	if n.kind == ops.KindImage || n.kind == ops.KindVideo {
		Send(l, "setMasksToBounds:", Bool(r > 0))
	}
}

// applyColor sets the text color of texts and fields, the tint of
// controls and images.
func (h *Host) applyColor(n *node) {
	c := h.nsColor(n.color)
	switch n.kind {
	case ops.KindText, ops.KindTextField:
		if c == 0 {
			c = Send(Class("NSColor"), "labelColor")
		}
		v := n.view
		if n.tracking != 0 {
			v = n.tracking
		}
		Send(v, "setTextColor:", uintptr(c))
	case ops.KindButton:
		if c != 0 && n.variant == int32(ops.ButtonVariantPlain) {
			h.buttonTitle(n)
		}
		if RespondsTo(n.view, "setContentTintColor:") {
			Send(n.view, "setContentTintColor:", uintptr(c))
		}
	case ops.KindRichText:
		n.rich.invalidate(h, n)
	case ops.KindImage, ops.KindSymbol:
		Send(n.view, "setContentTintColor:", uintptr(c))
	case ops.KindActivity, ops.KindProgress:
		if c != 0 && RespondsTo(n.view, "setContentFilters:") {
			// AppKit's indicators draw in the accent color; no tint.
		}
	case ops.KindSlider, ops.KindToggle:
		if RespondsTo(n.view, "setTrackFillColor:") {
			Send(n.view, "setTrackFillColor:", uintptr(c))
		}
	default:
		if RespondsTo(n.view, "setContentTintColor:") {
			Send(n.view, "setContentTintColor:", uintptr(c))
		}
	}
}

var (
	textStyleNames = [...]string{
		ops.TextStyleLargeTitle:  "NSCTFontUIFontLargeTitle",
		ops.TextStyleTitle:       "NSCTFontUIFontTitle1",
		ops.TextStyleTitle2:      "NSCTFontUIFontTitle2",
		ops.TextStyleTitle3:      "NSCTFontUIFontTitle3",
		ops.TextStyleHeadline:    "NSCTFontUIFontHeadline",
		ops.TextStyleSubheadline: "NSCTFontUIFontSubheadline",
		ops.TextStyleBody:        "NSCTFontUIFontBody",
		ops.TextStyleCallout:     "NSCTFontUIFontCallout",
		ops.TextStyleFootnote:    "NSCTFontUIFontFootnote",
		ops.TextStyleCaption:     "NSCTFontUIFontCaption1",
		ops.TextStyleCaption2:    "NSCTFontUIFontCaption2",
	}
	textStyleConsts = [...]string{
		ops.TextStyleLargeTitle:  "NSFontTextStyleLargeTitle",
		ops.TextStyleTitle:       "NSFontTextStyleTitle1",
		ops.TextStyleTitle2:      "NSFontTextStyleTitle2",
		ops.TextStyleTitle3:      "NSFontTextStyleTitle3",
		ops.TextStyleHeadline:    "NSFontTextStyleHeadline",
		ops.TextStyleSubheadline: "NSFontTextStyleSubheadline",
		ops.TextStyleBody:        "NSFontTextStyleBody",
		ops.TextStyleCallout:     "NSFontTextStyleCallout",
		ops.TextStyleFootnote:    "NSFontTextStyleFootnote",
		ops.TextStyleCaption:     "NSFontTextStyleCaption1",
		ops.TextStyleCaption2:    "NSFontTextStyleCaption2",
	}
	styleFonts = map[int32]ID{}
)

// styleFont is the system font of a text style (macOS's sizes: body 13).
func styleFont(style int32) ID {
	if style <= 0 || int(style) >= len(textStyleConsts) {
		style = int32(ops.TextStyleBody)
	}
	if f := styleFonts[style]; f != 0 {
		return f
	}
	name := Const(AppKit(), textStyleConsts[style])
	f := Send(Class("NSFont"), "preferredFontForTextStyle:options:", uintptr(name), uintptr(Dict()))
	Retain(f)
	styleFonts[style] = f
	return f
}

var weights = [...]float64{
	ops.FontWeightUltralight: -0.8,
	ops.FontWeightThin:       -0.6,
	ops.FontWeightLight:      -0.4,
	ops.FontWeightRegular:    0,
	ops.FontWeightMedium:     0.23,
	ops.FontWeightSemibold:   0.3,
	ops.FontWeightBold:       0.4,
	ops.FontWeightHeavy:      0.56,
	ops.FontWeightBlack:      0.62,
}

// font is the NSFont for a node's text props.
func (h *Host) font(textStyle, weight int32, size float32, family string) ID {
	base := styleFont(textStyle)
	pt := MsgFloat(base, Sel("pointSize"))
	if size > 0 {
		pt = float64(size)
	}
	if family != "" {
		if f := h.familyFont(family, pt, weight); f != 0 {
			return f
		}
	}
	if weight > 0 && int(weight) < len(weights) {
		return MsgFloat2ID(Class("NSFont"), Sel("systemFontOfSize:weight:"), pt, weights[weight])
	}
	if size > 0 {
		// The text style's font (its weight: a headline's bold) at the size.
		return MsgIDFloatID(Class("NSFont"), Sel("fontWithDescriptor:size:"), Send(base, "fontDescriptor"), pt)
	}
	return base
}

func (h *Host) nodeFont(n *node) ID {
	return h.font(n.textStyle, n.fontWeight, n.fontSize, n.fontFamily)
}

func (h *Host) applyFont(n *node) {
	switch {
	case n.rich != nil:
		n.rich.invalidate(h, n)
	case n.kind == ops.KindSymbol:
		h.applySymbol(n)
	case n.kind == ops.KindButton:
		Send(n.view, "setFont:", uintptr(h.nodeFont(n)))
		h.buttonTitle(n)
	case n.kind == ops.KindTextField && n.tracking != 0:
		Send(n.tracking, "setFont:", uintptr(h.nodeFont(n)))
	case RespondsTo(n.view, "setFont:"):
		Send(n.view, "setFont:", uintptr(h.nodeFont(n)))
	}
}

func (h *Host) applyLineLimit(n *node) {
	switch n.kind {
	case ops.KindText:
		Send(n.view, "setMaximumNumberOfLines:", uintptr(n.lineLimit))
		cell := Send(n.view, "cell")
		Send(cell, "setTruncatesLastVisibleLine:", Bool(n.lineLimit > 0))
		if n.lineLimit == 1 {
			Send(n.view, "setLineBreakMode:", 4) // byTruncatingTail
		} else {
			Send(n.view, "setLineBreakMode:", 0) // byWordWrapping
		}
	case ops.KindRichText:
		n.rich.invalidate(h, n)
	}
}

// setTransform applies Style.Transform (translate, scale, rotate, around
// the center) to the view's layer.
func (h *Host) setTransform(n *node, b []byte) {
	l := h.layer(n)
	f := n.frame
	if len(b) < 20 {
		MsgSetTransform(l, Sel("setAffineTransform:"), AffineTransform{A: 1, D: 1})
		return
	}
	tx, ty := float64(f32(b, 0)), float64(f32(b, 4))
	sx, sy := float64(f32(b, 8)), float64(f32(b, 12))
	rot := float64(f32(b, 16)) * math.Pi / 180
	// AppKit puts a layer's anchor at its origin: turn around the center.
	cx, cy := f.Size.Width/2, f.Size.Height/2
	cos, sin := math.Cos(rot), math.Sin(rot)
	// M = T(c + t) · R · S · T(-c), in the layer's (unflipped) space,
	// where a clockwise turn on screen is a negative angle.
	sin = -sin
	ty = -ty
	a, bb := cos*sx, sin*sx
	c, d := -sin*sy, cos*sy
	t := AffineTransform{A: a, B: bb, C: c, D: d}
	t.Tx = cx + tx - (a*cx + c*cy)
	t.Ty = cy + ty - (bb*cx + d*cy)
	MsgSetTransform(l, Sel("setAffineTransform:"), t)
}

func f32(b []byte, i int) float32 { return math.Float32frombits(le.Uint32(b[i:])) }

// setShadow sets a layer shadow: color, blur radius, offset.
func (h *Host) setShadow(n *node, b []byte) {
	l := h.layer(n)
	if len(b) < 20 || le.Uint64(b) == 0 {
		MsgSetFloat(l, Sel("setShadowOpacity:"), 0)
		return
	}
	c := h.nsColor(ops.Color(le.Uint64(b)))
	Send(l, "setShadowColor:", cgColor(c))
	msgSetFloat32(l, Sel("setShadowOpacity:"), 1)
	MsgSetFloat(l, Sel("setShadowRadius:"), float64(f32(b, 8))/2)
	// The layer's y axis points up.
	MsgSetSize(l, Sel("setShadowOffset:"), Size{Width: float64(f32(b, 12)), Height: -float64(f32(b, 16))})
	Send(l, "setMasksToBounds:", 0)
	if RespondsTo(n.view, "setClipsToBounds:") {
		Send(n.view, "setClipsToBounds:", 0)
	}
}

var msgSetFloat32Fn func(obj ID, sel SEL, v float32)

func msgSetFloat32(obj ID, s SEL, v float32) {
	if msgSetFloat32Fn == nil {
		purego.RegisterFunc(&msgSetFloat32Fn, MsgSendAddr())
	}
	msgSetFloat32Fn(obj, s, v)
}

// setGradient puts a CAGradientLayer under the view's content.
func (h *Host) setGradient(n *node, b []byte) {
	l := h.layer(n)
	if len(b) < 4 || le.Uint32(b) == 0 {
		if n.gradientLayer != 0 {
			Send(n.gradientLayer, "removeFromSuperlayer")
			Release(n.gradientLayer)
			n.gradientLayer = 0
		}
		return
	}
	if n.gradientLayer == 0 {
		n.gradientLayer = New("CAGradientLayer")
		Send(l, "insertSublayer:atIndex:", uintptr(n.gradientLayer), 0)
	}
	count := int(le.Uint32(b))
	if len(b) < 4+count*12+16 {
		return
	}
	colors := Send(Class("NSMutableArray"), "array")
	locs := Send(Class("NSMutableArray"), "array")
	for i := range count {
		o := 4 + i*12
		Send(colors, "addObject:", cgColor(h.nsColor(ops.Color(le.Uint64(b[o:])))))
		Send(locs, "addObject:", uintptr(NumberFloat(float64(f32(b, o+8)))))
	}
	o := 4 + count*12
	g := n.gradientLayer
	Send(g, "setColors:", uintptr(colors))
	Send(g, "setLocations:", uintptr(locs))
	// The layer's unit space has y up.
	MsgSetPoint(g, Sel("setStartPoint:"), Point{X: float64(f32(b, o)), Y: 1 - float64(f32(b, o+4))})
	MsgSetPoint(g, Sel("setEndPoint:"), Point{X: float64(f32(b, o+8)), Y: 1 - float64(f32(b, o+12))})
	h.layoutGradient(n)
}

func (h *Host) layoutGradient(n *node) {
	MsgSetRect(n.gradientLayer, Sel("setFrame:"), Rect{Size: n.frame.Size})
	MsgSetFloat(n.gradientLayer, Sel("setCornerRadius:"), MsgFloat(Send(n.view, "layer"), Sel("cornerRadius")))
}

// setMaterial puts an NSVisualEffectView behind the view's content.
func (h *Host) setMaterial(n *node, m ops.Material) {
	if m == ops.MaterialNone {
		if n.effect != 0 {
			Send(n.effect, "removeFromSuperview")
			Release(n.effect)
			n.effect = 0
		}
		return
	}
	if n.effect == 0 {
		n.effect = MsgInitRect(Send(Class("NSVisualEffectView"), "alloc"), Sel("initWithFrame:"), Rect{Size: n.frame.Size})
		Send(n.effect, "setBlendingMode:", 1) // within the window
		Send(n.effect, "setState:", 1)        // active
		Send(n.view, "addSubview:positioned:relativeTo:", uintptr(n.effect), nsWindowBelow, 0)
		h.applyCorner(n)
	}
	var material uintptr
	switch m {
	case ops.MaterialUltraThin, ops.MaterialThin:
		material = 13 // hudWindow
	case ops.MaterialRegular:
		material = 12 // underWindowBackground... popover-like
	case ops.MaterialThick:
		material = 6 // popover
	case ops.MaterialBar:
		material = 3 // titlebar
	}
	Send(n.effect, "setMaterial:", material)
}

// decodeActions reads a Menu prop's items.
func decodeActions(b []byte) []action {
	if len(b) < 4 {
		return nil
	}
	n := int(le.Uint32(b))
	b = b[4:]
	out := make([]action, 0, n)
	for range n {
		if len(b) < 5 {
			break
		}
		a := action{role: ops.AlertButtonRole(b[0])}
		b = b[1:]
		a.title, b = readString(b)
		a.icon, b = readString(b)
		out = append(out, a)
	}
	return out
}

func readString(b []byte) (string, []byte) {
	if len(b) < 4 {
		return "", nil
	}
	n := int(le.Uint32(b))
	if len(b) < 4+n {
		return "", nil
	}
	return string(b[4 : 4+n]), b[4+n:]
}

// decodeStrings reads a u32 count, then u32-length strings.
func decodeStrings(b []byte) []string {
	if len(b) < 4 {
		return nil
	}
	n := int(le.Uint32(b))
	b = b[4:]
	out := make([]string, 0, n)
	for range n {
		var s string
		s, b = readString(b)
		if b == nil && len(out) < n-1 {
			break
		}
		out = append(out, s)
	}
	return out
}

// a11yAction is a screen reader action of a view's (A11yActions).
type a11yAction struct {
	n     *node
	index int
}

// setA11yActions gives the view VoiceOver's custom actions, which send
// A11yAction with their index.
func (h *Host) setA11yActions(n *node, names []string) {
	for a, x := range h.a11yActions {
		if x.n == n {
			delete(h.a11yActions, a)
			Release(a)
		}
	}
	arr := Send(Class("NSMutableArray"), "array")
	for i, name := range names {
		a := Send(Send(Class("NSAccessibilityCustomAction"), "alloc"), "initWithName:target:selector:",
			uintptr(String(name)), uintptr(h.target), uintptr(Sel("a11yAction:")))
		h.a11yActions[a] = a11yAction{n, i}
		Send(arr, "addObject:", uintptr(a))
	}
	Send(n.view, "setAccessibilityCustomActions:", uintptr(arr))
	if len(names) > 0 {
		Send(n.view, "setAccessibilityElement:", 1)
	}
}
