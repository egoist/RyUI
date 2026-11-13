//go:build linux && !android

package linux

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"math"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	. "github.com/egoist/ryui/internal/gtk"
)

// ryui/imageedit on Linux: "edit", "size" and "videoFrame", each with
// JSON (source, actions, format, quality, at in ms), answer JSON (path,
// width, height) or fail "no image". GdkPixbuf's loaders decode (JPEG,
// PNG, WebP, HEIF, TIFF... as the system has them), upright as the
// file's orientation says; the actions are done in Go, on premultiplied
// RGBA (resizing with a Catmull-Rom filter, which averages as it shrinks);
// Go's encoders write JPEG or PNG. A video's frame is GStreamer's
// (uridecodebin, turned upright by videoflip). Off the main thread.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["imageEdit"] = h.imageEditModule()
	})
}

var errNoImage = errors.New("no image")

// ieAction is an action of the request's (imageedit's JSON).
type ieAction struct {
	Op      string  `json:"op"`
	Width   int     `json:"width"`
	Height  int     `json:"height"`
	X       int     `json:"x"`
	Y       int     `json:"y"`
	Degrees float64 `json:"degrees"`
	Block   int     `json:"block"`
	RGB     int     `json:"rgb"`
}

type ieRequest struct {
	Source  string     `json:"source"`
	Actions []ieAction `json:"actions"`
	Format  string     `json:"format"`
	Quality *float64   `json:"quality"`
	At      float64    `json:"at"` // ms
}

func (h *Host) imageEditModule() map[string]method {
	assets, cache := h.assets, filepath.Join(h.modCacheDir(), "ryui-images")
	run := func(f func(r *ieRequest) (map[string]any, error)) method {
		return func(args []byte, reply func([]byte, error)) {
			var r ieRequest
			if json.Unmarshal(args, &r) != nil || r.Source == "" {
				reply(nil, errors.New("bad args"))
				return
			}
			if r.Quality == nil {
				q := 0.9
				r.Quality = &q
			}
			go func() {
				out, err := f(&r)
				var b []byte
				if err == nil {
					b, err = json.Marshal(out)
				}
				h.Post(func() { reply(b, err) })
			}()
		}
	}
	return map[string]method{
		"size": run(func(r *ieRequest) (map[string]any, error) {
			w, ht, err := ieUprightSize(r.Source, assets)
			if err != nil {
				return nil, err
			}
			return map[string]any{"path": "", "width": w, "height": ht}, nil
		}),
		"edit": run(func(r *ieRequest) (map[string]any, error) {
			img, err := ieDecode(r.Source, assets)
			if err != nil {
				return nil, err
			}
			if img, err = ieApply(r.Actions, img); err != nil {
				return nil, err
			}
			return ieWrite(img, r, cache)
		}),
		"videoFrame": run(func(r *ieRequest) (map[string]any, error) {
			img, err := ieVideoFrame(r.Source, assets, r.At/1000)
			if err != nil {
				return nil, err
			}
			if img, err = ieApply(r.Actions, img); err != nil {
				return nil, err
			}
			return ieWrite(img, r, cache)
		}),
	}
}

// ieLocal is a source's file, if it's one (a path, a file:// URL, an
// asset), else "".
func ieLocal(source, assets string) string {
	switch {
	case strings.HasPrefix(source, "http://"), strings.HasPrefix(source, "https://"):
		return ""
	case strings.HasPrefix(source, "file://"):
		if u, err := url.Parse(source); err == nil {
			return u.Path
		}
		return strings.TrimPrefix(source, "file://")
	case filepath.IsAbs(source):
		return source
	}
	return filepath.Join(assets, filepath.FromSlash(source))
}

// ieDecode decodes the image upright, as premultiplied RGBA.
func ieDecode(source, assets string) (*image.RGBA, error) {
	var px Pixels
	var err error
	if p := ieLocal(source, assets); p != "" {
		if st, serr := os.Stat(p); serr != nil || st.IsDir() {
			return nil, errNoImage
		}
		px, err = DecodePixbuf(p, nil)
	} else {
		data, rerr := readSource(source, assets)
		if rerr != nil {
			return nil, rerr
		}
		px, err = DecodePixbuf("", data)
	}
	if err != nil {
		return nil, errNoImage
	}
	return ieFromPixels(px), nil
}

