//go:build linux && !android

package linux

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unsafe"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/native"
)

// asuka/sdk/camera on Linux, with GStreamer. The camera is the camera
// portal's (org.freedesktop.portal.Camera: it asks the user once, and
// gives a PipeWire remote with the cameras, which pipewiresrc reads),
// or, where there's no portal or it has no camera (a session without
// PipeWire's camera support), the first Video4Linux camera (v4l2src).
// The "asuka.camera" view is a GtkPicture showing the camera's frames
// (an appsink's, mirrored as a camera facing the user is), filling its
// box; Capture writes its latest frame as a JPEG; in video mode
// asuka/sdk/camera/record records an MPEG-4 file (H.264 and AAC; WebM with
// VP8 and Opus where there's no H.264 encoder), with the microphone
// unless muted; with scan, GStreamer's zbar element reads codes. "take"
// shows a dialog of the host's own over the window in use, as macOS
// does: a live preview with Take Photo and Cancel (Return and Escape).
// A computer's cameras face the user: front and back both take the
// first one, as on a Mac. Photos and videos go to the app's caches
// (asuka-camera).

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["camera"] = h.cameraModule() })
	builtinViews["asuka.camera"] = func(ctx native.Context) native.View {
		return newCameraView(theHost, ctx.Emit, false)
	}
}

// cameraSource is the pipeline's source, as gst-launch writes it: ""
// finds the camera (the portal's, else V4L2's); the tests use a test
// pattern.
var cameraSource string

var cameraGrant struct {
	pipewireFD int // the portal's PipeWire remote, once granted (-1: none)
	asked      bool
	waiting    []func(string, error)
}

var (
	errNoCamera      = errors.New("no camera")
	errCameraDenied  = errors.New("not allowed")
	errNoCameraView  = errors.New("no camera view")
	errCameraNotShow = errors.New("the camera isn't showing")
)

// cameraOpen finds the camera, asking the portal the first time, and
// gives the source's description.
func (h *Host) cameraOpen(done func(src string, err error)) {
	if err := LoadGstApp(); err != nil {
		done("", errors.New("the camera needs GStreamer: "+err.Error()))
		return
	}
	if cameraSource != "" {
		done(cameraSource, nil)
		return
	}
	g := &cameraGrant
	if g.asked && g.pipewireFD >= 0 {
		done(fmt.Sprintf("pipewiresrc fd=%d", g.pipewireFD), nil)
		return
	}
	g.waiting = append(g.waiting, done)
	if len(g.waiting) > 1 {
		return
	}
	finish := func(src string, err error) {
		ws := g.waiting
		g.waiting = nil
		for _, w := range ws {
			w(src, err)
		}
	}
	v4l2 := func() {
		devs := cameraDevices()
		if len(devs) == 0 || !GstHas("v4l2src") {
			finish("", errNoCamera)
			return
		}
		finish("v4l2src device="+devs[0]+" ! decodebin", nil)
	}
	conn, err := Bus(BusSession)
	if err != nil {
		v4l2()
		return
	}
	DBusCall(conn, portalDest, portalPath, "org.freedesktop.DBus.Properties", "Get",
		VTuple(VStr("org.freedesktop.portal.Camera"), VStr("IsCameraPresent")), 5000, func(r Ptr, err error) {
			present := false
			if err == nil {
				if args, _ := VariantGo(r).([]any); len(args) > 0 {
					present, _ = args[0].(bool)
				}
			}
			if !present || !GstHas("pipewiresrc") {
				// No portal, or no camera it shows (PipeWire without its
				// camera support): V4L2's, outside a sandbox.
				v4l2()
				return
			}
			h.portalRequest("org.freedesktop.portal.Camera", "AccessCamera", func(token string) Ptr {
				return VTuple(VDict(map[string]Ptr{"handle_token": VStr(token)}))
			}, nil, func(code uint32, _ map[string]any, err error) {
				if err != nil || code != 0 {
					finish("", errCameraDenied)
					return
				}
				DBusCallOutFDs(conn, portalDest, portalPath, "org.freedesktop.portal.Camera", "OpenPipeWireRemote",
					VTuple(VDict(map[string]Ptr{})), 5000, func(r Ptr, fds []int, err error) {
						idx := int32(-1)
						if err == nil {
							if args, _ := VariantGo(r).([]any); len(args) > 0 {
								idx, _ = args[0].(int32)
							}
						}
						if idx < 0 || int(idx) >= len(fds) {
							for _, fd := range fds {
								closeFD(fd)
							}
							finish("", errNoCamera)
							return
						}
						for i, fd := range fds {
							if i != int(idx) {
								closeFD(fd)
							}
						}
						g.asked, g.pipewireFD = true, fds[idx]
						finish(fmt.Sprintf("pipewiresrc fd=%d", g.pipewireFD), nil)
					})
			})
		})
}

