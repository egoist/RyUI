//go:build windows

package windows

import (
	"testing"

	"github.com/egoist/asuka/internal/boot"
	"github.com/egoist/asuka/internal/l10n"
	. "github.com/egoist/asuka/internal/windows"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/tray"
	"github.com/egoist/asuka/ui"
)

// A tray icon: in the notification area with its picture and tooltip; a
// click is its OnClick, an item chosen in its menu (a submenu's too) its
// OnPress; disposed, it goes.
func TestTray(t *testing.T) {
	clicks, deep := 0, 0
	shown := signal.New(true)
	tip := signal.New("Power")
	h, _ := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			tray.Show(tray.Props{
				Symbol:  ui.Val("bolt.fill"),
				Tooltip: tip,
				OnClick: func() { clicks++ },
				Menu: ui.Val([]ui.MenuItem{
					{Title: "Open"},
					{Title: "More", Submenu: []ui.MenuItem{{Title: "Deep", OnPress: func() { deep++ }}}},
					ui.MenuQuit,
				}),
			})
			return ui.StaticText("x")
		})
	})
	onMain(t, func() {
		s := h.tray
		if s == nil || len(s.order) != 1 {
			fatalf("no tray icon")
		}
		ic := s.icons[s.order[0]]
		if !ic.ready || ic.ni.Icon == 0 || !ic.ownIcon || ic.ni.Tip != "Power" || ic.ni.Wnd != s.hwnd {
			fatalf("icon %+v", ic)
		}
		if flat := ops.FlattenMenu(ic.menu); len(flat) != 4 || flat[2].Title != "Deep" || flat[3].Role != ops.MenuRoleQuit {
			fatalf("menu %+v", flat)
		}
		// A click.
		SendMessage(s.hwnd, trayMessage, 0, MakeLong(NIN_SELECT, uint16(ic.id)))
		h.runPosted()
		if clicks != 1 {
			fatalf("clicks %d", clicks)
		}
		// The submenu's item, as the menu returns it.
		h.trayChose(ic, 2)
		h.runPosted()
		if deep != 1 {
			fatalf("Deep ran %d times", deep)
		}
		// Its menu, built as it's shown: the submenu a popup.
		b := &menuBuild{h: h, base: 1}
		m := b.popup(ic.menu)
		if MenuItemCount(m) != 3 || SubMenu(m, 1) == 0 || MenuString(SubMenu(m, 1), 0) != "&Deep" {
			fatalf("the menu: %d items", MenuItemCount(m))
		}
		if got, want := MenuString(m, 2), accessKeys([]string{"Open", "More", l10n.Quit(h.locale, h.appName)})[2]; got != want {
			fatalf("the quit item says %q, want %q", got, want)
		}
		DestroyMenu(m)
		// A change sets it again.
		h.enter(func() { ui.Post(func() { tip.Set("Charging") }) })
		h.runPosted()
		if ic.ni.Tip != "Charging" {
			fatalf("tooltip %q", ic.ni.Tip)
		}
		// Disposed, it goes.
		h.enter(func() { ui.Post(func() { shown.Set(false) }) })
		h.runPosted()
		if len(s.icons) != 0 || len(s.order) != 0 || ic.ni.Icon != 0 {
			fatalf("%d icons after dispose", len(s.icons))
		}
	})
}

// The Dock menu's items as jump list tasks: what the jump list can't show
// left out, separators only between tasks.
func TestJumpTasks(t *testing.T) {
	items := ops.DecodeMenu(ui.EncodeMenu([]ui.MenuItem{
		ui.MenuSeparator,
		{Title: "New Window"},
		ui.MenuSeparator,
		ui.MenuSeparator,
		{Title: "Status", Submenu: []ui.MenuItem{{Title: "Away"}}},
		{Title: "Off", Disabled: true},
		{Title: "Compose"},
		ui.MenuSeparator,
		ui.MenuQuit,
	}))
	tasks := jumpTasks(items, `C:\a\App.exe`)
	if len(tasks) != 3 || tasks[0].Title != "New Window" || tasks[0].Args != "--asuka-dock-item=1" ||
		!tasks[1].Separator || tasks[2].Title != "Compose" || tasks[2].Args != "--asuka-dock-item=7" || tasks[2].Path != `C:\a\App.exe` {
		t.Fatalf("tasks %+v", tasks)
	}
}

// A submenu in the menu bar: a popup of its menu, its items' commands
// and shortcuts the app's.
func TestMenuBarSubmenu(t *testing.T) {
	chose := ""
	h, _ := mount(t, func() ui.Node {
		return ui.MenuBar(signal.Static([]ui.MenuBarMenu{{Title: "File", Items: []ui.MenuItem{
			{Title: "Open Recent", Submenu: []ui.MenuItem{
				{Title: "a.txt", Shortcut: ui.Cmd("1"), OnPress: func() { chose = "a" }},
				{Title: "Clear", Destructive: true, OnPress: func() { chose = "clear" }},
			}},
			ui.MenuQuit,
		}}}))
	})
	onMain(t, func() {
		file := SubMenu(GetMenu(h.window), 0)
		recent := SubMenu(file, 0)
		if recent == 0 || MenuItemCount(recent) != 2 {
			fatalf("no submenu")
		}
		if s := MenuString(recent, 0); s != "&a.txt\tCtrl+1" {
			fatalf("item %q", s)
		}
		// Its destructive item: the submenu is owner-drawn.
		owned := false
		for _, om := range h.barOwned {
			owned = owned || om.menu == recent
		}
		if !owned {
			fatalf("the submenu with a destructive item isn't owner-drawn")
		}
		// a.txt is item 1 (Open Recent is 0).
		SendMessage(h.window, WM_COMMAND, menuBase+1, 0)
		if chose != "a" {
			fatalf("chose %q", chose)
		}
		// Its shortcut.
		chose = ""
		saved := KeyboardState()
		st := saved
		st[VK_CONTROL] = 0x80
		SetKeyboardState(st)
		m := Msg{Hwnd: h.window, Message: WM_KEYDOWN, WParam: '1'}
		h.preTranslate(&m)
		SetKeyboardState(saved)
		if chose != "a" {
			fatalf("Ctrl+1 chose %q", chose)
		}
		// The submenu's own item does nothing.
		chose = ""
		SendMessage(h.window, WM_COMMAND, menuBase+0, 0)
		if chose != "" {
			fatalf("the submenu's item chose %q", chose)
		}
	})
}

// An app that keeps running: closing its window hides it.
func TestKeepRunning(t *testing.T) {
	boot.KeepRunning = true
	defer func() { boot.KeepRunning = false }()
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		ShowWindow(h.window, SW_SHOWNOACTIVATE)
		SendMessage(h.window, WM_CLOSE, 0, 0)
		if !IsWindow(h.window) || IsWindowVisible(h.window) || h.quitting {
			fatalf("closed: window %v, visible %v, quitting %v", IsWindow(h.window), IsWindowVisible(h.window), h.quitting)
		}
	})
}
