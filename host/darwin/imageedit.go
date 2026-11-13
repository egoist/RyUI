//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/sdk/imageedit on a Mac, as on iOS: "edit", "size" and "videoFrame",
// each with JSON (source, actions, format, quality, at in ms), answer
// JSON (path, width, height) or fail "no image". ImageIO decodes (HEIC,
// JPEG, PNG, WebP...) upright, at the size the first resize needs; Core
// Graphics does the actions; ImageIO writes JPEG or PNG. Off the main
// thread.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["imageEdit"] = h.imageEditModule()
	})
}

// ieAPI is Core Graphics' and ImageIO's functions, bound once.
type ieAPI struct {
	err error

	bitmapCreate      func(data uintptr, w, h, bpc, bpr, space uintptr, info uint32) uintptr
	spaceWithName     func(name uintptr) uintptr
	spaceGray         func() uintptr
	imageSpace        func(img uintptr) uintptr
	drawImage         func(c uintptr, r Rect, img uintptr)
	interpolation     func(c uintptr, q int32)
	bitmapImage       func(c uintptr) uintptr
	bitmapData        func(c uintptr) uintptr
	bitmapRow         func(c uintptr) uintptr
	antialias         func(c uintptr, on bool)
	colorSRGB         func(r, g, b, a float64) uintptr
	setFill           func(c, color uintptr)
	fillRect          func(c uintptr, r Rect)
	translate         func(c uintptr, x, y float64)
	rotate            func(c uintptr, a float64)
	scale             func(c uintptr, x, y float64)
	imageInRect       func(img uintptr, r Rect) uintptr
	imageWidth        func(img uintptr) uintptr
	imageHeight       func(img uintptr) uintptr
	alphaInfo         func(img uintptr) uint32
	sourceWithURL     func(url, opts uintptr) uintptr
	sourceWithData    func(data, opts uintptr) uintptr
	sourceCount       func(src uintptr) uintptr
	sourceProperties  func(src, i, opts uintptr) uintptr
	sourceThumbnail   func(src, i, opts uintptr) uintptr
	destinationCreate func(url, typ, count, opts uintptr) uintptr
	destinationAdd    func(d, img, props uintptr)
	destinationEnd    func(d uintptr) bool

	sRGB                                         ID
	pixelWidth, pixelHeight, orientation         ID
	thumbAlways, thumbTransform, thumbMax, cache ID
	lossyQuality                                 ID
}

