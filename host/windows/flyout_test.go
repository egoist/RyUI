//go:build windows

package windows

import (
	"math"
	"testing"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/flyout"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/sdk/tray"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// pxRect is the window rectangle flyout computed, as setRect rounds it.
func pxRect(r flyout.Rect) Rect {
	x, y := int32(math.Round(r.X)), int32(math.Round(r.Y))
	return Rect{Left: x, Top: y, Right: x + int32(math.Round(r.W)), Bottom: y + int32(math.Round(r.H))}
}

// escape is an Escape pressed in hwnd, as the message loop hands it to
// the host.
func escape(h *Host, hwnd uintptr) bool {
	m := Msg{Hwnd: hwnd, Message: WM_KEYDOWN, WParam: VK_ESCAPE}
	return h.keyMessage(&m)
}

// drain runs the messages the thread has now: what the host posted.
func drain() {
	var msg Msg
	for PeekMessage(&msg) {
		TranslateMessage(&msg)
		DispatchMessage(&msg)
	}
}

// A main window that's a panel: a frameless topmost tool window owned by
// the hidden window (no taskbar button), with no menu bar; it shows
// centered on the cursor's monitor a third of the way down, each time
// unless the app placed it, and closes (hides, in an app that keeps
// running) on Escape and as another window of the app's takes the
// keyboard.
func TestPanel(t *testing.T) {
	boot.KeepRunning = true
	defer func() { boot.KeepRunning = false }()
	ui.SetWindowOptions(ui.WindowOptions{Panel: true, Width: 400, Height: 200})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	other := signal.New(false)
	var w *ui.WindowHandle
	h, _ := mount(t, func() ui.Node {
		w = ui.UseWindow()
		return ui.Column(ui.Style{},
			ui.MenuBar(signal.Static([]ui.MenuBarMenu{{Title: "File", Items: []ui.MenuItem{{Title: "New", Shortcut: ui.Cmd("n")}}}})),
			ui.Window(ui.WindowProps{Open: other, Width: 300, Height: 200, Content: func() ui.Node { return ui.StaticText("other") }}))
	})
	onMain(t, func() {
		c := h.mainCtl
		style, ex := WindowLong(h.window, GWL_STYLE), WindowLong(h.window, GWL_EXSTYLE)
		if style&WS_POPUP == 0 || style&(WS_CAPTION|WS_THICKFRAME) != 0 {
			fatalf("style %#x: not frameless", style)
		}
		if ex&WS_EX_TOOLWINDOW == 0 || ex&WS_EX_TOPMOST == 0 {
			fatalf("exstyle %#x: not a topmost tool window", ex)
		}
		if Owner(h.window) != h.park {
			fatalf("owner %#x: not the hidden window, it may get a taskbar button", Owner(h.window))
		}
		if GetMenu(h.window) != 0 {
			fatalf("a panel with a menu bar")
		}
		centered := func() Rect {
			_, work, _ := MonitorAt(CursorPos())
			k := c.k(h)
			return pxRect(flyout.Center(float64(round(400*k)), float64(round(200*k)), rectF(work)))
		}
		h.showMain()
		h.runPosted()
		if !IsWindowVisible(h.window) || !w.Visible().Get() {
			fatalf("not shown")
		}
		if got, want := WindowRect(h.window), centered(); got != want {
			fatalf("shown at %v, want %v", got, want)
		}
		// Escape closes it: hidden, the app keeping running.
		if !escape(h, h.window) || IsWindowVisible(h.window) || h.quitting {
			fatalf("Escape: visible %v, quitting %v", IsWindowVisible(h.window), h.quitting)
		}
		h.runPosted()
		if w.Visible().Get() {
			fatalf("closed, it reads visible")
		}
		// Shown again, centered again.
		SetWindowPos(h.window, 0, 0, 0, 0, 0, SWP_NOSIZE|SWP_NOZORDER|SWP_NOACTIVATE)
		act(h, w.Show)
		if got, want := WindowRect(h.window), centered(); !IsWindowVisible(h.window) || got != want {
			fatalf("shown again at %v, want %v", got, want)
		}
		// Placed by the app: it stays there.
		act(h, func() { w.SetPosition(40, 50) })
		act(h, w.Hide)
		act(h, w.Show)
		if fb := FrameBounds(h.window); fb.Left != round(40*c.k(h)) || fb.Top != round(50*c.k(h)) {
			fatalf("placed by the app, shown at %d, %d", fb.Left, fb.Top)
		}
		// Another window of the app's takes the keyboard: it closes, once
		// the activation settled.
		act(h, func() { other.Set(true) })
		win := h.wins[0].nav.window
		SendMessage(h.window, WM_ACTIVATE, WA_INACTIVE, win)
		if !pumpFor(func() bool { return !IsWindowVisible(h.window) }) {
			fatalf("still shown after another window took the keyboard")
		}
	})
}

// A Window that's a panel: a topmost tool window owned by the hidden
// window, the size of its content.
func TestPanelWindowFitsContent(t *testing.T) {
	open := signal.New(false)
	rows := signal.New(2)
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, Panel: true, FitContent: true, Width: 600, Height: 400,
			Content: func() ui.Node {
				return ui.Column(ui.StyleFunc(func() ui.Style {
					return ui.Style{Width: ui.Pt(500), Height: ui.Pt(float32(40 * rows.Get()))}
				}))
			}})
	})
	onMain(t, func() {
		h.showMain()
		act(h, func() { open.Set(true) })
		if len(h.wins) != 1 {
			fatalf("%d windows", len(h.wins))
		}
		c := h.wins[0].nav.ctl
		ex := WindowLong(c.hwnd, GWL_EXSTYLE)
		if ex&WS_EX_TOOLWINDOW == 0 || ex&WS_EX_TOPMOST == 0 || Owner(c.hwnd) != h.park {
			fatalf("exstyle %#x, owner %#x", ex, Owner(c.hwnd))
		}
		k := c.k(h)
		if cr := ClientRect(c.hwnd); !shown(c.hwnd) || cr.W() != round(500*k) || cr.H() != round(80*k) {
			fatalf("shown %v at %dx%d, want its content's 500x80 points", shown(c.hwnd), cr.W(), cr.H())
		}
		// Its content grows: it grows down, its top left kept.
		before := WindowRect(c.hwnd)
		act(h, func() { rows.Set(5) })
		got := WindowRect(c.hwnd)
		if got.Left != before.Left || got.Top != before.Top || ClientRect(c.hwnd).H() != round(200*k) {
			fatalf("grown to %v from %v", got, before)
		}
		// Escape closes it, as the user closing it.
		if !escape(h, c.hwnd) || open.Peek() || len(h.wins) != 0 {
			fatalf("Escape: open %v, %d windows", open.Peek(), len(h.wins))
		}
	})
}

