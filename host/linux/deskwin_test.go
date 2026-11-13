//go:build linux && !android

package linux

import (
	"slices"
	"testing"
	"time"

	"github.com/egoist/asuka/internal/flyout"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/tray"
	"github.com/egoist/asuka/ui"
)

// onX11 skips a test off X11 (Xvfb is), where windows can't be placed.
func onX11(t *testing.T) Ptr {
	t.Helper()
	var disp Ptr
	ok := false
	onMain(func() { disp = DisplayGetDefault(); ok = IsX11(disp) && LoadXWM() })
	if !ok {
		t.Skip("not on X11")
	}
	return disp
}

// theWindow is the one Window node's window, once it shows.
func theWindow(t *testing.T, h *Host) *win {
	t.Helper()
	var w *win
	settle(t, h, func() bool {
		ws := find(h, ops.KindWindow)
		if len(ws) != 1 || ws[0].win == nil {
			return false
		}
		w = ws[0].win
		return WidgetGetMapped(w.window)
	})
	return w
}

// origin is where w is on the screen: its X window's place, as the X
// server says, and the window's in its surface (around it, GTK's frame
// without a compositor).
func origin(t *testing.T, disp Ptr, w *win) (int32, int32) {
	t.Helper()
	var x, y int32
	onMain(func() {
		_, xid, ok := (&Host{}).x11Win(w)
		if !ok {
			fatalf(t, "no X window")
		}
		XSync(disp)
		x, y, ok = XOrigin(disp, xid)
		if !ok {
			fatalf(t, "no origin")
		}
		var tx, ty float64
		WM.NativeGetSurfaceTransform(w.window, &tx, &ty)
		x, y = x+int32(tx), y+int32(ty)
	})
	return x, y
}

// pollFor checks cond, which runs on the main thread itself, until it
// holds, for at most d.
func pollFor(d time.Duration, cond func() bool) bool {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	return false
}

// pressKey has a window's key controller take a key, as it would a
// press its content left.
func pressKey(ctrl Ptr, keyval uint32) bool {
	var handled int32
	s := CString("key-pressed")
	Call(Sym("g_signal_emit_by_name"), ctrl, Addr(s), uintptr(keyval), 0, 0, Addr(&handled))
	return handled != 0
}

// A panel on X11: frameless, a utility window above the others, on every
// desktop and out of the taskbar and the pager, placed on the pointer's
// display centered across and a third down; Escape closes it, and so
// does losing the keyboard.
func TestPanelX11(t *testing.T) {
	disp := onX11(t)
	open := signal.New(false)
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("main"),
			ui.Window(ui.WindowProps{Open: open, Panel: true, Width: 400, Height: 120,
				Content: func() ui.Node { return ui.StaticText("panel") }}),
		)
	})
	// Opened once the main window shows (which would take the keyboard
	// from it).
	onMain(func() { open.Set(true); r.Flush() })
	w := theWindow(t, h)
	var want flyout.Rect
	onMain(func() {
		if GetBoolProperty(w.window, "decorated") || WidgetGetVisible(w.header) {
			fatalf(t, "a panel has a frame or a header bar")
		}
		_, xid, _ := h.x11Win(w)
		XSync(disp)
		atom := func(n string) uint64 { return XAtom(disp, n) }
		if typ, _ := XPropertyAtoms(disp, xid, "_NET_WM_WINDOW_TYPE"); !slices.Equal(typ, []uint64{atom("_NET_WM_WINDOW_TYPE_UTILITY")}) {
			fatalf(t, "window type %v", typ)
		}
		states, _ := XPropertyAtoms(disp, xid, "_NET_WM_STATE")
		for _, s := range []string{"_NET_WM_STATE_ABOVE", "_NET_WM_STATE_STICKY", "_NET_WM_STATE_SKIP_TASKBAR", "_NET_WM_STATE_SKIP_PAGER"} {
			if !slices.Contains(states, atom(s)) {
				fatalf(t, "_NET_WM_STATE lacks %s: %v", s, states)
			}
		}
		if d, _ := XPropertyAtoms(disp, xid, "_NET_WM_DESKTOP"); !slices.Equal(d, []uint64{0xFFFFFFFF}) {
			fatalf(t, "_NET_WM_DESKTOP %v", d)
		}
		if !h.onTop(w) || h.windowState(w).Flags&ops.WindowOnTop == 0 {
			fatalf(t, "not reported on top")
		}
	})
	// At its size as GTK allocates it (its default size holds GTK's frame
	// here).
	if !pollFor(2*time.Second, func() bool {
		onMain(func() {
			ww, wh := h.outerSize(w)
			want = flyout.Center(ww, wh, h.pointerWorkArea(disp, 1))
			want.X, want.Y = roundF(want.X), roundF(want.Y)
		})
		x, y := origin(t, disp, w)
		return x == int32(want.X) && y == int32(want.Y)
	}) {
		x, y := origin(t, disp, w)
		t.Fatalf("panel at %d, %d, want %v (the pointer's display, a third down)", x, y, want)
	}

	// Escape closes it, as the user closing it.
	onMain(func() {
		if !pressKey(w.desk.keys, keyEscape) {
			fatalf(t, "Escape not taken")
		}
	})
	settle(t, h, func() bool { return !open.Peek() && len(find(h, ops.KindWindow)) == 0 })

	// Open again, then the main window takes the keyboard: it closes.
	onMain(func() { open.Set(true); r.Flush() })
	w = theWindow(t, h)
	if !within(2*time.Second, func() bool { return WindowIsActive(w.window) }) {
		t.Skip("the X server gives the panel no keyboard here: losing it not checked")
	}
	onMain(func() { WindowPresent(h.main.window) })
	settle(t, h, func() bool { return !open.Peek() })
}

