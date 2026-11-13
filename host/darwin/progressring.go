//go:build darwin && !ios

package darwin

import (
	"math"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// A ProgressRing is an AsukaRing view. At AppKit's own sizes, 16 points
// (small) and 32 (regular), in the system's colors, it holds AppKit's
// NSProgressIndicator in its spinning style: its circle, determinate,
// and its spinner without a value. AppKit draws those at its control
// sizes only (a larger frame centers the regular one; scaled, its circle
// comes apart and its spinner, a bitmap, blurs) and in the accent color
// only, so at any other size, or in the Style's Color, the ring draws
// what AppKit draws, as large as its frame's shorter side, with Core
// Animation layers: the circle's track and arc, and the spinner's
// spokes. Their colors are read from what AppKit draws (an unseen
// indicator of its own), so they follow the accent, the appearance and
// whether the window is active as AppKit's do.
//
// The AsukaRing is the accessibility element either way, read as
// AppKit's indicator is: a progress indicator with its value, from 0 to
// 1 (VoiceOver reads its percentage), or, without a value, a busy
// indicator.
type progressRing struct {
	value         float64
	indeterminate bool
	native        ID // AppKit's NSProgressIndicator, when it draws the ring
	track, arc    ID // the drawn circle: CAShapeLayers
	spinner       ID // the drawn spinner: a CAReplicatorLayer of spokes
	spoke         ID // its spoke, a CAShapeLayer
	ref           ID // AppKit's indicator, unseen, drawn to read its colors
	look          ringLook
}

// ringLook is AppKit's colors, as read from its indicator: the arc, and
// the arc of a full circle (in dark mode a lighter blue), the track and
// the spinner's leading spoke, sRGB with alpha.
type ringLook struct {
	arc, full, track, spoke [4]float64
	ok                      bool // read; else read again
}

// The sizes AppKit draws its spinning NSProgressIndicator at: its
// regular control size's (the default) and its small one's.
const (
	ringRegular = 32
	ringSmall   = 16
)

// ringStroke is the width of the circle's stroke at a size. AppKit's
// circle is a 5-point stroke at both its sizes, its outer edge the
// frame's: at 32 points about a sixth of the ring, at 16 nearly a third.
// The ring keeps the regular circle's proportions above 32 points and
// the small one's below 16, and the 5 points between.
func ringStroke(side float64) float64 {
	switch {
	case side >= ringRegular:
		return side * 5 / ringRegular
	case side <= ringSmall:
		return side * 5 / ringSmall
	}
	return 5
}

// AppKit's spinner, measured: 8 spokes with round ends, from 3/16 of its
// size out to its edge, an eighth of its size wide (4 points at 32, 2 at
// 16). It turns clockwise in 24 steps a turn, 0.8 seconds: the leading
// spoke darkest, each behind it an eighth lighter.
const (
	spinnerSpokes = 8
	spinnerSteps  = 24
	spinnerTurn   = 0.8
)

// What AppKit draws, measured (sRGB, the system's blue accent): the
// colors used until they're read from its own indicator (a view not in a
// window yet).
var (
	ringArcLight      = [4]float64{10.0 / 255, 130.0 / 255, 1, 1}
	ringArcDark       = [4]float64{23.0 / 255, 105.0 / 255, 230.0 / 255, 1}
	ringFullDark      = [4]float64{23.0 / 255, 123.0 / 255, 229.0 / 255, 1}
	ringInactiveLight = [4]float64{185.0 / 255, 185.0 / 255, 185.0 / 255, 1}
	ringInactiveDark  = [4]float64{1, 1, 1, 83.0 / 255}
	ringTrackLight    = [4]float64{0, 0, 0, 13.0 / 255}
	ringTrackDark     = [4]float64{1, 1, 1, 26.0 / 255}
	ringSpokeLight    = [4]float64{0, 0, 0, 140.0 / 255}
	ringSpokeDark     = [4]float64{1, 1, 1, 140.0 / 255}
)

// ringValueDuration is how long AppKit's animator takes to move an
// NSProgressIndicator's value (NSAnimationContext's default; linear).
const ringValueDuration = 0.25

func registerRingClass(flipped objc.MethodDef) {
	// What VoiceOver reads: set with NSAccessibility's setters, and
	// answered through its attributes too, as AppKit's indicator answers
	// (an NSView's own leave its role unknown and its value out).
	DefineClass("AsukaRing", "NSView", nil, []objc.MethodDef{
		flipped,
		Method("accessibilityAttributeValue:", func(self ID, _ SEL, attr ID) ID {
			if n := theHost.byView[self]; n != nil && n.ring != nil {
				if v := n.ring.attribute(GoString(attr)); v != 0 {
					return v
				}
			}
			return SendSuper(self, "AsukaRing", "accessibilityAttributeValue:", uintptr(attr))
		}),
		Method("accessibilityAttributeNames", func(self ID, _ SEL) ID {
			names := Send(Send(SendSuper(self, "AsukaRing", "accessibilityAttributeNames"), "mutableCopy"), "autorelease")
			if n := theHost.byView[self]; n != nil && n.ring != nil {
				for _, a := range []string{"AXValue", "AXMinValue", "AXMaxValue"} {
					if n.ring.attribute(a) != 0 && !SendBool(names, "containsObject:", uintptr(String(a))) {
						Send(names, "addObject:", uintptr(String(a)))
					}
				}
			}
			return names
		}),
		Method("viewDidChangeEffectiveAppearance", func(self ID, _ SEL) {
			SendSuper(self, "AsukaRing", "viewDidChangeEffectiveAppearance")
			if n := theHost.byView[self]; n != nil && n.ring != nil {
				n.ring.restyle(theHost, n)
			}
		}),
		Method("viewDidMoveToWindow", func(self ID, _ SEL) {
			SendSuper(self, "AsukaRing", "viewDidMoveToWindow")
			if n := theHost.byView[self]; n != nil && Send(self, "window") != 0 {
				theHost.reassertShadow(n) // as keepsShadow's
				if n.ring != nil {
					n.ring.restyle(theHost, n) // the window's state
				}
			}
		}),
	})
	// The drawn rings' colors follow their windows' active state and the
	// system's accent. AppKit redraws its own as the window's state
	// changes, after the notification: they're read on the next turn of
	// the run loop.
	targetMethods = append(targetMethods,
		Method("ringsChanged:", func(self ID, _ SEL, note ID) {
			Send(Class("NSObject"), "cancelPreviousPerformRequestsWithTarget:selector:object:", uintptr(self), uintptr(Sel("restyleRings")), 0)
			msgPerformAfter(self, Sel("performSelector:withObject:afterDelay:"), Sel("restyleRings"), 0, 0)
		}),
		Method("restyleRings", func(self ID, _ SEL) {
			if h := theHost; h != nil {
				h.restyleRings()
			}
		}),
	)
}

// restyleRings reads AppKit's colors again for every drawn ring.
func (h *Host) restyleRings() {
	WithPool(func() {
		for _, n := range h.nodes {
			if n.ring != nil {
				n.ring.restyle(h, n)
			}
		}
	})
}

var msgPerformAfter func(obj ID, sel SEL, action SEL, arg ID, delay float64)

func (h *Host) makeRing(n *node) ID {
	if !h.ringsObserved {
		h.ringsObserved = true
		if msgPerformAfter == nil {
			purego.RegisterFunc(&msgPerformAfter, MsgSendAddr())
		}
		center := Send(Class("NSNotificationCenter"), "defaultCenter")
		for _, name := range []string{
			"NSWindowDidBecomeKeyNotification", "NSWindowDidResignKeyNotification",
			"NSWindowDidBecomeMainNotification", "NSWindowDidResignMainNotification",
			"NSApplicationDidBecomeActiveNotification", "NSApplicationDidResignActiveNotification",
			"NSSystemColorsDidChangeNotification",
		} {
			Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("ringsChanged:")), uintptr(String(name)), 0)
		}
	}
	n.ring = &progressRing{}
	v := initView("AsukaRing")
	Send(v, "setWantsLayer:", 1)
	Send(v, "setAccessibilityElement:", 1)
	n.ring.accessibility(v)
	return v
}

