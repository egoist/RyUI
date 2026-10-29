package layout

import (
	"bufio"
	"encoding/xml"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var updateKnown = flag.Bool("update-known", false, "rewrite testdata/css/known_failures.txt")
var verbose = flag.Bool("css-verbose", false, "print every failing fixture's trees")

type xNode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Text    string     `xml:",chardata"`
	Kids    []xNode    `xml:",any"`
}

func (x *xNode) attr(name string) (string, bool) {
	for _, a := range x.Attrs {
		if a.Name.Local == name {
			return strings.TrimSpace(a.Value), true
		}
	}
	return "", false
}

type xTest struct {
	Name     string `xml:"name,attr"`
	Rounding string `xml:"use-rounding,attr"`
	Viewport struct {
		Width  string `xml:"width,attr"`
		Height string `xml:"height,attr"`
	} `xml:"viewport"`
	Input  xNode `xml:"input"`
	Expect xNode `xml:"expectations"`
}

func parseLength(s string) Length {
	switch {
	case s == "" || s == "auto" || s == "none":
		return Auto
	case strings.HasSuffix(s, "%"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 32)
		return Pct(float32(v))
	case strings.HasSuffix(s, "px"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 32)
		return Pt(float32(v))
	}
	if v, err := strconv.ParseFloat(s, 32); err == nil {
		return Pt(float32(v))
	}
	return Auto // keywords we don't support (min-content, stretch, ...)
}

func parseAlign(s string) Align {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "safe "), "unsafe ")
	switch s {
	case "start", "self-start", "left":
		return AlignStart
	case "end", "self-end", "right":
		return AlignEnd
	case "flex-start":
		return AlignFlexStart
	case "flex-end":
		return AlignFlexEnd
	case "center":
		return AlignCenter
	case "baseline", "first baseline", "last baseline":
		return AlignBaseline
	case "stretch":
		return AlignStretch
	}
	return AlignAuto
}

func parseContent(s string) Content {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "safe "), "unsafe ")
	switch s {
	case "start", "left":
		return ContentStart
	case "end", "right":
		return ContentEnd
	case "flex-start":
		return ContentFlexStart
	case "flex-end":
		return ContentFlexEnd
	case "center":
		return ContentCenter
	case "stretch":
		return ContentStretch
	case "space-between":
		return ContentSpaceBetween
	case "space-around":
		return ContentSpaceAround
	case "space-evenly":
		return ContentSpaceEvenly
	}
	return ContentNormal
}

func parseOverflow(s string) Overflow {
	switch s {
	case "hidden":
		return OverflowHidden
	case "clip":
		return OverflowClip
	case "scroll", "auto":
		return OverflowScroll
	}
	return OverflowVisible
}

// splitTop splits s on spaces outside parentheses and brackets.
func splitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case ' ':
			if depth == 0 {
				if i > start {
					out = append(out, s[start:i])
				}
				start = i + 1
			}
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

func parseBreadth(s string) Breadth {
	s = strings.TrimSpace(s)
	switch {
	case s == "auto":
		return BAuto()
	case s == "min-content":
		return BMinContent()
	case s == "max-content":
		return BMaxContent()
	case strings.HasSuffix(s, "fr"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "fr"), 32)
		return BFr(float32(v))
	case strings.HasSuffix(s, "%"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "%"), 32)
		return BPct(float32(v))
	case strings.HasSuffix(s, "px"):
		v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 32)
		return BPx(float32(v))
	}
	v, _ := strconv.ParseFloat(s, 32)
	return BPx(float32(v))
}

func parseTrackSize(s string) TrackSize {
	switch {
	case strings.HasPrefix(s, "minmax("):
		args := strings.SplitN(strings.TrimSuffix(strings.TrimPrefix(s, "minmax("), ")"), ",", 2)
		return Minmax(parseBreadth(args[0]), parseBreadth(args[1]))
	case strings.HasPrefix(s, "fit-content("):
		arg := strings.TrimSuffix(strings.TrimPrefix(s, "fit-content("), ")")
		b := parseBreadth(arg)
		if b.Kind == BreadthPercent {
			return FitContentPct(b.V)
		}
		return FitContent(b.V)
	}
	b := parseBreadth(s)
	if b.Kind == BreadthFr {
		return Fr(b.V)
	}
	return TrackSize{b, b}
}

