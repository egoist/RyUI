package ui

import (
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/nui/internal/boot"
	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
)

// ImageMode is how an image fills its box.
type ImageMode = ops.ImageMode

const (
	ImageFit     = ops.ImageModeFit     // all of it, letterboxed
	ImageFill    = ops.ImageModeFill    // covers the box, cropped
	ImageStretch = ops.ImageModeStretch // the box's aspect ratio
	ImageCenter  = ops.ImageModeCenter  // its natural size, centered
)

// ImageProps configures an Image.
type ImageProps struct {
	// Source is an http(s) URL, the path of a file in the app's assets
	// directory, or an absolute path (a file the app wrote, a picked
	// photo).
	Source signal.Accessor[string]
	Mode   ImageMode
	Style  Styler
	// Placeholder is an image shown until Source loads, and if it fails:
	// usually an asset, at hand at once. It takes the image's Mode; the
	// box's size is still the image's (give it one so the placeholder
	// has room).
	Placeholder string
	// Label describes the image to screen readers; without it the image
	// is decorative.
	Label   string
	OnLoad  func(width, height float32)
	OnError func(message string)
}

// Image shows a picture the host loads and decodes off the main thread
// (and caches: in memory, and one fetched over http(s) on disk, across
// launches). Until it has loaded, an image without a size is empty;
// then it takes its natural size, one point per pixel, no wider than
// the space it has, and keeps its aspect ratio when one side is set.
func Image(p ImageProps) Node {
	e := newElement(ops.KindImage, true)
	var natural layout.Size
	e.box.SetMeasure(func(c layout.Constraints) layout.Size {
		return imageSize(natural, e.style.AspectRatio, c)
	})
	h := e.handlers()
	h.loaded = func(w, hh float32) {
		if s := (layout.Size{W: w, H: hh}); s != natural {
			natural = s
			e.box.MarkDirty()
		}
		if p.OnLoad != nil {
			p.OnLoad(w, hh)
		}
	}
	h.failed = p.OnError
	e.listen(ops.EventLoaded)
	if p.OnError != nil {
		e.listen(ops.EventFailed)
	}
	if p.Mode != ImageFit {
		e.r.batch.SetEnum(e.id, ops.PropImageMode, int32(p.Mode))
	}
	if p.Placeholder != "" {
		e.r.batch.SetString(e.id, ops.PropImagePlaceholder, assetURL(p.Placeholder))
	}
	if p.Label != "" {
		e.r.batch.SetString(e.id, ops.PropA11yLabel, p.Label)
		e.r.batch.SetEnum(e.id, ops.PropA11yRole, int32(RoleImage))
	}
	bind(p.Source, func(v string) { e.r.batch.SetString(e.id, ops.PropImageSource, assetURL(v)) })
	e.setStyle(p.Style, false)
	return Node{e}
}

// AssetURL is where the host gets source from: under nui dev, which runs
// the app's Go code on the computer, an asset's URL there; otherwise
// source as it is (an asset's path, a URL, a file). For a package of an
// app's own that hands the host a source, as Image does.
func AssetURL(source string) string { return assetURL(source) }

// assetURL is where a player gets an asset from (boot.AssetBase), or
// source as it is.
func assetURL(source string) string {
	// A file on the device (a picked photo) is the device's, not an asset.
	if boot.AssetBase == "" || source == "" || strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") ||
		strings.HasPrefix(source, "file://") || filepath.IsAbs(source) {
		return source
	}
	return strings.TrimSuffix(boot.AssetBase, "/") + "/" + strings.TrimPrefix(source, "/")
}

// imageSize is an image's size within c, from its natural size; a
// style's aspect ratio overrides the image's, as CSS's does.
func imageSize(n layout.Size, ratio float32, c layout.Constraints) layout.Size {
	known := func(f float32) bool { return f == f }
	if ratio > 0 && n.W > 0 && n.H > 0 {
		n.H = n.W / ratio
	}
	switch {
	case known(c.Width) && known(c.Height):
		return layout.Size{W: c.Width, H: c.Height}
	case ratio > 0 && known(c.Width):
		return layout.Size{W: c.Width, H: c.Width / ratio}
	case ratio > 0 && known(c.Height):
		return layout.Size{W: c.Height * ratio, H: c.Height}
	case n.W <= 0 || n.H <= 0:
		s := layout.Size{}
		if known(c.Width) {
			s.W = c.Width
		}
		if known(c.Height) {
			s.H = c.Height
		}
		return s
	case known(c.Width):
		return layout.Size{W: c.Width, H: c.Width * n.H / n.W}
	case known(c.Height):
		return layout.Size{W: c.Height * n.W / n.H, H: c.Height}
	}
	s := n
	if aw := c.AvailW; aw > 0 && !math.IsInf(float64(aw), 0) && s.W > aw {
		s = layout.Size{W: aw, H: aw * n.H / n.W}
	}
	return s
}

// Gestures

// Gesture is a pan's or pinch's progress, in points: X and Y are where
// the touch is in the view, the translation is how far it moved since
// it began (in the window), velocity is in points per second, and Scale
// is a pinch's scale since it began (1 for a pan).
type Gesture = ops.Gesture

// GestureState is where a gesture is.
type GestureState = ops.GestureState

const (
	GestureBegan     = ops.GestureStateBegan
	GestureChanged   = ops.GestureStateChanged
	GestureEnded     = ops.GestureStateEnded
	GestureCancelled = ops.GestureStateCancelled
)

// SwipeDirection is a swipe's direction.
type SwipeDirection int

const (
	SwipeRight SwipeDirection = 1 << iota
	SwipeLeft
	SwipeUp
	SwipeDown
)

// listenGestures wires a view's gesture handlers.
func (e *element) listenGestures(p *ViewProps) {
	h := e.handlers()
	if p.OnPan != nil {
		h.pan = p.OnPan
		e.listen(ops.EventPan)
	}
	if p.OnPinch != nil {
		h.pinch = p.OnPinch
		e.listen(ops.EventPinch)
	}
	if p.OnSwipe != nil {
		h.swipe = p.OnSwipe
		e.listen(ops.EventSwipe)
	}
	if p.OnRotate != nil {
		h.rotate = p.OnRotate
		e.listen(ops.EventRotate)
	}
}

// fonts maps a font family to what a player gets it as: its file's URL
// when the app has one in assets/fonts (nui dev runs the app in its
// directory), or the family as it is.
var fonts = map[string]string{}

func fontFamily(family string) string {
	if boot.AssetBase == "" || family == "" {
		return family
	}
	if v, ok := fonts[family]; ok {
		return v
	}
	v := family
	for _, ext := range []string{".ttf", ".otf"} {
		if _, err := os.Stat(filepath.Join("assets", "fonts", family+ext)); err == nil {
			v = assetURL("fonts/" + family + ext)
			break
		}
	}
	fonts[family] = v
	return v
}