func closeFD(fd int) { os.NewFile(uintptr(fd), "").Close() }

// cameraDevices are the Video4Linux cameras (their capture nodes: a
// camera's metadata node is its second), in order.
var cameraDevices = func() []string {
	dirs, _ := filepath.Glob("/sys/class/video4linux/video*")
	var out []string
	for _, d := range dirs {
		if idx, err := os.ReadFile(filepath.Join(d, "index")); err == nil && strings.TrimSpace(string(idx)) != "0" {
			continue
		}
		out = append(out, "/dev/"+filepath.Base(d))
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		return len(a) < len(b) || len(a) == len(b) && a < b
	})
	return out
}

// cameraEncoders picks the recording's encoders: H.264 and AAC in an
// MPEG-4 file, else VP8 and Opus (or Vorbis) in WebM.
func cameraEncoders(mute bool) (ext, video, audio, mux string, err error) {
	h264 := map[string]string{
		"x264enc":     "x264enc tune=zerolatency speed-preset=veryfast key-int-max=60",
		"openh264enc": "openh264enc",
		"vah264enc":   "vah264enc",
		"vah264lpenc": "vah264lpenc",
		"v4l2h264enc": "v4l2h264enc",
	}
	for _, e := range []string{"x264enc", "openh264enc", "vah264enc", "vah264lpenc", "v4l2h264enc"} {
		if GstHas(e) && GstHas("h264parse") && GstHas("mp4mux") {
			video = h264[e] + " ! h264parse"
			mux = "mp4mux"
			ext = ".mp4"
			if !mute {
				for _, a := range aacEncoders {
					if GstHas(a) {
						audio = a + " ! aacparse"
						break
					}
				}
				if audio == "" && GstHas("opusenc") {
					audio = "opusenc"
				}
			}
			break
		}
	}
	if video == "" {
		if !GstHas("vp8enc") || !GstHas("webmmux") {
			return "", "", "", "", errors.New("no video encoder (install GStreamer's ugly or bad plugins: x264 or OpenH264)")
		}
		video, mux, ext = "vp8enc deadline=1 cpu-used=8", "webmmux", ".webm"
		if !mute {
			for _, a := range []string{"opusenc", "vorbisenc"} {
				if GstHas(a) {
					audio = a
					break
				}
			}
		}
	}
	return
}

// The view.

type cameraView struct {
	h       *Host
	emit    func(string, map[string]any)
	picture Ptr // the GtkPicture: n.inner, or the take dialog's

	pipe, sink, bus Ptr
	busHandlers     []uint64
	mapHandlers     []uint64
	ticking         bool

	facing      string
	video, scan bool
	kinds       cameraKinds
	seen        cameraSeen
	mapped      bool
	frames      int // frames shown
	opening     bool
	configuring bool
	sheet       bool // the take dialog's: never the current one
	destroyed   bool
	src         string // the camera's source, once open

	capturing func([]byte, error)
	// A recording: its reply, its file, and its encoders.
	recording         func([]byte, error)
	recPath           string
	recMute, stopping bool
	recTimer          uint32
}

// cameraCurrent is the view on screen Capture and record use.
var cameraCurrent *cameraView

