//go:build windows

package windows

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/egoist/asuka/internal/shader"
	"github.com/egoist/asuka/internal/shadercode"
	"github.com/egoist/asuka/internal/shaderir/hlsl"
	. "github.com/egoist/asuka/internal/windows"
)

// Shaders on the GPU: one Direct3D 11 device (the GPU's, or WARP where
// there's none) draws every shader's frames, one at a time, since its
// immediate context serves one thread at a time. A frame is a triangle
// over a texture of the view's size in pixels, drawn by the shader's
// pixel shader (its Kage program compiled to HLSL by package hlsl, then
// by Windows's compiler, once per program), copied to a staging texture
// and read back: the goroutine waits for the GPU, never the main thread.
// A view larger than a texture can be renders at a fraction of its
// pixels, which the program's positions are still.

var d3d struct {
	mu        sync.Mutex
	device    uintptr
	ctx       uintptr
	level     uint32
	software  bool  // WARP's
	err       error // why there's no device
	vs        uintptr
	pipelines map[string]*d3dPipeline // by Kage source
}

// d3dPipeline is a program's pixel shader, and where its lanes go in its
// constant buffer.
type d3dPipeline struct {
	ps    uintptr
	lanes []int
	size  int
	err   error
}

// d3dTarget is a shader's own on the GPU: the texture its frames draw
// into, the one they're copied to for the CPU, and its constant buffers.
// Only the frames' goroutines touch it, holding d3d.mu.
type d3dTarget struct {
	device            uintptr // the device it was made on
	w, h              int
	tex, rtv, staging uintptr
	cb, stepCB        uintptr
	cbSize            int
	cbuf              []byte
	released          bool
}

// render draws a frame of c with the lanes in, for a view of w by ht
// pixels, into buf if it's large enough, and returns its premultiplied
// BGRA pixels, pw by ph. pix is nil when the target was released.
func (t *d3dTarget) render(c *shadercode.Code, in []uint32, w, ht int, buf []byte) (pix []byte, pw, ph int, err error) {
	d3d.mu.Lock()
	defer d3d.mu.Unlock()
	for range 2 { // again on a new device if the GPU was lost
		if t.released {
			return nil, 0, 0, nil
		}
		if err := d3dDevice(); err != nil {
			return nil, 0, 0, err
		}
		pix, pw, ph, err = t.draw(c, in, w, ht, buf)
		if err == nil || ComCall(d3d.device, D3DDevGetDeviceRemovedReason) == 0 {
			return pix, pw, ph, err
		}
		d3dReset()
	}
	return nil, 0, 0, err
}

// d3dDevice makes the device and the vertex shader the first time.
func d3dDevice() error {
	if d3d.device != 0 || d3d.err != nil {
		return d3d.err
	}
	dev, ctx, level, software, err := NewD3D11Device()
	if err != nil {
		d3d.err = err
		return err
	}
	var vs uintptr
	code, err := D3DCompile(hlslVertex, "asuka_vs", hlslTarget("vs", level))
	if err == nil {
		if hr := ComCall(dev, D3DDevCreateVertexShader, uintptr(unsafe.Pointer(&code[0])), uintptr(len(code)), 0, uintptr(unsafe.Pointer(&vs))); Failed(hr) {
			err = fmt.Errorf("d3d11: no vertex shader: %#x", uint32(hr))
		}
	}
	if err != nil {
		Release(ctx)
		Release(dev)
		d3d.err = err
		return err
	}
	d3d.device, d3d.ctx, d3d.level, d3d.software, d3d.vs = dev, ctx, level, software, vs
	d3d.pipelines = map[string]*d3dPipeline{}
	return nil
}

// d3dReset lets go of a lost device and what was made on it; targets
// make theirs again on the next.
func d3dReset() {
	for _, p := range d3d.pipelines {
		Release(p.ps)
	}
	d3d.pipelines = nil
	Release(d3d.vs)
	Release(d3d.ctx)
	Release(d3d.device)
	d3d.device, d3d.ctx, d3d.vs = 0, 0, 0
}

// hlslTarget is the shader model of a feature level: 5 on 11_0 and
// later, 4 before.
func hlslTarget(stage string, level uint32) string {
	if level >= 0xb000 {
		return stage + "_5_0"
	}
	return stage + "_4_0"
}

