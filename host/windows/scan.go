//go:build windows

package windows

import (
	"encoding/binary"
	"errors"
	stdimage "image"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/ryui/internal/barcode"
	. "github.com/egoist/ryui/internal/windows"
)

// Codes in images on Windows, which has no reader of its own for apps:
// package barcode's, in Go (QR codes, Data Matrix, EAN-13 and EAN-8,
// UPC-A and UPC-E, Code 128, Code 39). ryui/scan's "file" (the kinds,
// comma-separated, a newline, the path; answers a u32 count, then each
// code's kind and value, u32-length strings), vision's codes, and the
// camera view's frames.

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

// barcodeKinds is what to ask package barcode for; ok is false when
// none of the kinds asked for is one it reads (Aztec, PDF417...).
func (k scanKinds) barcodeKinds() (kinds []barcode.Kind, ok bool) {
	if len(k) == 0 {
		return nil, true
	}
	for _, b := range []barcode.Kind{barcode.QR, barcode.DataMatrix, barcode.EAN13, barcode.EAN8, barcode.UPCA, barcode.UPCE, barcode.Code128, barcode.Code39} {
		if k[string(b)] {
			kinds = append(kinds, b)
		}
	}
	return kinds, len(kinds) > 0
}

// scanImage reads the codes of the kinds asked for in img.
func scanImage(img stdimage.Image, k scanKinds) []barcode.Code {
	kinds, ok := k.barcodeKinds()
	if !ok {
		return nil
	}
	return barcode.Decode(img, kinds...)
}

// scanMaxSide: a larger photo is read scaled down to this first, then
// whole if nothing was found (a small code in a big photo).
const scanMaxSide = 2048

// scanFile reads a file's codes, the image upright. Off the main thread.
func scanFile(path string, k scanKinds) (codes []barcode.Code, w, h int, err error) {
	WithCOM(func() {
		var img *stdimage.NRGBA
		img, err = DecodeImageFile(path, 0)
		if err != nil {
			return
		}
		w, h = img.Rect.Dx(), img.Rect.Dy()
		if max(w, h) > scanMaxSide {
			if small, e := DecodeImageFile(path, scanMaxSide); e == nil {
				if codes = scanImage(small, k); len(codes) > 0 {
					sx, sy := float64(w)/float64(small.Rect.Dx()), float64(h)/float64(small.Rect.Dy())
					for i := range codes {
						b := codes[i].Bounds
						codes[i].Bounds = stdimage.Rect(int(float64(b.Min.X)*sx), int(float64(b.Min.Y)*sy), int(float64(b.Max.X)*sx+0.5), int(float64(b.Max.Y)*sy+0.5))
					}
					return
				}
			}
		}
		codes = scanImage(img, k)
	})
	return
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

func (h *Host) scanFileMethod(args []byte, reply func([]byte, error)) {
	kindsArg, path, ok := strings.Cut(string(args), "\n")
	if !ok {
		reply(nil, errors.New("bad arguments"))
		return
	}
	k := parseScanKinds(kindsArg)
	h.offMain(func() ([]byte, error) {
		codes, _, _, err := scanFile(path, k)
		if err != nil {
			return nil, errors.New("can't read the image: " + err.Error())
		}
		var out [][2]string
		for _, c := range codes {
			v := [2]string{string(c.Kind), c.Text}
			if k.wants(v[0]) && !slices.Contains(out, v) {
				out = append(out, v)
			}
		}
		return encodeCodes(out), nil
	}, reply)
}

// The camera's codes.

// frameScanEvery is how often the camera view reads a frame's codes.
const frameScanEvery = 250 * time.Millisecond

// codeSeen drops repeats of a code in view: a code is reported as it
// comes into view, and again once it's been out of it for a second (as
// iOS's RyUICodeSeen).
type codeSeen struct{ last map[string]time.Time }

func (s *codeSeen) isNew(kind, value string, now time.Time) bool {
	if s.last == nil {
		s.last = map[string]time.Time{}
	}
	key := kind + "\x00" + value
	t, ok := s.last[key]
	isNew := !ok || now.Sub(t) > time.Second
	s.last[key] = now
	if len(s.last) > 32 {
		for k, t := range s.last {
			if now.Sub(t) > time.Second {
				delete(s.last, k)
			}
		}
	}
	return isNew
}

// frameGray is a BGRX frame's luminance.
func frameGray(f Frame, dst *stdimage.Gray) *stdimage.Gray {
	if dst == nil || dst.Rect.Dx() != f.Width || dst.Rect.Dy() != f.Height {
		dst = stdimage.NewGray(stdimage.Rect(0, 0, f.Width, f.Height))
	}
	p := f.Pix
	for i, j := 0, 0; j < len(dst.Pix) && i+2 < len(p); i, j = i+4, j+1 {
		// BT.601's weights, in integers.
		dst.Pix[j] = uint8((299*uint32(p[i+2]) + 587*uint32(p[i+1]) + 114*uint32(p[i]) + 500) / 1000)
	}
	return dst
}

// cameraFrames is a scanning camera view's preview: the latest frame,
// drawn by the view, and the reading of codes in one every
// frameScanEvery, off the main thread.
type cameraFrames struct {
	v *cameraView

	mu      sync.Mutex // the fields below, which Media Foundation's threads write
	latest  []byte
	w, h    int
	posted  bool // a redraw waits on the main thread
	closed  bool
	kinds   scanKinds
	last    time.Time
	reading bool // a frame's codes are being read
	gray    *stdimage.Gray

	bmp        uintptr // the frame drawn, main thread
	bits       []byte
	bmpW, bmpH int
}

func newCameraFrames(v *cameraView) *cameraFrames {
	return &cameraFrames{v: v, kinds: v.kinds}
}

func (c *cameraFrames) setKinds(k scanKinds) {
	c.mu.Lock()
	c.kinds = k
	c.mu.Unlock()
}

// frame takes a frame, on Media Foundation's thread: kept for the view
// to draw, and read for codes if it's time.
func (c *cameraFrames) frame(f Frame) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if len(c.latest) != len(f.Pix) {
		c.latest = make([]byte, len(f.Pix))
	}
	copy(c.latest, f.Pix)
	c.w, c.h = f.Width, f.Height
	if !c.posted {
		c.posted = true
		theHost.Post(func() {
			c.mu.Lock()
			c.posted = false
			closed := c.closed
			c.mu.Unlock()
			if !closed {
				InvalidateRect(c.v.hwnd, nil)
			}
		})
	}
	now := time.Now()
	if c.reading || now.Sub(c.last) < frameScanEvery {
		return
	}
	c.reading, c.last = true, now
	c.gray = frameGray(f, c.gray)
	gray, kinds := c.gray, c.kinds
	go func() {
		codes := scanImage(gray, kinds)
		c.mu.Lock()
		c.reading = false
		c.mu.Unlock()
		theHost.Post(func() { c.found(codes, kinds, time.Now()) })
	}()
}

