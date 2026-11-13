//go:build linux && !android

package linux

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/internal/winstate"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// within lets the main loop run until cond holds, for at most d,
// reporting whether it did: for what a window manager does (maximize,
// full screen), which an X server without one never does.
func within(d time.Duration, cond func() bool) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		var ok bool
		onMain(func() { ok = cond() })
		if ok {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// The main window's options, as GTK has them.
func TestWindowOptionsApplied(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Width: 500, Height: 400, MinWidth: 360, DisableResize: true, DisableClose: true,
		DisableMinimize: true, TitleBar: ui.TitleBarHidden, Background: ui.RGB(0x1e, 0x1e, 0x1e)})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	h, _ := mount(t, func() ui.Node { return ui.SafeArea(ui.Style{}, ui.StaticText("x")) })
	onMain(func() {
		w := h.main
		if GetBoolProperty(w.window, "resizable") || GetBoolProperty(w.window, "deletable") {
			fatalf(t, "resizable %v deletable %v", GetBoolProperty(w.window, "resizable"), GetBoolProperty(w.window, "deletable"))
		}
		if l := GetStringProperty(w.header, "decoration-layout"); l == "" || strings.Contains(l, "minimize") || !strings.Contains(l, "close") {
			fatalf(t, "decoration layout %q", l)
		}
		if !GetBoolProperty(w.toolbar, "extend-content-to-top-edge") || GetBoolProperty(w.header, "show-title") {
			fatalf(t, "the title bar isn't hidden")
		}
		var dw, dh int32
		WindowGetDefaultSize(w.window, &dw, &dh)
		if dw != 500 || dh != 400 {
			fatalf(t, "default size %dx%d, want the content's 500x400 (no header above it)", dw, dh)
		}
		if w.css == 0 || w.cssName == "" {
			fatalf(t, "no style of its own")
		}
	})
	// The content goes to the top edge, under the header bar: SafeTop is
	// its height. (Under an X server without a compositor, GTK's frame
	// takes a few pixels of the window's size: solid-csd.)
	if !within(2*time.Second, func() bool { return h.env.SafeTop > 0 && h.main.envH > 400-headerHeight }) {
		onMain(func() { fatalf(t, "SafeTop %v, content %vx%v", h.env.SafeTop, h.main.envW, h.main.envH) })
	}
}

// Actions and the state they report, and a hidden, modal child Window.
func TestWindowActionsAndState(t *testing.T) {
	open := signal.New(true)
	child := ui.NewWindowHandle()
	var main *ui.WindowHandle
	h, r := mount(t, func() ui.Node {
		main = ui.UseWindow()
		return ui.Column(ui.Style{},
			ui.StaticText("main"),
			ui.Window(ui.WindowProps{Open: open, Handle: child, Hidden: true, Modal: true, Width: 300, Height: 200,
				Content: func() ui.Node { ui.UseWindow(); return ui.StaticText("child") }}),
		)
	})
	var cw *win
	onMain(func() {
		ws := find(h, ops.KindWindow)
		if len(ws) != 1 {
			fatalf(t, "%d windows", len(ws))
		}
		cw = ws[0].win
		if WidgetGetVisible(cw.window) {
			fatalf(t, "a hidden window shows")
		}
		if p := Call(Sym("gtk_window_get_transient_for"), cw.window); p != h.main.window {
			fatalf(t, "transient for %x, want the main window", p)
		}
		if !GetBoolProperty(cw.window, "modal") {
			fatalf(t, "not modal")
		}
	})
	// The state reaches the core's signals.
	if !within(2*time.Second, func() bool {
		return main.Visible().Get() && h.main.envW > 0 && main.Size().Get() == ui.Size{Width: h.main.envW, Height: h.main.envH}
	}) {
		onMain(func() {
			var dw, dh int32
			WindowGetDefaultSize(h.main.window, &dw, &dh)
			fatalf(t, "visible %v size %v; content %vx%v, default %dx%d", main.Visible().Get(), main.Size().Get(), h.main.envW, h.main.envH, dw, dh)
		})
	}
	settle(t, h, func() bool { return child.Size().Get() == ui.Size{Width: 300, Height: 200} })
	if child.Visible().Get() {
		t.Fatal("the hidden window reads visible")
	}
	onMain(func() { h.enter(func() { ui.Post(child.Show) }) })
	settle(t, h, func() bool {
		return WidgetGetVisible(cw.window) && child.Visible().Get() && cw.envW > 0 && child.Size().Get() == ui.Size{Width: cw.envW, Height: cw.envH}
	})
	// Sized by the app: its content takes the size (less GTK's frame,
	// here, as it was).
	var before ui.Size
	onMain(func() { before = child.Size().Get() })
	onMain(func() { h.enter(func() { ui.Post(func() { child.SetSize(420, 260) }) }) })
	settle(t, h, func() bool {
		return child.Size().Get() == ui.Size{Width: before.Width + 120, Height: before.Height + 60}
	})
	// Hidden and shown again, the main window too.
	onMain(func() { h.enter(func() { ui.Post(main.Hide) }) })
	settle(t, h, func() bool { return !WidgetGetVisible(h.main.window) && !main.Visible().Get() })
	onMain(func() { h.enter(func() { ui.Post(main.Focus) }) })
	settle(t, h, func() bool { return WidgetGetVisible(h.main.window) && main.Visible().Get() })
	// Maximize and full screen are the window manager's: an X server
	// without one (plain xvfb-run) leaves them be.
	onMain(func() { h.enter(func() { ui.Post(child.Maximize) }) })
	if within(time.Second, func() bool { return child.Maximized().Get() }) {
		onMain(func() { h.enter(func() { ui.Post(child.Restore) }) })
		settle(t, h, func() bool { return !child.Maximized().Get() })
		onMain(func() { h.enter(func() { ui.Post(func() { child.SetFullscreen(true) }) }) })
		settle(t, h, func() bool { return child.Fullscreen().Get() })
	} else {
		t.Log("no window manager maximizes windows here: maximize and full screen not checked")
	}
	_ = r
}

