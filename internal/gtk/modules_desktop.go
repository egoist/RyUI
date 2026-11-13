//go:build linux && !android

package gtk

// What the host's desktop modules (asuka/screen's displays, asuka/power,
// asuka/dock) call of GDK and GIO beyond the rest: the display's monitors
// (GdkMonitor; its scale and description are GTK 4.14's and 4.10's, nil
// before), X11's primary monitor, work area and urgency hint (GDK's X11
// backend, in libgtk-4; nil where GTK was built without it), and GIO's
// power profile monitor (2.70).

// Rectangle is a GdkRectangle.
type Rectangle struct{ X, Y, Width, Height int32 }

// Desk are the functions, bound at Load; a missing one stays nil.
var Desk struct {
	DisplayGetMonitors     func(d Ptr) Ptr
	MonitorGetGeometry     func(m Ptr, r *Rectangle)
	MonitorGetScale        func(m Ptr) float64
	MonitorGetScaleFactor  func(m Ptr) int32
	MonitorGetRefreshRate  func(m Ptr) int32
	MonitorGetConnector    func(m Ptr) Ptr
	MonitorGetDescription  func(m Ptr) Ptr
	MonitorGetManufacturer func(m Ptr) Ptr
	MonitorGetModel        func(m Ptr) Ptr
	X11DisplayGetPrimary   func(d Ptr) Ptr
	X11MonitorGetWorkarea  func(m Ptr, r *Rectangle)
	X11SurfaceSetUrgency   func(s Ptr, urgent bool)
	PowerProfileMonitorDup func() Ptr
	PowerProfileSaverOn    func(m Ptr) bool
}

func init() {
	d := &Desk
	bindings = append(bindings,
		binding{&d.DisplayGetMonitors, "gdk_display_get_monitors"},
		binding{&d.MonitorGetGeometry, "gdk_monitor_get_geometry"},
		binding{&d.MonitorGetScale, "gdk_monitor_get_scale"},
		binding{&d.MonitorGetScaleFactor, "gdk_monitor_get_scale_factor"},
		binding{&d.MonitorGetRefreshRate, "gdk_monitor_get_refresh_rate"},
		binding{&d.MonitorGetConnector, "gdk_monitor_get_connector"},
		binding{&d.MonitorGetDescription, "gdk_monitor_get_description"},
		binding{&d.MonitorGetManufacturer, "gdk_monitor_get_manufacturer"},
		binding{&d.MonitorGetModel, "gdk_monitor_get_model"},
		binding{&d.X11DisplayGetPrimary, "gdk_x11_display_get_primary_monitor"},
		binding{&d.X11MonitorGetWorkarea, "gdk_x11_monitor_get_workarea"},
		binding{&d.X11SurfaceSetUrgency, "gdk_x11_surface_set_urgency_hint"},
		binding{&d.PowerProfileMonitorDup, "g_power_profile_monitor_dup_default"},
		binding{&d.PowerProfileSaverOn, "g_power_profile_monitor_get_power_saver_enabled"},
	)
}

// IsA reports whether o is an instance of the type named name (false
// when no such type is registered: a backend GDK wasn't built with).
func IsA(o Ptr, name string) bool {
	if o == 0 {
		return false
	}
	t := TypeFromName(name)
	return t != 0 && TypeCheckInstanceIsA(o, t)
}
