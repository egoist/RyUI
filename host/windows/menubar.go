//go:build windows

package windows

import (
	"strings"
	"unicode"

	"github.com/egoist/asuka/internal/l10n"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
)

// The app's menus (ui.MenuBar): a menu bar (an HMENU) on the main window,
// made again each time they change, and an accelerator table for their
// shortcuts, which the message loop translates for any of the app's
// windows. A submenu is a popup of the menu it's in. A chosen item,
// clicked or by its shortcut, comes as WM_COMMAND and sends Action to
// the root with its number among all the menus' items (ops/menu.go); a
// quit item quits the app. Popup menus follow the app's dark mode
// through uxtheme (SetAppDark); the bar itself is drawn dark by the
// window (the UAH messages) while the app is. A destructive item is
// drawn red by the host (menudraw.go).

const (
	modPrimary = 1
	modShift   = 2
	modAlt     = 4
	modControl = 8

	// menuBase is the command ID of the menus' first item.
	menuBase = 0x100
)

// setMenuBar takes the MenuBar command's menus and makes the menu bar
// and the accelerators again.
func (h *Host) setMenuBar(b []byte) {
	h.appMenus = ops.DecodeMenuBar(b)
	h.buildMenuBar()
}

// menuBuild makes popup menus of the app's items (the menu bar's, a tray
// icon's): an item's command ID is base plus its number; the menus it
// owner-draws go in owned, for forget once the menus are gone.
type menuBuild struct {
	h         *Host
	base      int
	order     uint32 // MF_RIGHTORDER, right to left
	shortcuts bool   // items show their shortcuts
	owned     []*ownerMenu
}

// itemTitle is what an item says: a quit item's own title, else the
// platform's.
func (h *Host) itemTitle(it *ops.MenuItem) string {
	if it.Title == "" && it.Role == ops.MenuRoleQuit {
		return l10n.Quit(h.locale, h.appName)
	}
	if it.Title == "" && it.Role == ops.MenuRoleClose {
		return "Close"
	}
	return it.Title
}

// itemShortcut is an item's shortcut: its own, or its role's (Close's
// Ctrl+W).
func itemShortcut(it *ops.MenuItem) (string, byte) {
	if it.Key == "" && it.Role == ops.MenuRoleClose {
		return "w", modPrimary
	}
	return it.Key, it.Mods
}

// popup makes a popup menu of items, their submenus popups of it.
func (b *menuBuild) popup(items []ops.MenuItem) uintptr {
	pop := CreatePopupMenu()
	var names []string
	for i := range items {
		if !items[i].Is(ops.MenuSeparator) {
			names = append(names, b.h.itemTitle(&items[i]))
		}
	}
	names = accessKeys(names)
	k := 0
	var drawn []menuItem
	destructive := false
	for i := range items {
		it := &items[i]
		if it.Is(ops.MenuSeparator) {
			AppendMenu(pop, MF_SEPARATOR, 0, "")
			drawn = append(drawn, menuItem{separator: true})
			continue
		}
		flags := uint32(MF_STRING) | b.order
		if it.Is(ops.MenuDisabled) {
			flags |= MF_GRAYED
		}
		text := names[k]
		k++
		if len(it.Items) > 0 {
			AppendMenu(pop, flags|MF_POPUP, b.popup(it.Items), text)
			drawn = append(drawn, menuItem{text: text, submenu: true})
			continue
		}
		if it.Is(ops.MenuChecked) {
			flags |= MF_CHECKED
		}
		if s := shortcutText(itemShortcut(it)); s != "" && b.shortcuts {
			text += "\t" + s
		}
		AppendMenu(pop, flags, uintptr(b.base+it.Index), text)
		d := it.Is(ops.MenuDestructive)
		drawn = append(drawn, menuItem{text: text, destructive: d})
		destructive = destructive || d
	}
	if destructive {
		b.owned = append(b.owned, b.h.ownerDrawMenu(pop, drawn, b.order))
	}
	return pop
}

