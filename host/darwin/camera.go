//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/native"
)

// asuka/sdk/camera on a Mac, with AVFoundation as on iOS. The "asuka.camera"
// view is an AVCaptureSession's preview filling its box (the view's
// backing layer an AVCaptureVideoPreviewLayer), with a photo output for
// Capture, a movie file output for asuka/sdk/camera/record in video mode, and
// with scan on a metadata output reading codes (macOS 13). "take" has no
// system camera screen to show on a Mac: it shows a sheet of the host's
// own over the window in use, a live preview with Take Photo and Cancel
// (Return and Escape). Photos and videos go to the app's caches
// (asuka-camera), as on iOS. macOS asks the user once, with the app's
// NSCameraUsageDescription (and NSMicrophoneUsageDescription for a
// video's sound), which needs the app's bundle.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["camera"] = cameraModule(h) })
	builtinViews["asuka.camera"] = func(ctx native.Context) native.View {
		return newCameraView(theHost, ctx.Emit, false)
	}
}

// AVAuthorizationStatus.
const (
	avNotDetermined = 0
	avAuthorized    = 3
)

var (
	cameraLoadOnce sync.Once
	cameraLoaded   bool
	cameraDelegate ID // AsukaCameraDelegate: photos, recordings, codes
	cameraTakeTgt  ID // AsukaCameraTake: the take sheet's buttons

	msgSetCMTime     func(obj ID, sel SEL, t cmTime)
	msgRecordFor     func(obj ID, sel SEL, seconds float64) bool
	msgAveragePower  func(obj ID, sel SEL, channel uint) float32
	cameraVideoMedia ID
	cameraAudioMedia ID

	// The views by their preview NSView, and by their outputs (the
	// delegate's calls name the output).
	cameraByView   = map[ID]*cameraView{}
	cameraByOutput = map[ID]*cameraView{}
	// cameraCurrent is the view on screen Capture and record use.
	cameraCurrent *cameraView
)

// avConst is an AVFoundation (or AVFAudio) constant, from any image loaded.
func avConst(name string) ID { return Const(purego.RTLD_DEFAULT, name) }

// cameraLoad opens AVFoundation and makes the classes, once.
func cameraLoad() bool {
	cameraLoadOnce.Do(func() {
		if framework("AVFoundation") == 0 || !HasClass("AVCaptureSession") {
			return
		}
		framework("CoreMedia")
		purego.RegisterFunc(&msgSetCMTime, MsgSendAddr())
		purego.RegisterFunc(&msgRecordFor, MsgSendAddr())
		purego.RegisterFunc(&msgAveragePower, MsgSendAddr())
		cameraVideoMedia = avConst("AVMediaTypeVideo")
		if cameraVideoMedia == 0 {
			cameraVideoMedia = Retain(String("vide"))
		}
		cameraAudioMedia = avConst("AVMediaTypeAudio")
		if cameraAudioMedia == 0 {
			cameraAudioMedia = Retain(String("soun"))
		}
		registerCameraClasses()
		cameraLoaded = true
	})
	return cameraLoaded
}

