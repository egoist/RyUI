//go:build linux && !android

package linux

import (
	"time"

	"github.com/egoist/asuka/internal/flyout"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// Panels, flyouts, windows sized to their content and click-through
// windows (WindowConfig's Panel, Flyout, FitContent and ClickThrough),
// and, on X11, a window's place and staying on top.
//
// A panel or a flyout has no frame (gtk_window_set_decorated false, its
// header bar hidden: its content is the whole window). It takes the
// keyboard as it shows (gtk_window_present), and closes as the user
// closing it (close-request: the core's CloseRequest, else Dismissed for
// a Window, else the main window's close, which hides it in an app that
// keeps running) on an Escape its content leaves (a key controller on
// the window, bubbling: the focused view has it first) and as it stops
// being the active window (notify::is-active), but while a window over
// it shows (one transient for it: a Modal Window whose Parent it is, a
// dialog of GTK's), which keeps it open and gives it the keyboard back
// as it goes. A tray icon's flyout closed that way remembers it a
// moment, so the click on its icon that took its keyboard (a panel
// that takes focus) doesn't open it again: the click is the closing.
//
// What each display server lets an app do:
//
//   - X11 (Xlib on the GdkX11Surface's window, as GTK 4 dropped its
//     setters): a panel or a flyout is a utility window
//     (_NET_WM_WINDOW_TYPE_UTILITY) out of the taskbar and the pager
//     (GDK's skip hints); AlwaysOnTop is _NET_WM_STATE_ABOVE and a panel
//     sticky on every desktop (_NET_WM_STATE_STICKY, _NET_WM_DESKTOP
//     0xFFFFFFFF), asked of the window manager as EWMH says (a client
//     message) once it's mapped, or written where none runs. A window
//     goes where it's put (XMoveWindow before it maps, and again as it
//     maps for a window manager that placed it anyway): at X, Y; centered
//     on the display with the pointer; a panel without X and Y on that
//     display, centered across, a third down (flyout.Center), at each
//     show; a flyout next to its anchor (flyout.Place) within its
//     display's work area (_NET_WORKAREA, gdk_x11_monitor_get_workarea):
//     a rectangle of its parent's content (where XTranslateCoordinates
//     says the parent is), or the place its tray icon was clicked
//     (StatusNotifierItem's Activate, ContextMenu and SecondaryActivate
//     carry it where the panel knows it: KDE Plasma, GNOME's AppIndicator
//     extension; 0, 0 where it doesn't), else centered on the primary
//     display, a third down.
//   - Wayland has none of that for an ordinary window (an xdg_toplevel):
//     the compositor places it (GNOME centers a new window, KDE too by
//     default) and keeps it in the taskbar, and an xdg_popup needs a
//     parent surface, which a tray icon's flyout hasn't. A panel is an
//     overlay layer-shell surface with the keyboard to itself where the
//     app runs with gtk4-layer-shell (LD_PRELOAD, as it must come before
//     libwayland-client) and the compositor speaks wlr-layer-shell (KDE
//     Plasma, Sway, Hyprland, COSMIC; not GNOME): above every window, on
//     the output the compositor picks, centered there, or at X, Y.
//
// A window sized to its content (WindowAction Fit, from the core's
// layout, starting in the flush it opens in) has its content's size
// request and the window's default size set to it; one opening waits for
// its first Fit to show, so it never shows at another size first. A
// flyout is placed again as it resizes (X11).
//
// A click-through window's surface has an empty input region
// (gdk_surface_set_input_region: X11's SHAPE input shape, Wayland's
// wl_surface input region), so the pointer goes to what's behind it; a
// NULL region gives the input back. GTK sets a region of its own on a
// decorated window with shadows as it's allocated, so it's set again
// after each frame while it's on.

// deskState is a window's state as a panel, a flyout, a window sized to
// its content or one the mouse goes through.
type deskState struct {
	flyout    ops.Flyout
	hasFlyout bool

	fitW, fitH float32
	fitted     bool
	waitFit    bool   // to show once its size is known
	waitFocus  bool   // and take the keyboard then
	waitSrc    uint32 // the idle that shows it anyway

	placed     bool // put at its X, Y or centered once (a window not a panel or flyout)
	placedW    float64
	placedH    float64 // the size it was placed at
	through    bool    // the mouse goes through it
	regionSet  bool    // an input region of the host's is set
	clockHook  bool    // its frame clock's after-paint sets it again
	onTop      bool    // AlwaysOnTop asked
	statesSet  bool    // the host set EWMH states (above, sticky) on it
	hooked     bool
	keys       Ptr    // the Escape controller of a panel or flyout
	dismissSrc uint32 // the idle closing it as it lost the keyboard
	held       bool   // lost the keyboard to a window over it, which keeps it open
	layer      bool   // a layer-shell surface (Wayland)
}

// dropDesk lets a closing window's pending work go.
func (h *Host) dropDesk(w *win) {
	d := &w.desk
	SourceRemove(d.waitSrc)
	SourceRemove(d.dismissSrc)
	d.waitSrc, d.dismissSrc, d.waitFit = 0, 0, false
}

// frameless reports a panel or a flyout: no frame, no header bar.
func (w *win) frameless() bool { return w.cfg.Flags&(ops.WindowPanel|ops.WindowFlyout) != 0 }

// configureDesk applies what configure doesn't: a panel's or a flyout's
// look and behavior, staying on top, click-through.
func (h *Host) configureDesk(w *win, c ops.WindowConfig) {
	d := &w.desk
	if w.frameless() {
		WindowSetDecorated(w.window, false)
		WidgetSetVisible(w.header, false)
		if d.keys == 0 {
			d.keys = EventControllerKeyNew()
			Connect(d.keys, "key-pressed", func(keyval, keycode, state uint32) bool {
				if keyval != keyEscape || !w.frameless() || w.closing {
					return false
				}
				h.dismissDesk(w, false)
				return true
			})
			WidgetAddController(w.window, d.keys)
		}
		if c.Flags&ops.WindowFlyout != 0 && c.Parent != 0 {
			// Transient for the window it's declared in, unless it hangs
			// from a tray icon (setFlyout).
			if p := h.parentWin(c.Parent); p != nil && p != w && (!d.hasFlyout || d.flyout.Tray == 0) {
				WindowSetTransientFor(w.window, p.window)
				WindowSetDestroyWithParent(w.window, true)
			}
		}
	}
	if c.Flags&ops.WindowPanel != 0 {
		h.layerPanel(w)
	}
	d.through = c.Flags&ops.WindowClickThrough != 0
	d.onTop = c.Flags&ops.WindowAlwaysOnTop != 0
	if !d.hooked {
		d.hooked = true
		Connect(w.window, "map", func() { h.deskMapped(w) })
		Connect(w.window, "unmap", func() { h.deskUnmapped(w) })
		Connect(w.window, "notify::is-active", func(uintptr) { h.deskActiveChanged(w) })
	}
	h.applyClickThrough(w)
}

const keyEscape = 0xff1b

// setFlyout takes a flyout's Flyout prop: its anchor and placement.
func (h *Host) setFlyout(w *win, b []byte) {
	f, ok := ops.DecodeFlyout(b)
	if !ok {
		return
	}
	d := &w.desk
	d.flyout, d.hasFlyout = f, true
	if f.Tray != 0 {
		// A tray icon's hangs from no window of the app's (the main one
		// may be hidden).
		WindowSetTransientFor(w.window, 0)
	} else if p := h.parentWin(w.cfg.Parent); p != nil && p != w {
		WindowSetTransientFor(w.window, p.window)
		WindowSetDestroyWithParent(w.window, true)
	}
	if WidgetGetVisible(w.window) {
		h.place(w)
	}
}

// showWin shows w as the host does (opened, Show, Focus): placed first
// (X11); one sized to its content once it knows its size; a panel or a
// flyout taking the keyboard.
func (h *Host) showWin(w *win, focus bool) {
	d := &w.desk
	focus = focus || w.frameless()
	if w.cfg.Flags&ops.WindowFitContent != 0 && !d.fitted {
		// The core sends its size in the flush it opens in: until then
		// (an idle turn at the latest), it waits.
		d.waitFit, d.waitFocus = true, d.waitFocus || focus
		if d.waitSrc == 0 {
			d.waitSrc = IdleAdd(PriorityHighIdle, func() bool {
				d.waitSrc = 0
				h.endWaitFit(w)
				return false
			})
		}
		return
	}
	h.beforeShow(w)
	if focus {
		WindowPresent(w.window)
	} else {
		WidgetSetVisible(w.window, true)
	}
}

// endWaitFit shows a window that waited for its size.
func (h *Host) endWaitFit(w *win) {
	d := &w.desk
	if !d.waitFit || w.closing {
		return
	}
	d.waitFit = false
	SourceRemove(d.waitSrc)
	d.waitSrc = 0
	h.beforeShow(w)
	if d.waitFocus {
		WindowPresent(w.window)
	} else {
		WidgetSetVisible(w.window, true)
	}
	d.waitFocus = false
}

// fit sizes a window to its content (WindowAction Fit): x, y points.
func (h *Host) fit(w *win, x, y float32) {
	d := &w.desk
	changed := !d.fitted || x != d.fitW || y != d.fitH
	d.fitW, d.fitH, d.fitted = x, y, true
	if changed {
		cw, ch := windowSize(x), windowSize(y)
		WidgetSetSizeRequest(w.content, cw, ch)
		WindowSetDefaultSize(w.window, cw, ch+w.headerRoom())
	}
	if d.waitFit {
		h.endWaitFit(w)
		return
	}
	if changed && WidgetGetVisible(w.window) && w.cfg.Flags&ops.WindowFlyout != 0 {
		h.place(w) // a panel or a window keeps its top left
	}
}

// beforeShow readies a window to map: on X11, its hints and its place.
func (h *Host) beforeShow(w *win) {
	disp, xid, ok := h.x11Win(w)
	if !ok {
		return
	}
	s := NativeGetSurface(w.window)
	skip := w.cfg.Flags&ops.WindowSkipTaskbar != 0 || w.frameless()
	if WM.X11SurfaceSetSkipTaskbar != nil {
		// GDK's, kept in its state: it writes them as the window maps.
		WM.X11SurfaceSetSkipTaskbar(s, skip)
		WM.X11SurfaceSetSkipPager(s, w.frameless())
	}
	if w.frameless() {
		XSetAtoms(disp, xid, "_NET_WM_WINDOW_TYPE", "ATOM", []uint64{XAtom(disp, "_NET_WM_WINDOW_TYPE_UTILITY")})
	}
	h.placeAtShow(w)
}

// placeAtShow places a window as it shows: a panel or a flyout each
// time, another window the first time only (then it's where the user
// left it).
func (h *Host) placeAtShow(w *win) {
	if !w.frameless() {
		if w.desk.placed {
			return
		}
		w.desk.placed = WidgetGetMapped(w.window)
	}
	h.place(w)
}

// deskMapped: the window shows. On X11 the states GDK no longer sets,
// and its place again; its input region.
func (h *Host) deskMapped(w *win) {
	if w.closing {
		return
	}
	if disp, xid, ok := h.x11Win(w); ok {
		if w.frameless() {
			XSetAtoms(disp, xid, "_NET_WM_WINDOW_TYPE", "ATOM", []uint64{XAtom(disp, "_NET_WM_WINDOW_TYPE_UTILITY")})
		}
		h.x11States(w, disp, xid)
		h.placeAtShow(w)
	}
	h.applyClickThrough(w)
}

// deskUnmapped: a window hid; a panel or flyout it kept open takes the
// keyboard back.
func (h *Host) deskUnmapped(w *win) {
	IdleAdd(PriorityDefaultIdle, func() bool {
		for _, x := range h.allWins() {
			if !x.desk.held || x.closing || !WidgetGetVisible(x.window) {
				continue
			}
			if !h.heldOpen(x) {
				x.desk.held = false
				WindowPresent(x.window)
			}
		}
		return false
	})
}

// allWins is every window of the app's: the main one and the Windows'.
func (h *Host) allWins() []*win {
	out := make([]*win, 0, len(h.wins)+1)
	if h.main != nil {
		out = append(out, h.main)
	}
	for _, x := range h.wins {
		if x != h.main {
			out = append(out, x)
		}
	}
	return out
}

// deskActiveChanged: a panel or a flyout that stops being the active
// window closes, on the main loop's next idle turn if it's still not
// (the keyboard may only be passing through), unless a window over it
// keeps it open.
func (h *Host) deskActiveChanged(w *win) {
	d := &w.desk
	if !w.frameless() || w.closing || WindowIsActive(w.window) || !WidgetGetVisible(w.window) {
		return
	}
	if h.heldOpen(w) {
		d.held = true
		return
	}
	if d.dismissSrc != 0 {
		return
	}
	d.dismissSrc = IdleAdd(PriorityDefaultIdle, func() bool {
		d.dismissSrc = 0
		if w.closing || !WidgetGetVisible(w.window) || WindowIsActive(w.window) {
			return false
		}
		if h.heldOpen(w) {
			d.held = true
			return false
		}
		h.dismissDesk(w, true)
		return false
	})
}

// heldOpen reports whether a window over w shows: one transient for it,
// or for one that is.
func (h *Host) heldOpen(w *win) bool {
	if WindowGetToplevels == nil || WM.WindowGetTransientFor == nil {
		return false
	}
	tops := WindowGetToplevels()
	n := ListModelGetNItems(tops)
	held := false
	for i := uint32(0); i < n && !held; i++ {
		t := ListModelGetItem(tops, i)
		if t == 0 {
			continue
		}
		if t != w.window && WidgetGetVisible(t) {
			for p, depth := WM.WindowGetTransientFor(t), 0; p != 0 && depth < 8; p, depth = WM.WindowGetTransientFor(p), depth+1 {
				if p == w.window {
					held = true
					break
				}
			}
		}
		ObjectUnref(t)
	}
	return held
}

// dismissDesk closes a panel or a flyout as the user closing it.
// lostFocus: it stopped being the active window.
func (h *Host) dismissDesk(w *win, lostFocus bool) {
	if w.closing || !WidgetGetVisible(w.window) {
		return
	}
	if lostFocus && w.desk.hasFlyout && w.desk.flyout.Tray != 0 {
		h.trayFlyoutClosed(w.desk.flyout.Tray)
	}
	WindowClose(w.window)
}

// x11Win is w's X window, realized; ok false off X11.
func (h *Host) x11Win(w *win) (disp Ptr, xid uint64, ok bool) {
	disp = DisplayGetDefault()
	if w.closing || !IsX11(disp) || !LoadXWM() {
		return 0, 0, false
	}
	WM.WidgetRealize(w.window)
	s := NativeGetSurface(w.window)
	if s == 0 {
		return 0, 0, false
	}
	xid = WM.X11SurfaceGetXID(s)
	return disp, xid, xid != 0
}

// scaleOf is w's surface's scale: X11's pixels per point.
func scaleOf(w *win) float64 {
	if s := NativeGetSurface(w.window); s != 0 {
		if f := SurfaceGetScaleFactor(s); f > 0 {
			return float64(f)
		}
	}
	return 1
}

// x11States asks for (or, with no window manager, writes) the EWMH
// states GDK 4 no longer does: above and sticky.
func (h *Host) x11States(w *win, disp Ptr, xid uint64) {
	above, sticky := w.desk.onTop, w.cfg.Flags&ops.WindowPanel != 0
	if !above && !sticky && !w.desk.statesSet {
		return // nothing of the host's to set, or take back
	}
	w.desk.statesSet = above || sticky
	stateAtom := func(name string) uint64 { return XAtom(disp, name) }
	if XWindowManaged(disp) {
		// _NET_WM_STATE: action (0 remove, 1 add), two states, source (1:
		// an application).
		XClientMessage(disp, xid, "_NET_WM_STATE", [5]uint64{b2l(above), stateAtom("_NET_WM_STATE_ABOVE"), 0, 1})
		if sticky {
			XClientMessage(disp, xid, "_NET_WM_STATE", [5]uint64{1, stateAtom("_NET_WM_STATE_STICKY"), 0, 1})
			XClientMessage(disp, xid, "_NET_WM_DESKTOP", [5]uint64{0xFFFFFFFF, 1})
		}
		return
	}
	cur, _ := XPropertyAtoms(disp, xid, "_NET_WM_STATE")
	want := map[uint64]bool{stateAtom("_NET_WM_STATE_ABOVE"): above}
	if sticky {
		want[stateAtom("_NET_WM_STATE_STICKY")] = true
	}
	out := cur[:0:0]
	for _, a := range cur {
		if v, ok := want[a]; ok && !v {
			continue
		}
		if want[a] {
			delete(want, a)
		}
		out = append(out, a)
	}
	for a, v := range want {
		if v {
			out = append(out, a)
		}
	}
	XSetAtoms(disp, xid, "_NET_WM_STATE", "ATOM", out)
	if sticky {
		XSetAtoms(disp, xid, "_NET_WM_DESKTOP", "CARDINAL", []uint64{0xFFFFFFFF})
	}
}

func b2l(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// setOnTop is WindowAction AlwaysOnTop and NotAlwaysOnTop: X11 only.
func (h *Host) setOnTop(w *win, on bool) {
	w.desk.onTop = on
	if !WidgetGetMapped(w.window) {
		return
	}
	if disp, xid, ok := h.x11Win(w); ok {
		h.x11States(w, disp, xid)
	}
}

// onTop reports whether w is kept on top: asked, where it can be (X11).
func (h *Host) onTop(w *win) bool {
	return w.desk.onTop && IsX11(DisplayGetDefault())
}

// place puts a window where it goes, on X11: a flyout by its anchor, a
// window at its X, Y or centered, a panel on the pointer's display.
func (h *Host) place(w *win) {
	c := &w.cfg
	if c.Flags&(ops.WindowFlyout|ops.WindowPanel|ops.WindowPositioned|ops.WindowCentered) == 0 {
		return
	}
	disp, xid, ok := h.x11Win(w)
	if !ok {
		return
	}
	scale := scaleOf(w)
	ww, wh := h.outerSize(w)
	var r flyout.Rect
	switch {
	case c.Flags&ops.WindowFlyout != 0:
		if !w.desk.hasFlyout {
			return
		}
		r = h.flyoutFrame(w, ww, wh, scale)
	case c.Flags&ops.WindowPositioned != 0:
		r = flyout.Rect{X: float64(c.X), Y: float64(c.Y)}
	case c.Flags&ops.WindowPanel != 0:
		r = flyout.Center(ww, wh, h.pointerWorkArea(disp, scale))
	default: // centered
		work := h.pointerWorkArea(disp, scale)
		r = flyout.Rect{X: work.X + (work.W-ww)/2, Y: work.Y + (work.H-wh)/2}
	}
	w.desk.placedW, w.desk.placedH = ww, wh
	// The surface holds the window's shadow, if GTK draws one.
	var sx, sy float64
	if WM.NativeGetSurfaceTransform != nil {
		WM.NativeGetSurfaceTransform(w.window, &sx, &sy)
	}
	XMove(disp, xid, int32(roundF((r.X-sx)*scale)), int32(roundF((r.Y-sy)*scale)))
}

// deskAllocated: a window GTK allocated. A panel or a flyout placed
// before it knew its size (one not sized to its content, whose default
// size includes GTK's frame) is placed again at it.
func (h *Host) deskAllocated(w *win) {
	d := &w.desk
	if !w.frameless() || d.fitted || w.closing || !WidgetGetMapped(w.window) {
		return
	}
	if ww, wh := h.outerSize(w); ww != d.placedW || wh != d.placedH {
		h.place(w)
	}
}

// moveTo is WindowAction Position (X11): its config's X, Y.
func (h *Host) moveTo(w *win, x, y float32) {
	w.cfg.X, w.cfg.Y = x, y
	w.cfg.Flags = w.cfg.Flags&^ops.WindowCentered | ops.WindowPositioned
	if WidgetGetVisible(w.window) {
		h.place(w)
	} else {
		w.desk.placed = false // as it shows
	}
}

// center is WindowAction Center (X11).
func (h *Host) center(w *win) {
	w.cfg.Flags = w.cfg.Flags&^ops.WindowPositioned | ops.WindowCentered
	if WidgetGetVisible(w.window) {
		h.place(w)
	} else {
		w.desk.placed = false
	}
}

func roundF(f float64) float64 {
	if f < 0 {
		return -roundF(-f)
	}
	return float64(int64(f + 0.5))
}

// outerSize is the window's size in points: its content's (as fitted)
// and the header bar's, else as allocated, else its default size.
func (h *Host) outerSize(w *win) (float64, float64) {
	if d := &w.desk; d.fitted {
		return float64(windowSize(d.fitW)), float64(windowSize(d.fitH) + w.headerRoom())
	}
	if WidgetGetMapped(w.window) {
		if ww, wh := Call(FnWidgetGetWidth, w.window), Call(FnWidgetGetHeight, w.window); ww > 0 && wh > 0 {
			return float64(int32(ww)), float64(int32(wh))
		}
	}
	var dw, dh int32
	WindowGetDefaultSize(w.window, &dw, &dh)
	return float64(dw), float64(dh)
}

// flyoutFrame is where a flyout goes (points, on the screen): next to
// its anchor, in the work area of the display that's on.
func (h *Host) flyoutFrame(w *win, ww, wh, scale float64) flyout.Rect {
	f := w.desk.flyout
	disp := DisplayGetDefault()
	var anchor flyout.Rect
	if f.Tray != 0 {
		t := h.trays().icons[f.Tray]
		if t == nil || !t.clicked {
			// Where its icon is, no one said: the primary display's
			// middle.
			return flyout.Center(ww, wh, h.primaryWorkArea(disp))
		}
		anchor = flyout.Rect{X: float64(t.clickX) / scale, Y: float64(t.clickY) / scale, W: 1, H: 1}
	} else {
		p := h.parentWin(w.cfg.Parent)
		if p == nil || p == w {
			return flyout.Center(ww, wh, h.primaryWorkArea(disp))
		}
		ox, oy, ok := h.contentOrigin(p, scale)
		if !ok {
			return flyout.Center(ww, wh, h.primaryWorkArea(disp))
		}
		anchor = flyout.Rect{X: ox + float64(f.X), Y: oy + float64(f.Y), W: float64(f.Width), H: float64(f.Height)}
	}
	work := h.workAreaAt(disp, anchor.X+anchor.W/2, anchor.Y+anchor.H/2)
	return flyout.Place(anchor, ww, wh, f.Placement, float64(f.Gap), work)
}

// contentOrigin is where a window's content's top left is on the screen
// (points; X11).
func (h *Host) contentOrigin(w *win, scale float64) (x, y float64, ok bool) {
	disp, xid, ok := h.x11Win(w)
	if !ok {
		return 0, 0, false
	}
	sx, sy, ok := XOrigin(disp, xid)
	if !ok {
		return 0, 0, false
	}
	var tx, ty float64
	if WM.NativeGetSurfaceTransform != nil {
		WM.NativeGetSurfaceTransform(w.window, &tx, &ty)
	}
	var p Point
	WidgetComputePoint(w.content, w.window, &Point{}, &p)
	return float64(sx)/scale + tx + float64(p.X), float64(sy)/scale + ty + float64(p.Y), true
}

// pointerWorkArea is the work area of the display the pointer is on.
func (h *Host) pointerWorkArea(disp Ptr, scale float64) flyout.Rect {
	if x, y, ok := XPointer(disp); ok {
		return h.workAreaAt(disp, float64(x)/scale, float64(y)/scale)
	}
	return h.primaryWorkArea(disp)
}

// workAreaAt is the work area of the display at x, y (points), else the
// primary display's.
func (h *Host) workAreaAt(disp Ptr, x, y float64) flyout.Rect {
	var found flyout.Rect
	ok := false
	eachMonitor(disp, func(m Ptr) bool {
		var g Rectangle
		Desk.MonitorGetGeometry(m, &g)
		if (flyout.Rect{X: float64(g.X), Y: float64(g.Y), W: float64(g.Width), H: float64(g.Height)}).Contains(x, y) {
			found, ok = workArea(disp, m), true
			return false
		}
		return true
	})
	if ok {
		return found
	}
	return h.primaryWorkArea(disp)
}

// primaryWorkArea is the primary display's work area (X11's primary
// monitor, else the first).
func (h *Host) primaryWorkArea(disp Ptr) flyout.Rect {
	if IsX11(disp) && Desk.X11DisplayGetPrimary != nil {
		if m := Desk.X11DisplayGetPrimary(disp); m != 0 {
			return workArea(disp, m)
		}
	}
	var r flyout.Rect
	eachMonitor(disp, func(m Ptr) bool {
		r = workArea(disp, m)
		return false
	})
	return r
}

// workArea is a monitor's work area (X11's, the window manager's
// _NET_WORKAREA), else its bounds; points.
func workArea(disp, m Ptr) flyout.Rect {
	var g Rectangle
	if IsX11(disp) && Desk.X11MonitorGetWorkarea != nil {
		Desk.X11MonitorGetWorkarea(m, &g)
	} else {
		Desk.MonitorGetGeometry(m, &g)
	}
	return flyout.Rect{X: float64(g.X), Y: float64(g.Y), W: float64(g.Width), H: float64(g.Height)}
}

// eachMonitor calls f with each of the display's monitors while it
// returns true.
func eachMonitor(disp Ptr, f func(m Ptr) bool) {
	if disp == 0 || Desk.DisplayGetMonitors == nil || Desk.MonitorGetGeometry == nil {
		return
	}
	list := Desk.DisplayGetMonitors(disp)
	n := ListModelGetNItems(list)
	for i := uint32(0); i < n; i++ {
		m := ListModelGetItem(list, i)
		if m == 0 {
			continue
		}
		more := f(m)
		ObjectUnref(m)
		if !more {
			return
		}
	}
}

// layerPanel makes a panel an overlay layer-shell surface, before GTK
// realizes it, where it can be one (Wayland, gtk4-layer-shell in the
// process, a compositor with wlr-layer-shell).
func (h *Host) layerPanel(w *win) {
	d := &w.desk
	if d.layer || WM.WidgetGetRealized == nil || WM.WidgetGetRealized(w.window) {
		return
	}
	disp := DisplayGetDefault()
	if !IsWayland(disp) || !LoadLayerShell() {
		return
	}
	d.layer = true
	Layer.InitForWindow(w.window)
	Layer.SetNamespace(w.window, appID(h.appName))
	Layer.SetLayer(w.window, LayerOverlay)
	Layer.SetKeyboardMode(w.window, LayerKeyboardExcl)
	if c := w.cfg; c.Flags&ops.WindowPositioned != 0 {
		// At X, Y: anchored to the top left of the output there.
		var mon Ptr
		var g Rectangle
		eachMonitor(disp, func(m Ptr) bool {
			Desk.MonitorGetGeometry(m, &g)
			if (flyout.Rect{X: float64(g.X), Y: float64(g.Y), W: float64(g.Width), H: float64(g.Height)}).Contains(float64(c.X), float64(c.Y)) {
				mon = m
				ObjectRef(m)
				return false
			}
			return true
		})
		if mon != 0 {
			Layer.SetMonitor(w.window, mon)
			ObjectUnref(mon)
			Layer.SetAnchor(w.window, LayerEdgeTop, true)
			Layer.SetAnchor(w.window, LayerEdgeLeft, true)
			Layer.SetMargin(w.window, LayerEdgeTop, int32(c.Y)-g.Y)
			Layer.SetMargin(w.window, LayerEdgeLeft, int32(c.X)-g.X)
		}
	}
	// Unanchored, the compositor centers it on the output it picks.
}

// applyClickThrough gives a realized window an empty input region while
// the mouse goes through it, and its own back after.
func (h *Host) applyClickThrough(w *win) {
	d := &w.desk
	if w.closing || WM.SurfaceSetInputRegion == nil {
		return
	}
	s := NativeGetSurface(w.window)
	if s == 0 {
		return // set as it maps
	}
	if d.through {
		r := WM.RegionCreate()
		WM.SurfaceSetInputRegion(s, r)
		WM.RegionDestroy(r)
		d.regionSet = true
		if !d.clockHook && WM.SurfaceGetFrameClock != nil {
			if clock := WM.SurfaceGetFrameClock(s); clock != 0 {
				d.clockHook = true
				Connect(clock, "after-paint", func() {
					if d.through && !w.closing {
						h.applyClickThrough(w)
					}
				})
			}
		}
		return
	}
	if d.regionSet {
		d.regionSet = false
		WM.SurfaceSetInputRegion(s, 0)
		Call(FnQueueAllocate, w.window) // GTK sets its own (a decorated window's) as it's allocated
	}
}

// setClickThrough is WindowAction ClickThrough and NotClickThrough.
func (h *Host) setClickThrough(w *win, on bool) {
	w.desk.through = on
	h.applyClickThrough(w)
}

// The tray: where its icons were clicked, and a flyout closed by the
// click on its icon.

// clickAt remembers where the panel says an icon was clicked
// (screen pixels); 0, 0 is the panel not saying.
func (t *sni) clickAt(x, y int32) {
	if x == 0 && y == 0 {
		return
	}
	t.clickX, t.clickY, t.clicked = x, y, true
}

// trayFlyoutGrace is how long after a tray icon's flyout closed as it
// lost the keyboard a click on the icon is taken as the click that
// closed it.
const trayFlyoutGrace = 400 * time.Millisecond

// trayFlyoutClosed: icon's flyout closed as it lost the keyboard.
func (h *Host) trayFlyoutClosed(icon uint32) {
	if t := h.trays().icons[icon]; t != nil {
		t.flyoutClosed = time.Now()
	}
}

// clickClosedFlyout reports whether a click on t is the one that just
// closed its flyout (taking its keyboard), and forgets it.
func (t *sni) clickClosedFlyout() bool {
	if t.flyoutClosed.IsZero() {
		return false
	}
	closed := time.Since(t.flyoutClosed) < trayFlyoutGrace
	t.flyoutClosed = time.Time{}
	return closed
}
