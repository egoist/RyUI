//go:build linux && !android

package linux

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// Text fields: a GtkEntry (GtkPasswordEntry when Secure), or for a
// Multiline field a GtkTextView in a scrolled window, styled as
// libadwaita styles an entry (filled, rounded, the accent's focus ring)
// unless Plain, with a dim label for its placeholder while it's empty.
// The text view's text takes the node's font and color from a tag of its
// own (GTK styles a text view's text only through its style sheet or
// tags); a Rich field's styles are tags over it (richfield.go).

type field struct {
	editable    Ptr // the GtkEditable (entry), or 0
	view        Ptr // the GtkTextView (multiline), or 0
	buffer      Ptr // its buffer
	hint        Ptr // a multiline field's placeholder label
	base        Ptr // a multiline field's tag of the node's font and color
	keyboard    int32
	ret         int32 // ReturnKey: its label is a touch keyboard's, none here
	autocap     int32
	autocorrect int32
	contentType string
	pasteTypes  []string
	selStart    uint // as last reported, UTF-16
	selEnd      uint
	pending     []insertion // inserted, not yet tagged
	rich        *richField  // a Rich field's
}

// insertion is text going into a text view's buffer: its offset and
// length in characters, and the style it takes when typed in a rich field.
type insertion struct {
	at, count int32
	style     ops.RichStyle
	typed     bool
}

// The inset of a multiline field's text: an entry's padding.
const fieldPadX, fieldPadY = 9, 8

func (n *node) fieldPad() (x, y float32) {
	if n.plain {
		return 0, 0
	}
	return fieldPadX, fieldPadY
}

func (h *Host) makeField(n *node) {
	if n.field == nil {
		n.field = &field{}
	}
	f := n.field
	var keep []byte // a rich field's styled text, into the new view
	if f.rich != nil && f.buffer != 0 {
		keep = h.encodeRichField(n)
		f.rich.reset()
	}
	text := n.text
	f.editable, f.view, f.buffer, f.hint, f.base, f.pending = 0, 0, 0, 0, 0, nil
	var w Ptr
	switch {
	case n.multiline:
		sw := ScrolledWindowNew()
		ScrolledWindowSetPolicy(sw, PolicyNever, PolicyAutomatic)
		tv := TextViewNew()
		TextViewSetWrapMode(tv, 3) // GTK_WRAP_WORD_CHAR
		TextViewSetAcceptsTab(tv, false)
		WidgetAddCSSClass(tv, "asuka-clear")
		ScrolledWindowSetChild(sw, tv)
		px, py := n.fieldPad()
		TextViewSetTopMargin(tv, int32(py))
		TextViewSetBottomMargin(tv, int32(py))
		TextViewSetLeftMargin(tv, int32(px))
		TextViewSetRightMargin(tv, int32(px))
		if !n.plain {
			WidgetAddCSSClass(sw, "asuka-field")
			WidgetSetOverflow(sw, OverflowHidden)
		}
		// The placeholder: a dim label where the text starts, shown while
		// there's none.
		hint := LabelNew("")
		WidgetAddCSSClass(hint, "dim-label")
		LabelSetWrap(hint, true)
		LabelSetWrapMode(hint, WrapWordChar)
		LabelSetXAlign(hint, 0)
		WidgetSetCanTarget(hint, false)
		WidgetSetVAlign(hint, AlignStart)
		Txt.WidgetSetMarginStart(hint, int32(px))
		Txt.WidgetSetMarginEnd(hint, int32(px))
		Txt.WidgetSetMarginTop(hint, int32(py))
		ov := OverlayNew()
		OverlaySetChild(ov, sw)
		OverlayAddOverlay(ov, hint)
		f.view, f.buffer, f.hint = tv, TextViewGetBuffer(tv), hint
		// The node's font: the lowest tag, on all the text.
		f.base = Txt.TextTagNew(0)
		Txt.TextTagTableAdd(Txt.TextBufferGetTagTable(f.buffer), f.base)
		ObjectUnref(f.base)
		w = ov
	case n.secure:
		w = PasswordEntryNew()
		PasswordEntrySetShowPeekIcon(w, true)
		f.editable = w
	default:
		w = EntryNew()
		f.editable = w
	}
	h.setInner(n, w)
	if n.plain && f.editable != 0 {
		if !n.secure {
			EntrySetHasFrame(w, false)
		}
		WidgetAddCSSClass(w, "asuka-plain")
	}
	h.connectField(n)
	h.styleField(n)
	h.setFieldText(n, text)
	if keep != nil {
		h.setRichField(n, keep)
	}
	h.applyPlaceholder(n)
	h.applyPurpose(n)
	if n.disabled {
		WidgetSetSensitive(n.widget, false)
	}
}

