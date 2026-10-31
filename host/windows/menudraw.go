//go:build windows && amd64

package windows

import (
	"strings"

	. "github.com/egoist/ryui/internal/windows"
)

// Destructive menu items: a Win32 menu can't color an item's text, so a
// menu with a destructive item (a MenuBar item with Destructive, an
// Action with ActionDestructive in a popup menu) is owner-drawn, the
// destructive items red (Fluent's critical text color). All its items
// are: a menu with one owner-drawn item loses its visual style for the
// others, which the system then draws as classic menus. They're drawn as
// the system draws a themed menu's, with the visual style's Menu parts
// (Windows 11's, ImmersiveStart's) at the menu's DPI: the item height
// and margins, the check mark, the separator, the menu font, shortcuts
// right-aligned in a column of their own, gray when disabled; selected,
// the rounded backplate under the mouse and the focus outline from the
// keyboard. The items keep their text, so access keys and arrows work as
// in any menu.

type ownerItem struct {
	menu        *ownerMenu
	text        []uint16 // its title, & before its access key
	key         []uint16 // its shortcut's text
	separator   bool
	destructive bool
}

// ownerMenu is an owner-drawn menu: its items share a text and a
// shortcut column.
type ownerMenu struct {
	menu  uintptr
	items []*ownerItem
	ids   []uintptr
}

// menuItem is an item of a menu to owner-draw: the text AppendMenu took
// (its shortcut after a tab).
type menuItem struct {
	text        string
	separator   bool
	destructive bool
}

// ownerDrawMenu makes all of a menu's items owner-drawn (it has a
// destructive one).
func (h *Host) ownerDrawMenu(m uintptr, items []menuItem) *ownerMenu {
	if h.owned == nil {
		h.owned = map[uintptr]*ownerItem{}
	}
	om := &ownerMenu{menu: m}
	for i, mi := range items {
		title, key, _ := strings.Cut(mi.text, "\t")
		it := &ownerItem{menu: om, text: UTF16Units(title), key: UTF16Units(key), separator: mi.separator, destructive: mi.destructive}
		h.ownedNext++
		id := h.ownedNext
		h.owned[id] = it
		om.items = append(om.items, it)
		om.ids = append(om.ids, id)
		flags := uint32(0)
		if it.separator {
			flags = MF_SEPARATOR
		}
		SetMenuItemOwnerDraw(m, i, id, flags)
	}
	h.ownedMenus = append(h.ownedMenus, om)
	return om
}

// forget lets go of an owner-drawn menu's items (it's gone).
func (h *Host) forget(om *ownerMenu) {
	if om == nil {
		return
	}
	for _, id := range om.ids {
		delete(h.owned, id)
	}
	for i, x := range h.ownedMenus {
		if x == om {
			h.ownedMenus = append(h.ownedMenus[:i], h.ownedMenus[i+1:]...)
			break
		}
	}
}

// stripAccess is a title as it shows: & marks dropped, && one &.
func stripAccess(s []uint16) []uint16 {
	out := make([]uint16, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '&' {
			if i+1 < len(s) && s[i+1] == '&' {
				i++
			} else {
				continue
			}
		}
		out = append(out, s[i])
	}
	return out
}

// menuStyle is what a menu's items are drawn with at a DPI, light or
// dark: the visual style's Menu class and its metrics, and the menu
// font.
type menuStyle struct {
	theme      uintptr
	sepTheme   uintptr // the separator's
	checkTheme uintptr // the check mark's
	alt        uintptr // the other class they may be from
	font       uintptr
	k          float32
	check      [2]int32 // the check mark's size
	checkM     Margins  // around it
	checkBgM   Margins  // around its background
	itemM      Margins  // the item's content
	arrow      int32    // the submenu arrow's width: the column right of the shortcuts
	arrowM     Margins  // around it
	sep        int32    // the separator's height
	text       uint32   // COLORREFs: the text's, disabled
	disabled   uint32
}

type menuStyleKey struct {
	dpi  int
	dark bool
}

