//go:build darwin && !ios

package darwin

import (
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
	// The window comes back where the user left it.
	Send(w, "setFrameAutosaveName:", uintptr(String("nui "+h.appName)))
	h.window, h.content = w, content
	// A toolbar coming or going resizes the content, not the window.
	Send(content, "setPostsFrameChangedNotifications:", 1)
	center := Send(Class("NSNotificationCenter"), "defaultCenter")
	Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("contentResized:")),
		uintptr(String("NSViewFrameDidChangeNotification")), uintptr(content))
}

// makeMenu makes the main menu: the app's, Edit (text fields' copy and
// paste go through it) and Window.
func (h *Host) makeMenu(app ID) {
	bar := New("NSMenu")
	name := h.appName

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
	sub(bar, "Edit", edit)

	view := menu("View")
	item(view, "Back", "back:", "[", 0)
	sep(view)
	item(view, "Enter Full Screen", "toggleFullScreen:", "f", modControl)
	sub(bar, "View", view)

	win := menu("Window")
	item(win, "Minimize", "performMiniaturize:", "m", 0)
	item(win, "Zoom", "performZoom:", "", 0)
	sep(win)
	item(win, "Bring All to Front", "arrangeInFront:", "", 0)
	sub(bar, "Window", win)
	Send(app, "setWindowsMenu:", uintptr(win))

	Send(app, "setMainMenu:", uintptr(bar))
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
