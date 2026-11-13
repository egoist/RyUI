//go:build windows

package windows

import (
	"fmt"
	"path/filepath"
	"testing"
	"unsafe"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// act runs f as the app would, from a posted function: then the core
// flushes, and the host's events go back to it.
func act(h *Host, f func()) {
	h.enter(func() { ui.Post(f) })
	h.runPosted()
}

// near reports a and b within a pixel's rounding at any scale.
func near(a, b float32) bool { return a-b < 1 && b-a < 1 }

// The main window's options, as its styles and track sizes.
func TestWindowOptionsApplied(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Width: 500, Height: 400, MaxWidth: 900, MaxHeight: 700,
		DisableMaximize: true, DisableClose: true, AlwaysOnTop: true, SkipTaskbar: true})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		style := WindowLong(h.window, GWL_STYLE)
		if style&WS_MAXIMIZEBOX != 0 || style&WS_MINIMIZEBOX == 0 || style&WS_THICKFRAME == 0 {
			fatalf("style %#x", style)
		}
		if WindowLong(h.window, GWL_EXSTYLE)&WS_EX_TOPMOST == 0 {
			fatalf("not topmost")
		}
		if Owner(h.window) != h.park {
			fatalf("not kept off the taskbar: owner %#x", Owner(h.window))
		}
		var mi MinMaxInfo
		SendMessage(h.window, WM_GETMINMAXINFO, 0, uintptr(unsafe.Pointer(&mi)))
		most := Rect{Right: h.px(nil, 900), Bottom: h.px(nil, 700)}
		AdjustWindowRect(&most, uint32(style), 0, DPI(h.window))
		if mi.MaxTrackSize.X != most.W() || mi.MaxTrackSize.Y != most.H() {
			fatalf("most %v, want %dx%d", mi.MaxTrackSize, most.W(), most.H())
		}
		// The user can't close it.
		SendMessage(h.window, WM_CLOSE, 0, 0)
		SendMessage(h.window, WM_SYSCOMMAND, SC_CLOSE, 0)
		if !IsWindow(h.window) || h.quitting {
			fatalf("closed by the user")
		}
	})
}