func (h *Host) menuStyle(hwnd uintptr, dpi int) *menuStyle {
	key := menuStyleKey{dpi, h.dark}
	if s := h.menuStyles[key]; s != nil {
		return s
	}
	if h.menuStyles == nil {
		h.menuStyles = map[menuStyleKey]*menuStyle{}
	}
	class := "Menu"
	switch {
	case Windows11() && h.dark:
		class = "DarkMode_ImmersiveStart::Menu"
	case Windows11():
		class = "ImmersiveStart::Menu"
	case h.dark:
		class = "DarkMode::Menu"
	}
	s := &menuStyle{k: dpiScale(dpi)}
	s.theme = OpenTheme(hwnd, class, dpi)
	if s.theme == 0 {
		s.theme = OpenTheme(hwnd, "Menu", dpi)
	}
	// As the system's menus on Windows 11 are: in light, the separator
	// is the plain Menu class's; in dark, the check mark and the
	// disabled gray are DarkMode::Menu's.
	s.sepTheme, s.checkTheme = s.theme, s.theme
	if Windows11() {
		alt := "Menu"
		if h.dark {
			alt = "DarkMode::Menu"
		}
		if t := OpenTheme(hwnd, alt, dpi); t != 0 {
			s.alt = t
			if h.dark {
				s.checkTheme = t
			} else {
				s.sepTheme = t
			}
		}
	}
	lf := MenuFont(dpi)
	s.font = CreateFont(&lf)
	dc := h.measureDC
	s.check[0], s.check[1] = ThemePartSize(s.theme, dc, MENU_POPUPCHECK, MC_CHECKMARKNORMAL)
	s.checkM = ThemeMargins(s.theme, dc, MENU_POPUPCHECK, 0, TMT_CONTENTMARGINS)
	s.checkBgM = ThemeMargins(s.theme, dc, MENU_POPUPCHECKBACKGROUND, 0, TMT_CONTENTMARGINS)
	s.itemM = ThemeMargins(s.theme, dc, MENU_POPUPITEM, 0, TMT_CONTENTMARGINS)
	s.arrow, _ = ThemePartSize(s.theme, dc, MENU_POPUPSUBMENU, 0)
	s.arrowM = ThemeMargins(s.theme, dc, MENU_POPUPSUBMENU, 0, TMT_CONTENTMARGINS)
	_, s.sep = ThemePartSize(s.theme, dc, MENU_POPUPSEPARATOR, 0)
	var ok bool
	if s.text, ok = ThemeColor(s.theme, MENU_POPUPITEM, MPI_NORMAL, TMT_TEXTCOLOR); !ok {
		s.text = SysColor(COLOR_MENUTEXT)
	}
	if s.disabled, ok = ThemeColor(s.checkTheme, MENU_POPUPITEM, MPI_DISABLED, TMT_TEXTCOLOR); !ok {
		s.disabled = SysColor(COLOR_GRAYTEXT)
	}
	h.menuStyles[key] = s
	return s
}

// clearMenuStyles drops the menu styles (the theme changed).
func (h *Host) clearMenuStyles() {
	for k, s := range h.menuStyles {
		CloseTheme(s.theme)
		CloseTheme(s.alt)
		DeleteObject(s.font)
		delete(h.menuStyles, k)
	}
}

// The metrics, in pixels, as the system lays out a themed menu's items:
// the selection inset into the item, then the check mark's column with
// its margins, then the text; the shortcut right-aligned before the
// submenu arrow's column.
func (s *menuStyle) px(v int32) int32  { return int32(float32(v)*s.k + 0.5) }
func (s *menuStyle) inset() int32      { return s.px(3) }
func (s *menuStyle) checkLeft() int32  { return s.inset() + s.px(s.checkM.Left) }
func (s *menuStyle) itemHeight() int32 { return s.check[1] + s.px(s.checkM.Top+s.checkM.Bottom) }
func (s *menuStyle) sepHeight() int32  { return s.sep + s.px(s.itemM.Left+s.itemM.Right) }
func (s *menuStyle) keyGap() int32     { return s.px(24) }
func (s *menuStyle) right() int32 {
	return s.inset() + s.px(s.arrowM.Left) + s.arrow + s.px(s.arrowM.Right)
}
func (s *menuStyle) textLeft() int32 {
	return s.checkLeft() + s.check[0] + s.px(s.checkM.Right) + s.px(s.checkBgM.Right) + s.px(s.itemM.Left)
}

// columns are an owner-drawn menu's widest text and shortcut, in pixels.
func (om *ownerMenu) columns(dc uintptr) (text, key int32) {
	for _, it := range om.items {
		if it.separator {
			continue
		}
		w, _ := TextExtent(dc, stripAccess(it.text))
		text = max(text, w)
		if len(it.key) > 0 {
			w, _ := TextExtent(dc, it.key)
			key = max(key, w)
		}
	}
	return text, key
}

