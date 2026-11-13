//go:build linux && !android

package linux

import (
	"encoding/binary"
	"errors"
	"hash/fnv"
	"runtime"
	"strconv"
	"strings"

	. "github.com/egoist/asuka/internal/gtk"
	"github.com/egoist/asuka/ops"
)

// asuka/tray on Linux. GTK 4 has no tray, and the GTK 3 appindicator
// libraries can't load in a GTK 4 process, so the host speaks the
// protocol they speak itself, through GDBus:
//
//   - Each icon is a StatusNotifierItem (org.kde.StatusNotifierItem) on
//     a session bus connection of its own, at /StatusNotifierItem, with
//     its menu a com.canonical.dbusmenu object at /MenuBar on the same
//     connection; it's registered with org.kde.StatusNotifierWatcher
//     (again when the watcher's name gets a new owner, as the panel
//     restarts). Removing it closes its connection: its unique name goes,
//     which every watcher follows (an object going off a connection that
//     stays isn't seen by all of them).
//   - Its icon is IconName, the icon theme's name for a Symbol (with
//     IconThemePath the host's directory of Material symbols), or
//     IconPixmap, an Image's pixels (ARGB32, network order) at its size
//     and smaller ones; a Template image is white while libadwaita's
//     style is dark, black otherwise. ToolTip is its tooltip, and
//     XAyatanaLabel its Title (GNOME's AppIndicator extension shows it).
//   - ItemIsMenu is true without OnClick, so a click opens the menu;
//     with it, Activate is the click, and the panel opens the menu on a
//     secondary click (ContextMenu).
//   - The Dock menu is the launcher's quicklist: a dbusmenu object on the
//     app's session bus connection, named by com.canonical.Unity.
//     LauncherEntry's Update signal for the app's desktop file
//     (application://<id>.desktop), which Ubuntu's Dock, Dash to Dock,
//     Plank and KDE's task manager follow.
//
// The module's methods are tray/tray.go's.

const (
	sniPath     = "/StatusNotifierItem"
	sniMenuPath = "/MenuBar"
	sniIface    = "org.kde.StatusNotifierItem"
	menuIface   = "com.canonical.dbusmenu"
	watcherName = "org.kde.StatusNotifierWatcher"
	watcherPath = "/StatusNotifierWatcher"

	quicklistPath = "/org/asuka/Quicklist"
	launcherPath  = "/org/asuka/LauncherEntry"
	launcherIface = "com.canonical.Unity.LauncherEntry"
)

const sniXML = `<node><interface name="org.kde.StatusNotifierItem">
<property name="Category" type="s" access="read"/>
<property name="Id" type="s" access="read"/>
<property name="Title" type="s" access="read"/>
<property name="Status" type="s" access="read"/>
<property name="WindowId" type="i" access="read"/>
<property name="IconThemePath" type="s" access="read"/>
<property name="IconName" type="s" access="read"/>
<property name="IconPixmap" type="a(iiay)" access="read"/>
<property name="OverlayIconName" type="s" access="read"/>
<property name="OverlayIconPixmap" type="a(iiay)" access="read"/>
<property name="AttentionIconName" type="s" access="read"/>
<property name="AttentionIconPixmap" type="a(iiay)" access="read"/>
<property name="AttentionMovieName" type="s" access="read"/>
<property name="ToolTip" type="(sa(iiay)ss)" access="read"/>
<property name="ItemIsMenu" type="b" access="read"/>
<property name="Menu" type="o" access="read"/>
<property name="XAyatanaLabel" type="s" access="read"/>
<property name="XAyatanaLabelGuide" type="s" access="read"/>
<method name="ContextMenu"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
<method name="Activate"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
<method name="SecondaryActivate"><arg name="x" type="i" direction="in"/><arg name="y" type="i" direction="in"/></method>
<method name="Scroll"><arg name="delta" type="i" direction="in"/><arg name="orientation" type="s" direction="in"/></method>
<method name="ProvideXdgActivationToken"><arg name="token" type="s" direction="in"/></method>
<signal name="NewTitle"/><signal name="NewIcon"/><signal name="NewAttentionIcon"/><signal name="NewOverlayIcon"/>
<signal name="NewToolTip"/><signal name="NewStatus"><arg type="s"/></signal><signal name="NewMenu"/>
<signal name="XAyatanaNewLabel"><arg type="s"/><arg type="s"/></signal>
</interface></node>`

