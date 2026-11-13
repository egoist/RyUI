//go:build windows

package windows

import (
	"errors"
	"fmt"
	"unsafe"
)

// The camera, with Media Foundation's capture engine (IMFCaptureEngine,
// Windows 8): its preview drawn into a window (the preview sink renders
// there itself), photos to JPEG files (the photo sink), videos to H.264
// and AAC in MPEG-4 (the record sink). Its events come on Media
// Foundation's threads: OnEvent is called there.

var (
	clsidCaptureEngineFactory = NewGUID("EFCE38D3-8914-4674-A7DF-AE1B3D654B8A")
	clsidCaptureEngine        = NewGUID("EFCE38D3-8914-4674-A7DF-AE1B3D654B8A")
	iidCaptureEngineFactory   = NewGUID("8F02D140-56FC-4302-A705-3A97C78BE779")
	iidCaptureEngine          = NewGUID("A6BBA433-176B-48B2-B375-53AA03473207")
	iidCaptureEventCallback   = NewGUID("AEDA51C0-9025-4983-9012-DE597B88B089")
	iidCapturePreviewSink     = NewGUID("77346CFD-5B49-4D73-ACE0-5B52A859F2E0")
	iidCapturePhotoSink       = NewGUID("D2D43CC8-48BB-4AA7-95DB-10C06977E777")
	iidCaptureRecordSink      = NewGUID("3323B55A-F92A-4FE2-8EDC-E9BFC0634D77")
	mfCaptureVideoOnly        = NewGUID("7E025171-CF32-4F2E-8F19-410577B73A66")
	mfMediaTypeImage          = NewGUID("72178C23-E45B-11D5-BC2A-00B0D0F3F4AB")

	// The capture engine's events.
	CaptureInitialized    = NewGUID("219992BC-CF92-4531-A1AE-96E1E886C8F1")
	CapturePreviewStarted = NewGUID("A416DF21-F9D3-4A74-991B-B817298952C4")
	CapturePreviewStopped = NewGUID("13D5143C-1EDD-4E50-A2EF-350A47678060")
	CaptureRecordStarted  = NewGUID("AC2B027B-DDF9-48A0-89BE-38AB35EF45C0")
	CaptureRecordStopped  = NewGUID("55E5200A-F98F-4C0D-A9EC-9EB25ED3D773")
	CapturePhotoTaken     = NewGUID("3C50C445-7304-48EB-865D-BBA19BA3AF5C")
	CaptureError          = NewGUID("46B89FC6-33CC-4399-9DAD-784DE77D587C")
)

// The preferred streams of the engine's source.
const (
	streamPreview = 0xFFFFFFFA
	streamRecord  = 0xFFFFFFF9
	streamPhoto   = 0xFFFFFFF8
	streamAudio   = 0xFFFFFFF7
)

// CaptureEngine is a camera's capture engine.
type CaptureEngine struct {
	engine   uintptr
	source   uintptr // the device's IMFMediaSource
	callback uintptr
	audio    bool
	// Width and Height are the preview's frames, once it's set up.
	Width, Height int
}

// NewCaptureEngine starts a capture engine on a camera (with the default
// microphone, for recording, if audio): onEvent gets its events (the
// event's kind and status), on Media Foundation's threads;
// CaptureInitialized comes first.
func NewCaptureEngine(d VideoDevice, audio bool, onEvent func(kind GUID, status uintptr)) (*CaptureEngine, error) {
	if err := MFStart(); err != nil {
		return nil, err
	}
	f := CoCreate(&clsidCaptureEngineFactory, &iidCaptureEngineFactory)
	if f == 0 {
		return nil, errors.New("no capture engine on this Windows")
	}
	defer Release(f)
	c := &CaptureEngine{audio: audio}
	if hr := ComCall(f, 3, uintptr(unsafe.Pointer(&clsidCaptureEngine)), uintptr(unsafe.Pointer(&iidCaptureEngine)), uintptr(unsafe.Pointer(&c.engine))); Failed(hr) {
		return nil, fmt.Errorf("no capture engine: %#x", uint32(hr))
	}
	src, err := d.Activate()
	if err != nil {
		Release(c.engine)
		return nil, err
	}
	c.source = src
	attrs, err := NewAttributes()
	if err != nil {
		c.Close()
		return nil, err
	}
	defer attrs.Release()
	if !audio {
		attrs.SetU32(mfCaptureVideoOnly, 1)
	}
	c.callback = NewDelegate(iidCaptureEventCallback, func(event, _ uintptr) {
		var kind GUID
		var status uintptr
		ComCall(event, 34, uintptr(unsafe.Pointer(&kind)))   // GetExtendedType
		ComCall(event, 35, uintptr(unsafe.Pointer(&status))) // GetStatus
		onEvent(kind, uintptr(uint32(status)))
	})
	if hr := ComCall(c.engine, 3, c.callback, uintptr(attrs), 0, src); Failed(hr) { // Initialize
		c.Close()
		return nil, fmt.Errorf("the camera: %#x", uint32(hr))
	}
	return c, nil
}

