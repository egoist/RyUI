//go:build linux && !android

package linux

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/scan's "file" and asuka/vision's "analyze" on Linux: ZBar reads
// codes (QR, EAN, UPC, Code 128, 39 and 93, Codabar, interleaved 2 of 5,
// PDF417: it reads no Aztec or Data Matrix codes) and Tesseract reads
// text, where they're installed (libzbar0; libtesseract5 and its
// languages' data). No library on a Linux desktop finds faces: none are
// found.
//
// scan.file: the kinds, comma-separated, a newline, the path; answers a
// u32 count, then each code's kind and value (u32-length strings), or
// fails "not available" without ZBar.
//
// vision.analyze: JSON (path, text, faces, codes, languages); answers
// JSON: the image's size upright, its lines of text with their words,
// its faces and codes, each with a box in fractions of the image from
// its top left (what's not installed is left empty). Off the main
// thread.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["scan"] = map[string]method{"file": h.scanFile}
		m["vision"] = map[string]method{"analyze": h.visionAnalyze}
	})
}

// encodeCodes is the reply asuka/scan's decode reads.
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

// grayOf is the image's luma, a byte a pixel (on white where it's
// transparent).
func grayOf(img *image.RGBA) []byte {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	g := make([]byte, w*h)
	for y := range h {
		row := img.Pix[y*img.Stride:]
		for x := range w {
			p := row[x*4:]
			white := 255 - uint32(p[3])
			r, gg, b := uint32(p[0])+white, uint32(p[1])+white, uint32(p[2])+white
			g[y*w+x] = byte((299*r + 587*gg + 114*b) / 1000)
		}
	}
	return g
}

func (h *Host) scanFile(args []byte, reply func([]byte, error)) {
	kindsArg, path, ok := strings.Cut(string(args), "\n")
	if !ok {
		reply(nil, errors.New("bad arguments"))
		return
	}
	kinds := newCameraKinds(kindsArg)
	assets := h.assets
	go func() {
		b, err := func() ([]byte, error) {
			if err := LoadZBar(); err != nil {
				return nil, errors.New("not available: " + err.Error())
			}
			img, err := ieDecode(path, assets)
			if err != nil {
				return nil, err
			}
			var codes [][2]string
			seen := map[[2]string]bool{}
			for _, s := range ZBarScan(grayOf(img), img.Rect.Dx(), img.Rect.Dy()) {
				kind := zbarKind(s.Type)
				if kind == "" {
					continue
				}
				kind, value := kinds.named(kind, s.Data)
				c := [2]string{kind, value}
				if !kinds.wants(kind) || seen[c] {
					continue
				}
				seen[c] = true
				codes = append(codes, c)
			}
			return encodeCodes(codes), nil
		}()
		h.Post(func() { reply(b, err) })
	}()
}

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
	var req struct {
		Path      string   `json:"path"`
		Text      bool     `json:"text"`
		Faces     bool     `json:"faces"`
		Codes     bool     `json:"codes"`
		Languages []string `json:"languages"`
	}
	if json.Unmarshal(args, &req) != nil || req.Path == "" {
		reply(nil, errors.New("bad args"))
		return
	}
	assets, locale := h.assets, h.locale
	go func() {
		b, err := func() ([]byte, error) {
			img, err := ieDecode(req.Path, assets)
			if err != nil {
				return nil, err
			}
			w, ht := img.Rect.Dx(), img.Rect.Dy()
			res := visResult{Width: w, Height: ht, Lines: []visLine{}, Faces: []visBox{}, Codes: []visCode{}}
			box := func(x0, y0, x1, y1 int) visBox {
				return visBox{X: float64(x0) / float64(w), Y: float64(y0) / float64(ht), W: float64(x1-x0) / float64(w), H: float64(y1-y0) / float64(ht)}
			}
			gray := grayOf(img)
			if req.Codes && LoadZBar() == nil {
				for _, s := range ZBarScan(gray, w, ht) {
					if kind := zbarKind(s.Type); kind != "" {
						res.Codes = append(res.Codes, visCode{Kind: kind, Value: s.Data, Box: box(s.X0, s.Y0, s.X1, s.Y1)})
					}
				}
			}
			if req.Text && LoadTesseract() == nil {
				langs := tessLanguages(req.Languages, locale)
				words, err := TessRead(gray, w, ht, langs)
				if err != nil && langs != "eng" {
					words, _ = TessRead(gray, w, ht, "eng")
				}
				res.Lines = visLines(words, box)
			}
			return json.Marshal(res)
		}()
		h.Post(func() { reply(b, err) })
	}()
}

