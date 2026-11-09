//go:build windows && amd64

package windows

import (
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/egoist/ryui/internal/rtf"
	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ops"
)

// A Rich TextField is a rich edit control (msftedit's RICHEDIT50W)
// instead of an EDIT. Its styled text goes in and comes out as RTF
// (internal/rtf): links are the control's friendly links (HYPERLINK
// fields), which only RTF carries. Ctrl+B, Ctrl+I and Ctrl+U are the
// control's own; the Format command sets the selection's character
// format, undoably, as they do. A span's colors come back as they were
// sent (a semantic color stays one, drawn again as the theme changes); a
// color the user gave, as drawn. Its text style and size are the field's.

const (
	emStreamOut     = WM_USER + 74
	emSetTextEx     = WM_USER + 97
	emGetCharFormat = WM_USER + 58
	emExGetSel      = WM_USER + 52
	enSelChange     = 0x0702
	enmChange       = 0x00000001
	enmSelChange    = 0x00080000
	sfRTF           = 0x0002
	sffSelection    = 0x8000
	stKeepUndo      = 0x0001
	stSelection     = 0x0002
)

// editStream is EDITSTREAM, as richedit.h packs it (#pragma pack(4)):
// on amd64 the callback is at byte 12, not 16 where Go would put it.
type editStream struct {
	cookie   uintptr
	err      uint32
	callback [2]uint32 // EDITSTREAMCALLBACK, unaligned
}

func (es *editStream) setCallback(cb uintptr) {
	es.callback = [2]uint32{uint32(cb), uint32(uint64(cb) >> 32)}
}

// streamed is what EM_STREAMOUT's callback is writing (main thread).
var (
	streamed     []byte
	streamOutCB  = syscall.NewCallback(streamOutFn)
	monoFace     = "Cascadia Mono"
	richFieldDLL bool
)

func streamOutFn(cookie, buf, cb, pcb uintptr) uintptr {
	if cb > 0 {
		streamed = append(streamed, unsafe.Slice(Native[byte](buf), int(cb))...)
	}
	*Native[int32](pcb) = int32(cb)
	return 0
}

// streamOut is the control's RTF: all of it, or its selection's.
func streamOut(ctl uintptr, selection bool) []byte {
	streamed = streamed[:0]
	var es editStream
	es.setCallback(streamOutCB)
	flags := uintptr(sfRTF)
	if selection {
		flags |= sffSelection
	}
	SendMessage(ctl, emStreamOut, flags, uintptr(unsafe.Pointer(&es)))
	return append([]byte(nil), streamed...)
}

// richDoc is the field's defaults as RTF has them: its font, size and
// color.
func (h *Host) richDoc(n *node) rtf.Doc {
	s := h.nodeSpec(n)
	c, ok := h.resolve(n.color)
	if !ok {
		c = h.theme.label
	}
	// The control's twips, as RichText's: half points at the system's DPI.
	return rtf.Doc{Face: h.face(s), MonoFace: monoFace, Size: float32(h.twips(s.size, s.k)) / 20, Color: rtf.Color{R: c.r, G: c.g, B: c.b}}
}

// richClass is the class a field's control is made of: a Rich field's
// rich edit, else an EDIT.
func (h *Host) richClass(n *node) string {
	if !n.richText {
		return "EDIT"
	}
	if !richFieldDLL {
		richFieldDLL = true
		LoadSystemLibrary("msftedit.dll")
	}
	return "RICHEDIT50W"
}

// setupRichEdit readies a Rich field's new control: its changes and
// selection's are reported, its background is the field's.
func (h *Host) setupRichEdit(n *node) {
	if !n.richText || n.ctl == 0 {
		return
	}
	SendMessage(n.ctl, emSetEventMask, 0, enmChange|enmSelChange)
	bg := h.theme.fieldBg
	if n.plain {
		bg = h.backdrop(n)
	}
	SendMessage(n.ctl, emSetBkgndColor, 0, uintptr(bg.ref()))
	if len(n.richContent) > 0 {
		h.setRichText(n, n.richContent)
	}
}

// setRichText is the Rich prop: the field's styled text, as RTF.
func (h *Host) setRichText(n *node, b []byte) {
	n.richContent = append(n.richContent[:0], b...)
	if !n.richText {
		n.richText = true
		h.makeEdit(n) // a rich edit now
		h.themeControl(n)
		return // setupRichEdit sets the text
	}
	// Each color as drawn now, and the color it was sent as: what the
	// control gives back in it is that color again (a semantic one stays
	// semantic, and follows the theme).
	n.richColors = map[rtf.Color]ops.Color{}
	var runs []rtf.Run
	for _, r := range ops.DecodeRich(b) {
		if r.Style.Flags&16 != 0 {
			continue // inline components aren't editable
		}
		runs = append(runs, h.rtfRun(n, r))
	}
	doc := h.richDoc(n)
	n.richDoc = doc
	data := append(rtf.Write(runs, doc), 0)
	sel := [2]int32{}
	SendMessage(n.ctl, emExGetSel, 0, Ptr(&sel))
	n.settingText = true
	settext := [2]uint32{0, 0} // SETTEXTEX: ST_DEFAULT, CP_ACP
	SendMessage(n.ctl, emSetTextEx, uintptr(unsafe.Pointer(&settext)), uintptr(unsafe.Pointer(&data[0])))
	n.settingText = false
	length := int32(len(utf16.Encode([]rune(WindowText(n.ctl)))))
	sel[0], sel[1] = min(sel[0], length), min(sel[1], length)
	SendMessage(n.ctl, emExSetSel, 0, Ptr(&sel))
	n.text = h.richPlain(n)
	h.richRuns(n)
	h.reportRichFormats(n)
}

