//go:build windows

package windows

import (
	"math"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/internal/winstate"
	"github.com/egoist/asuka/ops"
)

// The app's top-level windows, the main one and each Window's, as the app
// controls them (ui.WindowHandle): the options they open with
// (ops.WindowConfig: WindowOptions' for the main window, a Window's Config
// prop), the WindowAction commands, their state reported as it changes
// (WindowState), the user's closing asked about first (CloseRequest), and
// where they were (placement.go).
//
//   - Abilities are window styles: a window the user can't resize has no
//     sizing frame (WS_THICKFRAME) or maximize button, and the minimize and
//     maximize buttons go with WS_MINIMIZEBOX and WS_MAXIMIZEBOX; one the
//     user can't close has its close button and its system menu's Close
//     grayed, and the user's closing it (Alt+F4, the taskbar) does nothing.
//   - Its most size is WM_GETMINMAXINFO's, as its least is.
//   - Always on top is the topmost band (HWND_TOPMOST).
//   - A window kept off the taskbar is owned by the host's hidden window:
//     Windows gives owned windows no taskbar button, which holds when the
//     window shows again or Explorer restarts, with nothing to redo, as
//     ITaskbarList's DeleteTab would need; it stays in Alt+Tab.
//   - A Window that's a child of another (WindowChild) is owned by it: it
//     stays above it, and minimizes and hides with it. A modal one also
//     disables it while it's open, as a sheet does its window.
//   - A background color fills the window behind its content (the root's,
//     a Window's, their screens and bars), the theme's otherwise. A
//     transparent window isn't: every view is a child window, and Windows
//     composes child windows over their top-level window's opaque surface,
//     so it shows its background color instead.
//   - A hidden title bar (titlebar.go) gives the client area the whole
//     window, its caption buttons drawn over the content.
//   - Full screen is the window without its frame, covering its monitor,
//     its placement kept to go back to.

// wctl is one of the app's top-level windows, as the app controls it.
type wctl struct {
	hwnd  uintptr
	main  bool
	n     *node // a Window's node; the main window's root, once it's known
	frame *node // a Window's top-level window's node (its scale)
	cfg   ops.WindowConfig
	key   string // where its placement is kept ("": nowhere)

	owner uintptr // a child's owner, disabled while a modal one is open

	// Its placement (placement.go).
	placed     bool // it has shown: what the user does with it is kept
	sizing     bool // the user moves or sizes it
	normal     Rect // its rectangle when not maximized or minimized
	restoreMax bool // it was left maximized: it shows maximized

	active   bool // it has the keyboard (WM_ACTIVATE)
	full     *fullScreen
	sent     ops.WindowState
	reported bool

	hidden  bool        // its title bar is hidden (titlebar.go)
	caption *captionBar // its caption buttons, then
	topEdge uintptr     // the strip along its top that resizes it, then
}

// fullScreen is what a window in full screen goes back to.
type fullScreen struct {
	style, ex uintptr
	place     WindowPlacement
	maximized bool
}

// k is the window's pixels per point.
func (c *wctl) k(h *Host) float32 {
	if c.frame != nil && c.frame.k > 0 {
		return c.frame.k
	}
	return h.scale
}

// node is what the window reports to: its Window node, or the root.
func (c *wctl) node(h *Host) *node {
	if c.main && h.root != nil {
		return h.root
	}
	return c.n
}

func (c *wctl) has(f ops.WindowFlags) bool { return c.cfg.Flags&f != 0 }

// style is the window's style for its options.
func (c *wctl) style() uint32 {
	s := uint32(WS_OVERLAPPEDWINDOW | WS_CLIPCHILDREN)
	if c.has(ops.WindowFixedSize) {
		s &^= WS_THICKFRAME | WS_MAXIMIZEBOX
	}
	if c.has(ops.WindowNoMinimize) {
		s &^= WS_MINIMIZEBOX
	}
	if c.has(ops.WindowNoMaximize) {
		s &^= WS_MAXIMIZEBOX
	}
	return s
}

// ctlFor is the window n controls: a Window node's, or the main window's
// for the root (the only other node WindowOptions, WindowAction and the
// window's events go to; it may not be the root yet, which is set last in
// the batch that mounts it).
func (h *Host) ctlFor(n *node) *wctl {
	if n == nil {
		return nil
	}
	if n.kind == ops.KindWindow {
		if n.nav != nil {
			return n.nav.ctl
		}
		return nil
	}
	if n.kind == ops.KindView && (h.root == nil || h.root == n) && h.mainCtl != nil {
		h.mainCtl.n = n
		return h.mainCtl
	}
	return nil
}

