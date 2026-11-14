//go:build darwin && !ios

package darwin

import (
	"github.com/egoist/asuka/internal/routestate"
	"github.com/egoist/asuka/ops"
)

// A window's Router address comes back as the window does: the last
// Location command each window was sent is kept, by the window's key, in
// the app's data directory (internal/routestate), and given back as its
// Router mounts. Written a moment after it changes, and as the app goes
// to the background or quits.

var routes *routestate.Store

func routeStore() *routestate.Store {
	if routes == nil {
		routes = routestate.Open(routestate.Path())
	}
	return routes
}

// WindowRoute implements host.Locator.
func (h *Host) WindowRoute(window string) (string, []byte, bool) {
	return routeStore().Location(window)
}

// location keeps a Location command's address for its window.
func (h *Host) location(args []byte) {
	if l, ok := ops.DecodeLocation(args); ok {
		routeStore().Set(l.Window, l.Href, l.State)
	}
}
