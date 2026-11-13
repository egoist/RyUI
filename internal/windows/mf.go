//go:build windows

package windows

import (
	"errors"
	"fmt"
	"image"
	"sync"
	"syscall"
	"unsafe"
)

// Media Foundation, as the host's modules need it: media types, a sink
// writer encoding PCM to AAC (in MPEG-4, .m4a, or ADTS, .aac), a source
// reader taking a video's frame, the video capture devices. Its objects
// are free-threaded: a goroutine uses them from a thread in COM's
// multithreaded apartment (WithCOM).

var (
	mfplat                          = systemDLL("mfplat.dll")
	mfreadwrite                     = systemDLL("mfreadwrite.dll")
	mfdll                           = systemDLL("mf.dll")
	procMFStartup                   = mfplat.NewProc("MFStartup")
	procMFCreateMediaType           = mfplat.NewProc("MFCreateMediaType")
	procMFCreateAttributes          = mfplat.NewProc("MFCreateAttributes")
	procMFCreateMemoryBuffer        = mfplat.NewProc("MFCreateMemoryBuffer")
	procMFCreateSample              = mfplat.NewProc("MFCreateSample")
	procMFCreateSinkWriterFromURL   = mfreadwrite.NewProc("MFCreateSinkWriterFromURL")
	procMFCreateSourceReaderFromURL = mfreadwrite.NewProc("MFCreateSourceReaderFromURL")
	procMFEnumDeviceSources         = mfdll.NewProc("MFEnumDeviceSources")
)

// Media Foundation's GUIDs.
var (
	MFMTMajorType         = NewGUID("48EBA18E-F8C9-4687-BF11-0A74C9F96A8F")
	MFMTSubtype           = NewGUID("F7E34C9A-42E8-4714-B74B-CB29D72C35E5")
	MFMTAudioChannels     = NewGUID("37E48BF5-645E-4C5B-89DE-ADA9E29B696A")
	MFMTAudioRate         = NewGUID("5FAEEAE7-0290-4C31-9E8A-C534F68D9DBA")
	MFMTAudioBits         = NewGUID("F2DEB57F-40FA-4764-AA33-ED4F2D1FF669")
	MFMTAudioBytesPerSec  = NewGUID("1AAB75C8-CFEF-451C-AB95-AC034B8E1731")
	MFMTAudioBlockAlign   = NewGUID("322DE230-9EEB-43BD-AB7A-FF412251541D")
	MFMTAACPayload        = NewGUID("BFBABE79-7434-4D1C-94F0-72A3B9E17188")
	MFMTAACProfile        = NewGUID("7632F0E6-9538-4D61-ACDA-EA29C8C14456")
	MFMTAllIndependent    = NewGUID("C9173739-5E56-461C-B713-46FB995CB95F")
	MFMTFrameSize         = NewGUID("1652C33D-D6B2-4012-B834-72030849A37D")
	MFMTFrameRate         = NewGUID("C459A2E8-3D2C-4E44-B132-FEE5156C7BB0")
	MFMTAvgBitrate        = NewGUID("20332624-FB0D-4D9E-BD0D-CBF6786C102E")
	MFMTInterlace         = NewGUID("E2724BB8-E676-4806-B4B2-A8D6EFB44CCD")
	MFMTPixelAspect       = NewGUID("C6376A1E-8D0A-4027-BE45-6D9A0AD39BB6")
	MFMTDefaultStride     = NewGUID("644B4E48-1E02-4516-B0EB-C01CA9D49AC6")
	MFMediaTypeAudio      = NewGUID("73647561-0000-0010-8000-00AA00389B71")
	MFMediaTypeVideo      = NewGUID("73646976-0000-0010-8000-00AA00389B71")
	MFAudioFormatPCM      = NewGUID("00000001-0000-0010-8000-00AA00389B71")
	MFAudioFormatAAC      = NewGUID("00001610-0000-0010-8000-00AA00389B71")
	MFVideoFormatH264     = NewGUID("34363248-0000-0010-8000-00AA00389B71")
	MFVideoFormatRGB32    = NewGUID("00000016-0000-0010-8000-00AA00389B71")
	MFImageFormatJPEG     = NewGUID("19E4A5AA-5662-4FC5-A0C0-1758028E1057")
	mfTranscodeContainer  = NewGUID("150FF23F-4ABC-478B-AC4F-E1916FBA1CCA")
	mfContainerMPEG4      = NewGUID("DC6CD05D-B9D0-40EF-BD35-FA622C1AB28A")
	mfContainerADTS       = NewGUID("132FD27D-0F02-43DE-A301-38FBBBB3834E")
	mfReaderVideoProcess  = NewGUID("FB394F3D-CCF1-42EE-BBB3-F9B845D5681D")
	mfPDDuration          = NewGUID("6C990D33-BB8E-477A-8598-0D5D96FCD88A")
	mfDevSourceType       = NewGUID("C60AC5FE-252A-478F-A0EF-BC8FA5F7CAD3")
	mfDevSourceVidcap     = NewGUID("8AC3587A-4AE7-42D8-99E0-0A6013EEF90F")
	mfDevSourceName       = NewGUID("60D0E559-52F8-4FA2-BBCE-ACDB34A8EC01")
	mfDevSourceVidcapLink = NewGUID("58F0AAD8-22BF-4F8A-BB3D-D2C4978C6E2F")
	iidIMFMediaSource     = NewGUID("279A808D-AEC7-40C8-9C6B-A6B492C78A66")
)