// parentWindow is the top-level window of the node a child Window belongs
// to: a Window's, else the main window.
func (h *Host) parentWindow(id ops.NodeID) uintptr {
	if p := h.nodes[id]; p != nil && p.kind == ops.KindWindow && p.nav != nil && p.nav.window != 0 {
		return p.nav.window
	}
	return h.window
}

// configure gives a window its options' styles, before it shows: its
// abilities, its title bar, the topmost band, its taskbar button.
func (h *Host) configure(c *wctl) {
	cur := WindowLong(c.hwnd, GWL_STYLE)
	SetWindowLong(c.hwnd, GWL_STYLE, uintptr(c.style())|cur&(WS_VISIBLE|WS_MINIMIZE|WS_MAXIMIZE))
	if c.cfg.TitleBar == ops.TitleBarHidden && !c.hidden {
		h.hideTitleBar(c) // titlebar.go
	}
	SetWindowPos(c.hwnd, 0, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE|SWP_FRAMECHANGED)
	EnableSystemCommand(c.hwnd, SC_CLOSE, !c.has(ops.WindowNoClose))
	if c.has(ops.WindowAlwaysOnTop) {
		SetWindowPos(c.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	}
	if c.has(ops.WindowSkipTaskbar) && Owner(c.hwnd) == 0 {
		SetWindowLong(c.hwnd, GWLP_HWNDPARENT, h.park)
	}
	h.layoutChrome(c)
}

// least and most are the window's least and most content sizes, in
// points (0: none).
func (h *Host) least(c *wctl) [2]float32 {
	if c.main {
		return h.leastSize()
	}
	if c.n != nil && c.n.nav != nil {
		return c.n.nav.minSize
	}
	return [2]float32{}
}

func (c *wctl) most() [2]float32 { return [2]float32{c.cfg.MaxWidth, c.cfg.MaxHeight} }

// outerSize is the size of the window around a content of w by ht
// points, at its DPI: its frame (none at the top under a hidden title
// bar) and menu bar.
func (h *Host) outerSize(c *wctl, w, ht float32) (int32, int32) {
	dpi := DPI(c.hwnd)
	k := float64(dpiScale(dpi))
	r := Rect{Right: int32(math.Round(float64(w) * k)), Bottom: int32(math.Round(float64(ht) * k))}
	style := uint32(WindowLong(c.hwnd, GWL_STYLE))
	if c.full != nil {
		return r.W(), r.H()
	}
	AdjustWindowRect(&r, style, 0, dpi)
	if c.hidden {
		r.Top = -hiddenTop()
	} else if GetMenu(c.hwnd) != 0 {
		r.Bottom += int32(SystemMetric(SM_CYMENU, dpi))
	}
	return r.W(), r.H()
}

// minMax fills WM_GETMINMAXINFO's least and most track sizes from the
// window's least and most content sizes.
func (h *Host) minMax(c *wctl, mi *MinMaxInfo) {
	least, most := h.least(c), c.most()
	if least[0] > 0 || least[1] > 0 {
		w, ht := h.outerSize(c, least[0], least[1])
		if least[0] > 0 {
			mi.MinTrackSize.X = max(mi.MinTrackSize.X, w)
		}
		if least[1] > 0 {
			mi.MinTrackSize.Y = max(mi.MinTrackSize.Y, ht)
		}
	}
	if most[0] > 0 || most[1] > 0 {
		w, ht := h.outerSize(c, max(most[0], 1), max(most[1], 1))
		if most[0] > 0 {
			mi.MaxTrackSize.X = max(w, mi.MinTrackSize.X)
		}
		if most[1] > 0 {
			mi.MaxTrackSize.Y = max(ht, mi.MinTrackSize.Y)
		}
	}
}

// clampSize keeps a content size within the window's least and most.
func (h *Host) clampSize(c *wctl, w, ht float32) (float32, float32) {
	least, most := h.least(c), c.most()
	w, ht = max(w, least[0]), max(ht, least[1])
	if most[0] > 0 {
		w = min(w, max(most[0], least[0]))
	}
	if most[1] > 0 {
		ht = min(ht, max(most[1], least[1]))
	}
	return w, ht
}

// sizeContent sizes the window so that its content is w by ht points,
// keeping its top left.
func (h *Host) sizeContent(c *wctl, w, ht float32) {
	w, ht = h.clampSize(c, w, ht)
	cr, wr := ClientRect(c.hwnd), WindowRect(c.hwnd)
	k := c.k(h)
	dw := int32(math.Round(float64(w*k))) - cr.W()
	dh := int32(math.Round(float64(ht*k))) - cr.H()
	if dw == 0 && dh == 0 {
		return
	}
	SetWindowPos(c.hwnd, 0, 0, 0, wr.W()+dw, wr.H()+dh, SWP_NOMOVE|SWP_NOZORDER|SWP_NOACTIVATE|SWP_NOCOPYBITS)
}

// moveTo puts the window's visible frame's top left at x, y points from
// the main display's top left (its invisible resize borders outside it).
func (h *Host) moveTo(c *wctl, x, y float32) {
	k := c.k(h)
	wr, fb := WindowRect(c.hwnd), FrameBounds(c.hwnd)
	px := int32(math.Round(float64(x*k))) - (fb.Left - wr.Left)
	py := int32(math.Round(float64(y*k))) - (fb.Top - wr.Top)
	SetWindowPos(c.hwnd, 0, px, py, 0, 0, SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
}

// center centers the window on the work area of the monitor of from.
func (h *Host) center(c *wctl, from uintptr) {
	wr, work := WindowRect(c.hwnd), WorkArea(from)
	x := work.Left + (work.W()-wr.W())/2
	y := work.Top + max((work.H()-wr.H())/2, 0)
	SetWindowPos(c.hwnd, 0, x, y, 0, 0, SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
}

// showCtl shows a window (activate: and gives it the keyboard): the first
// time maximized or in full screen if it opens so or was left maximized.
func (h *Host) showCtl(c *wctl, activate bool) {
	first := !c.placed
	switch {
	case first && (c.restoreMax || c.has(ops.WindowMaximized)) && c.full == nil:
		ShowWindow(c.hwnd, SW_SHOWMAXIMIZED)
	case snapshotting() || !activate:
		ShowWindow(c.hwnd, SW_SHOWNOACTIVATE)
	default:
		ShowWindow(c.hwnd, SW_SHOW)
	}
	if first {
		if !IsZoomed(c.hwnd) {
			c.normal = WindowRect(c.hwnd)
		}
		c.placed = true
		if c.has(ops.WindowFullscreen) {
			h.enterFullScreen(c)
		}
	}
	if activate && !snapshotting() {
		SetForegroundWindow(c.hwnd)
	}
	h.layoutChrome(c)
	h.reportState(c)
}

// focus shows the window if it's hidden or minimized, and brings it to the
// front with the keyboard: as a tray icon's menu brings a window back. A
// click on the tray icon, or on one of the app's windows, lets the app
// take the foreground (Windows lets the process that got the last input).
func (h *Host) focus(c *wctl) {
	if IsIconic(c.hwnd) {
		ShowWindow(c.hwnd, SW_RESTORE)
	}
	if !IsWindowVisible(c.hwnd) {
		h.showCtl(c, true)
		return
	}
	if snapshotting() {
		return
	}
	BringWindowToTop(c.hwnd)
	SetForegroundWindow(c.hwnd)
}

func (h *Host) enterFullScreen(c *wctl) {
	if c.full != nil {
		return
	}
	f := &fullScreen{style: WindowLong(c.hwnd, GWL_STYLE), ex: WindowLong(c.hwnd, GWL_EXSTYLE), place: Placement(c.hwnd), maximized: IsZoomed(c.hwnd)}
	c.full = f
	area := MonitorArea(c.hwnd)
	SetWindowLong(c.hwnd, GWL_STYLE, f.style&^uintptr(WS_CAPTION|WS_THICKFRAME|WS_MAXIMIZE|WS_MINIMIZE)|WS_POPUP)
	SetWindowPos(c.hwnd, 0, area.Left, area.Top, area.W(), area.H(), SWP_NOZORDER|SWP_NOACTIVATE|SWP_FRAMECHANGED)
	h.layoutChrome(c)
	h.chromeChanged(c)
}

func (h *Host) exitFullScreen(c *wctl) {
	f := c.full
	if f == nil {
		return
	}
	c.full = nil
	SetWindowLong(c.hwnd, GWL_STYLE, f.style&^uintptr(WS_MAXIMIZE|WS_MINIMIZE)|WindowLong(c.hwnd, GWL_STYLE)&WS_VISIBLE)
	SetPlacement(c.hwnd, f.place)
	SetWindowPos(c.hwnd, 0, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE|SWP_FRAMECHANGED)
	h.layoutChrome(c)
	h.chromeChanged(c)
}

// chromeChanged tells the core of the room a window's content has under
// its caption buttons, which full screen takes away.
func (h *Host) chromeChanged(c *wctl) {
	if c.main {
		h.environmentChanged()
		return
	}
	if c.n != nil && c.n.hwnd != 0 {
		r := ClientRect(c.hwnd)
		h.reportScreen(c.n, h.pt(c.n, r.W()), h.pt(c.n, r.H()))
	}
}

// safeTop is the room the window's caption buttons take at the top of its
// content, in points.
func (c *wctl) safeTop() float32 {
	if c == nil || !c.hidden || c.full != nil {
		return 0
	}
	return captionHeight
}

// windowAction runs a WindowAction command on the window n controls.
func (h *Host) windowAction(n *node, args []byte) {
	a, x, y, ok := ops.DecodeWindowAction(args)
	c := h.ctlFor(n)
	if !ok || c == nil || c.hwnd == 0 || !IsWindow(c.hwnd) {
		return
	}
	switch a {
	case ops.WindowActionShow:
		if !IsWindowVisible(c.hwnd) {
			h.showCtl(c, true)
		}
	case ops.WindowActionHide:
		ShowWindow(c.hwnd, SW_HIDE)
	case ops.WindowActionFocus:
		h.focus(c)
	case ops.WindowActionMinimize:
		ShowWindow(c.hwnd, SW_MINIMIZE)
	case ops.WindowActionMaximize:
		if c.full == nil {
			ShowWindow(c.hwnd, SW_MAXIMIZE)
		}
	case ops.WindowActionRestore:
		if IsIconic(c.hwnd) || IsZoomed(c.hwnd) {
			ShowWindow(c.hwnd, SW_RESTORE)
		}
	case ops.WindowActionFullscreen:
		h.enterFullScreen(c)
	case ops.WindowActionExitFullscreen:
		h.exitFullScreen(c)
	case ops.WindowActionCenter:
		if c.full == nil && !IsZoomed(c.hwnd) && !IsIconic(c.hwnd) {
			h.center(c, c.hwnd)
		}
	case ops.WindowActionSize:
		if c.full == nil {
			if IsZoomed(c.hwnd) || IsIconic(c.hwnd) {
				ShowWindow(c.hwnd, SW_RESTORE)
			}
			h.sizeContent(c, x, y)
		}
	case ops.WindowActionPosition:
		if c.full == nil {
			if IsZoomed(c.hwnd) || IsIconic(c.hwnd) {
				ShowWindow(c.hwnd, SW_RESTORE)
			}
			h.moveTo(c, x, y)
		}
	case ops.WindowActionAlwaysOnTop:
		SetWindowPos(c.hwnd, HWND_TOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	case ops.WindowActionNotAlwaysOnTop:
		SetWindowPos(c.hwnd, HWND_NOTOPMOST, 0, 0, 0, 0, SWP_NOMOVE|SWP_NOSIZE|SWP_NOACTIVATE)
	case ops.WindowActionClose:
		// The core closes a Window itself (Destroy); this is the main
		// window, closing for good: the app quits.
		if c.main {
			h.closeMain()
			return
		}
	}
	h.reportState(c)
}

// windowState is what the window's WindowState reports: its visible
// frame's top left and its content's size, in points (a minimized
// window's as it was), and its state.
func (h *Host) windowState(c *wctl) ops.WindowState {
	s := c.sent
	var f ops.WindowStateFlags
	if IsWindowVisible(c.hwnd) {
		f |= ops.WindowVisible
	}
	iconic := IsIconic(c.hwnd)
	if c.active && !iconic {
		f |= ops.WindowFocused
	}
	if iconic {
		f |= ops.WindowMinimizedState
	} else {
		k := c.k(h)
		fb, cr := FrameBounds(c.hwnd), ClientRect(c.hwnd)
		s.X, s.Y = float32(fb.Left)/k, float32(fb.Top)/k
		s.Width, s.Height = float32(cr.W())/k, float32(cr.H())/k
	}
	if IsZoomed(c.hwnd) {
		f |= ops.WindowMaximizedState
	}
	if c.full != nil {
		f |= ops.WindowFullscreenState
	}
	if WindowLong(c.hwnd, GWL_EXSTYLE)&WS_EX_TOPMOST != 0 {
		f |= ops.WindowOnTop
	}
	s.Flags = f
	return s
}

// reportState sends the window's state if the core listens for it and it
// changed.
func (h *Host) reportState(c *wctl) {
	n := c.node(h)
	if n == nil || n.dying || !n.listening(ops.EventWindowState) || c.hwnd == 0 || !IsWindow(c.hwnd) {
		return
	}
	s := h.windowState(c)
	if c.reported && s == c.sent {
		return
	}
	c.sent, c.reported = s, true
	h.emit(n, ops.Event{Kind: ops.EventWindowState, Bytes: ops.AppendWindowState(nil, s)})
}

// listenWindow follows the core starting to listen for a window's state:
// it's reported at once, once the batch is applied.
func (h *Host) listenWindow(n *node, e ops.EventKind) {
	if e != ops.EventWindowState {
		return
	}
	c := h.ctlFor(n)
	if c == nil {
		return // a Window not opened yet reports as it opens
	}
	c.reported = false
	h.afterCore(func() { h.reportState(c) })
}

// ctlMessage handles what every controlled window shares, before its own
// window procedure's: its most size, its hidden title bar, the user's
// closing, and its state as it changes. ok false lets the window's own
// handling (and DefWindowProc) run.
func (h *Host) ctlMessage(c *wctl, m uint32, wp, lp uintptr) (uintptr, bool) {
	if c.key != "" {
		h.placementMessage(c, m, wp)
	}
	if r, ok := h.titleBarMessage(c, m, wp, lp); ok {
		return r, true
	}
	switch m {
	case WM_GETMINMAXINFO:
		h.minMax(c, Native[MinMaxInfo](lp))
		return 0, true
	case WM_SYSCOMMAND:
		if wp&0xFFF0 == SC_CLOSE && c.has(ops.WindowNoClose) {
			return 0, true
		}
	case WM_CLOSE:
		// The user's: Alt+F4, the close button, the taskbar's menu.
		if c.has(ops.WindowNoClose) {
			return 0, true
		}
		if n := c.node(h); n != nil && !n.dying && n.listening(ops.EventCloseRequest) {
			h.emit(n, ops.Event{Kind: ops.EventCloseRequest})
			return 0, true
		}
	case WM_ACTIVATE:
		c.active = LoWord(wp) != WA_INACTIVE
		if c.caption != nil {
			c.caption.activate(h, c.active)
		}
		h.reportState(c)
	case WM_SIZE:
		h.layoutChrome(c)
		h.reportState(c)
	case WM_MOVE, WM_WINDOWPOSCHANGED:
		h.reportState(c)
	}
	return 0, false
}

// closeMain closes the main window for good, as its close button does when
// nothing asks first: the app quits.
func (h *Host) closeMain() {
	// Closing it quits the app: OnQuit's handlers may keep it open.
	if !h.askQuit(false) {
		return
	}
	h.savePlacement(h.mainCtl)
	h.quitting = true
	DestroyWindow(h.window)
}

// mainKey is the main window's state key: the app's, or "main".
func mainKey(cfg *ops.WindowConfig) string {
	if cfg.StateKey != "" {
		return cfg.StateKey
	}
	return winstate.Main
}

// bgOf is the background of the top-level window hwnd: its options' color
// over the theme's, or the theme's.
func (h *Host) bgOf(hwnd uintptr) rgba {
	if c := h.ctls[hwnd]; c != nil && c.cfg.Background != 0 {
		if bg, ok := h.resolve(c.cfg.Background); ok {
			return bg.over(h.theme.bg)
		}
	}
	return h.theme.bg
}

// bgIn is the window background behind a view: its top-level window's.
func (h *Host) bgIn(hwnd uintptr) rgba {
	if hwnd == 0 || len(h.ctls) == 0 {
		return h.theme.bg
	}
	return h.bgOf(RootWindow(hwnd))
}

// underCaption reports whether a view is in a window whose title bar is
// hidden (its top strip the caption).
func (h *Host) underCaption(hwnd uintptr) bool {
	c := h.ctls[RootWindow(hwnd)]
	return c != nil && c.hidden && c.full == nil
}
