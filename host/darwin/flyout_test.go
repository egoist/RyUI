//go:build darwin && !ios

package darwin

import (
	"math"
	"os"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/sdk/tray"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// appActive reports whether the app is the one in front (the workspace's
// frontmost application): NSApp's isActive reads true while a
// nonactivating panel is key.
func appActive() bool {
	front := Send(Send(Class("NSWorkspace"), "sharedWorkspace"), "frontmostApplication")
	return front != 0 && SendInt(front, "processIdentifier") == os.Getpid()
}

// frameYDown is w's frame on the screen, y down.
func frameYDown(w ID) (x, y, width, height float64) {
	r := yDown(MsgRect(w, Sel("frame")))
	return r.X, r.Y, r.W, r.H
}

func closeTo(a, b float64) bool { return math.Abs(a-b) <= 1 }

// A tray icon's flyout: an AsukaPanel, nonactivating, at the level of
// menus, under the status item's button and centered on it, sized to its
// content and placed again as that grows; a modal Window over it is its
// child window and keeps it open; it closes as it loses the keyboard and
// on Escape.
func TestFlyoutDarwin(t *testing.T) {
	open := signal.New(false)
	rows := signal.New(2)
	confirm := signal.New(false)
	var icon *tray.Icon
	h, _ := mount(t, func() ui.Node {
		icon = tray.Show(tray.Props{Symbol: ui.Val("bolt.fill"), OnClick: func() { open.Set(!open.Peek()) }})
		return ui.Window(ui.WindowProps{
			Open:       open,
			Flyout:     &ui.Flyout{Anchor: icon.Anchor(), Placement: ui.PlacementBottom},
			FitContent: true, MinWidth: 200, MaxHeight: 500,
			Content: func() ui.Node {
				return ui.Column(ui.Style{Padding: ui.All(10)},
					ui.Column(ui.StyleFunc(func() ui.Style {
						return ui.Style{Width: ui.Pt(240), Height: ui.Pt(float32(30 * rows.Get()))}
					})),
					ui.Window(ui.WindowProps{Open: confirm, Modal: true, Title: ui.Val("Quit?"), Width: 300, Height: 120,
						Content: func() ui.Node { return ui.StaticText("Quit?") }}),
				)
			},
		})
	})
	bar := false
	onMain(func() {
		active := appActive()
		// The status item goes in the menu bar as the run loop turns.
		inBar := func() bool {
			for _, ic := range trayState.icons {
				b := Send(ic.item, "button")
				if bw := Send(b, "window"); bw != 0 {
					return yDown(MsgRect(bw, Sel("frame"))).Y < 100
				}
			}
			return false
		}
		if bar = spin(inBar); !bar {
			return
		}
		post(h, func() { open.Set(true) })
		x := onlyWin(t, h)
		w := x.window
		if !SendBool(w, "isKindOfClass:", uintptr(Class("AsukaPanel"))) || Send(w, "styleMask")&styleNonactivating == 0 {
			fatal(t, "the flyout isn't a nonactivating panel")
		}
		if !SendBool(w, "isVisible") || SendInt(w, "level") != popUpMenuLevel || SendBool(w, "isOpaque") || !SendBool(w, "hasShadow") {
			fatalf(t, "visible %v level %d opaque %v", SendBool(w, "isVisible"), SendInt(w, "level"), SendBool(w, "isOpaque"))
		}
		if appActive() != active {
			fatal(t, "showing the flyout activated the app")
		}
		if b := MsgRect(x.content, Sel("bounds")).Size; b.Width != 260 || b.Height != 80 {
			fatalf(t, "content %+v, want 260 by 80", b)
		}
		a, ok := h.flyoutAnchor(x)
		if !ok || a.W <= 0 {
			fatalf(t, "no anchor %+v", a)
		}
		fx, fy, fw, fh := frameYDown(w)
		work := workArea(a.X+a.W/2, a.Y+a.H/2)
		wantX := max(min(a.X+(a.W-fw)/2, work.Right()-fw), work.X)
		if !closeTo(fy, max(a.Bottom(), work.Y)) || !closeTo(fx, wantX) || fh != 80 {
			fatalf(t, "flyout at %v,%v %vx%v; anchor %+v, work %+v", fx, fy, fw, fh, a, work)
		}
		// Its content grows: resized, still under the icon.
		post(h, func() { rows.Set(5) })
		if _, y2, _, h2 := frameYDown(w); h2 != 170 || !closeTo(y2, fy) {
			fatalf(t, "after growing: y %v height %v", y2, h2)
		}
		// A dialog over it: its child window, not a sheet, at its level;
		// the flyout stays as it loses the keyboard to it.
		post(h, func() { confirm.Set(true) })
		var d *win
		for _, y := range h.wins {
			if y != x {
				d = y
			}
		}
		if d == nil || Send(d.window, "parentWindow") != w || Send(d.window, "sheetParent") != 0 || SendInt(d.window, "level") != popUpMenuLevel {
			fatal(t, "the dialog isn't the flyout's child window at its level")
		}
		h.resignedKey(w)
		h.runPosted()
		if !open.Peek() {
			fatal(t, "the flyout closed under its dialog")
		}
		post(h, func() { confirm.Set(false) })
		// It loses the keyboard: closed, as the user closes it.
		Send(w, "resignKeyWindow")
		h.resignedKey(w)
		h.runPosted()
		if open.Peek() || len(h.wins) != 0 {
			fatalf(t, "open %v, %d windows after losing the keyboard", open.Peek(), len(h.wins))
		}
		// Open again; Escape closes it.
		post(h, func() { open.Set(true) })
		x = onlyWin(t, h)
		h.cancel(x.content)
		h.runPosted()
		if open.Peek() {
			fatal(t, "Escape didn't close the flyout")
		}
	})
	if !bar {
		t.Skip("the status item isn't in the menu bar (no menu bar in this session, or no room in it)")
	}
}

// A flyout hanging from a rectangle of its parent: the parent's child
// window, below the rectangle and lined up with its start, gap away.
func TestFlyoutAnchoredDarwin(t *testing.T) {
	open := signal.New(true)
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{
			Open:   open,
			Flyout: &ui.Flyout{Anchor: ui.Val(ui.Anchor{X: 10, Y: 20, Width: 100, Height: 30}), Placement: ui.PlacementBottomStart, Gap: 4},
			Width:  180, Height: 90,
			Content: func() ui.Node { return ui.StaticText("Hello") },
		})
	})
	onMain(func() {
		Send(h.window, "orderFront:", 0)
		h.setTopLeft(h.window, 200, 200)
		post(h, func() { open.Set(false) })
		post(h, func() { open.Set(true) })
		x := onlyWin(t, h)
		w := x.window
		c := yDown(MsgRectToRect(h.window, Sel("convertRectToScreen:"), MsgRectIDToRect(h.content, Sel("convertRect:toView:"), MsgRect(h.content, Sel("bounds")), 0)))
		fx, fy, fw, fh := frameYDown(w)
		if !closeTo(fx, c.X+10) || !closeTo(fy, c.Y+54) || fw != 180 || fh != 90 {
			fatalf(t, "flyout at %v,%v %vx%v; the parent's content at %+v", fx, fy, fw, fh, c)
		}
		if Send(w, "parentWindow") != h.window {
			fatal(t, "not the parent's child window")
		}
	})
}

