//go:build windows

package windows

import (
	"encoding/json"
	"errors"
	stdimage "image"
	"strings"
	"unicode/utf8"

	. "github.com/egoist/asuka/internal/windows"
)

// asuka/sdk/vision's "analyze" on Windows: the image decoded upright (WIC),
// its text read by Windows's OCR (Windows.Media.Ocr: line by line, word
// by word, in the first language asked for whose OCR is installed, else
// the user's), its faces found by Windows.Media.FaceAnalysis; boxes in
// fractions of the image from its top left, its codes read by package
// barcode (scan.go). Off the main thread.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["vision"] = map[string]asyncMethod{"analyze": h.visionAnalyze}
		m["scan"] = map[string]asyncMethod{"file": h.scanFileMethod}
	})
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

type visResult struct {
	Width  int       `json:"width"`
	Height int       `json:"height"`
	Lines  []visLine `json:"lines"`
	Faces  []visBox  `json:"faces"`
	Codes  []visCode `json:"codes"`
}

type visCode struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Box   visBox `json:"box"`
}

func (b visBox) union(o visBox) visBox {
	if b.W <= 0 || b.H <= 0 {
		return o
	}
	x0, y0 := min(b.X, o.X), min(b.Y, o.Y)
	x1, y1 := max(b.X+b.W, o.X+o.W), max(b.Y+b.H, o.Y+o.H)
	return visBox{x0, y0, x1 - x0, y1 - y0}
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
	h.offMain(func() (b []byte, err error) {
		WithCOM(func() {
			img, e := DecodeImageFile(path, 0)
			if e != nil {
				err = errNoImage
				return
			}
			w, ht := img.Rect.Dx(), img.Rect.Dy()
			out := visResult{Width: w, Height: ht, Lines: []visLine{}, Faces: []visBox{}, Codes: []visCode{}}
			if text {
				if out.Lines, err = visReadText(path, img, languages); err != nil {
					return
				}
			}
			if faces {
				rs, e := DetectFaces(img)
				if e != nil {
					err = e
					return
				}
				for _, r := range rs {
					out.Faces = append(out.Faces, visBox{float64(r.Min.X) / float64(w), float64(r.Min.Y) / float64(ht), float64(r.Dx()) / float64(w), float64(r.Dy()) / float64(ht)})
				}
			}
			if codes {
				for _, c := range scanImage(img, nil) {
					r := c.Bounds
					out.Codes = append(out.Codes, visCode{Kind: string(c.Kind), Value: c.Text,
						Box: visBox{float64(r.Min.X) / float64(w), float64(r.Min.Y) / float64(ht), float64(r.Dx()) / float64(w), float64(r.Dy()) / float64(ht)}})
				}
			}
			b, err = json.Marshal(out)
		})
		return b, err
	}, reply)
}

// visReadText reads the lines: the image scaled down to what the OCR
// takes, if it's larger (the boxes are fractions: the same).
func visReadText(path string, img *stdimage.NRGBA, languages []string) ([]visLine, error) {
	w, ht := img.Rect.Dx(), img.Rect.Dy()
	src := img
	if m := OCRMaxSide(); m > 0 && max(w, ht) > m {
		small, err := DecodeImageFile(path, m)
		if err != nil {
			return nil, err
		}
		src = small
	}
	sw, sh := float64(src.Rect.Dx()), float64(src.Rect.Dy())
	lines, err := OCR(src, languages)
	if errors.Is(err, ErrNoOCR) {
		return nil, errors.New("not available: " + err.Error())
	}
	if err != nil {
		return nil, err
	}
	out := []visLine{}
	for _, l := range lines {
		line := visLine{Text: l.Text, Words: []visWord{}}
		at := 0 // bytes into the line's text
		for _, wd := range l.Words {
			box := visBox{wd.X / sw, wd.Y / sh, wd.W / sw, wd.H / sh}
			line.Box = line.Box.union(box)
			start, end := 0, 0
			if i := strings.Index(l.Text[at:], wd.Text); i >= 0 {
				start = utf8.RuneCountInString(l.Text[:at+i])
				at += i + len(wd.Text)
				end = start + utf8.RuneCountInString(wd.Text)
			}
			line.Words = append(line.Words, visWord{Text: wd.Text, Box: box, Start: start, End: end})
		}
		out = append(out, line)
	}
	return out, nil
}
