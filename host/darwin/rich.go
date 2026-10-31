//go:build darwin && !ios

package darwin

import (
	"math"
	"strings"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/nui/internal/darwin"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
)

// A RichText is an NSTextView (TextKit's typesetter, links, selection
// and accessibility), not editable, without insets, so what's measured
// is what's drawn. Outside a Selectable it takes only clicks on links:
// others go to the views under it. Inline components are attachments of
// their size; their views are placed where the text puts them.
type rich struct {
	view     ID
	content  []byte
	inlines  []inlineSize
	dirty    bool
	str      ID // the attributed string (+1)
	width    float64
	selectOK bool
}

type inlineSize struct {
	id   ops.NodeID
	w, h float64
}

func newRich() *rich { return &rich{dirty: true} }

func registerRichClass(flipped objc.MethodDef) {
	DefineClass("NuiTextView", "NSTextView", nil, []objc.MethodDef{
		Method("hitTest:", func(self ID, _ SEL, p Point) ID {
			hit := SendSuperPointID(self, "NuiTextView", "hitTest:", p)
			n := theHost.byView[self]
			if n == nil || n.rich == nil || n.rich.selectOK || hit != self {
				return hit
			}
			// A link takes the click; the rest is the view's under it.
			local := MsgPointFromID(self, Sel("convertPoint:fromView:"), p, Send(self, "superview"))
			if n.rich.linkAt(self, local) {
				return self
			}
			return 0
		}),
		Method("acceptsFirstMouse:", func(self ID, _ SEL, ev ID) bool { return true }),
	})
}

func (r *rich) makeView() ID {
	v := initView("NuiTextView")
	Send(v, "setEditable:", 0)
	Send(v, "setSelectable:", 1)
	Send(v, "setRichText:", 1)
	Send(v, "setDrawsBackground:", 0)
	MsgSetSize(v, Sel("setTextContainerInset:"), Size{})
	tc := Send(v, "textContainer")
	MsgSetFloat(tc, Sel("setLineFragmentPadding:"), 0)
	Send(tc, "setWidthTracksTextView:", 0)
	MsgSetSize(tc, Sel("setContainerSize:"), Size{Width: big, Height: big})
	Send(v, "setVerticallyResizable:", 0)
	Send(v, "setHorizontallyResizable:", 0)
	Send(v, "setDelegate:", uintptr(theHost.target))
	Send(v, "setAutomaticLinkDetectionEnabled:", 0)
	r.view = v
	return v
}

func (r *rich) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropRichTextContent:
		r.content = append(r.content[:0], v.Bytes...)
		r.invalidate(h, n)
	case ops.PropRichTextInlines:
		r.inlines = r.inlines[:0]
		b := v.Bytes
		if len(b) >= 4 {
			count := int(le.Uint32(b))
			for i := range count {
				o := 4 + i*12
				if o+12 > len(b) {
					break
				}
				r.inlines = append(r.inlines, inlineSize{ops.NodeID(le.Uint32(b[o:])), float64(f32(b, o+4)), float64(f32(b, o+8))})
			}
		}
		r.invalidate(h, n)
	}
}

func (r *rich) invalidate(h *Host, n *node) { r.dirty = true }