// A flyout hanging from a rectangle of the main window: owned by it, a
// tool window, placed against the rectangle at its content's size, placed
// again as either changes; it waits for its size to show, and closes on
// Escape and as its parent takes the keyboard back.
func TestFlyoutFromRectangle(t *testing.T) {
	open := signal.New(false)
	rows := signal.New(2)
	anchor := signal.New(ui.Anchor{X: 20, Y: 40, Width: 120, Height: 28})
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, FitContent: true,
			Flyout: &ui.Flyout{Anchor: anchor, Placement: ui.PlacementBottomStart, Gap: 4},
			Content: func() ui.Node {
				return ui.Column(ui.StyleFunc(func() ui.Style {
					return ui.Style{Width: ui.Pt(200), Height: ui.Pt(float32(30 * rows.Get()))}
				}))
			}})
	})
	onMain(t, func() {
		h.showMain()
		h.runPosted()
		act(h, func() { open.Set(true) })
		if len(h.wins) != 1 {
			fatalf("%d windows", len(h.wins))
		}
		c := h.wins[0].nav.ctl
		style, ex := WindowLong(c.hwnd, GWL_STYLE), WindowLong(c.hwnd, GWL_EXSTYLE)
		if style&WS_POPUP == 0 || style&WS_CAPTION != 0 || ex&WS_EX_TOOLWINDOW == 0 {
			fatalf("style %#x, exstyle %#x", style, ex)
		}
		if Owner(c.hwnd) != h.window || c.owner != h.window {
			fatalf("owner %#x, want the main window", Owner(c.hwnd))
		}
		if !shown(c.hwnd) || c.waitFit || !c.fitted || c.fit != [2]float32{200, 60} {
			fatalf("shown %v, waiting %v, fit %v", shown(c.hwnd), c.waitFit, c.fit)
		}
		placed := func() Rect {
			a := anchor.Peek()
			o := WindowRect(h.root.hwnd)
			ar := Rect{Left: o.Left + round(a.X*h.scale), Top: o.Top + round(a.Y*h.scale)}
			ar.Right, ar.Bottom = ar.Left+round(a.Width*h.scale), ar.Top+round(a.Height*h.scale)
			work, _ := MonitorOfRect(ar)
			k := c.k(h)
			return pxRect(flyout.Place(rectF(ar), float64(round(c.fit[0]*k)), float64(round(c.fit[1]*k)),
				ops.PlacementBottomStart, float64(4*k), rectF(work)))
		}
		if got, want := WindowRect(c.hwnd), placed(); got != want {
			fatalf("at %v, want %v", got, want)
		}
		// Its content grows: placed again, at the new size.
		act(h, func() { rows.Set(4) })
		if got, want := WindowRect(c.hwnd), placed(); c.fit != [2]float32{200, 120} || got != want {
			fatalf("grown: fit %v at %v, want %v", c.fit, got, want)
		}
		// Its anchor moves: placed again.
		act(h, func() { anchor.Set(ui.Anchor{X: 60, Y: 10, Width: 40, Height: 20}) })
		if got, want := WindowRect(c.hwnd), placed(); got != want {
			fatalf("anchor moved: at %v, want %v", got, want)
		}
		// A click on its parent while it has the keyboard only closes it.
		if c.ownsActive() {
			if r := SendMessage(h.window, WM_MOUSEACTIVATE, h.window, MakeLong(HTCLIENT, WM_LBUTTONDOWN)); r != MA_ACTIVATEANDEAT {
				fatalf("a click on the parent went on: %d", r)
			}
		}
		// Escape closes it.
		if !escape(h, c.hwnd) || open.Peek() || len(h.wins) != 0 {
			fatalf("Escape: open %v, %d windows", open.Peek(), len(h.wins))
		}
		// Opened again, its parent takes the keyboard back: it closes.
		act(h, func() { open.Set(true) })
		c = h.wins[0].nav.ctl
		SetForegroundWindow(h.window)
		SendMessage(c.hwnd, WM_ACTIVATE, WA_INACTIVE, h.window)
		if !pumpFor(func() bool { return !open.Peek() }) {
			fatalf("still open after its parent took the keyboard")
		}
	})
}

