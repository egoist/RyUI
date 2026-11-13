//go:build linux && !android

package linux

import (
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// win is a top-level window of the app's: the main one, holding the root,
// or a Window node's. Its content is an AdwToolbarView: the header bar
// (the window's title bar, with the innermost stack's back button, title
// and bar buttons), and under it a view of the host's that holds the root
// at its size.
type win struct {
	window   Ptr // AdwApplicationWindow
	toolbar  Ptr // AdwToolbarView
	header   Ptr // AdwHeaderBar
	title    Ptr // AdwWindowTitle
	content  Ptr // the host's AsukaView under the header
	toasts   Ptr // the AdwToastOverlay around it
	search   *searchField
	restored bool
	root     *node
	node     *node // the Window node (nil for the main window)

	name       string  // its title (the app's name for the main window, unless set)
	edited     bool    // its document has unsaved changes: a * before the title
	file       string  // its document's file: its folder is the subtitle
	subtitled  bool    // its title has a subtitle (its file's folder)
	envW, envH float32 // as last reported
	chrome     chrome  // what its header bar shows (nav.go)
	closing    bool

	// Its options and state (winctl.go).
	cfg        ops.WindowConfig
	configured bool
	hidden     bool   // kept hidden until a Show or Focus
	forced     bool   // closing for good: the core's Close
	stateKey   string // where its size is remembered ("": not)
	css        Ptr    // its own style provider: background, title bar
	cssName    string // its widget name, which that style selects
	sent       ops.WindowState
	reported   bool
	desk       deskState // as a panel, a flyout... (deskwin.go)
}

func (h *Host) newMainWindow() *win {
	w := h.newWin(AdwApplicationWindowNew(h.app))
	if os.Getenv("ASUKA_TEST") != "" {
		WindowSetDecorated(w.window, false)
	}
	w.name = h.appName
	WindowSetTitle(w.window, h.appName)
	AdwWindowTitleSetTitle(w.title, h.appName)
	WindowSetDefaultSize(w.window, 800, 600+headerHeight)
	// The user closing it: the core asks first while it listens for
	// CloseRequest (the window stays). Then, for an app that keeps
	// running with no window (app.KeepRunning, app.RunInBackground), it
	// hides, the root mounted and the app in the background
	// (lifecycle.go); else it closes, which quits the app.
	Connect(w.window, "close-request", func() bool {
		if !w.forced && h.root != nil && h.root.listening(ops.EventCloseRequest) {
			h.emit(h.root, ops.Event{Kind: ops.EventCloseRequest})
			return true
		}
		if boot.KeepsRunning() && !h.quitting {
			h.saveWindow(w)
			WidgetSetVisible(w.window, false)
			return true
		}
		// Closing the app's last window quits it: OnQuit's handlers may
		// keep it open (quit.go).
		if h.closingLast(w) && !h.askQuit(false) {
			return true
		}
		h.saveWindow(w)
		return false
	})
	return w
}

// headerHeight is libadwaita's header bar's height (47: 46 and its
// border), which the window's default size includes.
const headerHeight = 47

// newWin wraps a GtkWindow in the host's structure.
func (h *Host) newWin(window Ptr) *win {
	w := &win{window: window}
	w.toolbar = AdwToolbarViewNew()
	w.header = AdwHeaderBarNew()
	w.title = AdwWindowTitleNew("", "")
	ObjectRefSink(w.title) // kept while a tabs' switcher takes its place
	AdwHeaderBarSetTitleWidget(w.header, w.title)
	AdwToolbarViewAddTopBar(w.toolbar, w.header)
	w.content = newView()
	w.toasts = AdwToastOverlayNew()
	AdwToastOverlaySetChild(w.toasts, w.content)
	AdwToolbarViewSetContent(w.toolbar, w.toasts)
	if TypeCheckInstanceIsA(window, Call(Sym("adw_application_window_get_type"))) {
		AdwApplicationWindowSetContent(window, w.toolbar)
	} else {
		AdwWindowSetContent(window, w.toolbar)
	}
	h.wins[window] = w
	h.routeKeys(w)
	h.watchWindow(w)
	Connect(window, "notify::is-active", func(uintptr) {
		if WindowIsActive(window) {
			h.activeWin = w
		}
	})
	h.watchWindowState(w)
	return w
}

// winByContent is the window whose content view w is.
func (h *Host) winByContent(w Ptr) *win {
	if h.main != nil && h.main.content == w {
		return h.main
	}
	for _, x := range h.wins {
		if x.content == w {
			return x
		}
	}
	return nil
}

func (w *win) setRoot(h *Host, n *node) {
	if w.root == n {
		return
	}
	if w.root != nil && w.root.widget != 0 && WidgetGetParent(w.root.widget) == w.content {
		WidgetUnparent(w.root.widget)
	}
	w.root = n
	h.detach(n.widget)
	WidgetSetParent(n.widget, w.content)
	Call(FnQueueAllocate, w.content)
}

// allocated is the window's content view sized: the root takes the size,
// and the core hears of it.
func (w *win) allocated(h *Host, width, height int32) {
	h.deskAllocated(w)
	fw, fh := float32(width), float32(height)
	if w.root != nil && w.root.widget != 0 {
		allocate(w.root.widget, width, height, 0, 0, nil)
	}
	if fw == w.envW && fh == w.envH {
		return
	}
	w.envW, w.envH = fw, fh
	h.queueWindowState(w)
	if w.node == nil {
		// The main window: the root's environment.
		env := h.environment(w)
		if env == h.env {
			return
		}
		h.env = env
		if h.root != nil {
			h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventEnvironment, Env: env})
		}
		return
	}
	// A Window's node is its root, sized here: it reports its own
	// environment (rootAllocated).
}

