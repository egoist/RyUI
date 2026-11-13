//go:build darwin && !ios

package darwin

import (
	"image"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/egoist/asuka/conformance"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// TestConformance draws the conformance sheet in a real window and
// measures it (package conformance).
func TestConformance(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return conformance.Sheet() })
	var img image.Image
	onMain(func() {
		Send(h.window, "orderFront:", 0)
		img = capture(t, h.content)
	})
	conformance.Check(t, "macos", img, "macos")
}

type (
	mtlRegion   struct{ X, Y, Z, W, H, D uint }
	transform3D struct {
		M11, M12, M13, M14, M21, M22, M23, M24, M31, M32, M33, M34, M41, M42, M43, M44 float64
	}
)

var (
	msgBeginFrame   func(obj ID, sel SEL, t float64, ts uintptr)
	msgGetBytes     func(obj ID, sel SEL, buf unsafe.Pointer, row uint, r mtlRegion, level uint)
	msgSetTransform func(obj ID, sel SEL, t transform3D)
	cgColorSpace    func(name ID) ID
	caMediaTime     func() float64
	libQuartzCore   uintptr
)

// capture renders v's layer tree with Core Animation's own renderer
// (CARenderer, into a Metal texture), in sRGB at its window's scale, as
// the window server composites it. NSView's cacheDisplayInRect draws
// the layers again with Core Graphics (renderInContext), which leaves
// out their transforms and shadows and blends in the display's colors.
func capture(t *testing.T, v ID) image.Image {
	loadMetal()
	if mtlDevice == 0 {
		t.Skip("no Metal device")
	}
	if msgBeginFrame == nil {
		purego.RegisterFunc(&msgBeginFrame, MsgSendAddr())
		purego.RegisterFunc(&msgGetBytes, MsgSendAddr())
		purego.RegisterFunc(&msgSetTransform, MsgSendAddr())
		libQuartzCore = Dlopen("/System/Library/Frameworks/QuartzCore.framework/QuartzCore")
		purego.RegisterLibFunc(&cgColorSpace, CoreGraphics(), "CGColorSpaceCreateWithName")
		purego.RegisterLibFunc(&caMediaTime, libQuartzCore, "CACurrentMediaTime")
	}
	Send(Send(v, "window"), "display")
	k := MsgFloat(Send(v, "window"), Sel("backingScaleFactor"))
	b := MsgRect(v, Sel("bounds"))
	w, ht := uint(b.Size.Width*k+0.5), uint(b.Size.Height*k+0.5)
	px := Rect{Size: Size{Width: float64(w), Height: float64(ht)}}

	d := Send(Class("MTLTextureDescriptor"), "texture2DDescriptorWithPixelFormat:width:height:mipmapped:", 80, uintptr(w), uintptr(ht), 0) // BGRA8Unorm
	Send(d, "setUsage:", 1|2|4)                                                                                                            // shader read and write, render target
	Send(d, "setStorageMode:", 0)                                                                                                          // shared
	tex := Send(mtlDevice, "newTextureWithDescriptor:", uintptr(d))
	space := cgColorSpace(Const(CoreGraphics(), "kCGColorSpaceSRGB"))
	opts := Dict(Const(libQuartzCore, "kCARendererMetalCommandQueue"), mtlQueue, Const(libQuartzCore, "kCARendererColorSpace"), space)
	r := Send(Class("CARenderer"), "rendererWithMTLTexture:options:", uintptr(tex), uintptr(opts))

	// The view's layer, in a layer of the renderer's own that scales it
	// to pixels: CARenderer draws only a tree that's in no other (a
	// window's), so it's taken out of its window for the render and put
	// back after.
	l := Send(v, "layer")
	super := Send(l, "superlayer")
	at := 0
	if super != 0 {
		subs := Send(super, "sublayers")
		at = int(SendInt(subs, "indexOfObject:", uintptr(l)))
	}
	pos := MsgPoint(l, Sel("position"))
	wrap := New("CALayer")
	transaction(func() {
		MsgSetPoint(wrap, Sel("setAnchorPoint:"), Point{})
		MsgSetPoint(wrap, Sel("setPosition:"), Point{})
		MsgSetRect(wrap, Sel("setBounds:"), px)
		msgSetTransform(wrap, Sel("setSublayerTransform:"), transform3D{M11: k, M22: k, M33: 1, M44: 1})
		Send(wrap, "addSublayer:", uintptr(l))
		// Its top left at the wrapper's, y down as the window shows it.
		MsgSetPoint(l, Sel("setPosition:"), Point{X: b.Size.Width * MsgPoint(l, Sel("anchorPoint")).X, Y: b.Size.Height * MsgPoint(l, Sel("anchorPoint")).Y})
		Send(wrap, "setGeometryFlipped:", 1)
	})
	Send(r, "setLayer:", uintptr(wrap))
	MsgSetRect(r, Sel("setBounds:"), px)
	Send(Class("CATransaction"), "flush")
	msgBeginFrame(r, Sel("beginFrameAtTime:timeStamp:"), caMediaTime(), 0)
	MsgSetRect(r, Sel("addUpdateRect:"), px)
	Send(r, "render")
	Send(r, "endFrame")
	Send(r, "setLayer:", 0)
	transaction(func() {
		Send(l, "removeFromSuperlayer")
		MsgSetPoint(l, Sel("setPosition:"), pos)
		if super != 0 {
			Send(super, "insertSublayer:atIndex:", uintptr(l), uintptr(at))
		}
	})
	fence := Send(mtlQueue, "commandBuffer")
	Send(fence, "commit")
	Send(fence, "waitUntilCompleted")

	bgra := make([]byte, w*ht*4)
	msgGetBytes(tex, Sel("getBytes:bytesPerRow:fromRegion:mipmapLevel:"), unsafe.Pointer(&bgra[0]), w*4, mtlRegion{W: w, H: ht, D: 1}, 0)
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(ht)))
	for y := range int(ht) {
		src := bgra[y*int(w)*4:] // top down: the wrapper's geometry is flipped
		dst := img.Pix[y*img.Stride:]
		for x := range int(w) {
			dst[x*4], dst[x*4+1], dst[x*4+2], dst[x*4+3] = src[x*4+2], src[x*4+1], src[x*4], 255
		}
	}
	return img
}

// transaction runs f in a Core Animation transaction without animations.
func transaction(f func()) {
	Send(Class("CATransaction"), "begin")
	Send(Class("CATransaction"), "setDisableActions:", 1)
	f()
	Send(Class("CATransaction"), "commit")
}
