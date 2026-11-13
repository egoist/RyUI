//go:build linux && !android

package linux

import (
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A full-screen sheet is a floating AdwDialog as big as its whole
// window, which libadwaita shows over all of it: its content is the
// window's size, and follows the window as it resizes. OnBack makes
// Escape ask the core (the dialog can't close itself).
func TestFullScreenSheet(t *testing.T) {
	open := signal.New(false)
	keep := signal.New(false)
	h, _ := mount(t, func() ui.Node {
		return ui.Sheet(ui.SheetProps{Open: open, FullScreen: true, Content: func() ui.Node {
			ui.OnBack(func() bool { return keep.Peek() })
			return ui.TextField(ui.TextFieldProps{Value: signal.New("")})
		}})
	})
	onMain(func() { h.enter(func() { open.Set(true) }) })
	var s *node
	covers := func() bool {
		ss := find(h, ops.KindSheet)
		if len(ss) != 1 || ss[0].nav.dialog == 0 {
			return false
		}
		s = ss[0]
		ww, wh := float32(Call(FnWidgetGetWidth, h.main.window)), float32(Call(FnWidgetGetHeight, h.main.window))
		return ww > 0 && s.nav.envW == ww && s.nav.envH == wh
	}
	settle(t, h, covers)
	onMain(func() {
		if AdwDialogGetPresentationMode(s.nav.dialog) != dialogFloating {
			fatalf(t, "not floating: a narrow window would make it a bottom sheet")
		}
		if AdwDialogGetCanClose(s.nav.dialog) {
			fatalf(t, "OnBack's sheet closes itself on Escape")
		}
		WindowSetDefaultSize(h.main.window, int32(h.main.envW)+120, int32(h.main.envH)+80)
	})
	settle(t, h, covers)
}
