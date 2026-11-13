//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"
	"unicode"
	"unicode/utf16"
	"unsafe"

	. "github.com/egoist/ryui/internal/darwin"
)

// ryui/scan's "file" and ryui/vision's "analyze" on a Mac, with Vision as
// on iOS.
//
// scan.file: the kinds, comma-separated, a newline, the path; answers a
// u32 count, then each code's kind and value (u32-length strings).
//
// vision.analyze: JSON (path, text, faces, codes, languages); answers
// JSON: the image's size upright, its lines of text with their words,
// its faces and codes, each with a box in fractions of the image from its
// top left. Off the main thread.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["scan"] = map[string]method{"file": h.scanFile}
		m["vision"] = map[string]method{"analyze": h.visionAnalyze}
	})
}

// scanSymbologies are ryui/scan's kinds and Vision's symbologies.
var scanSymbologies = []struct {
	kind  string
	names []string
}{
	{"qr", []string{"VNBarcodeSymbologyQR"}}, {"aztec", []string{"VNBarcodeSymbologyAztec"}},
	{"datamatrix", []string{"VNBarcodeSymbologyDataMatrix"}}, {"pdf417", []string{"VNBarcodeSymbologyPDF417"}},
	{"ean13", []string{"VNBarcodeSymbologyEAN13"}}, {"ean8", []string{"VNBarcodeSymbologyEAN8"}},
	{"upca", []string{"VNBarcodeSymbologyEAN13"}}, {"upce", []string{"VNBarcodeSymbologyUPCE"}},
	{"code128", []string{"VNBarcodeSymbologyCode128"}}, {"code39", []string{"VNBarcodeSymbologyCode39"}},
	{"code93", []string{"VNBarcodeSymbologyCode93"}}, {"codabar", []string{"VNBarcodeSymbologyCodabar"}},
	{"itf", []string{"VNBarcodeSymbologyITF14", "VNBarcodeSymbologyI2of5"}},
}

var visLoad = sync.OnceValue(func() uintptr { return framework("Vision") })

// scanSymbology is a Vision symbology's NSString (0 if this macOS lacks
// it: Codabar is macOS 12's).
func scanSymbology(name string) ID { return Const(visLoad(), name) }

// scanKind is ryui/scan's name of a symbology.
func scanKind(sym string) string {
	for _, s := range scanSymbologies {
		if s.kind == "upca" {
			continue
		}
		for _, n := range s.names {
			if c := scanSymbology(n); c != 0 && GoString(c) == sym {
				return s.kind
			}
		}
	}
	return ""
}

// scanKinds is what ryui/scan asks for ("": every kind).
type scanKinds map[string]bool

func parseScanKinds(s string) scanKinds {
	k := scanKinds{}
	for _, x := range strings.Split(s, ",") {
		if x != "" {
			k[x] = true
		}
	}
	return k
}

func (k scanKinds) wants(kind string) bool { return len(k) == 0 || k[kind] }

// named is the code as ryui/scan names it: Apple reads a UPC-A as an
// EAN-13 with a leading 0, a UPC-A again if the app asked for those.
func (k scanKinds) named(kind, value string) (string, string) {
	if kind == "ean13" && len(value) == 13 && strings.HasPrefix(value, "0") && k.wants("upca") {
		return "upca", value[1:]
	}
	return kind, value
}

// encodeCodes is the reply ryui/scan's decode reads.
func encodeCodes(codes [][2]string) []byte {
	le := binary.LittleEndian
	b := le.AppendUint32(nil, uint32(len(codes)))
	for _, c := range codes {
		for _, s := range c {
			b = le.AppendUint32(b, uint32(len(s)))
			b = append(b, s...)
		}
	}
	return b
}

// visPerform runs a request on a handler, failing with its error.
func visPerform(handler, req ID) error {
	var nserr ID
	ok := SendBool(handler, "performRequests:error:", uintptr(Array(req)), uintptr(unsafe.Pointer(&nserr)))
	if !ok {
		return errorText(nserr, "Vision failed")
	}
	return nil
}