// environment is a window's: its content's size, its display's scale,
// the desktop's settings.
func (h *Host) environment(w *win) ops.Environment {
	env := ops.Environment{
		Width: w.envW, Height: w.envH,
		Scale:     1,
		FontScale: h.fontScale,
		Dark:      h.dark,
		Platform:  ops.PlatformLinux,
		Locale:    h.posixLocale(),
		Languages: h.languages,
		TimeZone:  timeZone(),
	}
	if env.Width == 0 && env.Height == 0 {
		var dw, dh int32
		WindowGetDefaultSize(w.window, &dw, &dh)
		env.Width, env.Height = float32(dw), float32(max(dh-w.headerRoom(), 0))
	}
	if nat := WidgetGetNative(w.content); nat != 0 {
		if s := NativeGetSurface(nat); s != 0 {
			if SurfaceGetScale != nil {
				env.Scale = float32(SurfaceGetScale(s))
			} else {
				env.Scale = float32(SurfaceGetScaleFactor(s))
			}
		}
	}
	if env.Scale <= 0 {
		env.Scale = 1
	}
	if w.cfg.TitleBar == ops.TitleBarHidden {
		// The content goes under the header bar: SafeArea keeps clear of
		// it, its window buttons.
		top := AdwToolbarViewGetTopBarHeight(w.toolbar)
		if top <= 0 {
			top = headerHeight
		}
		env.SafeTop = float32(top)
	}
	if s := SettingsGetDefault(); s != 0 {
		env.ReduceMotion = !GetBoolProperty(s, "gtk-enable-animations")
	}
	env.ScreenReader = h.screenReader
	env.HourCycle = h.hourCycle
	return env
}

// watchSettings follows the desktop's settings: dark style, animations,
// text scaling, the font.
func (h *Host) watchSettings() {
	sm := AdwStyleManagerGetDefault()
	Connect(sm, "notify::dark", func(uintptr) {
		h.dark = AdwStyleManagerGetDark(sm)
		h.themeChanged()
		h.trayStyleChanged()
	})
	Connect(sm, "notify::high-contrast", func(uintptr) { h.themeChanged() })
	if s := SettingsGetDefault(); s != 0 {
		for _, prop := range []string{"gtk-enable-animations", "gtk-xft-dpi", "gtk-font-name"} {
			Connect(s, "notify::"+prop, func(uintptr) {
				h.base = systemFont()
				h.fontScale = textScale()
				h.envChanged()
			})
		}
	}
	h.watchDesktop()
}

// themeChanged reads the palette again, redraws every view and tells
// the core.
func (h *Host) themeChanged() {
	h.theme = h.readPalette(h.main.content)
	for _, w := range h.wins {
		if w.cfg.Background != 0 {
			h.windowCSS(w) // a semantic color, the style's
		}
	}
	for _, n := range h.nodes {
		queueDraw(n)
		if n.kind == ops.KindText || n.kind == ops.KindTextField || n.kind == ops.KindButton || n.kind == ops.KindRichText {
			h.applyText(n) // colors are attributes, resolved once
		}
	}
	h.envChanged()
}

// envChanged reports every window's environment again.
func (h *Host) envChanged() {
	if h.main == nil {
		return
	}
	env := h.environment(h.main)
	if env != h.env {
		h.env = env
		if h.root != nil {
			h.dispatch(ops.Event{ID: h.root.id, Kind: ops.EventEnvironment, Env: env})
		}
	}
	h.rootsEnvChanged()
}

// posixLocale is the locale of the user's formats: LC_ALL, LC_TIME or
// LANG, as en_US.
func (h *Host) posixLocale() string {
	for _, k := range []string{"LC_ALL", "LC_TIME", "LANG"} {
		if v := os.Getenv(k); v != "" && v != "C" && v != "POSIX" {
			v, _, _ = strings.Cut(v, ".")
			v, _, _ = strings.Cut(v, "@")
			return v
		}
	}
	return "en_US"
}