// rtfRun is a run of the core's styled text as RTF writes it.
func (h *Host) rtfRun(n *node, r ops.RichRun) rtf.Run {
	s := r.Style
	out := rtf.Run{Text: r.Text, Bold: s.Weight >= ops.FontWeightSemibold, Italic: s.Flags&1 != 0, Mono: s.Flags&2 != 0,
		Underline: s.Flags&4 != 0, Strike: s.Flags&8 != 0, Link: s.Link}
	keep := func(c rgba, sent ops.Color) *rtf.Color {
		rc := rtf.Color{R: c.r, G: c.g, B: c.b}
		if _, ok := n.richColors[rc]; !ok {
			n.richColors[rc] = sent
		}
		return &rc
	}
	if c, ok := h.resolve(s.Color); ok {
		out.Color = keep(c, s.Color)
	}
	if c, ok := h.resolve(s.Background); ok && c.a > 0 {
		out.Background = keep(c, s.Background)
	}
	return out
}

// coreRun is a run the control gave as the core's styled text has it:
// its colors as they were sent, where they're those (the user's own as
// they're drawn).
func coreRun(n *node, r rtf.Run) ops.RichRun {
	color := func(c rtf.Color) ops.Color {
		if sent, ok := n.richColors[c]; ok {
			return sent
		}
		return ops.RGBA(c.R, c.G, c.B, 255)
	}
	var s ops.RichStyle
	if r.Bold {
		s.Weight = ops.FontWeightBold
	}
	if r.Italic {
		s.Flags |= 1
	}
	if r.Mono {
		s.Flags |= 2
	}
	if r.Underline {
		s.Flags |= 4
	}
	if r.Strike {
		s.Flags |= 8
	}
	if c := r.Color; c != nil {
		s.Color = color(*c)
	}
	if c := r.Background; c != nil {
		s.Background = color(*c)
	}
	s.Link = r.Link
	return ops.RichRun{Style: s, Text: r.Text}
}

