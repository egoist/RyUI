package ui

import (
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// Desktop: windows, the menu bar and keyboard shortcuts. A phone has
// none of them: a Window panics on a mobile host (it can't show one), a
// WindowHandle does nothing there, and a MenuBar is ignored there.

// TitleBarStyle is how a window's title bar looks.
type TitleBarStyle = ops.TitleBar

const (
	// TitleBarDefault is the platform's title bar, above the content.
	TitleBarDefault = ops.TitleBarDefault
	// TitleBarHidden hides the title bar's background and title and lays
	// the content out under it, to the window's top edge; the window
	// buttons stay, over the content (a Mac's traffic lights; Windows'
	// minimize, maximize and close; the header bar's on Linux, flat).
	// The window's environment's SafeTop is the title bar's height, so
	// SafeArea keeps clear of the buttons, and the strip still drags the
	// window where the content takes no clicks.
	TitleBarHidden = ops.TitleBarHidden
)

// WindowProps configures a Window. Sizes are its content's, in points;
// X and Y its frame's top left on the screen, in points from the main
// display's top left.
type WindowProps struct {
	// Open shows the window while true; the user closing it sets it
	// false.
	Open *signal.Signal[bool]
	// Title is the window's title (nil: the app's name).
	Title signal.Accessor[string]
	// Width and Height are its content's size when it opens, in points
	// (0: the host's default); MinWidth and MinHeight the least the user
	// can make it, MaxWidth and MaxHeight the most (0: no most).
	Width, Height       float32
	MinWidth, MinHeight float32
	MaxWidth, MaxHeight float32
	// X and Y place it; with both 0 it opens down and right of the window
	// in use, as new windows cascade, or centered on its display with
	// Center. Under Wayland the compositor places windows itself: an app
	// has no way to.
	X, Y   float32
	Center bool
	// DisableResize, DisableMinimize, DisableMaximize and DisableClose
	// take the user's abilities away: resizing it, and its buttons.
	DisableResize, DisableMinimize, DisableMaximize, DisableClose bool
	// AlwaysOnTop keeps it above other apps' windows (not under Wayland,
	// where an app can't ask for it).
	AlwaysOnTop bool
	TitleBar    TitleBarStyle
	// Background fills the window behind its content (0: the platform's
	// window background); Transparent shows what's behind the window
	// where the content draws nothing (macOS, Linux with a compositor).
	Background  Color
	Transparent bool
	// Hidden opens it without showing it: the content is built, laid out
	// and drawn, and WindowHandle.Show shows it ready, with no flash of
	// an empty window.
	Hidden bool
	// SkipTaskbar leaves it out of the taskbar (Windows) and the Window
	// menu (macOS).
	SkipTaskbar bool
	// Maximized and Fullscreen open it so.
	Maximized, Fullscreen bool
	// StateKey remembers where the user left it, its position, size and
	// whether it was maximized, across launches, under this key: it opens
	// there next time, over Width, Height, X and Y. Give each kind of
	// window its own ("inspector", "preferences").
	StateKey string
	// Child makes it a child of the window it's declared in (the main
	// window, or the Window whose content has it): it stays above that
	// window and closes with it. Modal makes it a child that blocks that
	// window until it closes (on a Mac, a sheet of it).
	Child, Modal bool
	// Flyout, if set, makes it a flyout: a window without a frame or a
	// taskbar button hanging from an anchor, a tray icon (a menu bar
	// app's panel) or a rectangle of the window it's declared in, placed
	// on the side Placement names and kept in its display's work area. It
	// takes the keyboard as it shows and closes, as the user closing it,
	// as it loses the keyboard (a click elsewhere, another app) or on an
	// Escape its content leaves, as menus and popovers do; a Modal Window
	// declared in it, or an Alert, keeps it open while it shows. With no
	// Background it has the system's material behind its content (a
	// Mac's menus'), rounded corners and a shadow.
	Flyout *Flyout
	// Panel makes it a panel, a launcher's or a command palette's window:
	// it floats above every app's windows, on whichever Space or virtual
	// desktop the user is on and over apps in full screen, out of the
	// taskbar, the Dock's windows and the window cycle, and takes the
	// keyboard without activating the app on a Mac, so the app the user
	// was in stays in front. Frameless, with the system's material behind
	// its content unless it has a Background, it closes, as the user
	// closing it, on an Escape its content leaves or as it loses the
	// keyboard: give it OnCloseRequest to hide it instead, so it shows
	// again at once. Without X and Y it shows on the display with the
	// pointer, centered across it, a third of the way down.
	Panel bool
	// FitContent sizes the window to its content, and follows it as it
	// changes, within MinWidth, MaxWidth, MinHeight and MaxHeight (and its
	// display): its width the content's widest line (max-content), its
	// height the content's at that width, beyond MaxHeight a Scroll in it
	// scrolls. A flyout is placed again against its anchor as it resizes;
	// a panel or a window keeps its top edge. Width and Height are its size
	// until its content is laid out, which happens before it shows. The
	// user can't resize it.
	FitContent bool
	// ClickThrough lets the mouse go through the window to what's behind
	// it, another app's window or the desktop, as an overlay's or a HUD's
	// does (with Transparent and AlwaysOnTop); WindowHandle.SetClickThrough
	// switches it.
	ClickThrough bool
	// Handle, if set, is the window's handle (NewWindowHandle), to
	// control it from outside it: UseWindow in its content returns the
	// same.
	Handle  *WindowHandle
	Content func() Node
	// OnClose runs when the user closes the window.
	OnClose func()
	// OnCloseRequest, if set, is asked when the user closes the window
	// (or WindowHandle.Close is called): false keeps it open, to ask
	// about unsaved changes first; WindowHandle.Destroy, or Open set
	// false, closes it then.
	OnCloseRequest func() bool
}

func (p *WindowProps) config() ops.WindowConfig {
	c := ops.WindowConfig{
		Width: p.Width, Height: p.Height, MinWidth: p.MinWidth, MinHeight: p.MinHeight,
		MaxWidth: p.MaxWidth, MaxHeight: p.MaxHeight, X: p.X, Y: p.Y,
		TitleBar: p.TitleBar, Background: p.Background, StateKey: p.StateKey,
	}
	c.Flags = windowFlags(p.DisableResize, p.DisableMinimize, p.DisableMaximize, p.DisableClose, p.AlwaysOnTop,
		p.Hidden, p.SkipTaskbar, p.Transparent, p.Maximized, p.Fullscreen)
	if p.Center {
		c.Flags |= ops.WindowCentered
	}
	if p.X != 0 || p.Y != 0 {
		c.Flags |= ops.WindowPositioned
	}
	if p.Child {
		c.Flags |= ops.WindowChild
	}
	if p.Modal {
		c.Flags |= ops.WindowChild | ops.WindowModal
	}
	c.Flags |= kindFlags(p.Panel, p.FitContent, p.ClickThrough)
	if p.Flyout != nil {
		c.Flags |= ops.WindowFlyout | ops.WindowSkipTaskbar | ops.WindowNoMinimize | ops.WindowNoMaximize
		c.Flags &^= ops.WindowPanel | ops.WindowChild | ops.WindowModal | ops.WindowCentered | ops.WindowPositioned |
			ops.WindowMaximized | ops.WindowFullscreen
	}
	return c
}

// kindFlags are the flags of a panel, a window sized to its content and
// one the mouse goes through.
func kindFlags(panel, fit, clickThrough bool) ops.WindowFlags {
	var f ops.WindowFlags
	if panel {
		f |= ops.WindowPanel | ops.WindowAlwaysOnTop | ops.WindowSkipTaskbar | ops.WindowNoMinimize | ops.WindowNoMaximize
	}
	if fit {
		f |= ops.WindowFitContent | ops.WindowFixedSize
	}
	if clickThrough {
		f |= ops.WindowClickThrough
	}
	return f
}

// Placement is where a flyout goes from its anchor: the side, and how it
// lines up with the anchor along that side.
type Placement = ops.Placement

const (
	// PlacementBottom is below the anchor, centered under it: a menu bar
	// icon's panel (a taskbar's at the bottom of the screen flips above
	// it).
	PlacementBottom      = ops.PlacementBottom
	PlacementBottomStart = ops.PlacementBottomStart // below, left edges lined up
	PlacementBottomEnd   = ops.PlacementBottomEnd   // below, right edges lined up
	PlacementTop         = ops.PlacementTop
	PlacementTopStart    = ops.PlacementTopStart
	PlacementTopEnd      = ops.PlacementTopEnd
	PlacementRight       = ops.PlacementRight
	PlacementRightStart  = ops.PlacementRightStart // right of it, top edges lined up
	PlacementRightEnd    = ops.PlacementRightEnd
	PlacementLeft        = ops.PlacementLeft
	PlacementLeftStart   = ops.PlacementLeftStart
	PlacementLeftEnd     = ops.PlacementLeftEnd
)

// Anchor is what a flyout hangs from: a tray icon (tray.Icon's Anchor
// gives one, its Tray set) or a rectangle of the window the flyout is
// declared in, in points from the top left of its content (a rectangle of
// no size is a point).
type Anchor struct {
	// Tray is a tray icon's (from tray.Icon.Anchor); 0: the rectangle.
	Tray                uint32
	X, Y, Width, Height float32
}

// Flyout is a flyout window's anchor and placement (WindowProps.Flyout).
type Flyout struct {
	// Anchor is what it hangs from; it follows the accessor, placed
	// again as it changes.
	Anchor signal.Accessor[Anchor]
	// Placement is the side of the anchor it goes to (PlacementBottom:
	// below it, centered): where the display has no room for it there, it
	// flips to the other side, then slides into the display, then
	// shrinks to fit it.
	Placement Placement
	// Gap is the room between the anchor and the flyout, in points.
	Gap float32
}

func windowFlags(noResize, noMin, noMax, noClose, onTop, hidden, skipTaskbar, transparent, maximized, fullscreen bool) ops.WindowFlags {
	var f ops.WindowFlags
	for _, b := range [...]struct {
		on bool
		f  ops.WindowFlags
	}{
		{noResize, ops.WindowFixedSize}, {noMin, ops.WindowNoMinimize}, {noMax, ops.WindowNoMaximize},
		{noClose, ops.WindowNoClose}, {onTop, ops.WindowAlwaysOnTop}, {hidden, ops.WindowHidden},
		{skipTaskbar, ops.WindowSkipTaskbar}, {transparent, ops.WindowTransparent},
		{maximized, ops.WindowMaximized}, {fullscreen, ops.WindowFullscreen},
	} {
		if b.on {
			f |= b.f
		}
	}
	return f
}

// Window shows Content in a window of its own (macOS, Windows, Linux; on
// the web, a window in the page) while Open is true. Its content is built
// when it opens and disposed when it closes; UseEnvironment inside it is
// the window's (its size follows the user resizing it), and UseWindow its
// handle. The node it returns takes no space; put it anywhere.
func Window(p WindowProps) Node {
	f := newFragment()
	r := f.r
	owner := signal.CurrentOwner()
	parent := windowCtx.Use()
	if parent == nil {
		parent = r.win
	}
	wh := p.Handle
	if wh == nil {
		wh = NewWindowHandle()
	} else {
		wh.watched = true // the app reads it from outside
	}
	wh.r = r
	wh.open = p.Open
	wh.ask = p.OnCloseRequest
	var win *element
	var dispose func()
	closeWindow := func() {
		if win == nil {
			return
		}
		dispose()
		win.destroy()
		win, dispose = nil, nil
		wh.detach()
	}
	// The user's closing, or Close that OnCloseRequest let through.
	wh.closed = func() {
		p.Open.Set(false)
		if p.OnClose != nil {
			p.OnClose()
		}
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
			c := p.config()
			if pe := parent.el(); c.Flags&(ops.WindowChild|ops.WindowFlyout) != 0 && pe != nil {
				c.Parent = pe.id
			} else {
				c.Flags &^= ops.WindowChild | ops.WindowModal
			}
			if c.Flags&ops.WindowFitContent != 0 {
				fitWindow(win, &c)
			}
			r.batch.SetBytes(win.id, ops.PropWindowConfig, ops.AppendWindowConfig(nil, &c))
			win.handlers().dismissed = func() {
				if win != nil {
					wh.closed()
				}
			}
			win.listen(ops.EventDismissed)
			w := win
			wh.attach(w)
			signal.RunWithOwner(owner, func() {
				signal.Root(func(d func()) {
					dispose = d
					if p.Title != nil {
						bind(p.Title, func(v string) { r.batch.SetString(w.id, ops.PropWindowTitle, v) })
					}
					if f := p.Flyout; f != nil {
						var sent []byte
						signal.RenderEffect(func() {
							var a Anchor
							if f.Anchor != nil {
								a = f.Anchor.Get()
							}
							b := ops.AppendFlyout(sent[:0:0], ops.Flyout{Tray: a.Tray, X: a.X, Y: a.Y, Width: a.Width, Height: a.Height,
								Placement: f.Placement, Gap: f.Gap})
							if string(b) != string(sent) {
								sent = b
								r.batch.SetBytes(w.id, ops.PropWindowFlyout, b)
							}
						})
					}
					// Opened before its content is built: a Window declared
					// in it, a child of it, opens after it.
					r.batch.Command(w.id, ops.CmdPresent, nil)
					windowCtx.Provide(wh, func() {
						envCtx.Provide(w.env, func() {
							if n := p.Content(); n.e != nil {
								fill(n)
								w.appendKid(n.e)
							}
						})
					})
				})
			})
			return Node{}
		})
	})
	signal.OnCleanup(closeWindow)
	return Node{f}
}

