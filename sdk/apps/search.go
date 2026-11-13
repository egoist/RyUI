package apps

import (
	"cmp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Search is the apps whose names match query, best first, as a
// launcher ranks them. Case and accents don't count ("cafe" finds
// Café), nor does the width of a character. The ranks:
//
//  1. the name is the query;
//  2. the name starts with it ("cal": Calculator);
//  3. a word of the name does ("code": Visual Studio Code; a word
//     starts after a space or a mark, and at a capital in CamelCase:
//     "edit" finds TextEdit), or the query is the starts of its words
//     in order ("vsc", "visstu": Visual Studio Code; "gc": Google
//     Chrome);
//  4. the name has it ("ode": Xcode);
//  5. the name has its letters in order ("clc": Calculator).
//
// Within a rank the apps are by name; apps that match none are left
// out. An empty query (or only spaces) is apps as they are.
func Search(apps []App, query string) []App {
	q := prepare(query)
	if len(q.text) == 0 {
		return apps
	}
	qc := q.compact()
	type hit struct {
		rank int
		key  string
		i    int
	}
	var hits []hit
	for i, a := range apps {
		if r := rank(prepare(a.Name), q.text, qc); r >= 0 {
			hits = append(hits, hit{r, fold(a.Name), i})
		}
	}
	slices.SortFunc(hits, func(a, b hit) int {
		return cmp.Or(a.rank-b.rank, strings.Compare(a.key, b.key), strings.Compare(apps[a.i].Name, apps[b.i].Name), a.i-b.i)
	})
	out := make([]App, len(hits))
	for j, h := range hits {
		out[j] = apps[h.i]
	}
	return out
}

// The ranks, best first.
const (
	rankExact = iota
	rankPrefix
	rankWord
	rankSubstring
	rankSubsequence
)

// rank is how name matches the query (q folded, qc without its spaces
// and marks), or -1 if it doesn't.
func rank(name prepared, q, qc []rune) int {
	n := name.text
	switch {
	case slices.Equal(n, q):
		return rankExact
	case hasPrefix(n, q):
		return rankPrefix
	}
	for i := range n {
		if name.start[i] && hasPrefix(n[i:], q) {
			return rankWord
		}
	}
	if len(qc) > 0 && initials(name.words(), qc) {
		return rankWord
	}
	if index(n, q) >= 0 {
		return rankSubstring
	}
	if len(qc) > 0 && subsequence(n, qc) {
		return rankSubsequence
	}
	return -1
}

// initials reports whether q is made of starts of words, in order: one
// start (a letter or more) per word, words skipped between them.
func initials(words [][]rune, q []rune) bool {
	// failed[{w, i}]: q[i:] can't be made of starts of words[w:].
	failed := make(map[[2]int]bool)
	var match func(w, i int) bool
	match = func(w, i int) bool {
		if i == len(q) {
			return true
		}
		if w == len(words) || failed[[2]int{w, i}] {
			return false
		}
		word := words[w]
		n := 0
		for n < len(word) && i+n < len(q) && word[n] == q[i+n] {
			n++
		}
		for l := n; l > 0; l-- {
			if match(w+1, i+l) {
				return true
			}
		}
		if match(w+1, i) {
			return true
		}
		failed[[2]int{w, i}] = true
		return false
	}
	return match(0, 0)
}

// subsequence reports whether s has q's runes in order.
func subsequence(s, q []rune) bool {
	j := 0
	for _, r := range s {
		if j < len(q) && r == q[j] {
			j++
		}
	}
	return j == len(q)
}

func hasPrefix(s, p []rune) bool { return len(s) >= len(p) && slices.Equal(s[:len(p)], p) }

func index(s, q []rune) int {
	for i := 0; i+len(q) <= len(s); i++ {
		if slices.Equal(s[i:i+len(q)], q) {
			return i
		}
	}
	return -1
}

// prepared is a name folded for matching: its runes, which of them
// start a word, and which are a word's (letters and digits) rather than
// what's between words.
type prepared struct {
	text  []rune
	start []bool
	word  []bool
}

// compact is the text without what's between words.
func (p prepared) compact() []rune {
	out := make([]rune, 0, len(p.text))
	for i, r := range p.text {
		if p.word[i] {
			out = append(out, r)
		}
	}
	return out
}

// words are the text's words: from each start to the next start or the
// next rune that's not a word's.
func (p prepared) words() [][]rune {
	var ws [][]rune
	for i := 0; i < len(p.text); {
		if !p.start[i] {
			i++
			continue
		}
		j := i + 1
		for j < len(p.text) && p.word[j] && !p.start[j] {
			j++
		}
		ws = append(ws, p.text[i:j])
		i = j
	}
	return ws
}

// The kinds of runes, for where words start.
const (
	kindOther = iota // a space, punctuation: between words
	kindUpper
	kindLower // and letters without case
	kindDigit
	kindMark // a combining mark: its letter's
)

func kindOf(r rune) int {
	switch {
	case unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r):
		return kindMark
	case unicode.IsUpper(r) || unicode.IsTitle(r):
		return kindUpper
	case unicode.IsLetter(r):
		return kindLower
	case unicode.IsDigit(r) || unicode.IsNumber(r):
		return kindDigit
	}
	return kindOther
}

