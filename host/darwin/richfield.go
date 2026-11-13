//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"math"
	"sync"
	"unicode/utf16"
	"unsafe"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// A Rich TextField: its text view, made rich, from and to the core's
// styled text (RichText's Content encoding). Bold, italic, underline,
// strikethrough, monospaced and links are what the text has (the Format
// menu's ⌘B, ⌘I, ⌘U, and the Format and SetLink commands change them);
// each run's style as the core sent it rides along (an index into the
// field's styles, in an attribute of its own), so what the user didn't
// change comes back as it was sent.

type richField struct {
	styles  []fieldStyle // the styles runs have had: "asuka.style" indexes them
	setting bool
	formats byte
	link    string
	known   bool // formats and link have been reported
}

// fieldStyle is a run's style (ops RichText.Content).
type fieldStyle struct {
	textStyle int32
	size      float32
	weight    int32
	flags     byte
	color     uint64
	bg        uint64
	link      string
}

const (
	nsBoldFontMask   = 2
	nsItalicFontMask = 1
	nsFixedPitchMask = 0x400
	fieldSemibold    = int32(ops.FontWeightSemibold)
	fieldBold        = int32(ops.FontWeightBold)
)

// fieldStyleKey is the attribute a run's style index is kept in.
var fieldStyleKey = sync.OnceValue(func() ID { return Retain(String("asuka.fieldStyle")) })

func (s fieldStyle) format() byte {
	var f byte
	if s.weight >= fieldSemibold {
		f |= 1
	}
	if s.flags&1 != 0 {
		f |= 2
	}
	if s.flags&4 != 0 {
		f |= 4
	}
	if s.flags&8 != 0 {
		f |= 8
	}
	if s.flags&2 != 0 {
		f |= 16
	}
	return f
}

// formatted is s with the TextFormat bits on or off.
func (s fieldStyle) formatted(bits byte, on bool) fieldStyle {
	if bits&1 != 0 {
		switch {
		case on:
			s.weight = max(s.weight, fieldBold)
		case s.weight >= fieldSemibold:
			s.weight = 0
		}
	}
	for _, m := range [][2]byte{{2, 1}, {16, 2}, {4, 4}, {8, 8}} {
		if bits&m[0] != 0 {
			if on {
				s.flags |= m[1]
			} else {
				s.flags &^= m[1]
			}
		}
	}
	return s
}

// index is s's place in the field's styles, added if it's new.
func (f *richField) index(s fieldStyle) int {
	for i, x := range f.styles {
		if x == s {
			return i
		}
	}
	f.styles = append(f.styles, s)
	return len(f.styles) - 1
}

// attrs are the text attributes of a style in n.
func (h *Host) fieldAttrs(n *node, s fieldStyle) ID {
	ak := AppKit()
	ts, size, weight := s.textStyle, s.size, s.weight
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
	fm := Send(Class("NSFontManager"), "sharedFontManager")
	if s.flags&2 != 0 {
		w := 0.0
		if weight > 0 && int(weight) < len(weights) {
			w = weights[weight]
		}
		font = MsgFloat2ID(Class("NSFont"), Sel("monospacedSystemFontOfSize:weight:"), MsgFloat(font, Sel("pointSize")), w)
	}
	if s.weight >= fieldSemibold {
		font = Send(fm, "convertFont:toHaveTrait:", uintptr(font), nsBoldFontMask)
	}
	if s.flags&1 != 0 {
		font = Send(fm, "convertFont:toHaveTrait:", uintptr(font), nsItalicFontMask)
	}
	attrs := Send(Class("NSMutableDictionary"), "dictionary")
	Send(attrs, "setObject:forKey:", uintptr(font), uintptr(Const(ak, "NSFontAttributeName")))
	color := h.nsColor(ops.Color(s.color))
	if color == 0 {
		if color = h.nsColor(n.color); color == 0 {
			color = Send(Class("NSColor"), "textColor")
		}
	}
	Send(attrs, "setObject:forKey:", uintptr(color), uintptr(Const(ak, "NSForegroundColorAttributeName")))
	if s.bg != 0 {
		Send(attrs, "setObject:forKey:", uintptr(h.nsColor(ops.Color(s.bg))), uintptr(Const(ak, "NSBackgroundColorAttributeName")))
	}
	if s.flags&4 != 0 {
		Send(attrs, "setObject:forKey:", uintptr(NumberInt(1)), uintptr(Const(ak, "NSUnderlineStyleAttributeName")))
	}
	if s.flags&8 != 0 {
		Send(attrs, "setObject:forKey:", uintptr(NumberInt(1)), uintptr(Const(ak, "NSStrikethroughStyleAttributeName")))
	}
	if s.link != "" {
		Send(attrs, "setObject:forKey:", uintptr(String(s.link)), uintptr(Const(ak, "NSLinkAttributeName")))
	}
	Send(attrs, "setObject:forKey:", uintptr(NumberInt(n.field.index(s))), uintptr(fieldStyleKey()))
	return attrs
}

