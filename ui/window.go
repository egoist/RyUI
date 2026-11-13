package ui

import (
	"encoding/binary"
	"math"

	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// Desktop: windows, the menu bar and keyboard shortcuts. A phone has
// none of them: a Window panics on a mobile host (it can't show one), and
// a MenuBar is ignored there.

// WindowProps configures a Window.
type WindowProps struct {
	// Open shows the window while true; the user closing it sets it
	// false.
	Open *signal.Signal[bool]
	// Title is the window's title (nil: the app's name).
	Title signal.Accessor[string]
	// Width and Height are its content's size when it opens, in points
	// (0: the host's default); MinWidth and MinHeight the least the user
	// can make it.
	Width, Height       float32
	MinWidth, MinHeight float32
	Content             func() Node
	// OnClose runs when the user closes the window.
	OnClose func()
}

// Window shows Content in a window of its own (macOS, Windows) while
// Open is true. Its content is built when it opens and disposed when it
// closes; UseEnvironment inside it is the window's (its size follows the
// user resizing it). The node it returns takes no space; put it
// anywhere.
func Window(p WindowProps) Node {
	f := newFragment()
	r := f.r
	owner := signal.CurrentOwner()
	var win *element
	var dispose func()
	closeWindow := func() {
		if win == nil {
			return
		}
		dispose()
		win.destroy()
		win, dispose = nil, nil
	}
	signal.RenderEffect(func() {
		open := p.Open.Get()
		if open == (win != nil) {
			return
		}
		if !open {
			closeWindow()
			return
		}
		withRoot(r, func() Node {
			win = newProvider(ops.KindWindow)
			b := r.batch
			for _, v := range [...]struct {
				prop ops.PropID
				v    float32
			}{{ops.PropWindowWidth, p.Width}, {ops.PropWindowHeight, p.Height}, {ops.PropWindowMinWidth, p.MinWidth}, {ops.PropWindowMinHeight, p.MinHeight}} {
				if v.v > 0 {
					b.SetFloat(win.id, v.prop, float64(v.v))
				}
			}
			win.handlers().dismissed = func() {
				p.Open.Set(false)
				if p.OnClose != nil {
					p.OnClose()
				}
			}
			win.listen(ops.EventDismissed)
			w := win
			signal.RunWithOwner(owner, func() {
				signal.Root(func(d func()) {
					dispose = d
					if p.Title != nil {
						bind(p.Title, func(v string) { r.batch.SetString(w.id, ops.PropWindowTitle, v) })
					}
					envCtx.Provide(w.env, func() {
						if n := p.Content(); n.e != nil {
							fill(n)
							w.appendKid(n.e)
						}
					})
				})
			})
			r.batch.Command(w.id, ops.CmdPresent, nil)
			return Node{}
		})
	})
	signal.OnCleanup(closeWindow)
	return Node{f}
}

// WindowOptions are the main window's: its title (empty: the app's
// name), its content's size when it opens and the least the user can
// make it, in points (0: the host's).
type WindowOptions struct {
	// Title can follow the app's language; WindowTitle sends its changes.
	Title               signal.Accessor[string]
	Width, Height       float32
	MinWidth, MinHeight float32
}

// SetWindowOptions sets the main window's options (the zero value: the
// host's); app.RunWindow calls it before the root mounts.
func SetWindowOptions(o WindowOptions) {
	mainWindow = nil
	if o != (WindowOptions{}) {
		mainWindow = &o
	}
}

var mainWindow *WindowOptions

func (r *Root) sendWindowOptions() {
	o := mainWindow
	if o == nil {
		return
	}
	var b []byte
	for _, v := range [...]float32{o.Width, o.Height, o.MinWidth, o.MinHeight} {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(v))
	}
	if o.Title == nil {
		r.batch.Command(r.el.id, ops.CmdWindowOptions, b)
		return
	}
	id := r.el.id
	first := true
	bind(o.Title, func(title string) {
		if first {
			first = false
			r.batch.Command(id, ops.CmdWindowOptions, append(b, title...))
			return
		}
		r.batch.Command(id, ops.CmdWindowTitle, []byte(title))
	})
}

// Modifiers are a shortcut's modifier keys.
type Modifiers uint8

const (
	// Primary is the platform's command key: ⌘ on macOS, Ctrl on
	// Windows.
	Primary Modifiers = 1 << iota
	Shift
	Alt // a Mac's option
	// Control is the control key itself (on Windows, as Primary).
	Control
)

// Shortcut is a key, with modifiers, that chooses a menu item. Key is a
// character ("n", ",", "=") or a name: enter, escape, tab, space,
// delete, backspace, up, down, left, right, home, end, pageup, pagedown,
// f1 to f12.
type Shortcut struct {
	Key  string
	Mods Modifiers
}

// Cmd is a shortcut of key with the platform's command key (⌘, Ctrl):
// Cmd("n"), Cmd("s").With(Shift).
func Cmd(key string) Shortcut { return Shortcut{Key: key, Mods: Primary} }

// With adds modifiers.
func (s Shortcut) With(m Modifiers) Shortcut { s.Mods |= m; return s }

// MenuItem is an item of a menu: a menu bar's (MenuBar), a tray icon's
// or the Dock icon's (asuka/tray).
type MenuItem struct {
	// Title is what the item says (a role's own when empty).
	Title string
	// Shortcut chooses it from the keyboard (a menu bar's items; other
	// menus show none).
	Shortcut Shortcut
	OnPress  func()
	Disabled bool
	Checked  bool
	// Destructive shows it red, as an item that deletes (macOS,
	// Windows; GTK sets it apart in a section of its own).
	Destructive bool
	// Separator makes it a line between groups of items (the rest is
	// ignored).
	Separator bool
	// Submenu makes it open a menu of these items, beside the menu it's
	// in; its own OnPress, Shortcut and Checked are ignored.
	Submenu []MenuItem
	// Role makes it do what the platform does itself, with the
	// platform's title for it (unless Title says otherwise) and
	// shortcut; OnPress is ignored.
	Role MenuRole
}