var ie = sync.OnceValue(func() *ieAPI {
	a := &ieAPI{}
	cg, io := CoreGraphics(), framework("ImageIO")
	if cg == 0 || io == 0 {
		a.err = errors.New("ImageIO isn't there")
		return a
	}
	for _, f := range []struct {
		fn   any
		lib  uintptr
		name string
	}{
		{&a.bitmapCreate, cg, "CGBitmapContextCreate"}, {&a.spaceWithName, cg, "CGColorSpaceCreateWithName"},
		{&a.spaceGray, cg, "CGColorSpaceCreateDeviceGray"}, {&a.imageSpace, cg, "CGImageGetColorSpace"},
		{&a.drawImage, cg, "CGContextDrawImage"}, {&a.interpolation, cg, "CGContextSetInterpolationQuality"},
		{&a.bitmapImage, cg, "CGBitmapContextCreateImage"}, {&a.bitmapData, cg, "CGBitmapContextGetData"},
		{&a.bitmapRow, cg, "CGBitmapContextGetBytesPerRow"}, {&a.antialias, cg, "CGContextSetShouldAntialias"},
		{&a.colorSRGB, cg, "CGColorCreateSRGB"}, {&a.setFill, cg, "CGContextSetFillColorWithColor"},
		{&a.fillRect, cg, "CGContextFillRect"}, {&a.translate, cg, "CGContextTranslateCTM"},
		{&a.rotate, cg, "CGContextRotateCTM"}, {&a.scale, cg, "CGContextScaleCTM"},
		{&a.imageInRect, cg, "CGImageCreateWithImageInRect"}, {&a.imageWidth, cg, "CGImageGetWidth"},
		{&a.imageHeight, cg, "CGImageGetHeight"}, {&a.alphaInfo, cg, "CGImageGetAlphaInfo"},
		{&a.sourceWithURL, io, "CGImageSourceCreateWithURL"}, {&a.sourceWithData, io, "CGImageSourceCreateWithData"},
		{&a.sourceCount, io, "CGImageSourceGetCount"}, {&a.sourceProperties, io, "CGImageSourceCopyPropertiesAtIndex"},
		{&a.sourceThumbnail, io, "CGImageSourceCreateThumbnailAtIndex"}, {&a.destinationCreate, io, "CGImageDestinationCreateWithURL"},
		{&a.destinationAdd, io, "CGImageDestinationAddImage"}, {&a.destinationEnd, io, "CGImageDestinationFinalize"},
	} {
		purego.RegisterLibFunc(f.fn, f.lib, f.name)
	}
	a.sRGB = Const(cg, "kCGColorSpaceSRGB")
	a.pixelWidth, a.pixelHeight = Const(io, "kCGImagePropertyPixelWidth"), Const(io, "kCGImagePropertyPixelHeight")
	a.orientation = Const(io, "kCGImagePropertyOrientation")
	a.thumbAlways = Const(io, "kCGImageSourceCreateThumbnailFromImageAlways")
	a.thumbTransform = Const(io, "kCGImageSourceCreateThumbnailWithTransform")
	a.thumbMax = Const(io, "kCGImageSourceThumbnailMaxPixelSize")
	a.cache = Const(io, "kCGImageSourceShouldCacheImmediately")
	a.lossyQuality = Const(io, "kCGImageDestinationLossyCompressionQuality")
	return a
})

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
			h.offMain(func() ([]byte, error) {
				if a := ie(); a.err != nil {
					return nil, a.err
				}
				out, err := f(&r)
				if err != nil {
					return nil, err
				}
				return json.Marshal(out)
			}, reply)
		}
	}
	return map[string]method{
		"size": run(func(r *ieRequest) (map[string]any, error) {
			src, err := ieSource(h.ieURL(r.Source))
			if err != nil {
				return nil, err
			}
			defer CFRelease(src)
			w, ht := ieUprightSize(src)
			return map[string]any{"path": "", "width": w, "height": ht}, nil
		}),
		"edit": run(func(r *ieRequest) (map[string]any, error) {
			src, err := ieSource(h.ieURL(r.Source))
			if err != nil {
				return nil, err
			}
			defer CFRelease(src)
			img, err := ieDecode(src, r.Actions)
			if err != nil {
				return nil, err
			}
			if img, err = ieApply(r.Actions, img); err != nil {
				return nil, err
			}
			defer CFRelease(img)
			return ieWrite(img, r)
		}),
		"videoFrame": run(func(r *ieRequest) (map[string]any, error) {
			img, err := ieFrame(h.ieURL(r.Source), r.At/1000)
			if err != nil {
				return nil, err
			}
			if img, err = ieApply(r.Actions, img); err != nil {
				return nil, err
			}
			defer CFRelease(img)
			return ieWrite(img, r)
		}),
	}
}

// ieURL is a source's URL, as ui.Image reads it: a URL, a file's path,
// or a file of the app's assets.
func (h *Host) ieURL(source string) ID {
	switch {
	case strings.HasPrefix(source, "http://"), strings.HasPrefix(source, "https://"), strings.HasPrefix(source, "file://"):
		return URL(source)
	case filepath.IsAbs(source):
		return FileURL(source)
	}
	abs, _ := filepath.Abs(filepath.Join(h.assets, filepath.FromSlash(source)))
	return FileURL(abs)
}

// ieSource opens an image (+1), downloading a remote one first (this is a
// thread of its own).
func ieSource(u ID) (uintptr, error) {
	a := ie()
	if u == 0 {
		return 0, errNoImage
	}
	if SendBool(u, "isFileURL") {
		s := a.sourceWithURL(uintptr(u), 0)
		if s == 0 || a.sourceCount(s) == 0 {
			CFRelease(s)
			return 0, errNoImage
		}
		return s, nil
	}
	data, err := ieDownload(u)
	if err != nil {
		return 0, err
	}
	s := a.sourceWithData(uintptr(data), 0)
	if s == 0 || a.sourceCount(s) == 0 {
		CFRelease(s)
		return 0, errNoImage
	}
	return s, nil
}

// ieDownload fetches a URL with NSURLSession, waiting for it (off the
// main thread). The NSData is autoreleased.
func ieDownload(u ID) (ID, error) {
	type result struct {
		data ID
		err  error
	}
	done := make(chan result, 1)
	var block objc.Block
	block = NewBlock(func(_ objc.Block, data, resp, nserr ID) {
		var r result
		if resp != 0 && SendBool(resp, "isKindOfClass:", uintptr(Class("NSHTTPURLResponse"))) {
			if code := SendInt(resp, "statusCode"); code < 200 || code >= 300 {
				r.err = fmt.Errorf("HTTP %d", code)
			}
		}
		if r.err == nil && nserr != 0 {
			r.err = errorText(nserr, "")
		}
		r.data = Retain(data)
		done <- r
	})
	defer block.Release()
	task := Send(Send(Class("NSURLSession"), "sharedSession"), "dataTaskWithURL:completionHandler:", uintptr(u), uintptr(block))
	Send(task, "resume")
	r := <-done
	Autorelease(r.data)
	if r.err != nil {
		return 0, r.err
	}
	if r.data == 0 {
		return 0, errNoImage
	}
	return r.data, nil
}

