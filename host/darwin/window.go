//go:build darwin && !ios

package darwin

import (
	"os"
	"strings"
	"testing"

	. "github.com/egoist/nui/internal/darwin"
	"github.com/egoist/nui/ops"
)

const (
	styleTitled         = 1 << 0
	styleClosable       = 1 << 1
	styleMiniaturizable = 1 << 2
	styleResizable      = 1 << 3
	backingBuffered     = 2
)

// makeWindow makes the app's window, its content a flipped container the
// root goes in.
func (h *Host) makeWindow() {
	r := Rect{Size: Size{Width: 480, Height: 760}}
	w := MsgInitWindow(Send(Class("NSWindow"), "alloc"), Sel("initWithContentRect:styleMask:backing:defer:"),
		r, styleTitled|styleClosable|styleMiniaturizable|styleResizable, backingBuffered, false)
	Send(w, "setReleasedWhenClosed:", 0)
	Send(w, "setTitle:", uintptr(String(h.appName)))
	content := MsgInitRect(Send(Class("NuiView"), "alloc"), Sel("initWithFrame:"), r)
	Send(content, "setAutoresizesSubviews:", 0)
	Send(w, "setContentView:", uintptr(content))
	MsgSetSize(w, Sel("setContentMinSize:"), Size{Width: 320, Height: 320})
	Send(w, "setDelegate:", uintptr(h.target))
	Send(w, "center")
	// The window comes back where the user left it (not a test's, nor
	// an automation run's: they'd leave theirs in the user's defaults).
	if !testing.Testing() && os.Getenv("NUI_SNAPSHOT") == "" {
		Send(w, "setFrameAutosaveName:", uintptr(String("nui "+h.appName)))
	}
	h.window, h.content = w, content
	h.main = &win{window: w, content: content, base: h.appName}
	// A toolbar coming or going resizes the content, not the window.
	Send(content, "setPostsFrameChangedNotifications:", 1)
	center := Send(Class("NSNotificationCenter"), "defaultCenter")
	Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("contentResized:")),
		uintptr(String("NSViewFrameDidChangeNotification")), uintptr(content))
}

// makeMenu makes the main menu: the app's, File, Edit (text fields' copy
// and paste go through it), View, Window and Help, with the app's own
// menus (MenuBar): one titled as a standard one adds its items to it,
// the others go between View and Window, as macOS orders them.
func (h *Host) makeMenu(app ID) {
	bar := New("NSMenu")
	name := h.appName
	own := map[string]*appMenu{}
	for i := range h.appMenus {
		m := &h.appMenus[i]
		switch strings.ToLower(m.title) {
		case "file", "edit", "view", "window", "help":
			own[strings.ToLower(m.title)] = m
		}
	}

	appMenu := menu("")
	item(appMenu, "About "+name, "orderFrontStandardAboutPanel:", "", 0)
	sep(appMenu)
	services := menu("Services")
	sub(appMenu, "Services", services)
	Send(app, "setServicesMenu:", uintptr(services))
	sep(appMenu)
	item(appMenu, "Hide "+name, "hide:", "h", 0)
	item(appMenu, "Hide Others", "hideOtherApplications:", "h", modOption)
	item(appMenu, "Show All", "unhideAllApplications:", "", 0)
	sep(appMenu)
	item(appMenu, "Quit "+name, "terminate:", "q", 0)
	sub(bar, name, appMenu)

	file := menu("File")
	if m := own["file"]; m != nil {
		h.addAppItems(file, m)
		sep(file)
	}
	item(file, "Close Window", "performClose:", "w", 0)
	sub(bar, "File", file)

	edit := menu("Edit")
	item(edit, "Undo", "undo:", "z", 0)
	item(edit, "Redo", "redo:", "z", modShift)
	sep(edit)
	item(edit, "Cut", "cut:", "x", 0)
	item(edit, "Copy", "copy:", "c", 0)
	item(edit, "Paste", "paste:", "v", 0)
	item(edit, "Select All", "selectAll:", "a", 0)
	if m := own["edit"]; m != nil {
		sep(edit)
		h.addAppItems(edit, m)
	}
	sub(bar, "Edit", edit)

	view := menu("View")
	if m := own["view"]; m != nil {
		h.addAppItems(view, m)
		sep(view)
	}
	item(view, "Back", "back:", "[", 0)
	sep(view)
	item(view, "Enter Full Screen", "toggleFullScreen:", "f", modControl)
	sub(bar, "View", view)

	for i := range h.appMenus {
		m := &h.appMenus[i]
		if own[strings.ToLower(m.title)] == m {
			continue
		}
		mm := menu(m.title)
		h.addAppItems(mm, m)
		sub(bar, m.title, mm)
	}

	win := menu("Window")
	item(win, "Minimize", "performMiniaturize:", "m", 0)
	item(win, "Zoom", "performZoom:", "", 0)
	if m := own["window"]; m != nil {
		sep(win)
		h.addAppItems(win, m)
	}
	sep(win)
	item(win, "Bring All to Front", "arrangeInFront:", "", 0)
	sub(bar, "Window", win)
	Send(app, "setWindowsMenu:", uintptr(win))

	if m := own["help"]; m != nil {
		help := menu("Help")
		h.addAppItems(help, m)
		sub(bar, "Help", help)
		Send(app, "setHelpMenu:", uintptr(help))
	}

	old := Send(app, "mainMenu")
	Send(app, "setMainMenu:", uintptr(bar))
	Release(bar)
	_ = old
}

