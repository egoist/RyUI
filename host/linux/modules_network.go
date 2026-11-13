//go:build linux && !android

package linux

import (
	"errors"

	. "github.com/egoist/asuka/internal/gtk"
)

// asuka/network on Linux: GNetworkMonitor (NetworkManager's, the portal's
// in a sandbox) says whether the network is there and metered;
// NetworkManager, when it runs, says what carries it (its primary
// connection's type). The state is u8 connected, type (1 Wi-Fi, 2
// cellular, 3 ethernet, 4 other), metered, constrained (never: Linux
// has no low data mode).

func init() {
	moduleHooks = append(moduleHooks, func(h *Host, m map[string]map[string]method) {
		m["network"] = h.networkModule()
	})
}

type netWatch struct {
	monitor Ptr
	state   []byte // the last state, nil until read
	kind    byte   // NetworkManager's primary connection's type, 0: not known
	reading bool   // NetworkManager's being asked
	current []func([]byte, error)
	next    []netWait
}

type netWait struct {
	last  []byte
	reply func([]byte, error)
}

var netw netWatch

// startNetwork watches the monitor, once.
func (h *Host) startNetwork() error {
	n := &netw
	if n.monitor != 0 {
		return nil
	}
	n.monitor = Sys.NetworkMonitorGetDefault()
	if n.monitor == 0 {
		return errors.New("asuka/network: no network monitor")
	}
	changed := func() { h.readNetwork() }
	Connect(n.monitor, "network-changed", func(uintptr) { changed() })
	Connect(n.monitor, "notify::network-metered", func(uintptr) { changed() })
	Connect(n.monitor, "notify::connectivity", func(uintptr) { changed() })
	if conn, err := Bus(BusSystem); err == nil {
		// The primary connection changing (Wi-Fi to a cable) isn't the
		// monitor's change.
		DBusSubscribe(conn, "org.freedesktop.NetworkManager", "org.freedesktop.DBus.Properties", "PropertiesChanged", "/org/freedesktop/NetworkManager", func(DBusSignal) {
			changed()
		})
	}
	h.readNetwork()
	return nil
}

// readNetwork reads the state (asking NetworkManager its connection's
// type), then answers who waits.
func (h *Host) readNetwork() {
	n := &netw
	if n.reading {
		return
	}
	conn, err := Bus(BusSystem)
	if err != nil {
		n.kind = 0
		h.networkRead()
		return
	}
	n.reading = true
	DBusCall(conn, "org.freedesktop.NetworkManager", "/org/freedesktop/NetworkManager", "org.freedesktop.DBus.Properties", "Get",
		VTuple(VStr("org.freedesktop.NetworkManager"), VStr("PrimaryConnectionType")), 2000, func(r Ptr, err error) {
			n.reading = false
			n.kind = 0
			if err == nil {
				if a, _ := VariantGo(r).([]any); len(a) > 0 {
					t, _ := a[0].(string)
					n.kind = nmType(t)
				}
			}
			h.networkRead()
		})
}

// nmType is a NetworkManager connection type as the module's.
func nmType(t string) byte {
	switch t {
	case "":
		return 0
	case "802-11-wireless", "wifi-p2p":
		return 1
	case "gsm", "cdma", "wwan":
		return 2
	case "802-3-ethernet", "pppoe", "infiniband", "veth":
		return 3
	}
	return 4
}

// networkState is the monitor's state now, with NetworkManager's type.
func networkState(monitor Ptr, kind byte) []byte {
	const full = 4 // G_NETWORK_CONNECTIVITY_FULL
	connected := Sys.NetworkMonitorAvailable(monitor) && Sys.NetworkMonitorConnectivity(monitor) == full
	if !connected {
		return []byte{0, 0, 0, 0}
	}
	if kind == 0 {
		kind = 4 // connected, by what isn't known
	}
	return []byte{1, kind, b2u(Sys.NetworkMonitorMetered(monitor)), 0}
}

func (h *Host) networkRead() {
	n := &netw
	s := networkState(n.monitor, n.kind)
	n.state = s
	cs := n.current
	n.current = nil
	for _, c := range cs {
		c(s, nil)
	}
	var keep []netWait
	for _, w := range n.next {
		if string(w.last) != string(s) {
			w.reply(s, nil)
		} else {
			keep = append(keep, w)
		}
	}
	n.next = keep
}

// networkModule: "current", and, while watching, "next" (answered once
// the state isn't the one given) and "stop".
func (h *Host) networkModule() map[string]method {
	n := &netw
	return map[string]method{
		"current": func(_ []byte, reply func([]byte, error)) {
			if err := h.startNetwork(); err != nil {
				reply(nil, err)
				return
			}
			if n.state != nil && !n.reading {
				reply(n.state, nil)
				return
			}
			n.current = append(n.current, reply)
		},
		"next": func(args []byte, reply func([]byte, error)) {
			if err := h.startNetwork(); err != nil {
				reply(nil, err)
				return
			}
			if n.state == nil {
				n.current = append(n.current, reply)
				return
			}
			if len(args) == 0 || string(args) != string(n.state) {
				reply(n.state, nil)
				return
			}
			n.next = append(n.next, netWait{last: append([]byte(nil), args...), reply: reply})
		},
		"stop": func(_ []byte, reply func([]byte, error)) {
			ws := n.next
			n.next = nil
			for _, w := range ws {
				w.reply(nil, errors.New("stopped"))
			}
			reply(nil, nil)
		},
	}
}

func b2u(b bool) byte {
	if b {
		return 1
	}
	return 0
}
