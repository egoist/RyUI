# A freedesktop notification server for the Linux host's tests
# (modules_test.go): it keeps what it's shown, and does what the test
# asks of a user (Test.Invoke: a tap or a button; Test.Dismiss).
# Usage: python3 notifications.py (on the session bus; exits with stdin).

import sys, threading
from gi.repository import Gio, GLib

XML = """
<node>
  <interface name="org.freedesktop.Notifications">
    <method name="Notify">
      <arg type="s" direction="in"/><arg type="u" direction="in"/><arg type="s" direction="in"/>
      <arg type="s" direction="in"/><arg type="s" direction="in"/><arg type="as" direction="in"/>
      <arg type="a{sv}" direction="in"/><arg type="i" direction="in"/><arg type="u" direction="out"/>
    </method>
    <method name="CloseNotification"><arg type="u" direction="in"/></method>
    <method name="GetCapabilities"><arg type="as" direction="out"/></method>
    <method name="GetServerInformation">
      <arg type="s" direction="out"/><arg type="s" direction="out"/><arg type="s" direction="out"/><arg type="s" direction="out"/>
    </method>
    <signal name="ActionInvoked"><arg type="u"/><arg type="s"/></signal>
    <signal name="NotificationClosed"><arg type="u"/><arg type="u"/></signal>
  </interface>
  <interface name="dev.asuka.Test">
    <method name="Shown"><arg type="a(ussas)" direction="out"/></method>
    <method name="Invoke"><arg type="u" direction="in"/><arg type="s" direction="in"/></method>
  </interface>
</node>
"""

shown = {}  # id: (summary, body, actions)
last = [0]
conn = None


def emit(name, params):
    conn.emit_signal(None, "/org/freedesktop/Notifications", "org.freedesktop.Notifications", name, params)


def call(c, sender, path, iface, method, params, inv):
    if method == "Notify":
        app, replaces, icon, summary, body, actions, hints, timeout = params.unpack()
        n = replaces if replaces in shown else last[0] + 1
        last[0] = max(last[0], n)
        shown[n] = (summary, body, actions)
        inv.return_value(GLib.Variant("(u)", (n,)))
    elif method == "CloseNotification":
        (n,) = params.unpack()
        if shown.pop(n, None) is not None:
            emit("NotificationClosed", GLib.Variant("(uu)", (n, 3)))
        inv.return_value(None)
    elif method == "GetCapabilities":
        inv.return_value(GLib.Variant("(as)", (["actions", "body", "body-markup"],)))
    elif method == "GetServerInformation":
        inv.return_value(GLib.Variant("(ssss)", ("test", "asuka", "1", "1.2")))
    elif method == "Shown":
        inv.return_value(GLib.Variant("(a(ussas))", ([(n, s, b, a) for n, (s, b, a) in sorted(shown.items())],)))
    elif method == "Invoke":
        n, key = params.unpack()
        emit("ActionInvoked", GLib.Variant("(us)", (n, key)))
        shown.pop(n, None)
        emit("NotificationClosed", GLib.Variant("(uu)", (n, 2)))
        inv.return_value(None)


def acquired(c, name):
    print("ready", flush=True)


def bus(c, name):
    global conn
    conn = c
    node = Gio.DBusNodeInfo.new_for_xml(XML)
    for i in node.interfaces:
        c.register_object("/org/freedesktop/Notifications", i, call, None, None)


loop = GLib.MainLoop()
Gio.bus_own_name(Gio.BusType.SESSION, "org.freedesktop.Notifications", Gio.BusNameOwnerFlags.REPLACE, bus, acquired, lambda *a: loop.quit())
threading.Thread(target=lambda: (sys.stdin.read(), GLib.idle_add(loop.quit)), daemon=True).start()
loop.run()