// ieFromPixels premultiplies GdkPixbuf's RGB or RGBA.
func ieFromPixels(px Pixels) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, px.W, px.H))
	for y := range px.H {
		src, dst := px.Data[y*px.Stride:], img.Pix[y*img.Stride:]
		for x := range px.W {
			s, d := src[x*px.Channels:], dst[x*4:x*4+4]
			if px.Channels == 3 {
				d[0], d[1], d[2], d[3] = s[0], s[1], s[2], 255
				continue
			}
			a := uint32(s[3])
			d[0], d[1], d[2], d[3] = byte(uint32(s[0])*a/255), byte(uint32(s[1])*a/255), byte(uint32(s[2])*a/255), byte(a)
		}
	}
	return img
}

// ieUprightSize is the image's size upright, read from its header where
// GdkPixbuf can (a JPEG's EXIF orientation 5 to 8 turns it a quarter).
func ieUprightSize(source, assets string) (int, int, error) {
	if p := ieLocal(source, assets); p != "" {
		if _, err := os.Stat(p); err != nil {
			return 0, 0, errNoImage
		}
		if w, h := PixbufFileInfo(p); w > 0 && h > 0 {
			if f, err := os.Open(p); err == nil {
				head := make([]byte, 128<<10)
				n, _ := f.Read(head)
				f.Close()
				if o := jpegOrientation(head[:n]); o >= 5 && o <= 8 {
					return h, w, nil
				}
			}
			return w, h, nil
		}
	}
	img, err := ieDecode(source, assets)
	if err != nil {
		return 0, 0, err
	}
	return img.Rect.Dx(), img.Rect.Dy(), nil
}

// jpegOrientation is a JPEG's EXIF orientation (1 to 8; 0: none).
func jpegOrientation(b []byte) int {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return 0
	}
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xFF {
			return 0
		}
		marker, n := b[i+1], int(binary.BigEndian.Uint16(b[i+2:]))
		if marker == 0xDA || n < 2 { // the image data: no EXIF before it
			return 0
		}
		seg := b[i+4 : min(i+2+n, len(b))]
		if marker == 0xE1 && len(seg) > 14 && string(seg[:6]) == "Exif\x00\x00" {
			t := seg[6:]
			var bo binary.ByteOrder = binary.BigEndian
			if string(t[:2]) == "II" {
				bo = binary.LittleEndian
			}
			ifd := int(bo.Uint32(t[4:]))
			if ifd+2 > len(t) {
				return 0
			}
			count := int(bo.Uint16(t[ifd:]))
			for k := range count {
				e := ifd + 2 + 12*k
				if e+12 > len(t) {
					return 0
				}
				if bo.Uint16(t[e:]) == 0x0112 {
					return int(bo.Uint16(t[e+8:]))
				}
			}
			return 0
		}
		i += 2 + n
	}
	return 0
}

// ieTarget is a resize's size: a 0 keeps the aspect ratio from the other.
func ieTarget(a ieAction, w, h int) (int, int) {
	tw, th := a.Width, a.Height
	if tw <= 0 && th <= 0 {
		return w, h
	}
	if tw <= 0 {
		tw = max(1, int(math.Round(float64(w)*float64(th)/float64(h))))
	}
	if th <= 0 {
		th = max(1, int(math.Round(float64(h)*float64(tw)/float64(w))))
	}
	return tw, th
}

// ieIntersect is the rectangle within the image, and whether any of it is.
func ieIntersect(x, y, w, h int, img *image.RGBA) (image.Rectangle, bool) {
	if w <= 0 || h <= 0 {
		return image.Rectangle{}, false
	}
	r := image.Rect(x, y, x+w, y+h).Intersect(img.Rect)
	return r, !r.Empty()
}

