// Package bluez is what the Linux host's "bluetooth" module
// (host/linux/bluetooth*.go) makes of BlueZ's D-Bus API (org.bluez:
// https://github.com/bluez/bluez/tree/master/doc), without D-Bus: its
// errors as internal/bluetoothspec's, the characteristics' flags as the
// spec's property bits and back, a Device1's properties as an
// advertisement, the scans' merged discovery filter. Plain Go, so it's
// tested anywhere.
package bluez

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
)

// Error is a D-Bus error, as GIO's GError message has it: GDBus writes
// a remote error as "GDBus.Error:org.bluez.Error.Failed: message", and
// internal/gtk's GError puts the name in front again ("name: GDBus.Error:
// name: message"). Name is "" for a local error (GIO's "Timeout was
// reached").
func Error(err error) (name, msg string) {
	if err == nil {
		return "", ""
	}
	s := err.Error()
	if i := strings.LastIndex(s, "GDBus.Error:"); i >= 0 {
		s = s[i+len("GDBus.Error:"):]
	}
	if n, m, ok := strings.Cut(s, ": "); ok && isErrorName(n) {
		return n, m
	}
	if isErrorName(s) {
		return s, ""
	}
	return "", s
}

// isErrorName reports whether s looks like a D-Bus error name: dotted
// elements, no spaces.
func isErrorName(s string) bool {
	return strings.Count(s, ".") >= 2 && !strings.ContainsAny(s, " \t:")
}

// D-Bus errors that say BlueZ isn't there, or the app may not talk to it.
const (
	errServiceUnknown = "org.freedesktop.DBus.Error.ServiceUnknown"
	errNameHasNoOwner = "org.freedesktop.DBus.Error.NameHasNoOwner"
	errAccessDenied   = "org.freedesktop.DBus.Error.AccessDenied"
	errUnknownObject  = "org.freedesktop.DBus.Error.UnknownObject"
	errUnknownMethod  = "org.freedesktop.DBus.Error.UnknownMethod"
	errNoReply        = "org.freedesktop.DBus.Error.NoReply"
	errTimeout        = "org.freedesktop.DBus.Error.Timeout"
	errTimedOut       = "org.freedesktop.DBus.Error.TimedOut"
)

// BusState is the adapter's state when talking to BlueZ failed with
// err: "unsupported" when bluetoothd isn't on the bus, "denied" when the
// bus doesn't let the app talk to it (a Flatpak without
// --system-talk-name=org.bluez, a policy), "" for another error.
func BusState(err error) string {
	name, msg := Error(err)
	switch name {
	case errServiceUnknown, errNameHasNoOwner:
		return spec.StateUnsupported
	case errAccessDenied:
		return spec.StateDenied
	}
	if name == "" && strings.Contains(msg, "AccessDenied") {
		return spec.StateDenied
	}
	return ""
}

// ATT error codes BlueZ's errors stand for.
const (
	attReadNotPermitted   = 0x02
	attWriteNotPermitted  = 0x03
	attAuthentication     = 0x05
	attNotSupported       = 0x06
	attInvalidOffset      = 0x07
	attAuthorization      = 0x08
	attInvalidLength      = 0x0d
	attUnlikely           = 0x0e
	attCCCDImproper       = 0xfd
	attInProgress         = 0xfe
	attApplicationErrorLo = 0x80
	attApplicationErrorHi = 0x9f
)

