//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/egoist/ryui/internal/shadercode"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

// A Shader is a view whose layer holds a CAMetalLayer, under its
// subviews, drawn by the shader's Metal library (package shadercode's), a
// triangle over the whole box, its uniforms in a buffer as the code lays
// them out; the host sets the size, the scale and Time. Animated shaders
// draw at each tick of a display link of their own; the others when their
// size or uniforms change.

type shader struct {
	layer    ID
	code     *shadercode.Code
	words    []uint32 // the uniforms' lanes
	buf      []byte   // the Metal buffer
	animated bool
	pipeline ID
	failed   bool
	start    time.Time
}

var (
	mtlDevice, mtlQueue ID
	mtlPipelines        = map[string]ID{}
	mtlLoaded           bool
)

func loadMetal() {
	if mtlLoaded {
		return
	}
	mtlLoaded = true
	lib, err := purego.Dlopen("/System/Library/Frameworks/Metal.framework/Metal", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		return
	}
	purego.Dlopen("/System/Library/Frameworks/QuartzCore.framework/QuartzCore", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	var create func() ID
	purego.RegisterLibFunc(&create, lib, "MTLCreateSystemDefaultDevice")
	mtlDevice = create()
	if mtlDevice != 0 {
		mtlQueue = Send(mtlDevice, "newCommandQueue")
	}
}

func (h *Host) makeShader(n *node) ID {
	loadMetal()
	n.shader = &shader{start: time.Now()}
	v := initView("RyUIView")
	Send(v, "setWantsLayer:", 1)
	if mtlDevice != 0 {
		l := New("CAMetalLayer")
		Send(l, "setDevice:", uintptr(mtlDevice))
		Send(l, "setPixelFormat:", 80) // BGRA8Unorm
		Send(l, "setOpaque:", 0)
		Send(l, "setFramebufferOnly:", 1)
		Send(Send(v, "layer"), "insertSublayer:atIndex:", uintptr(l), 0)
		n.shader.layer = l
	}
	return v
}

func (h *Host) setShaderProp(n *node, p ops.PropID, v *ops.Value) {
	s := n.shader
	switch p {
	case ops.PropShaderCode:
		c, err := shadercode.Decode(v.Bytes)
		if err != nil {
			return
		}
		s.code, s.pipeline, s.failed = c, 0, false
		if len(s.words) != c.Lanes {
			s.words = make([]uint32, c.Lanes)
		}
	case ops.PropShaderAnimated:
		s.animated = v.Int != 0
		h.updateShaderClock()
	case ops.PropShaderUniforms:
		s.words = s.words[:0]
		for i := 0; i+4 <= len(v.Bytes); i += 4 {
			s.words = append(s.words, binary.LittleEndian.Uint32(v.Bytes[i:]))
		}
	}
	h.drawShader(n)
}

func (s *shader) makePipeline() ID {
	src := s.code.Metal
	if p, ok := mtlPipelines[src]; ok {
		return p
	}
	var errID ID
	lib := Send(mtlDevice, "newLibraryWithSource:options:error:", uintptr(String(src)), 0, uintptr(unsafe.Pointer(&errID)))
	if lib == 0 {
		fmt.Fprintf(os.Stderr, "ryui: a ui.Shader's Metal library doesn't compile: %s\n", GoString(Send(errID, "localizedDescription")))
		return 0
	}
	d := New("MTLRenderPipelineDescriptor")
	Send(d, "setVertexFunction:", uintptr(Send(lib, "newFunctionWithName:", uintptr(String("ryui_vertex")))))
	Send(d, "setFragmentFunction:", uintptr(Send(lib, "newFunctionWithName:", uintptr(String("ryui_fragment")))))
	Send(Send(Send(d, "colorAttachments"), "objectAtIndexedSubscript:", 0), "setPixelFormat:", 80)
	p := Send(mtlDevice, "newRenderPipelineStateWithDescriptor:error:", uintptr(d), uintptr(unsafe.Pointer(&errID)))
	Release(d)
	Release(lib)
	if p != 0 {
		mtlPipelines[src] = p
	}
	return p
}

// layoutShader sizes the Metal layer to its view's bounds, in pixels.
func (h *Host) layoutShader(n *node) {
	s := n.shader
	if s == nil || s.layer == 0 {
		return
	}
	b := MsgRect(n.view, Sel("bounds"))
	k := 2.0
	if w := Send(n.view, "window"); w != 0 {
		k = MsgFloat(w, Sel("backingScaleFactor"))
	}
	Send(Class("CATransaction"), "begin")
	Send(Class("CATransaction"), "setDisableActions:", 1)
	MsgSetRect(s.layer, Sel("setFrame:"), b)
	MsgSetFloat(s.layer, Sel("setContentsScale:"), k)
	MsgSetSize(s.layer, Sel("setDrawableSize:"), Size{Width: b.Size.Width * k, Height: b.Size.Height * k})
	Send(Class("CATransaction"), "commit")
	h.drawShader(n)
}

func (h *Host) drawShader(n *node) {
	s := n.shader
	if s == nil || s.layer == 0 || s.code == nil || mtlQueue == 0 {
		return
	}
	b := MsgRect(n.view, Sel("bounds"))
	if b.Size.Width <= 0 || b.Size.Height <= 0 || Send(n.view, "window") == 0 {
		return
	}
	if s.pipeline == 0 && !s.failed {
		s.pipeline = s.makePipeline()
		s.failed = s.pipeline == 0
	}
	if s.pipeline == 0 {
		return
	}
	c := s.code
	if len(s.words) < c.Lanes {
		s.words = append(s.words, make([]uint32, c.Lanes-len(s.words))...)
	}
	s.words[c.Size] = math.Float32bits(float32(b.Size.Width))
	s.words[c.Size+1] = math.Float32bits(float32(b.Size.Height))
	s.words[c.Scale] = math.Float32bits(float32(MsgFloat(s.layer, Sel("contentsScale"))))
	if c.Time >= 0 {
		s.words[c.Time] = math.Float32bits(float32(time.Since(s.start).Seconds()))
	}
	if len(s.buf) != max(c.MetalSize, 16) {
		s.buf = make([]byte, max(c.MetalSize, 16))
	}
	for i, o := range c.MetalOffsets {
		if c.MetalBool[i] {
			s.buf[o] = byte(min(s.words[i], 1))
		} else {
			binary.LittleEndian.PutUint32(s.buf[o:], s.words[i])
		}
	}
	WithPool(func() {
		drawable := Send(s.layer, "nextDrawable")
		if drawable == 0 {
			return
		}
		pass := Send(Class("MTLRenderPassDescriptor"), "renderPassDescriptor")
		att := Send(Send(pass, "colorAttachments"), "objectAtIndexedSubscript:", 0)
		Send(att, "setTexture:", uintptr(Send(drawable, "texture")))
		Send(att, "setLoadAction:", 0) // don't care: the triangle covers every pixel
		Send(att, "setStoreAction:", 1)
		buf := Send(mtlQueue, "commandBuffer")
		enc := Send(buf, "renderCommandEncoderWithDescriptor:", uintptr(pass))
		Send(enc, "setRenderPipelineState:", uintptr(s.pipeline))
		Send(enc, "setFragmentBytes:length:atIndex:", uintptr(unsafe.Pointer(&s.buf[0])), uintptr(len(s.buf)), 0)
		for i := range 4 {
			Send(enc, "setFragmentTexture:atIndex:", uintptr(blankTexture()), uintptr(i))
		}
		Send(enc, "drawPrimitives:vertexStart:vertexCount:", 3, 0, 3) // triangles
		Send(enc, "endEncoding")
		Send(buf, "presentDrawable:", uintptr(drawable))
		Send(buf, "commit")
	})
}

var blank ID

// blankTexture is what the library's image parameters take: ui.Shader
// reads no images.
func blankTexture() ID {
	if blank == 0 && mtlDevice != 0 {
		d := Send(Class("MTLTextureDescriptor"), "texture2DDescriptorWithPixelFormat:width:height:mipmapped:", 70, 1, 1, 0) // RGBA8Unorm
		blank = Send(mtlDevice, "newTextureWithDescriptor:", uintptr(d))
	}
	return blank
}

// updateShaderClock runs the animated shaders' clock while any is shown.
func (h *Host) updateShaderClock() {
	on := false
	for _, n := range h.nodes {
		if n.shader != nil && n.shader.animated {
			on = true
			break
		}
	}
	if on == (h.shaderTimer != 0) {
		return
	}
	if !on {
		Send(h.shaderTimer, "invalidate")
		Release(h.shaderTimer)
		h.shaderTimer = 0
		return
	}
	// A display link where the system has view display links (14), else
	// a timer at 60 Hz.
	if RespondsTo(h.content, "displayLinkWithTarget:selector:") {
		t := Retain(Send(h.content, "displayLinkWithTarget:selector:", uintptr(h.target), uintptr(Sel("shaderTick:"))))
		Send(t, "addToRunLoop:forMode:", uintptr(Send(Class("NSRunLoop"), "mainRunLoop")), uintptr(String("kCFRunLoopCommonModes")))
		h.shaderTimer = t
		return
	}
	t := Retain(MsgTargetTimer(Class("NSTimer"), Sel("timerWithTimeInterval:target:selector:userInfo:repeats:"), 1.0/60, h.target, Sel("shaderTick:"), 0, true))
	Send(Send(Class("NSRunLoop"), "mainRunLoop"), "addTimer:forMode:", uintptr(t), uintptr(String("kCFRunLoopCommonModes")))
	h.shaderTimer = t
}

func (h *Host) shaderTick() {
	for _, n := range h.nodes {
		if n.shader != nil && n.shader.animated {
			h.drawShader(n)
		}
	}
}