func newCameraView(h *Host, emit func(string, map[string]any), sheet bool) *cameraView {
	v := &cameraView{h: h, emit: emit, facing: "back", sheet: sheet}
	v.picture = PictureNew()
	PictureSetContentFit(v.picture, 2) // GTK_CONTENT_FIT_COVER
	PictureSetCanShrink(v.picture, true)
	WidgetAddCSSClass(v.picture, "asuka-camera")
	v.mapHandlers = append(v.mapHandlers,
		Connect(v.picture, "map", func() { v.attached(true) }),
		Connect(v.picture, "unmap", func() { v.attached(false) }),
	)
	if sheet {
		v.facing = "front"
	}
	return v
}

func (v *cameraView) Handle() uintptr { return v.picture }

// Measure: none of its own; its style gives it a size.
func (v *cameraView) Measure(w, h float32) (float32, float32) {
	if w != w {
		w = 0
	}
	if h != h {
		h = 0
	}
	return w, h
}

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
		return // the same camera
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
	if v.configuring || v.pipe == 0 {
		return
	}
	v.configuring = true
	v.h.Post(func() {
		v.configuring = false
		if v.pipe != 0 && v.recording == nil {
			v.start()
		}
	})
}

func (v *cameraView) attached(on bool) {
	v.mapped = on
	if !v.sheet {
		if on {
			cameraCurrent = v
		} else if cameraCurrent == v {
			cameraCurrent = nil
		}
	}
	if on {
		v.open()
	} else {
		v.stopPipe()
	}
}

// open finds the camera (once), then starts the pipeline.
func (v *cameraView) open() {
	if v.destroyed || v.opening {
		return
	}
	if v.src != "" {
		v.start()
		return
	}
	v.opening = true
	v.h.cameraOpen(func(src string, err error) {
		v.opening = false
		if v.destroyed {
			return
		}
		if err != nil {
			v.fail(err.Error())
			return
		}
		v.src = src
		if v.mapped {
			v.start()
		}
	})
}

// description is the pipeline: the camera, through a tee, to the
// preview's appsink (mirrored), to zbar with scan, and to the encoders
// and a file while recording.
func (v *cameraView) description() (string, error) {
	var b strings.Builder
	b.WriteString(v.src)
	b.WriteString(" ! queue leaky=downstream max-size-buffers=2 ! videoconvert ! tee name=t")
	b.WriteString(" t. ! queue leaky=downstream max-size-buffers=1 ! videoflip method=horizontal-flip ! videoconvert ! video/x-raw,format=RGBA ! appsink name=frames max-buffers=1 drop=true sync=false")
	if v.scan && GstHas("zbar") {
		b.WriteString(" t. ! queue leaky=downstream max-size-buffers=1 ! videoconvert ! zbar cache=false ! fakesink sync=false async=false")
	}
	if v.recording != nil {
		ext, video, audio, mux, err := cameraEncoders(v.recMute)
		if err != nil {
			return "", err
		}
		if !strings.HasSuffix(v.recPath, ext) {
			v.recPath = strings.TrimSuffix(v.recPath, filepath.Ext(v.recPath)) + ext
		}
		fmt.Fprintf(&b, " t. ! queue max-size-buffers=0 max-size-bytes=0 max-size-time=3000000000 ! videoconvert ! %s ! %s name=mux ! filesink name=file", video, mux)
		if audio != "" {
			fmt.Fprintf(&b, " %s ! queue ! audioconvert ! audioresample ! %s ! mux.", recordSource, audio)
		}
	}
	return b.String(), nil
}

