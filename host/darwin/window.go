//go:build darwin && !ios

package darwin

import (
	"strings"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/internal/l10n"
	"github.com/egoist/asuka/ops"
)

const (
	styleTitled         = 1 << 0
	styleClosable       = 1 << 1
	styleMiniaturizable = 1 << 2
	styleResizable      = 1 << 3
	backingBuffered     = 2
)

// A window's content size, in points, when the app gives none.
const (
	defaultWindowWidth  = 800
	defaultWindowHeight = 600
)

// makeWindow makes the app's window, its content a flipped container the
// root goes in: 800 by 600 points if the app gives no size, as desktop
// frameworks' windows are (Electron's), centered.
func (h *Host) makeWindow() {
	r := Rect{Size: Size{Width: defaultWindowWidth, Height: defaultWindowHeight}}
	w := MsgInitWindow(Send(Class("NSWindow"), "alloc"), Sel("initWithContentRect:styleMask:backing:defer:"),
		r, styleTitled|styleClosable|styleMiniaturizable|styleResizable, backingBuffered, false)
	Send(w, "setReleasedWhenClosed:", 0)
	Send(w, "setTitle:", uintptr(String(h.appName)))
	content := MsgInitRect(Send(Class("AsukaView"), "alloc"), Sel("initWithFrame:"), r)
	Send(content, "setAutoresizesSubviews:", 0)
	Send(w, "setContentView:", uintptr(content))
	MsgSetSize(w, Sel("setContentMinSize:"), Size{Width: 320, Height: 320})
	Send(w, "setDelegate:", uintptr(h.target))
	Send(w, "center")
	// Where the user left it comes back as its options are applied
	// (configure, under its key).
	h.window, h.content = w, content
	h.main = &win{window: w, content: content, base: h.appName}
	// A toolbar coming or going resizes the content, not the window.
	Send(content, "setPostsFrameChangedNotifications:", 1)
	center := Send(Class("NSNotificationCenter"), "defaultCenter")
	Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("contentResized:")),
		uintptr(String("NSViewFrameDidChangeNotification")), uintptr(content))
}

// makeMenu makes the main menu: the app's, File, Edit (text fields' copy
// and paste go through it), View, Window and Help, in the language shown,
// with the app's own menus (MenuBar): one titled as a standard one, in
// English or in that language, adds its items to it, the others go between
// View and Window, as macOS orders them.
func (h *Host) makeMenu(app ID) {
	bar := New("NSMenu")
	name := h.appName
	own := map[string]*ops.MenuBarMenu{}
	joined := map[*ops.MenuBarMenu]bool{}
	for i := range h.appMenus {
		m := &h.appMenus[i]
		for _, s := range []string{"File", "Edit", "View", "Window", "Help"} {
			if strings.EqualFold(m.Title, s) || strings.EqualFold(m.Title, h.menuWord(s)) {
				if old := own[s]; old != nil {
					delete(joined, old)
				}
				own[s], joined[m] = m, true
				break
			}
		}
	}

	appMenu := menu("")
	item(appMenu, l10n.About(h.locale, name), "orderFrontStandardAboutPanel:", "", 0)
	sep(appMenu)
	services := menu(h.menuWord("Services"))
	sub(appMenu, h.menuWord("Services"), services)
	Send(app, "setServicesMenu:", uintptr(services))
	sep(appMenu)
	item(appMenu, l10n.Hide(h.locale, name), "hide:", "h", 0)
	item(appMenu, l10n.HideOthers(h.locale), "hideOtherApplications:", "h", modOption)
	item(appMenu, h.menuWord("Show All"), "unhideAllApplications:", "", 0)
	sep(appMenu)
	item(appMenu, l10n.Quit(h.locale, name), "terminate:", "q", 0)
	sub(bar, name, appMenu)

	file := menu(h.menuWord("File"))
	if m := own["File"]; m != nil {
		h.addAppItems(file, m)
		sep(file)
	}
	if h.hasOpenRecent() { // files.go
		h.addOpenRecent(file)
		sep(file)
	}
	item(file, h.menuWord("Close Window"), "performClose:", "w", 0)
	sub(bar, h.menuWord("File"), file)

	edit := menu(h.menuWord("Edit"))
	item(edit, h.menuWord("Undo"), "undo:", "z", 0)
	item(edit, h.menuWord("Redo"), "redo:", "z", modShift)
	sep(edit)
	item(edit, h.menuWord("Cut"), "cut:", "x", 0)
	item(edit, h.menuWord("Copy"), "copy:", "c", 0)
	item(edit, h.menuWord("Paste"), "paste:", "v", 0)
	item(edit, h.menuWord("Select All"), "selectAll:", "a", 0)
	if m := own["Edit"]; m != nil {
		sep(edit)
		h.addAppItems(edit, m)
	}
	sub(bar, h.menuWord("Edit"), edit)

	view := menu(h.menuWord("View"))
	if m := own["View"]; m != nil {
		h.addAppItems(view, m)
		sep(view)
	}
	item(view, l10n.Back(h.locale), "back:", "[", 0)
	sep(view)
	item(view, h.menuWord("Enter Full Screen"), "toggleFullScreen:", "f", modControl)
	sub(bar, h.menuWord("View"), view)

	for i := range h.appMenus {
		m := &h.appMenus[i]
		if joined[m] {
			continue
		}
		mm := menu(m.Title)
		h.addAppItems(mm, m)
		sub(bar, m.Title, mm)
	}

	win := menu(h.menuWord("Window"))
	item(win, h.menuWord("Minimize"), "performMiniaturize:", "m", 0)
	item(win, l10n.Zoom(h.locale), "performZoom:", "", 0)
	if m := own["Window"]; m != nil {
		sep(win)
		h.addAppItems(win, m)
	}
	sep(win)
	item(win, h.menuWord("Bring All to Front"), "arrangeInFront:", "", 0)
	sub(bar, h.menuWord("Window"), win)
	Send(app, "setWindowsMenu:", uintptr(win))

	if m := own["Help"]; m != nil {
		help := menu(h.menuWord("Help"))
		h.addAppItems(help, m)
		sub(bar, h.menuWord("Help"), help)
		Send(app, "setHelpMenu:", uintptr(help))
	}

	setMenuDirection(bar, h.rtl)
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
	// Text fields' selections, as their text views move them.
	Send(center, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("textSelectionChanged:")),
		uintptr(String("NSTextViewDidChangeSelectionNotification")), 0)
	ws := Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "notificationCenter")
	Send(ws, "addObserver:selector:name:object:", uintptr(h.target), uintptr(Sel("systemChanged:")),
		uintptr(String("NSWorkspaceAccessibilityDisplayOptionsDidChangeNotification")), 0)
}

