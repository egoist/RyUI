//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/native"
	"github.com/egoist/asuka/ui"
)

// cameraDenied makes the camera and microphone denied (a test can't
// answer the system's prompt), and the device lookup what the test says,
// until the test ends.
func cameraDenied(t *testing.T, status int, device func(string) ID) {
	status0, device0 := cameraAuthStatus, cameraDeviceFor
	cameraAuthStatus = func(ID) int { return status }
	if device != nil {
		cameraDeviceFor = device
	}
	t.Cleanup(func() { cameraAuthStatus, cameraDeviceFor = status0, device0 })
}

// pumpUntil runs the main run loop until cond (posted work answers
// there), with a safety net that isn't what passes.
func pumpUntil(cond func() bool) bool {
	app := Send(Class("NSApplication"), "sharedApplication")
	mode := Const(Foundation(), "NSDefaultRunLoopMode")
	for i := 0; !cond() && i < 2000; i++ {
		until := MsgFloatID(Class("NSDate"), Sel("dateWithTimeIntervalSinceNow:"), 0.01)
		if ev := Send(app, "nextEventMatchingMask:untilDate:inMode:dequeue:", ^uintptr(0), uintptr(until), uintptr(mode), 1); ev != 0 {
			Send(app, "sendEvent:", uintptr(ev))
		}
	}
	return cond()
}

// The arguments asuka/sdk/camera/record and asuka/sdk/audio/record send.
func TestCameraArgs(t *testing.T) {
	args := []byte{1, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(args[1:], 1500)
	if mute, max, ok := cameraRecordArgs(args); !ok || !mute || max != 1500*time.Millisecond {
		t.Errorf("record args: %v %v %v", mute, max, ok)
	}
	if _, _, ok := cameraRecordArgs([]byte{1, 2}); ok {
		t.Error("short record args decoded")
	}
	// As record.Start encodes them.
	start := []byte{1, 0, 0, 0, 0, 3}
	binary.LittleEndian.PutUint32(start[1:], 60000)
	start = binary.LittleEndian.AppendUint32(start, 9)
	start = append(start, "Recording"...)
	start = binary.LittleEndian.AppendUint32(start, 8)
	start = append(start, "/tmp/a.m"...)
	a, ok := decodeRecArgs(start)
	if !ok || a.quality != 1 || a.maxMillis != 60000 || a.flags != 3 || a.title != "Recording" || a.path != "/tmp/a.m" {
		t.Errorf("start args: %+v %v", a, ok)
	}
	if _, ok := decodeRecArgs(start[:len(start)-3]); ok {
		t.Error("a cut path decoded")
	}
	if _, ok := decodeRecArgs([]byte{0, 1}); ok {
		t.Error("short start args decoded")
	}
}

// Codes are named and repeats dropped as on iOS.
func TestCameraCodeKinds(t *testing.T) {
	all := newCameraKinds("")
	if !all.wants("qr") || !all.wants("upca") {
		t.Error("no kinds should want every kind")
	}
	k := newCameraKinds("qr,upca")
	if k.wants("ean13") || !k.wants("qr") {
		t.Error("kinds asked for")
	}
	if kind, v := k.named("ean13", "0123456789012"); kind != "upca" || v != "123456789012" {
		t.Errorf("a UPC-A read as an EAN-13: %s %s", kind, v)
	}
	if kind, _ := newCameraKinds("ean13").named("ean13", "0123456789012"); kind != "ean13" {
		t.Error("an EAN-13 stays one when UPC-A isn't asked for")
	}
	var s cameraSeen
	now := time.Unix(100, 0)
	if !s.isNew("qr", "a", now) || s.isNew("qr", "a", now.Add(500*time.Millisecond)) {
		t.Error("a code in view is reported once")
	}
	if !s.isNew("qr", "a", now.Add(2*time.Second)) {
		t.Error("a code back in view after a second is reported again")
	}
}

// With no camera view on screen, Capture and record fail as on iOS;
// take answers "not allowed" and "no camera" without showing anything.
func TestCameraModule(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		for _, m := range []string{"capture", "record"} {
			if _, err := await(h, "camera", m, []byte{0, 0, 0, 0, 0}); err == nil || err.Error() != "no camera view" {
				fatalf(t, "camera.%s: %v", m, err)
			}
		}
		if _, err := await(h, "camera", "stopRecording", nil); err != nil {
			fatal(t, "stopRecording:", err)
		}
	})
	cameraDenied(t, 2, nil)
	onMain(func() {
		if _, err := await(h, "camera", "take", nil); err == nil || err.Error() != "not allowed" {
			fatalf(t, "take, denied: %v", err)
		}
	})
	cameraDenied(t, avAuthorized, func(string) ID { return 0 })
	onMain(func() {
		if _, err := await(h, "camera", "take", nil); err == nil || err.Error() != "no camera" {
			fatalf(t, "take, no camera: %v", err)
		}
		if cameraTaking != nil {
			fatal(t, "a sheet shows")
		}
	})
	// A camera there as take looks, gone as the sheet's view configures:
	// the sheet shows, then closes with the view's error.
	looked := 0
	cameraDenied(t, avAuthorized, func(string) ID {
		looked++
		if looked == 1 {
			return Class("NSObject")
		}
		return 0
	})
	onMain(func() {
		sheets := SendInt(Send(h.window, "sheets"), "count")
		if _, err := await(h, "camera", "take", nil); err == nil || err.Error() != "no camera" {
			fatalf(t, "take, the camera gone: %v", err)
		}
		if cameraTaking != nil || SendInt(Send(h.window, "sheets"), "count") != sheets || len(cameraByView) != 0 {
			fatal(t, "the sheet stays")
		}
	})
}

