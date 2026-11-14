package ui

import (
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/layout"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
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
	// photo). PNG, JPEG, GIF, WebP, HEIC and AVIF, animated or not (GIF,
	// APNG, WebP), and SVG, where the platform decodes them (docs:
	// Images).
	Source signal.Accessor[string]
	Mode   ImageMode
	Style  Styler
	// Placeholder is an image shown until Source loads, and if it fails:
	// usually an asset, at hand at once. It takes the image's Mode; the
	// box's size is still the image's (give it one so the placeholder
	// has room).
	Placeholder string
	// BlurHash and ThumbHash are placeholders from a hash an app keeps
	// with the image (blurha.sh, evanw.github.io/thumbhash; a ThumbHash
	// in base64), decoded by the core into a small picture, once per
	// hash, and shown in Placeholder's place until Source loads. A
	// BlurHash has no shape of its own: it's decoded at the style's
	// AspectRatio, or square and stretched to the box without one. A
	// ThumbHash keeps its image's aspect ratio and alpha. Accessors, as a
	// list's rows change images.
	BlurHash  signal.Accessor[string]
	ThumbHash signal.Accessor[string]
	// PlaceholderMode is how the placeholder fills the box: as the image
	// does (PlaceholderAsImage, the default), or its own.
	PlaceholderMode PlaceholderMode
	// Transition is how long, in seconds, the image takes to fade in as
	// it arrives: over its placeholder, cross-dissolving, or from
	// nothing. 0: over a placeholder, the platform's image transition
	// (a cross-dissolve of its short duration: 0.25s on iOS and the Mac,
	// config_shortAnimTime on Android), none without one; NoTransition:
	// none.
	Transition float32
	// Animated plays an animated image (GIF, APNG, WebP, and AVIF and
	// HEIF sequences where the platform decodes them) while true, and
	// stops it where it is while false. nil: it plays, as many times as
	// its file says, unless the user asked for less motion (Reduce
	// Motion, animations off), when it shows its first frame.
	Animated signal.Accessor[bool]
	// Label describes the image to screen readers; without it the image
	// is decorative.
	Label   signal.Accessor[string]
	OnLoad  func(width, height float32)
	OnError func(message string)
}

// PlaceholderMode is how an Image's placeholder fills its box.
type PlaceholderMode uint8

const (
	PlaceholderAsImage PlaceholderMode = iota // as the image does: its Mode
	PlaceholderFit                            // all of it, letterboxed
	PlaceholderFill                           // covers the box, cropped
	PlaceholderStretch                        // the box's aspect ratio
	PlaceholderCenter                         // its natural size, centered
)

// NoTransition is an ImageProps.Transition: the image shows at once.
const NoTransition = -1

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
	if p.PlaceholderMode != PlaceholderAsImage {
		e.r.batch.SetEnum(e.id, ops.PropImagePlaceholderMode, int32(p.PlaceholderMode-1))
	}
	if p.Transition != 0 {
		e.r.batch.SetFloat(e.id, ops.PropImageTransition, float64(p.Transition))
	}
	if p.ThumbHash != nil {
		bind(p.ThumbHash, func(v string) {
			e.r.batch.SetBytes(e.id, ops.PropImagePlaceholderImage, thumbHashImage(v))
		})
	}
	if p.Animated != nil {
		bind(p.Animated, func(v bool) { e.r.batch.SetBool(e.id, ops.PropImagePaused, !v) })
	} else {
		// Only an image that turns out to have frames follows Reduce
		// Motion: the effect is made once the host says so, owned by
		// what owns the image.
		owner := signal.CurrentOwner()
		env := UseEnvironment()
		watching := false
		h.animated = func() {
			if watching {
				return
			}
			watching = true
			signal.RunWithOwner(owner, func() {
				paused := false
				signal.RenderEffect(func() {
					if v := env.Get().ReduceMotion; v != paused {
						paused = v
						e.r.batch.SetBool(e.id, ops.PropImagePaused, v)
					}
				})
			})
		}
		e.listen(ops.EventAnimated)
	}
	if hasText(p.Label) {
		bind(p.Label, func(v string) { e.r.batch.SetString(e.id, ops.PropA11yLabel, v) })
		e.r.batch.SetEnum(e.id, ops.PropA11yRole, int32(RoleImage))
	}
	bind(p.Source, func(v string) { e.r.batch.SetString(e.id, ops.PropImageSource, assetURL(v)) })
	e.setStyle(p.Style, false)
	// After the style: a BlurHash takes its aspect ratio.
	if p.BlurHash != nil {
		stretch := p.PlaceholderMode == PlaceholderAsImage
		bind(p.BlurHash, func(v string) {
			ratio := e.style.AspectRatio
			if ratio > 0 && stretch {
				// Decoded at the image's shape, it fills the box as the
				// image will; without one, stretched to it.
				e.r.batch.SetEnum(e.id, ops.PropImagePlaceholderMode, int32(p.Mode))
			} else if stretch {
				e.r.batch.SetEnum(e.id, ops.PropImagePlaceholderMode, int32(ImageStretch))
			}
			e.r.batch.SetBytes(e.id, ops.PropImagePlaceholderImage, blurHashImage(v, ratio))
		})
	}
	return Node{e}
}

// AssetURL is where the host gets source from: under asuka dev, which runs
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

// fontURLs maps a font family to what a player gets it as: its file's URL
// when the app has one in assets/fonts (asuka dev runs the app in its
// directory), or the family as it is.
var fontURLs = map[string]string{}

func fontFamily(family string) string {
	if boot.AssetBase == "" || family == "" {
		return family
	}
	if v, ok := fontURLs[family]; ok {
		return v
	}
	v := family
	for _, ext := range []string{".ttf", ".otf"} {
		if _, err := os.Stat(filepath.Join("assets", "fonts", family+ext)); err == nil {
			v = assetURL("fonts/" + family + ext)
			break
		}
	}
	fontURLs[family] = v
	return v
}
