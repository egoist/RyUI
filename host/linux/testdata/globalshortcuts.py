# An XDG desktop portal with GlobalShortcuts alone, for the Linux host's
# tests (hotkey_test.go): it makes sessions, binds what it's asked to
# (or declines the next binding, as a user may: Test.Decline), keeps
# what's bound (Test.Bound) and presses a shortcut as the desktop would
# (Test.Press: Activated, with an activation token). Usage: python3 globalshortcuts.py (on the session bus,
# replacing a portal there; exits with stdin).

import sys, threading
from gi.repository import Gio, GLib

XML = """
<node>
  <interface name="org.freedesktop.portal.GlobalShortcuts">
    <method name="CreateSession">
      <arg type="a{sv}" direction="in"/><arg type="o" direction="out"/>
    </method>
    <method name="BindShortcuts">
      <arg type="o" direction="in"/><arg type="a(sa{sv})" direction="in"/><arg type="s" direction="in"/>
      <arg type="a{sv}" direction="in"/><arg type="o" direction="out"/>
    </method>
    <signal name="Activated"><arg type="o"/><arg type="s"/><arg type="t"/><arg type="a{sv}"/></signal>
    <property name="version" type="u" access="read"/>
  </interface>
  <interface name="dev.asuka.Test">
    <method name="Bound"><arg type="a(sss)" direction="out"/></method>
    <method name="Press"><arg type="s" direction="in"/></method>
    <method name="Decline"/>
  </interface>
</node>
"""

SESSION_XML = """
<node>
  <interface name="org.freedesktop.portal.Session">
    <method name="Close"/>
  </interface>
</node>
"""

conn = None
sessions = {}  # path: registration id
bound = {}  # session path: [(id, trigger, description)]
decline = [False]
nodes = {}


def request_path(sender, token):
    return "/org/freedesktop/portal/desktop/request/%s/%s" % (sender[1:].replace(".", "_"), token)


def respond(sender, path, code, results):
    def emit():
        conn.emit_signal(sender, path, "org.freedesktop.portal.Request", "Response", GLib.Variant("(ua{sv})", (code, results)))
        return False
    GLib.idle_add(emit)


def session_call(c, sender, path, iface, method, params, inv):
    if method == "Close":
        reg = sessions.pop(path, None)
        bound.pop(path, None)
        if reg is not None:
            c.unregister_object(reg)
        inv.return_value(None)


def call(c, sender, path, iface, method, params, inv):
    if method == "CreateSession":
        (opts,) = params.unpack()
        req = request_path(sender, opts["handle_token"])
        session = "/org/freedesktop/portal/desktop/session/%s/%s" % (sender[1:].replace(".", "_"), opts["session_handle_token"])
        sessions[session] = c.register_object(session, nodes["session"], session_call, None, None)
        inv.return_value(GLib.Variant("(o)", (req,)))
        respond(sender, req, 0, {"session_handle": GLib.Variant("s", session)})
    elif method == "BindShortcuts":
        session, shortcuts, parent, opts = params.unpack()
        req = request_path(sender, opts["handle_token"])
        inv.return_value(GLib.Variant("(o)", (req,)))
        if decline[0] or session not in sessions:
            decline[0] = False
            respond(sender, req, 1, {})
            return
        bound[session] = [(i, o.get("preferred_trigger", ""), o.get("description", "")) for i, o in shortcuts]
        out = [(i, {"description": GLib.Variant("s", d), "trigger_description": GLib.Variant("s", t)}) for i, t, d in bound[session]]
        respond(sender, req, 0, {"shortcuts": GLib.Variant("a(sa{sv})", out)})
    elif method == "Bound":
        inv.return_value(GLib.Variant("(a(sss))", ([(s, i, t + "|" + d) for s, l in sorted(bound.items()) for i, t, d in l],)))
    elif method == "Press":
        (sid,) = params.unpack()
        for s, l in bound.items():
            if any(i == sid for i, _, _ in l):
                conn.emit_signal(None, "/org/freedesktop/portal/desktop", "org.freedesktop.portal.GlobalShortcuts", "Activated",
                                 GLib.Variant("(osta{sv})", (s, sid, 1, {"activation_token": GLib.Variant("s", "token-" + sid)})))
        inv.return_value(None)
    elif method == "Decline":
        decline[0] = True
        inv.return_value(None)


def get_property(c, sender, path, iface, name):
    if name == "version":
        return GLib.Variant("u", 1)


def acquired(c, name):
    print("ready", flush=True)


def bus(c, name):
    global conn
    conn = c
    node = Gio.DBusNodeInfo.new_for_xml(XML)
    nodes["session"] = Gio.DBusNodeInfo.new_for_xml(SESSION_XML).interfaces[0]
    for i in node.interfaces:
        c.register_object("/org/freedesktop/portal/desktop", i, call, get_property, None)


loop = GLib.MainLoop()
Gio.bus_own_name(Gio.BusType.SESSION, "org.freedesktop.portal.Desktop",
                 Gio.BusNameOwnerFlags.REPLACE | Gio.BusNameOwnerFlags.ALLOW_REPLACEMENT, bus, acquired, lambda *a: loop.quit())
threading.Thread(target=lambda: (sys.stdin.read(), GLib.idle_add(loop.quit)), daemon=True).start()
loop.run()
