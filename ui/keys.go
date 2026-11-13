package ui

import (
	"slices"
	"strings"

	"github.com/egoist/asuka/ops"
)

// KeyMap is what a view does with keys while the keyboard's focus is in
// it: it, or a view inside it (a text field, a button), as the
// platforms' responder chains carry a key from the focused view out to
// its ancestors. A key with no modifiers is a named key (up, down,
// escape, enter, tab, pageup...) or a character; with them, a shortcut
// (Cmd("d"), Shortcut{Key: "up", Mods: Alt}). A key goes down (and
// repeats, held) to the nearest view, from the focused one out, whose
// KeyMap has it: that view's func runs, and neither the focused control
// nor the menu bar gets the key. So a search field's Up and Down can
// move through results below it, and Escape can close what it's in.
//
//	ui.View(ui.ViewProps{Keys: ui.KeyMap{
//		{Key: "down"}: func() { sel.Set(min(sel.Peek()+1, n-1)) },
//		{Key: "up"}:   func() { sel.Set(max(sel.Peek()-1, 0)) },
//		{Key: "enter"}: open,
//	}}, field, results)
//
// Out is through the views' parents as the app built them; a List's row
// goes on to its List, and a Sheet's or a Popover's content to the
// window it covers (on a phone, the app's root), ending at that window's
// UseKeys. A Window's content ends at its own UseKeys, never the main
// window's.
//
// A character with no modifiers takes typing from a field inside: give
// characters a modifier, or put the map where no field is. The keys of
// an input method composing text (a Japanese or Chinese keyboard's) go to
// it, never to a KeyMap. A phone takes keys from a hardware keyboard (an
// iPad's, a Chromebook's), so a KeyMap never is the only way to do
// something there.
type KeyMap map[Shortcut]func()

// keyList is m's keys in a fixed order (the Keys prop's), with their
// funcs.
func (m KeyMap) keyList() ([]ops.Key, []func()) {
	keys := make([]ops.Key, 0, len(m))
	for s := range m {
		keys = append(keys, ops.Key{Name: ops.KeyName(s.Key), Mods: uint8(s.Mods)})
	}
	slices.SortFunc(keys, func(a, b ops.Key) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return int(a.Mods) - int(b.Mods)
	})
	funcs := make([]func(), len(keys))
	for s, f := range m {
		i := slices.Index(keys, ops.Key{Name: ops.KeyName(s.Key), Mods: uint8(s.Mods)})
		funcs[i] = f
	}
	return keys, funcs
}

// setKeys gives e the keys of m.
func (e *element) setKeys(m KeyMap) {
	if len(m) == 0 {
		return
	}
	keys, funcs := m.keyList()
	e.handlers().keys = funcs
	e.r.batch.SetBytes(e.id, ops.PropKeys, ops.AppendKeys(nil, keys))
	e.listen(ops.EventKey)
}