func (h *Host) connectField(n *node) {
	f := n.field
	changed := func() {
		h.fieldInserted(n)
		h.updateHint(n)
		if n.updating {
			return
		}
		n.text = h.fieldText(n)
		h.emit(n, ops.Event{Kind: ops.EventTextChanged, Str: n.text})
		h.richFieldChanged(n)
		h.reportSelection(n) // a multiline one grows: the core measures it again
	}
	focus := EventControllerFocusNew()
	Connect(focus, "enter", func() { h.emit(n, ops.Event{Kind: ops.EventFocus}) })
	Connect(focus, "leave", func() { h.emit(n, ops.Event{Kind: ops.EventBlur}) })
	if e := f.editable; e != 0 {
		// The entry is the node's inner: its handlers go with it.
		n.handlers = append(n.handlers,
			Connect(e, "changed", changed),
			Connect(e, "activate", func() {
				h.emit(n, ops.Event{Kind: ops.EventSubmit})
			}),
			Connect(e, "notify::cursor-position", func(uintptr) { h.reportSelection(n) }),
			Connect(e, "notify::selection-bound", func(uintptr) { h.reportSelection(n) }),
		)
		if d := EditableGetDelegate(e); d != 0 {
			Connect(d, "paste-clipboard", func() { h.pasteContent(n, d) })
		}
		WidgetAddController(e, focus)
		return
	}
	// The text view and its buffer go with the node's inner, and their
	// handlers with them.
	buf, tv := f.buffer, f.view
	Connect(buf, "insert-text", func(it, text, length uintptr) { h.fieldInserting(n, it, text, length) })
	Connect(buf, "changed", changed)
	Connect(buf, "mark-set", func(it, mark uintptr) {
		if mark != Txt.TextBufferGetInsert(buf) && mark != Txt.TextBufferGetSelectionBound(buf) {
			return
		}
		if rf := f.rich; rf != nil && !rf.setting && len(f.pending) == 0 {
			rf.typingSet = false // the cursor moved: it types what's before it
		}
		h.reportSelection(n)
		h.reportFormats(n)
	})
	Connect(tv, "paste-clipboard", func() { h.pasteContent(n, tv) })
	keys := EventControllerKeyNew()
	EventControllerSetPropagationPhase(keys, PhaseCapture)
	Connect(keys, "key-pressed", func(keyval, keycode, state uint32) bool { return h.richKey(n, keyval, state) })
	WidgetAddController(tv, keys)
	WidgetAddController(tv, focus)
}

// fieldInserting notes text going into a text view's buffer (its
// "insert-text", before it's in): where, how long, and in a rich field
// the style it's typed in.
func (h *Host) fieldInserting(n *node, it, text, length uintptr) {
	f := n.field
	at := TextIterGetOffset(At[TextIter](it))
	count := 0
	if int(length) > 0 {
		count = utf8.RuneCount(GoBytes(text, int(length)))
	}
	ins := insertion{at: at, count: int32(count)}
	if rf := f.rich; rf != nil && !rf.setting {
		ins.style, ins.typed = h.typingStyle(n, at), true
	}
	f.pending = append(f.pending, ins)
}

