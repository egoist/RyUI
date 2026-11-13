package ui_test

import (
	"testing"

	"github.com/egoist/asuka/colors"
	"github.com/egoist/asuka/host/fake"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// The main window's handle: its actions go to the root as WindowAction
// commands, and its state follows what the host reports, the user's
// changes too, as signals.
func TestMainWindowHandle(t *testing.T) {
	var w *ui.WindowHandle
	runs := 0
	h, r := mount(t, func() ui.Node {
		w = ui.UseWindow()
		return ui.Text(signal.Func(func() string {
			runs++
			if w.Focused().Get() {
				return "focused"
			}
			return "not focused"
		}))
	})
	root := h.Root()
	if !root.Listens[ops.EventWindowState] {
		t.Fatal("not listening for the window's state")
	}
	if root.Listens[ops.EventCloseRequest] {
		t.Fatal("listening for close requests nobody asks about")
	}
	if ui.MainWindow() == nil {
		t.Fatal("no MainWindow")
	}
	h.RunPosted() // the state reported as the core started listening
	if w.Size().Get() != (ui.Size{Width: 390, Height: 844}) || !w.Visible().Get() {
		t.Fatalf("state %+v visible %v", w.Size().Get(), w.Visible().Get())
	}
	w.Minimize()
	w.SetSize(640, 480)
	w.SetPosition(10, 20)
	w.SetAlwaysOnTop(true)
	r.Flush()
	if got := h.Window(root).Actions; len(got) != 4 || got[0] != ops.WindowActionMinimize || got[3] != ops.WindowActionAlwaysOnTop {
		t.Fatalf("actions %v", got)
	}
	h.RunPosted()
	if !w.Minimized().Get() || w.Focused().Get() || w.Size().Get() != (ui.Size{Width: 640, Height: 480}) ||
		w.Position().Get() != (ui.Point{X: 10, Y: 20}) || !w.AlwaysOnTop().Get() {
		t.Fatalf("after the actions: minimized %v focused %v size %v position %v on top %v",
			w.Minimized().Get(), w.Focused().Get(), w.Size().Get(), w.Position().Get(), w.AlwaysOnTop().Get())
	}
	if s := h.OfKind(ops.KindText)[0].Text(); s != "not focused" {
		t.Fatalf("the text says %q", s)
	}
	// The user brings it back and maximizes it.
	before := runs
	h.SetWindowState(root, ops.WindowState{Width: 1200, Height: 800, Flags: ops.WindowVisible | ops.WindowFocused | ops.WindowMaximizedState})
	if !w.Maximized().Get() || w.Minimized().Get() || h.OfKind(ops.KindText)[0].Text() != "focused" || runs != before+1 {
		t.Fatalf("after the user's change: maximized %v, text %q, %d runs", w.Maximized().Get(), h.OfKind(ops.KindText)[0].Text(), runs-before)
	}
	// The same state again changes nothing.
	h.SetWindowState(root, ops.WindowState{Width: 1200, Height: 800, Flags: ops.WindowVisible | ops.WindowFocused | ops.WindowMaximizedState})
	if runs != before+1 {
		t.Fatal("an unchanged state re-ran the text")
	}
	w.Restore()
	w.SetFullscreen(true)
	w.Hide()
	w.Show()
	w.Focus()
	w.Center()
	r.Flush()
	acts := h.Window(root).Actions[4:]
	want := []ops.WindowAction{ops.WindowActionRestore, ops.WindowActionFullscreen, ops.WindowActionHide, ops.WindowActionShow, ops.WindowActionFocus, ops.WindowActionCenter}
	if len(acts) != len(want) {
		t.Fatalf("actions %v", acts)
	}
	for i := range want {
		if acts[i] != want[i] {
			t.Fatalf("actions %v, want %v", acts, want)
		}
	}
}

// A phone has no windows: the handle sends nothing and listens for
// nothing.
func TestWindowHandleMobile(t *testing.T) {
	h := fake.New()
	h.Env.Platform = ops.PlatformIOS
	var w *ui.WindowHandle
	r := ui.Mount(h, func() ui.Node {
		w = ui.UseWindow()
		return ui.StaticText("x")
	})
	t.Cleanup(r.Dispose)
	w.Maximize()
	w.OnCloseRequest(func() bool { return false })
	r.Flush()
	if len(h.Commands(ops.CmdWindowAction)) != 0 || len(h.Root().Listens) != 0 {
		t.Fatalf("on a phone: %d actions, listens %v", len(h.Commands(ops.CmdWindowAction)), h.Root().Listens)
	}
	if !w.Visible().Get() || !w.Focused().Get() {
		t.Fatal("a phone's app isn't visible and focused")
	}
}

// Asking before the main window closes: the host asks the core
// (CloseRequest), which closes it (WindowAction Close: the app quits)
// once every function asked agrees.
func TestMainWindowCloseRequest(t *testing.T) {
	dirty := signal.New(true)
	asked, optionAsked := 0, 0
	ui.SetWindowOptions(ui.WindowOptions{OnCloseRequest: func() bool { optionAsked++; return true }})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	editor := signal.New(true)
	var w *ui.WindowHandle
	h, r := mount(t, func() ui.Node {
		w = ui.UseWindow()
		return ui.Show(editor, func() ui.Node {
			ui.UseWindow().OnCloseRequest(func() bool { asked++; return !dirty.Peek() })
			return ui.StaticText("editor")
		})
	})
	root := h.Root()
	if !root.Listens[ops.EventCloseRequest] {
		t.Fatal("not listening for close requests")
	}
	h.CloseWindow(root)
	if asked != 1 || optionAsked != 1 || h.Window(root).Closed {
		t.Fatalf("asked %d, %d; closed %v", asked, optionAsked, h.Window(root).Closed)
	}
	// Saved: it closes.
	dirty.Set(false)
	h.CloseWindow(root)
	if !h.Window(root).Closed {
		t.Fatal("the window didn't close once allowed")
	}
	// The editor gone, its question goes; the options' stays.
	editor.Set(false)
	r.Flush()
	asked = 0
	w.Close()
	r.Flush()
	if asked != 0 || optionAsked != 3 {
		t.Fatalf("after the editor left: asked %d, %d", asked, optionAsked)
	}
}

// A Window's close request; Destroy closes it without asking; its handle
// from outside opens and controls it.
func TestWindowCloseRequestAndHandle(t *testing.T) {
	open := signal.New(false)
	handle := ui.NewWindowHandle()
	allow, closed, asked := false, 0, 0
	var inner *ui.WindowHandle
	h, r := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, Handle: handle, Hidden: true, StateKey: "inspector", DisableResize: true,
			OnClose:        func() { closed++ },
			OnCloseRequest: func() bool { asked++; return allow },
			Content: func() ui.Node {
				inner = ui.UseWindow()
				return ui.StaticText("inspector")
			}})
	})
	// Focus on the closed window opens it.
	handle.Focus()
	r.Flush()
	ps := h.Presented()
	if !open.Peek() || len(ps) != 1 || inner != handle {
		t.Fatalf("open %v, %d presented, same handle %v", open.Peek(), len(ps), inner == handle)
	}
	win := ps[0]
	if !win.Listens[ops.EventWindowState] {
		t.Fatal("a Handle's window doesn't report its state")
	}
	c := h.Window(win).Config
	if c.Flags != ops.WindowHidden|ops.WindowFixedSize || c.StateKey != "inspector" {
		t.Fatalf("config %+v", c)
	}
	h.RunPosted()
	if handle.Visible().Get() {
		t.Fatal("a hidden window reads visible")
	}
	handle.Show()
	r.Flush()
	h.RunPosted()
	if !handle.Visible().Get() {
		t.Fatal("Show didn't show it")
	}
	// The user closes it: asked, refused.
	h.CloseWindow(win)
	if asked != 1 || !open.Peek() || closed != 0 {
		t.Fatalf("asked %d open %v closed %d", asked, open.Peek(), closed)
	}
	// Destroy closes it without asking.
	handle.Destroy()
	r.Flush()
	if asked != 1 || open.Peek() || closed != 1 || len(h.OfKind(ops.KindWindow)) != 0 {
		t.Fatalf("after Destroy: asked %d open %v closed %d", asked, open.Peek(), closed)
	}
	if handle.Visible().Get() {
		t.Fatal("a closed window reads visible")
	}
	// Closed, its actions do nothing.
	handle.Maximize()
	r.Flush()
	if n := len(h.Commands(ops.CmdWindowAction)); n != 1 {
		t.Fatalf("%d WindowActions", n)
	}
	// Opened again and allowed, Close closes it.
	open.Set(true)
	r.Flush()
	allow = true
	handle.Close()
	r.Flush()
	if open.Peek() || closed != 2 {
		t.Fatalf("Close: open %v closed %d", open.Peek(), closed)
	}
}