func registerCameraClasses() {
	// The preview: its backing layer is its view's preview layer, so it
	// fills the view as the core places it; it runs while in a window.
	DefineClass("AsukaCameraPreview", "NSView", nil, []objc.MethodDef{
		Method("makeBackingLayer", func(self ID, _ SEL) ID {
			if v := cameraByView[self]; v != nil && v.layer != 0 {
				return v.layer
			}
			return SendSuper(self, "AsukaCameraPreview", "makeBackingLayer")
		}),
		Method("viewDidMoveToWindow", func(self ID, _ SEL) {
			SendSuper(self, "AsukaCameraPreview", "viewDidMoveToWindow")
			if v := cameraByView[self]; v != nil {
				v.attached(Send(self, "window") != 0)
			}
		}),
	})
	DefineClass("AsukaCameraDelegate", "NSObject", []string{
		"AVCapturePhotoCaptureDelegate", "AVCaptureFileOutputRecordingDelegate", "AVCaptureMetadataOutputObjectsDelegate",
	}, []objc.MethodDef{
		// On the photo output's queue: the photo is read there.
		Method("captureOutput:didFinishProcessingPhoto:error:", func(self ID, _ SEL, out, photo, err ID) {
			var data []byte
			if err == 0 && photo != 0 {
				data = Bytes(Send(photo, "fileDataRepresentation"))
			}
			fail := errorText(err, "no photo")
			theHost.Post(func() {
				if v := cameraByOutput[out]; v != nil {
					v.photoDone(data, fail)
				}
			})
		}),
		Method("captureOutput:didFinishRecordingToOutputFileAtURL:fromConnections:error:", func(self ID, _ SEL, out, url, conns, err ID) {
			path := GoString(Send(url, "path"))
			// Stopped by its maximum duration (or the disk filling) the
			// file is still whole.
			ok := err == 0
			if !ok {
				if info := Send(err, "userInfo"); info != 0 {
					if k := avConst("AVErrorRecordingSuccessfullyFinishedKey"); k != 0 {
						if n := Send(info, "objectForKey:", uintptr(k)); n != 0 {
							ok = SendBool(n, "boolValue")
						}
					}
				}
			}
			fail := errorText(err, "the recording failed")
			theHost.Post(func() {
				if v := cameraByOutput[out]; v != nil {
					r := v.recording
					v.recording = nil
					if r == nil {
						return
					}
					if ok {
						r([]byte(path), nil)
					} else {
						r(nil, fail)
					}
				}
			})
		}),
		// On the main queue.
		Method("captureOutput:didOutputMetadataObjects:fromConnection:", func(self ID, _ SEL, out, objects, conn ID) {
			if v := cameraByOutput[out]; v != nil {
				v.codesFound(objects)
			}
		}),
	})
	cameraDelegate = New("AsukaCameraDelegate")

	DefineClass("AsukaCameraTake", "NSObject", nil, []objc.MethodDef{
		Method("take:", func(self ID, _ SEL, sender ID) { cameraTaking.take() }),
		Method("cancel:", func(self ID, _ SEL, sender ID) { cameraTaking.finish(nil, nil) }),
	})
	cameraTakeTgt = New("AsukaCameraTake")
}

// Authorization, seams for the tests (which can't answer a prompt).
var (
	cameraAuthStatus = func(media ID) int {
		return SendInt(Class("AVCaptureDevice"), "authorizationStatusForMediaType:", uintptr(media))
	}
	cameraRequestAccess = func(h *Host, media ID, done func(bool)) {
		var b objc.Block
		b = NewBlock(func(_ objc.Block, ok bool) {
			h.Post(func() {
				b.Release()
				done(ok)
			})
		})
		Send(Class("AVCaptureDevice"), "requestAccessForMediaType:completionHandler:", uintptr(media), uintptr(b))
	}
	// cameraDeviceFor is the camera facing that way, or 0.
	cameraDeviceFor = func(facing string) ID {
		// A Mac's built-in camera faces the user; there's no back one: "back"
		// takes the built-in camera too, or the default (an external one,
		// Continuity Camera).
		pos := uintptr(2) // AVCaptureDevicePositionFront
		if facing == "back" {
			pos = 1
		}
		if t := avConst("AVCaptureDeviceTypeBuiltInWideAngleCamera"); t != 0 {
			for _, p := range []uintptr{pos, 3 - pos, 0} {
				if d := Send(Class("AVCaptureDevice"), "defaultDeviceWithDeviceType:mediaType:position:", uintptr(t), uintptr(cameraVideoMedia), p); d != 0 {
					return d
				}
			}
		}
		return Send(Class("AVCaptureDevice"), "defaultDeviceWithMediaType:", uintptr(cameraVideoMedia))
	}
)

// cameraAuthorize asks the first time, then calls then with whether
// media (video or audio) is allowed.
func cameraAuthorize(h *Host, media ID, then func(bool)) {
	switch cameraAuthStatus(media) {
	case avAuthorized:
		then(true)
	case avNotDetermined:
		cameraRequestAccess(h, media, then)
	default:
		then(false)
	}
}