func (h *Host) scanFile(args []byte, reply func([]byte, error)) {
	kindsArg, path, ok := strings.Cut(string(args), "\n")
	if !ok {
		reply(nil, errors.New("bad arguments"))
		return
	}
	kinds := parseScanKinds(kindsArg)
	h.offMain(func() ([]byte, error) {
		if visLoad() == 0 {
			return nil, errors.New("not available")
		}
		var wanted []ID
		for _, s := range scanSymbologies {
			if !kinds.wants(s.kind) {
				continue
			}
			for _, n := range s.names {
				if c := scanSymbology(n); c != 0 && !slices.Contains(wanted, c) {
					wanted = append(wanted, c)
				}
			}
		}
		req := Autorelease(New("VNDetectBarcodesRequest"))
		Send(req, "setSymbologies:", uintptr(Array(wanted...)))
		handler := Autorelease(Send(Send(Class("VNImageRequestHandler"), "alloc"), "initWithURL:options:", uintptr(FileURL(path)), uintptr(Dict())))
		if err := visPerform(handler, req); err != nil {
			return nil, errors.New("can't read the image: " + err.Error())
		}
		var codes [][2]string
		for _, o := range ArrayItems(Send(req, "results")) {
			v := Send(o, "payloadStringValue")
			k := scanKind(GoString(Send(o, "symbology")))
			if v == 0 || k == "" {
				continue
			}
			kind, value := kinds.named(k, GoString(v))
			c := [2]string{kind, value}
			if kinds.wants(kind) && !slices.Contains(codes, c) {
				codes = append(codes, c)
			}
		}
		return encodeCodes(codes), nil
	}, reply)
}

// Vision.