// buildMenuBar makes the menu bar and the accelerators from the menus, in
// the app's direction: a mirrored window's menu bar runs from the right
// itself (MFT_RIGHTJUSTIFY would push it back to the other side), and its
// menus open mirrored; MF_RIGHTORDER has their text read right to left and
// their submenus cascade to the left.
func (h *Host) buildMenuBar() {
	for _, om := range h.barOwned {
		h.forget(om)
	}
	h.barOwned = h.barOwned[:0]
	h.appItems = h.appItems[:0]
	for _, m := range h.appMenus {
		h.appItems = append(h.appItems, ops.FlattenMenu(m.Items)...)
	}
	var bar uintptr
	// A hidden title bar's main window has no menu bar: Windows would
	// draw it under a caption that isn't there (titlebar.go).
	if len(h.appMenus) > 0 && (h.mainCtl == nil || !h.mainCtl.hidden) {
		bar = h.newMenuBar()
	}
	// A menu bar coming or going before the window shows keeps its client
	// size (WindowOptions'), and the window centered.
	before := ClientRect(h.window)
	old := GetMenu(h.window)
	SetMenu(h.window, bar)
	if old != 0 {
		DestroyMenu(old)
	}
	DrawMenuBar(h.window)
	if !IsWindowVisible(h.window) {
		if dh := before.H() - ClientRect(h.window).H(); dh != 0 {
			wr := WindowRect(h.window)
			SetWindowPos(h.window, 0, wr.Left, wr.Top-dh/2, wr.W(), wr.H()+dh, SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
		}
	}

	// The Windows that show the menu bar too (a document's).
	for _, c := range h.ctls {
		if !c.main && c.cfg.Flags&ops.WindowMenuBar != 0 {
			h.setWindowMenu(c)
		}
	}

	DestroyAcceleratorTable(h.accel)
	var accels []Accel
	for _, it := range h.appItems {
		if it.Flags&(ops.MenuSeparator|ops.MenuDisabled|ops.MenuSubmenu) != 0 {
			continue
		}
		if a, ok := accelerator(itemShortcut(&it)); ok {
			a.Cmd = uint16(menuBase + it.Index)
			accels = append(accels, a)
		}
	}
	h.accel = CreateAcceleratorTable(accels)
}

// newMenuBar makes a menu bar (an HMENU) of the app's menus, in the
// app's direction.
func (h *Host) newMenuBar() uintptr {
	order := uint32(0)
	if h.rtl {
		order = MF_RIGHTORDER
	}
	bar := CreateMenu()
	titles := make([]string, len(h.appMenus))
	for i, m := range h.appMenus {
		titles[i] = m.Title
	}
	titles = accessKeys(titles)
	b := &menuBuild{h: h, base: menuBase, order: order, shortcuts: true}
	for i, m := range h.appMenus {
		AppendMenu(bar, MF_POPUP|order, b.popup(m.Items), titles[i])
	}
	h.barOwned = append(h.barOwned, b.owned...)
	return bar
}

// setWindowMenu gives a Window that asks for it (WindowMenuBar: a
// document's, as Notepad's windows each have theirs) a menu bar of the
// app's menus of its own, again as they change.
func (h *Host) setWindowMenu(c *wctl) {
	if c.main || c.hwnd == 0 || c.cfg.Flags&ops.WindowMenuBar == 0 {
		return
	}
	var bar uintptr
	if len(h.appMenus) > 0 && !c.hidden {
		bar = h.newMenuBar()
	}
	old := GetMenu(c.hwnd)
	SetMenu(c.hwnd, bar)
	if old != 0 {
		DestroyMenu(old)
	}
	DrawMenuBar(c.hwnd)
}

// accessKeys gives each title an access key (Alt and the underlined
// letter): its first letter or digit no title before it took. A title's
// own & is shown as one.
func accessKeys(titles []string) []string {
	used := map[rune]bool{}
	out := make([]string, len(titles))
	for i, t := range titles {
		s := strings.ReplaceAll(t, "&", "&&")
		for j, r := range s {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				continue
			}
			k := unicode.ToLower(r)
			if used[k] {
				continue
			}
			used[k] = true
			s = s[:j] + "&" + s[j:]
			break
		}
		out[i] = s
	}
	return out
}

