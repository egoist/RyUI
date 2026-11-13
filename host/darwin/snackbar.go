//go:build darwin && !ios

package darwin

import (
	"math"
	"time"

	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/ops"
)

// A Snackbar: AppKit has none, so it's an overlay at the bottom of the
// window it's on, in the system's material (an NSVisualEffectView,
// rounded, with a shadow): its message, its action's button and a close
// button. It fades and rises in, and out (a fade with Reduce Motion);
// its time stops while the mouse is over it; VoiceOver announces it
// (NSAccessibility's announcement) and reaches its buttons.
type snack struct {
	box    ID // AsukaSnackbar: the shadow, placed in the window's content
	effect ID // NSVisualEffectView: the material, rounded
	label  ID // NSTextField: the message, wrapping
	button ID // NSButton: its action (0: none)
	close  ID // NSButton: closes it
	on     ID // the content view it's in

	duration ops.SnackbarDuration
	timer    ID // NSTimer
	block    objc.Block
	left     time.Duration // of its time, while it's paused
	started  time.Time
	hovered  bool
	done     bool // reported (its action, a timeout, closing) or destroyed
	leaving  bool
}

const (
	snackMargin  = 20  // from the window's edges
	snackMaxW    = 480 // at most this wide, as a toast reads
	snackPadX    = 14
	snackPadY    = 8
	snackGap     = 8
	snackRadius  = 10
	snackMinH    = 36
	snackCloseSz = 20
)

var snackClassDone bool

// registerSnackClass makes AsukaSnackbar: a flipped view whose tracking
// area pauses the snackbar's time under the mouse, and which VoiceOver's
// cancel (escape) closes.
func registerSnackClass() {
	if snackClassDone {
		return
	}
	snackClassDone = true
	DefineClass("AsukaSnackbar", "NSView", nil, []objc.MethodDef{
		Method("isFlipped", func(self ID, _ SEL) bool { return true }),
		Method("mouseEntered:", func(self ID, _ SEL, ev ID) { theHost.snackHover(self, true) }),
		Method("mouseExited:", func(self ID, _ SEL, ev ID) { theHost.snackHover(self, false) }),
		Method("accessibilityPerformCancel", func(self ID, _ SEL) bool {
			if n := theHost.snackOf(self); n != nil {
				theHost.snackReport(n, ops.EventDismissed)
				return true
			}
			return false
		}),
	})
}

// snackOf is the snackbar node whose view (or button) v is.
func (h *Host) snackOf(v ID) *node {
	n := h.byView[v]
	if n == nil || n.kind != ops.KindSnackbar || n.snack == nil {
		return nil
	}
	return n
}

func (h *Host) setSnackProp(n *node, p ops.PropID, v *ops.Value) {
	s := n.snack
	switch p {
	case ops.PropSnackbarMessage:
		n.message = v.Str
		if s.label != 0 {
			Send(s.label, "setStringValue:", uintptr(String(v.Str)))
			Send(s.box, "setAccessibilityLabel:", uintptr(String(v.Str)))
			h.placeSnack(n)
		}
	case ops.PropSnackbarAction:
		n.title = v.Str
		if s.box != 0 {
			h.snackButton(n)
			h.placeSnack(n)
		}
	case ops.PropSnackbarDuration:
		s.duration = ops.SnackbarDuration(v.Int)
	case ops.PropSnackbarWindow:
		n.nav.on = ops.NodeID(v.Int)
	}
}

