package ops

// Menus as the core sends them: the app's menu bar (MenuBar), and the
// menus of its tray icons and of its Dock icon (package tray's module
// calls). One encoding for all, read by every desktop host with
// DecodeMenuBar and DecodeMenu.
//
// A menu is u32 count, then its items. An item is u8 MenuFlags, a
// u32-length title, a u32-length key (a character, or enter, escape,
// tab, space, delete, backspace, up, down, left, right, home, end,
// pageup, pagedown, f1-f12; empty: no shortcut), u8 modifiers (1
// primary: ⌘ on macOS, Ctrl elsewhere; 2 shift; 4 alt, a Mac's option;
// 8 control) and u8 MenuRole; with MenuSubmenu, its submenu follows, a
// menu. A menu bar is u32 count, then per menu a u32-length title and a
// menu.
//
// Items are numbered depth first, each submenu's items after the item
// that opens it, and separators and submenus count: a chosen item is
// reported by that number (in a menu bar, among all its menus' items).

// MenuFlags say what a menu item is.
type MenuFlags uint8

const (
	MenuSeparator MenuFlags = 1 << iota
	MenuDisabled
	MenuChecked
	MenuDestructive
	// MenuSubmenu: the item opens a submenu, which follows it.
	MenuSubmenu
	// MenuInline: with MenuSubmenu, an action menu's section rather than a
	// submenu: its items shown in place, between dividers, under the
	// item's title if it has one (UIMenu's displayInline; SwiftUI's
	// Section in a Menu). Only action menus (ActionItem) use it.
	MenuInline
)

// MenuRole is what a menu item does that the host does itself, with its
// own title (when the item has none) and shortcut.
type MenuRole uint8

const (
	MenuRoleNone MenuRole = iota
	// MenuRoleQuit quits the app: "Quit <app>", ⌘Q on a Mac.
	MenuRoleQuit
	// MenuRoleClose closes the window in use as its close button does
	// (asking first where the core listens for CloseRequest): "Close",
	// ⌘W, Ctrl+W. A Mac's File menu has no Close Window of the host's
	// then.
	MenuRoleClose
	// MenuRoleOpenRecent opens the recent documents: on a Mac the host's
	// own Open Recent submenu, NSDocumentController's, in its place;
	// elsewhere the item's submenu, as the app gives it.
	MenuRoleOpenRecent
)

// MenuItem is a menu's item.
type MenuItem struct {
	// Index is the item's number in its menu (or menu bar), depth first.
	Index int
	Flags MenuFlags
	Title string
	Key   string
	Mods  uint8
	Role  MenuRole
	// Items are its submenu's (MenuSubmenu).
	Items []MenuItem
}

// Is reports whether the item has all of flags.
func (it *MenuItem) Is(flags MenuFlags) bool { return it.Flags&flags == flags }

// MenuBarMenu is one of a menu bar's menus.
type MenuBarMenu struct {
	Title string
	Items []MenuItem
}

// AppendMenu appends a menu's items. Their Index is ignored: the
// position numbers them.
func AppendMenu(b []byte, items []MenuItem) []byte {
	b = le.AppendUint32(b, uint32(len(items)))
	for i := range items {
		it := &items[i]
		flags := it.Flags &^ MenuSubmenu
		if len(it.Items) > 0 {
			flags |= MenuSubmenu
		}
		b = append(b, byte(flags))
		b = appendStr(b, it.Title)
		b = appendStr(b, it.Key)
		b = append(b, it.Mods, byte(it.Role))
		if flags&MenuSubmenu != 0 {
			b = AppendMenu(b, it.Items)
		}
	}
	return b
}

// AppendMenuBar appends a menu bar's menus.
func AppendMenuBar(b []byte, menus []MenuBarMenu) []byte {
	b = le.AppendUint32(b, uint32(len(menus)))
	for i := range menus {
		b = appendStr(b, menus[i].Title)
		b = AppendMenu(b, menus[i].Items)
	}
	return b
}

// DecodeMenu reads what AppendMenu writes, numbering the items; a short
// menu ends where it's cut.
func DecodeMenu(b []byte) []MenuItem {
	r := &reader{b: b}
	n := 0
	return decodeMenu(r, &n, 0)
}

// DecodeMenuBar reads what AppendMenuBar writes, numbering the items
// across the menus.
func DecodeMenuBar(b []byte) []MenuBarMenu {
	r := &reader{b: b}
	count := r.u32()
	var menus []MenuBarMenu
	n := 0
	for i := uint32(0); i < count && r.err == nil; i++ {
		m := MenuBarMenu{Title: string(r.bytes())}
		m.Items = decodeMenu(r, &n, 0)
		if r.err != nil && len(m.Items) == 0 {
			break
		}
		menus = append(menus, m)
	}
	return menus
}

// menuDepth bounds submenus' nesting: deeper ones are cut.
const menuDepth = 16

func decodeMenu(r *reader, n *int, depth int) []MenuItem {
	count := r.u32()
	var items []MenuItem
	for i := uint32(0); i < count && r.err == nil; i++ {
		it := MenuItem{Index: *n, Flags: MenuFlags(r.u8())}
		it.Title = string(r.bytes())
		it.Key = string(r.bytes())
		it.Mods = r.u8()
		it.Role = MenuRole(r.u8())
		if r.err != nil {
			break
		}
		*n++
		if it.Flags&MenuSubmenu != 0 {
			if depth >= menuDepth {
				r.err = errShort
				break
			}
			it.Items = decodeMenu(r, n, depth+1)
		}
		items = append(items, it)
	}
	return items
}

// FlattenMenu lists items and their submenus' items depth first, so that
// an item's Index is its place in the list.
func FlattenMenu(items []MenuItem) []MenuItem {
	var out []MenuItem
	var walk func([]MenuItem)
	walk = func(items []MenuItem) {
		for _, it := range items {
			out = append(out, it)
			walk(it.Items)
		}
	}
	walk(items)
	return out
}
