//go:build darwin && !ios

package darwin

import (
	"encoding/json"
	"strconv"

	. "github.com/egoist/asuka/internal/darwin"
)

// asuka/screen's displays on a Mac: NSScreen's screens (the first is the
// one with the menu bar, the primary), read again as AppKit says they
// changed (NSApplicationDidChangeScreenParametersNotification: one
// plugged in or out, arranged, a resolution or scale changed, the Dock
// or the menu bar moving the visible frame). AppKit's global
// coordinates are points from the primary display's bottom left corner,
// y up; Asuka's are from its top left, y down.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["displays"] = displaysWatch.methods()
	})
}

var displaysWatch = watched{
	read: readDisplays,
	start: func(w *watched) {
		observeNamed(0, "NSApplicationDidChangeScreenParametersNotification", w.changed)
	},
}

// display is screen.Display's JSON.
type display struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Bounds      displayRect `json:"bounds"`
	WorkArea    displayRect `json:"workArea"`
	Scale       float64     `json:"scale"`
	RefreshRate float64     `json:"refreshRate"`
	Primary     bool        `json:"primary"`
}

type displayRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// readDisplays is the screens as JSON, the primary first.
func readDisplays() []byte {
	screens := ArrayItems(Send(Class("NSScreen"), "screens"))
	out := make([]display, 0, len(screens))
	var primaryH float64
	for i, s := range screens {
		f := MsgRect(s, Sel("frame"))
		if i == 0 {
			primaryH = f.Size.Height
		}
		flip := func(r Rect) displayRect {
			return displayRect{X: r.Origin.X, Y: primaryH - r.Origin.Y - r.Size.Height, Width: r.Size.Width, Height: r.Size.Height}
		}
		d := display{
			Bounds:   flip(f),
			WorkArea: flip(MsgRect(s, Sel("visibleFrame"))),
			Scale:    MsgFloat(s, Sel("backingScaleFactor")),
			Primary:  i == 0,
		}
		if n := Send(Send(s, "deviceDescription"), "objectForKey:", uintptr(String("NSScreenNumber"))); n != 0 {
			d.ID = strconv.FormatUint(uint64(uint32(SendInt(n, "unsignedIntValue"))), 10)
		}
		if RespondsTo(s, "localizedName") {
			d.Name = GoString(Send(s, "localizedName"))
		}
		if RespondsTo(s, "maximumFramesPerSecond") {
			d.RefreshRate = float64(SendInt(s, "maximumFramesPerSecond"))
		}
		out = append(out, d)
	}
	b, _ := json.Marshal(out)
	return b
}