func cameraModule(h *Host) map[string]method {
	noView := errors.New("no camera view")
	return map[string]method{
		"capture": func(_ []byte, reply func([]byte, error)) {
			v := cameraCurrent
			if v == nil {
				reply(nil, noView)
				return
			}
			v.capture(reply)
		},
		// args: u8 mute, u32 max milliseconds (0: none).
		"record": func(args []byte, reply func([]byte, error)) {
			v := cameraCurrent
			if v == nil {
				reply(nil, noView)
				return
			}
			mute, max, ok := cameraRecordArgs(args)
			if !ok {
				reply(nil, errors.New("bad arguments"))
				return
			}
			v.record(mute, max, reply)
		},
		"stopRecording": func(_ []byte, reply func([]byte, error)) {
			if v := cameraCurrent; v != nil {
				v.stopRecording()
			}
			reply(nil, nil)
		},
		"take": func(_ []byte, reply func([]byte, error)) { cameraTake(h, reply) },
	}
}

// cameraRecordArgs decodes record's: u8 mute, u32 max milliseconds.
func cameraRecordArgs(b []byte) (mute bool, max time.Duration, ok bool) {
	if len(b) < 5 {
		return false, 0, false
	}
	return b[0] == 1, time.Duration(binary.LittleEndian.Uint32(b[1:])) * time.Millisecond, true
}

// The view.

type cameraView struct {
	h        *Host
	emit     func(string, map[string]any)
	view     ID // AsukaCameraPreview
	layer    ID // its AVCaptureVideoPreviewLayer
	session  ID
	photo    ID // AVCapturePhotoOutput
	movie    ID // AVCaptureMovieFileOutput
	metadata ID // AVCaptureMetadataOutput, with scan on (macOS 13)

	facing       string
	video, scan  bool
	kinds        cameraKinds
	seen         cameraSeen
	configuring  bool
	inWindow     bool
	sheet        bool // the take sheet's: never the current one
	destroyed    bool
	warnedNoScan bool
	capturing    func([]byte, error)
	recording    func([]byte, error)
	work         chan func() // the session's start and stop, in order, off the main thread
}

func newCameraView(h *Host, emit func(string, map[string]any), sheet bool) *cameraView {
	v := &cameraView{h: h, emit: emit, facing: "back", sheet: sheet, work: make(chan func(), 8)}
	if !cameraLoad() {
		v.view = New("NSView")
		h.Post(func() { v.fail("no camera") })
		return v
	}
	v.session = New("AVCaptureSession")
	v.layer = Retain(Send(Class("AVCaptureVideoPreviewLayer"), "layerWithSession:", uintptr(v.session)))
	if g := avConst("AVLayerVideoGravityResizeAspectFill"); g != 0 {
		Send(v.layer, "setVideoGravity:", uintptr(g))
	}
	v.view = New("AsukaCameraPreview")
	cameraByView[v.view] = v
	Send(v.view, "setWantsLayer:", 1)
	go func() {
		runtime.LockOSThread()
		for f := range v.work {
			WithPool(f)
		}
	}()
	if sheet {
		v.facing = "front"
	}
	return v
}

func (v *cameraView) Handle() uintptr { return uintptr(v.view) }

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
		v.kinds = newCameraKinds(s)
	default:
		return
	}
	// Once for the props set together.
	if v.configuring || v.session == 0 {
		return
	}
	v.configuring = true
	v.h.Post(func() {
		v.configuring = false
		v.configure()
	})
}

// codeTypes are the metadata types of each kind asuka/sdk/scan names.
var codeTypes = []struct {
	kind  string
	types []string
}{
	{"qr", []string{"AVMetadataObjectTypeQRCode"}}, {"aztec", []string{"AVMetadataObjectTypeAztecCode"}},
	{"datamatrix", []string{"AVMetadataObjectTypeDataMatrixCode"}}, {"pdf417", []string{"AVMetadataObjectTypePDF417Code"}},
	{"ean13", []string{"AVMetadataObjectTypeEAN13Code"}}, {"ean8", []string{"AVMetadataObjectTypeEAN8Code"}},
	{"upca", []string{"AVMetadataObjectTypeEAN13Code"}}, {"upce", []string{"AVMetadataObjectTypeUPCECode"}},
	{"code128", []string{"AVMetadataObjectTypeCode128Code"}}, {"code39", []string{"AVMetadataObjectTypeCode39Code"}},
	{"code93", []string{"AVMetadataObjectTypeCode93Code"}}, {"codabar", []string{"AVMetadataObjectTypeCodabarCode"}},
	{"itf", []string{"AVMetadataObjectTypeITF14Code", "AVMetadataObjectTypeInterleaved2of5Code"}},
}

