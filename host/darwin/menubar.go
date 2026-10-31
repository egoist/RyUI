//go:build darwin && !ios

package darwin

import (
	"encoding/binary"
	"strings"

	. "github.com/egoist/ryui/internal/darwin"
	"github.com/egoist/ryui/ops"
)

// The app's menus (ui.MenuBar): the menu bar is made again with them
// each time they change. A chosen item, by a click or its shortcut,
// sends Action to the root with its index among all the menus' items.

type appMenu struct {
	title string
	items []appItem
}

type appItem struct {
	index int // among all the menus' items
	flags byte
	title string
	key   string
	mods  byte
}

const (
	itemSeparator   = 1
	itemDisabled    = 2
	itemChecked     = 4
	itemDestructive = 8
)

// setMenuBar takes the MenuBar command's menus and makes the menu bar
// again.
func (h *Host) setMenuBar(b []byte) {
	h.appMenus = h.appMenus[:0]
	str := func() string {
		if len(b) < 4 || int(binary.LittleEndian.Uint32(b)) > len(b)-4 {
			b = nil
			return ""
		}
		n := int(binary.LittleEndian.Uint32(b))
		s := string(b[4 : 4+n])
		b = b[4+n:]
		return s
	}
	u32 := func() int {
		if len(b) < 4 {
			b = nil
			return 0
		}
		v := int(binary.LittleEndian.Uint32(b))
		b = b[4:]
		return v
	}
	index := 0
	for range u32() {
		m := appMenu{title: str()}
		for range u32() {
			if len(b) < 1 {
				break
			}
			it := appItem{index: index, flags: b[0]}
			b = b[1:]
			it.title, it.key = str(), str()
			if len(b) > 0 {
				it.mods = b[0]
				b = b[1:]
			}
			index++
			m.items = append(m.items, it)
		}
		h.appMenus = append(h.appMenus, m)
	}
	h.makeMenu(Send(Class("NSApplication"), "sharedApplication"))
}

// addAppItems adds an app menu's items to m.
func (h *Host) addAppItems(m ID, am *appMenu) {
	for _, it := range am.items {
		if it.flags&itemSeparator != 0 {
			sep(m)
			continue
		}
		key, mods := keyEquivalent(it.key, it.mods)
		mi := Send(m, "addItemWithTitle:action:keyEquivalent:", uintptr(String(it.title)), uintptr(Sel("appMenuItem:")), uintptr(String(key)))
		Send(mi, "setKeyEquivalentModifierMask:", uintptr(mods))
		Send(mi, "setTarget:", uintptr(h.target))
		Send(mi, "setTag:", uintptr(it.index))
		if it.flags&itemChecked != 0 {
			Send(mi, "setState:", 1)
		}
	}
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
		for _, m := range h.appMenus {
			for _, it := range m.items {
				if it.index == i {
					return it.flags&itemDisabled == 0
				}
			}
		}
		return false
	case Sel("back:"):
		return h.canGoBack(h.keyWin())
	}
	return true
}