// prepare folds s (fold) rune by rune, marking where its words start: at
// its first letter or digit, after what's between words, at a capital
// after a small letter (TextEdit), at the last capital of a run before
// a small letter (XMLParser: Parser), and between letters and digits
// (Office365). Spaces run together into one; those at the ends go.
func prepare(s string) prepared {
	var p prepared
	rs := []rune(s)
	prev := kindOther
	for i, r := range rs {
		k := kindOf(r)
		if k == kindMark {
			continue // its letter's (folded away)
		}
		next := kindOther
		for j := i + 1; j < len(rs); j++ {
			if nk := kindOf(rs[j]); nk != kindMark {
				next = nk
				break
			}
		}
		isWord := k != kindOther
		start := isWord && (prev == kindOther ||
			prev == kindLower && k == kindUpper ||
			prev == kindUpper && k == kindUpper && next == kindLower ||
			(prev == kindDigit) != (k == kindDigit))
		prev = k
		if !isWord && unicode.IsSpace(r) {
			if len(p.text) == 0 || p.text[len(p.text)-1] == ' ' {
				continue
			}
			p.text = append(p.text, ' ')
			p.start = append(p.start, false)
			p.word = append(p.word, false)
			continue
		}
		first := true
		for _, f := range foldRune(r) {
			p.text = append(p.text, f)
			p.start = append(p.start, start && first)
			p.word = append(p.word, isWord)
			first = false
		}
	}
	if n := len(p.text); n > 0 && p.text[n-1] == ' ' {
		p.text, p.start, p.word = p.text[:n-1], p.start[:n-1], p.word[:n-1]
	}
	return p
}

// fold is s as Search compares it: small letters, without accents
// (é is e), compatibility forms as their plain ones (ﬁ is fi, a
// full-width Ａ is a), and letters that don't decompose as the plain
// ones they're typed as (ß is ss, ø is o, æ is ae).
func fold(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if c := s[i]; c >= utf8.RuneSelf || 'A' <= c && c <= 'Z' {
			ascii = false
			break
		}
	}
	if ascii {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		for _, f := range foldRune(r) {
			b.WriteRune(f)
		}
	}
	return b.String()
}

// foldRune is r folded (fold): no runes for a combining mark.
func foldRune(r rune) []rune {
	if r < utf8.RuneSelf {
		return []rune{unicode.ToLower(r)}
	}
	r = unicode.ToLower(r)
	if s, ok := plain[r]; ok {
		return []rune(s)
	}
	var out []rune
	for _, d := range norm.NFKD.String(string(r)) {
		if unicode.Is(unicode.Mn, d) {
			continue
		}
		d = unicode.ToLower(d)
		if s, ok := plain[d]; ok {
			out = append(out, []rune(s)...)
			continue
		}
		out = append(out, d)
	}
	return out
}

// plain are the letters with no decomposition, as they're typed on a
// keyboard without them.
var plain = map[rune]string{
	'ß': "ss", 'æ': "ae", 'œ': "oe", 'ø': "o", 'đ': "d", 'ð': "d",
	'ł': "l", 'ħ': "h", 'ı': "i", 'þ': "th", 'ŀ': "l", 'ŧ': "t",
}