func (v *cameraView) configure() {
	if v.destroyed {
		return
	}
	cameraAuthorize(v.h, cameraVideoMedia, func(ok bool) {
		if v.destroyed {
			return
		}
		if !ok {
			v.fail("not allowed")
			return
		}
		dev := cameraDeviceFor(v.facing)
		if dev == 0 {
			v.fail("no camera")
			return
		}
		input := Send(Class("AVCaptureDeviceInput"), "deviceInputWithDevice:error:", uintptr(dev), 0)
		if input == 0 {
			v.fail("no camera")
			return
		}
		s := v.session
		Send(s, "beginConfiguration")
		preset := "AVCaptureSessionPresetPhoto"
		if v.video {
			preset = "AVCaptureSessionPresetHigh"
		}
		if p := avConst(preset); p != 0 && SendBool(s, "canSetSessionPreset:", uintptr(p)) {
			Send(s, "setSessionPreset:", uintptr(p))
		}
		for _, in := range ArrayItems(Send(s, "inputs")) {
			if d := Send(in, "device"); d == 0 || SendBool(d, "hasMediaType:", uintptr(cameraVideoMedia)) {
				Send(s, "removeInput:", uintptr(in))
			}
		}
		if SendBool(s, "canAddInput:", uintptr(input)) {
			Send(s, "addInput:", uintptr(input))
		}
		has := func(o ID) bool { return o != 0 && SendBool(Send(s, "outputs"), "containsObject:", uintptr(o)) }
		if v.video {
			if v.movie == 0 {
				v.movie = New("AVCaptureMovieFileOutput")
				cameraByOutput[v.movie] = v
			}
			if !has(v.movie) && SendBool(s, "canAddOutput:", uintptr(v.movie)) {
				Send(s, "addOutput:", uintptr(v.movie))
			}
		} else if has(v.movie) {
			Send(s, "removeOutput:", uintptr(v.movie))
		}
		if v.photo == 0 {
			v.photo = New("AVCapturePhotoOutput")
			cameraByOutput[v.photo] = v
		}
		if !has(v.photo) && SendBool(s, "canAddOutput:", uintptr(v.photo)) {
			Send(s, "addOutput:", uintptr(v.photo))
		}
		if v.scan {
			v.configureScan()
		} else if has(v.metadata) {
			Send(s, "removeOutput:", uintptr(v.metadata))
		}
		Send(s, "commitConfiguration")
		v.run(v.inWindow)
	})
}

// configureScan adds the metadata output (in the configuration under
// way) reading the kinds asked for that this camera can.
func (v *cameraView) configureScan() {
	s := v.session
	if !HasClass("AVCaptureMetadataOutput") { // before macOS 13
		if !v.warnedNoScan {
			v.warnedNoScan = true
			v.fail("scanning codes needs macOS 13")
		}
		return
	}
	if v.metadata == 0 {
		v.metadata = New("AVCaptureMetadataOutput")
		cameraByOutput[v.metadata] = v
		Send(v.metadata, "setMetadataObjectsDelegate:queue:", uintptr(cameraDelegate), mainQueue())
	}
	if !SendBool(Send(s, "outputs"), "containsObject:", uintptr(v.metadata)) {
		if !SendBool(s, "canAddOutput:", uintptr(v.metadata)) {
			return
		}
		Send(s, "addOutput:", uintptr(v.metadata))
	}
	// What it can read is known once it's in the session.
	available := Send(v.metadata, "availableMetadataObjectTypes")
	var types []ID
	seen := map[string]bool{}
	for _, c := range codeTypes {
		if !v.kinds.wants(c.kind) {
			continue
		}
		for _, name := range c.types {
			t := avConst(name)
			if t == 0 || seen[name] || !SendBool(available, "containsObject:", uintptr(t)) {
				continue
			}
			seen[name] = true
			types = append(types, t)
		}
	}
	Send(v.metadata, "setMetadataObjectTypes:", uintptr(Array(types...)))
}

