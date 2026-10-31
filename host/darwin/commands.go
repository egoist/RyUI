//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"time"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

func (h *Host) command(n *node, c ops.CommandID, args []byte) {
	switch c {
	case ops.CmdFocus:
		v := n.view
		if n.tracking != 0 {
			v = n.tracking
		}
		Send(Send(v, "window"), "makeFirstResponder:", uintptr(v))
	case ops.CmdBlur:
		w := Send(n.view, "window")
		Send(w, "makeFirstResponder:", 0)
	case ops.CmdListUpdate:
		if n.list != nil {
			n.list.update(h, args)
		}
	case ops.CmdPresent:
		h.present(n)
	case ops.CmdHaptic:
		// The trackpad's feedback: alignment for a selection, a level
		// change for the rest.
		pattern := uintptr(2)
		if len(args) > 0 && ops.Haptic(args[0]) == ops.HapticSelection {
			pattern = 1
		}
		p := Send(Class("NSHapticFeedbackManager"), "defaultPerformer")
		Send(p, "performFeedbackPattern:performanceTime:", pattern, 0)
	case ops.CmdAnimateBegin:
		h.beginAnimation(args)
	case ops.CmdAnimateEnd:
		h.endAnimation()
	case ops.CmdOpenURL:
		if u := URL(string(args)); u != 0 {
			Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "openURL:", uintptr(u))
		}
	case ops.CmdCall:
		h.call(n, args)
	case ops.CmdCopy:
		pb := Send(Class("NSPasteboard"), "generalPasteboard")
		Send(pb, "clearContents")
		Send(pb, "setString:forType:", uintptr(String(string(args))), uintptr(Const(AppKit(), "NSPasteboardTypeString")))
	case ops.CmdPlay:
		if n.video != nil {
			n.video.play()
		}
	case ops.CmdPause:
		if n.video != nil {
			n.video.pause()
		}
	case ops.CmdSeek:
		if n.video != nil && len(args) >= 8 {
			n.video.seek(math.Float64frombits(binary.LittleEndian.Uint64(args)))
		}
	case ops.CmdEval:
		if n.web != nil {
			n.web.eval(string(args))
		}
	case ops.CmdWebNavigate:
		if n.web != nil && len(args) > 0 {
			n.web.navigate(args[0])
		}
	case ops.CmdFrames:
		h.setFrames(len(args) > 0 && args[0] != 0)
	case ops.CmdAnnounce:
		h.announce(string(args))
	case ops.CmdShare:
		h.share(n, string(args))
	case ops.CmdScrollTo:
		if n.list != nil && len(args) >= 5 {
			n.list.scrollTo(h, int(binary.LittleEndian.Uint32(args)), args[4] != 0)
		}
	case ops.CmdScrollToOffset:
		if len(args) >= 9 {
			x, y := float64(f32(args, 0)), float64(f32(args, 4))
			h.scrollToOffset(n, x, y, args[8] != 0)
		}
	case ops.CmdAccent:
		if len(args) >= 8 {
			h.setAccent(ops.Color(binary.LittleEndian.Uint64(args)))
		}
	case ops.CmdMenuBar:
		h.setMenuBar(args)
	case ops.CmdWindowOptions:
		h.windowOptions(args)
	case ops.CmdColorScheme:
		if len(args) > 0 {
			h.setColorScheme(ops.ColorScheme(args[0]))
		}
	}
}

// setAccent makes c the app's accent: Accent colors, filled buttons,
// links. AppKit's own controls keep the system's accent, which the user
// picks.
func (h *Host) setAccent(c ops.Color) {
	h.accent = c
	for _, n := range h.nodes {
		switch {
		case n.kind == ops.KindButton:
			h.buttonVariant(n)
		case n.rich != nil:
			n.rich.invalidate(h, n)
		}
	}
}

func (h *Host) setColorScheme(s ops.ColorScheme) {
	app := Send(Class("NSApplication"), "sharedApplication")
	var a ID
	switch s {
	case ops.ColorSchemeLight:
		a = Send(Class("NSAppearance"), "appearanceNamed:", uintptr(String("NSAppearanceNameAqua")))
	case ops.ColorSchemeDark:
		a = Send(Class("NSAppearance"), "appearanceNamed:", uintptr(String("NSAppearanceNameDarkAqua")))
	}
	Send(app, "setAppearance:", uintptr(a))
}

func (h *Host) scrolled(clip ID) {
	h.scrolledList(clip)
	sv := Send(clip, "superview")
	n := h.byView[sv]
	if n == nil {
		return
	}
	b := MsgRect(clip, Sel("bounds"))
	h.emit(n, ops.Event{Kind: ops.EventScrolled, X: b.Origin.X, Y: b.Origin.Y})
}

