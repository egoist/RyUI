//go:build linux && !android

package linux

import (
	"fmt"
	"strconv"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/ops"
)

// Menus are GMenu models in GtkPopoverMenus: a button's drops from it, a
// view's LongPress menu is its context menu (a secondary click, a long
// press) at the pointer. Items are actions of a group on the view
// ("asuka.item<i>"); choosing one sends Action with its index.

func (h *Host) menuChanged(n *node) {
	if n.widget == 0 {
		return
	}
	if len(n.menu) > 0 && ops.MenuTrigger(n.menuTrigger) == ops.MenuTriggerLongPress {
		h.listenLongPress(n)
	}
	if len(n.menu) > 0 && ops.MenuTrigger(n.menuTrigger) == ops.MenuTriggerTap && n.kind != ops.KindButton {
		h.listenClick(n)
		h.makePressable(n)
	}
}

// menuModel builds a GMenu of actions, their items named prefix+index.
func (h *Host) menuModel(actions []action, prefix string) Ptr {
	m := MenuNew()
	section := MenuNew()
	for i, a := range actions {
		item := MenuItemNew(a.title, prefix+strconv.Itoa(i))
		if a.icon != "" {
			if name := h.iconName(a.icon); name != "" {
				icon := ThemedIconNew(name)
				MenuItemSetIcon(item, icon)
				ObjectUnref(icon)
			}
		}
		if a.role == ops.AlertButtonRoleDestructive {
			// GTK's menus have no destructive style: a section of its own,
			// as GNOME's apps set a destructive item apart.
			if i > 0 {
				MenuAppendSection(m, 0, section)
				ObjectUnref(section)
				section = MenuNew()
			}
		}
		MenuAppendItem(section, item)
		ObjectUnref(item)
	}
	MenuAppendSection(m, 0, section)
	ObjectUnref(section)
	return m
}

// popupMenu shows n's menu: from the view (a button's, x < 0) or at a
// point in it (a context menu).
func (h *Host) popupMenu(n *node, x, y float64) {
	if len(n.menu) == 0 || n.widget == 0 {
		return
	}
	group := SimpleActionGroupNew()
	for i := range n.menu {
		a := SimpleActionNew("item"+strconv.Itoa(i), 0)
		i := i
		id := n.id
		Connect(a, "activate", func(param uintptr) {
			if n := h.nodes[id]; n != nil {
				h.emit(n, ops.Event{Kind: ops.EventAction, Int: int64(i)})
			}
		})
		ActionMapAddAction(group, a)
		ObjectUnref(a)
	}
	WidgetInsertActionGroup(n.widget, "asuka", group)
	ObjectUnref(group)
	model := h.menuModel(n.menu, "asuka.item")
	pop := PopoverMenuNewFromModel(model)
	ObjectUnref(model)
	WidgetSetParent(pop, n.widget)
	if x >= 0 {
		r := IntRect{X: int32(x), Y: int32(y), W: 1, H: 1}
		PopoverSetPointingTo(pop, &r)
		PopoverSetHasArrow(pop, false)
		if n.rtl {
			WidgetSetHAlign(pop, AlignEnd)
		} else {
			WidgetSetHAlign(pop, AlignStart)
		}
	} else {
		PopoverSetPosition(pop, 3) // GTK_POS_BOTTOM
	}
	Connect(pop, "closed", func() {
		// Unparented once its action (which runs after it closes) has.
		IdleAdd(PriorityDefaultIdle, func() bool {
			WidgetUnparent(pop)
			return false
		})
	})
	h.popups = append(h.popups, pop)
	PopoverPopup(pop)
}

// The app's menus (MenuBar): GNOME's apps have no menu bar but a main
// menu, the header bar's last button; each of the app's menus is a
// submenu of it (its items, when it has one), and an item with a submenu
// opens one of its own. The items' shortcuts are the application's
// accelerators, in every window. A chosen item sends Action on the root
// with its number among all the menus' items (ops.DecodeMenuBar's); a
// Quit item (MenuRoleQuit) quits the app instead.

func (h *Host) setMenuBar(b []byte) {
	menus := ops.DecodeMenuBar(b)
	// The previous menus' actions and accelerators go.
	for _, name := range h.appActions {
		none := [1]Ptr{}
		ApplicationSetAccelsForAction(h.app, "app."+name, &none[0])
		ActionMapRemoveAction(h.app, name)
	}
	h.appActions = h.appActions[:0]
	h.appItems = h.appItems[:0]
	for _, m := range menus {
		h.appItems = append(h.appItems, ops.FlattenMenu(m.Items)...)
	}
	if h.appMenu != 0 {
		ObjectUnref(h.appMenu)
		h.appMenu = 0
	}
	if len(menus) > 0 {
		root := MenuNew()
		for _, m := range menus {
			sub := h.appMenuModel(m.Items)
			if len(menus) == 1 {
				MenuAppendSection(root, 0, sub)
			} else {
				MenuAppendSubmenu(root, m.Title, sub)
			}
			ObjectUnref(sub)
		}
		h.appMenu = root
	}
	h.menusVersion++
	h.queueChrome()
}