// IMFAttributes' (and so IMFMediaType's, IMFSample's...) vtable indices.
const (
	attrGetUINT32  = 7
	attrGetUINT64  = 8
	attrGetGUID    = 10
	attrGetString  = 12
	attrGetStrLen  = 11
	attrSetUINT32  = 21
	attrSetUINT64  = 22
	attrSetGUID    = 24
	sampleSetTime  = 36
	sampleSetDur   = 38
	sampleAddBuf   = 42
	sampleToBuffer = 41
	bufLock        = 3
	bufUnlock      = 4
	bufSetLen      = 6
	bufGetLen      = 5
)

var mfStarted = sync.OnceValue(func() error {
	if err := procMFStartup.Find(); err != nil {
		return errors.New("Media Foundation isn't on this Windows (an N edition without its Media Feature Pack)")
	}
	if hr, _, _ := procMFStartup.Call(0x00020070, 0); Failed(hr) { // MF_VERSION, MFSTARTUP_FULL
		return fmt.Errorf("Media Foundation: %#x", uint32(hr))
	}
	return nil
})

// MFStart starts Media Foundation, once.
func MFStart() error { return mfStarted() }

// MediaType is an IMFMediaType (or IMFAttributes).
type MediaType uintptr

// NewMediaType makes an empty media type (a reference the caller
// releases).
func NewMediaType() (MediaType, error) {
	var t uintptr
	if hr, _, _ := procMFCreateMediaType.Call(uintptr(unsafe.Pointer(&t))); Failed(hr) {
		return 0, fmt.Errorf("MFCreateMediaType: %#x", uint32(hr))
	}
	return MediaType(t), nil
}

// NewAttributes makes an attribute store.
func NewAttributes() (MediaType, error) {
	var a uintptr
	if hr, _, _ := procMFCreateAttributes.Call(uintptr(unsafe.Pointer(&a)), 4); Failed(hr) {
		return 0, fmt.Errorf("MFCreateAttributes: %#x", uint32(hr))
	}
	return MediaType(a), nil
}

func (t MediaType) SetGUID(key, v GUID) {
	ComCall(uintptr(t), attrSetGUID, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&v)))
}

func (t MediaType) SetU32(key GUID, v uint32) {
	ComCall(uintptr(t), attrSetUINT32, uintptr(unsafe.Pointer(&key)), uintptr(v))
}

func (t MediaType) SetU64(key GUID, v uint64) {
	ComCall(uintptr(t), attrSetUINT64, uintptr(unsafe.Pointer(&key)), uintptr(v))
}

// SetPair sets a pair of 32-bit values (a frame's size, a rate).
func (t MediaType) SetPair(key GUID, hi, lo uint32) { t.SetU64(key, uint64(hi)<<32|uint64(lo)) }

func (t MediaType) U32(key GUID) (uint32, bool) {
	var v uint32
	return v, !Failed(ComCall(uintptr(t), attrGetUINT32, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&v))))
}