const dbusMenuXML = `<node><interface name="com.canonical.dbusmenu">
<property name="Version" type="u" access="read"/>
<property name="TextDirection" type="s" access="read"/>
<property name="Status" type="s" access="read"/>
<property name="IconThemePath" type="as" access="read"/>
<method name="GetLayout"><arg type="i" direction="in"/><arg type="i" direction="in"/><arg type="as" direction="in"/><arg type="u" direction="out"/><arg type="(ia{sv}av)" direction="out"/></method>
<method name="GetGroupProperties"><arg type="ai" direction="in"/><arg type="as" direction="in"/><arg type="a(ia{sv})" direction="out"/></method>
<method name="GetProperty"><arg type="i" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="out"/></method>
<method name="Event"><arg type="i" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="in"/><arg type="u" direction="in"/></method>
<method name="EventGroup"><arg type="a(isvu)" direction="in"/><arg type="ai" direction="out"/></method>
<method name="AboutToShow"><arg type="i" direction="in"/><arg type="b" direction="out"/></method>
<method name="AboutToShowGroup"><arg type="ai" direction="in"/><arg type="ai" direction="out"/><arg type="ai" direction="out"/></method>
<signal name="ItemsPropertiesUpdated"><arg type="a(ia{sv})"/><arg type="a(ias)"/></signal>
<signal name="LayoutUpdated"><arg type="u"/><arg type="i"/></signal>
<signal name="ItemActivationRequested"><arg type="i"/><arg type="u"/></signal>
</interface></node>`

// What "next" reports (tray/tray.go).
const (
	trayClick = 1
	trayItem  = 2
)

// trayState is the module's: the icons, the Dock menu, and what happened
// that Go hasn't asked for yet.
type trayState struct {
	icons    map[uint32]*sni
	dock     *dbusMenu
	waiting  func([]byte, error)
	queue    [][]byte
	watching uint32 // the NameOwnerChanged subscription
}

// sni is a tray icon: a StatusNotifierItem and its menu.
type sni struct {
	id      uint32
	conn    Ptr // its own session bus connection
	reg     uint32
	menu    *dbusMenu
	flags   byte // 1 template, 2 clickable
	image   string
	symbol  string
	title   string
	tooltip string
	name    string // the app's ID for it

	iconName string
	pixels   *decoded // the Image's, once decoded
	loading  string   // the Image being decoded
}

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) { m["tray"] = h.trayModule() })
}

func (h *Host) trays() *trayState {
	if h.tray == nil {
		h.tray = &trayState{icons: map[uint32]*sni{}}
	}
	return h.tray
}

func (h *Host) trayModule() map[string]method {
	return map[string]method{
		"supported": func(_ []byte, reply func([]byte, error)) {
			conn, err := Bus(BusSession)
			if err != nil {
				reply([]byte{0}, nil)
				return
			}
			DBusCall(conn, watcherName, watcherPath, "org.freedesktop.DBus.Properties", "Get",
				VTuple(VStr(watcherName), VStr("IsStatusNotifierHostRegistered")), 2000, func(r Ptr, err error) {
					ok := false
					if err == nil {
						if v, _ := VariantGo(r).([]any); len(v) == 1 {
							ok, _ = v[0].(bool)
						}
					}
					reply([]byte{b2u(ok)}, nil)
				})
		},
		"set": func(args []byte, reply func([]byte, error)) {
			reply(nil, h.traySet(args))
		},
		"remove": func(args []byte, reply func([]byte, error)) {
			if len(args) >= 4 {
				h.trayRemove(binary.LittleEndian.Uint32(args))
			}
			reply(nil, nil)
		},
		"dock": func(args []byte, reply func([]byte, error)) {
			reply(nil, h.setDockMenu(ops.DecodeMenu(args)))
		},
		"next": func(_ []byte, reply func([]byte, error)) {
			s := h.trays()
			if len(s.queue) > 0 {
				e := s.queue[0]
				s.queue = s.queue[1:]
				reply(e, nil)
				return
			}
			s.waiting = reply
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			s := h.trays()
			if w := s.waiting; w != nil {
				s.waiting = nil
				w(nil, errors.New("stopped"))
			}
			s.queue = nil
			reply(nil, nil)
		},
	}
}