// appMenuModel builds a GMenu of the app's items: sections between
// separators, a submenu for an item with one, each item an action of the
// application's ("app.asuka-menu-<number>").
func (h *Host) appMenuModel(items []ops.MenuItem) Ptr {
	m := MenuNew()
	section := MenuNew()
	for i := range items {
		it := &items[i]
		if it.Is(ops.MenuSeparator) {
			MenuAppendSection(m, 0, section)
			ObjectUnref(section)
			section = MenuNew()
			continue
		}
		title := h.menuItemTitle(it)
		if it.Is(ops.MenuSubmenu) {
			sub := h.appMenuModel(it.Items)
			MenuAppendSubmenu(section, title, sub)
			ObjectUnref(sub)
			continue
		}
		name := "asuka-menu-" + strconv.Itoa(it.Index)
		var a Ptr
		if it.Is(ops.MenuChecked) {
			a = SimpleActionNewStateful(name, 0, VariantNewBoolean(true))
		} else {
			a = SimpleActionNew(name, 0)
		}
		SimpleActionSetEnabled(a, !it.Is(ops.MenuDisabled))
		index, role := it.Index, it.Role
		Connect(a, "activate", func(uintptr) { h.chooseAppItem(index, role) })
		ActionMapAddAction(h.app, a)
		ObjectUnref(a)
		h.appActions = append(h.appActions, name)
		key, mods := it.Key, it.Mods
		if role == ops.MenuRoleQuit && key == "" {
			key, mods = "q", 1
		}
		if role == ops.MenuRoleClose && key == "" {
			key, mods = "w", 1
		}
		if accel := accelerator(key, mods); accel != "" && !it.Is(ops.MenuDisabled) {
			s := CString(accel)
			list := [2]Ptr{Addr(s), 0}
			ApplicationSetAccelsForAction(h.app, "app."+name, &list[0])
		}
		item := MenuItemNew(title, "app."+name)
		MenuAppendItem(section, item)
		ObjectUnref(item)
	}
	MenuAppendSection(m, 0, section)
	ObjectUnref(section)
	return m
}

// menuItemTitle is what an item says: its title, or its role's.
func (h *Host) menuItemTitle(it *ops.MenuItem) string {
	if it.Title == "" && it.Role == ops.MenuRoleQuit {
		return l10n.Quit(h.locale, h.appName)
	}
	if it.Title == "" && it.Role == ops.MenuRoleClose {
		return "Close"
	}
	return it.Title
}

// chooseAppItem is the app menu's item chosen: Action on the root, or
// the app quitting for a Quit item.
func (h *Host) chooseAppItem(index int, role ops.MenuRole) {
	if role == ops.MenuRoleQuit {
		h.afterCore(h.quit)
		return
	}
	if role == ops.MenuRoleClose {
		// The window in use, as its close button closes it (close-request:
		// the core asks first where it listens).
		h.afterCore(func() {
			if w := ApplicationGetActiveWindow(h.app); w != 0 {
				WindowClose(w)
			}
		})
		return
	}
	if h.root != nil {
		h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventAction, Int: int64(index)})
	}
}

// accelerator is a shortcut in GTK's accelerator syntax: <Control><Shift>s.
func accelerator(key string, mods uint8) string {
	if key == "" {
		return ""
	}
	var b strings.Builder
	if mods&1 != 0 || mods&8 != 0 {
		b.WriteString("<Control>")
	}
	if mods&2 != 0 {
		b.WriteString("<Shift>")
	}
	if mods&4 != 0 {
		b.WriteString("<Alt>")
	}
	name := keyName(key)
	if name == "" {
		return ""
	}
	b.WriteString(name)
	return b.String()
}

// keyName is a key as GDK names it.
func keyName(key string) string {
	switch key {
	case "enter":
		return "Return"
	case "escape":
		return "Escape"
	case "tab":
		return "Tab"
	case "space":
		return "space"
	case "delete":
		return "Delete"
	case "backspace":
		return "BackSpace"
	case "up", "down", "left", "right", "home", "end":
		return strings.ToUpper(key[:1]) + key[1:]
	case "pageup":
		return "Page_Up"
	case "pagedown":
		return "Page_Down"
	}
	if len(key) >= 2 && (key[0] == 'f' || key[0] == 'F') {
		if _, err := strconv.Atoi(key[1:]); err == nil {
			return "F" + key[1:]
		}
	}
	punct := map[string]string{
		",": "comma", ".": "period", "/": "slash", "\\": "backslash", ";": "semicolon",
		"'": "apostrophe", "[": "bracketleft", "]": "bracketright", "-": "minus", "=": "equal",
		"`": "grave", "+": "plus",
	}
	if p, ok := punct[key]; ok {
		return p
	}
	if len([]rune(key)) == 1 {
		return strings.ToLower(key)
	}
	return ""
}

// activateAppItem chooses the app menu's item with a title, as a click
// on it would.
func (h *Host) activateAppItem(title string) error {
	for i := range h.appItems {
		it := &h.appItems[i]
		if h.menuItemTitle(it) == title && !it.Is(ops.MenuSeparator) && !it.Is(ops.MenuSubmenu) {
			if it.Is(ops.MenuDisabled) {
				return fmt.Errorf("%q is disabled", title)
			}
			h.chooseAppItem(it.Index, it.Role)
			return nil
		}
	}
	return fmt.Errorf("no menu item %q", title)
}