// environment is the window's: its content's size, its display's scale,
// dark mode, Reduce Motion, the locale, languages and formats, and the time
// zone. A Mac has no safe area or keyboard over the window, and no text size
// setting.
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
		SafeTop: h.main.safeTop(),
		Scale:   float32(scale), FontScale: 1,
		Dark:         h.isDark(),
		ReduceMotion: SendBool(ws, "accessibilityDisplayShouldReduceMotion"),
		ScreenReader: SendBool(ws, "isVoiceOverEnabled"),
		Platform:     ops.PlatformMacOS,
		Locale:       locale,
		TimeZone:     tz,
		Languages:    h.languages,
		FirstWeekday: h.firstWeekday,
		HourCycle:    h.hourCycle,
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

// openWindow opens a Window's window with its options (winctl.go): its
// content's size (the main window's default if the app gives none), its
// least and most, its title; where the user left it if it has a key, else
// where its options say, else down and right of the window in use, as new
// windows cascade. A child goes above its parent, a modal one is a sheet
// of it. It shows unless it opens hidden.
func (h *Host) openWindow(n *node) {
	if n.nav.window != 0 {
		return
	}
	c := n.nav.cfg
	size := Size{Width: float64(c.Width), Height: float64(c.Height)}
	if size.Width <= 0 {
		size.Width = defaultWindowWidth
	}
	if size.Height <= 0 {
		size.Height = defaultWindowHeight
	}
	c.Width, c.Height = float32(size.Width), float32(size.Height)
	w := MsgInitWindow(Send(Class("NSWindow"), "alloc"), Sel("initWithContentRect:styleMask:backing:defer:"),
		Rect{Size: size}, styleMask(&c), backingBuffered, false)
	Send(w, "setReleasedWhenClosed:", 0)
	content := MsgInitRect(Send(Class("AsukaView"), "alloc"), Sel("initWithFrame:"), Rect{Size: size})
	Send(content, "setAutoresizesSubviews:", 0)
	Send(w, "setContentView:", uintptr(content))
	Release(content)
	if c.MinWidth > 0 || c.MinHeight > 0 {
		MsgSetSize(w, Sel("setContentMinSize:"), Size{Width: max(float64(c.MinWidth), 1), Height: max(float64(c.MinHeight), 1)})
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
	if c.Flags&ops.WindowChild != 0 {
		parent := h.parentWindow(c.Parent)
		if c.Flags&ops.WindowModal != 0 {
			x.sheetOf = parent
		} else {
			x.child = parent
		}
	}
	// Cascade from the window in use, unless it's placed.
	from := MsgRect(h.keyWin().window, Sel("frame"))
	MsgPointID(w, Sel("cascadeTopLeftFromPoint:"), Point{X: from.Origin.X + 24, Y: from.Origin.Y + from.Size.Height - 24})
	h.configure(x, c)
	h.reportScreen(n, MsgRect(content, Sel("bounds")).Size)
	h.updateWindowChrome()
	if c.Flags&ops.WindowHidden == 0 {
		// An app in the background (an accessory) isn't active: its
		// window comes over the app in use, as the user asked for it.
		h.showWin(x, inBackground())
	}
}

// windowOptions applies WindowOptions to the main window: its title, its
// least size, and its other options (configure: its size and place
// unless the user's are remembered).
func (h *Host) windowOptions(args []byte) {
	c := ops.DecodeWindowConfig(args)
	if c.Title != "" {
		h.main.base = c.Title
	}
	if c.MinWidth > 0 || c.MinHeight > 0 {
		MsgSetSize(h.window, Sel("setContentMinSize:"), Size{Width: max(float64(c.MinWidth), 1), Height: max(float64(c.MinHeight), 1)})
	}
	h.configure(h.main, c)
	h.updateWindowChrome()
	h.environmentChanged()
}
