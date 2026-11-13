//go:build darwin && !ios

package darwin

import (
	"os"
	"testing"

	"github.com/ebitengine/purego/objc"

	"github.com/egoist/asuka/internal/flyout"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// Flyouts, panels, windows sized to their content and click-through
// windows (ops.WindowFlyout, WindowPanel, WindowFitContent,
// WindowClickThrough).
//
// A flyout and a panel are AsukaPanels: NSPanels with the nonactivating
// style from the start (the window server learns that a window never
// activates its app only from the style it's made with), borderless, a
// clear background under the system's material (NSVisualEffectView: a
// menu's for a flyout, a popover's for a panel) unless the app gives a
// background, rounded corners and the window's shadow. They can be the
// key window, never the main one: the app the user is in stays active,
// its menu bar showing. A panel joins every Space, as an auxiliary of
// apps in full screen, out of Mission Control and the window cycle, at
// the floating level; without a place of the app's it shows on the
// display with the pointer, centered across it, a third of the way down.
// A flyout hanging from a tray icon is at the level of menus, placed
// against its status item's button (the copy of the menu bar the button
// is in); one hanging from a rectangle of its parent is the parent's
// child window. Both close as the user closes them (CloseRequest, or
// Dismissed; the main window's closing) on an Escape their content
// leaves, as they stop being the key window, and on a click in another
// app (a global monitor: a nonactivating panel's app isn't active, so
// another app's activating doesn't always take the key window from it),
// unless a sheet or a modal Window of theirs shows: a modal Window over
// a flyout or a panel is its child window at its level, centered over
// it, not a sheet hanging from it, and gives it the key back as it
// closes.
//
// A window sized to its content takes WindowAction Fit's size, keeping
// its top left (a flyout is placed again); one that opens shown shows
// once it has, so it never shows at another size first. Click-through is
// ignoresMouseEvents.

const (
	styleBorderless     = 0
	styleNonactivating  = 1 << 7
	floatingPanelLevel  = 3   // NSFloatingWindowLevel
	popUpMenuLevel      = 101 // NSPopUpMenuWindowLevel
	materialMenu        = 5   // NSVisualEffectMaterialMenu
	materialPopover     = 6   // NSVisualEffectMaterialPopover
	flyoutRadius        = 12
	panelRadius         = 16
	joinAllSpaces       = 1 << 0
	collectionTransient = 1 << 3
	ignoresCycle        = 1 << 6
	fullScreenAux       = 1 << 8
)

// snapshotting reports an automation run (ASUKA_SNAPSHOT), whose windows
// show without taking the user's focus.
func snapshotting() bool { return os.Getenv("ASUKA_SNAPSHOT") != "" }

// testingHost reports a test, which watches no other apps' clicks.
func testingHost() bool { return testing.Testing() }

// floating reports whether c is a flyout's or a panel's.
func floating(c *ops.WindowConfig) bool { return c.Flags&(ops.WindowPanel|ops.WindowFlyout) != 0 }

func (x *win) isFlyout() bool { return x != nil && x.cfg.Flags&ops.WindowFlyout != 0 }
func (x *win) isPanel() bool  { return x != nil && x.cfg.Flags&ops.WindowPanel != 0 }

// floatingStyle is a flyout's or a panel's style mask: borderless and
// nonactivating, a panel resizable unless it's fixed.
func floatingStyle(c *ops.WindowConfig) uint {
	m := uint(styleBorderless | styleNonactivating)
	if c.Flags&ops.WindowPanel != 0 && c.Flags&ops.WindowFixedSize == 0 {
		m |= styleResizable
	}
	return m
}

// registerPanelClass makes AsukaPanel, the window of flyouts and panels.
func registerPanelClass() {
	DefineClass("AsukaPanel", "NSPanel", nil, []objc.MethodDef{
		Method("canBecomeKeyWindow", func(self ID, _ SEL) bool { return true }),
		Method("canBecomeMainWindow", func(self ID, _ SEL) bool { return false }),
		// NSPanel closes on an Escape (cancelOperation:) past the core:
		// the content's views have their say first, then the user's
		// closing.
		Method("cancelOperation:", func(self ID, _ SEL, sender ID) {
			if h := theHost; h != nil {
				h.cancel(Send(self, "contentView"))
			}
		}),
	})
}

// newPanelWindow makes an AsukaPanel of content size, with c's style.
func newPanelWindow(size Size, c *ops.WindowConfig) ID {
	return MsgInitWindow(Send(Class("AsukaPanel"), "alloc"), Sel("initWithContentRect:styleMask:backing:defer:"),
		Rect{Size: size}, floatingStyle(c), backingBuffered, false)
}

// makeMainPanel makes the main window a panel, as its options ask: an
// AsukaPanel takes the content of the window made before them, which
// nothing has shown yet.
func (h *Host) makeMainPanel(c *ops.WindowConfig) {
	old := h.window
	if old == 0 || Send(old, "isKindOfClass:", uintptr(Class("NSPanel"))) != 0 {
		return
	}
	size := MsgRect(h.content, Sel("bounds")).Size
	w := newPanelWindow(size, c)
	Send(w, "setReleasedWhenClosed:", 0)
	Send(w, "setTitle:", uintptr(Send(old, "title")))
	Retain(h.content)
	Send(old, "setContentView:", uintptr(New("NSView")))
	Send(old, "setDelegate:", 0)
	Send(w, "setContentView:", uintptr(h.content))
	Release(h.content)
	Send(w, "setDelegate:", uintptr(h.target))
	Send(old, "close")
	Release(old)
	h.window, h.main.window = w, w
}

// floatingChrome gives a flyout or a panel its look and behavior as its
// options are applied: clear under the system's material (or its
// background), rounded, shadowed, on every Space for a panel, at the
// level of menus for a tray's flyout.
func (h *Host) floatingChrome(x *win) {
	w, c := x.window, &x.cfg
	Send(w, "setOpaque:", 0)
	Send(w, "setBackgroundColor:", uintptr(Send(Class("NSColor"), "clearColor")))
	Send(w, "setHasShadow:", 1)
	Send(w, "setHidesOnDeactivate:", 0)
	Send(w, "setExcludedFromWindowsMenu:", 1)
	radius, material := float64(flyoutRadius), materialMenu
	behavior := uint(collectionTransient | ignoresCycle | fullScreenAux)
	level := 0
	if x.isPanel() {
		radius, material = panelRadius, materialPopover
		behavior |= joinAllSpaces
		level = floatingPanelLevel
	} else if x.flyout.Tray != 0 {
		level = popUpMenuLevel
	}
	Send(w, "setCollectionBehavior:", uintptr(behavior))
	if level != 0 {
		Send(w, "setLevel:", uintptr(level))
	}
	content := x.content
	Send(content, "setWantsLayer:", 1)
	layer := Send(content, "layer")
	MsgSetFloat(layer, Sel("setCornerRadius:"), radius)
	Send(layer, "setMasksToBounds:", 1)
	switch {
	case c.Flags&ops.WindowTransparent != 0:
	case c.Background != 0:
		if col := h.nsColor(c.Background); col != 0 {
			Send(layer, "setBackgroundColor:", uintptr(Send(col, "CGColor")))
		}
	default:
		// The material, under the content, following its size.
		Send(content, "setAutoresizesSubviews:", 1)
		ev := MsgInitRect(Send(Class("NSVisualEffectView"), "alloc"), Sel("initWithFrame:"), MsgRect(content, Sel("bounds")))
		Send(ev, "setMaterial:", uintptr(material))
		Send(ev, "setBlendingMode:", 0) // behind the window
		Send(ev, "setState:", 1)        // active, as the app isn't
		Send(ev, "setAutoresizingMask:", 2|16)
		Send(content, "addSubview:positioned:relativeTo:", uintptr(ev), ^uintptr(0), 0) // NSWindowBelow
		Release(ev)
	}
}

// yDown is r, in AppKit's screen coordinates (y up from the main
// display's bottom), in Asuka's (y down from its top).
func yDown(r Rect) flyout.Rect {
	return flyout.Rect{X: r.Origin.X, Y: mainScreenHeight() - r.Origin.Y - r.Size.Height, W: r.Size.Width, H: r.Size.Height}
}

// yUp is r back in AppKit's coordinates.
func yUp(r flyout.Rect) Rect {
	return Rect{Origin: Point{X: r.X, Y: mainScreenHeight() - r.Y - r.H}, Size: Size{Width: r.W, Height: r.H}}
}

// workArea is the visible frame (below the menu bar, beside the Dock) of
// the display with the point x, y (y down), or of the main display.
func workArea(x, y float64) flyout.Rect {
	screens := ArrayItems(Send(Class("NSScreen"), "screens"))
	for _, s := range screens {
		if yDown(MsgRect(s, Sel("frame"))).Contains(x, y) {
			return yDown(MsgRect(s, Sel("visibleFrame")))
		}
	}
	if len(screens) == 0 {
		return flyout.Rect{W: 1440, H: 900}
	}
	return yDown(MsgRect(screens[0], Sel("visibleFrame")))
}

// pointer is where the mouse is, y down.
func pointer() (float64, float64) {
	p := MsgPoint(Class("NSEvent"), Sel("mouseLocation"))
	return p.X, mainScreenHeight() - p.Y
}

// flyoutAnchor is the rectangle x hangs from on the screen, y down: its
// tray icon's button, or the rectangle of its parent's content.
func (h *Host) flyoutAnchor(x *win) (flyout.Rect, bool) {
	f := x.flyout
	if f.Tray != 0 {
		t := trayState.icons[f.Tray]
		if t == nil {
			return flyout.Rect{}, false
		}
		bw := Send(Send(t.item, "button"), "window")
		if bw == 0 {
			return flyout.Rect{}, false
		}
		frame := MsgRect(bw, Sel("frame"))
		if t.clicked.Size.Width > 0 && frame == t.of {
			return yDown(t.clicked), true
		}
		// Not clicked (opened from code): the item's window, where it's
		// on a display. A Mac whose menu bar has no room for the item,
		// or whose menu bar shows on several displays, may not show it
		// there: then where the pointer is, as a menu would open.
		if r := yDown(frame); onScreen(r.X+r.W/2, r.Y+r.H/2) {
			return r, true
		}
		px, py := pointer()
		return flyout.Rect{X: px, Y: py}, true
	}
	p := h.parentWindow(x.cfg.Parent)
	if p == 0 {
		return flyout.Rect{}, false
	}
	content := Send(p, "contentView")
	r := Rect{Origin: Point{X: float64(f.X), Y: float64(f.Y)}, Size: Size{Width: float64(f.Width), Height: float64(f.Height)}}
	r = MsgRectIDToRect(content, Sel("convertRect:toView:"), r, 0)
	return yDown(MsgRectToRect(p, Sel("convertRectToScreen:"), r)), true
}

// onScreen reports whether the point x, y (y down) is on a display.
func onScreen(x, y float64) bool {
	for _, s := range ArrayItems(Send(Class("NSScreen"), "screens")) {
		if yDown(MsgRect(s, Sel("frame"))).Contains(x, y) {
			return true
		}
	}
	return false
}

// noteClick keeps where on screen the copy of t's button the click ev
// hit shows: the pointer less the click's place in the button's window,
// which the copies (one per display's menu bar) share. A flyout hangs
// from it while the window keeps its frame.
func (t *trayIcon) noteClick(button, ev ID) {
	w := Send(button, "window")
	if ev == 0 || w == 0 || Send(ev, "window") != w {
		return
	}
	of := MsgRect(w, Sel("frame"))
	at := MsgPoint(Class("NSEvent"), Sel("mouseLocation"))
	in := MsgPoint(ev, Sel("locationInWindow"))
	t.clicked = Rect{Origin: Point{X: at.X - in.X, Y: at.Y - in.Y}, Size: of.Size}
	t.of = of
}

// placeFlyout places x against its anchor, in the work area of the
// anchor's display; with no anchor (its tray icon gone), in the middle
// of the pointer's display.
func (h *Host) placeFlyout(x *win) {
	if !x.isFlyout() {
		return
	}
	size := MsgRect(x.window, Sel("frame")).Size
	var r flyout.Rect
	if a, ok := h.flyoutAnchor(x); ok {
		r = flyout.Place(a, size.Width, size.Height, x.flyout.Placement, float64(x.flyout.Gap), workArea(a.X+a.W/2, a.Y+a.H/2))
	} else {
		r = flyout.Center(size.Width, size.Height, workArea(pointer()))
	}
	MsgSetRectBool(x.window, Sel("setFrame:display:"), yUp(r), true)
}

// centerPanel puts a panel the app hasn't placed on the display with
// the pointer, centered across it, a third of the way down.
func (h *Host) centerPanel(x *win) {
	if !x.isPanel() || x.placed {
		return
	}
	size := MsgRect(x.window, Sel("frame")).Size
	r := flyout.Center(size.Width, size.Height, workArea(pointer()))
	MsgSetRectBool(x.window, Sel("setFrame:display:"), yUp(r), true)
}

// showFloating shows a flyout or a panel as the key window without
// activating the app: placed, a rectangle's flyout attached to its
// parent.
func (h *Host) showFloating(x *win) {
	w := x.window
	h.placeFlyout(x)
	h.centerPanel(x)
	if snapshotting() {
		Send(w, "orderFront:", 0)
	} else {
		// Key without activating the app: NSApp's isActive reads true
		// while it's key, but the app in front stays so (the
		// workspace's frontmost application).
		Send(w, "makeKeyAndOrderFront:", 0)
	}
	if x.isFlyout() && x.flyout.Tray == 0 && Send(w, "parentWindow") == 0 {
		if p := h.parentWindow(x.cfg.Parent); p != 0 && p != w {
			Send(p, "addChildWindow:ordered:", uintptr(w), 1)
		}
	}
	h.watchOutsideClicks()
}

// hideFloating hides a flyout or a panel, a flyout no longer its
// parent's child (AppKit would show it again with its parent).
func (h *Host) hideFloating(x *win) {
	w := x.window
	if p := Send(w, "parentWindow"); p != 0 {
		Send(p, "removeChildWindow:", uintptr(w))
	}
	Send(w, "orderOut:", 0)
}

// overFloating is the flyout or panel a modal Window c's parent is, or
// nil: the modal Window is its child window, not a sheet.
func (h *Host) overFloating(c *ops.WindowConfig) *win {
	if c.Flags&ops.WindowModal == 0 {
		return nil
	}
	if x := h.winOf(h.parentWindow(c.Parent)); x != nil && floating(&x.cfg) {
		return x
	}
	return nil
}

// placeOver centers x, a modal Window, over the flyout or panel it's
// modal to, within its display, at its level.
func (h *Host) placeOver(x, over *win) {
	Send(x.window, "setLevel:", uintptr(SendInt(over.window, "level")))
	Send(x.window, "setCollectionBehavior:", collectionTransient|fullScreenAux)
	o := yDown(MsgRect(over.window, Sel("frame")))
	f := yDown(MsgRect(x.window, Sel("frame")))
	work := workArea(o.X+o.W/2, o.Y+o.H/2)
	r := flyout.Rect{X: o.X + (o.W-f.W)/2, Y: o.Y + (o.H-f.H)/2, W: f.W, H: f.H}
	r.X = max(min(r.X, work.Right()-r.W), work.X)
	r.Y = max(min(r.Y, work.Bottom()-r.H), work.Y)
	MsgSetRectBool(x.window, Sel("setFrame:display:"), yUp(r), false)
}

// keptOpen reports whether a flyout or panel stays open as it loses the
// keyboard: a sheet of it, or a modal Window over it, shows, or the key
// window is one of its own.
func (h *Host) keptOpen(x *win) bool {
	if Send(x.window, "attachedSheet") != 0 {
		return true
	}
	for _, y := range h.wins {
		if y.child == x.window && SendBool(y.window, "isVisible") {
			return true
		}
	}
	return false
}

// dismissFloating closes a flyout or a panel as the user closes it, if
// it shows and isn't kept open; unless outside (a click in another app),
// only once another window, or none, is key.
func (h *Host) dismissFloating(x *win, outside bool) {
	if x == nil || !floating(&x.cfg) || !SendBool(x.window, "isVisible") || h.keptOpen(x) {
		return
	}
	if !outside && SendBool(x.window, "isKeyWindow") {
		return
	}
	h.userClose(x)
}

// userClose closes x as its close button would: the core asks the app
// first if it listens, a Window closes through the core (Dismissed), the
// main window closes (hidden in an app that keeps running).
func (h *Host) userClose(x *win) {
	if x == h.main {
		if h.root != nil && h.root.listening(ops.EventCloseRequest) {
			h.emit(h.root, ops.Event{Kind: ops.EventCloseRequest})
			return
		}
		h.hideFloating(x)
		Send(x.window, "close")
		h.windowsChanged()
		return
	}
	if x.root.listening(ops.EventCloseRequest) {
		h.emit(x.root, ops.Event{Kind: ops.EventCloseRequest})
	} else {
		h.emit(x.root, ops.Event{Kind: ops.EventDismissed})
	}
}

// escapeFloating closes the flyout or panel w is, on an Escape its
// content left; it reports whether w is one.
func (h *Host) escapeFloating(w ID) bool {
	x := h.winOf(w)
	if x == nil || !floating(&x.cfg) {
		return false
	}
	if !h.keptOpen(x) {
		h.userClose(x)
	}
	return true
}

// appWindowAt reports whether the point x, y (y down) is in one of the
// app's windows that shows, a flyout's dialog or an alert's too.
func (h *Host) appWindowAt(x, y float64) bool {
	app := Send(Class("NSApplication"), "sharedApplication")
	for _, w := range ArrayItems(Send(app, "windows")) {
		if SendBool(w, "isVisible") && yDown(MsgRect(w, Sel("frame"))).Contains(x, y) {
			return true
		}
	}
	return false
}

// outsideClicks is the monitor of clicks in other apps, made as the
// first flyout or panel shows.
var outsideClicks ID

func (h *Host) watchOutsideClicks() {
	if outsideClicks != 0 || testingHost() {
		return
	}
	const mask = 1<<1 | 1<<3 | 1<<25 // left, right and other mouse down
	blk := NewBlock(func(_ objc.Block, ev ID) {
		// An inactive app's windows get a global monitor's events too:
		// a click on one of its own is no click elsewhere.
		at := MsgPoint(ev, Sel("locationInWindow")) // on the screen: no window of the app's
		if h.appWindowAt(at.X, mainScreenHeight()-at.Y) {
			return
		}
		for _, x := range h.allWins() {
			h.dismissFloating(x, true)
		}
	})
	outsideClicks = Retain(Send(Class("NSEvent"), "addGlobalMonitorForEventsMatchingMask:handler:", mask, uintptr(blk)))
}

// resignedKey is a window no longer key: a flyout or panel closes once
// the key window has settled, unless the key went to its own.
func (h *Host) resignedKey(w ID) {
	x := h.winOf(w)
	if x == nil || !floating(&x.cfg) {
		return
	}
	h.Post(func() {
		if h.winOf(x.window) != x {
			return
		}
		if k := h.winOf(Send(Send(Class("NSApplication"), "sharedApplication"), "keyWindow")); k != nil && k.child == x.window {
			return
		}
		h.dismissFloating(x, false)
	})
}

// fit sizes x's content to width by height points, as its content's
// size: a flyout placed again, other windows keeping their top left. A
// window waiting for its size shows.
func (h *Host) fit(x *win, width, height float32) {
	w := x.window
	if width <= 0 || height <= 0 {
		return
	}
	f := MsgRect(w, Sel("frame"))
	top := Point{X: f.Origin.X, Y: f.Origin.Y + f.Size.Height}
	MsgSetSize(w, Sel("setContentSize:"), Size{Width: float64(width), Height: float64(height)})
	if x.isFlyout() {
		if SendBool(w, "isVisible") {
			h.placeFlyout(x)
		}
	} else {
		MsgPointID(w, Sel("setFrameTopLeftPoint:"), top)
	}
	if x.isFlyout() || x.isPanel() {
		Send(w, "invalidateShadow")
	}
	x.fitted = true
	if x.showOnFit {
		x.showOnFit = false
		h.showWin(x, inBackground())
	}
}

// setClickThrough lets the mouse go through x, or takes it back.
func setClickThrough(x *win, on bool) { Send(x.window, "setIgnoresMouseEvents:", Bool(on)) }

// regainKey gives the key back to the flyout or panel a modal Window
// over it was the child of, as that closes.
func (h *Host) regainKey(parent ID) {
	if x := h.winOf(parent); x != nil && floating(&x.cfg) && SendBool(parent, "isVisible") {
		Send(parent, "makeKeyWindow")
	}
}