// build makes the attributed string of the content, styled by the
// view's text props.
func (r *rich) build(h *Host, n *node) {
	if !r.dirty {
		return
	}
	r.dirty = false
	if r.str != 0 {
		Release(r.str)
	}
	s := New("NSMutableAttributedString")
	r.str = s
	b := r.content
	ak := AppKit()
	fontKey, colorKey, bgKey := Const(ak, "NSFontAttributeName"), Const(ak, "NSForegroundColorAttributeName"), Const(ak, "NSBackgroundColorAttributeName")
	underKey, strikeKey, linkKey := Const(ak, "NSUnderlineStyleAttributeName"), Const(ak, "NSStrikethroughStyleAttributeName"), Const(ak, "NSLinkAttributeName")
	attachKey, paraKey := Const(ak, "NSAttachmentAttributeName"), Const(ak, "NSParagraphStyleAttributeName")

	para := New("NSMutableParagraphStyle")
	defer Release(para)
	Send(para, "setAlignment:", uintptr(textAlignment(n.textAlign)))
	baseColor := h.nsColor(n.color)
	if baseColor == 0 {
		baseColor = Send(Class("NSColor"), "labelColor")
	}

	type style struct{ attrs ID }
	var styles []style
	if len(b) >= 4 {
		count := int(le.Uint32(b))
		b = b[4:]
		for range count {
			if len(b) < 23 {
				break
			}
			ts, size, weight, flags := int32(b[0]), f32(b, 1), int32(b[5]), b[6]
			color, bg := ops.Color(le.Uint64(b[7:])), ops.Color(le.Uint64(b[15:]))
			var link string
			link, b = readString(b[23:])
			if ts == 0 {
				ts = n.textStyle
			}
			if size == 0 {
				size = n.fontSize
			}
			if weight == 0 {
				weight = n.fontWeight
			}
			font := h.font(ts, weight, size, n.fontFamily)
			if flags&2 != 0 { // monospaced
				w := 0.0
				if weight > 0 && int(weight) < len(weights) {
					w = weights[weight]
				}
				font = MsgFloat2ID(Class("NSFont"), Sel("monospacedSystemFontOfSize:weight:"), MsgFloat(font, Sel("pointSize")), w)
			}
			if flags&1 != 0 { // italic
				fm := Send(Class("NSFontManager"), "sharedFontManager")
				font = Send(fm, "convertFont:toHaveTrait:", uintptr(font), 1)
			}
			attrs := Send(Class("NSMutableDictionary"), "dictionary")
			Send(attrs, "setObject:forKey:", uintptr(font), uintptr(fontKey))
			Send(attrs, "setObject:forKey:", uintptr(para), uintptr(paraKey))
			c := baseColor
			if color != 0 {
				c = h.nsColor(color)
			} else if link != "" {
				c = h.accentColor()
			}
			Send(attrs, "setObject:forKey:", uintptr(c), uintptr(colorKey))
			if bg != 0 {
				Send(attrs, "setObject:forKey:", uintptr(h.nsColor(bg)), uintptr(bgKey))
			}
			if flags&4 != 0 {
				Send(attrs, "setObject:forKey:", uintptr(NumberInt(1)), uintptr(underKey))
			}
			if flags&8 != 0 {
				Send(attrs, "setObject:forKey:", uintptr(NumberInt(1)), uintptr(strikeKey))
			}
			if link != "" {
				Send(attrs, "setObject:forKey:", uintptr(String(link)), uintptr(linkKey))
			}
			if flags&16 != 0 {
				Send(attrs, "setObject:forKey:", uintptr(NumberInt(1)), uintptr(String("nui.inline")))
			}
			styles = append(styles, style{attrs})
		}
	}
	inline := 0
	if len(b) >= 4 {
		paras := int(le.Uint32(b))
		b = b[4:]
		for p := range paras {
			if len(b) < 4 {
				break
			}
			runs := int(le.Uint32(b))
			b = b[4:]
			var last ID
			for range runs {
				if len(b) < 4 {
					break
				}
				si := int(le.Uint32(b))
				var text string
				text, b = readString(b[4:])
				if si >= len(styles) {
					continue
				}
				attrs := styles[si].attrs
				last = attrs
				if Send(attrs, "objectForKey:", uintptr(String("nui.inline"))) != 0 {
					// An inline component: an attachment of its size, on
					// the baseline.
					a := New("NSTextAttachment")
					if inline < len(r.inlines) {
						in := r.inlines[inline]
						MsgSetRect(a, Sel("setBounds:"), Rect{Size: Size{Width: in.w, Height: in.h}})
					}
					inline++
					as := Send(Class("NSMutableAttributedString"), "attributedStringWithAttachment:", uintptr(a))
					Release(a)
					m := Send(as, "mutableCopy")
					Send(m, "addAttributes:range:", uintptr(attrs), 0, uintptr(SendInt(m, "length")))
					Send(s, "appendAttributedString:", uintptr(m))
					Release(m)
					continue
				}
				as := Send(Send(Class("NSAttributedString"), "alloc"), "initWithString:attributes:", uintptr(String(text)), uintptr(attrs))
				Send(s, "appendAttributedString:", uintptr(as))
				Release(as)
			}
			if p < paras-1 {
				if last == 0 && len(styles) > 0 {
					last = styles[0].attrs
				}
				nl := Send(Send(Class("NSAttributedString"), "alloc"), "initWithString:attributes:", uintptr(String("\n")), uintptr(last))
				Send(s, "appendAttributedString:", uintptr(nl))
				Release(nl)
			}
		}
	}
	_ = attachKey
	ts := Send(r.view, "textStorage")
	Send(ts, "setAttributedString:", uintptr(s))
	tc := Send(r.view, "textContainer")
	Send(tc, "setMaximumNumberOfLines:", uintptr(n.lineLimit))
	if n.lineLimit > 0 {
		Send(tc, "setLineBreakMode:", 4) // truncating tail
	} else {
		Send(tc, "setLineBreakMode:", 0)
	}
	attrs := Dict(colorKey, h.accentColor(), Const(ak, "NSCursorAttributeName"), Send(Class("NSCursor"), "pointingHandCursor"))
	Send(r.view, "setLinkTextAttributes:", uintptr(attrs))
	r.selectOK = r.inSelectable(n)
}

func (r *rich) inSelectable(n *node) bool {
	for p := n.parent; p != nil; p = p.parent {
		if p.kind == ops.KindSelectable {
			return true
		}
	}
	return false
}