// fieldInserted tags what went in: the node's font, and in a rich field
// the style typed text takes (where it has none, as pasted rich text
// has).
func (h *Host) fieldInserted(n *node) {
	f := n.field
	if f.buffer == 0 || len(f.pending) == 0 {
		return
	}
	pending := f.pending
	f.pending = nil
	buf := f.buffer
	total := Txt.TextBufferGetCharCount(buf)
	var a, b TextIter
	for _, ins := range pending {
		s, e := min(ins.at, total), min(ins.at+ins.count, total)
		if s >= e {
			continue
		}
		TextBufferGetIterAtOffset(buf, &a, s)
		TextBufferGetIterAtOffset(buf, &b, e)
		Txt.TextBufferApplyTag(buf, f.base, &a, &b)
		if !ins.typed {
			continue
		}
		t := h.styleTag(n, ins.style)
		if t == 0 {
			continue
		}
		h.fieldRuns(n, s, e, func(rs, re int32, _ ops.RichStyle, tag Ptr) {
			if tag == 0 {
				TextBufferGetIterAtOffset(buf, &a, rs)
				TextBufferGetIterAtOffset(buf, &b, re)
				Txt.TextBufferApplyTag(buf, t, &a, &b)
			}
		})
	}
}

// updateHint shows a multiline field's placeholder while it's empty.
func (h *Host) updateHint(n *node) {
	f := n.field
	if f.hint == 0 {
		return
	}
	WidgetSetVisible(f.hint, n.placeholder != "" && Txt.TextBufferGetCharCount(f.buffer) == 0)
}

func (h *Host) fieldText(n *node) string {
	f := n.field
	if f.editable != 0 {
		return EditableGetText(f.editable)
	}
	var s, e TextIter
	TextBufferGetStartIter(f.buffer, &s)
	TextBufferGetEndIter(f.buffer, &e)
	return GoStringFree(TextBufferGetText(f.buffer, &s, &e, true))
}

func (h *Host) setFieldText(n *node, s string) {
	f := n.field
	n.text = s
	if h.fieldText(n) == s {
		return
	}
	n.updating = true
	if f.editable != 0 {
		EditableSetText(f.editable, s)
		EditableSetPosition(f.editable, -1)
	} else {
		if f.rich != nil {
			f.rich.setting = true
		}
		TextBufferSetText(f.buffer, s, -1)
		if f.rich != nil {
			f.rich.setting = false
		}
	}
	n.updating = false
	h.updateHint(n)
}

func (h *Host) setFieldProp(n *node, p ops.PropID, v *ops.Value) {
	f := n.field
	switch p {
	case ops.PropTextFieldValue:
		h.setFieldText(n, v.Str)
	case ops.PropTextFieldPlaceholder:
		n.placeholder = v.Str
		h.applyPlaceholder(n)
	case ops.PropTextFieldSecure:
		if n.secure != (v.Int != 0) {
			n.secure = v.Int != 0
			h.makeField(n)
		}
	case ops.PropTextFieldMultiline:
		if n.multiline != (v.Int != 0) {
			n.multiline = v.Int != 0
			h.makeField(n)
		}
	case ops.PropTextFieldPlain:
		if n.plain != (v.Int != 0) {
			n.plain = v.Int != 0
			h.makeField(n)
		}
	case ops.PropTextFieldKeyboard:
		f.keyboard = int32(v.Int)
		h.applyPurpose(n)
	case ops.PropTextFieldReturnKey:
		f.ret = int32(v.Int) // a touch keyboard's label: none on the desktop
	case ops.PropTextFieldAutocapitalize:
		f.autocap = int32(v.Int)
		h.applyPurpose(n)
	case ops.PropTextFieldAutocorrect:
		f.autocorrect = int32(v.Int)
		h.applyPurpose(n)
	case ops.PropTextFieldContentType:
		f.contentType = v.Str
		h.applyPurpose(n)
	case ops.PropTextFieldSelection:
		r := reader{b: v.Bytes}
		start, end := uint(r.u32()), uint(r.u32())
		h.setSelection(n, start, end)
	case ops.PropTextFieldRich:
		h.setRichField(n, v.Bytes)
	case ops.PropTextFieldPasteTypes:
		f.pasteTypes = ops.DecodeStrings(v.Bytes)
	default:
		h.setFieldMore(n, p, v)
	}
}

