//go:build darwin && !ios

package darwin

import (
	"path/filepath"
	"strings"

	"github.com/ebitengine/purego"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

// A Video is an AVPlayerView (AVKit's controls) playing an AVPlayer.
type video struct {
	view     ID
	player   ID
	item     ID
	autoPlay bool
	loop     bool
	muted    bool
	sized    bool
}

var avLoaded bool

var msgSeek func(obj ID, sel SEL, t cmTime)

type cmTime struct {
	Value     int64
	Timescale int32
	Flags     uint32
	Epoch     int64
}

func (h *Host) makeVideo(n *node) ID {
	if !avLoaded {
		avLoaded = true
		Dlopen("/System/Library/Frameworks/AVFoundation.framework/AVFoundation")
		Dlopen("/System/Library/Frameworks/AVKit.framework/AVKit")
		purego.RegisterFunc(&msgSeek, MsgSendAddr())
	}
	v := initView("AVPlayerView")
	Send(v, "setControlsStyle:", 2) // floating
	p := New("AVPlayer")
	Send(v, "setPlayer:", uintptr(p))
	n.video = &video{view: v, player: p}
	return v
}

func (vd *video) setProp(h *Host, n *node, p ops.PropID, v *ops.Value) {
	switch p {
	case ops.PropVideoSource:
		vd.setSource(h, n, v.Str)
	case ops.PropVideoMode:
		g := "AVLayerVideoGravityResizeAspect"
		if ops.ImageMode(v.Int) == ops.ImageModeFill {
			g = "AVLayerVideoGravityResizeAspectFill"
		}
		if RespondsTo(vd.view, "setVideoGravity:") {
			Send(vd.view, "setVideoGravity:", uintptr(String(g)))
		}
	case ops.PropVideoHideControls:
		style := uintptr(2)
		if v.Int != 0 {
			style = 0 // none
		}
		Send(vd.view, "setControlsStyle:", style)
	case ops.PropVideoAutoPlay:
		vd.autoPlay = v.Int != 0
		if vd.autoPlay && vd.item != 0 {
			vd.play()
		}
	case ops.PropVideoLoop:
		vd.loop = v.Int != 0
	case ops.PropVideoMuted:
		vd.muted = v.Int != 0
		Send(vd.player, "setMuted:", Bool(vd.muted))
	}
}

func (vd *video) setSource(h *Host, n *node, s string) {
	center := Send(Class("NSNotificationCenter"), "defaultCenter")
	if vd.item != 0 {
		Send(center, "removeObserver:name:object:", uintptr(h.target), 0, uintptr(vd.item))
		Send(vd.item, "removeObserver:forKeyPath:", uintptr(h.target), uintptr(String("presentationSize")))
		Release(vd.item)
		vd.item = 0
	}
	if s == "" {
		Send(vd.player, "replaceCurrentItemWithPlayerItem:", 0)
		return
	}
	var u ID
	switch {
	case strings.Contains(s, "://"):
		u = URL(s)
	case filepath.IsAbs(s):
		u = FileURL(s)
	default:
		abs, _ := filepath.Abs(filepath.Join(h.assets, filepath.FromSlash(s)))
		u = FileURL(abs)
	}
	item := Retain(Send(Class("AVPlayerItem"), "playerItemWithURL:", uintptr(u)))
	vd.item = item
	vd.sized = false
	h.videos[item] = n.id
	Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("videoEnded:")),
		uintptr(String("AVPlayerItemDidPlayToEndTimeNotification")), uintptr(item))
	Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("videoFailed:")),
		uintptr(String("AVPlayerItemFailedToPlayToEndTimeNotification")), uintptr(item))
	Send(item, "addObserver:forKeyPath:options:context:", uintptr(h.target), uintptr(String("presentationSize")), 1, 1)
	Send(vd.player, "replaceCurrentItemWithPlayerItem:", uintptr(item))
	Send(vd.player, "setMuted:", Bool(vd.muted))
	if vd.autoPlay {
		vd.play()
	}
}

func (vd *video) play()  { Send(vd.player, "play") }
func (vd *video) pause() { Send(vd.player, "pause") }

func (vd *video) seek(sec float64) {
	msgSeek(vd.player, Sel("seekToTime:"), cmTime{Value: int64(sec * 600), Timescale: 600, Flags: 1})
}

func (vd *video) close() {
	Send(vd.player, "pause")
	if vd.item != 0 {
		center := Send(Class("NSNotificationCenter"), "defaultCenter")
		Send(center, "removeObserver:name:object:", uintptr(theHost.target), 0, uintptr(vd.item))
		Send(vd.item, "removeObserver:forKeyPath:", uintptr(theHost.target), uintptr(String("presentationSize")))
		delete(theHost.videos, vd.item)
		Release(vd.item)
		vd.item = 0
	}
	Send(vd.view, "setPlayer:", 0)
	Release(vd.player)
}

// videoSized reports a video's natural size once AVFoundation knows it.
func (h *Host) videoSized(item ID) {
	n := h.nodes[h.videos[item]]
	if n == nil || n.video == nil || n.video.sized {
		return
	}
	s := MsgSize(item, Sel("presentationSize"))
	if s.Width <= 0 || s.Height <= 0 {
		return
	}
	n.video.sized = true
	h.emit(n, ops.Event{Kind: ops.EventLoaded, X: s.Width, Y: s.Height})
}

func (h *Host) videoEnded(item ID) {
	n := h.nodes[h.videos[item]]
	if n == nil || n.video == nil {
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventEnded})
	if n.video.loop {
		n.video.seek(0)
		n.video.play()
	}
}

func (h *Host) videoFailed(item ID) {
	n := h.nodes[h.videos[item]]
	if n == nil {
		return
	}
	msg := "can't play"
	if err := Send(item, "error"); err != 0 {
		msg = GoString(Send(err, "localizedDescription"))
	}
	h.emit(n, ops.Event{Kind: ops.EventFailed, Str: msg})
}