// draw draws the frame on the device, then reads it back.
func (t *d3dTarget) draw(c *shadercode.Code, in []uint32, w, ht int, buf []byte) (pix []byte, pw, ph int, err error) {
	p := d3dPipelineOf(c)
	if p.err != nil {
		return nil, 0, 0, p.err
	}
	// A view larger than a texture can be renders at a fraction of its
	// pixels.
	m := 8192 // D3D10_REQ_TEXTURE2D_U_OR_V_DIMENSION
	if d3d.level >= 0xb000 {
		m = 16384
	}
	pw, ph = w, ht
	if pw > m || ph > m {
		f := min(float64(m)/float64(pw), float64(m)/float64(ph))
		pw, ph = max(1, min(m, int(float64(pw)*f))), max(1, min(m, int(float64(ph)*f)))
	}
	if err := t.prepare(pw, ph, p.size); err != nil {
		return nil, 0, 0, err
	}
	ctx := d3d.ctx
	if t.cb != 0 {
		for i, o := range p.lanes {
			binary.LittleEndian.PutUint32(t.cbuf[o:], in[i])
		}
		ComCall(ctx, D3DCtxUpdateSubresource, t.cb, 0, 0, uintptr(unsafe.Pointer(&t.cbuf[0])), 0, 0)
	}
	step := [4]float32{float32(w) / float32(pw), float32(ht) / float32(ph)}
	ComCall(ctx, D3DCtxUpdateSubresource, t.stepCB, 0, 0, uintptr(unsafe.Pointer(&step[0])), 0, 0)

	ComCall(ctx, D3DCtxClearState)
	ComCall(ctx, D3DCtxOMSetRenderTargets, 1, uintptr(unsafe.Pointer(&t.rtv)), 0)
	vp := D3DViewport{W: float32(pw), H: float32(ph), MaxDepth: 1}
	ComCall(ctx, D3DCtxRSSetViewports, 1, uintptr(unsafe.Pointer(&vp)))
	ComCall(ctx, D3DCtxIASetPrimitiveTopology, D3DTriangleList)
	ComCall(ctx, D3DCtxVSSetShader, d3d.vs, 0, 0)
	ComCall(ctx, D3DCtxPSSetShader, p.ps, 0, 0)
	cbs := [2]uintptr{t.cb, t.stepCB}
	ComCall(ctx, D3DCtxPSSetConstantBuffers, 0, 2, uintptr(unsafe.Pointer(&cbs[0])))
	ComCall(ctx, D3DCtxDraw, 3, 0)
	ComCall(ctx, D3DCtxCopyResource, t.staging, t.tex)

	var mp D3DMapped
	if hr := ComCall(ctx, D3DCtxMap, t.staging, 0, D3DMapRead, 0, uintptr(unsafe.Pointer(&mp))); Failed(hr) {
		return nil, 0, 0, fmt.Errorf("d3d11: the frame doesn't map: %#x", uint32(hr))
	}
	defer ComCall(ctx, D3DCtxUnmap, t.staging, 0)
	if cap(buf) < pw*ph*4 {
		buf = make([]byte, pw*ph*4)
	}
	pix = buf[:pw*ph*4]
	for y := range ph {
		copy(pix[y*pw*4:(y+1)*pw*4], mp.Row(y, pw*4))
	}
	return pix, pw, ph, nil
}

// prepare makes the target's textures for frames of w by ht pixels, and
// its constant buffers, on the current device.
func (t *d3dTarget) prepare(w, ht, cbSize int) error {
	if t.device != d3d.device {
		t.free()
		t.device = d3d.device
	}
	dev := d3d.device
	if t.stepCB == 0 {
		desc := D3DBufferDesc{ByteWidth: 16, Usage: D3DUsageDefault, BindFlags: D3DBindConstant}
		if hr := ComCall(dev, D3DDevCreateBuffer, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&t.stepCB))); Failed(hr) {
			return fmt.Errorf("d3d11: no constant buffer: %#x", uint32(hr))
		}
	}
	if t.cbSize != cbSize {
		Release(t.cb)
		t.cb, t.cbSize = 0, 0
		if cbSize > 0 {
			desc := D3DBufferDesc{ByteWidth: uint32(cbSize), Usage: D3DUsageDefault, BindFlags: D3DBindConstant}
			if hr := ComCall(dev, D3DDevCreateBuffer, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&t.cb))); Failed(hr) {
				return fmt.Errorf("d3d11: no constant buffer: %#x", uint32(hr))
			}
			t.cbSize = cbSize
			t.cbuf = make([]byte, cbSize)
		}
	}
	if t.tex != 0 && t.w == w && t.h == ht {
		return nil
	}
	for _, p := range []*uintptr{&t.rtv, &t.tex, &t.staging} {
		Release(*p)
		*p = 0
	}
	t.w, t.h = 0, 0
	desc := D3DTexture2DDesc{Width: uint32(w), Height: uint32(ht), MipLevels: 1, ArraySize: 1, Format: D3DFormatB8G8R8A8,
		SampleCount: 1, Usage: D3DUsageDefault, BindFlags: D3DBindRenderTarget}
	if hr := ComCall(dev, D3DDevCreateTexture2D, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&t.tex))); Failed(hr) {
		return fmt.Errorf("d3d11: no texture of %dx%d: %#x", w, ht, uint32(hr))
	}
	if hr := ComCall(dev, D3DDevCreateRenderTargetView, t.tex, 0, uintptr(unsafe.Pointer(&t.rtv))); Failed(hr) {
		return fmt.Errorf("d3d11: no render target: %#x", uint32(hr))
	}
	desc.Usage, desc.BindFlags, desc.CPUAccessFlags = D3DUsageStaging, 0, D3DCPURead
	if hr := ComCall(dev, D3DDevCreateTexture2D, uintptr(unsafe.Pointer(&desc)), 0, uintptr(unsafe.Pointer(&t.staging))); Failed(hr) {
		return fmt.Errorf("d3d11: no staging texture: %#x", uint32(hr))
	}
	t.w, t.h = w, ht
	return nil
}

