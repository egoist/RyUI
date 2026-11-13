//go:build linux && !android

package linux

import (
	"encoding/binary"
	"math"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

var le = binary.LittleEndian

// reader reads a prop's or command's bytes; past the end it reads zeros.
type reader struct {
	b   []byte
	off int
}

func (r *reader) ok(n int) bool { return r.off+n <= len(r.b) }
func (r *reader) u8() uint8 {
	if !r.ok(1) {
		r.off = len(r.b)
		return 0
	}
	v := r.b[r.off]
	r.off++
	return v
}
func (r *reader) u32() uint32 {
	if !r.ok(4) {
		r.off = len(r.b)
		return 0
	}
	v := le.Uint32(r.b[r.off:])
	r.off += 4
	return v
}
func (r *reader) u64() uint64 {
	if !r.ok(8) {
		r.off = len(r.b)
		return 0
	}
	v := le.Uint64(r.b[r.off:])
	r.off += 8
	return v
}
func (r *reader) f32() float32 { return math.Float32frombits(r.u32()) }
func (r *reader) f64() float64 { return math.Float64frombits(r.u64()) }
func (r *reader) str() string {
	n := int(r.u32())
	if !r.ok(n) {
		r.off = len(r.b)
		return ""
	}
	s := string(r.b[r.off : r.off+n])
	r.off += n
	return s
}
func (r *reader) rest() []byte { b := r.b[r.off:]; r.off = len(r.b); return b }

// decodeMenu reads a Menu-encoded list: u32 count, per item u8 role, a
// title and an icon.
func decodeMenu(b []byte) []action {
	r := reader{b: b}
	n := int(r.u32())
	out := make([]action, 0, min(n, 256))
	for i := 0; i < n && r.off < len(b); i++ {
		role := ops.AlertButtonRole(r.u8())
		title := r.str()
		icon := r.str()
		out = append(out, action{role: role, title: title, icon: icon})
	}
	return out
}

type shadow struct {
	color  ops.Color
	blur   float32
	dx, dy float32
}

type gradientStop struct {
	color    ops.Color
	location float32
}

type transform struct {
	set            bool
	tx, ty, sx, sy float32
	rotation       float32 // degrees, clockwise
}

func (h *Host) setCommon(n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropBackground:
		n.background = ops.Color(v.Int)
		h.styleChanged(n)
	case ops.PropCornerRadius:
		n.cornerRadius = float32(v.Float)
		queueDraw(n)
	case ops.PropBorderWidth:
		n.borderWidth = float32(v.Float)
		queueDraw(n)
	case ops.PropBorderColor:
		n.borderColor = ops.Color(v.Int)
		queueDraw(n)
	case ops.PropOpacity:
		n.opacity = float32(v.Float)
		if n.widget != 0 {
			if h.animating {
				h.animateOpacity(n)
			} else {
				WidgetSetOpacity(n.widget, v.Float)
			}
		}
	case ops.PropHidden:
		n.hidden = v.Int != 0
		if n.widget != 0 {
			WidgetSetVisible(n.widget, !n.hidden)
		}
	case ops.PropClip:
		n.clip = v.Int != 0
		queueDraw(n)
	case ops.PropA11yLabel:
		n.a11yLabel = v.Str
		h.updateA11y(n)
	case ops.PropA11yHint, ops.PropA11yRole, ops.PropA11yHidden:
		h.setA11y(n, p, v)
	case ops.PropColor:
		n.color = ops.Color(v.Int)
		h.applyText(n)
	case ops.PropTextStyle:
		n.textStyle = int32(v.Int)
		h.applyText(n)
	case ops.PropFontSize:
		n.fontSize = float32(v.Float)
		h.applyText(n)
	case ops.PropFontWeight:
		n.fontWeight = int32(v.Int)
		h.applyText(n)
	case ops.PropFontFamily:
		n.fontFamily = v.Str
		h.applyText(n)
	case ops.PropTextAlign:
		n.textAlign = int32(v.Int)
		h.applyText(n)
	case ops.PropLineLimit:
		n.lineLimit = int(v.Int)
		h.applyText(n)
	case ops.PropDisabled:
		n.disabled = v.Int != 0
		if n.widget != 0 {
			WidgetSetSensitive(n.widget, !n.disabled)
		}
	case ops.PropMenu:
		n.menu = decodeMenu(v.Bytes)
		h.menuChanged(n)
	case ops.PropMenuTrigger:
		n.menuTrigger = int32(v.Int)
		h.menuChanged(n)
	case ops.PropTransform:
		r := reader{b: v.Bytes}
		t := transform{set: len(v.Bytes) >= 20}
		t.tx, t.ty, t.sx, t.sy, t.rotation = r.f32(), r.f32(), r.f32(), r.f32(), r.f32()
		if t.set && t.tx == 0 && t.ty == 0 && t.sx == 1 && t.sy == 1 && t.rotation == 0 {
			t.set = false
		}
		h.setTransform(n, t)
	case ops.PropShadow:
		r := reader{b: v.Bytes}
		n.shadow = shadow{color: ops.Color(r.u64()), blur: r.f32(), dx: r.f32(), dy: r.f32()}
		queueDraw(n)
	case ops.PropGradient:
		r := reader{b: v.Bytes}
		c := int(r.u32())
		n.gradient = n.gradient[:0]
		for i := 0; i < c && r.ok(12); i++ {
			n.gradient = append(n.gradient, gradientStop{color: ops.Color(r.u64()), location: r.f32()})
		}
		n.gradientLine = [4]float32{r.f32(), r.f32(), r.f32(), r.f32()}
		queueDraw(n)
	case ops.PropTransition:
		h.setTransition(n, v.Bytes)
	case ops.PropMaterial:
		n.material = int32(v.Int)
		queueDraw(n)
	case ops.PropLayoutDirection:
		n.dir = ops.LayoutDirection(v.Int)
		h.applyDirection(n)
	case ops.PropContentInset:
		e := v.Edges
		n.inset = [4]float32{e[0], e[1], e[2], e[3]}
		if n.widget != 0 {
			Call(FnQueueAllocate, n.widget)
		}
	case ops.PropTooltip:
		if n.widget != 0 {
			WidgetSetTooltipText(n.widget, v.Str)
		}
	case ops.PropInterceptBack:
		n.interceptBack = v.Int != 0
	default:
		h.setCommonMore(n, p, v)
	}
}