func (h *Host) applyPlaceholder(n *node) {
	f := n.field
	switch {
	case f.editable != 0 && !n.secure:
		EntrySetPlaceholderText(f.editable, n.placeholder)
	case f.editable != 0:
		SetStringProperty(f.editable, "placeholder-text", n.placeholder)
	case f.hint != 0:
		LabelSetText(f.hint, n.placeholder)
		h.updateHint(n)
	}
}

// GtkInputPurpose and GtkInputHints.
const (
	purposeFree     = 0
	purposeDigits   = 2
	purposeNumber   = 3
	purposePhone    = 4
	purposeURL      = 5
	purposeEmail    = 6
	purposeName     = 7
	purposePassword = 8
	purposePIN      = 9

	hintSpellcheck         = 1 << 0
	hintNoSpellcheck       = 1 << 1
	hintWordCompletion     = 1 << 2
	hintUppercaseChars     = 1 << 4
	hintUppercaseWords     = 1 << 5
	hintUppercaseSentences = 1 << 6
	hintPrivate            = 1 << 11
)

// applyPurpose tells the input method what the field takes (its
// keyboard, its content type) and how to help (capitals, spelling).
func (h *Host) applyPurpose(n *node) {
	f := n.field
	purpose := int32(purposeFree)
	switch ops.Keyboard(f.keyboard) {
	case ops.KeyboardNumber:
		purpose = purposeDigits
	case ops.KeyboardDecimal:
		purpose = purposeNumber
	case ops.KeyboardEmail:
		purpose = purposeEmail
	case ops.KeyboardURL:
		purpose = purposeURL
	case ops.KeyboardPhone:
		purpose = purposePhone
	}
	hints := int32(0)
	switch ops.Autocapitalize(f.autocap) {
	case ops.AutocapitalizeSentences:
		hints |= hintUppercaseSentences
	case ops.AutocapitalizeWords:
		hints |= hintUppercaseWords
	case ops.AutocapitalizeCharacters:
		hints |= hintUppercaseChars
	}
	switch ops.Autocorrect(f.autocorrect) {
	case ops.AutocorrectYes:
		hints |= hintSpellcheck | hintWordCompletion
	case ops.AutocorrectNo:
		hints |= hintNoSpellcheck
	}
	switch f.contentType {
	case "password", "newPassword":
		purpose, hints = purposePassword, hintNoSpellcheck|hintPrivate
	case "oneTimeCode":
		purpose, hints = purposePIN, hintNoSpellcheck
	case "email":
		purpose, hints = purposeEmail, hints&^(hintUppercaseSentences|hintUppercaseWords|hintSpellcheck)|hintNoSpellcheck
	case "phone":
		purpose = purposePhone
	case "name", "givenName", "familyName":
		purpose = purposeName
	case "cardNumber", "postalCode":
		if f.contentType == "cardNumber" {
			purpose = purposeDigits
		}
		hints |= hintNoSpellcheck
	case "username", "url":
		if f.contentType == "url" {
			purpose = purposeURL
		}
		hints = hints&^(hintUppercaseSentences|hintUppercaseWords|hintSpellcheck) | hintNoSpellcheck
	}
	switch {
	case f.view != 0:
		Txt.TextViewSetInputPurpose(f.view, purpose)
		Txt.TextViewSetInputHints(f.view, hints)
	case f.editable != 0 && !n.secure:
		EntrySetInputPurpose(f.editable, purpose)
		EntrySetInputHints(f.editable, hints)
	}
}

