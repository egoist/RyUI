//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/ebitengine/purego"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// visWritePNG writes a CGImage as a PNG.
func visWritePNG(t *testing.T, img uintptr, path string) {
	a := ie()
	d := a.destinationCreate(uintptr(FileURL(path)), uintptr(String("public.png")), 1, 0)
	a.destinationAdd(d, img, 0)
	if !a.destinationEnd(d) {
		fatalf(t, "can't write %s", path)
	}
	CFRelease(d)
}

var (
	visTestOnce    sync.Once
	visTransform   func(obj ID, sel SEL, t AffineTransform) ID
	visCreateImage func(obj ID, sel SEL, img ID, r Rect) uintptr
)

// visQR writes a QR code of value, made by Core Image, 10 pixels a
// module, on white.
func visQR(t *testing.T, value, path string) {
	visTestOnce.Do(func() {
		purego.RegisterFunc(&visTransform, MsgSendAddr())
		purego.RegisterFunc(&visCreateImage, MsgSendStretAddr())
	})
	Dlopen("/System/Library/Frameworks/CoreImage.framework/CoreImage")
	f := Send(Class("CIFilter"), "filterWithName:", uintptr(String("CIQRCodeGenerator")))
	Send(f, "setValue:forKey:", uintptr(Data([]byte(value))), uintptr(String("inputMessage")))
	out := visTransform(Send(f, "outputImage"), Sel("imageByApplyingTransform:"), AffineTransform{A: 10, D: 10})
	ctx := Send(Class("CIContext"), "context")
	qr := visCreateImage(ctx, Sel("createCGImage:fromRect:"), out, MsgRect(out, Sel("extent")))
	if qr == 0 {
		fatal(t, "no QR image")
	}
	defer CFRelease(qr)
	// A quiet zone around it, on white.
	w, h := ieSize(qr)
	c, _ := ieContext(w+80, h+80, true, 0)
	white := ie().colorSRGB(1, 1, 1, 1)
	ie().setFill(c, white)
	CFRelease(white)
	ie().fillRect(c, ieRect(0, 0, float64(w+80), float64(h+80)))
	ie().drawImage(c, ieRect(40, 40, float64(w), float64(h)), qr)
	img := ieImage(c)
	defer CFRelease(img)
	visWritePNG(t, img, path)
}

// visTextImage writes text, black on white, as AppKit draws it.
func visTextImage(t *testing.T, text, path string) {
	size := Size{Width: 900, Height: 140}
	img := MsgSizeID(Send(Class("NSImage"), "alloc"), Sel("initWithSize:"), size)
	defer Release(img)
	Send(img, "lockFocus")
	Send(Send(Class("NSColor"), "whiteColor"), "set")
	var rectFill func(r Rect)
	purego.RegisterLibFunc(&rectFill, AppKit(), "NSRectFill")
	rectFill(Rect{Size: size})
	attrs := Dict(Const(AppKit(), "NSFontAttributeName"), MsgFloatID(Class("NSFont"), Sel("systemFontOfSize:"), 64),
		Const(AppKit(), "NSForegroundColorAttributeName"), Send(Class("NSColor"), "blackColor"))
	MsgDrawInRect(String(text), Sel("drawInRect:withAttributes:"), Rect{Origin: Point{X: 20, Y: 20}, Size: Size{Width: 860, Height: 100}}, attrs)
	Send(img, "unlockFocus")
	rep := Send(Class("NSBitmapImageRep"), "imageRepWithData:", uintptr(Send(img, "TIFFRepresentation")))
	data := Send(rep, "representationUsingType:properties:", 4, uintptr(Dict())) // PNG
	if err := os.WriteFile(path, Bytes(data), 0o644); err != nil {
		fatal(t, err)
	}
}

