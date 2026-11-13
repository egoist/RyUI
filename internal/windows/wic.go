//go:build windows

package windows

import (
	"errors"
	"image"
	"runtime"
	"unsafe"
)

// Images with Windows's own codecs (WIC): whatever the system decodes
// (JPEG, PNG, GIF, BMP, TIFF, ICO, and HEIC, WebP, AVIF, RAW where their
// extensions are installed), turned upright as their EXIF orientation
// says, scaled while decoding when asked.

var (
	clsidWICImagingFactory = NewGUID("CACAF262-9370-4615-A13B-9F5539DA4C0A")
	iidWICImagingFactory   = NewGUID("EC5EC8A9-C395-4314-9C77-54D7A935FF70")
	wicPixelFormatBGRA     = NewGUID("6FDDC324-4E03-4BFE-B185-3D77768DC90F")

	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procPropVariantClear = ole32.NewProc("PropVariantClear")
)

// IWICImagingFactory, IWICBitmapDecoder, IWICBitmapSource and the
// others' vtable indices.
const (
	wicCreateDecoderFromFilename = 3
	wicCreateFormatConverter     = 10
	wicCreateBitmapScaler        = 11
	decGetFrame                  = 13
	srcGetSize                   = 3
	srcCopyPixels                = 7
	frameGetMetadataQueryReader  = 8
	convInitialize               = 8
	scalerInitialize             = 8
	qrGetMetadataByName          = 5
)

// WithCOM runs f on a thread of its own in COM's multithreaded apartment
// (WIC, Media Foundation and WinRT objects off the main thread).
func WithCOM(f func()) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	hr, _, _ := procCoInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
	if !Failed(hr) {
		defer procCoUninitialize.Call()
	}
	f()
}

// ErrNoImage is a file that isn't there, or isn't an image Windows reads.
var ErrNoImage = errors.New("no image")

func wicFactory() uintptr {
	return CoCreate(&clsidWICImagingFactory, &iidWICImagingFactory)
}

// wicFrame opens path's first frame (a reference the caller releases),
// with its EXIF orientation (1 to 8).
func wicFrame(factory uintptr, path string) (frame uintptr, orientation int, err error) {
	var dec uintptr
	const genericRead, onDemand = 0x80000000, 0
	if hr := ComCall(factory, wicCreateDecoderFromFilename, Str(path), 0, genericRead, onDemand, uintptr(unsafe.Pointer(&dec))); Failed(hr) || dec == 0 {
		return 0, 0, ErrNoImage
	}
	defer Release(dec)
	if hr := ComCall(dec, decGetFrame, 0, uintptr(unsafe.Pointer(&frame))); Failed(hr) || frame == 0 {
		return 0, 0, ErrNoImage
	}
	return frame, wicOrientation(frame), nil
}

// wicOrientation is a frame's EXIF orientation (System.Photo.Orientation:
// JPEG's, TIFF's, HEIF's), 1 if it has none.
func wicOrientation(frame uintptr) int {
	var qr uintptr
	if Failed(ComCall(frame, frameGetMetadataQueryReader, uintptr(unsafe.Pointer(&qr)))) || qr == 0 {
		return 1
	}
	defer Release(qr)
	for _, name := range []string{"System.Photo.Orientation", "/app1/ifd/{ushort=274}", "/ifd/{ushort=274}"} {
		var pv [4]uint64 // PROPVARIANT (24 bytes), padded
		if Failed(ComCall(qr, qrGetMetadataByName, Str(name), uintptr(unsafe.Pointer(&pv[0])))) {
			continue
		}
		vt := uint16(pv[0])
		v := 0
		switch vt {
		case 18: // VT_UI2
			v = int(uint16(pv[1]))
		case 19, 3: // VT_UI4, VT_I4
			v = int(uint32(pv[1]))
		case 17: // VT_UI1
			v = int(uint8(pv[1]))
		}
		procPropVariantClear.Call(uintptr(unsafe.Pointer(&pv[0])))
		if v >= 1 && v <= 8 {
			return v
		}
	}
	return 1
}