// found reports the codes newly in view, on the main thread.
func (c *cameraFrames) found(codes []barcode.Code, kinds scanKinds, now time.Time) {
	v := c.v
	if c.closed || v.destroyed || v.frames != c {
		return
	}
	for _, code := range codes {
		kind := string(code.Kind)
		if !kinds.wants(kind) || !v.seen.isNew(kind, code.Text, now) {
			continue
		}
		v.emit("code", map[string]any{"kind": kind, "value": code.Text})
	}
}

// paint draws the latest frame filling r, cropped to keep its aspect.
func (c *cameraFrames) paint(dc uintptr, r Rect) {
	c.mu.Lock()
	w, h := c.w, c.h
	if w > 0 && h > 0 {
		if c.bmp == 0 || c.bmpW != w || c.bmpH != h {
			if c.bmp != 0 {
				DeleteObject(c.bmp)
			}
			c.bmp, c.bits = DIB(int32(w), int32(h))
			c.bmpW, c.bmpH = w, h
		}
		if c.bmp != 0 {
			copy(c.bits, c.latest)
		}
	}
	c.mu.Unlock()
	if c.bmp == 0 || r.W() <= 0 || r.H() <= 0 {
		FillColor(dc, r, 0)
		return
	}
	// The part of the frame of the view's aspect, centered.
	sx, sy, sw, sh := 0, 0, w, h
	if fa, va := float64(r.W())/float64(r.H()), float64(w)/float64(h); va > fa {
		sw = int(float64(h) * fa)
		sx = (w - sw) / 2
	} else {
		sh = int(float64(w) / fa)
		sy = (h - sh) / 2
	}
	mem := CreateCompatibleDC(dc)
	old := SelectObject(mem, c.bmp)
	StretchBlt(dc, 0, 0, r.W(), r.H(), mem, int32(sx), int32(sy), int32(sw), int32(sh))
	SelectObject(mem, old)
	DeleteDC(mem)
}

func (c *cameraFrames) close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	if c.bmp != 0 {
		DeleteObject(c.bmp)
		c.bmp = 0
	}
}