func (h *Host) setRing(n *node, p ops.PropID, v *ops.Value) {
	r := n.ring
	switch p {
	case ops.PropProgressRingValue:
		r.setValue(h, n, v.Float)
	case ops.PropProgressRingIndeterminate:
		if r.indeterminate == (v.Int != 0) {
			return
		}
		r.indeterminate = v.Int != 0
		r.look.ok = false // the spinner's colors are read from AppKit's spinner
		if r.native != 0 {
			Send(r.native, "setIndeterminate:", Bool(r.indeterminate))
			MsgSetFloat(r.native, Sel("setDoubleValue:"), r.value)
			if r.indeterminate {
				Send(r.native, "startAnimation:", 0)
			} else {
				Send(r.native, "stopAnimation:", 0)
			}
		}
		r.accessibility(n.view)
		r.layout(h, n)
	}
}

// nativeAt is whether AppKit's own indicator draws the ring at side: one
// of its sizes, in its colors.
func (r *progressRing) nativeAt(n *node, side float64) bool {
	return n.color == 0 && (side == ringRegular || side == ringSmall)
}

// layout fits the ring to its view's size: AppKit's indicator, centered,
// at its sizes and in its colors; else the drawn ring, as large as the
// shorter side. It runs as the frame or the Color changes.
func (r *progressRing) layout(h *Host, n *node) {
	if !n.placed {
		return // its first frame lays it out
	}
	size := n.frame.Size
	side := max(min(size.Width, size.Height), 0)
	square := Rect{
		Origin: Point{X: (size.Width - side) / 2, Y: (size.Height - side) / 2},
		Size:   Size{Width: side, Height: side},
	}
	if r.nativeAt(n, side) {
		r.removeDrawn()
		r.ensureNative(n)
		cs := uintptr(0) // regular
		if side == ringSmall {
			cs = 1
		}
		if uintptr(SendInt(r.native, "controlSize")) != cs {
			Send(r.native, "setControlSize:", cs)
		}
		MsgSetRect(r.native, selSetFrame, square)
		return
	}
	r.removeNative()
	noActions(func() {
		if r.indeterminate {
			r.removeCircle()
			r.layoutSpinner(n, square)
		} else {
			r.removeSpinner()
			r.layoutCircle(n, square)
		}
	})
	r.colors(h, n)
}