const (
	modShift   = 1 << 17
	modControl = 1 << 18
	modOption  = 1 << 19
	modCommand = 1 << 20
)

func menu(title string) ID {
	return Send(Send(Class("NSMenu"), "alloc"), "initWithTitle:", uintptr(String(title)))
}

func item(m ID, title, action, key string, mods uint) ID {
	var s uintptr
	if action != "" {
		s = uintptr(Sel(action))
	}
	it := Send(m, "addItemWithTitle:action:keyEquivalent:", uintptr(String(title)), s, uintptr(String(key)))
	if mods != 0 {
		Send(it, "setKeyEquivalentModifierMask:", uintptr(mods|modCommand))
	}
	return it
}

func sep(m ID) { Send(m, "addItem:", uintptr(Send(Class("NSMenuItem"), "separatorItem"))) }

func sub(m ID, title string, s ID) {
	it := Send(m, "addItemWithTitle:action:keyEquivalent:", uintptr(String(title)), 0, uintptr(String("")))
	Send(it, "setSubmenu:", uintptr(s))
}

// observe follows what the environment reports: dark mode, Reduce
// Motion, the locale and time zone (the window's size and scale come to
// its delegate).
func (h *Host) observe() {
	app := Send(Class("NSApplication"), "sharedApplication")
	Send(app, "addObserver:forKeyPath:options:context:", uintptr(h.target), uintptr(String("effectiveAppearance")), 0, 0)
	center := Send(Class("NSNotificationCenter"), "defaultCenter")
	for _, name := range []string{"NSCurrentLocaleDidChangeNotification", "NSSystemTimeZoneDidChangeNotification", "NSSystemColorsDidChangeNotification"} {
		Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("systemChanged:")), uintptr(String(name)), 0)
	}
	ws := Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "notificationCenter")
	Send(ws, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("systemChanged:")),
		uintptr(String("NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification")), 0)
}

// environment is the window's: its content's size, its display's scale,
// dark mode, Reduce Motion, the locale and time zone. A Mac has no safe
// area or keyboard over the window, and no text size setting.
func (h *Host) environment() ops.Environment {
	b := MsgRect(h.content, Sel("bounds"))
	scale := MsgFloat(h.window, Sel("backingScaleFactor"))
	if scale <= 0 {
		scale = 2
	}
	tz := GoString(Send(Send(Class("NSTimeZone"), "localTimeZone"), "name"))
	locale := GoString(Send(Send(Class("NSLocale"), "currentLocale"), "localeIdentifier"))
	ws := Send(Class("NSWorkspace"), "sharedWorkspace")
	return ops.Environment{
		Width: float32(b.Size.Width), Height: float32(b.Size.Height),
		Scale: float32(scale), FontScale: 1,
		Dark:         h.isDark(),
		ReduceMotion: SendBool(ws, "accessibilityDisplayShouldReduceMotion"),
		Platform:     ops.PlatformMacOS,
		Locale:       locale,
		TimeZone:     tz,
	}
}

func (h *Host) isDark() bool {
	a := Send(h.window, "effectiveAppearance")
	if a == 0 {
		a = Send(Send(Class("NSApplication"), "sharedApplication"), "effectiveAppearance")
	}
	names := Array(String("NSAppearanceNameAqua"), String("NSAppearanceNameDarkAqua"))
	return GoString(Send(a, "bestMatchFromAppearancesWithNames:", uintptr(names))) == "NSAppearanceNameDarkAqua"
}

// environmentChanged reports a new environment to the root, and to the
// screens and sheets the host sizes.
func (h *Host) environmentChanged() {
	if h.content == 0 {
		return
	}
	env := h.environment()
	if env == h.env {
		h.layoutNav()
		return
	}
	h.env = env
	if h.root != nil {
		h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventEnvironment, Env: env})
	}
	h.layoutNav()
}