// trayEvent hands Go what happened: to the "next" waiting, or queued.
func (h *Host) trayEvent(id uint32, kind byte, index int) {
	b := binary.LittleEndian.AppendUint32(nil, id)
	b = append(b, kind)
	b = binary.LittleEndian.AppendUint32(b, uint32(index))
	s := h.trays()
	if w := s.waiting; w != nil {
		s.waiting = nil
		w(b, nil)
		return
	}
	s.queue = append(s.queue, b)
}

// traySet makes an icon, or changes one.
func (h *Host) traySet(args []byte) error {
	if len(args) < 5 {
		return errors.New("tray.set: bad arguments")
	}
	r := reader{b: args}
	id := r.u32()
	flags := r.u8()
	image, symbol, title, tooltip, name := r.str(), r.str(), r.str(), r.str(), r.str()
	menu := ops.DecodeMenu(r.b[r.off:])
	s := h.trays()
	t := s.icons[id]
	created := t == nil
	if created {
		t = &sni{id: id}
		if err := h.trayOpen(t); err != nil {
			return err
		}
		s.icons[id] = t
		h.watchWatcher()
	}
	old := *t
	t.flags, t.image, t.symbol, t.title, t.tooltip, t.name = flags, image, symbol, title, tooltip, name
	t.iconName = ""
	if t.image == "" {
		t.iconName = h.iconName(t.symbol)
		t.pixels = nil
	} else if t.image != old.image || (old.pixels == nil && t.loading != t.image) {
		t.pixels = nil
		h.trayLoadImage(t)
	}
	t.menu.set(menu)
	if created {
		h.trayRegister(t)
		return nil
	}
	if t.image != old.image || t.symbol != old.symbol || t.flags&1 != old.flags&1 {
		t.emit("NewIcon")
	}
	if t.tooltip != old.tooltip {
		t.emit("NewToolTip")
	}
	if t.title != old.title {
		DBusEmit(t.conn, sniPath, sniIface, "XAyatanaNewLabel", VTuple(VStr(t.title), VStr(t.title)))
	}
	return nil
}

// trayOpen connects the icon to the session bus on a connection of its
// own and exports its item and menu there.
func (h *Host) trayOpen(t *sni) error {
	conn, err := privateSessionBus()
	if err != nil {
		return err
	}
	t.conn = conn
	t.menu = &dbusMenu{h: h, conn: conn, path: sniMenuPath, click: func(it *ops.MenuItem) {
		if it.Role == ops.MenuRoleQuit {
			h.afterCore(h.quit)
			return
		}
		h.trayEvent(t.id, trayItem, it.Index)
	}}
	if err := t.menu.export(); err != nil {
		closeBus(conn)
		return err
	}
	reg, err := DBusRegister(conn, sniPath, sniXML, sniIface, DBusObject{
		Method:   func(c DBusInvocation) { h.sniMethod(t, c) },
		Property: func(name string) Ptr { return h.sniProperty(t, name) },
	})
	if err != nil {
		t.menu.unexport()
		closeBus(conn)
		return err
	}
	t.reg = reg
	return nil
}

// trayRegister tells the watcher (the panel's) of the icon, if one runs.
func (h *Host) trayRegister(t *sni) {
	DBusCall(t.conn, watcherName, watcherPath, watcherName, "RegisterStatusNotifierItem",
		VTuple(VStr(UniqueName(t.conn))), 5000, func(Ptr, error) {})
}

// watchWatcher registers the icons again with each new watcher (the
// panel restarting, the AppIndicator extension turned on).
func (h *Host) watchWatcher() {
	s := h.trays()
	if s.watching != 0 {
		return
	}
	conn, err := Bus(BusSession)
	if err != nil {
		return
	}
	s.watching = DBusSubscribe(conn, "org.freedesktop.DBus", "org.freedesktop.DBus", "NameOwnerChanged", "/org/freedesktop/DBus", func(sig DBusSignal) {
		v, _ := VariantGo(sig.Params).([]any)
		if len(v) == 3 && v[0] == watcherName && v[2] != "" {
			for _, t := range h.trays().icons {
				h.trayRegister(t)
			}
		}
	})
}

