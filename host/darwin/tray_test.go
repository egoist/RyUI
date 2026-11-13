//go:build darwin && !ios

package darwin

import (
	"testing"

	"github.com/egoist/asuka/internal/boot"

	. "github.com/egoist/asuka/internal/darwin"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/tray"
	"github.com/egoist/asuka/ui"
)

// A tray icon is a status item in the menu bar: its symbol a template
// image, its title, tooltip and menu (with a submenu, a destructive item
// and Quit); a click is OnClick, a chosen item its OnPress; disposed, it
// leaves the menu bar.
func TestTray(t *testing.T) {
	shown := signal.New(true)
	speed := signal.New("1.0 MB/s")
	clicks, chose := 0, ""
	h, _ := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			tray.Show(tray.Props{
				Symbol:  ui.Val("bolt.fill"),
				Title:   speed,
				Tooltip: ui.Val("Power"),
				OnClick: func() { clicks++ },
				Menu: ui.Val([]ui.MenuItem{
					{Title: "Open", OnPress: func() { chose = "open" }},
					{Title: "Mode", Submenu: []ui.MenuItem{
						{Title: "Fast", Checked: true, OnPress: func() { chose = "fast" }},
						{Title: "Slow", Disabled: true},
					}},
					{Title: "Reset", Destructive: true},
					ui.MenuSeparator,
					ui.MenuQuit,
				}),
			})
			return ui.StaticText("x")
		})
	})
	onMain(func() {
		if len(trayState.icons) != 1 {
			fatalf(t, "%d icons", len(trayState.icons))
		}
		var ic *trayIcon
		for _, x := range trayState.icons {
			ic = x
		}
		button := Send(ic.item, "button")
		img := Send(button, "image")
		if img == 0 || !SendBool(img, "isTemplate") {
			fatal(t, "no template image")
		}
		if s := GoString(Send(button, "title")); s != "1.0 MB/s" {
			fatalf(t, "title %q", s)
		}
		if s := GoString(Send(button, "toolTip")); s != "Power" {
			fatalf(t, "tooltip %q", s)
		}
		// With OnClick, the menu isn't the item's own (a click is the app's).
		if Send(ic.item, "menu") != 0 || ic.menu == 0 {
			fatal(t, "the menu opens on a click")
		}
		m := ic.menu
		if n := SendInt(m, "numberOfItems"); n != 5 {
			fatalf(t, "%d items", n)
		}
		mode := Send(Send(m, "itemAtIndex:", 1), "submenu")
		if mode == 0 || SendInt(mode, "numberOfItems") != 2 {
			fatal(t, "no Mode submenu")
		}
		fast, slow := Send(mode, "itemAtIndex:", 0), Send(mode, "itemAtIndex:", 1)
		if SendInt(fast, "state") != 1 || SendBool(slow, "isEnabled") {
			fatal(t, "Fast isn't checked, or Slow isn't off")
		}
		if Send(Send(m, "itemAtIndex:", 2), "attributedTitle") == 0 {
			fatal(t, "Reset isn't red")
		}
		quit := Send(m, "itemAtIndex:", 4)
		if GoString(Send(quit, "title")) == "" || SEL(Send(quit, "action")) != Sel("terminate:") {
			fatal(t, "no Quit")
		}

		// A click, then a submenu's item, as AppKit sends them.
		Send(Send(button, "target"), "trayClicked:", uintptr(button))
		h.runPosted()
		Send(Send(fast, "target"), "trayMenuItem:", uintptr(fast))
		h.runPosted()
		if clicks != 1 || chose != "fast" {
			fatalf(t, "clicks %d, chose %q", clicks, chose)
		}

		// The title follows its signal.
		h.enter(func() { ui.Post(func() { speed.Set("12.5 MB/s") }) })
		h.runPosted()
		if s := GoString(Send(button, "title")); s != "12.5 MB/s" {
			fatalf(t, "title %q after a change", s)
		}

		// Disposed, it leaves the menu bar.
		h.enter(func() { ui.Post(func() { shown.Set(false) }) })
		h.runPosted()
		if len(trayState.icons) != 0 {
			fatal(t, "the icon stayed")
		}
	})
}