// The handle's actions, and the state the host reports as it changes:
// the app's doing and the user's.
func TestWindowActionsAndState(t *testing.T) {
	var w *ui.WindowHandle
	h, _ := mount(t, func() ui.Node {
		w = ui.UseWindow()
		return ui.StaticText("x")
	})
	onMain(t, func() {
		h.showMain()
		h.runPosted()
		if !w.Visible().Get() {
			fatalf("shown, it reads hidden")
		}
		act(h, w.Hide)
		if IsWindowVisible(h.window) || w.Visible().Get() {
			fatalf("Hide: visible %v, reads %v", IsWindowVisible(h.window), w.Visible().Get())
		}
		act(h, w.Focus) // as a tray icon's menu brings it back
		if !IsWindowVisible(h.window) || !w.Visible().Get() {
			fatalf("Focus didn't show it")
		}
		act(h, func() { w.SetSize(500, 400) })
		if cr := ClientRect(h.window); cr.W() != h.px(nil, 500) || cr.H() != h.px(nil, 400) {
			fatalf("SetSize: client %dx%d", cr.W(), cr.H())
		}
		if s := w.Size().Get(); s != (ui.Size{Width: 500, Height: 400}) {
			fatalf("Size reads %v", s)
		}
		act(h, func() { w.SetPosition(100, 120) })
		if fb := FrameBounds(h.window); fb.Left != h.px(nil, 100) || fb.Top != h.px(nil, 120) {
			fatalf("SetPosition: frame at %d, %d", fb.Left, fb.Top)
		}
		if p := w.Position().Get(); !near(p.X, 100) || !near(p.Y, 120) {
			fatalf("Position reads %v", p)
		}
		act(h, func() { w.SetAlwaysOnTop(true) })
		if WindowLong(h.window, GWL_EXSTYLE)&WS_EX_TOPMOST == 0 || !w.AlwaysOnTop().Get() {
			fatalf("not on top")
		}
		act(h, func() { w.SetAlwaysOnTop(false) })
		if w.AlwaysOnTop().Get() {
			fatalf("still on top")
		}
		act(h, w.Maximize)
		if !IsZoomed(h.window) || !w.Maximized().Get() {
			fatalf("Maximize: zoomed %v, reads %v", IsZoomed(h.window), w.Maximized().Get())
		}
		act(h, w.Restore)
		if IsZoomed(h.window) || w.Maximized().Get() {
			fatalf("Restore left it maximized")
		}
		before := WindowRect(h.window)
		act(h, func() { w.SetFullscreen(true) })
		if WindowRect(h.window) != MonitorArea(h.window) || WindowLong(h.window, GWL_STYLE)&WS_CAPTION != 0 || !w.Fullscreen().Get() {
			fatalf("full screen at %v, monitor %v", WindowRect(h.window), MonitorArea(h.window))
		}
		act(h, func() { w.SetFullscreen(false) })
		if WindowRect(h.window) != before || w.Fullscreen().Get() {
			fatalf("out of full screen at %v, was %v", WindowRect(h.window), before)
		}
		act(h, w.Minimize)
		if !IsIconic(h.window) || !w.Minimized().Get() || w.Focused().Get() {
			fatalf("Minimize: iconic %v, reads %v, focused %v", IsIconic(h.window), w.Minimized().Get(), w.Focused().Get())
		}
		// The user brings it back.
		ShowWindow(h.window, SW_RESTORE)
		h.runPosted()
		if w.Minimized().Get() {
			fatalf("restored by the user, it reads minimized")
		}
		// The user moves it.
		fb := FrameBounds(h.window)
		wr := WindowRect(h.window)
		SetWindowPos(h.window, 0, wr.Left+h.px(nil, 10), wr.Top, 0, 0, SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
		if p := w.Position().Get(); !near(p.X, h.pt(nil, fb.Left)+10) {
			fatalf("moved by the user, Position reads %v", p)
		}
	})
}

// The user's closing asks the app first, the main window's and a
// Window's; the Window closes once it says yes.
func TestWindowCloseRequest(t *testing.T) {
	open := signal.New(true)
	allow, asked, mainAsked := false, 0, 0
	h, _ := mount(t, func() ui.Node {
		ui.UseWindow().OnCloseRequest(func() bool { mainAsked++; return false })
		return ui.Window(ui.WindowProps{Open: open,
			OnCloseRequest: func() bool { asked++; return allow },
			Content:        func() ui.Node { return ui.StaticText("w") }})
	})
	onMain(t, func() {
		SendMessage(h.window, WM_CLOSE, 0, 0)
		if mainAsked != 1 || !IsWindow(h.window) || h.quitting {
			fatalf("main: asked %d, window %v", mainAsked, IsWindow(h.window))
		}
		win := h.wins[0].nav.window
		SendMessage(win, WM_CLOSE, 0, 0)
		if asked != 1 || !open.Peek() || !IsWindow(win) {
			fatalf("Window: asked %d, open %v", asked, open.Peek())
		}
		allow = true
		SendMessage(win, WM_CLOSE, 0, 0)
		if asked != 2 || open.Peek() || IsWindow(win) || len(h.wins) != 0 {
			fatalf("allowed: asked %d, open %v, window %v", asked, open.Peek(), IsWindow(win))
		}
	})
}

// A window opened hidden is made and laid out but not shown, until the
// app shows it.
func TestHiddenWindows(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Hidden: true})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	var main, inner *ui.WindowHandle
	h, _ := mount(t, func() ui.Node {
		main = ui.UseWindow()
		return ui.Window(ui.WindowProps{Open: signal.New(true), Hidden: true, Width: 300, Height: 200,
			Content: func() ui.Node {
				inner = ui.UseWindow()
				return ui.Text(signal.Func(func() string { return fmt.Sprint("width ", ui.UseEnvironment().Get().Width) }))
			}})
	})
	onMain(t, func() {
		h.showMain()
		h.runPosted()
		win := h.wins[0].nav.window
		if IsWindowVisible(h.window) || IsWindowVisible(win) || main.Visible().Get() || inner.Visible().Get() {
			fatalf("hidden windows shown")
		}
		if s := firstText(h.wins[0]).text; s != "width 300" {
			fatalf("a hidden window's content says %q", s)
		}
		act(h, inner.Show)
		act(h, main.Show)
		if !IsWindowVisible(win) || !IsWindowVisible(h.window) || !inner.Visible().Get() || !main.Visible().Get() {
			fatalf("Show didn't show them")
		}
	})
}

