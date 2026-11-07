// Package rtf writes and reads the Rich Text Format a rich edit control
// (Windows's RICHEDIT50W) takes and gives: styled runs of text, with
// bold, italic, underline, strikethrough, a monospaced font, colors and
// HYPERLINK fields (the control's friendly links). It's the Windows
// host's way to a Rich TextField's styled text, kept here, without
// cgo or syscall, so it's tested on every system.
package rtf

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
)

// Color is an RGB color.
type Color struct{ R, G, B uint8 }

// Run is styled text; a "\n" in Text ends a paragraph.
type Run struct {
	Text      string
	Bold      bool
	Italic    bool
	Underline bool
	Strike    bool
	Mono      bool
	// Color and Background, if set (nil: the document's).
	Color      *Color
	Background *Color
	Link       string
}

// sameStyle reports whether a and b look the same.
func sameStyle(a, b Run) bool {
	return a.Bold == b.Bold && a.Italic == b.Italic && a.Underline == b.Underline && a.Strike == b.Strike &&
		a.Mono == b.Mono && a.Link == b.Link && eqColor(a.Color, b.Color) && eqColor(a.Background, b.Background)
}

func eqColor(a, b *Color) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

// Doc is the document's defaults: its font, its monospaced font, its size
// in points and its color.
type Doc struct {
	Face     string
	MonoFace string
	Size     float32
	Color    Color
}

// Write writes runs as RTF, in ASCII (\u escapes for the rest).
func Write(runs []Run, d Doc) []byte {
	var colors []Color
	color := func(c Color) int {
		for i, x := range colors {
			if x == c {
				return i + 1
			}
		}
		colors = append(colors, c)
		return len(colors)
	}
	color(d.Color)
	var body bytes.Buffer
	half := int(d.Size*2 + 0.5)
	for _, r := range runs {
		texts := strings.Split(r.Text, "\n")
		for i, t := range texts {
			if i > 0 {
				body.WriteString(`\par `)
			}
			if t == "" {
				continue
			}
			body.WriteByte('{')
			fmt.Fprintf(&body, `\f%d\fs%d`, map[bool]int{false: 0, true: 1}[r.Mono], half)
			if r.Bold {
				body.WriteString(`\b`)
			}
			if r.Italic {
				body.WriteString(`\i`)
			}
			if r.Underline {
				body.WriteString(`\ul`)
			}
			if r.Strike {
				body.WriteString(`\strike`)
			}
			if r.Color != nil {
				fmt.Fprintf(&body, `\cf%d`, color(*r.Color))
			} else {
				body.WriteString(`\cf1`)
			}
			if r.Background != nil {
				fmt.Fprintf(&body, `\chcbpat%d\highlight%d`, color(*r.Background), color(*r.Background))
			}
			body.WriteByte(' ')
			if r.Link != "" {
				body.WriteString(`{\field{\*\fldinst{HYPERLINK "`)
				escape(&body, strings.ReplaceAll(r.Link, `"`, `%22`))
				body.WriteString(`"}}{\fldrslt{`)
				escape(&body, t)
				body.WriteString(`}}}`)
			} else {
				escape(&body, t)
			}
			body.WriteByte('}')
		}
	}
	var b bytes.Buffer
	b.WriteString(`{\rtf1\ansi\ansicpg1252\deff0\uc1{\fonttbl{\f0\fnil `)
	escape(&b, d.Face)
	b.WriteString(`;}{\f1\fmodern\fprq1 `)
	escape(&b, d.MonoFace)
	b.WriteString(`;}}{\colortbl ;`)
	for _, c := range colors {
		fmt.Fprintf(&b, `\red%d\green%d\blue%d;`, c.R, c.G, c.B)
	}
	fmt.Fprintf(&b, "}\\pard\\f0\\fs%d\\cf1 ", half)
	b.Write(body.Bytes())
	b.WriteString("}")
	return b.Bytes()
}

// escape writes s as RTF text: its specials escaped, what isn't ASCII as
// \u escapes (UTF-16 units, a ? for readers without them).
func escape(b *bytes.Buffer, s string) {
	for _, r := range s {
		switch {
		case r == '\\' || r == '{' || r == '}':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\t':
			b.WriteString(`\tab `)
		case r >= 0x20 && r < 0x80:
			b.WriteRune(r)
		case r < 0x20:
		default:
			for _, u := range utf16.Encode([]rune{r}) {
				fmt.Fprintf(b, `\u%d?`, int16(u))
			}
		}
	}
}