// ClientError is the spec's error for a GATT client call's (ReadValue,
// WriteValue, StartNotify on a device's attribute) D-Bus error: BlueZ's
// gatt-client.c turns the device's ATT errors into its own D-Bus errors
// (create_gatt_dbus_error), which this turns back. write: the call
// wrote (a NotPermitted is the write's then).
func ClientError(err error, write bool) string {
	name, msg := Error(err)
	att := func(code int) string {
		if msg == "" {
			msg = "refused"
		}
		return fmt.Sprintf("att %d: %s", code, msg)
	}
	switch name {
	case "org.bluez.Error.NotPermitted":
		switch {
		case strings.HasPrefix(msg, "Read not permitted"):
			return att(attReadNotPermitted)
		case strings.HasPrefix(msg, "Write not permitted"):
			return att(attWriteNotPermitted)
		case strings.HasPrefix(msg, "Not paired"):
			return att(attAuthentication)
		}
		if write {
			return att(attWriteNotPermitted)
		}
		return att(attReadNotPermitted)
	case "org.bluez.Error.NotAuthorized":
		return att(attAuthorization)
	case "org.bluez.Error.InvalidOffset":
		return att(attInvalidOffset)
	case "org.bluez.Error.InvalidValueLength":
		return att(attInvalidLength)
	case "org.bluez.Error.InvalidArguments":
		switch {
		case strings.HasPrefix(msg, "Invalid offset"):
			return att(attInvalidOffset)
		case strings.HasPrefix(msg, "Invalid Length"), strings.HasPrefix(msg, "Invalid length"):
			return att(attInvalidLength)
		}
	case "org.bluez.Error.NotSupported":
		return att(attNotSupported)
	case "org.bluez.Error.NotConnected":
		return "disconnected"
	case "org.bluez.Error.Failed":
		// "Operation failed with ATT error: 0x%02x": the device's own.
		if _, code, ok := strings.Cut(msg, "ATT error: "); ok {
			if n, err := strconv.ParseUint(strings.TrimSpace(code), 0, 8); err == nil && n > 0 {
				return att(int(n))
			}
		}
		if strings.HasPrefix(msg, "Not connected") {
			return "disconnected"
		}
	case errUnknownObject, errUnknownMethod:
		// The attribute's object went: the device disconnected (BlueZ
		// takes a device's GATT objects down with its connection).
		return "disconnected"
	}
	return common(name, msg)
}

// common is the spec's error for the errors any call may get.
func common(name, msg string) string {
	switch name {
	case errServiceUnknown, errNameHasNoOwner:
		return spec.StateUnsupported
	case errAccessDenied:
		return spec.StateDenied
	case "org.bluez.Error.NotReady":
		return spec.StateOff
	case errNoReply, errTimeout, errTimedOut:
		return "timeout"
	case "":
		if strings.Contains(msg, "Timeout was reached") || strings.Contains(msg, "timed out") {
			return "timeout"
		}
		if msg == "" {
			return "failed"
		}
		return msg
	}
	if msg == "" {
		return name
	}
	return msg
}

// ConnectError is the spec's error for Device1.Connect's: BlueZ's
// Failed carries the mgmt status's words ("le-connection-abort-by-local"
// when the kernel gave up creating the connection: its timeout).
func ConnectError(err error) string {
	name, msg := Error(err)
	if name == "org.bluez.Error.Failed" || name == "org.bluez.Error.ConnectionAttemptFailed" {
		switch msg {
		case "le-connection-abort-by-local", "br-connection-page-timeout", "Page Timeout", "Connection timed out":
			return "timeout"
		case "br-connection-canceled", "le-connection-canceled", "Connection canceled":
			return "canceled"
		}
	}
	if name == "org.bluez.Error.DoesNotExist" || name == errUnknownObject || name == errUnknownMethod {
		return "not found"
	}
	return common(name, msg)
}

// Retry reports whether a connection that waits for the device
// (ConnectArgs.AutoConnect) tries again after Device1.Connect's err:
// the device wasn't in range, or didn't answer in time.
func Retry(err error) bool {
	name, msg := Error(err)
	switch name {
	case "org.bluez.Error.Failed", "org.bluez.Error.ConnectionAttemptFailed", "org.bluez.Error.NotAvailable",
		"org.bluez.Error.InProgress", errNoReply, errTimeout, errTimedOut:
		return !strings.Contains(msg, "canceled")
	case "":
		return strings.Contains(msg, "Timeout was reached")
	}
	return false
}

// Reason is the spec's disconnection reason for the reason BlueZ's
// Device1.Disconnected signal names (BlueZ 5.82 and later).
func Reason(name string) string {
	switch name {
	case "org.bluez.Reason.Timeout":
		return spec.ReasonTimeout
	case "org.bluez.Reason.Remote":
		return spec.ReasonRemote
	case "org.bluez.Reason.Local", "org.bluez.Reason.Suspend":
		return spec.ReasonLocal
	case "", "org.bluez.Reason.Unknown":
		return spec.ReasonUnknown
	}
	return spec.ReasonFailed // Authentication, and any BlueZ adds
}

