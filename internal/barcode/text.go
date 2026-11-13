package barcode

import (
	"strings"
	"sync"
	"unicode/utf8"
)

// ECI charsets the readers honor.
const (
	eciNone     = -1
	eciLatin1   = 3
	eciShiftJIS = 20
	eciUTF8     = 26
)

// bytesText is b as text in the charset ECI eci names; with none, UTF-8
// if b is valid UTF-8, else ISO-8859-1.
func bytesText(b []byte, eci int) string {
	switch eci {
	case 1, eciLatin1:
		return latin1(b)
	case eciShiftJIS:
		return shiftJIS(b)
	case eciUTF8, 27, 170:
		return strings.ToValidUTF8(string(b), "�")
	}
	if utf8.Valid(b) {
		return string(b)
	}
	return latin1(b)
}

func latin1(b []byte) string {
	var s strings.Builder
	s.Grow(len(b))
	for _, c := range b {
		s.WriteRune(rune(c))
	}
	return s.String()
}

var (
	jisOnce  sync.Once
	jisRunes []rune
)

// jis is the character at JIS X 0208's row and cell (from 0), or 0.
func jis(row, cell int) rune {
	jisOnce.Do(func() { jisRunes = []rune(jisX0208) })
	if row < 0 || row >= 94 || cell < 0 || cell >= 94 {
		return 0
	}
	r := jisRunes[row*94+cell]
	if r == utf8.RuneError {
		return 0
	}
	return r
}

// sjisRune is the character of Shift JIS's two bytes, or 0.
func sjisRune(s1, s2 byte) rune {
	var row int
	switch {
	case s1 >= 0x81 && s1 <= 0x9F:
		row = int(s1-0x81) * 2
	case s1 >= 0xE0 && s1 <= 0xEF:
		row = int(s1-0xC1) * 2
	default:
		return 0
	}
	var cell int
	switch {
	case s2 >= 0x40 && s2 <= 0x7E:
		cell = int(s2 - 0x40)
	case s2 >= 0x80 && s2 <= 0x9E:
		cell = int(s2 - 0x41)
	case s2 >= 0x9F && s2 <= 0xFC:
		row++
		cell = int(s2 - 0x9F)
	default:
		return 0
	}
	return jis(row, cell)
}

// shiftJIS is b, Shift JIS, as text.
func shiftJIS(b []byte) string {
	var s strings.Builder
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch {
		case c < 0x80:
			s.WriteByte(c)
		case c >= 0xA1 && c <= 0xDF:
			// Half-width katakana.
			s.WriteRune(0xFF61 + rune(c-0xA1))
		case i+1 < len(b):
			if r := sjisRune(c, b[i+1]); r != 0 {
				s.WriteRune(r)
			} else {
				s.WriteRune(utf8.RuneError)
			}
			i++
		default:
			s.WriteRune(utf8.RuneError)
		}
	}
	return s.String()
}
