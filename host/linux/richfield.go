//go:build linux && !android

package linux

import (
	"bytes"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A Rich TextField: its text view's buffer, styled with a text tag per
// style the core sent or the user made (bold, italic, underline,
// strikethrough, monospaced, links, colors), from and to the core's
// styled text (RichText's Content encoding). Each run's style rides on
// its tag, so what the user didn't change comes back as it was sent.
// Ctrl+B, Ctrl+I and Ctrl+U toggle formats, as the Format command does;
// typed text takes the style before it, or what a Format at the cursor
// set.

type richField struct {
	tags      map[ops.RichStyle]Ptr // the buffer's tag of each style
	styles    map[Ptr]ops.RichStyle
	setting   bool          // the host is setting its text: no events
	typing    ops.RichStyle // what's typed next, when typingSet
	typingSet bool          // a Format at the cursor set typing
	formats   byte          // as last reported
	link      string
	known     bool
}

// reset forgets the tags: the field has a new buffer.
func (rf *richField) reset() {
	rf.tags = map[ops.RichStyle]Ptr{}
	rf.styles = map[Ptr]ops.RichStyle{}
	rf.typingSet = false
	rf.known = false
}

func init() {
	commandHooks = append(commandHooks, func(h *Host, n *node, c ops.CommandID, args []byte) bool {
		if n.kind != ops.KindTextField {
			return false
		}
		switch c {
		case ops.CmdFormat:
			h.formatField(n, args)
		case ops.CmdSetLink:
			h.setFieldLink(n, string(args))
		default:
			return false
		}
		return true
	})
}

// richFormat is a style's TextFormat bits.
func richFormat(s ops.RichStyle) byte {
	var f byte
	if s.Weight >= ops.FontWeightSemibold {
		f |= 1
	}
	if s.Flags&1 != 0 {
		f |= 2
	}
	if s.Flags&4 != 0 {
		f |= 4
	}
	if s.Flags&8 != 0 {
		f |= 8
	}
	if s.Flags&2 != 0 {
		f |= 16
	}
	return f
}

// richFormatted is s with the TextFormat bits on or off.
func richFormatted(s ops.RichStyle, bits byte, on bool) ops.RichStyle {
	if bits&1 != 0 {
		switch {
		case on:
			s.Weight = max(s.Weight, ops.FontWeightBold)
		case s.Weight >= ops.FontWeightSemibold:
			s.Weight = 0
		}
	}
	for _, m := range [][2]byte{{2, 1}, {16, 2}, {4, 4}, {8, 8}} {
		if bits&m[0] != 0 {
			if on {
				s.Flags |= m[1]
			} else {
				s.Flags &^= m[1]
			}
		}
	}
	return s
}

// styleTag is the buffer's tag of a style (0: the field's own style, no
// tag), made the first time a run has it.
func (h *Host) styleTag(n *node, s ops.RichStyle) Ptr {
	rf := n.field.rich
	if s == (ops.RichStyle{}) || rf == nil {
		return 0
	}
	if t, ok := rf.tags[s]; ok {
		return t
	}
	t := Txt.TextTagNew(0)
	Txt.TextTagTableAdd(Txt.TextBufferGetTagTable(n.field.buffer), t)
	ObjectUnref(t) // the table's
	h.styleTagProps(n, t, s)
	rf.tags[s] = t
	rf.styles[t] = s
	return t
}

// styleTagProps sets a tag's look from a style, over the field's own (its
// base tag's): what the style sets, as a RichText draws it.
func (h *Host) styleTagProps(n *node, t Ptr, s ops.RichStyle) {
	desc := Txt.FontDescriptionNew()
	if s.TextStyle != 0 || s.Size != 0 || s.Weight != 0 {
		x := node{textStyle: n.textStyle, fontSize: n.fontSize, fontWeight: n.fontWeight}
		if s.TextStyle != 0 {
			x.textStyle = int32(s.TextStyle)
		}
		if s.Size != 0 {
			x.fontSize = s.Size
		}
		if s.Weight != 0 {
			x.fontWeight = int32(s.Weight)
		}
		size, weight := h.font(&x)
		Txt.FontDescriptionSetAbsoluteSize(desc, float64(size)*1024)
		Txt.FontDescriptionSetWeight(desc, weight)
	}
	if s.Flags&1 != 0 {
		Txt.FontDescriptionSetStyle(desc, 2) // italic
	}
	if s.Flags&2 != 0 {
		Txt.FontDescriptionSetFamily(desc, "monospace")
	}
	setBoxed(t, "font-desc", Txt.FontDescriptionGetType(), desc)
	FontDescriptionFree(desc)
	under := int32(0)
	if s.Flags&4 != 0 || s.Link != "" {
		under = 1
	}
	setEnum(t, "underline", Txt.UnderlineGetType(), under)
	SetBoolProperty(t, "strikethrough", s.Flags&8 != 0)
	c, ok := h.resolve(s.Color)
	if !ok && s.Link != "" {
		c, ok = h.theme.accent, true
	}
	if ok {
		setBoxed(t, "foreground-rgba", Txt.RGBAGetType(), Addr(&c))
	} else {
		SetBoolProperty(t, "foreground-set", false)
	}
	if bg, ok := h.resolve(s.Background); ok && bg.A > 0 {
		setBoxed(t, "background-rgba", Txt.RGBAGetType(), Addr(&bg))
	} else {
		SetBoolProperty(t, "background-set", false)
	}
}

// restyleTags sets every style tag's look again (the theme or the field's
// font changed).
func (h *Host) restyleTags(n *node) {
	if rf := n.field.rich; rf != nil {
		for s, t := range rf.tags {
			h.styleTagProps(n, t, s)
		}
	}
}

// styleAt is the style of the character at it, and its tag.
func (h *Host) styleAt(n *node, it *TextIter) (ops.RichStyle, Ptr) {
	rf := n.field.rich
	if rf == nil {
		return ops.RichStyle{}, 0
	}
	l := Txt.TextIterGetTags(it)
	defer Txt.SListFree(l)
	for _, t := range SList(l) {
		if s, ok := rf.styles[t]; ok {
			return s, t
		}
	}
	return ops.RichStyle{}, 0
}

func (h *Host) styleAtOffset(n *node, off int32) ops.RichStyle {
	var it TextIter
	TextBufferGetIterAtOffset(n.field.buffer, &it, off)
	s, _ := h.styleAt(n, &it)
	return s
}

// fieldRuns calls fn for each run of one style in [from, to) (character
// offsets).
func (h *Host) fieldRuns(n *node, from, to int32, fn func(start, end int32, s ops.RichStyle, tag Ptr)) {
	buf := n.field.buffer
	to = min(to, Txt.TextBufferGetCharCount(buf))
	var it, next TextIter
	for off := from; off < to; {
		TextBufferGetIterAtOffset(buf, &it, off)
		s, tag := h.styleAt(n, &it)
		next = it
		end := to
		if Txt.TextIterForwardToTagToggle(&next, 0) {
			end = min(TextIterGetOffset(&next), to)
		}
		if end <= off {
			end = off + 1
		}
		fn(off, end, s, tag)
		off = end
	}
}

// fieldSlice is the text in [from, to) (character offsets).
func (h *Host) fieldSlice(n *node, from, to int32) string {
	var a, b TextIter
	TextBufferGetIterAtOffset(n.field.buffer, &a, from)
	TextBufferGetIterAtOffset(n.field.buffer, &b, to)
	return GoStringFree(TextBufferGetText(n.field.buffer, &a, &b, true))
}

// setRichField is the Rich prop: the field's text, styled.
func (h *Host) setRichField(n *node, b []byte) {
	f := n.field
	if f.rich == nil {
		f.rich = &richField{}
		f.rich.reset()
	}
	if f.view == 0 {
		n.multiline = true
		h.makeField(n)
		f = n.field
	}
	if bytes.Equal(h.encodeRichField(n), b) {
		return
	}
	rf := f.rich
	buf := f.buffer
	ss, se := h.selection(n)
	rf.setting, n.updating = true, true
	Txt.TextBufferBeginIrreversibleAction(buf)
	TextBufferSetText(buf, "", 0)
	var end TextIter
	for _, run := range ops.DecodeRich(b) {
		if run.Style.Flags&16 != 0 || run.Text == "" {
			continue
		}
		start := Txt.TextBufferGetCharCount(buf)
		TextBufferGetEndIter(buf, &end)
		Txt.TextBufferInsert(buf, &end, run.Text, int32(len(run.Text)))
		if t := h.styleTag(n, run.Style); t != 0 {
			var a TextIter
			TextBufferGetIterAtOffset(buf, &a, start)
			TextBufferGetEndIter(buf, &end)
			Txt.TextBufferApplyTag(buf, t, &a, &end)
		}
	}
	Txt.TextBufferEndIrreversibleAction(buf)
	count := int(Txt.TextBufferGetCharCount(buf))
	var a, c TextIter
	TextBufferGetIterAtOffset(buf, &a, int32(min(ss, count)))
	TextBufferGetIterAtOffset(buf, &c, int32(min(se, count)))
	TextBufferSelectRange(buf, &a, &c)
	rf.typingSet = false
	rf.setting, n.updating = false, false
	n.text = h.fieldText(n)
	h.updateHint(n)
	h.reportFormats(n)
}

// encodeRichField is the field's styled text as the core encodes it
// (ops.AppendRich: canonical).
func (h *Host) encodeRichField(n *node) []byte {
	f := n.field
	if f == nil || f.buffer == 0 {
		return nil
	}
	var runs []ops.RichRun
	h.fieldRuns(n, 0, Txt.TextBufferGetCharCount(f.buffer), func(start, end int32, s ops.RichStyle, _ Ptr) {
		runs = append(runs, ops.RichRun{Style: s, Text: h.fieldSlice(n, start, end)})
	})
	return ops.AppendRich(nil, runs)
}

// richFieldChanged reports the user's change of the text or its styles.
func (h *Host) richFieldChanged(n *node) {
	f := n.field
	if f == nil || f.rich == nil || f.rich.setting {
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventRichChanged, Bytes: h.encodeRichField(n)})
	h.reportFormats(n)
}

