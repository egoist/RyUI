package bluez

import (
	"errors"
	"reflect"
	"testing"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
)

// remote is an error as internal/gtk's GError writes GDBus's remote ones.
func remote(name, msg string) error {
	return errors.New(name + ": GDBus.Error:" + name + ": " + msg)
}

func TestError(t *testing.T) {
	for _, c := range []struct {
		err       error
		name, msg string
	}{
		{remote("org.bluez.Error.Failed", "le-connection-abort-by-local"), "org.bluez.Error.Failed", "le-connection-abort-by-local"},
		{errors.New("GDBus.Error:org.bluez.Error.NotPermitted: Read not permitted"), "org.bluez.Error.NotPermitted", "Read not permitted"},
		{remote("org.bluez.Error.Failed", "Operation failed with ATT error: 0x0e"), "org.bluez.Error.Failed", "Operation failed with ATT error: 0x0e"},
		{errors.New("Timeout was reached"), "", "Timeout was reached"},
		{errors.New("org.bluez.Error.NotReady"), "org.bluez.Error.NotReady", ""},
		{nil, "", ""},
	} {
		name, msg := Error(c.err)
		if name != c.name || msg != c.msg {
			t.Errorf("Error(%v) = %q, %q; want %q, %q", c.err, name, msg, c.name, c.msg)
		}
	}
}

func TestBusState(t *testing.T) {
	for err, want := range map[error]string{
		remote("org.freedesktop.DBus.Error.ServiceUnknown", "The name org.bluez was not provided by any .service files"): "unsupported",
		remote("org.freedesktop.DBus.Error.NameHasNoOwner", "no owner"):                                                  "unsupported",
		remote("org.freedesktop.DBus.Error.AccessDenied", "not allowed"):                                                 "denied",
		remote("org.bluez.Error.Failed", "x"):                                                                            "",
	} {
		if got := BusState(err); got != want {
			t.Errorf("BusState(%v) = %q, want %q", err, got, want)
		}
	}
}

func TestClientError(t *testing.T) {
	for _, c := range []struct {
		err   error
		write bool
		want  string
	}{
		{remote("org.bluez.Error.NotPermitted", "Read not permitted"), false, "att 2: Read not permitted"},
		{remote("org.bluez.Error.NotPermitted", "Write not permitted"), true, "att 3: Write not permitted"},
		{remote("org.bluez.Error.NotPermitted", "Not paired"), false, "att 5: Not paired"},
		{remote("org.bluez.Error.NotPermitted", "Write acquired"), true, "att 3: Write acquired"},
		{remote("org.bluez.Error.NotAuthorized", "Operation Not Authorized"), true, "att 8: Operation Not Authorized"},
		{remote("org.bluez.Error.InvalidArguments", "Invalid offset"), false, "att 7: Invalid offset"},
		{remote("org.bluez.Error.InvalidArguments", "Invalid Length"), true, "att 13: Invalid Length"},
		{remote("org.bluez.Error.InvalidOffset", "x"), false, "att 7: x"},
		{remote("org.bluez.Error.InvalidValueLength", "x"), true, "att 13: x"},
		{remote("org.bluez.Error.NotSupported", "Operation is not supported"), false, "att 6: Operation is not supported"},
		{remote("org.bluez.Error.Failed", "Operation failed with ATT error: 0x80"), true, "att 128: Operation failed with ATT error: 0x80"},
		{remote("org.bluez.Error.Failed", "Not connected"), false, "disconnected"},
		{remote("org.bluez.Error.NotConnected", "Not Connected"), false, "disconnected"},
		{remote("org.freedesktop.DBus.Error.UnknownObject", "No such object path"), false, "disconnected"},
		{remote("org.bluez.Error.NotReady", "Resource Not Ready"), false, "off"},
		{remote("org.freedesktop.DBus.Error.AccessDenied", "x"), false, "denied"},
		{errors.New("Timeout was reached"), false, "timeout"},
		{remote("org.bluez.Error.InProgress", "In Progress"), false, "In Progress"},
	} {
		if got := ClientError(c.err, c.write); got != c.want {
			t.Errorf("ClientError(%v, %v) = %q, want %q", c.err, c.write, got, c.want)
		}
	}
}