// styleField sets a field's font, color and alignment: an entry's as
// attributes, a text view's as its base tag (and a rich field's styles
// again, which resolve colors), and its placeholder's.
func (h *Host) styleField(n *node) {
	f := n.field
	if f == nil {
		return
	}
	attrs := h.attrs(n, true)
	defer AttrListUnref(attrs)
	x := h.xalign(n)
	switch {
	case f.editable != 0 && !n.secure:
		EntrySetAttributes(f.editable, attrs)
		EntrySetAlignment(f.editable, x)
	case f.editable != 0:
		if d := EditableGetDelegate(f.editable); d != 0 {
			Call(Sym("gtk_text_set_attributes"), d, attrs)
		}
	case f.view != 0:
		j := int32(JustifyLeft)
		switch h.physicalAlign(n) {
		case 1:
			j = JustifyCenter
		case 2:
			j = JustifyRight
		}
		TextViewSetJustification(f.view, j)
		h.styleBaseTag(n)
		h.restyleTags(n)
		if f.hint != 0 {
			ha := h.attrs(n, false)
			LabelSetAttributes(f.hint, ha)
			AttrListUnref(ha)
			LabelSetXAlign(f.hint, x)
			LabelSetJustify(f.hint, j)
		}
	}
}

// styleBaseTag sets the base tag's look: the node's font and color.
func (h *Host) styleBaseTag(n *node) {
	t := n.field.base
	if t == 0 {
		return
	}
	size, weight := h.font(n)
	desc := Txt.FontDescriptionNew()
	if fam := h.family(n); fam != "" {
		Txt.FontDescriptionSetFamily(desc, fam)
	}
	Txt.FontDescriptionSetAbsoluteSize(desc, float64(size)*1024)
	Txt.FontDescriptionSetWeight(desc, weight)
	setBoxed(t, "font-desc", Txt.FontDescriptionGetType(), desc)
	FontDescriptionFree(desc)
	if c, ok := h.resolve(n.color); ok {
		setBoxed(t, "foreground-rgba", Txt.RGBAGetType(), Addr(&c))
	} else {
		SetBoolProperty(t, "foreground-set", false)
	}
}

// measureMultiline is a multiline field's height at width w: its text
// laid out as the text view lays it out (its font, a rich field's
// styles) inside its padding, at least a line, at most its line limit.
func (h *Host) measureMultiline(n *node, w float32) float32 {
	f := n.field
	px, py := n.fieldPad()
	var text string
	var attrs Ptr
	if f.rich != nil {
		text, attrs = h.richLayoutText(n)
	} else {
		text, attrs = h.fieldText(n), h.attrs(n, false)
	}
	pl := WidgetCreatePangoLayout(f.view, text)
	LayoutSetAttributes(pl, attrs)
	AttrListUnref(attrs)
	LayoutSetWrap(pl, WrapWordChar)
	LayoutSetWidth(pl, int32(max(w-2*px, 1)*1024))
	var pw, ph int32
	LayoutGetPixelSize(pl, &pw, &ph)
	ObjectUnref(pl)
	ht := float32(ph)
	line := h.lineHeight(n)
	if n.lineLimit > 0 {
		ht = min(ht, line*float32(n.lineLimit))
	}
	ht = max(ht, line) + 2*py
	if !n.plain {
		ht = max(ht, entryHeight) // a line: as tall as an entry
	}
	return ht
}

// entryHeight is libadwaita's entry's least height.
const entryHeight = 34

// Selection, in UTF-16 code units of the text (as the core counts);
// GTK counts characters.

func utf16Len(s string) uint {
	n := uint(0)
	for _, r := range s {
		n += uint(utf16.RuneLen(r))
	}
	return n
}

// charsToUTF16 converts a character offset into s to UTF-16 units.
func charsToUTF16(s string, chars int) uint {
	u := uint(0)
	i := 0
	for _, r := range s {
		if i >= chars {
			break
		}
		u += uint(utf16.RuneLen(r))
		i++
	}
	return u
}