// noActions runs f without Core Animation's implicit animations.
func noActions(f func()) {
	ca := Class("CATransaction")
	Send(ca, "begin")
	Send(ca, "setDisableActions:", 1)
	f()
	Send(ca, "commit")
}

// newIndicator makes an NSProgressIndicator in the spinning style, from
// 0 to 1, read by VoiceOver through the AsukaRing.
func newIndicator(indeterminate bool, value float64) ID {
	v := initView("NSProgressIndicator")
	Send(v, "setStyle:", 1) // spinning
	MsgSetFloat(v, Sel("setMinValue:"), 0)
	MsgSetFloat(v, Sel("setMaxValue:"), 1)
	Send(v, "setIndeterminate:", Bool(indeterminate))
	MsgSetFloat(v, Sel("setDoubleValue:"), value)
	Send(v, "setAccessibilityElement:", 0) // the AsukaRing is the element
	return v
}

func (r *progressRing) ensureNative(n *node) {
	if r.native != 0 {
		return
	}
	v := newIndicator(r.indeterminate, r.value)
	Send(v, "setDisplayedWhenStopped:", 0)
	Send(n.view, "addSubview:", uintptr(v))
	Release(v)
	r.native = v
	if r.indeterminate {
		Send(v, "startAnimation:", 0)
	}
}

func (r *progressRing) removeNative() {
	if r.native == 0 {
		return
	}
	Send(r.native, "stopAnimation:", 0)
	Send(r.native, "removeFromSuperview")
	r.native = 0
}

func (r *progressRing) removeDrawn() {
	noActions(func() {
		r.removeCircle()
		r.removeSpinner()
	})
	if r.ref != 0 {
		Send(r.ref, "removeFromSuperview")
		r.ref = 0
		r.look.ok = false
	}
}

// release lets go of what the ring holds, as its view goes.
func (r *progressRing) release() {
	r.removeNative()
	r.removeDrawn()
}