// ieUprightSize is the image's size upright: EXIF orientations 5 to 8
// turn it a quarter.
func ieUprightSize(src uintptr) (int, int) {
	a := ie()
	p := ID(a.sourceProperties(src, 0, 0))
	if p == 0 {
		return 0, 0
	}
	defer CFRelease(uintptr(p))
	num := func(k ID) int {
		if v := Send(p, "objectForKey:", uintptr(k)); v != 0 {
			return SendInt(v, "integerValue")
		}
		return 0
	}
	w, h, o := num(a.pixelWidth), num(a.pixelHeight), num(a.orientation)
	if o >= 5 {
		return h, w
	}
	return w, h
}

// ieThumbnail decodes the image upright, its longer side at most max
// (+1).
func ieThumbnail(src uintptr, maxSide int) uintptr {
	a := ie()
	opts := Dict(a.thumbAlways, NumberBool(true), a.thumbTransform, NumberBool(true),
		a.thumbMax, NumberInt(maxSide), a.cache, NumberBool(true))
	return a.sourceThumbnail(src, 0, uintptr(opts))
}

// ieDecode decodes the image upright, no larger than the first action
// needs (a resize first: decoded near its size, which saves memory).
func ieDecode(src uintptr, actions []ieAction) (uintptr, error) {
	w, h := ieUprightSize(src)
	if w <= 0 || h <= 0 {
		return 0, errNoImage
	}
	maxSide := max(w, h)
	if len(actions) > 0 && actions[0].Op == "resize" {
		tw, th := ieTarget(actions[0], w, h)
		maxSide = min(maxSide, max(tw, th))
	}
	img := ieThumbnail(src, maxSide)
	if img == 0 {
		return 0, errNoImage
	}
	return img, nil
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

// Bitmap layouts (CGImageAlphaInfo).
const (
	ieAlphaNone              = 0
	ieAlphaPremultipliedLast = 1
	ieAlphaNoneSkipLast      = 5
	ieAlphaNoneSkipFirst     = 6
)

// ieContext is an RGBA bitmap context of w×h pixels (+1), in space or,
// if it can't be, sRGB.
func ieContext(w, h int, opaque bool, space uintptr) (uintptr, error) {
	a := ie()
	info := uint32(ieAlphaPremultipliedLast)
	if opaque {
		info = ieAlphaNoneSkipLast
	}
	if w <= 0 || h <= 0 || w*h > 400_000_000 {
		return 0, fmt.Errorf("can't make a %d×%d image", w, h)
	}
	var c uintptr
	if space != 0 {
		c = a.bitmapCreate(0, uintptr(w), uintptr(h), 8, 0, space, info)
	}
	if c == 0 {
		srgb := a.spaceWithName(uintptr(a.sRGB))
		c = a.bitmapCreate(0, uintptr(w), uintptr(h), 8, 0, srgb, info)
		CFRelease(srgb)
	}
	if c == 0 {
		return 0, fmt.Errorf("can't make a %d×%d image", w, h)
	}
	a.interpolation(c, 3) // high
	return c, nil
}

// ieImage is the context's image (+1); the context is released.
func ieImage(c uintptr) uintptr {
	img := ie().bitmapImage(c)
	CFRelease(c)
	return img
}

func ieSize(img uintptr) (int, int) {
	a := ie()
	return int(a.imageWidth(img)), int(a.imageHeight(img))
}

func ieRect(x, y, w, h float64) Rect {
	return Rect{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: h}}
}

// ieIntersect is r within 0, 0, w×h, and whether any of it is.
func ieIntersect(r Rect, w, h int) (Rect, bool) {
	x0, y0 := max(r.Origin.X, 0), max(r.Origin.Y, 0)
	x1, y1 := min(r.Origin.X+r.Size.Width, float64(w)), min(r.Origin.Y+r.Size.Height, float64(h))
	if x1 < x0+1 || y1 < y0+1 || r.Size.Width <= 0 || r.Size.Height <= 0 {
		return Rect{}, false
	}
	return ieRect(x0, y0, x1-x0, y1-y0), true
}

