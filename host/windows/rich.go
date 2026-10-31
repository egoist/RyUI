//go:build windows && amd64

package windows

import (
	"math"
	"unicode/utf16"
	"unsafe"

	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
)

// A RichText is a rich edit control (msftedit's RICHEDIT50W: its
// typesetter, selection, copying, links and accessibility), read-only,
// without margins, so what's measured is what's drawn. Its background is
// what's behind it (a rich edit draws an opaque one). Outside a
// Selectable it takes the mouse only over links: elsewhere the mouse
// goes to the views under it. Inline components are pictures of their
// size (EM_INSERTIMAGE), their views placed where the text puts them.
type rich struct {
	content   []byte
	inlines   []inlineSize
	dirty     bool
	links     []linkRun
	inlineAt  []int32 // each inline component's character index
	paraEnds  []int32 // each paragraph's last character index (its \r, or the end)
	requested Rect    // EN_REQUESTRESIZE's answer
	maxWidth  float32 // the widest paragraph, unwrapped (points)
	built     bool
	selectOK  bool
	pressed   int  // the link the mouse went down on
	bg        rgba // the background it has, what's behind it
}

type inlineSize struct {
	id   ops.NodeID
	w, h float32
}

type linkRun struct {
	start, end int32
	url        string
}

const (
	enLink          = 0x070B
	enRequestResize = 0x0701

	emRequestResize  = WM_USER + 65
	emSetBkgndColor  = WM_USER + 67
	emSetCharFormat  = WM_USER + 68
	emSetEventMask   = WM_USER + 69
	emSetParaFormat  = WM_USER + 71
	emExSetSel       = WM_USER + 55
	emExLineFromChar = WM_USER + 54
	emSetUndoLimit   = WM_USER + 82
	emSetTextMode    = WM_USER + 89
	emSetTypography  = WM_USER + 202
	emInsertImage    = WM_USER + 314
	emPosFromChar    = 0x00D6
	emCharFromPos    = 0x00D7
	emLineIndex      = 0x00BB
	emGetLineCount   = 0x00BA
	emSetEditStyle   = WM_USER + 204
	emSetReadOnly    = 0x00CF
	enmLink          = 0x04000000
	enmRequestResize = 0x00040000
	cfmBold          = 0x00000001
	cfmItalic        = 0x00000002
	cfmUnderline     = 0x00000004
	cfmStrikeOut     = 0x00000008
	cfmLink          = 0x00000020
	cfmSize          = 0x80000000
	cfmColor         = 0x40000000
	cfmFace          = 0x20000000
	cfmWeight        = 0x00400000
	cfmBackColor     = 0x04000000
	cfeLink          = 0x00000020
	cfeAutoBackColor = 0x04000000
	scfSelection     = 0x0001
	scfAll           = 0x0004
)

// charFormat is CHARFORMAT2W.
type charFormat struct {
	Size          uint32
	Mask          uint32
	Effects       uint32
	Height        int32 // twips
	Offset        int32
	TextColor     uint32
	CharSet       byte
	PitchFamily   byte
	FaceName      [32]uint16
	Weight        uint16
	Spacing       int16
	BackColor     uint32
	LCID          uint32
	Reserved      uint32
	Style         int16
	Kerning       uint16
	UnderlineType byte
	Animation     byte
	RevAuthor     byte
	UnderColor    byte
}

var richLoaded bool

func (h *Host) makeRich(n *node) {
	if !richLoaded {
		richLoaded = true
		LoadSystemLibrary("msftedit.dll")
	}
	n.rich = &rich{dirty: true}
	h.makeControl(n, "RICHEDIT50W", ES_MULTILINE|ES_READONLY|ES_NOHIDESEL, 0)
	ctl := n.ctl
	SendMessage(ctl, emSetEventMask, 0, enmRequestResize)
	SendMessage(ctl, emSetUndoLimit, 0, 0)
	SendMessage(ctl, EM_SETMARGINS, EC_LEFTMARGIN|EC_RIGHTMARGIN, 0)
	SendMessage(ctl, emSetTypography, 1, 1) // TO_ADVANCEDTYPOGRAPHY: kerning, ligatures
	Subclass(ctl, func(hwnd uintptr, m uint32, w, l uintptr) uintptr {
		return h.richMessage(hwnd, m, w, l)
	})
}

