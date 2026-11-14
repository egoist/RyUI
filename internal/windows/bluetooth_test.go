//go:build windows

package windows

import (
	"strings"
	"testing"
)

// The parameterized interfaces' IIDs, from their signatures, as the
// Windows SDK's headers have them.
func TestBluetoothIIDs(t *testing.T) {
	for name, c := range map[string]struct {
		got  GUID
		want string
	}{
		"TypedEventHandler<BluetoothLEAdvertisementWatcher, BluetoothLEAdvertisementReceivedEventArgs>": {iidReceivedHandler, "90eb4eca-d465-5ea0-a61c-033c8c5ecef2"},
		"AsyncOperationCompletedHandler<BluetoothLEDevice>":                                             {iidLEDeviceCompleted, "9156b79f-c54a-5277-8f8b-d2cc43c7e004"},
		"TypedEventHandler<BluetoothLEDevice, Object>":                                                  {iidLEDeviceHandler, "a90661e2-372e-5d1e-bbbb-b8a2ce0e7c4d"},
	} {
		if got := strings.Trim(c.got.Signature(), "{}"); got != c.want {
			t.Errorf("%s: %s, want %s", name, got, c.want)
		}
	}
	u := [16]byte{0, 0, 0x18, 0x0d, 0, 0, 0x10, 0, 0x80, 0, 0, 0x80, 0x5f, 0x9b, 0x34, 0xfb}
	if g := BTGUID(u); g.Data1 != 0x180d || g.Data3 != 0x1000 || BTUUID(g) != u {
		t.Errorf("BTGUID: %+v", g)
	}
}
