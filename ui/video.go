package ui

import (
	"encoding/binary"
	"math"

	"github.com/egoist/nui/layout"
	"github.com/egoist/nui/ops"
	"github.com/egoist/nui/signal"
)

// VideoProps configures a Video.
type VideoProps struct {
	// Source is an http(s) URL (a file, an HLS stream), or a file in the
	// app's assets.
	Source signal.Accessor[string]
	// Mode is ImageFit (letterboxed, the default) or ImageFill (cropped).
	Mode  ImageMode
	Style Styler
	// HideControls hides the platform's playback controls (play, the
	// timeline, full screen, AirPlay and picture in picture on iOS).
	HideControls bool
	AutoPlay     bool
	Loop         bool
	Muted        bool
	// OnEnd is called when it plays to its end (each time, looping).
	OnEnd   func()
	OnError func(message string)
}

// Video plays a video with the platform's player: AVPlayerViewController,
// Media3's ExoPlayer. It sizes itself as an Image does: until its natural
// size is known it's empty unless given a size (an AspectRatio, a Height),
// then it keeps the video's aspect ratio. Drive it with Play, Pause and
// Seek.
//
// Never inlined: nui build looks for its symbol (cmd/nui iosFeatures).
//
//go:noinline
func Video(p VideoProps) Node {
	e := newElement(ops.KindVideo, true)
	var natural layout.Size
	e.box.SetMeasure(func(c layout.Constraints) layout.Size {
		return imageSize(natural, e.style.AspectRatio, c)
	})
	h := e.handlers()
	h.loaded = func(w, hh float32) {
		if s := (layout.Size{W: w, H: hh}); s != natural {
			natural = s
			e.box.MarkDirty()
		}
	}
	e.listen(ops.EventLoaded)
	if p.OnEnd != nil {
		h.ended = p.OnEnd
		e.listen(ops.EventEnded)
	}
	if p.OnError != nil {
		h.failed = p.OnError
		e.listen(ops.EventFailed)
	}
	b := e.r.batch
	if p.Mode != ImageFit {
		b.SetEnum(e.id, ops.PropVideoMode, int32(p.Mode))
	}
	for _, f := range [...]struct {
		on   bool
		prop ops.PropID
	}{{p.HideControls, ops.PropVideoHideControls}, {p.AutoPlay, ops.PropVideoAutoPlay}, {p.Loop, ops.PropVideoLoop}, {p.Muted, ops.PropVideoMuted}} {
		if f.on {
			b.SetBool(e.id, f.prop, true)
		}
	}
	bind(p.Source, func(v string) { e.r.batch.SetString(e.id, ops.PropVideoSource, assetURL(v)) })
	e.setStyle(p.Style, false)
	return Node{e}
}

func videoCommand(v Node, c ops.CommandID, args []byte) {
	if e := v.e; e != nil && e.kind == ops.KindVideo && !e.dead {
		e.r.batch.Command(e.id, c, args)
	}
}

// Play plays a Video. Main thread.
func Play(video Node) { videoCommand(video, ops.CmdPlay, nil) }

// Pause pauses a Video. Main thread.
func Pause(video Node) { videoCommand(video, ops.CmdPause, nil) }

// Seek moves a Video to seconds from its start. Main thread.
func Seek(video Node, seconds float64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], math.Float64bits(seconds))
	videoCommand(video, ops.CmdSeek, b[:])
}
