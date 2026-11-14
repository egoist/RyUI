package fake

import (
	"fmt"

	"github.com/egoist/asuka/ops"
)

// A Router's addresses, as the web host keeps them: each window's (the
// root's, a Window node's) Location commands drive a browser's history of
// its own, which a test goes back and forward in as the user does with a
// browser's buttons (GoBack, GoForward: the Location event, with the
// state the entry kept); and, as a desktop host keeps where a window was,
// the last one is kept by the window's key (Locations), which the next
// Router mounted on the host starts at (host.Locator).

// HistoryEntry is an entry of a window's history.
type HistoryEntry struct {
	Href  string
	State []byte
}

// History is a window's browser history.
type History struct {
	Entries []HistoryEntry
	Index   int // the entry shown
	// Commands are the Location commands it was sent, in order.
	Commands []ops.Location
}

// Current is the entry shown ("" when there's none).
func (hi *History) Current() HistoryEntry {
	if hi == nil || hi.Index < 0 || hi.Index >= len(hi.Entries) {
		return HistoryEntry{}
	}
	return hi.Entries[hi.Index]
}

// Hrefs are the entries' hrefs, oldest first.
func (hi *History) Hrefs() []string {
	var out []string
	if hi != nil {
		for _, e := range hi.Entries {
			out = append(out, e.Href)
		}
	}
	return out
}

// Saved is where a window was left: what Location gives back, by the
// window's key ("main" for the main window, or its StateKey).
type Saved struct {
	Href  string
	State []byte
}

// History returns the history of the window v is (the root, a Window
// node); nil when it has none yet.
func (h *Host) History(v *View) *History {
	if v == nil {
		v = h.Root()
	}
	if v == nil {
		return nil
	}
	return h.histories[v.ID]
}

// Location implements host.Locator: where the window keyed window was
// left (Locations).
func (h *Host) Location(window string) (string, []byte, bool) {
	s, ok := h.Locations[window]
	return s.Href, s.State, ok
}

// windowKey is the key of the window v is: "main" for the root (or its
// options' StateKey), a Window's StateKey.
func (h *Host) windowKey(v *View) string {
	if v.Kind == ops.KindWindow {
		if w := h.wins[v.ID]; w != nil {
			return w.Config.StateKey
		}
		return ""
	}
	if w := h.wins[v.ID]; w != nil && w.Config.StateKey != "" {
		return w.Config.StateKey
	}
	return "main"
}

// location applies a Location command to the window v's history, as a
// browser's pushState, replaceState and history.go do.
func (h *Host) location(v *View, args []byte) {
	l, ok := ops.DecodeLocation(args)
	if !ok {
		panic("fake: Location: its args don't decode")
	}
	if v.Kind != ops.KindView && v.Kind != ops.KindWindow {
		panic(fmt.Sprintf("fake: Location sent to a %s: it goes to the root or a Window", v.Kind))
	}
	if h.histories == nil {
		h.histories = map[ops.NodeID]*History{}
	}
	hi := h.histories[v.ID]
	if hi == nil {
		hi = &History{Index: -1}
		h.histories[v.ID] = hi
	}
	hi.Commands = append(hi.Commands, l)
	e := HistoryEntry{Href: l.Href, State: l.State}
	if h.popping {
		// The browser went there already: the router's state is that
		// entry's, or the app stays where it was (Forward: back again).
		if l.Action == ops.LocationActionForward {
			hi.Index = h.popFrom
		}
		if hi.Index >= 0 && hi.Index < len(hi.Entries) {
			hi.Entries[hi.Index] = e
		}
	} else {
		switch l.Action {
		case ops.LocationActionPush:
			hi.Entries = append(hi.Entries[:hi.Index+1], e)
			hi.Index++
		case ops.LocationActionBack:
			// The latest entry back with the same href, as the web host
			// goes back (react-navigation's linking does so too); none: the
			// state shrank in a way the history never grew (a deep link's
			// screens popped one by one), and this entry is replaced.
			i := -1
			for j := hi.Index - 1; j >= 0; j-- {
				if hi.Entries[j].Href == l.Href {
					i = j
					break
				}
			}
			if i >= 0 {
				hi.Index = i
			}
			if hi.Index < 0 {
				hi.Entries = append(hi.Entries, e)
				hi.Index = 0
			}
			hi.Entries[hi.Index] = e
		default:
			if hi.Index < 0 {
				hi.Entries = append(hi.Entries, e)
				hi.Index = 0
			} else {
				hi.Entries[hi.Index] = e
			}
		}
	}
	if h.Locations == nil {
		h.Locations = map[string]Saved{}
	}
	if k := h.windowKey(v); k != "" {
		h.Locations[k] = Saved{Href: l.Href, State: l.State}
	}
}

// GoBack goes back in the window's history (nil: the main window's), as
// a browser's back button does; false when it's at its first entry.
func (h *Host) GoBack(win *View) bool { return h.Go(win, -1) }

// GoForward goes forward, as a browser's forward button does.
func (h *Host) GoForward(win *View) bool { return h.Go(win, 1) }

// Go moves n entries in the window's history (popstate), reporting the
// entry's href and state to the core with Location.
func (h *Host) Go(win *View, n int) bool {
	if win == nil {
		win = h.Root()
	}
	hi := h.History(win)
	if hi == nil {
		return false
	}
	i := hi.Index + n
	if i < 0 || i >= len(hi.Entries) {
		return false
	}
	h.popFrom = hi.Index
	hi.Index = i
	e := hi.Entries[i]
	h.listening(win, ops.EventLocation)
	h.popping = true
	defer func() { h.popping = false }()
	h.dispatch(ops.Event{ID: win.ID, Kind: ops.EventLocation, Bytes: ops.AppendLocationEvent(nil, e.Href, e.State)})
	return true
}