// codesFound reports the codes newly in view.
func (v *cameraView) codesFound(objects ID) {
	now := time.Now()
	for _, o := range ArrayItems(objects) {
		if !RespondsTo(o, "stringValue") {
			continue
		}
		value := GoString(Send(o, "stringValue"))
		t := Send(o, "type")
		kind := ""
		for _, c := range codeTypes {
			if c.kind == "upca" {
				continue
			}
			for _, name := range c.types {
				if k := avConst(name); k != 0 && SendBool(t, "isEqualToString:", uintptr(k)) {
					kind = c.kind
				}
			}
			if kind != "" {
				break
			}
		}
		if kind == "" || value == "" {
			continue
		}
		kind, value = v.kinds.named(kind, value)
		if !v.kinds.wants(kind) || !v.seen.isNew(kind, value, now) {
			continue
		}
		v.emit("code", map[string]any{"kind": kind, "value": value})
	}
}

func (v *cameraView) attached(on bool) {
	v.inWindow = on
	if !v.sheet {
		if on {
			cameraCurrent = v
		} else if cameraCurrent == v {
			cameraCurrent = nil
		}
	}
	v.run(on)
}

// run starts or stops the session, off the main thread (startRunning
// blocks), in the order asked.
func (v *cameraView) run(on bool) {
	if v.session == 0 || v.destroyed {
		return
	}
	s := v.session
	v.work <- func() {
		running := SendBool(s, "isRunning")
		switch {
		case on && !running && SendInt(Send(s, "inputs"), "count") > 0:
			Send(s, "startRunning")
		case !on && running:
			Send(s, "stopRunning")
		}
	}
}

func (v *cameraView) capture(reply func([]byte, error)) {
	if v.session == 0 || !SendBool(v.session, "isRunning") {
		reply(nil, errors.New("the camera isn't showing"))
		return
	}
	if c := v.capturing; c != nil {
		c(nil, errors.New("another photo was taken"))
	}
	v.capturing = reply
	Send(v.photo, "capturePhotoWithSettings:delegate:", uintptr(Send(Class("AVCapturePhotoSettings"), "photoSettings")), uintptr(cameraDelegate))
}

func (v *cameraView) photoDone(data []byte, fail error) {
	reply := v.capturing
	v.capturing = nil
	if reply == nil {
		return
	}
	if data == nil {
		reply(nil, fail)
		return
	}
	path, err := cacheFile("asuka-camera", ".jpg")
	if err == nil {
		err = os.WriteFile(path, data, 0o644)
	}
	if err != nil {
		reply(nil, errors.New("couldn't save the photo"))
		return
	}
	reply([]byte(path), nil)
}