// typingStyle is what's typed at offset at: a Format's at the cursor, or
// the style of the character before it (the first's, at the start).
func (h *Host) typingStyle(n *node, at int32) ops.RichStyle {
	rf := n.field.rich
	if rf.typingSet {
		return rf.typing
	}
	if at > 0 {
		return h.styleAtOffset(n, at-1)
	}
	if Txt.TextBufferGetCharCount(n.field.buffer) > 0 {
		return h.styleAtOffset(n, 0)
	}
	return ops.RichStyle{}
}

// formatsOf is the styles at the cursor (what's typed next) or that all
// the selection has, and its link.
func (h *Host) formatsOf(n *node) (byte, string) {
	s, e := h.selection(n)
	if s == e {
		st := h.typingStyle(n, int32(s))
		return richFormat(st), st.Link
	}
	all := byte(31)
	link, first := "", true
	h.fieldRuns(n, int32(min(s, e)), int32(max(s, e)), func(_, _ int32, st ops.RichStyle, _ Ptr) {
		all &= richFormat(st)
		if first {
			link, first = st.Link, false
		} else if link != st.Link {
			link = ""
		}
	})
	return all, link
}

// reportFormats sends Formats if they changed.
func (h *Host) reportFormats(n *node) {
	f := n.field
	if f == nil || f.rich == nil || f.buffer == 0 || !n.listening(ops.EventFormats) {
		return
	}
	bits, link := h.formatsOf(n)
	rf := f.rich
	if rf.known && bits == rf.formats && link == rf.link {
		return
	}
	rf.known, rf.formats, rf.link = true, bits, link
	b := append([]byte{bits}, le.AppendUint32(nil, uint32(len(link)))...)
	h.emit(n, ops.Event{Kind: ops.EventFormats, Bytes: append(b, link...)})
}