func (r *progressRing) removeCircle() {
	for _, l := range []*ID{&r.track, &r.arc} {
		if *l != 0 {
			Send(*l, "removeFromSuperlayer")
			Release(*l)
			*l = 0
		}
	}
}

func (r *progressRing) removeSpinner() {
	if r.spinner != 0 {
		Send(r.spinner, "removeFromSuperlayer")
		Release(r.spinner)
		Release(r.spoke)
		r.spinner, r.spoke = 0, 0
	}
}

// layoutCircle draws the circle in square: from the top, clockwise, its
// stroke's outer edge the square's, the arc over the track as far as the
// value, its ends round.
func (r *progressRing) layoutCircle(n *node, square Rect) {
	if r.track == 0 {
		layer := Send(n.view, "layer")
		r.track = New("CAShapeLayer")
		r.arc = New("CAShapeLayer")
		for _, l := range []ID{r.track, r.arc} {
			Send(l, "setFillColor:", 0)
			Send(l, "setLineCap:", uintptr(String("round")))
			Send(layer, "addSublayer:", uintptr(l))
		}
		MsgSetFloat(r.arc, Sel("setStrokeEnd:"), r.value)
	}
	side := square.Size.Width
	w := ringStroke(side)
	loadCG()
	p := cgPathCreateMutable()
	// The layer is flipped (y down): the screen's clockwise is Core
	// Graphics' flag false.
	cx, cy := square.Origin.X+side/2, square.Origin.Y+side/2
	cgPathArc(p, 0, cx, cy, max(side/2-w/2, 0), -math.Pi/2, 3*math.Pi/2, false)
	b := Rect{Size: n.frame.Size}
	for _, l := range []ID{r.track, r.arc} {
		MsgSetRect(l, selSetFrame, b)
		Send(l, "setPath:", uintptr(p))
		MsgSetFloat(l, Sel("setLineWidth:"), w)
		Send(l, "setHidden:", Bool(side <= 0))
	}
	cgPathRelease(p)
}

// layoutSpinner draws AppKit's spinner in square: a spoke, from the top,
// replicated round the center, each copy a step of the turn behind the
// one before, its opacity falling an eighth a spoke, in steps.
func (r *progressRing) layoutSpinner(n *node, square Rect) {
	side := square.Size.Width
	if r.spinner == 0 {
		r.spinner = New("CAReplicatorLayer")
		r.spoke = New("CAShapeLayer")
		Send(r.spoke, "setFillColor:", 0)
		Send(r.spoke, "setLineCap:", uintptr(String("round")))
		Send(r.spinner, "addSublayer:", uintptr(r.spoke))
		Send(r.spinner, "setInstanceCount:", spinnerSpokes)
		MsgSetFloat(r.spinner, Sel("setInstanceDelay:"), spinnerTurn/spinnerSpokes)
		if msgValueWithTransform == nil {
			purego.RegisterFunc(&msgValueWithTransform, MsgSendAddr())
		}
		// A clockwise turn on screen is a positive angle, y down.
		a := 2 * math.Pi / spinnerSpokes
		t := caTransform3D{M11: math.Cos(a), M12: math.Sin(a), M21: -math.Sin(a), M22: math.Cos(a), M33: 1, M44: 1}
		Send(r.spinner, "setValue:forKey:", uintptr(msgValueWithTransform(Class("NSValue"), Sel("valueWithCATransform3D:"), t)), uintptr(String("instanceTransform")))
		Send(Send(n.view, "layer"), "addSublayer:", uintptr(r.spinner))
		r.spin()
	}
	MsgSetRect(r.spinner, selSetFrame, square)
	MsgSetRect(r.spoke, selSetFrame, Rect{Size: square.Size})
	w := side / 8
	loadCG()
	p := cgPathCreateMutable()
	// From 3/16 of the size out to the edge, the round ends included.
	cgPathMove(p, 0, side/2, side/2-(side*3/16+w/2))
	cgPathLine(p, 0, side/2, w/2)
	Send(r.spoke, "setPath:", uintptr(p))
	cgPathRelease(p)
	MsgSetFloat(r.spoke, Sel("setLineWidth:"), w)
	Send(r.spinner, "setHidden:", Bool(side <= 0))
}