// richMessage is the rich edit's subclass: outside a Selectable it lets
// the mouse through but over links, and the wheel goes on to the scroll
// view it's in.
func (h *Host) richMessage(hwnd uintptr, m uint32, w, l uintptr) uintptr {
	n := h.byHwnd[hwnd]
	if n == nil || n.rich == nil {
		return DefSubclass(hwnd, m, w, l)
	}
	switch m {
	case wmNCHitTest:
		if n.rich.selectOK {
			break
		}
		p := ScreenToClient(hwnd, Point{X: XParam(l), Y: YParam(l)})
		if n.rich.linkAt(hwnd, p) < 0 {
			return htTransparent
		}
	case WM_PAINT:
		// What's behind it, now that the layout is done (a paragraph
		// streamed in can be laid out before its parent grows to it).
		if _, ok := h.resolve(n.background); !ok {
			if c := h.colorBehind(n); c != n.rich.bg {
				n.rich.bg = c
				SendMessage(hwnd, emSetBkgndColor, 0, uintptr(c.ref()))
			}
		}
	case WM_MOUSEWHEEL, WM_MOUSEHWHEEL:
		return SendMessage(Parent(hwnd), m, w, l)
	case WM_SETCURSOR:
		if n.rich.linkAt(hwnd, ScreenToClient(hwnd, CursorPos())) >= 0 {
			SetCursor(Cursor(IDC_HAND))
			return 1
		}
	case WM_LBUTTONDOWN:
		n.rich.pressed = n.rich.linkAt(hwnd, Point{X: XParam(l), Y: YParam(l)})
	case WM_LBUTTONUP:
		if k := n.rich.linkAt(hwnd, Point{X: XParam(l), Y: YParam(l)}); k >= 0 && k == n.rich.pressed {
			r := DefSubclass(hwnd, m, w, l)
			h.openLink(n, n.rich.links[k].url)
			return r
		}
	case WM_SETFOCUS:
		if !n.rich.selectOK {
			// Not a place to type: the focus goes back where it was.
			r := DefSubclass(hwnd, m, w, l)
			HideCaret(hwnd)
			return r
		}
	}
	return DefSubclass(hwnd, m, w, l)
}

// linkAt is the index of the link under p (client pixels), or -1.
func (r *rich) linkAt(hwnd uintptr, p Point) int {
	if len(r.links) == 0 {
		return -1
	}
	pt := p
	i := int32(SendMessage(hwnd, emCharFromPos, 0, Ptr(&pt)))
	for k, lr := range r.links {
		if i >= lr.start && i < lr.end {
			// The character's box contains p (not just the nearest one).
			var a, b Point
			SendMessage(hwnd, emPosFromChar, Ptr(&a), uintptr(i))
			SendMessage(hwnd, emPosFromChar, Ptr(&b), uintptr(i+1))
			if p.Y >= a.Y && (b.X > a.X && p.X <= b.X+2 || b.Y > a.Y) {
				return k
			}
		}
	}
	return -1
}

func (r *rich) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropRichTextContent:
		r.content = append(r.content[:0], v.Bytes...)
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
				r.inlines = append(r.inlines, inlineSize{ops.NodeID(le.Uint32(b[o:])), f32(b, o+4), f32(b, o+8)})
			}
		}
	}
	r.invalidate(h, n)
}

func (r *rich) invalidate(h *Host, n *node) {
	r.dirty = true
	if r.built {
		h.afterCore(func() {
			if n.rich == r && n.ctl != 0 {
				r.build(h, n)
				r.place(h, n)
			}
		})
	}
}

// style is a span's style from the content's table.
type richStyle struct {
	textStyle, weight int32
	size              float32
	flags             byte
	color, bg         ops.Color
	link              string
}

// twips is a size in points (DIPs) as the rich edit's font size: twips
// at the window's DPI, as the control converts them at the system's.
func (h *Host) twips(size float32) int32 {
	sys := float32(DPI(0))
	return int32(math.Round(float64(size * 15 * float32(h.dpi) / sys)))
}

