//go:build linux && !android

package linux

import (
	"strings"
	"testing"
	"time"

	"github.com/egoist/asuka/internal/boot"
	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
	"github.com/egoist/asuka/sdk/tray"
	"github.com/egoist/asuka/signal"
	"github.com/egoist/asuka/ui"
)

// busCall calls a method on the session bus from the test's connection
// and waits for the reply, as Go values.
func busCall(t *testing.T, dest, path, iface, method string, params func() Ptr) (any, error) {
	t.Helper()
	var r struct {
		v    any
		err  error
		done bool
	}
	onMain(func() {
		conn, err := Bus(BusSession)
		if err != nil {
			fatalf(t, "%v", err)
		}
		var p Ptr
		if params != nil {
			p = params()
		}
		DBusCall(conn, dest, path, iface, method, p, 5000, func(reply Ptr, err error) {
			r.v, r.err, r.done = VariantGo(reply), err, true
		})
	})
	for i := 0; i < 5000; i++ {
		var d bool
		onMain(func() { d = r.done })
		if d {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if !r.done {
		t.Fatalf("%s: no reply", method)
	}
	return r.v, r.err
}

const testWatcherXML = `<node><interface name="org.kde.StatusNotifierWatcher">
<method name="RegisterStatusNotifierItem"><arg type="s" direction="in"/></method>
<property name="IsStatusNotifierHostRegistered" type="b" access="read"/>
</interface></node>`

// startWatcher puts a StatusNotifierWatcher of the test's on the session
// bus, as a panel's: it records the items registered with it.
func startWatcher(t *testing.T) *[]string {
	registered := new([]string)
	onMain(func() {
		conn, err := Bus(BusSession)
		if err != nil {
			fatalf(t, "%v", err)
		}
		id, err := DBusRegister(conn, watcherPath, testWatcherXML, watcherName, DBusObject{
			Method: func(c DBusInvocation) {
				if v, _ := VariantGo(c.Params).([]any); len(v) == 1 {
					*registered = append(*registered, v[0].(string))
				}
				c.Return(0)
			},
			Property: func(name string) Ptr {
				if name == "IsStatusNotifierHostRegistered" {
					return VBool(true)
				}
				return 0
			},
		})
		if err != nil {
			fatalf(t, "registering the watcher: %v", err)
		}
		t.Cleanup(func() { onMain(func() { DBusUnregister(conn, id) }) })
	})
	if _, err := busCall(t, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "RequestName",
		func() Ptr { return VTuple(VStr(watcherName), VU32(4)) }); err != nil {
		t.Fatal("owning the watcher's name:", err)
	}
	t.Cleanup(func() {
		busCall(t, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "ReleaseName",
			func() Ptr { return VTuple(VStr(watcherName)) })
	})
	return registered
}

// A tray icon is a StatusNotifierItem registered with the watcher, its
// menu a dbusmenu with submenus; the panel's events reach the app, and
// it goes with its owner.
func TestTray(t *testing.T) {
	registered := startWatcher(t)
	shown := signal.New(true)
	tip := signal.New("Power")
	var clicks, opened int
	var shared string
	h, r := mount(t, func() ui.Node {
		return ui.Show(shown, func() ui.Node {
			tray.Show(tray.Props{
				Symbol:  ui.Val("bolt.fill"),
				Title:   ui.Val("42%"),
				Tooltip: tip,
				OnClick: func() { clicks++ },
				Menu: ui.Val([]ui.MenuItem{
					{Title: "Open_Power", OnPress: func() { opened++ }},
					{Title: "Share", Submenu: []ui.MenuItem{
						{Title: "Mail", OnPress: func() { shared = "mail" }},
						{Title: "Messages", Checked: true, OnPress: func() { shared = "messages" }},
					}},
					{Title: "Delete", Destructive: true},
					ui.MenuSeparator,
					ui.MenuQuit,
				}),
			})
			return emptyRoot()
		})
	})
	settle(t, h, func() bool { return len(*registered) == 1 })
	service := (*registered)[0]
	if !strings.HasPrefix(service, ":") {
		t.Fatalf("registered %q", service)
	}
	if b, err := callModule(t, h, "tray", "supported", nil); err != nil || len(b) != 1 || b[0] != 1 {
		t.Fatalf("supported: %v %v", b, err)
	}

	all, err := busCall(t, service, sniPath, "org.freedesktop.DBus.Properties", "GetAll", func() Ptr { return VTuple(VStr(sniIface)) })
	if err != nil {
		t.Fatal("GetAll:", err)
	}
	props := all.([]any)[0].(map[string]any)
	tooltip := props["ToolTip"].([]any)
	if props["IconName"] == "" || props["ItemIsMenu"] != false || props["Menu"] != sniMenuPath ||
		props["Status"] != "Active" || props["XAyatanaLabel"] != "42%" || tooltip[2] != "Power" {
		t.Fatalf("properties %v", props)
	}

	got, err := busCall(t, service, sniMenuPath, menuIface, "GetLayout", func() Ptr {
		return VTuple(VI32(0), VI32(-1), VStrings(nil))
	})
	if err != nil {
		t.Fatal("GetLayout:", err)
	}
	root := got.([]any)[1].([]any)
	kids := root[2].([]any)
	label := func(n any) string {
		s, _ := n.([]any)[1].(map[string]any)["label"].(string)
		return s
	}
	typ := func(n any) string {
		s, _ := n.([]any)[1].(map[string]any)["type"].(string)
		return s
	}
	// Open, Share, a separator setting Delete apart, Delete, the
	// separator, Quit.
	if len(kids) != 6 || label(kids[0]) != "Open__Power" || label(kids[1]) != "Share" || typ(kids[2]) != "separator" ||
		label(kids[3]) != "Delete" || typ(kids[4]) != "separator" || !strings.HasPrefix(label(kids[5]), "Quit") {
		t.Fatalf("layout %v", kids)
	}
	share := kids[1].([]any)
	sub := share[2].([]any)
	if share[1].(map[string]any)["children-display"] != "submenu" || len(sub) != 2 || label(sub[1]) != "Messages" ||
		sub[1].([]any)[1].(map[string]any)["toggle-state"] != int32(1) {
		t.Fatalf("submenu %v", share)
	}

	// The panel's click on items, and on the icon.
	event := func(id int32) {
		if _, err := busCall(t, service, sniMenuPath, menuIface, "Event", func() Ptr {
			return VTuple(VI32(id), VStr("clicked"), VVariant(VI32(0)), VU32(0))
		}); err != nil {
			t.Fatal("Event:", err)
		}
	}
	event(kids[0].([]any)[0].(int32))
	event(sub[1].([]any)[0].(int32))
	if _, err := busCall(t, service, sniPath, sniIface, "Activate", func() Ptr { return VTuple(VI32(0), VI32(0)) }); err != nil {
		t.Fatal("Activate:", err)
	}
	settle(t, h, func() bool { return opened == 1 && shared == "messages" && clicks == 1 })

	// A change is the item's again.
	onMain(func() { tip.Set("Charging"); r.Flush() })
	settle(t, h, func() bool { return anyTooltip(h, "Charging") })
	tt, err := busCall(t, service, sniPath, "org.freedesktop.DBus.Properties", "Get", func() Ptr { return VTuple(VStr(sniIface), VStr("ToolTip")) })
	if err != nil || tt.([]any)[0].([]any)[2] != "Charging" {
		t.Fatalf("ToolTip after a change: %v %v", tt, err)
	}

	// Disposed, its connection closes: its name goes.
	onMain(func() { shown.Set(false); r.Flush() })
	settle(t, h, func() bool { return len(h.tray.icons) == 0 })
	owned, err := busCall(t, "org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "NameHasOwner", func() Ptr { return VTuple(VStr(service)) })
	if err != nil || owned.([]any)[0] != false {
		t.Fatalf("after dispose, the item's name: %v %v", owned, err)
	}
}

func anyTooltip(h *Host, tip string) bool {
	for _, t := range h.tray.icons {
		if t.tooltip == tip {
			return true
		}
	}
	return false
}

// An Image is IconPixmap: ARGB32 in network order, at its size and
// smaller; a template is drawn in the style's ink.
func TestTrayPixmap(t *testing.T) {
	h := newHost()
	px := make([]byte, 32*32*4)
	for i := 0; i < len(px); i += 4 {
		px[i], px[i+1], px[i+2], px[i+3] = 255, 0, 0, 255
	}
	ic := &sni{pixels: &decoded{w: 32, h: 32, pixels: px}, flags: 1}
	h.dark = true
	var got any
	onMain(func() { got = VariantGo(h.sniPixmaps(ic)) })
	list := got.([]any)
	if len(list) != 4 { // 32, 24, 22, 16
		t.Fatalf("%d pixmaps", len(list))
	}
	first := list[0].([]any)
	data := first[2].([]any)
	if first[0] != int32(32) || first[1] != int32(32) || len(data) != 32*32*4 ||
		data[0] != uint8(255) || data[1] != uint8(255) || data[2] != uint8(255) {
		t.Fatalf("first pixmap %v %v, ARGB %v", first[0], first[1], data[:4])
	}
	last := list[3].([]any)
	if last[0] != int32(16) || len(last[2].([]any)) != 16*16*4 {
		t.Fatalf("last pixmap %v", last[:2])
	}
}

// The Dock menu is the launcher's quicklist: a dbusmenu that
// LauncherEntry's Update names.
func TestTrayQuicklist(t *testing.T) {
	var quicklist []string
	var sub uint32
	onMain(func() {
		conn, _ := Bus(BusSession)
		sub = DBusSubscribe(conn, "", launcherIface, "Update", "", func(s DBusSignal) {
			v, _ := VariantGo(s.Params).([]any)
			if len(v) == 2 {
				if p, ok := v[1].(map[string]any)["quicklist"].(string); ok {
					quicklist = append(quicklist, v[0].(string)+" "+p)
				}
			}
		})
	})
	t.Cleanup(func() { onMain(func() { conn, _ := Bus(BusSession); DBusUnsubscribe(conn, sub) }) })
	on := signal.New(true)
	chose := ""
	h, r := mount(t, func() ui.Node {
		return ui.Show(on, func() ui.Node {
			tray.DockMenu(ui.Val([]ui.MenuItem{{Title: "New Window", OnPress: func() { chose = "new" }}}))
			return emptyRoot()
		})
	})
	settle(t, h, func() bool { return len(quicklist) == 1 })
	if want := "application://" + h.appID + ".desktop " + quicklistPath; quicklist[0] != want {
		t.Fatalf("Update %q, want %q", quicklist[0], want)
	}
	var unique string
	onMain(func() { conn, _ := Bus(BusSession); unique = UniqueName(conn) })
	got, err := busCall(t, unique, quicklistPath, menuIface, "GetLayout", func() Ptr { return VTuple(VI32(0), VI32(-1), VStrings(nil)) })
	if err != nil {
		t.Fatal("GetLayout:", err)
	}
	kids := got.([]any)[1].([]any)[2].([]any)
	if len(kids) != 1 {
		t.Fatalf("quicklist %v", kids)
	}
	if _, err := busCall(t, unique, quicklistPath, menuIface, "Event", func() Ptr {
		return VTuple(VI32(kids[0].([]any)[0].(int32)), VStr("clicked"), VVariant(VI32(0)), VU32(0))
	}); err != nil {
		t.Fatal("Event:", err)
	}
	settle(t, h, func() bool { return chose == "new" })
	onMain(func() { on.Set(false); r.Flush() })
	settle(t, h, func() bool { return len(quicklist) == 2 })
	if !strings.HasSuffix(quicklist[1], " /") {
		t.Fatalf("cleared: %q", quicklist[1])
	}
}

// The app menu's submenus are GMenu submenus; their items are chosen as
// any, and a Quit item is the app's, with Ctrl+Q.
func TestMenuBarSubmenu(t *testing.T) {
	chose := ""
	h, _ := mount(t, func() ui.Node {
		return ui.Column(ui.Style{},
			ui.MenuBar(ui.Val([]ui.MenuBarMenu{
				{Title: "File", Items: []ui.MenuItem{
					{Title: "Open Recent", Submenu: []ui.MenuItem{
						{Title: "a.txt", OnPress: func() { chose = "a" }},
						{Title: "More", Submenu: []ui.MenuItem{{Title: "b.txt", Shortcut: ui.Cmd("b"), OnPress: func() { chose = "b" }}}},
					}},
					ui.MenuQuit,
				}},
				{Title: "View", Items: []ui.MenuItem{{Title: "Zoom", OnPress: func() { chose = "zoom" }}}},
			})),
		)
	})
	settle(t, h, func() bool { return len(h.appItems) == 6 })
	onMain(func() {
		if err := h.activateAppItem("b.txt"); err != nil {
			fatalf(t, "%v", err)
		}
	})
	settle(t, h, func() bool { return chose == "b" })
	onMain(func() {
		if err := h.activateAppItem("Open Recent"); err == nil {
			fatalf(t, "a submenu's item was chosen")
		}
		accels := func(i int) string {
			name := CString("app.asuka-menu-" + itoa(i))
			list := Call(Sym("gtk_application_get_accels_for_action"), h.app, Addr(name))
			defer Call(Sym("g_strfreev"), list)
			if list == 0 || *At[Ptr](list) == 0 {
				return ""
			}
			return GoString(*At[Ptr](list))
		}
		if a := accels(3); a != "<Control>b" {
			fatalf(t, "b.txt's accelerator %q", a)
		}
		if a := accels(4); a != "<Control>q" {
			fatalf(t, "Quit's accelerator %q", a)
		}
		quit := h.appItems[4]
		if quit.Role != ops.MenuRoleQuit || !strings.HasPrefix(h.menuItemTitle(&quit), "Quit") {
			fatalf(t, "quit item %+v", quit)
		}
		// The model: File's first item is a submenu.
		file := Call(Sym("g_menu_model_get_item_link"), h.appMenu, 0, Addr(CString("submenu")))
		if file == 0 {
			fatalf(t, "no File submenu")
		}
	})
}

func itoa(i int) string {
	s := ""
	if i == 0 {
		return "0"
	}
	for ; i > 0; i /= 10 {
		s = string(rune('0'+i%10)) + s
	}
	return s
}

// An app that keeps running hides its main window as it's closed, and
// is in the background.
func TestKeepRunning(t *testing.T) {
	boot.KeepRunning = true
	defer func() { boot.KeepRunning = false }()
	h, _ := mount(t, emptyRoot)
	onMain(func() { WindowClose(h.main.window) })
	settle(t, h, func() bool { return !WidgetGetVisible(h.main.window) && h.inBackground })
	onMain(func() {
		if h.main.closing {
			fatalf(t, "the main window was destroyed")
		}
	})
}
