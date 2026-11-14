//go:build windows

package windows

import (
	"encoding/json"
	"strings"
	"testing"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
	"github.com/egoist/asuka/ui"
)

// What the bluetooth module answers without a device: the adapter's
// state (whatever this computer has), the event queue's "stop", and the
// calls on a connection or a server that isn't there.
func TestBluetoothModule(t *testing.T) {
	h, _ := mount(t, func() ui.Node { return ui.StaticText("x") })
	onMain(t, func() {
		ok := func(m string, args []byte) []byte {
			b, err, answered := await(h, "bluetooth", m, args)
			if !answered || err != nil {
				fatalf("bluetooth.%s: %v (answered %v)", m, err, answered)
			}
			return b
		}
		fails := func(m string, args []byte, want string) {
			_, err, answered := await(h, "bluetooth", m, args)
			if !answered || err == nil || !strings.HasPrefix(err.Error(), want) {
				fatalf("bluetooth.%s: %v, want %q", m, err, want)
			}
		}
		var st spec.State
		if json.Unmarshal(ok("state", nil), &st) != nil {
			fatalf("bluetooth.state: not a State")
		}
		switch st.State {
		case spec.StateOn, spec.StateOff, spec.StateUnsupported, spec.StateUnknown, spec.StateDenied:
		default:
			fatalf("bluetooth.state: %q", st.State)
		}
		if b := ok("restore", nil); string(b) != "{}" {
			fatalf("bluetooth.restore: %s", b)
		}
		ok("stopBackgroundScan", nil)

		// A waiting "next" ends at "stop".
		var waited error
		h.asyncModule("bluetooth")["next"]([]byte("{}"), func(_ []byte, e error) { waited = e })
		ok("stop", nil)
		if waited == nil || waited.Error() != "stopped" {
			fatalf("bluetooth.next after stop: %v", waited)
		}

		ok("disconnect", []byte(`{"conn":7}`))
		fails("services", []byte(`{"conn":7}`), "disconnected")
		fails("read", []byte(`{"conn":7,"handle":3}`), "disconnected")
		fails("rssi", []byte(`{"conn":7}`), "disconnected")
		fails("mtu", []byte(`{"conn":7}`), "disconnected")
		fails("notify", []byte(`{"server":9,"handle":1,"value":"AA=="}`), "not found")
		ok("respond", []byte(`{"server":9,"request":1}`))
		ok("stopScan", []byte(`{"id":3}`))
		ok("stopAdvertise", []byte(`{"id":3}`))
		ok("unserve", []byte(`{"id":3}`))
		fails("connect", []byte(`{"conn":1,`), "bad args")
		if st.State == spec.StateOn {
			fails("connect", []byte(`{"conn":1,"device":"not an address"}`), "not found")
		} else {
			fails("scan", []byte(`{"id":1}`), st.State)
		}
		if b := ok("known", []byte(`{"ids":["not an address"]}`)); string(b) != "[]" {
			fatalf("bluetooth.known: %s", b)
		}
	})
}
