//go:build linux && !android

package gtk

import (
	"runtime"

	"github.com/ebitengine/purego"
)

// Objects the app exports on D-Bus (g_dbus_connection_register_object):
// a tray icon's StatusNotifierItem and its menu (host/linux/tray.go). One
// GDBusInterfaceVTable, its callbacks made once, serves every object;
// each registration's Go handlers are found by the user data GDBus
// passes back, as signals' are.

// DBusInvocation is a method call to an object of the app's. Answer it
// once, with Return or Error, on the main thread (now or later).
type DBusInvocation struct {
	Sender, Path, Iface, Method string
	// Params is the call's tuple, valid during the handler.
	Params Ptr
	inv    Ptr
}

// Return answers the call with a tuple (a floating VTuple; 0: none).
func (c DBusInvocation) Return(v Ptr) { Call(fnInvReturnValue, c.inv, v) }

// Error answers the call with a D-Bus error (its name and message).
func (c DBusInvocation) Error(name, msg string) {
	n, m := CString(name), CString(msg)
	Call(fnInvReturnError, c.inv, Addr(n), Addr(m))
	runtime.KeepAlive(n)
	runtime.KeepAlive(m)
}

// DBusObject is what an exported object does: its methods' calls, and
// its properties' values (a floating variant of the property's type; 0:
// no such property).
type DBusObject struct {
	Method   func(c DBusInvocation)
	Property func(name string) Ptr
}

var (
	fnRegisterObject, fnUnregisterObject, fnNodeInfoNew, fnNodeLookup uintptr
	fnInvReturnValue, fnInvReturnError, fnSetErrorLiteral             uintptr
	fnDBusErrorQuark                                                  uintptr

	dbusVTable  [11]uintptr // GDBusInterfaceVTable: method, get, set, 8 padding
	dbusIfaces  = map[string]Ptr{}
	dbusObjects = map[uintptr]DBusObject{}
)

func initDBusObjects() bool {
	if fnRegisterObject != 0 {
		return true
	}
	for _, f := range []struct {
		p    *uintptr
		name string
	}{
		{&fnRegisterObject, "g_dbus_connection_register_object"},
		{&fnUnregisterObject, "g_dbus_connection_unregister_object"},
		{&fnNodeInfoNew, "g_dbus_node_info_new_for_xml"},
		{&fnNodeLookup, "g_dbus_node_info_lookup_interface"},
		{&fnInvReturnValue, "g_dbus_method_invocation_return_value"},
		{&fnInvReturnError, "g_dbus_method_invocation_return_dbus_error"},
		{&fnSetErrorLiteral, "g_set_error_literal"},
		{&fnDBusErrorQuark, "g_dbus_error_quark"},
	} {
		if *f.p = Sym(f.name); *f.p == 0 {
			return false
		}
	}
	dbusVTable[0] = purego.NewCallback(func(conn, sender, path, iface, method, params, inv, data uintptr) uintptr {
		o, ok := dbusObjects[data]
		c := DBusInvocation{Sender: GoString(sender), Path: GoString(path), Iface: GoString(iface), Method: GoString(method), Params: params, inv: inv}
		if !ok || o.Method == nil {
			c.Error("org.freedesktop.DBus.Error.UnknownMethod", "no such method")
			return 0
		}
		o.Method(c)
		return 0
	})
	dbusVTable[1] = purego.NewCallback(func(conn, sender, path, iface, prop, gerr, data uintptr) uintptr {
		var v Ptr
		if o, ok := dbusObjects[data]; ok && o.Property != nil {
			v = o.Property(GoString(prop))
		}
		if v == 0 {
			msg := CString("no such property")
			Call(fnSetErrorLiteral, gerr, Call(fnDBusErrorQuark), 0, Addr(msg)) // G_DBUS_ERROR_FAILED
			runtime.KeepAlive(msg)
		}
		return v
	})
	return true
}

// dbusInterface is the interface named iface of the introspection XML,
// parsed once (the node info is kept for good).
func dbusInterface(xml, iface string) (Ptr, error) {
	if i := dbusIfaces[iface]; i != 0 {
		return i, nil
	}
	x := CString(xml)
	var gerr Ptr
	info := Call(fnNodeInfoNew, Addr(x), Addr(&gerr))
	runtime.KeepAlive(x)
	if info == 0 {
		return 0, GError(gerr)
	}
	n := CString(iface)
	i := Call(fnNodeLookup, info, Addr(n))
	runtime.KeepAlive(n)
	dbusIfaces[iface] = i
	return i, nil
}

// DBusRegister exports o at path on conn as the interface iface of the
// introspection XML; it returns the registration's ID, for
// DBusUnregister. Main thread: the calls come on the main context.
func DBusRegister(conn Ptr, path, xml, iface string, o DBusObject) (uint32, error) {
	if !initDBusObjects() {
		return 0, errNoDBusObjects
	}
	info, err := dbusInterface(xml, iface)
	if err != nil {
		return 0, err
	}
	id := register(nil)
	delete(handlers, id) // the ID only: dbusObjects holds o
	dbusObjects[id] = o
	p := CString(path)
	var gerr Ptr
	r := Call(fnRegisterObject, conn, Addr(p), info, Addr(&dbusVTable[0]), id, cbDBusFree, Addr(&gerr))
	runtime.KeepAlive(p)
	if r == 0 {
		delete(dbusObjects, id)
		return 0, GError(gerr)
	}
	return uint32(r), nil
}

// DBusUnregister takes an exported object off the bus.
func DBusUnregister(conn Ptr, id uint32) {
	if id != 0 && fnUnregisterObject != 0 {
		Call(fnUnregisterObject, conn, uintptr(id))
	}
}

// cbDBusFree forgets an object's handlers once GDBus is done with it.
var cbDBusFree = purego.NewCallback(func(data uintptr) uintptr {
	delete(dbusObjects, data)
	return 0
})

type dbusError string

func (e dbusError) Error() string { return string(e) }

const errNoDBusObjects = dbusError("gtk: GIO can't export D-Bus objects")