// ieApply does the actions on img in order.
func ieApply(actions []ieAction, img *image.RGBA) (*image.RGBA, error) {
	for _, act := range actions {
		w, h := img.Rect.Dx(), img.Rect.Dy()
		switch act.Op {
		case "resize":
			tw, th := ieTarget(act, w, h)
			if tw == w && th == h {
				continue
			}
			if tw*th > 400_000_000 {
				return nil, fmt.Errorf("can't make a %d×%d image", tw, th)
			}
			img = ieResize(img, tw, th)
		case "crop":
			r, ok := ieIntersect(act.X, act.Y, act.Width, act.Height, img)
			if !ok {
				return nil, fmt.Errorf("the crop is outside the %d×%d image", w, h)
			}
			out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
			for y := range r.Dy() {
				copy(out.Pix[y*out.Stride:y*out.Stride+r.Dx()*4], img.Pix[img.PixOffset(r.Min.X, r.Min.Y+y):])
			}
			img = out
		case "rotate":
			img = ieRotate(img, act.Degrees)
		case "flipH":
			out := image.NewRGBA(img.Rect)
			for y := range h {
				for x := range w {
					copy(out.Pix[out.PixOffset(x, y):out.PixOffset(x, y)+4], img.Pix[img.PixOffset(w-1-x, y):])
				}
			}
			img = out
		case "flipV":
			out := image.NewRGBA(img.Rect)
			for y := range h {
				copy(out.Pix[y*out.Stride:(y+1)*out.Stride], img.Pix[(h-1-y)*img.Stride:])
			}
			img = out
		case "fill":
			r, ok := ieIntersect(act.X, act.Y, act.Width, act.Height, img)
			if !ok {
				continue
			}
			c := [4]byte{byte(act.RGB >> 16), byte(act.RGB >> 8), byte(act.RGB), 255}
			for y := r.Min.Y; y < r.Max.Y; y++ {
				for x := r.Min.X; x < r.Max.X; x++ {
					copy(img.Pix[img.PixOffset(x, y):], c[:])
				}
			}
		case "pixelate":
			r, ok := ieIntersect(act.X, act.Y, act.Width, act.Height, img)
			if !ok {
				continue
			}
			iePixelate(img, r, max(act.Block, 1))
		default:
			return nil, fmt.Errorf("unknown action %s", act.Op)
		}
	}
	return img, nil
}

// iePixelate makes squares of block pixels, aligned to the image, each
// the average of all it covers; only those within r change.
func iePixelate(img *image.RGBA, r image.Rectangle, block int) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	for by := r.Min.Y / block * block; by < r.Max.Y; by += block {
		by1 := min(by+block, h)
		for bx := r.Min.X / block * block; bx < r.Max.X; bx += block {
			bx1 := min(bx+block, w)
			var sum [4]int
			for y := by; y < by1; y++ {
				p := img.Pix[y*img.Stride:]
				for x := bx; x < bx1; x++ {
					for k := range 4 {
						sum[k] += int(p[x*4+k])
					}
				}
			}
			n := (by1 - by) * (bx1 - bx)
			for y := max(by, r.Min.Y); y < min(by1, r.Max.Y); y++ {
				p := img.Pix[y*img.Stride:]
				for x := max(bx, r.Min.X); x < min(bx1, r.Max.X); x++ {
					for k := range 4 {
						p[x*4+k] = byte(sum[k] / n)
					}
				}
			}
		}
	}
}

// catmullRom is the resampling kernel (support 2).
func catmullRom(x float64) float64 {
	x = math.Abs(x)
	switch {
	case x < 1:
		return 1.5*x*x*x - 2.5*x*x + 1
	case x < 2:
		return -0.5*x*x*x + 2.5*x*x - 4*x + 2
	}
	return 0
}

// ieWeights are each output position's source pixels and weights, the
// kernel stretched as the image shrinks (so every source pixel counts).
func ieWeights(src, dst int) (starts []int, weights [][]float32) {
	scale := float64(src) / float64(dst)
	support := 2 * max(scale, 1)
	starts, weights = make([]int, dst), make([][]float32, dst)
	for i := range dst {
		center := (float64(i)+0.5)*scale - 0.5
		lo, hi := int(math.Ceil(center-support)), int(math.Floor(center+support))
		lo, hi = max(lo, 0), min(hi, src-1)
		ws := make([]float32, hi-lo+1)
		var sum float64
		for j := lo; j <= hi; j++ {
			wt := catmullRom((float64(j) - center) / max(scale, 1))
			ws[j-lo] = float32(wt)
			sum += wt
		}
		if sum != 0 {
			for k := range ws {
				ws[k] /= float32(sum)
			}
		}
		starts[i], weights[i] = lo, ws
	}
	return
}

