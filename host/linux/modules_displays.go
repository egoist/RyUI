//go:build linux && !android

package linux

import (
	"encoding/json"
	"errors"
	"math"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sdk/screen's displays on Linux: the default GdkDisplay's monitors
// (gdk_display_get_monitors, which changes as one's plugged in or out),
// each one's geometry in application pixels (points, in the desktop's
// layout), its scale (fractional, GTK 4.14), refresh rate, connector
// (the ID: DP-1, eDP-1) and description. GTK 4 has no primary monitor of
// its own: on X11 it's the X server's (gdk_x11_display_get_primary_monitor)
// and the work area the window manager's (_NET_WORKAREA, through
// gdk_x11_monitor_get_workarea); on Wayland the compositor keeps both,
// so the work area is the bounds, and the primary monitor is GNOME's
// (Mutter's DisplayConfig, its logical monitor marked primary), else
// the one at the layout's origin, else the first.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["displays"] = dispw.w.methods(h.startDisplays)
	})
}

type displayWatch struct {
	w        watched
	started  bool
	list     Ptr          // the display's monitors (GListModel)
	monitors map[Ptr]bool // those whose changes are followed (a reference each)
	primary  string       // Mutter's primary monitor's connector, "" if not known
	mutter   bool         // Mutter's state is being asked
}

var dispw displayWatch

// display is screen.Display's JSON.
type display struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Bounds      displayRect `json:"bounds"`
	WorkArea    displayRect `json:"workArea"`
	Scale       float32     `json:"scale"`
	RefreshRate float32     `json:"refreshRate"`
	Primary     bool        `json:"primary"`
}

type displayRect struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

func rectOf(r Rectangle) displayRect {
	return displayRect{X: float32(r.X), Y: float32(r.Y), Width: float32(r.Width), Height: float32(r.Height)}
}

// startDisplays follows the monitors, once.
func (h *Host) startDisplays() error {
	d := &dispw
	if d.started {
		return nil
	}
	disp := DisplayGetDefault()
	if disp == 0 || Desk.DisplayGetMonitors == nil {
		return errors.New("asuka/sdk/screen: no display")
	}
	d.started = true
	d.monitors = map[Ptr]bool{}
	d.list = Desk.DisplayGetMonitors(disp)
	Connect(d.list, "items-changed", func(_, _, _ uintptr) { h.readDisplays() })
	if !IsA(disp, "GdkX11Display") {
		// GNOME on Wayland says which monitor is primary.
		if conn, err := Bus(BusSession); err == nil {
			DBusSubscribe(conn, "org.gnome.Mutter.DisplayConfig", "org.gnome.Mutter.DisplayConfig", "MonitorsChanged", "/org/gnome/Mutter/DisplayConfig", func(DBusSignal) {
				h.askMutter()
			})
			h.askMutter()
		}
	}
	h.readDisplays()
	return nil
}

// askMutter reads GNOME's primary monitor (GetCurrentState's logical
// monitors: x, y, scale, transform, primary, monitors (connector, vendor,
// product, serial), properties), then the displays again.
func (h *Host) askMutter() {
	d := &dispw
	conn, err := Bus(BusSession)
	if err != nil || d.mutter {
		return
	}
	d.mutter = true
	DBusCall(conn, "org.gnome.Mutter.DisplayConfig", "/org/gnome/Mutter/DisplayConfig", "org.gnome.Mutter.DisplayConfig", "GetCurrentState",
		0, 2000, func(r Ptr, err error) {
			d.mutter = false
			d.primary = ""
			if err == nil {
				d.primary = mutterPrimary(VariantGo(r))
			}
			h.readDisplays()
		})
}

// mutterPrimary is the connector of GetCurrentState's primary logical
// monitor's first monitor ("" if none says).
func mutterPrimary(state any) string {
	a, _ := state.([]any)
	if len(a) < 3 {
		return ""
	}
	logical, _ := a[2].([]any)
	for _, l := range logical {
		lm, _ := l.([]any)
		if len(lm) < 6 {
			continue
		}
		if p, _ := lm[4].(bool); !p {
			continue
		}
		mons, _ := lm[5].([]any)
		for _, m := range mons {
			if spec, _ := m.([]any); len(spec) > 0 {
				if c, _ := spec[0].(string); c != "" {
					return c
				}
			}
		}
	}
	return ""
}

