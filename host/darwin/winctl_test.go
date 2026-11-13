//go:build darwin && !ios

package darwin

import (
	"testing"

	"github.com/egoist/asuka/colors"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// post runs f in the core, as an event's handler would, and flushes.
func post(h *Host, f func()) {
	h.enter(func() { ui.Post(f) })
	h.runPosted()
}

func onlyWin(t *testing.T, h *Host) *win {
	t.Helper()
	if len(h.wins) != 1 {
		fatalf(t, "%d windows", len(h.wins))
	}
	for _, x := range h.wins {
		return x
	}
	return nil
}

// A Window's options become its NSWindow's; its handle's actions act on
// it, and the state AppKit reports comes back to the handle's signals.
func TestWindowControl(t *testing.T) {
	open := signal.New(true)
	handle := ui.NewWindowHandle()
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, Handle: handle, Title: ui.Val("Tool"), Width: 300, Height: 200,
			MaxWidth: 600, MaxHeight: 500, Hidden: true, DisableResize: true, DisableMinimize: true, AlwaysOnTop: true,
			SkipTaskbar: true, TitleBar: ui.TitleBarHidden, Background: colors.RGB(255, 0, 0),
			Content: func() ui.Node {
				return ui.Text(signal.Func(func() string { return "top " + itoa(int(ui.UseEnvironment().Get().SafeTop)) }))
			}})
	})
	onMain(func() {
		x := onlyWin(t, h)
		w := x.window
		mask := Send(w, "styleMask")
		if mask&styleResizable != 0 || mask&styleMiniaturizable != 0 || mask&styleClosable == 0 || mask&styleFullSizeContent == 0 {
			fatalf(t, "style mask %b", mask)
		}
		if SendBool(w, "isVisible") {
			fatal(t, "a hidden window shows")
		}
		if SendInt(w, "level") != floatingLevel || !SendBool(w, "isExcludedFromWindowsMenu") || !SendBool(w, "titlebarAppearsTransparent") {
			fatal(t, "not floating, in the Window menu, or its title bar opaque")
		}
		if m := MsgSize(w, Sel("contentMaxSize")); m.Width != 600 || m.Height != 500 {
			fatalf(t, "most %+v", m)
		}
		if b := MsgRect(x.content, Sel("bounds")).Size; b.Width != 300 || b.Height != 200 {
			fatalf(t, "content %+v", b)
		}
		// The content goes under the title bar, and knows how far.
		top := x.safeTop()
		if top < 20 {
			fatalf(t, "a hidden title bar's room is %v", top)
		}
		if s := text(firstText(x.root)); s != "top "+itoa(int(top)) {
			fatalf(t, "the content says %q", s)
		}
		// The core listens: it got the state at once.
		if !x.root.listening(ops.EventWindowState) || !x.stateSent {
			fatal(t, "no state reported")
		}
		if handle.Visible().Get() || handle.Size().Get() != (ui.Size{Width: 300, Height: 200}) || !handle.AlwaysOnTop().Get() {
			fatalf(t, "state: visible %v size %v on top %v", handle.Visible().Get(), handle.Size().Get(), handle.AlwaysOnTop().Get())
		}
		post(h, func() {
			handle.Show()
			handle.SetSize(320, 240)
			handle.SetPosition(100, 120)
			handle.SetAlwaysOnTop(false)
		})
		if !SendBool(w, "isVisible") || SendInt(w, "level") != 0 {
			fatal(t, "Show or SetAlwaysOnTop didn't act")
		}
		if b := MsgRect(x.content, Sel("bounds")).Size; b.Width != 320 || b.Height != 240 {
			fatalf(t, "content after SetSize %+v", b)
		}
		f := MsgRect(w, Sel("frame"))
		if f.Origin.X != 100 || mainScreenHeight()-f.Origin.Y-f.Size.Height != 120 {
			fatalf(t, "frame after SetPosition %+v", f)
		}
		if !handle.Visible().Get() || handle.AlwaysOnTop().Get() || handle.Size().Get() != (ui.Size{Width: 320, Height: 240}) ||
			handle.Position().Get() != (ui.Point{X: 100, Y: 120}) {
			fatalf(t, "state after: visible %v on top %v size %v position %v", handle.Visible().Get(),
				handle.AlwaysOnTop().Get(), handle.Size().Get(), handle.Position().Get())
		}
		// The user moves it: reported.
		h.setTopLeft(w, 140, 160)
		h.enter(func() {})
		if handle.Position().Get() != (ui.Point{X: 140, Y: 160}) {
			fatalf(t, "the user's move read %v", handle.Position().Get())
		}
		post(h, handle.Hide)
		if SendBool(w, "isVisible") || handle.Visible().Get() {
			fatal(t, "Hide didn't hide it")
		}
	})
}

