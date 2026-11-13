//go:build darwin && !ios

package darwin

import (
	"strings"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/ops"
)

// The app's menus (ui.MenuBar): the menu bar is made again with them
// each time they change. A chosen item, by a click or its shortcut,
// sends Action to the root with its number among all the menus' items
// (ops.MenuItem.Index); a submenu is an NSMenu of its own, a role's item
// does what AppKit does (Quit: terminate:).

// setMenuBar takes the MenuBar command's menus and makes the menu bar
// again.
func (h *Host) setMenuBar(b []byte) {
	h.appMenus = ops.DecodeMenuBar(b)
	h.appItems = h.appItems[:0]
	for _, m := range h.appMenus {
		h.appItems = append(h.appItems, ops.FlattenMenu(m.Items)...)
	}
	h.makeMenu(Send(Class("NSApplication"), "sharedApplication"))
}

// addAppItems adds an app menu's items to m.
func (h *Host) addAppItems(m ID, am *ops.MenuBarMenu) {
	h.addMenuItems(m, am.Items, Sel("appMenuItem:"), 0, true)
}

// addMenuItems adds items to m, their submenus too: chosen, an item
// sends action to the host's target, its tag tagBase plus its number
// (ops.MenuItem.Index). keys says whether they show their shortcuts (a
// tray icon's and the Dock's menus show none). A role's item does what
// AppKit does.
func (h *Host) addMenuItems(m ID, items []ops.MenuItem, action SEL, tagBase int, keys bool) {
	for i := range items {
		it := &items[i]
		if it.Is(ops.MenuSeparator) {
			sep(m)
			continue
		}
		title := it.Title
		key, mods := "", uint(0)
		if keys {
			key, mods = keyEquivalent(it.Key, it.Mods)
		}
		act := action
		if it.Role == ops.MenuRoleQuit {
			if title == "" {
				title = l10n.Quit(h.locale, h.appName)
			}
			if key == "" && keys {
				key, mods = "q", modCommand
			}
			act = Sel("terminate:")
		}
		if it.Is(ops.MenuSubmenu) {
			act = 0
			key, mods = "", 0
		}
		mi := Send(m, "addItemWithTitle:action:keyEquivalent:", uintptr(String(title)), uintptr(act), uintptr(String(key)))
		Send(mi, "setKeyEquivalentModifierMask:", uintptr(mods))
		if act == action {
			Send(mi, "setTarget:", uintptr(h.target))
			Send(mi, "setTag:", uintptr(tagBase+it.Index))
		}
		if it.Is(ops.MenuChecked) && !it.Is(ops.MenuSubmenu) {
			Send(mi, "setState:", 1)
		}
		if it.Is(ops.MenuDestructive) {
			destructiveTitle(mi, title)
		}
		if it.Is(ops.MenuSubmenu) {
			s := menu(title)
			Send(s, "setAutoenablesItems:", Bool(Send(m, "autoenablesItems") != 0))
			h.addMenuItems(s, it.Items, action, tagBase, keys)
			Send(mi, "setSubmenu:", uintptr(s))
			Release(s)
			if it.Is(ops.MenuDisabled) {
				Send(mi, "setEnabled:", 0)
			}
		} else if Send(m, "autoenablesItems") == 0 {
			Send(mi, "setEnabled:", Bool(!it.Is(ops.MenuDisabled)))
		}
	}
}

// destructiveTitle shows a menu item's title in the system's red, as
// macOS shows a destructive item.
func destructiveTitle(mi ID, title string) {
	red := Send(Class("NSColor"), "systemRedColor")
	key := Const(AppKit(), "NSForegroundColorAttributeName")
	attrs := Send(Class("NSDictionary"), "dictionaryWithObject:forKey:", uintptr(red), uintptr(key))
	s := Send(Send(Class("NSAttributedString"), "alloc"), "initWithString:attributes:", uintptr(String(title)), uintptr(attrs))
	Send(mi, "setAttributedTitle:", uintptr(s))
	Release(s)
}

// keyEquivalent maps a shortcut to AppKit's key equivalent and modifier
// mask: Primary is ⌘.
func keyEquivalent(key string, mods byte) (string, uint) {
	if key == "" {
		return "", 0
	}
	var mask uint
	if mods&1 != 0 {
		mask |= modCommand
	}
	if mods&2 != 0 {
		mask |= modShift
	}
	if mods&4 != 0 {
		mask |= modOption
	}
	if mods&8 != 0 {
		mask |= modControl
	}
	named := map[string]rune{
		"enter": '\r', "escape": 0x1b, "tab": '\t', "space": ' ', "backspace": 0x08, "delete": 0x7f,
		"up": 0xF700, "down": 0xF701, "left": 0xF702, "right": 0xF703,
		"home": 0xF729, "end": 0xF72B, "pageup": 0xF72C, "pagedown": 0xF72D,
	}
	k := strings.ToLower(key)
	if r, ok := named[k]; ok {
		return string(r), mask
	}
	if len(k) >= 2 && k[0] == 'f' {
		if n := atoi(k[1:]); n >= 1 && n <= 12 {
			return string(rune(0xF704 + n - 1)), mask
		}
	}
	// A letter: AppKit reads an uppercase one as shifted.
	return k, mask
}

// appMenuItem is an app menu item chosen: Action on the root.
func (h *Host) appMenuItem(mi ID) {
	if h.root == nil {
		return
	}
	h.emit(h.root, ops.Event{Kind: ops.EventAction, Int: int64(SendInt(mi, "tag"))})
}

// validateMenuItem enables the app's items as the app says, and Back
// when there's a screen to go back from.
func (h *Host) validateMenuItem(mi ID) bool {
	switch SEL(Send(mi, "action")) {
	case Sel("appMenuItem:"):
		i := SendInt(mi, "tag")
		if i >= 0 && i < len(h.appItems) {
			return !h.appItems[i].Is(ops.MenuDisabled)
		}
		return false
	case Sel("back:"):
		return h.canGoBack(h.keyWin())
	}
	return true
}
