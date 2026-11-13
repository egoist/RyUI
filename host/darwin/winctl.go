//go:build darwin && !ios

package darwin

import (
	"math"
	"os"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
)

// Controlling windows (ui.WindowHandle, WindowOptions, WindowProps): the
// main window's and each Window's options as it opens, WindowAction
// commands, and their state reported as it changes.
//
// The options are NSWindow's: its style mask (no resize, close or
// minimize button; a full-size content view under a transparent,
// title-less title bar for TitleBarHidden, the traffic lights kept, the
// environment's SafeTop the title bar's height, from contentLayoutRect),
// the zoom button turned off for DisableMaximize, the content's least and
// most size, the floating level for AlwaysOnTop, left out of the Window
// menu for SkipTaskbar (a Mac has no taskbar), a background color or a
// clear, non-opaque window for Transparent, zoomed or in full screen as it
// shows. A child is a child window of its parent's (ordered above it,
// moving with it), a modal one a sheet of it. Positions are the frame's
// top left from the main display's top left, y down: AppKit's are from its
// bottom left, y up.
//
// Where a window was is NSWindow's frame autosave: the main window's under
// "asuka <app>" (as before keys) when its key is "main", the others' under
// "asuka <app> <key>"; restored as the window opens, over its size and
// place. A Mac has no maximized state to keep: a zoomed window's frame is
// what's restored.
//
// The state is reported (WindowState) while the core listens: at once,
// then as the window shows or hides (the app's actions), becomes or stops
// being key, the app comes to the front or leaves it, the window moves,
// resizes, is minimized, zoomed or goes in or out of full screen.

const (
	styleFullScreen      = 1 << 14
	styleFullSizeContent = 1 << 15
	windowZoomButton     = 2
	floatingLevel        = 3
)

// autosaveName is where AppKit keeps a window's frame for key.
func (h *Host) autosaveName(key string) string {
	if key == "" || key == "main" {
		return "asuka " + h.appName
	}
	return "asuka " + h.appName + " " + key
}

// remembers reports whether windows' places are kept: not a test's, nor
// an automation run's, which would leave theirs in the user's defaults.
func remembers() bool {
	return !testing.Testing() && os.Getenv("ASUKA_SNAPSHOT") == "" && os.Getenv("ASUKA_SCRIPT") == ""
}

// styleMask is the style a window's options give it.
func styleMask(c *ops.WindowConfig) uint {
	m := uint(styleTitled | styleClosable | styleMiniaturizable | styleResizable)
	if c.Flags&ops.WindowFixedSize != 0 {
		m &^= styleResizable
	}
	if c.Flags&ops.WindowNoClose != 0 {
		m &^= styleClosable
	}
	if c.Flags&ops.WindowNoMinimize != 0 {
		m &^= styleMiniaturizable
	}
	if c.TitleBar == ops.TitleBarHidden {
		m |= styleFullSizeContent
	}
	return m
}

// configure applies a window's options to x as it opens: its style, its
// least and most size, its size and place (unless its frame was kept
// under its key, which wins), level, background. It reports whether a
// kept frame was restored.
func (h *Host) configure(x *win, c ops.WindowConfig) (restored bool) {
	w := x.window
	x.cfg, x.configured = c, true
	full := Send(w, "styleMask")&styleFullScreen != 0
	mask := styleMask(&c)
	if full {
		mask |= styleFullScreen
	}
	Send(w, "setStyleMask:", uintptr(mask))
	if c.TitleBar == ops.TitleBarHidden {
		Send(w, "setTitlebarAppearsTransparent:", 1)
		Send(w, "setTitleVisibility:", 1) // hidden
	}
	if b := Send(w, "standardWindowButton:", windowZoomButton); b != 0 {
		Send(b, "setEnabled:", Bool(c.Flags&(ops.WindowNoMaximize|ops.WindowFixedSize) == 0))
	}
	if c.MaxWidth > 0 || c.MaxHeight > 0 {
		mw, mh := float64(c.MaxWidth), float64(c.MaxHeight)
		if mw <= 0 {
			mw = math.MaxFloat32
		}
		if mh <= 0 {
			mh = math.MaxFloat32
		}
		MsgSetSize(w, Sel("setContentMaxSize:"), Size{Width: mw, Height: mh})
	}
	if c.Flags&ops.WindowAlwaysOnTop != 0 {
		Send(w, "setLevel:", floatingLevel)
	}
	if c.Flags&ops.WindowSkipTaskbar != 0 {
		Send(w, "setExcludedFromWindowsMenu:", 1)
	}
	switch {
	case c.Flags&ops.WindowTransparent != 0:
		Send(w, "setOpaque:", 0)
		Send(w, "setBackgroundColor:", uintptr(Send(Class("NSColor"), "clearColor")))
	case c.Background != 0:
		if col := h.nsColor(c.Background); col != 0 {
			Send(w, "setBackgroundColor:", uintptr(col))
		}
	}
	// Its size, then its place: where it was left, or the options'.
	if c.Width > 0 || c.Height > 0 {
		cur := MsgRect(x.content, Sel("bounds")).Size
		width, height := float64(c.Width), float64(c.Height)
		if width <= 0 {
			width = cur.Width
		}
		if height <= 0 {
			height = cur.Height
		}
		MsgSetSize(w, Sel("setContentSize:"), Size{Width: width, Height: height})
	}
	if x == h.main || c.Flags&ops.WindowCentered != 0 {
		Send(w, "center")
	}
	if c.Flags&ops.WindowPositioned != 0 {
		h.setTopLeft(w, float64(c.X), float64(c.Y))
	}
	key := c.StateKey
	if x == h.main && key == "" {
		key = "main"
	}
	if key != "" && remembers() {
		name := uintptr(String(h.autosaveName(key)))
		restored = SendBool(w, "setFrameUsingName:", name)
		Send(w, "setFrameAutosaveName:", name)
	}
	x.layoutChanged()
	return restored
}