// WindowOptions are the main window's (app.RunWindow): what it opens
// with, as WindowProps' are a Window's. Sizes are its content's, in
// points (0: the host's: 800 by 600 as it opens, and 320 square at the
// least).
type WindowOptions struct {
	// Title can follow the app's language; WindowTitle sends its changes.
	Title               signal.Accessor[string]
	Width, Height       float32
	MinWidth, MinHeight float32
	MaxWidth, MaxHeight float32
	// X and Y place it; with both 0 it's centered on the main display.
	X, Y                                                          float32
	DisableResize, DisableMinimize, DisableMaximize, DisableClose bool
	AlwaysOnTop                                                   bool
	TitleBar                                                      TitleBarStyle
	Background                                                    Color
	Transparent                                                   bool
	// Hidden keeps it hidden as the app starts: UseWindow().Show() (or
	// MainWindow().Show()) shows it once what it shows is ready, or a
	// tray icon's menu does, in an app that starts in the background.
	Hidden                bool
	SkipTaskbar           bool
	Maximized, Fullscreen bool
	// Panel, FitContent and ClickThrough are WindowProps': a launcher's
	// main window is a panel (with app.RunInBackground, hidden until a
	// global shortcut shows it), sized to its results; an overlay's lets
	// the mouse through. A panel closing hides it in an app that keeps
	// running (app.KeepRunning, app.RunInBackground); else the app quits.
	Panel, FitContent, ClickThrough bool
	// StateKey is where the host remembers where the user left it
	// ("main" if empty): it opens there, over Width, Height, X and Y.
	StateKey string
	// OnCloseRequest, if set, is asked when the user closes the main
	// window, which quits the app: false keeps it open, as
	// WindowProps.OnCloseRequest does a Window.
	OnCloseRequest func() bool
}

