package ui

import (
	"encoding/binary"

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
	// Center. Linux's desktop places windows itself: GTK has no way to.
	X, Y   float32
	Center bool
	// DisableResize, DisableMinimize, DisableMaximize and DisableClose
	// take the user's abilities away: resizing it, and its buttons.
	DisableResize, DisableMinimize, DisableMaximize, DisableClose bool
	// AlwaysOnTop keeps it above other apps' windows (not on Linux, where
	// GTK 4 can't ask for it).
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
	return c
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
			if pe := parent.el(); c.Flags&ops.WindowChild != 0 && pe != nil {
				c.Parent = pe.id
			} else {
				c.Flags &^= ops.WindowChild | ops.WindowModal
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
			r.batch.Command(w.id, ops.CmdPresent, nil)
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

// MenuItem is an item of a menu bar's menu.
type MenuItem struct {
	Title       string
	Shortcut    Shortcut
	OnPress     func()
	Disabled    bool
	Checked     bool
	Destructive bool
	// Separator makes it a line between groups of items (the rest is
	// ignored).
	Separator bool
}

// MenuBarMenu is one of the menu bar's menus.
type MenuBarMenu struct {
	Title string
	Items []MenuItem
}

// MenuSeparator is a line between a menu's groups of items.
var MenuSeparator = MenuItem{Separator: true}

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
	b := binary.LittleEndian.AppendUint32(nil, uint32(len(menus)))
	str := func(s string) {
		b = binary.LittleEndian.AppendUint32(b, uint32(len(s)))
		b = append(b, s...)
	}
	for _, m := range menus {
		str(m.Title)
		b = binary.LittleEndian.AppendUint32(b, uint32(len(m.Items)))
		for _, it := range m.Items {
			var flags byte
			if it.Separator {
				flags |= 1
			}
			if it.Disabled {
				flags |= 2
			}
			if it.Checked {
				flags |= 4
			}
			if it.Destructive {
				flags |= 8
			}
			b = append(b, flags)
			str(it.Title)
			str(it.Shortcut.Key)
			b = append(b, byte(it.Shortcut.Mods))
		}
	}
	r.menuItems = r.menuItems[:0]
	for _, m := range menus {
		r.menuItems = append(r.menuItems, m.Items...)
	}
	if !r.menuListen {
		r.menuListen = true
		r.el.handlers().action = func(i int64) {
			if i >= 0 && int(i) < len(r.menuItems) {
				call(r.menuItems[i].OnPress)
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
