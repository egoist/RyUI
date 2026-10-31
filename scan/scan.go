// Package scan reads barcodes and QR codes: live, in what a camera.View
// sees (its Scan, set to Codes), or in an image file (File). iOS reads
// them with AVFoundation and Vision; Android with ML Kit, which an app
// that imports this package is built with (about 2 MB; the others go
// without).
package scan

import (
	"errors"
	"strings"

	"github.com/egoist/ryui/ui"
)

// Kind is a code's symbology.
type Kind string

const (
	QR         Kind = "qr"
	Aztec      Kind = "aztec"
	DataMatrix Kind = "datamatrix"
	PDF417     Kind = "pdf417"
	EAN13      Kind = "ean13"
	EAN8       Kind = "ean8"
	UPCA       Kind = "upca"
	UPCE       Kind = "upce"
	Code128    Kind = "code128"
	Code39     Kind = "code39"
	Code93     Kind = "code93"
	Codabar    Kind = "codabar"
	ITF        Kind = "itf" // interleaved 2 of 5, ITF-14
)

// Code is a code read: its kind and what it says.
type Code struct {
	Kind  Kind
	Value string
}

// Scanner reads codes in a camera.View; Codes makes one.
type Scanner struct {
	kinds string
	found func(Code)
}

// Codes reads the codes of the kinds given (every kind, if none) in what
// a camera.View sees: camera.ViewProps{Scan: scan.Codes(found, scan.QR)}.
// found is called on the main thread as a code comes into view, once
// while it stays (again after it's been out of view for a second).
func Codes(found func(Code), kinds ...Kind) *Scanner {
	return &Scanner{kinds: joinKinds(kinds), found: found}
}

// CameraScan is what camera.View reads: the kinds, comma-separated, and
// the function to call with each code.
func (s *Scanner) CameraScan() (kinds string, found func(kind, value string)) {
	return s.kinds, func(kind, value string) {
		if s.found != nil {
			s.found(Code{Kind(kind), value})
		}
	}
}

// File reads the codes of the kinds given (every kind, if none) in the
// image at path (a photo taken or picked) and calls done on the main
// thread with them: none, if there are none. Main thread.
//
// Never inlined: ryui build looks for its symbol (cmd/ryui iosFeatures).
//
//go:noinline
func File(path string, done func([]Code, error), kinds ...Kind) {
	arg := joinKinds(kinds) + "\n" + path
	ui.Call("scan", "file", []byte(arg), func(b []byte, err error) {
		if err != nil {
			done(nil, err)
			return
		}
		codes, err := decode(b)
		done(codes, err)
	})
}

func joinKinds(kinds []Kind) string {
	s := make([]string, len(kinds))
	for i, k := range kinds {
		s[i] = string(k)
	}
	return strings.Join(s, ",")
}

var errBadReply = errors.New("scan: a bad reply")

// decode reads the host's reply: a u32 count, then each code's kind and
// value, each a u32 length and its bytes (little-endian).
func decode(b []byte) ([]Code, error) {
	u32 := func() (int, bool) {
		if len(b) < 4 {
			return 0, false
		}
		n := int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24
		b = b[4:]
		return n, true
	}
	str := func() (string, bool) {
		n, ok := u32()
		if !ok || n > len(b) {
			return "", false
		}
		s := string(b[:n])
		b = b[n:]
		return s, true
	}
	n, ok := u32()
	if !ok || n > len(b)/8 {
		return nil, errBadReply
	}
	codes := make([]Code, 0, n)
	for range n {
		k, ok1 := str()
		v, ok2 := str()
		if !ok1 || !ok2 {
			return nil, errBadReply
		}
		codes = append(codes, Code{Kind(k), v})
	}
	return codes, nil
}
