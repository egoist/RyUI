//go:build windows

package windows

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/ryui/internal/l10n"
	. "github.com/egoist/ryui/internal/windows"
	"github.com/egoist/ryui/native"
)

// ryui/camera on Windows, with Media Foundation's capture engine. The
// "ryui.camera" view is a window of its own the engine's preview sink
// draws the camera into, filling it (cropped to keep its aspect); its
// photo sink takes Capture's photos (JPEG), its record sink
// ryui/camera/record's videos in video mode (H.264 and AAC in MPEG-4).
// With scan on, the preview comes to the view as frames (the preview
// sink's sample callback), which it draws itself and reads codes in, four
// times a second, with package barcode (scan.go): each code reported as
// it comes into view, again once it's been out of view for a second. "take" has no system camera screen to show: it
// shows a dialog of the host's own over the window in use, a live preview
// with Take Photo and Cancel (Enter and Escape), as on a Mac. Photos and
// videos go to the app's caches (ryui-camera). Settings' camera privacy
// switch refused: "not allowed".
//
// The camera is chosen by its facing as Windows names it ("Front",
// "Rear"/"Back" in its name); else the first one.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) { m["camera"] = h.cameraModule() })
	builtinViews["ryui.camera"] = func(ctx native.Context) native.View { return newCameraView(theHost, ctx.Parent(), ctx.Emit, false) }
}

// cameraCurrent is the view on screen Capture and record use: the last
// made of those still there.
var cameraCurrent *cameraView

// cameraDevices and cameraDenied ask Windows; a test replaces them.
var (
	cameraDevices = VideoDevices
	cameraDenied  = func() bool { return CapabilityDenied("webcam") }
)

func (h *Host) cameraModule() map[string]asyncMethod {
	noView := errors.New("no camera view")
	return map[string]asyncMethod{
		"capture": func(_ []byte, reply func([]byte, error)) {
			if v := cameraCurrent; v != nil {
				v.capture(reply)
				return
			}
			reply(nil, noView)
		},
		// args: u8 mute, u32 max milliseconds (0: none).
		"record": func(args []byte, reply func([]byte, error)) {
			v := cameraCurrent
			if v == nil {
				reply(nil, noView)
				return
			}
			if len(args) < 5 {
				reply(nil, errors.New("bad arguments"))
				return
			}
			v.record(args[0] == 1, time.Duration(binary.LittleEndian.Uint32(args[1:]))*time.Millisecond, reply)
		},
		"stopRecording": func(_ []byte, reply func([]byte, error)) {
			if v := cameraCurrent; v != nil {
				v.stopRecording()
			}
			reply(nil, nil)
		},
		"take": func(_ []byte, reply func([]byte, error)) { h.cameraTake(reply) },
	}
}

// cameraFile is a new file in the caches' ryui-camera.
func (h *Host) cameraFile(ext string) (string, error) {
	dir := filepath.Join(h.cacheDir(), "ryui-camera")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(dir, "*"+ext)
	if err != nil {
		return "", err
	}
	f.Close()
	return f.Name(), nil
}

// The view.

type cameraView struct {
	h       *Host
	emit    func(string, map[string]any)
	hwnd    uintptr
	engine  *CaptureEngine
	running string // the facing and mode the engine was made for

	facing      string
	video, scan bool
	configuring bool
	previewing  bool
	sheet       bool // the take dialog's: never the current one
	destroyed   bool
	kinds       scanKinds
	seen        codeSeen
	frames      *cameraFrames // the preview's frames, scanning
	failed      bool
	capturing   func([]byte, error)
	photoPath   string
	recording   func([]byte, error)
	recPath     string
	recStop     *time.Timer
}