func (h *Host) scrollToOffset(n *node, x, y float64, animated bool) {
	if n.kind != ops.KindScroll && n.kind != ops.KindList {
		return
	}
	clip := Send(n.view, "contentView")
	doc := Send(n.view, "documentView")
	if doc == 0 {
		return
	}
	if n.list != nil {
		n.list.flush(h)
	}
	ds := MsgRect(doc, Sel("frame")).Size
	cs := MsgRect(clip, Sel("bounds")).Size
	x = min(max(x, 0), max(ds.Width-cs.Width, 0))
	y = min(max(y, 0), max(ds.Height-cs.Height, 0))
	if animated {
		MsgSetPoint(Send(clip, "animator"), Sel("setBoundsOrigin:"), Point{X: x, Y: y})
	} else {
		MsgScrollPoint(clip, Sel("scrollToPoint:"), Point{X: x, Y: y})
	}
	Send(n.view, "reflectScrolledClipView:", uintptr(clip))
}

// setMenu makes the view's menu: a button's pop-up (Tap), a view's
// context menu (LongPress: a secondary click shows it).
func (h *Host) setMenu(n *node) {
	if n.menu != 0 {
		delete(h.byMenu, n.menu)
		Release(n.menu)
		n.menu = 0
	}
	if len(n.menuActions) == 0 || n.view == 0 {
		return
	}
	m := h.makeMenuOf(n.menuActions, "menuItem:")
	n.menu = m
	h.byMenu[m] = n
	if n.kind == ops.KindView && n.menuTrigger == int32(ops.MenuTriggerTap) && !n.listening(ops.EventPress) {
		g := Send(Send(Class("NSClickGestureRecognizer"), "alloc"), "initWithTarget:action:", uintptr(h.target), uintptr(Sel("click:")))
		Send(n.view, "addGestureRecognizer:", uintptr(g))
		Release(g)
	}
}

// makeMenuOf makes an NSMenu (+1) of actions whose items send action to
// the target, tagged with their index.
func (h *Host) makeMenuOf(actions []action, sel string) ID {
	m := menu("")
	Send(m, "setAutoenablesItems:", 0)
	for i, a := range actions {
		if a.role == ops.AlertButtonRoleCancel {
			continue
		}
		it := item(m, a.title, sel, "", 0)
		Send(it, "setTarget:", uintptr(h.target))
		Send(it, "setTag:", uintptr(i))
		if img := symbolImage(a.icon); img != 0 {
			Send(it, "setImage:", uintptr(img))
		}
		if a.role == ops.AlertButtonRoleDestructive {
			attrs := Dict(Const(AppKit(), "NSForegroundColorAttributeName"), Send(Class("NSColor"), "systemRedColor"))
			s := Send(Send(Class("NSAttributedString"), "alloc"), "initWithString:attributes:", uintptr(String(a.title)), uintptr(attrs))
			Send(it, "setAttributedTitle:", uintptr(Autorelease(s)))
		}
	}
	return m
}

func (h *Host) popUp(n *node) {
	b := MsgRect(n.view, Sel("bounds"))
	at := Point{X: 0, Y: b.Size.Height + 4}
	if !SendBool(n.view, "isFlipped") {
		at.Y = -4
	}
	MsgPopUpMenu(n.menu, Sel("popUpMenuPositioningItem:atLocation:inView:"), 0, at, n.view)
}

func (h *Host) menuItem(it ID) {
	n := h.byMenu[Send(it, "menu")]
	if n == nil {
		return
	}
	h.emit(n, ops.Event{Kind: ops.EventAction, Int: int64(SendInt(it, "tag"))})
}

// animation is what AnimateBegin asked for.
type animation struct {
	duration float64
	curve    ops.AnimationCurve
}

func (h *Host) beginAnimation(args []byte) {
	if len(args) < 5 {
		return
	}
	a := animation{duration: float64(f32(args, 0)), curve: ops.AnimationCurve(args[4])}
	if h.env.ReduceMotion {
		a.duration = 0
	}
	h.anim = a
	h.animating = a.duration > 0
	if !h.animating {
		return
	}
	Send(Class("NSAnimationContext"), "beginGrouping")
	ctx := Send(Class("NSAnimationContext"), "currentContext")
	MsgSetFloat(ctx, Sel("setDuration:"), a.duration)
	Send(ctx, "setAllowsImplicitAnimation:", 1)
	name := "easeInEaseOut"
	switch a.curve {
	case ops.AnimationCurveLinear:
		name = "linear"
	case ops.AnimationCurveEaseIn:
		name = "easeIn"
	case ops.AnimationCurveEaseOut, ops.AnimationCurveSpring:
		name = "easeOut"
	}
	tf := Send(Class("CAMediaTimingFunction"), "functionWithName:", uintptr(String(name)))
	Send(ctx, "setTimingFunction:", uintptr(tf))
}