// styleOf is the style of text with these attributes: the core's as it
// was sent, with what the user changed.
func (h *Host) styleOf(n *node, attrs ID) fieldStyle {
	ak := AppKit()
	var s fieldStyle
	if i := Send(attrs, "objectForKey:", uintptr(fieldStyleKey())); i != 0 {
		if k := int(SendInt(i, "integerValue")); k >= 0 && k < len(n.field.styles) {
			s = n.field.styles[k]
		}
	}
	traitsOf := func(font ID) uint {
		if font == 0 {
			return 0
		}
		t := uint(Send(Send(Class("NSFontManager"), "sharedFontManager"), "traitsOfFont:", uintptr(font)))
		if SendBool(font, "isFixedPitch") {
			t |= nsFixedPitchMask
		}
		return t
	}
	traits := traitsOf(Send(attrs, "objectForKey:", uintptr(Const(ak, "NSFontAttributeName"))))
	// What the field's own font has (a headline's bold) isn't the run's:
	// the run keeps what it was sent with for it.
	base := traitsOf(h.nodeFont(n))
	if base&nsBoldFontMask == 0 {
		switch {
		case traits&nsBoldFontMask != 0 && s.weight < fieldSemibold:
			s.weight = fieldBold
		case traits&nsBoldFontMask == 0 && s.weight >= fieldSemibold:
			s.weight = 0
		}
	}
	flags := s.flags & 3 // kept where the field's font decides
	if base&nsItalicFontMask == 0 {
		flags &^= 1
		if traits&nsItalicFontMask != 0 {
			flags |= 1
		}
	}
	if base&nsFixedPitchMask == 0 {
		flags &^= 2
		if traits&nsFixedPitchMask != 0 {
			flags |= 2
		}
	}
	if u := Send(attrs, "objectForKey:", uintptr(Const(ak, "NSUnderlineStyleAttributeName"))); u != 0 && SendInt(u, "integerValue") != 0 {
		flags |= 4
	}
	if u := Send(attrs, "objectForKey:", uintptr(Const(ak, "NSStrikethroughStyleAttributeName"))); u != 0 && SendInt(u, "integerValue") != 0 {
		flags |= 8
	}
	s.flags = flags
	s.link = ""
	if l := Send(attrs, "objectForKey:", uintptr(Const(ak, "NSLinkAttributeName"))); l != 0 {
		if SendBool(l, "isKindOfClass:", uintptr(Class("NSURL"))) {
			s.link = GoString(Send(l, "absoluteString"))
		} else {
			s.link = GoString(l)
		}
	}
	return s
}

