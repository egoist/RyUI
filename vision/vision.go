// Package vision finds what's in an image, on the device: its text, line
// by line and word by word, faces, and barcodes and QR codes, each with
// where it is. iOS reads them with the Vision framework; Android with ML
// Kit and its bundled models (about 30 MB), which only the apps that
// import this package are built with.
//
//	r, err := vision.Analyze(path, vision.Options{})
//	for _, l := range r.Lines {
//		fmt.Println(l.Text, l.Box)
//	}
//
// Nothing leaves the device. ryui/scan reads the value of a code; use this
// package for where a code is.
package vision

import (
	"encoding/json"

	"github.com/egoist/ryui/ui"
)

// Box is a rectangle in an image, as fractions of its width and height
// from its top left, the image upright. Multiply by the image's size for
// pixels, or a view's for points: it holds at any scale.
type Box struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Union is the smallest box holding both; an empty box adds nothing.
func (b Box) Union(o Box) Box {
	if b.W <= 0 || b.H <= 0 {
		return o
	}
	if o.W <= 0 || o.H <= 0 {
		return b
	}
	x0, y0 := min(b.X, o.X), min(b.Y, o.Y)
	x1, y1 := max(b.X+b.W, o.X+o.W), max(b.Y+b.H, o.Y+o.H)
	return Box{x0, y0, x1 - x0, y1 - y0}
}

// Word is a word of a line, or a mark of punctuation, as the platform's
// word breaking finds them (dictionary-based for Chinese and Japanese).
// Start and End are where it is in the line's Text, in runes. Spaces have
// no word.
type Word struct {
	Text  string `json:"text"`
	Box   Box    `json:"box"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// Line is a line of text.
type Line struct {
	Text  string `json:"text"`
	Box   Box    `json:"box"`
	Words []Word `json:"words"`
}

// Code is a barcode or QR code: its symbology, as ryui/scan names them
// ("qr", "ean13"...), and what it says.
type Code struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Box   Box    `json:"box"`
}

// Result is what's in an image of Width×Height pixels, upright.
type Result struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Lines  []Line `json:"lines"`
	Faces  []Box  `json:"faces"`
	Codes  []Code `json:"codes"`
}

// Options say what to look for; the zero value looks for everything.
type Options struct {
	NoText, NoFaces, NoCodes bool
	// Languages are the text's, BCP 47 tags, most likely first
	// ("zh-Hans", "en-US"); none: the device's languages and English.
	// iOS only: Android's model reads Chinese and Latin scripts whatever
	// it's told.
	Languages []string
}

// Analyze looks in the image at path (a photo taken or picked; what
// ui.Image's platform decoder reads). Not on the main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func Analyze(path string, o Options) (Result, error) {
	b, err := ui.CallWait("vision", "analyze", request(path, o))
	if err != nil {
		return Result{}, err
	}
	var r Result
	err = json.Unmarshal(b, &r)
	return r, err
}

func request(path string, o Options) []byte {
	b, _ := json.Marshal(map[string]any{
		"path": path, "text": !o.NoText, "faces": !o.NoFaces, "codes": !o.NoCodes, "languages": o.Languages,
	})
	return b
}