// release lets go of the target's resources; it renders no more.
func (t *d3dTarget) release() {
	d3d.mu.Lock()
	defer d3d.mu.Unlock()
	t.released = true
	t.free()
}

func (t *d3dTarget) free() {
	for _, p := range []*uintptr{&t.rtv, &t.tex, &t.staging, &t.cb, &t.stepCB} {
		Release(*p)
		*p = 0
	}
	t.device, t.w, t.h, t.cbSize = 0, 0, 0, 0
}

// d3dPipelineOf is c's pipeline, compiled the first time.
func d3dPipelineOf(c *shadercode.Code) *d3dPipeline {
	if p := d3d.pipelines[c.Source]; p != nil {
		return p
	}
	p := &d3dPipeline{}
	d3d.pipelines[c.Source] = p
	src, lanes, size, err := hlslSource(c)
	if err == nil {
		var code []byte
		if code, err = D3DCompile(src, "asuka_ps", hlslTarget("ps", d3d.level)); err == nil {
			if hr := ComCall(d3d.device, D3DDevCreatePixelShader, uintptr(unsafe.Pointer(&code[0])), uintptr(len(code)), 0, uintptr(unsafe.Pointer(&p.ps))); Failed(hr) {
				err = fmt.Errorf("d3d11: no pixel shader: %#x", uint32(hr))
			}
		}
	}
	p.lanes, p.size, p.err = lanes, size, err
	return p
}

// hlslSource is c's pixel shader, asuka_ps, and where each lane goes in
// its constant buffer (register b0); asuka_step (b1) is 1 but where the
// frame has fewer pixels than the view. Its colors are clamped,
// premultiplied, as GDI+ draws them.
func hlslSource(c *shadercode.Code) (src string, lanes []int, size int, err error) {
	p, err := shader.CompileFragment([]byte(c.Source))
	if err != nil {
		return "", nil, 0, err
	}
	scale := -1
	for i, u := range c.Uniforms {
		if u.Name == shader.ScaleUniform {
			scale = i
		}
	}
	if scale < 0 {
		return "", nil, 0, errors.New("shader: no scale uniform in the bridge")
	}
	lanes, size = hlsl.UniformLanes(p)
	if len(lanes) != c.Lanes {
		return "", nil, 0, fmt.Errorf("shader: %d lanes, the code has %d", len(lanes), c.Lanes)
	}
	return hlsl.Fragment(p, "asuka_shade", 0, 0) + fmt.Sprintf(hlslEntry, scale), lanes, size, nil
}

const hlslVertex = `
float4 asuka_vs(uint id : SV_VertexID) : SV_Position {
	float2 p = float2((id << 1) & 2, id & 2);
	return float4(p * float2(2, -2) + float2(-1, 1), 0, 1);
}
`

const hlslEntry = `
cbuffer AsukaStep : register(b1) {
	float2 asuka_step;
}

float4 asuka_ps(float4 position : SV_Position) : SV_Target {
	float4 p = float4(position.xy * asuka_step, position.zw);
	float4 c = saturate(asuka_shade(p, p.xy / U%d));
	return float4(min(c.rgb, c.aaa), c.a);
}
`