// A flyout hanging from a rectangle of its parent: frameless, transient
// for the parent, sized to its content before it shows, placed below the
// rectangle, and placed again as its content grows; a Modal Window over
// it keeps it open as it takes the keyboard, which the flyout gets back
// after; then losing the keyboard closes it.
func TestFlyoutRectX11(t *testing.T) {
	disp := onX11(t)
	open, modal := signal.New(false), signal.New(false)
	width := signal.New[float32](200)
	anchor := ui.Anchor{X: 100, Y: 50, Width: 40, Height: 20}
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("main"),
			ui.Window(ui.WindowProps{Open: open, FitContent: true,
				Flyout: &ui.Flyout{Anchor: ui.Val(anchor), Placement: ui.PlacementBottom, Gap: 4},
				Content: func() ui.Node {
					return ui.Column(ui.Style{},
						ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(200), Height: ui.Pt(100)}}),
						ui.Dynamic(func() ui.Node {
							return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(width.Get()), Height: ui.Pt(10)}})
						}),
						ui.Window(ui.WindowProps{Open: modal, Modal: true, Width: 200, Height: 100,
							Content: func() ui.Node { return ui.StaticText("modal") }}),
					)
				}}),
		)
	})
	onMain(func() { open.Set(true); r.Flush() })
	var w *win
	settle(t, h, func() bool {
		for _, n := range find(h, ops.KindWindow) {
			if n.win != nil && n.win.cfg.Flags&ops.WindowFlyout != 0 && WidgetGetMapped(n.win.window) {
				w = n.win
				return true
			}
		}
		return false
	})
	check := func(cw, ch float64) {
		t.Helper()
		var want flyout.Rect
		if !within(2*time.Second, func() bool {
			return WidgetGetMapped(w.window) && Call(FnWidgetGetWidth, w.window) == uintptr(cw) && Call(FnWidgetGetHeight, w.window) == uintptr(ch)
		}) {
			onMain(func() {
				s := NativeGetSurface(w.window)
				var tx, ty float64
				WM.NativeGetSurfaceTransform(w.window, &tx, &ty)
				fatalf(t, "surface %dx%d, want %vx%v; transform %v,%v; widget %dx%d; closing %v open %v",
					Call(Sym("gdk_surface_get_width"), s), Call(Sym("gdk_surface_get_height"), s), cw, ch, tx, ty,
					Call(FnWidgetGetWidth, w.window), Call(FnWidgetGetHeight, w.window), w.closing, open.Peek())
			})
		}
		onMain(func() {
			if !w.desk.fitted || w.desk.fitW != float32(cw) || w.desk.fitH != float32(ch) {
				fatalf(t, "fitted %v %vx%v, want %vx%v", w.desk.fitted, w.desk.fitW, w.desk.fitH, cw, ch)
			}
			if p := WM.WindowGetTransientFor(w.window); p != h.main.window {
				fatalf(t, "transient for %x, want the main window", p)
			}
			ox, oy, ok := h.contentOrigin(h.main, 1)
			if !ok {
				fatalf(t, "no origin for the main window")
			}
			a := flyout.Rect{X: ox + float64(anchor.X), Y: oy + float64(anchor.Y), W: float64(anchor.Width), H: float64(anchor.Height)}
			want = flyout.Place(a, cw, ch, ops.PlacementBottom, 4, h.workAreaAt(disp, a.X, a.Y))
			if want.Y != a.Bottom()+4 {
				fatalf(t, "expected below the anchor: %v, anchor %v", want, a)
			}
		})
		if !pollFor(2*time.Second, func() bool {
			x, y := origin(t, disp, w)
			return x == int32(want.X) && y == int32(want.Y)
		}) {
			x, y := origin(t, disp, w)
			t.Fatalf("flyout at %d, %d, want %v", x, y, want)
		}
	}
	onMain(func() {
		if GetBoolProperty(w.window, "decorated") {
			fatalf(t, "a flyout has a frame")
		}
	})
	check(200, 110)
	onMain(func() { width.Set(260); r.Flush() })
	check(260, 110)

	if !within(2*time.Second, func() bool { return WindowIsActive(w.window) }) {
		t.Skip("the X server gives the flyout no keyboard here: losing it not checked")
	}
	// A Modal Window over it: it stays, and has the keyboard back after.
	onMain(func() { modal.Set(true); r.Flush() })
	settle(t, h, func() bool { return len(find(h, ops.KindWindow)) == 2 && !WindowIsActive(w.window) })
	onMain(func() {
		if !open.Peek() || !w.desk.held {
			fatalf(t, "the flyout closed (open %v) or isn't held, under its modal window", open.Peek())
		}
	})
	onMain(func() { modal.Set(false); r.Flush() })
	settle(t, h, func() bool { return len(find(h, ops.KindWindow)) == 1 && WindowIsActive(w.window) && open.Peek() })
	// The main window takes the keyboard: it closes.
	onMain(func() { WindowPresent(h.main.window) })
	settle(t, h, func() bool { return !open.Peek() && len(find(h, ops.KindWindow)) == 0 })
}