func parseTracks(s string) []GridTrack {
	var out []GridTrack
	for _, tok := range splitTop(s) {
		if strings.HasPrefix(tok, "[") || tok == "none" {
			continue
		}
		if strings.HasPrefix(tok, "repeat(") {
			inner := strings.TrimSuffix(strings.TrimPrefix(tok, "repeat("), ")")
			i := strings.Index(inner, ",")
			countS, rest := strings.TrimSpace(inner[:i]), strings.TrimSpace(inner[i+1:])
			count := 0
			switch countS {
			case "auto-fill":
				count = RepeatAutoFill
			case "auto-fit":
				count = RepeatAutoFit
			default:
				count, _ = strconv.Atoi(countS)
			}
			var sizes []TrackSize
			for _, t := range splitTop(rest) {
				if !strings.HasPrefix(t, "[") {
					sizes = append(sizes, parseTrackSize(t))
				}
			}
			out = append(out, Repeat(count, sizes...))
			continue
		}
		out = append(out, T(parseTrackSize(tok)))
	}
	return out
}

func parseAutoTracks(s string) []TrackSize {
	var out []TrackSize
	for _, t := range splitTop(s) {
		out = append(out, parseTrackSize(t))
	}
	return out
}

func parseLine(s string) GridLine {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "span ") {
		n, err := strconv.Atoi(strings.TrimPrefix(s, "span "))
		if err != nil {
			return GridLine{}
		}
		return Span(n)
	}
	if n, err := strconv.Atoi(s); err == nil {
		return Line(n)
	}
	return GridLine{}
}

func styleOf(x *xNode) Style {
	s := DefaultStyle
	get := func(k string) string { v, _ := x.attr(k); return v }
	switch get("display") {
	case "block", "flow-root":
		s.Display = DisplayBlock
	case "grid":
		s.Display = DisplayGrid
	case "none":
		s.Display = DisplayNone
	}
	if get("position") == "absolute" {
		s.Position = Absolute
	}
	if get("box-sizing") == "content-box" {
		s.BoxSizing = ContentBox
	}
	s.OverflowX, s.OverflowY = parseOverflow(get("overflow-x")), parseOverflow(get("overflow-y"))
	if v, err := strconv.ParseFloat(get("scrollbar-width"), 32); err == nil {
		s.ScrollbarWidth = float32(v)
	}
	s.Width, s.Height = parseLength(get("width")), parseLength(get("height"))
	s.MinWidth, s.MinHeight = parseLength(get("min-width")), parseLength(get("min-height"))
	s.MaxWidth, s.MaxHeight = parseLength(get("max-width")), parseLength(get("max-height"))
	if v := get("aspect-ratio"); v != "" {
		if a, b, ok := strings.Cut(v, "/"); ok {
			x, _ := strconv.ParseFloat(strings.TrimSpace(a), 32)
			y, _ := strconv.ParseFloat(strings.TrimSpace(b), 32)
			s.AspectRatio = float32(x / y)
		} else {
			r, _ := strconv.ParseFloat(v, 32)
			s.AspectRatio = float32(r)
		}
	}
	edge := func(prefix string, zeroDefault bool) Rect[Length] {
		r := Rect[Length]{}
		def := Auto
		if zeroDefault {
			def = Pt(0)
		}
		for _, side := range []struct {
			name string
			dst  *Length
		}{{"top", &r.Top}, {"right", &r.Right}, {"bottom", &r.Bottom}, {"left", &r.Left}} {
			*side.dst = def
			if v, ok := x.attr(prefix + side.name); ok {
				*side.dst = parseLength(v)
			}
		}
		return r
	}
	s.Margin = edge("margin-", true)
	s.Padding = edge("padding-", true)
	s.Border = edge("border-", true)
	s.Inset = edge("", false)
	s.ColumnGap, s.RowGap = parseLength(get("column-gap")), parseLength(get("row-gap"))
	s.AlignItems, s.AlignSelf = parseAlign(get("align-items")), parseAlign(get("align-self"))
	s.JustifyItems, s.JustifySelf = parseAlign(get("justify-items")), parseAlign(get("justify-self"))
	s.AlignContent, s.JustifyContent = parseContent(get("align-content")), parseContent(get("justify-content"))
	switch get("flex-direction") {
	case "column":
		s.FlexDirection = Column
	case "row-reverse":
		s.FlexDirection = RowReverse
	case "column-reverse":
		s.FlexDirection = ColumnReverse
	}
	switch get("flex-wrap") {
	case "wrap":
		s.FlexWrap = Wrap
	case "wrap-reverse":
		s.FlexWrap = WrapReverse
	}
	if v, err := strconv.ParseFloat(get("flex-grow"), 32); err == nil {
		s.FlexGrow = float32(v)
	}
	if v, err := strconv.ParseFloat(get("flex-shrink"), 32); err == nil {
		s.FlexShrink = float32(v)
	}
	s.FlexBasis = parseLength(get("flex-basis"))
	s.GridTemplateColumns = parseTracks(get("grid-template-columns"))
	s.GridTemplateRows = parseTracks(get("grid-template-rows"))
	if v := get("grid-auto-columns"); v != "" {
		s.GridAutoColumns = parseAutoTracks(v)
	}
	if v := get("grid-auto-rows"); v != "" {
		s.GridAutoRows = parseAutoTracks(v)
	}
	switch get("grid-auto-flow") {
	case "column":
		s.GridAutoFlow = FlowColumn
	case "row dense", "dense":
		s.GridAutoFlow = FlowRowDense
	case "column dense":
		s.GridAutoFlow = FlowColumnDense
	}
	s.GridColumn = Placement{parseLine(get("grid-column-start")), parseLine(get("grid-column-end"))}
	s.GridRow = Placement{parseLine(get("grid-row-start")), parseLine(get("grid-row-end"))}
	return s
}