// presentSnack shows n at the bottom of its window's content.
func (h *Host) presentSnack(n *node) {
	s := n.snack
	w := h.onWin(n)
	if s.box != 0 || s.done || w == nil || w.content == 0 {
		return
	}
	registerSnackClass()
	s.on = w.content
	s.box = initView("AsukaSnackbar")
	Send(s.box, "setWantsLayer:", 1)
	layer := Send(s.box, "layer")
	MsgSetFloat(layer, Sel("setShadowOpacity:"), 0.12)
	MsgSetFloat(layer, Sel("setShadowRadius:"), 8)
	MsgSetSize(layer, Sel("setShadowOffset:"), Size{Height: -3})
	Send(s.box, "setAccessibilityElement:", 1)
	Send(s.box, "setAccessibilityRole:", uintptr(String("AXGroup")))
	Send(s.box, "setAccessibilityLabel:", uintptr(String(n.message)))
	h.byView[s.box] = n

	s.effect = initView("NSVisualEffectView")
	Send(s.effect, "setMaterial:", 6)     // popover: a light or dark panel over the content
	Send(s.effect, "setBlendingMode:", 1) // within the window: it blurs the content
	Send(s.effect, "setState:", 1)        // active, as the window isn't always
	Send(s.effect, "setWantsLayer:", 1)
	el := Send(s.effect, "layer")
	MsgSetFloat(el, Sel("setCornerRadius:"), snackRadius)
	Send(el, "setCornerCurve:", uintptr(String("continuous")))
	Send(el, "setMasksToBounds:", 1)
	MsgSetFloat(el, Sel("setBorderWidth:"), 0.5)
	Send(s.box, "addSubview:", uintptr(s.effect))
	Release(s.effect)

	s.label = Send(Class("NSTextField"), "wrappingLabelWithString:", uintptr(String(n.message)))
	Send(s.label, "setSelectable:", 0)
	Send(s.label, "setFont:", uintptr(MsgFloatID(Class("NSFont"), Sel("systemFontOfSize:"), 0))) // the system size
	Send(s.label, "setTextColor:", uintptr(Send(Class("NSColor"), "labelColor")))
	Send(s.effect, "addSubview:", uintptr(s.label))

	img := Send(Class("NSImage"), "imageWithSystemSymbolName:accessibilityDescription:",
		uintptr(String("xmark")), uintptr(String(l10n.Close(h.locale))))
	s.close = Send(Class("NSButton"), "buttonWithImage:target:action:", uintptr(img), uintptr(h.target), uintptr(Sel("act:")))
	Send(s.close, "setBordered:", 0)
	Send(s.close, "setImageScaling:", 0) // proportionally down
	Send(s.close, "setContentTintColor:", uintptr(Send(Class("NSColor"), "secondaryLabelColor")))
	Send(s.close, "setToolTip:", uintptr(String(l10n.Close(h.locale))))
	Send(s.effect, "addSubview:", uintptr(s.close))
	h.byView[s.close] = n
	h.snackButton(n)
	h.snackBorder(n)

	Send(s.on, "addSubview:", uintptr(s.box))
	h.placeSnack(n)
	area := MsgInitTracking(Send(Class("NSTrackingArea"), "alloc"), Sel("initWithRect:options:owner:userInfo:"), Rect{},
		uint(nsTrackingMouseEnteredAndExited|nsTrackingActiveInActiveApp|nsTrackingInVisibleRect), s.box, 0)
	Send(s.box, "addTrackingArea:", uintptr(area))
	Release(area)

	// In: rising a little as it fades in; a fade with Reduce Motion.
	f := MsgRect(s.box, Sel("frame"))
	MsgSetFloat(s.box, Sel("setAlphaValue:"), 0)
	if !h.reduceMotion() {
		g := f
		g.Origin.Y += 16
		MsgSetRect(s.box, selSetFrame, g)
	}
	box := s.box
	animate(0.25, func() {
		a := Send(box, "animator")
		MsgSetFloat(a, Sel("setAlphaValue:"), 1)
		MsgSetRect(a, selSetFrame, f)
	}, func() {})

	text := n.message
	if n.title != "" {
		text += ", " + n.title
	}
	h.announce(text)
	h.startSnack(n)
}

// snackButton makes, retitles or removes its action's button.
func (h *Host) snackButton(n *node) {
	s := n.snack
	if n.title == "" {
		if s.button != 0 {
			delete(h.byView, s.button)
			Send(s.button, "removeFromSuperview")
			s.button = 0
		}
		return
	}
	if s.button == 0 {
		s.button = Send(Class("NSButton"), "buttonWithTitle:target:action:", uintptr(String(n.title)), uintptr(h.target), uintptr(Sel("act:")))
		Send(s.button, "setBezelStyle:", 1) // push
		Send(s.effect, "addSubview:", uintptr(s.button))
		h.byView[s.button] = n
		return
	}
	Send(s.button, "setTitle:", uintptr(String(n.title)))
}