func (t MediaType) U64(key GUID) (uint64, bool) {
	var v uint64
	return v, !Failed(ComCall(uintptr(t), attrGetUINT64, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&v))))
}

func (t MediaType) GUIDOf(key GUID) (GUID, bool) {
	var v GUID
	return v, !Failed(ComCall(uintptr(t), attrGetGUID, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&v))))
}

// String is a string attribute.
func (t MediaType) String(key GUID) string {
	var n uint32
	if Failed(ComCall(uintptr(t), attrGetStrLen, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&n)))) {
		return ""
	}
	buf := make([]uint16, n+1)
	if Failed(ComCall(uintptr(t), attrGetString, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)) {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

func (t MediaType) Release() { Release(uintptr(t)) }

// PCMType is 16-bit PCM of channels at rate.
func PCMType(channels, rate int) (MediaType, error) {
	t, err := NewMediaType()
	if err != nil {
		return 0, err
	}
	t.SetGUID(MFMTMajorType, MFMediaTypeAudio)
	t.SetGUID(MFMTSubtype, MFAudioFormatPCM)
	t.SetU32(MFMTAudioChannels, uint32(channels))
	t.SetU32(MFMTAudioRate, uint32(rate))
	t.SetU32(MFMTAudioBits, 16)
	t.SetU32(MFMTAudioBlockAlign, uint32(2*channels))
	t.SetU32(MFMTAudioBytesPerSec, uint32(2*channels*rate))
	t.SetU32(MFMTAllIndependent, 1)
	return t, nil
}

// AACType is AAC-LC of channels at rate (44.1 or 48 kHz), bytesPerSec
// (12000, 16000, 20000 or 24000: what Windows's encoder makes).
func AACType(channels, rate, bytesPerSec int) (MediaType, error) {
	t, err := NewMediaType()
	if err != nil {
		return 0, err
	}
	t.SetGUID(MFMTMajorType, MFMediaTypeAudio)
	t.SetGUID(MFMTSubtype, MFAudioFormatAAC)
	t.SetU32(MFMTAudioChannels, uint32(channels))
	t.SetU32(MFMTAudioRate, uint32(rate))
	t.SetU32(MFMTAudioBits, 16)
	t.SetU32(MFMTAudioBytesPerSec, uint32(bytesPerSec))
	t.SetU32(MFMTAACProfile, 0x29) // AAC profile, level 2
	return t, nil
}

// AudioWriter encodes 16-bit PCM to AAC in a file: MPEG-4 (.m4a) or
// ADTS (.aac, each frame whole in itself).
type AudioWriter struct {
	w              uintptr // IMFSinkWriter
	stream         uint32
	channels, rate int
	written        int64 // frames
}

// NewAudioWriter starts a file at path.
func NewAudioWriter(path string, channels, rate, bytesPerSec int, adts bool) (*AudioWriter, error) {
	if err := MFStart(); err != nil {
		return nil, err
	}
	if err := procMFCreateSinkWriterFromURL.Find(); err != nil {
		return nil, errors.New("Media Foundation has no sink writer here")
	}
	attrs, err := NewAttributes()
	if err != nil {
		return nil, err
	}
	defer attrs.Release()
	if adts {
		attrs.SetGUID(mfTranscodeContainer, mfContainerADTS)
	} else {
		attrs.SetGUID(mfTranscodeContainer, mfContainerMPEG4)
	}
	a := &AudioWriter{channels: channels, rate: rate}
	if hr, _, _ := procMFCreateSinkWriterFromURL.Call(Str(path), 0, uintptr(attrs), uintptr(unsafe.Pointer(&a.w))); Failed(hr) {
		return nil, fmt.Errorf("couldn't write %s: %#x", path, uint32(hr))
	}
	out, err := AACType(channels, rate, bytesPerSec)
	if err != nil {
		a.release()
		return nil, err
	}
	defer out.Release()
	if hr := ComCall(a.w, 3, uintptr(out), uintptr(unsafe.Pointer(&a.stream))); Failed(hr) { // AddStream
		a.release()
		return nil, fmt.Errorf("no AAC encoder: %#x", uint32(hr))
	}
	in, err := PCMType(channels, rate)
	if err != nil {
		a.release()
		return nil, err
	}
	defer in.Release()
	if hr := ComCall(a.w, 4, uintptr(a.stream), uintptr(in), 0); Failed(hr) { // SetInputMediaType
		a.release()
		return nil, fmt.Errorf("the AAC encoder refused the sound: %#x", uint32(hr))
	}
	if hr := ComCall(a.w, 5); Failed(hr) { // BeginWriting
		a.release()
		return nil, fmt.Errorf("couldn't start writing: %#x", uint32(hr))
	}
	return a, nil
}

// Write adds PCM (whole frames), which follows what was written before.
func (a *AudioWriter) Write(pcm []byte) error {
	frames := int64(len(pcm) / (2 * a.channels))
	if frames == 0 {
		return nil
	}
	s, err := NewSample(pcm)
	if err != nil {
		return err
	}
	defer Release(s)
	ComCall(s, sampleSetTime, uintptr(a.written*10_000_000/int64(a.rate)))
	ComCall(s, sampleSetDur, uintptr(frames*10_000_000/int64(a.rate)))
	a.written += frames
	if hr := ComCall(a.w, 6, uintptr(a.stream), s); Failed(hr) { // WriteSample
		return fmt.Errorf("couldn't encode: %#x", uint32(hr))
	}
	return nil
}

// Close finishes the file.
func (a *AudioWriter) Close() error {
	if a.w == 0 {
		return nil
	}
	hr := ComCall(a.w, 11) // Finalize
	a.release()
	if Failed(hr) {
		return fmt.Errorf("couldn't finish the file: %#x", uint32(hr))
	}
	return nil
}

func (a *AudioWriter) release() {
	Release(a.w)
	a.w = 0
}

// NewSample is a sample holding a copy of b (a reference the caller
// releases).
func NewSample(b []byte) (uintptr, error) {
	var buf, s uintptr
	if hr, _, _ := procMFCreateMemoryBuffer.Call(uintptr(len(b)), uintptr(unsafe.Pointer(&buf))); Failed(hr) {
		return 0, fmt.Errorf("MFCreateMemoryBuffer: %#x", uint32(hr))
	}
	defer Release(buf)
	var p uintptr
	ComCall(buf, bufLock, uintptr(unsafe.Pointer(&p)), 0, 0)
	copy(unsafe.Slice((*byte)(native(p)), len(b)), b)
	ComCall(buf, bufUnlock)
	ComCall(buf, bufSetLen, uintptr(len(b)))
	if hr, _, _ := procMFCreateSample.Call(uintptr(unsafe.Pointer(&s))); Failed(hr) {
		return 0, fmt.Errorf("MFCreateSample: %#x", uint32(hr))
	}
	ComCall(s, sampleAddBuf, buf)
	return s, nil
}

// VideoFrame decodes the frame of a video (a file or URL) at ms
// milliseconds (its first for 0; its last if it's shorter), as RGB.
func VideoFrame(src string, ms int64) (*image.NRGBA, error) {
	if err := MFStart(); err != nil {
		return nil, err
	}
	attrs, err := NewAttributes()
	if err != nil {
		return nil, err
	}
	defer attrs.Release()
	attrs.SetU32(mfReaderVideoProcess, 1) // decode to RGB32, whatever the codec gives
	var r uintptr
	if hr, _, _ := procMFCreateSourceReaderFromURL.Call(Str(src), uintptr(attrs), uintptr(unsafe.Pointer(&r))); Failed(hr) || r == 0 {
		return nil, ErrNoImage
	}
	defer Release(r)
	const firstVideo, allStreams = 0xFFFFFFFC, 0xFFFFFFFE
	ComCall(r, 4, allStreams, 0) // SetStreamSelection: none
	ComCall(r, 4, firstVideo, 1) // but the video
	t, err := NewMediaType()
	if err != nil {
		return nil, err
	}
	t.SetGUID(MFMTMajorType, MFMediaTypeVideo)
	t.SetGUID(MFMTSubtype, MFVideoFormatRGB32)
	hr := ComCall(r, 7, firstVideo, 0, uintptr(t)) // SetCurrentMediaType
	t.Release()
	if Failed(hr) {
		return nil, ErrNoImage
	}
	var cur uintptr
	if Failed(ComCall(r, 6, firstVideo, uintptr(unsafe.Pointer(&cur)))) { // GetCurrentMediaType
		return nil, ErrNoImage
	}
	size, _ := MediaType(cur).U64(MFMTFrameSize)
	stride, hasStride := MediaType(cur).U32(MFMTDefaultStride)
	MediaType(cur).Release()
	w, h := int(size>>32), int(uint32(size))
	if w <= 0 || h <= 0 {
		return nil, ErrNoImage
	}
	if !hasStride {
		stride = uint32(w * 4)
	}
	at := ms * 10_000 // 100 ns
	if ms > 0 {
		var pv [4]uint64 // PROPVARIANT: VT_I8 (20)
		pv[0] = 20
		pv[1] = uint64(at)
		var timeFormat GUID                                                                  // GUID_NULL: 100 ns
		ComCall(r, 8, uintptr(unsafe.Pointer(&timeFormat)), uintptr(unsafe.Pointer(&pv[0]))) // SetCurrentPosition
	}
	// Read until the frame at the time (seeking lands on the key frame
	// before it); past the end, the last one read.
	var last uintptr
	defer func() { Release(last) }()
	for i := 0; i < 600; i++ {
		var stream, flags uint32
		var ts int64
		var s uintptr
		if hr := ComCall(r, 9, firstVideo, 0, uintptr(unsafe.Pointer(&stream)), uintptr(unsafe.Pointer(&flags)), uintptr(unsafe.Pointer(&ts)), uintptr(unsafe.Pointer(&s))); Failed(hr) {
			break
		}
		if s != 0 {
			Release(last)
			last = s
		}
		if flags&0x2 != 0 || (s != 0 && ts >= at-10_000) { // MF_SOURCE_READERF_ENDOFSTREAM
			break
		}
	}
	if last == 0 {
		return nil, ErrNoImage
	}
	var buf uintptr
	if Failed(ComCall(last, sampleToBuffer, uintptr(unsafe.Pointer(&buf)))) {
		return nil, ErrNoImage
	}
	defer Release(buf)
	var p uintptr
	var n uint32
	ComCall(buf, bufLock, uintptr(unsafe.Pointer(&p)), 0, uintptr(unsafe.Pointer(&n)))
	defer ComCall(buf, bufUnlock)
	st := int(int32(stride))
	bottomUp := st < 0
	if bottomUp {
		st = -st
	}
	if st < w*4 || int(n) < st*h {
		return nil, ErrNoImage
	}
	src2 := unsafe.Slice((*byte)(native(p)), int(n))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := y
		if bottomUp {
			sy = h - 1 - y
		}
		row := src2[sy*st:]
		d := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			d[x*4], d[x*4+1], d[x*4+2], d[x*4+3] = row[x*4+2], row[x*4+1], row[x*4], 255
		}
	}
	return img, nil
}

