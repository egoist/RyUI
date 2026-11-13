package ui

import (
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
)

// WindowHandle controls a window, the main one or a Window's, and reads
// its state as signals that follow the user's changes as well as the
// app's: read them in an effect or a StyleFunc. Sizes are its content's,
// in points; positions its frame's top left on the screen, in points
// from the main display's top left.
//
// Its methods act on the window's next flush (an event's handler's, a
// Post's), as signal writes do; on a closed Window they do nothing, but
// Show and Focus, which open it. On a phone there are no windows: they do
// nothing, and the state stays as it starts (visible and focused).
//
// Where a platform can't do something, the method does nothing there:
// Linux's desktop places windows (SetPosition, Center) and keeps them on
// top itself (SetAlwaysOnTop) with no way for GTK 4 to ask; a browser tab
// can only go full screen and take the focus (the main window on the
// web), while a Window in the page does all but minimize.
type WindowHandle struct {
	r      *Root
	e      *element // the window's while it's open: the root's or the Window node's
	main   bool
	open   *signal.Signal[bool] // a Window's Open
	closed func()               // a Window's: closes it as the user's closing does
	ask    func() bool          // WindowProps' or WindowOptions' OnCloseRequest
	asks   []*func() bool       // OnCloseRequest's
	// watched: the core listens for the window's state (UseWindow,
	// MainWindow or a Handle asked for it).
	watched   bool
	listening uint8 // what the open window's element listens for: 1 state, 2 close requests

	visible, focused, minimized, maximized, fullscreen, onTop *signal.Signal[bool]
	pos                                                       *signal.Signal[Point]
	size                                                      *signal.Signal[Size]
}

// Size is a size in points.
type Size struct{ Width, Height float32 }

// NewWindowHandle makes a handle for a Window (WindowProps.Handle), to
// control it from outside it.
func NewWindowHandle() *WindowHandle {
	return &WindowHandle{
		visible: signal.New(false), focused: signal.New(false), minimized: signal.New(false),
		maximized: signal.New(false), fullscreen: signal.New(false), onTop: signal.New(false),
		pos: signal.New(Point{}), size: signal.New(Size{}),
	}
}

// windowCtx passes the window a component is in: a Window's handle, or
// the root's (the main window's).
var windowCtx = signal.NewContext[*WindowHandle](nil)

// UseWindow returns the handle of the window the calling component is
// in: the Window whose content it is, or the main window. Call it while
// building the component.
func UseWindow() *WindowHandle {
	w := windowCtx.Use()
	if w == nil {
		w = current().win
	}
	w.watch()
	return w
}

// MainWindow returns the main window's handle (the first root's), from
// anywhere on the main thread: a tray menu's item, a global shortcut.
// Nil before the app mounts.
func MainWindow() *WindowHandle {
	if len(roots) == 0 {
		return nil
	}
	w := roots[0].win
	w.watch()
	return w
}

func newMainWindow(r *Root) *WindowHandle {
	w := NewWindowHandle()
	w.r, w.main = r, true
	w.visible.Set(true)
	w.focused.Set(true)
	return w
}

// watch has the window's state reported from now on.
func (w *WindowHandle) watch() {
	if w.watched {
		return
	}
	w.watched = true
	w.sync()
}

// attach is the Window opening: e is its element.
func (w *WindowHandle) attach(e *element) {
	w.e = e
	w.listening = 0
	e.handlers().win = w
	w.sync()
}

// detach is the Window closing: it reads closed from now on.
func (w *WindowHandle) detach() {
	w.e = nil
	w.listening = 0
	signal.Batch(func() {
		w.visible.Set(false)
		w.focused.Set(false)
		w.minimized.Set(false)
		w.maximized.Set(false)
		w.fullscreen.Set(false)
	})
}

// el is the window's element, if it's open: a Window's, or the root's.
func (w *WindowHandle) el() *element {
	if w.main {
		if w.r == nil || w.r.el == nil || w.r.el.dead {
			return nil
		}
		return w.r.el
	}
	if w.e == nil || w.e.dead {
		return nil
	}
	return w.e
}

