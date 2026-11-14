package uitest

import "github.com/egoist/asuka/host/fake"

// At mounts the app with its main window at href, as a browser opened at
// that address (a link pasted, a reload) or a desktop window left there
// does: its Router starts there, with the screens a link to it builds
// under it.
func At(href string) Option {
	return func(c *config) {
		c.setup = append(c.setup, func(h *fake.Host) {
			if h.Locations == nil {
				h.Locations = map[string]fake.Saved{}
			}
			h.Locations["main"] = fake.Saved{Href: href}
		})
	}
}

// Href is the address the main window's Router shows: the focused
// screen's href ("" with no Router).
func (a *App) Href() string { return a.Host.History(nil).Current().Href }

// History is the main window's history, as a browser keeps it from its
// Router's addresses: their hrefs, oldest first, and the one shown.
func (a *App) History() (hrefs []string, at int) {
	h := a.Host.History(nil)
	if h == nil {
		return nil, -1
	}
	return h.Hrefs(), h.Index
}

// GoBack goes back in the main window's history as a browser's back
// button does: the Router shows the entry's screens (asking the OnBack of
// one it leaves). It fails at the first entry.
func (a *App) GoBack() {
	a.t.Helper()
	if !a.Host.GoBack(nil) {
		a.t.Fatalf("uitest: no history to go back in\n%s", a.screenFor())
	}
	a.Settle()
}

// GoForward goes forward as a browser's forward button does.
func (a *App) GoForward() {
	a.t.Helper()
	if !a.Host.GoForward(nil) {
		a.t.Fatalf("uitest: no history to go forward in\n%s", a.screenFor())
	}
	a.Settle()
}