// VideoDevice is a camera.
type VideoDevice struct {
	Name, Link string
	activate   uintptr
}

// VideoDevices are the cameras (activation objects the caller lets go
// with ReleaseDevices).
func VideoDevices() ([]VideoDevice, error) {
	if err := MFStart(); err != nil {
		return nil, err
	}
	if err := procMFEnumDeviceSources.Find(); err != nil {
		return nil, errors.New("no cameras on this Windows")
	}
	attrs, err := NewAttributes()
	if err != nil {
		return nil, err
	}
	defer attrs.Release()
	attrs.SetGUID(mfDevSourceType, mfDevSourceVidcap)
	var arr uintptr
	var n uint32
	if hr, _, _ := procMFEnumDeviceSources.Call(uintptr(attrs), uintptr(unsafe.Pointer(&arr)), uintptr(unsafe.Pointer(&n))); Failed(hr) {
		return nil, fmt.Errorf("MFEnumDeviceSources: %#x", uint32(hr))
	}
	var out []VideoDevice
	for i := 0; i < int(n); i++ {
		a := *(*uintptr)(unsafe.Add(native(arr), i*int(unsafe.Sizeof(uintptr(0)))))
		out = append(out, VideoDevice{Name: MediaType(a).String(mfDevSourceName), Link: MediaType(a).String(mfDevSourceVidcapLink), activate: a})
	}
	if arr != 0 {
		procCoTaskMemFree.Call(arr)
	}
	return out, nil
}

