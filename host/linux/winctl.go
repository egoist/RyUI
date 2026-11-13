//go:build linux && !android

package linux

import (
	"fmt"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/winstate"
	"github.com/egoist/asuka/ops"
)

// Controlling windows (ui.WindowHandle, WindowOptions, WindowProps): the
// main window's and each Window's options as it opens, the app's
// WindowAction commands, and their state reported as it changes.
//
// What GTK 4 does: the content's size (the window's default size, the
// header bar's height on top) and its least (the content's size
// request); not resizable (gtk_window_set_resizable), no close button
// (gtk_window_set_deletable), no minimize or maximize button (the header
// bar's decoration layout, the desktop's without them: the window
// manager's shortcuts still minimize it), maximized and full screen,
// hidden until a Show, a background color or none (the window's CSS,
// the compositor showing what's behind), a hidden title bar (the content
// under the header bar, which goes flat and transparent and shows no
// title, its window buttons kept; the environment's SafeTop is its
// height), a child (transient for its parent, destroyed with it) and a
// modal one (gtk_window_set_modal: the parent takes no input while it's
// open).
//
// What it can't: a window's place (X, Y, Center, SetPosition): Wayland
// leaves it to the compositor, and GTK 4 dropped gtk_window_move on X11
// too; a most size (GDK's toplevel layout has a least size only); always
// on top and skipping the taskbar (the hints GTK 3 set on X11 are gone,
// and Wayland has no such requests). Those options and actions do
// nothing, and the position reported is 0, 0. Wayland doesn't tell an
// app its window is minimized either: Minimized reads true on X11 only.
//
// The state is reported (WindowState) while the core listens: at once,
// then as the window shows or hides, takes or loses the keyboard, is
// maximized, minimized or put in full screen, or its content is resized.
// The user closing a window the core asks about (it listens for
// CloseRequest) leaves it open and sends CloseRequest; the core then
// closes a Window itself, and the main window with WindowAction Close.

// winFor is the window a node controls: the root's is the main window, a
// Window node's its own.
func (h *Host) winFor(n *node) *win {
	if n == nil {
		return nil
	}
	if n.kind == ops.KindWindow {
		return n.win
	}
	if n == h.root {
		return h.main
	}
	return nil
}

// windowNode is the node a window's events go to: the root for the main
// window, else its Window node.
func (h *Host) windowNode(w *win) *node {
	if w.node != nil {
		return w.node
	}
	if w == h.main {
		return h.root
	}
	return nil
}

// headerRoom is how much of the window's height its header bar takes
// above the content: none with a hidden title bar, the content going
// under it.
func (w *win) headerRoom() int32 {
	if w.cfg.TitleBar == ops.TitleBarHidden {
		return 0
	}
	return headerHeight
}

// configure applies a window's options before it shows: its size (or
// the one remembered under its key), its least, what the user can do,
// how it looks, and whose child it is.
func (h *Host) configure(w *win, c ops.WindowConfig) {
	w.cfg = c
	w.configured = true
	w.hidden = c.Flags&ops.WindowHidden != 0

	WindowSetResizable(w.window, c.Flags&ops.WindowFixedSize == 0)
	WindowSetDeletable(w.window, c.Flags&ops.WindowNoClose == 0)
	if c.Flags&(ops.WindowNoMinimize|ops.WindowNoMaximize) != 0 {
		AdwHeaderBarSetDecorationLayout(w.header, decorationLayout(c.Flags&ops.WindowNoMinimize != 0, c.Flags&ops.WindowNoMaximize != 0))
	}

	hidden := c.TitleBar == ops.TitleBarHidden
	AdwToolbarViewSetExtendContentToTopEdge(w.toolbar, hidden)
	AdwHeaderBarSetShowTitle(w.header, !hidden)
	if hidden {
		AdwToolbarViewSetTopBarStyle(w.toolbar, 0) // flat
		Connect(w.toolbar, "notify::top-bar-height", func(uintptr) { h.envChanged() })
	}
	h.windowCSS(w)

	// Its size: the content's, the header bar on top; the user's, if it's
	// remembered.
	width, height := c.Width, c.Height
	if width <= 0 {
		width = 800
	}
	if height <= 0 {
		height = 600
	}
	WindowSetDefaultSize(w.window, windowSize(width), windowSize(height)+w.headerRoom())
	w.stateKey = c.StateKey
	if w == h.main && w.stateKey == "" {
		w.stateKey = winstate.Main
	}
	h.restoreWindow(w)
	minW, minH := c.MinWidth, c.MinHeight
	if w == h.main {
		// The main window's least is 320 points square unless the app
		// says.
		if minW <= 0 {
			minW = 320
		}
		if minH <= 0 {
			minH = 320
		}
	}
	WidgetSetSizeRequest(w.content, windowSize(minW), windowSize(minH))
	if c.Flags&ops.WindowMaximized != 0 {
		WindowMaximize(w.window)
	}
	if c.Flags&ops.WindowFullscreen != 0 {
		WindowFullscreen(w.window)
	}

	if c.Flags&ops.WindowChild != 0 {
		if p := h.parentWin(c.Parent); p != nil && p != w {
			WindowSetTransientFor(w.window, p.window)
			WindowSetDestroyWithParent(w.window, true)
			WindowSetModal(w.window, c.Flags&ops.WindowModal != 0)
		}
	}
}