// A tray icon's flyout goes below where the panel says the icon was
// clicked, within the display; closed as it loses the keyboard to the
// click on its icon, the click doesn't open it again.
func TestFlyoutTrayX11(t *testing.T) {
	disp := onX11(t)
	registered := startWatcher(t)
	open := signal.New(false)
	clicks := 0
	h, r := mount(t, func() ui.Node {
		icon := tray.Show(tray.Props{Symbol: ui.Val("bolt"), OnClick: func() { clicks++; open.Set(!open.Peek()) }})
		return ui.Column(ui.Style{},
			ui.StaticText("main"),
			ui.Window(ui.WindowProps{Open: open, Width: 300, Height: 200,
				Flyout:  &ui.Flyout{Anchor: icon.Anchor(), Placement: ui.PlacementBottom, Gap: 6},
				Content: func() ui.Node { return ui.StaticText("flyout") }}),
		)
	})
	_ = r
	settle(t, h, func() bool { return len(*registered) == 1 })
	service := (*registered)[0]
	activate := func(x, y int32) {
		t.Helper()
		if _, err := busCall(t, service, sniPath, sniIface, "Activate", func() Ptr { return VTuple(VI32(x), VI32(y)) }); err != nil {
			t.Fatal("Activate:", err)
		}
	}
	activate(500, 20)
	w := theWindow(t, h)
	var want flyout.Rect
	onMain(func() {
		if p := WM.WindowGetTransientFor(w.window); p != 0 {
			fatalf(t, "a tray icon's flyout is transient for %x", p)
		}
	})
	// At its size as GTK allocates it.
	if !pollFor(2*time.Second, func() bool {
		onMain(func() {
			ww, wh := h.outerSize(w)
			want = flyout.Place(flyout.Rect{X: 500, Y: 20, W: 1, H: 1}, ww, wh, ops.PlacementBottom, 6, h.workAreaAt(disp, 500, 20))
			want.X, want.Y = roundF(want.X), roundF(want.Y)
		})
		x, y := origin(t, disp, w)
		return x == int32(want.X) && y == int32(want.Y)
	}) {
		x, y := origin(t, disp, w)
		t.Fatalf("flyout at %d, %d, want %v (below the click)", x, y, want)
	}
	if !within(2*time.Second, func() bool { return WindowIsActive(w.window) }) {
		t.Skip("the X server gives the flyout no keyboard here: losing it not checked")
	}
	// The click on the icon takes its keyboard (a panel taking focus):
	// it closes, and the click doesn't open it again.
	onMain(func() { WindowPresent(h.main.window) })
	settle(t, h, func() bool { return !open.Peek() })
	activate(500, 20)
	time.Sleep(50 * time.Millisecond)
	onMain(func() {
		if open.Peek() || clicks != 1 {
			fatalf(t, "the closing click opened it again (open %v, clicks %d)", open.Peek(), clicks)
		}
	})
	// A click later opens it.
	onMain(func() {
		for _, ic := range h.trays().icons {
			ic.flyoutClosed = time.Time{}
		}
	})
	activate(500, 20)
	settle(t, h, func() bool { return open.Peek() && clicks == 2 })
}

