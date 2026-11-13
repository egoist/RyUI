//go:build linux && !android

package linux

import (
	"encoding/binary"
	"math"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/sdk/dock on Linux. The progress, like notify's badge, is the launcher
// API's (com.canonical.Unity.LauncherEntry, an Update signal on the
// session bus naming the app's desktop file): Dash to Dock and Ubuntu's
// dock, KDE Plasma's task manager and others draw it over the app's
// icon; GNOME's own dash shows none. Every Update carries the whole
// entry (count, progress, urgency), so one doesn't undo another in a
// dock that takes an Update for the whole state. Indeterminate is a full
// bar; paused and error show as normal (the API has no states).
//
// Attention, asked while none of the app's windows is active: the
// entry's urgent flag (the dock's icon highlighted), and the window in
// use (the one active last) marked urgent on X11 (its urgency hint, the
// taskbar's highlight), or presented on Wayland, where GTK 4 has no
// urgency: an activation without the user's token, which GNOME and KDE
// don't grant but show as a notification that the app is ready (a click
// on it brings the window), and which can't be withdrawn. It ends as
// the app's window becomes active, or on cancel.

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["dock"] = h.dockModule()
	})
}

// launcher is the app's launcher entry.
type launcher struct {
	path     string
	count    int
	progress float64
	showing  bool // progress
	urgent   bool
}

var launch launcher

// updateLauncher emits the entry's whole state.
func (h *Host) updateLauncher() error {
	conn, err := Bus(BusSession)
	if err != nil {
		return err
	}
	l := &launch
	if l.path == "" {
		l.path = "/" + strings.ReplaceAll(h.appID, ".", "/") + "/LauncherEntry"
	}
	return DBusEmit(conn, l.path, "com.canonical.Unity.LauncherEntry", "Update", VTuple(
		VStr("application://"+h.appID+".desktop"),
		VDict(map[string]Ptr{
			"count": VI64(int64(l.count)), "count-visible": VBool(l.count > 0),
			"progress": VDouble(l.progress), "progress-visible": VBool(l.showing),
			"urgent": VBool(l.urgent),
		}),
	))
}

type attention struct {
	id      uint32 // the request's, 0: none
	next    uint32
	surface Ptr            // the X11 surface marked urgent
	windows map[Ptr]uint64 // the windows followed till one's active: their handlers
}

var attn attention

// appActive reports whether one of the app's windows is the active one.
func (h *Host) appActive() bool {
	for w := range h.wins {
		if WindowIsActive(w) {
			return true
		}
	}
	return h.main != nil && WindowIsActive(h.main.window)
}

func (h *Host) dockModule() map[string]method {
	return map[string]method{
		// args: u8 state (0 none, 1 normal, 2 indeterminate, 3 paused, 4
		// error), f64 value.
		"progress": func(args []byte, reply func([]byte, error)) {
			l := &launch
			state := byte(0)
			v := 0.0
			if len(args) >= 9 {
				state = args[0]
				v = math.Float64frombits(binary.LittleEndian.Uint64(args[1:]))
			}
			l.showing = state != 0
			l.progress = min(max(v, 0), 1)
			if state == 2 {
				l.progress = 1
			}
			reply(nil, h.updateLauncher())
		},
		// args: u8 critical; result: u32 the request's ID (0: the app is
		// in use, nothing asked).
		"attention": func(_ []byte, reply func([]byte, error)) {
			if h.appActive() {
				reply(binary.LittleEndian.AppendUint32(nil, 0), nil)
				return
			}
			h.endAttention()
			a := &attn
			a.next++
			a.id = a.next
			launch.urgent = true
			h.updateLauncher()
			w := h.activeWin
			if w == nil {
				w = h.main
			}
			if w != nil {
				surface := NativeGetSurface(w.window)
				if IsA(surface, "GdkX11Surface") && Desk.X11SurfaceSetUrgency != nil {
					Desk.X11SurfaceSetUrgency(surface, true)
					a.surface = surface
					ObjectRef(surface)
				} else if surface != 0 {
					WindowPresent(w.window)
				}
			}
			// Ended as a window of the app's becomes active.
			a.windows = map[Ptr]uint64{}
			for win := range h.wins {
				win := win
				a.windows[win] = Connect(win, "notify::is-active", func(uintptr) {
					if WindowIsActive(win) {
						h.endAttention()
					}
				})
			}
			reply(binary.LittleEndian.AppendUint32(nil, a.id), nil)
		},
		// args: u32 the request's ID.
		"cancelAttention": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 && binary.LittleEndian.Uint32(args) == attn.id {
				h.endAttention()
			}
			reply(nil, nil)
		},
	}
}

// endAttention takes the request back, if there's one.
func (h *Host) endAttention() {
	a := &attn
	if a.id == 0 {
		return
	}
	a.id = 0
	for w, handler := range a.windows {
		if h.wins[w] != nil {
			Disconnect(w, handler)
		}
	}
	a.windows = nil
	if a.surface != 0 {
		Desk.X11SurfaceSetUrgency(a.surface, false)
		ObjectUnref(a.surface)
		a.surface = 0
	}
	if launch.urgent {
		launch.urgent = false
		h.updateLauncher()
	}
}