// start (re)makes the pipeline and plays it.
func (v *cameraView) start() {
	v.stopPipe()
	if v.destroyed || !v.mapped || v.src == "" {
		return
	}
	desc, err := v.description()
	if err != nil {
		v.recordFailed(err)
		return
	}
	var gerr Ptr
	pipe := Gst.ParseLaunch(desc, &gerr)
	if pipe == 0 {
		msg := GError(gerr)
		v.fail("the camera can't show: " + msg.Error())
		v.recordFailed(msg)
		return
	}
	if gerr != 0 {
		ErrorFree(gerr)
	}
	ObjectRefSink(pipe)
	v.pipe = pipe
	v.sink = Gst.BinGetByName(pipe, "frames")
	if f := Gst.BinGetByName(pipe, "file"); f != 0 {
		SetStringProperty(f, "location", v.recPath)
		Gst.ObjectUnref(f)
	}
	v.bus = Gst.ElementGetBus(pipe)
	Gst.BusAddSignalWatch(v.bus)
	v.busHandlers = append(v.busHandlers,
		Connect(v.bus, "message::error", func(msg uintptr) {
			err := GstMessageError(msg)
			v.stopPipe()
			if v.recording != nil {
				v.recordFailed(err)
				v.start()
				return
			}
			v.failAll(err)
		}),
		Connect(v.bus, "message::eos", func(uintptr) { v.ended() }),
		Connect(v.bus, "message::element", func(msg uintptr) { v.codeMessage(msg) }),
	)
	if Gst.ElementSetState(pipe, GstStatePlaying) == GstStateChangeFailure {
		v.stopPipe()
		v.failAll(errNoCamera)
		return
	}
	if !v.ticking {
		v.ticking = true
		AddTickCallback(v.picture, func(Ptr) bool {
			if v.pipe == 0 || v.destroyed {
				v.ticking = false
				return false
			}
			v.pullFrame()
			return true
		})
	}
}

// failAll reports the camera's failing: the view's error, and a pending
// photo's.
func (v *cameraView) failAll(err error) {
	v.fail(err.Error())
	if c := v.capturing; c != nil {
		v.capturing = nil
		c(nil, err)
	}
}

// pullFrame shows the camera's latest frame, if there's a new one, and
// gives it to a pending Capture.
func (v *cameraView) pullFrame() {
	if v.sink == 0 {
		return
	}
	sample := Call(FnAppSinkTryPullSample, v.sink, 0)
	if sample == 0 {
		return
	}
	GstSampleFrame(sample, func(data Ptr, w, h, stride int) {
		SetPictureRGBA(v.picture, data, w, h, stride)
		v.frames++
		if v.capturing != nil {
			// The preview's mirrored: the photo isn't.
			img := image.NewRGBA(image.Rect(0, 0, w, h))
			src := unsafe.Slice(At[byte](data), stride*h)
			for y := range h {
				row, out := src[y*stride:], img.Pix[y*img.Stride:]
				for x := range w {
					copy(out[x*4:x*4+4], row[(w-1-x)*4:(w-1-x)*4+4])
				}
			}
			v.photoTaken(img)
		}
	})
	Call(FnMiniObjectUnref, sample)
}

func (v *cameraView) capture(reply func([]byte, error)) {
	if v.pipe == 0 {
		reply(nil, errCameraNotShow)
		return
	}
	if c := v.capturing; c != nil {
		c(nil, errors.New("another photo was taken"))
	}
	v.capturing = reply
}

// photoTaken writes the photo, off the main thread, and answers.
func (v *cameraView) photoTaken(img *image.RGBA) {
	reply := v.capturing
	v.capturing = nil
	dir := filepath.Join(v.h.modCacheDir(), "asuka-camera")
	h := v.h
	go func() {
		path := filepath.Join(dir, randomName()+".jpg")
		err := os.MkdirAll(dir, 0o755)
		var f *os.File
		if err == nil {
			f, err = os.Create(path)
		}
		if err == nil {
			err = jpeg.Encode(f, img, &jpeg.Options{Quality: 90})
			if cerr := f.Close(); err == nil {
				err = cerr
			}
		}
		h.Post(func() {
			if err != nil {
				reply(nil, errors.New("couldn't save the photo"))
				return
			}
			reply([]byte(path), nil)
		})
	}()
}

// codeMessage reports a code zbar read, newly in view.
func (v *cameraView) codeMessage(msg Ptr) {
	st := GstMessageStructurePtr(msg)
	if st == 0 || GoString(Gst.StructureGetName(st)) != "barcode" {
		return
	}
	kind, value := zbarKind(GstStructureString(st, "type")), GstStructureString(st, "symbol")
	if kind == "" || value == "" {
		return
	}
	kind, value = v.kinds.named(kind, value)
	if !v.kinds.wants(kind) || !v.seen.isNew(kind, value, time.Now()) {
		return
	}
	v.emit("code", map[string]any{"kind": kind, "value": value})
}

