package ui

import (
	"encoding/binary"
	"math"

	"github.com/egoist/ryui/ops"
	"github.com/egoist/ryui/signal"
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
	Title               string
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
	b = append(b, o.Title...)
	r.batch.Command(r.el.id, ops.CmdWindowOptions, b)
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