// styleChanged redraws a view whose look changed, and its control's.
func (h *Host) styleChanged(n *node) {
	queueDraw(n)
	if n.kind == ops.KindButton {
		h.styleButton(n)
	}
}

func (h *Host) setTransform(n *node, t transform) {
	if h.animating && n.placed {
		h.animateTransform(n, t)
		return
	}
	n.transform = t
	if n.widget != 0 {
		if p := WidgetGetParent(n.widget); p != 0 {
			Call(FnQueueAllocate, p)
		}
	}
}

// applyText sets a text-showing control's font, color and alignment from
// its node's text props.
func (h *Host) applyText(n *node) {
	if n.kind == ops.KindSymbol {
		h.styleSymbol(n)
		return
	}
	if n.inner == 0 {
		h.applyTextMore(n)
		return
	}
	switch n.kind {
	case ops.KindText:
		h.setLabelAttrs(n, n.inner)
		LabelSetXAlign(n.inner, h.xalign(n))
		j := int32(JustifyLeft)
		switch h.physicalAlign(n) {
		case 1:
			j = JustifyCenter
		case 2:
			j = JustifyRight
		case 3:
			j = JustifyFill
		}
		LabelSetJustify(n.inner, j)
		if n.lineLimit > 0 {
			LabelSetLines(n.inner, int32(n.lineLimit))
			LabelSetEllipsize(n.inner, EllipsizeEnd)
		} else {
			LabelSetLines(n.inner, -1)
			LabelSetEllipsize(n.inner, EllipsizeNone)
		}
	case ops.KindButton:
		h.styleButton(n)
	case ops.KindTextField:
		h.styleField(n)
	default:
		h.applyTextMore(n)
	}
}

// physicalAlign is a node's text alignment on screen: 0 left, 1 center,
// 2 right, 3 justified (Start and End follow the way it reads).
func (h *Host) physicalAlign(n *node) int {
	switch ops.TextAlign(n.textAlign) {
	case ops.TextAlignCenter:
		return 1
	case ops.TextAlignJustify:
		return 3
	case ops.TextAlignEnd:
		if n.rtl {
			return 0
		}
		return 2
	}
	if n.rtl {
		return 2
	}
	return 0
}

func (h *Host) xalign(n *node) float32 {
	switch h.physicalAlign(n) {
	case 1:
		return 0.5
	case 2:
		return 1
	}
	return 0
}

// attrs are a node's text props as Pango attributes: its font's family,
// size and weight, and its color.
func (h *Host) attrs(n *node, withColor bool) Ptr {
	list := AttrListNew()
	size, weight := h.font(n)
	AttrListInsert(list, AttrSizeNewAbsolute(int32(size*1024+0.5)))
	AttrListInsert(list, AttrWeightNew(weight))
	if fam := h.family(n); fam != "" {
		AttrListInsert(list, AttrFamilyNew(fam))
	}
	if withColor {
		if c, ok := h.resolve(n.color); ok {
			AttrListInsert(list, AttrForegroundNew(u16(c.R), u16(c.G), u16(c.B)))
			if c.A < 1 {
				AttrListInsert(list, AttrForegroundAlphaNew(u16(c.A)))
			}
		}
	}
	return list
}

func u16(f float32) uint16 { return uint16(min(max(f, 0), 1)*65535 + 0.5) }

func (h *Host) setLabelAttrs(n *node, label Ptr) {
	list := h.attrs(n, true)
	if _, ok := h.resolve(n.color); !ok {
		// The label color: libadwaita's foreground, which GTK draws with.
	}
	LabelSetAttributes(label, list)
	AttrListUnref(list)
}

// family is the font family a node's FontFamily names: the app's font,
// a generic family, or the desktop's.
func (h *Host) family(n *node) string {
	switch n.fontFamily {
	case "":
		return ""
	case "monospace", "mono":
		return "monospace"
	case "serif":
		return "serif"
	case "rounded":
		return "" // no rounded system face on Linux
	}
	return h.customFamily(n.fontFamily)
}