// Properties is a characteristic's Flags as the spec's property bits
// (an access that needs encryption, "encrypt-read" and the like, is the
// access's property).
func Properties(flags []string) int {
	p := 0
	for _, f := range flags {
		switch f {
		case "broadcast":
			p |= spec.PropBroadcast
		case "read", "encrypt-read", "encrypt-authenticated-read", "secure-read":
			p |= spec.PropRead
		case "write-without-response":
			p |= spec.PropWriteNoResponse
		case "write", "encrypt-write", "encrypt-authenticated-write", "secure-write":
			p |= spec.PropWrite
		case "notify":
			p |= spec.PropNotify
		case "indicate":
			p |= spec.PropIndicate
		case "authenticated-signed-writes":
			p |= spec.PropSignedWrite
		case "extended-properties", "reliable-write", "writable-auxiliaries":
			p |= spec.PropExtendedProperties
		}
	}
	return p
}

// Flags is a local characteristic's Flags, of its properties and
// permissions: an encrypted permission makes its access "encrypt-read"
// ("encrypt-write"), which BlueZ takes as the property too.
func Flags(props, perms int) []string {
	var out []string
	if props&spec.PropBroadcast != 0 {
		out = append(out, "broadcast")
	}
	if props&spec.PropRead != 0 {
		if perms&spec.PermReadEncrypted != 0 {
			out = append(out, "encrypt-read")
		} else {
			out = append(out, "read")
		}
	}
	if props&spec.PropWriteNoResponse != 0 {
		out = append(out, "write-without-response")
	}
	if props&spec.PropWrite != 0 {
		if perms&spec.PermWriteEncrypted != 0 {
			out = append(out, "encrypt-write")
		} else {
			out = append(out, "write")
		}
	}
	if props&spec.PropNotify != 0 {
		out = append(out, "notify")
	}
	if props&spec.PropIndicate != 0 {
		out = append(out, "indicate")
	}
	if props&spec.PropSignedWrite != 0 {
		out = append(out, "authenticated-signed-writes")
	}
	if props&spec.PropExtendedProperties != 0 {
		out = append(out, "extended-properties")
	}
	return out
}

// ServerError is the D-Bus error a local attribute's ReadValue or
// WriteValue answers to give the central the ATT error code: BlueZ's
// gatt-database.c turns them back (dbus_error_to_att_ecode). A code it
// has no error for is answered as the application error it's nearest
// to: 0x80 to 0x9f as themselves, the rest as Unlikely Error (0x0e).
func ServerError(code int, write bool) (name, msg string) {
	switch {
	case code >= attApplicationErrorLo && code <= attApplicationErrorHi:
		return "org.bluez.Error.Failed", fmt.Sprintf("0x%02x", code)
	case code == attReadNotPermitted && !write, code == attWriteNotPermitted && write:
		return "org.bluez.Error.NotPermitted", "not permitted"
	case code == attNotSupported:
		return "org.bluez.Error.NotSupported", "not supported"
	case code == attAuthorization:
		return "org.bluez.Error.NotAuthorized", "not authorized"
	case code == attInvalidLength:
		return "org.bluez.Error.InvalidValueLength", "invalid value length"
	case code == attInvalidOffset:
		return "org.bluez.Error.InvalidOffset", "invalid offset"
	case code == attInProgress:
		return "org.bluez.Error.InProgress", "in progress"
	case code == attCCCDImproper:
		return "org.bluez.Error.ImproperlyConfigured", "improperly configured"
	}
	// Any other name is Unlikely Error to BlueZ.
	return "org.bluez.Error.Rejected", fmt.Sprintf("ATT error 0x%02x", code)
}

// AddressFromPath is the address of a device's object path
// (/org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF), "" if it isn't one.
func AddressFromPath(path string) string {
	i := strings.LastIndex(path, "/dev_")
	if i < 0 {
		return ""
	}
	a := path[i+len("/dev_"):]
	if j := strings.IndexByte(a, '/'); j >= 0 {
		a = a[:j]
	}
	if len(a) != 17 {
		return ""
	}
	return strings.ReplaceAll(a, "_", ":")
}

// DevicePath is the device's object path an attribute's (or the
// device's own) path is under: /org/bluez/hci0/dev_AA_BB_CC_DD_EE_FF of
// .../dev_AA_BB_CC_DD_EE_FF/service000a/char000b; "" if none.
func DevicePath(path string) string {
	i := strings.Index(path, "/dev_")
	if i < 0 {
		return ""
	}
	if j := strings.IndexByte(path[i+1:], '/'); j >= 0 {
		return path[:i+1+j]
	}
	return path
}

// Device is a Device1's properties, as they're read.
type Device struct {
	Adapter          string
	Address          string
	Name, Alias      string
	RSSI, TxPower    *int
	UUIDs            []string // normalized
	ServiceData      map[string][]byte
	ManufacturerData map[string][]byte
	Connected        bool
	ServicesResolved bool
}