// snackBorder sets its hairline in the separator's color, for the
// appearance it's in (a layer's CGColor doesn't follow it).
func (h *Host) snackBorder(n *node) {
	s := n.snack
	if s.effect == 0 {
		return
	}
	WithPool(func() {
		h.withAppearance(func() {
			Send(Send(s.effect, "layer"), "setBorderColor:", uintptr(Send(Send(Class("NSColor"), "separatorColor"), "CGColor")))
		})
	})
}

// placeSnack lays it out: as wide as its message needs, up to
// snackMaxW and the window's width, centered at the bottom; the
// message, then its button and the close button (mirrored right to
// left).
func (h *Host) placeSnack(n *node) {
	s := n.snack
	if s == nil || s.box == 0 || s.on == 0 {
		return
	}
	cb := MsgRect(s.on, Sel("bounds"))
	maxW := math.Min(snackMaxW, cb.Size.Width-2*snackMargin)
	var bw, bh float64
	if s.button != 0 {
		bs := MsgSize(s.button, Sel("fittingSize"))
		bw, bh = math.Ceil(bs.Width), math.Ceil(bs.Height)
	}
	fixed := float64(2*snackPadX + snackGap + snackCloseSz)
	if bw > 0 {
		fixed += bw + snackGap
	}
	lw := math.Max(maxW-fixed, 60)
	MsgSetFloat(s.label, Sel("setPreferredMaxLayoutWidth:"), lw)
	// The cell's size wrapped at lw: a label's fittingSize wraps at its
	// frame's width, which it hasn't yet.
	ls := MsgRectToSize(Send(s.label, "cell"), Sel("cellSizeForBounds:"), Rect{Size: Size{Width: lw, Height: 1e6}})
	lw = math.Min(math.Ceil(ls.Width), lw)
	lh := math.Ceil(ls.Height)
	height := math.Max(snackMinH, math.Max(lh, math.Max(bh, snackCloseSz))+2*snackPadY)
	width := lw + fixed
	x := math.Round((cb.Size.Width - width) / 2)
	y := cb.Size.Height - snackMargin - height
	if !SendBool(s.on, "isFlipped") {
		y = snackMargin
	}
	f := Rect{Origin: Point{X: x, Y: y}, Size: Size{Width: width, Height: height}}
	if s.leaving {
		return
	}
	MsgSetRect(s.box, selSetFrame, f)
	MsgSetRect(s.effect, selSetFrame, Rect{Size: f.Size})
	// The shadow follows the rounded card, not what's drawn in it.
	loadCG()
	sp := cgPathCreateMutable()
	cgPathRoundedRect(sp, 0, Rect{Size: f.Size}, snackRadius, snackRadius)
	Send(Send(s.box, "layer"), "setShadowPath:", sp)
	cgPathRelease(sp)
	// Within the effect view (not flipped), each centered on its height.
	mid := func(h float64) float64 { return math.Round((height - h) / 2) }
	place := func(v ID, x, w, hh float64) {
		if h.rtl {
			x = width - x - w
		}
		MsgSetRect(v, selSetFrame, Rect{Origin: Point{X: x, Y: mid(hh)}, Size: Size{Width: w, Height: hh}})
	}
	place(s.label, snackPadX, lw, lh)
	at := snackPadX + lw + snackGap
	if s.button != 0 {
		place(s.button, at, bw, bh)
		at += bw + snackGap
	}
	place(s.close, at, snackCloseSz, snackCloseSz)
	Send(s.on, "addSubview:positioned:relativeTo:", uintptr(s.box), 1, 0) // NSWindowAbove: over what the core adds
}

// placeSnacks lays out the snackbars showing: a window resized.
func (h *Host) placeSnacks() {
	for _, n := range h.nodes {
		if n.kind == ops.KindSnackbar && n.snack != nil && n.snack.box != 0 {
			h.placeSnack(n)
			h.snackBorder(n)
		}
	}
}