// mainScreenHeight is the height of the main display (the one with the
// menu bar, AppKit's origin), which positions are flipped by.
func mainScreenHeight() float64 {
	screens := ArrayItems(Send(Class("NSScreen"), "screens"))
	if len(screens) == 0 {
		return 0
	}
	return MsgRect(screens[0], Sel("frame")).Size.Height
}

// setTopLeft puts w's frame's top left at x, y points from the main
// display's top left.
func (h *Host) setTopLeft(w ID, x, y float64) {
	MsgPointID(w, Sel("setFrameTopLeftPoint:"), Point{X: x, Y: mainScreenHeight() - y})
}

// showWin shows x: a modal one as a sheet of its parent, the others in
// front, key; then what it was to open as (zoomed, in full screen).
func (h *Host) showWin(x *win, activate bool) {
	w := x.window
	switch {
	case x.sheetOf != 0:
		if Send(w, "sheetParent") == 0 {
			Send(x.sheetOf, "beginSheet:completionHandler:", uintptr(w), 0)
		}
	case os.Getenv("ASUKA_SNAPSHOT") != "":
		Send(w, "orderFront:", 0)
	default:
		if activate {
			Send(Send(Class("NSApplication"), "sharedApplication"), "activateIgnoringOtherApps:", 1)
		}
		Send(w, "makeKeyAndOrderFront:", 0)
	}
	if x.child != 0 && Send(w, "parentWindow") == 0 {
		Send(x.child, "addChildWindow:ordered:", uintptr(w), 1) // above
	}
	if !x.shown {
		x.shown = true
		if x.cfg.Flags&ops.WindowMaximized != 0 && !SendBool(w, "isZoomed") {
			Send(w, "zoom:", 0)
		}
		if x.cfg.Flags&ops.WindowFullscreen != 0 && Send(w, "styleMask")&styleFullScreen == 0 {
			Send(w, "toggleFullScreen:", 0)
		}
	}
	h.windowsChanged()
}

// parentWindow is the NSWindow a child's Config.Parent names: a Window
// node's, else the main window (the root's, whose SetRoot may come after
// the child's props).
func (h *Host) parentWindow(id ops.NodeID) ID {
	if p := h.nodes[id]; p != nil && p.kind == ops.KindWindow && p.nav != nil && p.nav.window != 0 {
		return p.nav.window
	}
	return h.window
}

// winNode is the node a window's events go to: its Window node, or the
// root for the main window.
func (h *Host) winNode(x *win) *node {
	if x == h.main {
		return h.root
	}
	return x.root
}

// nodeWin is the window a WindowAction's node controls.
func (h *Host) nodeWin(n *node) *win {
	if n.kind == ops.KindWindow {
		if n.nav == nil {
			return nil
		}
		return h.wins[n.nav.window]
	}
	if n == h.root || n.kind == ops.KindView && h.root == nil {
		return h.main
	}
	return nil
}

// windowAction does what the app asked of a window.
func (h *Host) windowAction(n *node, args []byte) {
	x := h.nodeWin(n)
	a, px, py, ok := ops.DecodeWindowAction(args)
	if x == nil || !ok {
		return
	}
	w := x.window
	app := Send(Class("NSApplication"), "sharedApplication")
	switch a {
	case ops.WindowActionShow:
		h.showWin(x, false)
	case ops.WindowActionHide:
		if p := Send(w, "sheetParent"); p != 0 {
			Send(p, "endSheet:", uintptr(w))
		}
		Send(w, "orderOut:", 0)
	case ops.WindowActionFocus:
		if SendBool(app, "isHidden") {
			Send(app, "unhide:", 0)
		}
		if SendBool(w, "isMiniaturized") {
			Send(w, "deminiaturize:", 0)
		}
		h.showWin(x, true)
	case ops.WindowActionMinimize:
		Send(w, "miniaturize:", 0)
	case ops.WindowActionMaximize:
		if !SendBool(w, "isZoomed") {
			Send(w, "zoom:", 0)
		}
	case ops.WindowActionRestore:
		switch {
		case SendBool(w, "isMiniaturized"):
			Send(w, "deminiaturize:", 0)
		case SendBool(w, "isZoomed"):
			Send(w, "zoom:", 0)
		}
	case ops.WindowActionFullscreen, ops.WindowActionExitFullscreen:
		if full := Send(w, "styleMask")&styleFullScreen != 0; full != (a == ops.WindowActionFullscreen) {
			Send(w, "toggleFullScreen:", 0)
		}
	case ops.WindowActionCenter:
		Send(w, "center")
	case ops.WindowActionSize:
		MsgSetSize(w, Sel("setContentSize:"), Size{Width: float64(px), Height: float64(py)})
	case ops.WindowActionPosition:
		h.setTopLeft(w, float64(px), float64(py))
	case ops.WindowActionAlwaysOnTop:
		Send(w, "setLevel:", floatingLevel)
	case ops.WindowActionNotAlwaysOnTop:
		Send(w, "setLevel:", 0)
	case ops.WindowActionClose:
		if x == h.main {
			// As the user's closing does, past the core's question: the
			// app quits once its last window has closed.
			h.afterCore(func() { Send(w, "close") })
		}
	}
	h.windowsChanged()
}

