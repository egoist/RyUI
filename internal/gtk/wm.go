//go:build linux && !android

package gtk

import (
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// What the host's desktop windows (panels, flyouts, windows sized to
// their content, click-through windows) call beyond the rest: GTK's and
// GDK's (an input region, a surface's transform, a realized window), the
// X11 backend's hints GTK 4 still has (gdk_x11_surface_set_skip_*_hint),
// and Xlib's for what GTK 4 dropped: moving a window, the EWMH states it
// no longer sets (above, sticky), the window type, where the pointer and
// a window are. gtk4-layer-shell, where the app was started with it
// (LD_PRELOAD), makes a Wayland panel an overlay surface.

// WM are GTK's and GDK's, bound at Load; a missing one stays nil.
var WM struct {
	WidgetRealize              func(w Ptr)
	WidgetGetRealized          func(w Ptr) bool
	WindowGetTransientFor      func(w Ptr) Ptr
	NativeGetSurfaceTransform  func(n Ptr, x, y *float64)
	SurfaceSetInputRegion      func(s Ptr, region Ptr)
	SurfaceGetFrameClock       func(s Ptr) Ptr
	RegionCreate               func() Ptr
	RegionCreateRectangle      func(r *Rectangle) Ptr
	RegionDestroy              func(r Ptr)
	DisplayGetMonitorAtSurface func(d, s Ptr) Ptr

	X11SurfaceGetXID            func(s Ptr) uint64
	X11SurfaceSetSkipTaskbar    func(s Ptr, skip bool)
	X11SurfaceSetSkipPager      func(s Ptr, skip bool)
	X11DisplayGetXRootWindow    func(d Ptr) uint64
	X11GetXAtomByNameForDisplay func(d Ptr, name string) uint64
}

func init() {
	w := &WM
	bindings = append(bindings,
		binding{&w.WidgetRealize, "gtk_widget_realize"},
		binding{&w.WidgetGetRealized, "gtk_widget_get_realized"},
		binding{&w.WindowGetTransientFor, "gtk_window_get_transient_for"},
		binding{&w.NativeGetSurfaceTransform, "gtk_native_get_surface_transform"},
		binding{&w.SurfaceSetInputRegion, "gdk_surface_set_input_region"},
		binding{&w.SurfaceGetFrameClock, "gdk_surface_get_frame_clock"},
		binding{&w.RegionCreate, "cairo_region_create"},
		binding{&w.RegionCreateRectangle, "cairo_region_create_rectangle"},
		binding{&w.RegionDestroy, "cairo_region_destroy"},
		binding{&w.DisplayGetMonitorAtSurface, "gdk_display_get_monitor_at_surface"},
		binding{&w.X11SurfaceGetXID, "gdk_x11_surface_get_xid"},
		binding{&w.X11SurfaceSetSkipTaskbar, "gdk_x11_surface_set_skip_taskbar_hint"},
		binding{&w.X11SurfaceSetSkipPager, "gdk_x11_surface_set_skip_pager_hint"},
		binding{&w.X11DisplayGetXRootWindow, "gdk_x11_display_get_xrootwindow"},
		binding{&w.X11GetXAtomByNameForDisplay, "gdk_x11_get_xatom_by_name_for_display"},
	)
}

// Xlib's, bound by LoadXWM.
var XWM struct {
	MoveWindow           func(dpy Ptr, w uint64, x, y int32) int32
	TranslateCoordinates func(dpy Ptr, src, dst uint64, x, y int32, dx, dy *int32, child *uint64) bool
	QueryPointer         func(dpy Ptr, w uint64, root, child *uint64, rx, ry, wx, wy *int32, mask *uint32) bool
	ChangeProperty       func(dpy Ptr, w, prop, typ uint64, format, mode int32, data unsafe.Pointer, n int32) int32
	DeleteProperty       func(dpy Ptr, w, prop uint64) int32
	GetWindowProperty    func(dpy Ptr, w, prop uint64, offset, length int64, del bool, reqType uint64, actualType *uint64, actualFormat *int32, n, after *uint64, data *unsafe.Pointer) int32
	SendEvent            func(dpy Ptr, w uint64, propagate bool, mask int64, ev unsafe.Pointer) int32
	Free                 func(p unsafe.Pointer) int32
	Sync                 func(dpy Ptr, discard bool) int32
	// ShapeGetRectangles is libXext's (the SHAPE extension), nil without
	// it: what a window's input shape is, for tests.
	ShapeGetRectangles func(dpy Ptr, w uint64, kind int32, count, ordering *int32) unsafe.Pointer
}

var (
	xwmOnce sync.Once
	xwmOK   bool
)

// LoadXWM binds the Xlib functions above (LoadX11's too), once; false
// where GDK isn't built for X11 or libX11 is missing.
func LoadXWM() bool {
	xwmOnce.Do(func() {
		if !LoadX11() || WM.X11SurfaceGetXID == nil || WM.X11GetXAtomByNameForDisplay == nil || WM.X11DisplayGetXRootWindow == nil {
			return
		}
		lib, err := modOpenLib("libX11.so.6", "libX11.so")
		if err != nil {
			return
		}
		x := &XWM
		if modBindLib(lib, []binding{
			{&x.MoveWindow, "XMoveWindow"},
			{&x.TranslateCoordinates, "XTranslateCoordinates"},
			{&x.QueryPointer, "XQueryPointer"},
			{&x.ChangeProperty, "XChangeProperty"},
			{&x.DeleteProperty, "XDeleteProperty"},
			{&x.GetWindowProperty, "XGetWindowProperty"},
			{&x.SendEvent, "XSendEvent"},
			{&x.Free, "XFree"},
			{&x.Sync, "XSync"},
		}) != nil {
			return
		}
		if ext, err := modOpenLib("libXext.so.6", "libXext.so"); err == nil {
			modBindLib(ext, []binding{{&x.ShapeGetRectangles, "XShapeGetRectangles"}})
		}
		xwmOK = true
	})
	return xwmOK
}

// XAtom is the X atom named name on a GdkX11Display (GDK caches them).
func XAtom(display Ptr, name string) uint64 {
	return WM.X11GetXAtomByNameForDisplay(display, name)
}

// XPropertyAtoms reads a window's property of atoms (or cardinals: a
// format 32 property's items, C longs); ok false where it has none.
func XPropertyAtoms(display Ptr, w uint64, prop string) (out []uint64, ok bool) {
	dpy := X11.XDisplay(display)
	var typ, n, after uint64
	var format int32
	var data unsafe.Pointer
	X11.ErrorTrapPush(display)
	r := XWM.GetWindowProperty(dpy, w, XAtom(display, prop), 0, 1024, false, 0 /* AnyPropertyType */, &typ, &format, &n, &after, &data)
	X11.ErrorTrapPop(display)
	if r != 0 || typ == 0 {
		if data != nil {
			XWM.Free(data)
		}
		return nil, false
	}
	if format == 32 && n > 0 {
		// C longs, a 32-bit value each (Xlib sign-extends them).
		for _, v := range unsafe.Slice((*uint64)(data), n) {
			out = append(out, v&0xFFFFFFFF)
		}
	}
	if data != nil {
		XWM.Free(data)
	}
	return out, true
}

// XSetAtoms replaces a window's property with atoms of type typ ("ATOM",
// "CARDINAL"), as format 32 (C longs).
func XSetAtoms(display Ptr, w uint64, prop, typ string, vals []uint64) {
	dpy := X11.XDisplay(display)
	X11.ErrorTrapPush(display)
	var p unsafe.Pointer
	if len(vals) > 0 {
		p = unsafe.Pointer(&vals[0])
	}
	XWM.ChangeProperty(dpy, w, XAtom(display, prop), XAtom(display, typ), 32, 0 /* PropModeReplace */, p, int32(len(vals)))
	X11.ErrorTrapPop(display)
}

// XClientMessage sends a format 32 client message about w to the root
// window, as EWMH asks a mapped window's changes of the window manager
// (_NET_WM_STATE, _NET_WM_DESKTOP).
func XClientMessage(display Ptr, w uint64, msg string, data [5]uint64) {
	dpy := X11.XDisplay(display)
	// XClientMessageEvent (LP64): type, serial, send_event, display,
	// window, message_type, format, then data.l[5]; XEvent is 24 longs.
	var ev [24]uint64
	*(*int32)(unsafe.Pointer(&ev[0])) = 33 // ClientMessage
	ev[4] = w
	ev[5] = XAtom(display, msg)
	*(*int32)(unsafe.Pointer(&ev[6])) = 32
	copy(ev[7:12], data[:])
	X11.ErrorTrapPush(display)
	const substructure = 1<<19 | 1<<20 // SubstructureNotifyMask | SubstructureRedirectMask
	XWM.SendEvent(dpy, WM.X11DisplayGetXRootWindow(display), false, substructure, unsafe.Pointer(&ev[0]))
	X11.Flush(dpy)
	X11.ErrorTrapPop(display)
}

// XWindowManaged reports whether a window manager runs that follows
// EWMH (_NET_SUPPORTING_WM_CHECK on the root window).
func XWindowManaged(display Ptr) bool {
	v, ok := XPropertyAtoms(display, WM.X11DisplayGetXRootWindow(display), "_NET_SUPPORTING_WM_CHECK")
	return ok && len(v) > 0 && v[0] != 0
}

// XMove moves a window to x, y on the screen (pixels).
func XMove(display Ptr, w uint64, x, y int32) {
	dpy := X11.XDisplay(display)
	X11.ErrorTrapPush(display)
	XWM.MoveWindow(dpy, w, x, y)
	X11.Flush(dpy)
	X11.ErrorTrapPop(display)
}

// XOrigin is where a window's top left corner is on the screen (pixels).
func XOrigin(display Ptr, w uint64) (x, y int32, ok bool) {
	dpy := X11.XDisplay(display)
	var child uint64
	X11.ErrorTrapPush(display)
	ok = XWM.TranslateCoordinates(dpy, w, WM.X11DisplayGetXRootWindow(display), 0, 0, &x, &y, &child)
	ok = X11.ErrorTrapPop(display) == 0 && ok
	return x, y, ok
}

// XPointer is where the pointer is on the screen (pixels).
func XPointer(display Ptr) (x, y int32, ok bool) {
	dpy := X11.XDisplay(display)
	var root, child uint64
	var wx, wy int32
	var mask uint32
	ok = XWM.QueryPointer(dpy, WM.X11DisplayGetXRootWindow(display), &root, &child, &x, &y, &wx, &wy, &mask)
	return x, y, ok
}

// XSync waits for the X server to have done what was asked of it.
func XSync(display Ptr) { XWM.Sync(X11.XDisplay(display), false) }

// XInputShape is the rectangles of a window's input shape (SHAPE's
// ShapeInput), x, y, width, height each; ok false without libXext.
func XInputShape(display Ptr, w uint64) (rects [][4]int16, ok bool) {
	if XWM.ShapeGetRectangles == nil {
		return nil, false
	}
	var n, ordering int32
	X11.ErrorTrapPush(display)
	p := XWM.ShapeGetRectangles(X11.XDisplay(display), w, 2 /* ShapeInput */, &n, &ordering)
	X11.ErrorTrapPop(display)
	if p == nil {
		return nil, true
	}
	// XRectangle: short x, y; unsigned short width, height.
	for _, r := range unsafe.Slice((*[4]int16)(p), n) {
		rects = append(rects, r)
	}
	XWM.Free(p)
	return rects, true
}

// gtk4-layer-shell, where the process has it already: it must come
// before libwayland-client to work (it wraps its calls), so it's only
// used when the app was linked or preloaded with it (LD_PRELOAD), never
// opened late.
var Layer struct {
	IsSupported     func() bool
	InitForWindow   func(w Ptr)
	SetNamespace    func(w Ptr, ns string)
	SetLayer        func(w Ptr, layer int32)
	SetKeyboardMode func(w Ptr, mode int32)
	SetAnchor       func(w Ptr, edge int32, anchor bool)
	SetMargin       func(w Ptr, edge, margin int32)
	SetMonitor      func(w Ptr, monitor Ptr)
}

var (
	layerOnce sync.Once
	layerOK   bool
)

// Layer shell's values.
const (
	LayerOverlay      = 3 // GTK_LAYER_SHELL_LAYER_OVERLAY
	LayerKeyboardExcl = 1 // GTK_LAYER_SHELL_KEYBOARD_MODE_EXCLUSIVE
	LayerEdgeLeft     = 0
	LayerEdgeRight    = 1
	LayerEdgeTop      = 2
	LayerEdgeBottom   = 3
)

// LoadLayerShell binds gtk4-layer-shell if the process has it loaded and
// the compositor speaks wlr-layer-shell (KDE Plasma, Sway, Hyprland,
// COSMIC; not GNOME), once.
func LoadLayerShell() bool {
	layerOnce.Do(func() {
		const noLoad = 0x4 // RTLD_NOLOAD: only if it's there already
		var lib uintptr
		for _, name := range []string{"libgtk4-layer-shell.so.0", "libgtk4-layer-shell.so"} {
			if l, err := purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL|noLoad); err == nil {
				lib = l
				break
			}
		}
		if lib == 0 {
			return
		}
		l := &Layer
		if modBindLib(lib, []binding{
			{&l.IsSupported, "gtk_layer_is_supported"},
			{&l.InitForWindow, "gtk_layer_init_for_window"},
			{&l.SetNamespace, "gtk_layer_set_namespace"},
			{&l.SetLayer, "gtk_layer_set_layer"},
			{&l.SetKeyboardMode, "gtk_layer_set_keyboard_mode"},
			{&l.SetAnchor, "gtk_layer_set_anchor"},
			{&l.SetMargin, "gtk_layer_set_margin"},
			{&l.SetMonitor, "gtk_layer_set_monitor"},
		}) != nil {
			return
		}
		layerOK = l.IsSupported()
	})
	return layerOK
}