// restyleField changes each run of the selection through fn, as one
// undoable action; at the cursor, what's typed next (with wholeLink, the
// link the cursor is in).
func (h *Host) restyleField(n *node, fn func(ops.RichStyle) ops.RichStyle, wholeLink bool) {
	f := n.field
	buf := f.buffer
	ss, se := h.selection(n)
	s, e := int32(min(ss, se)), int32(max(ss, se))
	if s == e && wholeLink {
		total := Txt.TextBufferGetCharCount(buf)
		if total == 0 {
			return
		}
		at := s
		if at > 0 {
			at--
		}
		at = min(at, total-1)
		type run struct {
			start, end int32
			link       string
		}
		var runs []run
		k := -1
		h.fieldRuns(n, 0, total, func(start, end int32, st ops.RichStyle, _ Ptr) {
			if at >= start && at < end {
				k = len(runs)
			}
			runs = append(runs, run{start, end, st.Link})
		})
		if k < 0 || runs[k].link == "" {
			return
		}
		lo, hi := k, k
		for lo > 0 && runs[lo-1].link == runs[k].link {
			lo--
		}
		for hi < len(runs)-1 && runs[hi+1].link == runs[k].link {
			hi++
		}
		s, e = runs[lo].start, runs[hi].end
	}
	if s == e {
		rf := f.rich
		rf.typing = fn(h.typingStyle(n, s))
		rf.typingSet = true
		h.reportFormats(n)
		return
	}
	type change struct {
		start, end int32
		old, new   Ptr
	}
	var changes []change
	h.fieldRuns(n, s, e, func(start, end int32, st ops.RichStyle, tag Ptr) {
		if ns := fn(st); ns != st {
			changes = append(changes, change{start, end, tag, h.styleTag(n, ns)})
		}
	})
	if len(changes) == 0 {
		h.reportFormats(n)
		return
	}
	Txt.TextBufferBeginUserAction(buf)
	var a, b TextIter
	for _, c := range changes {
		TextBufferGetIterAtOffset(buf, &a, c.start)
		TextBufferGetIterAtOffset(buf, &b, c.end)
		if c.old != 0 {
			Txt.TextBufferRemoveTag(buf, c.old, &a, &b)
		}
		if c.new != 0 {
			Txt.TextBufferApplyTag(buf, c.new, &a, &b)
		}
	}
	Txt.TextBufferEndUserAction(buf)
	h.richFieldChanged(n)
}