// richPlain is the field's text, its lines ended by \n (a rich edit's
// end in \r).
func (h *Host) richPlain(n *node) string {
	s := visibleText(n.ctl)
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// visibleText is a rich edit's text without its hidden text: a link's
// field instruction (HYPERLINK "url"), which WM_GETTEXT includes.
func visibleText(ctl uintptr) string {
	n := len([]rune(WindowText(ctl))) + 1 // at least as long, hidden text and all
	buf := make([]uint16, 2*n)
	// GETTEXTEX, as richedit.h packs it: cb, flags, codepage, then two
	// null pointers (Unicode takes no default character).
	var gt struct {
		cb, flags, codepage uint32
		_                   [4]uint32
	}
	const gtNoHiddenText, utf16CodePage = 8, 1200
	gt.cb, gt.flags, gt.codepage = uint32(len(buf)*2), gtNoHiddenText, utf16CodePage
	got := SendMessage(ctl, WM_USER+94, Ptr(&gt), Ptr(&buf[0])) // EM_GETTEXTEX
	return string(utf16.Decode(buf[:min(int(got), len(buf))]))
}

// fieldRun is a run of the field's text and where it is (UTF-16).
type fieldRun struct {
	start, end int32
	run        rtf.Run
}

// richRuns reads the field's runs (its RTF) and keeps them, with their
// places, for its formats.
func (h *Host) richRuns(n *node) []rtf.Run {
	runs := rtf.Parse(streamOut(n.ctl, false), n.richDoc)
	n.fieldRuns = n.fieldRuns[:0]
	var at int32
	for _, r := range runs {
		l := int32(len(utf16.Encode([]rune(r.Text))))
		n.fieldRuns = append(n.fieldRuns, fieldRun{at, at + l, r})
		at += l
	}
	return runs
}

// richEdited reports the user's change of the text or its styles.
func (h *Host) richEdited(n *node) {
	if !n.richText || n.settingText {
		return
	}
	var core []ops.RichRun
	for _, r := range h.richRuns(n) {
		core = append(core, coreRun(n, r))
	}
	h.emit(n, ops.Event{Kind: ops.EventRichChanged, Bytes: ops.AppendRich(nil, core)})
	h.reportRichFormats(n)
}

// rethemeRich draws a Rich field's text again in the theme's colors (a
// semantic color resolves differently in dark mode), as the user left
// it.
func (h *Host) rethemeRich(n *node) {
	if !n.richText || n.ctl == 0 {
		return
	}
	var core []ops.RichRun
	for _, r := range h.richRuns(n) {
		core = append(core, coreRun(n, r))
	}
	h.setRichText(n, ops.AppendRich(nil, core))
}

// richFormats is the styles at the cursor (what's typed next: the
// control's insertion format) or that all the selection has, and its
// link.
func (h *Host) richFormats(n *node) (byte, string) {
	cf := charFormat{}
	cf.Size = uint32(unsafe.Sizeof(cf))
	SendMessage(n.ctl, emGetCharFormat, scfSelection, Ptr(&cf))
	var f byte
	all := func(bit uint32) bool { return cf.Mask&bit != 0 && cf.Effects&bit != 0 }
	if all(cfmBold) {
		f |= 1
	}
	if all(cfmItalic) {
		f |= 2
	}
	if all(cfmUnderline) {
		f |= 4
	}
	if all(cfmStrikeOut) {
		f |= 8
	}
	if cf.Mask&cfmFace != 0 && strings.EqualFold(syscall.UTF16ToString(cf.FaceName[:]), monoFace) {
		f |= 16
	}
	sel := [2]int32{}
	SendMessage(n.ctl, emExGetSel, 0, Ptr(&sel))
	link, first := "", true
	for _, r := range n.fieldRuns {
		in := sel[0] == sel[1] && sel[0] > r.start && sel[0] <= r.end || sel[0] != sel[1] && r.end > sel[0] && r.start < sel[1]
		if !in {
			continue
		}
		if first {
			link, first = r.run.Link, false
		} else if link != r.run.Link {
			link = ""
		}
	}
	return f, link
}

// reportRichFormats sends Formats if they changed.
func (h *Host) reportRichFormats(n *node) {
	if !n.richText || n.ctl == 0 || !n.listening(ops.EventFormats) {
		return
	}
	f, link := h.richFormats(n)
	if n.formatsKnown && f == n.formats && link == n.formatLink {
		return
	}
	n.formatsKnown, n.formats, n.formatLink = true, f, link
	b := append([]byte{f}, le.AppendUint32(nil, uint32(len(link)))...)
	h.emit(n, ops.Event{Kind: ops.EventFormats, Bytes: append(b, link...)})
}

// formatRich is the Format command: the selection's character format
// (at the cursor, the insertion format), undoably.
func (h *Host) formatRich(n *node, args []byte) {
	if !n.richText || n.ctl == 0 || len(args) < 2 {
		return
	}
	bits, how := args[0], args[1]
	has, _ := h.richFormats(n)
	on := how == 1
	if how == 2 {
		on = has&bits != bits
	}
	cf := charFormat{}
	cf.Size = uint32(unsafe.Sizeof(cf))
	for _, m := range [][2]uint32{{1, cfmBold}, {2, cfmItalic}, {4, cfmUnderline}, {8, cfmStrikeOut}} {
		if uint32(bits)&m[0] != 0 {
			cf.Mask |= m[1]
			if on {
				cf.Effects |= m[1]
			}
		}
	}
	if bits&16 != 0 {
		cf.Mask |= cfmFace
		face := monoFace
		if on {
			cf.PitchFamily = 1 | 0x30 // FIXED_PITCH, FF_MODERN: \fmodern\fprq1 in its RTF
		} else {
			face = h.richDoc(n).Face
		}
		copy(cf.FaceName[:31], UTF16Units(face))
	}
	SendMessage(n.ctl, emSetCharFormat, scfSelection, Ptr(&cf))
	h.richEdited(n)
}

// setRichLink is the SetLink command: the selection (at the cursor, the
// link it's in) becomes a friendly link to url, or plain text again.
func (h *Host) setRichLink(n *node, url string) {
	if !n.richText || n.ctl == 0 {
		return
	}
	sel := [2]int32{}
	SendMessage(n.ctl, emExGetSel, 0, Ptr(&sel))
	if sel[0] == sel[1] {
		found := false
		for _, r := range n.fieldRuns {
			if r.run.Link != "" && sel[0] > r.start && sel[0] <= r.end {
				sel, found = [2]int32{r.start, r.end}, true
				break
			}
		}
		if !found {
			return
		}
		SendMessage(n.ctl, emExSetSel, 0, Ptr(&sel))
	}
	doc := n.richDoc
	runs := rtf.Parse(streamOut(n.ctl, true), doc)
	for i := range runs {
		runs[i].Link = url
	}
	data := append(rtf.Write(runs, doc), 0)
	settext := [2]uint32{stSelection | stKeepUndo, 0}
	SendMessage(n.ctl, emSetTextEx, uintptr(unsafe.Pointer(&settext)), uintptr(unsafe.Pointer(&data[0])))
	SendMessage(n.ctl, emExSetSel, 0, Ptr(&sel))
	h.richEdited(n)
}

func init() {
	// EDITSTREAM's layout (richedit.h's pack(4)): the callback at 12. Go
	// pads the struct to 24 bytes, past the 20 the control reads.
	var es editStream
	if unsafe.Offsetof(es.callback) != 12 {
		panic("ryui: EDITSTREAM's layout")
	}
}