// ahem measures text in the Ahem font: 10×10 glyphs, breaking only at
// zero-width spaces.
func ahem(text string) MeasureFunc {
	words := strings.Split(text, "\u200b")
	minW, maxW := 0, 0
	for _, w := range words {
		n := len([]rune(w))
		minW = max(minW, n)
		maxW += n
	}
	return func(c Constraints) Size {
		inline := c.Width
		if isNaN(inline) {
			switch {
			case c.AvailW == MinContentSpace:
				inline = float32(minW * 10)
			case c.AvailW == MaxContentSpace:
				inline = float32(maxW * 10)
			default:
				inline = minf(c.AvailW, float32(maxW*10))
			}
		}
		inline = maxf(inline, float32(minW*10))
		block := c.Height
		if isNaN(block) {
			perLine := int(math.Floor(float64(inline / 10)))
			lines, cur := 1, 0
			for _, w := range words {
				n := len([]rune(w))
				if cur+n > perLine {
					if cur > 0 {
						lines++
					}
					cur = n
				} else {
					cur += n
				}
			}
			block = float32(lines * 10)
		}
		return Size{inline, block}
	}
}

func build(x *xNode) *Node {
	n := New()
	n.Style = styleOf(x)
	var kids []*Node
	for i := range x.Kids {
		kids = append(kids, build(&x.Kids[i]))
	}
	if len(kids) > 0 {
		n.SetChildren(kids)
	} else if t := strings.TrimSpace(x.Text); t != "" {
		n.SetMeasure(ahem(t))
		st := &n.Style
		n.SetBaseline(func(Size) float32 {
			return st.Padding.Top.resolve0(0) + st.Border.Top.resolve0(0) + 8
		})
	}
	return n
}