// visLines groups Tesseract's words in their lines, each word's place
// in its line's text in runes.
func visLines(words []TessWord, box func(x0, y0, x1, y1 int) visBox) []visLine {
	lines := []visLine{}
	at := 0 // bytes into the line's text searched
	for _, wd := range words {
		if wd.NewLine {
			text := strings.TrimRight(wd.Line, " \n")
			if strings.TrimSpace(text) == "" {
				continue
			}
			lines = append(lines, visLine{Text: text, Box: box(wd.LineBox[0], wd.LineBox[1], wd.LineBox[2], wd.LineBox[3]), Words: []visWord{}})
			at = 0
		}
		if len(lines) == 0 || wd.Text == "" {
			continue
		}
		l := &lines[len(lines)-1]
		i := strings.Index(l.Text[at:], wd.Text)
		if i < 0 {
			continue
		}
		start := utf8.RuneCountInString(l.Text[:at+i])
		end := start + utf8.RuneCountInString(wd.Text)
		at += i + len(wd.Text)
		l.Words = append(l.Words, visWord{Text: wd.Text, Box: box(wd.Box[0], wd.Box[1], wd.Box[2], wd.Box[3]), Start: start, End: end})
	}
	return lines
}

// tessCodes are Tesseract's names of languages, by BCP 47's.
var tessCodes = map[string]string{
	"en": "eng", "de": "deu", "fr": "fra", "es": "spa", "it": "ita", "pt": "por", "nl": "nld",
	"sv": "swe", "da": "dan", "nb": "nor", "no": "nor", "fi": "fin", "pl": "pol", "cs": "ces",
	"sk": "slk", "hu": "hun", "ro": "ron", "tr": "tur", "el": "ell", "ru": "rus", "uk": "ukr",
	"bg": "bul", "ar": "ara", "he": "heb", "fa": "fas", "hi": "hin", "th": "tha", "vi": "vie",
	"id": "ind", "ms": "msa", "ja": "jpn", "ko": "kor", "zh-Hans": "chi_sim", "zh-Hant": "chi_tra",
	"zh": "chi_sim", "zh-TW": "chi_tra", "zh-HK": "chi_tra", "ca": "cat", "hr": "hrv", "sr": "srp",
}

// tessDataDirs are where Tesseract's languages' data may be.
func tessDataDirs() []string {
	dirs := []string{}
	if d := os.Getenv("TESSDATA_PREFIX"); d != "" {
		dirs = append(dirs, d, filepath.Join(d, "tessdata"))
	}
	m, _ := filepath.Glob("/usr/share/tesseract-ocr/*/tessdata")
	return append(append(dirs, m...), "/usr/share/tessdata", "/usr/local/share/tessdata")
}

// tessLanguages is Tesseract's languages for the request's (none: the
// app's), those it has data for ("eng" if none).
func tessLanguages(tags []string, locale string) string {
	if len(tags) == 0 && locale != "" {
		tags = []string{strings.ReplaceAll(strings.SplitN(locale, ".", 2)[0], "_", "-")}
	}
	var out []string
	for _, t := range tags {
		code := tessCodes[t]
		if code == "" {
			base, _, _ := strings.Cut(t, "-")
			code = tessCodes[base]
		}
		if code == "" || inList(out, code) {
			continue
		}
		for _, d := range tessDataDirs() {
			if _, err := os.Stat(filepath.Join(d, code+".traineddata")); err == nil {
				out = append(out, code)
				break
			}
		}
	}
	if len(out) == 0 {
		return "eng"
	}
	return strings.Join(out, "+")
}