// record records to a file until stopRecording (or max, if not 0), with
// the microphone unless mute (or not allowed).
func (v *cameraView) record(mute bool, max time.Duration, reply func([]byte, error)) {
	if v.session == 0 || !SendBool(v.session, "isRunning") {
		reply(nil, errors.New("the camera isn't showing"))
		return
	}
	if !v.video || v.movie == 0 || !SendBool(Send(v.session, "outputs"), "containsObject:", uintptr(v.movie)) {
		reply(nil, errors.New("the camera view isn't in video mode"))
		return
	}
	if SendBool(v.movie, "isRecording") || v.recording != nil {
		reply(nil, errors.New("already recording"))
		return
	}
	start := func(sound bool) {
		if v.destroyed {
			reply(nil, errors.New("the camera isn't showing"))
			return
		}
		s := v.session
		Send(s, "beginConfiguration")
		var mic ID
		for _, in := range ArrayItems(Send(s, "inputs")) {
			if d := Send(in, "device"); d != 0 && SendBool(d, "hasMediaType:", uintptr(cameraAudioMedia)) {
				mic = in
			}
		}
		if sound && mic == 0 {
			if d := Send(Class("AVCaptureDevice"), "defaultDeviceWithMediaType:", uintptr(cameraAudioMedia)); d != 0 {
				if in := Send(Class("AVCaptureDeviceInput"), "deviceInputWithDevice:error:", uintptr(d), 0); in != 0 && SendBool(s, "canAddInput:", uintptr(in)) {
					Send(s, "addInput:", uintptr(in))
				}
			}
		} else if !sound && mic != 0 {
			Send(s, "removeInput:", uintptr(mic))
		}
		Send(s, "commitConfiguration")
		var limit cmTime // kCMTimeInvalid
		if max > 0 {
			limit = cmTime{Value: int64(max.Seconds() * 600), Timescale: 600, Flags: 1}
		}
		msgSetCMTime(v.movie, Sel("setMaxRecordedDuration:"), limit)
		path, err := cacheFile("asuka-camera", ".mov")
		if err != nil {
			reply(nil, err)
			return
		}
		v.recording = reply
		Send(v.movie, "startRecordingToOutputFileURL:recordingDelegate:", uintptr(FileURL(path)), uintptr(cameraDelegate))
	}
	if mute {
		start(false)
		return
	}
	cameraAuthorize(v.h, cameraAudioMedia, start)
}

func (v *cameraView) stopRecording() {
	if v.movie != 0 && SendBool(v.movie, "isRecording") {
		Send(v.movie, "stopRecording")
	}
}

// Destroy implements native.Destroyer: the session stops, and what the
// view holds goes.
func (v *cameraView) Destroy() {
	if v.destroyed {
		return
	}
	v.destroyed = true
	if cameraCurrent == v {
		cameraCurrent = nil
	}
	if c := v.capturing; c != nil {
		v.capturing = nil
		c(nil, errors.New("the camera isn't showing"))
	}
	if r := v.recording; r != nil {
		v.recording = nil
		r(nil, errors.New("the camera isn't showing"))
	}
	delete(cameraByView, v.view)
	for _, o := range []ID{v.photo, v.movie, v.metadata} {
		if o != 0 {
			delete(cameraByOutput, o)
		}
	}
	if v.metadata != 0 {
		Send(v.metadata, "setMetadataObjectsDelegate:queue:", 0, 0)
	}
	if v.session != 0 {
		s, layer, outs := v.session, v.layer, []ID{v.photo, v.movie, v.metadata}
		v.work <- func() {
			if SendBool(s, "isRunning") {
				Send(s, "stopRunning")
			}
			for _, o := range outs {
				Release(o)
			}
			Release(layer)
			Release(s)
		}
	}
	close(v.work)
	Release(v.view)
}

// Codes' kinds and repeats, as iOS's AsukaCodeKinds and AsukaCodeSeen.

// cameraKinds are the kinds of code asked for (none: every kind).
type cameraKinds map[string]bool

func newCameraKinds(s string) cameraKinds {
	k := cameraKinds{}
	for _, x := range strings.Split(s, ",") {
		if x != "" {
			k[x] = true
		}
	}
	return k
}

func (k cameraKinds) wants(kind string) bool { return len(k) == 0 || k[kind] }

// named is the code as asuka/sdk/scan names it: Apple reads a UPC-A as an
// EAN-13 with a leading 0, a UPC-A again if the app asked for those.
func (k cameraKinds) named(kind, value string) (string, string) {
	if kind == "ean13" && len(value) == 13 && value[0] == '0' && k.wants("upca") {
		return "upca", value[1:]
	}
	return kind, value
}

// cameraSeen drops repeats of a code in view: a code is reported as it
// comes into view, and again once it's been out of it for a second.
type cameraSeen struct{ last map[string]time.Time }