// trayRemove takes an icon away: its connection closes, and the watcher
// sees its name go.
func (h *Host) trayRemove(id uint32) {
	s := h.trays()
	t := s.icons[id]
	if t == nil {
		return
	}
	delete(s.icons, id)
	DBusUnregister(t.conn, t.reg)
	t.menu.unexport()
	closeBus(t.conn)
	t.conn = 0
}

func (t *sni) emit(signal string) {
	if t.conn != 0 {
		DBusEmit(t.conn, sniPath, sniIface, signal, 0)
	}
}

// sniMethod answers the panel: Activate is a click, for an icon that
// takes them; the rest the panel does itself (the menu, through
// dbusmenu).
func (h *Host) sniMethod(t *sni, c DBusInvocation) {
	switch c.Method {
	case "Activate":
		if t.flags&2 != 0 {
			h.trayEvent(t.id, trayClick, 0)
		}
	}
	c.Return(0)
}

func (h *Host) sniProperty(t *sni, name string) Ptr {
	switch name {
	case "Category":
		return VStr("ApplicationStatus")
	case "Id":
		id := h.appID
		switch {
		case t.name != "":
			id += "-" + t.name
		case t.id > 1:
			id += "-" + strconv.Itoa(int(t.id))
		}
		return VStr(id)
	case "Title":
		return VStr(h.appName)
	case "Status":
		return VStr("Active")
	case "WindowId":
		return VI32(0)
	case "IconThemePath":
		return VStr(iconDir)
	case "IconName":
		return VStr(t.iconName)
	case "IconPixmap":
		return h.sniPixmaps(t)
	case "OverlayIconName", "AttentionIconName", "AttentionMovieName":
		return VStr("")
	case "OverlayIconPixmap", "AttentionIconPixmap":
		return VArray("(iiay)")
	case "ToolTip":
		return VTuple(VStr(""), VArray("(iiay)"), VStr(t.tooltip), VStr(""))
	case "ItemIsMenu":
		return VBool(t.flags&2 == 0)
	case "Menu":
		return VPath(sniMenuPath)
	case "XAyatanaLabel", "XAyatanaLabelGuide":
		return VStr(t.title)
	}
	return 0
}

// trayLoadImage decodes the icon's Image off the main thread.
func (h *Host) trayLoadImage(t *sni) {
	src, id := t.image, t.id
	t.loading = src
	go func() {
		d := decodeImage(src, h.assets)
		h.Post(func() {
			t := h.trays().icons[id]
			if t == nil || t.image != src {
				return
			}
			t.loading = ""
			if d.err != nil || d.pixels == nil {
				println("asuka: tray icon", src+":", errString(d.err, "can't decode it"))
				return
			}
			t.pixels = d
			t.emit("NewIcon")
		})
	}()
}

func errString(err error, otherwise string) string {
	if err != nil {
		return err.Error()
	}
	return otherwise
}

// sniPixmaps is the Image at its size (at most 128 pixels tall) and
// smaller ones panels draw at, as ARGB32 in network byte order; a
// template's color the desktop's style's.
func (h *Host) sniPixmaps(t *sni) Ptr {
	d := t.pixels
	if d == nil {
		return VArray("(iiay)")
	}
	var items []Ptr
	seen := map[int32]bool{}
	for _, size := range []int32{min(d.h, 128), 48, 32, 24, 22, 16} {
		if size > d.h || size <= 0 || seen[size] {
			continue
		}
		seen[size] = true
		w, hh, px := scaleRGBA(d.pixels, d.w, d.h, size)
		argb := make([]byte, len(px))
		var ink byte
		if h.dark {
			ink = 255
		}
		for i := 0; i < len(px); i += 4 {
			r, g, b, a := px[i], px[i+1], px[i+2], px[i+3]
			if t.flags&1 != 0 {
				r, g, b = ink, ink, ink
			}
			argb[i], argb[i+1], argb[i+2], argb[i+3] = a, r, g, b
		}
		items = append(items, VTuple(VI32(w), VI32(hh), vByteArray(argb)))
	}
	return VArray("(iiay)", items...)
}