func (h *Host) endAnimation() {
	if h.animating {
		Send(Class("NSAnimationContext"), "endGrouping")
	}
	h.animating = false
}

// withoutAnimation runs f with implicit animations off. An animated batch
// turns them on, and a view made in it would grow out of its zero frame
// into its first one (a label ellipsizing its text as it does).
func withoutAnimation(f func()) {
	ctx := Class("NSAnimationContext")
	Send(ctx, "beginGrouping")
	cur := Send(ctx, "currentContext")
	MsgSetFloat(cur, Sel("setDuration:"), 0)
	Send(cur, "setAllowsImplicitAnimation:", 0)
	f()
	Send(ctx, "endGrouping")
}

// frameClock sends Frame at each display refresh while it's on.
type frameClock struct {
	timer ID
	start time.Time
}

func (h *Host) setFrames(on bool) {
	if on == (h.frames != nil) {
		return
	}
	if !on {
		Send(h.frames.timer, "invalidate")
		Release(h.frames.timer)
		h.frames = nil
		return
	}
	f := &frameClock{start: time.Now()}
	// A display link when the system has view display links (14), else a
	// timer at 60 Hz.
	if RespondsTo(h.content, "displayLinkWithTarget:selector:") {
		f.timer = Retain(Send(h.content, "displayLinkWithTarget:selector:", uintptr(h.target), uintptr(Sel("frameTick:"))))
		Send(f.timer, "addToRunLoop:forMode:", uintptr(Send(Class("NSRunLoop"), "mainRunLoop")), uintptr(String("kCFRunLoopCommonModes")))
	} else {
		f.timer = Retain(Send(Send(Class("NSTimer"), "alloc"), "initWithFireDate:interval:target:selector:userInfo:repeats:",
			uintptr(Send(Class("NSDate"), "date")), 0, uintptr(h.target), uintptr(Sel("frameTick:")), 0, 1))
		MsgSetFloat(f.timer, Sel("setTolerance:"), 0)
		Send(Send(Class("NSRunLoop"), "mainRunLoop"), "addTimer:forMode:", uintptr(f.timer), uintptr(String("kCFRunLoopCommonModes")))
	}
	h.frames = f
}

var monoStart = time.Now()

func (h *Host) tick() {
	if h.frames == nil || h.root == nil {
		return
	}
	h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventFrame, Float: time.Since(monoStart).Seconds()})
}

func (h *Host) announce(s string) {
	post := announcer()
	if post == nil {
		return
	}
	info := Dict(
		String("AXAnnouncementKey"), String(s),
		String("AXPriorityKey"), NumberInt(90), // high
	)
	post(Send(Class("NSApplication"), "sharedApplication"), String("AXAnnouncementRequested"), info)
}

func (h *Host) share(n *node, s string) {
	var item ID
	if u := URL(s); u != 0 && strings.Contains(s, "://") {
		item = u
	} else {
		item = String(s)
	}
	picker := Send(Send(Class("NSSharingServicePicker"), "alloc"), "initWithItems:", uintptr(Array(item)))
	Autorelease(picker)
	v := h.content
	b := MsgRect(v, Sel("bounds"))
	r := Rect{Origin: Point{X: b.Size.Width / 2, Y: 0}, Size: Size{Width: 1, Height: 1}}
	msgShowPicker(picker, Sel("showRelativeToRect:ofView:preferredEdge:"), r, v, 1)
}

// call runs a module's method (ui.Call) and answers with Result.
func (h *Host) call(root *node, args []byte) {
	if len(args) < 4 {
		return
	}
	callID := binary.LittleEndian.Uint32(args)
	module, rest := readString(args[4:])
	method, rest := readString(rest)
	id := root.id
	answer := func(res []byte, err error) {
		out := binary.LittleEndian.AppendUint32(nil, callID)
		if err != nil {
			out = append(out, 0)
			out = append(out, err.Error()...)
		} else {
			out = append(out, 1)
			out = append(out, res...)
		}
		ev := ops.Event{ID: id, Kind: ops.EventResult, Bytes: out}
		// Answered after the batch, as a host's modules do.
		h.Post(func() { h.core.Dispatch(ev) })
	}
	m := h.module(module)[method]
	if m == nil {
		answer(nil, errors.New("ryui: "+module+"."+method+" isn't on macOS yet"))
		return
	}
	m(rest, answer)
}
