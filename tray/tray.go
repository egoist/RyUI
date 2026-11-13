// Package tray puts the app outside its windows: an icon in the tray
// (the menu bar on macOS, the notification area on Windows, the panel's
// status icons on Linux) with its menu, and items of the app's own in
// its Dock icon's menu.
//
//	func App() ui.Node {
//		open := signal.New(false)
//		tray.Show(tray.Props{
//			Symbol:  ui.Val("bolt.fill"),
//			Tooltip: ui.Val("Power"),
//			Menu: ui.Val([]ui.MenuItem{
//				{Title: "Open Power", OnPress: func() { open.Set(true) }},
//				ui.MenuSeparator,
//				ui.MenuQuit,
//			}),
//		})
//		return ui.Window(ui.WindowProps{Open: open, Content: Panel})
//	}
//
// Both are called inside a component, follow their signals, and go when
// its owner is disposed. An app that lives in the tray, with no Dock icon
// and no window at launch, is a background app (app.RunInBackground); one that
// keeps running when its windows close, app.KeepRunning.
//
// What each platform does:
//
//   - macOS: an NSStatusItem in the menu bar. Its menu opens on a click,
//     or, with OnClick, on a secondary click (a right click, a Control
//     click). A Symbol, and an Image that's a Template, are template
//     images, which macOS colors as the menu bar is. DockMenu's items go
//     above the Dock icon's own (applicationDockMenu:).
//   - Windows: an icon in the notification area (Shell_NotifyIcon),
//     shown again when Explorer restarts. A click is OnClick (else it
//     opens the menu), a right click opens the menu; its items are drawn
//     as the menu bar's. A Symbol is Segoe Fluent Icons' glyph, white on
//     a dark taskbar, black on a light one; so is a Template image.
//     DockMenu's items are the Tasks of the taskbar button's jump list
//     (ICustomDestinationList): each starts the app with an argument,
//     which a running app (one with asuka.json's id runs once) is handed;
//     its separators are the jump list's, and submenus, checks and
//     disabled items it can't show are left out.
//   - Linux: a StatusNotifierItem on the session bus, its menu exported
//     with com.canonical.dbusmenu, as libappindicator does, which KDE
//     Plasma, Xfce, Cinnamon, MATE, Budgie, and GNOME with the
//     AppIndicator extension (Ubuntu's default) show. Activate is OnClick
//     (else the panel opens the menu); ContextMenu opens the menu. A
//     Symbol is the icon theme's symbolic icon, or Material's, as the
//     host's views draw it. DockMenu's items are the launcher's quicklist
//     (com.canonical.Unity.LauncherEntry), which Ubuntu's Dock, Dash to
//     Dock, Plank and KDE's task manager show.
//
// Phones and the web have no tray: Supported reports false there, and
// Show and DockMenu do nothing.
package tray