// userLanguages are the languages the user reads, most preferred first,
// as BCP 47 tags: LANGUAGE's list, else LC_ALL, LC_MESSAGES or LANG.
func userLanguages() string {
	var out []string
	add := func(v string) {
		v, _, _ = strings.Cut(v, ".")
		v, _, _ = strings.Cut(v, "@")
		if v == "" || v == "C" || v == "POSIX" {
			return
		}
		out = append(out, strings.ReplaceAll(v, "_", "-"))
	}
	if l := os.Getenv("LANGUAGE"); l != "" {
		for _, v := range strings.Split(l, ":") {
			add(v)
		}
	}
	if len(out) == 0 {
		for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
			if v := os.Getenv(k); v != "" {
				add(v)
				break
			}
		}
	}
	if len(out) == 0 {
		return "en-US"
	}
	return strings.Join(out, ",")
}

// timeZone is the system's IANA time zone: TZ, /etc/localtime's link,
// or /etc/timezone.
func timeZone() string {
	if tz := strings.TrimPrefix(os.Getenv("TZ"), ":"); tz != "" && !strings.HasPrefix(tz, "/") {
		return tz
	}
	if p, err := filepath.EvalSymlinks("/etc/localtime"); err == nil {
		if _, after, ok := strings.Cut(p, "zoneinfo/"); ok {
			return after
		}
	}
	if b, err := os.ReadFile("/etc/timezone"); err == nil {
		return strings.TrimSpace(string(b))
	}
	return "UTC"
}

// appName is the app's name: asuka.json's, or the executable's.
func appName() string {
	if boot.Name != "" {
		return boot.Name
	}
	return filepath.Base(os.Args[0])
}

// appID is the application's ID: asuka.json's, else one made of its name
// (GApplication wants one with a dot).
func appID(name string) string {
	if boot.ID != "" && ApplicationIDIsValid(boot.ID) {
		return boot.ID
	}
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('_')
		}
	}
	id := "dev.asuka.app." + b.String()
	if b.Len() == 0 || !ApplicationIDIsValid(id) {
		return "dev.asuka.App"
	}
	return id
}

// assetsDir is where the app's assets are: assets beside the executable
// (a release), ASUKA_ASSETS (asuka dev), or assets in the working directory.
func assetsDir() string {
	if d := os.Getenv("ASUKA_ASSETS"); d != "" {
		return d
	}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Join(filepath.Dir(exe), "assets")
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
		d = filepath.Join(filepath.Dir(exe), "..", "share", filepath.Base(exe), "assets")
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	return "assets"
}

// setWindowOptions applies WindowOptions to the main window, before it
// shows.
func (h *Host) setWindowOptions(args []byte) {
	c := ops.DecodeWindowConfig(args)
	w := h.main
	if w == nil {
		return
	}
	if c.Title != "" {
		w.name = c.Title
	}
	h.updateTitle(w)
	h.configure(w, c)
}

func (h *Host) updateTitle(w *win) {
	t := w.name
	if w.edited {
		t = "*" + t // as gedit and GNOME Text Editor mark it
	}
	if c := w.chrome.title; c != "" {
		AdwWindowTitleSetTitle(w.title, c)
	} else {
		AdwWindowTitleSetTitle(w.title, t)
	}
	if w.file != "" || w.subtitled {
		// A document's folder under its name, as GNOME Text Editor shows
		// it, the home folder as ~.
		Dev.AdwWindowTitleSetSubtitle(w.title, folderLabel(w.file))
		w.subtitled = w.file != ""
	}
	if w.chrome.title != "" && w.node == nil {
		WindowSetTitle(w.window, w.chrome.title)
	} else {
		WindowSetTitle(w.window, t)
	}
}

// folderLabel is the folder of the file at path as a title bar shows it:
// the home folder as ~.
func folderLabel(path string) string {
	if path == "" {
		return ""
	}
	dir := filepath.Dir(path)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if dir == home {
			return "~"
		}
		if rel, err := filepath.Rel(home, dir); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + rel
		}
	}
	return dir
}

// detachFromContainer takes w out of a container of GTK's.
func (h *Host) detachFromContainer(p, w Ptr) {
	switch {
	case TypeCheckInstanceIsA(p, Call(Sym("gtk_viewport_get_type"))):
		ViewportSetChild(p, 0)
	case TypeCheckInstanceIsA(p, Call(Sym("adw_navigation_page_get_type"))):
		AdwNavigationPageSetChild(p, 0)
	case TypeCheckInstanceIsA(p, Call(Sym("adw_bin_get_type"))):
		AdwBinSetChild(p, 0)
	case TypeCheckInstanceIsA(p, Call(Sym("adw_dialog_get_type"))):
		AdwDialogSetChild(p, 0)
	case TypeCheckInstanceIsA(p, Call(Sym("gtk_popover_get_type"))):
		PopoverSetChild(p, 0)
	default:
		WidgetUnparent(w)
	}
}

// windowSize rounds a size in points to the window's whole points.
func windowSize(f float32) int32 { return int32(math.Round(float64(f))) }