// record records to a file until stopRecording (or max, if not 0), with
// the microphone unless mute.
func (v *cameraView) record(mute bool, max time.Duration, reply func([]byte, error)) {
	if v.pipe == 0 {
		reply(nil, errCameraNotShow)
		return
	}
	if !v.video {
		reply(nil, errors.New("the camera view isn't in video mode"))
		return
	}
	if v.recording != nil {
		reply(nil, errors.New("already recording"))
		return
	}
	if _, _, _, _, err := cameraEncoders(mute); err != nil {
		reply(nil, err)
		return
	}
	v.recording, v.recMute, v.stopping = reply, mute, false
	v.recPath = filepath.Join(v.h.modCacheDir(), "asuka-camera", randomName()+".mp4")
	if err := os.MkdirAll(filepath.Dir(v.recPath), 0o755); err != nil {
		v.recordFailed(err)
		return
	}
	v.start()
	if v.recording != nil && max > 0 {
		v.recTimer = TimeoutAdd(uint32(max/time.Millisecond), func() bool {
			v.recTimer = 0
			v.stopRecording()
			return false
		})
	}
}

// stopRecording ends the recording: the encoders and the muxer finish
// the file (the end of the stream), then record's reply gets it.
func (v *cameraView) stopRecording() {
	if v.recording == nil || v.stopping || v.pipe == 0 {
		return
	}
	v.stopping = true
	if v.recTimer != 0 {
		SourceRemove(v.recTimer)
		v.recTimer = 0
	}
	Gst.SendEvent(v.pipe, Gst.EventNewEOS())
}

// ended is the pipeline's end: a recording's file is whole; the preview
// goes on.
func (v *cameraView) ended() {
	r, path := v.recording, v.recPath
	v.recording, v.stopping = nil, false
	v.stopPipe()
	if r != nil {
		r([]byte(path), nil)
	}
	v.start()
}

func (v *cameraView) recordFailed(err error) {
	r := v.recording
	v.recording, v.stopping = nil, false
	if v.recTimer != 0 {
		SourceRemove(v.recTimer)
		v.recTimer = 0
	}
	if r != nil {
		r(nil, errors.New("the recording failed: "+err.Error()))
	}
}

// stopPipe stops and lets go of the pipeline.
func (v *cameraView) stopPipe() {
	if v.pipe == 0 {
		return
	}
	for _, hd := range v.busHandlers {
		Disconnect(v.bus, hd)
	}
	v.busHandlers = nil
	Gst.BusRemoveSignalWat(v.bus)
	Gst.ObjectUnref(v.bus)
	v.bus = 0
	if v.sink != 0 {
		Gst.ObjectUnref(v.sink)
		v.sink = 0
	}
	Gst.ElementSetState(v.pipe, GstStateNull)
	Gst.ObjectUnref(v.pipe)
	v.pipe = 0
}

// Destroy implements native.Destroyer: the pipeline stops, and what's
// pending fails.
func (v *cameraView) Destroy() {
	if v.destroyed {
		return
	}
	if cameraCurrent == v {
		cameraCurrent = nil
	}
	if r := v.recording; r != nil {
		// What's recorded so far is lost with the pipeline.
		v.recordFailed(errCameraNotShow)
	}
	v.stopPipe()
	v.destroyed = true
	if c := v.capturing; c != nil {
		v.capturing = nil
		c(nil, errCameraNotShow)
	}
	for _, hd := range v.mapHandlers {
		Disconnect(v.picture, hd)
	}
	v.mapHandlers = nil
}

