package signal

import (
	"runtime"
	"strconv"
	"strings"
)

// Places: asuka dev reads the app's source as it builds it, and gives the
// run the names of the places there (KeepPlaces), so that kept state
// (keep.go) is known by names that edits elsewhere don't change. Go names
// a closure by its place among its function's closures (Screen.func2),
// so adding one, a button's OnPress, renumbers those after it; here a
// closure is named by what it is in its function: the variable it's
// assigned to, the field it's for, the call and argument it's passed as
// (Screen>body, Screen>ui.Show#1). A signal is named likewise, by the
// variable or field it's made for, so that adding one doesn't move the
// others.
//
// The run finds the closure a frame is in by its file and line: the
// closures there whose lines hold the frame's, at the depth its runtime
// name says past its function's (Screen.func2.func1: two deep), and, of
// those on the same line, the one at its place among its siblings.

var places *placeTable

type placeTable struct {
	files map[string]*placeFile // by path, as runtime.Frame.File says it
}

type placeFile struct {
	funcs []string // the declared functions' runtime names
	lits  []placeLit
	sites []placeSite
	// reset, when the file says //asuka:reset, is a digest of its
	// content: its signals start over when it's edited.
	reset string
}

// placeLit is a function literal: its lines, its depth among the
// function's literals (1: in the function itself), its place among its
// siblings (1 for the first) and its name.
type placeLit struct {
	start, end, depth, num int
	name                   string
}

// placeSite is a call that makes a signal, its lines and the name of
// what it's made for.
type placeSite struct {
	start, end int
	name       string
}

// KeepPlaces gives keeping (KeepState) the names of the places in the
// app's source where it makes signals and closures. asuka dev calls it
// from the code it adds to the app's main package as it builds it, from
// what it read there; an app never does. The table has a line per fact,
// fields separated by tabs: "F path" starts a file's, "R digest" says it
// resets, "D name" is a function declared in it (its runtime name), "L
// start end depth num name" a function literal, "S start end name" a
// call that makes a signal.
func KeepPlaces(table string) {
	t := &placeTable{files: map[string]*placeFile{}}
	var f *placeFile
	for line := range strings.SplitSeq(table, "\n") {
		fields := strings.Split(line, "\t")
		switch {
		case fields[0] == "F" && len(fields) == 2:
			f = &placeFile{}
			t.files[fields[1]] = f
		case f == nil:
		case fields[0] == "R" && len(fields) == 2:
			f.reset = fields[1]
		case fields[0] == "D" && len(fields) == 2:
			f.funcs = append(f.funcs, fields[1])
		case fields[0] == "L" && len(fields) == 6:
			f.lits = append(f.lits, placeLit{atoi(fields[1]), atoi(fields[2]), atoi(fields[3]), atoi(fields[4]), fields[5]})
		case fields[0] == "S" && len(fields) == 4:
			f.sites = append(f.sites, placeSite{atoi(fields[1]), atoi(fields[2]), fields[3]})
		}
	}
	places = t
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// closure names the function literal fr is in ("" when it isn't one, or
// when the table can't tell which).
func (t *placeTable) closure(fr runtime.Frame) string {
	f := t.files[fr.File]
	if f == nil {
		return ""
	}
	// The function it's in: the longest declared one its name starts with.
	decl := ""
	for _, d := range f.funcs {
		if len(d) > len(decl) && len(fr.Function) > len(d) && strings.HasPrefix(fr.Function, d) &&
			(fr.Function[len(d)] == '.' || fr.Function[len(d)] == '-') {
			decl = d
		}
	}
	if decl == "" {
		return ""
	}
	depth, num := closureDepth(fr.Function[len(decl):])
	if depth == 0 {
		return ""
	}
	var pick *placeLit
	n := 0
	for i := range f.lits {
		l := &f.lits[i]
		if l.depth != depth || fr.Line < l.start || fr.Line > l.end {
			continue
		}
		n++
		if n == 1 || l.num == num {
			pick = l
		}
	}
	if n > 1 && pick.num != num {
		return ""
	}
	if pick == nil {
		return ""
	}
	return pick.name
}

// site names what the signal made at fr is for ("" when nothing does),
// and the digest of a file that resets.
func (t *placeTable) site(fr runtime.Frame) (name, reset string) {
	f := t.files[fr.File]
	if f == nil {
		return "", ""
	}
	for _, s := range f.sites {
		if fr.Line < s.start || fr.Line > s.end {
			continue
		}
		if name != "" {
			return "", f.reset // two on the line: can't tell
		}
		name = s.name
	}
	return name, f.reset
}

// closureDepth reads, from what follows a function's name in a
// closure's runtime name, how deep among the function's literals it is
// (0: not a literal) and its place among its siblings. Literals are
// named .funcN after their parent, or .N (as inlined, or in a
// range-over-func loop's body); a loop's body is -rangeN, not a literal:
// .func2.func1, .1, -range1.func1, -range1-range1.1, .func1-range1.func1.
func closureDepth(rest string) (depth, num int) {
	for seg := range strings.SplitSeq(rest, ".") {
		base, _, _ := strings.Cut(seg, "-")
		if n, ok := number(strings.TrimPrefix(base, "func")); ok {
			depth++
			num = n
		}
	}
	return depth, num
}

func number(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, _ := strconv.Atoi(s)
	return n, true
}