// spin starts the spokes' animation, a turn already under way so every
// spoke shows its place in it from the start. AppKit's spinner turns
// whether or not Reduce Motion is on, and so does this.
func (r *progressRing) spin() {
	a := Send(Class("CAKeyframeAnimation"), "animationWithKeyPath:", uintptr(String("opacity")))
	values := Send(Class("NSMutableArray"), "array")
	for i := range spinnerSteps {
		Send(values, "addObject:", uintptr(NumberFloat(1-float64(i)/spinnerSteps)))
	}
	Send(a, "setValues:", uintptr(values))
	Send(a, "setCalculationMode:", uintptr(String("discrete")))
	MsgSetFloat(a, Sel("setDuration:"), spinnerTurn)
	MsgSetFloat32(a, Sel("setRepeatCount:"), float32(math.Inf(1))) // a float
	MsgSetFloat(a, Sel("setBeginTime:"), mediaTime()-spinnerTurn)
	Send(a, "setRemovedOnCompletion:", 0)
	Send(r.spoke, "addAnimation:forKey:", uintptr(a), uintptr(String("asuka.spin")))
}

var mediaTimeF func() float64

func mediaTime() float64 {
	if mediaTimeF == nil {
		purego.RegisterLibFunc(&mediaTimeF, Dlopen("/System/Library/Frameworks/QuartzCore.framework/QuartzCore"), "CACurrentMediaTime")
	}
	return mediaTimeF()
}

// setValue moves the ring to v, as AppKit's animator moves its
// indicator's value: over a quarter second, linearly, or with the
// Animate it's in; at once when Reduce Motion is on, or before it shows.
func (r *progressRing) setValue(h *Host, n *node, v float64) {
	from := r.value
	r.value = v
	if !r.indeterminate {
		Send(n.view, "setAccessibilityValue:", uintptr(NumberFloat(v)))
	}
	animate := n.placed && from != v && Send(n.view, "window") != 0 && (h.animating || !h.reduceMotion())
	if r.arc != 0 && n.color == 0 && (from >= 1) != (v >= 1) {
		r.colors(h, n) // a full circle's blue
	}
	switch {
	case r.native != 0:
		if !animate || r.indeterminate {
			MsgSetFloat(r.native, Sel("setDoubleValue:"), v)
			return
		}
		if h.animating { // the Animate's context
			MsgSetFloat(Send(r.native, "animator"), Sel("setDoubleValue:"), v)
			return
		}
		ctx := Class("NSAnimationContext")
		Send(ctx, "beginGrouping")
		MsgSetFloat(Send(ctx, "currentContext"), Sel("setDuration:"), ringValueDuration)
		MsgSetFloat(Send(r.native, "animator"), Sel("setDoubleValue:"), v)
		Send(ctx, "endGrouping")
	case r.arc != 0:
		end := String("strokeEnd")
		if animate {
			now := r.arc
			if p := Send(r.arc, "presentationLayer"); p != 0 {
				now = p
			}
			a := Send(Class("CABasicAnimation"), "animationWithKeyPath:", uintptr(end))
			Send(a, "setFromValue:", uintptr(NumberFloat(MsgFloat(now, Sel("strokeEnd")))))
			Send(a, "setToValue:", uintptr(NumberFloat(v)))
			if h.animating {
				MsgSetFloat(a, Sel("setDuration:"), h.anim.duration)
				Send(a, "setTimingFunction:", uintptr(Send(Send(Class("NSAnimationContext"), "currentContext"), "timingFunction")))
			} else {
				MsgSetFloat(a, Sel("setDuration:"), ringValueDuration)
			}
			Send(r.arc, "addAnimation:forKey:", uintptr(a), uintptr(end))
		} else {
			Send(r.arc, "removeAnimationForKey:", uintptr(end))
		}
		noActions(func() { MsgSetFloat(r.arc, Sel("setStrokeEnd:"), v) })
	}
}

