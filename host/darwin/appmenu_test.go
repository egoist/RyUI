package darwin

import (
	"testing"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// A menu titled as the app adds its items to the app's menu, after About:
// "Check for Updates…" (asuka/update's AppMenu).
func TestMenuBarAppMenu(t *testing.T) {
	defer func(n string) { boot.Name = n }(boot.Name)
	boot.Name = "Notes"
	checked := 0
	h, _ := mount(t, func() ui.Node {
		return ui.MenuBar(signal.Static([]ui.MenuBarMenu{
			{Title: "Notes", Items: []ui.MenuItem{{Title: "Check for Updates…", OnPress: func() { checked++ }}}},
		}))
	})
	onMain(func() {
		bar := Send(Send(Class("NSApplication"), "sharedApplication"), "mainMenu")
		app := Send(Send(bar, "itemAtIndex:", 0), "submenu")
		if n := SendInt(bar, "numberOfItems"); n == 0 || app == 0 {
			fatal(t, "no app menu")
		}
		if title := GoString(Send(Send(app, "itemAtIndex:", 1), "title")); title != "Check for Updates…" {
			fatalf(t, "the app menu's second item is %q", title)
		}
		for i := range SendInt(bar, "numberOfItems") {
			if GoString(Send(Send(bar, "itemAtIndex:", uintptr(i)), "title")) == "Notes" && i != 0 {
				fatal(t, "the app's menu shows as a menu of its own too")
			}
		}
		item := Send(app, "itemAtIndex:", 1)
		Send(Send(item, "target"), "appMenuItem:", uintptr(item))
		if checked != 1 {
			fatal(t, "the item didn't run")
		}
		_ = h
	})
}