// state is a group's character formatting.
type state struct {
	run      Run
	font     int
	uc       int  // the characters after a \u to skip
	skip     bool // a destination whose text isn't the document's
	fldinst  bool // a field's instruction
	fldrslt  bool // a field's result: its text, linked
	field    *string
	fonttbl  bool
	colortbl bool
}

// Parse reads RTF into runs, a run per stretch of one style; paragraphs
// end in "\n" (but the last: a document's final \par is its end). Text
// in the document's color is in no color of its own.
func Parse(data []byte, d Doc) []Run {
	p := &parser{data: data, monoFace: d.MonoFace}
	p.parse()
	for i := range p.runs {
		if c := p.runs[i].Color; c != nil && *c == d.Color {
			p.runs[i].Color = nil
		}
	}
	// Merge, drop the final paragraph end.
	var out []Run
	for _, r := range p.runs {
		if r.Text == "" {
			continue
		}
		if k := len(out); k > 0 && sameStyle(out[k-1], r) {
			out[k-1].Text += r.Text
		} else {
			out = append(out, r)
		}
	}
	if k := len(out); k > 0 && strings.HasSuffix(out[k-1].Text, "\n") {
		out[k-1].Text = strings.TrimSuffix(out[k-1].Text, "\n")
		if out[k-1].Text == "" {
			out = out[:k-1]
		}
	}
	return out
}

type parser struct {
	data   []byte
	i      int
	stack  []state
	st     state
	runs   []Run
	fonts  map[int]bool // font number: monospaced
	colors []*Color     // \colortbl's, index 0 the auto color
	color  Color        // the table entry being read
	text   []uint16     // UTF-16 units waiting (a surrogate pair's halves)
	cur     Run    // the style text is appended in
	pend    int    // characters to skip after \u
	link     string // the URL of the field whose result is read
	fontNum  int    // the font table entry being read
	fontName []rune // its name, so far
	monoFace string // the document's monospaced face: a font of that name is
}

func (p *parser) parse() {
	p.fonts = map[int]bool{}
	p.st.uc = 1
	for p.i < len(p.data) {
		c := p.data[p.i]
		switch c {
		case '{':
			p.flush()
			p.stack = append(p.stack, p.st)
			p.i++
		case '}':
			p.flush()
			if p.st.fldinst && p.st.field != nil {
				// The instruction's text: HYPERLINK "url".
				inst := strings.TrimSpace(*p.st.field)
				if strings.HasPrefix(inst, "HYPERLINK") {
					u := strings.TrimSpace(strings.TrimPrefix(inst, "HYPERLINK"))
					if strings.HasPrefix(u, `"`) {
						if e := strings.Index(u[1:], `"`); e >= 0 {
							u = u[1 : e+1]
						}
					} else if f := strings.Fields(u); len(f) > 0 {
						u = f[0]
					}
					p.link = u
				}
			}
			if k := len(p.stack); k > 0 {
				wasRslt := p.st.fldrslt
				p.st = p.stack[k-1]
				p.stack = p.stack[:k-1]
				if wasRslt && !p.st.fldrslt {
					p.link = ""
				}
			}
			p.i++
		case '\\':
			p.control()
		case '\r', '\n':
			p.i++
		default:
			if p.pend > 0 {
				p.pend--
				p.i++
				continue
			}
			p.char(uint16(c))
			p.i++
		}
	}
	p.flush()
}