// accessibility makes the view read as AppKit's indicator: a progress
// indicator with its value, minimum and maximum, or a busy indicator.
func (r *progressRing) accessibility(v ID) {
	if r.indeterminate {
		Send(v, "setAccessibilityRole:", uintptr(String("AXBusyIndicator")))
		Send(v, "setAccessibilityValue:", uintptr(NumberInt(0)))
		Send(v, "setAccessibilityMinValue:", 0)
		Send(v, "setAccessibilityMaxValue:", 0)
		return
	}
	Send(v, "setAccessibilityRole:", uintptr(String("AXProgressIndicator")))
	Send(v, "setAccessibilityValue:", uintptr(NumberFloat(r.value)))
	Send(v, "setAccessibilityMinValue:", uintptr(NumberInt(0)))
	Send(v, "setAccessibilityMaxValue:", uintptr(NumberInt(1)))
}

// attribute is what the ring answers for an accessibility attribute, as
// AppKit's indicator: its role, its value, from 0 to 1; a busy
// indicator's value 0, with no range. 0: the view's own.
func (r *progressRing) attribute(name string) ID {
	switch name {
	case "AXRole":
		if r.indeterminate {
			return String("AXBusyIndicator")
		}
		return String("AXProgressIndicator")
	case "AXValue":
		if r.indeterminate {
			return NumberInt(0)
		}
		return NumberFloat(r.value)
	case "AXMinValue", "AXMaxValue":
		if r.indeterminate {
			return 0
		}
		if name == "AXMinValue" {
			return NumberInt(0)
		}
		return NumberInt(1)
	}
	return 0
}

// restyle reads AppKit's colors again (the appearance, the window's
// state or the accent changed) and sets them.
func (r *progressRing) restyle(h *Host, n *node) {
	r.look.ok = false
	r.colors(h, n)
}

// colors sets the drawn ring's colors: the track AppKit's; the arc and
// the spokes the Style's Color, or AppKit's.
func (r *progressRing) colors(h *Host, n *node) {
	if r.track == 0 && r.spoke == 0 || n.view == 0 {
		return
	}
	if !r.look.ok {
		r.read(n)
	}
	cls := Class("NSAppearance")
	prev := Send(cls, "currentAppearance")
	Send(cls, "setCurrentAppearance:", uintptr(Send(n.view, "effectiveAppearance")))
	custom := h.nsColor(n.color)
	noActions(func() {
		if r.track != 0 {
			Send(r.track, "setStrokeColor:", cgColor(srgb(r.look.track)))
			arc := custom
			if arc == 0 {
				arc = srgb(r.look.arcAt(r.value))
			}
			Send(r.arc, "setStrokeColor:", cgColor(arc))
		}
		if r.spoke != 0 {
			spoke := custom // the leading spoke the Color
			if spoke == 0 {
				spoke = srgb(r.look.spoke)
			}
			Send(r.spoke, "setStrokeColor:", cgColor(spoke))
		}
	})
	Send(cls, "setCurrentAppearance:", uintptr(prev))
}

// arcAt is the arc's color at a value: AppKit's, or a full circle's.
func (l *ringLook) arcAt(v float64) [4]float64 {
	if v >= 1 {
		return l.full
	}
	return l.arc
}

func srgb(c [4]float64) ID {
	return MsgColor(Class("NSColor"), Sel("colorWithSRGBRed:green:blue:alpha:"), c[0], c[1], c[2], c[3])
}