// A click-through window has an empty input shape, reported in its
// state; switched off, its own again.
func TestClickThroughLinux(t *testing.T) {
	disp := onX11(t)
	open := signal.New(true)
	handle := ui.NewWindowHandle()
	h, r := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, Handle: handle, ClickThrough: true, Width: 300, Height: 200,
			Content: func() ui.Node { ui.UseWindow(); return ui.StaticText("overlay") }})
	})
	_ = r
	w := theWindow(t, h)
	shape := func() [][4]int16 {
		var rects [][4]int16
		onMain(func() {
			_, xid, _ := h.x11Win(w)
			XSync(disp)
			var ok bool
			if rects, ok = XInputShape(disp, xid); !ok {
				fatalf(t, "no SHAPE extension (libXext)")
			}
		})
		return rects
	}
	settle(t, h, func() bool { return handle.ClickThrough().Get() })
	if rs := shape(); len(rs) != 0 {
		t.Fatalf("input shape %v, want none", rs)
	}
	onMain(func() { h.enter(func() { ui.Post(func() { handle.SetClickThrough(false) }) }) })
	settle(t, h, func() bool { return !handle.ClickThrough().Get() })
	if !pollFor(2*time.Second, func() bool { return len(shape()) > 0 }) {
		t.Fatal("the input shape stays empty")
	}
	onMain(func() { h.enter(func() { ui.Post(func() { handle.SetClickThrough(true) }) }) })
	settle(t, h, func() bool { return handle.ClickThrough().Get() })
	if !pollFor(2*time.Second, func() bool { return len(shape()) == 0 }) {
		t.Fatalf("input shape %v after SetClickThrough(true)", shape())
	}
}

// The main window sized to its content: its default size (the header bar
// on top) and its content's request follow Fit, from the first flush on.
func TestFitContentMain(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{FitContent: true, Width: 640, Height: 480})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	height := signal.New[float32](120)
	var mappedAt [2]int32
	h, r := mountWith(t, func(h *Host) {}, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(320), Height: ui.Pt(40)}}),
			ui.Dynamic(func() ui.Node {
				return ui.View(ui.ViewProps{Style: ui.Style{Width: ui.Pt(100), Height: ui.Pt(height.Get())}})
			}),
		)
	})
	onMain(func() {
		WindowGetDefaultSize(h.main.window, &mappedAt[0], &mappedAt[1])
		if !h.main.desk.fitted || mappedAt != [2]int32{320, 160 + headerHeight} {
			fatalf(t, "fitted %v, default size %v, want 320x160 and the header bar", h.main.desk.fitted, mappedAt)
		}
	})
	settle(t, h, func() bool { return h.main.envW == 320 && h.main.envH == 160 })
	onMain(func() { height.Set(200); r.Flush() })
	settle(t, h, func() bool {
		var dw, dh int32
		WindowGetDefaultSize(h.main.window, &dw, &dh)
		return dw == 320 && dh == 240+headerHeight && h.main.envH == 240
	})
}

// On X11 a window goes where the app puts it: at X, Y as it opens, and
// SetPosition moves it.
func TestWindowPositionX11(t *testing.T) {
	disp := onX11(t)
	open := signal.New(false)
	handle := ui.NewWindowHandle()
	h, r := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.StaticText("main"),
			ui.Window(ui.WindowProps{Open: open, Handle: handle, X: 200, Y: 150, Width: 300, Height: 200,
				Content: func() ui.Node { ui.UseWindow(); return ui.StaticText("placed") }}),
		)
	})
	onMain(func() { open.Set(true); r.Flush() })
	w := theWindow(t, h)
	at := func(x, y int32) {
		t.Helper()
		if !pollFor(2*time.Second, func() bool {
			ox, oy := origin(t, disp, w)
			return ox == x && oy == y
		}) {
			ox, oy := origin(t, disp, w)
			t.Fatalf("window at %d, %d, want %d, %d", ox, oy, x, y)
		}
	}
	at(200, 150)
	onMain(func() { h.enter(func() { ui.Post(func() { handle.SetPosition(320, 260) }) }) })
	at(320, 260)
}