// ImageFileSize is an image file's size in pixels, upright, without
// decoding it.
func ImageFileSize(path string) (w, h int, err error) {
	f := wicFactory()
	if f == 0 {
		return 0, 0, errors.New("WIC isn't there")
	}
	defer Release(f)
	frame, o, err := wicFrame(f, path)
	if err != nil {
		return 0, 0, err
	}
	defer Release(frame)
	var fw, fh uint32
	ComCall(frame, srcGetSize, uintptr(unsafe.Pointer(&fw)), uintptr(unsafe.Pointer(&fh)))
	if o >= 5 {
		fw, fh = fh, fw
	}
	return int(fw), int(fh), nil
}

// DecodeImageFile decodes an image file, upright; with maxSide > 0, a
// larger image is scaled down while it's decoded so its longer side is
// maxSide.
func DecodeImageFile(path string, maxSide int) (*image.NRGBA, error) {
	f := wicFactory()
	if f == 0 {
		return nil, errors.New("WIC isn't there")
	}
	defer Release(f)
	frame, o, err := wicFrame(f, path)
	if err != nil {
		return nil, err
	}
	defer Release(frame)
	src := frame
	var w, h uint32
	ComCall(frame, srcGetSize, uintptr(unsafe.Pointer(&w)), uintptr(unsafe.Pointer(&h)))
	if w == 0 || h == 0 {
		return nil, ErrNoImage
	}
	if long := max(w, h); maxSide > 0 && long > uint32(maxSide) {
		sw := max(1, uint32(uint64(w)*uint64(maxSide)/uint64(long)))
		sh := max(1, uint32(uint64(h)*uint64(maxSide)/uint64(long)))
		var sc uintptr
		if !Failed(ComCall(f, wicCreateBitmapScaler, uintptr(unsafe.Pointer(&sc)))) && sc != 0 {
			const fant = 3 // WICBitmapInterpolationModeFant: good when shrinking
			if !Failed(ComCall(sc, scalerInitialize, src, uintptr(sw), uintptr(sh), fant)) {
				defer Release(sc)
				src, w, h = sc, sw, sh
			} else {
				Release(sc)
			}
		}
	}
	var conv uintptr
	if Failed(ComCall(f, wicCreateFormatConverter, uintptr(unsafe.Pointer(&conv)))) || conv == 0 {
		return nil, ErrNoImage
	}
	defer Release(conv)
	// dither none, no palette, alpha threshold 0 (palettes only), custom
	// palette translation.
	if hr := ComCall(conv, convInitialize, src, uintptr(unsafe.Pointer(&wicPixelFormatBGRA)), 0, 0, 0, 0); Failed(hr) {
		return nil, ErrNoImage
	}
	stride := int(w) * 4
	buf := make([]byte, stride*int(h))
	if hr := ComCall(conv, srcCopyPixels, 0, uintptr(stride), uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0]))); Failed(hr) {
		return nil, ErrNoImage
	}
	for i := 0; i < len(buf); i += 4 {
		buf[i], buf[i+2] = buf[i+2], buf[i] // BGRA to RGBA
	}
	img := &image.NRGBA{Pix: buf, Stride: stride, Rect: image.Rect(0, 0, int(w), int(h))}
	return Orient(img, o), nil
}

// Orient turns an image as its EXIF orientation (1 to 8) says, so it's
// upright.
func Orient(img *image.NRGBA, o int) *image.NRGBA {
	if o <= 1 || o > 8 {
		return img
	}
	w, h := img.Rect.Dx(), img.Rect.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	out := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			var sx, sy int
			switch o {
			case 2:
				sx, sy = w-1-x, y
			case 3:
				sx, sy = w-1-x, h-1-y
			case 4:
				sx, sy = x, h-1-y
			case 5:
				sx, sy = y, x
			case 6:
				sx, sy = y, h-1-x
			case 7:
				sx, sy = w-1-y, h-1-x
			case 8:
				sx, sy = w-1-y, x
			}
			si := sy*img.Stride + sx*4
			di := y*out.Stride + x*4
			copy(out.Pix[di:di+4], img.Pix[si:si+4])
		}
	}
	return out
}