// parentWin is the window of a Window's Parent: the root's (the main
// window) or a Window node's.
func (h *Host) parentWin(id ops.NodeID) *win {
	n := h.nodes[id]
	if n == nil {
		return nil
	}
	if n.kind == ops.KindWindow {
		return n.win
	}
	// The root (a parent is a Window or the root, which the core may not
	// have said is the root yet: a Window open as the app starts).
	return h.main
}

// decorationLayout is the desktop's layout of a header bar's window
// buttons (gtk-decoration-layout: "icon:minimize,maximize,close") less
// minimize or maximize.
func decorationLayout(noMin, noMax bool) string {
	layout := ""
	if s := SettingsGetDefault(); s != 0 {
		layout = GetStringProperty(s, "gtk-decoration-layout")
	}
	if layout == "" {
		layout = "menu:minimize,maximize,close"
	}
	sides := strings.SplitN(layout, ":", 2)
	for i, side := range sides {
		var keep []string
		for _, b := range strings.Split(side, ",") {
			if b == "minimize" && noMin || b == "maximize" && noMax {
				continue
			}
			keep = append(keep, b)
		}
		sides[i] = strings.Join(keep, ",")
	}
	return strings.Join(sides, ":")
}

// windowCSS gives a window its own background (a color, or none, so the
// compositor shows what's behind where the content draws nothing) and,
// with a hidden title bar, a header bar with none either: a style
// provider of its own, selecting the window by its name. Semantic colors
// follow the style (themeChanged calls it again).
func (h *Host) windowCSS(w *win) {
	c := &w.cfg
	var css strings.Builder
	sel := "window#" + w.cssName
	if w.cssName == "" {
		h.nextWin++
		w.cssName = fmt.Sprintf("asuka-window-%d", h.nextWin)
		sel = "window#" + w.cssName
	}
	switch {
	case c.Flags&ops.WindowTransparent != 0:
		fmt.Fprintf(&css, "%s, %s toolbarview { background: none; background-color: transparent; }\n", sel, sel)
	case c.Background != 0:
		if col, ok := h.resolve(c.Background); ok {
			fmt.Fprintf(&css, "%s { background-color: rgba(%d,%d,%d,%g); }\n", sel,
				int(col.R*255+0.5), int(col.G*255+0.5), int(col.B*255+0.5), col.A)
		}
	}
	if c.TitleBar == ops.TitleBarHidden {
		fmt.Fprintf(&css, "%s toolbarview > .top-bar, %s headerbar { background: none; background-color: transparent; box-shadow: none; }\n", sel, sel)
	}
	if css.Len() == 0 && w.css == 0 {
		return
	}
	WidgetSetName(w.window, w.cssName)
	if w.css == 0 {
		w.css = CSSProviderNew()
		StyleContextAddProviderForDisplay(DisplayGetDefault(), w.css, 800) // above the app's (700)
	}
	CSSProviderLoadFromString(w.css, css.String())
}

// dropWindowCSS removes a closing window's style provider.
func (h *Host) dropWindowCSS(w *win) {
	if w.css != 0 {
		StyleContextRemoveProviderForDisplay(DisplayGetDefault(), w.css)
		ObjectUnref(w.css)
		w.css = 0
	}
}

// showMain shows the main window as the app starts, unless it's to start
// hidden.
func (h *Host) showMain() {
	w := h.main
	if !w.configured {
		h.configure(w, ops.WindowConfig{})
	}
	if !w.hidden {
		WindowPresent(w.window)
	}
}

