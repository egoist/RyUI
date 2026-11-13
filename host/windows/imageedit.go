//go:build windows

package windows

import (
	"bytes"
	"encoding/json"
	"errors"
	stdimage "image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"

	. "github.com/egoist/asuka/internal/windows"
)

// asuka/imageedit on Windows: "edit", "size" and "videoFrame", each with
// JSON (source, actions, format, quality, at in ms), answer JSON (path,
// width, height) or fail "no image". Windows's codecs (WIC) decode what
// the system reads (HEIC, WebP where their extensions are installed),
// upright as their EXIF orientation says; Media Foundation takes a
// video's frame; the actions are Go's; Go writes JPEG or PNG. Off the
// main thread.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["imageEdit"] = map[string]asyncMethod{
			"edit":       func(args []byte, reply func([]byte, error)) { h.imageEdit("edit", args, reply) },
			"size":       func(args []byte, reply func([]byte, error)) { h.imageEdit("size", args, reply) },
			"videoFrame": func(args []byte, reply func([]byte, error)) { h.imageEdit("videoFrame", args, reply) },
		}
	})
}

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
	Quality float64    `json:"quality"`
	At      int64      `json:"at"`
}

var errNoImage = errors.New("no image")

func (h *Host) imageEdit(method string, args []byte, reply func([]byte, error)) {
	var r ieRequest
	if err := json.Unmarshal(args, &r); err != nil || r.Source == "" {
		reply(nil, errors.New("bad arguments"))
		return
	}
	src := r.Source
	if method == "videoFrame" && (len(src) > 7 && (src[:7] == "http://" || src[:8] == "https://")) {
		// Media Foundation reads a remote video as far as the frame needs.
		h.offMain(func() ([]byte, error) { return h.ieRun(method, src, r) }, reply)
		return
	}
	h.localFiles([]source{{Source: src}}, func(paths []string, err error) {
		if err != nil {
			reply(nil, errNoImage)
			return
		}
		h.offMain(func() ([]byte, error) { return h.ieRun(method, paths[0], r) }, reply)
	})
}

// ieRun does the request on a thread of COM's.
func (h *Host) ieRun(method, path string, r ieRequest) (out []byte, err error) {
	WithCOM(func() {
		if method == "size" {
			w, ht, e := ImageFileSize(path)
			if e != nil {
				if f, ferr := os.Open(path); ferr == nil {
					c, _, cerr := stdimage.DecodeConfig(f)
					f.Close()
					w, ht, e = c.Width, c.Height, cerr
				}
			}
			if e != nil {
				err = errNoImage
				return
			}
			out, err = json.Marshal(map[string]any{"path": "", "width": w, "height": ht})
			return
		}
		var img *stdimage.NRGBA
		if method == "videoFrame" {
			img, err = VideoFrame(path, r.At)
		} else {
			img, err = DecodeImageFile(path, 0)
			if err != nil {
				img, err = goDecode(path)
			}
		}
		if err != nil {
			err = errNoImage
			return
		}
		for _, a := range r.Actions {
			img = ieApply(img, a)
		}
		out, err = h.ieWrite(img, r.Format, r.Quality)
	})
	return out, err
}

// goDecode decodes an image Go's decoders read (WIC failing).
func goDecode(path string) (*stdimage.NRGBA, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := stdimage.Decode(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return toNRGBA(img), nil
}

func toNRGBA(img stdimage.Image) *stdimage.NRGBA {
	if n, ok := img.(*stdimage.NRGBA); ok && n.Rect.Min == (stdimage.Point{}) {
		return n
	}
	b := img.Bounds()
	out := stdimage.NewNRGBA(stdimage.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.SetNRGBA(x, y, color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA))
		}
	}
	return out
}