// A Window with a StateKey opens where the user left it.
func TestWindowStateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), placementName)
	open := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, StateKey: "inspector", Width: 300, Height: 200,
			Content: func() ui.Node { return ui.StaticText("w") }})
	})
	var want Rect
	onMain(t, func() {
		h.placementPath = path
		h.showMain()
		act(h, func() { open.Set(true) })
		c := h.wins[0].nav.ctl
		work := WorkArea(c.hwnd)
		want = Rect{Left: work.Left + 60, Top: work.Top + 50}
		want.Right, want.Bottom = want.Left+h.px(nil, 420), want.Top+h.px(nil, 330)
		h.placementMessage(c, WM_ENTERSIZEMOVE, 0)
		SetWindowPos(c.hwnd, 0, want.Left, want.Top, want.W(), want.H(), SWP_NOZORDER|SWP_NOACTIVATE)
		h.placementMessage(c, WM_EXITSIZEMOVE, 0)
		act(h, func() { open.Set(false) })
		h.states = nil // as the next launch reads it
		act(h, func() { open.Set(true) })
		if got := WindowRect(h.wins[0].nav.window); got != want {
			fatalf("reopened at %v, want %v", got, want)
		}
	})
}

// A modal Window is owned by the window it's declared in, which it
// disables while it's open; a child is owned by its parent Window.
func TestChildAndModalWindows(t *testing.T) {
	modal, outer := signal.New(true), signal.New(true)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Window(ui.WindowProps{Open: modal, Modal: true, Content: func() ui.Node { return ui.StaticText("modal") }}),
			ui.Window(ui.WindowProps{Open: outer, Content: func() ui.Node {
				return ui.Window(ui.WindowProps{Open: signal.New(true), Child: true, Content: func() ui.Node { return ui.StaticText("child") }})
			}}))
	})
	onMain(t, func() {
		if len(h.wins) != 3 {
			fatalf("%d windows", len(h.wins))
		}
		var m, o, c *node
		for _, n := range h.wins {
			switch t := firstText(n); {
			case t == nil:
				o = n // its content is the child Window alone
			case t.text == "modal":
				m = n
			default:
				c = n
			}
		}
		if m == nil || o == nil || c == nil {
			fatalf("modal %v, outer %v, child %v", m != nil, o != nil, c != nil)
		}
		if Owner(m.nav.window) != h.window || IsWindowEnabled(h.window) {
			fatalf("modal: owner %#x, main enabled %v", Owner(m.nav.window), IsWindowEnabled(h.window))
		}
		if Owner(c.nav.window) != o.nav.window {
			fatalf("child: owner %#x, want its parent %#x", Owner(c.nav.window), o.nav.window)
		}
		act(h, func() { modal.Set(false) })
		if !IsWindowEnabled(h.window) {
			fatalf("the main window stayed disabled")
		}
		act(h, func() { outer.Set(false) })
		if len(h.wins) != 0 {
			fatalf("%d windows after the parent closed", len(h.wins))
		}
	})
}