func (h *Host) cameraModule() map[string]method {
	return map[string]method{
		"capture": func(_ []byte, reply func([]byte, error)) {
			v := cameraCurrent
			if v == nil {
				reply(nil, errNoCameraView)
				return
			}
			v.capture(reply)
		},
		// args: u8 mute, u32 max milliseconds (0: none).
		"record": func(args []byte, reply func([]byte, error)) {
			v := cameraCurrent
			if v == nil {
				reply(nil, errNoCameraView)
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

// Codes' kinds and repeats, as macOS's.

// zbarKind is asuka/sdk/scan's name of a ZBar symbology ("": none of its).
func zbarKind(t string) string {
	switch t {
	case "QR-Code":
		return "qr"
	case "EAN-13":
		return "ean13"
	case "EAN-8":
		return "ean8"
	case "UPC-A":
		return "upca"
	case "UPC-E":
		return "upce"
	case "CODE-128":
		return "code128"
	case "CODE-39":
		return "code39"
	case "CODE-93":
		return "code93"
	case "Codabar", "CODABAR":
		return "codabar"
	case "I2/5":
		return "itf"
	case "PDF417":
		return "pdf417"
	}
	return ""
}

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

// named is the code as asuka/sdk/scan names it: a UPC-A for an app that asked
// for EAN-13s but not UPC-As is an EAN-13 with a leading 0, as Apple's
// readers give it.
func (k cameraKinds) named(kind, value string) (string, string) {
	if kind == "upca" && !k.wants("upca") && k.wants("ean13") && len(value) == 12 {
		return "ean13", "0" + value
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

// The take dialog.

type cameraSheet struct {
	h      *Host
	reply  func([]byte, error)
	dialog Ptr
	view   *cameraView
	button Ptr // Take Photo
	cancel Ptr
}

var cameraTaking *cameraSheet

// cameraTake shows the dialog and answers with the photo's path, or ""
// if cancelled.
func (h *Host) cameraTake(reply func([]byte, error)) {
	cameraTaking.finish(nil, nil) // one at a time
	h.cameraOpen(func(src string, err error) {
		if err != nil {
			reply(nil, err)
			return
		}
		cameraTaking.finish(nil, nil)
		h.showCameraSheet(src, reply)
	})
}

func (h *Host) showCameraSheet(src string, reply func([]byte, error)) {
	s := &cameraSheet{h: h, reply: reply}
	s.dialog = ObjectRefSink(AdwDialogNew())
	AdwDialogSetTitle(s.dialog, l10n.TakePhoto(h.locale))
	AdwDialogSetContentWidth(s.dialog, 640)
	AdwDialogSetContentHeight(s.dialog, 532)
	tv := AdwToolbarViewNew()
	bar := AdwHeaderBarNew()
	AdwHeaderBarSetShowStartTitleButtons(bar, false)
	AdwHeaderBarSetShowEndTitleButtons(bar, false)
	s.cancel = ButtonNew()
	ButtonSetLabel(s.cancel, l10n.Cancel(h.locale))
	AdwHeaderBarPackStart(bar, s.cancel)
	s.button = ButtonNew()
	ButtonSetLabel(s.button, l10n.TakePhoto(h.locale))
	WidgetAddCSSClass(s.button, "suggested-action")
	AdwHeaderBarPackEnd(bar, s.button)
	AdwToolbarViewAddTopBar(tv, bar)
	s.view = newCameraView(h, func(name string, values map[string]any) {
		if name == "error" {
			msg, _ := values["message"].(string)
			s.finish(nil, errors.New(msg))
		}
	}, true)
	s.view.src = src
	AdwToolbarViewSetContent(tv, s.view.picture)
	AdwDialogSetChild(s.dialog, tv)
	if Dev.AdwDialogSetDefaultWidget != nil {
		Dev.AdwDialogSetDefaultWidget(s.dialog, s.button) // Return
	}
	Connect(s.button, "clicked", func() { s.take() })
	Connect(s.cancel, "clicked", func() { s.finish(nil, nil) })
	Connect(s.dialog, "closed", func() { s.finish(nil, nil) }) // Escape, too
	cameraTaking = s
	AdwDialogPresent(s.dialog, h.dialogParent())
}

func (s *cameraSheet) take() {
	if s == nil || cameraTaking != s {
		return
	}
	WidgetSetSensitive(s.button, false)
	s.view.capture(func(b []byte, err error) { s.finish(b, err) })
}

// finish closes the dialog and answers (nil, nil: cancelled).
func (s *cameraSheet) finish(b []byte, err error) {
	if s == nil || cameraTaking != s {
		return
	}
	cameraTaking = nil
	s.view.Destroy()
	AdwDialogForceClose(s.dialog)
	ObjectUnref(s.dialog)
	if b == nil && err == nil {
		b = []byte{}
	}
	s.reply(b, err)
}