// ieWrite writes the image to the caches as JPEG (quality 0 to 1;
// transparency on white) or PNG.
func (h *Host) ieWrite(img *stdimage.NRGBA, format string, quality float64) ([]byte, error) {
	dir := filepath.Join(h.cacheDir(), "asuka-imageedit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	ext := ".jpg"
	if format == "png" {
		ext = ".png"
	}
	f, err := os.CreateTemp(dir, "*"+ext)
	if err != nil {
		return nil, err
	}
	if format == "png" {
		err = png.Encode(f, img)
	} else {
		if quality <= 0 || quality > 1 {
			quality = 0.9
		}
		err = jpeg.Encode(f, onWhite(img), &jpeg.Options{Quality: max(1, int(math.Round(quality*100)))})
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return nil, err
	}
	b := img.Bounds()
	return json.Marshal(map[string]any{"path": f.Name(), "width": b.Dx(), "height": b.Dy()})
}

// onWhite is the image over white (JPEG has no transparency).
func onWhite(img *stdimage.NRGBA) *stdimage.RGBA {
	out := stdimage.NewRGBA(img.Rect)
	for i := 0; i < len(img.Pix); i += 4 {
		a := uint32(img.Pix[i+3])
		for c := 0; c < 3; c++ {
			out.Pix[i+c] = uint8((uint32(img.Pix[i+c])*a + 255*(255-a) + 127) / 255)
		}
		out.Pix[i+3] = 255
	}
	return out
}

// ieApply does an action.
func ieApply(img *stdimage.NRGBA, a ieAction) *stdimage.NRGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	clip := func() stdimage.Rectangle {
		return stdimage.Rect(a.X, a.Y, a.X+a.Width, a.Y+a.Height).Intersect(img.Rect)
	}
	switch a.Op {
	case "resize":
		tw, th := a.Width, a.Height
		switch {
		case tw <= 0 && th <= 0:
			return img
		case tw <= 0:
			tw = max(1, int(math.Round(float64(w)*float64(th)/float64(h))))
		case th <= 0:
			th = max(1, int(math.Round(float64(h)*float64(tw)/float64(w))))
		}
		return resample(img, tw, th)
	case "crop":
		r := clip()
		if r.Empty() {
			return img
		}
		out := stdimage.NewNRGBA(stdimage.Rect(0, 0, r.Dx(), r.Dy()))
		for y := 0; y < r.Dy(); y++ {
			copy(out.Pix[y*out.Stride:], img.Pix[(r.Min.Y+y)*img.Stride+r.Min.X*4:(r.Min.Y+y)*img.Stride+r.Max.X*4])
		}
		return out
	case "flipH":
		return Orient(img, 2)
	case "flipV":
		return Orient(img, 4)
	case "rotate":
		d := math.Mod(a.Degrees, 360)
		if d < 0 {
			d += 360
		}
		switch d {
		case 0:
			return img
		case 90:
			return Orient(img, 6)
		case 180:
			return Orient(img, 3)
		case 270:
			return Orient(img, 8)
		}
		return rotate(img, d)
	case "fill":
		r := clip()
		c := [4]uint8{uint8(a.RGB >> 16), uint8(a.RGB >> 8), uint8(a.RGB), 255}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				copy(img.Pix[y*img.Stride+x*4:], c[:])
			}
		}
		return img
	case "pixelate":
		r := clip()
		b := max(a.Block, 1)
		// Squares aligned to the image: each the average of what's under
		// it, painted where it meets the rectangle.
		for by := r.Min.Y / b * b; by < r.Max.Y; by += b {
			for bx := r.Min.X / b * b; bx < r.Max.X; bx += b {
				sq := stdimage.Rect(bx, by, bx+b, by+b).Intersect(img.Rect)
				var sum [4]uint64
				for y := sq.Min.Y; y < sq.Max.Y; y++ {
					for x := sq.Min.X; x < sq.Max.X; x++ {
						p := img.Pix[y*img.Stride+x*4:]
						for c := 0; c < 4; c++ {
							sum[c] += uint64(p[c])
						}
					}
				}
				n := uint64(sq.Dx() * sq.Dy())
				avg := [4]uint8{uint8(sum[0] / n), uint8(sum[1] / n), uint8(sum[2] / n), uint8(sum[3] / n)}
				paint := sq.Intersect(r)
				for y := paint.Min.Y; y < paint.Max.Y; y++ {
					for x := paint.Min.X; x < paint.Max.X; x++ {
						copy(img.Pix[y*img.Stride+x*4:], avg[:])
					}
				}
			}
		}
		return img
	}
	return img
}