// A menu bar's submenu is an NSMenu of its own, its items' shortcuts key
// equivalents and their numbers the core's; Quit is AppKit's terminate:.
func TestMenuBarSubmenu(t *testing.T) {
	chose := ""
	h, _ := mount(t, func() ui.Node {
		return ui.MenuBar(signal.Static([]ui.MenuBarMenu{
			{Title: "Tools", Items: []ui.MenuItem{
				{Title: "Export", Submenu: []ui.MenuItem{
					{Title: "PDF", Shortcut: ui.Cmd("e"), OnPress: func() { chose = "pdf" }},
					{Title: "Off", Disabled: true},
				}},
				ui.MenuQuit,
			}},
		}))
	})
	onMain(func() {
		bar := Send(Send(Class("NSApplication"), "sharedApplication"), "mainMenu")
		tools := Send(Send(bar, "itemWithTitle:", uintptr(String("Tools"))), "submenu")
		export := Send(Send(tools, "itemWithTitle:", uintptr(String("Export"))), "submenu")
		if export == 0 {
			fatal(t, "no Export submenu")
		}
		pdf := Send(export, "itemWithTitle:", uintptr(String("PDF")))
		if GoString(Send(pdf, "keyEquivalent")) != "e" || SendInt(pdf, "tag") != 1 {
			fatal(t, "PDF has no ⌘E, or isn't item 1")
		}
		Send(Send(pdf, "target"), "appMenuItem:", uintptr(pdf))
		if chose != "pdf" {
			fatalf(t, "chose %q", chose)
		}
		if off := Send(export, "itemWithTitle:", uintptr(String("Off"))); h.validateMenuItem(off) {
			fatal(t, "Off is on")
		}
		quit := Send(tools, "itemAtIndex:", 1)
		if SEL(Send(quit, "action")) != Sel("terminate:") || GoString(Send(quit, "keyEquivalent")) != "q" {
			fatal(t, "Quit isn't terminate: with ⌘Q")
		}
	})
}

// Without OnClick, the menu is the status item's own: AppKit opens it on
// any click.
func TestTrayMenuOnClick(t *testing.T) {
	mount(t, func() ui.Node {
		tray.Show(tray.Props{Symbol: ui.Val("star"), Menu: ui.Val([]ui.MenuItem{{Title: "Open"}})})
		return ui.StaticText("x")
	})
	onMain(func() {
		for _, ic := range trayState.icons {
			if Send(ic.item, "menu") == 0 {
				fatal(t, "no menu of the item's")
			}
		}
	})
}

// The Dock menu: the application delegate's applicationDockMenu: gives
// the app's items, a chosen one its OnPress.
func TestDockMenu(t *testing.T) {
	chose := ""
	h, _ := mount(t, func() ui.Node {
		tray.DockMenu(ui.Val([]ui.MenuItem{
			{Title: "New Window", OnPress: func() { chose = "new" }},
			{Title: "Status", Submenu: []ui.MenuItem{{Title: "Away", OnPress: func() { chose = "away" }}}},
		}))
		return ui.StaticText("x")
	})
	onMain(func() {
		app := Send(Class("NSApplication"), "sharedApplication")
		m := Send(h.target, "applicationDockMenu:", uintptr(app))
		if m == 0 || SendInt(m, "numberOfItems") != 2 {
			fatal(t, "no Dock menu")
		}
		away := Send(Send(Send(m, "itemAtIndex:", 1), "submenu"), "itemAtIndex:", 0)
		if GoString(Send(away, "title")) != "Away" {
			fatal(t, "no Away in Status")
		}
		Send(Send(away, "target"), "dockMenuItem:", uintptr(away))
		h.runPosted()
		if chose != "away" {
			fatalf(t, "chose %q", chose)
		}
	})
}

// An app that keeps running doesn't quit with its last window; with its
// main window closed, its Dock icon brings the window back.
func TestKeepRunning(t *testing.T) {
	boot.KeepRunning = true
	defer func() { boot.KeepRunning = false }()
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(func() {
		app := Send(Class("NSApplication"), "sharedApplication")
		if SendBool(h.target, "applicationShouldTerminateAfterLastWindowClosed:", uintptr(app)) {
			fatal(t, "quits with its last window")
		}
		Send(h.window, "orderFront:", 0)
		Send(h.window, "performClose:", 0)
		if SendBool(h.window, "isVisible") {
			fatal(t, "the window didn't close")
		}
		Send(h.target, "applicationShouldHandleReopen:hasVisibleWindows:", uintptr(app), 0)
		if !SendBool(h.window, "isVisible") {
			fatal(t, "the Dock icon didn't bring the window back")
		}
	})
	boot.KeepRunning = false
	onMain(func() {
		app := Send(Class("NSApplication"), "sharedApplication")
		if !SendBool(h.target, "applicationShouldTerminateAfterLastWindowClosed:", uintptr(app)) {
			fatal(t, "keeps running without KeepRunning")
		}
	})
}