// utf16ToChars converts a UTF-16 offset into s to characters (one inside
// a surrogate pair goes to the character's start).
func utf16ToChars(s string, u uint) int {
	c := 0
	for _, r := range s {
		if u == 0 {
			break
		}
		l := uint(utf16.RuneLen(r))
		if l > u {
			break
		}
		u -= l
		c++
	}
	return c
}

// selection is the selection's bounds in characters, start first.
func (h *Host) selection(n *node) (start, end int) {
	f := n.field
	if f.editable != 0 {
		var s, e int32
		if EditableGetSelectionBounds(f.editable, &s, &e) {
			return int(min(s, e)), int(max(s, e))
		}
		p := int(EditableGetPosition(f.editable))
		return p, p
	}
	var s, e TextIter
	TextBufferGetSelectionBounds(f.buffer, &s, &e)
	return int(TextIterGetOffset(&s)), int(TextIterGetOffset(&e))
}

func (h *Host) reportSelection(n *node) {
	if n.updating || !n.listening(ops.EventSelectionChanged) {
		return
	}
	s, e := h.selection(n)
	text := h.fieldText(n) // now: the selection may move before "changed"
	us, ue := charsToUTF16(text, s), charsToUTF16(text, e)
	if us == n.field.selStart && ue == n.field.selEnd {
		return
	}
	n.field.selStart, n.field.selEnd = us, ue
	var b [8]byte
	le.PutUint32(b[0:], uint32(us))
	le.PutUint32(b[4:], uint32(ue))
	h.emit(n, ops.Event{Kind: ops.EventSelectionChanged, Bytes: b[:]})
}

func (h *Host) setSelection(n *node, start, end uint) {
	f := n.field
	text := h.fieldText(n)
	s, e := utf16ToChars(text, start), utf16ToChars(text, end)
	f.selStart, f.selEnd = charsToUTF16(text, s), charsToUTF16(text, e)
	n.updating = true
	if f.editable != 0 {
		EditableSelectRegion(f.editable, int32(s), int32(e))
	} else {
		var a, b TextIter
		TextBufferGetIterAtOffset(f.buffer, &a, int32(e))
		TextBufferGetIterAtOffset(f.buffer, &b, int32(s))
		TextBufferSelectRange(f.buffer, &a, &b) // the cursor at the end
	}
	n.updating = false
}

// insertText replaces the selection with s, as if typed: one undoable
// change, reported as typing is.
func (h *Host) insertText(n *node, s string) {
	f := n.field
	if f == nil {
		return
	}
	if f.editable != 0 {
		EditableDeleteSelection(f.editable)
		pos := EditableGetPosition(f.editable)
		EditableInsertText(f.editable, s, -1, &pos)
		EditableSetPosition(f.editable, pos)
		return
	}
	Txt.TextBufferBeginUserAction(f.buffer)
	TextBufferDeleteSelection(f.buffer, true, true)
	Txt.TextBufferInsertInteractiveAtCursor(f.buffer, s, -1, true)
	Txt.TextBufferEndUserAction(f.buffer)
}

// focusField gives a field the keyboard focus.
func (h *Host) focusField(n *node) {
	if f := n.field; f != nil {
		if f.editable != 0 {
			WidgetGrabFocus(f.editable)
		} else if f.view != 0 {
			WidgetGrabFocus(f.view)
		}
	}
}

// Pastes of content the field takes (PasteTypes): files, images and
// other data on the clipboard of the types it takes are copied into the
// app's caches and reported with Pasted instead of going into the text.
// Whether the clipboard has such content is known as the paste starts
// (its formats); the content comes asynchronously, as GDK reads it.

