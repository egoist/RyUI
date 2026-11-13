//go:build linux && !android

package linux

import (
	"fmt"
	"testing"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/ui"
)

// A screen's Toolbar and Primary are buttons of a GtkActionBar at the
// bottom of its page, the primary one a suggested-action button; each
// reports its own Action, and the screen is sized above the bar.
func TestScreenActionBar(t *testing.T) {
	var picked []string
	pick := func(s string) func() { return func() { picked = append(picked, s) } }
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node { return ui.StaticText("Inbox") }, ui.ScreenOptions{
			Title: ui.Val("Inbox"),
			Toolbar: []ui.Action{
				{Title: ui.Val("Filter"), Icon: "line.3.horizontal.decrease", OnPress: pick("filter")},
				{Title: ui.Val("Sort"), Children: []ui.Action{{Title: ui.Val("Date"), OnPress: pick("date")}}},
				{Title: ui.Val("Delete all"), Disabled: ui.Val(true), OnPress: pick("delete")},
			},
			Primary: &ui.Action{Title: ui.Val("Compose"), Icon: "square.and.pencil", OnPress: pick("compose")},
		})
	})
	var s *node
	settle(t, h, func() bool {
		ss := find(h, ops.KindScreen)
		if len(ss) == 1 && ss[0].nav.bottom != nil && WidgetGetMapped(ss[0].nav.bottom.bar) {
			s = ss[0]
		}
		return s != nil
	})
	onMain(func() {
		b := s.nav.bottom
		if len(b.packed) != 4 || WidgetGetParent(s.widget) != b.view {
			fatalf(t, "%d buttons; the screen's parent %v, the toolbar view %v", len(b.packed), WidgetGetParent(s.widget), b.view)
		}
		filter, sort, del, compose := b.packed[0], b.packed[1], b.packed[2], b.packed[3]
		if !WidgetHasCSSClass(compose, "suggested-action") || WidgetHasCSSClass(filter, "suggested-action") {
			fatalf(t, "Compose isn't the suggested action")
		}
		if WidgetGetSensitive(del) {
			fatalf(t, "Delete all is sensitive")
		}
		WidgetActivate(filter)
		WidgetActivate(compose)
		if !WidgetActivateAction(sort, "asukatool.item"+fmt.Sprint(ops.ScreenToolbarActions+2), 0) {
			fatalf(t, "no action for Date")
		}
	})
	settle(t, h, func() bool { return fmt.Sprint(picked) == "[filter compose date]" })
}