// snackPressed is a press of its action's button or its close button.
func (h *Host) snackPressed(n *node, sender ID) {
	if sender == n.snack.close {
		h.snackReport(n, ops.EventDismissed)
		return
	}
	h.snackReport(n, ops.EventAction)
}

// snackReport tells the core it went (its action, its time running out,
// closed), and takes it out; the core destroys it.
func (h *Host) snackReport(n *node, kind ops.EventKind) {
	s := n.snack
	if s.done {
		return
	}
	s.done = true
	h.stopSnackTimer(n)
	h.leaveSnack(n)
	h.emit(n, ops.Event{Kind: kind}) // Action's Int: 0, its one action
}

// startSnack starts its time: four seconds, ten for SnackbarLong. With
// VoiceOver on, one with a button stays until it's closed, and one
// without stays long, as Material does without a recommended timeout.
func (h *Host) startSnack(n *node) {
	s := n.snack
	if s.duration == ops.SnackbarDurationIndefinite {
		return
	}
	d := 4 * time.Second
	if s.duration == ops.SnackbarDurationLong {
		d = 10 * time.Second
	}
	if SendBool(Send(Class("NSWorkspace"), "sharedWorkspace"), "isVoiceOverEnabled") {
		if n.title != "" {
			return
		}
		d = max(d, 10*time.Second)
	}
	s.left = d
	if !s.hovered {
		h.resumeSnack(n)
	}
}

func (h *Host) resumeSnack(n *node) {
	s := n.snack
	if s.timer != 0 || s.left <= 0 || s.done {
		return
	}
	s.started = time.Now()
	id := n.id
	s.block = NewBlock(func(_ objc.Block, _ ID) {
		// After the timer's call: reporting lets the block go.
		h.Post(func() {
			if n := h.nodes[id]; n != nil && n.snack != nil {
				h.snackReport(n, ops.EventDismissed)
			}
		})
	})
	s.timer = Retain(MsgTimer(Class("NSTimer"), Sel("timerWithTimeInterval:repeats:block:"), s.left.Seconds(), false, ID(s.block)))
	Send(Send(Class("NSRunLoop"), "mainRunLoop"), "addTimer:forMode:", uintptr(s.timer), uintptr(String("kCFRunLoopCommonModes")))
}

func (h *Host) stopSnackTimer(n *node) {
	s := n.snack
	if s.timer != 0 {
		Send(s.timer, "invalidate")
		Release(s.timer)
		s.timer = 0
		s.left = max(time.Second, s.left-time.Since(s.started))
	}
	if s.block != 0 {
		s.block.Release()
		s.block = 0
	}
}

// snackHover pauses its time while the mouse is over it.
func (h *Host) snackHover(box ID, on bool) {
	n := h.snackOf(box)
	if n == nil || n.snack.hovered == on {
		return
	}
	n.snack.hovered = on
	if on {
		h.stopSnackTimer(n)
	} else {
		h.resumeSnack(n)
	}
}

// leaveSnack fades it out (sinking a little), then lets it go.
func (h *Host) leaveSnack(n *node) {
	s := n.snack
	if s.leaving {
		return
	}
	s.leaving = true
	if s.box == 0 {
		return
	}
	box, button, closeB := s.box, s.button, s.close
	for _, v := range []ID{box, button, closeB} {
		if v != 0 && h.byView[v] == n {
			delete(h.byView, v)
		}
	}
	Send(box, "setAccessibilityElement:", 0)
	f := MsgRect(box, Sel("frame"))
	if !h.reduceMotion() {
		f.Origin.Y += 12
	}
	animate(0.2, func() {
		a := Send(box, "animator")
		MsgSetFloat(a, Sel("setAlphaValue:"), 0)
		MsgSetRect(a, selSetFrame, f)
	}, func() {
		Send(box, "removeFromSuperview")
		Release(box)
	})
	s.box = 0
}

// destroySnack is the core's Destroy: out, animated.
func (h *Host) destroySnack(n *node) {
	n.snack.done = true
	h.stopSnackTimer(n)
	h.leaveSnack(n)
}

func (h *Host) reduceMotion() bool {
	return SendBool(Send(Class("NSWorkspace"), "sharedWorkspace"), "accessibilityDisplayShouldReduceMotion")
}