// scaleRGBA scales NRGBA pixels to height size, the aspect kept, each
// pixel the mean of those it covers (weighted by alpha).
func scaleRGBA(px []byte, w, h, size int32) (int32, int32, []byte) {
	if size == h {
		return w, h, px
	}
	nw := max(1, int32(int64(w)*int64(size)/int64(h)))
	out := make([]byte, int(nw)*int(size)*4)
	for y := int32(0); y < size; y++ {
		y0, y1 := y*h/size, max((y+1)*h/size, y*h/size+1)
		for x := int32(0); x < nw; x++ {
			x0, x1 := x*w/nw, max((x+1)*w/nw, x*w/nw+1)
			var r, g, b, a, n uint32
			for sy := y0; sy < y1 && sy < h; sy++ {
				for sx := x0; sx < x1 && sx < w; sx++ {
					i := (int(sy)*int(w) + int(sx)) * 4
					pa := uint32(px[i+3])
					r += uint32(px[i]) * pa
					g += uint32(px[i+1]) * pa
					b += uint32(px[i+2]) * pa
					a += pa
					n++
				}
			}
			o := (int(y)*int(nw) + int(x)) * 4
			if a > 0 {
				out[o], out[o+1], out[o+2] = byte(r/a), byte(g/a), byte(b/a)
			}
			if n > 0 {
				out[o+3] = byte(a / n)
			}
		}
	}
	return nw, size, out
}

// vByteArray is an "ay" of b, copied.
func vByteArray(b []byte) Ptr {
	if len(b) == 0 {
		return VArray("y")
	}
	y := CString("y") // a GVariantType is its type string
	v := Call(Sym("g_variant_new_fixed_array"), Addr(y), Addr(&b[0]), uintptr(len(b)), 1)
	runtime.KeepAlive(y)
	runtime.KeepAlive(b)
	return v
}

// trayStyleChanged draws template images again in the desktop's style.
func (h *Host) trayStyleChanged() {
	if h.tray == nil {
		return
	}
	for _, t := range h.tray.icons {
		if t.flags&1 != 0 && t.pixels != nil {
			t.emit("NewIcon")
		}
	}
}

// setDockMenu sets the launcher's quicklist: a dbusmenu of the items,
// announced with LauncherEntry's Update (an empty menu: none).
func (h *Host) setDockMenu(items []ops.MenuItem) error {
	conn, err := Bus(BusSession)
	if err != nil {
		return err
	}
	s := h.trays()
	if len(items) == 0 {
		if s.dock == nil {
			return nil
		}
		s.dock.unexport()
		s.dock = nil
		h.launcherUpdate(conn, "/")
		return nil
	}
	if s.dock == nil {
		d := &dbusMenu{h: h, conn: conn, path: quicklistPath, click: func(it *ops.MenuItem) {
			if it.Role == ops.MenuRoleQuit {
				h.afterCore(h.quit)
				return
			}
			h.trayEvent(0, trayItem, it.Index)
		}}
		if err := d.export(); err != nil {
			return err
		}
		s.dock = d
	}
	s.dock.set(items)
	h.launcherUpdate(conn, quicklistPath)
	return nil
}

// launcherUpdate tells the dock of the quicklist ("/": none).
func (h *Host) launcherUpdate(conn Ptr, path string) {
	f := fnv.New32a()
	f.Write([]byte(h.appID))
	p := launcherPath + "/q" + strconv.FormatUint(uint64(f.Sum32()), 16)
	uri := "application://" + h.appID + ".desktop"
	DBusEmit(conn, p, launcherIface, "Update", VTuple(VStr(uri), VDict(map[string]Ptr{"quicklist": VStr(path)})))
}

// privateSessionBus is a new connection to the session bus, the icon's
// own (g_bus_get's is shared).
func privateSessionBus() (Ptr, error) {
	var gerr Ptr
	addr := Call(Sym("g_dbus_address_get_for_bus_sync"), BusSession, 0, Addr(&gerr))
	if addr == 0 {
		return 0, GError(gerr)
	}
	// G_DBUS_CONNECTION_FLAGS_AUTHENTICATION_CLIENT | MESSAGE_BUS_CONNECTION
	conn := Call(Sym("g_dbus_connection_new_for_address_sync"), addr, 1|8, 0, 0, Addr(&gerr))
	Call(FnFree, addr)
	if conn == 0 {
		return 0, GError(gerr)
	}
	return conn, nil
}

