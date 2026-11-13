//go:build windows

package windows

import (
	"errors"
	"fmt"
	"unsafe"
)

// The microphone, through waveIn (16-bit PCM from the default device,
// Windows converting from its own format), and the privacy settings
// that let desktop apps have the microphone and the camera.

var (
	procWaveInGetNumDevs      = winmm.NewProc("waveInGetNumDevs")
	procWaveInOpen            = winmm.NewProc("waveInOpen")
	procWaveInClose           = winmm.NewProc("waveInClose")
	procWaveInPrepareHeader   = winmm.NewProc("waveInPrepareHeader")
	procWaveInUnprepareHeader = winmm.NewProc("waveInUnprepareHeader")
	procWaveInAddBuffer       = winmm.NewProc("waveInAddBuffer")
	procWaveInStart           = winmm.NewProc("waveInStart")
	procWaveInStop            = winmm.NewProc("waveInStop")
	procWaveInReset           = winmm.NewProc("waveInReset")
	procWaitForSingleObject   = kernel32.NewProc("WaitForSingleObject")
)

// ErrNoMicrophone is a PC without a microphone.
var ErrNoMicrophone = errors.New("no microphone")

type waveHdr struct {
	data     uintptr
	length   uint32
	recorded uint32
	user     uintptr
	flags    uint32
	loops    uint32
	next     uintptr
	reserved uintptr
}

type waveFormat struct {
	tag                    uint16
	channels               uint16
	rate, bytesPerSec      uint32
	blockAlign, bits, size uint16
}

// WaveIn records from the default microphone.
type WaveIn struct {
	h, event uintptr
	hdrs     []*waveHdr
	bufs     [][]byte
	next     int // the buffer that comes back next
	Channels int
	Rate     int
}

// HasMicrophone reports whether the PC has a microphone.
func HasMicrophone() bool {
	n, _, _ := procWaveInGetNumDevs.Call()
	return n > 0
}

// OpenWaveIn opens the default microphone for 16-bit PCM of channels at
// rate, in buffers of 50 ms.
func OpenWaveIn(channels, rate int) (*WaveIn, error) {
	if !HasMicrophone() {
		return nil, ErrNoMicrophone
	}
	w := &WaveIn{Channels: channels, Rate: rate}
	w.event, _, _ = procCreateEventW.Call(0, 0, 0, 0)
	f := waveFormat{tag: 1, channels: uint16(channels), rate: uint32(rate), bytesPerSec: uint32(rate * channels * 2), blockAlign: uint16(channels * 2), bits: 16}
	const waveMapper, callbackEvent = 0xFFFFFFFF, 0x00050000
	if r, _, _ := procWaveInOpen.Call(uintptr(unsafe.Pointer(&w.h)), waveMapper, uintptr(unsafe.Pointer(&f)), w.event, 0, callbackEvent); r != 0 {
		procCloseHandle.Call(w.event)
		return nil, fmt.Errorf("the microphone: waveIn error %d", r)
	}
	size := rate / 20 * channels * 2
	for i := 0; i < 8; i++ {
		b := make([]byte, size)
		hd := &waveHdr{data: uintptr(unsafe.Pointer(&b[0])), length: uint32(size)}
		w.bufs = append(w.bufs, b)
		w.hdrs = append(w.hdrs, hd)
		procWaveInPrepareHeader.Call(w.h, uintptr(unsafe.Pointer(hd)), unsafe.Sizeof(*hd))
		procWaveInAddBuffer.Call(w.h, uintptr(unsafe.Pointer(hd)), unsafe.Sizeof(*hd))
	}
	return w, nil
}

// Start starts (or resumes) recording.
func (w *WaveIn) Start() error {
	if r, _, _ := procWaveInStart.Call(w.h); r != 0 {
		return fmt.Errorf("the microphone: waveIn error %d", r)
	}
	return nil
}

// Stop pauses: the buffer being filled comes back with what it has.
func (w *WaveIn) Stop() { procWaveInStop.Call(w.h) }

// Wait waits up to ms milliseconds for a buffer to come back.
func (w *WaveIn) Wait(ms uint32) { procWaitForSingleObject.Call(w.event, uintptr(ms)) }

// Read calls f with each buffer recorded since, in order, and gives the
// buffers back to the device.
func (w *WaveIn) Read(f func(pcm []byte)) {
	const whdrDone = 0x1
	for range w.hdrs {
		hd := w.hdrs[w.next]
		if hd.flags&whdrDone == 0 {
			return
		}
		if hd.recorded > 0 {
			f(w.bufs[w.next][:hd.recorded])
		}
		hd.flags &^= whdrDone
		hd.recorded = 0
		procWaveInAddBuffer.Call(w.h, uintptr(unsafe.Pointer(hd)), unsafe.Sizeof(*hd))
		w.next = (w.next + 1) % len(w.hdrs)
	}
}

// Close stops recording and lets the device go: what was recorded and
// not read yet goes to f first.
func (w *WaveIn) Close(f func(pcm []byte)) {
	procWaveInStop.Call(w.h)
	w.Read(f)
	procWaveInReset.Call(w.h)
	for _, hd := range w.hdrs {
		procWaveInUnprepareHeader.Call(w.h, uintptr(unsafe.Pointer(hd)), unsafe.Sizeof(*hd))
	}
	procWaveInClose.Call(w.h)
	procCloseHandle.Call(w.event)
	w.hdrs, w.bufs = nil, nil
}

// CapabilityDenied reports whether Settings (Privacy & security) keeps
// desktop apps from a device: "microphone", "webcam".
func CapabilityDenied(name string) bool {
	const hkcu, hklm = 0x80000001, 0x80000002
	base := `Software\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\` + name
	for _, root := range []uintptr{hklm, hkcu} {
		if regValue(root, base, "Value") == "Deny" || regValue(root, base+`\NonPackaged`, "Value") == "Deny" {
			return true
		}
	}
	return false
}