// A modal Window declared in a flyout is owned by it, centered over it,
// and keeps it open as it takes the keyboard; the flyout is enabled
// again as it closes.
func TestFlyoutKeptByItsDialog(t *testing.T) {
	open, confirm := signal.New(false), signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, FitContent: true,
			Flyout: &ui.Flyout{Anchor: ui.Val(ui.Anchor{X: 100, Y: 100})},
			Content: func() ui.Node {
				return ui.Column(ui.Style{Width: ui.Pt(240), Height: ui.Pt(160)},
					ui.Window(ui.WindowProps{Open: confirm, Modal: true, Width: 200, Height: 100,
						Content: func() ui.Node { return ui.StaticText("Sure?") }}))
			}})
	})
	onMain(t, func() {
		h.showMain()
		act(h, func() { open.Set(true) })
		var fl, dlg *wctl
		for _, n := range h.wins {
			if n.nav.ctl.has(ops.WindowFlyout) {
				fl = n.nav.ctl
			}
		}
		act(h, func() { confirm.Set(true) })
		for _, n := range h.wins {
			if n.nav.ctl.has(ops.WindowModal) {
				dlg = n.nav.ctl
			}
		}
		if fl == nil || dlg == nil {
			fatalf("flyout %v, dialog %v", fl != nil, dlg != nil)
		}
		if Owner(dlg.hwnd) != fl.hwnd || IsWindowEnabled(fl.hwnd) {
			fatalf("dialog owner %#x, want the flyout %#x; flyout enabled %v", Owner(dlg.hwnd), fl.hwnd, IsWindowEnabled(fl.hwnd))
		}
		o, d := WindowRect(fl.hwnd), WindowRect(dlg.hwnd)
		work := MonitorWorkArea(o)
		if cx := o.Left + (o.W()-d.W())/2; d.Left != min(max(cx, work.Left), max(work.Right-d.W(), work.Left)) {
			fatalf("dialog at %v, flyout at %v: not centered over it", d, o)
		}
		// It takes the keyboard: the flyout stays.
		SendMessage(fl.hwnd, WM_ACTIVATE, WA_INACTIVE, dlg.hwnd)
		h.windowActivated(dlg.hwnd)
		drain() // the posted messages run
		if fl.dismissPending || !open.Peek() {
			fatalf("the flyout closed under its dialog")
		}
		act(h, func() { confirm.Set(false) })
		if !IsWindowEnabled(fl.hwnd) || !open.Peek() {
			fatalf("after the dialog: enabled %v, open %v", IsWindowEnabled(fl.hwnd), open.Peek())
		}
	})
}

