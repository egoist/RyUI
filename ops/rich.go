package ops

import (
	"math"
	"strings"
)

// RichStyle is a run's style in styled text's encoding (RichText's
// Content, a Rich TextField's Rich): zero fields are the view's.
type RichStyle struct {
	TextStyle  TextStyle
	Size       float32
	Weight     FontWeight
	Flags      uint8 // 1 italic, 2 monospaced, 4 underline, 8 strikethrough, 16 inline
	Color      Color
	Background Color
	Link       string
}

// RichRun is styled text; a "\n" in Text ends a paragraph.
type RichRun struct {
	Style RichStyle
	Text  string
}

// AppendRich appends runs in styled text's encoding, canonically: styles
// in the order runs first use them, a paragraph per line, runs of one
// style merged (so an editing host's text can be told from what it was
// sent).
func AppendRich(b []byte, runs []RichRun) []byte {
	type run struct {
		style int
		text  string
	}
	var styles []RichStyle
	paras := [][]run{nil}
	for _, r := range runs {
		for n, part := range strings.Split(r.Text, "\n") {
			if n > 0 {
				paras = append(paras, nil)
			}
			if part == "" {
				continue
			}
			k := -1
			for i, s := range styles {
				if s == r.Style {
					k = i
					break
				}
			}
			if k < 0 {
				styles = append(styles, r.Style)
				k = len(styles) - 1
			}
			p := &paras[len(paras)-1]
			if l := len(*p); l > 0 && (*p)[l-1].style == k {
				(*p)[l-1].text += part
			} else {
				*p = append(*p, run{k, part})
			}
		}
	}
	b = le.AppendUint32(b, uint32(len(styles)))
	for _, s := range styles {
		b = append(b, byte(s.TextStyle))
		b = le.AppendUint32(b, math.Float32bits(s.Size))
		b = append(b, byte(s.Weight), s.Flags)
		b = le.AppendUint64(b, uint64(s.Color))
		b = le.AppendUint64(b, uint64(s.Background))
		b = appendStr(b, s.Link)
	}
	b = le.AppendUint32(b, uint32(len(paras)))
	for _, p := range paras {
		b = le.AppendUint32(b, uint32(len(p)))
		for _, r := range p {
			b = le.AppendUint32(b, uint32(r.style))
			b = appendStr(b, r.text)
		}
	}
	return b
}

// DecodeRich reads styled text: a run per encoded run, each paragraph
// but the first starting with a "\n" (as a run of the last style); what
// can be read of a short one.
func DecodeRich(b []byte) []RichRun {
	var styles []RichStyle
	if len(b) < 4 {
		return nil
	}
	n := int(le.Uint32(b))
	b = b[4:]
	for range n {
		if len(b) < 23 {
			return nil
		}
		s := RichStyle{TextStyle: TextStyle(b[0]), Size: math.Float32frombits(le.Uint32(b[1:])), Weight: FontWeight(b[5]), Flags: b[6],
			Color: Color(le.Uint64(b[7:])), Background: Color(le.Uint64(b[15:]))}
		var ok bool
		if s.Link, b, ok = readStr(b[23:]); !ok {
			return nil
		}
		styles = append(styles, s)
	}
	if len(b) < 4 {
		return nil
	}
	paras := int(le.Uint32(b))
	b = b[4:]
	var out []RichRun
	var last RichStyle
	for p := range paras {
		if p > 0 {
			out = append(out, RichRun{Style: last, Text: "\n"})
		}
		if len(b) < 4 {
			break
		}
		runs := int(le.Uint32(b))
		b = b[4:]
		for range runs {
			if len(b) < 4 {
				return out
			}
			i := int(le.Uint32(b))
			var text string
			var ok bool
			if text, b, ok = readStr(b[4:]); !ok || i >= len(styles) {
				return out
			}
			last = styles[i]
			out = append(out, RichRun{Style: styles[i], Text: text})
		}
	}
	return out
}

func readStr(b []byte) (string, []byte, bool) {
	if len(b) < 4 {
		return "", b, false
	}
	n := int(le.Uint32(b))
	if n < 0 || 4+n > len(b) {
		return "", b, false
	}
	return string(b[4 : 4+n]), b[4+n:], true
}
