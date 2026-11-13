# A file manager's org.freedesktop.FileManager1 for the Linux host's
# tests (modules_dialog_test.go): it prints each ShowItems call's URIs,
# a line each, as "show <uri>".
# Usage: python3 filemanager.py (on the session bus; exits with stdin).

import sys, threading
from gi.repository import Gio, GLib

XML = """
<node>
  <interface name="org.freedesktop.FileManager1">
    <method name="ShowFolders"><arg type="as" direction="in"/><arg type="s" direction="in"/></method>
    <method name="ShowItems"><arg type="as" direction="in"/><arg type="s" direction="in"/></method>
    <method name="ShowItemProperties"><arg type="as" direction="in"/><arg type="s" direction="in"/></method>
  </interface>
</node>
"""


def call(c, sender, path, iface, method, params, inv):
    uris, startup = params.unpack()
    if method == "ShowItems":
        for u in uris:
            print("show " + u, flush=True)
    inv.return_value(None)


def acquired(c, name):
    print("ready", flush=True)


def bus(c, name):
    node = Gio.DBusNodeInfo.new_for_xml(XML)
    c.register_object("/org/freedesktop/FileManager1", node.interfaces[0], call, None, None)


loop = GLib.MainLoop()
Gio.bus_own_name(Gio.BusType.SESSION, "org.freedesktop.FileManager1", Gio.BusNameOwnerFlags.REPLACE, bus, acquired, lambda *a: loop.quit())
threading.Thread(target=lambda: (sys.stdin.read(), GLib.idle_add(loop.quit)), daemon=True).start()
loop.run()