// menuItemMessage measures and draws the owner-drawn menu items, keeps
// their menus open on keys no item takes, gives them their background
// and follows their menu windows, for the window that owns their menu.
func (h *Host) menuItemMessage(hwnd uintptr, m uint32, wp, lp uintptr) (uintptr, bool) {
	switch m {
	case WM_MEASUREITEM:
		mi := Native[MeasureItem](lp)
		it := h.owned[mi.ItemData]
		if mi.CtlType != ODT_MENU || it == nil {
			return 0, false
		}
		dpi := DPI(hwnd)
		s := h.menuStyle(hwnd, dpi)
		if it.separator {
			mi.ItemHeight = uint32(s.sepHeight())
			return 1, true
		}
		dc := h.measureDC
		old := SelectObject(dc, s.font)
		tw, kw := it.menu.columns(dc)
		SelectObject(dc, old)
		w := s.textLeft() + tw + s.right()
		if kw > 0 {
			w += s.keyGap() + kw
		}
		// The system adds the check mark's width to an owner-drawn item's
		// (less 3 pixels, as measured on Windows 11).
		mi.ItemWidth = uint32(max(w-int32(SystemMetric(SM_CXMENUCHECK, dpi)-3), 0))
		mi.ItemHeight = uint32(s.itemHeight())
		return 1, true
	case WM_DRAWITEM:
		d := Native[DrawItem](lp)
		it := h.owned[d.ItemData]
		if d.CtlType != ODT_MENU || it == nil {
			return 0, false
		}
		h.drawMenuItem(h.menuStyle(hwnd, DPI(hwnd)), d, it)
		return 1, true
	case WM_INITMENUPOPUP:
		// Its background, light or dark as the app is now.
		for _, om := range h.ownedMenus {
			if om.menu == wp {
				SetMenuBackground(wp, h.brush(h.menuBackground()))
			}
		}
		return 0, false
	case WM_INITMENU, WM_ENTERMENULOOP, WM_EXITMENULOOP:
		h.menuLoop(m != WM_EXITMENULOOP)
		return 0, false
	case WM_MENUCHAR:
		// The items keep their text, so the system takes their access
		// keys (and first letters) itself, as in any menu; a key none
		// takes is ignored, the menu staying open as a themed one does
		// (for an owner-drawn one, DefWindowProc would close it).
		for _, om := range h.ownedMenus {
			if om.menu == lp {
				return 0, true // MNC_IGNORE
			}
		}
	}
	return 0, false
}

// drawMenuItem draws an owner-drawn menu's item as the system draws a
// themed menu's, a destructive one's text red.
func (h *Host) drawMenuItem(s *menuStyle, d *DrawItem, it *ownerItem) {
	dc, r, state := d.DC, d.Item, d.ItemState
	FillColor(dc, r, h.menuBackground().ref())
	if it.separator {
		top := r.Top + (r.H()-s.sep)/2
		DrawThemeBackground(s.sepTheme, dc, MENU_POPUPSEPARATOR, 0, Rect{Left: r.Left + s.px(6), Top: top, Right: r.Right - s.px(6), Bottom: top + s.sep})
		return
	}
	disabled := state&(ODS_GRAYED|ODS_DISABLED) != 0
	sel := Rect{Left: r.Left + s.inset(), Top: r.Top, Right: r.Right - s.inset(), Bottom: r.Bottom}
	DrawThemeBackground(s.theme, dc, MENU_POPUPITEM, MPI_NORMAL, sel)
	if state&ODS_SELECTED != 0 {
		if state&ODS_NOFOCUSRECT != 0 {
			// Under the mouse: the backplate.
			st := MPI_HOT
			if disabled {
				st = MPI_DISABLEDHOT
			}
			DrawThemeBackground(s.theme, dc, MENU_POPUPITEM, st, sel)
		} else {
			// From the keyboard: the focus outline.
			h.menuFocus(dc, sel, s.k)
		}
	}
	if state&ODS_CHECKED != 0 {
		st := MC_CHECKMARKNORMAL
		if disabled {
			st = MC_CHECKMARKDISABLED
		}
		top := r.Top + (r.H()-s.check[1])/2
		left := r.Left + s.checkLeft()
		DrawThemeBackground(s.checkTheme, dc, MENU_POPUPCHECK, st, Rect{Left: left, Top: top, Right: left + s.check[0], Bottom: top + s.check[1]})
	}
	color := s.text
	if disabled {
		color = s.disabled
	}
	old := SelectObject(dc, s.font)
	SetBkMode(dc, TRANSPARENT)
	// A destructive item's title is red, its shortcut as the others' (as
	// macOS's).
	if it.destructive && !disabled {
		SetTextColor(dc, h.theme.red.ref())
	} else {
		SetTextColor(dc, color)
	}
	flags := uint32(DT_SINGLELINE | DT_VCENTER | DT_LEFT)
	if state&ODS_NOACCEL != 0 {
		flags |= DT_HIDEPREFIX
	}
	tr := Rect{Left: r.Left + s.textLeft(), Top: r.Top, Right: r.Right - s.right(), Bottom: r.Bottom}
	DrawText(dc, it.text, &tr, flags)
	if len(it.key) > 0 {
		SetTextColor(dc, color)
		DrawText(dc, it.key, &tr, DT_SINGLELINE|DT_VCENTER|DT_RIGHT|DT_NOPREFIX)
	}
	SelectObject(dc, old)
}