func compare(n *Node, x *xNode, path string, diffs *[]string) {
	f := func(k string) float32 {
		v, _ := x.attr(k)
		r, _ := strconv.ParseFloat(v, 32)
		return float32(r)
	}
	want := [4]float32{f("x"), f("y"), f("width"), f("height")}
	got := [4]float32{n.X, n.Y, n.W, n.H}
	for i := range want {
		if abs(want[i]-got[i]) >= 0.1 {
			*diffs = append(*diffs, fmt.Sprintf("%s: got %v, want %v", path, got, want))
			break
		}
	}
	for i := range x.Kids {
		if i < len(n.children) {
			compare(n.children[i], &x.Kids[i], fmt.Sprintf("%s/%d", path, i), diffs)
		}
	}
}

func space(s string) float32 {
	switch s {
	case "", "max-content":
		return MaxContentSpace
	case "min-content":
		return MinContentSpace
	}
	v, _ := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 32)
	return float32(v)
}

func runFixture(path string) (name string, diffs []string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	var t xTest
	if err := xml.Unmarshal(data, &t); err != nil {
		return "", nil, err
	}
	name = filepath.Base(filepath.Dir(path)) + "/" + strings.TrimSuffix(filepath.Base(path), ".xml")
	if len(t.Input.Kids) != 1 || len(t.Expect.Kids) != 1 {
		return name, nil, fmt.Errorf("%s: want one root", path)
	}
	root := build(&t.Input.Kids[0])
	var p Pass
	func() {
		defer func() {
			if r := recover(); r != nil {
				diffs = append(diffs, fmt.Sprintf("panic: %v", r))
			}
		}()
		root.Layout(Size{space(t.Viewport.Width), space(t.Viewport.Height)}, &p)
		if t.Rounding != "false" {
			Round(root, 1)
		}
		compare(root, &t.Expect.Kids[0], "root", &diffs)
	}()
	return name, diffs, nil
}

func TestCSSFixtures(t *testing.T) {
	files, _ := filepath.Glob("testdata/css/*/*.xml")
	if len(files) == 0 {
		t.Fatal("no fixtures")
	}
	known := map[string]bool{}
	if f, err := os.Open("testdata/css/known_failures.txt"); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			if l := strings.TrimSpace(sc.Text()); l != "" && !strings.HasPrefix(l, "#") {
				known[l] = true
			}
		}
		f.Close()
	}
	var failing []string
	groups := map[string][2]int{}
	for _, path := range files {
		name, diffs, err := runFixture(path)
		if err != nil {
			t.Fatal(err)
		}
		g := strings.Split(name, "/")[0]
		c := groups[g]
		c[1]++
		if len(diffs) == 0 {
			c[0]++
			if known[name] && !*updateKnown {
				t.Logf("now passes (remove from known_failures.txt): %s", name)
			}
		} else {
			failing = append(failing, name)
			if !known[name] && !*updateKnown {
				t.Errorf("%s:\n  %s", name, strings.Join(diffs[:min(len(diffs), 4)], "\n  "))
			} else if *verbose {
				t.Logf("%s:\n  %s", name, strings.Join(diffs[:min(len(diffs), 4)], "\n  "))
			}
		}
		groups[g] = c
	}
	var gs []string
	pass, total := 0, 0
	for g, c := range groups {
		gs = append(gs, fmt.Sprintf("%s %d/%d", g, c[0], c[1]))
		pass += c[0]
		total += c[1]
	}
	sort.Strings(gs)
	t.Logf("CSS fixtures: %d/%d pass (%s)", pass, total, strings.Join(gs, ", "))
	if *updateKnown {
		sort.Strings(failing)
		body := "# Fixtures nui's layout doesn't match yet. go test ./layout -run CSS -update-known rewrites this.\n" + strings.Join(failing, "\n") + "\n"
		if err := os.WriteFile("testdata/css/known_failures.txt", []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestCSSFixture runs one fixture verbosely: go test ./layout -run CSSFixture -fixture flex/foo
var one = flag.String("fixture", "", "fixture to run, e.g. flex/align_items_center")

func TestCSSFixture(t *testing.T) {
	if *one == "" {
		t.Skip("set -fixture")
	}
	_, diffs, err := runFixture(filepath.Join("testdata/css", *one+".xml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range diffs {
		t.Error(d)
	}
}
