//go:build windows

package windows

import (
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A full-screen sheet is a borderless window over all of its window's
// client area, its owner disabled meanwhile; it follows the window as it
// resizes and moves, its environment with it; Escape dismisses it.
func TestFullScreenSheet(t *testing.T) {
	open := signal.New(false)
	var env ui.Environment
	h, _ := mount(t, func() ui.Node {
		return ui.Sheet(ui.SheetProps{Open: open, FullScreen: true, Content: func() ui.Node {
			e := ui.UseEnvironment()
			signal.Effect(func() { env = e.Get() })
			return ui.TextField(ui.TextFieldProps{Value: signal.New("")})
		}})
	})
	onMain(t, func() {
		h.enter(func() { open.Set(true) })
		sheets := find(h, ops.KindSheet)
		if len(sheets) != 1 || sheets[0].nav.window == 0 {
			fatalf("no sheet window")
		}
		s := sheets[0]
		w := s.nav.window
		if style := WindowLong(w, GWL_STYLE); style&WS_CAPTION != 0 || style&WS_THICKFRAME != 0 {
			fatalf("a full-screen sheet with a frame: style %#x", style)
		}
		if IsWindowEnabled(h.window) {
			fatalf("its window takes input under it")
		}
		covers := func(what string) {
			want := h.coverRect(h.window)
			if got := WindowRect(w); got != want {
				fatalf("%s: the sheet at %v, its window's content at %v", what, got, want)
			}
			cr := ClientRect(h.window)
			if env.Width != h.pt(s, cr.W()) || env.Height != h.pt(s, cr.H()) || env.SafeTop != 0 {
				fatalf("%s: its environment %v×%v (safe top %v), the window's content %v×%v", what, env.Width, env.Height, env.SafeTop, h.pt(s, cr.W()), h.pt(s, cr.H()))
			}
		}
		covers("opened")
		r := WindowRect(h.window)
		SetWindowPos(h.window, 0, r.Left+40, r.Top+30, r.W()+120, r.H()+80, SWP_NOZORDER|SWP_NOACTIVATE)
		h.runPosted()
		covers("its window moved and grown")

		// Escape dismisses it, and its window takes input again.
		pressKey(h, w, VK_ESCAPE, 0, 0)
		h.runPosted()
		if open.Peek() || !IsWindowEnabled(h.window) {
			fatalf("Escape: open %v, the window enabled %v", open.Peek(), IsWindowEnabled(h.window))
		}
	})
}