func newCameraView(h *Host, parent uintptr, emit func(string, map[string]any), sheet bool) *cameraView {
	v := &cameraView{h: h, emit: emit, facing: "back", sheet: sheet}
	if sheet {
		v.facing = "front"
	}
	v.hwnd = ownWindow(0, "", WS_CHILD|WS_VISIBLE|WS_CLIPCHILDREN, 0, 0, 0, 0, parent, func(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
		switch m {
		case WM_PAINT:
			if v.frames != nil {
				var ps PaintStruct
				dc := BeginPaint(hwnd, &ps)
				v.frames.paint(dc, ClientRect(hwnd))
				EndPaint(hwnd, &ps)
				return 0, true
			}
		case WM_SIZE:
			if v.frames != nil {
				Invalidate(hwnd)
			} else if v.engine != nil && v.previewing {
				r := ClientRect(hwnd)
				v.engine.FitPreview(r.W(), r.H())
			}
		case WM_ERASEBKGND:
			if v.frames != nil && v.frames.bmp != 0 {
				return 1, true // the frame covers it
			}
			r := ClientRect(hwnd)
			b := CreateSolidBrush(0)
			FillRect(wp, &r, b)
			DeleteObject(b)
			return 1, true
		}
		return 0, false
	})
	if !sheet {
		cameraCurrent = v
	}
	v.configureLater()
	return v
}

func (v *cameraView) Handle() uintptr { return v.hwnd }

func (v *cameraView) fail(msg string) {
	if !v.destroyed && v.emit != nil {
		v.emit("error", map[string]any{"message": msg})
	}
}

// Set implements native.View: facing, video, scan, codes.
func (v *cameraView) Set(name string, value any) {
	switch name {
	case "facing":
		s, _ := value.(string)
		v.facing = map[bool]string{true: "front", false: "back"}[s == "front"]
	case "video":
		v.video, _ = value.(bool)
	case "scan":
		v.scan, _ = value.(bool)
	case "codes":
		s, _ := value.(string)
		v.kinds = parseScanKinds(s)
		if v.frames != nil {
			v.frames.setKinds(v.kinds)
		}
		return
	default:
		return
	}
	v.configureLater()
}

// configureLater configures once for the props set together.
func (v *cameraView) configureLater() {
	if v.configuring {
		return
	}
	v.configuring = true
	v.h.Post(func() {
		v.configuring = false
		v.configure()
	})
}

// cameraFor is the device of a facing: named so, else the first.
func cameraFor(ds []VideoDevice, facing string) (VideoDevice, bool) {
	if len(ds) == 0 {
		return VideoDevice{}, false
	}
	words := []string{"rear", "back", "world"}
	if facing == "front" {
		words = []string{"front", "user", "integrated"}
	}
	for _, d := range ds {
		n := strings.ToLower(d.Name)
		for _, w := range words {
			if strings.Contains(n, w) {
				return d, true
			}
		}
	}
	return ds[0], true
}

func (v *cameraView) configure() {
	if v.destroyed {
		return
	}
	want := v.facing + map[bool]string{true: "/video", false: "/photo"}[v.video] + map[bool]string{true: "/scan", false: ""}[v.scan]
	if v.engine != nil && v.running == want {
		return
	}
	v.closeEngine()
	if cameraDenied() {
		v.failed = true
		v.fail("not allowed")
		return
	}
	ds, err := cameraDevices()
	defer ReleaseDevices(ds)
	d, ok := cameraFor(ds, v.facing)
	if err != nil || !ok {
		v.failed = true
		v.fail("no camera")
		return
	}
	audio := v.video && HasMicrophone() && !CapabilityDenied("microphone")
	var e *CaptureEngine
	e, err = NewCaptureEngine(d, audio, func(kind GUID, status uintptr) {
		theHost.Post(func() {
			if v.engine == e && !v.destroyed {
				v.event(kind, status)
			}
		})
	})
	if err != nil {
		v.failed = true
		v.fail("no camera")
		return
	}
	v.engine, v.running, v.failed = e, want, false
}