// The user closing a window while the app asks: it stays, until the app
// lets it go.
func TestWindowCloseRequestLinux(t *testing.T) {
	open := signal.New(true)
	allowMain, allowWin := false, false
	askedMain, askedWin := 0, 0
	ui.SetWindowOptions(ui.WindowOptions{OnCloseRequest: func() bool { askedMain++; return allowMain }})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, OnCloseRequest: func() bool { askedWin++; return allowWin },
			Content: func() ui.Node { return ui.StaticText("inspector") }})
	})
	var cw Ptr
	onMain(func() {
		ws := find(h, ops.KindWindow)
		if len(ws) != 1 {
			fatalf(t, "%d windows", len(ws))
		}
		cw = ws[0].win.window
		WindowClose(cw)
	})
	if !within(2*time.Second, func() bool { return askedWin == 1 }) {
		onMain(func() {
			n := find(h, ops.KindWindow)
			fatalf(t, "asked %d, open %v, %d windows, listening %v", askedWin, open.Get(), len(n), len(n) > 0 && n[0].listening(ops.EventCloseRequest))
		})
	}
	onMain(func() {
		if !open.Peek() || h.wins[cw] == nil {
			fatalf(t, "the Window closed though the app said no")
		}
		allowWin = true
		WindowClose(cw)
	})
	settle(t, h, func() bool { return !open.Peek() && h.wins[cw] == nil })
	onMain(func() { WindowClose(h.main.window) })
	settle(t, h, func() bool { return askedMain == 1 })
	onMain(func() {
		if h.main.closing {
			fatalf(t, "the main window closed though the app said no")
		}
		allowMain = true
		WindowClose(h.main.window)
	})
	settle(t, h, func() bool { return h.main.closing })
}

// A Window with a key comes back at the size it was closed at; the main
// window's key is "main".
func TestWindowStateKeyLinux(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asuka-window.json")
	open := signal.New(true)
	h, _ := mountWith(t, func(h *Host) { h.states = winstate.Open(path) }, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, StateKey: "inspector", Width: 300, Height: 200,
			Content: func() ui.Node { return ui.StaticText("inspector") }})
	})
	onMain(func() {
		if h.main.stateKey != winstate.Main {
			fatalf(t, "the main window's key %q", h.main.stateKey)
		}
		cw := find(h, ops.KindWindow)[0].win
		WindowSetDefaultSize(cw.window, 640, 480) // as the user resizing it leaves it
	})
	onMain(func() { h.enter(func() { ui.Post(func() { open.Set(false) }) }) })
	settle(t, h, func() bool { return len(find(h, ops.KindWindow)) == 0 })
	var s windowState
	if !winstate.Open(path).Load("inspector", &s) || s.Width != 640 || s.Height != 480 {
		t.Fatalf("saved %+v", s)
	}
	onMain(func() { h.enter(func() { ui.Post(func() { open.Set(true) }) }) })
	settle(t, h, func() bool { return len(find(h, ops.KindWindow)) == 1 })
	onMain(func() {
		var dw, dh int32
		WindowGetDefaultSize(find(h, ops.KindWindow)[0].win.window, &dw, &dh)
		if dw != 640 || dh != 480 {
			fatalf(t, "reopened at %dx%d, want 640x480", dw, dh)
		}
	})
}

// A main window that starts hidden shows when the app says.
func TestMainWindowHiddenLinux(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Hidden: true})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	var w *ui.WindowHandle
	h, _ := mount(t, func() ui.Node { w = ui.UseWindow(); return ui.StaticText("x") })
	onMain(func() {
		if WidgetGetVisible(h.main.window) || w.Visible().Get() {
			fatalf(t, "a hidden main window shows")
		}
		h.enter(func() { ui.Post(w.Show) })
	})
	settle(t, h, func() bool { return WidgetGetVisible(h.main.window) && w.Visible().Get() && h.main.envW > 0 })
}