func (s *cameraSeen) isNew(kind, value string, now time.Time) bool {
	if s.last == nil {
		s.last = map[string]time.Time{}
	}
	key := kind + "\x00" + value
	t, ok := s.last[key]
	isNew := !ok || now.Sub(t) > time.Second
	s.last[key] = now
	if len(s.last) > 32 {
		for k, t := range s.last {
			if now.Sub(t) > time.Second {
				delete(s.last, k)
			}
		}
	}
	return isNew
}

// The take sheet.

type cameraSheet struct {
	h      *Host
	reply  func([]byte, error)
	parent ID // the window it's over (0: shown on its own)
	window ID
	view   *cameraView
	button ID // Take Photo
}

var cameraTaking *cameraSheet

// cameraTake shows the sheet and answers with the photo's path, or ""
// if cancelled.
func cameraTake(h *Host, reply func([]byte, error)) {
	if !cameraLoad() {
		reply(nil, errors.New("no camera"))
		return
	}
	cameraTaking.finish(nil, nil) // one at a time
	cameraAuthorize(h, cameraVideoMedia, func(ok bool) {
		if !ok {
			reply(nil, errNotAllowed)
			return
		}
		if cameraDeviceFor("front") == 0 {
			reply(nil, errors.New("no camera"))
			return
		}
		cameraTaking.finish(nil, nil)
		showCameraSheet(h, reply)
	})
}

// showCameraSheet shows the sheet, which is cameraTaking until it's
// finished.
func showCameraSheet(h *Host, reply func([]byte, error)) {
	const w, ht, bar = 640.0, 532.0, 52.0
	s := &cameraSheet{h: h, reply: reply}
	s.window = MsgInitWindow(Send(Class("NSWindow"), "alloc"), Sel("initWithContentRect:styleMask:backing:defer:"),
		Rect{Size: Size{Width: w, Height: ht}}, 1, 2, false) // titled, buffered
	Send(s.window, "setReleasedWhenClosed:", 0)
	content := Send(s.window, "contentView")
	s.view = newCameraView(h, func(name string, values map[string]any) {
		if name == "error" {
			msg, _ := values["message"].(string)
			s.finish(nil, errors.New(msg))
		}
	}, true)
	MsgSetRect(s.view.view, selSetFrame, Rect{Origin: Point{Y: bar}, Size: Size{Width: w, Height: ht - bar}})
	Send(s.view.view, "setAutoresizingMask:", 18)
	Send(content, "addSubview:", uintptr(s.view.view))
	button := func(title, key string, sel string, x float64) ID {
		b := Send(Class("NSButton"), "buttonWithTitle:target:action:", uintptr(String(title)), uintptr(cameraTakeTgt), uintptr(Sel(sel)))
		Send(b, "setKeyEquivalent:", uintptr(String(key)))
		MsgSetRect(b, selSetFrame, Rect{Origin: Point{X: x, Y: 12}, Size: Size{Width: 120, Height: 28}})
		Send(content, "addSubview:", uintptr(b))
		return b
	}
	s.button = button(l10n.TakePhoto(h.locale), "\r", "take:", w-20-120)
	button(l10n.Cancel(h.locale), "\x1b", "cancel:", w-20-120-12-120)
	if win := h.keyWin(); win != nil && win.window != 0 {
		s.parent = win.window
		Send(s.parent, "beginSheet:completionHandler:", uintptr(s.window), 0)
	} else {
		Send(s.window, "center")
		Send(s.window, "makeKeyAndOrderFront:", 0)
	}
	cameraTaking = s
	s.view.configure()
}

func (s *cameraSheet) take() {
	if s == nil {
		return
	}
	Send(s.button, "setEnabled:", 0)
	s.view.capture(func(b []byte, err error) { s.finish(b, err) })
}

// finish closes the sheet and answers (nil, nil: cancelled).
func (s *cameraSheet) finish(b []byte, err error) {
	if s == nil || cameraTaking != s {
		return
	}
	cameraTaking = nil
	if s.parent != 0 {
		Send(s.parent, "endSheet:", uintptr(s.window))
	}
	Send(s.window, "orderOut:", 0)
	s.view.Destroy()
	Release(s.window)
	if b == nil && err == nil {
		b = []byte{}
	}
	s.reply(b, err)
}