func TestConnectError(t *testing.T) {
	for err, want := range map[error]string{
		remote("org.bluez.Error.Failed", "le-connection-abort-by-local"):   "timeout",
		remote("org.bluez.Error.Failed", "br-connection-page-timeout"):     "timeout",
		remote("org.bluez.Error.Failed", "br-connection-canceled"):         "canceled",
		remote("org.bluez.Error.Failed", "br-connection-refused"):          "br-connection-refused",
		remote("org.bluez.Error.NotReady", "Resource Not Ready"):           "off",
		remote("org.freedesktop.DBus.Error.UnknownObject", "No such path"): "not found",
	} {
		if got := ConnectError(err); got != want {
			t.Errorf("ConnectError(%v) = %q, want %q", err, got, want)
		}
	}
	if !Retry(remote("org.bluez.Error.Failed", "le-connection-abort-by-local")) || Retry(remote("org.bluez.Error.NotReady", "x")) ||
		Retry(remote("org.bluez.Error.Failed", "br-connection-canceled")) {
		t.Error("Retry")
	}
}

func TestReason(t *testing.T) {
	for name, want := range map[string]string{
		"org.bluez.Reason.Timeout":        spec.ReasonTimeout,
		"org.bluez.Reason.Remote":         spec.ReasonRemote,
		"org.bluez.Reason.Local":          spec.ReasonLocal,
		"org.bluez.Reason.Suspend":        spec.ReasonLocal,
		"org.bluez.Reason.Authentication": spec.ReasonFailed,
		"org.bluez.Reason.Unknown":        spec.ReasonUnknown,
		"":                                spec.ReasonUnknown,
	} {
		if got := Reason(name); got != want {
			t.Errorf("Reason(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestProperties(t *testing.T) {
	got := Properties([]string{"broadcast", "read", "write-without-response", "write", "notify", "indicate",
		"authenticated-signed-writes", "extended-properties"})
	if got != 0xff {
		t.Errorf("all flags: %#x", got)
	}
	if got := Properties([]string{"read", "notify"}); got != spec.PropRead|spec.PropNotify {
		t.Errorf("read, notify: %#x", got)
	}
	if got := Properties([]string{"encrypt-read", "encrypt-write"}); got != spec.PropRead|spec.PropWrite {
		t.Errorf("encrypt-read, encrypt-write: %#x", got)
	}
	if got := Properties([]string{"reliable-write"}); got != spec.PropExtendedProperties {
		t.Errorf("reliable-write: %#x", got)
	}
}

func TestFlags(t *testing.T) {
	for _, c := range []struct {
		props, perms int
		want         []string
	}{
		{spec.PropRead | spec.PropNotify, spec.PermRead, []string{"read", "notify"}},
		{spec.PropRead | spec.PropWrite, spec.PermReadEncrypted | spec.PermWriteEncrypted, []string{"encrypt-read", "encrypt-write"}},
		{spec.PropWrite | spec.PropWriteNoResponse | spec.PropIndicate, spec.PermWrite, []string{"write-without-response", "write", "indicate"}},
		{0xff, 0, []string{"broadcast", "read", "write-without-response", "write", "notify", "indicate", "authenticated-signed-writes", "extended-properties"}},
	} {
		if got := Flags(c.props, c.perms); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Flags(%#x, %#x) = %v, want %v", c.props, c.perms, got, c.want)
		}
		if Properties(Flags(c.props, c.perms)) != c.props {
			t.Errorf("Flags(%#x) doesn't read back", c.props)
		}
	}
}

func TestServerError(t *testing.T) {
	for _, c := range []struct {
		code  int
		write bool
		name  string
	}{
		{0x02, false, "org.bluez.Error.NotPermitted"},
		{0x03, true, "org.bluez.Error.NotPermitted"},
		{0x06, false, "org.bluez.Error.NotSupported"},
		{0x07, false, "org.bluez.Error.InvalidOffset"},
		{0x08, true, "org.bluez.Error.NotAuthorized"},
		{0x0d, true, "org.bluez.Error.InvalidValueLength"},
		{0x80, true, "org.bluez.Error.Failed"},
		{0x9f, false, "org.bluez.Error.Failed"},
		{0x0e, false, "org.bluez.Error.Rejected"},
	} {
		name, msg := ServerError(c.code, c.write)
		if name != c.name {
			t.Errorf("ServerError(%#x) = %q", c.code, name)
		}
		if c.code >= 0x80 && msg != "0x"+[]string{"80", "9f"}[map[int]int{0x80: 0, 0x9f: 1}[c.code]] {
			t.Errorf("ServerError(%#x)'s message %q: BlueZ reads the code from it", c.code, msg)
		}
	}
}

func TestPaths(t *testing.T) {
	if a := AddressFromPath("/org/bluez/hci0/dev_AA_BB_CC_DD_EE_0F"); a != "AA:BB:CC:DD:EE:0F" {
		t.Errorf("AddressFromPath: %q", a)
	}
	if a := AddressFromPath("/org/bluez/hci0"); a != "" {
		t.Errorf("AddressFromPath of an adapter: %q", a)
	}
	for p, want := range map[string]string{
		"/org/bluez/hci0/dev_AA_BB_CC_DD_EE_0F/service000a/char000b/desc000d": "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_0F",
		"/org/bluez/hci0/dev_AA_BB_CC_DD_EE_0F":                               "/org/bluez/hci0/dev_AA_BB_CC_DD_EE_0F",
		"/org/bluez/hci0":                                                     "",
	} {
		if got := DevicePath(p); got != want {
			t.Errorf("DevicePath(%q) = %q", p, got)
		}
	}
}

func TestAdvertisement(t *testing.T) {
	props := map[string]any{
		"Adapter":          "/org/bluez/hci0",
		"Address":          "AA:BB:CC:DD:EE:FF",
		"Name":             "Polar H10",
		"Alias":            "Polar H10",
		"RSSI":             int16(-60),
		"TxPower":          int16(4),
		"UUIDs":            []any{"0000180d-0000-1000-8000-00805f9b34fb", "0000180F-0000-1000-8000-00805F9B34FB"},
		"ServiceData":      map[string]any{"0000feaa-0000-1000-8000-00805f9b34fb": []byte{1, 2}},
		"ManufacturerData": map[string]any{"76": []any{uint8(2), uint8(21)}},
		"Connected":        false,
	}
	d := ParseDevice(props)
	a := d.Advertisement(1234)
	tx := 4
	want := spec.Advertisement{
		Device: spec.Device{ID: "AA:BB:CC:DD:EE:FF", Name: "Polar H10"}, RSSI: -60, LocalName: "Polar H10",
		Services:         []string{"0000180d-0000-1000-8000-00805f9b34fb", "0000180f-0000-1000-8000-00805f9b34fb"},
		ServiceData:      map[string][]byte{"0000feaa-0000-1000-8000-00805f9b34fb": {1, 2}},
		ManufacturerData: map[string][]byte{"76": {2, 21}},
		TxPower:          &tx, Time: 1234,
	}
	if !reflect.DeepEqual(a, want) {
		t.Errorf("advertisement:\n got %+v\nwant %+v", a, want)
	}
	if !d.Matches(nil) || !d.Matches([]string{spec.Normalize("180f")}) || !d.Matches([]string{spec.Normalize("feaa")}) || d.Matches([]string{spec.Normalize("1810")}) {
		t.Error("Matches")
	}
	// No name: the alias BlueZ makes of the address isn't one.
	anon := ParseDevice(map[string]any{"Address": "AA:BB:CC:DD:EE:FF", "Alias": "AA-BB-CC-DD-EE-FF"})
	if s := anon.Spec(); s.Name != "" || s.ID != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("anonymous: %+v", s)
	}
	if anon.Advertisement(0).TxPower != nil || anon.Advertisement(0).Connectable != nil {
		t.Error("TxPower and Connectable, unknown, must be nil")
	}
	// The user's alias names it.
	if s := ParseDevice(map[string]any{"Address": "AA:BB:CC:DD:EE:FF", "Name": "HR", "Alias": "My strap"}).Spec(); s.Name != "My strap" {
		t.Errorf("alias: %+v", s)
	}
}

func TestFilter(t *testing.T) {
	uuids, dup := Filter([]spec.ScanArgs{{Services: []string{"180d"}}, {Services: []string{"180f", "180D"}, Duplicates: true}})
	if !reflect.DeepEqual(uuids, []string{spec.Normalize("180d"), spec.Normalize("180f")}) || !dup {
		t.Errorf("Filter: %v %v", uuids, dup)
	}
	uuids, dup = Filter([]spec.ScanArgs{{Services: []string{"180d"}}, {}})
	if uuids != nil || dup {
		t.Errorf("Filter with a scan of everything: %v %v", uuids, dup)
	}
}