// event is the engine's event, on the main thread.
func (v *cameraView) event(kind GUID, status uintptr) {
	ok := !Failed(status)
	switch kind {
	case CaptureInitialized:
		if !ok {
			v.failed = true
			if uint32(status) == 0x80070005 { // E_ACCESSDENIED
				v.fail("not allowed")
			} else {
				v.fail("no camera")
			}
			return
		}
		var err error
		if v.scan {
			v.frames = newCameraFrames(v)
			err = v.engine.PreviewFrames(v.frames.frame)
		} else {
			err = v.engine.StartPreview(v.hwnd)
		}
		if err != nil {
			v.failed = true
			v.fail(err.Error())
		}
	case CapturePreviewStarted:
		v.previewing = ok
		if ok && v.frames == nil {
			r := ClientRect(v.hwnd)
			v.engine.FitPreview(r.W(), r.H())
		}
	case CapturePreviewStopped:
		v.previewing = false
	case CapturePhotoTaken:
		reply, path := v.capturing, v.photoPath
		v.capturing, v.photoPath = nil, ""
		if reply == nil {
			return
		}
		if !ok {
			os.Remove(path)
			reply(nil, errors.New("no photo"))
			return
		}
		reply([]byte(path), nil)
	case CaptureRecordStarted:
		if !ok {
			v.recordDone(errors.New("couldn't record"))
		}
	case CaptureRecordStopped:
		if ok {
			v.recordDone(nil)
		} else {
			v.recordDone(errors.New("the recording failed"))
		}
	case CaptureError:
		if v.capturing != nil {
			c := v.capturing
			v.capturing = nil
			c(nil, errors.New("no photo"))
		}
		if v.recording != nil {
			v.recordDone(errors.New("the recording failed"))
		}
		if !v.previewing {
			v.failed = true
			v.fail("the camera stopped")
		}
	}
}

func (v *cameraView) capture(reply func([]byte, error)) {
	if v.engine == nil || !v.previewing {
		reply(nil, errors.New("the camera isn't showing"))
		return
	}
	if c := v.capturing; c != nil {
		v.capturing = nil
		c(nil, errors.New("another photo was taken"))
	}
	path, err := v.h.cameraFile(".jpg")
	if err != nil {
		reply(nil, err)
		return
	}
	if err := v.engine.TakePhoto(path); err != nil {
		os.Remove(path)
		reply(nil, err)
		return
	}
	v.capturing, v.photoPath = reply, path
}

// record records until stopRecording (or max, if not 0), with the
// microphone unless mute (or there's none, or it's not allowed).
func (v *cameraView) record(mute bool, max time.Duration, reply func([]byte, error)) {
	switch {
	case v.engine == nil || !v.previewing:
		reply(nil, errors.New("the camera isn't showing"))
		return
	case !v.video:
		reply(nil, errors.New("the camera view isn't in video mode"))
		return
	case v.recording != nil:
		reply(nil, errors.New("already recording"))
		return
	}
	path, err := v.h.cameraFile(".mp4")
	if err != nil {
		reply(nil, err)
		return
	}
	if err := v.engine.StartRecording(path, !mute); err != nil {
		os.Remove(path)
		reply(nil, err)
		return
	}
	v.recording, v.recPath = reply, path
	if max > 0 {
		var t *time.Timer
		t = time.AfterFunc(max, func() {
			v.h.Post(func() {
				if v.recStop == t {
					v.stopRecording()
				}
			})
		})
		v.recStop = t
	}
}

func (v *cameraView) stopRecording() {
	if v.recording != nil && v.engine != nil {
		v.engine.StopRecording()
	}
}

func (v *cameraView) recordDone(err error) {
	reply, path := v.recording, v.recPath
	v.recording, v.recPath = nil, ""
	if v.recStop != nil {
		v.recStop.Stop()
		v.recStop = nil
	}
	if reply == nil {
		return
	}
	if err != nil {
		os.Remove(path)
		reply(nil, err)
		return
	}
	reply([]byte(path), nil)
}

func (v *cameraView) closeEngine() {
	if v.engine == nil {
		return
	}
	if v.previewing {
		v.engine.StopPreview()
	}
	v.engine.Close()
	v.engine, v.previewing, v.running = nil, false, ""
	if v.frames != nil {
		v.frames.close()
		v.frames = nil
	}
}

// Destroy implements native.Destroyer: the camera stops, and what waits
// on it is told.
func (v *cameraView) Destroy() {
	if v.destroyed {
		return
	}
	if c := v.capturing; c != nil {
		v.capturing = nil
		c(nil, errors.New("the camera isn't showing"))
	}
	if v.recording != nil {
		v.engine.StopRecording()
		v.recordDone(errors.New("the camera isn't showing"))
	}
	v.destroyed = true
	if cameraCurrent == v {
		cameraCurrent = nil
	}
	v.closeEngine()
}

