//go:build linux && !android

package linux

import (
	"path/filepath"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// A Video is a GtkMediaFile (GTK's GStreamer backend) shown by a GtkVideo,
// with GTK's playback controls, or by a GtkPicture without them
// (HideControls). Mode is the picture's content fit. Loaded reports the
// natural size once the stream knows it, Ended each end (looping, it
// plays again), Failed the stream's error.
type video struct {
	h        *Host
	n        *node
	stream   Ptr // the GtkMediaFile, the host's reference
	widget   Ptr // the GtkVideo or GtkPicture (n.inner)
	handlers []webHandler
	mode     ops.ImageMode
	hide     bool
	autoPlay bool
	loop     bool
	muted    bool
	sized    bool
	play     bool // play once prepared
}

func init() {
	createHooks = append(createHooks, func(h *Host, n *node) bool {
		if n.kind != ops.KindVideo {
			return false
		}
		vd := &video{h: h, n: n}
		n.ext = vd
		vd.makeWidget(h)
		return true
	})
	propHooks = append(propHooks, func(h *Host, n *node, p ops.PropID, v *ops.Value) bool {
		vd, ok := n.ext.(*video)
		if !ok || n.kind != ops.KindVideo {
			return false
		}
		vd.setProp(h, p, v)
		return true
	})
	commandHooks = append(commandHooks, func(h *Host, n *node, c ops.CommandID, args []byte) bool {
		vd, ok := n.ext.(*video)
		if !ok {
			return false
		}
		switch c {
		case ops.CmdPlay:
			vd.start()
		case ops.CmdPause:
			vd.play = false
			if vd.stream != 0 {
				MediaStreamPause(vd.stream)
			}
		case ops.CmdSeek:
			r := reader{b: args}
			vd.seek(r.f64())
		default:
			return false
		}
		return true
	})
	releaseHooks = append(releaseHooks, func(h *Host, n *node) {
		if vd, ok := n.ext.(*video); ok {
			vd.setStream(0)
		}
	})
}

// makeWidget makes the view of the video: a GtkVideo, or a GtkPicture
// when the controls are hidden.
func (vd *video) makeWidget(h *Host) {
	var w Ptr
	if vd.hide {
		w = PictureNew()
		PictureSetPaintable(w, vd.stream)
	} else {
		w = VideoNew()
		VideoSetMediaStream(w, vd.stream)
	}
	vd.widget = w
	h.setInner(vd.n, w)
	vd.fit()
}

// fit sets the picture's content fit from Mode (a GtkVideo's own
// picture, inside it).
func (vd *video) fit() {
	fit := int32(ContentFitContain)
	switch vd.mode {
	case ops.ImageModeFill:
		fit = ContentFitCover
	case ops.ImageModeStretch:
		fit = ContentFitFill
	case ops.ImageModeCenter:
		fit = ContentFitScaleDown
	}
	if p := findPicture(vd.widget); p != 0 {
		PictureSetContentFit(p, fit)
	}
}

// findPicture is w if it's a GtkPicture, else the first one in it.
func findPicture(w Ptr) Ptr {
	pictureType := Call(Sym("gtk_picture_get_type"))
	var find func(w Ptr) Ptr
	find = func(w Ptr) Ptr {
		if TypeCheckInstanceIsA(w, pictureType) {
			return w
		}
		for c := WidgetGetFirstChild(w); c != 0; c = WidgetGetNextSibling(c) {
			if p := find(c); p != 0 {
				return p
			}
		}
		return 0
	}
	return find(w)
}

func (vd *video) setProp(h *Host, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropVideoSource:
		vd.setSource(h, v.Str)
	case ops.PropVideoMode:
		vd.mode = ops.ImageMode(v.Int)
		vd.fit()
	case ops.PropVideoHideControls:
		if hide := v.Int != 0; hide != vd.hide {
			vd.hide = hide
			vd.makeWidget(h)
		}
	case ops.PropVideoAutoPlay:
		vd.autoPlay = v.Int != 0
		if vd.autoPlay && vd.stream != 0 {
			vd.start()
		}
	case ops.PropVideoLoop:
		vd.loop = v.Int != 0
	case ops.PropVideoMuted:
		vd.muted = v.Int != 0
		if vd.stream != 0 {
			MediaStreamSetMuted(vd.stream, vd.muted)
		}
	}
}

