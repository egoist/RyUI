package screen

import (
	"encoding/json"
	"slices"

	follower "github.com/egoist/asuka/internal/follow"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Rect is a rectangle in points.
type Rect struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// Contains reports whether the point x, y is in r.
func (r Rect) Contains(x, y float32) bool {
	return x >= r.X && y >= r.Y && x < r.X+r.Width && y < r.Y+r.Height
}

// Display is a screen the device shows the app on: a Mac's built-in one
// and the monitors plugged in, a phone's own (and one it mirrors or
// extends to).
//
// Bounds and WorkArea are in points (the display's pixels over Scale),
// in the desktop's coordinates: the origin at the top left corner of the
// primary display, y down, the other displays where the user arranged
// them. Where displays have different scales (Windows), each one's
// origin is its top left corner in pixels over the smallest scale of
// the displays, and it goes on in its own scale, so displays never
// overlap (one of a larger scale than its neighbor's leaves a gap after
// it); where they share one, coordinates are the pixels over it.
type Display struct {
	// ID names the display while it's connected, and again when it comes
	// back where the system keeps one: the Mac's CGDirectDisplayID,
	// Windows's device name (\\.\DISPLAY1), the connector on Linux
	// (DP-1), Android's display ID, "main" for an iPhone's own screen.
	ID string `json:"id"`
	// Name is what the system calls it: "Built-in Retina Display",
	// "DELL U2720Q". Empty where it has no name.
	Name string `json:"name"`
	// Bounds is the whole display.
	Bounds Rect `json:"bounds"`
	// WorkArea is what windows take, less the menu bar, the Dock, the
	// taskbar and the panels. Linux on Wayland doesn't say (the
	// compositor keeps it, and places windows itself): there it's
	// Bounds. A phone's is its Bounds: the safe area is the app's
	// (ui.UseEnvironment).
	WorkArea Rect `json:"workArea"`
	// Scale is pixels per point: 2 on a Retina display, 1.5 at 150% on
	// Windows.
	Scale float32 `json:"scale"`
	// RefreshRate is how many times a second the display draws (60,
	// 120); 0 where the system doesn't say.
	RefreshRate float32 `json:"refreshRate"`
	// Primary is the main display: the one with the menu bar on a Mac,
	// the primary monitor on Windows and Linux, a phone's own screen.
	Primary bool `json:"primary"`
}

// decodeDisplays reads the host's JSON (the list, the primary one first).
func decodeDisplays(b []byte, err error) ([]Display, error) {
	if err != nil {
		return nil, err
	}
	var ds []Display
	if err := json.Unmarshal(b, &ds); err != nil {
		return nil, err
	}
	return ds, nil
}

// Displays lists the displays connected now, the primary one first. Not
// on the main thread.
func Displays() ([]Display, error) {
	return decodeDisplays(ui.CallWait("displays", "current", nil))
}

var displays = follower.Module{Name: "displays"}

// WatchDisplays calls f, on the main thread, with the displays now and
// again each time they change: one plugged in or out, the displays
// arranged, a resolution, scale or refresh rate changed, the work area
// moved (the Dock or the taskbar moving, hiding or showing, where the
// system says so), a phone turned (its display's bounds turn too); until
// stop.
func WatchDisplays(f func([]Display)) (stop func()) {
	return displays.Watch(func(b []byte) {
		if ds, err := decodeDisplays(b, nil); err == nil {
			f(ds)
		}
	})
}

// UseDisplays is the displays as a signal, for a component: it follows
// changes while the component lives. Empty until the host first answers.
// Main thread.
func UseDisplays() signal.Accessor[[]Display] {
	s := signal.NewFunc[[]Display](nil, slices.Equal)
	signal.OnCleanup(WatchDisplays(s.Set))
	return s
}

// Primary is the primary display among ds (the first, if none says it
// is); ok false: ds is empty.
func Primary(ds []Display) (d Display, ok bool) {
	for _, d := range ds {
		if d.Primary {
			return d, true
		}
	}
	if len(ds) == 0 {
		return Display{}, false
	}
	return ds[0], true
}

// Nearest is the display among ds that a point in the desktop's
// coordinates is on, or else the one nearest it (the cursor, a window's
// center); ok false: ds is empty.
func Nearest(ds []Display, x, y float32) (d Display, ok bool) {
	best := float32(-1)
	for _, c := range ds {
		if c.Bounds.Contains(x, y) {
			return c, true
		}
		dx := max(c.Bounds.X-x, 0, x-(c.Bounds.X+c.Bounds.Width))
		dy := max(c.Bounds.Y-y, 0, y-(c.Bounds.Y+c.Bounds.Height))
		if dist := dx*dx + dy*dy; best < 0 || dist < best {
			best, d, ok = dist, c, true
		}
	}
	return d, ok
}

// Matching is the display among ds that a rectangle overlaps most (the
// one a window is on), or else the one nearest its center; ok false: ds
// is empty.
func Matching(ds []Display, r Rect) (d Display, ok bool) {
	best := float32(0)
	for _, c := range ds {
		w := min(r.X+r.Width, c.Bounds.X+c.Bounds.Width) - max(r.X, c.Bounds.X)
		h := min(r.Y+r.Height, c.Bounds.Y+c.Bounds.Height) - max(r.Y, c.Bounds.Y)
		if w > 0 && h > 0 && w*h > best {
			best, d, ok = w*h, c, true
		}
	}
	if ok {
		return d, true
	}
	return Nearest(ds, r.X+r.Width/2, r.Y+r.Height/2)
}