// ParseDevice reads a Device1's properties, as internal/gtk's variant
// reader gives them (strings, int16s, []any, map[string]any, []byte for
// a byte array).
func ParseDevice(p map[string]any) Device {
	d := Device{}
	d.Adapter, _ = p["Adapter"].(string)
	d.Address, _ = p["Address"].(string)
	d.Name, _ = p["Name"].(string)
	d.Alias, _ = p["Alias"].(string)
	if n, ok := Int(p["RSSI"]); ok {
		d.RSSI = &n
	}
	if n, ok := Int(p["TxPower"]); ok {
		d.TxPower = &n
	}
	d.UUIDs = normalized(Strings(p["UUIDs"]))
	d.ServiceData = byteMap(p["ServiceData"], true)
	d.ManufacturerData = byteMap(p["ManufacturerData"], false)
	d.Connected, _ = p["Connected"].(bool)
	d.ServicesResolved, _ = p["ServicesResolved"].(bool)
	return d
}

// Spec is the device as the spec has it: its address and name (the
// user's alias for it, when they gave one; else its own).
func (d Device) Spec() spec.Device {
	name := d.Name
	if d.Alias != "" && d.Alias != strings.ReplaceAll(d.Address, ":", "-") {
		name = d.Alias
	}
	return spec.Device{ID: d.Address, Name: name}
}

// Advertisement is what the device advertised, as BlueZ keeps it from
// its advertisements (and scan responses): connectable isn't one of
// them. now is the time in milliseconds.
func (d Device) Advertisement(now int64) spec.Advertisement {
	a := spec.Advertisement{Device: d.Spec(), LocalName: d.Name, Services: d.UUIDs,
		ServiceData: d.ServiceData, ManufacturerData: d.ManufacturerData, TxPower: d.TxPower, Time: now}
	if d.RSSI != nil {
		a.RSSI = *d.RSSI
	}
	return a
}

// Matches reports whether the device advertises one of the services
// (normalized; none: any device), in its UUIDs or its service data.
func (d Device) Matches(services []string) bool {
	if len(services) == 0 {
		return true
	}
	for _, s := range services {
		for _, u := range d.UUIDs {
			if u == s {
				return true
			}
		}
		if _, ok := d.ServiceData[s]; ok {
			return true
		}
	}
	return false
}

// Filter is the discovery filter that serves every scan: the union of
// their services (none if any scan wants every device), duplicates if
// any wants them.
func Filter(scans []spec.ScanArgs) (uuids []string, duplicates bool) {
	set := map[string]bool{}
	all := false
	for _, s := range scans {
		duplicates = duplicates || s.Duplicates
		if len(s.Services) == 0 {
			all = true
		}
		for _, u := range s.Services {
			set[spec.Normalize(u)] = true
		}
	}
	if all {
		return nil, duplicates
	}
	for u := range set {
		uuids = append(uuids, u)
	}
	sort.Strings(uuids)
	return uuids, duplicates
}

// Int is an integer property's value.
func Int(v any) (int, bool) {
	switch n := v.(type) {
	case int16:
		return int(n), true
	case uint16:
		return int(n), true
	case int32:
		return int(n), true
	case uint32:
		return int(n), true
	case uint8:
		return int(n), true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// Strings is an "as" property's value.
func Strings(v any) []string {
	switch a := v.(type) {
	case []string:
		return a
	case []any:
		out := make([]string, 0, len(a))
		for _, s := range a {
			if s, ok := s.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// Bytes is an "ay" property's value (a []byte, or []any of bytes).
func Bytes(v any) []byte {
	switch a := v.(type) {
	case []byte:
		return a
	case []any:
		out := make([]byte, 0, len(a))
		for _, b := range a {
			if b, ok := b.(uint8); ok {
				out = append(out, b)
			}
		}
		return out
	}
	return nil
}

func normalized(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = spec.Normalize(s)
	}
	return out
}

// byteMap is ServiceData's a{sv} (uuids: its keys normalized) or
// ManufacturerData's a{qv} (keys the company IDs in decimal), of byte
// arrays.
func byteMap(v any, uuids bool) map[string][]byte {
	m, _ := v.(map[string]any)
	if len(m) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(m))
	for k, b := range m {
		if uuids {
			k = spec.Normalize(k)
		}
		out[k] = Bytes(b)
	}
	return out
}