// A Window declared in another's content is its child: its parent the
// other's node; one in the root's, the main window's.
func TestChildWindow(t *testing.T) {
	outer, inner, modal := signal.New(true), signal.New(true), signal.New(true)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Window(ui.WindowProps{Open: modal, Modal: true, Content: func() ui.Node { return ui.StaticText("modal") }}),
			ui.Window(ui.WindowProps{Open: outer, Content: func() ui.Node {
				return ui.Window(ui.WindowProps{Open: inner, Child: true, Content: func() ui.Node { return ui.StaticText("child") }})
			}}),
		)
	})
	ps := h.Presented()
	if len(ps) != 3 {
		t.Fatalf("%d windows", len(ps))
	}
	m, o, c := h.Window(ps[0]).Config, h.Window(ps[1]).Config, h.Window(ps[2]).Config
	if m.Parent != h.Root().ID || m.Flags != ops.WindowChild|ops.WindowModal {
		t.Fatalf("modal %+v", m)
	}
	if o.Parent != 0 || o.Flags != 0 {
		t.Fatalf("outer %+v", o)
	}
	if c.Parent != ps[1].ID || c.Flags != ops.WindowChild {
		t.Fatalf("child %+v", c)
	}
	// A parent opens before the child declared in it, so the host knows
	// it by the time the child names it.
	order := map[ops.NodeID]int{}
	for i, op := range h.Commands(ops.CmdPresent) {
		order[op.ID] = i
	}
	if order[ps[1].ID] > order[ps[2].ID] {
		t.Fatal("the child opened before its parent")
	}
	// The parent closes: the child goes with it.
	outer.Set(false)
	h.CloseWindow(ps[1]) // already closing; no-op for the core
	if n := len(h.OfKind(ops.KindWindow)); n != 1 {
		t.Fatalf("%d windows after the parent closed", n)
	}
}

// The main window's options, all of them, as the host gets them.
func TestWindowOptionsAll(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Width: 900, MaxWidth: 1600, X: 40, Y: 50, DisableMaximize: true, AlwaysOnTop: true,
		TitleBar: ui.TitleBarHidden, Background: colors.RGB(0x1e, 0x1e, 0x1e), Hidden: true, StateKey: "editor"})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	var w *ui.WindowHandle
	h, _ := mount(t, func() ui.Node { w = ui.UseWindow(); return ui.StaticText("x") })
	c := h.Window(h.Root()).Config
	want := ops.WindowConfig{Width: 900, MaxWidth: 1600, X: 40, Y: 50, TitleBar: ops.TitleBarHidden, Background: colors.RGB(0x1e, 0x1e, 0x1e),
		Flags: ops.WindowNoMaximize | ops.WindowAlwaysOnTop | ops.WindowHidden | ops.WindowPositioned, StateKey: "editor"}
	if c != want {
		t.Fatalf("config %+v\nwant %+v", c, want)
	}
	if w.Visible().Get() {
		t.Fatal("a hidden main window reads visible")
	}
}