// layoutAt typesets the text at width and returns its used size.
func (r *rich) layoutAt(width float64) Size {
	tc := Send(r.view, "textContainer")
	lm := Send(r.view, "layoutManager")
	MsgSetSize(tc, Sel("setContainerSize:"), Size{Width: width, Height: big})
	Send(lm, "ensureLayoutForTextContainer:", uintptr(tc))
	return MsgIDToRect(lm, Sel("usedRectForTextContainer:"), tc).Size
}

func (r *rich) measure(h *Host, n *node, c layout.Constraints) Size {
	r.build(h, n)
	if SendInt(Send(r.view, "textStorage"), "length") == 0 {
		return Size{}
	}
	minContent := 0.0
	if c.AvailW <= -layout.Inf && c.Width != c.Width {
		minContent = r.widestWord(h, n)
	}
	w := fit(c.Width, c.AvailW, minContent)
	s := r.layoutAt(w)
	if w < big {
		s.Width = min(math.Ceil(s.Width), w)
	}
	r.layoutAt(max(r.width, 1))
	return Size{Width: math.Ceil(s.Width), Height: math.Ceil(s.Height)}
}

func (r *rich) widestWord(h *Host, n *node) float64 {
	str := GoString(Send(Send(r.view, "textStorage"), "string"))
	attrs := Dict(Const(AppKit(), "NSFontAttributeName"), h.nodeFont(n))
	widest := 0.0
	for _, w := range strings.Fields(str) {
		widest = max(widest, MsgIDToSize(String(w), Sel("sizeWithAttributes:"), attrs).Width)
	}
	return math.Ceil(widest)
}

// placeInlines lays the text out at the view's width and puts each
// inline component's view where its attachment is.
func (r *rich) placeInlines(h *Host, n *node) {
	r.build(h, n)
	r.width = n.frame.Size.Width
	r.layoutAt(max(r.width, 1))
	if len(r.inlines) == 0 {
		return
	}
	ts := Send(r.view, "textStorage")
	lm := Send(r.view, "layoutManager")
	tc := Send(r.view, "textContainer")
	str := GoString(Send(ts, "string"))
	k := 0
	u16 := 0
	for _, ch := range str {
		if ch == 0xFFFC && k < len(r.inlines) {
			in := r.inlines[k]
			k++
			g := SendInt(lm, "glyphIndexForCharacterAtIndex:", uintptr(u16))
			rect := MsgRangeToRect(lm, Sel("boundingRectForGlyphRange:inTextContainer:"), uint(g), 1, tc)
			if c := h.nodes[in.id]; c != nil && c.view != 0 {
				// The attachment's box: the line's, its bottom on the baseline.
				y := rect.Origin.Y + rect.Size.Height - in.h
				if loc := MsgPointFromInt(lm, Sel("locationForGlyphAtIndex:"), g); loc.Y > 0 {
					frag := MsgLineFragment(lm, Sel("lineFragmentRectForGlyphAtIndex:effectiveRange:"), g, 0)
					y = frag.Origin.Y + loc.Y - in.h
				}
				MsgSetRect(c.view, selSetFrame, Rect{Origin: Point{X: rect.Origin.X, Y: y}, Size: Size{Width: in.w, Height: in.h}})
			}
		}
		if ch >= 0x10000 {
			u16 += 2
		} else {
			u16++
		}
	}
}

// linkAt reports whether a link is under p (in the view).
func (r *rich) linkAt(v ID, p Point) bool {
	lm := Send(v, "layoutManager")
	tc := Send(v, "textContainer")
	var frac float64
	g := MsgGlyphAtPoint(lm, Sel("glyphIndexForPoint:inTextContainer:fractionOfDistanceThroughGlyph:"), p, tc, &frac)
	rect := MsgRangeToRect(lm, Sel("boundingRectForGlyphRange:inTextContainer:"), uint(g), 1, tc)
	if p.X < rect.Origin.X || p.X > rect.Origin.X+rect.Size.Width || p.Y < rect.Origin.Y || p.Y > rect.Origin.Y+rect.Size.Height {
		return false
	}
	i := SendInt(lm, "characterIndexForGlyphAtIndex:", uintptr(g))
	ts := Send(v, "textStorage")
	if i >= SendInt(ts, "length") {
		return false
	}
	return Send(ts, "attribute:atIndex:effectiveRange:", uintptr(Const(AppKit(), "NSLinkAttributeName")), uintptr(i), 0) != 0
}

func (h *Host) linkClicked(tv, link ID) bool {
	n := h.byView[tv]
	if n == nil || n.rich == nil {
		return false
	}
	var s string
	if SendBool(link, "isKindOfClass:", uintptr(Class("NSURL"))) {
		s = GoString(Send(link, "absoluteString"))
	} else {
		s = GoString(Send(link, "description"))
	}
	if !n.listening(ops.EventLink) {
		return false
	}
	h.emit(n, ops.Event{Kind: ops.EventLink, Str: s})
	return true
}