func (o *WindowOptions) zero() bool {
	return o.Title == nil && o.Width == 0 && o.Height == 0 && o.MinWidth == 0 && o.MinHeight == 0 &&
		o.MaxWidth == 0 && o.MaxHeight == 0 && o.X == 0 && o.Y == 0 && o.TitleBar == 0 && o.Background == 0 &&
		o.StateKey == "" && o.OnCloseRequest == nil && o.config().Flags == 0
}

func (o *WindowOptions) config() ops.WindowConfig {
	c := ops.WindowConfig{
		Width: o.Width, Height: o.Height, MinWidth: o.MinWidth, MinHeight: o.MinHeight,
		MaxWidth: o.MaxWidth, MaxHeight: o.MaxHeight, X: o.X, Y: o.Y,
		TitleBar: o.TitleBar, Background: o.Background, StateKey: o.StateKey,
	}
	c.Flags = windowFlags(o.DisableResize, o.DisableMinimize, o.DisableMaximize, o.DisableClose, o.AlwaysOnTop,
		o.Hidden, o.SkipTaskbar, o.Transparent, o.Maximized, o.Fullscreen)
	if o.X != 0 || o.Y != 0 {
		c.Flags |= ops.WindowPositioned
	}
	c.Flags |= kindFlags(o.Panel, o.FitContent, o.ClickThrough)
	return c
}

// SetWindowOptions sets the main window's options (the zero value: the
// host's); app.RunWindow calls it before the root mounts.
func SetWindowOptions(o WindowOptions) {
	mainWindow = nil
	if !o.zero() {
		mainWindow = &o
	}
}

var mainWindow *WindowOptions

func (r *Root) sendWindowOptions() {
	o := mainWindow
	if o == nil {
		return
	}
	if o.OnCloseRequest != nil {
		r.win.ask = o.OnCloseRequest
		r.win.sync()
	}
	if o.Hidden {
		r.win.visible.Set(false)
		r.win.focused.Set(false)
	}
	c := o.config()
	b := ops.AppendWindowConfig(nil, &c)
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