// visPNG writes a w×h PNG: its left half red, its right half blue, its
// top left pixel green.
func visPNG(t *testing.T, path string, w, h int) {
	m := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{255, 0, 0, 255}
			if x >= w/2 {
				c = color.NRGBA{0, 0, 255, 255}
			}
			m.SetNRGBA(x, y, c)
		}
	}
	m.SetNRGBA(0, 0, color.NRGBA{0, 255, 0, 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	png.Encode(f, m)
}

func visDecode(t *testing.T, path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		fatal(t, err)
	}
	defer f.Close()
	m, _, err := image.Decode(f)
	if err != nil {
		fatal(t, err)
	}
	return m
}

// near reports whether c is within 24 of r, g, b (the color space
// converted, the JPEG compressed).
func near(c color.Color, r, g, b int) bool {
	cr, cg, cb, _ := c.RGBA()
	d := func(x uint32, y int) bool { v := int(x>>8) - y; return v < 24 && v > -24 }
	return d(cr, r) && d(cg, g) && d(cb, b)
}

// scan.file reads the codes in an image file, with the kinds asked for;
// a file that isn't there, or arguments without a path, fail.
func TestScanFile(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	dir := t.TempDir()
	qr := filepath.Join(dir, "qr.png")
	onMain(func() {
		visQR(t, "https://asuka.dev/scan", qr)
		b, err := await(h, "scan", "file", []byte("\n"+qr))
		if err != nil {
			fatal(t, "scan.file:", err)
		}
		if want := string(encodeCodes([][2]string{{"qr", "https://asuka.dev/scan"}})); string(b) != want {
			fatalf(t, "scan.file: %q, want %q", b, want)
		}
		if b, err := await(h, "scan", "file", []byte("ean13,code128\n"+qr)); err != nil || string(b) != "\x00\x00\x00\x00" {
			fatalf(t, "a QR code when asking for barcodes: %q, %v", b, err)
		}
		if _, err := await(h, "scan", "file", []byte("qr\n"+filepath.Join(dir, "missing.png"))); err == nil || !strings.HasPrefix(err.Error(), "can't read the image") {
			fatalf(t, "a missing file: %v", err)
		}
		if _, err := await(h, "scan", "file", []byte("qr")); err == nil || err.Error() != "bad arguments" {
			fatalf(t, "no path: %v", err)
		}
	})
	if k := parseScanKinds("upca"); true {
		if kind, v := k.named("ean13", "0123456789012"); kind != "upca" || v != "123456789012" {
			t.Errorf("a UPC-A read as an EAN-13: %s %s", kind, v)
		}
	}
}