func closeBus(conn Ptr) {
	if conn == 0 {
		return
	}
	Call(Sym("g_dbus_connection_close_sync"), conn, 0, 0)
	ObjectUnref(conn)
}

// dbusMenu is a menu exported with com.canonical.dbusmenu, as libdbusmenu
// does: items by ID (0 the root, an item's number plus one), each with its
// properties and children. A destructive item is set apart by separators,
// as GTK's menus set it apart in a section.
type dbusMenu struct {
	h        *Host
	conn     Ptr
	path     string
	reg      uint32
	revision uint32
	nodes    map[int32]*dmNode
	click    func(it *ops.MenuItem)
}

type dmNode struct {
	item     *ops.MenuItem // nil: the root, or a separator of the host's
	kids     []int32
	separate bool // a separator of the host's
}

// dmSeparator numbers the host's separators, past any item's.
const dmSeparator = 1 << 24

func (m *dbusMenu) export() error {
	reg, err := DBusRegister(m.conn, m.path, dbusMenuXML, menuIface, DBusObject{
		Method:   m.method,
		Property: m.property,
	})
	if err != nil {
		return err
	}
	m.reg = reg
	return nil
}

func (m *dbusMenu) unexport() {
	if m.reg != 0 {
		DBusUnregister(m.conn, m.reg)
		m.reg = 0
	}
}

// set takes the menu's items, and tells the panel the layout changed.
func (m *dbusMenu) set(items []ops.MenuItem) {
	m.nodes = map[int32]*dmNode{}
	seps := int32(dmSeparator)
	var add func(items []ops.MenuItem) []int32
	add = func(items []ops.MenuItem) []int32 {
		var kids []int32
		sep := func() {
			seps++
			m.nodes[seps] = &dmNode{separate: true}
			kids = append(kids, seps)
		}
		for i := range items {
			it := &items[i]
			id := int32(it.Index + 1)
			destructive := it.Is(ops.MenuDestructive) && !it.Is(ops.MenuSeparator)
			if destructive && len(kids) > 0 && !m.isSeparator(kids[len(kids)-1]) {
				sep()
			}
			n := &dmNode{item: it}
			if it.Is(ops.MenuSubmenu) {
				n.kids = add(it.Items)
			}
			m.nodes[id] = n
			kids = append(kids, id)
			if destructive && i+1 < len(items) && !items[i+1].Is(ops.MenuSeparator) {
				sep()
			}
		}
		return kids
	}
	m.nodes[0] = &dmNode{kids: add(items)}
	m.revision++
	if m.reg != 0 {
		DBusEmit(m.conn, m.path, menuIface, "LayoutUpdated", VTuple(VU32(m.revision), VI32(0)))
	}
}

func (m *dbusMenu) isSeparator(id int32) bool {
	n := m.nodes[id]
	return n != nil && (n.separate || n.item != nil && n.item.Is(ops.MenuSeparator))
}

// props are an item's properties (only those named, if any are).
func (m *dbusMenu) props(id int32, names []string) Ptr {
	n := m.nodes[id]
	v := map[string]Ptr{}
	switch {
	case n == nil:
	case id == 0:
		v["children-display"] = VStr("submenu")
	case n.separate || n.item.Is(ops.MenuSeparator):
		v["type"] = VStr("separator")
	default:
		it := n.item
		v["label"] = VStr(strings.ReplaceAll(m.h.menuItemTitle(it), "_", "__"))
		v["enabled"] = VBool(!it.Is(ops.MenuDisabled))
		v["visible"] = VBool(true)
		if it.Is(ops.MenuChecked) {
			v["toggle-type"] = VStr("checkmark")
			v["toggle-state"] = VI32(1)
		}
		if it.Is(ops.MenuSubmenu) {
			v["children-display"] = VStr("submenu")
		}
	}
	if len(names) > 0 {
		keep := map[string]Ptr{}
		for _, k := range names {
			if p, ok := v[k]; ok {
				keep[k] = p
				delete(v, k)
			}
		}
		for _, p := range v {
			Sys.VariantUnref(Sys.VariantRefSink(p)) // floating, not taken
		}
		v = keep
	}
	return VDict(v)
}