// namedKeys are the shortcut keys a name stands for: their virtual key
// and how a menu writes them.
var namedKeys = map[string]struct {
	vk   uint16
	text string
}{
	"enter": {VK_RETURN, "Enter"}, "escape": {VK_ESCAPE, "Esc"}, "tab": {VK_TAB, "Tab"},
	"space": {VK_SPACE, "Space"}, "delete": {VK_DELETE, "Del"}, "backspace": {VK_BACK, "Backspace"},
	"up": {VK_UP, "Up"}, "down": {VK_DOWN, "Down"}, "left": {VK_LEFT, "Left"}, "right": {VK_RIGHT, "Right"},
	"home": {VK_HOME, "Home"}, "end": {VK_END, "End"}, "pageup": {VK_PRIOR, "PgUp"}, "pagedown": {VK_NEXT, "PgDn"},
}

// functionKey is n of a name f1 to f12 (0: not one).
func functionKey(k string) int {
	if len(k) < 2 || len(k) > 3 || k[0] != 'f' {
		return 0
	}
	n := 0
	for _, c := range k[1:] {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	if n < 1 || n > 12 {
		return 0
	}
	return n
}

// shortcutText is how a menu item shows its shortcut: "Ctrl+Shift+K",
// "Ctrl+Enter", "F5" (Primary and Control are both Ctrl).
func shortcutText(key string, mods byte) string {
	if key == "" {
		return ""
	}
	var b strings.Builder
	if mods&(modPrimary|modControl) != 0 {
		b.WriteString("Ctrl+")
	}
	if mods&modShift != 0 {
		b.WriteString("Shift+")
	}
	if mods&modAlt != 0 {
		b.WriteString("Alt+")
	}
	k := strings.ToLower(key)
	switch nk, ok := namedKeys[k]; {
	case ok:
		b.WriteString(nk.text)
	case functionKey(k) != 0:
		b.WriteString(strings.ToUpper(k))
	default:
		b.WriteString(strings.ToUpper(key))
	}
	return b.String()
}

// accelerator is a shortcut's accelerator: a character's virtual key on
// the keyboard's layout (with Shift if it takes it, as + does), or a
// named key's.
func accelerator(key string, mods byte) (Accel, bool) {
	if key == "" {
		return Accel{}, false
	}
	a := Accel{Virt: FVIRTKEY | FNOINVERT}
	k := strings.ToLower(key)
	switch nk, ok := namedKeys[k]; {
	case ok:
		a.Key = nk.vk
	case functionKey(k) != 0:
		a.Key = uint16(VK_F1 + functionKey(k) - 1)
	default:
		rs := []rune(k)
		if len(rs) != 1 {
			return Accel{}, false
		}
		switch r := rs[0]; {
		case r >= 'a' && r <= 'z':
			a.Key = uint16(r - 'a' + 'A')
		case r >= '0' && r <= '9':
			a.Key = uint16(r)
		default:
			vk, shift, ok := VkKeyScan(r)
			if !ok {
				return Accel{}, false
			}
			a.Key = uint16(vk)
			if shift&1 != 0 {
				a.Virt |= FSHIFT
			}
		}
	}
	if mods&(modPrimary|modControl) != 0 {
		a.Virt |= FCONTROL
	}
	if mods&modShift != 0 {
		a.Virt |= FSHIFT
	}
	if mods&modAlt != 0 {
		a.Virt |= FALT
	}
	return a, true
}

// menuCommand is a menu item chosen, clicked or by its accelerator, in
// one of the app's windows: Action on the root, unless it's off; a quit
// item quits.
func (h *Host) menuCommand(id int) bool {
	i := id - menuBase
	if i < 0 || i >= len(h.appItems) {
		return false
	}
	it := h.appItems[i]
	switch {
	case it.Flags&(ops.MenuSeparator|ops.MenuDisabled|ops.MenuSubmenu) != 0:
	case it.Role == ops.MenuRoleQuit:
		h.quit()
	case it.Role == ops.MenuRoleClose:
		// The window in use, as its close button closes it (WM_CLOSE: the
		// core asks first where it listens).
		PostMessage(h.windowInUse(), WM_CLOSE, 0, 0)
	case h.root != nil:
		h.emit(h.root, ops.Event{Kind: ops.EventAction, Int: int64(it.Index)})
	}
	return true
}

// accelerate translates a key message for the app's window w into a menu
// item's command, if it's one's shortcut.
func (h *Host) accelerate(w uintptr, m *Msg) bool {
	return h.accel != 0 && TranslateAccelerator(w, h.accel, m)
}

// The menu bar's colors while dark: the dark title bar's, and for an
// item under the mouse and one open, WinUI's backplates over it.
const (
	darkMenuBar      = 0x202020 // COLORREF
	darkMenuHot      = 0x2D2D2D
	darkMenuOpen     = 0x333333
	darkMenuText     = 0xFFFFFF
	darkMenuDisabled = 0x6D6D6D
)

// menuBarMessage draws the main window's menu bar dark while the app is
// (Windows draws it light whatever the theme): the bar, its items, and
// over the light line the frame draws under it.
func (h *Host) menuBarMessage(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case WM_UAHDRAWMENU, WM_UAHDRAWMENUITEM, WM_NCPAINT, WM_NCACTIVATE:
	default:
		return 0, false
	}
	if !h.dark || GetMenu(hwnd) == 0 {
		return 0, false
	}
	switch m {
	case WM_UAHDRAWMENU:
		um := Native[UAHMenu](lp)
		if r, ok := MenuBarRect(hwnd); ok {
			FillColor(um.DC, r, darkMenuBar)
		}
	case WM_UAHDRAWMENUITEM:
		h.drawMenuBarItem(Native[UAHDrawMenuItem](lp))
	default:
		r := DefWindowProc(hwnd, m, wp, lp)
		if bar, ok := MenuBarRect(hwnd); ok {
			if dc := WindowDC(hwnd); dc != 0 {
				FillColor(dc, Rect{Left: bar.Left, Top: bar.Bottom, Right: bar.Right, Bottom: bar.Bottom + 1}, darkMenuBar)
				ReleaseDC(hwnd, dc)
			}
		}
		return r, true
	}
	return 0, true
}