// vision.analyze reads an image's lines of text and their words, boxed
// from the top left, and its codes; bad arguments and a missing image
// fail.
func TestVisionAnalyze(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	dir := t.TempDir()
	text, qr := filepath.Join(dir, "text.png"), filepath.Join(dir, "qr.png")
	onMain(func() {
		visTextImage(t, "Hello, world 42", text)
		visQR(t, "asuka", qr)
		args, _ := json.Marshal(map[string]any{"path": text, "faces": false, "codes": false, "languages": []string{"en-US"}})
		b, err := await(h, "vision", "analyze", args)
		if err != nil {
			fatal(t, "vision.analyze:", err)
		}
		var r visResult
		if err := json.Unmarshal(b, &r); err != nil {
			fatal(t, err)
		}
		if r.Width < 900 || r.Height < 140 || len(r.Lines) != 1 || len(r.Faces) != 0 || len(r.Codes) != 0 {
			fatalf(t, "vision.analyze: %s", b)
		}
		l := r.Lines[0]
		var words []string
		for _, w := range l.Words {
			words = append(words, w.Text)
			if got := string([]rune(l.Text)[w.Start:w.End]); got != w.Text {
				t.Errorf("word %q at %d-%d is %q in the line", w.Text, w.Start, w.End, got)
			}
			if w.Box.X < l.Box.X-0.02 || w.Box.X+w.Box.W > l.Box.X+l.Box.W+0.02 || w.Box.W <= 0 {
				t.Errorf("word %q's box %+v isn't in the line's %+v", w.Text, w.Box, l.Box)
			}
		}
		if l.Text != "Hello, world 42" || strings.Join(words, "|") != "Hello|,|world|42" {
			fatalf(t, "read %q as %q", l.Text, words)
		}
		// Drawn 20 points from the left and 20 from the top of 900 by 140.
		if l.Box.X < 0.01 || l.Box.X > 0.06 || l.Box.Y < 0.05 || l.Box.Y > 0.5 {
			t.Errorf("the line's box: %+v", l.Box)
		}
		for i := 1; i < len(l.Words); i++ {
			if l.Words[i].Box.X <= l.Words[i-1].Box.X {
				t.Errorf("words out of order: %+v", l.Words)
			}
		}

		args, _ = json.Marshal(map[string]any{"path": qr, "text": false})
		b, err = await(h, "vision", "analyze", args)
		if err != nil {
			fatal(t, "vision.analyze:", err)
		}
		r = visResult{}
		json.Unmarshal(b, &r)
		if len(r.Codes) != 1 || r.Codes[0].Kind != "qr" || r.Codes[0].Value != "asuka" || r.Lines != nil && len(r.Lines) != 0 {
			fatalf(t, "codes: %s", b)
		}
		if c := r.Codes[0].Box; c.X < 0.1 || c.X > 0.3 || c.W < 0.5 || c.H < 0.5 {
			t.Errorf("the code's box: %+v", c)
		}

		if _, err := await(h, "vision", "analyze", []byte(`{"text":true}`)); err == nil || err.Error() != "bad args" {
			fatalf(t, "no path: %v", err)
		}
		args, _ = json.Marshal(map[string]any{"path": filepath.Join(dir, "missing.png")})
		if _, err := await(h, "vision", "analyze", args); err == nil || err.Error() != "no image" {
			fatalf(t, "a missing image: %v", err)
		}
	})
}