// format is the CHARFORMAT2 of a font spec and colors.
func (h *Host) format(s fontSpec, color rgba, bg *rgba, link bool) charFormat {
	cf := charFormat{Mask: cfmSize | cfmFace | cfmWeight | cfmColor | cfmItalic | cfmUnderline | cfmStrikeOut | cfmLink | cfmBackColor | cfmBold}
	cf.Size = uint32(unsafe.Sizeof(cf))
	cf.Height = h.twips(s.size)
	cf.Weight = uint16(s.weight)
	if s.weight >= 600 {
		cf.Effects |= cfmBold
	}
	copy(cf.FaceName[:31], UTF16Units(h.face(s)))
	cf.Mask |= 0x08000000 // CFM_CHARSET
	cf.CharSet = 1        // DEFAULT_CHARSET: the face named, not one of the charset's
	if s.mono {
		cf.PitchFamily = 1 | 0x30 // FIXED_PITCH, FF_MODERN
	}
	cf.TextColor = color.ref()
	if s.italic {
		cf.Effects |= cfmItalic
	}
	if s.underline {
		cf.Effects |= cfmUnderline
	}
	if s.strike {
		cf.Effects |= cfmStrikeOut
	}
	if link {
		// Not CFE_LINK, which the control draws in a color of its own:
		// the host colors links and takes their clicks.
		cf.Effects |= cfmUnderline
	}
	if bg != nil {
		cf.BackColor = bg.ref()
	} else {
		cf.Effects |= cfeAutoBackColor
	}
	return cf
}

// build sets the control's text and formats from the content, styled by
// the view's text props (the spans' defaults).
func (r *rich) build(h *Host, n *node) {
	if !r.dirty || n.ctl == 0 {
		return
	}
	r.dirty = false
	r.built = true
	r.selectOK = false
	for p := n.parent; p != nil; p = p.parent {
		if p.kind == ops.KindSelectable {
			r.selectOK = true
		}
	}
	ctl := n.ctl
	behind := h.backdrop(n)
	if c, ok := h.resolve(n.background); ok && c.a > 0 {
		behind = c.over(behind)
	}
	base, ok := h.resolve(n.color)
	if !ok {
		base = h.theme.label
	}
	b := r.content
	var styles []richStyle
	if len(b) >= 4 {
		count := int(le.Uint32(b))
		b = b[4:]
		for range count {
			if len(b) < 23 {
				break
			}
			s := richStyle{textStyle: int32(b[0]), size: f32(b, 1), weight: int32(b[5]), flags: b[6],
				color: ops.Color(le.Uint64(b[7:])), bg: ops.Color(le.Uint64(b[15:]))}
			s.link, b = readString(b[23:])
			styles = append(styles, s)
		}
	}
	type run struct {
		start, end int32
		style      int
	}
	var text []uint16
	var runs []run
	r.links, r.inlineAt, r.paraEnds = r.links[:0], r.inlineAt[:0], r.paraEnds[:0]
	if len(b) >= 4 {
		paras := int(le.Uint32(b))
		b = b[4:]
		for p := range paras {
			if len(b) < 4 {
				break
			}
			count := int(le.Uint32(b))
			b = b[4:]
			for range count {
				if len(b) < 4 {
					break
				}
				si := int(le.Uint32(b))
				var s string
				s, b = readString(b[4:])
				if si >= len(styles) {
					continue
				}
				start := int32(len(text))
				if styles[si].flags&16 != 0 {
					// An inline component: a placeholder, a picture once
					// formatted.
					r.inlineAt = append(r.inlineAt, start)
					text = append(text, ' ')
				} else {
					text = append(text, utf16.Encode([]rune(s))...)
				}
				runs = append(runs, run{start, int32(len(text)), si})
				if l := styles[si].link; l != "" {
					r.links = append(r.links, linkRun{start, int32(len(text)), l})
				}
			}
			r.paraEnds = append(r.paraEnds, int32(len(text)))
			if p < paras-1 {
				text = append(text, '\r')
			}
		}
	}
	SendMessage(ctl, WM_SETREDRAW, 0, 0)
	r.bg = behind
	SendMessage(ctl, emSetBkgndColor, 0, uintptr(behind.ref()))
	SetWindowText(ctl, string(utf16.Decode(text)))
	all := h.format(h.nodeSpec(n), base.over(behind), nil, false)
	SendMessage(ctl, emSetCharFormat, scfAll, Ptr(&all))
	for _, ru := range runs {
		st := styles[ru.style]
		ts, weight, size := st.textStyle, st.weight, st.size
		if ts == 0 {
			ts = n.textStyle
		}
		if size == 0 {
			size = n.fontSize
		}
		if weight == 0 {
			weight = n.fontWeight
		}
		spec := h.spec(ts, weight, size, n.fontFamily)
		spec.italic = st.flags&1 != 0
		spec.mono = st.flags&2 != 0
		spec.underline = st.flags&4 != 0
		spec.strike = st.flags&8 != 0
		color := base
		if c, ok := h.resolve(st.color); ok {
			color = c
		} else if st.link != "" {
			color = h.theme.accentText
		}
		var bg *rgba
		if c, ok := h.resolve(st.bg); ok && c.a > 0 {
			c = c.over(behind)
			bg = &c
		}
		under := behind
		if bg != nil {
			under = *bg
		}
		cf := h.format(spec, color.over(under), bg, st.link != "")
		sel := [2]int32{ru.start, ru.end}
		SendMessage(ctl, emExSetSel, 0, Ptr(&sel))
		SendMessage(ctl, emSetCharFormat, scfSelection, Ptr(&cf))
	}
	// Alignment, for every paragraph.
	pf := paraFormat{Mask: 0x8} // PFM_ALIGNMENT
	pf.Size = uint32(unsafe.Sizeof(pf))
	switch ops.TextAlign(n.textAlign) {
	case ops.TextAlignCenter:
		pf.Alignment = 3
	case ops.TextAlignEnd:
		pf.Alignment = 2
	case ops.TextAlignJustify:
		pf.Alignment = 4
	default:
		pf.Alignment = 1
	}
	sel := [2]int32{0, -1}
	SendMessage(ctl, emExSetSel, 0, Ptr(&sel))
	SendMessage(ctl, emSetParaFormat, 0, Ptr(&pf))
	h.insertInlines(r, ctl)
	// A line limit ends the last line shown with an ellipsis.
	ellipsis := uintptr(0)
	if n.lineLimit > 0 {
		ellipsis = 1 // ELLIPSIS_END
	}
	SendMessage(ctl, WM_USER+306, 0, ellipsis) // EM_SETELLIPSISMODE
	sel = [2]int32{0, 0}
	SendMessage(ctl, emExSetSel, 0, Ptr(&sel))
	SendMessage(ctl, WM_SETREDRAW, 1, 0)
	Invalidate(ctl)
	r.maxWidth = -1
}