// appearanceChanged redoes the colors that don't follow the appearance
// themselves (layers' CGColors), and reports dark mode.
func (h *Host) appearanceChanged() {
	dark := h.isDark()
	WithPool(func() {
		h.withAppearance(func() {
			for _, n := range h.nodes {
				h.applyLayerColors(n)
			}
		})
	})
	if dark != h.dark {
		h.dark = dark
	}
	h.environmentChanged()
}

// withAppearance runs f with the window's appearance current, so the
// dynamic system colors resolve to it (a layer takes a fixed CGColor).
func (h *Host) withAppearance(f func()) {
	cls := Class("NSAppearance")
	prev := Send(cls, "currentAppearance")
	Send(cls, "setCurrentAppearance:", uintptr(Send(h.window, "effectiveAppearance")))
	f()
	Send(cls, "setCurrentAppearance:", uintptr(prev))
}

func (h *Host) lifecycle(s ops.Lifecycle) {
	if h.root == nil {
		return
	}
	h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventLifecycle, Int: int64(s)})
}

func (h *Host) openURL(u string) {
	if h.root == nil {
		return
	}
	h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventIncomingURL, Str: u})
}

// openWindow opens a Window's window: its content's size, its least, its
// title; placed down and right of the window in use, as new windows
// cascade.
func (h *Host) openWindow(n *node) {
	if n.nav.window != 0 {
		return
	}
	size := n.nav.size
	if size.Width <= 0 {
		size.Width = 480
	}
	if size.Height <= 0 {
		size.Height = 600
	}
	w := MsgInitWindow(Send(Class("NSWindow"), "alloc"), Sel("initWithContentRect:styleMask:backing:defer:"),
		Rect{Size: size}, styleTitled|styleClosable|styleMiniaturizable|styleResizable, backingBuffered, false)
	Send(w, "setReleasedWhenClosed:", 0)
	content := MsgInitRect(Send(Class("NuiView"), "alloc"), Sel("initWithFrame:"), Rect{Size: size})
	Send(content, "setAutoresizesSubviews:", 0)
	Send(w, "setContentView:", uintptr(content))
	Release(content)
	if n.nav.minSize.Width > 0 || n.nav.minSize.Height > 0 {
		MsgSetSize(w, Sel("setContentMinSize:"), Size{Width: max(n.nav.minSize.Width, 1), Height: max(n.nav.minSize.Height, 1)})
	}
	Send(w, "setDelegate:", uintptr(h.target))
	MsgSetRect(n.view, selSetFrame, Rect{Size: size})
	Send(content, "addSubview:", uintptr(n.view))
	base := n.title
	if base == "" {
		base = h.appName
	}
	x := &win{window: w, content: content, root: n, base: base}
	h.wins[w] = x
	n.nav.window = w
	n.presented = true
	// Cascade from the window in use.
	from := MsgRect(h.keyWin().window, Sel("frame"))
	MsgPointID(w, Sel("cascadeTopLeftFromPoint:"), Point{X: from.Origin.X + 24, Y: from.Origin.Y + from.Size.Height - 24})
	h.reportScreen(n, size)
	h.updateWindowChrome()
	if os.Getenv("NUI_SNAPSHOT") != "" {
		Send(w, "orderFront:", 0)
	} else {
		Send(w, "makeKeyAndOrderFront:", 0)
	}
}

// windowOptions applies WindowOptions to the main window: its title, its
// least size, and its size unless the user's is remembered.
func (h *Host) windowOptions(args []byte) {
	if len(args) < 16 {
		return
	}
	w, ht, minW, minH := float64(f32(args, 0)), float64(f32(args, 4)), float64(f32(args, 8)), float64(f32(args, 12))
	if title := string(args[16:]); title != "" {
		h.main.base = title
	}
	if minW > 0 || minH > 0 {
		MsgSetSize(h.window, Sel("setContentMinSize:"), Size{Width: max(minW, 1), Height: max(minH, 1)})
	}
	defaults := Send(Class("NSUserDefaults"), "standardUserDefaults")
	saved := Send(defaults, "objectForKey:", uintptr(String("NSWindow Frame nui "+h.appName))) != 0
	if (w > 0 || ht > 0) && !saved {
		cur := MsgRect(h.content, Sel("bounds")).Size
		if w <= 0 {
			w = cur.Width
		}
		if ht <= 0 {
			ht = cur.Height
		}
		MsgSetSize(h.window, Sel("setContentSize:"), Size{Width: w, Height: ht})
		Send(h.window, "center")
	}
	h.updateWindowChrome()
	h.environmentChanged()
}