// pasteContent is a field's paste-clipboard: it takes the clipboard's
// content of the field's types, if it has any, and stops the paste of
// text.
func (h *Host) pasteContent(n *node, w Ptr) {
	f := n.field
	if f == nil || len(f.pasteTypes) == 0 || !n.listening(ops.EventPasted) {
		return
	}
	cb := DisplayGetClipboard(DisplayGetDefault())
	formats := Txt.ClipboardGetFormats(cb)
	if formats == 0 {
		return
	}
	types := f.pasteTypes
	var mimes []string
	var count uintptr
	if p := Txt.ContentFormatsGetMimeTypes(formats, &count); p != 0 {
		for i := uintptr(0); i < count; i++ {
			mimes = append(mimes, GoString(*At[Ptr](p + i*8)))
		}
	}
	hasText := false
	for _, m := range mimes {
		if strings.HasPrefix(m, "text/plain") || m == "UTF8_STRING" || m == "STRING" {
			hasText = true
		}
	}
	stop := func() { SignalStopEmissionByName(w, "paste-clipboard") }
	switch {
	case Txt.ContentFormatsContainGType(formats, Txt.FileListGetType()):
		// Files (a file manager's copy): those of the field's types.
		stop()
		h.pasteFiles(n, cb, hasText)
		return
	}
	// Data of a type the field takes, as it is (a GIF stays a GIF).
	for _, m := range mimes {
		if strings.HasPrefix(m, "text/") || !strings.Contains(m, "/") || strings.HasPrefix(m, "application/x-") || !ops.MatchMIME(types, m) {
			continue
		}
		stop()
		h.pasteData(n, cb, m)
		return
	}
	switch {
	case Txt.ContentFormatsContainGType(formats, Txt.TextureGetType()) && ops.MatchMIME(types, "image/png"):
		// An image GDK can read (as another app's copy offers it): a PNG.
		stop()
		h.pasteImage(n, cb)
	case hasText && ops.MatchMIME(types, "text/plain"):
		stop()
		h.readClipboardText(func(s string, ok bool) {
			if ok && h.nodes[n.id] == n {
				h.emit(n, ops.Event{Kind: ops.EventPasted, Bytes: ops.AppendItems(nil, []ops.Item{{Kind: ops.ShareKindText, Text: s, MIME: "text/plain", Size: -1}})})
			}
		})
	}
}

// pasted reports the items pasted into a field still there; with none,
// its text paste goes ahead after all (a file of a type it doesn't take).
func (h *Host) pasted(n *node, items []ops.Item, text bool) {
	if h.nodes[n.id] != n {
		return
	}
	if len(items) == 0 {
		if text {
			h.readClipboardText(func(s string, ok bool) {
				if ok && h.nodes[n.id] == n && n.field != nil {
					h.insertText(n, s)
				}
			})
		}
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventPasted, Bytes: ops.AppendItems(nil, items)})
}

func (h *Host) pasteFiles(n *node, cb Ptr, text bool) {
	types := n.field.pasteTypes
	Async(func(c AsyncCall) {
		Txt.ClipboardReadValueAsync(cb, Txt.FileListGetType(), 0, 0, c.Callback, c.Data)
	}, func(_, res Ptr) {
		var gerr Ptr
		v := Txt.ClipboardReadValueFinish(cb, res, &gerr)
		if gerr != 0 {
			ErrorFree(gerr)
		}
		var items []ops.Item
		if v != 0 {
			if list := ValueGetBoxed(At[Value](v)); list != 0 {
				files := Txt.FileListGetFiles(list)
				for _, file := range SList(files) {
					path := GoStringFree(Txt.FileGetPath(file))
					if path == "" {
						continue
					}
					m := mime.TypeByExtension(filepath.Ext(path))
					if m == "" {
						m = "application/octet-stream"
					}
					if !ops.MatchMIME(types, m) {
						continue
					}
					dst, size, err := copyToPastes(path)
					if err != nil {
						continue
					}
					items = append(items, ops.Item{Kind: shareKind(m), Path: dst, Name: filepath.Base(path), MIME: m, Size: size})
				}
				Txt.SListFree(files)
			}
		}
		h.pasted(n, items, text)
	})
}