// ieApply does the actions on img, which it takes (released), and gives
// the result (+1).
func ieApply(actions []ieAction, img uintptr) (out uintptr, err error) {
	a := ie()
	// Fills and pixelations in a row share one drawing, in the image's own
	// color space: a screenshot in Display P3 stays in it.
	var canvas uintptr
	finish := func() {
		if canvas != 0 {
			if done := ieImage(canvas); done != 0 {
				CFRelease(img)
				img = done
			}
			canvas = 0
		}
	}
	defer func() {
		if err != nil {
			if canvas != 0 {
				CFRelease(canvas)
			}
			CFRelease(img)
		}
	}()
	replace := func(c uintptr) {
		next := ieImage(c)
		CFRelease(img)
		img = next
	}
	for _, act := range actions {
		if act.Op != "fill" && act.Op != "pixelate" {
			finish()
		}
		w, h := ieSize(img)
		switch act.Op {
		case "fill", "pixelate":
			if canvas == 0 {
				c, err := ieContext(w, h, false, a.imageSpace(img))
				if err != nil {
					return 0, err
				}
				a.drawImage(c, ieRect(0, 0, float64(w), float64(h)), img)
				canvas = c
			}
			r, ok := ieIntersect(ieRect(float64(act.X), float64(act.Y), float64(act.Width), float64(act.Height)), w, h)
			if !ok {
				continue
			}
			if act.Op == "fill" {
				a.antialias(canvas, false)
				rgb := act.RGB
				col := a.colorSRGB(float64(rgb>>16&255)/255, float64(rgb>>8&255)/255, float64(rgb&255)/255, 1)
				a.setFill(canvas, col)
				CFRelease(col)
				// y is up
				a.fillRect(canvas, ieRect(r.Origin.X, float64(h)-(r.Origin.Y+r.Size.Height), r.Size.Width, r.Size.Height))
			} else {
				iePixelate(canvas, w, h, r, max(act.Block, 1))
			}
		case "resize":
			tw, th := ieTarget(act, w, h)
			if tw == w && th == h {
				continue
			}
			c, err := ieContext(tw, th, false, 0)
			if err != nil {
				return 0, err
			}
			a.drawImage(c, ieRect(0, 0, float64(tw), float64(th)), img)
			replace(c)
		case "crop":
			r, ok := ieIntersect(ieRect(float64(act.X), float64(act.Y), float64(act.Width), float64(act.Height)), w, h)
			var cropped uintptr
			if ok {
				cropped = a.imageInRect(img, r)
			}
			if cropped == 0 {
				return 0, fmt.Errorf("the crop is outside the %d×%d image", w, h)
			}
			CFRelease(img)
			img = cropped
		case "rotate":
			deg := math.Mod(act.Degrees, 360)
			if deg == 0 {
				continue
			}
			t := -deg * math.Pi / 180 // clockwise on screen; Core Graphics' y is up
			cosv, sinv := math.Abs(math.Cos(t)), math.Abs(math.Sin(t))
			nw, nh := float64(w)*cosv+float64(h)*sinv, float64(w)*sinv+float64(h)*cosv
			if math.Mod(deg, 90) == 0 {
				nw, nh = math.Round(nw), math.Round(nh)
			} else {
				nw, nh = math.Ceil(nw), math.Ceil(nh)
			}
			c, err := ieContext(int(nw), int(nh), false, 0)
			if err != nil {
				return 0, err
			}
			a.translate(c, nw/2, nh/2)
			a.rotate(c, t)
			a.drawImage(c, ieRect(-float64(w)/2, -float64(h)/2, float64(w), float64(h)), img)
			replace(c)
		case "flipH", "flipV":
			c, err := ieContext(w, h, false, 0)
			if err != nil {
				return 0, err
			}
			if act.Op == "flipH" {
				a.translate(c, float64(w), 0)
				a.scale(c, -1, 1)
			} else {
				a.translate(c, 0, float64(h))
				a.scale(c, 1, -1)
			}
			a.drawImage(c, ieRect(0, 0, float64(w), float64(h)), img)
			replace(c)
		default:
			return 0, fmt.Errorf("unknown action %s", act.Op)
		}
	}
	finish()
	return img, nil
}

// iePixels is a bitmap context's memory, top row first, and its row's
// length in bytes.
func iePixels(c uintptr, h int) ([]byte, int) {
	a := ie()
	p, row := a.bitmapData(c), int(a.bitmapRow(c))
	if p == 0 {
		return nil, 0
	}
	return unsafe.Slice(*(**byte)(unsafe.Pointer(&p)), row*h), row
}