// read reads AppKit's colors from an indicator of its own in the ring's
// view, unseen: drawn into a bitmap, its pixels as they are (AppKit's
// drawing is sRGB, and a device RGB bitmap leaves it so). The circle,
// half full, gives the arc and the track; the spinner, stopped, the
// darkest of its spokes. Out of a window, or should the drawing hold
// nothing, it's what AppKit draws as measured.
func (r *progressRing) read(n *node) {
	dark := viewIsDark(n.view)
	l := ringLook{arc: ringArcLight, full: ringArcLight, track: ringTrackLight, spoke: ringSpokeLight}
	if dark {
		l = ringLook{arc: ringArcDark, full: ringFullDark, track: ringTrackDark, spoke: ringSpokeDark}
	}
	if !windowActive(Send(n.view, "window")) {
		l.arc = ringInactiveLight
		if dark {
			l.arc = ringInactiveDark
		}
		l.full = l.arc
	}
	r.look = l
	if Send(n.view, "window") == 0 {
		return
	}
	if r.ref == 0 {
		r.ref = newIndicator(false, 0.5)
		MsgSetRect(r.ref, selSetFrame, Rect{Size: Size{Width: ringRegular, Height: ringRegular}})
		Send(r.ref, "setDisplayedWhenStopped:", 1)
		MsgSetFloat(r.ref, Sel("setAlphaValue:"), 0)
		Send(n.view, "addSubview:", uintptr(r.ref))
		Release(r.ref)
	}
	// Transparent, AppKit draws it otherwise (another blue): it's opaque
	// while it's drawn here, and transparent again before the window
	// shows anything.
	noActions(func() { MsgSetFloat(r.ref, Sel("setAlphaValue:"), 1) })
	defer noActions(func() { MsgSetFloat(r.ref, Sel("setAlphaValue:"), 0) })
	const scale = 2
	b := Rect{Size: Size{Width: ringRegular, Height: ringRegular}}
	draw := func() ID {
		rep := Send(Send(Class("NSBitmapImageRep"), "alloc"), "initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bytesPerRow:bitsPerPixel:",
			0, ringRegular*scale, ringRegular*scale, 8, 4, 1, 0, uintptr(String("NSDeviceRGBColorSpace")), 0, 0)
		MsgSetSize(rep, Sel("setSize:"), b.Size)
		MsgRectIDVoid(r.ref, Sel("cacheDisplayInRect:toBitmapImageRep:"), b, rep)
		return Autorelease(rep)
	}
	at := func(rep ID, radius, deg float64) [4]float64 {
		a := deg * math.Pi / 180
		x := (ringRegular/2 + radius*math.Sin(a)) * scale
		y := (ringRegular/2 - radius*math.Cos(a)) * scale
		c := Send(rep, "colorAtX:y:", uintptr(x), uintptr(y))
		if c == 0 {
			return [4]float64{}
		}
		return [4]float64{MsgFloat(c, Sel("redComponent")), MsgFloat(c, Sel("greenComponent")), MsgFloat(c, Sel("blueComponent")), MsgFloat(c, Sel("alphaComponent"))}
	}
	if r.indeterminate {
		Send(r.ref, "setIndeterminate:", 1)
		rep := draw()
		var best [4]float64
		for i := range spinnerSpokes {
			if c := at(rep, 11, float64(i)*360/spinnerSpokes); c[3] > best[3] {
				best = c
			}
		}
		if best[3] > 0 {
			l.spoke = best
		}
		Send(r.ref, "setIndeterminate:", 0)
	} else {
		// The stroke's middle, 13.5 points out: the arc at 45 degrees,
		// the track at 270; and a full circle's arc.
		MsgSetFloat(r.ref, Sel("setDoubleValue:"), 0.5)
		rep := draw()
		if c := at(rep, 13.5, 45); c[3] > 0 {
			l.arc = c
		}
		if c := at(rep, 13.5, 270); c[3] > 0 {
			l.track = c
		}
		MsgSetFloat(r.ref, Sel("setDoubleValue:"), 1)
		l.full = l.arc
		if c := at(draw(), 13.5, 270); c[3] > 0 {
			l.full = c
		}
	}
	l.ok = true
	r.look = l
}

// windowActive is whether w draws its controls active, in the accent:
// the key or main window of the active app.
func windowActive(w ID) bool {
	if w == 0 || !SendBool(Send(Class("NSApplication"), "sharedApplication"), "isActive") {
		return false
	}
	return SendBool(w, "isKeyWindow") || SendBool(w, "isMainWindow")
}

// viewIsDark is whether v's appearance is a dark one.
func viewIsDark(v ID) bool {
	names := Array(String("NSAppearanceNameAqua"), String("NSAppearanceNameDarkAqua"))
	return GoString(Send(Send(v, "effectiveAppearance"), "bestMatchFromAppearancesWithNames:", uintptr(names))) == "NSAppearanceNameDarkAqua"
}