import (
	"encoding/binary"

	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// Props configure a tray icon.
type Props struct {
	// Image is the icon: an image of the app's assets ("tray.png") or a
	// file's path, as ui.Image's Source takes. It's drawn 16 points tall
	// (a PNG 32 pixels tall is sharp on a Retina display), its aspect
	// kept.
	Image signal.Accessor[string]
	// Symbol is the icon when there's no Image: a symbol's name, as
	// ui.Symbol's (an SF Symbol's name, drawn as each platform draws
	// them). It's always a template.
	Symbol signal.Accessor[string]
	// Template says Image is black on transparent, only its alpha
	// counting, for the platform to color as its own icons: macOS as the
	// menu bar is (light, dark, highlighted); Windows white on a dark
	// taskbar and black on a light one; Linux as the panel's text where
	// it recolors symbolic icons, else as the desktop's dark style says.
	Template bool
	// Title is text beside the icon: in the menu bar on macOS, a label
	// where the panel shows one on Linux (XAyatanaLabel: GNOME's
	// AppIndicator extension). Windows has none. On macOS its digits are
	// all as wide (pad a changing number with figure spaces, U+2007, to
	// keep its width).
	Title signal.Accessor[string]
	// Tooltip shows when the pointer rests on the icon.
	Tooltip signal.Accessor[string]
	// Menu is the icon's menu, with submenus; MenuItem's Shortcut isn't
	// shown. It's read again when it changes, and shows its state the
	// next time it opens.
	Menu signal.Accessor[[]ui.MenuItem]
	// OnClick runs on a click (a left click; Linux's Activate); the
	// menu then opens on a secondary click. Without it, a click opens
	// the menu.
	OnClick func()
	// ID names the icon among the app's, so it comes back where the user
	// put it (macOS, where icons are moved with ⌘ held: the status
	// item's autosave name). Default: its place among the app's icons.
	ID string
}

// Module "tray" (desktop hosts):
//
//   - "supported" answers u8 1.
//   - "set" makes or changes an icon: u32 id (1 and up), u8 flags (1
//     template, 2 clickable: OnClick), string image, string symbol,
//     string title, string tooltip, string ID, then its menu, a menu
//     (ops.AppendMenu).
//   - "remove" takes an icon away: u32 id.
//   - "dock" sets the Dock menu's items, a menu (none: none).
//   - "next" answers once something happens, with what: u32 icon id (0:
//     the Dock menu), u8 kind (1 click, 2 a menu item chosen), u32 the
//     item's number (ops.MenuItem.Index).
//   - "stop" answers the "next" waiting, if any, with an error.
//
// A menu item with a role the host does itself (ui.MenuRoleQuit) isn't
// reported.
const module = "tray"

// What "next" reports.
const (
	eventClick = 1
	eventItem  = 2
)

// icon is a tray icon shown.
type icon struct {
	p    Props
	menu []ui.MenuItem // as last sent
	sent []byte
}

var (
	icons    = map[uint32]*icon{}
	nextID   uint32
	docks    []*dock // DockMenu's mounted; the last shows
	sentDock []byte
	dockSet  bool

	following bool
	gen       int // a "next" of another generation is stale
)

type dock struct{ items []ui.MenuItem }

// Icon is an icon Show shows.
type Icon struct{ id uint32 }

// Anchor is the icon as a flyout's anchor (ui.Flyout's): a window that
// hangs from it, a menu bar app's panel, goes below it in the Mac's menu
// bar (under the copy of the menu bar the user clicked, on a Mac with
// several displays) and above it in a taskbar at the bottom of the
// screen, on the side the flyout's Placement names, flipped where the
// display has no room. Linux's panels don't say where their icons are:
// there the flyout goes where the pointer was as the icon was clicked
// (X11), or where the desktop puts a new window (Wayland).
//
//	icon := tray.Show(tray.Props{Symbol: ui.Val("bolt"), OnClick: func() { open.Set(!open.Peek()) }})
//	ui.Window(ui.WindowProps{Open: open, Flyout: &ui.Flyout{Anchor: icon.Anchor()}, FitContent: true, Content: panel})
func (i *Icon) Anchor() signal.Accessor[ui.Anchor] { return ui.Val(ui.Anchor{Tray: i.id}) }

// Show shows an icon in the tray while the component that calls it is
// mounted: it follows p's signals, and goes when the component's owner
// is disposed. Several icons may show; on macOS a new one goes left of
// those there. It returns the icon, which a flyout can hang from
// (Icon.Anchor). Main thread.
func Show(p Props) *Icon {
	nextID++
	id := nextID
	t := &icon{p: p}
	icons[id] = t
	follow()
	signal.RenderEffect(func() {
		var flags byte
		if p.Template {
			flags |= 1
		}
		if p.OnClick != nil {
			flags |= 2
		}
		var menu []ui.MenuItem
		if p.Menu != nil {
			menu = p.Menu.Get()
		}
		b := binary.LittleEndian.AppendUint32(nil, id)
		b = append(b, flags)
		for _, s := range []string{get(p.Image), get(p.Symbol), get(p.Title), get(p.Tooltip), p.ID} {
			b = appendStr(b, s)
		}
		b = append(b, ui.EncodeMenu(menu)...)
		t.menu = menu
		if string(b) == string(t.sent) {
			return
		}
		t.sent = b
		ui.Call(module, "set", b, nil)
	})
	signal.OnCleanup(func() {
		delete(icons, id)
		ui.Call(module, "remove", binary.LittleEndian.AppendUint32(nil, id), nil)
		unfollow()
	})
	return &Icon{id}
}

// DockMenu adds items to the menu of the app's icon in the Dock (macOS),
// above its own; on Windows they're the Tasks of its taskbar button's
// jump list, on Linux its launcher's quicklist (see the package's doc).
// It follows its signal; while several are mounted, the last one shows,
// and when it's disposed, the one before. Main thread.
func DockMenu(items signal.Accessor[[]ui.MenuItem]) {
	d := &dock{}
	docks = append(docks, d)
	follow()
	signal.RenderEffect(func() {
		d.items = items.Get()
		sendDock()
	})
	signal.OnCleanup(func() {
		for i, x := range docks {
			if x == d {
				docks = append(docks[:i], docks[i+1:]...)
				break
			}
		}
		sendDock()
		unfollow()
	})
}

// dockItems are the Dock menu's items: the last DockMenu's.
func dockItems() []ui.MenuItem {
	if len(docks) == 0 {
		return nil
	}
	return docks[len(docks)-1].items
}

func sendDock() {
	b := ui.EncodeMenu(dockItems())
	if dockSet && string(b) == string(sentDock) {
		return
	}
	dockSet, sentDock = true, b
	ui.Call(module, "dock", b, nil)
}

// Supported reports whether the platform has a tray (macOS, Windows,
// Linux; on Linux, whether a StatusNotifierWatcher runs, as the panel's,
// or GNOME's AppIndicator extension). Not on the main thread.
func Supported() bool {
	b, err := ui.CallWait(module, "supported", nil)
	return err == nil && len(b) > 0 && b[0] == 1
}

// follow asks the host for what happens to the icons and the Dock menu,
// a "next" at a time, while there are any. A host without the module
// answers with an error: no tray here.
func follow() {
	if following {
		return
	}
	following = true
	gen++
	poll(gen)
}

func poll(g int) {
	ui.Call(module, "next", nil, func(b []byte, err error) {
		if g != gen {
			return
		}
		if err != nil {
			following = false
			return
		}
		happened(b)
		if g == gen {
			poll(g)
		}
	})
}

// unfollow stops asking once no icon or Dock menu is left.
func unfollow() {
	if !following || len(icons) > 0 || len(docks) > 0 {
		return
	}
	following = false
	gen++
	ui.Call(module, "stop", nil, nil)
}

// happened runs what "next" reported.
func happened(b []byte) {
	if len(b) < 9 {
		return
	}
	id := binary.LittleEndian.Uint32(b)
	kind := b[4]
	index := int(binary.LittleEndian.Uint32(b[5:]))
	if id == 0 {
		if kind == eventItem {
			press(ui.MenuItemAt(dockItems(), index))
		}
		return
	}
	t := icons[id]
	if t == nil {
		return
	}
	switch kind {
	case eventClick:
		if t.p.OnClick != nil {
			t.p.OnClick()
		}
	case eventItem:
		press(ui.MenuItemAt(t.menu, index))
	}
}

func press(it *ui.MenuItem) {
	if it == nil || it.Separator || it.Disabled || it.Role != ui.MenuRoleNone || len(it.Submenu) > 0 || it.OnPress == nil {
		return
	}
	it.OnPress()
}

func get(a signal.Accessor[string]) string {
	if a == nil {
		return ""
	}
	return a.Get()
}

func appendStr(b []byte, s string) []byte {
	b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}