// paraFormat is PARAFORMAT.
type paraFormat struct {
	Size      uint32
	Mask      uint32
	Numbering uint16
	Effects   uint16
	Start     int32
	Right     int32
	Offset    int32
	Alignment uint16
	TabCount  int16
	Tabs      [32]int32
}

// insertInlines replaces each inline component's placeholder with a
// transparent picture of its size, sitting on the baseline.
func (h *Host) insertInlines(r *rich, ctl uintptr) {
	if len(r.inlineAt) == 0 {
		return
	}
	// A read-only control takes no pictures.
	SendMessage(ctl, emSetReadOnly, 0, 0)
	defer SendMessage(ctl, emSetReadOnly, 1, 0)
	for i := len(r.inlineAt) - 1; i >= 0; i-- {
		if i >= len(r.inlines) {
			continue
		}
		in := r.inlines[i]
		at := r.inlineAt[i]
		sel := [2]int32{at, at + 1}
		SendMessage(ctl, emExSetSel, 0, Ptr(&sel))
		stream := TransparentPNGStream()
		if stream == 0 {
			continue
		}
		// HIMETRIC: hundredths of a millimeter, 2540 an inch, at 96 DPI
		// as the control takes it.
		params := struct {
			Width, Height int32
			Ascent        int32
			Type          int32
			Alt           *uint16
			Stream        uintptr
		}{
			Width:  int32(math.Round(float64(in.w) * 2540 / 96 * float64(h.dpi) / float64(DPI(0)))),
			Height: int32(math.Round(float64(in.h) * 2540 / 96 * float64(h.dpi) / float64(DPI(0)))),
			Type:   24, // TA_BASELINE
			Alt:    UTF16(" "),
			Stream: stream,
		}
		SendMessage(ctl, emInsertImage, 0, Ptr(&params))
		Release(stream)
	}
}

// layoutAt lays the text out at a width (pixels) and returns the height
// it takes (EN_REQUESTRESIZE answers EM_REQUESTRESIZE).
func (r *rich) layoutAt(h *Host, n *node, width int32) int32 {
	ctl := n.ctl
	cur := ClientRect(ctl)
	if cur.W() != width {
		SetWindowPos(ctl, 0, 0, 0, width, max(cur.H(), 1), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOMOVE|SWP_NOREDRAW)
	}
	r.requested = Rect{}
	SendMessage(ctl, emRequestResize, 0, 0)
	return r.requested.H()
}