// A hidden title bar: the content gets the whole window, its caption
// buttons over it, its top strip a caption, and the content's environment
// says so (SafeTop).
func TestHiddenTitleBar(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Width: 500, Height: 400, TitleBar: ui.TitleBarHidden})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.MenuBar(signal.Static([]ui.MenuBarMenu{{Title: "File", Items: []ui.MenuItem{{Title: "New", Shortcut: ui.Cmd("n")}}}})),
			ui.Window(ui.WindowProps{Open: signal.New(true), TitleBar: ui.TitleBarHidden, Width: 300, Height: 200,
				Content: func() ui.Node {
					return ui.Text(signal.Func(func() string { return fmt.Sprint("top ", ui.UseEnvironment().Get().SafeTop) }))
				}}))
	})
	onMain(t, func() {
		h.showMain()
		h.runPosted()
		c := h.mainCtl
		if !c.hidden || c.caption == nil || c.topEdge == 0 {
			fatalf("no hidden title bar")
		}
		if h.env.SafeTop != captionHeight {
			fatalf("SafeTop %v", h.env.SafeTop)
		}
		if GetMenu(h.window) != 0 || h.accel == 0 {
			fatalf("menu bar %#x, accelerators %#x: want none, and its shortcuts", GetMenu(h.window), h.accel)
		}
		// The client area is the window's but its borders and Windows 11's
		// pixel at the top.
		wr, cr := WindowRect(h.window), ClientRect(h.window)
		top := ClientToScreen(h.window, Point{}).Y - wr.Top
		if top != hiddenTop() {
			fatalf("the client area starts %d pixels down", top)
		}
		if cr.W() != h.px(nil, 500) || cr.H() != h.px(nil, 400) {
			fatalf("client %dx%d", cr.W(), cr.H())
		}
		// The buttons at the top right, shown.
		b := WindowRectIn(c.caption.hwnd, h.window)
		if !IsWindowVisible(c.caption.hwnd) || b.Right != cr.Right || b.Top != 0 || b.W() != 3*h.px(nil, captionButtonWidth) {
			fatalf("buttons at %v", b)
		}
		// The strip under them, past the content that lets the mouse
		// through, is a caption; below it, the content.
		p := ClientToScreen(h.window, Point{X: cr.W() / 2, Y: h.px(nil, 10)})
		if r := SendMessage(h.window, WM_NCHITTEST, 0, MakeLong(uint16(int16(p.X)), uint16(int16(p.Y)))); r != HTCAPTION {
			fatalf("the strip hit-tests %d", r)
		}
		p = ClientToScreen(h.window, Point{X: cr.W() / 2, Y: h.px(nil, 60)})
		if r := SendMessage(h.window, WM_NCHITTEST, 0, MakeLong(uint16(int16(p.X)), uint16(int16(p.Y)))); r != HTCLIENT {
			fatalf("the content hit-tests %d", r)
		}
		// Over the close button, its code (snap layouts over maximize).
		p = ClientToScreen(c.caption.hwnd, Point{X: b.W() - 2, Y: 2})
		if r := SendMessage(c.caption.hwnd, WM_NCHITTEST, 0, MakeLong(uint16(int16(p.X)), uint16(int16(p.Y)))); r != HTCLOSE {
			fatalf("the close button hit-tests %d", r)
		}
		// A Window's content has the room too.
		if s := firstText(h.wins[0]).text; s != fmt.Sprint("top ", captionHeight) {
			fatalf("the Window's content says %q", s)
		}
		// In full screen, no buttons and no room for them.
		h.enterFullScreen(c)
		if IsWindowVisible(c.caption.hwnd) || h.env.SafeTop != 0 {
			fatalf("full screen: buttons %v, SafeTop %v", IsWindowVisible(c.caption.hwnd), h.env.SafeTop)
		}
		h.exitFullScreen(c)
		if !IsWindowVisible(c.caption.hwnd) || h.env.SafeTop != captionHeight {
			fatalf("out of full screen: buttons %v, SafeTop %v", IsWindowVisible(c.caption.hwnd), h.env.SafeTop)
		}
	})
}