// resample scales the image to w×h: each output pixel the
// alpha-weighted average of the source under it (a triangle filter as
// wide as the scale, so shrinking doesn't alias), separable.
func resample(img *stdimage.NRGBA, w, h int) *stdimage.NRGBA {
	sw, sh := img.Rect.Dx(), img.Rect.Dy()
	// Premultiplied floats, one row pass then one column pass.
	src := make([]float32, sw*sh*4)
	for i := 0; i < sw*sh; i++ {
		p := img.Pix[(i/sw)*img.Stride+(i%sw)*4:]
		a := float32(p[3]) / 255
		src[i*4], src[i*4+1], src[i*4+2], src[i*4+3] = float32(p[0])*a, float32(p[1])*a, float32(p[2])*a, float32(p[3])
	}
	type tap struct {
		i int
		w float32
	}
	weights := func(dst, srcN int) [][]tap {
		scale := float64(srcN) / float64(dst)
		support := max(scale, 1)
		out := make([][]tap, dst)
		for d := 0; d < dst; d++ {
			center := (float64(d)+0.5)*scale - 0.5
			lo, hi := int(math.Floor(center-support)), int(math.Ceil(center+support))
			var ts []tap
			var sum float64
			for s := lo; s <= hi; s++ {
				x := math.Abs(float64(s)-center) / support
				if x >= 1 {
					continue
				}
				wt := 1 - x
				ts = append(ts, tap{min(max(s, 0), srcN-1), float32(wt)})
				sum += wt
			}
			for i := range ts {
				ts[i].w /= float32(sum)
			}
			out[d] = ts
		}
		return out
	}
	wx, wy := weights(w, sw), weights(h, sh)
	mid := make([]float32, w*sh*4)
	for y := 0; y < sh; y++ {
		for x := 0; x < w; x++ {
			var acc [4]float32
			for _, t := range wx[x] {
				p := src[(y*sw+t.i)*4:]
				for c := 0; c < 4; c++ {
					acc[c] += p[c] * t.w
				}
			}
			copy(mid[(y*w+x)*4:], acc[:])
		}
	}
	out := stdimage.NewNRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			var acc [4]float32
			for _, t := range wy[y] {
				p := mid[(t.i*w+x)*4:]
				for c := 0; c < 4; c++ {
					acc[c] += p[c] * t.w
				}
			}
			d := out.Pix[y*out.Stride+x*4:]
			a := acc[3]
			d[3] = clamp8(a)
			if a > 0 {
				k := 255 / a
				d[0], d[1], d[2] = clamp8(acc[0]*k), clamp8(acc[1]*k), clamp8(acc[2]*k)
			}
		}
	}
	return out
}

func clamp8(v float32) uint8 {
	return uint8(min(max(v+0.5, 0), 255))
}

// rotate turns the image clockwise by d degrees into the turned image's
// bounding box, its corners transparent, sampled bilinearly.
func rotate(img *stdimage.NRGBA, d float64) *stdimage.NRGBA {
	rad := d * math.Pi / 180
	sin, cos := math.Sin(rad), math.Cos(rad)
	sw, sh := float64(img.Rect.Dx()), float64(img.Rect.Dy())
	w := int(math.Ceil(math.Abs(sw*cos) + math.Abs(sh*sin) - 1e-9))
	h := int(math.Ceil(math.Abs(sw*sin) + math.Abs(sh*cos) - 1e-9))
	out := stdimage.NewNRGBA(stdimage.Rect(0, 0, w, h))
	cx, cy := sw/2, sh/2
	ox, oy := float64(w)/2, float64(h)/2
	at := func(x, y int) [4]float64 {
		if x < 0 || y < 0 || x >= int(sw) || y >= int(sh) {
			return [4]float64{}
		}
		p := img.Pix[y*img.Stride+x*4:]
		a := float64(p[3]) / 255
		return [4]float64{float64(p[0]) * a, float64(p[1]) * a, float64(p[2]) * a, float64(p[3])}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			// The source point (inverse rotation), pixel centers.
			dx, dy := float64(x)+0.5-ox, float64(y)+0.5-oy
			sx := dx*cos + dy*sin + cx - 0.5
			sy := -dx*sin + dy*cos + cy - 0.5
			x0, y0 := int(math.Floor(sx)), int(math.Floor(sy))
			fx, fy := sx-float64(x0), sy-float64(y0)
			var acc [4]float64
			for _, t := range [4]struct {
				x, y int
				w    float64
			}{{x0, y0, (1 - fx) * (1 - fy)}, {x0 + 1, y0, fx * (1 - fy)}, {x0, y0 + 1, (1 - fx) * fy}, {x0 + 1, y0 + 1, fx * fy}} {
				p := at(t.x, t.y)
				for c := 0; c < 4; c++ {
					acc[c] += p[c] * t.w
				}
			}
			d := out.Pix[y*out.Stride+x*4:]
			if acc[3] > 0 {
				k := 255 / acc[3]
				d[0], d[1], d[2] = clamp8(float32(acc[0]*k)), clamp8(float32(acc[1]*k)), clamp8(float32(acc[2]*k))
				d[3] = clamp8(float32(acc[3]))
			}
		}
	}
	return out
}