// desktop reports whether the window's host has windows (not a phone's).
func (w *WindowHandle) desktop() bool {
	if w.r == nil {
		return false
	}
	p := w.r.envV.Platform
	return p != ops.PlatformIOS && p != ops.PlatformAndroid
}

// sync listens for what the window needs reported: its state while
// watched, the user's close requests while something asks about them.
func (w *WindowHandle) sync() {
	e := w.el()
	if e == nil || !w.desktop() {
		return
	}
	if w.main {
		e.handlers().win = w
	}
	want := uint8(0)
	if w.watched {
		want |= 1
	}
	if w.ask != nil || len(w.asks) > 0 {
		want |= 2
	}
	for _, x := range [...]struct {
		bit uint8
		ev  ops.EventKind
	}{{1, ops.EventWindowState}, {2, ops.EventCloseRequest}} {
		switch {
		case want&x.bit != 0 && w.listening&x.bit == 0:
			e.listen(x.ev)
		case want&x.bit == 0 && w.listening&x.bit != 0:
			e.r.batch.Unlisten(e.id, x.ev)
		}
	}
	w.listening = want
}

func (w *WindowHandle) do(a ops.WindowAction, x, y float32) {
	e := w.el()
	if e == nil || !w.desktop() {
		return
	}
	e.r.batch.CommandArgs(e.id, ops.CmdWindowAction, func(dst []byte) []byte {
		return ops.AppendWindowAction(dst, a, x, y)
	})
}

// openIfClosed opens a closed Window (its Open set true), and reports
// whether it was closed.
func (w *WindowHandle) openIfClosed() bool {
	if w.main || w.el() != nil || w.open == nil {
		return false
	}
	w.open.Set(true)
	return true
}

// Show shows the window (one opened Hidden, or hidden since), in front of
// the app's others; a closed Window opens.
func (w *WindowHandle) Show() {
	if !w.openIfClosed() {
		w.do(ops.WindowActionShow, 0, 0)
	}
}

// Hide hides the window, which stays open: its content lives on, and Show
// brings it back as it was. The main window hidden, the app keeps running
// (a tray icon's app, say).
func (w *WindowHandle) Hide() { w.do(ops.WindowActionHide, 0, 0) }

// Focus shows the window if it's hidden or minimized, brings it to the
// front and gives it the keyboard, bringing the app to the front too; a
// closed Window opens. It's how a tray icon's menu brings a window back.
func (w *WindowHandle) Focus() {
	if w.openIfClosed() {
		return
	}
	w.do(ops.WindowActionFocus, 0, 0)
}

// Minimize minimizes the window (to the Dock, the taskbar).
func (w *WindowHandle) Minimize() { w.do(ops.WindowActionMinimize, 0, 0) }

// Maximize maximizes it (a Mac's zoom: as large as its display lets it).
func (w *WindowHandle) Maximize() { w.do(ops.WindowActionMaximize, 0, 0) }

// Restore brings it back from minimized or maximized.
func (w *WindowHandle) Restore() { w.do(ops.WindowActionRestore, 0, 0) }

// SetFullscreen puts it in full screen, or takes it out (the web's main
// window: the Fullscreen API, which a browser lets an app enter only in
// answer to the user, as a press's handler is).
func (w *WindowHandle) SetFullscreen(on bool) {
	a := ops.WindowActionExitFullscreen
	if on {
		a = ops.WindowActionFullscreen
	}
	w.do(a, 0, 0)
}

// SetSize sizes its content to width by height points (within its least
// and most).
func (w *WindowHandle) SetSize(width, height float32) { w.do(ops.WindowActionSize, width, height) }

// SetPosition moves its frame's top left to x, y on the screen, in points
// from the main display's top left (not on Linux).
func (w *WindowHandle) SetPosition(x, y float32) { w.do(ops.WindowActionPosition, x, y) }

// Center centers it on its display (not on Linux).
func (w *WindowHandle) Center() { w.do(ops.WindowActionCenter, 0, 0) }

