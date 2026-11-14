package windows

import (
	"encoding/binary"
	"fmt"
	"slices"
	"strconv"
	"strings"

	spec "github.com/egoist/asuka/internal/bluetoothspec"
)

// asuka/sdk/bluetooth's parts apart from WinRT (bluetooth.go): addresses,
// advertisements' data sections, a device's advertisement and scan
// response put together, errors as the wire has them. They build
// anywhere, so they're tested anywhere.

// btAddress is a 48-bit Bluetooth address as Device.ID has it:
// "AA:BB:CC:DD:EE:FF".
func btAddress(a uint64) string {
	const hex = "0123456789ABCDEF"
	var b [17]byte
	for i := range 6 {
		v := byte(a >> (8 * (5 - i)))
		b[3*i], b[3*i+1] = hex[v>>4], hex[v&15]
		if i < 5 {
			b[3*i+2] = ':'
		}
	}
	return string(b[:])
}

// btParseAddress reads an address: six bytes in hex, with ':' or '-'
// between them, or twelve hex digits.
func btParseAddress(s string) (uint64, bool) {
	s = strings.TrimSpace(s)
	if len(s) == 17 {
		var sep byte
		var out []byte
		for i := 0; i < 17; i++ {
			if i%3 == 2 {
				if sep == 0 {
					sep = s[i]
				}
				if s[i] != sep || (sep != ':' && sep != '-') {
					return 0, false
				}
				continue
			}
			out = append(out, s[i])
		}
		s = string(out)
	}
	if len(s) != 12 {
		return 0, false
	}
	v, err := strconv.ParseUint(s, 16, 64)
	return v, err == nil
}

// btCentralID is a central's Device.ID from the device ID Windows gives
// its session ("BluetoothLE#BluetoothLE<this computer's address>-<its
// address>"): its address, or the ID itself if it has none.
func btCentralID(deviceID string) string {
	if i := strings.LastIndexByte(deviceID, '-'); i >= 0 {
		if a, ok := btParseAddress(deviceID[i+1:]); ok {
			return btAddress(a)
		}
	}
	return deviceID
}

// btSection is an advertisement's data section (its AD type, its data).
type btSection struct {
	typ  byte
	data []byte
}

// btAd is what an advertisement's (or a scan response's) data sections
// say, as the wire has it.
type btAd struct {
	name                      string
	services, solicited       []string
	serviceData, manufacturer map[string][]byte
	txPower                   *int
}

// btUUIDLE is a UUID as an advertisement writes it (little endian: 2, 4
// or 16 bytes), in the wire's form.
func btUUIDLE(b []byte) string {
	u := spec.Base
	switch len(b) {
	case 2:
		u[2], u[3] = b[1], b[0]
	case 4:
		u[0], u[1], u[2], u[3] = b[3], b[2], b[1], b[0]
	case 16:
		for i := range 16 {
			u[i] = b[15-i]
		}
	default:
		return ""
	}
	return spec.FormatUUID(u)
}

// btParseSections reads data sections (the Bluetooth Core Supplement's
// AD types).
func btParseSections(secs []btSection) btAd {
	var a btAd
	uuids := func(dst *[]string, b []byte, n int) {
		for i := 0; i+n <= len(b); i += n {
			if u := btUUIDLE(b[i : i+n]); u != "" && !slices.Contains(*dst, u) {
				*dst = append(*dst, u)
			}
		}
	}
	data := func(b []byte, n int) {
		if len(b) < n {
			return
		}
		if a.serviceData == nil {
			a.serviceData = map[string][]byte{}
		}
		a.serviceData[btUUIDLE(b[:n])] = append([]byte{}, b[n:]...)
	}
	for _, s := range secs {
		switch s.typ {
		case 0x02, 0x03: // incomplete, complete 16-bit service UUIDs
			uuids(&a.services, s.data, 2)
		case 0x04, 0x05: // 32-bit
			uuids(&a.services, s.data, 4)
		case 0x06, 0x07: // 128-bit
			uuids(&a.services, s.data, 16)
		case 0x08, 0x09: // shortened, complete local name
			if s.typ == 0x09 || a.name == "" {
				a.name = strings.TrimRight(string(s.data), "\x00")
			}
		case 0x0a: // Tx Power Level
			if len(s.data) == 1 {
				p := int(int8(s.data[0]))
				a.txPower = &p
			}
		case 0x14: // solicitation: 16-bit
			uuids(&a.solicited, s.data, 2)
		case 0x1f: // 32-bit
			uuids(&a.solicited, s.data, 4)
		case 0x15: // 128-bit
			uuids(&a.solicited, s.data, 16)
		case 0x16: // service data: 16-bit UUID
			data(s.data, 2)
		case 0x20: // 32-bit
			data(s.data, 4)
		case 0x21: // 128-bit
			data(s.data, 16)
		case 0xff: // manufacturer specific: the company first, little endian
			if len(s.data) >= 2 {
				if a.manufacturer == nil {
					a.manufacturer = map[string][]byte{}
				}
				a.manufacturer[strconv.Itoa(int(binary.LittleEndian.Uint16(s.data)))] = append([]byte{}, s.data[2:]...)
			}
		}
	}
	return a
}

