//go:build windows

package windows

import (
	"fmt"
	"testing"

	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
)

// A screen's Toolbar and Primary are a command bar under the stack's
// screens: its buttons named by their titles, the primary one an accent
// button at the trailing end; the screen is sized above it, and each
// button reports its own Action.
func TestCommandBar(t *testing.T) {
	var picked []string
	pick := func(s string) func() { return func() { picked = append(picked, s) } }
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node { return ui.StaticText("Inbox") }, ui.ScreenOptions{
			Title: ui.Val("Inbox"),
			Toolbar: []ui.Action{
				{Title: ui.Val("Filter"), Icon: "line.3.horizontal.decrease", OnPress: pick("filter")},
				{Title: ui.Val("Delete all"), Disabled: ui.Val(true), OnPress: pick("delete")},
			},
			Primary: &ui.Action{Title: ui.Val("Compose"), Icon: "square.and.pencil", OnPress: pick("compose")},
		})
	})
	onMain(t, func() {
		stack := find(h, ops.KindStack)[0]
		f := stack.nav.foot
		if f == nil || !IsWindowVisible(f.hwnd) {
			fatalf("no command bar")
		}
		c := f.nav.cmd
		if len(c.buttons) != 2 || WindowText(c.buttons[0]) != "Filter" || WindowText(c.primary) != "Compose" {
			fatalf("buttons %d, %q, %q", len(c.buttons), WindowText(c.buttons[0]), WindowText(c.primary))
		}
		if IsWindowEnabled(c.buttons[1]) {
			fatalf("Delete all is enabled")
		}
		bar := WindowRectIn(f.hwnd, stack.hwnd)
		screen := find(h, ops.KindScreen)[0]
		if s := WindowRectIn(screen.hwnd, stack.hwnd); s.Bottom != bar.Top || bar.H() != h.px(f, cmdBarHeight) {
			fatalf("the screen %+v, the bar %+v", s, bar)
		}
		if p := WindowRectIn(c.primary, f.hwnd); p.Right != ClientRect(f.hwnd).W()-h.px(f, 8) {
			fatalf("the primary button is at %+v", p)
		}
		SendMessage(c.buttons[0], BM_CLICK, 0, 0)
		SendMessage(c.primary, BM_CLICK, 0, 0)
		h.runPosted()
		if fmt.Sprint(picked) != "[filter compose]" {
			fatalf("picked %v", picked)
		}
	})
}