func (h *Host) pasteImage(n *node, cb Ptr) {
	Async(func(c AsyncCall) {
		Txt.ClipboardReadTextureAsync(cb, 0, c.Callback, c.Data)
	}, func(_, res Ptr) {
		var gerr Ptr
		t := Txt.ClipboardReadTextureFinish(cb, res, &gerr)
		if gerr != 0 {
			ErrorFree(gerr)
		}
		var items []ops.Item
		if t != 0 {
			b := Txt.TextureSaveToPNGBytes(t)
			ObjectUnref(t)
			if b != 0 {
				var size uintptr
				data := GoBytes(BytesGetData(b, &size), int(size))
				BytesUnref(b)
				if p, err := writePaste("Image.png", data); err == nil {
					items = append(items, ops.Item{Kind: ops.ShareKindImage, Path: p, Name: "Image.png", MIME: "image/png", Size: int64(len(data))})
				}
			}
		}
		h.pasted(n, items, false)
	})
}

// pasteData reads the clipboard's data of one MIME type into a file.
func (h *Host) pasteData(n *node, cb Ptr, m string) {
	cm := CString(m)
	list := [2]Ptr{Addr(cm), 0}
	Async(func(c AsyncCall) {
		Txt.ClipboardReadAsync(cb, Addr(&list), 0, 0, c.Callback, c.Data)
	}, func(_, res Ptr) {
		_ = list
		var gerr, outMime Ptr
		in := Txt.ClipboardReadFinish(cb, res, &outMime, &gerr)
		if gerr != 0 {
			ErrorFree(gerr)
		}
		if in == 0 {
			h.pasted(n, nil, false)
			return
		}
		out := Txt.MemoryOutputStreamNewResizable()
		Async(func(c AsyncCall) {
			Txt.OutputStreamSpliceAsync(out, in, 1|2, 0, 0, c.Callback, c.Data) // close both
		}, func(_, res Ptr) {
			var gerr Ptr
			Txt.OutputStreamSpliceFinish(out, res, &gerr)
			if gerr != 0 {
				ErrorFree(gerr)
			}
			b := Txt.MemoryOutputStreamStealAsBytes(out)
			ObjectUnref(out)
			ObjectUnref(in)
			var items []ops.Item
			if b != 0 {
				var size uintptr
				data := GoBytes(BytesGetData(b, &size), int(size))
				BytesUnref(b)
				name := "Pasted"
				if exts, _ := mime.ExtensionsByType(m); len(exts) > 0 {
					name += exts[0]
				}
				if p, err := writePaste(name, data); err == nil && len(data) > 0 {
					items = append(items, ops.Item{Kind: shareKind(m), Path: p, Name: name, MIME: m, Size: int64(len(data))})
				}
			}
			h.pasted(n, items, false)
		})
	})
}

func shareKind(m string) ops.ShareKind {
	switch {
	case strings.HasPrefix(m, "image/"):
		return ops.ShareKindImage
	case strings.HasPrefix(m, "video/"):
		return ops.ShareKindVideo
	case strings.HasPrefix(m, "audio/"):
		return ops.ShareKindAudio
	}
	return ops.ShareKindFile
}

// pasteDir is a new folder for a paste's files in the app's caches.
func pasteDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	app := "asuka"
	if theHost != nil && theHost.appID != "" {
		app = theHost.appID
	}
	pasteSeq++
	dir := filepath.Join(base, app, "Pastes", fmt.Sprintf("%d-%d", time.Now().UnixNano(), pasteSeq))
	return dir, os.MkdirAll(dir, 0o755)
}

var pasteSeq int

func writePaste(name string, data []byte) (string, error) {
	dir, err := pasteDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, name)
	return p, os.WriteFile(p, data, 0o644)
}

// copyToPastes copies another app's file into the app's caches.
func copyToPastes(src string) (string, int64, error) {
	dir, err := pasteDir()
	if err != nil {
		return "", 0, err
	}
	dst := filepath.Join(dir, filepath.Base(src))
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return "", 0, err
	}
	size, err := io.Copy(out, in)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	return dst, size, err
}