// The user closing a window the app asks about: it stays, the app is
// asked, and the core closes it once the app agrees.
func TestWindowCloseRequestDarwin(t *testing.T) {
	open := signal.New(true)
	allowMain, allowWin := false, false
	asked := 0
	ui.SetWindowOptions(ui.WindowOptions{OnCloseRequest: func() bool { asked++; return allowMain }})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, OnCloseRequest: func() bool { asked++; return allowWin },
			Content: func() ui.Node { return ui.StaticText("w") }})
	})
	onMain(func() {
		x := onlyWin(t, h)
		if SendBool(h.target, "windowShouldClose:", uintptr(x.window)) || asked != 1 || !open.Peek() {
			fatalf(t, "a refused close: asked %d open %v", asked, open.Peek())
		}
		allowWin = true
		SendBool(h.target, "windowShouldClose:", uintptr(x.window))
		if open.Peek() || len(h.wins) != 0 {
			fatal(t, "an allowed close left the window open")
		}
		if SendBool(h.target, "windowShouldClose:", uintptr(h.window)) || asked != 3 {
			fatalf(t, "the main window's refused close: asked %d", asked)
		}
	})
}

// A modal Window is a sheet of the window it's in, a child one a child
// window of it; the main window's options: a hidden title bar's room in
// its environment.
func TestChildWindowsDarwin(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{TitleBar: ui.TitleBarHidden, Width: 500, Height: 400})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	modal, child := signal.New(false), signal.New(false)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Window(ui.WindowProps{Open: modal, Modal: true, Width: 200, Height: 100, Content: func() ui.Node { return ui.StaticText("m") }}),
			ui.Window(ui.WindowProps{Open: child, Child: true, Content: func() ui.Node { return ui.StaticText("c") }}),
		)
	})
	onMain(func() {
		if top := r.Environment().SafeTop; top < 20 || h.main.cfg.TitleBar != ops.TitleBarHidden {
			fatalf(t, "the main window's SafeTop %v", top)
		}
		Send(h.window, "orderFront:", 0)
		post(h, func() { modal.Set(true) })
		x := onlyWin(t, h)
		if Send(x.window, "sheetParent") != h.window {
			fatal(t, "the modal window isn't a sheet of the main one")
		}
		post(h, func() { modal.Set(false) })
		if Send(h.window, "attachedSheet") != 0 || len(h.wins) != 0 {
			fatal(t, "the sheet stayed")
		}
		post(h, func() { child.Set(true) })
		x = onlyWin(t, h)
		if Send(x.window, "parentWindow") != h.window {
			fatal(t, "the child isn't the main window's child window")
		}
		post(h, func() { child.Set(false) })
		if n := len(ArrayItems(Send(h.window, "childWindows"))); n != 0 {
			fatalf(t, "%d child windows left", n)
		}
		Send(h.window, "orderOut:", 0)
	})
}

// The main window's handle: Hide and Focus, as a tray icon's menu does.
func TestMainWindowHideFocus(t *testing.T) {
	var w *ui.WindowHandle
	h, _ := mount(t, func() ui.Node {
		w = ui.UseWindow()
		return ui.StaticText("main")
	})
	onMain(func() {
		post(h, w.Show)
		if !w.Visible().Get() {
			fatal(t, "the shown main window reads hidden")
		}
		post(h, w.Hide)
		if SendBool(h.window, "isVisible") || w.Visible().Get() {
			fatal(t, "Hide didn't hide the main window")
		}
		post(h, w.Focus)
		if !SendBool(h.window, "isVisible") || !w.Visible().Get() {
			fatal(t, "Focus didn't bring it back")
		}
		Send(h.window, "orderOut:", 0)
	})
}