// BluetoothLEAdvertisementType's values.
const (
	btAdvConnectableUndirected    = 0
	btAdvConnectableDirected      = 1
	btAdvScannableUndirected      = 2
	btAdvNonConnectableUndirected = 3
	btAdvScanResponse             = 4
)

// btPacket is one advertising packet heard: an advertisement, or a scan
// response.
type btPacket struct {
	ad          btAd
	rssi        int
	time        int64
	response    bool
	scannable   bool  // a scan response may follow
	typ         int32 // BluetoothLEAdvertisementType
	connectable *bool // Windows 10 2004's IsConnectable (nil before: from typ)
}

// btHeard is a device as a scan hears it: its last advertisement and
// its last scan response, put together (Windows delivers them apart; a
// scan gives them as one, as the other platforms do).
type btHeard struct {
	adv, rsp       btAd
	hasAdv, hasRsp bool
	advs           int // advertisements heard without a scan response
	rssi           int
	time           int64
	scannable      bool
	connectable    *bool
}

func (h *btHeard) take(p btPacket) {
	h.rssi, h.time = p.rssi, p.time
	if p.response {
		h.rsp, h.hasRsp = p.ad, true
		return
	}
	h.adv, h.hasAdv = p.ad, true
	h.scannable = p.scannable
	if !h.hasRsp {
		h.advs++
	}
	switch {
	case p.connectable != nil:
		c := *p.connectable
		h.connectable = &c
	case p.typ == btAdvConnectableUndirected || p.typ == btAdvConnectableDirected:
		c := true
		h.connectable = &c
	case p.typ == btAdvScannableUndirected || p.typ == btAdvNonConnectableUndirected:
		c := false
		h.connectable = &c
	}
}

// complete reports whether the device is heard whole: its scan response
// came, or none will (not scannable), or it didn't come with the first
// advertisement (the device doesn't answer the scan's requests).
func (h *btHeard) complete() bool {
	return h.hasRsp || h.hasAdv && (!h.scannable || h.advs >= 2)
}

// advertisement is the device's, put together.
func (h *btHeard) advertisement(id string) *spec.Advertisement {
	a := &spec.Advertisement{Device: spec.Device{ID: id}, RSSI: h.rssi, Time: h.time, Connectable: h.connectable}
	parts := []btAd{}
	if h.hasAdv {
		parts = append(parts, h.adv)
	}
	if h.hasRsp {
		parts = append(parts, h.rsp)
	}
	for _, p := range parts {
		if a.LocalName == "" {
			a.LocalName = p.name
		}
		for _, s := range p.services {
			if !slices.Contains(a.Services, s) {
				a.Services = append(a.Services, s)
			}
		}
		for _, s := range p.solicited {
			if !slices.Contains(a.Solicited, s) {
				a.Solicited = append(a.Solicited, s)
			}
		}
		for k, v := range p.serviceData {
			if a.ServiceData == nil {
				a.ServiceData = map[string][]byte{}
			}
			a.ServiceData[k] = v
		}
		for k, v := range p.manufacturer {
			if a.ManufacturerData == nil {
				a.ManufacturerData = map[string][]byte{}
			}
			a.ManufacturerData[k] = v
		}
		if a.TxPower == nil && p.txPower != nil {
			t := *p.txPower
			a.TxPower = &t
		}
	}
	a.Device.Name = a.LocalName
	return a
}

// btMatches reports whether a scan for services hears the advertisement
// (none: every device).
func btMatches(a *spec.Advertisement, services []string) bool {
	if len(services) == 0 {
		return true
	}
	for _, s := range services {
		if slices.Contains(a.Services, spec.Normalize(s)) {
			return true
		}
	}
	return false
}

var btATTNames = map[int]string{
	0x01: "invalid handle", 0x02: "read not permitted", 0x03: "write not permitted",
	0x04: "invalid PDU", 0x05: "insufficient authentication", 0x06: "request not supported",
	0x07: "invalid offset", 0x08: "insufficient authorization", 0x09: "prepare queue full",
	0x0a: "attribute not found", 0x0b: "attribute not long", 0x0c: "insufficient encryption key size",
	0x0d: "invalid attribute value length", 0x0e: "unlikely error", 0x0f: "insufficient encryption",
	0x10: "unsupported group type", 0x11: "insufficient resources",
}

