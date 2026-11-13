//go:build windows

package windows

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"
)

// The camera's preview as frames Go gets (to read codes in them): the
// preview sink's sample callback, in RGB32 (the engine converts what the
// camera gives), in place of its drawing into a window, which the view
// then does itself.

var iidCaptureSampleCallback = NewGUID("52150B82-AB39-4467-980F-E48BF0822ECD")

// Frame is a preview frame: Width×Height BGRX pixels, top row first.
type Frame struct {
	Width, Height int
	Pix           []byte
}

// PreviewFrames starts the preview without drawing it: f gets each frame,
// on Media Foundation's threads (the frame's pixels are f's only while
// it runs).
func (c *CaptureEngine) PreviewFrames(f func(Frame)) error {
	s := c.sink(1, &iidCapturePreviewSink)
	if s == 0 {
		return errors.New("no preview")
	}
	defer Release(s)
	ComCall(s, 7) // RemoveAllStreams
	dt := c.deviceType(streamPreview)
	if dt == 0 {
		return errors.New("no preview")
	}
	defer dt.Release()
	size, _ := dt.U64(MFMTFrameSize)
	c.Width, c.Height = int(size>>32), int(uint32(size))
	t, err := NewMediaType()
	if err != nil {
		return err
	}
	defer t.Release()
	ComCall(uintptr(dt), 32, uintptr(t)) // CopyAllItems
	t.SetGUID(MFMTSubtype, MFVideoFormatRGB32)
	t.SetU32(MFMTDefaultStride, uint32(c.Width*4))
	var idx uint32
	if hr := ComCall(s, 5, streamPreview, uintptr(t), 0, uintptr(unsafe.Pointer(&idx))); Failed(hr) { // AddStream
		return fmt.Errorf("no preview: %#x", uint32(hr))
	}
	w, h := c.Width, c.Height
	var mu sync.Mutex
	var buf []byte
	cb := NewDelegate(iidCaptureSampleCallback, func(sample, _ uintptr) { // OnSample
		mu.Lock()
		defer mu.Unlock()
		var mb uintptr
		if sample == 0 || Failed(ComCall(sample, sampleToBuffer, uintptr(unsafe.Pointer(&mb)))) || mb == 0 {
			return
		}
		defer Release(mb)
		var p uintptr
		var n uint32
		if Failed(ComCall(mb, bufLock, uintptr(unsafe.Pointer(&p)), 0, uintptr(unsafe.Pointer(&n)))) {
			return
		}
		defer ComCall(mb, bufUnlock)
		if int(n) < w*h*4 || w <= 0 || h <= 0 {
			return
		}
		if len(buf) != w*h*4 {
			buf = make([]byte, w*h*4)
		}
		copy(buf, unsafe.Slice((*byte)(native(p)), w*h*4))
		f(Frame{Width: w, Height: h, Pix: buf})
	})
	defer Release(cb)
	if hr := ComCall(s, 11, uintptr(idx), cb); Failed(hr) { // SetSampleCallback
		return fmt.Errorf("no preview: %#x", uint32(hr))
	}
	if hr := ComCall(c.engine, 4); Failed(hr) { // StartPreview
		return fmt.Errorf("no preview: %#x", uint32(hr))
	}
	return nil
}
