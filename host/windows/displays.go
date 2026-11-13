//go:build windows

package windows

import (
	"encoding/json"

	. "github.com/egoist/asuka/internal/windows"
)

// asuka/sdk/screen's displays on Windows: the monitors attached to the
// desktop (EnumDisplayMonitors, GetMonitorInfo), each at its effective
// DPI (GetDpiForMonitor), named by its EDID (the display
// configuration's friendly name; "Built-in Display" for a laptop's
// panel without one, else its adapter's description) and at its current
// mode's refresh rate. A change is WM_DISPLAYCHANGE (a monitor plugged
// in or out, a resolution), WM_SETTINGCHANGE (the work area: the
// taskbar moved or hid) or WM_DPICHANGED (a scale), which the system
// window hears; the displays are read again then, and again a moment
// later, as some changes settle after the message.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]asyncMethod) {
		m["displays"] = displays.methods()
	})
	displays.start = watchDisplays
}

var displays = watched{read: readDisplays}

// watchDisplays reads the displays again as they change.
func watchDisplays() {
	settle := func() {}
	onSystemMessage(func(m uint32, _, _ uintptr) {
		switch m {
		case WM_DISPLAYCHANGE, WM_SETTINGCHANGE, WM_DPICHANGED:
			displays.changed()
			settle()
			var stop func()
			stop = ownTimer(250, func() {
				stop()
				displays.changed()
			})
			settle = stop
		}
	})
}

// display is screen.Display's JSON.
type display struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Bounds      pointRect `json:"bounds"`
	WorkArea    pointRect `json:"workArea"`
	Scale       float32   `json:"scale"`
	RefreshRate float32   `json:"refreshRate"`
	Primary     bool      `json:"primary"`
}

type pointRect struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

func readDisplays() []byte {
	b, _ := json.Marshal(displaysInPoints(Monitors()))
	return b
}

// displaysInPoints places the monitors in points, as asuka/sdk/screen has
// them: each one's origin is its top left corner in pixels over the
// smallest scale of the monitors, and it goes on in its own scale (its
// size, its work area's place in it), so monitors never overlap.
func displaysInPoints(ms []Monitor) []display {
	out := make([]display, 0, len(ms))
	least := float32(0)
	for _, m := range ms {
		if s := dpiScale(m.DPI); least == 0 || s < least {
			least = s
		}
	}
	for _, m := range ms {
		k := dpiScale(m.DPI)
		x, y := float32(m.Bounds.Left)/least, float32(m.Bounds.Top)/least
		out = append(out, display{
			ID: m.Device, Name: m.Name,
			Bounds: pointRect{X: x, Y: y, Width: float32(m.Bounds.W()) / k, Height: float32(m.Bounds.H()) / k},
			WorkArea: pointRect{
				X: x + float32(m.Work.Left-m.Bounds.Left)/k, Y: y + float32(m.Work.Top-m.Bounds.Top)/k,
				Width: float32(m.Work.W()) / k, Height: float32(m.Work.H()) / k,
			},
			Scale: k, RefreshRate: m.Hz, Primary: m.Primary,
		})
	}
	return out
}
