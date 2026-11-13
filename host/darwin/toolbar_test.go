//go:build darwin && !ios

package darwin

import (
	"fmt"
	"testing"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/ui"
)

// toolbarItems returns the window's toolbar items by label.
func toolbarItems(h *Host) (map[string]ID, []string) {
	items := Send(h.main.chrome.toolbar, "items")
	byLabel := map[string]ID{}
	var labels []string
	for i := 0; i < int(SendInt(items, "count")); i++ {
		it := Send(items, "objectAtIndex:", uintptr(i))
		l := GoString(Send(it, "label"))
		if l == "" {
			continue
		}
		byLabel[l] = it
		labels = append(labels, l)
	}
	return byLabel, labels
}

// A screen's Toolbar and Primary are items of the window's toolbar after
// its bar buttons, the primary one last and prominent; each reports its
// own Action.
func TestScreenToolbarItems(t *testing.T) {
	var picked []string
	pick := func(s string) func() { return func() { picked = append(picked, s) } }
	h, _ := mount(t, func() ui.Node {
		return ui.Stack(func() ui.Node { return ui.StaticText("Inbox") }, ui.ScreenOptions{
			Title:   ui.Val("Inbox"),
			Actions: []ui.Action{{Title: ui.Val("Edit"), OnPress: pick("edit")}},
			Toolbar: []ui.Action{
				{Title: ui.Val("Filter"), Icon: "line.3.horizontal.decrease", OnPress: pick("filter")},
				{Title: ui.Val("Sort"), Icon: "arrow.up.arrow.down", Children: []ui.Action{
					{Title: ui.Val("Date"), OnPress: pick("date")},
					{Title: ui.Val("Sender"), OnPress: pick("sender")},
				}},
				{Title: ui.Val("Delete all"), Disabled: ui.Val(true), OnPress: pick("delete")},
			},
			Primary: &ui.Action{Title: ui.Val("Compose"), Icon: "square.and.pencil", OnPress: pick("compose")},
		})
	})
	onMain(func() {
		items, labels := toolbarItems(h)
		if fmt.Sprint(labels) != "[Edit Filter Sort Delete all Compose]" && fmt.Sprint(labels) != "[Compose Delete all Sort Filter Edit]" {
			fatalf(t, "toolbar %v", labels)
		}
		if SendBool(items["Delete all"], "isEnabled") {
			fatal(t, "Delete all is enabled")
		}
		compose := items["Compose"]
		if RespondsTo(compose, "style") && SendInt(compose, "style") != 1 {
			fatalf(t, "Compose's style %d, want prominent", SendInt(compose, "style"))
		}
		if RespondsTo(items["Filter"], "style") && SendInt(items["Filter"], "style") != 0 {
			fatal(t, "Filter is prominent")
		}
		h.enter(func() {
			h.barItem(items["Edit"])
			h.barItem(items["Filter"])
			h.barItem(compose)
			h.menuItem(Send(Send(items["Sort"], "menu"), "itemAtIndex:", 1))
		})
		if fmt.Sprint(picked) != "[edit filter compose sender]" {
			fatalf(t, "picked %v", picked)
		}
	})
}