// layout is the (ia{sv}av) of an item and its children, depth levels
// down (-1: all).
func (m *dbusMenu) layout(id, depth int32, names []string) Ptr {
	var kids []Ptr
	if n := m.nodes[id]; n != nil && depth != 0 {
		for _, k := range n.kids {
			kids = append(kids, VVariant(m.layout(k, depth-1, names)))
		}
	}
	return VTuple(VI32(id), m.props(id, names), VArray("v", kids...))
}

func (m *dbusMenu) method(c DBusInvocation) {
	args, _ := VariantGo(c.Params).([]any)
	i32 := func(i int) int32 {
		if i < len(args) {
			v, _ := args[i].(int32)
			return v
		}
		return 0
	}
	strs := func(i int) []string {
		var out []string
		if i < len(args) {
			l, _ := args[i].([]any)
			for _, s := range l {
				if s, ok := s.(string); ok {
					out = append(out, s)
				}
			}
		}
		return out
	}
	switch c.Method {
	case "GetLayout":
		id := i32(0)
		if m.nodes[id] == nil {
			c.Error("com.canonical.dbusmenu.Error.UnknownId", "no item "+strconv.Itoa(int(id)))
			return
		}
		c.Return(VTuple(VU32(m.revision), m.layout(id, i32(1), strs(2))))
	case "GetGroupProperties":
		var ids []int32
		if len(args) > 0 {
			l, _ := args[0].([]any)
			for _, v := range l {
				if v, ok := v.(int32); ok {
					ids = append(ids, v)
				}
			}
		}
		if len(ids) == 0 {
			for id := range m.nodes {
				ids = append(ids, id)
			}
		}
		var out []Ptr
		for _, id := range ids {
			if m.nodes[id] != nil {
				out = append(out, VTuple(VI32(id), m.props(id, strs(1))))
			}
		}
		c.Return(VTuple(VArray("(ia{sv})", out...)))
	case "GetProperty":
		var name string
		if len(args) > 1 {
			name, _ = args[1].(string)
		}
		props := Sys.VariantRefSink(m.props(i32(0), []string{name}))
		if Sys.VariantNChildren(props) == 0 {
			Sys.VariantUnref(props)
			c.Error("com.canonical.dbusmenu.Error.UnknownProperty", "no property "+name)
			return
		}
		// The dictionary's one value, a variant: the reply's.
		e := Sys.VariantGetChildValue(props, 0)
		val := Sys.VariantGetChildValue(e, 1)
		c.Return(VTuple(val))
		Sys.VariantUnref(val)
		Sys.VariantUnref(e)
		Sys.VariantUnref(props)
	case "Event":
		name, _ := args[1].(string)
		m.event(i32(0), name)
		c.Return(0)
	case "EventGroup":
		if len(args) > 0 {
			l, _ := args[0].([]any)
			for _, e := range l {
				if e, ok := e.([]any); ok && len(e) >= 2 {
					id, _ := e[0].(int32)
					name, _ := e[1].(string)
					m.event(id, name)
				}
			}
		}
		c.Return(VTuple(VArray("i")))
	case "AboutToShow":
		c.Return(VTuple(VBool(false)))
	case "AboutToShowGroup":
		c.Return(VTuple(VArray("i"), VArray("i")))
	default:
		c.Error("org.freedesktop.DBus.Error.UnknownMethod", "no method "+c.Method)
	}
}

// event is the panel's event on an item: a click chooses it.
func (m *dbusMenu) event(id int32, name string) {
	n := m.nodes[id]
	if name != "clicked" || n == nil || n.item == nil {
		return
	}
	it := n.item
	if it.Is(ops.MenuSeparator) || it.Is(ops.MenuDisabled) || it.Is(ops.MenuSubmenu) {
		return
	}
	m.h.afterCore(func() { m.click(it) })
}

func (m *dbusMenu) property(name string) Ptr {
	switch name {
	case "Version":
		return VU32(3)
	case "TextDirection":
		if m.h.rtl {
			return VStr("rtl")
		}
		return VStr("ltr")
	case "Status":
		return VStr("normal")
	case "IconThemePath":
		if iconDir != "" {
			return VStrings([]string{iconDir})
		}
		return VStrings(nil)
	}
	return 0
}