// The view: its preview layer backs it, it's the current one while in a
// window, props configure it (here: not allowed), and it goes.
func TestCameraView(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	cameraDenied(t, 2, nil)
	onMain(func() {
		var errs []string
		nv := builtinViews["asuka.camera"](native.NewContext(0, func(name string, values map[string]any) {
			if name == "error" {
				errs = append(errs, values["message"].(string))
			}
		}))
		v := nv.(*cameraView)
		view := ID(nv.Handle())
		if view == 0 || !SendBool(view, "isKindOfClass:", uintptr(Class("AsukaCameraPreview"))) {
			fatal(t, "no preview view")
		}
		if l := Send(view, "layer"); l == 0 || !SendBool(l, "isKindOfClass:", uintptr(Class("AVCaptureVideoPreviewLayer"))) {
			fatal(t, "the view isn't backed by the preview layer")
		}
		for _, p := range []struct {
			name  string
			value any
		}{{"facing", "front"}, {"video", true}, {"scan", true}, {"codes", "qr,ean13"}} {
			nv.Set(p.name, p.value)
		}
		if !pumpUntil(func() bool { return len(errs) > 0 }) || errs[0] != "not allowed" || len(errs) != 1 {
			fatalf(t, "errors: %v (want one not allowed, the props configured once)", errs)
		}
		if v.facing != "front" || !v.video || !v.scan || !v.kinds.wants("qr") || v.kinds.wants("aztec") {
			fatalf(t, "props: %+v", v)
		}
		Send(h.content, "addSubview:", uintptr(view))
		if cameraCurrent != v {
			fatal(t, "the view in a window isn't the current one")
		}
		if _, err := await(h, "camera", "capture", nil); err == nil || err.Error() != "the camera isn't showing" {
			fatalf(t, "capture with the session stopped: %v", err)
		}
		if _, err := await(h, "camera", "record", []byte{1, 0, 0, 0, 0}); err == nil || err.Error() != "the camera isn't showing" {
			fatalf(t, "record with the session stopped: %v", err)
		}
		Send(view, "removeFromSuperview")
		if cameraCurrent != nil {
			fatal(t, "the view out of its window is still the current one")
		}
		Send(h.content, "addSubview:", uintptr(view))
		nv.(native.Destroyer).Destroy()
		if cameraCurrent != nil || cameraByView[view] != nil || len(cameraByOutput) != 0 {
			fatal(t, "the destroyed view is still known")
		}
		Send(view, "removeFromSuperview")
	})
}

// audioRecord with nothing recording: zeros, and stop, pause and resume
// answer; start fails on bad arguments and when not allowed.
func TestAudioRecord(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	cameraDenied(t, 2, nil)
	onMain(func() {
		if b, err := await(h, "audioRecord", "status", nil); err != nil || string(b) != "\x00\x00\x00\x00\x00\x00" {
			fatalf(t, "status: %v, %v", b, err)
		}
		if b, err := await(h, "audioRecord", "level", nil); err != nil || string(b) != "\x00" {
			fatalf(t, "level: %v, %v", b, err)
		}
		for _, m := range []string{"stop", "pause", "resume"} {
			if _, err := await(h, "audioRecord", m, nil); err != nil {
				fatalf(t, "%s: %v", m, err)
			}
		}
		if _, err := await(h, "audioRecord", "start", []byte{0}); err == nil || err.Error() != "bad arguments" {
			fatalf(t, "start, bad arguments: %v", err)
		}
		if _, err := await(h, "audioRecord", "start", []byte{0, 0, 0, 0, 0, 0}); err == nil || err.Error() != "not allowed" {
			fatalf(t, "start, denied: %v", err)
		}
		if rec.recorder != 0 {
			fatal(t, "a recorder was made")
		}
		if d := recDelegate(); d == 0 || !RespondsTo(d, "audioRecorderDidFinishRecording:successfully:") {
			fatal(t, "no recorder delegate")
		}
	})
}