// iePixelate makes squares of block pixels, aligned to the image, each
// the average of all it covers; only those within r change.
func iePixelate(c uintptr, w, h int, r Rect, block int) {
	data, row := iePixels(c, h)
	if data == nil {
		return
	}
	rx0, ry0 := int(r.Origin.X), int(r.Origin.Y)
	rx1, ry1 := int(r.Origin.X+r.Size.Width), int(r.Origin.Y+r.Size.Height)
	for by := ry0 / block * block; by < ry1; by += block {
		by1 := min(by+block, h)
		for bx := rx0 / block * block; bx < rx1; bx += block {
			bx1 := min(bx+block, w)
			var sum [4]int
			for y := by; y < by1; y++ {
				p := data[y*row:]
				for x := bx; x < bx1; x++ {
					for k := range 4 {
						sum[k] += int(p[x*4+k])
					}
				}
			}
			n := (by1 - by) * (bx1 - bx)
			for y := max(by, ry0); y < min(by1, ry1); y++ {
				p := data[y*row:]
				for x := max(bx, rx0); x < min(bx1, rx1); x++ {
					for k := range 4 {
						p[x*4+k] = byte(sum[k] / n)
					}
				}
			}
		}
	}
}

// ieWrite writes the image to the caches: PNG, or JPEG on white (it has
// no transparency).
func ieWrite(img uintptr, r *ieRequest) (map[string]any, error) {
	a := ie()
	png := r.Format == "png"
	w, h := ieSize(img)
	if !png {
		switch a.alphaInfo(img) {
		case ieAlphaNone, ieAlphaNoneSkipLast, ieAlphaNoneSkipFirst:
		default:
			c, err := ieContext(w, h, true, 0)
			if err != nil {
				return nil, err
			}
			white := a.colorSRGB(1, 1, 1, 1)
			a.setFill(c, white)
			CFRelease(white)
			a.fillRect(c, ieRect(0, 0, float64(w), float64(h)))
			a.drawImage(c, ieRect(0, 0, float64(w), float64(h)), img)
			opaque := ieImage(c)
			defer CFRelease(opaque)
			img = opaque
		}
	}
	ext, typ := ".jpg", "public.jpeg"
	if png {
		ext, typ = ".png", "public.png"
	}
	path, err := cacheFile("asuka-images", ext)
	if err != nil {
		return nil, err
	}
	d := a.destinationCreate(uintptr(FileURL(path)), uintptr(String(typ)), 1, 0)
	if d == 0 {
		return nil, fmt.Errorf("can't write %s", path)
	}
	defer CFRelease(d)
	var props ID
	if !png {
		props = Dict(a.lossyQuality, NumberFloat(*r.Quality))
	}
	a.destinationAdd(d, img, uintptr(props))
	if !a.destinationEnd(d) {
		return nil, fmt.Errorf("can't write %s", path)
	}
	return map[string]any{"path": path, "width": w, "height": h}, nil
}

var ieCopyFrame func(obj ID, sel SEL, t cmTime, actual uintptr, err *ID) uintptr
var ieSetTolerance func(obj ID, sel SEL, t cmTime)

// ieFrame is the video's frame at seconds (exactly), upright (+1).
func ieFrame(u ID, seconds float64) (uintptr, error) {
	if u == 0 {
		return 0, errNoImage
	}
	if loadAVFoundation() == 0 {
		return 0, errors.New("AVFoundation isn't there")
	}
	ieFrameOnce.Do(func() {
		purego.RegisterFunc(&ieCopyFrame, MsgSendAddr())
		purego.RegisterFunc(&ieSetTolerance, MsgSendAddr())
	})
	asset := Send(Class("AVURLAsset"), "URLAssetWithURL:options:", uintptr(u), 0)
	g := Send(Class("AVAssetImageGenerator"), "assetImageGeneratorWithAsset:", uintptr(asset))
	Send(g, "setAppliesPreferredTrackTransform:", 1)
	zero := cmTime{Timescale: 1, Flags: 1}
	ieSetTolerance(g, Sel("setRequestedTimeToleranceBefore:"), zero)
	ieSetTolerance(g, Sel("setRequestedTimeToleranceAfter:"), zero)
	var nserr ID
	img := ieCopyFrame(g, Sel("copyCGImageAtTime:actualTime:error:"),
		cmTime{Value: int64(math.Round(max(seconds, 0) * 600)), Timescale: 600, Flags: 1}, 0, &nserr)
	if img != 0 {
		return img, nil
	}
	if nserr != 0 && GoString(Send(nserr, "domain")) == "AVFoundationErrorDomain" {
		switch SendInt(nserr, "code") {
		case -11828, -11821, -11832: // file format not recognized, decode failed, no image at time
			return 0, errNoImage
		}
	}
	return 0, errorText(nserr, "no image")
}

var ieFrameOnce sync.Once