// formatField is the Format command.
func (h *Host) formatField(n *node, args []byte) {
	f := n.field
	if f == nil || f.rich == nil || f.buffer == 0 || len(args) < 2 {
		return
	}
	bits, how := args[0], args[1]
	has, _ := h.formatsOf(n)
	on := how == 1
	if how == 2 {
		on = has&bits != bits
	}
	h.restyleField(n, func(s ops.RichStyle) ops.RichStyle { return richFormatted(s, bits, on) }, false)
}

// setFieldLink is the SetLink command.
func (h *Host) setFieldLink(n *node, url string) {
	f := n.field
	if f == nil || f.rich == nil || f.buffer == 0 {
		return
	}
	h.restyleField(n, func(s ops.RichStyle) ops.RichStyle { s.Link = url; return s }, true)
}

// richKey is a key in a rich field: Ctrl+B, Ctrl+I and Ctrl+U toggle
// bold, italic and underline.
func (h *Host) richKey(n *node, keyval, state uint32) bool {
	if n.field == nil || n.field.rich == nil || state&(ModControl|ModAlt|ModShift|ModSuper) != ModControl {
		return false
	}
	var bits byte
	switch keyval {
	case 'b', 'B':
		bits = 1
	case 'i', 'I':
		bits = 2
	case 'u', 'U':
		bits = 4
	default:
		return false
	}
	h.formatField(n, []byte{bits, 2})
	return true
}

// richLayoutText is the field's text and its styles as Pango attributes,
// as the text view draws them: for measuring. The caller unrefs the list.
func (h *Host) richLayoutText(n *node) (string, Ptr) {
	list := h.attrs(n, false)
	var b strings.Builder
	h.fieldRuns(n, 0, Txt.TextBufferGetCharCount(n.field.buffer), func(start, end int32, s ops.RichStyle, _ Ptr) {
		from := b.Len()
		b.WriteString(h.fieldSlice(n, start, end))
		to := b.Len()
		h.runAttrs(n, s, func(a Ptr) {
			setAttrRange(a, uint32(from), uint32(to))
			AttrListInsert(list, a)
		})
	})
	return b.String(), list
}

// setBoxed sets a boxed property (a GdkRGBA, a font description: the
// value is copied).
func setBoxed(o Ptr, name string, t GType, p Ptr) {
	var v Value
	ValueInit(&v, t)
	Txt.ValueSetBoxed(&v, p)
	ObjectSetProperty(o, name, &v)
	ValueUnset(&v)
}

// setEnum sets an enum property of an enum type.
func setEnum(o Ptr, name string, t GType, e int32) {
	var v Value
	ValueInit(&v, t)
	ValueSetEnum(&v, e)
	ObjectSetProperty(o, name, &v)
	ValueUnset(&v)
}