// A flyout hanging from a tray icon: a topmost tool window owned by the
// hidden window, against the icon's rectangle (above it in a taskbar at
// the bottom), toggled by the icon's click.
func TestTrayFlyout(t *testing.T) {
	open := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		icon := tray.Show(tray.Props{Symbol: ui.Val("bolt.fill"), OnClick: func() { open.Set(!open.Peek()) }})
		return ui.Window(ui.WindowProps{Open: open, FitContent: true,
			Flyout:  &ui.Flyout{Anchor: icon.Anchor(), Placement: ui.PlacementBottom, Gap: 8},
			Content: func() ui.Node { return ui.Column(ui.Style{Width: ui.Pt(300), Height: ui.Pt(200)}) }})
	})
	onMain(t, func() {
		h.showMain()
		s := h.tray
		if s == nil || len(s.order) != 1 {
			fatalf("no tray icon")
		}
		id := s.order[0]
		click := func() {
			SendMessage(s.hwnd, trayMessage, 0, MakeLong(NIN_SELECT, uint16(id)))
			h.runPosted()
		}
		click()
		if !open.Peek() || len(h.wins) != 1 {
			fatalf("the click didn't open it")
		}
		c := h.wins[0].nav.ctl
		ex := WindowLong(c.hwnd, GWL_EXSTYLE)
		if ex&WS_EX_TOOLWINDOW == 0 || ex&WS_EX_TOPMOST == 0 || Owner(c.hwnd) != h.park || c.owner != 0 {
			fatalf("exstyle %#x, owner %#x", ex, Owner(c.hwnd))
		}
		icon, known := NotifyIconRect(s.hwnd, id)
		if !known {
			p := CursorPos()
			icon = Rect{Left: p.X, Top: p.Y, Right: p.X, Bottom: p.Y}
		}
		work, _ := MonitorOfRect(icon)
		k := c.k(h)
		want := pxRect(flyout.Place(rectF(icon), float64(round(300*k)), float64(round(200*k)), ops.PlacementBottom, float64(8*k), rectF(work)))
		got := WindowRect(c.hwnd)
		if got != want {
			fatalf("at %v, want %v (icon %v, known %v)", got, want, icon, known)
		}
		if known && icon.Top >= work.Bottom && got.Bottom > icon.Top {
			fatalf("at %v: not above its icon %v in a taskbar at the bottom", got, icon)
		}
		// The icon's click again toggles it.
		click()
		if open.Peek() || len(h.wins) != 0 {
			fatalf("the second click left it open")
		}
	})
}

// A click-through window is layered and transparent to the mouse, as its
// state says; the handle switches it.
func TestClickThrough(t *testing.T) {
	w := ui.NewWindowHandle()
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: signal.New(true), Handle: w, ClickThrough: true, AlwaysOnTop: true, Width: 200, Height: 100,
			Content: func() ui.Node { return ui.StaticText("pet") }})
	})
	onMain(t, func() {
		h.showMain()
		h.runPosted()
		c := h.wins[0].nav.ctl
		through := func() bool {
			return WindowLong(c.hwnd, GWL_EXSTYLE)&(WS_EX_LAYERED|WS_EX_TRANSPARENT) == WS_EX_LAYERED|WS_EX_TRANSPARENT
		}
		if !through() || !w.ClickThrough().Get() || h.windowState(c).Flags&ops.WindowClickThroughState == 0 {
			fatalf("opened click-through: exstyle %#x, reads %v", WindowLong(c.hwnd, GWL_EXSTYLE), w.ClickThrough().Get())
		}
		act(h, func() { w.SetClickThrough(false) })
		if WindowLong(c.hwnd, GWL_EXSTYLE)&(WS_EX_LAYERED|WS_EX_TRANSPARENT) != 0 || w.ClickThrough().Get() {
			fatalf("not click-through: exstyle %#x, reads %v", WindowLong(c.hwnd, GWL_EXSTYLE), w.ClickThrough().Get())
		}
		act(h, func() { w.SetClickThrough(true) })
		if !through() || !w.ClickThrough().Get() {
			fatalf("click-through again: exstyle %#x", WindowLong(c.hwnd, GWL_EXSTYLE))
		}
	})
}

// A main window sized to its content shows at that size, never at its
// options' first, and follows it.
func TestMainWindowFitsContent(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{FitContent: true, Width: 800, Height: 600})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	wide := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.StyleFunc(func() ui.Style {
			w := float32(240)
			if wide.Get() {
				w = 360
			}
			return ui.Style{Width: ui.Pt(w), Height: ui.Pt(120)}
		}))
	})
	onMain(t, func() {
		c := h.mainCtl
		if !c.fitted || c.waitFit || c.fit != [2]float32{240, 120} {
			fatalf("fit %v, waiting %v", c.fit, c.waitFit)
		}
		h.showMain()
		k := c.k(h)
		if cr := ClientRect(h.window); cr.W() != round(240*k) || cr.H() != round(120*k) {
			fatalf("shown at %dx%d", cr.W(), cr.H())
		}
		before := WindowRect(h.window)
		act(h, func() { wide.Set(true) })
		if cr, wr := ClientRect(h.window), WindowRect(h.window); cr.W() != round(360*k) || wr.Left != before.Left || wr.Top != before.Top {
			fatalf("wider: %dx%d at %v, was at %v", cr.W(), cr.H(), wr, before)
		}
	})
}