// widest is the widest paragraph's width, unwrapped, in pixels: its
// paragraph end's x.
func (r *rich) widest(h *Host, n *node) int32 {
	r.layoutAt(h, n, 32000)
	widest := int32(0)
	for _, e := range r.paraEnds {
		var p Point
		SendMessage(n.ctl, emPosFromChar, Ptr(&p), uintptr(e))
		widest = max(widest, p.X)
	}
	return widest + 1
}

func (r *rich) measure(h *Host, n *node, c layout.Constraints) (float32, float32) {
	r.build(h, n)
	if len(r.paraEnds) == 0 || len(r.paraEnds) == 1 && r.paraEnds[0] == 0 {
		return 0, 0
	}
	if r.maxWidth < 0 {
		r.maxWidth = h.pt(r.widest(h, n))
	}
	minContent := float32(0)
	if c.AvailW <= -layout.Inf && c.Width != c.Width {
		minContent = min(r.minContent(h, n), r.maxWidth)
	}
	w := avail(c.Width, c.AvailW, minContent)
	w = min(w, r.maxWidth)
	if c.Width == c.Width {
		w = c.Width
	}
	ht := r.layoutAt(h, n, max(h.px(w), 1))
	if n.lineLimit > 0 {
		if lines := int32(SendMessage(n.ctl, emGetLineCount, 0, 0)); lines > int32(n.lineLimit) {
			first := int32(SendMessage(n.ctl, emLineIndex, uintptr(n.lineLimit), 0))
			var p Point
			SendMessage(n.ctl, emPosFromChar, Ptr(&p), uintptr(first))
			ht = min(ht, p.Y)
		}
	}
	// Back at the view's width.
	if f := n.frame[2]; f > 0 {
		r.layoutAt(h, n, h.px(f))
	}
	return w, h.pt(ht)
}

// minContent is the widest word, in points: the text laid out a pixel
// wide breaks at every word.
func (r *rich) minContent(h *Host, n *node) float32 {
	r.layoutAt(h, n, 1)
	widest := int32(0)
	lines := int32(SendMessage(n.ctl, emGetLineCount, 0, 0))
	for i := range lines {
		start := int32(SendMessage(n.ctl, emLineIndex, uintptr(i), 0))
		end := int32(SendMessage(n.ctl, emLineIndex, uintptr(i+1), 0))
		if i == lines-1 || end < 0 {
			end = start + int32(SendMessage(n.ctl, 0x00C1, uintptr(start), 0)) // EM_LINELENGTH
		}
		var a, b Point
		SendMessage(n.ctl, emPosFromChar, Ptr(&a), uintptr(start))
		SendMessage(n.ctl, emPosFromChar, Ptr(&b), uintptr(max(end-1, start)))
		widest = max(widest, b.X-a.X+h.px(10))
	}
	return h.pt(widest)
}

func (r *rich) resized(h *Host, n *node) {
	r.place(h, n)
}

// place lays the text out at the view's size and puts the inline
// components where their pictures are.
func (r *rich) place(h *Host, n *node) {
	if n.ctl == 0 {
		return
	}
	r.build(h, n)
	cr := ClientRect(n.hwnd)
	SetWindowPos(n.ctl, 0, 0, 0, cr.W(), cr.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
	r.placeInlines(h, n)
}

func (r *rich) placeInlines(h *Host, n *node) {
	for i, at := range r.inlineAt {
		if i >= len(r.inlines) {
			break
		}
		in := r.inlines[i]
		c := h.nodes[in.id]
		if c == nil || c.hwnd == 0 {
			continue
		}
		var p Point
		SendMessage(n.ctl, emPosFromChar, Ptr(&p), uintptr(at))
		if Parent(c.hwnd) != n.hwnd {
			SetParent(c.hwnd, n.hwnd)
		}
		SetWindowPos(c.hwnd, HWND_TOP, p.X, p.Y, h.px(in.w), h.px(in.h), SWP_NOACTIVATE|SWP_NOCOPYBITS)
	}
}

// openLink opens a link the user clicked: Link, or the system's browser
// when the core doesn't listen.
func (h *Host) openLink(n *node, url string) {
	if n.listening(ops.EventLink) {
		h.emit(n, ops.Event{Kind: ops.EventLink, Str: url})
	} else {
		OpenURL(url)
	}
}
