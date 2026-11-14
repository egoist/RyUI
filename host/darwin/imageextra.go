//go:build darwin && !ios

package darwin

import (
	"bytes"
	"encoding/binary"
	stdimage "image"
	"image/png"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// An Image's placeholder, arrival and frames. The placeholder (the core's
// decoded hash, else the Placeholder image) is a sublayer over the image
// view's content, in its own mode's gravity (a tint is its color, masked
// by it); the image's arrival fades in with a CATransition while the
// placeholder's layer fades out (Transition; 0.25s, NSAnimationContext's
// default, over a placeholder). An animated file (GIF, APNG, WebP, HEICS)
// plays through ImageIO's CGAnimateImageDataWithBlock, which times and
// loops it as the file says, while the view is shown and not Paused.

type imageExtra struct {
	hash       ID      // PlaceholderImage's NSImage (+1)
	shown      ID      // the placeholder showing (the hash or the asset's)
	tinted     bool    // shown is the asset's, in the image's color
	mode       int32   // PlaceholderMode; -1: the image's
	transition float64 // seconds
	paused     bool
	layer      ID // the placeholder's sublayer (+1)
	anim       []byte
	frame      int // the frame the animation is at
	gen        int // a playback's: a newer one stops the older
}

func (n *node) imx() *imageExtra {
	if n.imgx == nil {
		n.imgx = &imageExtra{mode: -1}
	}
	return n.imgx
}

var (
	imageIOOnce       sync.Once
	cgSourceCreate    func(data, opts uintptr) uintptr
	cgSourceCount     func(src uintptr) uintptr
	cgAnimateData     func(data, opts uintptr, block objc.Block) int32
	cgImageAnimStart  uintptr // kCGImageAnimationStartIndex
	cgImageGetWidth   func(img uintptr) uintptr
	cgImageGetHeight  func(img uintptr) uintptr
	msgInitCGImage    func(obj ID, sel SEL, img uintptr, size Size) ID
	caLayerAutoresize = uintptr(2 | 16) // width and height sizable
)

func loadImageIO() {
	imageIOOnce.Do(func() {
		lib := Dlopen("/System/Library/Frameworks/ImageIO.framework/ImageIO")
		if lib == 0 {
			return
		}
		purego.RegisterLibFunc(&cgSourceCreate, lib, "CGImageSourceCreateWithData")
		purego.RegisterLibFunc(&cgSourceCount, lib, "CGImageSourceGetCount")
		purego.RegisterLibFunc(&cgAnimateData, lib, "CGAnimateImageDataWithBlock")
		cgImageAnimStart = uintptr(Const(lib, "kCGImageAnimationStartIndex"))
		purego.RegisterLibFunc(&cgImageGetWidth, CoreGraphics(), "CGImageGetWidth")
		purego.RegisterLibFunc(&cgImageGetHeight, CoreGraphics(), "CGImageGetHeight")
		purego.RegisterFunc(&msgInitCGImage, MsgSendAddr())
	})
}

// frameCount is how many frames an image file has (0: ImageIO can't
// read it).
func frameCount(data []byte) int {
	loadImageIO()
	if cgSourceCreate == nil || len(data) == 0 {
		return 0
	}
	n := 0
	WithPool(func() {
		src := cgSourceCreate(uintptr(Data(data)), 0)
		if src == 0 {
			return
		}
		n = int(cgSourceCount(src))
		CFRelease(src)
	})
	return n
}

// hashImage is an NSImage (+1) of PlaceholderImage's pixels, one point
// per pixel.
func hashImage(b []byte) ID {
	if len(b) < 8 {
		return 0
	}
	w, h := int(binary.LittleEndian.Uint32(b)), int(binary.LittleEndian.Uint32(b[4:]))
	if w <= 0 || h <= 0 || len(b) < 8+w*h*4 {
		return 0
	}
	img := &stdimage.NRGBA{Pix: b[8 : 8+w*h*4], Stride: w * 4, Rect: stdimage.Rect(0, 0, w, h)}
	var buf bytes.Buffer
	if png.Encode(&buf, img) != nil {
		return 0
	}
	var out ID
	WithPool(func() {
		out = Send(Send(Class("NSImage"), "alloc"), "initWithData:", uintptr(Data(buf.Bytes())))
	})
	return out
}

func gravity(mode int32) string {
	switch ops.ImageMode(mode) {
	case ops.ImageModeFill:
		return "resizeAspectFill"
	case ops.ImageModeStretch:
		return "resize"
	case ops.ImageModeCenter:
		return "center"
	}
	return "resizeAspect"
}

// setPlaceholderImage shows img (0: none) over the image until it
// arrives; tinted, in the image's color.
func (h *Host) setPlaceholderImage(n *node, img ID, tinted bool) {
	x := n.imx()
	x.shown, x.tinted = img, tinted
	if img == 0 {
		if x.layer != 0 {
			Send(x.layer, "removeFromSuperlayer")
			Release(x.layer)
			x.layer = 0
		}
		return
	}
	WithPool(func() {
		parent := h.layer(n)
		Send(parent, "setMasksToBounds:", 1)
		Send(Class("CATransaction"), "begin")
		Send(Class("CATransaction"), "setDisableActions:", 1)
		if x.layer == 0 {
			x.layer = Retain(Send(Class("CALayer"), "layer"))
			Send(x.layer, "setAutoresizingMask:", caLayerAutoresize)
			Send(parent, "addSublayer:", uintptr(x.layer))
		}
		Send(x.layer, "removeAllAnimations")
		MsgSetRect(x.layer, Sel("setFrame:"), MsgRect(parent, Sel("bounds")))
		msgSetFloat32(x.layer, Sel("setOpacity:"), 1)
		mode := x.mode
		if mode < 0 {
			mode = n.imageMode
		}
		g := uintptr(String(gravity(mode)))
		if tinted && n.color != 0 {
			// The color, masked by the image.
			mask := Send(Class("CALayer"), "layer")
			MsgSetRect(mask, Sel("setFrame:"), MsgRect(x.layer, Sel("bounds")))
			Send(mask, "setAutoresizingMask:", caLayerAutoresize)
			Send(mask, "setContents:", uintptr(img))
			Send(mask, "setContentsGravity:", g)
			Send(x.layer, "setContents:", 0)
			Send(x.layer, "setMask:", uintptr(mask))
			Send(x.layer, "setBackgroundColor:", cgColor(h.nsColor(n.color)))
		} else {
			Send(x.layer, "setMask:", 0)
			Send(x.layer, "setBackgroundColor:", 0)
			Send(x.layer, "setContents:", uintptr(img))
			Send(x.layer, "setContentsGravity:", g)
		}
		Send(Class("CATransaction"), "commit")
	})
}

// arrive fades the image in over its placeholder, or from nothing, for
// the node's Transition; then the placeholder goes.
func (h *Host) arrive(n *node) {
	x := n.imx()
	had := x.layer != 0 && x.shown != 0
	d := x.transition
	switch {
	case d < 0:
		d = 0
	case d == 0 && had:
		d = 0.25
	}
	if d <= 0 || Send(n.view, "window") == 0 {
		h.setPlaceholderImage(n, 0, false)
		return
	}
	WithPool(func() {
		l := h.layer(n)
		t := Send(Class("CATransition"), "animation")
		Send(t, "setType:", uintptr(String("fade")))
		MsgSetFloat(t, Sel("setDuration:"), d)
		Send(l, "addAnimation:forKey:", uintptr(t), uintptr(String("asuka.image")))
		if had {
			Send(Class("CATransaction"), "begin")
			MsgSetFloat(Class("CATransaction"), Sel("setAnimationDuration:"), d)
			msgSetFloat32(x.layer, Sel("setOpacity:"), 0)
			Send(Class("CATransaction"), "commit")
		}
	})
	if had {
		id, layer := n.id, x.layer
		time.AfterFunc(time.Duration(d*float64(time.Second)), func() {
			h.Post(func() {
				if n := h.nodes[id]; n != nil && n.imgx != nil && n.imgx.layer == layer && n.loaded {
					h.setPlaceholderImage(n, 0, false)
				}
			})
		})
	}
}

// showPlaceholder shows the image's placeholder while it hasn't loaded:
// the hash's, else the Placeholder image's.
func (h *Host) showPlaceholder(n *node) {
	if n.loaded {
		return
	}
	x := n.imx()
	if x.hash != 0 {
		h.setPlaceholderImage(n, x.hash, false)
		return
	}
	if n.placeholder == "" {
		h.setPlaceholderImage(n, 0, false)
		return
	}
	h.loadImage(n, n.placeholder, true)
}

// stopFrames stops the node's animation (it stays at its frame).
func (x *imageExtra) stopFrames() { x.gen++ }

// playFrames plays the node's animated image from the frame it's at, if
// it has one, is shown and isn't paused.
func (h *Host) playFrames(n *node) {
	x := n.imx()
	x.stopFrames()
	if x.anim == nil || x.paused || !n.loaded {
		return
	}
	loadImageIO()
	if cgAnimateData == nil {
		return
	}
	gen, id := x.gen, n.id
	var block objc.Block
	block = objc.NewBlock(func(_ objc.Block, index uintptr, img uintptr, stop *bool) {
		n := h.nodes[id]
		if n == nil || n.imgx == nil || n.imgx.gen != gen || n.view == 0 {
			*stop = true
			return
		}
		n.imgx.frame = int(index)
		WithPool(func() {
			size := Size{Width: float64(cgImageGetWidth(img)), Height: float64(cgImageGetHeight(img))}
			ns := msgInitCGImage(Send(Class("NSImage"), "alloc"), Sel("initWithCGImage:size:"), img, size)
			h.showImage(n, ns)
			Release(ns)
		})
	})
	WithPool(func() {
		var opts ID
		if cgImageAnimStart != 0 {
			opts = Send(Class("NSDictionary"), "dictionaryWithObject:forKey:", uintptr(NumberInt(x.frame)), cgImageAnimStart)
		}
		cgAnimateData(uintptr(Data(x.anim)), uintptr(opts), block)
	})
	block.Release() // ImageIO keeps its copy while it plays
}

// releaseImage lets go of what an Image holds.
func (n *node) releaseImage() {
	x := n.imgx
	if x == nil {
		return
	}
	x.stopFrames()
	if x.hash != 0 {
		Release(x.hash)
		x.hash = 0
	}
	if x.layer != 0 {
		Release(x.layer)
		x.layer = 0
	}
}
