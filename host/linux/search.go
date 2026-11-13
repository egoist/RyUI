//go:build linux && !android

package linux

import (
	. "github.com/egoist/ryui/internal/gtk"
	"github.com/egoist/ryui/ops"
)

// A screen's search (ScreenOptions.Search): a search button in the header
// bar and, under it, GTK's search bar with its entry, as GNOME's apps
// search; typing anywhere in the window starts it. Typing is the screen's
// TextChanged, Enter its Submit.

type searchField struct {
	bar, entry, button Ptr
	screen             *node
	updating           bool
}

func (s *searchField) setText(t string) {
	if EditableGetText(s.entry) == t {
		return
	}
	s.updating = true
	EditableSetText(s.entry, t)
	s.updating = false
}

// searchFor is the window's search button, its bar set for the screen.
func (h *Host) searchFor(w *win, screen *node) Ptr {
	s := w.search
	if s == nil {
		s = &searchField{}
		w.search = s
		s.bar = SearchBarNew()
		s.entry = SearchEntryNew()
		SearchBarSetChild(s.bar, s.entry)
		SearchBarConnectEntry(s.bar, s.entry)
		SearchBarSetKeyCaptureWidget(s.bar, w.window)
		AdwToolbarViewAddTopBar(w.toolbar, s.bar)
		Connect(s.entry, "search-changed", func() {
			if s.updating || s.screen == nil {
				return
			}
			t := EditableGetText(s.entry)
			s.screen.nav.searchTx = t
			h.emit(s.screen, ops.Event{Kind: ops.EventTextChanged, Str: t})
		})
		Connect(s.entry, "activate", func() {
			if s.screen != nil {
				h.emit(s.screen, ops.Event{Kind: ops.EventSubmit})
			}
		})
	}
	if s.screen != screen {
		s.screen = screen
		screen.nav.search = s
		SearchEntrySetPlaceholderText(s.entry, screen.nav.searchPh)
		s.setText(screen.nav.searchTx)
		SearchBarSetSearchMode(s.bar, screen.nav.searchTx != "")
	}
	b := ToggleButtonNew()
	ButtonSetIconName(b, "system-search-symbolic")
	WidgetSetTooltipText(b, "Search")
	ToggleButtonSetActive(b, SearchBarGetSearchMode(s.bar))
	Connect(b, "toggled", func() {
		SearchBarSetSearchMode(s.bar, ToggleButtonGetActive(b))
	})
	Connect(s.bar, "notify::search-mode-enabled", func(uintptr) {
		ToggleButtonSetActive(b, SearchBarGetSearchMode(s.bar))
	})
	WidgetSetVisible(s.bar, true)
	return b
}

func (h *Host) hideSearch(w *win) {
	if s := w.search; s != nil {
		if s.screen != nil && s.screen.nav != nil {
			s.screen.nav.search = nil
		}
		s.screen = nil
		SearchBarSetSearchMode(s.bar, false)
		WidgetSetVisible(s.bar, false)
	}
}