// MenuRole is what a menu item does that the platform does itself.
type MenuRole = ops.MenuRole

const (
	MenuRoleNone = ops.MenuRoleNone
	// MenuRoleQuit quits the app ("Quit Notes", ⌘Q in a Mac's menu bar),
	// as the system quits it: lifecycle Background first, so what's
	// pending is saved. An app that keeps running with no window
	// (app.KeepRunning, app.RunInBackground) puts it in its tray icon's menu.
	MenuRoleQuit = ops.MenuRoleQuit
)

// MenuBarMenu is one of the menu bar's menus.
type MenuBarMenu struct {
	Title string
	Items []MenuItem
}

// MenuSeparator is a line between a menu's groups of items.
var MenuSeparator = MenuItem{Separator: true}

// MenuQuit is the item that quits the app (MenuRoleQuit).
var MenuQuit = MenuItem{Role: MenuRoleQuit}

// EncodeMenu is items as the hosts read a menu (ops.DecodeMenu): for a
// package that shows menus of its own (asuka/tray) through a module.
// The item a host reports by its number is MenuItemAt's.
func EncodeMenu(items []MenuItem) []byte { return ops.AppendMenu(nil, menuOps(items)) }

// MenuItemAt is the item numbered i in items, depth first (each
// submenu's items after the item that opens it; separators and
// submenus count), as hosts number them; nil when there's none.
func MenuItemAt(items []MenuItem, i int) *MenuItem {
	var at func([]MenuItem) *MenuItem
	at = func(items []MenuItem) *MenuItem {
		for k := range items {
			if i == 0 {
				return &items[k]
			}
			i--
			if items[k].Separator {
				continue
			}
			if it := at(items[k].Submenu); it != nil {
				return it
			}
		}
		return nil
	}
	if i < 0 {
		return nil
	}
	return at(items)
}

// menuOps is items in ops' form.
func menuOps(items []MenuItem) []ops.MenuItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]ops.MenuItem, len(items))
	for i, it := range items {
		var f ops.MenuFlags
		if it.Separator {
			f |= ops.MenuSeparator
		}
		if it.Disabled {
			f |= ops.MenuDisabled
		}
		if it.Checked {
			f |= ops.MenuChecked
		}
		if it.Destructive {
			f |= ops.MenuDestructive
		}
		out[i] = ops.MenuItem{Flags: f, Title: it.Title, Key: it.Shortcut.Key, Mods: uint8(it.Shortcut.Mods), Role: it.Role}
		if !it.Separator {
			out[i].Items = menuOps(it.Submenu)
		}
	}
	return out
}

// pressMenuItem runs the item's OnPress, unless it's the host's own or
// opens a submenu (the hosts send no disabled item).
func pressMenuItem(it *MenuItem) {
	if it == nil || it.Separator || it.Role != MenuRoleNone || len(it.Submenu) > 0 {
		return
	}
	call(it.OnPress)
}

// MenuBar gives the app these menus (macOS, Windows), with their
// keyboard shortcuts, beside the ones the platform makes: on a Mac the
// app's menu, Edit and Window; a menu titled as one of those (File,
// Edit, View, Window, Help) adds its items to it. Menus follows its
// signals: an item turned off or checked, a menu that comes and goes.
// While several are mounted, the last one shows; when it's disposed, the
// one before. A phone has no menu bar: mobile hosts ignore it. The node
// it returns takes no space; put it anywhere.
func MenuBar(menus signal.Accessor[[]MenuBarMenu]) Node {
	f := newFragment()
	r := f.r
	bar := &menuBar{}
	r.menuBars = append(r.menuBars, bar)
	signal.RenderEffect(func() {
		bar.menus = menus.Get()
		r.sendMenuBar()
	})
	signal.OnCleanup(func() {
		for i, b := range r.menuBars {
			if b == bar {
				r.menuBars = append(r.menuBars[:i], r.menuBars[i+1:]...)
				break
			}
		}
		r.sendMenuBar()
	})
	return Node{f}
}

type menuBar struct{ menus []MenuBarMenu }

// sendMenuBar sends the menus of the last MenuBar mounted (none: no
// menus of the app's), if they changed.
func (r *Root) sendMenuBar() {
	var menus []MenuBarMenu
	if n := len(r.menuBars); n > 0 {
		menus = r.menuBars[n-1].menus
	}
	bar := make([]ops.MenuBarMenu, len(menus))
	r.menuItems = r.menuItems[:0]
	for i, m := range menus {
		bar[i] = ops.MenuBarMenu{Title: m.Title, Items: menuOps(m.Items)}
		r.menuItems = append(r.menuItems, m.Items)
	}
	b := ops.AppendMenuBar(nil, bar)
	if !r.menuListen {
		r.menuListen = true
		r.el.handlers().action = func(i int64) {
			for _, items := range r.menuItems {
				n := int64(menuCount(items))
				if i < n {
					pressMenuItem(MenuItemAt(items, int(i)))
					return
				}
				i -= n
			}
		}
		r.el.listen(ops.EventAction)
	}
	if string(b) == string(r.menuSent) {
		return
	}
	r.menuSent = b
	r.batch.Command(r.el.id, ops.CmdMenuBar, b)
}

// menuCount is how many items items numbers, with its submenus'.
func menuCount(items []MenuItem) int {
	n := len(items)
	for _, it := range items {
		if !it.Separator {
			n += menuCount(it.Submenu)
		}
	}
	return n
}