// btATT is an ATT error as the wire has it: "att <n>: <what>".
func btATT(code int) string {
	name := btATTNames[code]
	if name == "" {
		name = fmt.Sprintf("error %#02x", code)
	}
	return fmt.Sprintf("att %d: %s", code, name)
}

// btGattError is a GATT operation's error as the wire has it, "" for
// none: status a GattCommunicationStatus (-1: the operation failed, hr
// its HRESULT), proto its ATT error (-1: none).
func btGattError(status int32, proto int, hr uint32) string {
	switch status {
	case 0: // Success
		return ""
	case 1: // Unreachable
		return "disconnected"
	case 2: // ProtocolError
		if proto < 0 {
			proto = 0x0e
		}
		return btATT(proto)
	case 3: // AccessDenied
		return "denied"
	}
	switch hr {
	case 0x80000013, 0x8007048f, 0x800710df: // RO_E_CLOSED, ERROR_DEVICE_NOT_CONNECTED, ERROR_DEVICE_NOT_AVAILABLE
		return "disconnected"
	case 0x80070005: // E_ACCESSDENIED
		return "denied"
	case 0:
		return "failed"
	}
	return fmt.Sprintf("failed: %#x", hr)
}

// btError is a BluetoothError as the wire has it, "" for Success.
func btError(e int32) string {
	switch e {
	case 0:
		return ""
	case 1: // RadioNotAvailable
		return spec.StateOff
	case 2:
		return "failed: another app uses the radio"
	case 3:
		return "disconnected"
	case 5, 7, 8: // DisabledByPolicy, DisabledByUser, ConsentRequired
		return spec.StateDenied
	case 6, 9: // NotSupported, TransportNotSupported
		return "not available: this Bluetooth adapter can't"
	}
	return "failed"
}

// btRadioState is a RadioState as the wire has it.
func btRadioState(st int32) string {
	switch st {
	case 1:
		return spec.StateOn
	case 2, 3: // Off, Disabled
		return spec.StateOff
	}
	return spec.StateUnknown
}

// btCCCD is what "subscribe" writes to a characteristic's CCCD (1
// notifications, 2 indications), or false where it has neither.
func btCCCD(props int, indicate bool) (int32, bool) {
	switch {
	case props&(spec.PropNotify|spec.PropIndicate) == 0:
		return 0, false
	case indicate && props&spec.PropIndicate != 0, props&spec.PropNotify == 0:
		return 2, true
	}
	return 1, true
}

// btChooserSelectors are the AQS filters of the device picker: bases
// (Bluetooth LE devices, paired and not), each narrowed to a filter's
// name where it has one. A filter without a name takes every device:
// Windows' picker can't filter by what devices advertise.
func btChooserSelectors(bases []string, filters []spec.Filter) []string {
	if len(filters) == 0 {
		return bases
	}
	var out []string
	for _, f := range filters {
		var clause string
		quote := func(s string) string { return `"` + strings.ReplaceAll(s, `"`, "") + `"` }
		switch {
		case f.Name != "":
			clause = " AND System.ItemNameDisplay:=" + quote(f.Name)
		case f.NamePrefix != "":
			clause = " AND System.ItemNameDisplay:~<" + quote(f.NamePrefix)
		default:
			return bases
		}
		for _, b := range bases {
			out = append(out, "("+b+")"+clause)
		}
	}
	return out
}

// btDiff is what changed from old to now: the IDs added (on) and gone
// (off), in order.
func btDiff(old, now []string) (on, off []string) {
	for _, n := range now {
		if !slices.Contains(old, n) && !slices.Contains(on, n) {
			on = append(on, n)
		}
	}
	for _, o := range old {
		if !slices.Contains(now, o) && !slices.Contains(off, o) {
			off = append(off, o)
		}
	}
	return on, off
}

// btWindowsDescriptor is how Windows takes a local characteristic's
// descriptor of uuid: "" for one CreateDescriptorAsync makes, "user"
// (the User Description: the parameters' UserDescription), "format"
// (Presentation Format: its PresentationFormats), "skip" (those the
// system makes itself: the Extended Properties, the CCCD, the SCCD).
func btWindowsDescriptor(uuid string) string {
	switch spec.Normalize(uuid) {
	case spec.Normalize("2901"):
		return "user"
	case spec.Normalize("2904"):
		return "format"
	case spec.Normalize("2900"), spec.Normalize("2902"), spec.Normalize("2903"):
		return "skip"
	}
	return ""
}