// ReleaseDevices lets the devices go.
func ReleaseDevices(ds []VideoDevice) {
	for _, d := range ds {
		Release(d.activate)
	}
}

// Activate is the device's media source (a reference the caller
// releases).
func (d VideoDevice) Activate() (uintptr, error) {
	if d.activate == 0 {
		return 0, errors.New("no camera")
	}
	var src uintptr
	if hr := ComCall(d.activate, 33, uintptr(unsafe.Pointer(&iidIMFMediaSource)), uintptr(unsafe.Pointer(&src))); Failed(hr) || src == 0 { // ActivateObject
		return 0, fmt.Errorf("the camera: %#x", uint32(hr))
	}
	return src, nil
}

// VideoWriter encodes frames (32-bit BGRX, top-down) to H.264 in an
// MPEG-4 file.
type VideoWriter struct {
	w           uintptr
	stream      uint32
	width, high int
	fps, frames int64
}

// NewVideoWriter starts a video of w×h at fps.
func NewVideoWriter(path string, w, h, fps int) (*VideoWriter, error) {
	if err := MFStart(); err != nil {
		return nil, err
	}
	v := &VideoWriter{width: w, high: h, fps: int64(fps)}
	if hr, _, _ := procMFCreateSinkWriterFromURL.Call(Str(path), 0, 0, uintptr(unsafe.Pointer(&v.w))); Failed(hr) {
		return nil, fmt.Errorf("couldn't write %s: %#x", path, uint32(hr))
	}
	set := func(t MediaType, sub GUID) {
		t.SetGUID(MFMTMajorType, MFMediaTypeVideo)
		t.SetGUID(MFMTSubtype, sub)
		t.SetPair(MFMTFrameSize, uint32(w), uint32(h))
		t.SetPair(MFMTFrameRate, uint32(fps), 1)
		t.SetPair(MFMTPixelAspect, 1, 1)
		t.SetU32(MFMTInterlace, 2) // progressive
	}
	out, err := NewMediaType()
	if err != nil {
		Release(v.w)
		return nil, err
	}
	defer out.Release()
	set(out, MFVideoFormatH264)
	out.SetU32(MFMTAvgBitrate, uint32(w*h*fps/8))
	if hr := ComCall(v.w, 3, uintptr(out), uintptr(unsafe.Pointer(&v.stream))); Failed(hr) {
		Release(v.w)
		return nil, fmt.Errorf("no H.264 encoder: %#x", uint32(hr))
	}
	in, err := NewMediaType()
	if err != nil {
		Release(v.w)
		return nil, err
	}
	defer in.Release()
	set(in, MFVideoFormatRGB32)
	in.SetU32(MFMTDefaultStride, uint32(w*4))
	if hr := ComCall(v.w, 4, uintptr(v.stream), uintptr(in), 0); Failed(hr) {
		Release(v.w)
		return nil, fmt.Errorf("the H.264 encoder refused the frames: %#x", uint32(hr))
	}
	if hr := ComCall(v.w, 5); Failed(hr) {
		Release(v.w)
		return nil, fmt.Errorf("couldn't start writing: %#x", uint32(hr))
	}
	return v, nil
}

// Write adds a frame.
func (v *VideoWriter) Write(bgrx []byte) error {
	s, err := NewSample(bgrx)
	if err != nil {
		return err
	}
	defer Release(s)
	ComCall(s, sampleSetTime, uintptr(v.frames*10_000_000/v.fps))
	ComCall(s, sampleSetDur, uintptr(10_000_000/v.fps))
	v.frames++
	if hr := ComCall(v.w, 6, uintptr(v.stream), s); Failed(hr) {
		return fmt.Errorf("couldn't encode: %#x", uint32(hr))
	}
	return nil
}

// Close finishes the file.
func (v *VideoWriter) Close() error {
	hr := ComCall(v.w, 11)
	Release(v.w)
	if Failed(hr) {
		return fmt.Errorf("couldn't finish the file: %#x", uint32(hr))
	}
	return nil
}
