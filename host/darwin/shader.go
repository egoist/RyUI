//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"strings"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

// A Shader is a view whose layer holds a CAMetalLayer, under its
// subviews, drawn by the app's shade(uv) in the Metal program iOS's host
// makes too (the source inside a struct whose members are time, size and
// the uniforms). Animated shaders draw at each tick of a 60 Hz clock of
// their own; the others when their size or uniforms change.

type shader struct {
	layer    ID
	source   string
	names    []string
	values   []float32
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
	case ops.PropShaderSource:
		s.source, s.pipeline, s.failed = v.Str, 0, false
	case ops.PropShaderAnimated:
		s.animated = v.Int != 0
		h.updateShaderClock()
	case ops.PropShaderUniforms:
		names, values := decodeUniforms(v.Bytes)
		if strings.Join(names, ",") != strings.Join(s.names, ",") {
			s.pipeline, s.failed = 0, false
		}
		s.names, s.values = names, values
	}
	h.drawShader(n)
}

func decodeUniforms(b []byte) (names []string, values []float32) {
	le := binary.LittleEndian
	if len(b) < 4 {
		return
	}
	i := 4
	for range le.Uint32(b) {
		if i+4 > len(b) {
			break
		}
		l := int(le.Uint32(b[i:]))
		i += 4
		if i+l+16 > len(b) {
			break
		}
		names = append(names, string(b[i:i+l]))
		i += l
		for range 4 {
			values = append(values, math.Float32frombits(le.Uint32(b[i:])))
			i += 4
		}
	}
	return
}

// shaderProgram is the Metal source around the app's function (as iOS's
// host makes it).
func shaderProgram(source string, names []string) string {
	var members, loads strings.Builder
	for i, n := range names {
		fmt.Fprintf(&members, "    float4 %s;\n", n)
		fmt.Fprintf(&loads, "    s.%s = float4(u[%d], u[%d], u[%d], u[%d]);\n", n, 4+4*i, 5+4*i, 6+4*i, 7+4*i)
	}
	return `#include <metal_stdlib>
using namespace metal;
float mod(float x, float y) { return x - y * floor(x / y); }
float2 mod(float2 x, float2 y) { return x - y * floor(x / y); }
float3 mod(float3 x, float3 y) { return x - y * floor(x / y); }
float4 mod(float4 x, float4 y) { return x - y * floor(x / y); }
float2 mod(float2 x, float y) { return x - y * floor(x / y); }
float3 mod(float3 x, float y) { return x - y * floor(x / y); }
float4 mod(float4 x, float y) { return x - y * floor(x / y); }
float atan(float y, float x) { return atan2(y, x); }
struct RyUIShader {
    float time;
    float2 size;
` + members.String() + source + `
};
struct RyUIVertex { float4 position [[position]]; float2 uv; };
vertex RyUIVertex ryui_vertex(uint id [[vertex_id]]) {
    float2 p = float2((id << 1) & 2, id & 2);
    RyUIVertex v;
    v.position = float4(p * float2(2.0, -2.0) + float2(-1.0, 1.0), 0.0, 1.0);
    v.uv = p;
    return v;
}
fragment float4 ryui_fragment(RyUIVertex in [[stage_in]], constant float *u [[buffer(0)]]) {
    RyUIShader s;
    s.time = u[0];
    s.size = float2(u[1], u[2]);
` + loads.String() + `    float4 c = s.shade(in.uv);
    return float4(c.rgb * c.a, c.a);
}
`
}

func (s *shader) makePipeline() ID {
	key := s.source + "\x00" + strings.Join(s.names, ",")
	if p, ok := mtlPipelines[key]; ok {
		return p
	}
	var errID ID
	lib := Send(mtlDevice, "newLibraryWithSource:options:error:", uintptr(String(shaderProgram(s.source, s.names))), 0, uintptr(unsafe.Pointer(&errID)))
	if lib == 0 {
		fmt.Fprintf(os.Stderr, "ryui: ui.Shader's source doesn't compile with Metal: %s\n", GoString(Send(errID, "localizedDescription")))
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
		mtlPipelines[key] = p
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
	if s == nil || s.layer == 0 || s.source == "" || mtlQueue == 0 {
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
		u := append([]float32{float32(time.Since(s.start).Seconds()), float32(b.Size.Width), float32(b.Size.Height), 0}, s.values...)
		Send(enc, "setRenderPipelineState:", uintptr(s.pipeline))
		Send(enc, "setFragmentBytes:length:atIndex:", uintptr(unsafe.Pointer(&u[0])), uintptr(4*len(u)), 0)
		Send(enc, "drawPrimitives:vertexStart:vertexCount:", 3, 0, 3) // triangles
		Send(enc, "endEncoding")
		Send(buf, "presentDrawable:", uintptr(drawable))
		Send(buf, "commit")
	})
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