// setRichField is the Rich prop: the field's text, styled.
func (h *Host) setRichField(n *node, b []byte) {
	if n.tracking == 0 {
		h.makeMultiline(n)
	}
	if n.field == nil {
		n.field = &richField{}
	}
	tv := n.tracking
	Send(tv, "setRichText:", 1)
	Send(tv, "setImportsGraphics:", 0)
	var styles []fieldStyle
	if len(b) >= 4 {
		count := int(le.Uint32(b))
		b = b[4:]
		for range count {
			if len(b) < 23 {
				break
			}
			s := fieldStyle{textStyle: int32(b[0]), size: f32(b, 1), weight: int32(b[5]), flags: b[6],
				color: le.Uint64(b[7:]), bg: le.Uint64(b[15:])}
			s.link, b = readString(b[23:])
			styles = append(styles, s)
		}
	}
	str := New("NSMutableAttributedString")
	defer Release(str)
	appendRun := func(text string, s fieldStyle) {
		a := Send(Send(Class("NSAttributedString"), "alloc"), "initWithString:attributes:", uintptr(String(text)), uintptr(h.fieldAttrs(n, s)))
		Send(str, "appendAttributedString:", uintptr(a))
		Release(a)
	}
	if len(b) >= 4 {
		paras := int(le.Uint32(b))
		b = b[4:]
		var last fieldStyle
		for p := range paras {
			if p > 0 {
				appendRun("\n", last)
			}
			if len(b) < 4 {
				break
			}
			runs := int(le.Uint32(b))
			b = b[4:]
			for range runs {
				if len(b) < 4 {
					break
				}
				si := int(le.Uint32(b))
				var text string
				text, b = readString(b[4:])
				if si >= len(styles) || styles[si].flags&16 != 0 {
					continue
				}
				last = styles[si]
				appendRun(text, last)
			}
		}
	}
	sel := MsgRange(tv, Sel("selectedRange"))
	n.field.setting = true
	Send(Send(tv, "textStorage"), "setAttributedString:", uintptr(str))
	length := uint(SendInt(str, "length"))
	loc := min(sel.Location, length)
	Send(tv, "setSelectedRange:", uintptr(loc), uintptr(min(sel.Length, length-loc)))
	if length == 0 {
		Send(tv, "setTypingAttributes:", uintptr(h.fieldAttrs(n, fieldStyle{})))
	}
	n.field.setting = false
	n.text = GoString(Send(tv, "string"))
	h.reportFormats(n)
}

// encodeRichField is the field's styled text as the core encodes it:
// styles in the order runs first use them, a paragraph per line, runs of
// one style merged.
func (h *Host) encodeRichField(n *node) []byte {
	ts := Send(n.tracking, "textStorage")
	text := utf16.Encode([]rune(GoString(Send(ts, "string"))))
	type run struct {
		style int
		text  []uint16
	}
	var styles []fieldStyle
	paras := [][]run{nil}
	add := func(s fieldStyle, t []uint16) {
		for len(t) > 0 {
			nl := -1
			for i, c := range t {
				if c == '\n' {
					nl = i
					break
				}
			}
			part := t
			if nl >= 0 {
				part = t[:nl]
			}
			if len(part) > 0 {
				k := -1
				for i, x := range styles {
					if x == s {
						k = i
						break
					}
				}
				if k < 0 {
					styles = append(styles, s)
					k = len(styles) - 1
				}
				p := &paras[len(paras)-1]
				if l := len(*p); l > 0 && (*p)[l-1].style == k {
					(*p)[l-1].text = append((*p)[l-1].text, part...)
				} else {
					*p = append(*p, run{k, append([]uint16(nil), part...)})
				}
			}
			if nl < 0 {
				break
			}
			paras = append(paras, nil)
			t = t[nl+1:]
		}
	}
	for i := 0; i < len(text); {
		var r Range
		attrs := Send(ts, "attributesAtIndex:effectiveRange:", uintptr(i), uintptr(unsafe.Pointer(&r)))
		end := min(int(r.Location+r.Length), len(text))
		if end <= i {
			end = i + 1
		}
		add(h.styleOf(n, attrs), text[i:end])
		i = end
	}
	var b []byte
	b = binary.LittleEndian.AppendUint32(b, uint32(len(styles)))
	for _, s := range styles {
		b = append(b, byte(s.textStyle))
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(s.size))
		b = append(b, byte(s.weight), s.flags)
		b = binary.LittleEndian.AppendUint64(b, s.color)
		b = binary.LittleEndian.AppendUint64(b, s.bg)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s.link)))
		b = append(b, s.link...)
	}
	b = binary.LittleEndian.AppendUint32(b, uint32(len(paras)))
	for _, p := range paras {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(p)))
		for _, r := range p {
			t := string(utf16.Decode(r.text))
			b = binary.LittleEndian.AppendUint32(b, uint32(r.style))
			b = binary.LittleEndian.AppendUint32(b, uint32(len(t)))
			b = append(b, t...)
		}
	}
	return b
}

// richFieldChanged reports the user's change of the text or its styles.
func (h *Host) richFieldChanged(n *node) {
	if n.field == nil || n.field.setting {
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventRichChanged, Bytes: h.encodeRichField(n)})
	h.reportFormats(n)
}

