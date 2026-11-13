//go:build linux && !android

package linux

import (
	"fmt"
	"strconv"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
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
// submenu of it (its items, when it has one). The items' shortcuts are
// the application's accelerators, in every window. A chosen item sends
// Action on the root with its index among all the menus' items.

type appItem struct {
	index      int
	flags      uint8
	title, key string
	mods       uint8
}

type appMenu struct {
	title string
	items []appItem
}

const (
	itemSeparator   = 1
	itemDisabled    = 2
	itemChecked     = 4
	itemDestructive = 8
)

func (h *Host) setMenuBar(b []byte) {
	r := reader{b: b}
	var menus []appMenu
	index := 0
	for range int(r.u32()) {
		m := appMenu{title: r.str()}
		for range int(r.u32()) {
			if !r.ok(1) {
				break
			}
			it := appItem{index: index, flags: r.u8()}
			it.title, it.key = r.str(), r.str()
			it.mods = r.u8()
			index++
			m.items = append(m.items, it)
		}
		menus = append(menus, m)
	}
	// The previous menus' actions and accelerators go.
	for _, name := range h.appActions {
		none := [1]Ptr{}
		ApplicationSetAccelsForAction(h.app, "app."+name, &none[0])
		ActionMapRemoveAction(h.app, name)
	}
	h.appActions = h.appActions[:0]
	h.appItems = h.appItems[:0]
	for _, m := range menus {
		h.appItems = append(h.appItems, m.items...)
	}
	if h.appMenu != 0 {
		ObjectUnref(h.appMenu)
		h.appMenu = 0
	}
	if len(menus) > 0 {
		root := MenuNew()
		for _, m := range menus {
			sub := MenuNew()
			section := MenuNew()
			for _, it := range m.items {
				if it.flags&itemSeparator != 0 {
					MenuAppendSection(sub, 0, section)
					ObjectUnref(section)
					section = MenuNew()
					continue
				}
				name := "asuka-menu-" + strconv.Itoa(it.index)
				var a Ptr
				if it.flags&itemChecked != 0 {
					a = SimpleActionNewStateful(name, 0, VariantNewBoolean(true))
				} else {
					a = SimpleActionNew(name, 0)
				}
				SimpleActionSetEnabled(a, it.flags&itemDisabled == 0)
				index := it.index
				Connect(a, "activate", func(uintptr) {
					if h.root != nil {
						h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventAction, Int: int64(index)})
					}
				})
				ActionMapAddAction(h.app, a)
				ObjectUnref(a)
				h.appActions = append(h.appActions, name)
				if accel := accelerator(it.key, it.mods); accel != "" && it.flags&itemDisabled == 0 {
					s := CString(accel)
					list := [2]Ptr{Addr(s), 0}
					ApplicationSetAccelsForAction(h.app, "app."+name, &list[0])
				}
				item := MenuItemNew(it.title, "app."+name)
				MenuAppendItem(section, item)
				ObjectUnref(item)
			}
			MenuAppendSection(sub, 0, section)
			ObjectUnref(section)
			if len(menus) == 1 {
				MenuAppendSection(root, 0, sub)
			} else {
				MenuAppendSubmenu(root, m.title, sub)
			}
			ObjectUnref(sub)
		}
		h.appMenu = root
	}
	h.menusVersion++
	h.queueChrome()
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
	for _, it := range h.appItems {
		if it.title == title && it.flags&itemSeparator == 0 {
			if it.flags&itemDisabled != 0 {
				return fmt.Errorf("%q is disabled", title)
			}
			if h.root != nil {
				h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventAction, Int: int64(it.index)})
			}
			return nil
		}
	}
	return fmt.Errorf("no menu item %q", title)
}