// windowAction does a WindowAction to w.
func (h *Host) windowAction(w *win, args []byte) {
	a, x, y, ok := ops.DecodeWindowAction(args)
	if !ok || w.closing {
		return
	}
	win := w.window
	switch a {
	case ops.WindowActionShow:
		w.hidden = false
		WidgetSetVisible(win, true)
	case ops.WindowActionHide:
		WidgetSetVisible(win, false)
	case ops.WindowActionFocus:
		w.hidden = false
		WindowUnminimize(win)
		WindowPresent(win)
	case ops.WindowActionMinimize:
		WindowMinimize(win)
	case ops.WindowActionMaximize:
		WindowMaximize(win)
	case ops.WindowActionRestore:
		if h.minimized(w) {
			WindowUnminimize(win)
			WindowPresent(win)
		} else if WindowIsMaximized(win) {
			WindowUnmaximize(win)
		}
	case ops.WindowActionFullscreen:
		WindowFullscreen(win)
	case ops.WindowActionExitFullscreen:
		WindowUnfullscreen(win)
	case ops.WindowActionSize:
		// The window's size unmaximized: it takes it now, if it isn't
		// maximized or in full screen.
		WindowSetDefaultSize(win, windowSize(x), windowSize(y)+w.headerRoom())
	case ops.WindowActionClose:
		if w == h.main {
			// On the main loop's next turn: the core's answer to a close
			// request comes while GTK is still emitting close-request.
			w.forced = true
			IdleAdd(PriorityDefault, func() bool {
				if !w.closing {
					WindowClose(win)
				}
				return false
			})
		}
	case ops.WindowActionPosition, ops.WindowActionCenter, ops.WindowActionAlwaysOnTop, ops.WindowActionNotAlwaysOnTop:
		// The desktop's to say: GTK 4 can't ask.
	}
	h.queueWindowState(w)
}

// minimized is whether w's surface is minimized.
func (h *Host) minimized(w *win) bool {
	s := NativeGetSurface(w.window)
	return s != 0 && toplevelState(s)&toplevelMinimized != 0
}

// watchWindowState follows what a window's state is made of, to report
// it: shown or hidden, active, maximized, full screen, its surface's
// state (minimized). Its size comes as its content is allocated.
func (h *Host) watchWindowState(w *win) {
	window := w.window
	for _, p := range []string{"notify::visible", "notify::is-active", "notify::maximized", "notify::fullscreened"} {
		Connect(window, p, func(uintptr) { h.queueWindowState(w) })
	}
	Connect(window, "realize", func() {
		if s := NativeGetSurface(window); s != 0 {
			Connect(s, "notify::state", func(uintptr) { h.queueWindowState(w) })
		}
	})
}

// queueWindowState reports w's state once the call into the core in
// progress returns (now, if none is), if the core listens for it.
func (h *Host) queueWindowState(w *win) {
	n := h.windowNode(w)
	if n == nil || !n.listening(ops.EventWindowState) {
		return
	}
	h.afterCore(func() { h.reportWindowState(w) })
}

// reportWindowState sends w's state, if it changed since it was last
// sent.
func (h *Host) reportWindowState(w *win) {
	n := h.windowNode(w)
	if n == nil || w.closing || !n.listening(ops.EventWindowState) {
		return
	}
	s := h.windowState(w)
	if w.reported && s == w.sent {
		return
	}
	w.reported, w.sent = true, s
	h.emit(n, ops.Event{Kind: ops.EventWindowState, Bytes: ops.AppendWindowState(nil, s)})
}

// windowState is w's state: no position (GTK 4 doesn't know it), its
// content's size (as allocated; before that, as it will be).
func (h *Host) windowState(w *win) ops.WindowState {
	s := ops.WindowState{Width: w.envW, Height: w.envH}
	if s.Width == 0 && s.Height == 0 {
		var dw, dh int32
		WindowGetDefaultSize(w.window, &dw, &dh)
		s.Width, s.Height = float32(dw), float32(max(dh-w.headerRoom(), 0))
	}
	if WidgetGetVisible(w.window) {
		s.Flags |= ops.WindowVisible
	}
	if WindowIsActive(w.window) {
		s.Flags |= ops.WindowFocused
	}
	if h.minimized(w) {
		s.Flags |= ops.WindowMinimizedState
	}
	if WindowIsMaximized(w.window) {
		s.Flags |= ops.WindowMaximizedState
	}
	if WindowIsFullscreen(w.window) {
		s.Flags |= ops.WindowFullscreenState
	}
	return s
}
