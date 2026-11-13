//go:build windows

package windows

import (
	"encoding/json"
	stdimage "image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/ui"
)

// scanReply decodes scan.file's answer.
func scanReply(b []byte) [][2]string {
	out := [][2]string{}
	str := func() string {
		n := int(b[0]) | int(b[1])<<8 | int(b[2])<<16 | int(b[3])<<24
		s := string(b[4 : 4+n])
		b = b[4+n:]
		return s
	}
	n := int(b[0]) | int(b[1])<<8
	b = b[4:]
	for range n {
		k := str()
		out = append(out, [2]string{k, str()})
	}
	return out
}

// scan.file reads the codes of the kinds asked for in an image (a QR
// code turned in a cluttered picture; four barcodes on their side), a
// UPC-A named so unless only EAN-13s are asked for; vision's codes come
// with their boxes.
func TestScanFile(t *testing.T) {
	qr, _ := filepath.Abs(filepath.Join("testdata", "qr.png"))
	bars, _ := filepath.Abs(filepath.Join("testdata", "barcodes.png"))
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		for _, c := range []struct {
			args string
			want string
		}{
			{"\n" + qr, "[[qr https://ryui.dev/scan]]"},
			{"qr,ean13\n" + qr, "[[qr https://ryui.dev/scan]]"},
			{"ean13\n" + qr, "[]"},
			{"aztec,pdf417\n" + qr, "[]"},
			{"\n" + bars, "[[code39 RYUI39] [code128 RyUI-128] [upca 036000291452] [ean13 4006381333931]]"},
			{"ean13\n" + bars, "[[ean13 0036000291452] [ean13 4006381333931]]"},
			{"code39,upca\n" + bars, "[[code39 RYUI39] [upca 036000291452]]"},
		} {
			b, err, _ := await(h, "scan", "file", []byte(c.args))
			if err != nil {
				fatalf("scan.file %q: %v", c.args, err)
			}
			got := scanReply(b)
			if s := strings.ReplaceAll(strings.ReplaceAll(fmtCodes(got), "\"", ""), ",", " "); s != c.want {
				fatalf("scan.file %q: %s, want %s", c.args, s, c.want)
			}
		}
		if _, err, _ := await(h, "scan", "file", []byte("\nC:\\nope.png")); err == nil || !strings.HasPrefix(err.Error(), "can't read the image") {
			fatalf("a missing image: %v", err)
		}
		args, _ := json.Marshal(map[string]any{"path": qr, "text": false, "faces": false})
		b, err, _ := await(h, "vision", "analyze", args)
		var res visResult
		if err != nil || json.Unmarshal(b, &res) != nil || len(res.Codes) != 1 {
			fatalf("vision's codes: %s, %v", b, err)
		}
		c := res.Codes[0]
		// The code's box, about the turned square's in the picture.
		if c.Kind != "qr" || c.Value != "https://ryui.dev/scan" || c.Box.X < 0.12 || c.Box.X > 0.24 || c.Box.Y < 0.15 || c.Box.Y > 0.28 || c.Box.W < 0.4 || c.Box.H < 0.45 {
			fatalf("vision's code: %+v", c)
		}
	})
}

func fmtCodes(cs [][2]string) string {
	b, _ := json.Marshal(cs)
	s := strings.ReplaceAll(string(b), "],[", "] [")
	return s
}

// A scanning camera view reads codes in its frames: a code is reported
// as it comes into view, once while it stays, again once it's been out
// of view for a second; the kinds asked for only.
func TestCameraScansFrames(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "qr.png"))
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	rgba := stdimage.NewRGBA(img.Bounds())
	draw.Draw(rgba, rgba.Rect, img, img.Bounds().Min, draw.Src)
	frame := Frame{Width: rgba.Rect.Dx(), Height: rgba.Rect.Dy(), Pix: rgba.Pix} // gray: RGBA reads as BGRX
	blank := Frame{Width: frame.Width, Height: frame.Height, Pix: make([]byte, len(frame.Pix))}
	for i := range blank.Pix {
		blank.Pix[i] = 200
	}
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		var codes []map[string]any
		v := &cameraView{h: h, emit: func(name string, values map[string]any) {
			if name == "code" {
				codes = append(codes, values)
			}
		}}
		v.hwnd = ownWindow(0, "", WS_CHILD, 0, 0, 100, 100, h.window, nil)
		defer DestroyWindow(v.hwnd)
		v.frames = newCameraFrames(v)
		feed := func(f Frame) {
			c := v.frames
			c.mu.Lock()
			c.last = time.Time{} // read this one
			c.mu.Unlock()
			done := make(chan struct{})
			go func() { c.frame(f); close(done) }() // from another thread, as Media Foundation's
			<-done
			pumpUntil(func() bool {
				c.mu.Lock()
				defer c.mu.Unlock()
				return !c.reading
			})
			h.runPosted()
		}
		feed(frame)
		if !pumpUntil(func() bool { return len(codes) == 1 }) || codes[0]["kind"] != "qr" || codes[0]["value"] != "https://ryui.dev/scan" {
			fatalf("the code in view: %v", codes)
		}
		feed(frame)
		feed(blank)
		if len(codes) != 1 {
			fatalf("a code staying in view is reported again: %v", codes)
		}
		// Out of view for over a second: reported again.
		v.frames.found(scanImage(frameGray(frame, nil), nil), nil, time.Now().Add(2*time.Second))
		if len(codes) != 2 {
			fatalf("back in view: %v", codes)
		}
		// Only the kinds asked for.
		v.Set("codes", "ean13")
		v.seen = codeSeen{}
		feed(frame)
		if len(codes) != 2 {
			fatalf("a QR code, EAN-13s asked for: %v", codes)
		}
		// The frame drawn fills the view.
		dc := CreateCompatibleDC(0)
		bmp, bits := DIB(100, 100)
		old := SelectObject(dc, bmp)
		v.frames.paint(dc, Rect{Right: 100, Bottom: 100})
		SelectObject(dc, old)
		dark := 0
		for i := 0; i < len(bits); i += 4 {
			if bits[i] < 80 {
				dark++
			}
		}
		DeleteObject(bmp)
		DeleteDC(dc)
		if dark < 300 {
			fatalf("the frame isn't drawn: %d dark pixels", dark)
		}
		v.frames.close()
	})
}
