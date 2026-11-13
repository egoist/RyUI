//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"math"
	"time"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/sdk/dock on a Mac. The progress is drawn on the Dock tile, under its
// badge: the tile's content view becomes the app's icon with a bar over
// its bottom, a track and a fill. NSProgressIndicator doesn't draw in a
// Dock tile (the tile renders its view offscreen, once per display), so
// the bar is two NSBoxes (NSBoxCustom), which draw themselves: the fill
// in the accent color, red for an error, yellow while paused; an
// indeterminate one a third of the track sliding across, the tile drawn
// again 15 times a second. Attention is NSApp's requestUserAttention:
// (critical bounces until the app is active, informational once), which
// does nothing while the app is active.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["dock"] = map[string]method{
			// args: u8 state (none, normal, indeterminate, paused, error),
			// f64 value.
			"progress": func(args []byte, reply func([]byte, error)) {
				var state byte
				var v float64
				if len(args) >= 9 {
					state = args[0]
					v = math.Float64frombits(binary.LittleEndian.Uint64(args[1:]))
				}
				setDockProgress(state, v)
				reply(nil, nil)
			},
			// args: u8 critical; result: u32 the request (0: none, the
			// app's active).
			"attention": func(args []byte, reply func([]byte, error)) {
				app := Send(Class("NSApplication"), "sharedApplication")
				var id int
				if !SendBool(app, "isActive") {
					kind := uintptr(10) // NSInformationalRequest
					if len(args) > 0 && args[0] == 1 {
						kind = 0 // NSCriticalRequest
					}
					id = SendInt(app, "requestUserAttention:", kind)
				}
				reply(binary.LittleEndian.AppendUint32(nil, uint32(id)), nil)
			},
			"cancelAttention": func(args []byte, reply func([]byte, error)) {
				if len(args) >= 4 {
					app := Send(Class("NSApplication"), "sharedApplication")
					Send(app, "cancelUserAttentionRequest:", uintptr(int(int32(binary.LittleEndian.Uint32(args)))))
				}
				reply(nil, nil)
			},
		}
	})
}

// dockBar is the Dock tile's progress.
var dockBar struct {
	view, icon, track, fill ID
	state                   byte
	value                   float64
	timer                   ID
	timerBlock              objc.Block
	started                 time.Time
}

const (
	progressNone = iota
	progressNormal
	progressIndeterminate
	progressPaused
	progressError
)

func dockTile() ID {
	return Send(Send(Class("NSApplication"), "sharedApplication"), "dockTile")
}

// setDockProgress shows a progress on the Dock tile, or takes it off.
func setDockProgress(state byte, value float64) {
	p := &dockBar
	tile := dockTile()
	if state == progressNone || state > progressError {
		stopDockTimer()
		if p.view != 0 {
			Send(tile, "setContentView:", 0)
			for _, v := range []ID{p.view, p.icon, p.track, p.fill} {
				Release(v)
			}
			p.view, p.icon, p.track, p.fill = 0, 0, 0, 0
			Send(tile, "display")
		}
		p.state = progressNone
		return
	}
	if p.view == 0 {
		frame := Rect{Size: MsgSize(tile, Sel("size"))}
		p.view = MsgInitRect(Send(Class("NSView"), "alloc"), Sel("initWithFrame:"), frame)
		p.icon = MsgInitRect(Send(Class("NSImageView"), "alloc"), Sel("initWithFrame:"), frame)
		Send(p.icon, "setImageScaling:", 3) // NSImageScaleProportionallyUpOrDown
		Send(p.view, "addSubview:", uintptr(p.icon))
		box := func() ID {
			b := MsgInitRect(Send(Class("NSBox"), "alloc"), Sel("initWithFrame:"), Rect{})
			Send(b, "setBoxType:", 4)       // NSBoxCustom
			Send(b, "setTitlePosition:", 0) // NSNoTitle
			Send(p.view, "addSubview:", uintptr(b))
			return b
		}
		p.track, p.fill = box(), box()
		Send(p.track, "setFillColor:", uintptr(MsgColor(Class("NSColor"), Sel("colorWithSRGBRed:green:blue:alpha:"), 0.15, 0.15, 0.15, 0.8)))
		Send(p.track, "setBorderColor:", uintptr(MsgColor(Class("NSColor"), Sel("colorWithSRGBRed:green:blue:alpha:"), 0.7, 0.7, 0.7, 1)))
		MsgSetFloat(p.track, Sel("setBorderWidth:"), 1)
		MsgSetFloat(p.fill, Sel("setBorderWidth:"), 0)
		Send(tile, "setContentView:", uintptr(p.view))
	}
	p.state, p.value = state, min(max(value, 0), 1)
	Send(p.icon, "setImage:", uintptr(Send(Send(Class("NSApplication"), "sharedApplication"), "applicationIconImage")))
	color := "controlAccentColor"
	switch state {
	case progressError:
		color = "systemRedColor"
	case progressPaused:
		color = "systemYellowColor"
	}
	c := Send(Class("NSColor"), color)
	Send(p.fill, "setFillColor:", uintptr(c))
	Send(p.fill, "setBorderColor:", uintptr(c))
	if state == progressIndeterminate {
		if p.timer == 0 {
			p.started = time.Now()
			p.timerBlock = NewBlock(func(_ objc.Block, _ ID) { layoutDockBar(); Send(dockTile(), "display") })
			p.timer = Retain(MsgTimer(Class("NSTimer"), Sel("timerWithTimeInterval:repeats:block:"), 1.0/15, true, ID(p.timerBlock)))
			Send(Send(Class("NSRunLoop"), "mainRunLoop"), "addTimer:forMode:", uintptr(p.timer), uintptr(String("kCFRunLoopCommonModes")))
		}
	} else {
		stopDockTimer()
	}
	layoutDockBar()
	Send(tile, "display")
}

// layoutDockBar places the track and the fill: a bar across the bottom
// of the icon, as Finder's copies show.
func layoutDockBar() {
	p := &dockBar
	if p.view == 0 {
		return
	}
	size := MsgSize(dockTile(), Sel("size"))
	h := math.Round(size.Height * 0.11)
	inset := math.Round(size.Width * 0.1)
	track := Rect{Origin: Point{X: inset, Y: math.Round(size.Height * 0.08)}, Size: Size{Width: size.Width - 2*inset, Height: h}}
	MsgSetRect(p.track, Sel("setFrame:"), track)
	MsgSetFloat(p.track, Sel("setCornerRadius:"), h/2)
	MsgSetFloat(p.fill, Sel("setCornerRadius:"), h/2)
	fill := track
	if p.state == progressIndeterminate {
		// A third of the track, sliding across once a second and a half.
		w := track.Size.Width / 3
		t := math.Mod(time.Since(p.started).Seconds()/1.5, 1)
		x := track.Origin.X - w + t*(track.Size.Width+w)
		left, right := max(x, track.Origin.X), min(x+w, track.Origin.X+track.Size.Width)
		fill.Origin.X, fill.Size.Width = left, max(right-left, 0)
	} else {
		fill.Size.Width = max(h, track.Size.Width*p.value)
	}
	Send(p.fill, "setHidden:", Bool(fill.Size.Width <= 0 || p.state != progressIndeterminate && p.value <= 0))
	MsgSetRect(p.fill, Sel("setFrame:"), fill)
}

func stopDockTimer() {
	p := &dockBar
	if p.timer != 0 {
		Send(p.timer, "invalidate")
		Release(p.timer)
		p.timer = 0
		p.timerBlock.Release()
	}
}