func (p *parser) control() {
	p.i++ // the backslash
	if p.i >= len(p.data) {
		return
	}
	c := p.data[p.i]
	if !isLetter(c) {
		p.i++
		switch c {
		case '\\', '{', '}':
			p.char(uint16(c))
		case '\'':
			if p.i+2 <= len(p.data) {
				if v, err := strconv.ParseUint(string(p.data[p.i:p.i+2]), 16, 8); err == nil {
					if p.pend > 0 {
						p.pend--
					} else {
						p.char(cp1252(byte(v)))
					}
				}
				p.i += 2
			}
		case '*':
			p.st.skip = true
		case '~':
			p.char(0xA0)
		case '-':
		case '_':
			p.char('-')
		}
		return
	}
	start := p.i
	for p.i < len(p.data) && isLetter(p.data[p.i]) {
		p.i++
	}
	word := string(p.data[start:p.i])
	num, has := 0, false
	if p.i < len(p.data) && (p.data[p.i] == '-' || isDigit(p.data[p.i])) {
		s := p.i
		p.i++
		for p.i < len(p.data) && isDigit(p.data[p.i]) {
			p.i++
		}
		num, _ = strconv.Atoi(string(p.data[s:p.i]))
		has = true
	}
	if p.i < len(p.data) && p.data[p.i] == ' ' {
		p.i++
	}
	on := !has || num != 0
	r := &p.st.run
	switch word {
	case "fonttbl":
		p.st.fonttbl, p.st.skip = true, false
	case "colortbl":
		p.st.colortbl = true
		p.colors = p.colors[:0]
		p.color = Color{}
	case "red":
		p.color.R = uint8(num)
	case "green":
		p.color.G = uint8(num)
	case "blue":
		p.color.B = uint8(num)
	case "f":
		p.st.font = num
		if p.st.fonttbl {
			p.fontNum = num
			p.fontName = p.fontName[:0]
			return
		}
		r.Mono = p.fonts[num]
	case "fmodern":
		if p.st.fonttbl {
			p.fonts[p.fontNum] = true
		}
	case "fprq":
		if p.st.fonttbl && num == 1 {
			p.fonts[p.fontNum] = true
		}
	case "b":
		r.Bold = on
	case "i":
		r.Italic = on
	case "ul":
		r.Underline = on
	case "ulnone":
		r.Underline = false
	case "strike":
		r.Strike = on
	case "cf":
		r.Color = p.colorAt(num)
	case "highlight", "chcbpat", "cb":
		r.Background = p.colorAt(num)
	case "plain":
		*r = Run{Mono: p.fonts[0]}
	case "par", "line":
		p.char('\n')
	case "tab":
		p.char('\t')
	case "uc":
		p.st.uc = num
	case "u":
		u := num
		if u < 0 {
			u += 65536
		}
		p.char(uint16(u))
		p.pend = p.st.uc
	case "fldinst":
		p.st.fldinst = true
		s := ""
		p.st.field = &s
	case "fldrslt":
		p.st.fldrslt = true
	case "field":
	case "pict", "object", "info", "stylesheet", "generator", "listtable", "listoverridetable", "themedata", "colorschememapping", "latentstyles", "datastore", "xmlnstbl", "rsidtbl", "mmathPr":
		p.st.skip = true
	}
}

// colorAt is \colortbl's color n (nil: the auto color, or none).
func (p *parser) colorAt(n int) *Color {
	if n <= 0 || n >= len(p.colors) {
		return nil
	}
	c := p.colors[n]
	if c == nil {
		return nil
	}
	cc := *c
	return &cc
}

// char takes a character of the current group.
func (p *parser) char(u uint16) {
	switch {
	case p.st.colortbl && u == ';':
		c := p.color
		if len(p.colors) == 0 && c == (Color{}) {
			p.colors = append(p.colors, nil) // the auto color
		} else {
			p.colors = append(p.colors, &c)
		}
		p.color = Color{}
		return
	case p.st.fonttbl:
		// A font's name, up to its ;: the monospaced face's is monospaced
		// (a control may name it without \fmodern or \fprq1).
		if u == ';' {
			if p.monoFace != "" && strings.EqualFold(strings.TrimSpace(string(p.fontName)), p.monoFace) {
				p.fonts[p.fontNum] = true
			}
			p.fontName = p.fontName[:0]
		} else {
			p.fontName = append(p.fontName, rune(u))
		}
		return
	case p.st.colortbl:
		return
	case p.st.fldinst:
		if p.st.field != nil {
			*p.st.field += string(rune(u))
		}
		return
	case p.st.skip:
		return
	}
	r := p.st.run
	r.Link = p.link
	if !sameStyle(r, p.cur) {
		p.flush()
		p.cur = r
	}
	p.text = append(p.text, u)
}

// flush ends the run text was appended to.
func (p *parser) flush() {
	if len(p.text) == 0 {
		return
	}
	r := p.cur
	r.Text = string(utf16.Decode(p.text))
	p.runs = append(p.runs, r)
	p.text = p.text[:0]
}

func isLetter(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }

// cp1252 is a Windows-1252 byte's character (\'hh escapes in \ansicpg1252).
func cp1252(b byte) uint16 {
	if b >= 0x80 && b <= 0x9F {
		return cp1252High[b-0x80]
	}
	return uint16(b)
}

var cp1252High = [32]uint16{
	0x20AC, 0x81, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x8D, 0x017D, 0x8F,
	0x90, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x9D, 0x017E, 0x0178,
}