// imageEdit sizes images and resizes, crops, turns, flips, fills and
// pixelates them, writing PNG or JPEG.
func TestImageEdit(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	dir := t.TempDir()
	src := filepath.Join(dir, "src.png")
	visPNG(t, src, 40, 20)
	onMain(func() {
		edit := func(method string, req map[string]any) (map[string]any, error) {
			req["source"] = src
			args, _ := json.Marshal(req)
			b, err := await(h, "imageEdit", method, args)
			if err != nil {
				return nil, err
			}
			var r map[string]any
			json.Unmarshal(b, &r)
			if p, _ := r["path"].(string); p != "" {
				t.Cleanup(func() { os.Remove(p) })
			}
			return r, nil
		}
		size := func(r map[string]any) (int, int) { return int(r["width"].(float64)), int(r["height"].(float64)) }
		r, err := edit("size", map[string]any{})
		if err != nil || r["path"] != "" {
			fatalf(t, "size: %v, %v", r, err)
		}
		if w, ht := size(r); w != 40 || ht != 20 {
			fatalf(t, "size: %d×%d", w, ht)
		}
		type act = map[string]any
		cases := []struct {
			name    string
			actions []act
			format  string
			w, h    int
			check   func(m image.Image) bool
		}{
			{"resize", []act{{"op": "resize", "width": 20}}, "png", 20, 10, nil},
			{"crop", []act{{"op": "crop", "x": 25, "y": 5, "width": 10, "height": 10}}, "png", 10, 10,
				func(m image.Image) bool { return near(m.At(5, 5), 0, 0, 255) }},
			{"rotate 90", []act{{"op": "rotate", "degrees": 90}}, "png", 20, 40,
				// Clockwise: the top left corner goes to the top right.
				func(m image.Image) bool { return near(m.At(19, 0), 0, 255, 0) && near(m.At(10, 5), 255, 0, 0) }},
			{"rotate 45", []act{{"op": "rotate", "degrees": 45}}, "png", 43, 43,
				func(m image.Image) bool { _, _, _, a := m.At(0, 0).RGBA(); return a == 0 }},
			{"flip", []act{{"op": "flipH"}}, "png", 40, 20,
				func(m image.Image) bool { return near(m.At(39, 0), 0, 255, 0) && near(m.At(5, 10), 0, 0, 255) }},
			{"flipV", []act{{"op": "flipV"}}, "png", 40, 20,
				func(m image.Image) bool { return near(m.At(0, 19), 0, 255, 0) }},
			{"fill", []act{{"op": "fill", "x": 0, "y": 0, "width": 10, "height": 5, "rgb": 0xffff00}}, "jpeg", 40, 20,
				func(m image.Image) bool { return near(m.At(4, 2), 255, 255, 0) && near(m.At(4, 12), 255, 0, 0) }},
			{"pixelate", []act{{"op": "pixelate", "x": 0, "y": 0, "width": 40, "height": 20, "block": 40}}, "png", 40, 20,
				// One square: the average of half red, half blue.
				func(m image.Image) bool { return near(m.At(0, 0), 127, 0, 127) && near(m.At(30, 10), 127, 0, 127) }},
		}
		for _, c := range cases {
			r, err := edit("edit", map[string]any{"actions": c.actions, "format": c.format, "quality": 0.95})
			if err != nil {
				t.Errorf("%s: %v", c.name, err)
				continue
			}
			if w, ht := size(r); w != c.w || ht != c.h {
				t.Errorf("%s: %d×%d, want %d×%d", c.name, w, ht, c.w, c.h)
				continue
			}
			path := r["path"].(string)
			if ext := filepath.Ext(path); ext != map[string]string{"png": ".png", "jpeg": ".jpg"}[c.format] {
				t.Errorf("%s: wrote %s", c.name, path)
			}
			m := visDecode(t, path)
			if b := m.Bounds(); b.Dx() != c.w || b.Dy() != c.h {
				t.Errorf("%s: the file is %v", c.name, b)
			}
			if c.check != nil && !c.check(m) {
				t.Errorf("%s: not as expected (top left %v)", c.name, m.At(0, 0))
			}
		}
		if _, err := edit("edit", map[string]any{"actions": []act{{"op": "crop", "x": 100, "y": 100, "width": 5, "height": 5}}}); err == nil || !strings.Contains(err.Error(), "outside") {
			t.Errorf("a crop outside: %v", err)
		}
		if _, err := edit("edit", map[string]any{"actions": []act{{"op": "twirl"}}}); err == nil || !strings.Contains(err.Error(), "unknown action") {
			t.Errorf("an unknown action: %v", err)
		}
		args, _ := json.Marshal(map[string]any{"source": filepath.Join(dir, "missing.png")})
		if _, err := await(h, "imageEdit", "edit", args); err == nil || err.Error() != "no image" {
			t.Errorf("a missing image: %v", err)
		}
		if _, err := await(h, "imageEdit", "videoFrame", args); err == nil {
			t.Errorf("a missing video: no error")
		}
		if _, err := await(h, "imageEdit", "size", []byte("{")); err == nil || err.Error() != "bad args" {
			t.Errorf("bad args: %v", err)
		}
	})
}

// transcribe.check names the recognizer's language; file fails "not
// allowed" when the user hasn't allowed speech recognition (the prompt
// itself can't be answered in a test).
func TestTranscribe(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		b, err := await(h, "transcribe", "check", []byte("en-US"))
		if err != nil {
			fatal(t, "check:", err)
		}
		var a struct {
			OnDevice *bool  `json:"onDevice"`
			Language string `json:"language"`
		}
		if json.Unmarshal(b, &a) != nil || a.OnDevice == nil || a.Language != "en-US" {
			fatalf(t, "check: %s", b)
		}
		was := trAuthorize
		defer func() { trAuthorize = was }()
		trAuthorize = func(h *Host, then func(bool)) { h.Post(func() { then(false) }) }
		args, _ := json.Marshal(map[string]string{"path": "/nonexistent.m4a", "language": "en-US"})
		if _, err := await(h, "transcribe", "file", args); err == nil || err.Error() != "not allowed" {
			fatalf(t, "file, not allowed: %v", err)
		}
	})
}