// formatsOf is the styles at the cursor (what's typed next: the typing
// attributes) or that all the selection has, and its link.
func (h *Host) formatsOf(n *node) (byte, string) {
	tv := n.tracking
	sel := MsgRange(tv, Sel("selectedRange"))
	if sel.Length == 0 {
		s := h.styleOf(n, Send(tv, "typingAttributes"))
		return s.format(), s.link
	}
	ts := Send(tv, "textStorage")
	all := byte(31)
	link, first := "", true
	for i := sel.Location; i < sel.Location+sel.Length; {
		var r Range
		attrs := Send(ts, "attributesAtIndex:effectiveRange:", uintptr(i), uintptr(unsafe.Pointer(&r)))
		s := h.styleOf(n, attrs)
		all &= s.format()
		if first {
			link, first = s.link, false
		} else if link != s.link {
			link = ""
		}
		i = max(r.Location+r.Length, i+1)
	}
	return all, link
}

// reportFormats sends Formats if they changed.
func (h *Host) reportFormats(n *node) {
	if n.field == nil || n.tracking == 0 || !n.listening(ops.EventFormats) {
		return
	}
	f, link := h.formatsOf(n)
	if n.field.known && f == n.field.formats && link == n.field.link {
		return
	}
	n.field.known, n.field.formats, n.field.link = true, f, link
	b := append([]byte{f}, binary.LittleEndian.AppendUint32(nil, uint32(len(link)))...)
	h.emit(n, ops.Event{Kind: ops.EventFormats, Bytes: append(b, link...)})
}

// restyleField changes each run of the selection through f, undoably
// (the text view's own undo, as its Format menu's changes are); at the
// cursor, the typing attributes.
func (h *Host) restyleField(n *node, f func(fieldStyle) fieldStyle, wholeLink bool) {
	tv := n.tracking
	sel := MsgRange(tv, Sel("selectedRange"))
	ts := Send(tv, "textStorage")
	if sel.Length == 0 && wholeLink {
		// The link the cursor is in.
		length := uint(SendInt(ts, "length"))
		if length == 0 {
			return
		}
		at := min(sel.Location, length-1)
		if sel.Location > 0 {
			at = sel.Location - 1
		}
		var r Range
		if Send(ts, "attribute:atIndex:effectiveRange:", uintptr(Const(AppKit(), "NSLinkAttributeName")), uintptr(at), uintptr(unsafe.Pointer(&r))) == 0 {
			return
		}
		sel = r
	}
	if sel.Length == 0 {
		s := f(h.styleOf(n, Send(tv, "typingAttributes")))
		Send(tv, "setTypingAttributes:", uintptr(h.fieldAttrs(n, s)))
		h.reportFormats(n)
		return
	}
	if !SendBool(tv, "shouldChangeTextInRange:replacementString:", uintptr(sel.Location), uintptr(sel.Length), 0) {
		return
	}
	Send(ts, "beginEditing")
	for i := sel.Location; i < sel.Location+sel.Length; {
		var r Range
		attrs := Send(ts, "attributesAtIndex:effectiveRange:", uintptr(i), uintptr(unsafe.Pointer(&r)))
		end := min(r.Location+r.Length, sel.Location+sel.Length)
		s := f(h.styleOf(n, attrs))
		Send(ts, "setAttributes:range:", uintptr(h.fieldAttrs(n, s)), uintptr(i), uintptr(end-i))
		i = max(end, i+1)
	}
	Send(ts, "endEditing")
	Send(tv, "didChangeText") // the undo, and textDidChange: reported there
}

// formatField is the Format command.
func (h *Host) formatField(n *node, args []byte) {
	if n.field == nil || n.tracking == 0 || len(args) < 2 {
		return
	}
	bits, how := args[0], args[1]
	has, _ := h.formatsOf(n)
	on := how == 1
	if how == 2 {
		on = has&bits != bits
	}
	h.restyleField(n, func(s fieldStyle) fieldStyle { return s.formatted(bits, on) }, false)
}

// setFieldLink is the SetLink command.
func (h *Host) setFieldLink(n *node, url string) {
	if n.field == nil || n.tracking == 0 {
		return
	}
	h.restyleField(n, func(s fieldStyle) fieldStyle { s.link = url; return s }, true)
}