// sink is one of the engine's sinks as iid (0 record, 1 preview, 2
// photo).
func (c *CaptureEngine) sink(kind uintptr, iid *GUID) uintptr {
	var s uintptr
	if Failed(ComCall(c.engine, 9, kind, uintptr(unsafe.Pointer(&s)))) || s == 0 { // GetSink
		return 0
	}
	defer Release(s)
	return QueryInterface(s, iid)
}

// deviceType is the source's current media type for a stream (a
// reference the caller releases).
func (c *CaptureEngine) deviceType(stream uintptr) MediaType {
	var src, t uintptr
	if Failed(ComCall(c.engine, 10, uintptr(unsafe.Pointer(&src)))) || src == 0 { // GetSource
		return 0
	}
	defer Release(src)
	ComCall(src, 11, stream, uintptr(unsafe.Pointer(&t))) // GetCurrentDeviceMediaType
	return MediaType(t)
}

// StartPreview draws the preview into hwnd (CapturePreviewStarted
// follows).
func (c *CaptureEngine) StartPreview(hwnd uintptr) error {
	s := c.sink(1, &iidCapturePreviewSink)
	if s == 0 {
		return errors.New("no preview")
	}
	defer Release(s)
	ComCall(s, 7) // RemoveAllStreams
	t := c.deviceType(streamPreview)
	if t == 0 {
		return errors.New("no preview")
	}
	defer t.Release()
	if size, ok := t.U64(MFMTFrameSize); ok {
		c.Width, c.Height = int(size>>32), int(uint32(size))
	}
	var idx uint32
	if hr := ComCall(s, 5, streamPreview, uintptr(t), 0, uintptr(unsafe.Pointer(&idx))); Failed(hr) { // AddStream
		return fmt.Errorf("no preview: %#x", uint32(hr))
	}
	if hr := ComCall(s, 8, hwnd); Failed(hr) { // SetRenderHandle
		return fmt.Errorf("no preview: %#x", uint32(hr))
	}
	if hr := ComCall(c.engine, 4); Failed(hr) { // StartPreview
		return fmt.Errorf("no preview: %#x", uint32(hr))
	}
	return nil
}

// FitPreview draws the preview filling w×h pixels of its window, cropped
// to keep its aspect.
func (c *CaptureEngine) FitPreview(w, h int32) {
	s := c.sink(1, &iidCapturePreviewSink)
	if s == 0 || w <= 0 || h <= 0 {
		Release(s)
		return
	}
	defer Release(s)
	src := [4]float32{0, 0, 1, 1} // MFVideoNormalizedRect
	if c.Width > 0 && c.Height > 0 {
		fa, va := float32(w)/float32(h), float32(c.Width)/float32(c.Height)
		if va > fa { // wider: crop the sides
			k := fa / va
			src[0], src[2] = (1-k)/2, (1+k)/2
		} else {
			k := va / fa
			src[1], src[3] = (1-k)/2, (1+k)/2
		}
	}
	dst := Rect{Right: w, Bottom: h}
	black := uint32(0)
	ComCall(s, 10, uintptr(unsafe.Pointer(&src[0])), uintptr(unsafe.Pointer(&dst)), uintptr(unsafe.Pointer(&black))) // UpdateVideo
}

// StopPreview stops the preview.
func (c *CaptureEngine) StopPreview() { ComCall(c.engine, 5) }