// A panel as the main window: an AsukaPanel made from the main window,
// nonactivating, on every Space, floating; shown without activating the
// app, sized to its content and centered on the pointer's display.
func TestPanelDarwin(t *testing.T) {
	ui.SetWindowOptions(ui.WindowOptions{Panel: true, FitContent: true, Hidden: true, MinWidth: 600, MaxHeight: 400})
	defer ui.SetWindowOptions(ui.WindowOptions{})
	results := signal.New(0)
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.Column(ui.Style{Height: ui.Pt(56)}),
			ui.Column(ui.StyleFunc(func() ui.Style { return ui.Style{Height: ui.Pt(float32(40 * results.Get()))} })),
		)
	})
	onMain(func() {
		w := h.window
		if !SendBool(w, "isKindOfClass:", uintptr(Class("AsukaPanel"))) || Send(w, "styleMask")&styleNonactivating == 0 {
			fatal(t, "the main window isn't a nonactivating panel")
		}
		b := uint(SendInt(w, "collectionBehavior"))
		if b&joinAllSpaces == 0 || b&fullScreenAux == 0 || b&ignoresCycle == 0 || SendInt(w, "level") != floatingPanelLevel {
			fatalf(t, "collection behavior %b level %d", b, SendInt(w, "level"))
		}
		if SendBool(w, "isVisible") {
			fatal(t, "a hidden panel shows")
		}
		active := appActive()
		post(h, func() { ui.MainWindow().Show() })
		if !SendBool(w, "isVisible") || appActive() != active {
			fatalf(t, "visible %v, active %v (was %v)", SendBool(w, "isVisible"), appActive(), active)
		}
		if s := MsgRect(h.content, Sel("bounds")).Size; s.Width != 600 || s.Height != 56 {
			fatalf(t, "content %+v", s)
		}
		fx, fy, fw, fh := frameYDown(w)
		work := workArea(pointer())
		if !closeTo(fx, work.X+(work.W-fw)/2) || !closeTo(fy, work.Y+(work.H-fh)/3) {
			fatalf(t, "panel at %v,%v; work area %+v", fx, fy, work)
		}
		// Results come: it grows down from its top.
		post(h, func() { results.Set(3) })
		if _, y2, _, h2 := frameYDown(w); h2 != 176 || !closeTo(y2, fy) {
			fatalf(t, "after results: y %v height %v (was at %v)", y2, h2, fy)
		}
		post(h, func() { results.Set(30) })
		if _, _, _, h2 := frameYDown(w); h2 != 400 {
			fatalf(t, "height %v past MaxHeight", h2)
		}
		// Escape hides it (closes the main window: the app quits unless it
		// keeps running).
		h.cancel(h.content)
		if SendBool(w, "isVisible") {
			fatal(t, "Escape left the panel up")
		}
	})
}

// Click-through: ignoresMouseEvents, switched from the handle, and
// reported.
func TestClickThroughDarwin(t *testing.T) {
	open := signal.New(true)
	handle := ui.NewWindowHandle()
	h, _ := mount(t, func() ui.Node {
		return ui.Window(ui.WindowProps{Open: open, Handle: handle, ClickThrough: true, Transparent: true, AlwaysOnTop: true,
			Width: 200, Height: 100, Content: func() ui.Node { return ui.StaticText("HUD") }})
	})
	onMain(func() {
		x := onlyWin(t, h)
		if !SendBool(x.window, "ignoresMouseEvents") || !handle.ClickThrough().Get() {
			fatal(t, "not click-through")
		}
		post(h, func() { handle.SetClickThrough(false) })
		if SendBool(x.window, "ignoresMouseEvents") || handle.ClickThrough().Get() {
			fatal(t, "still click-through")
		}
	})
}