// setSource plays s: an http(s) URL, a file, or one of the app's assets.
func (vd *video) setSource(h *Host, s string) {
	if s == "" {
		vd.setStream(0)
		return
	}
	var file Ptr
	switch {
	case strings.Contains(s, "://"):
		file = WebGLib.FileNewForURI(s)
	case filepath.IsAbs(s):
		file = WebGLib.FileNewForPath(s)
	default:
		abs, _ := filepath.Abs(filepath.Join(h.assets, filepath.FromSlash(s)))
		file = WebGLib.FileNewForPath(abs)
	}
	stream := MediaFileNewForFile(file)
	ObjectUnref(file)
	vd.setStream(stream)
	ObjectUnref(stream) // setStream took its own
	if vd.autoPlay {
		vd.start()
	}
}

// setStream lets the stream shown go, and shows s (0: none).
func (vd *video) setStream(s Ptr) {
	if old := vd.stream; old != 0 {
		for _, hd := range vd.handlers {
			Disconnect(hd.obj, hd.id)
		}
		vd.handlers = nil
		MediaStreamPause(old)
		vd.stream = 0
		vd.attach()
		ObjectUnref(old)
	}
	vd.sized, vd.play = false, false
	if s == 0 {
		return
	}
	vd.stream = ObjectRef(s)
	n := vd.n
	on := func(signal string, f any) {
		vd.handlers = append(vd.handlers, webHandler{s, Connect(s, signal, f)})
	}
	on("notify::prepared", func(uintptr) {
		vd.sizeKnown()
		if vd.play && MediaStreamIsPrepared(s) && !MediaStreamGetPlaying(s) {
			MediaStreamPlay(s)
		}
	})
	on("invalidate-size", func() { vd.sizeKnown() })
	on("notify::ended", func(uintptr) {
		if !MediaStreamGetEnded(s) {
			return
		}
		vd.h.emit(n, ops.Event{Kind: ops.EventEnded})
		if vd.loop && vd.stream == s {
			MediaStreamSeek(s, 0)
			MediaStreamPlay(s)
		}
	})
	on("notify::error", func(uintptr) {
		if e := MediaStreamGetError(s); e != 0 {
			vd.h.emit(n, ops.Event{Kind: ops.EventFailed, Str: webKitErrorMessage(e)})
		}
	})
	MediaStreamSetMuted(s, vd.muted)
	vd.attach()
}

// attach shows the stream in the widget.
func (vd *video) attach() {
	if vd.widget == 0 {
		return
	}
	if vd.hide {
		PictureSetPaintable(vd.widget, vd.stream)
	} else {
		VideoSetMediaStream(vd.widget, vd.stream)
	}
}

// sizeKnown reports the natural size, once, when the stream knows it.
func (vd *video) sizeKnown() {
	if vd.sized || vd.stream == 0 {
		return
	}
	w, ht := PaintableGetIntrinsicWidth(vd.stream), PaintableGetIntrinsicHeight(vd.stream)
	if w <= 0 || ht <= 0 {
		return
	}
	vd.sized = true
	vd.h.emit(vd.n, ops.Event{Kind: ops.EventLoaded, X: float64(w), Y: float64(ht)})
}

// start plays the video, once it's ready.
func (vd *video) start() {
	vd.play = true
	if s := vd.stream; s != 0 {
		if MediaStreamGetEnded(s) {
			MediaStreamSeek(s, 0)
		}
		MediaStreamPlay(s)
	}
}

func (vd *video) seek(sec float64) {
	if vd.stream != 0 {
		MediaStreamSeek(vd.stream, int64(sec*1e6))
	}
}