func clampByte(f float32) byte {
	switch {
	case f <= 0:
		return 0
	case f >= 255:
		return 255
	}
	return byte(f + 0.5)
}

// ieResize resamples img to w×h, rows then columns.
func ieResize(img *image.RGBA, w, h int) *image.RGBA {
	sw, sh := img.Rect.Dx(), img.Rect.Dy()
	xs, xw := ieWeights(sw, w)
	ys, yw := ieWeights(sh, h)
	tmp := make([]float32, w*sh*4)
	for y := range sh {
		row := img.Pix[y*img.Stride:]
		for x := range w {
			var acc [4]float32
			for k, wt := range xw[x] {
				p := row[(xs[x]+k)*4:]
				acc[0] += wt * float32(p[0])
				acc[1] += wt * float32(p[1])
				acc[2] += wt * float32(p[2])
				acc[3] += wt * float32(p[3])
			}
			copy(tmp[(y*w+x)*4:], acc[:])
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		o := out.Pix[y*out.Stride:]
		for x := range w {
			var acc [4]float32
			for k, wt := range yw[y] {
				p := tmp[((ys[y]+k)*w+x)*4:]
				acc[0] += wt * p[0]
				acc[1] += wt * p[1]
				acc[2] += wt * p[2]
				acc[3] += wt * p[3]
			}
			a := clampByte(acc[3])
			// Premultiplied: no channel above its alpha.
			o[x*4], o[x*4+1], o[x*4+2], o[x*4+3] = min(clampByte(acc[0]), a), min(clampByte(acc[1]), a), min(clampByte(acc[2]), a), a
		}
	}
	return out
}

// ieRotate turns img clockwise by degrees: exactly by quarters, else
// sampled bilinearly into the turned image's bounds, its corners clear.
func ieRotate(img *image.RGBA, degrees float64) *image.RGBA {
	deg := math.Mod(degrees, 360)
	if deg < 0 {
		deg += 360
	}
	if deg == 0 {
		return img
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if math.Mod(deg, 90) == 0 {
		q := int(deg / 90)
		ow, oh := w, h
		if q%2 == 1 {
			ow, oh = h, w
		}
		out := image.NewRGBA(image.Rect(0, 0, ow, oh))
		for y := range h {
			for x := range w {
				var nx, ny int
				switch q {
				case 1:
					nx, ny = h-1-y, x
				case 2:
					nx, ny = w-1-x, h-1-y
				case 3:
					nx, ny = y, w-1-x
				}
				copy(out.Pix[out.PixOffset(nx, ny):out.PixOffset(nx, ny)+4], img.Pix[img.PixOffset(x, y):])
			}
		}
		return out
	}
	t := deg * math.Pi / 180
	cosv, sinv := math.Cos(t), math.Sin(t)
	ow := int(math.Ceil(float64(w)*math.Abs(cosv) + float64(h)*math.Abs(sinv)))
	oh := int(math.Ceil(float64(w)*math.Abs(sinv) + float64(h)*math.Abs(cosv)))
	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	cx, cy := float64(w)/2, float64(h)/2
	ocx, ocy := float64(ow)/2, float64(oh)/2
	at := func(x, y int) [4]float64 {
		if x < 0 || y < 0 || x >= w || y >= h {
			return [4]float64{}
		}
		p := img.Pix[img.PixOffset(x, y):]
		return [4]float64{float64(p[0]), float64(p[1]), float64(p[2]), float64(p[3])}
	}
	for y := range oh {
		for x := range ow {
			// The output pixel's center, turned back into the source.
			dx, dy := float64(x)+0.5-ocx, float64(y)+0.5-ocy
			sx := dx*cosv + dy*sinv + cx - 0.5
			sy := -dx*sinv + dy*cosv + cy - 0.5
			x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
			fx, fy := sx-float64(x0), sy-float64(y0)
			if x0 < -1 || y0 < -1 || x0 >= w || y0 >= h {
				continue
			}
			a, b, c, d := at(x0, y0), at(x0+1, y0), at(x0, y0+1), at(x0+1, y0+1)
			o := out.Pix[out.PixOffset(x, y):]
			for k := range 4 {
				v := (a[k]*(1-fx)+b[k]*fx)*(1-fy) + (c[k]*(1-fx)+d[k]*fx)*fy
				o[k] = byte(math.Round(v))
			}
		}
	}
	return out
}

// ieWrite writes the image to the caches: PNG, or JPEG on white (it has
// no transparency).
func ieWrite(img *image.RGBA, r *ieRequest, dir string) (map[string]any, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	ext := ".jpg"
	if r.Format == "png" {
		ext = ".png"
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
	} else {
		flat := image.NewRGBA(img.Rect)
		for i := 0; i < len(img.Pix); i += 4 {
			a := 255 - uint32(img.Pix[i+3])
			flat.Pix[i] = byte(uint32(img.Pix[i]) + a)
			flat.Pix[i+1] = byte(uint32(img.Pix[i+1]) + a)
			flat.Pix[i+2] = byte(uint32(img.Pix[i+2]) + a)
			flat.Pix[i+3] = 255
		}
		q := int(math.Round(*r.Quality * 100))
		if err := jpeg.Encode(&buf, flat, &jpeg.Options{Quality: min(max(q, 1), 100)}); err != nil {
			return nil, err
		}
	}
	path := filepath.Join(dir, randomName()+ext)
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		return nil, fmt.Errorf("can't write %s", path)
	}
	return map[string]any{"path": path, "width": img.Rect.Dx(), "height": img.Rect.Dy()}, nil
}

// ieVideoFrame is the video's frame at seconds, upright: GStreamer
// decodes, seeks exactly, and gives the frame it prerolls.
func ieVideoFrame(source, assets string, seconds float64) (*image.RGBA, error) {
	if err := LoadGstApp(); err != nil {
		return nil, errors.New("video frames need GStreamer: " + err.Error())
	}
	uri := source
	if p := ieLocal(source, assets); p != "" {
		if _, err := os.Stat(p); err != nil {
			return nil, errNoImage
		}
		abs, _ := filepath.Abs(p)
		uri = (&url.URL{Scheme: "file", Path: abs}).String()
	}
	const desc = "uridecodebin name=source ! videoconvert ! videoflip method=automatic ! videoconvert ! video/x-raw,format=RGBA ! appsink name=frames sync=false"
	var gerr Ptr
	pipe := Gst.ParseLaunch(desc, &gerr)
	if pipe == 0 {
		return nil, GError(gerr)
	}
	if gerr != 0 {
		ErrorFree(gerr)
	}
	ObjectRefSink(pipe)
	defer func() {
		Gst.ElementSetState(pipe, GstStateNull)
		Gst.ObjectUnref(pipe)
	}()
	if src := Gst.BinGetByName(pipe, "source"); src != 0 {
		SetStringProperty(src, "uri", uri) // as it is: no quoting for gst-launch's syntax
		Gst.ObjectUnref(src)
	}
	sink := Gst.BinGetByName(pipe, "frames")
	if sink == 0 {
		return nil, errNoImage
	}
	defer Gst.ObjectUnref(sink)
	const wait = 15_000_000_000 // ns
	var state, pending int32
	Gst.ElementSetState(pipe, GstStatePaused)
	if Gst.ElementGetState(pipe, &state, &pending, wait) == GstStateChangeFailure {
		return nil, errNoImage
	}
	if seconds > 0 {
		if !Gst.SeekSimple(pipe, GstFormatTime, GstSeekFlagFlush|GstSeekFlagAccurate, int64(seconds*1e9)) {
			return nil, errNoImage
		}
		if Gst.ElementGetState(pipe, &state, &pending, wait) == GstStateChangeFailure {
			return nil, errNoImage
		}
	}
	sample := Call(FnAppSinkTryPullPreroll, sink, wait)
	if sample == 0 {
		return nil, errNoImage
	}
	defer Call(FnMiniObjectUnref, sample)
	var img *image.RGBA
	GstSampleFrame(sample, func(data Ptr, w, h, stride int) {
		img = image.NewRGBA(image.Rect(0, 0, w, h))
		copy(img.Pix, GoBytes(data, stride*h))
		// Video is opaque.
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	})
	if img == nil {
		return nil, errNoImage
	}
	return img, nil
}