type visBox struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type visWord struct {
	Text  string `json:"text"`
	Box   visBox `json:"box"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

type visLine struct {
	Text  string    `json:"text"`
	Box   visBox    `json:"box"`
	Words []visWord `json:"words"`
}

type visCode struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Box   visBox `json:"box"`
}

type visResult struct {
	Width  int       `json:"width"`
	Height int       `json:"height"`
	Lines  []visLine `json:"lines"`
	Faces  []visBox  `json:"faces"`
	Codes  []visCode `json:"codes"`
}

func (h *Host) visionAnalyze(args []byte, reply func([]byte, error)) {
	o := struct {
		Path      *string  `json:"path"`
		Text      *bool    `json:"text"`
		Faces     *bool    `json:"faces"`
		Codes     *bool    `json:"codes"`
		Languages []string `json:"languages"`
	}{}
	if json.Unmarshal(args, &o) != nil || o.Path == nil {
		reply(nil, errors.New("bad args"))
		return
	}
	yes := func(b *bool) bool { return b == nil || *b }
	text, faces, codes := yes(o.Text), yes(o.Faces), yes(o.Codes)
	path, languages := *o.Path, o.Languages
	h.offMain(func() ([]byte, error) {
		if visLoad() == 0 || ie().err != nil {
			return nil, errors.New("not available")
		}
		img, err := visUpright(path)
		if err != nil {
			return nil, err
		}
		defer CFRelease(img)
		w, ht := ieSize(img)
		out := visResult{Width: w, Height: ht, Lines: []visLine{}, Faces: []visBox{}, Codes: []visCode{}}
		// One handler per kind: one that fails doesn't lose the others.
		if text {
			if out.Lines, err = visReadText(img, languages); err != nil {
				return nil, err
			}
		}
		if faces {
			if out.Faces, err = visFindFaces(img); err != nil {
				return nil, err
			}
		}
		if codes {
			if out.Codes, err = visFindCodes(img); err != nil {
				return nil, err
			}
		}
		return json.Marshal(out)
	}, reply)
}

// visUpright is the image upright, whatever its orientation says (+1):
// the boxes are in the same space as ryui/imageedit's pixels.
func visUpright(path string) (uintptr, error) {
	u := FileURL(path)
	if strings.HasPrefix(path, "file://") {
		u = URL(path)
	}
	if u == 0 {
		return 0, errNoImage
	}
	a := ie()
	src := a.sourceWithURL(uintptr(u), 0)
	if src == 0 || a.sourceCount(src) == 0 {
		CFRelease(src)
		return 0, errNoImage
	}
	defer CFRelease(src)
	w, h := ieUprightSize(src)
	img := ieThumbnail(src, max(w, h, 1))
	if img == 0 {
		return 0, errNoImage
	}
	return img, nil
}

func visHandler(img uintptr) ID {
	return Autorelease(Send(Send(Class("VNImageRequestHandler"), "alloc"), "initWithCGImage:options:", img, uintptr(Dict())))
}

// visToBox is Vision's rectangle, y up, as fractions from the top left.
func visToBox(r Rect) visBox {
	return visBox{X: r.Origin.X, Y: 1 - (r.Origin.Y + r.Size.Height), W: r.Size.Width, H: r.Size.Height}
}

// visQuadRect is a rectangle observation's quadrilateral's bounds.
func visQuadRect(q ID) Rect {
	ps := []Point{MsgPoint(q, Sel("topLeft")), MsgPoint(q, Sel("topRight")), MsgPoint(q, Sel("bottomLeft")), MsgPoint(q, Sel("bottomRight"))}
	x0, y0, x1, y1 := ps[0].X, ps[0].Y, ps[0].X, ps[0].Y
	for _, p := range ps[1:] {
		x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
	}
	return ieRect(x0, y0, x1-x0, y1-y0)
}

// visRangeBox is a recognized text's range's box (UTF-16 offsets), if
// Vision has one.
func visRangeBox(c ID, loc, n int) (Rect, bool) {
	q := Send(c, "boundingBoxForRange:error:", uintptr(loc), uintptr(n), 0)
	if q == 0 {
		return Rect{}, false
	}
	return visQuadRect(q), true
}

// visLanguages is the recognition languages to ask for: the app's, or the
// user's, as Vision names them.
func visLanguages(req ID, languages []string) []string {
	var supported []string
	if RespondsTo(req, "supportedRecognitionLanguagesAndReturnError:") {
		for _, s := range ArrayItems(Send(req, "supportedRecognitionLanguagesAndReturnError:", 0)) {
			supported = append(supported, GoString(s))
		}
	}
	wanted := languages
	if len(wanted) == 0 {
		for _, l := range ArrayItems(Send(Class("NSLocale"), "preferredLanguages")) {
			wanted = append(wanted, GoString(l))
		}
		wanted = append(wanted, "en-US")
	}
	var langs []string
	for _, l := range wanted {
		m := ""
		switch {
		case slices.Contains(supported, l):
			m = l
		case strings.HasPrefix(l, "zh"):
			m = "zh-Hans"
			if strings.Contains(l, "Hant") || strings.HasSuffix(l, "TW") || strings.HasSuffix(l, "HK") {
				m = "zh-Hant"
			}
		default:
			base, _, _ := strings.Cut(l, "-")
			for _, s := range supported {
				if s == base || strings.HasPrefix(s, base+"-") {
					m = s
					break
				}
			}
		}
		if m != "" && slices.Contains(supported, m) && !slices.Contains(langs, m) {
			langs = append(langs, m)
		}
	}
	return langs
}

func visReadText(img uintptr, languages []string) ([]visLine, error) {
	req := Autorelease(New("VNRecognizeTextRequest"))
	Send(req, "setRecognitionLevel:", 0) // accurate
	// Correction turns numbers and names into likelier words: here the
	// text as it's written matters.
	Send(req, "setUsesLanguageCorrection:", 0)
	if langs := visLanguages(req, languages); len(langs) > 0 {
		ids := make([]ID, len(langs))
		for i, l := range langs {
			ids[i] = String(l)
		}
		Send(req, "setRecognitionLanguages:", uintptr(Array(ids...)))
	}
	if err := visPerform(visHandler(img), req); err != nil {
		return nil, err
	}
	lines := []visLine{}
	for _, ob := range ArrayItems(Send(req, "results")) {
		cs := ArrayItems(Send(ob, "topCandidates:", 1))
		if len(cs) == 0 {
			continue
		}
		c := cs[0]
		str := Send(c, "string")
		t := GoString(str)
		line, ok := visRangeBox(c, 0, SendInt(str, "length"))
		if !ok {
			line = MsgRect(ob, Sel("boundingBox"))
		}
		lines = append(lines, visLine{Text: t, Box: visToBox(line), Words: visWords(c, str, t, line)})
	}
	return lines, nil
}

// visText is a string's characters, with the UTF-16 and scalar offsets
// they start at.
type visText struct {
	runes []rune
	u16   []int // each rune's UTF-16 offset, then the end's
}

func newVisText(s string) visText {
	t := visText{runes: []rune(s)}
	t.u16 = make([]int, len(t.runes)+1)
	n := 0
	for i, r := range t.runes {
		t.u16[i] = n
		n += utf16.RuneLen(r)
	}
	t.u16[len(t.runes)] = n
	return t
}

// scalar is the rune index at a UTF-16 offset.
func (t visText) scalar(u int) int {
	i, _ := slices.BinarySearch(t.u16, u)
	return i
}

// extends reports whether r belongs to the character before it
// (combining marks, variation selectors, emoji modifiers, after a joiner).
func visExtends(prev, r rune) bool {
	return unicode.Is(unicode.M, r) || r == 0x200D || prev == 0x200D || (r >= 0x1F3FB && r <= 0x1F3FF)
}

// visWords is the line's words, and its marks of punctuation each as a
// word of its own: what the platform's word breaking finds, boxed.
func visWords(c, str ID, s string, line Rect) []visWord {
	t := newVisText(s)
	type span struct{ a, b int } // runes
	var spans []span
	at := 0
	marks := func(end int) {
		for i := at; i < end; {
			j := i + 1
			for j < end && visExtends(t.runes[j-1], t.runes[j]) {
				j++
			}
			if !unicode.IsSpace(t.runes[i]) {
				spans = append(spans, span{i, j})
			}
			i = j
		}
	}
	if framework("NaturalLanguage") != 0 && HasClass("NLTokenizer") {
		tok := Autorelease(Send(Send(Class("NLTokenizer"), "alloc"), "initWithUnit:", 0)) // word
		Send(tok, "setString:", uintptr(str))
		for _, v := range ArrayItems(Send(tok, "tokensForRange:", 0, uintptr(SendInt(str, "length")))) {
			r := MsgRange(v, Sel("rangeValue"))
			a, b := t.scalar(int(r.Location)), t.scalar(int(r.Location+r.Length))
			if a < at || b <= a {
				continue
			}
			marks(a)
			spans = append(spans, span{a, b})
			at = b
		}
	}
	marks(len(t.runes))

	boxes := make([]*Rect, len(spans))
	texts := make([]string, len(spans))
	for i, sp := range spans {
		texts[i] = string(t.runes[sp.a:sp.b])
		if r, ok := visRangeBox(c, t.u16[sp.a], t.u16[sp.b]-t.u16[sp.a]); ok {
			boxes[i] = &r
		}
	}
	visSpread(boxes, texts, line)
	out := []visWord{}
	for i, sp := range spans {
		b := line
		if boxes[i] != nil {
			b = *boxes[i]
		}
		out = append(out, visWord{Text: texts[i], Box: visToBox(b), Start: sp.a, End: sp.b})
	}
	return out
}

// visSpread: Vision's accurate level boxes what it read as one group:
// several words of a Chinese line get the same box, as can a word it has
// no box for. Those share the group's box by how wide their characters
// are, wide (CJK) as two narrow ones.
func visSpread(boxes []*Rect, texts []string, line Rect) {
	weight := func(s string) float64 {
		w := 0.0
		for _, r := range s {
			if r >= 0x2E80 {
				w += 2
			} else {
				w++
			}
		}
		return w
	}
	for i := 0; i < len(boxes); {
		j := i + 1
		b := boxes[i]
		for j < len(boxes) && visSame(boxes[j], b) {
			j++
		}
		if j-i > 1 || b == nil {
			group := line
			if b != nil {
				group = *b
			}
			total := 0.0
			for _, s := range texts[i:j] {
				total += weight(s)
			}
			total = max(total, 1)
			x := group.Origin.X
			for k := i; k < j; k++ {
				w := group.Size.Width * weight(texts[k]) / total
				nb := ieRect(x, group.Origin.Y, w, group.Size.Height)
				boxes[k] = &nb
				x += w
			}
		}
		i = j
	}
}

func visSame(a, b *Rect) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	const e = 0.0005
	return math.Abs(a.Origin.X-b.Origin.X) < e && math.Abs(a.Origin.Y-b.Origin.Y) < e &&
		math.Abs(a.Size.Width-b.Size.Width) < e && math.Abs(a.Size.Height-b.Size.Height) < e
}

func visFindFaces(img uintptr) ([]visBox, error) {
	req := Autorelease(New("VNDetectFaceRectanglesRequest"))
	if err := visPerform(visHandler(img), req); err != nil {
		return nil, err
	}
	out := []visBox{}
	for _, o := range ArrayItems(Send(req, "results")) {
		out = append(out, visToBox(MsgRect(o, Sel("boundingBox"))))
	}
	return out, nil
}

func visFindCodes(img uintptr) ([]visCode, error) {
	req := Autorelease(New("VNDetectBarcodesRequest"))
	if err := visPerform(visHandler(img), req); err != nil {
		return nil, err
	}
	out := []visCode{}
	var seen []Rect
	for _, o := range ArrayItems(Send(req, "results")) {
		k := scanKind(GoString(Send(o, "symbology")))
		if k == "" {
			k = "other"
		}
		r := visBarsExtent(img, MsgRect(o, Sel("boundingBox")))
		// The same code found twice (several scan lines over a barcode).
		dup := false
		for _, s := range seen {
			if i, ok := visIntersection(s, r); ok && i.Size.Width*i.Size.Height > 0.5*min(s.Size.Width*s.Size.Height, r.Size.Width*r.Size.Height) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		seen = append(seen, r)
		out = append(out, visCode{Kind: k, Value: GoString(Send(o, "payloadStringValue")), Box: visToBox(r)})
	}
	return out, nil
}

func visIntersection(a, b Rect) (Rect, bool) {
	x0, y0 := max(a.Origin.X, b.Origin.X), max(a.Origin.Y, b.Origin.Y)
	x1, y1 := min(a.Origin.X+a.Size.Width, b.Origin.X+b.Size.Width), min(a.Origin.Y+a.Size.Height, b.Origin.Y+b.Size.Height)
	if x1 <= x0 || y1 <= y0 {
		return Rect{}, false
	}
	return ieRect(x0, y0, x1-x0, y1-y0), true
}

// visBarsExtent: a barcode's box from Vision can be the line it read
// along, not the bars. The bars are what's above and below that line that
// looks like it: rows that match, from the line outward. Sideways
// barcodes and boxes with a height already are left as they are.
func visBarsExtent(img uintptr, r Rect) Rect {
	if r.Size.Width <= 0 || r.Size.Height >= r.Size.Width*0.25 {
		return r
	}
	a := ie()
	W, H := ieSize(img)
	gray := a.spaceGray()
	c := a.bitmapCreate(0, uintptr(W), uintptr(H), 8, uintptr(W), gray, ieAlphaNone)
	CFRelease(gray)
	if c == 0 {
		return r
	}
	defer CFRelease(c)
	a.drawImage(c, ieRect(0, 0, float64(W), float64(H)), img)
	data, row := iePixels(c, H)
	if data == nil {
		return r
	}
	// Top row first in memory; Vision's y is up.
	x0, x1 := max(int(r.Origin.X*float64(W)), 0), min(int((r.Origin.X+r.Size.Width)*float64(W)), W)
	mid := min(max(int((1-(r.Origin.Y+r.Size.Height/2))*float64(H)), 0), H-1)
	if x1-x0 <= 8 {
		return r
	}
	differs := func(y int) float64 {
		diff := 0
		am, b := data[mid*row:], data[y*row:]
		for x := x0; x < x1; x++ {
			if d := int(am[x]) - int(b[x]); d > 64 || d < -64 {
				diff++
			}
		}
		return float64(diff) / float64(x1-x0)
	}
	top, bottom := mid, mid
	for top > 0 && differs(top-1) < 0.2 {
		top--
	}
	for bottom < H-1 && differs(bottom+1) < 0.2 {
		bottom++
	}
	return ieRect(r.Origin.X, 1-float64(bottom+1)/float64(H), r.Size.Width, float64(bottom+1-top)/float64(H))
}