// SetAlwaysOnTop keeps it above other apps' windows, or not (not on
// Linux).
func (w *WindowHandle) SetAlwaysOnTop(on bool) {
	a := ops.WindowActionNotAlwaysOnTop
	if on {
		a = ops.WindowActionAlwaysOnTop
	}
	w.do(a, 0, 0)
}

// Close closes the window as the user does: its OnCloseRequest functions
// are asked first, and any false keeps it open. The main window closing
// quits the app.
func (w *WindowHandle) Close() {
	if w.el() == nil {
		return
	}
	if w.allowClose() {
		w.Destroy()
	}
}

// Destroy closes the window now, without asking: what an OnCloseRequest
// that said no calls once the user has chosen (saved, or discarded the
// changes).
func (w *WindowHandle) Destroy() {
	if w.el() == nil {
		return
	}
	if w.main {
		w.do(ops.WindowActionClose, 0, 0)
		return
	}
	if w.closed != nil {
		w.closed()
	}
}

// OnCloseRequest asks f when the user closes the window (or Close is
// called), while the calling component lives: f returning false keeps
// it open. Every function asked must agree; WindowProps' and
// WindowOptions' OnCloseRequest are asked too.
func (w *WindowHandle) OnCloseRequest(f func() bool) {
	p := &f
	w.asks = append(w.asks, p)
	w.sync()
	signal.OnCleanup(func() {
		for i, x := range w.asks {
			if x == p {
				w.asks = append(w.asks[:i], w.asks[i+1:]...)
				break
			}
		}
		w.sync()
	})
}

// allowClose asks the OnCloseRequest functions; all must agree.
func (w *WindowHandle) allowClose() bool {
	ok := true
	if w.ask != nil && !w.ask() {
		ok = false
	}
	for _, f := range append([]*func() bool(nil), w.asks...) {
		if !(*f)() {
			ok = false
		}
	}
	return ok
}

// closeRequested is the user's asking to close it (CloseRequest).
func (w *WindowHandle) closeRequested() {
	if w.allowClose() {
		w.Destroy()
	}
}

// setState takes what the host reports.
func (w *WindowHandle) setState(s ops.WindowState) {
	w.visible.Set(s.Flags&ops.WindowVisible != 0)
	w.focused.Set(s.Flags&ops.WindowFocused != 0)
	w.minimized.Set(s.Flags&ops.WindowMinimizedState != 0)
	w.maximized.Set(s.Flags&ops.WindowMaximizedState != 0)
	w.fullscreen.Set(s.Flags&ops.WindowFullscreenState != 0)
	w.onTop.Set(s.Flags&ops.WindowOnTop != 0)
	w.pos.Set(Point{X: s.X, Y: s.Y})
	w.size.Set(Size{Width: s.Width, Height: s.Height})
}

// Visible is whether the window shows: not hidden (a minimized window, or
// one behind others, is visible) and, for a Window, open.
func (w *WindowHandle) Visible() signal.Accessor[bool] { return w.visible }

// Focused is whether the window has the keyboard: it's the app's key
// (active) window and the app is in front.
func (w *WindowHandle) Focused() signal.Accessor[bool] { return w.focused }

// Minimized is whether it's minimized.
func (w *WindowHandle) Minimized() signal.Accessor[bool] { return w.minimized }

// Maximized is whether it's maximized (a Mac's zoomed).
func (w *WindowHandle) Maximized() signal.Accessor[bool] { return w.maximized }

// Fullscreen is whether it's in full screen.
func (w *WindowHandle) Fullscreen() signal.Accessor[bool] { return w.fullscreen }

// AlwaysOnTop is whether it's kept above other apps' windows.
func (w *WindowHandle) AlwaysOnTop() signal.Accessor[bool] { return w.onTop }

// Position is its frame's top left on the screen, in points from the main
// display's top left (0, 0 on Linux under Wayland, which doesn't say).
func (w *WindowHandle) Position() signal.Accessor[Point] { return w.pos }

// Size is its content's size, in points (as its environment's Width and
// Height).
func (w *WindowHandle) Size() signal.Accessor[Size] { return w.size }
