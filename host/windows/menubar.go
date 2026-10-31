//go:build windows && amd64

package windows

import (
	"encoding/binary"
	"strings"
	"unicode"

	. "github.com/egoist/nui/internal/windows"
	"github.com/egoist/nui/ops"
)

// The app's menus (ui.MenuBar): a menu bar (an HMENU) on the main window,
// made again each time they change, and an accelerator table for their
// shortcuts, which the message loop translates for any of the app's
// windows. A chosen item, clicked or by its shortcut, comes as WM_COMMAND
// and sends Action to the root with its index among all the menus'
// items. Popup menus follow the app's dark mode through uxtheme
// (SetAppDark); the bar itself is drawn dark by the window (the UAH
// messages) while the app is.

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
	h.appMenus = parseMenus(b)
	h.appItems = h.appItems[:0]
	for _, m := range h.appMenus {
		h.appItems = append(h.appItems, m.items...)
	}
	var bar uintptr
	if len(h.appMenus) > 0 {
		bar = CreateMenu()
		titles := make([]string, len(h.appMenus))
		for i, m := range h.appMenus {
			titles[i] = m.title
		}
		for i, m := range h.appMenus {
			pop := CreatePopupMenu()
			var names []string
			for _, it := range m.items {
				if it.flags&itemSeparator == 0 {
					names = append(names, it.title)
				}
			}
			names = accessKeys(names)
			k := 0
			for _, it := range m.items {
				if it.flags&itemSeparator != 0 {
					AppendMenu(pop, MF_SEPARATOR, 0, "")
					continue
				}
				flags := uint32(MF_STRING)
				if it.flags&itemDisabled != 0 {
					flags |= MF_GRAYED
				}
				if it.flags&itemChecked != 0 {
					flags |= MF_CHECKED
				}
				text := names[k]
				k++
				if s := shortcutText(it.key, it.mods); s != "" {
					text += "\t" + s
				}
				AppendMenu(pop, flags, uintptr(menuBase+it.index), text)
			}
			AppendMenu(bar, MF_POPUP, pop, accessKeys(titles)[i])
		}
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

	DestroyAcceleratorTable(h.accel)
	var accels []Accel
	for _, it := range h.appItems {
		if it.flags&(itemSeparator|itemDisabled) != 0 {
			continue
		}
		if a, ok := accelerator(it.key, it.mods); ok {
			a.Cmd = uint16(menuBase + it.index)
			accels = append(accels, a)
		}
	}
	h.accel = CreateAcceleratorTable(accels)
}

func parseMenus(b []byte) []appMenu {
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
	var menus []appMenu
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
		menus = append(menus, m)
	}
	return menus
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
// one of the app's windows: Action on the root, unless it's off.
func (h *Host) menuCommand(id int) bool {
	i := id - menuBase
	if i < 0 || i >= len(h.appItems) {
		return false
	}
	if it := h.appItems[i]; it.flags&(itemSeparator|itemDisabled) == 0 && h.root != nil {
		h.emit(h.root, ops.Event{Kind: ops.EventAction, Int: int64(it.index)})
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
		if Windows11() {
			k := h.scale
			g := NewGraphics(dc)
			c := MakeARGB(255, uint8(plate), uint8(plate>>8), uint8(plate>>16))
			g.FillRoundRect(c, float32(r.Left)+2*k, float32(r.Top)+2*k, float32(r.W())-4*k, float32(r.H())-4*k, 4*k)
			g.Close()
		} else {
			FillColor(dc, r, plate)
		}
	}
	flags := uint32(DT_CENTER | DT_VCENTER | DT_SINGLELINE)
	if state&ODS_NOACCEL != 0 {
		flags |= DT_HIDEPREFIX
	}
	SetBkMode(dc, TRANSPARENT)
	SetTextColor(dc, fore)
	DrawText(dc, MenuStringUnits(d.Menu.Menu, int(d.Position)), &r, flags)
}
