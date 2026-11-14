//go:build windows

package windows

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Direct3D 11, as host/windows's shaders draw with it: a device, the
// shader compiler Windows has (d3dcompiler_47, in System32 from Windows
// 10 on), and the vtable slots of the interfaces they call (ComCall).

var (
	d3d11            = systemDLL("d3d11.dll")
	d3dcompiler      = systemDLL("d3dcompiler_47.dll")
	procD3D11Create  = d3d11.NewProc("D3D11CreateDevice")
	procD3DCompile   = d3dcompiler.NewProc("D3DCompile")
	d3dFeatureLevels = []uint32{0xb100, 0xb000, 0xa100, 0xa000} // 11_1, 11_0, 10_1, 10_0
	errNoD3DDevice   = fmt.Errorf("d3d11: no device")
)

// ID3D11Device's methods.
const (
	D3DDevCreateBuffer           = 3
	D3DDevCreateTexture2D        = 5
	D3DDevCreateRenderTargetView = 9
	D3DDevCreateVertexShader     = 12
	D3DDevCreatePixelShader      = 15
	D3DDevGetDeviceRemovedReason = 39
)

// ID3D11DeviceContext's methods.
const (
	D3DCtxPSSetShader            = 9
	D3DCtxVSSetShader            = 11
	D3DCtxDraw                   = 13
	D3DCtxMap                    = 14
	D3DCtxUnmap                  = 15
	D3DCtxPSSetConstantBuffers   = 16
	D3DCtxIASetPrimitiveTopology = 24
	D3DCtxOMSetRenderTargets     = 33
	D3DCtxRSSetViewports         = 44
	D3DCtxCopyResource           = 47
	D3DCtxUpdateSubresource      = 48
	D3DCtxClearState             = 110
)

// Direct3D's enums the shaders use.
const (
	D3DFormatB8G8R8A8   = 87 // DXGI_FORMAT_B8G8R8A8_UNORM
	D3DUsageDefault     = 0
	D3DUsageStaging     = 3
	D3DBindConstant     = 0x4
	D3DBindRenderTarget = 0x20
	D3DCPURead          = 0x20000
	D3DMapRead          = 1
	D3DTriangleList     = 4
)

// D3DTexture2DDesc is D3D11_TEXTURE2D_DESC.
type D3DTexture2DDesc struct {
	Width, Height, MipLevels, ArraySize, Format uint32
	SampleCount, SampleQuality                  uint32
	Usage, BindFlags, CPUAccessFlags, MiscFlags uint32
}

// D3DBufferDesc is D3D11_BUFFER_DESC.
type D3DBufferDesc struct {
	ByteWidth, Usage, BindFlags, CPUAccessFlags, MiscFlags, StructureByteStride uint32
}

// D3DViewport is D3D11_VIEWPORT.
type D3DViewport struct{ X, Y, W, H, MinDepth, MaxDepth float32 }

// D3DMapped is D3D11_MAPPED_SUBRESOURCE.
type D3DMapped struct {
	Data                 uintptr
	RowPitch, DepthPitch uint32
}

// NewD3D11Device creates a Direct3D 11 device and its immediate context:
// on the GPU, or, where there's none, with Windows's software
// rasterizer (WARP). The caller releases both. level is the device's
// feature level (0xb000: 11_0).
func NewD3D11Device() (device, context uintptr, level uint32, software bool, err error) {
	if err := procD3D11Create.Find(); err != nil {
		return 0, 0, 0, false, err
	}
	const hardware, warp = 1, 5
	const bgraSupport = 0x20
	var hr uintptr
	for _, driver := range []uintptr{hardware, warp} {
		levels := d3dFeatureLevels
		for len(levels) > 0 {
			hr, _, _ = procD3D11Create.Call(0, driver, 0, bgraSupport,
				uintptr(unsafe.Pointer(&levels[0])), uintptr(len(levels)), 7, // D3D11_SDK_VERSION
				uintptr(unsafe.Pointer(&device)), uintptr(unsafe.Pointer(&level)), uintptr(unsafe.Pointer(&context)))
			if !Failed(hr) {
				return device, context, level, driver == warp, nil
			}
			// Direct3D 11.0's runtime refuses 11_1 (E_INVALIDARG): without it.
			if uint32(hr) != 0x80070057 || levels[0] != 0xb100 {
				break
			}
			levels = levels[1:]
		}
	}
	return 0, 0, 0, false, fmt.Errorf("%w: %#x", errNoD3DDevice, uint32(hr))
}

// D3DCompile compiles the function entry of an HLSL source for target
// (ps_5_0, vs_4_0...), optimized, and returns its bytecode.
func D3DCompile(source, entry, target string) ([]byte, error) {
	if err := procD3DCompile.Find(); err != nil {
		return nil, fmt.Errorf("d3d11: no shader compiler: %w", err)
	}
	src := []byte(source)
	name, _ := syscall.BytePtrFromString("shader.hlsl")
	e, _ := syscall.BytePtrFromString(entry)
	t, _ := syscall.BytePtrFromString(target)
	const optimize3, ieeeStrictness = 1 << 15, 1 << 13
	var blob, errs uintptr
	hr, _, _ := procD3DCompile.Call(uintptr(unsafe.Pointer(&src[0])), uintptr(len(src)), uintptr(unsafe.Pointer(name)),
		0, 0, uintptr(unsafe.Pointer(e)), uintptr(unsafe.Pointer(t)), optimize3|ieeeStrictness, 0,
		uintptr(unsafe.Pointer(&blob)), uintptr(unsafe.Pointer(&errs)))
	if errs != 0 {
		defer Release(errs)
	}
	if Failed(hr) || blob == 0 {
		msg := fmt.Sprintf("%#x", uint32(hr))
		if errs != 0 {
			msg = string(blobBytes(errs))
		}
		return nil, fmt.Errorf("d3d11: the shader doesn't compile: %s", msg)
	}
	defer Release(blob)
	return append([]byte(nil), blobBytes(blob)...), nil
}

// blobBytes is an ID3DBlob's contents, until it's released.
func blobBytes(blob uintptr) []byte {
	p, n := ComCall(blob, 3), ComCall(blob, 4) // GetBufferPointer, GetBufferSize
	if p == 0 || n == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(native(p)), n)
}

// Row is n bytes of the mapped subresource's row y.
func (m *D3DMapped) Row(y, n int) []byte {
	return unsafe.Slice((*byte)(native(m.Data+uintptr(y)*uintptr(m.RowPitch))), n)
}