// The take dialog.

type cameraSheet struct {
	h      *Host
	reply  func([]byte, error)
	d      *dialog
	view   *cameraView
	button uintptr // Take Photo
	cancel uintptr
}

var cameraTaking *cameraSheet

// cameraTake shows the dialog and answers with the photo's path, or ""
// if cancelled.
func (h *Host) cameraTake(reply func([]byte, error)) {
	cameraTaking.finish(nil, nil) // one at a time
	if cameraDenied() {
		reply(nil, errNotAllowed)
		return
	}
	ds, err := cameraDevices()
	ReleaseDevices(ds)
	if err != nil || len(ds) == 0 {
		reply(nil, errors.New("no camera"))
		return
	}
	const w, ht, bar = 640.0, 532.0, 52.0
	s := &cameraSheet{h: h, reply: reply}
	s.d = h.newDialog(l10n.TakePhoto(h.locale), w, ht, func(d *dialog, m uint32, wp, lp uintptr) (uintptr, bool) {
		switch m {
		case WM_COMMAND:
			switch lp {
			case s.button:
				s.take()
			case s.cancel:
				s.finish(nil, nil)
			}
			return 0, true
		case WM_KEYDOWN:
			switch wp {
			case 0x0D: // VK_RETURN
				s.take()
				return 0, true
			case 0x1B: // VK_ESCAPE
				s.finish(nil, nil)
				return 0, true
			}
		case WM_SIZE:
			s.layout()
		}
		return 0, false
	})
	s.d.onClose = func() { s.finish(nil, nil) }
	s.view = newCameraView(h, s.d.hwnd, func(name string, values map[string]any) {
		if name == "error" {
			msg, _ := values["message"].(string)
			s.finish(nil, errors.New(msg))
		}
	}, true)
	font := h.font(fontSpec{size: 14, weight: 400, k: s.d.k})
	button := func(title string, id uintptr) uintptr {
		b := CreateWindow(0, "BUTTON", title, WS_CHILD|WS_VISIBLE|BS_PUSHBUTTON, 0, 0, 0, 0, s.d.hwnd, id)
		SendMessage(b, WM_SETFONT, font, 1)
		return b
	}
	s.button = button(l10n.TakePhoto(h.locale), 1)
	s.cancel = button(l10n.Cancel(h.locale), 2)
	s.layout()
	cameraTaking = s
	s.d.show()
	SetFocus(s.d.hwnd)
}

// layout places the preview above the bar, the buttons in it, from the
// trailing edge.
func (s *cameraSheet) layout() {
	r := ClientRect(s.d.hwnd)
	bar, bw, bh, gap := s.d.px(52), s.d.px(120), s.d.px(32), s.d.px(12)
	SetWindowPos(s.view.hwnd, 0, 0, 0, r.W(), r.H()-bar, SWP_NOZORDER|SWP_NOACTIVATE)
	y := r.H() - bar + (bar-bh)/2
	x := r.W() - gap*2 - bw
	SetWindowPos(s.button, 0, x, y, bw, bh, SWP_NOZORDER|SWP_NOACTIVATE)
	SetWindowPos(s.cancel, 0, x-gap-bw, y, bw, bh, SWP_NOZORDER|SWP_NOACTIVATE)
}

func (s *cameraSheet) take() {
	if s == nil || cameraTaking != s || !IsWindowEnabled(s.button) {
		return
	}
	EnableWindow(s.button, false)
	s.view.capture(func(b []byte, err error) {
		if err != nil && err.Error() == "the camera isn't showing" {
			EnableWindow(s.button, true) // not ready yet: try again
			return
		}
		s.finish(b, err)
	})
}

// finish closes the dialog and answers (nil, nil: cancelled).
func (s *cameraSheet) finish(b []byte, err error) {
	if s == nil || cameraTaking != s {
		return
	}
	cameraTaking = nil
	s.view.Destroy()
	s.d.close()
	if b == nil && err == nil {
		b = []byte{}
	}
	s.reply(b, err)
}