// readDisplays reads the monitors, follows the new ones' changes, and
// answers who waits.
func (h *Host) readDisplays() {
	d := &dispw
	disp := DisplayGetDefault()
	x11 := IsA(disp, "GdkX11Display")
	var x11Primary Ptr
	if x11 && Desk.X11DisplayGetPrimary != nil {
		x11Primary = Desk.X11DisplayGetPrimary(disp)
	}
	n := Sys.ListModelGetNItems(d.list)
	seen := make(map[Ptr]bool, n)
	var out []display
	var mons []Ptr
	for i := range n {
		m := Sys.ListModelGetItem(d.list, i)
		if m == 0 {
			continue
		}
		seen[m] = true
		if !d.monitors[m] {
			d.monitors[m] = true
			ObjectRef(m)
			for _, s := range []string{"notify::geometry", "notify::scale", "notify::scale-factor", "notify::refresh-rate", "notify::description"} {
				Connect(m, s, func(uintptr) { h.readDisplays() })
			}
		}
		mons = append(mons, m)
		out = append(out, monitorDisplay(m, x11))
		ObjectUnref(m)
	}
	for m := range d.monitors {
		if !seen[m] {
			delete(d.monitors, m)
			ObjectUnref(m) // its handlers go with it
		}
	}
	// The primary one.
	primary := -1
	for i, m := range mons {
		if x11 && m == x11Primary || !x11 && d.primary != "" && out[i].ID == d.primary {
			primary = i
			break
		}
	}
	if primary < 0 {
		for i := range out {
			if out[i].Bounds.X == 0 && out[i].Bounds.Y == 0 {
				primary = i
				break
			}
		}
	}
	if primary < 0 && len(out) > 0 {
		primary = 0
	}
	if primary > 0 {
		p := out[primary]
		copy(out[1:primary+1], out[:primary])
		out[0] = p
	}
	if len(out) > 0 {
		out[0].Primary = true
	}
	if out == nil {
		out = []display{}
	}
	b, err := json.Marshal(out)
	if err != nil {
		return
	}
	if string(b) != string(d.w.state) {
		d.w.set(b)
	}
}

// monitorDisplay describes a monitor (not yet whether it's primary).
func monitorDisplay(m Ptr, x11 bool) display {
	var g Rectangle
	Desk.MonitorGetGeometry(m, &g)
	o := display{Bounds: rectOf(g), WorkArea: rectOf(g)}
	o.ID = GoString(Desk.MonitorGetConnector(m))
	switch {
	case Desk.MonitorGetDescription != nil && Desk.MonitorGetDescription(m) != 0:
		o.Name = GoString(Desk.MonitorGetDescription(m))
	default:
		maker, model := GoString(Desk.MonitorGetManufacturer(m)), GoString(Desk.MonitorGetModel(m))
		o.Name = maker
		if model != "" {
			if o.Name != "" {
				o.Name += " "
			}
			o.Name += model
		}
	}
	if Desk.MonitorGetScale != nil {
		o.Scale = float32(Desk.MonitorGetScale(m))
	}
	if o.Scale <= 0 {
		o.Scale = float32(Desk.MonitorGetScaleFactor(m))
	}
	if hz := Desk.MonitorGetRefreshRate(m); hz > 0 {
		o.RefreshRate = float32(math.Round(float64(hz)/10) / 100) // milli-Hertz, to 0.01
	}
	if x11 && Desk.X11MonitorGetWorkarea != nil {
		var w Rectangle
		Desk.X11MonitorGetWorkarea(m, &w)
		if w.Width > 0 && w.Height > 0 {
			o.WorkArea = rectOf(w)
		}
	}
	return o
}
