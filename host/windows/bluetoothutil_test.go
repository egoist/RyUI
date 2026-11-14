package windows

import (
	"reflect"
	"slices"
	"testing"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
)

func TestBTAddress(t *testing.T) {
	if got := btAddress(0xC47C8D6A1F20); got != "C4:7C:8D:6A:1F:20" {
		t.Errorf("btAddress = %q", got)
	}
	if got := btAddress(0x01); got != "00:00:00:00:00:01" {
		t.Errorf("btAddress = %q", got)
	}
	for in, want := range map[string]uint64{
		"C4:7C:8D:6A:1F:20": 0xC47C8D6A1F20,
		"c4:7c:8d:6a:1f:20": 0xC47C8D6A1F20,
		"c4-7c-8d-6a-1f-20": 0xC47C8D6A1F20,
		"c47c8d6a1f20":      0xC47C8D6A1F20,
	} {
		if got, ok := btParseAddress(in); !ok || got != want {
			t.Errorf("btParseAddress(%q) = %x, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "C4:7C:8D:6A:1F", "C4:7C-8D:6A:1F:20", "C4.7C.8D.6A.1F.20", "zz:7c:8d:6a:1f:20", "0000180d-0000-1000-8000-00805f9b34fb"} {
		if _, ok := btParseAddress(in); ok {
			t.Errorf("btParseAddress(%q) took it", in)
		}
	}
	if got := btCentralID("BluetoothLE#BluetoothLE00:1a:7d:da:71:13-c4:7c:8d:6a:1f:20"); got != "C4:7C:8D:6A:1F:20" {
		t.Errorf("btCentralID = %q", got)
	}
	if got := btCentralID("weird"); got != "weird" {
		t.Errorf("btCentralID = %q", got)
	}
}

func TestBTSections(t *testing.T) {
	a := btParseSections([]btSection{
		{0x01, []byte{0x06}},                   // flags: ignored
		{0x03, []byte{0x0d, 0x18, 0x0f, 0x18}}, // 180d, 180f
		{0x07, []byte{0xfb, 0x34, 0x9b, 0x5f, 0x80, 0, 0, 0x80, 0, 0x10, 0, 0, 0x78, 0x56, 0x34, 0x12}}, // 12345678-...
		{0x08, []byte("Pol")},                  // shortened
		{0x09, []byte("Polar H10")},            // complete wins
		{0x0a, []byte{0xf4}},                   // -12 dBm
		{0x14, []byte{0x02, 0x18}},             // solicits 1802
		{0x16, []byte{0x0f, 0x18, 0x64}},       // battery service data: 100
		{0xff, []byte{0x4c, 0x00, 0x02, 0x15}}, // Apple's
		{0xff, []byte{0x01}},                   // too short: ignored
	})
	if a.name != "Polar H10" {
		t.Errorf("name %q", a.name)
	}
	wantServices := []string{spec.Normalize("180d"), spec.Normalize("180f"), "12345678-0000-1000-8000-00805f9b34fb"}
	if !reflect.DeepEqual(a.services, wantServices) {
		t.Errorf("services %v", a.services)
	}
	if !reflect.DeepEqual(a.solicited, []string{spec.Normalize("1802")}) {
		t.Errorf("solicited %v", a.solicited)
	}
	if a.txPower == nil || *a.txPower != -12 {
		t.Errorf("txPower %v", a.txPower)
	}
	if v := a.serviceData[spec.Normalize("180f")]; !reflect.DeepEqual(v, []byte{0x64}) {
		t.Errorf("serviceData %v", a.serviceData)
	}
	if v := a.manufacturer["76"]; !reflect.DeepEqual(v, []byte{0x02, 0x15}) || len(a.manufacturer) != 1 {
		t.Errorf("manufacturer %v", a.manufacturer)
	}
	if got := btUUIDLE([]byte{0x78, 0x56, 0x34, 0x12}); got != "12345678-0000-1000-8000-00805f9b34fb" {
		t.Errorf("32-bit %q", got)
	}
}

// A scan without duplicates hears a device once, whole: its
// advertisement and its scan response (which Windows delivers apart).
func TestBTHeard(t *testing.T) {
	adv := btParseSections([]btSection{{0x03, []byte{0x0d, 0x18}}})
	rsp := btParseSections([]btSection{{0x09, []byte("HR")}, {0xff, []byte{0x59, 0x00, 1}}})

	var h btHeard
	h.take(btPacket{ad: adv, rssi: -60, typ: btAdvConnectableUndirected, scannable: true})
	if h.complete() {
		t.Fatal("complete before its scan response")
	}
	h.take(btPacket{ad: rsp, rssi: -61, response: true, typ: btAdvScanResponse})
	if !h.complete() {
		t.Fatal("not complete with its scan response")
	}
	a := h.advertisement("AA:BB:CC:DD:EE:FF")
	if a.LocalName != "HR" || a.Device.Name != "HR" || a.Device.ID != "AA:BB:CC:DD:EE:FF" || a.RSSI != -61 {
		t.Errorf("advertisement %+v", a)
	}
	if !slices.Equal(a.Services, []string{spec.Normalize("180d")}) || len(a.ManufacturerData["89"]) != 1 {
		t.Errorf("advertisement %+v", a)
	}
	if a.Connectable == nil || !*a.Connectable {
		t.Errorf("connectable %v", a.Connectable)
	}
	if !btMatches(a, []string{"180d"}) || btMatches(a, []string{"180f"}) || !btMatches(a, nil) {
		t.Error("btMatches")
	}

	// No scan response comes: whole at its second advertisement.
	var quiet btHeard
	quiet.take(btPacket{ad: adv, typ: btAdvConnectableUndirected, scannable: true})
	quiet.take(btPacket{ad: adv, typ: btAdvConnectableUndirected, scannable: true})
	if !quiet.complete() {
		t.Error("a device that doesn't answer isn't complete")
	}
	// Not scannable: whole at once; IsConnectable (Windows 10 2004) wins
	// over the type.
	var beacon btHeard
	no := false
	beacon.take(btPacket{ad: adv, typ: btAdvConnectableUndirected, connectable: &no})
	if !beacon.complete() || *beacon.advertisement("x").Connectable {
		t.Error("beacon")
	}
	var nonconn btHeard
	nonconn.take(btPacket{ad: adv, typ: btAdvNonConnectableUndirected})
	if c := nonconn.advertisement("x").Connectable; c == nil || *c {
		t.Error("non-connectable type")
	}
}

func TestBTErrors(t *testing.T) {
	for _, c := range []struct {
		status int32
		proto  int
		hr     uint32
		want   string
	}{
		{0, -1, 0, ""},
		{1, -1, 0, "disconnected"},
		{2, 3, 0, "att 3: write not permitted"},
		{2, -1, 0, "att 14: unlikely error"},
		{3, -1, 0, "denied"},
		{-1, -1, 0x80000013, "disconnected"},
		{-1, -1, 0x80070005, "denied"},
		{-1, -1, 0x80004005, "failed: 0x80004005"},
	} {
		if got := btGattError(c.status, c.proto, c.hr); got != c.want {
			t.Errorf("btGattError(%d, %d, %#x) = %q, want %q", c.status, c.proto, c.hr, got, c.want)
		}
	}
	if btError(0) != "" || btError(1) != "off" || btError(7) != "denied" || btError(6)[:13] != "not available" {
		t.Error("btError")
	}
	if btRadioState(1) != "on" || btRadioState(2) != "off" || btRadioState(3) != "off" || btRadioState(0) != "unknown" {
		t.Error("btRadioState")
	}
	if v, ok := btCCCD(spec.PropNotify|spec.PropIndicate, false); !ok || v != 1 {
		t.Error("notify")
	}
	if v, ok := btCCCD(spec.PropNotify|spec.PropIndicate, true); !ok || v != 2 {
		t.Error("indicate asked")
	}
	if v, ok := btCCCD(spec.PropIndicate, false); !ok || v != 2 {
		t.Error("indicate only")
	}
	if _, ok := btCCCD(spec.PropRead, false); ok {
		t.Error("neither")
	}
}

func TestBTChooserAndDiff(t *testing.T) {
	bases := []string{"A", "B"}
	if got := btChooserSelectors(bases, nil); !slices.Equal(got, bases) {
		t.Errorf("no filters: %v", got)
	}
	if got := btChooserSelectors(bases, []spec.Filter{{Name: "x"}, {Services: []string{"180d"}}}); !slices.Equal(got, bases) {
		t.Errorf("a filter without a name: %v", got)
	}
	got := btChooserSelectors(bases, []spec.Filter{{Name: `Po"lar`}, {NamePrefix: "HR"}})
	want := []string{`(A) AND System.ItemNameDisplay:="Polar"`, `(B) AND System.ItemNameDisplay:="Polar"`,
		`(A) AND System.ItemNameDisplay:~<"HR"`, `(B) AND System.ItemNameDisplay:~<"HR"`}
	if !slices.Equal(got, want) {
		t.Errorf("names: %v", got)
	}
	on, off := btDiff([]string{"a", "b"}, []string{"b", "c", "c"})
	if !slices.Equal(on, []string{"c"}) || !slices.Equal(off, []string{"a"}) {
		t.Errorf("btDiff: %v %v", on, off)
	}
	for u, want := range map[string]string{"2901": "user", "2904": "format", "2902": "skip", "2900": "skip", "2a37": ""} {
		if got := btWindowsDescriptor(u); got != want {
			t.Errorf("btWindowsDescriptor(%s) = %q", u, got)
		}
	}
}