// TakePhoto writes a JPEG of what the camera sees to path
// (CapturePhotoTaken follows).
func (c *CaptureEngine) TakePhoto(path string) error {
	s := c.sink(2, &iidCapturePhotoSink)
	if s == 0 {
		return errors.New("no photo")
	}
	defer Release(s)
	ComCall(s, 7)
	dt := c.deviceType(streamPhoto)
	if dt == 0 {
		return errors.New("no photo")
	}
	defer dt.Release()
	t, err := NewMediaType()
	if err != nil {
		return err
	}
	defer t.Release()
	ComCall(uintptr(dt), 32, uintptr(t)) // CopyAllItems
	t.SetGUID(MFMTMajorType, mfMediaTypeImage)
	t.SetGUID(MFMTSubtype, MFImageFormatJPEG)
	var idx uint32
	if hr := ComCall(s, 5, streamPhoto, uintptr(t), 0, uintptr(unsafe.Pointer(&idx))); Failed(hr) {
		return fmt.Errorf("no photo: %#x", uint32(hr))
	}
	if hr := ComCall(s, 8, Str(path)); Failed(hr) { // SetOutputFileName
		return fmt.Errorf("no photo: %#x", uint32(hr))
	}
	if hr := ComCall(c.engine, 8); Failed(hr) { // TakePhoto
		return fmt.Errorf("no photo: %#x", uint32(hr))
	}
	return nil
}

// StartRecording records H.264 (and, with sound, AAC from the
// microphone) to an MPEG-4 file at path (CaptureRecordStarted follows).
func (c *CaptureEngine) StartRecording(path string, sound bool) error {
	s := c.sink(0, &iidCaptureRecordSink)
	if s == 0 {
		return errors.New("couldn't record")
	}
	defer Release(s)
	ComCall(s, 7)
	if hr := ComCall(s, 9, Str(path)); Failed(hr) { // SetOutputFileName
		return fmt.Errorf("couldn't record: %#x", uint32(hr))
	}
	dt := c.deviceType(streamRecord)
	if dt == 0 {
		return errors.New("couldn't record")
	}
	defer dt.Release()
	v, err := NewMediaType()
	if err != nil {
		return err
	}
	defer v.Release()
	v.SetGUID(MFMTMajorType, MFMediaTypeVideo)
	v.SetGUID(MFMTSubtype, MFVideoFormatH264)
	size, _ := dt.U64(MFMTFrameSize)
	rate, ok := dt.U64(MFMTFrameRate)
	if !ok || uint32(rate) == 0 {
		rate = 30<<32 | 1
	}
	v.SetU64(MFMTFrameSize, size)
	v.SetU64(MFMTFrameRate, rate)
	v.SetPair(MFMTPixelAspect, 1, 1)
	v.SetU32(MFMTInterlace, 2)
	w, h := uint64(size>>32), uint64(uint32(size))
	fps := float64(rate>>32) / float64(max(uint32(rate), 1))
	v.SetU32(MFMTAvgBitrate, uint32(min(float64(w*h)*fps*0.15, 20e6)))
	var idx uint32
	if hr := ComCall(s, 5, streamRecord, uintptr(v), 0, uintptr(unsafe.Pointer(&idx))); Failed(hr) {
		return fmt.Errorf("couldn't record video: %#x", uint32(hr))
	}
	if sound && c.audio {
		a, err := AACType(2, 48000, 24000)
		if err == nil {
			ComCall(s, 5, streamAudio, uintptr(a), 0, uintptr(unsafe.Pointer(&idx))) // without sound if refused
			a.Release()
		}
	}
	if hr := ComCall(c.engine, 6); Failed(hr) { // StartRecord
		return fmt.Errorf("couldn't record: %#x", uint32(hr))
	}
	return nil
}

// StopRecording finishes the file (CaptureRecordStopped follows).
func (c *CaptureEngine) StopRecording() { ComCall(c.engine, 7, 1, 0) }

// Close lets the engine and the camera go.
func (c *CaptureEngine) Close() {
	if c.engine != 0 {
		Release(c.engine)
		c.engine = 0
	}
	if c.source != 0 {
		ComCall(c.source, 12) // IMFMediaSource.Shutdown
		Release(c.source)
		c.source = 0
	}
	if c.callback != 0 {
		Release(c.callback)
		c.callback = 0
	}
}

// HasCamera reports whether the PC has a camera.
func HasCamera() bool {
	ds, err := VideoDevices()
	defer ReleaseDevices(ds)
	return err == nil && len(ds) > 0
}