// drawMenuBarItem draws a dark menu bar's item: its backplate (rounded
// and inset on Windows 11) and its title.
func (h *Host) drawMenuBarItem(d *UAHDrawMenuItem) {
	dc, r, state := d.Draw.DC, d.Draw.Item, d.Draw.ItemState
	plate, fore := uint32(darkMenuBar), uint32(darkMenuText)
	switch {
	case state&(ODS_GRAYED|ODS_DISABLED) != 0:
		fore = darkMenuDisabled
	case state&ODS_SELECTED != 0:
		plate = darkMenuOpen
	case state&ODS_HOTLIGHT != 0:
		plate = darkMenuHot
	}
	FillColor(dc, r, darkMenuBar)
	if plate != darkMenuBar {
		if Windows11() && Mirrored(dc) {
			// A mirrored window's DC: GDI+ doesn't follow it.
			k := h.scale
			in := int32(2*k + 0.5)
			FillRoundRectGDI(dc, Rect{Left: r.Left + in, Top: r.Top + in, Right: r.Right - in, Bottom: r.Bottom - in}, int32(4*k+0.5), plate)
		} else if Windows11() {
			k := h.scale
			g := NewGraphics(dc)
			c := MakeARGB(255, uint8(plate), uint8(plate>>8), uint8(plate>>16))
			g.FillRoundRect(c, float32(r.Left)+2*k, float32(r.Top)+2*k, float32(r.W())-4*k, float32(r.H())-4*k, 4*k)
			g.Close()
		} else {
			FillColor(dc, r, plate)
		}
	}
	flags := uint32(DT_CENTER|DT_VCENTER|DT_SINGLELINE) | textDirection(h.rtl)
	if state&ODS_NOACCEL != 0 {
		flags |= DT_HIDEPREFIX
	}
	SetBkMode(dc, TRANSPARENT)
	SetTextColor(dc, fore)
	DrawText(dc, MenuStringUnits(d.Menu.Menu, int(d.Position)), &r, flags)
}
