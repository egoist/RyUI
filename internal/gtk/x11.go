//go:build linux && !android

package gtk

import "sync"

// Xlib, for what GDK doesn't do on an X server: grabbing a key on the
// root window (a global shortcut). GDK's X11 backend gives the Display
// and traps the errors; its GdkX11Display's "xevent" signal gives the
// events. Opened the first time it's used; false where GDK isn't on X11
// (Wayland) or libX11 isn't there.

var X11 struct {
	StringToKeysym     func(name string) uint64
	KeysymToKeycode    func(dpy Ptr, keysym uint64) uint8
	KeycodeToKeysym    func(dpy Ptr, keycode uint32, group, level int32) uint64 // XkbKeycodeToKeysym
	GrabKey            func(dpy Ptr, keycode int32, mods uint32, window uint64, ownerEvents bool, pointerMode, keyboardMode int32) int32
	UngrabKey          func(dpy Ptr, keycode int32, mods uint32, window uint64) int32
	DefaultRootWindow  func(dpy Ptr) uint64
	Flush              func(dpy Ptr) int32
	DisplayType        func() GType // gdk_x11_display_get_type
	XDisplay           func(display Ptr) Ptr
	ErrorTrapPush      func(display Ptr)
	ErrorTrapPop       func(display Ptr) int32
	WaylandDisplayType func() GType // gdk_wayland_display_get_type, nil without the backend
}

var (
	x11Once sync.Once
	x11OK   bool
)

// LoadX11 binds Xlib and GDK's X11 functions, once; false if either is
// missing.
func LoadX11() bool {
	x11Once.Do(func() {
		lib, err := modOpenLib("libX11.so.6", "libX11.so")
		if err != nil {
			return
		}
		x := &X11
		if modBindLib(lib, []binding{
			{&x.StringToKeysym, "XStringToKeysym"},
			{&x.KeysymToKeycode, "XKeysymToKeycode"},
			{&x.KeycodeToKeysym, "XkbKeycodeToKeysym"},
			{&x.GrabKey, "XGrabKey"},
			{&x.UngrabKey, "XUngrabKey"},
			{&x.DefaultRootWindow, "XDefaultRootWindow"},
			{&x.Flush, "XFlush"},
		}) != nil {
			return
		}
		for _, b := range []binding{
			{&x.DisplayType, "gdk_x11_display_get_type"},
			{&x.XDisplay, "gdk_x11_display_get_xdisplay"},
			{&x.ErrorTrapPush, "gdk_x11_display_error_trap_push"},
			{&x.ErrorTrapPop, "gdk_x11_display_error_trap_pop"},
		} {
			if Sym(b.name) == 0 {
				return // GTK built without its X11 backend
			}
			fn(b.p, b.name)
		}
		x11OK = true
	})
	return x11OK
}

// IsX11 reports whether a GdkDisplay is GDK's X11 backend's.
func IsX11(display Ptr) bool {
	return display != 0 && LoadX11() && TypeCheckInstanceIsA(display, X11.DisplayType())
}

// IsWayland reports whether a GdkDisplay is GDK's Wayland backend's.
func IsWayland(display Ptr) bool {
	if display == 0 || !Has("gdk_wayland_display_get_type") {
		return false
	}
	if X11.WaylandDisplayType == nil {
		fn(&X11.WaylandDisplayType, "gdk_wayland_display_get_type")
	}
	return TypeCheckInstanceIsA(display, X11.WaylandDisplayType())
}

var waylandSetStartupID func(display Ptr, id *byte)

// WaylandSetStartupID sets the XDG activation token the window the
// display presents or focuses next takes the focus with
// (gdk_wayland_display_set_startup_notification_id, which copies it);
// "" clears it. Nothing off Wayland.
func WaylandSetStartupID(display Ptr, token string) {
	if !IsWayland(display) || !Has("gdk_wayland_display_set_startup_notification_id") {
		return
	}
	if waylandSetStartupID == nil {
		fn(&waylandSetStartupID, "gdk_wayland_display_set_startup_notification_id")
	}
	var p *byte
	if token != "" {
		p = CString(token)
	}
	waylandSetStartupID(display, p)
}