// menuBackground is a popup menu's background: Windows 11's, light or
// dark (its items' own part, MENU_POPUPITEM, is #2C2C2C over it).
func (h *Host) menuBackground() rgba {
	if h.dark {
		return hex(0x2B2B2B)
	}
	return hex(0xF9F9F9)
}

// menuFocus draws the keyboard focus outline of Windows 11's menus: a
// hairline rounded rectangle in the text's color.
func (h *Host) menuFocus(dc uintptr, r Rect, k float32) {
	c := uint8(0)
	if h.dark {
		c = 255
	}
	w := max(1, float32(int32(k/2+0.5)))
	g := NewGraphics(dc)
	// On whole pixels: GDI+ puts their centers at half coordinates.
	g.StrokeRoundRect(MakeARGB(255, c, c, c), w, float32(r.Left), float32(r.Top), float32(r.W()), float32(r.H()), 1.75*k)
	g.Close()
}

// The menu window of an owner-drawn menu: the system draws it a classic
// frame (square, a dark line and COLOR_MENU), so it's drawn as Windows
// 11's menus are: the menu's background, with DWM's rounded corners and
// border around it.

// menuLoop starts and ends following the menu windows the system makes
// while a menu is open.
func (h *Host) menuLoop(enter bool) {
	if !enter {
		StopWatchingMenuWindows()
		return
	}
	if len(h.ownedMenus) > 0 {
		WatchMenuWindows(func(w uintptr) { Subclass(w, h.menuWindowMessage) })
	}
}

func (h *Host) menuWindowMessage(hwnd uintptr, m uint32, wp, lp uintptr) uintptr {
	switch m {
	case WM_NCPAINT, WM_PRINT:
		if om := h.menuOf(hwnd); om != nil {
			if m == WM_PRINT {
				r := DefSubclass(hwnd, m, wp, lp)
				h.paintMenuFrame(hwnd, wp)
				return r
			}
			h.roundMenu(hwnd)
			dc := WindowDC(hwnd)
			h.paintMenuFrame(hwnd, dc)
			ReleaseDC(hwnd, dc)
			return 0
		}
	case WM_NCCREATE:
		h.roundMenu(hwnd)
	case WM_NCDESTROY:
		Unsubclass(hwnd)
	}
	return DefSubclass(hwnd, m, wp, lp)
}

// menuOf is the owner-drawn menu a menu window shows, if it shows one.
func (h *Host) menuOf(hwnd uintptr) *ownerMenu {
	m := SendMessage(hwnd, MN_GETHMENU, 0, 0)
	for _, om := range h.ownedMenus {
		if om.menu == m {
			return om
		}
	}
	return nil
}

// roundMenu has DWM round a menu window's corners and draw its border,
// as Windows 11's own menus are.
func (h *Host) roundMenu(hwnd uintptr) {
	if !Windows11() {
		return
	}
	SetCornerPreference(hwnd, 3) // DWMWCP_ROUNDSMALL
	border := uint32(0xE5E5E5)
	if h.dark {
		border = 0x3E3E3E
	}
	SetBorderColor(hwnd, border)
}

// paintMenuFrame paints a menu window's frame (what's outside its client
// area) in its background.
func (h *Host) paintMenuFrame(hwnd, dc uintptr) {
	wr, cr := WindowRect(hwnd), ClientRect(hwnd)
	w, ht := wr.W(), wr.H()
	// The client area, in window coordinates.
	p := ClientToScreen(hwnd, Point{})
	c := Rect{Left: p.X - wr.Left, Top: p.Y - wr.Top}
	c.Right, c.Bottom = c.Left+cr.W(), c.Top+cr.H()
	bg := h.menuBackground().ref()
	FillColor(dc, Rect{Right: w, Bottom: c.Top}, bg)
	FillColor(dc, Rect{Top: c.Bottom, Right: w, Bottom: ht}, bg)
	FillColor(dc, Rect{Top: c.Top, Right: c.Left, Bottom: c.Bottom}, bg)
	FillColor(dc, Rect{Left: c.Right, Top: c.Top, Right: w, Bottom: c.Bottom}, bg)
}