// windowState is x's state, as WindowState reports it.
func (h *Host) windowState(x *win) ops.WindowState {
	w := x.window
	f := MsgRect(w, Sel("frame"))
	b := MsgRect(x.content, Sel("bounds")).Size
	s := ops.WindowState{
		X: float32(f.Origin.X), Y: float32(mainScreenHeight() - f.Origin.Y - f.Size.Height),
		Width: float32(b.Width), Height: float32(b.Height),
	}
	app := Send(Class("NSApplication"), "sharedApplication")
	mini := SendBool(w, "isMiniaturized")
	if SendBool(w, "isVisible") || mini {
		s.Flags |= ops.WindowVisible
	}
	if SendBool(w, "isKeyWindow") && SendBool(app, "isActive") {
		s.Flags |= ops.WindowFocused
	}
	if mini {
		s.Flags |= ops.WindowMinimizedState
	}
	full := Send(w, "styleMask")&styleFullScreen != 0
	if full {
		s.Flags |= ops.WindowFullscreenState
	} else if SendBool(w, "isZoomed") && Send(w, "styleMask")&styleResizable != 0 {
		s.Flags |= ops.WindowMaximizedState
	}
	if SendInt(w, "level") > 0 {
		s.Flags |= ops.WindowOnTop
	}
	return s
}

// windowsChanged reports the state of each window the core watches, if
// it changed, once the call into the core in progress has returned.
func (h *Host) windowsChanged() {
	if h.winQueued {
		return
	}
	h.winQueued = true
	h.afterCore(func() {
		h.winQueued = false
		for _, x := range h.allWins() {
			n := h.winNode(x)
			if n == nil || !n.listening(ops.EventWindowState) {
				continue
			}
			s := h.windowState(x)
			if x.stateSent && s == x.state {
				continue
			}
			x.state, x.stateSent = s, true
			h.dispatch(ops.Event{ID: n.id, Kind: ops.EventWindowState, Bytes: ops.AppendWindowState(nil, s)})
		}
	})
}

// listenWindow starts reporting a window's state: it's sent at once.
func (h *Host) listenWindow(n *node) {
	if x := h.nodeWin(n); x != nil {
		x.stateSent = false
		h.windowsChanged()
	}
}

// safeTop is the room a hidden title bar's strip (and a toolbar in it)
// takes over x's content: what the content lays out under.
func (x *win) safeTop() float32 {
	if x == nil || x.cfg.TitleBar != ops.TitleBarHidden || Send(x.window, "styleMask")&styleFullScreen != 0 {
		return 0
	}
	b := MsgRect(x.content, Sel("bounds")).Size.Height
	l := MsgRect(x.window, Sel("contentLayoutRect")).Size.Height
	return float32(max(b-l, 0))
}

// layoutChanged reports what a window's options changed in its content's
// environment (a hidden title bar's room).
func (x *win) layoutChanged() {
	if h := theHost; h != nil {
		if x == h.main {
			h.environmentChanged()
		} else if x.root != nil && x.root.nav != nil {
			x.root.nav.reported = false
			h.reportScreen(x.root, MsgRect(x.content, Sel("bounds")).Size)
		}
	}
}

func init() {
	h := func() *Host { return theHost }
	changed := func(self ID, _ SEL, n ID) {
		if t := h(); t != nil {
			t.windowsChanged()
		}
	}
	targetMethods = append(targetMethods,
		Method("windowDidMove:", changed),
		Method("windowDidBecomeKey:", changed),
		Method("windowDidResignKey:", changed),
		Method("windowDidEnterFullScreen:", func(self ID, _ SEL, n ID) {
			if t := h(); t != nil {
				t.environmentChanged()
				t.windowsChanged()
			}
		}),
		Method("windowDidExitFullScreen:", func(self ID, _ SEL, n ID) {
			if t := h(); t != nil {
				t.environmentChanged()
				t.windowsChanged()
			}
		}),
	)
}
